package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// readRaw reads a raw log back with the replay tests' own reader: the
// directions in order, the keys and the output joined, and the sizes.
func readRaw(t *testing.T, base string) (dirs string, keys, output []byte, meta rawMeta) {
	t.Helper()
	data, err := os.ReadFile(base + ".rec")
	if err != nil {
		t.Fatal(err)
	}
	eachChunk(t, data, func(_ time.Time, dir byte, chunk []byte) {
		dirs += string(dir)
		switch dir {
		case 'i':
			keys = append(keys, chunk...)
		case 'o':
			output = append(output, chunk...)
		}
	})
	b, err := os.ReadFile(base + ".json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatal(err)
	}
	return dirs, keys, output, meta
}

// private fails unless every file named is readable by its owner only.
func private(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s is %v, want 0600", filepath.Base(p), fi.Mode().Perm())
		}
	}
}

func TestRawLog(t *testing.T) {
	base := filepath.Join(t.TempDir(), "x")
	before := time.Now().Truncate(time.Microsecond)
	r, err := openRaw(base, "claude", "2.1.282")
	if err != nil {
		t.Fatal(err)
	}
	r.resize(120, 40)
	r.record('i', []byte("a"))
	r.record('i', nil) // an empty read is not a chunk
	r.record('o', []byte("\x1b[2Ja"))
	r.resize(80, 24)
	r.close()
	after := time.Now()
	r.record('o', []byte("after the end")) // must not panic
	// The replay drives its save tick from these times.
	data, _ := os.ReadFile(base + ".rec")
	last := before
	eachChunk(t, data, func(at time.Time, dir byte, _ []byte) {
		if at.Before(last) || at.After(after) {
			t.Errorf("chunk %c at %v, want from %v to %v", dir, at, last, after)
		}
		last = at
	})
	dirs, keys, output, meta := readRaw(t, base)
	if dirs != "sioe" || string(keys) != "a" || string(output) != "\x1b[2Ja" {
		t.Fatalf("chunks %q, keys %q, output %q", dirs, keys, output)
	}
	if meta.Agent != "claude" || meta.Version != "2.1.282" || len(meta.Sizes) != 2 || meta.Sizes[1].Cols != 80 || meta.Sizes[1].Rows != 24 {
		t.Fatalf("meta %+v", meta)
	}
	private(t, base+".rec", base+".json")
	if _, err := openRaw(base, "claude", ""); err == nil {
		t.Fatal("a second log overwrote the first")
	}
	var none *rawLog // no log asked for: every call does nothing
	none.record('i', []byte("a"))
	none.resize(1, 1)
	none.close()
}

// Without UNSENT_DEBUG_DIR nothing but the drafts is written; with it the
// session's keys and output are in its folder, 0600, and replay.
func TestWrapLogsRawBytesOnlyWhenAsked(t *testing.T) {
	t.Chdir(t.TempDir())
	typed := func(type_ func(string)) { type_("hello"); type_("\x1b\rworld"); type_("\x04") }

	t.Setenv("UNSENT_DEBUG_DIR", "")
	_, st := runWrapped(t, typed)
	var extra []string
	for _, dir := range []string{".", st.dir} {
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && !strings.HasSuffix(p, ".json") && !strings.HasSuffix(p, ".lock") {
				extra = append(extra, p)
			}
			return nil
		})
	}
	if len(extra) > 0 {
		t.Fatalf("files written without UNSENT_DEBUG_DIR: %q", extra)
	}

	debug := t.TempDir()
	t.Setenv("UNSENT_DEBUG_DIR", debug)
	runWrapped(t, func(type_ func(string)) {
		type_("hello")
		// The window grows: os.Stdin is the wrapper's terminal here.
		pty.Setsize(os.Stdin, &pty.Winsize{Cols: 120, Rows: 40})
		syscall.Kill(os.Getpid(), syscall.SIGWINCH)
		type_("\x1b\rworld")
		type_("\x04")
	})
	recs, _ := filepath.Glob(filepath.Join(debug, "raw-*.rec"))
	if len(recs) != 1 {
		t.Fatalf("raw logs %q", recs)
	}
	base := strings.TrimSuffix(recs[0], ".rec")
	dirs, keys, output, meta := readRaw(t, base)
	if string(keys) != "hello\x1b\rworld\x04" || !strings.HasPrefix(dirs, "s") || !strings.HasSuffix(dirs, "e") {
		t.Fatalf("keys %q, chunks %q", keys, dirs)
	}
	if len(meta.Sizes) != 2 || meta.Sizes[0].Cols != 100 || meta.Sizes[0].Rows != 30 || meta.Sizes[1].Cols != 120 || meta.Sizes[1].Rows != 40 || meta.Agent != "claude" {
		t.Fatalf("meta %+v", meta)
	}
	private(t, base+".rec", base+".json")
	// The output alone draws the draft the session saved.
	if drafts := replayBytes(t, 100, 30, output); drafts[len(drafts)-1] != "hello\nworld" {
		t.Fatalf("replayed drafts %q", drafts)
	}
}

// unsent capture records a bundle the replay tests can read: at each Ctrl+G
// the draft replayed from the record is the editor copy taken there.
func TestCapture(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, k := range []string{"EDITOR", "VISUAL", "UNSENT_CAPTURE_TO"} {
		t.Setenv(k, "") // put back after the capture sets them
	}
	_, st, _, said := runWrappedOut(t, "claude", []string{"capture", "claude"}, func(type_ func(string)) {
		type_("hello")
		type_("\x07")
		type_(" there")
		type_("\x07")
		type_("\x04")
	})
	recs, _ := filepath.Glob(filepath.Join("testdata", "claude", "9.9.9", "capture-*.rec"))
	if len(recs) != 1 {
		t.Fatalf("captures %q, stderr %q", recs, said)
	}
	base := strings.TrimSuffix(recs[0], ".rec")
	absBase, err := filepath.Abs(base)
	if err != nil {
		t.Fatal(err)
	}
	// The message and $UNSENT_CAPTURE_TO carry the absolute path: the editor
	// script resolves it against the agent's working directory at Ctrl+G
	// time, which need not be the one capture started in.
	if !strings.Contains(said, "unsent: captured claude 9.9.9 in "+absBase+".rec") || !strings.Contains(said, "2 editor copies") {
		t.Fatalf("stderr %q", said)
	}
	_, keys, _, meta := readRaw(t, base)
	if string(keys) != "hello\x07 there\x07\x04" || meta.Version != "9.9.9" || meta.Sizes[0].Cols != 100 {
		t.Fatalf("keys %q, meta %+v", keys, meta)
	}
	copies := []string{base + ".editor-1.txt", base + ".editor-2.txt"}
	private(t, append(copies, base+".rec", base+".json")...)

	data, _ := os.ReadFile(base + ".rec")
	s := &session{
		screen: vt.NewEmulator(meta.Sizes[0].Cols, meta.Sizes[0].Rows),
		rec:    newRecord([]string{"claude"}, "/w"),
		store:  testStore(t),
		pastes: &pasteTracker{},
		prof:   &claude,
	}
	go io.Copy(io.Discard, s.screen)
	checked := 0
	eachChunk(t, data, func(_ time.Time, dir byte, chunk []byte) {
		switch dir {
		case 'i':
			if bytes.Equal(chunk, []byte{0x07}) {
				want, err := os.ReadFile(copies[checked])
				if err != nil || s.rec.Draft != string(want) {
					t.Fatalf("at Ctrl+G %d: draft %q, editor copy %q (%v)", checked+1, s.rec.Draft, want, err)
				}
				checked++
			}
			s.input(chunk)
		case 'o':
			s.write(chunk)
			s.save()
		}
	})
	if checked != 2 {
		t.Fatalf("%d Ctrl+G checks", checked)
	}
	// The session saved drafts as it always does.
	if rs := st.orphans(); len(rs) != 1 || rs[0].Draft != "hello there" {
		t.Fatalf("orphans %+v", rs)
	}
}

// A capture asks the command its version only when the command is the agent:
// a launcher UNSENT_AGENT names for another agent would answer with its own.
func TestCaptureAsksOnlyTheAgentItsVersion(t *testing.T) {
	for _, c := range []struct{ agentEnv, want string }{
		{"", "testdata/launcher/9.9.9"},
		{"gemini", "testdata/gemini/unknown"},
	} {
		t.Run(c.want, func(t *testing.T) {
			t.Chdir(t.TempDir())
			for _, k := range []string{"EDITOR", "VISUAL", "UNSENT_CAPTURE_TO"} {
				t.Setenv(k, "")
			}
			t.Setenv("UNSENT_AGENT", c.agentEnv)
			_, _, _, said := runWrappedOut(t, "launcher", []string{"capture", "launcher"}, func(type_ func(string)) { type_("\x04") })
			if recs, _ := filepath.Glob(filepath.Join(c.want, "capture-*.rec")); len(recs) != 1 {
				t.Fatalf("captures in %s: %q, stderr %q", c.want, recs, said)
			}
		})
	}
}

// A capture needs a terminal and a command; without them it records nothing.
func TestCaptureRefuses(t *testing.T) {
	t.Chdir(t.TempDir())
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	var said strings.Builder
	for _, args := range [][]string{{"capture"}, {"capture", "sh"}} {
		if code := run(args, io.Discard, &said); code != 2 {
			t.Fatalf("%q: exit %d", args, code)
		}
	}
	// On a terminal with stderr on a file, the wrapper would hand over and
	// record nothing, so the capture refuses too.
	user, tty, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { user.Close(); tty.Close() }()
	errLog, err := os.Create(filepath.Join(t.TempDir(), "err.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer errLog.Close()
	oldExec := execAgent
	execAgent = func(string, []string) error { return nil } // a regression fails, not execs
	defer func() { execAgent = oldExec }()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = tty, tty, errLog
	code := run([]string{"capture", "sh"}, io.Discard, &said)
	os.Stdout, os.Stderr = oldOut, oldErr
	if code != 2 {
		t.Fatalf("capture with stderr on a file: exit %d", code)
	}
	if _, err := os.Stat("testdata"); !os.IsNotExist(err) {
		t.Fatalf("a refused capture wrote testdata: %v", err)
	}
	if strings.Count(said.String(), "capture needs a terminal") != 2 {
		t.Fatalf("stderr %q", said.String())
	}
}
