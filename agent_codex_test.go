package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Codex's profile on real 0.158.0 frames (testdata/codex/0.158.0): what
// its reader must never take for a draft, what it must go red on when it
// is mutated, and its keys, session ids, chat starts and restores.

// codexNotABox are frames that show no box to save into: dialogs, pickers,
// Ctrl+R's history search, and the dim › Codex shows as it quits. Each
// must read as no box, so the draft is kept and no restore goes in.
var codexNotABox = []struct{ record, shows string }{
	{"dialogs", "Sign in with ChatGPT"},
	{"dialogs", "Use your own OpenAI API key"},
	{"dialogs", "Trust this folder"},
	{"dialogs", "esc back · ctrl+o copy"}, // the warnings view (F2)
	{"picker", "Resume a previous session"},
	{"slash-new", "Where should the new conversation run"},
	{"ctrlc-history", "reverse-i-search:"},
	{"dialogs", "Shutting down"},
}

// codexMenus are frames with a menu or popup drawn above the box: only
// what was typed is the draft, never a row of the menu.
var codexMenus = []struct{ record, shows, draft string }{
	{"dialogs", "choose what model", "/"},
	{"dialogs", "Mentions", "@"},
	{"dialogs", "Keyboard shortcuts", ""},
	{"picker", "Collaboration mode", ""}, // the /status card
}

// codexTyped is every draft each record shows in its box, in the order
// typed: no other text may ever be read from its frames.
var codexTyped = map[string][]string{
	"dialogs":       {"/", "@"},
	"picker":        {"/new", "/resume", "/status"},
	"slash-new":     {"fourth orphan, a new chat must not get it", "/new", "typed after /new"},
	"inline":        {},
	"daemon-draft":  {"a draft under the shared server"},
	"submit":        {"typed but not submitted, dummy", "dummy prompt that must fail on the dead base url", "a draft typed after the failed submit"},
	"ctrlc-history": {"a draft to clear with ctrl c\nits second line", " delt", "typed draft"},
}

// codexScreensCheck returns the first way codexBox misreads the frames
// above, or nil.
func codexScreensCheck(t *testing.T) error {
	t.Helper()
	frames := map[string][]*screen{}
	get := func(record string) []*screen {
		if frames[record] == nil {
			frames[record] = codexFrames(t, record)
		}
		return frames[record]
	}
	for _, c := range codexNotABox {
		f := lastFrame(t, get(c.record), c.shows)
		if v, ok := codexBox(f); ok {
			return fmt.Errorf("%s, the frame showing %q: read as a box %+v", c.record, c.shows, v)
		}
	}
	// Synthesized from a real frame, as no capture has them: the box with
	// its glyph dim, as Codex draws a box that takes no input, and with it
	// reversed, as it draws a menu's chosen row (source, chat_composer.rs).
	empty := lastFrame(t, get("typed"), "Ask Codex to do anything")
	y := codexAnchor(t, empty)
	for name, restyle := range map[string]func(*screenRow){
		"dim":      func(r *screenRow) { r.faint[0] = true },
		"reversed": func(r *screenRow) { r.look[0].reverse = true },
	} {
		f := cloneScreen(empty)
		restyle(&f.rows[y])
		if v, ok := codexBox(f); ok {
			return fmt.Errorf("the box with a %s glyph (synthesized): read as a box %+v", name, v)
		}
	}
	for _, c := range codexMenus {
		f := lastFrame(t, get(c.record), c.shows)
		v, ok := codexBox(f)
		var st stitcher
		if got := st.update(v, codex.unwrap); (!ok || got != c.draft) && (ok || c.draft != "") {
			return fmt.Errorf("%s, the frame showing %q: draft %q (box %v), want %q", c.record, c.shows, got, ok, c.draft)
		}
	}
	for record, typed := range codexTyped {
		for _, f := range get(record) {
			v, ok := codexBox(f)
			if !ok || v.empty {
				continue
			}
			text := strings.Join(v.rows, "\n")
			if !slices.ContainsFunc(typed, func(d string) bool { return strings.HasPrefix(d, strings.TrimRight(text, "\n")) }) {
				return fmt.Errorf("%s: read %q from a frame, which was never typed there:\n%s", record, text, f)
			}
		}
	}
	return nil
}

func TestCodexNegativeScreens(t *testing.T) {
	if err := codexScreensCheck(t); err != nil {
		t.Fatal(err)
	}
}

// A check that cannot fail is not a check: each part of the reader,
// mutated, turns a replay or a negative screen red.
func TestCodexReaderMutations(t *testing.T) {
	good := codexLayout
	t.Cleanup(func() { codexLayout = good })
	for _, m := range []struct {
		name   string
		mutate func(*boxLayout)
	}{
		{"marker", func(b *boxLayout) { b.glyphs = []string{"❯"} }},
		{"a dim or reversed glyph", func(b *boxLayout) { b.glyph = func(l cellLook, _ bool) bool { return l.bold } }},
		{"indent 3", func(b *boxLayout) { b.indent = 3 }},
		{"wrap at the width minus 4", func(b *boxLayout) { b.margin = 4 }},
		{"wrap at the width minus 2", func(b *boxLayout) { b.margin = 2 }},
		{"one footer row", func(b *boxLayout) { b.footer = 1 }},
		{"no footer check", func(b *boxLayout) { b.footerRow = func(screenRow) bool { return true } }},
		{"no history search", func(b *boxLayout) { b.search = "" }},
	} {
		t.Run(m.name, func(t *testing.T) {
			bad := good
			m.mutate(&bad)
			codexLayout = bad
			defer func() { codexLayout = good }()
			var red []string
			if err := codexScreensCheck(t); err != nil {
				red = append(red, "screens")
			}
			for _, r := range codexReplays {
				if replayCodex(t, &codex, r.name, r.recalled...) != nil {
					red = append(red, r.name)
				}
			}
			if len(red) == 0 {
				t.Fatal("every Codex replay and screen stayed green")
			}
			t.Logf("red: %s", strings.Join(red, ", "))
		})
	}
}

// The delete keys, the recall keys and the submit keys are what the
// captures need: without the keys that delete, text deleted on the last
// row of a box at its cap reads as scrolled out of sight below; without
// Up as a recall key, a history entry brought back is saved as a draft;
// without Enter as a submit key, the send in orphan goes to history, not
// to the sent log.
func TestCodexKeyMutations(t *testing.T) {
	for _, m := range []struct {
		name string
		drop func(*keyset)
	}{
		{"keys that delete a word or more", func(k *keyset) { k.many = nil }},
		{"keys that delete one character", func(k *keyset) { k.one = nil }},
		{"keys that delete after the cursor", func(k *keyset) { k.ahead = nil }},
	} {
		t.Run(m.name, func(t *testing.T) {
			p := codex
			m.drop(&p.keys)
			if replayCodex(t, &p, "edge-deletes") == nil {
				t.Fatal("edge-deletes stayed green")
			}
		})
	}
	t.Run("recall keys", func(t *testing.T) {
		p := codex
		p.keys.recall = nil
		if replayCodex(t, &p, "ctrlc-history", 1) == nil || replayCodex(t, &p, "placeholder", 2) == nil {
			t.Fatal("a history entry brought back with Up was not saved as a draft")
		}
	})
	t.Run("submit keys", func(t *testing.T) {
		p := codex
		p.keys.submit = nil
		if codexSendCheck(t, &p) == nil {
			t.Fatal("the send stayed in the sent log without a submit key")
		}
	})
}

// codexSendCheck replays orphan, where a prompt is sent with Enter (the
// request fails on the dead port), the turn is interrupted with Ctrl+C
// and a draft is typed: the prompt must be in the sent log and not in
// history, and the draft still in the box.
func codexSendCheck(t *testing.T, prof *profile) error {
	t.Helper()
	s, err := replayCodexSession(t, prof, "orphan")
	if err != nil {
		return err
	}
	const prompt = "first prompt, dummy, must fail on the dead port"
	l, _ := readSent(s.store.sentPath(s.rec.ID))
	var sent []string
	if l != nil {
		for _, m := range l.messages {
			sent = append(sent, m.Text)
		}
	}
	if !slices.Equal(sent, []string{prompt}) {
		return fmt.Errorf("sent log %q, want the prompt", sent)
	}
	if inHistory(s.store, prompt) {
		return fmt.Errorf("the sent prompt is in history too")
	}
	if want := "orphan draft line one, ñandú\nline two of the orphan"; s.rec.Draft != want {
		return fmt.Errorf("draft %q, want %q", s.rec.Draft, want)
	}
	return nil
}

func TestCodexSendGoesToTheSentLog(t *testing.T) {
	if err := codexSendCheck(t, &codex); err != nil {
		t.Fatal(err)
	}
}

// A history entry brought back with Up is no draft, but sending it is a
// send: in recall-send a prompt is cleared with Ctrl+C, brought back with
// Up and sent with Enter. It is in history once, from the Ctrl+C, and in
// the sent log once.
func TestCodexRecalledEntrySent(t *testing.T) {
	s, err := replayCodexSession(t, &codex, "recall-send")
	if err != nil {
		t.Fatal(err)
	}
	const prompt = "a prompt cleared, then sent from history, dummy"
	l, err := readSent(s.store.sentPath(s.rec.ID))
	if err != nil || len(l.messages) != 1 || l.messages[0].Text != prompt {
		t.Fatalf("sent log %+v, %v; want the prompt once", l, err)
	}
	n := 0
	for _, h := range s.store.load(true) {
		if h.Draft == prompt && h.ID != s.rec.ID {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d copies of the prompt in history, want the one Ctrl+C cleared", n)
	}
}

// codexKeysSession is a session with Codex's keys on the shadow screen of
// sent_test.go, where the test draws the box by hand: each redraw lands
// exactly where the test puts it, which no capture can do for Codex's own
// put-backs (a turn cannot run on the dead base URL).
func codexKeysSession(t *testing.T) sendSession {
	t.Helper()
	p := claude
	p.keys = codex.keys
	return newSendSession(t, &p)
}

// A box emptied with delete keys is as empty as one a submit or clear key
// emptied: Up then brings back a history entry, which is not a draft.
func TestCodexRecallAfterTheBoxIsDeletedEmpty(t *testing.T) {
	for _, c := range []struct{ name, empty string }{
		{"Backspace", "\x7f\x7f\x7f"},
		{"Ctrl+C", "\x03"}, // the control: a clear key
	} {
		t.Run(c.name, func(t *testing.T) {
			s := codexKeysSession(t)
			s.input([]byte("abc"))
			s.draw("abc")
			s.save()
			s.input([]byte(c.empty))
			s.draw("")
			s.save()
			s.input([]byte("\x1b[A"))
			s.draw("first entry for history")
			s.save()
			if s.rec.Draft != "" {
				t.Fatalf("the history entry was saved as the draft %q", s.rec.Draft)
			}
			s.input([]byte("!"))
			s.draw("first entry for history!")
			s.save()
			if s.rec.Draft != "first entry for history!" {
				t.Fatalf("the edited entry is not the draft: %q", s.rec.Draft)
			}
		})
	}
}

// Up right after a send brings back the prompt just sent: it is in the
// sent log once, and the box holds a history entry, not a draft.
func TestCodexRecallAfterASend(t *testing.T) {
	for _, submit := range []string{"\r", "\t"} {
		s := codexKeysSession(t)
		s.input([]byte("the prompt"))
		s.draw("the prompt")
		s.save()
		s.input([]byte(submit))
		s.draw("")
		s.save()
		s.input([]byte("\x1b[A"))
		s.draw("the prompt")
		s.save()
		if s.rec.Draft != "" {
			t.Fatalf("%q: the prompt sent and brought back with Up was saved as the draft", submit)
		}
		s.expect([]string{"the prompt"}, nil)
	}
	// Typed and sent within one save, with Codex's answer to Enter split
	// across two reads mid-frame: no save read the prompt, nor the screen
	// Enter was typed into. Up still brings back a history entry.
	s := codexKeysSession(t)
	s.input([]byte("the prompt"))
	s.draw("the prompt")
	s.write(frameBegin)
	s.input([]byte("\r"))
	s.write(append([]byte(drawBox(100, "")), frameEnd...))
	s.save()
	s.input([]byte("\x1b[A"))
	s.draw("the prompt")
	s.save()
	if s.rec.Draft != "" {
		t.Fatalf("a prompt sent unseen and brought back with Up was saved as the draft")
	}
}

// Text Codex puts back into the empty box by itself is a draft, although
// no key edited the box since the last submit or clear key: a message
// queued with Tab that an interrupted turn gives back, a send Codex
// refuses, a thread's box on a switch (codex-rs tui input_restore.rs).
// Here the Tab-send, then Ctrl+C on the empty box to interrupt the turn,
// then the queued message drawn back.
func TestCodexTextGivenBackIsADraft(t *testing.T) {
	s := codexKeysSession(t)
	s.input([]byte("queued while the turn runs"))
	s.draw("queued while the turn runs")
	s.save()
	s.input([]byte("\t"))
	s.draw("")
	s.save()
	s.input([]byte("\x03"))
	s.save()
	s.draw("queued while the turn runs")
	s.save()
	if s.rec.Draft != "queued while the turn runs" {
		t.Fatalf("the message given back is not saved: draft %q", s.rec.Draft)
	}
}

// The window closes, or Codex is killed, with a draft in the box: Codex
// keeps it in no file, and unsent keeps it as an orphan. kill9 kills the
// Codex process, killsession closes the window, daemon-killsession closes
// it under the shared background server, and daemon-quit quits with
// Ctrl+C on an empty box, which leaves nothing. Codex drew nothing after
// a kill or a closed window: the captures end at the draft.
var codexCloses = []struct {
	name   string
	closed bool
	draft  string
}{
	{"kill9", false, "kill nine draft zebra\nsecond line of it"},
	{"killsession", true, "kill session draft yak\nsecond line of it"},
	{"daemon-killsession", true, "daemon draft walrus"},
	{"daemon-quit", false, ""},
}

func codexCloseCheck(t *testing.T, prof *profile) error {
	t.Helper()
	for _, c := range codexCloses {
		s, err := replayCodexSession(t, prof, c.name)
		if err != nil {
			return err
		}
		if c.closed {
			s.closing()
		}
		s.save()
		s.finish()
		var kept []string
		for _, r := range s.store.orphans() {
			kept = append(kept, r.Draft)
		}
		var want []string
		if c.draft != "" {
			want = []string{c.draft}
		}
		if !slices.Equal(kept, want) {
			return fmt.Errorf("%s: orphans %q, want %q", c.name, kept, want)
		}
		for _, h := range s.store.load(true) {
			if h.ID != s.rec.ID {
				return fmt.Errorf("%s: %q in history", c.name, h.Draft)
			}
		}
	}
	return nil
}

func TestCodexWindowCloseKeepsTheDraft(t *testing.T) {
	if err := codexCloseCheck(t, &codex); err != nil {
		t.Fatal(err)
	}
	// A check that can fail: a reader that takes every box for empty loses
	// the drafts.
	empty := codex
	empty.read = func(scr *screen) (view, bool) {
		v, ok := codexBox(scr)
		v.rows, v.empty = nil, true
		return v, ok
	}
	if codexCloseCheck(t, &empty) == nil {
		t.Error("with every box read empty the close check stayed green")
	}
}

// Ctrl+Z is unsent's, in the form tmux sent it (ESC[122;5u), and unsent
// writes nothing for Codex as it suspends it: Codex's repaint after the
// resume turns none of its modes on again (testdata/codex/0.158.0/suspend),
// so unsent turns bracketed paste back on itself as it resumes Codex
// (TestWrapResumeTurnsBracketedPasteBackOn).
func TestCodexSuspendPolicy(t *testing.T) {
	if at, end := findKey([]byte("ab\x1b[122;5ucd"), suspendKeys(&codex)); at != 2 || end != 10 {
		t.Fatalf("Ctrl+Z found at %d..%d", at, end)
	}
	if codex.suspended != nil {
		t.Fatalf("Codex's suspend output %q: its repaint after a resume sets none of it again", codex.suspended)
	}
	// From the Ctrl+Z to the next key, a Ctrl+G typed once the box was
	// back: the stop, fg, and Codex's repaint.
	data := readCodexRec(t, "suspend")
	stop := bytes.Index(data, []byte("\x1b[122;5u"))
	next := bytes.Index(data[max(stop, 0):], []byte("\x1b[103;5u"))
	if stop < 0 || next < 0 {
		t.Fatal("no Ctrl+Z and Ctrl+G after it in the capture")
	}
	resumed := data[stop : stop+next]
	if !bytes.Contains(resumed, []byte("draft,")) {
		t.Fatal("no repaint of the draft between the Ctrl+Z and the next key")
	}
	for _, mode := range []string{"\x1b[?1049h", "\x1b[>7u", "\x1b[?2004h"} {
		if bytes.Contains(resumed, []byte(mode)) {
			t.Fatalf("Codex set %q again after the resume; its suspend output can go back in the profile", mode)
		}
	}
	// In suspend-paste, recorded by unsent with this policy, the shell's fg
	// turned bracketed paste off and unsent turned it on again: tmux sent
	// the paste typed after fg with its marks, and nothing was sent (a
	// build without session.resuming sent the draft at the paste's first
	// line break).
	data = readCodexRec(t, "suspend-paste")
	want := "\x1b[200~\rpasted line one\rpasted line two\rpasted line three\x1b[201~"
	var in []byte
	eachChunk(t, data, func(_ time.Time, dir byte, chunk []byte) {
		if dir == 'i' {
			in = append(in, chunk...)
		}
	})
	if !bytes.Contains(in, []byte(want)) {
		t.Fatalf("the paste after fg did not reach Codex as one bracketed paste: %q", in)
	}
	if !bytes.Contains(in, []byte("draft before the stop")) || bytes.Contains(bytes.Replace(in, []byte(want), nil, 1), []byte("\r")) {
		t.Fatal("no draft before the stop, or an Enter reached Codex")
	}
}

func readCodexRec(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "codex", "0.158.0", name+".rec"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Codex is a profile: `codex` runs through its reader, and setup wraps it.
func TestCodexIsAProfile(t *testing.T) {
	if p := profileFor("/opt/homebrew/bin/codex"); p != &codex {
		t.Fatalf("profile for codex: %v", p)
	}
	if !slices.Contains(agentCommands(), "codex") {
		t.Fatalf("setup wraps %q, not codex", agentCommands())
	}
}

func TestCodexChat(t *testing.T) {
	for _, c := range []struct {
		args []string
		chat bool
	}{
		{nil, true},
		{[]string{"-c", `sandbox_mode="danger-full-access"`}, true},
		{[]string{"--config=model=\"o3\"", "--no-daemon", "--search"}, true},
		{[]string{"resume"}, true},
		{[]string{"resume", "01a0eba3-2091-7411-84fe-2b5faf9f0a2a"}, true},
		{[]string{"-c", "x=1", "resume", "--last"}, true},
		{[]string{"resume", "--all", "-m", "gpt", "01a0eba3"}, true},
		{[]string{"fork", "--last"}, true},
		{[]string{"fix the tests"}, false},
		{[]string{"-m", "gpt", "fix the tests"}, false},
		{[]string{"resume", "01a0eba3", "and a prompt"}, false},
		{[]string{"resume", "--last", "a prompt"}, false},
		{[]string{"exec", "x"}, false},
		{[]string{"login"}, false},
		{[]string{"mcp", "list"}, false},
		{[]string{"-i", "shot.png"}, false},
		{[]string{"--version"}, false},
		{[]string{"--", "x"}, false},
	} {
		if got := codexChat(c.args); got != c.chat {
			t.Errorf("codexChat(%q) = %v, want %v", c.args, got, c.chat)
		}
	}
}

// The session id is the thread whose lock the process holds open, read
// from the process's open files: from lsof on macOS, from /proc on Linux.
// This test's own process holds the locks here.
func TestCodexSessionReadsTheHeldLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	locks := filepath.Join(home, "thread-writer-locks")
	if err := os.MkdirAll(locks, 0o755); err != nil {
		t.Fatal(err)
	}
	hold := func(name string) *os.File {
		f, err := os.Create(filepath.Join(locks, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return f
	}
	// A lock file nobody holds, and the coordination lock, name nothing.
	os.WriteFile(filepath.Join(locks, "01a0eb00-0000-7000-8000-000000000000.lock"), nil, 0o644)
	hold(".coordination.lock")
	pid := os.Getpid()
	codexHeld = heldLocks{}
	if id, found := codexSession(pid, time.Now()); found || id != "" {
		t.Fatalf("no thread lock held: %q, %v", id, found)
	}
	a := "01a0eba3-2091-7411-84fe-2b5faf9f0a2a"
	hold(a + ".lock")
	if id, found := codexSession(pid, time.Now()); !found || id != a {
		t.Fatalf("one lock held: %q, %v, want %q", id, found, a)
	}
	if pids := codexSessionPids(); !slices.Contains(pids, pid) {
		t.Fatalf("holders %v, want this process %d", pids, pid)
	}
	tr := newSessionTracker(codex.session, pid, time.Now())
	if got := tr.current(); got != a {
		t.Fatalf("the tracker names %q, want %q", got, a)
	}
	// /new keeps the first thread's lock: two held name no thread, and
	// the tracker lets go of the one it named.
	hold("01a0eba7-a867-74c2-a5d5-a7d880e67121.lock")
	if id, found := codexSession(pid, time.Now()); !found || id != sessionUnsure {
		t.Fatalf("two locks held: %q, %v, want %q", id, found, sessionUnsure)
	}
	if got := tr.current(); got != "" {
		t.Fatalf("the tracker names %q for two held locks", got)
	}
}

// Asking the process's open files runs lsof on macOS, so it is asked again
// only when the lock folder changes or codexRecheck has passed.
func TestCodexSessionCachesTheAnswer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	locks := filepath.Join(home, "thread-writer-locks")
	os.MkdirAll(locks, 0o755)
	asked := 0
	old := openFiles
	t.Cleanup(func() { openFiles = old })
	a := filepath.Join(locks, "01a0eba3-2091-7411-84fe-2b5faf9f0a2a.lock")
	real, _ := filepath.EvalSymlinks(locks)
	openFiles = func(int) []string {
		asked++
		return []string{filepath.Join(real, filepath.Base(a)), "/dev/null"}
	}
	codexHeld = heldLocks{}
	for range 3 {
		codexSession(42, time.Now())
	}
	if asked != 1 {
		t.Fatalf("asked %d times for an unchanged folder", asked)
	}
	codexSession(43, time.Now())
	if asked != 2 {
		t.Fatalf("another process was not asked: %d", asked)
	}
	codexHeld.at = time.Now().Add(-codexRecheck)
	codexSession(43, time.Now())
	if asked != 3 {
		t.Fatalf("not asked again after codexRecheck: %d", asked)
	}
}

// Under Codex's shared background server the thread is named by the
// server, a process Codex starts a moment after it draws the box. A
// draft typed first takes the thread's id once it is read, though the
// screen has not changed since (daemon-draft); a draft that has a session
// keeps it.
func TestLateSessionNamesTheDraft(t *testing.T) {
	s := newSendSession(t, &claude)
	id := ""
	fakeIDs(s.session, &id)
	s.draw("")
	s.save()
	s.input([]byte("abc"))
	s.draw("abc")
	s.save()
	if s.rec.AgentSession != "" {
		t.Fatalf("session %q before one is named", s.rec.AgentSession)
	}
	id = "thread-a"
	s.save() // nothing new on screen
	if s.rec.AgentSession != "thread-a" {
		t.Fatalf("session %q, want thread-a", s.rec.AgentSession)
	}
	id = "thread-b"
	s.save()
	if s.rec.AgentSession != "thread-a" {
		t.Fatalf("a draft that had a session moved to %q with no redraw", s.rec.AgentSession)
	}
}

// Saving stopped says which Codex ran and which one the reader was last
// checked against.
func TestCodexSavingStoppedLine(t *testing.T) {
	s := &session{prof: &codex, version: make(chan string, 1)}
	s.typed.Store(true)
	s.version <- agentVersionOf("codex-cli 0.159.0\n")
	want := "unsent: could not read codex 0.159.0's box this session (last verified 0.158.0), nothing was saved"
	if got := s.exitLines(); !slices.Equal(got, []string{want}) {
		t.Fatalf("exit lines %q, want %q", got, want)
	}
}

func agentVersionOf(out string) string { return versionRE.FindString(out) }

// Restore-in-box through Codex's own frames: the draft a window close
// left in orphan goes back into the box of codex resume <id>, codex
// resume --last and the picker once the box has been read empty, and is
// read back from the frames Codex drew for the paste; the orphan then
// moves to history. The recorded paste is unsent's own, so the replay
// drops it and the restore under test makes it again.
func TestCodexRestoreReplays(t *testing.T) {
	thread := "01a0eba3-2091-7411-84fe-2b5faf9f0a2a"
	for _, c := range []struct{ record, draft string }{
		{"restore-id", "orphan draft line one, ñandú\nline two of the orphan"},
		{"restore-last", "second orphan, for resume --last"},
		{"restore-picker", "third orphan, for the picker"},
		{"restore-long", codexLongDraft()},
	} {
		t.Run(c.record, func(t *testing.T) {
			r := codexRestoreRig(t, thread)
			seedOrphan(t, r.store, "old", "codex", thread, c.draft)
			paste := string(pasteStart) + c.draft + string(pasteEnd)
			// Up to the first key typed after the recorded paste: until then
			// the box shows what the paste put there.
			data := readCodexRec(t, c.record)
			at := bytes.Index(data, []byte(paste))
			if at < 0 {
				t.Fatal("no restore paste in the record")
			}
			r.replayRestore(codexUntilKey(t, data, at+len(paste)), paste)
			if got := r.pasted(); got != paste {
				t.Fatalf("pasted %q, want %q", got, paste)
			}
			if orphanAt(r.store, "old") != nil || !inHistory(r.store, c.draft) {
				t.Fatal("the orphan was not read back and moved to history")
			}
			// From then on it is the session's draft, so a window closed now
			// leaves it for the next restore.
			if r.rec.Draft != c.draft {
				t.Fatalf("the session's draft is %q, want the one put back", r.rec.Draft)
			}
		})
	}
}

// No restore goes into Codex's dialogs or the resume picker, drawn before
// the box (replayed up to the box's first frame), nor into the box of a
// new chat, nor once a key was typed.
func TestCodexNoRestore(t *testing.T) {
	for _, c := range []struct{ record, session, why string }{
		{"dialogs", "any", "sign-in, API key and trust dialogs"},
		{"picker", "any", "the resume picker"},
		{"early-paste", "01a0eba3-2091-7411-84fe-2b5faf9f0a2a", "a paste typed on the box's first frame"},
		{"orphan", "the-new-chat", "a new chat's box"},
	} {
		t.Run(c.record, func(t *testing.T) {
			r := codexRestoreRig(t, c.session)
			orphanSession := c.session
			if c.record == "orphan" {
				orphanSession = "an-older-thread"
			}
			seedOrphan(t, r.store, "old", "codex", orphanSession, "keep me")
			data := readCodexRec(t, c.record)
			if c.session == "any" {
				data = codexUntilBox(t, data)
			}
			r.replayRestore(data, "")
			if got := r.pasted(); strings.Contains(got, "keep me") {
				t.Fatalf("%s: pasted %q", c.why, got)
			}
		})
	}
}

// codexRestoreRig is a restore rig for Codex at 120x40, with no settle
// delay, in session thread.
func codexRestoreRig(t *testing.T, thread string) *restoreRig {
	t.Helper()
	p := codex
	caps := *codex.restore
	caps.settle = 0
	p.restore = &caps
	r := newRestoreRig(t, &p, thread)
	r.rec.Agent = "codex"
	r.resize(120, 40)
	r.quietUntil = time.Time{}
	return r
}

// replayRestore feeds a Codex record through the rig: its output frame by
// frame and its keys, with a tick for every saveInterval of the record's
// time, as the save loop ticks, but for the restore paste unsent itself
// made in the recording (skip), which the rig makes again.
func (r *restoreRig) replayRestore(data []byte, skip string) {
	r.t.Helper()
	var last time.Time
	eachChunk(r.t, data, func(at time.Time, dir byte, chunk []byte) {
		if last.IsZero() {
			last = at
		}
		for ; !at.Before(last.Add(saveInterval)); last = last.Add(saveInterval) {
			r.ticks(1)
		}
		switch dir {
		case 'i':
			if skip == "" || string(chunk) != skip {
				r.input(chunk)
			}
		case 'o':
			for len(chunk) > 0 {
				k := len(chunk)
				if i := bytes.Index(chunk, frameEnd); i >= 0 {
					k = i + len(frameEnd)
				}
				r.write(chunk[:k])
				chunk = chunk[k:]
			}
		}
	})
	// The save loop ticks on after the record ends.
	r.ticks(1)
}

// codexUntilBox is a record cut at the output chunk that first draws the
// box's empty hint, with every chunk and tick before it.
func codexUntilBox(t *testing.T, data []byte) []byte {
	t.Helper()
	hint := []byte("Ask Codex to do anything")
	for off := 0; off+24 <= len(data); {
		n := int(binary.LittleEndian.Uint64(data[off:]))
		if dir := data[off+20]; dir == 'o' && bytes.Contains(data[off+24:off+24+n], hint) {
			if off == 0 {
				t.Fatal("the box is in the first chunk")
			}
			return data[:off]
		}
		off += 24 + n
	}
	t.Fatal("the record never draws the box")
	return nil
}

// cloneScreen copies a screen, so a test can change a cell of the copy.
func cloneScreen(s *screen) *screen {
	c := *s
	c.rows = make([]screenRow, len(s.rows))
	for i, r := range s.rows {
		c.rows[i] = screenRow{cells: slices.Clone(r.cells), faint: slices.Clone(r.faint), look: slices.Clone(r.look)}
	}
	return &c
}

// codexUntilKey is a record cut before the first input chunk that starts
// at or after offset from.
func codexUntilKey(t *testing.T, data []byte, from int) []byte {
	t.Helper()
	for off := 0; off+24 <= len(data); {
		n := int(binary.LittleEndian.Uint64(data[off:]))
		if data[off+20] == 'i' && off >= from {
			return data[:off]
		}
		off += 24 + n
	}
	return data
}

// codexLongDraft is the draft restore-long puts back: 18 lines, 1,259
// characters, over Codex's paste threshold.
func codexLongDraft() string {
	var lines []string
	for i := 1; i <= 18; i++ {
		lines = append(lines, fmt.Sprintf("long restore line %02d, ñandú, dummy text to pass a thousand characters", i))
	}
	return strings.Join(lines, "\n")
}
