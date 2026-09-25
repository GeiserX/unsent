package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

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

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	defer func() { w.Close(); devnull.Close() }()

	code := make(chan int, 1)
	go func() { code <- wrap([]string{"claude"}, r, devnull) }()
	time.Sleep(300 * time.Millisecond)
	script(func(s string) {
		w.WriteString(s)
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
		t.Fatalf("orphans %+v", rs)
	}
	names, _ := filepath.Glob(filepath.Join(st.dir, "drafts", "*.json"))
	if len(names) != 0 {
		b, _ := os.ReadFile(names[0])
		t.Fatalf("left %v: %s", names, b)
	}
}

func TestWrapUnknownAgentPassesThrough(t *testing.T) {
	t.Setenv("UNSENT_HOME", t.TempDir())
	if code := wrap([]string{"sh", "-c", "exit 7"}, os.Stdin, os.Stdout); code != 7 {
		t.Fatalf("exit %d", code)
	}
	if code := wrap([]string{"no-such-agent-unsent-test"}, os.Stdin, os.Stdout); code != 127 {
		t.Fatalf("exit %d", code)
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
