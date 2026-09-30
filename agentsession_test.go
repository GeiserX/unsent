package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// sessionFixture is one of the session files copied from a real Claude
// Code run, with the pid and session id it holds.
type sessionFixture struct {
	data []byte
	pid  int
	id   string
	at   time.Time // startedAt
}

func loadSessionFixture(t *testing.T, name string) sessionFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "claude", "2.1.282", "sessions", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		PID       int    `json:"pid"`
		SessionID string `json:"sessionId"`
		StartedAt int64  `json:"startedAt"`
	}
	if err := json.Unmarshal(data, &f); err != nil || f.PID == 0 || f.SessionID == "" {
		t.Fatalf("%s: %v %+v", name, err, f)
	}
	return sessionFixture{data, f.PID, f.SessionID, time.UnixMilli(f.StartedAt)}
}

// claudeConfig points CLAUDE_CONFIG_DIR at a scratch folder and returns its
// sessions folder.
func claudeConfig(t *testing.T) string {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	dir := filepath.Join(cfg, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// place writes a fixture as Claude Code's file for its pid, as it would
// write it in place.
func (f sessionFixture) place(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(f.pid)+".json"), f.data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// The files of one real run, in the order Claude Code wrote them: the
// tracker follows the id through the --resume picker, /clear and the
// /resume picker, all in one process.
func TestClaudeSessionFollowsTheRealFiles(t *testing.T) {
	dir := claudeConfig(t)
	start := loadSessionFixture(t, "start")
	start.place(t, dir)
	if id := newSessionTracker(claude.session, start.pid, start.at).current(); id != start.id {
		t.Fatalf("at start: %q, want %q", id, start.id)
	}

	open := loadSessionFixture(t, "picker-open")
	tr := newSessionTracker(claude.session, open.pid, open.at)
	steps := []string{"picker-open", "picker-chosen", "clear", "slash-resume-open", "slash-resume-chosen"}
	var got []string
	for _, name := range steps {
		f := loadSessionFixture(t, name)
		if f.pid != open.pid {
			t.Fatalf("%s is pid %d, not the run's %d", name, f.pid, open.pid)
		}
		f.place(t, dir)
		got = append(got, tr.current())
	}
	chosen, cleared := loadSessionFixture(t, "picker-chosen").id, loadSessionFixture(t, "clear").id
	// The picker's fresh id first, then the conversation chosen, a new one
	// for /clear, which the /resume picker keeps while it is open, and the
	// chosen one again.
	want := []string{open.id, chosen, cleared, cleared, chosen}
	if !slices.Equal(got, want) {
		t.Fatalf("ids %q, want %q", got, want)
	}
	if open.id == chosen || cleared == chosen {
		t.Fatal("the fixtures do not switch session")
	}
}

// A read that finds no id keeps the last one: the file missing (the agent
// removes it on its way out), cut short, or of another shape. A file that
// was never there gives none.
func TestClaudeSessionMissingOrMalformed(t *testing.T) {
	dir := claudeConfig(t)
	f := loadSessionFixture(t, "start")
	tr := newSessionTracker(claude.session, f.pid, f.at)
	tr.now = func() time.Time { return f.at } // no descendant scan
	tr.next = f.at.Add(time.Hour)
	if id := tr.current(); id != "" {
		t.Fatalf("no file: %q", id)
	}
	f.place(t, dir)
	if id := tr.current(); id != f.id {
		t.Fatalf("with the file: %q", id)
	}
	path := filepath.Join(dir, strconv.Itoa(f.pid)+".json")
	for name, data := range map[string]string{
		"cut short":        string(f.data[:len(f.data)/2]),
		"empty":            "",
		"not an object":    `["sessionId"]`,
		"no sessionId":     `{"pid":` + strconv.Itoa(f.pid) + `}`,
		"sessionId number": `{"pid":` + strconv.Itoa(f.pid) + `,"sessionId":7}`,
		"a path for an id": `{"pid":` + strconv.Itoa(f.pid) + `,"sessionId":"../../etc/passwd"}`,
	} {
		os.WriteFile(path, []byte(data), 0o644)
		if id := tr.current(); id != f.id {
			t.Errorf("%s: %q, want the last id %q", name, id, f.id)
		}
	}
	os.Remove(path)
	if id := tr.current(); id != f.id {
		t.Errorf("file gone: %q, want the last id", id)
	}
}

// A file for the same pid left by an earlier process (kill -9 leaves it,
// and the pid can come round again), or naming another pid, is not the
// running process's.
func TestClaudeSessionStaleFile(t *testing.T) {
	dir := claudeConfig(t)
	f := loadSessionFixture(t, "start")
	f.place(t, dir)
	if id, found := claudeSession(f.pid, f.at.Add(time.Minute)); found || id != "" {
		t.Fatalf("stale file: %q %v", id, found)
	}
	if id, found := claudeSession(f.pid, f.at.Add(500*time.Millisecond)); !found || id != f.id {
		t.Fatalf("a start within a second is this one: %q %v", id, found)
	}
	os.Rename(filepath.Join(dir, strconv.Itoa(f.pid)+".json"), filepath.Join(dir, "7.json"))
	if id, found := claudeSession(7, f.at); found || id != "" {
		t.Fatalf("a file naming pid %d read as pid 7's: %q", f.pid, id)
	}
}

// Without CLAUDE_CONFIG_DIR the folder is ~/.claude/sessions.
func TestClaudeSessionDefaultFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	os.Unsetenv("CLAUDE_CONFIG_DIR")
	dir := filepath.Join(home, ".claude", "sessions")
	os.MkdirAll(dir, 0o700)
	f := loadSessionFixture(t, "start")
	f.place(t, dir)
	if id, _ := claudeSession(f.pid, f.at); id != f.id {
		t.Fatalf("%q, want %q", id, f.id)
	}
}

// The <pid>.<hash>.key file beside each session file is a peer token, and
// unsent never opens it. Here it is a named pipe: opening it for reading
// blocks until a writer comes, so a reader that opens it never returns.
func TestClaudeSessionNeverOpensTheKeyFile(t *testing.T) {
	dir := claudeConfig(t)
	f := loadSessionFixture(t, "start")
	f.place(t, dir)
	var keys []string
	for _, name := range []string{
		strconv.Itoa(f.pid) + ".5d76fcf665917db16b76a17063335c6c3ab896d66e1647b59405acdc4d328a74.key",
		strconv.Itoa(os.Getpid()) + ".0123.key",
		"1.json.key",
	} {
		k := filepath.Join(dir, name)
		if err := syscall.Mkfifo(k, 0o600); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
	}
	done := make(chan []string, 1)
	go func() {
		var ids []string
		// The pid's own file, then a root with no file of its own, which
		// lists the folder for a descendant.
		ids = append(ids, newSessionTracker(claude.session, f.pid, f.at).current())
		ids = append(ids, newSessionTracker(claude.session, os.Getpid(), f.at).current())
		done <- ids
	}()
	select {
	case ids := <-done:
		if ids[0] != f.id || ids[1] != "" {
			t.Fatalf("ids %q", ids)
		}
	case <-time.After(10 * time.Second):
		// Let the stuck open return, so the test binary can exit.
		for _, k := range keys {
			if w, err := os.OpenFile(k, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
				w.Close()
			}
		}
		t.Fatal("the reader opened a .key file")
	}
	if pids := claudeSessionPids(); !slices.Equal(pids, []int{f.pid}) {
		t.Fatalf("pids %v", pids)
	}
}

// When the process unsent started has no file (a launcher that does not
// exec the agent), the session is read from the nearest descendant's file,
// never from an unrelated process's; and listing processes is rate-limited.
func TestClaudeSessionFromADescendant(t *testing.T) {
	dir := claudeConfig(t)
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { child.Process.Kill(); child.Wait() }()
	since := time.Now().Add(-time.Minute)
	write := func(pid int, id string) {
		b, _ := json.Marshal(map[string]any{"pid": pid, "sessionId": id, "startedAt": time.Now().UnixMilli()})
		os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), b, 0o644)
	}
	write(1, "not-ours")
	write(os.Getppid(), "our-parent")
	calls := 0
	old := processParents
	processParents = func() map[int]int { calls++; return old() }
	defer func() { processParents = old }()

	now := time.Now()
	tr := newSessionTracker(claude.session, os.Getpid(), since)
	tr.now = func() time.Time { return now }
	if id := tr.current(); id != "" || calls != 1 {
		t.Fatalf("no descendant has a file: %q after %d process lists", id, calls)
	}
	write(child.Process.Pid, "the-child")
	if id := tr.current(); id != "" || calls != 1 {
		t.Fatalf("listed processes again at once: %q, %d lists", id, calls)
	}
	now = now.Add(2 * time.Second)
	if id := tr.current(); id != "the-child" || calls != 2 {
		t.Fatalf("after the wait: %q, %d lists", id, calls)
	}
	// Found, it is read directly from then on, switch included.
	write(child.Process.Pid, "the-child-resumed")
	if id := tr.current(); id != "the-child-resumed" || calls != 2 {
		t.Fatalf("followed: %q, %d lists", id, calls)
	}
	// The unsent-started process's own file wins once it appears.
	write(os.Getpid(), "the-root")
	os.Remove(filepath.Join(dir, strconv.Itoa(child.Process.Pid)+".json"))
	if id := tr.current(); id != "the-root" {
		t.Fatalf("root: %q", id)
	}
}

func TestDepthBelow(t *testing.T) {
	parents := map[int]int{10: 1, 11: 10, 12: 11, 13: 12, 20: 1, 30: 30}
	for _, c := range []struct{ pid, root, want int }{
		{11, 10, 1}, {13, 10, 3}, {20, 10, 0}, {10, 10, 0}, {99, 10, 0}, {30, 10, 0},
	} {
		if got := depthBelow(parents, c.pid, c.root); got != c.want {
			t.Errorf("depthBelow(%d, %d) = %d, want %d", c.pid, c.root, got, c.want)
		}
	}
}

// fakeIDs gives a session a tracker whose reads return *id.
func fakeIDs(s *session, id *string) {
	s.ids = newSessionTracker(&sessionSource{
		read: func(int, time.Time) (string, bool) { return *id, *id != "" },
		pids: func() []int { return nil },
	}, 4242, time.Now())
	s.ids.next = time.Now().Add(time.Hour)
}

// The record follows the session as it changes, and a message goes to the
// log of the session it was typed in, even when the agent switched as it
// took the message (/clear, a picker choice).
func TestSessionFollowsTheAgentSession(t *testing.T) {
	s := newSendSession(t, &claude)
	id := "conv-a"
	fakeIDs(s.session, &id)
	s.draw("")
	s.save()
	s.draw("typed in a")
	s.save()
	if s.rec.AgentSession != "conv-a" {
		t.Fatalf("record session %q", s.rec.AgentSession)
	}
	// Enter, and the agent switches session as it empties the box.
	s.input([]byte("\r"))
	id = "conv-b"
	s.draw("")
	s.save()
	s.draw("typed in b")
	s.save()
	s.input([]byte("\r"))
	s.draw("")
	s.save()
	// The id switches while the draft stays: the record follows.
	s.draw("left in b")
	s.save()
	id = "conv-c"
	s.draw("left in b")
	s.save()
	var rec record
	data, _ := os.ReadFile(s.store.draftPath(s.rec.ID))
	json.Unmarshal(data, &rec)
	if rec.AgentSession != "conv-c" || rec.Draft != "left in b" {
		t.Fatalf("record on disk: %q %q", rec.AgentSession, rec.Draft)
	}
	for conv, want := range map[string][]string{"conv-a": {"typed in a"}, "conv-b": {"typed in b"}} {
		l, err := readSent(s.store.sentPath("claude-" + conv))
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, m := range l.messages {
			got = append(got, m.Text)
		}
		if !slices.Equal(got, want) || l.AgentSession != conv || l.Session != s.rec.ID || len(l.resumes) != 0 {
			t.Errorf("%s: %q, header %+v, %d resumes", conv, got, l.sentHeader, len(l.resumes))
		}
	}
	if _, err := os.Stat(s.store.sentPath(s.rec.ID)); err == nil {
		t.Error("a per-run log was written too")
	}
}

// Without an agent session id the log stays per run.
func TestSessionWithoutAnIDLogsPerRun(t *testing.T) {
	s := newSendSession(t, &claude)
	id := ""
	fakeIDs(s.session, &id)
	s.draw("hello")
	s.save()
	s.input([]byte("\r"))
	s.draw("")
	s.save()
	s.expect([]string{"hello"}, nil)
	if s.rec.AgentSession != "" {
		t.Fatalf("session %q", s.rec.AgentSession)
	}
}

// Two runs of one conversation share its sent log: the second appends to
// it, after a line saying when it resumed, and the header keeps the first
// run's start. A run that switches back to a conversation it left says so
// again.
func TestSentLogContinuesAcrossRuns(t *testing.T) {
	st := testStore(t)
	t1 := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	r1 := testRecord("run-1", "claude", "/work", t1)
	r1.AgentSession = "conv"
	r1.Draft = "from the first run"
	if err := st.logSent(r1); err != nil {
		t.Fatal(err)
	}
	st2, err := openStore() // another process: it has appended nothing yet
	if err != nil {
		t.Fatal(err)
	}
	t2 := time.Now().Add(-time.Minute).Truncate(time.Millisecond)
	r2 := testRecord("run-2", "claude", "/work", t2)
	r2.AgentSession = "conv"
	for _, text := range []string{"resumed, one", "resumed, two"} {
		r2.Draft = text
		if err := st2.logSent(r2); err != nil {
			t.Fatal(err)
		}
	}
	// /resume away and back: a second resume line for the same run.
	t3 := time.Now().Truncate(time.Millisecond)
	r2.joined = t3
	r2.Draft = "back again"
	st2.logSent(r2)

	path := st.sentPath("claude-conv")
	l, err := readSent(path)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, m := range l.messages {
		texts = append(texts, m.Text)
	}
	if want := []string{"from the first run", "resumed, one", "resumed, two", "back again"}; !slices.Equal(texts, want) {
		t.Fatalf("messages %q", texts)
	}
	if l.Session != "run-1" || l.AgentSession != "conv" || !l.Started.Equal(t1) {
		t.Fatalf("header %+v", l.sentHeader)
	}
	if len(l.resumes) != 2 || l.resumes[0].Session != "run-2" || !l.resumes[0].Resumed.Equal(t2) || l.resumes[0].after != 1 ||
		!l.resumes[1].Resumed.Equal(t3) || l.resumes[1].after != 3 {
		t.Fatalf("resumes %+v", l.resumes)
	}
	if logs := st.sentLogs(); len(logs) != 1 {
		t.Fatalf("%d logs, want the one conversation", len(logs))
	}

	// unsent log finds it by the agent's session id too, and shows the resumes.
	for _, arg := range []string{"conv", "run-1", "1"} {
		code, out, errOut := runCLI("log", arg)
		if code != 0 || !strings.Contains(out, "claude session conv") || strings.Count(out, "(resumed ") != 2 ||
			strings.Index(out, "from the first run") > strings.Index(out, "(resumed ") ||
			strings.Index(out, "(resumed ") > strings.Index(out, "resumed, one") {
			t.Fatalf("log %s: %d\n%s%s", arg, code, out, errOut)
		}
	}
}

// One run that started two conversations' logs (a /clear) names both: log
// and forget ask for the agent's session id instead of picking one.
func TestSentLogRunIDNamingTwoConversations(t *testing.T) {
	st := testStore(t)
	r := testRecord("run-1", "claude", "/work", time.Now())
	for _, conv := range []string{"conv-a", "conv-b"} {
		r.AgentSession, r.Draft = conv, "in "+conv
		if err := st.logSent(r); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"log", "run-1"}, {"forget", "--log", "run-1"}} {
		if code, _, errOut := runCLI(args...); code != 2 || !strings.Contains(errOut, "conv-a") || !strings.Contains(errOut, "conv-b") {
			t.Fatalf("%q: %d %q", args, code, errOut)
		}
	}
	if len(st.sentLogs()) != 2 {
		t.Fatal("an ambiguous forget deleted a log")
	}
	if code, _, _ := runCLI("forget", "--log", "conv-a"); code != 0 {
		t.Fatal("forget by the agent's session id")
	}
	if logs := st.sentLogs(); len(logs) != 1 || logs[0].AgentSession != "conv-b" {
		t.Fatalf("left %d", len(logs))
	}
}

// Files written before agent_session existed still read: a record loads
// with no session, and a sent log keeps its messages and takes new ones.
func TestOldFilesWithoutAgentSession(t *testing.T) {
	st := testStore(t)
	old := `{"format":1,"id":"20260901-120000-1","command":["claude"],"agent":"claude","cwd":"/w","pid":1,` +
		`"started":"2026-09-01T12:00:00Z","updated":"2026-09-01T12:01:00Z","draft":"an old draft"}`
	os.WriteFile(st.draftPath("20260901-120000-1"), []byte(old), 0o600)
	rs := st.orphans()
	if len(rs) != 1 || rs[0].Draft != "an old draft" || rs[0].AgentSession != "" {
		t.Fatalf("old record: %+v", rs)
	}
	logOld := `{"format":1,"session":"20260901-120000-1","agent":"claude","cwd":"/w","started":"2026-09-01T12:00:00Z"}` + "\n" +
		`{"time":"2026-09-01T12:00:30Z","text":"an old message"}` + "\n"
	os.WriteFile(st.sentPath("20260901-120000-1"), []byte(logOld), 0o600)
	r := rs[0]
	r.Draft = "a new one"
	if err := st.logSent(r); err != nil {
		t.Fatal(err)
	}
	l, err := readSent(st.sentPath("20260901-120000-1"))
	if err != nil || len(l.messages) != 2 || l.AgentSession != "" || len(l.resumes) != 0 || l.messages[1].Text != "a new one" {
		t.Fatalf("old log: %+v %v", l, err)
	}
}

// Trimming a conversation's log keeps the resume lines of the messages it
// keeps, and a line of a kind this build does not know.
func TestSentLogTrimKeepsResumeLines(t *testing.T) {
	st := testStore(t)
	r := testRecord("run-1", "claude", "/work", time.Now())
	r.AgentSession = "conv"
	big := strings.Repeat("x", 1<<20)
	for i := range 3 {
		r.Draft = strconv.Itoa(i) + " " + big
		st.logSent(r)
	}
	path := st.sentPath("claude-conv")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"future":"kind"}` + "\n")
	f.Close()
	st2, _ := openStore()
	r.ID, r.joined = "run-2", time.Now()
	for i := 3; i < 6; i++ {
		r.Draft = strconv.Itoa(i) + " " + big
		if err := st2.logSent(r); err != nil {
			t.Fatal(err)
		}
	}
	l, _ := readSent(path)
	if l.Dropped != 2 || len(l.messages) != 4 || !strings.HasPrefix(l.messages[0].Text, "2 ") {
		t.Fatalf("dropped %d kept %d", l.Dropped, len(l.messages))
	}
	if len(l.resumes) != 1 || l.resumes[0].Session != "run-2" || l.resumes[0].after != 1 {
		t.Fatalf("resumes %+v", l.resumes)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `{"future":"kind"}`) {
		t.Fatal("an unknown line was dropped")
	}
}

// End to end through the wrapper and the fake agent's session files: two
// runs of one conversation share one sent log, and a switch mid-run (as
// /resume does) moves the record and the next message to the other
// conversation.
func TestWrapSentLogPerConversation(t *testing.T) {
	claudeConfig(t)
	home := t.TempDir()
	t.Setenv("UNSENT_FAKE_SESSION", "conv-a")
	t.Setenv("UNSENT_FAKE_SESSION_NEXT", "conv-b")
	runWrappedIn(t, home, "claude", []string{"claude"}, func(type_ func(string)) {
		type_("first run")
		type_("\r")
		type_("\x04")
	})
	_, st, _, _ := runWrappedIn(t, home, "claude", []string{"claude"}, func(type_ func(string)) {
		type_("second run")
		type_("\r")
		type_("\x0f") // the agent switches to conv-b
		type_("in b")
		type_("\r")
		type_("left in b")
		type_("\x04")
	})
	logs := map[string]*sentLog{}
	for _, l := range st.sentLogs() {
		logs[l.AgentSession] = l
	}
	a, b := logs["conv-a"], logs["conv-b"]
	if len(logs) != 2 || a == nil || b == nil {
		t.Fatalf("logs %v", logs)
	}
	texts := func(l *sentLog) (out []string) {
		for _, m := range l.messages {
			out = append(out, m.Text)
		}
		return out
	}
	if got := texts(a); !slices.Equal(got, []string{"first run", "second run"}) || len(a.resumes) != 1 || a.resumes[0].after != 1 {
		t.Fatalf("conv-a: %q, resumes %+v", got, a.resumes)
	}
	if got := texts(b); !slices.Equal(got, []string{"in b"}) || len(b.resumes) != 0 || b.Session != a.resumes[0].Session {
		t.Fatalf("conv-b: %q, header %+v", got, b.sentHeader)
	}
	rs := st.orphans()
	if len(rs) != 1 || rs[0].Draft != "left in b" || rs[0].AgentSession != "conv-b" {
		t.Fatalf("orphans %+v", rs)
	}
}

// launcherIDs gives a session, or returns, a tracker for a launcher (pid
// 100) that does not exec the agent: files maps each agent pid below it to
// the id its file names, and the clock moves only by hand.
func launcherIDs(t *testing.T, files map[int]string, now *time.Time) *sessionTracker {
	t.Helper()
	old := processParents
	processParents = func() map[int]int { return map[int]int{100: 1, 201: 100, 202: 100} }
	t.Cleanup(func() { processParents = old })
	tr := newSessionTracker(&sessionSource{
		read: func(pid int, _ time.Time) (string, bool) { id, ok := files[pid]; return id, ok },
		pids: func() []int { return slices.Sorted(maps.Keys(files)) },
	}, 100, *now)
	tr.now = func() time.Time { return *now }
	return tr
}

// The descendant whose file named the session goes while the launcher
// runs on: that session ended. The tracker never gives its id again, and
// finds the next agent's without waiting out the backoff the launcher's
// own screen built up.
func TestSessionTrackerDescendantEnds(t *testing.T) {
	now := time.Now()
	files := map[int]string{}
	tr := launcherIDs(t, files, &now)
	for range 6 { // the launcher's screen: the wait grows to a minute
		if id := tr.current(); id != "" {
			t.Fatalf("no agent yet: %q", id)
		}
		now = now.Add(time.Minute)
	}
	files[201] = "conv-of-d"
	if id := tr.current(); id != "conv-of-d" {
		t.Fatalf("first agent: %q", id)
	}
	delete(files, 201)
	if id := tr.current(); id != "" {
		t.Fatalf("its file gone, the launcher still running: %q", id)
	}
	files[202] = "conv-of-d2"
	if id := tr.current(); id != "" {
		t.Fatalf("before the next look: %q", id)
	}
	now = now.Add(2 * time.Second)
	if id := tr.current(); id != "conv-of-d2" {
		t.Fatalf("2 s later: %q", id)
	}
}

// Through a session: the draft left in the ended agent's box stays in its
// conversation, and text typed after that gets no session until the next
// agent's id is read, so a restore never takes it into the conversation
// that ended.
func TestSessionDescendantEndsMidRun(t *testing.T) {
	s := newSendSession(t, &claude)
	now := time.Now()
	files := map[int]string{201: "conv-of-d"}
	s.ids = launcherIDs(t, files, &now)
	onDisk := func() record {
		var rec record
		data, _ := os.ReadFile(s.store.draftPath(s.rec.ID))
		json.Unmarshal(data, &rec)
		return rec
	}
	s.draw("typed in d")
	s.save()
	delete(files, 201) // the agent exits, its box still on screen
	s.draw("typed in d")
	s.save()
	if rec := onDisk(); rec.AgentSession != "conv-of-d" || rec.Draft != "typed in d" {
		t.Fatalf("left in d: %q %q", rec.AgentSession, rec.Draft)
	}
	files[202] = "conv-of-d2" // the next agent, not read yet
	s.draw("")
	s.save()
	s.draw("typed in d2")
	s.save()
	if rec := onDisk(); rec.AgentSession != "" || rec.Draft != "typed in d2" {
		t.Fatalf("typed before its id was read: %q %q", rec.AgentSession, rec.Draft)
	}
	for _, r := range s.store.load(true) {
		if r.Draft == "typed in d" && r.AgentSession != "conv-of-d" {
			t.Fatalf("history of d: %q", r.AgentSession)
		}
	}
	now = now.Add(2 * time.Second)
	s.draw("typed in d2")
	s.save()
	if rec := onDisk(); rec.AgentSession != "conv-of-d2" {
		t.Fatalf("once read: %q", rec.AgentSession)
	}
}

// Messages sent in one conversation in one run, and a run that goes to
// another conversation and back, as /resume there and back does: the log
// says the run resumed only when it came back.
func TestSessionResumeLinesInOneRun(t *testing.T) {
	s := newSendSession(t, &claude)
	id := "conv-a"
	fakeIDs(s.session, &id)
	send := func(text string) {
		t.Helper()
		s.draw(text)
		s.save()
		s.input([]byte("\r"))
		s.draw("")
		s.save()
	}
	send("one")
	send("two")
	id = "conv-b"
	send("in b")
	id = "conv-a"
	send("back in a")
	a, err := readSent(s.store.sentPath("claude-conv-a"))
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, m := range a.messages {
		texts = append(texts, m.Text)
	}
	if !slices.Equal(texts, []string{"one", "two", "back in a"}) || len(a.resumes) != 1 || a.resumes[0].after != 2 ||
		a.resumes[0].Session != s.rec.ID {
		t.Fatalf("conv-a: %q, resumes %+v", texts, a.resumes)
	}
	b, err := readSent(s.store.sentPath("claude-conv-b"))
	if err != nil || len(b.messages) != 1 || len(b.resumes) != 0 {
		t.Fatalf("conv-b: %+v %v", b, err)
	}
}

// trimLog writes a sent log from lines after a header, padding the last
// line's text so the file is sentMaxBytes+over bytes, trims it and reads
// it back.
func trimLog(t *testing.T, over int, lines ...string) *sentLog {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude-conv.jsonl")
	head := `{"format":1,"session":"run-1","agent":"claude","agent_session":"conv","cwd":"/w","started":"2026-09-01T12:00:00Z"}` + "\n"
	body := head
	for _, l := range lines {
		body += l + "\n"
	}
	last := `{"time":"2026-09-01T12:09:00Z","text":"last %s"}` + "\n"
	pad := sentMaxBytes + over - len(body) - len(fmt.Sprintf(last, ""))
	body += fmt.Sprintf(last, strings.Repeat("x", pad))
	if len(body) != sentMaxBytes+over {
		t.Fatalf("log is %d bytes", len(body))
	}
	os.WriteFile(path, []byte(body), 0o600)
	if err := trimSent(path); err != nil {
		t.Fatal(err)
	}
	l, err := readSent(path)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// A trim that drops every message before a resume line keeps the line:
// the message after it still comes from the run that resumed.
func TestSentLogTrimAtAResumeLine(t *testing.T) {
	old := `{"time":"2026-09-01T12:01:00Z","text":"old"}`
	resume := `{"resumed":"2026-09-01T12:05:00Z","session":"run-2"}`
	// Over by less than the old message and the resume line together.
	l := trimLog(t, len(old)+1+5, old, resume)
	if l.Dropped != 1 || len(l.messages) != 1 || len(l.resumes) != 1 || l.resumes[0].Session != "run-2" || l.resumes[0].after != 0 {
		t.Fatalf("dropped %d, %d messages, resumes %+v", l.Dropped, len(l.messages), l.resumes)
	}

	// A resume line and an unknown line at the cut: the resume line whose
	// run lost every message goes, the next one stays, the unknown line
	// stays, and only messages count as dropped.
	first := `{"time":"2026-09-01T12:01:00Z","text":"first"}`
	second := `{"time":"2026-09-01T12:03:00Z","text":"second"}`
	l = trimLog(t, len(first)+1+len(second)+1+1,
		first,
		`{"resumed":"2026-09-01T12:02:00Z","session":"run-2"}`,
		second,
		`{"future":"kind"}`,
		`{"resumed":"2026-09-01T12:05:00Z","session":"run-3"}`,
		`{"time":"2026-09-01T12:06:00Z","text":"third"}`,
	)
	var texts []string
	for _, m := range l.messages {
		texts = append(texts, m.Text[:min(len(m.Text), 5)])
	}
	if l.Dropped != 2 || !slices.Equal(texts, []string{"third", "last "}) || len(l.resumes) != 1 ||
		l.resumes[0].Session != "run-3" || l.resumes[0].after != 0 {
		t.Fatalf("dropped %d, %q, resumes %+v", l.Dropped, texts, l.resumes)
	}
	if data, _ := os.ReadFile(l.path); !strings.Contains(string(data), `{"future":"kind"}`) {
		t.Fatal("an unknown line was dropped")
	}
}

// agyScratch points HOME at a scratch folder, makes agy's conversations
// folder in it and returns it, with the cache of held files cleared before
// and after: it is one variable for the whole process.
func agyScratch(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := agyConversations()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	agyHeld = heldFiles{}
	t.Cleanup(func() { agyHeld = heldFiles{} })
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// touchFile writes an empty file and returns its path.
func touchFile(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// holdOpen starts a process that holds the named files open, on file
// descriptors of its own, until the test ends, and waits until it really
// holds them. It returns its pid. The shell execs sleep, so the pid is
// the one process that holds them.
func holdOpen(t *testing.T, paths ...string) int {
	t.Helper()
	script := ""
	for i := range paths {
		script += fmt.Sprintf("exec %d<\"$%d\"; ", 3+i, i+1)
	}
	cmd := exec.Command("sh", append([]string{"-c", script + "exec sleep 120", "sh"}, paths...)...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	pid := cmd.Process.Pid
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		open := openFiles(pid)
		held := 0
		for _, want := range paths {
			for _, p := range open {
				if filepath.Base(p) == filepath.Base(want) {
					held++
					break
				}
			}
		}
		if held == len(paths) {
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("pid %d never opened %q; it holds %q", pid, paths, open)
		}
	}
}

// agy names the conversation a process is in by the database it holds
// open, so a real process holding conversations/<id>.db is in <id>. Its
// -wal and -shm name the same conversation and must not read as three.
// A process holding two databases, which is what /new leaves behind, is
// in no known session: nothing on disk says which of them is current, and
// guessing would file a message under the wrong conversation. A process
// that holds none, and one that has gone, are in none either.
func TestAgySessionIsTheConversationDatabaseHeldOpen(t *testing.T) {
	dir := agyScratch(t)
	a := touchFile(t, dir, "conv-a.db")
	aWAL := touchFile(t, dir, "conv-a.db-wal")
	b := touchFile(t, dir, "conv-b.db")
	notes := touchFile(t, dir, "notes.txt")

	pid := holdOpen(t, a, aWAL)
	if id, found := agySession(pid, time.Time{}); !found || id != "conv-a" {
		t.Fatalf("one conversation held: %q %v, want conv-a", id, found)
	}
	if pids := agySessionPids(); !slices.Equal(pids, []int{pid}) {
		t.Fatalf("pids %v, want the one holder %d", pids, pid)
	}

	agyHeld = heldFiles{}
	two := holdOpen(t, a, b)
	if id, found := agySession(two, time.Time{}); !found || id != sessionUnsure {
		t.Fatalf("two conversations held: %q %v, want %q", id, found, sessionUnsure)
	}

	agyHeld = heldFiles{}
	none := holdOpen(t, notes)
	if id, found := agySession(none, time.Time{}); found || id != "" {
		t.Fatalf("no conversation held: %q %v", id, found)
	}

	// The process goes: nothing is held, whatever was read before.
	gone := holdOpen(t, a)
	agyHeld = heldFiles{}
	if id, found := agySession(gone, time.Time{}); !found || id != "conv-a" {
		t.Fatalf("before it went: %q %v", id, found)
	}
	p, _ := os.FindProcess(gone)
	p.Kill()
	p.Wait()
	agyHeld = heldFiles{}
	if id, found := agySession(gone, time.Time{}); found || id != "" {
		t.Fatalf("the process is gone: %q %v", id, found)
	}
}

// Where there are no process files, lsof answers instead. Forcing that
// path runs it on Linux too, where /proc would otherwise hide it.
func TestHeldFilesReadsLsofWithoutProcessFiles(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		if _, err := os.Stat("/usr/sbin/lsof"); err != nil {
			t.Skip("no lsof on this system")
		}
	}
	dir := agyScratch(t)
	db := touchFile(t, dir, "conv-lsof.db")
	setProcFS(t, filepath.Join(t.TempDir(), "no-proc"))
	pid := holdOpen(t, db)
	if id, found := agySession(pid, time.Time{}); !found || id != "conv-lsof" {
		t.Fatalf("through lsof: %q %v, want conv-lsof", id, found)
	}
	if pids := agySessionPids(); !slices.Equal(pids, []int{pid}) {
		t.Fatalf("pids %v, want the one holder %d", pids, pid)
	}
}

// setProcFS points procFS at path for the length of the test.
func setProcFS(t *testing.T, path string) {
	t.Helper()
	old := procFS
	procFS = path
	agyHeld = heldFiles{}
	t.Cleanup(func() { procFS = old; agyHeld = heldFiles{} })
}

// Where there are process files, they answer and lsof is never run, not
// even for a process that has none (it is gone, or not ours). The
// tree here is the shape Linux gives: a folder per pid holding a link per
// descriptor. Forcing that path reads it on macOS too, which has no /proc
// of its own.
func TestHeldFilesReadsTheProcessFiles(t *testing.T) {
	dir := agyScratch(t)
	db := touchFile(t, dir, "conv-proc.db")
	wal := touchFile(t, dir, "conv-proc.db-wal")
	elsewhere := touchFile(t, t.TempDir(), "conv-elsewhere.db")

	proc := t.TempDir()
	setProcFS(t, proc)
	// An lsof on PATH that names another conversation: any answer that
	// went through it instead of the process files says so.
	bin := t.TempDir()
	lsofSays := "#!/bin/sh\necho p4242\necho p9999\necho n" + filepath.Join(dir, "conv-lsof.db") + "\n"
	if err := os.WriteFile(filepath.Join(bin, "lsof"), []byte(lsofSays), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if err := os.MkdirAll(filepath.Join(proc, "self", "fd"), 0o700); err != nil {
		t.Fatal(err)
	}
	fds := filepath.Join(proc, "4242", "fd")
	if err := os.MkdirAll(fds, 0o700); err != nil {
		t.Fatal(err)
	}
	for fd, target := range map[string]string{"3": db, "4": wal, "5": elsewhere} {
		if err := os.Symlink(target, filepath.Join(fds, fd)); err != nil {
			t.Fatal(err)
		}
	}
	// A process that holds a database of another folder only (another
	// home's agy), a descriptor that is not a link to a file (a socket
	// reads as one on Linux; here a plain file), and a folder that is not
	// a process.
	if err := os.MkdirAll(filepath.Join(proc, "5151", "fd"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(proc, "5151", "fd", "3")); err != nil {
		t.Fatal(err)
	}
	touchFile(t, fds, "6")
	if err := os.MkdirAll(filepath.Join(proc, "9999"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proc, "not-a-pid"), 0o700); err != nil {
		t.Fatal(err)
	}

	if got := openFiles(4242); !slices.Equal(got, []string{db, wal, elsewhere}) {
		t.Fatalf("open files %q", got)
	}
	if got := openFiles(9999); got != nil {
		t.Fatalf("a process with no descriptors of ours: %q", got)
	}
	if id, found := agySession(4242, time.Time{}); !found || id != "conv-proc" {
		t.Fatalf("through the process files: %q %v", id, found)
	}
	if pids := agySessionPids(); !slices.Equal(pids, []int{4242}) {
		t.Fatalf("pids %v, want just 4242", pids)
	}
}

// A listing tool that fails, or that stalls, leaves the session unknown.
// It must not hold the save loop: that loop is what writes the draft as
// the window closes, so it is given a second and no more.
func TestHeldFilesWhenTheListingToolFailsOrStalls(t *testing.T) {
	dir := agyScratch(t)
	touchFile(t, dir, "conv-a.db")
	setProcFS(t, filepath.Join(t.TempDir(), "no-proc"))
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	fake := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(bin, "lsof"), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		agyHeld = heldFiles{}
	}

	fake("exit 1")
	if id, found := agySession(os.Getpid(), time.Time{}); found || id != "" {
		t.Fatalf("a tool that fails: %q %v", id, found)
	}
	if pids := agySessionPids(); len(pids) != 0 {
		t.Fatalf("a tool that fails listed %v", pids)
	}

	fake("exec /bin/sleep 30") // PATH is the fake alone: sleep by its full path
	start := time.Now()
	if id, found := agySession(os.Getpid(), time.Time{}); found || id != "" {
		t.Fatalf("a tool that stalls: %q %v", id, found)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("a stalled tool held the save loop for %s", d)
	}
	agyHeld = heldFiles{}
	start = time.Now()
	if pids := agySessionPids(); len(pids) != 0 {
		t.Fatalf("a tool that stalls listed %v", pids)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("a stalled tool held the listing for %s", d)
	}
}

// lsof is run from PATH, or from /usr/sbin, which is where macOS keeps it
// and which a session started by launchd does not always have on PATH.
func TestLsofPathFallsBackToUsrSbin(t *testing.T) {
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	if p := lsofPath(); p != "/usr/sbin/lsof" {
		t.Fatalf("with no lsof on PATH: %q", p)
	}
	bin := t.TempDir()
	own := filepath.Join(bin, "lsof")
	if err := os.WriteFile(own, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if p := lsofPath(); p != own {
		t.Fatalf("with one on PATH: %q, want %q", p, own)
	}
}

// Asking which files a process holds open runs a process of its own, so
// the answer is kept: it is asked again when the folder changed, a
// conversation taken or let go, when another process is asked about, or
// when the recheck has passed. A folder that is not there is no ids.
func TestHeldFilesAsksAgainOnlyWhenItMust(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// The same conversation on three descriptors, and one file in another
	// folder: one id, and not that one.
	held := []string{
		filepath.Join(dir, "conv-a.db"),
		filepath.Join(dir, "conv-a.db-wal"),
		filepath.Join(dir, "conv-a.db"),
		filepath.Join(other, "conv-elsewhere.db"),
	}
	calls := 0
	old := openFiles
	openFiles = func(int) []string { calls++; return held }
	t.Cleanup(func() { openFiles = old })

	var h heldFiles
	want := []string{"conv-a"}
	if ids := h.of(7, dir, time.Minute, agyConversationID); !slices.Equal(ids, want) || calls != 1 {
		t.Fatalf("first ask: %q after %d", ids, calls)
	}
	if ids := h.of(7, dir, time.Minute, agyConversationID); !slices.Equal(ids, want) || calls != 1 {
		t.Fatalf("asked again at once: %q after %d", ids, calls)
	}
	if ids := h.of(8, dir, time.Minute, agyConversationID); !slices.Equal(ids, want) || calls != 2 {
		t.Fatalf("another process: %q after %d", ids, calls)
	}
	touchFile(t, dir, "conv-b.db")
	if ids := h.of(8, dir, time.Minute, agyConversationID); !slices.Equal(ids, want) || calls != 3 {
		t.Fatalf("the folder changed: %q after %d", ids, calls)
	}
	if ids := h.of(8, dir, 0, agyConversationID); !slices.Equal(ids, want) || calls != 4 {
		t.Fatalf("the recheck passed: %q after %d", ids, calls)
	}
	if ids := h.of(8, filepath.Join(dir, "gone"), time.Minute, agyConversationID); ids != nil || calls != 4 {
		t.Fatalf("a folder that is not there: %q after %d", ids, calls)
	}
}
