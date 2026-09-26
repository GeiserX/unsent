package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seed(t *testing.T, drafts ...string) *store {
	t.Helper()
	st := testStore(t)
	for i, d := range drafts {
		r := newRecord([]string{"claude"}, "/work")
		r.ID = "seed-" + string(rune('a'+i))
		r.Draft = d
		r.Updated = time.Now().Add(-time.Duration(i) * time.Minute)
		r.Ended = time.Now()
		if err := st.write(r); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func runCLI(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestCLIHelpAndVersion(t *testing.T) {
	if code, _, e := runCLI(); code != 2 || !strings.Contains(e, "Usage") {
		t.Fatal("no args")
	}
	if code, o, _ := runCLI("--help"); code != 0 || !strings.Contains(o, "AutoRecover") {
		t.Fatal("help")
	}
	if code, o, _ := runCLI("version"); code != 0 || !strings.HasPrefix(o, "unsent ") {
		t.Fatal("version")
	}
	if code, _, _ := runCLI("--"); code != 2 {
		t.Fatal("bare --")
	}
}

func TestCLIListAndShow(t *testing.T) {
	seed(t, "newest draft", "older draft\nwith two lines")
	code, out, _ := runCLI("list")
	if code != 0 || !strings.Contains(out, "  1  ") || !strings.Contains(out, "newest draft") ||
		!strings.Contains(out, "older draft with two lines") {
		t.Fatalf("list:\n%s", out)
	}
	if _, out, _ := runCLI("show"); out != "newest draft\n" {
		t.Fatalf("show %q", out)
	}
	if _, out, _ := runCLI("show", "2"); out != "older draft\nwith two lines\n" {
		t.Fatalf("show 2 %q", out)
	}
	if code, _, _ := runCLI("show", "9"); code != 2 {
		t.Fatal("show 9")
	}
}

func TestCLIEmpty(t *testing.T) {
	testStore(t)
	if _, out, _ := runCLI("list", "--all"); !strings.Contains(out, "No drafts") {
		t.Fatal(out)
	}
	if code, _, _ := runCLI("restore"); code != 1 {
		t.Fatal("restore with nothing")
	}
}

func TestCLIRestore(t *testing.T) {
	st := seed(t, "bring me back")
	bin := t.TempDir()
	clip := filepath.Join(t.TempDir(), "clip")
	script := "#!/bin/sh\n/bin/cat > " + clip + "\n"
	for _, name := range []string{"pbcopy", "wl-copy", "xclip", "xsel", "clip.exe"} {
		os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755)
	}
	t.Setenv("PATH", bin)
	code, _, errOut := runCLI("restore")
	if code != 0 || !strings.Contains(errOut, "Copied 1 line") {
		t.Fatalf("restore %d %q", code, errOut)
	}
	if b, _ := os.ReadFile(clip); string(b) != "bring me back" {
		t.Fatalf("clipboard %q", b)
	}
	if len(st.orphans()) != 0 {
		t.Fatal("restored draft still listed")
	}
	if _, out, _ := runCLI("list", "--all"); !strings.Contains(out, "bring me back") {
		t.Fatal("restored draft missing from history")
	}
}

func TestCLIRestoreWithoutClipboard(t *testing.T) {
	seed(t, "no clipboard here")
	t.Setenv("PATH", t.TempDir())
	code, out, errOut := runCLI("restore")
	if code != 0 || out != "no clipboard here\n" || !strings.Contains(errOut, "no clipboard") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
}

func TestNoticeOrphans(t *testing.T) {
	st := seed(t, "one", "two")
	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w
	noticeOrphans(st, "/work")
	noticeOrphans(st, "/elsewhere")
	w.Close()
	os.Stderr = old
	var b bytes.Buffer
	b.ReadFrom(r)
	if got := b.String(); strings.Count(got, "\n") != 1 || !strings.Contains(got, "(and 1 more)") {
		t.Fatalf("notice %q", got)
	}
}

func TestFormatting(t *testing.T) {
	now := time.Now()
	if !strings.HasSuffix(when(now), "today") {
		t.Fatal(when(now))
	}
	if got := when(now.AddDate(0, 0, -1)); !strings.HasSuffix(got, "yesterday") {
		t.Fatal(got)
	}
	if got := when(now.AddDate(0, 0, -10)); len(got) != len("2006-01-02 15:04") {
		t.Fatal(got)
	}
	if lines("a") != "1 line" || lines("a\nb") != "2 lines" {
		t.Fatal("lines")
	}
	if got := preview("a  b\nc "+strings.Repeat("x", 100), 10); got != "a b c xxx…" {
		t.Fatal(got)
	}
	home, _ := os.UserHomeDir()
	if got := shortPath(filepath.Join(home, "p"), 30); got != "~/p" {
		t.Fatal(got)
	}
	if got := shortPath("/a/very/long/path/indeed", 8); got != "…/indeed" {
		t.Fatal(got)
	}
	if !samePath("/tmp", "/tmp") || samePath("/tmp", "/usr") {
		t.Fatal("samePath")
	}
}

func TestCLIRestorePrefersThisFolder(t *testing.T) {
	st := testStore(t)
	here, _ := os.Getwd()
	for i, c := range []struct{ cwd, draft string }{{"/elsewhere", "newest, other folder"}, {here, "this folder's draft"}} {
		r := newRecord([]string{"claude"}, c.cwd)
		r.ID, r.Draft, r.Ended = "r"+string(rune('a'+i)), c.draft, time.Now()
		r.Updated = time.Now().Add(-time.Duration(i) * time.Minute)
		st.write(r)
	}
	if _, out, _ := runCLI("show"); out != "this folder's draft\n" {
		t.Fatalf("show %q", out)
	}
	if _, out, _ := runCLI("show", "1"); out != "newest, other folder\n" {
		t.Fatalf("show 1 %q", out)
	}
}

func TestCLIRestoreKeepsTheDraftIfHistoryFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only folder this test relies on")
	}
	st := seed(t, "precious")
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "pbcopy"), []byte("#!/bin/sh\n/bin/cat >/dev/null\n"), 0o755)
	os.WriteFile(filepath.Join(bin, "wl-copy"), []byte("#!/bin/sh\n/bin/cat >/dev/null\n"), 0o755)
	t.Setenv("PATH", bin)
	hist := filepath.Join(st.dir, "history")
	os.Chmod(hist, 0o500) // the history copy cannot be written
	defer os.Chmod(hist, 0o700)
	if code, _, _ := runCLI("restore"); code != 0 {
		t.Fatal("restore failed")
	}
	if len(st.orphans()) != 1 {
		t.Fatal("the only copy of the draft was removed")
	}
}

func TestCLIShowPrintsUnplacedPastes(t *testing.T) {
	st := testStore(t)
	r := newRecord([]string{"claude"}, "/w")
	r.ID, r.Draft, r.Ended = "p", "see [Pasted text #1 +3 lines]", time.Now()
	r.Pastes = []string{"a\nb\nc\nd\ne"}
	st.write(r)
	_, out, _ := runCLI("show")
	if !strings.Contains(out, "could not be placed") || !strings.Contains(out, "a\nb\nc\nd\ne") {
		t.Fatalf("show %q", out)
	}
}

func TestCLIListMarksEarlierVersions(t *testing.T) {
	st := seed(t, "the live draft")
	r := newRecord([]string{"claude"}, "/work")
	r.ID, r.Draft = "sess", "an earlier version of it"
	if err := st.keepVersion(r); err != nil {
		t.Fatal(err)
	}
	_, out, _ := runCLI("list", "--all")
	if !strings.Contains(out, "(earlier version) an earlier version") {
		t.Fatalf("list --all:\n%s", out)
	}
	if _, out, _ := runCLI("list"); strings.Contains(out, "earlier version") {
		t.Fatalf("plain list shows versions:\n%s", out)
	}
}

func TestBuildVersion(t *testing.T) {
	old := version
	defer func() { version = old }()
	version = "9.9.9"
	if buildVersion() != "9.9.9" {
		t.Fatal(buildVersion())
	}
	version = "dev"
	if v := buildVersion(); v == "" {
		t.Fatal("empty version")
	}
}

func TestRestoreMentionsUnplacedPastes(t *testing.T) {
	st := testStore(t)
	r := newRecord([]string{"claude"}, "/work")
	r.ID, r.Draft, r.Ended = "p", "see [Pasted text #1 +3 lines]", time.Now()
	r.Pastes = []string{"a\nb\nc\nd"}
	st.write(r)
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "pbcopy"), []byte("#!/bin/sh\n/bin/cat >/dev/null\n"), 0o755)
	os.WriteFile(filepath.Join(bin, "wl-copy"), []byte("#!/bin/sh\n/bin/cat >/dev/null\n"), 0o755)
	t.Setenv("PATH", bin)
	_, _, errOut := runCLI("restore")
	if !strings.Contains(errOut, "1 paste(s) could not be put back") {
		t.Fatalf("stderr %q", errOut)
	}
}
