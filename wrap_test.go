package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	if os.Getenv("UNSENT_FAKE_AGENT") == "1" {
		fakeAgent()
		return
	}
	os.Exit(m.Run())
}

func fakeAgent() {
	if old, err := term.MakeRaw(0); err == nil {
		defer term.Restore(0, old)
	}
	cols, _, err := term.GetSize(0)
	if err != nil {
		cols = 80
	}
	var draft []string
	pastes := 0
	var pasting bool
	var paste strings.Builder
	draw := func() {
		var b strings.Builder
		b.WriteString("\x1b[H\x1b[2J")
		rule := strings.Repeat("─", cols)
		b.WriteString(rule + "\r\n")
		rows := wrapText(strings.Join(draft, ""), cols-4)
		if len(draft) == 0 {
			b.WriteString("❯ \x1b[2mTry \"something\"\x1b[m\r\n")
		} else {
			for i, r := range rows {
				prefix := "  "
				if i == 0 {
					prefix = "❯ "
				}
				b.WriteString(prefix + r + "\r\n")
			}
		}
		b.WriteString(rule + "\r\n")
		os.Stdout.WriteString(b.String())
	}
	draw()
	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		in := string(buf[:n])
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
			case strings.HasPrefix(in, "\x1b\r"):
				draft = append(draft, "\n")
				in = in[2:]
			case in[0] == 4: // Ctrl+D quits
				return
			case in[0] == 5: // Ctrl+E quits with an error
				os.Exit(3)
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

// runWrapped runs wrap on the fake agent with keys typed through a pipe,
// and returns its exit code and the store it wrote to.
func runWrapped(t *testing.T, script func(type_ func(string))) (int, *store) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("UNSENT_HOME", home)
	t.Setenv("UNSENT_FAKE_AGENT", "1")
	bin := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(bin, "claude")); err != nil {
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
	go func() {
		// Drain the screen, and start typing once the box is drawn, as a
		// person would: keys sent earlier reach a terminal not yet in raw
		// mode, which echoes them and turns Enter into a line feed.
		var seen []byte
		waiting := true
		buf := make([]byte, 4096)
		for {
			n, err := user.Read(buf)
			if waiting {
				seen = append(seen, buf[:n]...)
				if strings.Contains(string(seen), "❯") {
					close(ready)
					waiting, seen = false, nil
				}
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() { user.Close(); term.Close() }()

	code := make(chan int, 1)
	go func() { code <- wrap([]string{"claude"}, term, term) }()
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("the fake agent never drew its box")
	}
	script(func(s string) {
		user.WriteString(s)
		time.Sleep(3 * saveInterval)
	})
	select {
	case c := <-code:
		st, _ := openStore()
		return c, st
	case <-time.After(10 * time.Second):
		t.Fatal("wrapper did not exit")
	}
	return 0, nil
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
	user, term, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	go io.Copy(io.Discard, user)
	defer func() { user.Close(); term.Close() }()
	if code := wrap([]string{"sh", "-c", "exit 7"}, term, term); code != 7 {
		t.Fatalf("exit %d", code)
	}
	if code := wrap([]string{"no-such-agent-unsent-test"}, term, term); code != 127 {
		t.Fatalf("exit %d", code)
	}
}

func TestWrapStepsAsideForPipes(t *testing.T) {
	t.Setenv("UNSENT_HOME", t.TempDir())
	var ran []string
	old := execAgent
	execAgent = func(bin string, args []string) error { ran = append([]string{bin}, args...); return nil }
	defer func() { execAgent = old }()
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	if code := wrap([]string{"sh", "-c", "true"}, r, w); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(ran) != 4 || !strings.HasSuffix(ran[0], "/sh") || ran[3] != "true" {
		t.Fatalf("ran %q", ran)
	}
	execAgent = func(string, []string) error { return os.ErrPermission }
	if code := wrap([]string{"sh"}, r, w); code != 126 {
		t.Fatalf("exit %d on exec failure", code)
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
		read: func(*screen) (view, bool) {
			reads++
			return view{}, false
		},
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
	s := &session{read: func(*screen) (view, bool) { t.Fatal("read a broken screen"); return view{}, false }}
	s.write([]byte("x")) // s.screen is nil: the emulator write panics
	if !s.broken {
		t.Fatal("panic not caught")
	}
	s.write([]byte("more output keeps flowing"))
	s.dirty = true
	s.save()
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
		{"hello \x1b[A\r", 0, false},
	}
	for _, c := range cases {
		if chars, ahead := deleteKeys([]byte(c.keys)); chars != c.chars || ahead != c.ahead {
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
