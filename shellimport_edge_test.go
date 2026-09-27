package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

// A log's name carries its host, pid and start time. Anything else in the
// shell folder is not a log, and is left alone.
func TestParseShellLogNames(t *testing.T) {
	for _, c := range []struct {
		name     string
		ok, done bool
		host     string
		pid      int
		stamp    string
	}{
		{"zsh-mac-12-345.log", true, false, "mac", 12, "345"},
		{"zsh-my-mac.local-12-345.done", true, true, "my-mac.local", 12, "345"},
		{"bash-mac-12-345.log", false, false, "", 0, ""},
		{"zsh-mac-12-345.txt", false, false, "", 0, ""},
		{"zsh-nodash.log", false, false, "", 0, ""},
		{"zsh-mac-12-.log", false, false, "", 0, ""},
		{"zsh-mac-12-34x.log", false, false, "", 0, ""},
		{"zsh-12-345.log", false, false, "", 0, ""}, // no host
		{"zsh-mac-pid-345.log", false, false, "", 0, ""},
		{"zsh-mac-0-345.log", false, false, "", 0, ""},
	} {
		l, ok := parseShellLog(filepath.Join("/x", c.name))
		if ok != c.ok || ok && (l.done != c.done || l.host != c.host || l.pid != c.pid || l.stamp != c.stamp) {
			t.Errorf("%s: ok=%v %+v", c.name, ok, l)
		}
	}
}

// Stamps are $EPOCHREALTIME without its dot: longer is later, and leading
// zeros count for nothing.
func TestShellStampsOrder(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"999", "1000", true},
		{"1000", "999", false},
		{"0999", "1000", true},
		{"1001", "1000", false},
		{"1000", "1001", true},
		{"1000", "1000", false},
	} {
		if got := before(c.a, c.b); got != c.want {
			t.Errorf("before(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

// A record that is not all there yet is waiting, not broken; one whose
// header or end is wrong is broken.
func TestNextShellRecord(t *testing.T) {
	for _, c := range []struct {
		in     string
		n      int
		broken bool
	}{
		{"b 1790000000 3\nabc\n", 19, false},
		{"b 1790000000 3\nab", 0, false},    // body still being written
		{"b 1790000000 3", 0, false},        // header still being written
		{"b 1790000000\nabc\n", 0, true},    // two fields
		{"bb 1790000000 3\nabc\n", 0, true}, // kind of two letters
		{"b 1790000000 -1\n\n", 0, true},
		{"b 1790000000 x\nabc\n", 0, true},
		{"b soon 3\nabc\n", 0, true},
		{"b 1790000000 3\nabcd\n", 0, true}, // longer than its size says
	} {
		kind, _, text, n, err := nextShellRecord([]byte(c.in))
		if n != c.n || (err != nil) != c.broken || n > 0 && (kind != 'b' || text != "abc") {
			t.Errorf("%q: kind %q text %q n %d err %v", c.in, kind, text, n, err)
		}
	}
}

// A state file that cannot be read is read again from the start of the
// logs, writing the same files; one from a newer unsent is left alone with
// its shell's logs.
func TestImportStateFileItCannotUse(t *testing.T) {
	st := testStore(t)
	folder := t.TempDir()
	pid := deadPid(t)
	log := writeShellLog(t, st, shellLogName(pid, 1, ".log"),
		zrec{"v", "1"}, zrec{"i", folder}, zrec{"b", "left at the prompt"})
	state := filepath.Join(st.dir, "shell", "zsh-"+shellHost()+"-"+strconv.Itoa(pid)+".import")
	if err := os.WriteFile(state, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	st.importShells()
	if o := shellOrphans(st); len(o) != 1 || o[0].Draft != "left at the prompt" || exists(log) || exists(state) {
		t.Fatalf("orphans %v, log there %v, state there %v", o, exists(log), exists(state))
	}

	st = testStore(t)
	pid = deadPid(t)
	log = writeShellLog(t, st, shellLogName(pid, 1, ".log"),
		zrec{"v", "1"}, zrec{"i", folder}, zrec{"b", "a newer unsent's line"})
	state = filepath.Join(st.dir, "shell", "zsh-"+shellHost()+"-"+strconv.Itoa(pid)+".import")
	if err := os.WriteFile(state, []byte(`{"format": 999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st.importShells()
	if o := shellOrphans(st); len(o) != 0 || !exists(log) || !exists(state) {
		t.Fatalf("a newer unsent's shell was imported: orphans %v, log there %v", o, exists(log))
	}
}

// Files in the shell folder that are neither logs nor import states are
// left alone, and the logs beside them are still imported.
func TestImportIgnoresStrayFiles(t *testing.T) {
	st := testStore(t)
	folder := t.TempDir()
	log := writeShellLog(t, st, shellLogName(deadPid(t), 1, ".log"),
		zrec{"v", "1"}, zrec{"i", folder}, zrec{"b", "kept"})
	dir := filepath.Join(st.dir, "shell")
	stray := []string{"zsh-x.import", "zsh--3.import", "zsh-mac-0.import", "zsh-mac-p.import", "notes.txt"}
	for _, n := range stray {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600)
	}
	st.importShells()
	if o := shellOrphans(st); len(o) != 1 || o[0].Draft != "kept" || exists(log) {
		t.Fatalf("orphans %v, log there %v", o, exists(log))
	}
	for _, n := range stray {
		if !exists(filepath.Join(dir, n)) {
			t.Errorf("%s was removed", n)
		}
	}
}

// The promise: when the store cannot take a line, the log that holds it
// stays, and the next import, once the store can write, takes every line.
// Each case fails a different write: history (a cleared line), the drafts
// (the line left at the prompt), the sent log (a line Enter ran, kept
// under log, which falls back to history) and the lock the import takes.
// After every import, either the log is still there or every line is in
// the store.
func TestImportKeepsTheLogWhenTheStoreFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only folders this test relies on")
	}
	t.Setenv("UNSENT_ON_SEND_ZSH", "log")
	missing := func(st *store) []string {
		var out []string
		s, h, o := sentTexts(st), shellHistory(st), shellOrphans(st)
		if !slices.Contains(s, "zsh: echo ran") && !slices.Contains(h, "echo ran") {
			out = append(out, "echo ran")
		}
		if !slices.Contains(h, "cleared line") {
			out = append(out, "cleared line")
		}
		if len(o) != 1 || o[0].Draft != "left at the prompt" {
			out = append(out, "left at the prompt")
		}
		return out
	}
	for _, folder := range []string{"history", "drafts", "sent", "shell"} {
		t.Run(folder, func(t *testing.T) {
			st := testStore(t)
			cwd := t.TempDir()
			log := writeShellLog(t, st, shellLogName(deadPid(t), 1, ".log"),
				zrec{"v", "1"}, zrec{"i", cwd},
				zrec{"b", "echo ran"}, zrec{"s", "echo ran"},
				zrec{"i", cwd},
				zrec{"b", "cleared line"},
				zrec{"i", cwd},
				zrec{"b", "left at the prompt"})
			ro := filepath.Join(st.dir, folder)
			if err := os.MkdirAll(ro, 0o700); err != nil {
				t.Fatal(err)
			}
			os.Chmod(ro, 0o500)
			defer os.Chmod(ro, 0o700)
			st.importShells()
			if m := missing(st); !exists(log) && len(m) > 0 {
				t.Fatalf("the log went while the store could not take %q", m)
			}
			os.Chmod(ro, 0o700)
			st.importShells()
			if exists(log) {
				t.Fatal("the log stayed after the store could write again")
			}
			if m := missing(st); len(m) > 0 {
				t.Fatalf("lost %q", m)
			}
		})
	}
}
