package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"golang.org/x/term"
)

// saveInterval is how often the shadow screen is read for a new draft.
const saveInterval = 400 * time.Millisecond

// wrap runs args[0] inside a pseudo-terminal, passes every byte between it
// and the terminal (in, out) untouched and keeps the text in the agent's
// input box saved on disk. agent names the agent (see agentFor); its
// profile reads the box. raw, when not nil, records the bytes both ways
// (unsent capture); otherwise UNSENT_DEBUG_DIR can ask for that.
func wrap(agent string, args []string, in, out *os.File, raw *rawLog) int {
	defer func() { raw.close() }()
	bin, err := exec.LookPath(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "unsent: %v\n", err)
		return 127
	}
	if off() || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		// Switched off, or piped (git diff | claude -p ...) or redirected:
		// the agent draws no input box, and a pseudo-terminal would turn the
		// pipe into keystrokes. Get out of the way.
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
	cwd = realPath(cwd)
	fmt.Fprint(os.Stderr, orphanNotice(st, cwd, agent))

	cmd := exec.Command(bin, args[1:]...)
	cols, rows := termSize(in)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		fmt.Fprintf(os.Stderr, "unsent: %v\n", err)
		return 1
	}
	defer ptmx.Close()

	s := &session{
		rec:    newRecord(args, cwd),
		store:  st,
		screen: vt.NewEmulator(cols, rows),
		pastes: &pasteTracker{rows: rows},
		prof:   profileFor(agent),
	}
	s.rec.Agent = agent
	if raw == nil {
		raw = debugRaw(s.rec.ID, agent)
	}
	raw.resize(cols, rows)
	if s.prof == nil {
		s.off = fmt.Sprintf("unsent: no reader for %q yet, running it without saving drafts", agent)
	} else if err := st.hold(s.rec); err != nil {
		// Without the lock another unsent would take this live session for
		// a dead one and could clean up its files.
		s.off = fmt.Sprintf("unsent: %v; running without saving drafts", err)
		s.prof = nil
	}
	if s.off != "" {
		fmt.Fprintln(os.Stderr, s.off)
	} else if profileFor(args[0]) == s.prof {
		// Asked now, the version is the one that runs: an agent that updates
		// itself while it runs would name the new one at exit. Only a command
		// the profile answers to is asked; with --as it may be npx or node.
		s.version = make(chan string, 1)
		go func() { s.version <- agentVersion(bin, s.prof.version) }()
	}
	defer st.release()
	// Raw mode only now: in raw mode the line feed ending the line above
	// would not bring the cursor back to the first column.
	cooked, err := term.MakeRaw(int(in.Fd()))
	if err == nil {
		defer term.Restore(int(in.Fd()), cooked)
	}
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
				raw.record('o', buf[:n])
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
			// Logged before the agent gets them, so the record has each key
			// ahead of the agent's answer. Ctrl+Z too, which the agent never gets.
			raw.record('i', buf[:n])
			// After a panic the paste tracker is no longer fed, and could
			// be stuck inside a paste.
			if n == 1 && buf[0] == ctrlZ && (s.broken.Load() || !s.pastes.inPaste()) {
				suspend <- struct{}{}
				continue
			}
			if n > 0 {
				s.input(buf[:n])
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
			raw.resize(c, r)
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
			raw.resize(c, r)
		case err := <-done:
			// Let the last output reach the shadow screen before the final save.
			time.Sleep(50 * time.Millisecond)
			s.save()
			s.finish()
			// The agent has left the screen, and with it anything printed
			// while it ran: say now what the user must know.
			if cooked != nil {
				term.Restore(int(in.Fd()), cooked)
			}
			for _, l := range s.exitLines() {
				fmt.Fprintln(os.Stderr, l)
			}
			// The notice printed before the agent started is hidden too, on
			// the alternate screen. Asked again, it leaves out a draft that
			// was restored while the agent ran. The lock goes first: held, it
			// hides the draft this session left in the box, which a plain
			// restore picks once we exit.
			st.release()
			fmt.Fprint(os.Stderr, orphanNotice(st, cwd, agent))
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

// off reports whether UNSENT_OFF switches unsent off: the way out when a
// reader misbehaves after an agent update, with no rc file to edit.
func off() bool {
	v := os.Getenv("UNSENT_OFF")
	return v != "" && v != "0"
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
	strings   stringSeqs
	dirty     bool
	inFrame   bool
	frameTail []byte
	// broken is set when reading or saving code panics (see fail): the
	// session is plain passthrough from then on.
	broken   atomic.Bool
	failOnce sync.Once
	failure  string
	failedAt time.Time
	// typed is set once the user types a key, and matched once the reader
	// finds the box: typing that never met a box saved nothing.
	typed   atomic.Bool
	matched bool
	// off says why this session saves nothing, when it saves nothing.
	off string
	// version delivers the agent's version, asked at startup, or is nil
	// when the command is not one the profile answers to.
	version chan string
	// quietUntil holds saves back just after a resize, until the agent
	// has redrawn at the new size.
	quietUntil time.Time
	// stitched is true once the box has scrolled: from then on the draft
	// is partly inferred, and every loss keeps a safety copy.
	stitched bool
	deletes  deleteLog
	// keys holds the submit and clear keys typed since the last save;
	// armed and cleared are what they say so far (see look).
	keys    keyLog
	armed   bool
	cleared bool

	rec    *record
	store  *store
	pastes *pasteTracker
	stitch stitcher
	// prof is the agent's profile, or nil when nothing is saved.
	prof *profile
}

// input notes one chunk of keys on its way to the agent: the pastes in it,
// the delete keys, and the submit and clear keys. Pasted text can hold
// control bytes (Word pastes \x0b for line breaks); only keys typed outside
// a paste count.
func (s *session) input(keys []byte) {
	if s.broken.Load() {
		return
	}
	defer s.guard()
	typed := s.pastes.feed(keys)
	if s.prof != nil {
		if !s.typed.Load() && typedKeys(keys) {
			s.typed.Store(true)
		}
		s.deletes.push(s.prof.keys.deletes(typed))
		s.keys.push(s.prof.keys, typed)
	}
}

// guard, deferred, turns a panic into plain passthrough for the rest of
// the session: a bug in reading or saving must never end the session.
func (s *session) guard() {
	if r := recover(); r != nil {
		s.fail(r)
	}
}

// fail stops saving for the rest of the session, and keeps the first
// panic to say so.
func (s *session) fail(r any) {
	s.failOnce.Do(func() {
		s.failure, s.failedAt = fmt.Sprint(r), time.Now()
		s.broken.Store(true)
	})
}

// typedKeys reports whether b holds a key the user typed, not only focus
// and mouse reports or the terminal's answers to the agent's queries: a
// string (OSC, DCS, APC, PM, SOS) or a CSI with a private marker (?, >, =),
// which no key carries.
func typedKeys(b []byte) bool {
	for i := 0; i < len(b); {
		if b[i] == 0x1b && i+1 < len(b) {
			switch b[i+1] {
			case ']', 'P', '_', '^', 'X':
				end := bytes.IndexAny(b[i+2:], "\x07\x1b")
				switch {
				case end < 0:
					return false
				case b[i+2+end] == 0x07:
					i += 2 + end + 1
				default:
					i += 2 + end + 2 // ESC \
				}
				continue
			case '[':
				if i+2 < len(b) && bytes.IndexByte([]byte("?>="), b[i+2]) >= 0 {
					n, _ := keyLen(b[i:])
					i += n
					continue
				}
			}
		}
		n, key := keyLen(b[i:])
		if key {
			return true
		}
		i += n
	}
	return false
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
	if s.broken.Load() {
		return
	}
	defer s.guard()
	// Keys waiting for the agent's answer were typed into the screen as it
	// stands now: keep it before this output changes it.
	s.keys.answer(func() *screen {
		if s.inFrame {
			return nil
		}
		return snapshot(s.screen)
	})
	s.trackFrames(chunk)
	s.screen.Write(s.strings.strip(chunk))
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
	defer s.mu.Unlock()
	if s.broken.Load() {
		return
	}
	defer s.guard()
	s.screen.Resize(cols, rows)
	s.pastes.resize(rows)
	s.dirty = true
	s.quietUntil = time.Now().Add(resizeQuiet)
}

// resizeQuiet is how long saves wait after a resize for the redraw.
const resizeQuiet = 150 * time.Millisecond

// save reads the input box off the shadow screen, after each screen a
// submit key was typed into since the last save, and writes the draft to
// disk when it changed (see look).
func (s *session) save() {
	if s.prof == nil {
		return
	}
	// A bug reading the screen, here or on the way in or out, must not take
	// the session down: stop saving, say so once, and keep passing bytes
	// through. (Deferred calls run last first: guard recovers, then this.)
	defer func() {
		if s.broken.Load() {
			s.store.warn(fmt.Errorf("stopped saving after an internal error: %v", s.failure))
		}
	}()
	defer s.guard()
	scr, events, answered := s.take()
	if scr == nil {
		return
	}
	if dir := os.Getenv("UNSENT_DEBUG_DIR"); dir != "" {
		os.WriteFile(filepath.Join(dir, "screen.txt"), []byte(scr.String()), 0o600)
	}
	// Replay the keys in order. Each submit key comes with the screen it was
	// typed into: that read shows the agent's answer to the keys before it,
	// and the text the key sends, up to the last key the agent drew. The
	// first keys after it come with theirs: a box the agent had emptied
	// before they arrived was sent, even though they are in it by now.
	for _, e := range events {
		switch e.kind {
		case keyOther:
			if s.armed && e.before != nil {
				s.look(e.before)
			}
			s.armed = false
		case keyClear:
			s.armed, s.cleared = false, true
		case keySubmit:
			s.armed = e.before != nil && s.look(e.before)
		}
	}
	if answered {
		// Until the agent draws something after a submit key, the screen is
		// the one that key was typed into, already read above.
		s.look(scr)
	}
}

// take snapshots the shadow screen for a save, with the keys typed since
// the last one, or returns a nil screen when there is nothing to read.
func (s *session) take() (scr *screen, events []*keyEvent, answered bool) {
	// If the agent is halfway through drawing, give it a moment to finish.
	for deadline := time.Now().Add(maxFrameWait); ; time.Sleep(5 * time.Millisecond) {
		s.mu.Lock()
		if !s.inFrame || time.Now().After(deadline) {
			break
		}
		s.mu.Unlock()
	}
	defer s.mu.Unlock()
	if !s.dirty || s.broken.Load() || time.Now().Before(s.quietUntil) {
		return nil, nil, false
	}
	s.dirty = false
	scr = snapshot(s.screen)
	inFrame := s.inFrame
	events, answered = s.keys.take(func() *screen {
		if inFrame {
			return nil
		}
		return scr
	})
	return scr, events, answered
}

// look reads the box off one screen and writes the draft to disk when it
// changed. It reports whether the box was on screen. A box that empties
// right after a submit key (armed), before any other key reached the
// agent and with no clear key since, was sent: the draft follows the
// on-send setting. Any other box that empties archives the draft it held,
// so a send the keys cannot vouch for counts as a clear and costs one
// history entry, never text.
func (s *session) look(scr *screen) bool {
	armed := s.armed
	s.armed = false
	v, ok := s.prof.read(scr)
	if !ok {
		// The box is not on screen (a menu, a permission prompt, an editor):
		// keep the last draft we saw.
		return false
	}
	s.matched = true
	sent := false
	if v.empty {
		sent = armed && !s.cleared
		s.cleared = false
	}
	v.deleted, v.deletedAhead = s.deletes.recent()
	before := s.stitch
	draft := s.pastes.expand(s.stitch.update(v), s.rec.Draft, s.prof)
	if dir := os.Getenv("UNSENT_DEBUG_DIR"); dir != "" {
		logView(filepath.Join(dir, "views.jsonl"), before, v, s.stitch)
	}
	if draft == s.rec.Draft {
		// Output that leaves the box as it was (a window title, a spinner
		// above it) is not the agent's answer to a submit key yet.
		s.armed = armed
		return true
	}
	s.stitched = s.stitched || v.capped
	history, version := keepOld(s.rec.Draft, draft, s.stitched)
	var err error
	switch {
	case history && sent:
		err = s.sendOff()
	case history:
		err = s.store.archive(s.rec)
	case version:
		err = s.store.keepVersion(s.rec)
	}
	if err != nil {
		// The old draft could not be kept: leave it in place rather than
		// replace it. The next save tries again.
		return true
	}
	if draft == "" {
		s.pastes.reset()
		s.store.dropVersions(s.rec)
		s.stitched = false
	}
	s.deletes.spend(shrunk(s.rec.Draft, draft))
	s.rec.Draft = draft
	s.rec.Pastes = s.pastes.all()
	s.rec.Updated = time.Now()
	if err := s.store.write(s.rec); err != nil {
		s.store.warn(err)
	}
	return true
}

// sendOff follows the on-send setting for a draft that was sent: log
// appends it to the session's sent log, delete keeps nothing. The agent
// holds the text now. A sent log that cannot be written sends the draft to
// history instead.
func (s *session) sendOff() error {
	if onSend(s.rec.agent()) == "delete" {
		return nil
	}
	if err := s.store.logSent(s.rec); err != nil {
		s.store.warn(err)
		return s.store.archive(s.rec)
	}
	return nil
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

// exitLines are what the user must know once the agent has exited: that it
// ran without saving, or that saving stopped. An agent on the alternate
// screen hid whatever was printed while it ran, and silence here is the
// worst failure: after an agent update a reader stops matching and nothing
// else would say so.
func (s *session) exitLines() []string {
	switch {
	case s.off != "":
		return []string{s.off}
	case s.prof == nil:
		return nil
	case s.broken.Load():
		return []string{fmt.Sprintf("unsent: an internal error stopped saving %s's box at %s (%s); what was typed after that was not saved",
			s.prof.name, s.failedAt.Format("15:04"), s.failure)}
	case !s.matched && s.typed.Load():
		name := s.prof.name
		if s.version != nil {
			if v := <-s.version; v != "" {
				name += " " + v
			}
		}
		return []string{fmt.Sprintf("unsent: could not read %s's box this session (last verified %s), nothing was saved", name, s.prof.verified)}
	}
	return nil
}

// agentVersion asks the agent for its version with args, and returns the
// first version number it prints, or "" when it gives none within a second.
func agentVersion(bin string, args []string) string {
	if len(args) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.WaitDelay = time.Second // a child left holding the output open
	out, _ := cmd.Output()
	return versionRE.FindString(string(out))
}

var versionRE = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?(?:[-+][0-9A-Za-z.-]+)?`)

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

// shrunk is how many characters shorter draft is than old: what the
// delete keys at least removed. (Counting lost words instead overcharges:
// "typed li" backspaced to "type" removes 4 characters, not 7.)
func shrunk(old, draft string) int {
	return max(0, utf8.RuneCountInString(old)-utf8.RuneCountInString(draft))
}

// keyset is the keys that remove text in an agent's input box: one
// character each, any amount, and (a subset of both) those that can remove
// text after the cursor. submit are the keys that send the box, and clear
// those that empty it without sending (see sent.go).
type keyset struct {
	one, many, ahead [][]byte
	submit, clear    [][]byte
}

// deletes returns how many characters the keys in b can delete, and
// whether any can delete after the cursor.
func (ks keyset) deletes(b []byte) (chars int64, ahead bool) {
	for _, k := range ks.one {
		chars += int64(bytes.Count(b, k))
	}
	for _, k := range ks.many {
		if bytes.Contains(b, k) {
			chars = unlimited
		}
	}
	for _, k := range ks.ahead {
		if bytes.Contains(b, k) {
			ahead = true
		}
	}
	return chars, ahead
}

// keepOld decides what happens to the draft a save is about to replace.
// It goes to history when the box empties (cleared by mistake, or sent
// when the save cannot be sure it was; see session.look) or the text was
// replaced wholesale (a history recall, an external editor). Otherwise,
// once the draft is stitched from a scrolling box, any lost text keeps a
// safety copy: text in view is read exactly, but text out of sight is
// inferred, and an inference can be wrong. That is the promise: no text
// the user did not delete is ever lost.
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
