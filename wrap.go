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
	"slices"
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

// heldWait is how long a key cut short at the end of a read waits for its
// rest before it goes to the agent as it is (see openSeq). The halves of a
// split key come back to back; tests raise it.
var heldWait = 100 * time.Millisecond

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
	if off() || !onTerminal(in, out) {
		// Switched off, or piped (git diff | claude -p ...) or redirected
		// (claude 2>err.log): the agent draws no input box, a
		// pseudo-terminal would turn the pipe into keystrokes, and the
		// agent's stderr would land in it instead of the file. Get out of
		// the way.
		return handOver(bin, args)
	}
	// Arm the signal handlers before anything slow (the store, the
	// pseudo-terminal): a hang-up or Ctrl+C during startup must be handled,
	// not take unsent down with the default action. The buffered channels
	// hold what arrives before the loop below runs.
	resizes, sigs := make(chan os.Signal, 1), make(chan os.Signal, 4)
	signal.Notify(resizes, syscall.SIGWINCH)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
	defer signal.Stop(resizes)
	defer signal.Stop(sigs)
	// broken hands the session to the agent when unsent cannot run it: a
	// draft saver that fails must not cost the user the session. The
	// handlers go first, so the agent gets the dispositions unsent was
	// started with (a hang-up ignored under nohup stays ignored).
	broken := func(what string, err error) int {
		fmt.Fprintf(os.Stderr, "unsent: %s: %v; drafts are not being saved this session\n", what, err)
		signal.Stop(resizes)
		signal.Stop(sigs)
		return handOver(bin, args)
	}

	// Only what decides whether unsent can run the agent comes before it
	// starts: a launcher that execs unsent and watches that pid should see
	// the agent's child at once. The store's folders are made here and read
	// only once the agent runs.
	st, err := makeStore()
	if err != nil {
		return broken("cannot open the draft folder", err)
	}
	cmd := exec.Command(bin, args[1:]...)
	cols, rows := termSize(in)
	started := time.Now()
	ptmx, err := startPty(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return broken("cannot start a pseudo-terminal", err)
	}
	defer ptmx.Close()

	// The agent runs. Its output waits in the pseudo-terminal until the
	// reader below starts, so what unsent prints until then still comes
	// before the agent's first paint, as when this ran before the start.
	// The import can wait on another unsent's import, and prunes history.
	st.importShells()
	cwd, _ := os.Getwd()
	cwd = realPath(cwd)
	if notices() {
		fmt.Fprint(os.Stderr, orphanNotice(st, cwd, agent))
	}

	s := &session{
		rec:    newRecord(args, cwd),
		store:  st,
		screen: vt.NewEmulator(cols, rows),
		pastes: &pasteTracker{rows: rows},
		prof:   profileFor(agent),
		out:    &termOut{w: out},
	}
	s.toAgent = func(b []byte) {
		raw.record('i', b)
		ptmx.Write(b)
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
	if s.prof != nil {
		s.ids = newSessionTracker(s.prof.session, cmd.Process.Pid, started)
		// With --as the arguments are a launcher's, read as the agent's
		// own: one that could be a prompt, or an option the agent does not
		// have, turns restore off, and the notice after exit names the draft.
		s.rs.on = s.ids != nil && s.prof.restore != nil && s.prof.restore.chat(args[1:])
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
				s.out.Write(buf[:n])
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
	// A suspend key (Ctrl+Z) can come in any form and anywhere in a read:
	// the keys before it reach the agent first, and the key itself never
	// does. The keys after it in the same read were typed for the shell,
	// not the agent, so they are dropped, as the kernel flushes pending
	// input on its own suspend key; an Enter among them would otherwise
	// send the draft once resumed. The profile picks the keys even when
	// the lock failed and nothing is saved.
	suspend := make(chan chan struct{}, 1)
	prof := profileFor(agent)
	stops := suspendKeys(prof)
	// A key the terminal split across two reads (ESC[12, then 2;5u) waits
	// in held for the rest (openSeq), and is decoded and passed on with the
	// next read. A key that really ends open, such as Alt+[ as ESC [, goes
	// to the agent heldWait later with no read after it.
	go func() {
		buf := make([]byte, 32*1024)
		var heldMu sync.Mutex
		var held []byte
		release := func() {
			heldMu.Lock()
			defer heldMu.Unlock()
			if len(held) > 0 {
				s.forward(held, ptmx)
				held = nil
			}
		}
		wait := time.AfterFunc(time.Hour, release)
		wait.Stop()
		for {
			n, err := in.Read(buf)
			// Logged before the agent gets them, so the record has each key
			// ahead of the agent's answer. Ctrl+Z too, which the agent never gets.
			raw.record('i', buf[:n])
			heldMu.Lock()
			wait.Stop()
			data := append(held, buf[:n]...)
			held = nil
			cut, suspended := openSeq(data), false
			for rest := data[:cut]; len(rest) > 0; {
				at, end := s.suspendAt(rest, stops)
				if at < 0 {
					s.forward(rest, ptmx)
					break
				}
				if at > 0 {
					s.forward(rest[:at], ptmx)
				}
				// After a panic the paste tracker is no longer fed, and
				// could be stuck inside a paste.
				if s.broken.Load() || !s.pastes.inPaste() {
					resumed := make(chan struct{})
					suspend <- resumed
					<-resumed
					suspended = true
					break
				}
				s.forward(rest[at:end], ptmx)
				rest = rest[end:]
			}
			// A held tail after a suspend was typed for the shell too.
			if !suspended && cut < len(data) {
				held = data[cut:]
				wait.Reset(heldWait)
			}
			heldMu.Unlock()
			if err != nil {
				release()
				return
			}
		}
	}()

	// (The stop signals and resizes were armed at the top of wrap; resizes
	// get their own channel so a burst of them cannot crowd out a hang-up.)
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
			s.closing()
			cmd.Process.Signal(sig)
			// In case it was stopped by Ctrl+Z: a stopped agent would never
			// act on the signal.
			cmd.Process.Signal(syscall.SIGCONT)
		case <-tick.C:
			timedSave(s)
			s.tryRestore()
			s.out.tick()
		case resumed := <-suspend:
			// The agent runs in its own terminal session, where the kernel
			// drops a suspend signal: it would print "suspended" and hang.
			// So the wrapper suspends instead, agent and all, and the shell's
			// fg brings both back.
			s.save()
			cmd.Process.Signal(syscall.SIGSTOP)
			// Once the stopped agent's last output is through, turn off what
			// it turned on, as its own suspend would (profile.suspended).
			if prof != nil && prof.suspended != nil {
				time.Sleep(50 * time.Millisecond)
				s.out.Write(s.leaving(prof.suspended))
			}
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
			close(resumed)
		case err := <-done:
			// Let the last output reach the shadow screen before the final save.
			time.Sleep(50 * time.Millisecond)
			s.save()
			s.endRestore()
			s.finish()
			// The agent has left the screen, and with it anything printed
			// while it ran: say now what the user must know.
			if cooked != nil {
				term.Restore(int(in.Fd()), cooked)
			}
			for _, l := range append(s.exitLines(), s.restoreLines()...) {
				fmt.Fprintln(os.Stderr, l)
			}
			// The notice printed before the agent started is hidden too, on
			// the alternate screen. Asked again, it leaves out a draft that
			// was restored while the agent ran. The lock goes first: held, it
			// hides the draft this session left in the box, which a plain
			// restore picks once we exit.
			st.release()
			st.importShells() // a shell may have closed with a line while the agent ran
			if notices() {
				fmt.Fprint(os.Stderr, orphanNotice(st, cwd, agent))
			}
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				if st, ok := exit.Sys().(syscall.WaitStatus); ok && st.Signaled() {
					killedBy = st.Signal()
					return 128 + int(st.Signal())
				}
				return exit.ExitCode()
			}
			return 0
		}
	}
}

const ctrlZ = 0x1a

// leaveAlt switches the terminal back from the alternate screen.
var leaveAlt = []byte("\x1b[?1049l")

// leaving is what unsent writes for the agent as it suspends it: the
// agent's own suspend output, without leaving the alternate screen when
// the agent draws on the main one, where that would move the cursor.
func (s *session) leaving(suspended []byte) []byte {
	s.mu.Lock()
	alt := s.screen.IsAltScreen()
	s.mu.Unlock()
	if alt {
		return suspended
	}
	return bytes.ReplaceAll(suspended, leaveAlt, nil)
}

// suspendAt returns where the first of the suspend keys starts and ends in
// b, or -1, -1. Should the decoder ever panic, saving stops (see fail) and
// that read suspends only when it is a lone Ctrl+Z byte, as before the
// decoder.
func (s *session) suspendAt(b []byte, keys []key) (at, end int) {
	defer func() {
		if r := recover(); r != nil {
			s.fail(r)
			at, end = -1, -1
			if len(b) == 1 && b[0] == ctrlZ && slices.Contains(keys, ctrl('z')) {
				at, end = 0, 1
			}
		}
	}()
	return findKey(b, keys)
}

// killedBy is the signal that killed the agent, once wrap has returned
// 128+n for it; exitAs dies of it too.
var killedBy syscall.Signal

// exitAs ends unsent with code, or, when the agent was killed by a signal,
// by that same signal, so the shell's $? and job control see what they
// would see without unsent. It runs only after wrap has returned: the
// terminal is restored, the draft saved and the lock released. Go's runtime
// dies of a hang-up, Ctrl+C or SIGTERM nobody listens for, and nothing
// survives SIGKILL. The others it would answer with a goroutine dump, or
// ignore, so they keep 128+n, as does a signal unsent was started with
// ignored, which signal.Reset leaves ignored.
func exitAs(code int) {
	switch sig := killedBy; sig {
	case syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL:
		signal.Reset(sig)
		syscall.Kill(os.Getpid(), sig)
		// Delivery is not instant; a signal still ignored never comes.
		time.Sleep(100 * time.Millisecond)
	}
	os.Exit(code)
}

// stopSelf suspends the wrapper the way Ctrl+Z suspends any job, and returns
// when the shell resumes it.
var stopSelf = func() {
	syscall.Kill(os.Getpid(), syscall.SIGTSTP)
}

// timedSave is the save each tick runs. Tests hook it to type the next key
// only once a save has read the screen the last key drew.
var timedSave = (*session).save

// onTerminal reports whether in, out and the process's stderr are all
// terminals. The pseudo-terminal replaces all three for the agent, so any
// one of them redirected would lose that redirect.
func onTerminal(in, out *os.File) bool {
	return term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
}

// execAgent replaces the wrapper with the agent.
var execAgent = func(bin string, args []string) error {
	return syscall.Exec(bin, args, os.Environ())
}

// startPty starts the agent on a new pseudo-terminal; tests make it fail.
var startPty = pty.StartWithSize

// handOver replaces unsent with the agent, so the agent's exit status is
// the session's. It returns only when the agent cannot be started.
func handOver(bin string, args []string) int {
	if err := execAgent(bin, args); err != nil {
		fmt.Fprintf(os.Stderr, "unsent: %v\n", err)
		return 126
	}
	return 0
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
	// Bracketed paste on and off: without it a line break in a restored
	// draft would be Enter. The same length as the frame marks, so the kept
	// tail covers a split one.
	bracketOn  = []byte("\x1b[?2004h")
	bracketOff = []byte("\x1b[?2004l")
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
	// leftOnSubmit is set when the screen after a submit key typed into the
	// box shows no box, until a box is read again or another key is typed
	// (the screens of a teardown that takes several frames, /exit, keep
	// it); goneOnSubmit stays set through the keys too (a picker the key
	// opened). closed is set once the window closed or unsent was asked to
	// stop (see closing).
	leftOnSubmit bool
	goneOnSubmit bool
	closed       bool

	rec    *record
	store  *store
	pastes *pasteTracker
	stitch stitcher
	// prof is the agent's profile, or nil when nothing is saved.
	prof *profile
	// ids follows the agent's own session id, nil when the profile reads
	// none; seen is the id the save read, while it reads the screen of
	// now ("" for none), and nil while it reads the screens keys were
	// typed into.
	ids  *sessionTracker
	seen *string

	// Restore-in-box (restore.go): rs is its state; shown is when the box
	// came into view (zero while it is out of view) and bracketed whether
	// the agent has bracketed paste on, both under mu. toAgent writes to the
	// agent as keys do, under ptyMu, which keeps keys out of a paste; out is
	// the user's terminal.
	rs        restoreState
	shown     time.Time
	bracketed bool
	ptyMu     sync.Mutex
	toAgent   func([]byte)
	out       *termOut
}

// forward passes a chunk of keys the user typed on to the agent, under
// ptyMu: a key never lands inside a restore's paste (tryRestore), it
// follows it.
func (s *session) forward(keys []byte, agent io.Writer) {
	s.ptyMu.Lock()
	defer s.ptyMu.Unlock()
	s.input(keys)
	agent.Write(keys)
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
	if s.rs.on && typedKeys(keys) {
		s.rs.lastKey.Store(time.Now().UnixNano())
	}
	if s.prof != nil {
		if !s.typed.Load() && typedKeys(keys) {
			s.typed.Store(true)
		}
		s.deletes.push(s.prof.keys.deletes(typed))
		s.keys.push(s.prof.keys, typed)
		if s.pastes.holdsEsc() {
			s.keys.hold()
		}
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
	s.watchBox()
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
	switch on, off := bytes.LastIndex(data, bracketOn), bytes.LastIndex(data, bracketOff); {
	case on > off:
		s.bracketed = true
	case off > on:
		s.bracketed = false
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
	// Read after the screen, so a box drawn for a session the agent just
	// switched to comes with that session's id. It is the id of this
	// screen only: the screens the keys were typed into come from before
	// the switch, and a /clear or a picker choice sent from one of them
	// belongs to the session it was typed in.
	seen := s.ids.current()
	s.seen = nil
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
			s.armed, s.leftOnSubmit = false, false
		case keyClear:
			// A clear key after a submit that took the box away (Ctrl+C
			// in a /resume picker or a /model dialog) means the agent did
			// not leave on that key: at exit its draft goes to history,
			// not the sent log, and is not left behind (finish).
			s.armed, s.cleared, s.leftOnSubmit = false, true, false
		case keySubmit:
			s.armed = e.before != nil && s.look(e.before)
		}
	}
	if answered {
		// Until the agent draws something after a submit key, the screen is
		// the one that key was typed into, already read above.
		s.seen = &seen
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
		// keep the last draft we saw. Right after a submit key, the agent
		// may have left for good with it (/exit): see finish. After the
		// close, the agent leaves because of it, not because of a key.
		if !s.closed {
			s.leftOnSubmit = s.leftOnSubmit || armed
			s.goneOnSubmit = s.goneOnSubmit || armed
		}
		return false
	}
	s.leftOnSubmit, s.goneOnSubmit = false, false
	s.matched = true
	sent := false
	if v.empty {
		sent = armed && !s.cleared
		s.cleared = false
	}
	v.deleted, v.deletedAhead = s.deletes.recent()
	before := s.stitch
	draft := s.pastes.expand(s.stitch.update(v), s.rec.Draft, s.prof)
	s.verifyRestore(draft, v.width)
	if dir := os.Getenv("UNSENT_DEBUG_DIR"); dir != "" {
		logView(filepath.Join(dir, "views.jsonl"), before, v, s.stitch)
	}
	if draft == s.rec.Draft {
		// Output that leaves the box as it was (a window title, a spinner
		// above it) is not the agent's answer to a submit key yet. A send
		// still to come goes to the session it was typed in.
		s.armed = armed
		if !armed && s.follow(false) && s.rec.Draft != "" {
			if err := s.store.write(s.rec); err != nil {
				s.store.warn(err)
			}
		}
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
	// The old draft went where its session's drafts go; the new one is in
	// the session the agent is in now.
	s.follow(true)
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

// follow moves the record to the session this save read, when the agent
// changed session (a picker choice, /resume, /clear), and reports whether
// it did. A read that found no id leaves the record where it was, unless
// the draft changed (changed) after the session ended: that text was
// typed after it, so it gets no session until the next one is read, and a
// restore never takes it into the conversation that ended.
func (s *session) follow(changed bool) bool {
	if s.seen == nil || *s.seen == s.rec.AgentSession || *s.seen == "" && !changed {
		return false
	}
	switch {
	case *s.seen == "":
	case s.rec.joined.IsZero():
		// The run's first session: it was in it from the start.
		s.rec.joined = s.rec.Started
	default:
		s.rec.joined = time.Now()
	}
	s.rec.AgentSession = *s.seen
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

// closing saves what is in the box as the window closes or unsent is
// asked to stop. From then on a screen with no box is the agent leaving
// because of the close, not on a submit key (look): a draft still in the
// box at the close is kept, while an agent that had already left on the
// key (/exit) took its draft.
func (s *session) closing() {
	s.save()
	s.closed = true
}

// finish runs when the agent exits. An empty box leaves nothing to recover,
// so its file goes away; a non-empty one stays for `unsent restore`.
func (s *session) finish() {
	s.rec.Ended = time.Now()
	switch {
	case s.rec.Draft == "":
	case s.leftOnSubmit && !s.cleared && s.sendOff() == nil:
		// The agent left on the submit key (/exit): it took the draft, which
		// must not come back as one left behind, nor be put back in the box
		// when the conversation is reopened.
		s.rec.Draft = ""
	case !s.leftOnSubmit && s.goneOnSubmit && s.store.archive(s.rec) == nil:
		// The submit key took the box away and it never came back: the
		// agent ran the draft (a /resume picker the window closed on, even
		// after Ctrl+C left it open). It was not left in the box, but
		// nothing proves a send, so it goes to history, as a box that
		// empties after such a screen does.
		s.rec.Draft = ""
	}
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

// keyset is the keys of an agent's input box that unsent must know about,
// as decoded keys (keys.go), whatever form the terminal sends them in. The
// delete keys: one removes one character, many any amount, and ahead (a
// subset of both) those that can remove text after the cursor; a key in
// neither of the first two only removes text before it. submit are the
// keys that send the box, and clear those that empty all of it without
// sending (see sent.go); each is a chord of one key or more, such as Esc
// Esc. suspend are the keys unsent takes for itself to suspend the agent,
// on the first press; the agent never gets them.
type keyset struct {
	one, many, ahead []key
	submit, clear    [][]key
	suspend          []key
}

// deletes returns how many characters the keys in b can delete, and
// whether any can delete after the cursor.
func (ks keyset) deletes(b []byte) (chars int64, ahead bool) {
	many := false
	for _, k := range pressed(b) {
		if slices.Contains(ks.one, k) {
			chars++
		}
		many = many || slices.Contains(ks.many, k)
		ahead = ahead || slices.Contains(ks.ahead, k)
	}
	if many {
		chars = unlimited
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
	p, q := sharedEnds(a, b)
	return 2*(p+q) >= len(a)
}

// sharedEnds returns how many bytes a and b share at the start, and then
// at the end of what is left.
func sharedEnds(a, b string) (p, q int) {
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	for q < len(a)-p && q < len(b)-p && a[len(a)-1-q] == b[len(b)-1-q] {
		q++
	}
	return p, q
}

func termSize(in *os.File) (int, int) {
	c, r, err := term.GetSize(int(in.Fd()))
	if err != nil || c <= 0 || r <= 0 {
		return 80, 24
	}
	return c, r
}
