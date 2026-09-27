package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fixtureTime is the time every fixture record carries.
var fixtureTime = time.Date(2026, 9, 27, 10, 0, 0, 0, time.Local)

// shellLogBytes writes records in the hooks' format.
func shellLogBytes(recs ...zrec) []byte {
	var b []byte
	for _, r := range recs {
		b = fmt.Appendf(b, "%s %d %d\n%s\n", r.kind, fixtureTime.Unix(), len(r.text), r.text)
	}
	return b
}

// shellLogName is the name the hooks give a log of this host.
func shellLogName(pid int, micro int64, ext string) string {
	return fmt.Sprintf("zsh-%s-%d-%d%s", shellHost(), pid, micro, ext)
}

// writeShellLog writes a fixture log into the store's shell folder.
func writeShellLog(t *testing.T, st *store, name string, recs ...zrec) string {
	t.Helper()
	dir := filepath.Join(st.dir, "shell")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, shellLogBytes(recs...), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func appendShellLog(t *testing.T, path string, recs ...zrec) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(shellLogBytes(recs...)); err != nil {
		t.Fatal(err)
	}
}

// deadPid is the pid of a process that has exited.
func deadPid(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	if pidAlive(pid) {
		t.Skipf("pid %d was taken again at once", pid)
	}
	return pid
}

// shellHistory is the text of every shell line in history, sorted.
func shellHistory(st *store) []string {
	var out []string
	for _, r := range st.load(true) {
		if r.agent() == "zsh" && !isDraftFile(st, r) {
			out = append(out, r.Draft)
		}
	}
	slices.Sort(out)
	return out
}

func shellOrphans(st *store) []*record {
	var out []*record
	for _, r := range st.orphans() {
		if r.agent() == "zsh" {
			out = append(out, r)
		}
	}
	return out
}

func sentTexts(st *store) []string {
	var out []string
	for _, l := range st.sentLogs() {
		for _, m := range l.messages {
			out = append(out, l.Agent+": "+m.Text)
		}
	}
	return out
}

// scenarioImport writes the log of a shell that is gone: a line run with
// Enter, one cleared at its prompt, one that starts with a space, one
// dropped by a forget mark (a leading space added later, or a match of
// HISTORY_IGNORE), and one left at the prompt. Then imp imports it, and the
// store must hold the cleared line in history, the last one as an orphan
// in the folder the prompt ran in, and nothing of the rest.
func scenarioImport(st *store, folder string, pid int, imp func(*store)) error {
	dir := filepath.Join(st.dir, "shell")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	log := filepath.Join(dir, shellLogName(pid, 1, ".log"))
	data := shellLogBytes(
		zrec{"v", "1"},
		zrec{"i", folder},
		zrec{"b", "echo sent"}, zrec{"s", "echo sent"},
		zrec{"i", folder},
		zrec{"b", "cleared line"},
		zrec{"i", folder},
		zrec{"b", " spaced secret"},
		zrec{"i", folder},
		zrec{"b", "l"}, zrec{"f", ""},
		zrec{"i", folder},
		zrec{"b", "left at the prompt"},
	)
	if err := os.WriteFile(log, data, 0o600); err != nil {
		return err
	}
	imp(st)
	var errs []error
	if h := shellHistory(st); !slices.Equal(h, []string{"cleared line"}) {
		errs = append(errs, fmt.Errorf("history holds %q, want the cleared line", h))
	}
	o := shellOrphans(st)
	if len(o) != 1 || o[0].Draft != "left at the prompt" || o[0].Cwd != realPath(folder) || o[0].PID != pid {
		errs = append(errs, fmt.Errorf("orphans %v, want the line left at the prompt in %s", o, folder))
	} else if o[0].Updated.Unix() != fixtureTime.Unix() || o[0].Started.Unix() != fixtureTime.Unix() {
		errs = append(errs, fmt.Errorf("orphan times %v %v, want the record's", o[0].Started, o[0].Updated))
	}
	if s := sentTexts(st); len(s) > 0 {
		errs = append(errs, fmt.Errorf("sent logs hold %q under the shells' default", s))
	}
	if exists(log) {
		errs = append(errs, errors.New("the dead shell's log is still there"))
	}
	return errors.Join(errs...)
}

// TestImportShellLines imports a dead shell's log, and checks the check
// goes red when the import imports nothing.
func TestImportShellLines(t *testing.T) {
	folder := t.TempDir()
	pid := deadPid(t)
	if err := scenarioImport(testStore(t), folder, pid, (*store).importShells); err != nil {
		t.Fatal(err)
	}
	if err := scenarioImport(testStore(t), folder, pid, func(*store) {}); err == nil {
		t.Fatal("the check passed with an import that imports nothing")
	}
}

// Every command that opens the store imports first: unsent list shows the
// dead shell's line, and the notice in that folder names it.
func TestImportOnEveryCommand(t *testing.T) {
	st := testStore(t)
	folder := t.TempDir()
	writeShellLog(t, st, shellLogName(deadPid(t), 1, ".log"),
		zrec{"v", "1"}, zrec{"i", folder}, zrec{"b", "make release"})
	code, out, _ := runCLI("list")
	if code != 0 || !strings.Contains(out, "zsh") || !strings.Contains(out, "make release") {
		t.Fatalf("unsent list: %d %q", code, out)
	}
	code, out, _ = runCLI("show", "--agent", "zsh")
	if code != 0 || out != "make release\n" {
		t.Fatalf("unsent show --agent zsh: %d %q", code, out)
	}
	n := orphanNotice(st, folder, "claude")
	if !strings.Contains(n, "a zsh line you typed here and did not run") || !strings.Contains(n, "unsent restore --agent zsh") {
		t.Fatalf("notice %q", n)
	}
	if n := orphanNotice(st, t.TempDir(), "claude"); n != "" {
		t.Fatalf("notice in another folder %q", n)
	}
	writeShellLog(t, st, shellLogName(deadPid(t), 2, ".log"),
		zrec{"v", "1"}, zrec{"i", folder}, zrec{"b", "git push"})
	st.importShells()
	if n := orphanNotice(st, folder, "claude"); !strings.Contains(n, "2 shell lines") {
		t.Fatalf("notice with two lines %q", n)
	}
}

// A line run with Enter follows the on-send setting: nothing by default,
// the sent log under log, with the per-shell variable winning.
func TestImportSentLine(t *testing.T) {
	for _, c := range []struct {
		global, zsh string
		want        []string
	}{
		{"", "", nil},
		{"", "log", []string{"zsh: echo one", "zsh: exit"}},
		{"log", "", []string{"zsh: echo one", "zsh: exit"}},
		{"log", "delete", nil},
	} {
		t.Run(c.global+"/"+c.zsh, func(t *testing.T) {
			t.Setenv("UNSENT_ON_SEND", c.global)
			t.Setenv("UNSENT_ON_SEND_ZSH", c.zsh)
			st := testStore(t)
			folder := t.TempDir()
			writeShellLog(t, st, shellLogName(deadPid(t), 1, ".log"),
				zrec{"v", "1"}, zrec{"i", folder},
				zrec{"b", "echo one"}, zrec{"s", "echo one"},
				zrec{"i", folder},
				zrec{"s", "exit"}) // the shell exits with no prompt after
			st.importShells()
			if got := sentTexts(st); !slices.Equal(got, c.want) {
				t.Fatalf("sent %q, want %q", got, c.want)
			}
			if h, o := shellHistory(st), shellOrphans(st); len(h) > 0 || len(o) > 0 {
				t.Fatalf("a sent line reached history %q or the drafts %v", h, o)
			}
			if c.want == nil {
				return
			}
			l := st.sentLogs()[0]
			if l.Cwd != realPath(folder) || l.messages[0].Time.Unix() != fixtureTime.Unix() {
				t.Fatalf("sent log folder %q, time %v", l.Cwd, l.messages[0].Time)
			}
			if fi, err := os.Stat(l.path); err != nil || fi.Mode().Perm() != 0o600 {
				t.Fatalf("sent log mode %v %v", fi.Mode(), err)
			}
		})
	}
}

func TestOnSendShellDefault(t *testing.T) {
	t.Setenv("UNSENT_ON_SEND", "")
	t.Setenv("UNSENT_ON_SEND_ZSH", "")
	if v, from := onSendFrom("zsh"); v != "delete" || from != "the default" {
		t.Fatalf("zsh: %s (%s)", v, from)
	}
	if v, _ := onSendFrom("claude"); v != "log" {
		t.Fatalf("claude: %s", v)
	}
}

// Enter at an open quote only opens a continuation prompt; Ctrl+C there
// clears both lines, which go to history together.
func TestImportContinuationIsNotASend(t *testing.T) {
	t.Setenv("UNSENT_ON_SEND_ZSH", "log")
	st := testStore(t)
	writeShellLog(t, st, shellLogName(deadPid(t), 1, ".log"),
		zrec{"v", "1"}, zrec{"i", "/"},
		zrec{"b", `echo "a`}, zrec{"s", `echo "a`}, zrec{"c", ""},
		zrec{"b", "echo \"a\nb"},
		zrec{"i", "/"})
	st.importShells()
	if h := shellHistory(st); !slices.Equal(h, []string{"echo \"a\nb"}) {
		t.Fatalf("history %q", h)
	}
	if s := sentTexts(st); len(s) > 0 {
		t.Fatalf("sent %q", s)
	}
}

// A line replaced wholesale, as Up or Ctrl+R does, goes to history; one
// edited in place does not; one emptied by hand does.
func TestImportReplacedLine(t *testing.T) {
	st := testStore(t)
	typed := "curl -X POST https://example.invalid/hook -d @body.json"
	writeShellLog(t, st, shellLogName(deadPid(t), 1, ".log"),
		zrec{"v", "1"}, zrec{"i", "/"},
		zrec{"b", typed},
		zrec{"b", typed + " -v"},
		zrec{"b", "git log --oneline --graph --decorate --all"},
		zrec{"i", "/"},
		zrec{"b", "echo emptied by hand"}, zrec{"b", ""},
		zrec{"i", "/"})
	st.importShells()
	want := []string{typed + " -v", "git log --oneline --graph --decorate --all", "echo emptied by hand"}
	slices.Sort(want)
	if h := shellHistory(st); !slices.Equal(h, want) {
		t.Fatalf("history %q, want %q", h, want)
	}
}

// Short lines follow the same rule: a recall of another short line that
// Enter runs keeps the typed one, and small edits keep nothing.
func TestImportReplacedShortLine(t *testing.T) {
	st := testStore(t)
	writeShellLog(t, st, shellLogName(deadPid(t), 1, ".log"),
		zrec{"v", "1"}, zrec{"i", "/"},
		zrec{"b", "git push -f"}, zrec{"s", "make test"},
		zrec{"i", "/"},
		zrec{"b", "git comit"}, zrec{"b", "git commit"}, zrec{"b", "'git commit'"},
		zrec{"b", "echo hi"}, zrec{"b", "cd"}, zrec{"s", "cd"},
		zrec{"i", "/"})
	st.importShells()
	want := []string{"'git commit'", "echo hi", "git push -f"}
	if h := shellHistory(st); !slices.Equal(h, want) {
		t.Fatalf("history %q, want %q", h, want)
	}
}

// A forget mark drops the line the user edited into an ignored text, and
// nothing else: a line cleared or replaced earlier at that prompt was
// another line, and one marked new was replaced by the ignored text.
func TestImportForgetDropsOnlyThatLine(t *testing.T) {
	kubectl := "kubectl apply -f deploy.yaml --context prod"
	for _, c := range []struct {
		name string
		recs []zrec
		want []string
	}{
		{"edited into a space", []zrec{{"b", "export TOKEN=pw1"}, {"f", ""}, {"b", "typed after"}}, []string{"typed after"}},
		// Ctrl+U, then a line typed with a leading space.
		{"after a clear", []zrec{{"b", kubectl}, {"b", ""}, {"f", ""}}, []string{kubectl}},
		// Ctrl+U, then ls typed under HISTORY_IGNORE='(ls|cd)'.
		{"a clear, then an ignored line", []zrec{{"b", kubectl}, {"b", ""}, {"b", "l"}, {"f", ""}}, []string{kubectl}},
		// Up recalls ls or a line with a leading space, and Enter runs it.
		{"replaced by an ignored recall", []zrec{{"b", kubectl}, {"f", "new"}}, []string{kubectl}},
		{"replaced, then the new line forgotten", []zrec{
			{"b", "export TOKEN=pw1 and a long enough tail"},
			{"b", "recalled from the history, long enough"},
			{"f", ""}}, []string{"export TOKEN=pw1 and a long enough tail"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			st := testStore(t)
			recs := append([]zrec{{"v", "1"}, {"i", "/"}}, c.recs...)
			writeShellLog(t, st, shellLogName(deadPid(t), 1, ".log"), append(recs, zrec{"i", "/"})...)
			st.importShells()
			if h := shellHistory(st); !slices.Equal(h, c.want) {
				t.Fatalf("history %q, want %q", h, c.want)
			}
			if o := shellOrphans(st); len(o) > 0 {
				t.Fatalf("orphans %v", o)
			}
		})
	}
}

// The h record, the line when the window closed, is the dead shell's last
// line even when the last redraw showed less.
func TestImportHupRecord(t *testing.T) {
	st := testStore(t)
	writeShellLog(t, st, shellLogName(deadPid(t), 1, ".log"),
		zrec{"v", "1"}, zrec{"i", "/"}, zrec{"b", "partial"}, zrec{"h", "partial and the rest"})
	st.importShells()
	if o := shellOrphans(st); len(o) != 1 || o[0].Draft != "partial and the rest" {
		t.Fatalf("orphans %v", o)
	}
}

// liveChild starts a process that lives until kill, which also reaps it.
func liveChild(t *testing.T) (pid int, kill func()) {
	t.Helper()
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	kill = func() {
		cmd.Process.Kill()
		cmd.Wait()
	}
	t.Cleanup(kill)
	return cmd.Process.Pid, kill
}

// A failed write moves the only log aside; the import reads it while the
// shell lives, and the window closes before a new log exists. The next
// import finds only the state file, and the line at the prompt becomes an
// orphan.
func TestImportShellEndedAfterItsLogsWent(t *testing.T) {
	st := testStore(t)
	pid, kill := liveChild(t)
	writeShellLog(t, st, shellLogName(pid, 1, ".done"),
		zrec{"v", "1"}, zrec{"i", "/"}, zrec{"b", "typed before the disk filled"})
	st.importShells()
	state := filepath.Join(st.dir, "shell", fmt.Sprintf("zsh-%s-%d.import", shellHost(), pid))
	if !exists(state) || len(shellOrphans(st)) > 0 {
		t.Fatalf("after the first import: state there %v, orphans %v", exists(state), shellOrphans(st))
	}
	kill()
	st.importShells()
	if o := shellOrphans(st); len(o) != 1 || o[0].Draft != "typed before the disk filled" {
		t.Fatalf("orphans %v", o)
	}
	if exists(state) {
		t.Fatal("the state file of an ended shell is still there")
	}
}

// The clock steps back and a live shell opens its next log: that log's
// stamp sorts before the one the state names. It is read, never deleted.
func TestImportLogOpenedAfterTheClockSteppedBack(t *testing.T) {
	st := testStore(t)
	pid := os.Getpid()
	first := writeShellLog(t, st, shellLogName(pid, 200, ".log"), zrec{"v", "1"}, zrec{"i", "/"}, zrec{"b", "first log"}, zrec{"i", "/"})
	st.importShells()
	if err := os.Rename(first, strings.TrimSuffix(first, ".log")+".done"); err != nil {
		t.Fatal(err)
	}
	next := writeShellLog(t, st, shellLogName(pid, 100, ".log"),
		zrec{"v", "1"}, zrec{"i", "/"}, zrec{"b", "after the clock stepped back"}, zrec{"i", "/"})
	st.importShells()
	if !exists(next) {
		t.Fatal("the importer deleted the live shell's new log")
	}
	if h := shellHistory(st); !slices.Equal(h, []string{"after the clock stepped back", "first log"}) {
		t.Fatalf("history %q", h)
	}
}

// An import that reads nothing new leaves a live shell's state file alone:
// rewriting it costs two syncs per shell on every unsent command.
func TestImportWithNothingNewWritesNothing(t *testing.T) {
	st := testStore(t)
	pid := os.Getpid()
	log := writeShellLog(t, st, shellLogName(pid, 1, ".log"), zrec{"v", "1"}, zrec{"i", "/"}, zrec{"b", "in progress"})
	st.importShells()
	state := filepath.Join(st.dir, "shell", fmt.Sprintf("zsh-%s-%d.import", shellHost(), pid))
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(state, old, old); err != nil {
		t.Fatal(err)
	}
	mtime := func() time.Time {
		fi, err := os.Stat(state)
		if err != nil {
			t.Fatal(err)
		}
		return fi.ModTime()
	}
	st.importShells()
	if !mtime().Equal(old) {
		t.Fatal("an import with nothing new rewrote the state file")
	}
	appendShellLog(t, log, zrec{"b", "in progress, more"})
	st.importShells()
	if mtime().Equal(old) {
		t.Fatal("an import with a new record left the state file as it was")
	}
}

// Imported shell lines go to history under their own cap: they never push
// agent drafts out, and one import of many cleared lines keeps the cap.
func TestImportKeepsTheShellCap(t *testing.T) {
	st := testStore(t)
	hist := filepath.Join(st.dir, "history")
	seedFiles(t, hist, historyLimit, []byte(`{"draft":"agent","agent":"claude"}`), func(i int) string { return fmt.Sprintf("20260101-%06d-1.json", i) })
	recs := []zrec{{"v", "1"}, {"i", "/"}}
	for i := range shellHistoryLimit + 20 {
		recs = append(recs, zrec{"b", fmt.Sprintf("cleared line %d", i)}, zrec{"i", "/"})
	}
	writeShellLog(t, st, shellLogName(deadPid(t), 1, ".log"), recs...)
	st.importShells()
	names, _ := filepath.Glob(filepath.Join(hist, "*.json"))
	agents, shells := 0, 0
	for _, n := range names {
		if strings.HasPrefix(filepath.Base(n), shellHistoryPrefix) {
			shells++
		} else {
			agents++
		}
	}
	if agents != historyLimit || shells != shellHistoryLimit {
		t.Fatalf("after the import: %d agent drafts, %d shell lines", agents, shells)
	}
}

// A live shell's log stays, and each import takes only the records added
// since the last one, across the log being moved aside and the next one.
// The shell is one session throughout.
func TestImportLiveShellKeepsItsPlace(t *testing.T) {
	t.Setenv("UNSENT_ON_SEND_ZSH", "log")
	st := testStore(t)
	pid := os.Getpid() // alive for the whole test
	log := writeShellLog(t, st, shellLogName(pid, 1, ".log"),
		zrec{"v", "1"}, zrec{"i", "/"}, zrec{"b", "first cleared"}, zrec{"i", "/"},
		zrec{"b", "ran"}, zrec{"s", "ran"}, zrec{"i", "/"},
		zrec{"b", "in progress"})
	// A record half written: the hook is in the middle of it.
	f, err := os.OpenFile(log, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("b 1790000000 11\nin progr")
	f.Close()
	for range 2 {
		st.importShells()
	}
	if h := shellHistory(st); !slices.Equal(h, []string{"first cleared"}) {
		t.Fatalf("history %q", h)
	}
	if o := shellOrphans(st); len(o) > 0 {
		t.Fatalf("a live shell's line is an orphan: %v", o)
	}
	if !exists(log) {
		t.Fatal("the importer deleted a live shell's log")
	}
	state := filepath.Join(st.dir, "shell", fmt.Sprintf("zsh-%s-%d.import", shellHost(), pid))
	if !exists(state) {
		t.Fatal("no state file for the live shell")
	}
	// The hook finishes the record, then moves the log aside at the next
	// prompt and opens a new one.
	f, _ = os.OpenFile(log, os.O_WRONLY|os.O_APPEND, 0o600)
	f.WriteString("ess\n")
	f.Close()
	appendShellLog(t, log, zrec{"s", "in progress"})
	if err := os.Rename(log, strings.TrimSuffix(log, ".log")+".done"); err != nil {
		t.Fatal(err)
	}
	st.importShells()
	if exists(strings.TrimSuffix(log, ".log") + ".done") {
		t.Fatal("the importer kept a log moved aside")
	}
	next := writeShellLog(t, st, shellLogName(pid, 2, ".log"),
		zrec{"v", "1"}, zrec{"i", "/"}, zrec{"b", "second log"}, zrec{"i", "/"})
	st.importShells()
	st.importShells()
	if h := shellHistory(st); !slices.Equal(h, []string{"first cleared", "second log"}) {
		t.Fatalf("history %q", h)
	}
	logs := st.sentLogs()
	if len(logs) != 1 || len(logs[0].messages) != 2 || logs[0].messages[1].Text != "in progress" {
		t.Fatalf("sent logs %v, want one session with both lines", sentTexts(st))
	}
	if !exists(next) {
		t.Fatal("the importer deleted the live shell's new log")
	}
}

// Two .log files of one host and pid: the older shell ended and a newer
// one took its pid. The older one's line is an orphan; the newer is live.
func TestImportPidTakenByANewShell(t *testing.T) {
	st := testStore(t)
	pid := os.Getpid()
	old := writeShellLog(t, st, shellLogName(pid, 1, ".log"),
		zrec{"v", "1"}, zrec{"i", "/"}, zrec{"b", "the old shell's line"})
	cur := writeShellLog(t, st, shellLogName(pid, 2, ".log"),
		zrec{"v", "1"}, zrec{"i", "/"}, zrec{"b", "the new shell's line"})
	st.importShells()
	o := shellOrphans(st)
	if len(o) != 1 || o[0].Draft != "the old shell's line" {
		t.Fatalf("orphans %v", o)
	}
	if exists(old) || !exists(cur) {
		t.Fatalf("old log there: %v, new log there: %v", exists(old), exists(cur))
	}
}

// A log named for another host is that host's to import, unless nothing
// has written to it for 30 days.
func TestImportLeavesAnotherHostsLog(t *testing.T) {
	st := testStore(t)
	name := fmt.Sprintf("zsh-%s-%d-1.log", "elsewhere.invalid", deadPid(t))
	log := writeShellLog(t, st, name, zrec{"v", "1"}, zrec{"i", "/"}, zrec{"b", "typed on the other host"})
	st.importShells()
	if !exists(log) || len(shellOrphans(st)) > 0 {
		t.Fatal("the importer took another host's log")
	}
	old := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(log, old, old); err != nil {
		t.Fatal(err)
	}
	st.importShells()
	if exists(log) || len(shellOrphans(st)) != 1 {
		t.Fatalf("a 31-day-old log of another host: there %v, orphans %v", exists(log), shellOrphans(st))
	}
}

// A log in a newer format, or with a broken record, is left for a later
// unsent; a record torn by a failed write at the end of a log moved aside
// is skipped, and the records before it count.
func TestImportLeavesWhatItCannotRead(t *testing.T) {
	st := testStore(t)
	newer := writeShellLog(t, st, shellLogName(deadPid(t), 1, ".log"),
		zrec{"v", "2"}, zrec{"i", "/"}, zrec{"b", "a newer unsent's line"})
	broken := writeShellLog(t, st, shellLogName(deadPid(t), 2, ".log"), zrec{"v", "1"}, zrec{"i", "/"})
	f, _ := os.OpenFile(broken, os.O_WRONLY|os.O_APPEND, 0o600)
	f.WriteString("not a header\n")
	f.Close()
	torn := writeShellLog(t, st, shellLogName(deadPid(t), 3, ".done"),
		zrec{"v", "1"}, zrec{"i", "/"}, zrec{"b", "before the failed write"})
	f, _ = os.OpenFile(torn, os.O_WRONLY|os.O_APPEND, 0o600)
	f.WriteString("b 1790000000 40\nbefore the failed write, and")
	f.Close()
	st.importShells()
	if !exists(newer) || !exists(broken) {
		t.Fatalf("newer there: %v, broken there: %v", exists(newer), exists(broken))
	}
	o := shellOrphans(st)
	if len(o) != 1 || o[0].Draft != "before the failed write" || exists(torn) {
		t.Fatalf("orphans %v, torn log there: %v", o, exists(torn))
	}
}

// Shell lines count against their own cap and never push agent drafts out
// of history, nor agent drafts shell lines.
func TestShellHistoryCap(t *testing.T) {
	st := testStore(t)
	hist := filepath.Join(st.dir, "history")
	seedFiles(t, hist, 20, []byte(`{"draft":"agent","agent":"claude"}`), func(i int) string { return fmt.Sprintf("20260101-%06d-1.json", i) })
	seedFiles(t, hist, shellHistoryLimit+30, []byte(`{"draft":"line","agent":"zsh"}`), func(i int) string { return fmt.Sprintf("sh-20260101-%06d.json", i) })
	st.prune()
	count := func(prefix string) int {
		n := 0
		names, _ := filepath.Glob(filepath.Join(hist, "*.json"))
		for _, name := range names {
			if strings.HasPrefix(filepath.Base(name), prefix) {
				n++
			}
		}
		return n
	}
	if a, s := count("2026"), count("sh-"); a != 20 || s != shellHistoryLimit {
		t.Fatalf("after pruning: %d agent drafts, %d shell lines", a, s)
	}
	if !exists(filepath.Join(hist, fmt.Sprintf("sh-20260101-%06d.json", shellHistoryLimit+29))) {
		t.Fatal("the newest shell line was pruned")
	}
	seedFiles(t, hist, historyLimit+10, []byte(`{"draft":"agent","agent":"claude"}`), func(i int) string { return fmt.Sprintf("20260102-%06d-1.json", i) })
	st.prune()
	if a, s := count("2026"), count("sh-"); a != historyLimit || s != shellHistoryLimit {
		t.Fatalf("after more agent drafts: %d agent drafts, %d shell lines", a, s)
	}
	// Restoring a shell line moves it to history under the shells' cap.
	r := &record{ID: "zsh-x-1-1", Agent: "zsh", Draft: "a line", PID: 1}
	if err := st.archive(r); err != nil {
		t.Fatal(err)
	}
	if s := count("sh-"); s != shellHistoryLimit {
		t.Fatalf("after archiving a shell line: %d shell lines", s)
	}
}

// scenarioZshToStore types at a real zsh with the hooks: a line run with
// Enter, one cleared with Ctrl+C, one typed until it matches
// HISTORY_IGNORE, one that starts with a space, one that gains a space at
// its start, then one left at the prompt when end ends the shell. unsent
// list must then show the last line, and history exactly the cleared line
// and the marks: no prefix of an ignored line either.
func scenarioZshToStore(t *testing.T, hooks string, end func(*zshSession)) error {
	s := startZsh(t, hooks, true)
	t.Setenv("UNSENT_HOME", filepath.Join(s.home, "state"))
	s.send("HISTORY_IGNORE='(ignored*|ls)'\r")
	s.mark("set")
	s.send("echo ran with enter\r")
	s.mark("ran")
	s.line("cleared with ctrl-c")
	s.clear()
	s.line("ignore") // saved: it does not match yet
	s.echo("d secret", "d secret")
	s.clear()
	s.echo(" spaced secret", "spaced secret")
	s.clear()
	n := len(s.records())
	s.line("spaced later")
	s.send("\x01 ") // Ctrl+A, then a space at the start
	s.waitFor("a forget mark", func(r []zrec) bool { return s.index(r, n, "f", "") >= 0 })
	s.clear()
	s.line("left at the prompt")
	end(s)
	s.exits("the end of the scenario")
	code, out, errOut := runCLI("list", "--all", "--agent", "zsh")
	var errs []error
	if code != 0 {
		errs = append(errs, fmt.Errorf("unsent list: exit %d: %s", code, errOut))
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	o := shellOrphans(st)
	if len(o) != 1 || o[0].Draft != "left at the prompt" || o[0].Cwd != realPath(s.home) {
		errs = append(errs, fmt.Errorf("orphans %v", o))
	}
	h := shellHistory(st)
	if want := []string{"cleared with ctrl-c", "mark-ran", "mark-ready", "mark-set"}; !slices.Equal(h, want) {
		errs = append(errs, fmt.Errorf("history %q, want %q", h, want))
	}
	for _, secret := range []string{"ran with enter", "ignore", "spaced secret", "spaced later"} {
		if strings.Contains(out, secret) {
			errs = append(errs, fmt.Errorf("%q reached unsent list: %q", secret, out))
		}
	}
	if !strings.Contains(out, "left at the prompt") || !strings.Contains(out, "cleared with ctrl-c") {
		errs = append(errs, fmt.Errorf("unsent list shows %q", out))
	}
	if l := s.logs(); len(l) > 0 {
		errs = append(errs, fmt.Errorf("logs left after the shell ended: %v", l))
	}
	return errors.Join(errs...)
}

// TestZshLinesReachTheStore runs the hooks in a real zsh, ends it with
// SIGHUP and with kill -9, and imports what it wrote. Without the
// HISTORY_IGNORE check in the hooks the check must go red.
func TestZshLinesReachTheStore(t *testing.T) {
	t.Setenv("UNSENT_ON_SEND", "")
	t.Setenv("UNSENT_ON_SEND_ZSH", "")
	hup := func(s *zshSession) { syscall.Kill(s.cmd.Process.Pid, syscall.SIGHUP) }
	kill := func(s *zshSession) { s.cmd.Process.Kill() }
	for name, end := range map[string]func(*zshSession){"hup": hup, "kill-9": kill} {
		t.Run(name, func(t *testing.T) {
			if err := scenarioZshToStore(t, zshHooks, end); err != nil {
				t.Fatal(err)
			}
		})
	}
	mutated := strings.Replace(zshHooks, "if [[ $t == ' '* ]] || (( ig )); then", "if [[ $t == ' '* ]]; then", 1)
	if mutated == zshHooks {
		t.Fatal("the mutation found nothing to change")
	}
	t.Run("mutated", func(t *testing.T) {
		if err := scenarioZshToStore(t, mutated, kill); err == nil {
			t.Fatal("the check passed with the HISTORY_IGNORE check taken out of the hooks")
		}
	})
	// Hooks that stop saving when a line comes to match, but write no
	// forget mark, leave the prefix typed before it in the log.
	unmarked := strings.Replace(zshHooks, "    if [[ $t == ' '* ]] || (( ig )); then\n",
		"    (( ig )) && { _unsent_n=0 _unsent_last=' '; return 0 }\n    if [[ $t == ' '* ]] || (( ig )); then\n", 1)
	if unmarked == zshHooks {
		t.Fatal("the mutation found nothing to change")
	}
	t.Run("no forget mark", func(t *testing.T) {
		if err := scenarioZshToStore(t, unmarked, kill); err == nil {
			t.Fatal("the check passed with hooks that write no forget mark for a line that comes to match")
		}
	})
}

// scenarioRecall browses the shell's history with Up in a real zsh. A line
// typed and then replaced by a recall that Enter runs goes to history,
// even when the recalled line matches HISTORY_IGNORE; the recalled lines,
// which the shell keeps, never do.
func scenarioRecall(t *testing.T, hooks string) error {
	s := startZsh(t, hooks, true)
	t.Setenv("UNSENT_HOME", filepath.Join(s.home, "state"))
	s.send("setopt hist_ignore_dups; HISTORY_IGNORE='(ls)'\r")
	s.mark("set")
	for _, c := range []string{"ls", "true alpha", ": beta words"} {
		s.send(c + "\r")
		s.mark(c[:2])
	}
	up := func(n int) {
		for range n {
			s.send("\x1b[A")
			time.Sleep(300 * time.Millisecond) // a redraw each, as a person gets
		}
	}
	s.line("typed then replaced")
	up(3) // ls, which HISTORY_IGNORE keeps out of the history file only
	s.send("\r")
	s.mark("ran")
	s.line("browsed past")
	up(2)
	s.send("\x1b[B\x1b[B")
	if err := s.takes("!", "browsed past!"); err != nil {
		return err
	}
	s.clear()
	s.mark("end")
	st, err := openStore()
	if err != nil {
		return err
	}
	var h []string
	for _, x := range shellHistory(st) {
		if !strings.HasPrefix(x, "mark-") {
			h = append(h, x)
		}
	}
	if want := []string{"browsed past!", "typed then replaced"}; !slices.Equal(h, want) {
		return fmt.Errorf("history %q, want %q", h, want)
	}
	return nil
}

// TestZshRecallsStayOutOfHistory runs scenarioRecall, and again with hooks
// that save recalled lines, and with hooks that never mark a forget as
// new, where it must go red.
func TestZshRecallsStayOutOfHistory(t *testing.T) {
	t.Setenv("UNSENT_ON_SEND", "")
	t.Setenv("UNSENT_ON_SEND_ZSH", "")
	if err := scenarioRecall(t, zshHooks); err != nil {
		t.Fatal(err)
	}
	for name, m := range map[string][2]string{
		"recalls saved":    {"    [[ $1 != s ]] && (( HISTNO != HISTCMD ))", "    [[ $1 != s ]] && (( 0 ))"},
		"forget never new": {"|| k=new", "|| k="},
	} {
		mutated := strings.Replace(zshHooks, m[0], m[1], 1)
		if mutated == zshHooks {
			t.Fatalf("%s: the mutation found nothing to change", name)
		}
		t.Run(name, func(t *testing.T) {
			if err := scenarioRecall(t, mutated); err == nil {
				t.Fatalf("the check passed with %s", name)
			}
		})
	}
}

// A HISTORY_IGNORE zsh cannot parse matches nothing, and the hooks go on
// saving.
func TestZshHooksSurviveABadHistoryIgnore(t *testing.T) {
	s := startZsh(t, zshHooks, true)
	s.send("HISTORY_IGNORE='(ls'\r")
	s.mark("set")
	r := s.line("ls still saved")
	if !holds(r, "ls still saved") {
		t.Fatalf("log %v", r)
	}
	s.mu.Lock()
	out := s.out.String()
	s.mu.Unlock()
	if strings.Contains(out, "bad pattern") {
		t.Fatal("the hooks printed zsh's pattern error")
	}
}
