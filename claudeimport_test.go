package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	if err := setGloss(path, 1, "the parser bug from yesterday"); err != nil {
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

// A pass the Stop hook starts (--from-hook) is skipped while the last pass
// over that transcript is under 10 minutes old, and writes the record after
// it runs; an explicit run never skips; a record 10 minutes old, from the
// future or unreadable skips nothing, and pruneSent drops the stale ones.
func TestContextFromHookIsDebounced(t *testing.T) {
	cfg := claudeHome(t)
	st := testStore(t)
	t0 := ctxBase()
	r := testRecord("20261001-100000-9", "claude", "/w", t0)
	r.AgentSession, r.Draft = ctxConv, "hello"
	st.logSentAt(r, t0)
	one := claudeTurn{UUID: "h-1", Kind: "typed", Time: t0.Add(time.Second), Text: "hello", SessionID: ctxConv}
	two := claudeTurn{UUID: "h-2", Kind: "queued", Time: t0.Add(time.Minute), Text: "and this", SessionID: ctxConv}
	three := claudeTurn{UUID: "h-3", Kind: "queued", Time: t0.Add(2 * time.Minute), Text: "and that", SessionID: ctxConv}
	four := claudeTurn{UUID: "h-4", Kind: "queued", Time: t0.Add(3 * time.Minute), Text: "and more", SessionID: ctxConv}
	p := writeTurns(t, cfg, "-w", ctxConv, one)
	state := st.contextStatePath(p)
	if exists(state) {
		t.Fatal("a record before any pass")
	}
	pass := func(args ...string) string {
		t.Helper()
		code, out, errOut := runCLI(append([]string{"context", "--transcript", p}, args...)...)
		if code != 0 {
			t.Fatalf("%q: exit %d, %q", args, code, errOut)
		}
		return out
	}
	if out := pass("--from-hook"); out != "1 logs updated, 0 messages added, 1 matched\n" || !exists(state) {
		t.Fatalf("first hook pass: %q, record %v", out, exists(state))
	}
	writeTurns(t, cfg, "-w", ctxConv, one, two)
	if out := pass("--from-hook"); out != "0 logs updated, 0 messages added, 0 matched\n" {
		t.Fatalf("a hook pass inside 10 minutes ran: %q", out)
	}
	if out := pass(); out != "1 logs updated, 1 messages added, 0 matched\n" {
		t.Fatalf("an explicit pass skipped: %q", out)
	}
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
	writeTurns(t, cfg, "-w", ctxConv, one, two, three)
	if out := pass("--from-hook"); out != "1 logs updated, 1 messages added, 0 matched\n" {
		t.Fatalf("an unreadable record skipped: %q", out)
	}
	os.WriteFile(state, []byte(time.Now().Add(-11*time.Minute).UTC().Format(time.RFC3339Nano)+"\n"), 0o600)
	writeTurns(t, cfg, "-w", ctxConv, one, two, three, four)
	if out := pass("--from-hook"); out != "1 logs updated, 1 messages added, 0 matched\n" {
		t.Fatalf("a hook pass after 11 minutes skipped: %q", out)
	}
	if code, _, _ := runCLI("context", "--from-hook"); code != 2 {
		t.Fatalf("--from-hook with no transcript: exit %d", code)
	}
	stale := st.contextStatePath("/x/projects/-w/" + ctxOther + ".jsonl")
	os.WriteFile(stale, []byte("x\n"), 0o600)
	old := time.Now().Add(-contextDebounce - time.Minute)
	os.Chtimes(stale, old, old)
	st.pruneSent()
	if exists(stale) || !exists(state) {
		t.Fatalf("after pruning: stale record kept %v, fresh record kept %v", exists(stale), exists(state))
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
