package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// TestZshInitGolden pins the hooks unsent init zsh prints, byte for byte,
// and checks the setup block carries the same text. A change to the hooks
// shows up as a change to the golden file.
// UNSENT_UPDATE_GOLDEN=1 rewrites it.
func TestZshInitGolden(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"init", "zsh"}, &out, &errOut); code != 0 {
		t.Fatalf("unsent init zsh: exit %d: %s", code, errOut.String())
	}
	golden := filepath.Join("testdata", "shell", "zsh-init.golden")
	if os.Getenv("UNSENT_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("unsent init zsh differs from %s (UNSENT_UPDATE_GOLDEN=1 rewrites it):\n%s", golden, out.String())
	}
	if !strings.Contains(setupBlock("zsh"), string(want)) {
		t.Fatal("the zsh setup block does not carry the hooks init prints")
	}
	if strings.Contains(setupBlock("bash"), "zle") {
		t.Fatal("the bash setup block carries zsh hooks")
	}
	for _, args := range [][]string{{"init"}, {"init", "bash"}, {"init", "zsh", "x"}} {
		if code := run(args, &out, &errOut); code != 2 {
			t.Errorf("unsent %v: exit %d, want 2", args, code)
		}
	}
}

// zrec is one record of a zsh log.
type zrec struct{ kind, text string }

func (r zrec) String() string { return r.kind + strconv.Quote(r.text) }

// parseZshLog reads a log's records: "<kind> <seconds> <bytes>\n", the
// text, "\n". A record still being written ends the list.
func parseZshLog(b []byte) ([]zrec, error) {
	var out []zrec
	for len(b) > 0 {
		nl := bytes.IndexByte(b, '\n')
		if nl < 0 {
			return out, nil
		}
		f := strings.Fields(string(b[:nl]))
		if len(f) != 3 || len(f[0]) != 1 {
			return out, fmt.Errorf("bad header %q", b[:nl])
		}
		if _, err := strconv.ParseInt(f[1], 10, 64); err != nil {
			return out, fmt.Errorf("bad time in %q", b[:nl])
		}
		n, err := strconv.Atoi(f[2])
		if err != nil || n < 0 {
			return out, fmt.Errorf("bad length in %q", b[:nl])
		}
		b = b[nl+1:]
		if len(b) < n+1 {
			return out, nil
		}
		if b[n] != '\n' {
			return out, fmt.Errorf("record %q does not end in a line break", b[:n+1])
		}
		out = append(out, zrec{f[0], string(b[:n])})
		b = b[n+1:]
	}
	return out, nil
}

// zshSession is a real zsh on a pseudo-terminal, started as `zsh -f -i`
// under an empty environment with HOME and ZDOTDIR in a scratch folder and
// only the hooks sourced.
type zshSession struct {
	t     *testing.T
	cmd   *exec.Cmd
	pty   *os.File
	home  string
	dir   string // where the logs go
	mu    sync.Mutex
	out   bytes.Buffer
	ended chan error
}

func testZsh(t *testing.T) string {
	for _, sh := range testShells(t) {
		if shellKind(sh) == "zsh" {
			return sh
		}
	}
	t.Skip("no zsh on this machine")
	return ""
}

// startZsh starts zsh with hooks sourced. With unsentHome set, the logs go
// under UNSENT_HOME; else under HOME, as stateDir falls back. env adds to
// the shell's environment.
func startZsh(t *testing.T, hooks string, unsentHome bool, env ...string) *zshSession {
	t.Helper()
	sh := testZsh(t)
	home := t.TempDir()
	agentBin := filepath.Join(home, "agent-bin")
	writeScript(t, filepath.Join(agentBin, "claude"), `printf 'claude[%s]\n' "$@"`)
	// The hooks save nothing unless unsent is on the shell's PATH.
	writeScript(t, filepath.Join(agentBin, "unsent"), "exit 0")
	hookFile := filepath.Join(home, "hooks.zsh")
	if err := os.WriteFile(hookFile, []byte(hooks), 0o644); err != nil {
		t.Fatal(err)
	}
	env = append([]string{"HOME=" + home, "ZDOTDIR=" + home, "PATH=" + agentBin + ":/usr/bin:/bin", "TERM=xterm", "PS1=" + zshPrompt}, env...)
	dir := filepath.Join(home, ".local", "state", "unsent", "shell")
	if unsentHome {
		env = append(env, "UNSENT_HOME="+filepath.Join(home, "state"))
		dir = filepath.Join(home, "state", "shell")
	}
	s := openZsh(t, exec.Command(sh, "-f", "-i"), env, home, dir)
	// Keys typed before the line editor runs are typeahead: the terminal
	// echoes them itself, and zsh on macOS can lose a line of it.
	s.prompt(0, zshStart)
	n := s.outLen()
	s.send("source " + hookFile + "\r")
	s.prompt(n, zshStart)
	s.mark("ready")
	return s
}

// openZsh starts cmd, a zsh, on a pseudo-terminal with env, in home, its
// logs going to dir.
func openZsh(t *testing.T, cmd *exec.Cmd, env []string, home, dir string) *zshSession {
	t.Helper()
	cmd.Env = env
	cmd.Dir = home
	p, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	s := &zshSession{t: t, cmd: cmd, pty: p, home: home, dir: dir, ended: make(chan error, 1)}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := p.Read(buf)
			s.mu.Lock()
			s.out.Write(buf[:n])
			s.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	go func() { s.ended <- cmd.Wait() }()
	t.Cleanup(func() {
		cmd.Process.Kill()
		p.Close()
	})
	return s
}

func (s *zshSession) send(keys string) {
	s.t.Helper()
	if _, err := s.pty.WriteString(keys); err != nil {
		s.t.Fatal(err)
	}
}

// echo sends keys and waits for the terminal to show want after them.
// zsh runs the line-pre-redraw hook before it redraws, so the record for
// the keys is written by then. Keys sent together with Ctrl+C may get no
// redraw at all, because zsh skips a redraw while input is pending.
func (s *zshSession) echo(keys, want string) {
	s.t.Helper()
	n := s.outLen()
	s.send(keys)
	s.until(zshStall, func() bool { return strings.Contains(s.shown(n), want) }, func() string {
		return fmt.Sprintf("waited for %q on the terminal; got %q\nthe log ends %v", want, s.shown(n), s.tail())
	})
}

// zshPrompt is the test shell's prompt, set from the environment, so no
// echo of a typed line shows it. setup_test.go sets it with a line that
// quotes it in two parts.
const zshPrompt = "<zsh-ready> "

// zleOn is what zle prints after a prompt once the line-init hook has run:
// the switch to bracketed paste. The prompt itself shows before the hook.
const zleOn = "\x1b[?2004h"

// outLen is how much the terminal has shown so far.
func (s *zshSession) outLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.Len()
}

// shown is what the terminal has shown after from.
func (s *zshSession) shown(from int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.String()[from:]
}

// zshStall is how long a wait lets the shell go without showing anything
// new on the terminal or changing its logs. A loaded shared runner can
// take a second over each key, so a wait bounded in total fails a shell
// that is slow but moving; one that shows and writes nothing for this long
// is stuck, and the wait fails at once with what it saw.
const zshStall = 20 * time.Second

// zshCap bounds any one wait however steadily the shell moves, so a shell
// that loops, writing a record after record, fails its test long before
// the test binary's timeout.
const zshCap = 3 * time.Minute

// progress sums what the terminal has shown and the sizes of the logs, so
// it changes whenever the shell draws or writes.
func (s *zshSession) progress() int64 {
	n := int64(s.outLen())
	m, _ := filepath.Glob(filepath.Join(s.dir, "zsh-*"))
	for _, f := range m {
		if fi, err := os.Stat(f); err == nil {
			n += fi.Size() + 1
		}
	}
	return n
}

// await polls ok every 10 ms until it holds, and fails once the shell has
// gone stall without progress, or after zshCap in all.
func (s *zshSession) await(stall time.Duration, ok func() bool) error {
	start := time.Now()
	last, moved := s.progress(), start
	for !ok() {
		now := time.Now()
		if p := s.progress(); p != last {
			last, moved = p, now
		}
		if now.Sub(moved) > stall {
			return fmt.Errorf("the shell showed and wrote nothing for %v", stall)
		}
		if now.Sub(start) > zshCap {
			return fmt.Errorf("still waiting after %v", zshCap)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// until is await that fails the test with failure's text.
func (s *zshSession) until(stall time.Duration, ok func() bool, failure func() string) {
	s.t.Helper()
	if err := s.await(stall, ok); err != nil {
		s.t.Fatalf("%v; %s", err, failure())
	}
}

// prompted says whether the terminal, after from, shows a prompt whose
// line-init hook has run, and has shown no newer prompt since.
func (s *zshSession) prompted(from int) bool {
	out := s.shown(from)
	i := strings.LastIndex(out, zshPrompt)
	return i >= 0 && strings.Contains(out[i:], zleOn)
}

// zshStart is how long a new shell may show nothing before its first
// prompts: it loads its modules first, and on a loaded Mac the malware
// scanner holds each one up.
const zshStart = time.Minute

// prompt waits for a prompt after from whose line-init hook has run,
// letting the shell go stall without progress.
func (s *zshSession) prompt(from int, stall time.Duration) {
	s.t.Helper()
	s.until(stall, func() bool { return s.prompted(from) }, func() string {
		return fmt.Sprintf("waited for a prompt past its line-init hook; the terminal shows %q\nthe log ends %v", s.shown(from), s.tail())
	})
}

// run types a command, runs it with Enter and waits for the next prompt's
// line-init hook. The terminal echoes keys typed while a command runs, so
// seeing them there says nothing about the line editor.
func (s *zshSession) run(line string) {
	s.t.Helper()
	n := s.outLen()
	s.send(line + "\r")
	s.prompt(n, zshStall)
}

// clear presses Ctrl+C and waits for the new prompt, on the terminal or as
// an i record. The terminal drops input still queued when Ctrl+C arrives,
// as the Linux one does, so keys sent before the prompt shows may never
// reach the shell. And zsh acts on a Ctrl+C that comes while the line
// editor is busy, drawing or running a hook, only when the next key
// arrives, which it then drops. So each half second without a prompt
// sends a Backspace: dropped behind a pending Ctrl+C, and at a fresh empty
// prompt it deletes nothing and the hooks write nothing.
func (s *zshSession) clear() {
	s.t.Helper()
	n := s.outLen()
	count := func() int {
		c := 0
		for _, r := range s.records() {
			if r.kind == "i" {
				c++
			}
		}
		return c
	}
	i := count()
	s.send("\x03")
	nudge := time.Now().Add(500 * time.Millisecond)
	s.until(zshStall, func() bool {
		if strings.Contains(s.shown(n), zshPrompt) || count() > i {
			return true
		}
		if time.Now().After(nudge) {
			s.send("\x7f")
			nudge = time.Now().Add(500 * time.Millisecond)
		}
		return false
	}, func() string {
		return fmt.Sprintf("no prompt after Ctrl+C; the terminal shows %q\nthe log ends %v", s.shown(n), s.tail())
	})
}

// tail is the end of the live log, for a failure message.
func (s *zshSession) tail() []zrec {
	r := s.records()
	return r[max(0, len(r)-8):]
}

// exits waits for the shell to exit after a signal, or fails with what the
// log and the terminal show. Its bound is in total, not on progress: a
// shell that goes on writing after its hangup, one h record after
// another, is the failure this catches.
func (s *zshSession) exits(what string) {
	s.t.Helper()
	select {
	case err := <-s.ended:
		s.ended <- err
	case <-time.After(30 * time.Second):
		s.mu.Lock()
		out := s.out.String()
		s.mu.Unlock()
		s.t.Fatalf("zsh did not exit on %s; %d records, the log ends %v\nterminal: %q", what, len(s.records()), s.tail(), out[max(0, len(out)-400):])
	}
}

// logs are the shell's log files.
func (s *zshSession) logs() []string {
	m, _ := filepath.Glob(filepath.Join(s.dir, "zsh-*"))
	return m
}

// records reads the one live log.
func (s *zshSession) records() []zrec {
	s.t.Helper()
	m, _ := filepath.Glob(filepath.Join(s.dir, "zsh-*.log"))
	if len(m) != 1 {
		return nil
	}
	b, err := os.ReadFile(m[0])
	if errors.Is(err, fs.ErrNotExist) {
		return nil // moved aside since the glob; the caller asks again
	}
	if err != nil {
		s.t.Fatal(err)
	}
	r, err := parseZshLog(b)
	if err != nil {
		s.t.Fatalf("%s: %v", m[0], err)
	}
	return r
}

// index finds the first record at or after from of that kind, and with
// that text unless text is "". A from below 0, a record not found, finds
// nothing.
func (s *zshSession) index(r []zrec, from int, kind, text string) int {
	if from < 0 {
		return -1
	}
	for i := from; i < len(r); i++ {
		if r[i].kind == kind && (text == "" || r[i].text == text) {
			return i
		}
	}
	return -1
}

// waitFor waits for the live log's records to satisfy ok and returns them.
func (s *zshSession) waitFor(what string, ok func([]zrec) bool) []zrec {
	s.t.Helper()
	var r []zrec
	s.until(zshStall, func() bool { r = s.records(); return ok(r) }, func() string {
		return fmt.Sprintf("waited for %s; the log holds %v\nterminal: %q", what, r, s.shown(0))
	})
	return r
}

// line types text at the prompt and waits for its redraw record.
func (s *zshSession) line(text string) []zrec {
	s.t.Helper()
	n := len(s.records())
	s.send(text)
	return s.waitFor("b "+strconv.Quote(text), func(r []zrec) bool { return s.index(r, n, "b", text) >= 0 })
}

// mark types a fresh marker line and waits for it, so every record the
// keys before it made is in the log. Ctrl+C clears the marker again.
func (s *zshSession) mark(name string) []zrec {
	s.t.Helper()
	r := s.line("mark-" + name)
	s.clear()
	return r
}

func holds(r []zrec, text string) bool {
	return slices.ContainsFunc(r, func(x zrec) bool { return strings.Contains(x.text, text) })
}

// scenarioCtrlC types a line, clears it with Ctrl+C and types the next:
// an i record must come between, and no s for the cleared line.
func scenarioCtrlC(s *zshSession) error {
	from := len(s.records())
	s.line("lost line")
	s.clear()
	r := s.line("next line")
	lost := s.index(r, from, "b", "lost line")
	next := s.index(r, lost, "b", "next line")
	if s.index(r, from, "s", "lost line") >= 0 {
		return fmt.Errorf("a cleared line was marked sent: %v", r[from:])
	}
	if i := s.index(r, lost, "i", ""); i < 0 || i > next {
		return fmt.Errorf("no i record between the cleared line and the next: %v", r[from:])
	}
	s.clear()
	return nil
}

// scenarioVared types a secret at vared and checks it never reaches the
// log.
func scenarioVared(s *zshSession) error {
	from := len(s.records())
	s.send("v=; vared v\r")
	s.waitFor("vared started", func(r []zrec) bool { return s.index(r, from, "s", "v=; vared v") >= 0 })
	s.echo("VAREDSECRET", "VAREDSECRET")
	s.send("\r")
	r := s.mark("vared")
	if holds(r, "VAREDSECRET") {
		return fmt.Errorf("text typed at vared reached the log: %v", r[from:])
	}
	return nil
}

// scenarioSubshellHup hangs up a background subshell, which inherits
// TRAPHUP, and checks the shell itself stays. The subshell waits in
// zselect, a builtin a signal interrupts, so it is alive until the kill
// and runs its trap at once; a subshell waiting for sleep would run it
// only after sleep ends.
func scenarioSubshellHup(s *zshSession) error {
	from := len(s.records())
	s.send("zmodload zsh/zselect; (zselect -t 6000; true) & print -r $! >sub\r")
	s.waitFor("the subshell started", func(r []zrec) bool {
		return s.index(r, s.index(r, from, "s", ""), "i", "") >= 0
	})
	pid, err := strconv.Atoi(strings.TrimSpace(readFile(s.t, filepath.Join(s.home, "sub"))))
	if err != nil {
		return err
	}
	s.t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		return fmt.Errorf("hang up the subshell %d: %v", pid, err)
	}
	if err := s.await(zshStall, func() bool { return syscall.Kill(pid, 0) != nil }); err != nil {
		return fmt.Errorf("the subshell %d did not exit on SIGHUP: %v", pid, err)
	}
	return s.takes("after the subshell", "after the subshell")
}

// takes sends keys and waits for a b record of want, so the shell is
// still running and saving. It fails as soon as the shell ends.
func (s *zshSession) takes(keys, want string) error {
	s.t.Helper()
	from := len(s.records())
	if _, err := s.pty.WriteString(keys); err != nil {
		return fmt.Errorf("the shell took no keys: %v", err)
	}
	var ended error
	err := s.await(zshStall, func() bool {
		select {
		case err := <-s.ended:
			s.ended <- err
			ended = fmt.Errorf("the shell ended (%v) before it saved %q", err, want)
			return true
		default:
		}
		return s.index(s.records(), from, "b", want) >= 0
	})
	if ended != nil {
		return ended
	}
	if err != nil {
		return fmt.Errorf("the shell saved no %q: %v", want, err)
	}
	return nil
}

// TestZshHooksHupToASubshellSparesTheShell checks the hooks' TRAPHUP, run
// in a subshell, hangs up the subshell and not the shell, and that the check
// goes red when the trap hangs up $$, which a subshell shares with its shell.
func TestZshHooksHupToASubshellSparesTheShell(t *testing.T) {
	if err := scenarioSubshellHup(startZsh(t, zshHooks, true)); err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(zshHooks, "kill -HUP ${sysparams[pid]}", "kill -HUP $$", 1)
	if mutated == zshHooks {
		t.Fatal("the mutation found nothing to change")
	}
	if err := scenarioSubshellHup(startZsh(t, mutated, true)); err == nil {
		t.Fatal("the check passed with the trap hanging up $$")
	}
}

// TestZshHooksRecordTheLine runs the hooks in a real zsh and types at it:
// a line run with Enter, one cleared with Ctrl+C, an open quote, lines
// that start with a space, secrets at vared and read -s, then a closed
// window.
func TestZshHooksRecordTheLine(t *testing.T) {
	s := startZsh(t, zshHooks, false)
	pid := s.cmd.Process.Pid

	// The log: one per shell, named for its host and pid, private.
	logs := s.logs()
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	host = strings.Map(func(r rune) rune {
		if r == '.' || r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			return r
		}
		return '_'
	}, host)
	prefix := fmt.Sprintf("zsh-%s-%d-", host, pid)
	if len(logs) != 1 || !strings.HasPrefix(filepath.Base(logs[0]), prefix) || !strings.HasSuffix(logs[0], ".log") {
		t.Fatalf("logs %v, want one %s*.log in %s", logs, prefix, s.dir)
	}
	if fi, err := os.Stat(logs[0]); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("log mode: %v %v", fi.Mode(), err)
	}
	if fi, err := os.Stat(s.dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("log folder mode: %v %v", fi.Mode(), err)
	}
	r := s.records()
	home, _ := filepath.EvalSymlinks(s.home)
	if len(r) < 2 || r[0] != (zrec{"v", "1"}) || r[1].kind != "i" {
		t.Fatalf("the log starts %v, want v \"1\" then i", r)
	}
	if got, _ := filepath.EvalSymlinks(r[1].text); got != home {
		t.Fatalf("i record holds %q, want the working folder %q", r[1].text, home)
	}

	// Enter: the line, then s with it, then the next prompt.
	from := len(r)
	s.line("echo one")
	s.send("\r")
	r = s.waitFor("s after Enter", func(r []zrec) bool { return s.index(r, from, "s", "echo one") >= 0 })
	sent := s.index(r, from, "s", "echo one")
	r = s.waitFor("i after Enter", func(r []zrec) bool { return s.index(r, sent, "i", "") >= 0 })

	if err := scenarioCtrlC(s); err != nil {
		t.Fatal(err)
	}

	// An open quote: Enter gives s, the continuation prompt c, and the
	// records after it carry the earlier line.
	from = len(s.records())
	s.send(`echo "a` + "\r")
	s.waitFor("c", func(r []zrec) bool { return s.index(r, from, "c", "") >= 0 })
	s.send(`b"`)
	s.waitFor("b of both lines", func(r []zrec) bool { return s.index(r, from, "b", "echo \"a\nb\"") >= 0 })
	s.send("\r")
	r = s.waitFor("s of both lines", func(r []zrec) bool { return s.index(r, from, "s", "echo \"a\nb\"") >= 0 })
	if i := s.index(r, from, "b", "echo \"a\nb\""); i < 0 || s.index(r, from, "s", `echo "a`) > s.index(r, from, "c", "") {
		t.Fatalf("open quote: %v", r[from:])
	}

	// A line that starts with a space writes nothing; one that gains a
	// space at the start gets a forget mark and nothing after it.
	from = len(s.records())
	s.echo(" spaced secret", "spaced secret")
	s.clear()
	r = s.mark("space")
	if holds(r[from:], "spaced secret") {
		t.Fatalf("a line starting with a space reached the log: %v", r[from:])
	}
	from = len(r)
	s.line("later spaced")
	s.send("\x01 ") // Ctrl+A, then a space at the start
	r = s.waitFor("f", func(r []zrec) bool { return s.index(r, from, "f", "") >= 0 })
	f := s.index(r, from, "f", "")
	s.send("\r")
	r = s.mark("forget")
	for _, x := range r[f:] {
		if strings.Contains(x.text, "later spaced") {
			t.Fatalf("a record after the forget mark holds the line: %v", r[from:])
		}
	}

	if err := scenarioVared(s); err != nil {
		t.Fatal(err)
	}

	from = len(s.records())
	s.send("read -s y\r")
	s.waitFor("read -s started", func(r []zrec) bool { return s.index(r, from, "s", "read -s y") >= 0 })
	time.Sleep(200 * time.Millisecond)
	s.send("READSECRET\r")
	r = s.mark("read")
	if holds(r[from:], "READSECRET") {
		t.Fatalf("text typed at read -s reached the log: %v", r[from:])
	}

	// The positive control: the same text typed at the prompt is saved.
	s.line("VAREDSECRET READSECRET")
	s.clear()

	// Commands the shell runs do not inherit the log's descriptor. The
	// same listing with a descriptor passed on purpose shows it can.
	from = len(s.records())
	s.send("print $_unsent_fd >fd; ls /dev/fd >fds; ls /dev/fd 7>/dev/null >fds7\r")
	s.waitFor("the listing ran", func(r []zrec) bool {
		return s.index(r, s.index(r, from, "s", ""), "i", "") >= 0
	})
	fd := strings.TrimSpace(readFile(t, filepath.Join(s.home, "fd")))
	listed := func(name string) []string { return strings.Fields(readFile(t, filepath.Join(s.home, name))) }
	if n, err := strconv.Atoi(fd); err != nil || n < 3 {
		t.Fatalf("the hook's descriptor is %q", fd)
	}
	if slices.Contains(listed("fds"), fd) {
		t.Fatalf("ls inherited descriptor %s: %v", fd, listed("fds"))
	}
	if !slices.Contains(listed("fds7"), "7") {
		t.Fatalf("the control did not see descriptor 7: %v", listed("fds7"))
	}

	// The window closes: TRAPHUP writes the line, and the shell exits.
	s.line("typed then hup")
	syscall.Kill(pid, syscall.SIGHUP)
	s.exits("SIGHUP")
	r = s.records()
	if last := r[len(r)-1]; last != (zrec{"h", "typed then hup"}) {
		t.Fatalf("last record %v, want h \"typed then hup\"", last)
	}
	if len(s.logs()) != 1 {
		t.Fatalf("logs %v, want one", s.logs())
	}
}

// TestZshHooksSurviveKill9 checks a shell killed with kill -9 leaves the
// line in its log, up to the last redraw.
func TestZshHooksSurviveKill9(t *testing.T) {
	s := startZsh(t, zshHooks, true)
	s.line("typed then killed")
	s.cmd.Process.Kill()
	s.exits("kill -9")
	r := s.records()
	if last := r[len(r)-1]; last != (zrec{"b", "typed then killed"}) {
		t.Fatalf("last record %v", last)
	}
}

// TestZshHooksRotate lowers the 256 KB limit to a few bytes and checks
// the next prompt moves the log aside as .done and starts a new one that
// opens with v.
func TestZshHooksRotate(t *testing.T) {
	small := strings.Replace(zshHooks, "_unsent_size > 262144", "_unsent_size > 64", 1)
	if small == zshHooks {
		t.Fatal("the mutation found nothing to change")
	}
	s := startZsh(t, small, true)
	const long = "a line long enough to pass the limit"
	s.line(long)
	s.clear()
	movedAside := func() bool {
		done, _ := filepath.Glob(filepath.Join(s.dir, "zsh-*.done"))
		for _, d := range done {
			if r, err := parseZshLog([]byte(readFile(t, d))); err == nil && s.index(r, 0, "b", long) >= 0 {
				return true
			}
		}
		return false
	}
	s.until(zshStall, movedAside, func() string { return fmt.Sprintf("no .done log holds the line: %v", s.logs()) })
	if n := len(s.logs()); n < 2 {
		t.Fatalf("logs %v", s.logs())
	}
	r := s.waitFor("the new log", func(r []zrec) bool { return len(r) >= 2 && !holds(r, long) })
	if r[0] != (zrec{"v", "1"}) || r[1].kind != "i" {
		t.Fatalf("the new log starts %v", r)
	}
}

// TestZshHooksChecksCanFail proves the Ctrl+C and vared checks go red:
// without the i mark a cleared line runs into the next, and without the
// $CONTEXT check text typed at vared is saved.
func TestZshHooksChecksCanFail(t *testing.T) {
	for _, c := range []struct {
		name, from, to string
		scenario       func(*zshSession) error
	}{
		{"no i mark", `_unsent_put i "$PWD"`, `:`, scenarioCtrlC},
		{"no context check", `[[ $CONTEXT == start || $CONTEXT == cont ]] || return 0`, `:`, scenarioVared},
	} {
		t.Run(c.name, func(t *testing.T) {
			hooks := strings.Replace(zshHooks, c.from, c.to, 1)
			if hooks == zshHooks {
				t.Fatal("the mutation found nothing to change")
			}
			s := startZsh(t, hooks, true)
			if err := c.scenario(s); err == nil {
				t.Fatalf("the check passed with %q removed", c.from)
			}
		})
	}
}

// TestZshHooksKeepTheUsersHupTrap checks a TRAPHUP the user defined before
// the hooks still runs, after the hooks' own last record, and keeps its
// say over whether the shell exits.
func TestZshHooksKeepTheUsersHupTrap(t *testing.T) {
	s := startZsh(t, "function TRAPHUP { print -r mine >$HOME/user-hup; return 0 }\n"+zshHooks, true)
	s.line("typed before hup")
	syscall.Kill(s.cmd.Process.Pid, syscall.SIGHUP)
	s.waitFor("h", func(r []zrec) bool { return s.index(r, 0, "h", "typed before hup") >= 0 })
	s.until(zshStall, func() bool {
		b, err := os.ReadFile(filepath.Join(s.home, "user-hup"))
		return err == nil && string(b) == "mine\n"
	}, func() string { return "the user's TRAPHUP did not run" })
	// The user's trap returned 0, so the shell stays, the line with it.
	s.send(" still")
	s.waitFor("the line going on", func(r []zrec) bool { return s.index(r, 0, "b", "typed before hup still") >= 0 })
}

// scenarioHupInADeepHook hangs up the shell while a line-pre-redraw hook
// of the user's runs under emulate -L, nested deeper than where the hooks
// set TRAPHUP, as the hooks' own _unsent_put is during every redraw. zsh
// puts back a trap removed in a function that has local_traps when that
// function returns, so a TRAPHUP that removes itself and hangs up again
// runs again, one h record each time, forever. The shell must exit with
// one h record.
func scenarioHupInADeepHook(t *testing.T, hooks string) error {
	s := startZsh(t, hooks+`zmodload zsh/zselect
function _deep {
  emulate -L zsh
  [[ -e $HOME/arm ]] || return 0
  zf_rm $HOME/arm
  () { () { () { () { print -rn -- $BUFFER >$HOME/in-hook.t; zf_mv $HOME/in-hook.t $HOME/in-hook; zselect -t 3000 } } } }
}
add-zle-hook-widget line-pre-redraw _deep
`, true)
	arm, in := filepath.Join(s.home, "arm"), filepath.Join(s.home, "in-hook")
	s.line("typed then hup")
	if err := os.WriteFile(arm, nil, 0o644); err != nil {
		return err
	}
	s.send("!")
	if err := s.await(zshStall, func() bool { return exists(in) }); err != nil {
		return fmt.Errorf("the deep hook never ran (%v); the log ends %v", err, s.tail())
	}
	if err := syscall.Kill(s.cmd.Process.Pid, syscall.SIGHUP); err != nil {
		return err
	}
	hups := func() []zrec {
		return slices.DeleteFunc(s.records(), func(r zrec) bool { return r.kind != "h" })
	}
	for deadline := time.Now().Add(30 * time.Second); ; {
		select {
		case err := <-s.ended:
			s.ended <- err
			// The hook may arm in the redraw before the ! key, whose
			// record the test saw; the h record holds the line it saw.
			line, err := os.ReadFile(in)
			if err != nil {
				return err
			}
			if h := hups(); !slices.Equal(h, []zrec{{"h", string(line)}}) {
				return fmt.Errorf("the shell exited with h records %v, want one of the line", h)
			}
			return nil
		case <-time.After(10 * time.Millisecond):
		}
		if h := hups(); len(h) > 1 || time.Now().After(deadline) {
			s.cmd.Process.Kill()
			return fmt.Errorf("the shell did not exit on SIGHUP; %d h records, the log ends %v", len(h), s.tail())
		}
	}
}

// TestZshHooksExitOnAHupInADeepHook runs scenarioHupInADeepHook with the
// hooks alone and after a user's TRAPHUP that exits the way CLAUDE.md
// says the default does, which the hooks' trap calls. Each runs again
// with a TRAPHUP that removes itself under the caller's local_traps,
// where the shell loops and the check must go red for that reason.
func TestZshHooksExitOnAHupInADeepHook(t *testing.T) {
	const userTrap = "function TRAPHUP { unfunction TRAPHUP; kill -HUP $$ }\n"
	for _, c := range []struct{ name, hooks string }{
		{"hooks", zshHooks},
		{"user trap", userTrap + zshHooks},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := scenarioHupInADeepHook(t, c.hooks); err != nil {
				t.Fatal(err)
			}
			looping := strings.Replace(c.hooks, "      setopt local_options no_local_traps\n", "", 1)
			if looping == c.hooks {
				t.Fatal("the mutation found nothing to change")
			}
			err := scenarioHupInADeepHook(t, looping)
			if err == nil || !strings.Contains(err.Error(), "did not exit on SIGHUP") {
				t.Fatalf("the check did not fail on a TRAPHUP that zsh puts back when it returns: %v", err)
			}
		})
	}
}

// TestZshHooksUnderUserOptions runs the Ctrl+C and vared checks with
// options a user's rc file may set before the block. zsh's own
// add-zle-hook-widget fails under sh_glob unless loaded as zsh, and
// $commands misses an unhashed unsent under no_hash_list_all.
func TestZshHooksUnderUserOptions(t *testing.T) {
	s := startZsh(t, "setopt no_unset ksh_arrays sh_word_split sh_glob no_hash_list_all\n"+zshHooks, true)
	if err := scenarioCtrlC(s); err != nil {
		t.Fatal(err)
	}
	if err := scenarioVared(s); err != nil {
		t.Fatal(err)
	}
}

// TestZshHooksRunBeforeAUsersFailingHook registers hooks of the user's
// before the block that return 1: a zle-line-init widget defined directly
// and line-pre-redraw and line-finish widgets added with
// add-zle-hook-widget. zsh's dispatcher stops at the first widget that
// fails, so the block must put its own first: every record still comes,
// and the user's widgets still run. It runs again under the options of
// TestZshHooksUnderUserOptions, set after the user's hooks. There the user
// loads add-zle-hook-widget under zsh emulation as the block does: loaded
// plainly, its dispatcher fails under sh_glob for every widget, the
// user's own included, and prints that at each key.
func TestZshHooksRunBeforeAUsersFailingHook(t *testing.T) {
	const mine = `
function _mine { print -rn -- "$WIDGET " >>$HOME/mine; return 1 }
zle -N zle-line-init _mine
zle -N _mine_redraw _mine
zle -N _mine_finish _mine
add-zle-hook-widget line-pre-redraw _mine_redraw
add-zle-hook-widget line-finish _mine_finish
`
	for _, c := range []struct{ name, load, opts string }{
		{"plain", "autoload -Uz add-zle-hook-widget", ""},
		{"user options", "emulate zsh -c 'autoload -Uz add-zle-hook-widget'", "setopt no_unset ksh_arrays sh_word_split sh_glob no_hash_list_all\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := startZsh(t, c.load+mine+c.opts+zshHooks, true)
			from := len(s.records())
			s.line("echo ran")
			s.send("\r")
			s.waitFor("s then i after Enter", func(r []zrec) bool { return s.index(r, s.index(r, from, "s", "echo ran"), "i", "") >= 0 })
			f := filepath.Join(s.home, "mine")
			var ran string
			s.until(zshStall, func() bool {
				b, _ := os.ReadFile(f)
				ran = string(b)
				return strings.Contains(ran, "zle-line-init ") && strings.Contains(ran, "_mine_redraw ") && strings.Contains(ran, "_mine_finish ")
			}, func() string { return fmt.Sprintf("the user's hooks ran: %q", ran) })
			if err := scenarioCtrlC(s); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// zshUsersHooks are hooks of the user's, set before the block, that write
// each widget's name to $HOME/mine and return 0. Ours run first, and the
// dispatcher stops at the first widget that returns non-zero, so ours must
// return 0 on every path for these to run.
const zshUsersHooks = `autoload -Uz add-zle-hook-widget
function _mine { print -rn -- "$WIDGET " >>$HOME/mine; return 0 }
zle -N zle-line-init _mine
zle -N _mine_redraw _mine
zle -N _mine_finish _mine
add-zle-hook-widget line-pre-redraw _mine_redraw
add-zle-hook-widget line-finish _mine_finish
`

// minesRan waits for the user's widgets in want to have written to
// $HOME/mine.
func minesRan(s *zshSession, want ...string) error {
	f := filepath.Join(s.home, "mine")
	var ran string
	if err := s.await(zshStall, func() bool {
		b, _ := os.ReadFile(f)
		ran = string(b)
		for _, w := range want {
			if !strings.Contains(ran, w+" ") {
				return false
			}
		}
		return true
	}); err != nil {
		return fmt.Errorf("the user's hooks %q did not all run (%v); they wrote %q", want, err, ran)
	}
	return nil
}

// scenarioUsersHooksWithoutUnsent runs the user's hooks with no unsent on
// PATH, where every _unsent_put fails: typing, Enter and the next prompt
// must still run the user's line-pre-redraw, line-finish and line-init.
func scenarioUsersHooksWithoutUnsent(t *testing.T, hooks string) error {
	home := t.TempDir()
	hookFile := filepath.Join(home, "hooks.zsh")
	if err := os.WriteFile(hookFile, []byte(zshUsersHooks+hooks), 0o644); err != nil {
		return err
	}
	env := []string{"HOME=" + home, "ZDOTDIR=" + home, "PATH=/usr/bin:/bin", "TERM=xterm", "PS1=" + zshPrompt}
	s := openZsh(t, exec.Command(testZsh(t), "-f", "-i"), env, home, filepath.Join(home, ".local", "state", "unsent", "shell"))
	s.prompt(0, zshStart)
	n := s.outLen()
	s.send("source " + hookFile + "\r")
	s.prompt(n, zshStart)
	if err := os.WriteFile(filepath.Join(home, "mine"), nil, 0o644); err != nil {
		return err
	}
	s.echo("echo hi", "echo hi")
	if err := minesRan(s, "_mine_redraw"); err != nil {
		return err
	}
	s.send("\r")
	if err := minesRan(s, "_mine_redraw", "_mine_finish", "zle-line-init"); err != nil {
		return err
	}
	if logs := s.logs(); len(logs) != 0 {
		return fmt.Errorf("a shell with no unsent on PATH wrote logs %v", logs)
	}
	return nil
}

// scenarioUsersHooksOnACursorMove moves the cursor over a saved line, a
// redraw whose text is unchanged, so ours writes nothing: the user's
// line-pre-redraw, the kind a syntax highlighter uses, must still run.
func scenarioUsersHooksOnACursorMove(t *testing.T, hooks string) error {
	s := startZsh(t, zshUsersHooks+hooks, true)
	s.line("abc")
	if err := os.WriteFile(filepath.Join(s.home, "mine"), nil, 0o644); err != nil {
		return err
	}
	s.send("\x1b[D")
	return minesRan(s, "_mine_redraw")
}

// TestZshHooksLetTheUsersHooksRun runs the user's hooks after ours with no
// unsent on PATH and on a cursor move, then again with each of our
// widgets returning non-zero on that path, where the check must go red.
func TestZshHooksLetTheUsersHooksRun(t *testing.T) {
	for _, c := range []struct {
		name, from, to string
		scenario       func(*testing.T, string) error
	}{
		{"line fails", "    _unsent_put $1 \"$t\" && (( ++_unsent_n ))\n    return 0\n", "    _unsent_put $1 \"$t\" && (( ++_unsent_n ))\n", scenarioUsersHooksWithoutUnsent},
		{"init fails", "      _unsent_put i \"$PWD\"\n    fi\n    return 0\n", "      _unsent_put i \"$PWD\"\n    fi\n", scenarioUsersHooksWithoutUnsent},
		{"unchanged redraw fails", `[[ $1 == b && $t == "$_unsent_last" ]] && return 0`, `[[ $1 == b && $t == "$_unsent_last" ]] && return 1`, scenarioUsersHooksOnACursorMove},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := c.scenario(t, zshHooks); err != nil {
				t.Fatal(err)
			}
			hooks := strings.Replace(zshHooks, c.from, c.to, 1)
			if hooks == zshHooks {
				t.Fatal("the mutation found nothing to change")
			}
			err := c.scenario(t, hooks)
			if err == nil || !strings.Contains(err.Error(), "did not all run") {
				t.Fatalf("the check did not fail with our widget returning non-zero: %v", err)
			}
		})
	}
}

// scenarioListHupTrap hangs up a shell whose rc file set trap '...' HUP
// before the hooks: the user's command runs, the shell stays, and the
// hooks write no h record over the user's trap.
func scenarioListHupTrap(s *zshSession) error {
	s.line("typed before hup")
	if err := syscall.Kill(s.cmd.Process.Pid, syscall.SIGHUP); err != nil {
		return err
	}
	if err := s.takes(" still", "typed before hup still"); err != nil {
		return err
	}
	if b, err := os.ReadFile(filepath.Join(s.home, "user-hup")); err != nil || string(b) != "mine\n" {
		return fmt.Errorf("the user's HUP trap did not run: %q %v", b, err)
	}
	if s.index(s.records(), 0, "h", "") >= 0 {
		return errors.New("the hooks wrote an h record in place of the user's trap")
	}
	return nil
}

// scenarioIgnoredHup does the same for an empty HUP trap: the shell ignores the
// hangup, as the user asked.
func scenarioIgnoredHup(s *zshSession) error {
	s.line("typed before hup")
	if err := syscall.Kill(s.cmd.Process.Pid, syscall.SIGHUP); err != nil {
		return err
	}
	return s.takes(" still", "typed before hup still")
}

// TestZshHooksLeaveAListHupTrap checks the hooks set no TRAPHUP over a
// list-form HUP trap, which a TRAPHUP function would replace, and that the
// checks go red when they do.
func TestZshHooksLeaveAListHupTrap(t *testing.T) {
	cases := []struct {
		name, rc string
		scenario func(*zshSession) error
	}{
		{"command", "trap 'print -r mine >$HOME/user-hup' HUP\n", scenarioListHupTrap},
		{"ignore", "trap '' HUP\n", scenarioIgnoredHup},
	}
	check := `(( ${#${(M)${(f)l}:#trap -- * HUP}} )) && return 0`
	blind := strings.Replace(zshHooks, check, ":", 1)
	if blind == zshHooks {
		t.Fatal("the mutation found nothing to change")
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.scenario(startZsh(t, c.rc+zshHooks, true)); err != nil {
				t.Fatal(err)
			}
			if err := c.scenario(startZsh(t, c.rc+blind, true)); err == nil {
				t.Fatal("the check passed with the hooks replacing the user's trap")
			}
		})
	}
}

// scenarioFailedWrite closes the log's descriptor behind the hooks' back,
// so the next write fails. The log must move aside as .done and the next
// prompt must open a fresh log that starts with v and saves again.
func scenarioFailedWrite(s *zshSession) error {
	s.run("exec {_unsent_fd}>&-")
	done, _ := filepath.Glob(filepath.Join(s.dir, "zsh-*.done"))
	if len(done) != 1 {
		return fmt.Errorf("after a failed write the logs are %v, want one .done", s.logs())
	}
	r, err := parseZshLog([]byte(readFile(s.t, done[0])))
	if err != nil || s.index(r, 0, "s", "exec {_unsent_fd}>&-") < 0 {
		return fmt.Errorf("the .done log holds %v, %v", r, err)
	}
	s.clear()
	if err := s.takes("after the failed write", "after the failed write"); err != nil {
		return err
	}
	if r := s.records(); r[0] != (zrec{"v", "1"}) || r[1].kind != "i" {
		return fmt.Errorf("the fresh log starts %v", r)
	}
	return nil
}

// TestZshHooksRecoverFromAFailedWrite runs scenarioFailedWrite, and again
// with the hooks keeping the failed descriptor, where it must go red.
func TestZshHooksRecoverFromAFailedWrite(t *testing.T) {
	if err := scenarioFailedWrite(startZsh(t, zshHooks, true)); err != nil {
		t.Fatal(err)
	}
	stuck := strings.Replace(zshHooks, "      _unsent_shut -2\n", "      :\n", 1)
	if stuck == zshHooks {
		t.Fatal("the mutation found nothing to change")
	}
	if err := scenarioFailedWrite(startZsh(t, stuck, true)); err == nil {
		t.Fatal("the check passed with the hooks keeping a failed descriptor")
	}
}

// scenarioLogGone deletes the live log behind the hooks' back, as an
// importer that takes the shell for dead does. The next prompt must open
// a fresh log that saves again.
func scenarioLogGone(s *zshSession) error {
	s.line("before the log went")
	for _, l := range s.logs() {
		os.Remove(l)
	}
	s.clear()
	return s.takes("after the log went", "after the log went")
}

// TestZshHooksReopenAGoneLog runs scenarioLogGone, and again with hooks
// that never check the log is there, where it must go red.
func TestZshHooksReopenAGoneLog(t *testing.T) {
	if err := scenarioLogGone(startZsh(t, zshHooks, true)); err != nil {
		t.Fatal(err)
	}
	blind := strings.Replace(zshHooks, "(( _unsent_fd >= 0 )) && [[ ! -e $_unsent_log ]]", "(( 0 ))", 1)
	if blind == zshHooks {
		t.Fatal("the mutation found nothing to change")
	}
	if err := scenarioLogGone(startZsh(t, blind, true)); err == nil {
		t.Fatal("the check passed with hooks that write on into a deleted log")
	}
}

// scenarioForeignFolder points UNSENT_HOME below a folder another user
// owns, root's /tmp, and makes the hooks open a new log there: they must
// create nothing and save nothing. Pointed back at a folder of the
// shell's own user, they save again.
func scenarioForeignFolder(s *zshSession, foreign string) error {
	s.run("UNSENT_HOME=" + foreign + "; exec {_unsent_fd}>&-")
	s.clear()
	s.echo("not saved", "not saved")
	if _, err := os.Lstat(foreign); !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("the hooks wrote into %s, a folder of another user (%v)", foreign, err)
	}
	s.clear()
	s.run("UNSENT_HOME=$HOME/mine")
	mine, _ := filepath.Glob(filepath.Join(s.home, "mine", "shell", "zsh-*.log"))
	if len(mine) != 1 {
		return fmt.Errorf("no log under the shell's own folder: %v", mine)
	}
	return nil
}

// TestZshHooksSkipAFolderOfAnotherUser checks the hooks write only where
// the shell's user owns the nearest folder that exists, as a root shell
// reading the user's rc file needs, and that the check goes red without
// the ownership test.
func TestZshHooksSkipAFolderOfAnotherUser(t *testing.T) {
	fi, err := os.Stat("/tmp")
	if err != nil || os.Geteuid() == 0 || fi.Sys().(*syscall.Stat_t).Uid != 0 || fi.Mode()&0o002 == 0 {
		t.Skip("needs a /tmp that root owns and anyone may write, and a test not run as root")
	}
	foreign := func(t *testing.T) string {
		p := filepath.Join("/tmp", fmt.Sprintf("unsent-owner-%d-%d", os.Getpid(), time.Now().UnixNano()))
		t.Cleanup(func() { os.RemoveAll(p) })
		return p
	}
	if err := scenarioForeignFolder(startZsh(t, zshHooks, true), foreign(t)); err != nil {
		t.Fatal(err)
	}
	trusting := strings.Replace(zshHooks, "[[ -O $p ]] || return 1", ":", 1)
	if trusting == zshHooks {
		t.Fatal("the mutation found nothing to change")
	}
	if err := scenarioForeignFolder(startZsh(t, trusting, true), foreign(t)); err == nil {
		t.Fatal("the check passed with the hooks writing into another user's folder")
	}
}

// utf8Locale finds a UTF-8 locale this zsh counts characters in, as a
// user's terminal runs it.
func utf8Locale(t *testing.T) string {
	sh := testZsh(t)
	for _, loc := range []string{"C.UTF-8", "en_US.UTF-8"} {
		cmd := exec.Command(sh, "-f", "-c", "print -r ${#${:-é}}")
		cmd.Env = []string{"LC_ALL=" + loc, "PATH=/usr/bin:/bin"}
		if out, err := cmd.Output(); err == nil && strings.TrimSpace(string(out)) == "1" {
			return loc
		}
	}
	t.Skip("no UTF-8 locale zsh counts characters in")
	return ""
}

// scenarioMultibyte types a line with characters of more than one byte
// and checks its record comes back exactly: the header counts bytes, not
// characters, even where zsh counts characters.
func scenarioMultibyte(s *zshSession) error {
	s.run("print -r ${#${:-é}} >len")
	if n := strings.TrimSpace(readFile(s.t, filepath.Join(s.home, "len"))); n != "1" {
		return fmt.Errorf("zsh counted %s characters in é; the locale did not take", n)
	}
	const text = "echo héllo ñ"
	s.echo(text, "ñ")
	m, _ := filepath.Glob(filepath.Join(s.dir, "zsh-*.log"))
	if len(m) != 1 {
		return fmt.Errorf("logs %v", s.logs())
	}
	r, err := parseZshLog([]byte(readFile(s.t, m[0])))
	if err != nil {
		return err
	}
	if s.index(r, 0, "b", text) < 0 {
		return fmt.Errorf("no b record of %q: %v", text, r)
	}
	return nil
}

// TestZshHooksCountBytesInAUTF8Locale runs scenarioMultibyte in a UTF-8
// locale, and again without setopt no_multibyte, where it must go red.
func TestZshHooksCountBytesInAUTF8Locale(t *testing.T) {
	loc := "LC_ALL=" + utf8Locale(t)
	if err := scenarioMultibyte(startZsh(t, zshHooks, true, loc)); err != nil {
		t.Fatal(err)
	}
	chars := strings.Replace(zshHooks, "    setopt no_multibyte\n", "", 1)
	if chars == zshHooks {
		t.Fatal("the mutation found nothing to change")
	}
	if err := scenarioMultibyte(startZsh(t, chars, true, loc)); err == nil {
		t.Fatal("the check passed with the header counting characters")
	}
}

// TestZshHooksLoadedTwice loads the hooks twice before the first prompt,
// as the setup block plus eval "$(unsent init zsh)" does, and once more at
// a prompt, as source ~/.zshrc does: one log, and a hangup still writes h
// and ends the shell.
func TestZshHooksLoadedTwice(t *testing.T) {
	s := startZsh(t, zshHooks+zshHooks, true)
	from := len(s.records())
	s.send("source $HOME/hooks.zsh\r")
	s.waitFor("the second source", func(r []zrec) bool {
		return s.index(r, s.index(r, from, "s", "source $HOME/hooks.zsh"), "i", "") >= 0
	})
	s.line("typed then hup")
	if err := syscall.Kill(s.cmd.Process.Pid, syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	s.exits("SIGHUP")
	if len(s.logs()) != 1 {
		t.Fatalf("logs %v, want one", s.logs())
	}
	r := s.records()
	if last := r[len(r)-1]; last != (zrec{"h", "typed then hup"}) {
		t.Fatalf("last record %v, want h \"typed then hup\"", last)
	}
}
