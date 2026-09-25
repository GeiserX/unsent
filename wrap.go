package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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

	var cooked *term.State
	if term.IsTerminal(int(in.Fd())) {
		if cooked, err = term.MakeRaw(int(in.Fd())); err == nil {
			defer term.Restore(int(in.Fd()), cooked)
		}
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
	if err := st.hold(s.rec); err != nil {
		fmt.Fprintf(os.Stderr, "unsent: %v\n", err)
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
				s.pastes.feed(buf[:n])
				ptmx.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGWINCH, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigs)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	tick := time.NewTicker(saveInterval)
	defer tick.Stop()

	for {
		select {
		case sig := <-sigs:
			switch sig {
			case syscall.SIGWINCH:
				c, r := termSize(in)
				pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(c), Rows: uint16(r)})
				s.resize(c, r)
			default:
				// The window is closing or someone asked us to stop: save
				// what is in the box first, then pass the signal on.
				s.save()
				cmd.Process.Signal(sig)
			}
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

// session holds the live state of one wrapped agent.
type session struct {
	mu     sync.Mutex
	screen *vt.Emulator
	dirty  bool

	rec    *record
	store  *store
	pastes *pasteTracker
	stitch stitcher
	read   extractor
}

func (s *session) feedScreen(output <-chan []byte) {
	for chunk := range output {
		s.mu.Lock()
		s.screen.Write(chunk)
		s.dirty = true
		s.mu.Unlock()
	}
}

func (s *session) resize(cols, rows int) {
	s.mu.Lock()
	s.screen.Resize(cols, rows)
	s.dirty = true
	s.mu.Unlock()
}

// save reads the input box off the shadow screen and writes the draft to
// disk when it changed. A box that empties archives the draft it held.
func (s *session) save() {
	if s.read == nil {
		return
	}
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return
	}
	s.dirty = false
	scr := snapshot(s.screen)
	s.mu.Unlock()

	if dir := os.Getenv("UNSENT_DEBUG_DIR"); dir != "" {
		os.WriteFile(filepath.Join(dir, "screen.txt"), []byte(scr.String()), 0o600)
	}
	v, ok := s.read(scr)
	if !ok {
		// The box is not on screen (a menu, a permission prompt, an editor):
		// keep the last draft we saw.
		return
	}
	draft := s.pastes.expand(s.stitch.update(v))
	if draft == s.rec.Draft {
		return
	}
	if draft == "" {
		s.store.archive(s.rec)
		s.pastes.reset()
	}
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
		return
	}
	if err := s.store.write(s.rec); err != nil {
		s.store.warn(err)
	}
}

func termSize(in *os.File) (int, int) {
	c, r, err := term.GetSize(int(in.Fd()))
	if err != nil || c <= 0 || r <= 0 {
		return 80, 24
	}
	return c, r
}
