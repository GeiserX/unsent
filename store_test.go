package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *store {
	t.Helper()
	t.Setenv("UNSENT_HOME", t.TempDir())
	st, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// replayStore is a store of its own for a replay, which may run in
// parallel with others: t.Setenv is not allowed there, and a replay reads
// no shell lines.
func replayStore(t *testing.T) *store {
	t.Helper()
	st, err := makeStoreAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestStoreLiveSessionIsNotAnOrphan(t *testing.T) {
	st := testStore(t)
	r := newRecord([]string{"claude"}, "/work")
	r.Draft = "half a thought"
	if err := st.hold(r); err != nil {
		t.Fatal(err)
	}
	if err := st.write(r); err != nil {
		t.Fatal(err)
	}
	if !st.alive(r) {
		t.Fatal("held session reads as dead")
	}
	if len(st.orphans()) != 0 {
		t.Fatal("live session listed as an orphan")
	}
	// The process dies: the kernel drops the lock.
	st.lock.Close()
	if st.alive(r) {
		t.Fatal("released session reads as alive")
	}
	got := st.orphans()
	if len(got) != 1 || got[0].Draft != "half a thought" {
		t.Fatalf("orphans %+v", got)
	}
}

func TestStoreCleansDeadEmptySessions(t *testing.T) {
	st := testStore(t)
	r := newRecord([]string{"claude"}, "/work")
	if err := st.write(r); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(st.lockPath(r.ID), nil, 0o600)
	if len(st.orphans()) != 0 {
		t.Fatal("empty draft listed")
	}
	if _, err := os.Stat(st.draftPath(r.ID)); !os.IsNotExist(err) {
		t.Fatal("empty dead session not cleaned up")
	}
	if _, err := os.Stat(st.lockPath(r.ID)); !os.IsNotExist(err) {
		t.Fatal("lock file not cleaned up")
	}
}

// seedFiles writes n plain files named by name(i) into dir, holding data,
// each i minutes older than the one before: a full history in moments,
// where writing each through the store would cost a durable write apiece.
func seedFiles(t *testing.T, dir string, n int, data []byte, name func(int) string) {
	t.Helper()
	for i := range n {
		path := filepath.Join(dir, name(i))
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Duration(n-i) * time.Minute)
		os.Chtimes(path, old, old)
	}
}

func TestStoreArchiveAndPrune(t *testing.T) {
	st := testStore(t)
	r := newRecord([]string{"claude"}, "/work")
	st.archive(r) // empty: nothing to keep
	old, _ := json.Marshal(&record{Draft: "old"})
	seed := func(i int) string { return fmt.Sprintf("20000101-000000.%06d-1.json", i) }
	seedFiles(t, filepath.Join(st.dir, "history"), historyLimit+3, old, seed)
	r.Draft = "draft"
	for pid := range 2 {
		r.PID = pid // distinct names within the same microsecond
		st.archive(r)
	}
	names, _ := filepath.Glob(filepath.Join(st.dir, "history", "*.json"))
	if len(names) != historyLimit {
		t.Fatalf("history holds %d, want %d", len(names), historyLimit)
	}
	for i, want := range map[int]bool{0: false, 4: false, 5: true, historyLimit + 2: true} {
		if got := exists(filepath.Join(st.dir, "history", seed(i))); got != want {
			t.Errorf("archive %d kept %v, want %v", i, got, want)
		}
	}
	got := st.load(true)
	if len(got) != historyLimit || got[0].Draft != "draft" || got[1].Draft != "draft" {
		t.Fatalf("load %d, newest %q", len(got), got[0].Draft)
	}
}

func TestStoreLoadOrder(t *testing.T) {
	st := testStore(t)
	old := newRecord([]string{"claude"}, "/a")
	old.ID, old.Draft, old.Updated = "old", "old", time.Now().Add(-time.Hour)
	young := newRecord([]string{"claude"}, "/b")
	young.ID, young.Draft = "young", "young"
	st.write(old)
	st.write(young)
	os.WriteFile(filepath.Join(st.dir, "drafts", "broken.json"), []byte("{"), 0o600)
	got := st.load(false)
	if len(got) != 2 || got[0].ID != "young" {
		t.Fatalf("order %v", got)
	}
}

func TestStateDir(t *testing.T) {
	t.Setenv("UNSENT_HOME", "")
	t.Setenv("XDG_STATE_HOME", "/x")
	if d, _ := stateDir(); d != "/x/unsent" {
		t.Fatal(d)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/h")
	if d, _ := stateDir(); d != "/h/.local/state/unsent" {
		t.Fatal(d)
	}
}

func TestStoreWarnOnce(t *testing.T) {
	st := testStore(t)
	st.warn(os.ErrPermission)
	if !st.warned {
		t.Fatal("not marked")
	}
	st.warn(os.ErrPermission)
}

func TestWriteDurableFailsOnMissingDir(t *testing.T) {
	if err := writeDurable(filepath.Join(t.TempDir(), "nope", "x.json"), &record{}); err == nil {
		t.Fatal("no error")
	}
}

func TestStoreLocksDownAnExistingFolder(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0o755)
	t.Setenv("UNSENT_HOME", dir)
	if _, err := openStore(); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

func TestStoreSweepsOldTempFiles(t *testing.T) {
	st := testStore(t)
	old := filepath.Join(st.dir, "drafts", ".tmp-old")
	fresh := filepath.Join(st.dir, "history", ".tmp-fresh")
	os.WriteFile(old, nil, 0o600)
	os.WriteFile(fresh, nil, 0o600)
	os.Chtimes(old, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour))
	st.orphans()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old temp file kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("a write in progress was swept")
	}
}

func TestStoreTrustsTheFileNameNotTheContent(t *testing.T) {
	st := testStore(t)
	victim := newRecord([]string{"claude"}, "/w")
	victim.ID, victim.Draft = "victim", "keep me"
	st.write(victim)
	os.WriteFile(st.lockPath("victim"), nil, 0o600)
	// A record whose content names another session's ID.
	liar := newRecord([]string{"claude"}, "/w")
	liar.ID = "victim"
	writeDurable(filepath.Join(st.dir, "drafts", "liar.json"), liar)
	st.orphans() // cleans up the empty "liar" session
	if _, err := os.Stat(st.draftPath("victim")); err != nil {
		t.Fatal("cleaning one record removed another's file")
	}
	if _, err := os.Stat(st.draftPath("liar")); !os.IsNotExist(err) {
		t.Fatal("the empty dead session was not cleaned")
	}
}

func TestStoreArchiveReportsFailure(t *testing.T) {
	st := testStore(t)
	r := newRecord([]string{"claude"}, "/w")
	r.Draft = "text"
	os.RemoveAll(filepath.Join(st.dir, "history"))
	if err := st.archive(r); err == nil {
		t.Fatal("archive into a missing folder reported success")
	}
}

func TestStoreVersionsAreCappedAndDropped(t *testing.T) {
	st := testStore(t)
	r := newRecord([]string{"claude"}, "/w")
	r.ID = "sess"
	for i := 0; i < versionsPerSession+5; i++ {
		r.Draft = fmt.Sprintf("version %d", i)
		if err := st.keepVersion(r); err != nil {
			t.Fatal(err)
		}
	}
	other := newRecord([]string{"claude"}, "/w")
	other.ID, other.Draft = "other", "someone else's"
	st.keepVersion(other)
	st.archive(other) // real history, never trimmed by versions
	count := func(p string) int { n, _ := filepath.Glob(filepath.Join(st.dir, "history", p)); return len(n) }
	if n := count("v-sess-*.json"); n != versionsPerSession {
		t.Fatalf("%d versions kept, want %d", n, versionsPerSession)
	}
	st.dropVersions(r)
	if count("v-sess-*.json") != 0 || count("v-other-*.json") != 1 || count("2*.json") != 1 {
		t.Fatal("dropVersions touched the wrong files")
	}
}

func TestStoreVersionsGlobalCap(t *testing.T) {
	st := testStore(t)
	seed := func(i int) string { return fmt.Sprintf("v-s%03d-20000101-000000.000000.json", i) }
	seedFiles(t, filepath.Join(st.dir, "history"), versionsLimit+2, []byte(`{"draft":"old","version":true}`), seed)
	r := newRecord([]string{"claude"}, "/w")
	r.ID, r.Draft = "new", "draft"
	if err := st.keepVersion(r); err != nil {
		t.Fatal(err)
	}
	names, _ := filepath.Glob(filepath.Join(st.dir, "history", "v-*.json"))
	if len(names) != versionsLimit {
		t.Fatalf("%d versions kept, want %d", len(names), versionsLimit)
	}
	for i, want := range map[int]bool{0: false, 2: false, 3: true, versionsLimit + 1: true} {
		if got := exists(filepath.Join(st.dir, "history", seed(i))); got != want {
			t.Errorf("version %d kept %v, want %v", i, got, want)
		}
	}
	if kept, _ := filepath.Glob(filepath.Join(st.dir, "history", "v-new-*.json")); len(kept) != 1 {
		t.Fatal("the newest version was trimmed")
	}
}

func TestStoreWriteFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only folders this test relies on")
	}
	st := testStore(t)
	r := newRecord([]string{"claude"}, "/w")
	r.Draft = "text"
	hist := filepath.Join(st.dir, "history")
	os.Chmod(hist, 0o500)
	defer os.Chmod(hist, 0o700)
	if err := st.keepVersion(r); err == nil {
		t.Fatal("a version written into a read-only folder")
	}
	if err := st.keepVersion(&record{ID: "empty"}); err != nil {
		t.Fatal("an empty draft needs no version")
	}
	// A state folder that cannot be created.
	file := filepath.Join(t.TempDir(), "a-file")
	os.WriteFile(file, nil, 0o600)
	t.Setenv("UNSENT_HOME", filepath.Join(file, "state"))
	if _, err := openStore(); err == nil {
		t.Fatal("opened a store under a file")
	}
}

func TestStoreHoldTwice(t *testing.T) {
	st := testStore(t)
	r := newRecord([]string{"claude"}, "/w")
	if err := st.hold(r); err != nil {
		t.Fatal(err)
	}
	other := &store{dir: st.dir}
	if err := other.hold(r); err == nil {
		t.Fatal("a second holder took a lock that is held")
	}
	st.release()
	if err := other.hold(r); err != nil {
		t.Fatalf("lock not free after release: %v", err)
	}
	other.release()
}

// A draft file written before records had a format field, byte for byte in
// the layout unsent used then.
const formatlessRecord = `{
  "id": "20260901-120000-4242",
  "command": [
    "claude"
  ],
  "cwd": "/work",
  "pid": 4242,
  "started": "2026-09-01T12:00:00Z",
  "updated": "2026-09-01T12:05:00Z",
  "draft": "the old draft\nsecond line",
  "pastes": [
    "pasted text"
  ]
}`

func TestStoreLoadsFileWithoutFormat(t *testing.T) {
	st := testStore(t)
	id := "20260901-120000-4242"
	os.WriteFile(st.draftPath(id), []byte(formatlessRecord), 0o600)
	os.WriteFile(filepath.Join(st.dir, "history", "20260901-120500.000000-4242.json"), []byte(formatlessRecord), 0o600)

	got := st.orphans()
	if len(got) != 1 || got[0].Draft != "the old draft\nsecond line" || got[0].Cwd != "/work" ||
		len(got[0].Pastes) != 1 || got[0].Format != 0 {
		t.Fatalf("orphans %+v", got)
	}
	// Written before records named their agent: it comes from the command.
	if got[0].Agent != "" || got[0].agent() != "claude" {
		t.Fatalf("agent %q, fallback %q", got[0].Agent, got[0].agent())
	}
	if _, err := os.Stat(st.draftPath(id)); err != nil {
		t.Fatalf("old draft file removed: %v", err)
	}
	if all := st.load(true); len(all) != 2 || all[0].Draft != "the old draft\nsecond line" || all[1].Draft != all[0].Draft {
		t.Fatalf("load %+v", all)
	}
}

func TestStoreWritesFormat(t *testing.T) {
	st := testStore(t)
	r := newRecord([]string{"claude"}, "/work")
	r.Draft = "draft"
	if err := st.write(r); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	data, _ := os.ReadFile(st.draftPath(r.ID))
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["format"] != float64(recordFormat) {
		t.Fatalf("format %v, want %d", raw["format"], recordFormat)
	}
	// Copies in history keep it.
	st.archive(r)
	st.keepVersion(r)
	for _, h := range st.load(true) {
		if h.Format != recordFormat {
			t.Fatalf("record %s has format %d", h.ID, h.Format)
		}
	}
}

func TestStoreKeepsNewerFormat(t *testing.T) {
	st := testStore(t)
	// A later unsent renamed the draft field; this build reads it as empty.
	id := "20270101-120000-4242"
	newer := fmt.Sprintf(`{"format": %d, "id": %q, "cwd": "/work", "text": "a draft this build cannot see"}`, recordFormat+1, id)
	os.WriteFile(st.draftPath(id), []byte(newer), 0o600)
	os.WriteFile(st.lockPath(id), nil, 0o600)
	if got := st.orphans(); len(got) != 0 {
		t.Fatalf("orphans %+v", got)
	}
	if _, err := os.Stat(st.draftPath(id)); err != nil {
		t.Fatalf("a newer draft file was deleted as empty: %v", err)
	}
	// The same file at this build's format really is empty, and goes.
	os.WriteFile(st.draftPath(id), []byte(strings.Replace(newer, fmt.Sprint(recordFormat+1), fmt.Sprint(recordFormat), 1)), 0o600)
	st.orphans()
	if _, err := os.Stat(st.draftPath(id)); !os.IsNotExist(err) {
		t.Fatal("control: an empty current-format file was kept")
	}
}

func TestRecordAgent(t *testing.T) {
	for _, c := range []struct {
		r    record
		want string
	}{
		{record{Agent: "claude", Command: []string{"npx"}}, "claude"},
		{record{Command: []string{"/opt/homebrew/bin/claude", "--resume"}}, "claude"},
		{record{Command: []string{"npx", "@anthropic-ai/claude-code"}}, "npx"},
		{record{}, ""},
	} {
		if got := c.r.agent(); got != c.want {
			t.Fatalf("agent of %+v = %q, want %q", c.r, got, c.want)
		}
	}
}

func TestAgentFor(t *testing.T) {
	t.Setenv("UNSENT_AGENT", "")
	if got := agentFor("", "/usr/local/bin/claude"); got != "claude" {
		t.Fatalf("base name: %q", got)
	}
	if got := agentFor("", "npx"); got != "npx" {
		t.Fatalf("unknown command: %q", got)
	}
	t.Setenv("UNSENT_AGENT", "claude")
	if got := agentFor("", "node"); got != "claude" {
		t.Fatalf("UNSENT_AGENT: %q", got)
	}
	// --as wins over the variable.
	if got := agentFor("codex", "node"); got != "codex" {
		t.Fatalf("--as: %q", got)
	}
	if p := profileFor(agentFor("", "node")); p != &claude {
		t.Fatal("UNSENT_AGENT did not pick the profile")
	}
	// A command that names a known agent keeps it: an exported variable
	// must not tag it as another agent. --as still wins.
	t.Setenv("UNSENT_AGENT", "codex")
	if got := agentFor("", "/usr/local/bin/claude"); got != "claude" {
		t.Fatalf("known base name under UNSENT_AGENT=codex: %q", got)
	}
	if got := agentFor("codex", "claude"); got != "codex" {
		t.Fatalf("--as over a known base name: %q", got)
	}
}
