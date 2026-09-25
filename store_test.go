package main

import (
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
