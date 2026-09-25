package main

import (
	"fmt"
	"os"
	"path/filepath"
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

func TestStoreArchiveAndPrune(t *testing.T) {
	st := testStore(t)
	r := newRecord([]string{"claude"}, "/work")
	st.archive(r) // empty: nothing to keep
	r.Draft = "draft"
	for i := 0; i < historyLimit+5; i++ {
		r.PID = i // distinct names within the same millisecond
		st.archive(r)
	}
	names, _ := filepath.Glob(filepath.Join(st.dir, "history", "*.json"))
	if len(names) != historyLimit {
		t.Fatalf("history holds %d, want %d", len(names), historyLimit)
	}
	if got := st.load(true); len(got) != historyLimit {
		t.Fatalf("load %d", len(got))
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
