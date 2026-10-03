package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The fake transcripts these tests write hold their turns as a JSON array;
// fakeTurns reads them back, so the engine is tested apart from the
// transcript reader. Every id, folder and text here is made up.

const (
	ctxConv  = "6f1e2d3c-4b5a-4e6f-8a7b-9c0d1e2f3a4b"
	ctxOther = "7a8b9c0d-1e2f-4a3b-8c4d-5e6f7a8b9c0d"
)

func fakeTurns(t *testing.T) {
	t.Helper()
	old := claudeTurns
	claudeTurns = func(r io.Reader) ([]claudeTurn, error) {
		var turns []claudeTurn
		err := json.NewDecoder(r).Decode(&turns)
		return turns, err
	}
	t.Cleanup(func() { claudeTurns = old })
}

// claudeHome points HOME and CLAUDE_CONFIG_DIR at scratch folders, so no
// test reads a real config folder, and returns the config folder.
func claudeHome(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	fakeTurns(t)
	return cfg
}

// writeTurns writes a fake transcript of session in cfg's projects folder.
func writeTurns(t *testing.T, cfg, slug, session string, turns ...claudeTurn) string {
	t.Helper()
	dir := filepath.Join(cfg, "projects", slug)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(turns)
	p := filepath.Join(dir, session+".jsonl")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// ctxBase is a recent whole second, so the turns are inside sentMaxAge.
func ctxBase() time.Time {
	return time.Now().Add(-48 * time.Hour).Truncate(time.Second)
}

func readMessages(t *testing.T, path string) []sentMessage {
	t.Helper()
	l, err := readSent(path)
	if err != nil {
		t.Fatal(err)
	}
	return l.messages
}

// A message seen live gets its turn's uuid, kind, asked and reply_to by
// its text, and keeps its gloss and the fields a later build wrote; the
// answer and the message typed while the agent worked are added, in time
// order; a second pass changes not one byte.
func TestContextMatchesLiveMessages(t *testing.T) {
	cfg := claudeHome(t)
	st := testStore(t)
	t0 := ctxBase()
	r := testRecord("20261001-100000-1", "claude", "/work/app", t0)
	r.AgentSession = ctxConv
	for i, c := range []struct {
		text string
		at   time.Duration
	}{
		{"fix the parser\n\tplease", 0},
		{"yes", time.Minute},
		{"yes", 10 * time.Minute},
	} {
		r.Draft = c.text
		if err := st.logSentAt(r, t0.Add(c.at)); err != nil {
			t.Fatal(i, err)
		}
	}
	path := st.claudeLogPath(ctxConv)
	if err := setGloss(path, messageKey{n: 1}, "the parser bug from yesterday", false); err != nil {
		t.Fatal(err)
	}
	// A later build's field on the third message.
	data, _ := os.ReadFile(path)
	data = bytes.Replace(data, []byte(`"text":"yes"}`+"\n"), []byte(`"text":"yes","future":{"a":1}}`+"\n"), 2)
	data = bytes.Replace(data, []byte(`"text":"yes","future":{"a":1}}`+"\n"), []byte(`"text":"yes"}`+"\n"), 1)
	os.WriteFile(path, data, 0o600)

	writeTurns(t, cfg, "-work-app", ctxConv,
		claudeTurn{UUID: "u-1", Kind: "typed", Time: t0.Add(-2 * time.Second), Text: "fix the parser\n    please ", Asked: "I read the code. Shall I fix the parser?", ReplyTo: true, Cwd: "/work/app", SessionID: ctxConv},
		claudeTurn{UUID: "u-4", Kind: "absorbed", Time: t0.Add(3 * time.Minute), Text: "also the tests", Cwd: "/work/app", SessionID: ctxConv},
		claudeTurn{UUID: "u-3", Kind: "answer", Time: t0.Add(5 * time.Minute), Text: "Which file? → main.go", Asked: "Which file?", ReplyTo: true, Cwd: "/work/app", SessionID: ctxConv},
		claudeTurn{UUID: "u-2", Kind: "typed", Time: t0.Add(9 * time.Minute), Text: "yes", Asked: "Done. Commit it?", ReplyTo: true, Cwd: "/work/app", SessionID: ctxConv},
	)
	code, out, errOut := runCLI("context", "--config-dir", cfg)
	if code != 0 || out != "1 logs updated, 2 messages added, 2 matched\n" {
		t.Fatalf("exit %d, %q, %q", code, out, errOut)
	}
	ms := readMessages(t, path)
	var got []string
	for _, m := range ms {
		got = append(got, m.UUID+"|"+m.Kind+"|"+m.Source+"|"+m.Text)
	}
	want := []string{
		"u-1|typed||fix the parser\n\tplease",
		"|||yes",
		"u-4|absorbed|transcript|also the tests",
		"u-3|answer|transcript|Which file? → main.go",
		"u-2|typed||yes",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("messages\n%q\nwant\n%q", got, want)
	}
	if m := ms[0]; m.Gloss != "the parser bug from yesterday" || m.Asked != "I read the code. Shall I fix the parser?" || !m.ReplyTo || !m.Time.Equal(t0) {
		t.Fatalf("matched message %+v", m)
	}
	if m := ms[4]; m.Asked != "Done. Commit it?" || !m.ReplyTo || !m.Time.Equal(t0.Add(10*time.Minute)) {
		t.Fatalf("the later yes, nearest the turn, %+v", m)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Contains(after, []byte(`"future":{"a":1}`)) {
		t.Fatalf("a later build's field was lost:\n%s", after)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 || fi.ModTime().Unix() != t0.Add(10*time.Minute).Unix() {
		t.Fatalf("mode %v, time %v, want 0600 and the last message's", fi.Mode(), fi.ModTime())
	}
	code, out, _ = runCLI("context", "--config-dir", cfg)
	again, _ := os.ReadFile(path)
	if code != 0 || out != "0 logs updated, 0 messages added, 0 matched\n" || !bytes.Equal(again, after) {
		t.Fatalf("second pass: exit %d, %q, file changed %v:\n%s", code, out, !bytes.Equal(again, after), again)
	}
	// --session by the list number and --json.
	code, out, _ = runCLI("context", "--config-dir", cfg, "--session", "1", "--json")
	var res contextResult
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res != (contextResult{}) {
		t.Fatalf("--session 1 --json: exit %d, %q", code, out)
	}
}

// A message the log already holds by uuid gets reply_to true when its turn
// now reads as a reply, as after a build that reads more turns as replies,
// and keeps its gloss and every other field; a second pass changes nothing,
// and a turn that no longer reads as a reply never turns the flag off.
func TestContextRefreshesReplyTo(t *testing.T) {
	cfg := claudeHome(t)
	st := testStore(t)
	t0 := ctxBase()
	r := testRecord("20261001-100000-r", "claude", "/w", t0)
	r.AgentSession = ctxConv
	for i, text := range []string{"Delete which copies exactly?", "and the docs"} {
		r.Draft = text
		st.logSentAt(r, t0.Add(time.Duration(i)*time.Minute))
	}
	asked := "Done. Shall I delete the copies, or keep them on disk? My default is to keep them."
	writeTurns(t, cfg, "-w", ctxConv,
		claudeTurn{UUID: "r-1", Kind: "typed", Time: t0, Text: "Delete which copies exactly?", Asked: asked, SessionID: ctxConv},
		claudeTurn{UUID: "r-2", Kind: "typed", Time: t0.Add(time.Minute), Text: "and the docs", Asked: "Is that all?", ReplyTo: true, SessionID: ctxConv},
	)
	if code, out, errOut := runCLI("context", "--config-dir", cfg); code != 0 || out != "1 logs updated, 0 messages added, 2 matched\n" {
		t.Fatalf("first pass: exit %d, %q, %q", code, out, errOut)
	}
	path := st.claudeLogPath(ctxConv)
	if err := setGloss(path, messageKey{uuid: "r-1"}, "Asked whether to delete the copies; asked which ones", false); err != nil {
		t.Fatal(err)
	}
	if ms := readMessages(t, path); ms[0].ReplyTo || !ms[1].ReplyTo {
		t.Fatalf("before the refresh %+v", ms)
	}

	// The same turns, now read as replies by a later build; r-2's turn no
	// longer is one.
	writeTurns(t, cfg, "-w", ctxConv,
		claudeTurn{UUID: "r-1", Kind: "typed", Time: t0, Text: "Delete which copies exactly?", Asked: asked, ReplyTo: true, SessionID: ctxConv},
		claudeTurn{UUID: "r-2", Kind: "typed", Time: t0.Add(time.Minute), Text: "and the docs", Asked: "Is that all?", SessionID: ctxConv},
	)
	if code, out, errOut := runCLI("context", "--config-dir", cfg); code != 0 || out != "1 logs updated, 0 messages added, 1 matched\n" {
		t.Fatalf("refresh: exit %d, %q, %q", code, out, errOut)
	}
	ms := readMessages(t, path)
	if m := ms[0]; !m.ReplyTo || m.Gloss != "Asked whether to delete the copies; asked which ones" || m.Asked != asked || m.Kind != "typed" || m.Text != "Delete which copies exactly?" || m.Source != "" {
		t.Fatalf("refreshed message %+v", m)
	}
	if !ms[1].ReplyTo {
		t.Fatalf("reply_to went back to false: %+v", ms[1])
	}
	after, _ := os.ReadFile(path)
	code, out, _ := runCLI("context", "--config-dir", cfg)
	if again, _ := os.ReadFile(path); code != 0 || out != "0 logs updated, 0 messages added, 0 matched\n" || !bytes.Equal(again, after) {
		t.Fatalf("second pass: exit %d, %q, file changed %v", code, out, !bytes.Equal(again, after))
	}
}

// context add --reply sets reply_to with the gloss, even when the gloss is
// the one the line already holds; without it reply_to is left as it is; the
// same gloss again, with or without --reply once the flag is set, writes
// nothing.
func TestContextAddReply(t *testing.T) {
	claudeHome(t)
	st := testStore(t)
	t0 := ctxBase()
	r := testRecord("20261001-100000-p", "claude", "/w", t0)
	r.AgentSession = ctxConv
	for i, text := range []string{"one", "two"} {
		r.Draft = text
		st.logSentAt(r, t0.Add(time.Duration(i)*time.Minute))
	}
	path := st.claudeLogPath(ctxConv)
	if code, _, errOut := runCLI("context", "add", ctxConv, "1", "--reply", "--gloss", "chose one"); code != 0 {
		t.Fatalf("--reply: exit %d, %q", code, errOut)
	}
	if code, _, errOut := runCLI("context", "add", ctxConv, "2", "--gloss", "a note"); code != 0 {
		t.Fatalf("no --reply: exit %d, %q", code, errOut)
	}
	ms := readMessages(t, path)
	if !ms[0].ReplyTo || ms[0].Gloss != "chose one" || ms[1].ReplyTo || ms[1].Gloss != "a note" {
		t.Fatalf("messages %+v", ms)
	}
	// Without --reply a set flag stays set.
	if code, _, _ := runCLI("context", "add", ctxConv, "1", "--gloss", "chose one, again"); code != 0 {
		t.Fatal("re-gloss failed")
	}
	if ms := readMessages(t, path); !ms[0].ReplyTo || ms[0].Gloss != "chose one, again" {
		t.Fatalf("after a gloss without --reply %+v", ms[0])
	}
	// The same gloss with --reply on a line not yet marked sets the flag.
	if code, _, errOut := runCLI("context", "add", ctxConv, "2", "--gloss", "a note", "--reply"); code != 0 {
		t.Fatalf("same gloss with --reply: exit %d, %q", code, errOut)
	}
	if ms := readMessages(t, path); !ms[1].ReplyTo || ms[1].Gloss != "a note" {
		t.Fatalf("after the same gloss with --reply %+v", ms[1])
	}
	stamp := t0.Add(-time.Hour)
	os.Chtimes(path, stamp, stamp)
	before, _ := os.ReadFile(path)
	for _, args := range [][]string{
		{"context", "add", ctxConv, "1", "--gloss", "chose one, again", "--reply"},
		{"context", "add", ctxConv, "1", "--gloss", "chose one, again"},
		{"context", "add", ctxConv, "2", "--gloss", "a note"},
	} {
		if code, _, errOut := runCLI(args...); code != 0 {
			t.Fatalf("%q: exit %d, %q", args, code, errOut)
		}
	}
	after, _ := os.ReadFile(path)
	if fi, _ := os.Stat(path); !bytes.Equal(before, after) || fi.ModTime().Unix() != stamp.Unix() {
		t.Fatalf("a no-op rewrote the log:\n%s", after)
	}
}

// Turns pair with logged messages nearest in time first, across the whole
// log, and never more than contextMatchWindow apart: an old turn with the
// same short text as a live message, read first, does not take it from the
// turn sent with it, and is added; a turn hours from the only message with
// its text is added, not matched.
func TestContextPairsNearestFirst(t *testing.T) {
	cfg := claudeHome(t)
	st := testStore(t)
	t0 := ctxBase()
	r := testRecord("20261001-100000-6", "claude", "/w", t0)
	r.AgentSession = ctxConv
	for i, text := range []string{"yes", "later", "ok"} {
		r.Draft = text
		if err := st.logSentAt(r, t0.Add(time.Duration(i)*10*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	writeTurns(t, cfg, "-w", ctxConv,
		claudeTurn{UUID: "old-yes", Kind: "typed", Time: t0.Add(-72 * time.Hour), Text: "yes", Asked: "Last week?", ReplyTo: true, SessionID: ctxConv},
		claudeTurn{UUID: "new-yes", Kind: "typed", Time: t0.Add(time.Second), Text: "yes", Asked: "Now?", ReplyTo: true, SessionID: ctxConv},
		claudeTurn{UUID: "far-later", Kind: "typed", Time: t0.Add(3 * time.Hour), Text: "later", SessionID: ctxConv},
		// Both inside the window: the nearer one takes the message.
		claudeTurn{UUID: "early-ok", Kind: "typed", Time: t0.Add(-10 * time.Minute), Text: "ok", SessionID: ctxConv},
		claudeTurn{UUID: "live-ok", Kind: "typed", Time: t0.Add(20*time.Minute + 2*time.Second), Text: "ok", SessionID: ctxConv},
	)
	code, out, errOut := runCLI("context", "--config-dir", cfg)
	if code != 0 || out != "1 logs updated, 3 messages added, 2 matched\n" {
		t.Fatalf("exit %d, %q, %q", code, out, errOut)
	}
	var got []string
	for _, m := range readMessages(t, st.claudeLogPath(ctxConv)) {
		got = append(got, m.UUID+"|"+m.Source+"|"+m.Text+"|"+m.Asked)
	}
	want := []string{
		"old-yes|transcript|yes|Last week?",
		"early-ok|transcript|ok|",
		"new-yes||yes|Now?",
		"||later|",
		"live-ok||ok|",
		"far-later|transcript|later|",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("messages\n%q\nwant\n%q", got, want)
	}
}

// A logged message is one message with its turn when the transcript wraps
// a paste in <pasted_content> tags, and when the log still shows a paste as
// a placeholder with the paste beside it, in any order: matched, never
// added a second time.
func TestContextMatchesPastes(t *testing.T) {
	cfg := claudeHome(t)
	st := testStore(t)
	t0 := ctxBase()
	r := testRecord("20261001-100000-7", "claude", "/w", t0)
	r.AgentSession = ctxConv
	for i, c := range []struct {
		draft  string
		pastes []string
	}{
		{"look at\nline one\nline two", nil},
		{"check [Pasted text #1 +2 lines] now", []string{"a\nb\nc"}},
		{"x [Pasted text #2 +1 line] y [Pasted text #1 +1 line]", []string{"p one\nq", "p two\nr"}},
	} {
		r.Draft, r.Pastes = c.draft, c.pastes
		if err := st.logSentAt(r, t0.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	writeTurns(t, cfg, "-w", ctxConv,
		claudeTurn{UUID: "p-1", Kind: "typed", Time: t0, Text: "look at\n<pasted_content id=\"1\">line one\nline two</pasted_content>", SessionID: ctxConv},
		claudeTurn{UUID: "p-2", Kind: "typed", Time: t0.Add(time.Minute), Text: "check a\nb\nc now", SessionID: ctxConv},
		claudeTurn{UUID: "p-3", Kind: "typed", Time: t0.Add(2 * time.Minute), Text: "x p two\nr y p one\nq", SessionID: ctxConv},
	)
	code, out, errOut := runCLI("context", "--config-dir", cfg)
	if code != 0 || out != "1 logs updated, 0 messages added, 3 matched\n" {
		t.Fatalf("exit %d, %q, %q", code, out, errOut)
	}
	ms := readMessages(t, st.claudeLogPath(ctxConv))
	if len(ms) != 3 || ms[0].UUID != "p-1" || ms[1].UUID != "p-2" || ms[2].UUID != "p-3" || len(ms[1].Pastes) != 1 {
		t.Fatalf("messages %+v", ms)
	}
}

// A conversation with no sent log gets none from unsent context, and a
// transcript named on the command line is read even outside the config
// folders.
func TestContextOnlyExistingLogsAndAnExplicitTranscript(t *testing.T) {
	cfg := claudeHome(t)
	st := testStore(t)
	t0 := ctxBase()
	writeTurns(t, cfg, "-w", ctxOther, claudeTurn{UUID: "o-1", Kind: "typed", Time: t0, Text: "elsewhere", SessionID: ctxOther})
	if code, out, _ := runCLI("context", "--config-dir", cfg); code != 0 || out != "0 logs updated, 0 messages added, 0 matched\n" || exists(st.claudeLogPath(ctxOther)) {
		t.Fatalf("exit %d, %q, log made %v", code, out, exists(st.claudeLogPath(ctxOther)))
	}
	r := testRecord("20261001-100000-2", "claude", "/w", t0)
	r.AgentSession, r.Draft = ctxConv, "hello"
	st.logSentAt(r, t0)
	p := writeTurns(t, t.TempDir(), "-w", ctxConv, claudeTurn{UUID: "c-1", Kind: "typed", Time: t0.Add(time.Second), Text: "hello", SessionID: ctxConv})
	if code, out, errOut := runCLI("context", "--transcript", p); code != 0 || out != "1 logs updated, 0 messages added, 1 matched\n" {
		t.Fatalf("--transcript: exit %d, %q, %q", code, out, errOut)
	}
	if code, _, _ := runCLI("context", "--transcript", filepath.Join(t.TempDir(), "none.jsonl")); code != 2 {
		t.Fatalf("a missing transcript: exit %d", code)
	}
	if code, _, _ := runCLI("context", "--bogus"); code != 2 {
		t.Fatalf("an unknown flag: exit %d", code)
	}
}

// context --session exits 2 for a conversation it cannot find (a number
// past the list, a typo, another agent's log) and for a Claude Code
// conversation that has a transcript but no sent log yet, naming the
// command that makes one; it writes nothing either way.
func TestContextSessionNotFound(t *testing.T) {
	cfg := claudeHome(t)
	st := testStore(t)
	t0 := ctxBase()
	r := testRecord("20261001-100000-8", "codex", "/w", t0)
	r.Draft = "a codex message"
	st.logSentAt(r, t0)
	writeTurns(t, cfg, "-w", ctxOther, claudeTurn{UUID: "n-1", Kind: "typed", Time: t0, Text: "no log", SessionID: ctxOther})
	for _, c := range []struct{ session, want string }{
		{"99", `unsent: no Claude Code conversation "99"; see unsent log` + "\n"},
		{"0", `unsent: no Claude Code conversation "0"; see unsent log` + "\n"},
		{"not-a-real-session", `unsent: no Claude Code conversation "not-a-real-session"; see unsent log` + "\n"},
		{"x y", `unsent: no Claude Code conversation "x y"; see unsent log` + "\n"},
		{"1", `unsent: no Claude Code conversation "1"; see unsent log` + "\n"},
		{ctxOther, `unsent: no sent log for "` + ctxOther + `"; unsent import claude makes one` + "\n"},
	} {
		code, out, errOut := runCLI("context", "--config-dir", cfg, "--session", c.session)
		if code != 2 || out != "" || errOut != c.want {
			t.Errorf("--session %q: exit %d, %q, %q", c.session, code, out, errOut)
		}
	}
	if exists(st.claudeLogPath(ctxOther)) {
		t.Fatal("a log was made")
	}
}

// A send that lands while unsent context rewrites the log is kept: both
// take the log's lock, so the send waits for the rewrite and appends to
// the new file. Without the lock it appends to the file the rewrite then
// replaces, and is lost.
func TestContextKeepsAConcurrentSend(t *testing.T) {
	cfg := claudeHome(t)
	st := testStore(t)
	t0 := ctxBase()
	r := testRecord("20261001-100000-3", "claude", "/w", t0)
	r.AgentSession, r.Draft = ctxConv, "first"
	if err := st.logSentAt(r, t0); err != nil {
		t.Fatal(err)
	}
	writeTurns(t, cfg, "-w", ctxConv,
		claudeTurn{UUID: "k-1", Kind: "typed", Time: t0, Text: "first", SessionID: ctxConv},
		claudeTurn{UUID: "k-2", Kind: "answer", Time: t0.Add(time.Minute), Text: "Q → A", SessionID: ctxConv})
	done := make(chan error, 1)
	mergeHook = func(string) {
		go func() {
			r2 := *r
			r2.Draft = "sent during the rewrite"
			done <- st.logSentAt(&r2, t0.Add(2*time.Minute))
		}()
		select {
		case err := <-done:
			done <- err // the send did not wait
		case <-time.After(300 * time.Millisecond):
		}
	}
	t.Cleanup(func() { mergeHook = nil })
	if code, out, errOut := runCLI("context", "--config-dir", cfg); code != 0 {
		t.Fatalf("exit %d, %q, %q", code, out, errOut)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, m := range readMessages(t, st.claudeLogPath(ctxConv)) {
		texts = append(texts, m.Text)
	}
	if !slices.Equal(texts, []string{"first", "Q → A", "sent during the rewrite"}) {
		t.Fatalf("messages %q", texts)
	}
}

// forget --log takes the log's lock: run while a context pass rewrites the
// log, it waits for the rewrite and the log stays gone. Without the lock the
// rewrite's rename puts the whole log back after forget said it was
// deleted.
func TestForgetLogWaitsForAContextPass(t *testing.T) {
	cfg := claudeHome(t)
	st := testStore(t)
	t0 := ctxBase()
	r := testRecord("20261001-100000-b", "claude", "/w", t0)
	r.AgentSession, r.Draft = ctxConv, "forget me"
	if err := st.logSentAt(r, t0); err != nil {
		t.Fatal(err)
	}
	writeTurns(t, cfg, "-w", ctxConv,
		claudeTurn{UUID: "f-1", Kind: "typed", Time: t0, Text: "forget me", SessionID: ctxConv},
		claudeTurn{UUID: "f-2", Kind: "answer", Time: t0.Add(time.Minute), Text: "Q → A", SessionID: ctxConv})
	done := make(chan int, 1)
	mergeHook = func(string) {
		go func() {
			code, _, _ := runCLI("forget", "--log", ctxConv)
			done <- code
		}()
		select {
		case code := <-done:
			done <- code // forget did not wait
		case <-time.After(300 * time.Millisecond):
		}
	}
	t.Cleanup(func() { mergeHook = nil })
	if code, out, errOut := runCLI("context", "--config-dir", cfg); code != 0 {
		t.Fatalf("exit %d, %q, %q", code, out, errOut)
	}
	if code := <-done; code != 0 {
		t.Fatalf("forget: exit %d", code)
	}
	if exists(st.claudeLogPath(ctxConv)) {
		t.Fatal("the forgotten log came back")
	}
}

// import makes a log for a transcript that has none, with the header a live
// run would write, from the turns inside sentMaxAge only, and never reads
// the folders beside a transcript.
func TestImportClaudeCreatesLogs(t *testing.T) {
	cfg := claudeHome(t)
	st := testStore(t)
	t0 := ctxBase()
	old := time.Now().Add(-sentMaxAge - 24*time.Hour)
	writeTurns(t, cfg, "-work-a", ctxConv,
		claudeTurn{UUID: "a-0", Kind: "typed", Time: old, Text: "too old", Cwd: "/work/a", SessionID: ctxConv},
		claudeTurn{UUID: "a-1", Kind: "typed", Time: t0, Text: "start here", Cwd: "/work/a", SessionID: ctxConv},
		claudeTurn{UUID: "a-2", Kind: "slash", Time: t0.Add(time.Minute), Text: "/review now", Asked: "Ready?", ReplyTo: true, Cwd: "/work/a", SessionID: ctxConv},
		claudeTurn{UUID: "a-2", Kind: "typed", Time: t0.Add(time.Hour), Text: "a copy of a-2", Cwd: "/work/a", SessionID: ctxConv},
	)
	writeTurns(t, cfg, "-work-b", ctxOther, claudeTurn{UUID: "b-0", Kind: "typed", Time: old, Text: "only old", Cwd: "/work/b", SessionID: ctxOther})
	// A subagent's transcript, one folder deeper: never read.
	sub := filepath.Join(cfg, "projects", "-work-a", ctxConv, "subagents")
	os.MkdirAll(sub, 0o700)
	b, _ := json.Marshal([]claudeTurn{{UUID: "s-1", Kind: "typed", Time: t0, Text: "subagent prompt", SessionID: ctxConv}})
	os.WriteFile(filepath.Join(sub, ctxConv+".jsonl"), b, 0o600)
	code, out, errOut := runCLI("import", "claude", "--config-dir", cfg, "--json")
	var res contextResult
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res != (contextResult{Logs: 1, Added: 2, Skipped: 2}) {
		t.Fatalf("exit %d, %q, %q", code, out, errOut)
	}
	path := st.claudeLogPath(ctxConv)
	l, err := readSent(path)
	if err != nil {
		t.Fatal(err)
	}
	h := l.sentHeader
	if h.Format != sentFormat || h.Session != "claude-"+ctxConv || h.Agent != "claude" || h.AgentSession != ctxConv || h.Cwd != "/work/a" || !h.Started.Equal(t0) || len(l.resumes) != 0 {
		t.Fatalf("header %+v, resumes %v", h, l.resumes)
	}
	if len(l.messages) != 2 || l.messages[0].Text != "start here" || l.messages[1].Kind != "slash" || l.messages[1].Source != "transcript" || l.messages[1].Asked != "Ready?" {
		t.Fatalf("messages %+v", l.messages)
	}
	if exists(st.claudeLogPath(ctxOther)) {
		t.Fatal("a conversation with only old turns got a log")
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 || fi.ModTime().Unix() != t0.Add(time.Minute).Unix() {
		t.Fatalf("mode %v, time %v", fi.Mode(), fi.ModTime())
	}
	// unsent log lists it, and a second import changes nothing.
	if _, list, _ := runCLI("log"); !strings.Contains(list, "claude-"+ctxConv) {
		t.Fatalf("unsent log: %q", list)
	}
	before, _ := os.ReadFile(path)
	code, out, _ = runCLI("import", "claude", "--config-dir", cfg)
	if after, _ := os.ReadFile(path); code != 0 || out != "0 logs updated, 0 messages added, 0 matched\n" || !bytes.Equal(before, after) {
		t.Fatalf("second import: exit %d, %q", code, out)
	}
}

// A forked conversation's transcript repeats the turns before the fork
// under its own session id. Each conversation's log gets them, whichever
// file sorts first, and a pass over the fork's transcript alone then finds
// nothing new.
func TestImportClaudeForkedConversation(t *testing.T) {
	cfg := claudeHome(t)
	st := testStore(t)
	t0 := ctxBase()
	// The fork sorts first, so a dedupe across files gave it every shared
	// turn and left the conversation it came from with no log.
	fork, parent := ctxConv, ctxOther
	writeTurns(t, cfg, "-work-a", parent,
		claudeTurn{UUID: "p-1", Kind: "typed", Time: t0, Text: "before the fork", Cwd: "/work/a", SessionID: parent},
		claudeTurn{UUID: "p-2", Kind: "typed", Time: t0.Add(2 * time.Minute), Text: "after, in the first", Cwd: "/work/a", SessionID: parent},
	)
	forkPath := writeTurns(t, cfg, "-work-a", fork,
		claudeTurn{UUID: "p-1", Kind: "typed", Time: t0, Text: "before the fork", Cwd: "/work/a", SessionID: fork},
		claudeTurn{UUID: "f-1", Kind: "typed", Time: t0.Add(time.Minute), Text: "after, in the fork", Cwd: "/work/a", SessionID: fork},
	)
	if code, out, errOut := runCLI("import", "claude", "--config-dir", cfg); code != 0 {
		t.Fatalf("exit %d, %q, %q", code, out, errOut)
	}
	texts := func(id string) []string {
		var out []string
		for _, m := range readMessages(t, st.claudeLogPath(id)) {
			out = append(out, m.Text)
		}
		return out
	}
	if got := texts(parent); !slices.Equal(got, []string{"before the fork", "after, in the first"}) {
		t.Fatalf("the first conversation's log: %q", got)
	}
	if got := texts(fork); !slices.Equal(got, []string{"before the fork", "after, in the fork"}) {
		t.Fatalf("the fork's log: %q", got)
	}
	before, _ := os.ReadFile(st.claudeLogPath(fork))
	if code, out, _ := runCLI("context", "--transcript", forkPath); code != 0 || !strings.HasPrefix(out, "0 logs updated") {
		t.Fatalf("a pass over the fork: exit %d, %q", code, out)
	}
	if after, _ := os.ReadFile(st.claudeLogPath(fork)); !bytes.Equal(before, after) {
		t.Fatal("a pass over the fork's transcript changed its log")
	}
}

// Under on-send delete for Claude Code, import and context keep nothing.
func TestImportClaudeRefusesUnderDelete(t *testing.T) {
	cfg := claudeHome(t)
	st := testStore(t)
	writeTurns(t, cfg, "-w", ctxConv, claudeTurn{UUID: "d-1", Kind: "typed", Time: ctxBase(), Text: "secret", SessionID: ctxConv})
	t.Setenv("UNSENT_ON_SEND_CLAUDE", "delete")
	code, out, errOut := runCLI("import", "claude", "--config-dir", cfg)
	if code != 2 || out != "" || errOut != "unsent: on send is delete for claude, nothing imported\n" {
		t.Fatalf("exit %d, %q, %q", code, out, errOut)
	}
	if code, _, _ := runCLI("context", "--config-dir", cfg); code != 2 {
		t.Fatalf("context under delete: exit %d", code)
	}
	if names, _ := filepath.Glob(filepath.Join(st.dir, "sent", "*")); len(names) != 0 {
		t.Fatalf("sent/ holds %v", names)
	}
	if code, _, _ := runCLI("import", "codex"); code != 2 {
		t.Fatalf("import codex: exit %d", code)
	}
}

// historyLine is one line of a synthetic history.jsonl.
func historyLine(session, project, display string, at time.Time, pastes map[string]any) string {
	b, _ := json.Marshal(map[string]any{
		"display": display, "pastedContents": pastes, "project": project,
		"sessionId": session, "timestamp": at.UnixMilli(),
	})
	return string(b) + "\n"
}

// --history makes a log from history.jsonl for a conversation whose
// transcript is gone, with pastes put back where history kept them, once
// although two config folders list it; a conversation with a transcript
// takes nothing from history.
func TestImportClaudeHistory(t *testing.T) {
	cfg := claudeHome(t)
	cfg2 := t.TempDir()
	st := testStore(t)
	t0 := ctxBase()
	const gone = "8b9c0d1e-2f3a-4b4c-9d5e-6f7a8b9c0d1e"
	writeTurns(t, cfg, "-w", ctxConv, claudeTurn{UUID: "t-1", Kind: "typed", Time: t0, Text: "from the transcript", SessionID: ctxConv})
	hist := historyLine(gone, "/work/old", "look at [Pasted text #1 +3 lines] and [Pasted text #2 +9 lines]", t0, map[string]any{
		"1": map[string]any{"id": 1, "type": "text", "content": "a\nb\nc\nd"},
		"2": map[string]any{"id": 2, "type": "text", "contentHash": "abc123"},
	}) + historyLine(gone, "/work/old", "/clear", t0.Add(time.Minute), nil) +
		historyLine(gone, "/work/old", "an old one", time.Now().Add(-sentMaxAge-time.Hour), nil) +
		historyLine(ctxConv, "/w", "from history, not taken", t0, nil) +
		historyLine("", "/w", "no session", t0, nil)
	os.WriteFile(filepath.Join(cfg, "history.jsonl"), []byte(hist), 0o600)
	os.WriteFile(filepath.Join(cfg2, "history.jsonl"), []byte(hist+`{"display":"torn`), 0o600)
	code, out, errOut := runCLI("import", "claude", "--config-dir", cfg2, "--history", "--json")
	var res contextResult
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res != (contextResult{Logs: 2, Added: 3, Skipped: 1}) {
		t.Fatalf("exit %d, %q, %q", code, out, errOut)
	}
	l, err := readSent(st.claudeLogPath(gone))
	if err != nil {
		t.Fatal(err)
	}
	if l.Cwd != "/work/old" || l.AgentSession != gone || !l.Started.Equal(t0) || len(l.messages) != 2 {
		t.Fatalf("log %+v %+v", l.sentHeader, l.messages)
	}
	if m := l.messages[0]; m.Text != "look at a\nb\nc\nd and [Pasted text #2 +9 lines]" || m.Kind != "typed" || m.Source != "history" || m.Asked != "" || m.UUID != "" {
		t.Fatalf("message %+v", m)
	}
	var texts []string
	for _, m := range readMessages(t, st.claudeLogPath(ctxConv)) {
		texts = append(texts, m.Text)
	}
	if !slices.Equal(texts, []string{"from the transcript"}) {
		t.Fatalf("the conversation with a transcript took %q", texts)
	}
	before, _ := os.ReadFile(st.claudeLogPath(gone))
	code, out, _ = runCLI("import", "claude", "--config-dir", cfg2, "--history")
	if after, _ := os.ReadFile(st.claudeLogPath(gone)); code != 0 || out != "0 logs updated, 0 messages added, 0 matched\n" || !bytes.Equal(before, after) {
		t.Fatalf("second import: exit %d, %q", code, out)
	}
}

// context add sets a message's gloss by the list number, unsent's id or
// the agent's session id, from an argument or stdin, as one line, and keeps
// the log's time; a session or message that is not there exits 2.
func TestContextAdd(t *testing.T) {
	claudeHome(t)
	st := testStore(t)
	t0 := ctxBase()
	r := testRecord("20261001-100000-4", "claude", "/w", t0)
	r.AgentSession = ctxConv
	for i, text := range []string{"one", "two"} {
		r.Draft = text
		st.logSentAt(r, t0.Add(time.Duration(i)*time.Minute))
	}
	path := st.claudeLogPath(ctxConv)
	stamp := t0.Add(-time.Hour)
	os.Chtimes(path, stamp, stamp)
	for _, c := range []struct{ session, n, gloss, want string }{
		{"1", "1", "by number", "by number"},
		{r.ID, "2", "  by unsent's\nid ", "by unsent's id"},
		{ctxConv, "1", "-", "from stdin"},
	} {
		var out, errb bytes.Buffer
		code := cmdContext([]string{"add", c.session, c.n, "--gloss", c.gloss}, strings.NewReader("from\nstdin\n"), &out, &errb)
		if code != 0 {
			t.Fatalf("%v: exit %d, %q", c, code, errb.String())
		}
		ms := readMessages(t, path)
		if i := c.n[0] - '1'; ms[i].Gloss != c.want {
			t.Fatalf("%v: gloss %q", c, ms[i].Gloss)
		}
	}
	if fi, _ := os.Stat(path); fi.ModTime().Unix() != stamp.Unix() {
		t.Fatalf("the log's time moved to %v", fi.ModTime())
	}
	for _, args := range [][]string{
		{"context", "add", "nope", "1", "--gloss", "x"},
		{"context", "add", ctxConv, "3", "--gloss", "x"},
		{"context", "add", ctxConv, "1"},
		{"context", "add", ctxConv, "zero", "--gloss", "x"},
	} {
		if code, _, _ := runCLI(args...); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
}

// context add names a message by its uuid too, which a pass that adds an
// earlier turn does not move, while the number does; a uuid the log does
// not hold exits 2.
func TestContextAddByUUID(t *testing.T) {
	cfg := claudeHome(t)
	st := testStore(t)
	t0 := ctxBase()
	r := testRecord("20261001-100000-a", "claude", "/w", t0)
	r.AgentSession = ctxConv
	for i, text := range []string{"one", "two"} {
		r.Draft = text
		st.logSentAt(r, t0.Add(time.Duration(i)*2*time.Minute))
	}
	writeTurns(t, cfg, "-w", ctxConv,
		claudeTurn{UUID: "g-1", Kind: "typed", Time: t0, Text: "one", SessionID: ctxConv},
		claudeTurn{UUID: "g-mid", Kind: "absorbed", Time: t0.Add(time.Minute), Text: "in between", SessionID: ctxConv},
		claudeTurn{UUID: "g-2", Kind: "typed", Time: t0.Add(2 * time.Minute), Text: "two", Asked: "Which?", ReplyTo: true, SessionID: ctxConv},
	)
	if code, out, errOut := runCLI("context", "--config-dir", cfg); code != 0 || out != "1 logs updated, 1 messages added, 2 matched\n" {
		t.Fatalf("exit %d, %q, %q", code, out, errOut)
	}
	// "two" was message 2 before the pass and is 3 now.
	if code, _, errOut := runCLI("context", "add", ctxConv, "g-2", "--gloss", "chose two"); code != 0 {
		t.Fatalf("by uuid: exit %d, %q", code, errOut)
	}
	ms := readMessages(t, st.claudeLogPath(ctxConv))
	if ms[2].Text != "two" || ms[2].Gloss != "chose two" || ms[1].Gloss != "" {
		t.Fatalf("messages %+v", ms)
	}
	if code, _, errOut := runCLI("context", "add", ctxConv, "g-none", "--gloss", "x"); code != 2 || errOut != "unsent: that session has no message g-none\n" {
		t.Fatalf("an unknown uuid: exit %d, %q", code, errOut)
	}
}

// The answering line is the last line of asked with text, cut to 200
// characters, never inside one.
func TestAnsweringLine(t *testing.T) {
	long := strings.Repeat("é", answeringMax+20)
	for _, c := range []struct{ asked, want string }{
		{"First.\nSecond?", "Second?"},
		{"Which one?\n\n \t\n", "Which one?"},
		{"  only line  ", "only line"},
		{"\n\n", ""},
		{"head\n" + long, strings.Repeat("é", answeringMax)},
		{"Shall I run it?\n```\ncurl x?y\n```", "Shall I run it?"},
		{"Run this?\n```sh\na\n\nb\n```\n", "Run this?"},
		{"```\nonly a block\n```", ""},
	} {
		if got := answeringLine(c.asked); got != c.want {
			t.Errorf("answeringLine(%q) = %q, want %q", c.asked, got, c.want)
		}
	}
}

// unsent log shows the line a message answered and its gloss under it, and
// log --json carries the context fields.
func TestCLILogShowsContext(t *testing.T) {
	seedSentJSON(t)
	_, text, _ := runCLI("log", "2")
	want := "\n3  " + when(time.Date(2026, 9, 28, 17, 41, 7, 0, time.FixedZone("", 2*3600))) + "\ncarry on\n" +
		"  ↳ answering: Shall I go on with the parser?\n  ↳ why: finish the refactor before the release\n"
	if !strings.Contains(text, want) {
		t.Fatalf("plain print:\n%s\nwant it to hold\n%s", text, want)
	}
	if strings.Count(text, "↳") != 2 {
		t.Fatalf("context lines on messages that have none:\n%s", text)
	}
	_, out, _ := runCLI("log", "2", "--json")
	var one logShowJSON
	if err := json.Unmarshal([]byte(out), &one); err != nil {
		t.Fatal(err)
	}
	m := one.Messages[len(one.Messages)-1].sentMessageJSON
	if m.UUID != "seed-uuid-3" || m.Kind != "typed" || !m.ReplyTo || m.Gloss == "" || !strings.Contains(m.Asked, "Shall I go on") || m.Source != "" {
		t.Fatalf("message %+v", m)
	}
}

// A pass the Stop hook starts (--from-hook) while the last pass over that
// transcript is under 10 minutes old waits until the 10 minutes are up and
// then runs, so a turn written inside the window is still merged; it
// records its pass before reading. While one hook pass holds the
// transcript's context lock, another does nothing (the holder reads later).
// An explicit run never waits; a record 10 minutes old, from the future or
// unreadable makes nothing wait, and pruneSent drops stale records and
// their locks.
func TestContextFromHookIsDebounced(t *testing.T) {
	cfg := claudeHome(t)
	st := testStore(t)
	t0 := ctxBase()
	r := testRecord("20261001-100000-9", "claude", "/w", t0)
	r.AgentSession, r.Draft = ctxConv, "hello"
	st.logSentAt(r, t0)
	turn := func(i int, text string) claudeTurn {
		return claudeTurn{UUID: fmt.Sprintf("h-%d", i), Kind: "queued", Time: t0.Add(time.Duration(i) * time.Minute), Text: text, SessionID: ctxConv}
	}
	one := claudeTurn{UUID: "h-0", Kind: "typed", Time: t0.Add(time.Second), Text: "hello", SessionID: ctxConv}
	turns := []claudeTurn{one}
	p := writeTurns(t, cfg, "-w", ctxConv, one)
	more := func(text string) {
		turns = append(turns, turn(len(turns), text))
		writeTurns(t, cfg, "-w", ctxConv, turns...)
	}
	state := st.contextStatePath(p)
	if exists(state) {
		t.Fatal("a record before any pass")
	}
	var slept []time.Duration
	contextSleep = func(d time.Duration) {
		slept = append(slept, d)
		// The window passes.
		os.WriteFile(state, []byte(time.Now().Add(-contextDebounce-time.Second).UTC().Format(time.RFC3339Nano)+"\n"), 0o600)
	}
	t.Cleanup(func() { contextSleep = time.Sleep })
	pass := func(wantSleeps int, args ...string) string {
		t.Helper()
		slept = nil
		code, out, errOut := runCLI(append([]string{"context", "--transcript", p}, args...)...)
		if code != 0 || len(slept) != wantSleeps {
			t.Fatalf("%q: exit %d, %q, waited %v", args, code, errOut, slept)
		}
		return out
	}
	if out := pass(0, "--from-hook"); out != "1 logs updated, 0 messages added, 1 matched\n" || !exists(state) {
		t.Fatalf("first hook pass: %q, record %v", out, exists(state))
	}
	more("and this")
	if out := pass(1, "--from-hook"); out != "1 logs updated, 1 messages added, 0 matched\n" {
		t.Fatalf("a hook pass inside 10 minutes: %q", out)
	}
	if d := slept[0]; d <= 0 || d > contextDebounce {
		t.Fatalf("waited %v", d)
	}
	// The hook pass recorded itself, so the explicit pass below starts
	// inside the window and must not wait.
	if !st.contextDebounced(p, time.Now()) {
		t.Fatal("the hook pass left no fresh record")
	}
	more("and that")
	if out := pass(0); out != "1 logs updated, 1 messages added, 0 matched\n" {
		t.Fatalf("an explicit pass: %q", out)
	}
	// Another hook pass holds the context lock: this one leaves the turn to
	// it and returns at once.
	f, err := os.OpenFile(st.contextLockPath(p), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	more("held")
	if out := pass(0, "--from-hook"); out != "0 logs updated, 0 messages added, 0 matched\n" {
		t.Fatalf("a hook pass while another holds the lock: %q", out)
	}
	f.Close()
	for name, at := range map[string]time.Time{
		"10 minutes old":  time.Now().Add(-contextDebounce),
		"from the future": time.Now().Add(time.Hour),
	} {
		os.WriteFile(state, []byte(at.UTC().Format(time.RFC3339Nano)+"\n"), 0o600)
		if st.contextDebounced(p, time.Now()) {
			t.Errorf("a record %s skips", name)
		}
	}
	os.WriteFile(state, []byte("not a time\n"), 0o600)
	if out := pass(0, "--from-hook"); out != "1 logs updated, 1 messages added, 0 matched\n" {
		t.Fatalf("an unreadable record: %q", out)
	}
	os.WriteFile(state, []byte(time.Now().Add(-11*time.Minute).UTC().Format(time.RFC3339Nano)+"\n"), 0o600)
	more("and more")
	if out := pass(0, "--from-hook"); out != "1 logs updated, 1 messages added, 0 matched\n" {
		t.Fatalf("a hook pass after 11 minutes: %q", out)
	}
	if code, _, _ := runCLI("context", "--from-hook"); code != 2 {
		t.Fatalf("--from-hook with no transcript: exit %d", code)
	}
	stale := st.contextStatePath("/x/projects/-w/" + ctxOther + ".jsonl")
	staleLock := st.contextLockPath("/x/projects/-w/" + ctxOther + ".jsonl")
	os.WriteFile(stale, []byte("x\n"), 0o600)
	os.WriteFile(staleLock, nil, 0o600)
	old := time.Now().Add(-contextDebounce - time.Minute)
	os.Chtimes(stale, old, old)
	st.pruneSent()
	if exists(stale) || exists(staleLock) || !exists(state) || !exists(st.contextLockPath(p)) {
		t.Fatalf("after pruning: stale record kept %v, its lock %v, fresh record kept %v, its lock %v",
			exists(stale), exists(staleLock), exists(state), exists(st.contextLockPath(p)))
	}
}

// import claude end to end through the real transcript reader: the fixture
// session, its dates moved inside sentMaxAge, becomes a log holding every
// turn with its kind and what it answered; a live message with the same
// text is matched, not doubled; a second pass changes nothing.
func TestImportClaudeReadsARealTranscript(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	st := testStore(t)
	const conv = "00000000-0000-4000-8000-0000000000aa"
	data, err := os.ReadFile(filepath.Join("testdata", "claude", "transcripts", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	day := time.Now().Add(-48 * time.Hour).UTC().Format("2006-01-02")
	data = bytes.ReplaceAll(data, []byte("2026-03-01T"), []byte(day+"T"))
	dir := filepath.Join(cfg, "projects", claudeSlug("/work/demo"))
	os.MkdirAll(dir, 0o700)
	if err := os.WriteFile(filepath.Join(dir, conv+".jsonl"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	sent, _ := time.Parse(time.RFC3339, day+"T10:00:02Z")
	r := testRecord("20261001-100000-5", "claude", "/work/demo", sent.Add(-time.Minute))
	r.AgentSession, r.Draft = conv, "List the files in the demo folder"
	if err := st.logSentAt(r, sent); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := runCLI("import", "claude", "--config-dir", cfg); code != 0 || out != "1 logs updated, 4 messages added, 1 matched\n" {
		t.Fatalf("exit %d, %q, %q", code, out, errOut)
	}
	l, err := readSent(st.claudeLogPath(conv))
	if err != nil {
		t.Fatal(err)
	}
	if l.AgentSession != conv || len(l.messages) != 5 || l.messages[0].Source != "" {
		t.Fatalf("header %+v, messages %+v", l.sentHeader, l.messages)
	}
	var kinds []string
	for i, m := range l.messages {
		kinds = append(kinds, m.Kind)
		if m.UUID == "" || (i > 0 && m.Source != "transcript") {
			t.Fatalf("message %+v", m)
		}
	}
	if !slices.Equal(kinds, []string{"typed", "typed", "absorbed", "answer", "slash"}) {
		t.Fatalf("kinds %q", kinds)
	}
	if m := l.messages[1]; m.Asked != "There are three files. Want me to open the biggest?" || !m.ReplyTo {
		t.Fatalf("second message %+v", m)
	}
	_, text, _ := runCLI("log", conv)
	if !strings.Contains(text, "yes, the big one\n  ↳ answering: There are three files. Want me to open the biggest?\n") {
		t.Fatalf("unsent log:\n%s", text)
	}
	if code, out, _ := runCLI("context", "--config-dir", cfg); code != 0 || out != "0 logs updated, 0 messages added, 0 matched\n" {
		t.Fatalf("second pass: exit %d, %q", code, out)
	}
}
