package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"golang.org/x/term"
)

// The test binary doubles as a fake agent that draws a box like Claude
// Code's, so the wrapper can be tested end to end without the real thing.
func TestMain(m *testing.M) {
	// The test binary as unsent in front of the fake agent: only the
	// agent it starts is the fake one.
	if os.Getenv("UNSENT_TEST_RUN") == "wrap" {
		os.Unsetenv("UNSENT_TEST_RUN")
		os.Setenv("UNSENT_FAKE_AGENT", "1")
		if os.Getenv("UNSENT_TEST_PTY") == "fail" {
			os.Unsetenv("UNSENT_TEST_PTY")
			startPty = func(*exec.Cmd, *pty.Winsize) (*os.File, error) {
				return nil, errors.New("forced pty failure")
			}
		}
		exitAs(run(os.Args[1:], os.Stdout, os.Stderr))
	}
	// The test binary as unsent at its end, after the agent was killed by
	// signal UNSENT_TEST_KILLED_BY, with the stop signals armed and
	// disarmed as wrap does.
	if os.Getenv("UNSENT_TEST_RUN") == "exit" {
		n, _ := strconv.Atoi(os.Getenv("UNSENT_TEST_KILLED_BY"))
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
		signal.Stop(sigs)
		killedBy = syscall.Signal(n)
		exitAs(128 + n)
	}
	if os.Getenv("UNSENT_FAKE_AGENT") == "1" {
		fakeAgent()
		return
	}
	// The test binary as unsent itself, for tests that need it in a
	// terminal of its own.
	if os.Getenv("UNSENT_TEST_RUN") == "1" {
		exitAs(run(os.Args[1:], os.Stdout, os.Stderr))
	}
	// No test reads a real Claude Code config: its session files name the
	// user's own conversations.
	cfg, err := os.MkdirTemp("", "unsent-claude-config-")
	if err != nil {
		panic(err)
	}
	os.Setenv("CLAUDE_CONFIG_DIR", cfg)
	code := m.Run()
	os.RemoveAll(cfg)
	os.Exit(code)
}

// fakeSession writes the fake agent's session file the way Claude Code
// writes sessions/<pid>.json, with the peer token file beside it.
func fakeSession(id string) {
	dir := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "sessions")
	os.MkdirAll(dir, 0o700)
	pid := os.Getpid()
	b, _ := json.Marshal(map[string]any{"pid": pid, "sessionId": id, "startedAt": time.Now().UnixMilli(), "kind": "interactive"})
	os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.json", pid)), b, 0o644)
	os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.0123abcd.key", pid)), []byte("peer token"), 0o600)
}

func fakeAgent() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		// UNSENT_FAKE_VERSION names a file that, once written, holds the
		// version of an update installed while the agent runs.
		v, err := os.ReadFile(os.Getenv("UNSENT_FAKE_VERSION"))
		if err != nil {
			v = []byte("9.9.9")
		}
		fmt.Printf("%s (Fake Agent)\n", v)
		return
	}
	// UNSENT_FAKE_STARTED names a file the agent creates as it starts.
	if f := os.Getenv("UNSENT_FAKE_STARTED"); f != "" {
		os.WriteFile(f, nil, 0o600)
	}
	// UNSENT_FAKE_HUP makes the agent hang itself up at start: it lives on
	// only if it was started with the hang-up ignored.
	if os.Getenv("UNSENT_FAKE_HUP") == "1" {
		syscall.Kill(os.Getpid(), syscall.SIGHUP)
	}
	// UNSENT_FAKE_STDERR is what the agent writes to its stderr at start.
	os.Stderr.WriteString(os.Getenv("UNSENT_FAKE_STDERR"))
	// UNSENT_FAKE_SESSION is the session the agent is in, in a session file
	// as Claude Code keeps it; Ctrl+O switches to UNSENT_FAKE_SESSION_NEXT
	// in place, as /resume does. The files go when the agent exits. As in
	// Claude Code, --resume <id> opens that session, --resume alone opens a
	// picker with a fresh session until Enter chooses UNSENT_FAKE_SESSION,
	// and --fork-session starts a session of its own.
	id, picking := os.Getenv("UNSENT_FAKE_SESSION"), false
	for i, a := range os.Args[1:] {
		switch {
		case a == "--fork-session":
			id = fmt.Sprintf("forked-%d", os.Getpid())
		case a == "--resume" && i+2 < len(os.Args):
			id = os.Args[i+2]
		case a == "--resume":
			picking = true
		}
	}
	if picking {
		fakeSession(fmt.Sprintf("fresh-%d", os.Getpid()))
	} else if id != "" {
		fakeSession(id)
	}
	if id != "" || picking {
		defer func() {
			names, _ := filepath.Glob(filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "sessions", fmt.Sprintf("%d.*", os.Getpid())))
			for _, n := range names {
				os.Remove(n)
			}
		}()
	}
	if old, err := term.MakeRaw(0); err == nil {
		defer term.Restore(0, old)
	}
	cols, _, err := term.GetSize(0)
	if err != nil {
		cols = 80
	}
	var draft []string
	pastes := 0
	var pasting, esc, away bool
	var paste strings.Builder
	// A picker (the --resume one, or the one /resume opens) hides the box
	// until Enter chooses; so does a menu (away).
	draw := func() {
		switch {
		case picking:
			os.Stdout.WriteString("\x1b[H\x1b[2JResume session\r\n   ❯ a conversation\r\n")
		case away:
			os.Stdout.WriteString("\x1b[H\x1b[2JTasks\r\n  nothing running\r\n")
		default:
			os.Stdout.WriteString(drawBox(cols, strings.Join(draft, "")))
		}
	}
	// The alternate screen, as Claude Code's fullscreen renderer uses it,
	// unless UNSENT_FAKE_MAIN_SCREEN draws on the main screen, below what
	// the terminal held, as its default renderer does. Bracketed paste on,
	// as Claude Code turns it on.
	if os.Getenv("UNSENT_FAKE_MAIN_SCREEN") != "1" {
		os.Stdout.WriteString("\x1b[?1049h")
	}
	os.Stdout.WriteString("\x1b[?2004h")
	draw()
	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		in := string(buf[:n])
		wasEsc := esc
		esc = false
		for len(in) > 0 {
			switch {
			case pasting && strings.HasPrefix(in, "\x1b[201~"):
				pasting = false
				text := paste.String()
				paste.Reset()
				if nl := strings.Count(text, "\r"); nl > 2 {
					pastes++
					draft = append(draft, fmt.Sprintf("[Pasted text #%d +%d lines]", pastes, nl))
				} else {
					draft = append(draft, strings.ReplaceAll(text, "\r", "\n"))
				}
				in = in[6:]
			case pasting:
				paste.WriteByte(in[0])
				in = in[1:]
			case strings.HasPrefix(in, "\x1b[200~"):
				pasting = true
				in = in[6:]
			case picking && in[0] == '\r': // the picker's choice
				picking = false
				fakeSession(os.Getenv("UNSENT_FAKE_SESSION"))
				in = in[1:]
			case picking:
				in = in[1:]
			case away && in[0] == 4: // Ctrl+D quits from a menu too
				return
			case away: // Ctrl+T closes the menu; other keys do nothing
				away = in[0] != 0x14
				in = in[1:]
			case in[0] == 0x14: // Ctrl+T opens a menu with no box, keeping the draft
				away = true
				in = in[1:]
			case in[0] == '\r' && strings.Join(draft, "") == "/tasks":
				// A command that opens a menu, as /config does.
				draft, away = nil, true
				in = in[1:]
			case in[0] == '\r' && strings.HasPrefix(strings.Join(draft, ""), "/resume "):
				// /resume <id> switches in place: no picker, the box stays.
				fakeSession(strings.TrimPrefix(strings.Join(draft, ""), "/resume "))
				draft = nil
				in = in[1:]
			case len(in) >= 3 && in[:2] == "\x1b[" && strings.IndexByte("ABCDIO", in[2]) >= 0:
				// Arrow keys and focus reports change nothing in the box.
				in = in[3:]
			case in[0] == '\r' && strings.Join(draft, "") == "/resume":
				// The /resume picker keeps the session until the choice.
				draft, picking = nil, true
				os.Setenv("UNSENT_FAKE_SESSION", os.Getenv("UNSENT_FAKE_SESSION_NEXT"))
				in = in[1:]
			case in[0] == '\r' && strings.Join(draft, "") == "/exit":
				// Claude Code leaves the screen without drawing an empty box.
				os.Stdout.WriteString("\x1b[H\x1b[2Jbye\r\n")
				return
			case in[0] == '\r' && strings.Join(draft, "") == "/clear":
				draft = nil
				fakeSession(fmt.Sprintf("cleared-%d", os.Getpid()))
				in = in[1:]
			case strings.HasPrefix(in, "\x1b\r"):
				draft = append(draft, "\n")
				in = in[2:]
			case strings.HasPrefix(in, "\x1b\x1b"): // Esc Esc clears the box
				draft = nil
				in = in[2:]
			case in == "\x1b": // a lone Esc: a second one clears the box
				if wasEsc {
					draft = nil
				}
				esc = !wasEsc
				in = ""
			case in[0] == 4: // Ctrl+D quits
				return
			case in[0] == 0x0f: // Ctrl+O switches session, as /resume does
				fakeSession(os.Getenv("UNSENT_FAKE_SESSION_NEXT"))
				in = in[1:]
			case in[0] == 5: // Ctrl+E quits with an error, or dies of UNSENT_FAKE_SIGNAL
				if n, err := strconv.Atoi(os.Getenv("UNSENT_FAKE_SIGNAL")); err == nil {
					syscall.Kill(os.Getpid(), syscall.Signal(n))
					time.Sleep(10 * time.Second)
				}
				os.Exit(3)
			case in[0] == 7: // Ctrl+G hands the draft to $VISUAL, then $EDITOR
				editDraft(strings.Join(draft, ""))
				in = in[1:]
			case in[0] == '\r' || in[0] == 3: // send, or Ctrl+C: the box empties
				draft = nil
				in = in[1:]
			default:
				draft = append(draft, in[:1])
				in = in[1:]
			}
		}
		draw()
	}
}

// editDraft writes the fake agent's draft to a file and runs the editor on
// it, as Claude Code's Ctrl+G does.
func editDraft(draft string) {
	f, err := os.CreateTemp("", "fake-prompt-*.md")
	if err != nil {
		return
	}
	defer os.Remove(f.Name())
	f.WriteString(draft)
	f.Close()
	// Readable by all, as a file written under the usual umask is: an
	// editor copy is private only because the capture editor makes it so.
	os.Chmod(f.Name(), 0o644)
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	exec.Command(editor, f.Name()).Run()
}

// drawBox is the fake agent's screen with text in its box, drawn the way
// Claude Code draws it: an empty box shows a dim hint.
func drawBox(cols int, text string) string {
	var b strings.Builder
	b.WriteString("\x1b[H\x1b[2J")
	rule := strings.Repeat("─", cols)
	b.WriteString(rule + "\r\n")
	if text == "" {
		b.WriteString("❯\u00a0\x1b[2mTry \"something\"\x1b[m\r\n")
	} else {
		for i, r := range wrapText(text, cols-4) {
			prefix := "  "
			if i == 0 {
				prefix = "❯\u00a0"
			}
			b.WriteString(prefix + r + "\r\n")
		}
	}
	b.WriteString(rule + "\r\n")
	return b.String()
}

// runWrapped runs wrap on the fake agent with keys typed through a pipe,
// and returns its exit code and the store it wrote to.
func runWrapped(t *testing.T, script func(type_ func(string))) (int, *store) {
	t.Helper()
	return runWrappedAs(t, "claude", []string{"claude"}, script)
}

// runWrappedAs is runWrapped with the fake agent installed as command and
// started through run with argv, so the agent is named the way the command
// line names it.
func runWrappedAs(t *testing.T, command string, argv []string, script func(type_ func(string))) (int, *store) {
	t.Helper()
	code, st, _, _ := runWrappedOut(t, command, argv, script)
	return code, st
}

// runWrappedOut is runWrappedAs that also returns everything the terminal
// showed and what unsent printed on its own stderr.
func runWrappedOut(t *testing.T, command string, argv []string, script func(type_ func(string))) (code int, st *store, screen, stderr string) {
	t.Helper()
	return runWrappedIn(t, t.TempDir(), command, argv, script)
}

// runWrappedIn is runWrappedOut with the state folder home, so two runs
// can share one.
func runWrappedIn(t *testing.T, home, command string, argv []string, script func(type_ func(string))) (code int, st *store, screen, stderr string) {
	t.Helper()
	t.Setenv("UNSENT_HOME", home)
	t.Setenv("UNSENT_FAKE_AGENT", "1")
	bin := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(bin, command)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	// A real terminal pair: the wrapper steps aside for pipes.
	user, term, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	pty.Setsize(user, &pty.Winsize{Cols: 100, Rows: 30})
	ready := make(chan struct{})
	var mu sync.Mutex
	var shown []byte
	go func() {
		// Drain the screen, and start typing once the box is drawn, as a
		// person would: keys sent earlier reach a terminal not yet in raw
		// mode, which echoes them and turns Enter into a line feed.
		waiting := true
		buf := make([]byte, 4096)
		for {
			n, err := user.Read(buf)
			mu.Lock()
			shown = append(shown, buf[:n]...)
			if waiting && strings.Contains(string(shown), "❯") {
				close(ready)
				waiting = false
			}
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	defer func() { user.Close(); term.Close() }()

	// run wraps on the process's own terminal, so lend it the pair, and
	// keep what unsent itself prints.
	said := lendStderr(t)
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = term, term
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	// saved holds when the last timed save to end began.
	var saved atomic.Int64
	oldSave := timedSave
	timedSave = func(s *session) {
		began := time.Now()
		oldSave(s)
		saved.Store(began.UnixNano())
	}
	defer func() { timedSave = oldSave }()
	exit := make(chan int, 1)
	go func() { exit <- run(argv, io.Discard, io.Discard) }()
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("the fake agent never drew its box")
	}
	// Each key waits for the agent to draw its answer, then for a timed
	// save that began at least 100 ms after the drawing reached the
	// terminal, by when the wrapper has it on its shadow screen too. A key
	// the agent draws nothing for, such as Ctrl+Z, waits three save
	// intervals.
	script(func(s string) {
		mu.Lock()
		before := len(shown)
		mu.Unlock()
		user.WriteString(s)
		var drawn time.Time
		for start := time.Now(); len(exit) == 0; time.Sleep(5 * time.Millisecond) {
			mu.Lock()
			grew := len(shown) > before
			mu.Unlock()
			if drawn.IsZero() && grew {
				drawn = time.Now()
			}
			if !drawn.IsZero() && saved.Load() > drawn.Add(100*time.Millisecond).UnixNano() ||
				drawn.IsZero() && time.Since(start) > 3*saveInterval {
				return
			}
		}
	})
	select {
	case code = <-exit:
	case <-time.After(10 * time.Second):
		t.Fatal("wrapper did not exit")
	}
	stderr = said()
	st, _ = openStore()
	mu.Lock()
	defer mu.Unlock()
	return code, st, string(shown), stderr
}

// lendStderr makes a terminal the process's stderr, as a shell does, since
// unsent wraps only when stderr is one too. said puts the old stderr back
// and returns what was printed on the terminal; raw mode keeps line feeds
// as written.
func lendStderr(t *testing.T) (said func() string) {
	t.Helper()
	user, tty, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := term.MakeRaw(int(tty.Fd())); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []byte
	grew := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := user.Read(buf)
			mu.Lock()
			got = append(got, buf[:n]...)
			mu.Unlock()
			select {
			case grew <- struct{}{}:
			default:
			}
			if err != nil {
				return
			}
		}
	}()
	old := os.Stderr
	os.Stderr = tty
	var once sync.Once
	restore := func() { once.Do(func() { os.Stderr = old }) }
	t.Cleanup(func() { restore(); user.Close(); tty.Close() })
	return func() string {
		restore()
		// The terminal keeps order: once the end mark is read, so is
		// everything printed before it.
		const end = "\x00end of stderr\x00"
		tty.WriteString(end)
		deadline := time.After(10 * time.Second)
		for {
			mu.Lock()
			s := string(got)
			mu.Unlock()
			if i := strings.Index(s, end); i >= 0 {
				return s[:i]
			}
			select {
			case <-grew:
			case <-deadline:
				t.Fatalf("stderr never showed the end mark: %q", s)
			}
		}
	}
}

func TestWrapSavesDraftLeftInTheBox(t *testing.T) {
	code, st := runWrapped(t, func(type_ func(string)) {
		type_("hello")
		type_("\x1b\rworld")
		type_("\x04")
	})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	rs := st.orphans()
	if len(rs) != 1 || rs[0].Draft != "hello\nworld" || rs[0].Ended.IsZero() {
		t.Fatalf("orphans %+v", rs)
	}
	if rs[0].Agent != "claude" {
		t.Fatalf("agent %q, want claude", rs[0].Agent)
	}
}

// --as, or UNSENT_AGENT, picks the profile for a command whose name does
// not say which agent it starts, and the draft is recorded under that agent.
// The folder is recorded as its real path, not the symlink the agent was
// started from.
func TestWrapAsNamesTheAgent(t *testing.T) {
	for _, c := range []struct {
		env  string
		argv []string
	}{
		{"", []string{"--as", "claude", "renamed-agent"}},
		{"claude", []string{"renamed-agent"}},
	} {
		t.Run(strings.Join(c.argv, " "), func(t *testing.T) {
			t.Setenv("UNSENT_AGENT", c.env)
			real := t.TempDir()
			link := filepath.Join(t.TempDir(), "link")
			if err := os.Symlink(real, link); err != nil {
				t.Fatal(err)
			}
			t.Chdir(link)
			code, st := runWrappedAs(t, "renamed-agent", c.argv, func(type_ func(string)) {
				type_("typed into a renamed binary")
				type_("\x04")
			})
			if code != 0 {
				t.Fatalf("exit %d", code)
			}
			rs := st.orphans()
			if len(rs) != 1 || rs[0].Draft != "typed into a renamed binary" {
				t.Fatalf("orphans %+v: the named profile did not read the box", rs)
			}
			want, _ := filepath.EvalSymlinks(real)
			if rs[0].Agent != "claude" || rs[0].Command[0] != "renamed-agent" || rs[0].Cwd != want {
				t.Fatalf("agent %q, command %q, cwd %q (want %q)", rs[0].Agent, rs[0].Command, rs[0].Cwd, want)
			}
		})
	}
}

func TestWrapExpandsPastesAndArchivesClearedBox(t *testing.T) {
	body := strings.Repeat("pasted line\r", 5) + "last"
	code, st := runWrapped(t, func(type_ func(string)) {
		type_("mistake")
		type_("\x03") // Ctrl+C clears the box
		type_("see: \x1b[200~" + body + "\x1b[201~")
		type_("\x05")
	})
	if code != 3 {
		t.Fatalf("exit %d, want the agent's 3", code)
	}
	rs := st.orphans()
	want := "see: " + strings.Repeat("pasted line\n", 5) + "last"
	if len(rs) != 1 || rs[0].Draft != want {
		t.Fatalf("orphans %+v", rs)
	}
	all := st.load(true)
	var cleared bool
	for _, r := range all {
		cleared = cleared || r.Draft == "mistake"
	}
	if !cleared {
		t.Fatal("the cleared draft is not in history")
	}
}

// Through the whole wrapper: a sent message goes to the session's sent log
// with its paste expanded, and a draft cleared with Ctrl+C or with Esc
// pressed twice goes to history.
func TestWrapSentAndClearedDrafts(t *testing.T) {
	body := strings.Repeat("pasted line\r", 5) + "last"
	_, st := runWrapped(t, func(type_ func(string)) {
		type_("see: \x1b[200~" + body + "\x1b[201~")
		type_("\r")
		type_("cleared with ctrl+c")
		type_("\x03")
		type_("cleared with esc esc")
		type_("\x1b")
		type_("\x1b")
		type_("second message")
		type_("\r")
		type_("\x04")
	})
	logs := st.sentLogs()
	if len(logs) != 1 || logs[0].Agent != "claude" {
		t.Fatalf("%d sent logs", len(logs))
	}
	var sent []string
	for _, m := range logs[0].messages {
		sent = append(sent, m.Text)
	}
	if want := []string{"see: " + strings.Repeat("pasted line\n", 5) + "last", "second message"}; !slices.Equal(sent, want) {
		t.Fatalf("sent %q, want %q", sent, want)
	}
	var history []string
	for _, r := range st.load(true) {
		history = append(history, r.Draft)
	}
	if want := []string{"cleared with esc esc", "cleared with ctrl+c"}; !slices.Equal(history, want) {
		t.Fatalf("history %q, want %q", history, want)
	}
}

// Under UNSENT_ON_SEND=delete no file in the state folder keeps a sent
// message.
func TestWrapOnSendDelete(t *testing.T) {
	t.Setenv("UNSENT_ON_SEND", "delete")
	_, st := runWrapped(t, func(type_ func(string)) {
		type_("a private message")
		type_("\r")
		type_("\x04")
	})
	filepath.Walk(st.dir, func(path string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			if b, _ := os.ReadFile(path); strings.Contains(string(b), "private") {
				t.Errorf("%s keeps the message", path)
			}
		}
		return nil
	})
}

func TestWrapSavesOnHangup(t *testing.T) {
	code, st := runWrapped(t, func(type_ func(string)) {
		type_("closing the window now")
		syscall.Kill(os.Getpid(), syscall.SIGHUP)
		time.Sleep(time.Second)
	})
	if code != 128+int(syscall.SIGHUP) {
		t.Fatalf("exit %d", code)
	}
	rs := st.orphans()
	if len(rs) != 1 || rs[0].Draft != "closing the window now" {
		t.Fatalf("orphans %+v", rs)
	}
}

func TestWrapEmptyBoxLeavesNothing(t *testing.T) {
	_, st := runWrapped(t, func(type_ func(string)) {
		type_("sent\r")
		type_("\x04")
	})
	if rs := st.orphans(); len(rs) != 0 {
		t.Fatalf("orphans %+v", *rs[0])
	}
	names, _ := filepath.Glob(filepath.Join(st.dir, "drafts", "*.json"))
	if len(names) != 0 {
		b, _ := os.ReadFile(names[0])
		t.Fatalf("left %v: %s", names, b)
	}
}

func TestWrapUnknownAgentPassesThrough(t *testing.T) {
	t.Setenv("UNSENT_HOME", t.TempDir())
	lendStderr(t)
	user, term, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	go io.Copy(io.Discard, user)
	defer func() { user.Close(); term.Close() }()
	if code := wrap("sh", []string{"sh", "-c", "exit 7"}, term, term, nil); code != 7 {
		t.Fatalf("exit %d", code)
	}
	if code := wrap("no-such-agent-unsent-test", []string{"no-such-agent-unsent-test"}, term, term, nil); code != 127 {
		t.Fatalf("exit %d", code)
	}
}

func TestWrapStepsAsideForPipes(t *testing.T) {
	lendStderr(t)
	t.Setenv("UNSENT_HOME", t.TempDir())
	var ran []string
	old := execAgent
	execAgent = func(bin string, args []string) error { ran = append([]string{bin}, args...); return nil }
	defer func() { execAgent = old }()
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	if code := wrap("sh", []string{"sh", "-c", "true"}, r, w, nil); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(ran) != 4 || !strings.HasSuffix(ran[0], "/sh") || ran[3] != "true" {
		t.Fatalf("ran %q", ran)
	}
	execAgent = func(string, []string) error { return os.ErrPermission }
	if code := wrap("sh", []string{"sh"}, r, w, nil); code != 126 {
		t.Fatalf("exit %d on exec failure", code)
	}
}

// unsentClaude runs unsent, the command that starts the test binary as
// unsent claude, in a terminal of its own, with bin, which holds the fake claude, first on PATH and env added
// to the environment. errLog, when not nil, is where its stderr goes
// instead. It types hello, waits past a save, quits the fake agent with 3
// and returns the exit code and everything the terminal showed.
func unsentClaude(t *testing.T, unsent []string, bin string, env []string, errLog *os.File) (code int, shown string) {
	t.Helper()
	state, shown := unsentClaudeState(t, unsent, bin, env, errLog)
	return state.ExitCode(), shown
}

// unsentClaudeState is unsentClaude returning how unsent ended, so a test
// can tell a signal from an exit code.
func unsentClaudeState(t *testing.T, unsent []string, bin string, env []string, errLog *os.File) (state *os.ProcessState, shown string) {
	t.Helper()
	user, tty, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { user.Close(); tty.Close() }()
	pty.Setsize(user, &pty.Winsize{Cols: 100, Rows: 30})
	cmd := exec.Command(unsent[0], unsent[1:]...)
	cmd.Env = append(append(os.Environ(), "UNSENT_TEST_RUN=wrap", "UNSENT_FAKE_STDERR=to-stderr",
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH")), env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	if errLog != nil {
		cmd.Stderr = errLog
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var out []byte
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := user.Read(buf)
			mu.Lock()
			out = append(out, buf[:n]...)
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	waitFor := func(want string) {
		t.Helper()
		for start := time.Now(); ; time.Sleep(10 * time.Millisecond) {
			mu.Lock()
			s := string(out)
			mu.Unlock()
			if strings.Contains(s, want) {
				return
			}
			if time.Since(start) > 10*time.Second {
				cmd.Process.Kill()
				t.Fatalf("the terminal never showed %q: %q", want, s)
			}
		}
	}
	waitFor("❯")
	user.WriteString("hello")
	waitFor("hello")
	time.Sleep(3 * saveInterval) // a wrapper has saved it by now
	user.WriteString("\x05")     // the fake agent quits with 3
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("unsent claude did not exit")
	}
	mu.Lock()
	defer mu.Unlock()
	return cmd.ProcessState, string(out)
}

// claude 2>err.log keeps its redirect: with stdin and stdout on the
// terminal and stderr on a file, unsent hands over to the agent, so the
// agent's stderr reaches the file and its exit status comes through. With
// stderr on the terminal too, the same run is wrapped and saves the draft.
func TestWrapStepsAsideWhenStderrIsRedirected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("UNSENT_HOME", home)
	bin := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(bin, "claude")); err != nil {
		t.Fatal(err)
	}
	start := func(errLog *os.File) (int, string) {
		return unsentClaude(t, []string{self, "claude"}, bin, nil, errLog)
	}

	errLog, err := os.Create(filepath.Join(t.TempDir(), "err.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer errLog.Close()
	code, shown := start(errLog)
	logged, _ := os.ReadFile(errLog.Name())
	if code != 3 || string(logged) != "to-stderr" || strings.Contains(shown, "to-stderr") {
		t.Fatalf("stderr on a file: exit %d, file %q, terminal showed it: %v; want 3, the text in the file only",
			code, logged, strings.Contains(shown, "to-stderr"))
	}
	st, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	if rs := st.orphans(); len(rs) != 0 {
		t.Fatalf("stderr on a file: orphans %+v, want none: unsent should have handed over", rs)
	}

	code, shown = start(nil)
	if code != 3 || !strings.Contains(shown, "to-stderr") {
		t.Fatalf("stderr on the terminal: exit %d, terminal showed it: %v", code, strings.Contains(shown, "to-stderr"))
	}
	if rs := st.orphans(); len(rs) != 1 || rs[0].Draft != "hello" {
		t.Fatalf("stderr on the terminal: orphans %+v, want the draft: unsent should have wrapped", rs)
	}
}

// A launcher that execs unsent and watches that pid sees the agent start at
// once: the slow work waits until the agent runs. The shell import, which
// waits while another unsent imports, holds up nothing, and still runs
// before the notice, which reaches the terminal before the agent's first
// paint and names the line a dead shell left.
func TestWrapStartsTheAgentBeforeTheSlowWork(t *testing.T) {
	home := t.TempDir()
	t.Setenv("UNSENT_HOME", home)
	work := realPath(t.TempDir())
	t.Chdir(work)
	st, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	seedAs(t, st, "left", "claude", work, "a draft left behind", 5)
	notice := strings.TrimSuffix(orphanNotice(st, work, "claude"), "\n")
	if notice == "" {
		t.Fatal("no notice for the seeded draft")
	}
	// A dead shell of this host left a line here: only the import at the
	// start puts it in the start notice (the one after exit imports again).
	// The test holds the import lock as another unsent importing would.
	writeShellLog(t, st, shellLogName(deadPid(t), 1, ".log"),
		zrec{"v", "1"}, zrec{"i", work}, zrec{"b", "make release"})
	shellLine := "unsent: a zsh line you typed here and did not run is saved"
	shell := filepath.Join(home, "shell")
	lock, err := os.OpenFile(filepath.Join(shell, ".import.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	started := filepath.Join(t.TempDir(), "started")
	whileLocked := make(chan bool, 1)
	go func() {
		defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			if _, err := os.Stat(started); err == nil {
				whileLocked <- true
				return
			}
		}
		whileLocked <- false
	}()

	bin := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(bin, "claude")); err != nil {
		t.Fatal(err)
	}
	code, shown := unsentClaude(t, []string{self, "claude"}, bin, []string{"UNSENT_FAKE_STARTED=" + started}, nil)
	if !<-whileLocked {
		t.Fatal("the agent started only once the shell import could run; it should start first")
	}
	if code != 3 {
		t.Fatalf("exit %d, want the agent's 3", code)
	}
	i, line, box := strings.Index(shown, notice), strings.Index(shown, shellLine), strings.Index(shown, "❯")
	if i < 0 || line < i || box < line {
		t.Fatalf("terminal %q: want the notice %q, then %q, before the agent's box", shown, notice, shellLine)
	}
}

// A draft saver that cannot run never costs the session: with the draft
// folder unwritable, or no pseudo-terminal to be had, unsent says so in one
// line and hands over, so the agent runs to the end on the terminal and its
// exit status is the session's. A hang-up ignored when unsent started (as
// under nohup) is still ignored by the agent it hands over to.
func TestWrapHandsOverWhenItCannotSave(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a read-only folder")
	}
	bin := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(bin, "claude")); err != nil {
		t.Fatal(err)
	}
	locked := t.TempDir()
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o700) })
	const off = "; drafts are not being saved this session"
	direct := []string{self, "claude"}
	// sh ignores the hang-up before unsent starts, as nohup does, and the
	// fake agent hangs itself up.
	noHup := []string{"/bin/sh", "-c", `trap "" HUP; exec "$0" claude`, self}
	for _, c := range []struct {
		name, home, pty, hup, want string
		unsent                     []string
	}{
		{"unwritable UNSENT_HOME", locked, "", "", "unsent: cannot open the draft folder: mkdir " + locked, direct},
		{"no pseudo-terminal", t.TempDir(), "fail", "", "unsent: cannot start a pseudo-terminal: forced pty failure" + off, direct},
		{"hang-up ignored", locked, "", "1", "unsent: cannot open the draft folder: mkdir " + locked, noHup},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, shown := unsentClaude(t, c.unsent, bin,
				[]string{"UNSENT_HOME=" + c.home, "UNSENT_TEST_PTY=" + c.pty, "UNSENT_FAKE_HUP=" + c.hup}, nil)
			if code != 3 || !strings.Contains(shown, c.want) || strings.Count(shown, off) != 1 {
				t.Fatalf("exit %d, terminal %q; want exit 3 and one line %q ending %q", code, shown, c.want, off)
			}
			if !strings.Contains(shown, "to-stderr") || !strings.Contains(shown, "hello") {
				t.Fatalf("terminal %q: the agent did not run on it", shown)
			}
		})
	}
}

// An agent killed by a signal is reported to the shell as killed by that
// signal, as it would be without unsent, not as the exit code 128+n: job
// control and $? tell the two apart. The draft is saved first, and a normal
// exit code still comes through as it is.
func TestWrapDiesOfTheSignalThatKilledTheAgent(t *testing.T) {
	bin := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(bin, "claude")); err != nil {
		t.Fatal(err)
	}
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGKILL, 0} {
		t.Run(fmt.Sprint(sig), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("UNSENT_HOME", home)
			env := []string{"UNSENT_HOME=" + home}
			if sig != 0 {
				env = append(env, fmt.Sprintf("UNSENT_FAKE_SIGNAL=%d", sig))
			}
			state, shown := unsentClaudeState(t, []string{self, "claude"}, bin, env, nil)
			ws := state.Sys().(syscall.WaitStatus)
			if sig == 0 {
				if ws.Signaled() || ws.ExitStatus() != 3 {
					t.Fatalf("unsent ended %v; want exit status 3", state)
				}
			} else if !ws.Signaled() || ws.Signal() != sig {
				t.Fatalf("unsent ended %v; want killed by %v, as the agent was (terminal %q)", state, sig, shown)
			}
			st, err := openStore()
			if err != nil {
				t.Fatal(err)
			}
			if rs := st.orphans(); len(rs) != 1 || rs[0].Draft != "hello" {
				t.Fatalf("orphans %+v, want the draft", rs)
			}
		})
	}
}

// When dying of the signal does not end unsent, the shell gets 128+n: a
// hang-up unsent was started with ignored (nohup) stays ignored, and a
// SIGQUIT would make Go's runtime print a goroutine dump and exit 2.
func TestExitAsFallsBackTo128PlusN(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		sig  syscall.Signal
		argv []string
	}{
		{"ignored hang-up", syscall.SIGHUP, []string{"/bin/sh", "-c", `trap "" HUP; exec "$0"`, self}},
		{"quit", syscall.SIGQUIT, []string{self}},
	} {
		t.Run(c.name, func(t *testing.T) {
			cmd := exec.Command(c.argv[0], c.argv[1:]...)
			cmd.Env = append(os.Environ(), "UNSENT_TEST_RUN=exit", fmt.Sprintf("UNSENT_TEST_KILLED_BY=%d", c.sig))
			out, err := cmd.CombinedOutput()
			ws := cmd.ProcessState.Sys().(syscall.WaitStatus)
			if ws.Signaled() || ws.ExitStatus() != 128+int(c.sig) || len(out) != 0 {
				t.Fatalf("ended %v (%v), printed %q; want exit status %d and nothing printed", cmd.ProcessState, err, out, 128+int(c.sig))
			}
		})
	}
}

func TestSessionWaitsForTheFrameToEnd(t *testing.T) {
	st := testStore(t)
	reads := 0
	s := &session{
		screen: vt.NewEmulator(40, 10),
		rec:    newRecord([]string{"claude"}, "/w"),
		store:  st,
		pastes: &pasteTracker{},
		prof: &profile{read: func(*screen) (view, bool) {
			reads++
			return view{}, false
		}},
	}
	go io.Copy(io.Discard, s.screen)
	s.write([]byte("\x1b[?2026hhalf a fra"))
	go func() {
		time.Sleep(20 * time.Millisecond)
		s.write([]byte("me\x1b[?2026"))
		s.write([]byte("l")) // the end mark split across two writes
	}()
	s.save()
	if reads != 1 || s.inFrame {
		t.Fatalf("reads %d, in frame %v: the save did not wait for the frame to close", reads, s.inFrame)
	}
	// A frame that stays open does not hold a save back for long.
	s.write([]byte("\x1b[?2026hstill drawing"))
	start := time.Now()
	s.save()
	if reads != 2 || time.Since(start) > 5*maxFrameWait {
		t.Fatalf("reads %d after %v with a frame left open", reads, time.Since(start))
	}
}

func TestSessionSurvivesAnEmulatorPanic(t *testing.T) {
	st := testStore(t)
	st.warned = true // keep the test output quiet
	s := &session{store: st, pastes: &pasteTracker{}, prof: &profile{name: "claude", read: func(*screen) (view, bool) { t.Fatal("read a broken screen"); return view{}, false }}}
	s.write([]byte("x")) // s.screen is nil: the emulator write panics
	if !s.broken.Load() {
		t.Fatal("panic not caught")
	}
	s.write([]byte("more output keeps flowing"))
	s.resize(80, 24)
	s.input([]byte("typing keeps flowing"))
	s.dirty = true
	s.save()
	if l := s.exitLines(); len(l) != 1 || !strings.Contains(l[0], "an internal error stopped saving claude's box") {
		t.Fatalf("exit lines %q", l)
	}
}

// A resize reaches the paste tracker: in a window under 12 rows Claude Code
// collapses a 3-line paste, so it fills a placeholder there.
func TestSessionResizeReachesThePasteRule(t *testing.T) {
	s := &session{screen: vt.NewEmulator(80, 40), pastes: &pasteTracker{rows: 40}, prof: &claude}
	go io.Copy(io.Discard, s.screen)
	s.resize(80, 11)
	s.input([]byte("\x1b[200~one\ntwo\nthree\x1b[201~"))
	if got := s.pastes.expand("[Pasted text #1 +2 lines]", "", &claude); got != "one\ntwo\nthree" {
		t.Fatalf("got %q", got)
	}
}

// A reader that panics on one crafted screen stops saving for the rest of
// the session, says so once, and keeps the last good draft.
func TestSessionSurvivesAReaderPanic(t *testing.T) {
	st := testStore(t)
	prof := claude
	prof.read = func(scr *screen) (view, bool) {
		if strings.Contains(scr.String(), "boom") {
			panic("crafted screen")
		}
		return claudeBox(scr)
	}
	s := &session{screen: vt.NewEmulator(100, 30), rec: newRecord([]string{"claude"}, "/w"), store: st, pastes: &pasteTracker{}, prof: &prof}
	go io.Copy(io.Discard, s.screen)
	s.write([]byte(drawBox(100, "hello")))
	s.save()
	s.write([]byte(drawBox(100, "hello boom")))
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	s.save()
	os.Stderr = old
	w.Close()
	said, _ := io.ReadAll(r)
	if !s.broken.Load() || s.rec.Draft != "hello" {
		t.Fatalf("broken %v, draft %q", s.broken.Load(), s.rec.Draft)
	}
	if !strings.Contains(string(said), "stopped saving after an internal error: crafted screen") {
		t.Fatalf("said %q", said)
	}
	s.write([]byte(drawBox(100, "hello again")))
	s.save()
	if s.rec.Draft != "hello" {
		t.Fatalf("draft %q saved after the panic", s.rec.Draft)
	}
}

// The passthrough path runs input on every key. A panic there must not
// take the terminal down, and turns the session into plain passthrough.
func TestSessionInputCannotPanic(t *testing.T) {
	s := &session{prof: &claude} // no paste tracker: feeding it panics
	s.input([]byte("abc"))
	if !s.broken.Load() || s.failure == "" {
		t.Fatal("input panic not caught")
	}
	s.input([]byte("more keys"))
}

// resize runs on the main loop (a window resize, fg after Ctrl+Z): a panic
// there must not take unsent down either.
func TestSessionResizeCannotPanic(t *testing.T) {
	s := &session{prof: &claude} // no screen: resizing it panics
	s.resize(80, 24)
	if !s.broken.Load() || s.failure == "" {
		t.Fatal("resize panic not caught")
	}
}

// After a panic the paste tracker is no longer fed, so it can stay inside a
// paste that has long ended. Ctrl+Z must still suspend unsent, or the agent
// gets it and hangs.
func TestWrapCtrlZSuspendsAfterAPanicMidPaste(t *testing.T) {
	stops := 0
	oldStop := stopSelf
	stopSelf = func() { stops++ }
	defer func() { stopSelf = oldStop }()
	old := claude.read
	claude.read = func(scr *screen) (view, bool) {
		if strings.Contains(scr.String(), "boom") {
			panic("crafted screen")
		}
		return old(scr)
	}
	defer func() { claude.read = old }()
	runWrapped(t, func(type_ func(string)) {
		type_("boom\x1b[200~x") // a paste starts; the next save panics
		type_("\x1b[201~")      // the paste ends, unseen by the tracker
		type_("\x1a")
		type_("\x04")
	})
	if stops != 1 {
		t.Fatalf("stopped %d times, want 1", stops)
	}
}

// Through the whole wrapper: after the reader panics, keys still reach the
// agent (it quits on Ctrl+E), its output still reaches the terminal, the
// draft saved before the panic stays, and the exit line says what happened.
func TestWrapReaderPanicFallsBackToPassthrough(t *testing.T) {
	old := claude.read
	claude.read = func(scr *screen) (view, bool) {
		if strings.Contains(scr.String(), "boom") {
			panic("crafted screen")
		}
		return old(scr)
	}
	defer func() { claude.read = old }()
	code, st, shown, said := runWrappedOut(t, "claude", []string{"claude"}, func(type_ func(string)) {
		type_("hello")
		type_(" boom")
		type_(" still typing")
		type_("\x05")
	})
	if code != 3 {
		t.Fatalf("exit %d, want the agent's 3: keys stopped reaching it", code)
	}
	if !strings.Contains(shown, "hello boom still typing") {
		t.Fatal("the agent's output stopped reaching the terminal")
	}
	if rs := st.orphans(); len(rs) != 1 || rs[0].Draft != "hello" {
		t.Fatalf("orphans %+v, want the draft from before the panic", rs)
	}
	if !strings.Contains(said, "an internal error stopped saving claude's box") || !strings.Contains(said, "crafted screen") {
		t.Fatalf("stderr %q", said)
	}
}

// When the reader never finds the box in a session the user typed in, the
// exit line names the version that ran, asked at startup, and the last
// verified one. A command the profile does not answer to (--as) is never
// asked. A box read, or a session with nothing typed or only the
// terminal's answers, says nothing.
func TestWrapSaysWhenTheBoxWasNeverRead(t *testing.T) {
	line := func(name string) string {
		return "unsent: could not read " + name + "'s box this session (last verified " + claude.verified + "), nothing was saved\n"
	}
	update := filepath.Join(t.TempDir(), "version")
	t.Setenv("UNSENT_FAKE_VERSION", update)
	hangUp := func() { syscall.Kill(os.Getpid(), syscall.SIGHUP); time.Sleep(time.Second) }
	for _, c := range []struct {
		name    string
		blind   bool
		command string
		argv    []string
		script  func(type_ func(string))
		want    string
	}{
		{"never read, typed", true, "claude", []string{"claude"}, func(type_ func(string)) {
			type_("hello")
			os.WriteFile(update, []byte("10.0.0"), 0o600) // updated while it runs
			type_("\x04")
		}, line("claude 9.9.9")},
		{"never read, typed, --as", true, "launcher", []string{"--as", "claude", "launcher"}, func(type_ func(string)) { type_("hello"); type_("\x04") }, line("claude")},
		{"never read, nothing typed", true, "claude", []string{"claude"}, func(func(string)) { hangUp() }, ""},
		{"never read, terminal answers only", true, "claude", []string{"claude"}, func(type_ func(string)) {
			type_("\x1b[I\x1b]11;rgb:0/0/0\x07\x1b[?62;22c")
			hangUp()
		}, ""},
		{"read", false, "claude", []string{"claude"}, func(type_ func(string)) { type_("hello"); type_("\x04") }, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			os.Remove(update)
			if c.blind {
				old := claude.read
				claude.read = func(*screen) (view, bool) { return view{}, false }
				defer func() { claude.read = old }()
			}
			_, _, _, said := runWrappedOut(t, c.command, c.argv, c.script)
			if c.want == "" && strings.Contains(said, "could not read") || c.want != "" && !strings.Contains(said, c.want) {
				t.Fatalf("stderr %q, want %q", said, c.want)
			}
		})
	}
}

// An agent with no reader is told about before it starts and again after
// it exits, because the alternate screen hides the first line at once.
func TestWrapSaysAgainOnExitThatAnAgentIsNotProtected(t *testing.T) {
	t.Setenv("UNSENT_HOME", t.TempDir())
	user, term, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	go io.Copy(io.Discard, user)
	defer func() { user.Close(); term.Close() }()
	stderr := lendStderr(t)
	code := wrap("sh", []string{"sh", "-c", "exit 0"}, term, term, nil)
	said := stderr()
	line := "unsent: no reader for \"sh\" yet, running it without saving drafts\n"
	if code != 0 || strings.Count(string(said), line) != 2 {
		t.Fatalf("exit %d, stderr %q", code, said)
	}
}

// The recovery notice is printed before the agent starts and again after it
// exits, because an agent on the alternate screen hides the first one. The
// second is asked afresh: a draft restored while the agent ran is not
// offered again.
func TestWrapSaysTheNoticeAgainOnExit(t *testing.T) {
	st := testStore(t)
	work := realPath(t.TempDir())
	t.Chdir(work)
	seedAs(t, st, "left", "claude", work, "a draft left behind", 5)
	user, term, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	go io.Copy(io.Discard, user)
	defer func() { user.Close(); term.Close() }()
	agent := func(want int, script string, args ...string) string {
		stderr := lendStderr(t)
		code := wrap("claude", append([]string{"sh", "-c", script, "sh"}, args...), term, term, nil)
		said := stderr()
		if code != want {
			t.Fatalf("exit %d, want %d, stderr %q", code, want, said)
		}
		return said
	}
	line := orphanNotice(st, work, "claude")
	if !strings.HasPrefix(line, "unsent: recovered a draft from ") {
		t.Fatalf("notice %q", line)
	}
	if said := agent(0, `printf '\033[?1049h'; printf '\033[?1049l'`); said != line+line {
		t.Fatalf("stderr %q, want the notice before and after", said)
	}
	// A crash is when the hidden notice matters most.
	if said := agent(3, `exit 3`); said != line+line {
		t.Fatalf("stderr %q, want the notice before and after a failed exit", said)
	}
	// The agent's output waits in the pseudo-terminal until the notice is
	// out, so a write larger than its buffer ends only after that.
	if said := agent(0, `head -c 262144 /dev/zero; rm "$1"`, st.draftPath("left")); said != line {
		t.Fatalf("stderr %q, want the notice only before: the draft was restored meanwhile", said)
	}
}

// When the agent exits with text in the box, the notice after exit counts
// that draft too and names it first, because a plain restore takes it.
func TestWrapNoticeOnExitNamesTheDraftRestoreTakes(t *testing.T) {
	var cwd string
	_, st, _, said := runWrappedOut(t, "claude", []string{"claude"}, func(type_ func(string)) {
		// Seeded after start, so only the notice after exit can name it.
		cwd, _ = os.Getwd()
		cwd = realPath(cwd)
		st, _ := openStore()
		seedAs(t, st, "left", "claude", cwd, "one\ntwo\nthree", 5)
		type_("hello")
		type_("\x04")
	})
	if o := st.orphans(); len(o) != 2 || o[0].Draft != "hello" {
		t.Fatalf("orphans %v, want this session's draft first", o)
	}
	want := orphanNotice(st, cwd, "claude")
	if !strings.Contains(want, ", 1 line (and 1 more). Run `unsent restore` to copy it.") {
		t.Fatalf("notice now %q, want this session's draft first and the seeded one counted", want)
	}
	if said != want {
		t.Fatalf("stderr %q, want the notice a restore now matches: %q", said, want)
	}
}

// UNSENT_OFF=1 hands over to the agent at once, even on a terminal; 0
// leaves unsent on.
func TestWrapOff(t *testing.T) {
	t.Setenv("UNSENT_HOME", t.TempDir())
	lendStderr(t)
	user, term, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	go io.Copy(io.Discard, user)
	defer func() { user.Close(); term.Close() }()
	var ran []string
	oldExec := execAgent
	execAgent = func(bin string, args []string) error { ran = args; return nil }
	defer func() { execAgent = oldExec }()
	t.Setenv("UNSENT_OFF", "1")
	if code := wrap("sh", []string{"sh", "-c", "exit 7"}, term, term, nil); code != 0 || len(ran) != 3 {
		t.Fatalf("exit %d, ran %q: UNSENT_OFF=1 did not hand over", code, ran)
	}
	ran = nil
	t.Setenv("UNSENT_OFF", "0")
	if code := wrap("sh", []string{"sh", "-c", "exit 7"}, term, term, nil); code != 7 || ran != nil {
		t.Fatalf("exit %d, ran %q: UNSENT_OFF=0 switched unsent off", code, ran)
	}
}

func TestTypedKeys(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"a", true},
		{"\r", true},
		{"\x03", true},
		{"\x1b", true},
		{"\x1b\x1b", true},
		{"\x1b[A", true},
		{"\x1b[200~text\x1b[201~", true},
		{"\x1b[I\x1b[O", false},                      // focus
		{"\x1b[<35;10;5M\x1b[<0;1;1m", false},        // mouse
		{"\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\", false},  // OSC answer, ST
		{"\x1b]10;rgb:ffff/ffff/ffff\x07", false},    // OSC answer, BEL
		{"\x1bP>|WezTerm 2024\x1b\\", false},         // XTVERSION
		{"\x1b[?62;22c\x1b[>1;10;0c\x1b[?1u", false}, // DA1, DA2, kitty flags
		{"\x1b]11;rgb:0/0/0", false},                 // cut short
		{"\x1b[6;32;16t\x1b[?1;2;4c", false},         // cell size, as tmux answers Claude Code
		{"\x1b[4;800;1200t\x1b[8;40;120t", false},    // window size in pixels and cells
		{"\x1b[2026;2$y\x1b[?2026;2$y", false},       // mode reports
		{"\x1b[6;32;16t" + "q", true},
		{"\x1b[?1u" + "x", true},
		{"\x1b]11;rgb:0/0/0\x07" + "\x1b[I" + "y", true},
	} {
		if got := typedKeys([]byte(c.in)); got != c.want {
			t.Errorf("typedKeys(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestAgentVersion(t *testing.T) {
	if v := agentVersion("/bin/sh", []string{"-c", "echo '2.1.283 (Claude Code)'"}); v != "2.1.283" {
		t.Fatalf("version %q", v)
	}
	if v := agentVersion("/bin/sh", nil); v != "" {
		t.Fatalf("version %q with no version arguments", v)
	}
	start := time.Now()
	if v := agentVersion("/bin/sh", []string{"-c", "sleep 10; echo 1.2.3"}); v != "" || time.Since(start) > 5*time.Second {
		t.Fatalf("version %q after %v from an agent that hangs", v, time.Since(start))
	}
}

// A session without a profile (an unknown agent, or a failed lock) still
// sees every key, and must not crash on the delete keys.
func TestSessionWithoutProfileIgnoresDeleteKeys(t *testing.T) {
	s := &session{pastes: &pasteTracker{}}
	s.input([]byte("abc\x7f\x17"))
	if n, ahead := s.deletes.recent(); n != 0 || ahead {
		t.Fatalf("recent %d %v, want nothing", n, ahead)
	}
	s.prof = &claude
	s.input([]byte("abc\x7f"))
	if n, _ := s.deletes.recent(); n != 1 {
		t.Fatalf("recent %d with a profile, want 1", n)
	}
}

func TestDeleteKeys(t *testing.T) {
	cases := []struct {
		keys  string
		chars int64
		ahead bool
	}{
		{"\x7f", 1, false},
		{"\x7f\x7f\x08", 3, false},
		{"x\x1b[3~", 1, true},
		{"\x17", unlimited, false},
		{"\x15", unlimited, false},
		{"\x0b", unlimited, true},
		{"\x1f", unlimited, true},
		{"\x04", 1, true}, // Ctrl+D, the character after the cursor
		{"\x04\x04\x7f", 3, true},
		{"\x1b\x7f", unlimited, false}, // Alt+Backspace, the word before
		{"\x1bd", unlimited, true},     // Alt+D, the word after
		{"hello \x1b[A\r", 0, false},
		{"\x1b[D d", 0, false},
	}
	for _, c := range cases {
		if chars, ahead := claude.keys.deletes([]byte(c.keys)); chars != c.chars || ahead != c.ahead {
			t.Errorf("%q: %d %v, want %d %v", c.keys, chars, ahead, c.chars, c.ahead)
		}
	}
}

func TestSimilar(t *testing.T) {
	a := "please refactor the auth module and keep every test green"
	if !similar(a, a+" today") || !similar(a, strings.Replace(a, "auth", "login", 1)) {
		t.Fatal("an edit read as a different text")
	}
	if similar(a, "an entirely different prompt recalled from history, nothing shared") {
		t.Fatal("a different text read as an edit")
	}
	if !similar("short", "other") {
		t.Fatal("short drafts are always edits")
	}
}

func TestWrapCtrlZSuspendsTheWrapper(t *testing.T) {
	stops := 0
	old := stopSelf
	stopSelf = func() { stops++ }
	defer func() { stopSelf = old }()
	debug := t.TempDir()
	t.Setenv("UNSENT_DEBUG_DIR", debug)
	_, st := runWrapped(t, func(type_ func(string)) {
		type_("before")
		type_("\x1a")
		type_(" after")
		type_("\x04")
	})
	if stops != 1 {
		t.Fatalf("stopped %d times", stops)
	}
	rs := st.orphans()
	if len(rs) != 1 || rs[0].Draft != "before after" {
		t.Fatalf("orphans %+v", rs)
	}
	// The raw log notes the size again after the resume, as the agent
	// repaints at it.
	recs, _ := filepath.Glob(filepath.Join(debug, "raw-*.rec"))
	if len(recs) != 1 {
		t.Fatalf("raw logs %q", recs)
	}
	if _, _, _, meta := readRaw(t, strings.TrimSuffix(recs[0], ".rec")); len(meta.Sizes) != 2 || meta.Sizes[1].Cols != 100 || meta.Sizes[1].Rows != 30 {
		t.Fatalf("sizes %+v", meta.Sizes)
	}
}

func TestKeepOld(t *testing.T) {
	old := "please refactor the auth module and keep every test green before the release"
	cases := []struct {
		draft            string
		stitched         bool
		history, version bool
		why              string
	}{
		{"", false, true, false, "the box emptied"},
		{"an unrelated prompt recalled from history with nothing in common", false, true, false, "replaced wholesale"},
		{strings.Replace(old, " module", "", 1), true, false, true, "text lost from a stitched draft keeps a copy"},
		{strings.Replace(old, " module", "", 1), false, false, false, "the whole draft is on screen: a real deletion"},
		{old + " today", true, false, false, "typing"},
		{strings.Replace(old, "release", "rel", 1), true, false, true, "a word shrank"},
		{strings.Replace(old, "green", "greenish", 1), true, false, false, "typing into a word"},
		{strings.Replace(old, "green", "gren", 1), true, false, true, "a letter deleted"},
	}
	for _, c := range cases {
		if h, v := keepOld(old, c.draft, c.stitched); h != c.history || v != c.version {
			t.Errorf("%s: keepOld = %v %v, want %v %v", c.why, h, v, c.history, c.version)
		}
	}
	if h, v := keepOld("", "anything", true); h || v {
		t.Error("an empty old draft has nothing to keep")
	}
}

func TestLostChars(t *testing.T) {
	if n := lostChars("a hel", "a hello"); n != 0 {
		t.Fatalf("a half-typed word counted as lost: %d", n)
	}
	if n := lostChars("x the y", "x then there y"); n != 0 {
		t.Fatalf("growth in place counted as lost: %d", n)
	}
	if n := lostChars("a helo b", "a hello b"); n != 0 {
		t.Fatalf("a letter typed into a word counted as lost: %d", n)
	}
	if n := lostChars("前文字後", "前後"); n != 2 {
		t.Fatalf("wide characters: lost %d, want 2 (characters, not bytes)", n)
	}
	// "the" lost at one end, "then" typed at the other: not growth.
	if n := lostChars("the a b c d", "a b c d then"); n != 3 {
		t.Fatalf("lost %d, want 3", n)
	}
}

func TestLogView(t *testing.T) {
	path := filepath.Join(t.TempDir(), "views.jsonl")
	before := stitcher{text: "old", a: 0, b: 3}
	after := stitcher{text: "old new"}
	logView(path, before, view{rows: []string{"old new"}, width: 80}, after)
	logView(path, after, view{rows: []string{"old new more"}, width: 80}, after)
	b, err := os.ReadFile(path)
	if err != nil || strings.Count(string(b), "\n") != 2 || !strings.Contains(string(b), `"after":"old new"`) {
		t.Fatalf("log %q, err %v", b, err)
	}
	logView(filepath.Join(path, "not-a-dir", "x"), before, view{}, after) // must not panic
}

// String sequences never reach the shadow screen, however the chunks split
// them, and the text around them does. The emulator alone prints the end
// of a title holding ✳ (its UTF-8 holds 0x9c, which it takes for ST).
func TestStringSeqsStripped(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a\x1b]0;✳ Claude Code\x07b", "ab"},
		{"a\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\b", "alinkb"},
		{"a\x1bPq✳ x\x1b\\b\x1b_apc\x1b\\c\x1b^pm\x07d\x1bXsos\x07e", "abcde"},
		{"a\x1b]0;cancelled\x18b", "ab"},
		{"a\x1b]0;cut short\x1b[1mb", "a\x1b[1mb"},
		{"\x1b\x1b[1ma\x1b[m", "\x1b\x1b[1ma\x1b[m"},
		{"plain", "plain"},
	}
	for _, c := range cases {
		// Whole, and split at every byte.
		var f stringSeqs
		if got := string(f.strip([]byte(c.in))); got != c.want {
			t.Errorf("%q: %q, want %q", c.in, got, c.want)
		}
		var g stringSeqs
		var out []byte
		for i := range len(c.in) {
			out = append(out, g.strip([]byte{c.in[i]})...)
		}
		if string(out) != c.want {
			t.Errorf("%q byte by byte: %q, want %q", c.in, out, c.want)
		}
	}
	s := &session{screen: vt.NewEmulator(40, 3)}
	go io.Copy(io.Discard, s.screen)
	s.write([]byte("\x1b]0;✳ Claude Code\x07"))
	if got := strings.TrimSpace(snapshot(s.screen).String()); got != "" {
		t.Fatalf("the title reached the screen: %q", got)
	}
}
