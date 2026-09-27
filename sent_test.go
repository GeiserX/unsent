package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

// sendSession is a session on a shadow screen that the test draws the fake
// agent's box on by hand, so each key, each redraw and each save lands
// exactly where the test puts it.
type sendSession struct {
	*session
	t *testing.T
}

func newSendSession(t *testing.T, prof *profile) sendSession {
	t.Helper()
	s := &session{
		screen: vt.NewEmulator(100, 30),
		rec:    newRecord([]string{"claude"}, "/work"),
		store:  testStore(t),
		pastes: &pasteTracker{},
		prof:   prof,
	}
	s.rec.Agent = "claude"
	go io.Copy(io.Discard, s.screen)
	return sendSession{s, t}
}

// draw redraws the box holding text, as the agent's answer to the keys.
func (s sendSession) draw(text string) { s.write([]byte(drawBox(100, text))) }

func (s sendSession) save() {
	s.t.Helper()
	if s.session.save(); s.broken {
		s.t.Fatal("the session stopped saving")
	}
}

// sent is the text of every message in the session's sent log.
func (s sendSession) sent() []string {
	s.t.Helper()
	l, err := readSent(s.store.sentPath(s.rec.ID))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		s.t.Fatal(err)
	}
	var out []string
	for _, m := range l.messages {
		out = append(out, m.Text)
	}
	return out
}

// history is the draft of every record in history/, oldest first.
func (s sendSession) history() []string {
	var out []string
	for _, r := range s.store.load(true) {
		if r.ID != s.rec.ID {
			out = append([]string{r.Draft}, out...)
		}
	}
	return out
}

func (s sendSession) expect(sent, history []string) {
	s.t.Helper()
	if got := s.sent(); !slices.Equal(got, sent) {
		s.t.Errorf("sent log %q, want %q", got, sent)
	}
	if got := s.history(); !slices.Equal(got, history) {
		s.t.Errorf("history %q, want %q", got, history)
	}
}

// Enter on a box with text sends it: the text goes to the sent log, pastes
// expanded, and not to history.
func TestSendEnterLogsTheText(t *testing.T) {
	s := newSendSession(t, &claude)
	body := strings.Repeat("pasted line\r", 5) + "last"
	s.input([]byte("see: \x1b[200~" + body + "\x1b[201~"))
	s.draw("see: [Pasted text #1 +5 lines]")
	s.save()
	s.input([]byte("\r"))
	s.draw("")
	s.save()
	s.expect([]string{"see: " + strings.Repeat("pasted line\n", 5) + "last"}, nil)
	if s.rec.Draft != "" {
		t.Fatalf("draft %q after the send", s.rec.Draft)
	}
}

// A key typed just before Enter, inside one save interval, is in the logged
// text: the screen the Enter was typed into is read before the agent's
// answer replaces it.
func TestSendLogsTheKeyTypedJustBeforeEnter(t *testing.T) {
	s := newSendSession(t, &claude)
	s.draw("hello")
	s.save()
	s.input([]byte("x"))
	s.draw("hellox")
	s.input([]byte("\r"))
	s.draw("")
	s.save()
	s.expect([]string{"hellox"}, nil)
}

// A save that runs after Enter but before the agent answers it must not
// take the unchanged box as proof that Enter did not send.
func TestSendAnsweredAfterTheSave(t *testing.T) {
	s := newSendSession(t, &claude)
	s.draw("slow agent")
	s.input([]byte("\r"))
	s.save()
	if s.rec.Draft != "slow agent" {
		t.Fatalf("draft %q", s.rec.Draft)
	}
	s.draw("")
	s.save()
	s.expect([]string{"slow agent"}, nil)
}

// clearThenSave types a draft, saves, types keys, draws an empty box and
// saves again.
func clearThenSave(s sendSession, keys ...string) {
	s.draw("mistake")
	s.save()
	for _, k := range keys {
		s.input([]byte(k))
	}
	s.draw("")
	s.save()
}

// Ctrl+C and Esc Esc clear the box: the draft goes to history, never to
// the sent log. Esc Esc arrives as one read or as two.
func TestSendCtrlCIsAClear(t *testing.T) {
	for _, keys := range [][]string{{"\x03"}, {"\x1b\x1b"}, {"\x1b", "\x1b"}} {
		t.Run(fmt.Sprintf("%q", keys), func(t *testing.T) {
			s := newSendSession(t, &claude)
			clearThenSave(s, keys...)
			s.expect(nil, []string{"mistake"})
		})
	}
}

// A check that cannot fail is not a check: with a submit key that matches
// every key, the Ctrl+C test above must fail.
func TestSendCtrlCTestCatchesAnAlwaysTrueSubmit(t *testing.T) {
	broken := claude
	broken.keys.submit = nil
	for b := range 256 {
		broken.keys.submit = append(broken.keys.submit, []byte{byte(b)})
	}
	s := newSendSession(t, &broken)
	clearThenSave(s, "\x03")
	if got := s.sent(); !slices.Equal(got, []string{"mistake"}) {
		t.Fatalf("sent log %q: the Ctrl+C test would stay green with a broken submit match", got)
	}
}

// A submit key and a clear key between the same two reads is doubt: the
// draft goes to history.
func TestSendAndClearTogetherIsAClear(t *testing.T) {
	for _, keys := range [][]string{{"\r\x03"}, {"\x03\r"}, {"\r", "\x03"}, {"\x03", "\r"}} {
		t.Run(fmt.Sprintf("%q", keys), func(t *testing.T) {
			s := newSendSession(t, &claude)
			clearThenSave(s, keys...)
			s.expect(nil, []string{"mistake"})
		})
	}
}

// Ctrl+C answered by the agent, then Enter on the empty box, both before
// the next save: the draft was cleared, not sent.
func TestSendEnterOnAClearedBox(t *testing.T) {
	s := newSendSession(t, &claude)
	s.draw("mistake")
	s.save()
	s.input([]byte("\x03"))
	s.draw("")
	s.input([]byte("\r"))
	s.draw("")
	s.save()
	s.expect(nil, []string{"mistake"})
}

// Two quick Enters: the first sends, the second finds the box empty.
func TestSendDoubleEnter(t *testing.T) {
	s := newSendSession(t, &claude)
	s.draw("once")
	s.save()
	s.input([]byte("\r"))
	s.draw("")
	s.input([]byte("\r"))
	s.draw("")
	s.save()
	s.expect([]string{"once"}, nil)
}

// Keys after Enter mean Enter was not the last key: doubt.
func TestSendKeyAfterEnterIsAClear(t *testing.T) {
	s := newSendSession(t, &claude)
	clearThenSave(s, "\r", "\x15")
	s.expect(nil, []string{"mistake"})
}

// Enter typed at a dialog does not send the box, even if the box then
// comes back empty.
func TestSendEnterAtADialog(t *testing.T) {
	s := newSendSession(t, &claude)
	s.draw("kept for later")
	s.save()
	s.write([]byte("\x1b[H\x1b[2JDo you trust the files in this folder?\r\n❯ 1. Yes, proceed\r\n  2. No, exit\r\n"))
	s.input([]byte("\r"))
	s.draw("")
	s.save()
	s.expect(nil, []string{"kept for later"})
}

// Enter that does not empty the box (a new line, a completion) does not
// count when the box empties later without a key.
func TestSendEnterThatKeptTheText(t *testing.T) {
	s := newSendSession(t, &claude)
	s.draw("line one")
	s.save()
	s.input([]byte("\r"))
	s.draw("line one\n")
	s.save()
	s.draw("")
	s.save()
	s.expect(nil, []string{"line one\n"})
}

// Focus reports and mouse moves are not keys: Enter stays the last key.
func TestSendIgnoresFocusAndMouseReports(t *testing.T) {
	s := newSendSession(t, &claude)
	clearThenSave(s, "\r", "\x1b[O", "\x1b[<35;10;5M\x1b[I")
	s.expect([]string{"mistake"}, nil)
}

// A profile without submit keys never writes a sent log.
func TestSendNeedsSubmitKeys(t *testing.T) {
	none := claude
	none.keys.submit = nil
	s := newSendSession(t, &none)
	clearThenSave(s, "\r")
	s.expect(nil, []string{"mistake"})
	if names, _ := filepath.Glob(filepath.Join(s.store.dir, "sent", "*")); len(names) != 0 {
		t.Fatalf("sent/ holds %v", names)
	}
}

// Under delete, nothing of a sent message stays anywhere in the state folder.
func TestSendDeleteKeepsNothing(t *testing.T) {
	t.Setenv("UNSENT_ON_SEND", "delete")
	s := newSendSession(t, &claude)
	s.input([]byte("secret: \x1b[200~" + strings.Repeat("pasted secret\r", 5) + "\x1b[201~"))
	s.draw("secret: [Pasted text #1 +5 lines]")
	s.save()
	s.store.keepVersion(s.rec)
	s.input([]byte("\r"))
	s.draw("")
	s.save()
	s.store.write(s.rec)
	filepath.Walk(s.store.dir, func(path string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			if b, _ := os.ReadFile(path); strings.Contains(string(b), "secret") {
				t.Errorf("%s keeps the message: %s", path, b)
			}
		}
		return nil
	})
}

// A sent log that cannot be written sends the draft to history instead.
func TestSendFallsBackToHistory(t *testing.T) {
	s := newSendSession(t, &claude)
	sent := filepath.Join(s.store.dir, "sent")
	os.Chmod(sent, 0o500)
	defer os.Chmod(sent, 0o700)
	s.store.warned = true // keep the test output quiet
	clearThenSave(s, "\r")
	s.expect(nil, []string{"mistake"})
}

func TestOnSend(t *testing.T) {
	cases := []struct{ global, claude, agent, want string }{
		{"", "", "claude", "log"},
		{"delete", "", "claude", "delete"},
		{"delete", "log", "claude", "log"},
		{"log", "delete", "claude", "delete"},
		{"DELETE", "", "claude", "delete"},
		{"nonsense", "", "claude", "log"},
		{"delete", "log", "codex", "delete"},
	}
	for _, c := range cases {
		t.Setenv("UNSENT_ON_SEND", c.global)
		t.Setenv("UNSENT_ON_SEND_CLAUDE", c.claude)
		if got := onSend(c.agent); got != c.want {
			t.Errorf("UNSENT_ON_SEND=%q UNSENT_ON_SEND_CLAUDE=%q: %s gets %q, want %q", c.global, c.claude, c.agent, got, c.want)
		}
	}
	t.Setenv("UNSENT_ON_SEND_CURSOR_AGENT", "delete")
	if got := onSend("cursor-agent"); got != "delete" {
		t.Errorf("cursor-agent gets %q", got)
	}
}

func TestKeyKinds(t *testing.T) {
	const o, s, c = keyOther, keySubmit, keyClear
	cases := []struct {
		keys string
		want []keyKind
		esc  bool
	}{
		{"\r", []keyKind{s}, false},
		{"hello\r", []keyKind{o, s}, false},
		{"\x1b\r", []keyKind{o}, false}, // Esc+Enter: a new line
		{"\x18\r", []keyKind{o, s}, false},
		{"\x03", []keyKind{c}, false},
		{"\x1b\x1b", []keyKind{c}, false},
		{"\r\x03", []keyKind{s, c}, false},
		{"\x1b[A\x1bOB\x1bb", []keyKind{o}, false},
		{"\x1b[13;2u", []keyKind{o}, false}, // Shift+Enter in CSI-u form: not known yet
		{"\r\x1b[I\x1b[O\x1b[<0;10;5M\x1b[<35;1;1m\x1b[M abc", []keyKind{s, o}, false},
		{"\r\x1b[<0;1", []keyKind{s, o}, false}, // cut short: a key, to be safe
		{"x\x1b", []keyKind{o}, true},
		{"\x1b[", []keyKind{o}, false},
	}
	for _, k := range cases {
		got, esc := claude.keys.kinds([]byte(k.keys))
		if !slices.Equal(got, k.want) || esc != k.esc {
			t.Errorf("%q: %v %v, want %v %v", k.keys, got, esc, k.want, k.esc)
		}
	}
}

func TestKeyLogJoinsEscEsc(t *testing.T) {
	var l keyLog
	l.push(claude.keys, []byte("\x1b"))
	l.push(claude.keys, []byte("\x1b"))
	events, _ := l.take(func() *screen { return nil })
	if len(events) != 2 || events[0].kind != keyOther || events[1].kind != keyClear {
		t.Fatalf("events %+v", events)
	}
	// Esc then an arrow key is not Esc Esc.
	l.push(claude.keys, []byte("\x1b"))
	l.push(claude.keys, []byte("\x1b[A"))
	if events, _ := l.take(func() *screen { return nil }); len(events) != 1 || events[0].kind != keyOther {
		t.Fatalf("events %+v", events)
	}
}

func testRecord(id, agent, cwd string, started time.Time) *record {
	r := newRecord([]string{agent}, cwd)
	r.ID, r.Agent, r.Started = id, agent, started
	return r
}

func TestSentLogFile(t *testing.T) {
	st := testStore(t)
	r := testRecord("s1", "claude", "/work", time.Now())
	r.Draft = "first"
	if err := st.logSent(r); err != nil {
		t.Fatal(err)
	}
	r.Draft, r.Pastes = "second [Pasted text #1 +2 lines]", []string{"a\nb\nc", "placed"}
	r.Draft += " placed"
	if err := st.logSent(r); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(st.sentPath("s1"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("sent log mode %v, err %v", fi, err)
	}
	if di, _ := os.Stat(filepath.Dir(st.sentPath("s1"))); di.Mode().Perm() != 0o700 {
		t.Fatalf("sent/ mode %v", di.Mode())
	}
	l, err := readSent(st.sentPath("s1"))
	if err != nil {
		t.Fatal(err)
	}
	if l.Format != sentFormat || l.Session != "s1" || l.Agent != "claude" || l.Cwd != "/work" || len(l.messages) != 2 {
		t.Fatalf("log %+v", l)
	}
	if m := l.messages[1]; !slices.Equal(m.Pastes, []string{"a\nb\nc"}) || m.Time.IsZero() {
		t.Fatalf("message %+v: only the unplaced paste is kept", m)
	}
	// A line cut short by a crash is skipped, not fatal.
	f, _ := os.OpenFile(st.sentPath("s1"), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"time":"2026-09-27T`)
	f.Close()
	if l, err := readSent(st.sentPath("s1")); err != nil || len(l.messages) != 2 {
		t.Fatalf("after a torn line: %v %v", l, err)
	}
	os.WriteFile(filepath.Join(st.dir, "sent", "junk.jsonl"), []byte("not json\n"), 0o600)
	if logs := st.sentLogs(); len(logs) != 1 {
		t.Fatalf("%d logs, want the junk skipped", len(logs))
	}
}

func TestSentLogTrimsAt5MB(t *testing.T) {
	st := testStore(t)
	r := testRecord("big", "claude", "/work", time.Now())
	for i := range 6 {
		r.Draft = fmt.Sprintf("%d %s", i, strings.Repeat("x", 1<<20))
		if err := st.logSent(r); err != nil {
			t.Fatal(err)
		}
	}
	fi, _ := os.Stat(st.sentPath("big"))
	if fi.Size() > sentMaxBytes {
		t.Fatalf("log is %d bytes", fi.Size())
	}
	l, _ := readSent(st.sentPath("big"))
	if l.Dropped != 2 || len(l.messages) != 4 || !strings.HasPrefix(l.messages[0].Text, "2 ") || !strings.HasPrefix(l.messages[3].Text, "5 ") {
		t.Fatalf("dropped %d, kept %d", l.Dropped, len(l.messages))
	}
	// One message over the cap on its own stays.
	r.ID, r.Draft = "huge", strings.Repeat("y", sentMaxBytes+1)
	st.logSent(r)
	if l, _ := readSent(st.sentPath("huge")); len(l.messages) != 1 {
		t.Fatal("a lone oversized message was dropped")
	}
}

func TestSentLogRetention(t *testing.T) {
	st := testStore(t)
	dir := filepath.Join(st.dir, "sent")
	old := filepath.Join(dir, "old.jsonl")
	os.WriteFile(old, []byte("{}\n"), 0o600)
	os.Chtimes(old, time.Now(), time.Now().Add(-sentMaxAge-time.Hour))
	recent := filepath.Join(dir, "recent.jsonl")
	os.WriteFile(recent, []byte("{}\n"), 0o600)
	os.Chtimes(recent, time.Now(), time.Now().Add(-sentMaxAge+time.Hour))
	for i := range sentLimit + 5 {
		name := filepath.Join(dir, fmt.Sprintf("n%04d.jsonl", i))
		os.WriteFile(name, []byte("{}\n"), 0o600)
		os.Chtimes(name, time.Now(), time.Now().Add(time.Duration(i-sentLimit)*time.Minute))
	}
	st.pruneSent()
	names, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if len(names) != sentLimit {
		t.Fatalf("%d logs kept, want %d", len(names), sentLimit)
	}
	for _, gone := range []string{old, recent, filepath.Join(dir, "n0000.jsonl"), filepath.Join(dir, "n0004.jsonl")} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s kept", filepath.Base(gone))
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "n0005.jsonl")); err != nil {
		t.Error("the oldest log within the cap was deleted")
	}
	// A log younger than the age limit stays when under the cap.
	os.WriteFile(recent, []byte("{}\n"), 0o600)
	os.Chtimes(recent, time.Now(), time.Now().Add(-sentMaxAge+time.Hour))
	os.Remove(filepath.Join(dir, "n2004.jsonl"))
	st.pruneSent()
	if _, err := os.Stat(recent); err != nil {
		t.Error("a log younger than 90 days was deleted")
	}
}

// seedSent writes a sent log for the CLI tests.
func seedSent(t *testing.T, st *store, id, agent, cwd string, minutes int, texts ...string) {
	t.Helper()
	r := testRecord(id, agent, cwd, time.Now().Add(-time.Duration(minutes)*time.Minute))
	for _, text := range texts {
		r.Draft = text
		if err := st.logSent(r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCLILog(t *testing.T) {
	st := testStore(t)
	here := t.TempDir()
	t.Chdir(here)
	seedSent(t, st, "older", "claude", "/elsewhere", 60, "fix the build")
	seedSent(t, st, "newer", "codex", realPath(here), 5, "first message", "second message\nwith a second line")
	code, out, _ := runCLI("log")
	if code != 0 || !strings.Contains(out, "  1  codex ") || !strings.Contains(out, "2 sent  second message\n") ||
		!strings.Contains(out, "  2  claude") || !strings.Contains(out, "1 sent  fix the build") {
		t.Fatalf("log %d:\n%s", code, out)
	}
	if _, out, _ := runCLI("log", "--agent", "claude"); strings.Contains(out, "codex") || !strings.Contains(out, "  2  claude") {
		t.Fatalf("log --agent claude:\n%s", out)
	}
	if _, out, _ := runCLI("log", "--here"); strings.Contains(out, "claude") || !strings.Contains(out, "  1  codex") {
		t.Fatalf("log --here:\n%s", out)
	}
	for _, arg := range []string{"1", "newer"} {
		_, out, _ := runCLI("log", arg)
		if !strings.Contains(out, "codex in ") || !strings.Contains(out, "\n1  ") || !strings.Contains(out, "first message\n") ||
			!strings.Contains(out, "\n2  ") || !strings.Contains(out, "second message\nwith a second line\n") ||
			strings.Index(out, "first") > strings.Index(out, "second message") {
			t.Fatalf("log %s:\n%s", arg, out)
		}
	}
	for _, args := range [][]string{{"log", "3"}, {"log", "nope"}, {"log", "1", "2"}, {"log", "--copy", "1"}, {"log", "1", "--copy", "x"}, {"log", "--bogus"}, {"log", "1", "--copy", "3"}} {
		if code, _, _ := runCLI(args...); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
}

func TestCLILogCopy(t *testing.T) {
	st := testStore(t)
	seedSent(t, st, "s", "claude", "/work", 1, "one", "two\nlines")
	bin := t.TempDir()
	clip := filepath.Join(t.TempDir(), "clip")
	script := "#!/bin/sh\n/bin/cat > " + clip + "\n"
	for _, name := range []string{"pbcopy", "wl-copy", "xclip", "xsel", "clip.exe"} {
		os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755)
	}
	t.Setenv("PATH", bin)
	code, _, errOut := runCLI("log", "1", "--copy", "2")
	if code != 0 || !strings.Contains(errOut, "Copied message 2, 2 lines") {
		t.Fatalf("copy %d %q", code, errOut)
	}
	if b, _ := os.ReadFile(clip); string(b) != "two\nlines" {
		t.Fatalf("clipboard %q", b)
	}
	t.Setenv("PATH", t.TempDir())
	if code, out, errOut := runCLI("log", "s", "--copy", "1"); code != 0 || out != "one\n" || !strings.Contains(errOut, "no clipboard") {
		t.Fatalf("no clipboard: %d %q %q", code, out, errOut)
	}
}

func TestCLILogEmptyAndDropped(t *testing.T) {
	st := testStore(t)
	if _, out, _ := runCLI("log"); !strings.Contains(out, "No sent messages") {
		t.Fatal(out)
	}
	seedSent(t, st, "s", "claude", "/work", 1, "kept")
	l, _ := readSent(st.sentPath("s"))
	l.Dropped = 3
	h, _ := json.Marshal(l.sentHeader)
	m, _ := json.Marshal(l.messages[0])
	os.WriteFile(st.sentPath("s"), []byte(string(h)+"\n"+string(m)+"\n"), 0o600)
	if _, out, _ := runCLI("log", "1"); !strings.Contains(out, "3 earlier messages were dropped") {
		t.Fatal(out)
	}
}

func TestCLIForgetLog(t *testing.T) {
	st := testStore(t)
	seedSent(t, st, "a", "claude", "/work", 2, "keep me")
	seedSent(t, st, "b", "claude", "/work", 1, "forget me")
	code, _, errOut := runCLI("forget", "--log", "1")
	if code != 0 || !strings.Contains(errOut, "Deleted the sent log") {
		t.Fatalf("forget %d %q", code, errOut)
	}
	if logs := st.sentLogs(); len(logs) != 1 || logs[0].Session != "a" {
		t.Fatalf("left %d logs", len(logs))
	}
	if code, _, _ := runCLI("forget", "--log", "a"); code != 0 {
		t.Fatal("forget by id")
	}
	for _, args := range [][]string{{"forget"}, {"forget", "1"}, {"forget", "--log", "1"}} {
		if code, _, _ := runCLI(args...); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
}
