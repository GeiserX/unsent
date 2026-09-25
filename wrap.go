package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"golang.org/x/term"
)

// saveInterval is how often the shadow screen is read for a new draft.
const saveInterval = 400 * time.Millisecond

// wrap runs args[0] inside a pseudo-terminal, passes every byte between it
// and the terminal (in, out) untouched and keeps the text in the agent's
// input box saved on disk.
func wrap(args []string, in, out *os.File) int {
	bin, err := exec.LookPath(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "unsent: %v\n", err)
		return 127
	}
	if !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		// Piped (git diff | claude -p ...) or redirected: the agent draws no
		// input box, and a pseudo-terminal would turn the pipe into
		// keystrokes. Get out of the way.
		if err := execAgent(bin, args); err != nil {
			fmt.Fprintf(os.Stderr, "unsent: %v\n", err)
			return 126
		}
		return 0
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unsent: %v\n", err)
		return 1
	}
	cwd, _ := os.Getwd()
	noticeOrphans(st, cwd)

	cmd := exec.Command(bin, args[1:]...)
	cols, rows := termSize(in)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		fmt.Fprintf(os.Stderr, "unsent: %v\n", err)
		return 1
	}
	defer ptmx.Close()

	cooked, err := term.MakeRaw(int(in.Fd()))
	if err == nil {
		defer term.Restore(int(in.Fd()), cooked)
	}

	s := &session{
		rec:    newRecord(args, cwd),
		store:  st,
		screen: vt.NewEmulator(cols, rows),
		pastes: &pasteTracker{},
		read:   extractorFor(args[0]),
	}
	if s.read == nil {
		fmt.Fprintf(os.Stderr, "unsent: no reader for %q yet, running it without saving drafts\n", args[0])
	}
	if s.read != nil {
		if err := st.hold(s.rec); err != nil {
			// Without the lock another unsent would take this live session
			// for a dead one and could clean up its files.
			fmt.Fprintf(os.Stderr, "unsent: %v; running without saving drafts\n", err)
			s.read = nil
		}
	}
	defer st.release()
	// The emulator answers terminal queries (cursor position, device
	// attributes) into its own pipe. The real terminal already answers the
	// agent, so these replies are drained and dropped.
	go io.Copy(io.Discard, s.screen)

	output := make(chan []byte, 4096)
	go s.feedScreen(output)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				out.Write(buf[:n])
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				output <- chunk
			}
			if err != nil {
				close(output)
				return
			}
		}
	}()
	suspend := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := in.Read(buf)
			if n == 1 && buf[0] == ctrlZ && !s.pastes.inPaste() {
				suspend <- struct{}{}
				continue
			}
			if n > 0 {
				// Pasted text can hold control bytes (Word pastes \x0b for
				// line breaks); only keys typed outside a paste count.
				s.deletes.add(s.pastes.feed(buf[:n]))
				ptmx.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	// Resizes and stop signals get separate channels: a burst of resizes
	// while a save runs must not crowd out a hang-up.
	resizes, sigs := make(chan os.Signal, 1), make(chan os.Signal, 4)
	signal.Notify(resizes, syscall.SIGWINCH)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
	defer signal.Stop(resizes)
	defer signal.Stop(sigs)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	tick := time.NewTicker(saveInterval)
	defer tick.Stop()

	for {
		select {
		case <-resizes:
			c, r := termSize(in)
			pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(c), Rows: uint16(r)})
			s.resize(c, r)
		case sig := <-sigs:
			// The window is closing or someone asked us to stop: save what
			// is in the box first, then pass the signal on.
			s.save()
			cmd.Process.Signal(sig)
			// In case it was stopped by Ctrl+Z: a stopped agent would never
			// act on the signal.
			cmd.Process.Signal(syscall.SIGCONT)
		case <-tick.C:
			s.save()
		case <-suspend:
			// The agent runs in its own terminal session, where the kernel
			// drops a suspend signal: it would print "suspended" and hang.
			// So the wrapper suspends instead, agent and all, and the shell's
			// fg brings both back.
			s.save()
			cmd.Process.Signal(syscall.SIGSTOP)
			if cooked != nil {
				term.Restore(int(in.Fd()), cooked)
			}
			stopSelf()
			if cooked != nil {
				term.MakeRaw(int(in.Fd()))
			}
			cmd.Process.Signal(syscall.SIGCONT)
			// Nudge the size so the agent repaints over whatever the shell
			// printed while it was away.
			c, r := termSize(in)
			pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(c), Rows: uint16(max(1, r-1))})
			time.Sleep(50 * time.Millisecond)
			pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(c), Rows: uint16(r)})
			s.resize(c, r)
		case err := <-done:
			// Let the last output reach the shadow screen before the final save.
			time.Sleep(50 * time.Millisecond)
			s.save()
			s.finish()
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				if st, ok := exit.Sys().(syscall.WaitStatus); ok && st.Signaled() {
					return 128 + int(st.Signal())
				}
				return exit.ExitCode()
			}
			return 0
		}
	}
}

const ctrlZ = 0x1a

// stopSelf suspends the wrapper the way Ctrl+Z suspends any job, and returns
// when the shell resumes it.
var stopSelf = func() {
	syscall.Kill(os.Getpid(), syscall.SIGTSTP)
}

// execAgent replaces the wrapper with the agent.
var execAgent = func(bin string, args []string) error {
	return syscall.Exec(bin, args, os.Environ())
}

// Agents wrap some redraws in "synchronized output" marks. The screen is
// not read between them, so a save never sees half of one.
var (
	frameBegin = []byte("\x1b[?2026h")
	frameEnd   = []byte("\x1b[?2026l")
)

// maxFrameWait is how long a save waits for an open frame to close. Frames
// close within milliseconds; while the user types they open again at once,
// so waiting for a quiet moment instead would put saves off indefinitely.
const maxFrameWait = 100 * time.Millisecond

// session holds the live state of one wrapped agent.
type session struct {
	mu        sync.Mutex
	screen    *vt.Emulator
	dirty     bool
	inFrame   bool
	frameTail []byte
	broken    bool
	// quietUntil holds saves back just after a resize, until the agent
	// has redrawn at the new size.
	quietUntil time.Time
	// stitched is true once the box has scrolled: from then on the draft
	// is partly inferred, and every loss keeps a safety copy.
	stitched bool
	deletes  deleteLog

	rec    *record
	store  *store
	pastes *pasteTracker
	stitch stitcher
	read   extractor
}

func (s *session) feedScreen(output <-chan []byte) {
	for chunk := range output {
		s.write(chunk)
	}
}

// write feeds one chunk of output to the shadow screen. If the emulator
// ever panics, saving stops but output keeps flowing: a broken shadow
// screen must never take the terminal down with it.
func (s *session) write(chunk []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken {
		return
	}
	defer func() {
		if recover() != nil {
			s.broken = true
		}
	}()
	s.trackFrames(chunk)
	s.screen.Write(chunk)
	s.dirty = true
}

// trackFrames notes whether the output so far ends inside a frame.
func (s *session) trackFrames(chunk []byte) {
	data := append(s.frameTail, chunk...)
	b, e := bytes.LastIndex(data, frameBegin), bytes.LastIndex(data, frameEnd)
	switch {
	case b > e:
		s.inFrame = true
	case e > b:
		s.inFrame = false
	}
	if keep := len(frameBegin) - 1; len(data) > keep {
		data = data[len(data)-keep:]
	}
	s.frameTail = append(s.frameTail[:0], data...)
}

func (s *session) resize(cols, rows int) {
	s.mu.Lock()
	s.screen.Resize(cols, rows)
	s.dirty = true
	s.quietUntil = time.Now().Add(resizeQuiet)
	s.mu.Unlock()
}

// resizeQuiet is how long saves wait after a resize for the redraw.
const resizeQuiet = 150 * time.Millisecond

// save reads the input box off the shadow screen and writes the draft to
// disk when it changed. A box that empties archives the draft it held.
func (s *session) save() {
	if s.read == nil {
		return
	}
	// If the agent is halfway through drawing, give it a moment to finish.
	for deadline := time.Now().Add(maxFrameWait); ; time.Sleep(5 * time.Millisecond) {
		s.mu.Lock()
		if !s.inFrame || time.Now().After(deadline) {
			break
		}
		s.mu.Unlock()
	}
	if !s.dirty || s.broken || time.Now().Before(s.quietUntil) {
		s.mu.Unlock()
		return
	}
	s.dirty = false
	scr := snapshot(s.screen)
	s.mu.Unlock()

	if dir := os.Getenv("UNSENT_DEBUG_DIR"); dir != "" {
		os.WriteFile(filepath.Join(dir, "screen.txt"), []byte(scr.String()), 0o600)
	}
	defer func() {
		// A bug reading the screen must not take the session down: stop
		// saving, say so once, and keep passing bytes through.
		if r := recover(); r != nil {
			s.mu.Lock()
			s.broken = true
			s.mu.Unlock()
			s.store.warn(fmt.Errorf("stopped saving after an internal error: %v", r))
		}
	}()
	v, ok := s.read(scr)
	if !ok {
		// The box is not on screen (a menu, a permission prompt, an editor):
		// keep the last draft we saw.
		return
	}
	v.deleted, v.deletedAhead = s.deletes.recent()
	before := s.stitch
	draft := s.pastes.expand(s.stitch.update(v))
	if dir := os.Getenv("UNSENT_DEBUG_DIR"); dir != "" {
		logView(filepath.Join(dir, "views.jsonl"), before, v, s.stitch)
	}
	if draft == s.rec.Draft {
		return
	}
	s.stitched = s.stitched || v.capped
	history, version := keepOld(s.rec.Draft, draft, s.stitched)
	var err error
	switch {
	case history:
		err = s.store.archive(s.rec)
	case version:
		err = s.store.keepVersion(s.rec)
	}
	if err != nil {
		// The old draft could not be kept: leave it in place rather than
		// replace it. The next save tries again.
		return
	}
	if draft == "" {
		s.pastes.reset()
		s.store.dropVersions(s.rec)
		s.stitched = false
	}
	s.deletes.spend(lostChars(s.rec.Draft, draft))
	s.rec.Draft = draft
	s.rec.Pastes = s.pastes.all()
	s.rec.Updated = time.Now()
	if err := s.store.write(s.rec); err != nil {
		s.store.warn(err)
	}
}

// finish runs when the agent exits. An empty box leaves nothing to recover,
// so its file goes away; a non-empty one stays for `unsent restore`.
func (s *session) finish() {
	s.rec.Ended = time.Now()
	if s.rec.Draft == "" {
		s.store.remove(s.rec)
		s.store.dropVersions(s.rec)
		return
	}
	if err := s.store.write(s.rec); err != nil {
		s.store.warn(err)
	}
}

// logView appends one save's view and the stitcher's state around it, for
// replaying a real session offline (UNSENT_DEBUG_DIR only).
func logView(path string, before stitcher, v view, after stitcher) {
	line, _ := json.Marshal(map[string]any{
		"text": before.text, "a": before.a, "b": before.b,
		"rows": v.rows, "cursor": v.cursor, "width": v.width, "capped": v.capped,
		"deleted": v.deleted, "after": after.text,
	})
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(append(line, '\n'))
}

// unlimited stands for "any amount" of deleted characters.
const unlimited = 1 << 20

// deleteWindow is how far back delete keys count for a save. The screen
// shows a key's effect a little after it is pressed, so a key pressed just
// before one save may only show at the next.
const deleteWindow = time.Second

// deleteLog remembers recent delete keys, and how much of each a save has
// already accounted for: a Backspace removes one character, once.
type deleteLog struct {
	mu   sync.Mutex
	keys []deleteKey
	now  func() time.Time // time.Now; tests move time by hand
}

type deleteKey struct {
	at    time.Time
	left  int64 // characters not yet accounted for; unlimited for Ctrl+W and co.
	ahead bool
}

func (d *deleteLog) clock() time.Time {
	if d.now != nil {
		return d.now()
	}
	return time.Now()
}

func (d *deleteLog) add(b []byte) {
	chars, ahead := deleteKeys(b)
	d.push(chars, ahead)
}

func (d *deleteLog) push(chars int64, ahead bool) {
	if chars == 0 && !ahead {
		return
	}
	d.mu.Lock()
	d.keys = append(d.keys, deleteKey{d.clock(), chars, ahead})
	d.trim()
	d.mu.Unlock()
}

// trim forgets keys older than the window. The caller holds d.mu.
func (d *deleteLog) trim() {
	cut := d.clock().Add(-deleteWindow)
	for len(d.keys) > 0 && d.keys[0].at.Before(cut) {
		d.keys = d.keys[1:]
	}
}

// recent returns how many characters the recent delete keys can still
// have removed, and whether any removes text after the cursor. It is a
// hint for reading views, never permission to lose text (see keepOld).
func (d *deleteLog) recent() (int, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.trim()
	var chars int64
	ahead := false
	for _, k := range d.keys {
		chars += k.left
		ahead = ahead || (k.ahead && k.left > 0)
	}
	return int(min(chars, unlimited)), ahead
}

// spend marks n deleted characters as accounted for by a save, oldest keys
// first. A key that deletes any amount is used up by any loss.
func (d *deleteLog) spend(n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.keys {
		if n <= 0 {
			return
		}
		k := &d.keys[i]
		switch {
		case k.left >= unlimited:
			k.left, n = 0, 0
		default:
			used := min(k.left, int64(n))
			k.left -= used
			n -= int(used)
		}
	}
}

// Keys that remove text in Claude Code's input box. Backspace, Ctrl+H and
// Delete remove one character; Ctrl+W, Ctrl+U, Ctrl+K and undo (Ctrl+_)
// any amount. Delete, Ctrl+K and undo can remove text after the cursor.
var (
	oneCharKeys = [][]byte{{0x7f}, {0x08}, []byte("\x1b[3~")}
	manyKeys    = [][]byte{{0x17}, {0x15}, {0x0b}, {0x1f}}
	aheadKeys   = [][]byte{[]byte("\x1b[3~"), {0x0b}, {0x1f}}
)

// deleteKeys returns how many characters the keys in b can delete, and
// whether any can delete after the cursor.
func deleteKeys(b []byte) (chars int64, ahead bool) {
	for _, k := range oneCharKeys {
		chars += int64(bytes.Count(b, k))
	}
	for _, k := range manyKeys {
		if bytes.Contains(b, k) {
			chars = unlimited
		}
	}
	for _, k := range aheadKeys {
		if bytes.Contains(b, k) {
			ahead = true
		}
	}
	return chars, ahead
}

// keepOld decides what happens to the draft a save is about to replace.
// It goes to history when the box empties (sent, or cleared by mistake)
// or the text was replaced wholesale (a history recall, an external
// editor). Otherwise, once the draft is stitched from a scrolling box, any
// lost text keeps a safety copy: text in view is read exactly, but text
// out of sight is inferred, and an inference can be wrong. That is the
// promise: no text the user did not delete is ever lost.
func keepOld(old, draft string, stitched bool) (history, version bool) {
	if strings.TrimSpace(old) == "" {
		return false, false
	}
	if draft == "" || !similar(old, draft) {
		return true, false
	}
	return false, stitched && lostChars(old, draft) > 0
}

// similar reports whether b is an edit of a rather than a different text:
// what they share at the start and end covers at least half of a.
func similar(a, b string) bool {
	if len(a) < 24 {
		return true
	}
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	q := 0
	for q < len(a)-p && q < len(b)-p && a[len(a)-1-q] == b[len(b)-1-q] {
		q++
	}
	return 2*(p+q) >= len(a)
}

func termSize(in *os.File) (int, int) {
	c, r, err := term.GetSize(int(in.Fd()))
	if err != nil || c <= 0 || r <= 0 {
		return 80, 24
	}
	return c, r
}
