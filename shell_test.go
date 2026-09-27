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
	hookFile := filepath.Join(home, "hooks.zsh")
	if err := os.WriteFile(hookFile, []byte(hooks), 0o644); err != nil {
		t.Fatal(err)
	}
	env = append([]string{"HOME=" + home, "ZDOTDIR=" + home, "PATH=" + agentBin + ":/usr/bin:/bin", "TERM=xterm"}, env...)
	dir := filepath.Join(home, ".local", "state", "unsent", "shell")
	if unsentHome {
		env = append(env, "UNSENT_HOME="+filepath.Join(home, "state"))
		dir = filepath.Join(home, "state", "shell")
	}
	cmd := exec.Command(sh, "-f", "-i")
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
	s.send("PS1='" + zshPrompt[:1] + "''" + zshPrompt[1:] + "'; source " + hookFile + "\r")
	s.mark("ready")
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
	s.mu.Lock()
	n := s.out.Len()
	s.mu.Unlock()
	s.send(keys)
	deadline := time.Now().Add(15 * time.Second)
	for {
		s.mu.Lock()
		out := s.out.String()[n:]
		s.mu.Unlock()
		if strings.Contains(out, want) {
			return
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("waited for %q on the terminal; got %q", want, out)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// zshPrompt is the test shell's prompt. The line that sets it quotes it
// in two parts, so its echo does not show it.
const zshPrompt = "<zsh-ready> "

// clear presses Ctrl+C and waits for the new prompt. The terminal drops
// input still queued when Ctrl+C arrives, as the Linux one does, so keys
// sent before the prompt shows may never reach the shell.
func (s *zshSession) clear() {
	s.t.Helper()
	s.echo("\x03", zshPrompt)
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

func (s *zshSession) waitFor(what string, ok func([]zrec) bool) []zrec {
	s.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		r := s.records()
		if ok(r) {
			return r
		}
		if time.Now().After(deadline) {
			s.mu.Lock()
			out := s.out.String()
			s.mu.Unlock()
			s.t.Fatalf("waited for %s; the log holds %v\nterminal: %q", what, r, out)
		}
		time.Sleep(20 * time.Millisecond)
	}
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
	deadline := time.Now().Add(10 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			return fmt.Errorf("the subshell %d did not exit on SIGHUP", pid)
		}
		time.Sleep(20 * time.Millisecond)
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
	deadline := time.Now().Add(15 * time.Second)
	for s.index(s.records(), from, "b", want) < 0 {
		select {
		case err := <-s.ended:
			s.ended <- err
			return fmt.Errorf("the shell ended (%v) before it saved %q", err, want)
		case <-time.After(20 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the shell saved no %q", want)
		}
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
	select {
	case <-s.ended:
	case <-time.After(10 * time.Second):
		t.Fatal("zsh did not exit on SIGHUP")
	}
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
	<-s.ended
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
	deadline := time.Now().Add(10 * time.Second)
	for !movedAside() {
		if time.Now().After(deadline) {
			t.Fatalf("no .done log holds the line: %v", s.logs())
		}
		time.Sleep(20 * time.Millisecond)
	}
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
	deadline := time.Now().Add(10 * time.Second)
	for {
		if b, err := os.ReadFile(filepath.Join(s.home, "user-hup")); err == nil && string(b) == "mine\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the user's TRAPHUP did not run")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The user's trap returned 0, so the shell stays, the line with it.
	s.send(" still")
	s.waitFor("the line going on", func(r []zrec) bool { return s.index(r, 0, "b", "typed before hup still") >= 0 })
}

// TestZshHooksUnderUserOptions runs the Ctrl+C and vared checks with
// options a user's rc file may set before the block. zsh's own
// add-zle-hook-widget fails under sh_glob unless loaded as zsh.
func TestZshHooksUnderUserOptions(t *testing.T) {
	s := startZsh(t, "setopt no_unset ksh_arrays sh_word_split sh_glob\n"+zshHooks, true)
	if err := scenarioCtrlC(s); err != nil {
		t.Fatal(err)
	}
	if err := scenarioVared(s); err != nil {
		t.Fatal(err)
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
	s.send("exec {_unsent_fd}>&-\r")
	s.echo("x", "x") // the next prompt's line-init has run
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
	s.send("UNSENT_HOME=" + foreign + "; exec {_unsent_fd}>&-\r")
	s.echo("x", "x")
	s.clear()
	s.echo("not saved", "not saved")
	if _, err := os.Lstat(foreign); !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("the hooks wrote into %s, a folder of another user (%v)", foreign, err)
	}
	s.clear()
	s.send("UNSENT_HOME=$HOME/mine\r")
	s.echo("q", "q") // not "y": the prompt holds one, which can show before line-init runs
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
	s.send("print -r ${#${:-é}} >len\r")
	s.echo("x", "x")
	if n := strings.TrimSpace(readFile(s.t, filepath.Join(s.home, "len"))); n != "1" {
		return fmt.Errorf("zsh counted %s characters in é; the locale did not take", n)
	}
	s.clear()
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
	select {
	case <-s.ended:
	case <-time.After(10 * time.Second):
		t.Fatal("zsh did not exit on SIGHUP")
	}
	if len(s.logs()) != 1 {
		t.Fatalf("logs %v, want one", s.logs())
	}
	r := s.records()
	if last := r[len(r)-1]; last != (zrec{"h", "typed then hup"}) {
		t.Fatalf("last record %v, want h \"typed then hup\"", last)
	}
}
