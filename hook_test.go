package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

// hookConv is the conversation the hook tests resume.
const hookConv = "0b1c2d3e-4f50-4a6b-8c9d-0e1f2a3b4c5d"

// hookDraft is the draft the matching orphan holds: a first line longer
// than list --json keeps, with a quote in it, and a second line the note
// must never carry.
var hookDraft = "\n  Refactor the \"parser\" & keep the tests " + strings.Repeat("x", 80) + "\nsecret second line\nthird"

// seedHook writes orphans for the hook tests into a fresh store and returns
// the folder the conversation ran in.
func seedHook(t *testing.T) string {
	t.Helper()
	st := testStore(t)
	work := realPath(t.TempDir())
	sub := filepath.Join(work, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	zone := time.FixedZone("", 2*3600)
	at := func(day, min int) time.Time { return time.Date(2026, 9, day, 18, min, 7, 0, zone) }
	for _, r := range []*record{
		{ID: "match-old", Agent: "claude", AgentSession: hookConv, Cwd: work, Updated: at(20, 30), Draft: "an older draft of the same conversation"},
		{ID: "match", Agent: "claude", AgentSession: hookConv, Cwd: work, Updated: at(20, 42), Draft: hookDraft},
		{ID: "other-conv", Agent: "claude", AgentSession: "11111111-2222-4333-8444-555555555555", Cwd: work, Updated: at(21, 0), Draft: "another conversation's draft"},
		{ID: "subfolder", Agent: "claude", AgentSession: "sub-conv", Cwd: sub, Updated: at(21, 1), Draft: "typed in a subfolder"},
		{ID: "other-agent", Agent: "codex", AgentSession: "codex-conv", Cwd: work, Updated: at(21, 2), Draft: "a codex draft"},
	} {
		r.Format, r.Command, r.PID, r.Started, r.Ended = recordFormat, []string{"claude"}, 4242, r.Updated, r.Updated
		if err := st.write(r); err != nil {
			t.Fatal(err)
		}
	}
	return work
}

// hookJSON is the hook's input as Claude Code sends it.
func hookJSON(session, cwd, source string) string {
	b, _ := json.Marshal(map[string]string{
		"session_id": session, "transcript_path": "/cfg/projects/x/" + session + ".jsonl",
		"cwd": cwd, "hook_event_name": "SessionStart", "source": source,
	})
	return string(b)
}

// runHook runs `unsent hook claude --run` with stdin, and fails the test
// unless it exits 0 and says nothing on stderr.
func runHook(t *testing.T, stdin string) string {
	t.Helper()
	var out, errb bytes.Buffer
	if code := cmdHook([]string{"claude", "--run"}, strings.NewReader(stdin), &out, &errb); code != 0 || errb.Len() > 0 {
		t.Fatalf("hook --run: exit %d, stderr %q", code, errb.String())
	}
	return out.String()
}

// checkHookOutput fails unless out is exactly one line of JSON holding
// hookSpecificOutput with hookEventName and additionalContext, and nothing
// else: never initialUserMessage, which would send.
func checkHookOutput(out string) string {
	if strings.Contains(out, "initialUserMessage") {
		return "holds initialUserMessage"
	}
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		return "is not one line"
	}
	var v map[string]map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return err.Error()
	}
	h, ok := v["hookSpecificOutput"]
	if len(v) != 1 || !ok || len(h) != 2 || h["hookEventName"] != "SessionStart" {
		return "holds more than hookSpecificOutput{hookEventName, additionalContext}"
	}
	if _, ok := h["additionalContext"].(string); !ok {
		return "has no additionalContext"
	}
	return ""
}

func TestHookClaudeRun(t *testing.T) {
	work := seedHook(t)
	for _, c := range []struct {
		name, stdin string
		note        bool
	}{
		{"resume with this conversation's orphan", hookJSON(hookConv, work, "resume"), true},
		{"resume through a symlinked folder", "", true}, // filled in below
		{"resume of another conversation", hookJSON("99999999-2222-4333-8444-555555555555", work, "resume"), false},
		{"resume of a subfolder's conversation, from the parent", hookJSON("sub-conv", work, "resume"), false},
		{"resume of another agent's conversation", hookJSON("codex-conv", work, "resume"), false},
		{"resume in another folder", hookJSON(hookConv, filepath.Join(work, "sub"), "resume"), false},
		{"startup", hookJSON(hookConv, work, "startup"), false},
		{"clear", hookJSON(hookConv, work, "clear"), false},
		{"compact", hookJSON(hookConv, work, "compact"), false},
		{"fork", hookJSON(hookConv, work, "fork"), false},
		{"another hook event", strings.Replace(hookJSON(hookConv, work, "resume"), "SessionStart", "UserPromptSubmit", 1), false},
		{"no session id", hookJSON("", work, "resume"), false},
		{"no folder", hookJSON(hookConv, "", "resume"), false},
		{"malformed JSON", `{"session_id":"` + hookConv + `","source":"resume"`, false},
		{"not an object", `["resume"]`, false},
		{"empty stdin", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			stdin := c.stdin
			if strings.Contains(c.name, "symlinked") {
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(work, link); err != nil {
					t.Fatal(err)
				}
				stdin = hookJSON(hookConv, link, "resume")
			}
			out := runHook(t, stdin)
			if !c.note {
				if out != "" {
					t.Fatalf("printed %q", out)
				}
				return
			}
			if why := checkHookOutput(out); why != "" {
				t.Fatalf("output %s: %q", why, out)
			}
			if strings.Contains(out, "secret second line") || strings.Contains(out, "older draft") {
				t.Fatalf("the note carries more than the newest draft's first line: %q", out)
			}
		})
	}
}

// The note for the newest matching orphan, as the hook prints it.
func TestHookClaudeNoteGolden(t *testing.T) {
	work := seedHook(t)
	golden(t, "hook-claude-note.golden", runHook(t, hookJSON(hookConv, work, "resume")))
}

// UNSENT_NOTICE=0 quiets the screen, not the hook: installing the hook is
// how the user asked for the note.
func TestHookClaudeRunIgnoresUnsentNotice(t *testing.T) {
	work := seedHook(t)
	t.Setenv("UNSENT_NOTICE", "0")
	out := runHook(t, hookJSON(hookConv, work, "resume"))
	if why := checkHookOutput(out); why != "" {
		t.Fatalf("with UNSENT_NOTICE=0 the output %s: %q", why, out)
	}
}

// A store that is not there gets no note, and the hook makes no folder.
func TestHookClaudeNoStore(t *testing.T) {
	home := filepath.Join(t.TempDir(), "none")
	t.Setenv("UNSENT_HOME", home)
	if out := runHook(t, hookJSON(hookConv, t.TempDir(), "resume")); out != "" {
		t.Fatalf("printed %q", out)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("the hook made the store: %v", err)
	}
	// A store folder with no drafts folder in it is no store either.
	if err := os.WriteFile(home, []byte("a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := runHook(t, hookJSON(hookConv, t.TempDir(), "resume")); out != "" {
		t.Fatalf("printed %q", out)
	}
}

// A terminal on stdin (someone ran --run by hand) is never read.
func TestHookClaudeRunLeavesATerminalAlone(t *testing.T) {
	seedHook(t)
	user, tty, err := pty.Open()
	if err != nil {
		t.Skip("no pseudo-terminal:", err)
	}
	defer user.Close()
	defer tty.Close()
	done := make(chan string, 1)
	go func() {
		var out bytes.Buffer
		cmdHook([]string{"claude", "--run"}, tty, &out, &out)
		done <- out.String()
	}()
	select {
	case out := <-done:
		if out != "" {
			t.Fatalf("printed %q", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the hook read the terminal")
	}
}

// A check that cannot fail is not a check: with the output mutated to add
// initialUserMessage, the output check goes red.
func TestHookClaudeOutputCheckCatchesInitialUserMessage(t *testing.T) {
	work := seedHook(t)
	old := hookOutput
	hookOutput = func(note string) any {
		v := old(note).(hookNote)
		return map[string]any{"hookSpecificOutput": map[string]string{
			"hookEventName": v.HookSpecificOutput.HookEventName, "additionalContext": note, "initialUserMessage": "go on",
		}}
	}
	t.Cleanup(func() { hookOutput = old })
	out := runHook(t, hookJSON(hookConv, work, "resume"))
	if why := checkHookOutput(out); why != "holds initialUserMessage" {
		t.Fatalf("the output check did not catch initialUserMessage: %q, output %q", why, out)
	}
}

// With a session match that is always true, another conversation's resume
// gets a note, so TestHookClaudeRun goes red.
func TestHookClaudeRunCatchesAnAlwaysTrueMatch(t *testing.T) {
	work := seedHook(t)
	old := sessionMatch
	sessionMatch = func(string, string) bool { return true }
	t.Cleanup(func() { sessionMatch = old })
	if out := runHook(t, hookJSON("99999999-2222-4333-8444-555555555555", work, "resume")); out == "" {
		t.Fatal("the other-conversation case would stay green with a session match that is always true")
	}
}

// The config names the unsent binary by its absolute path, and says on
// stderr where it goes.
func TestHookClaudeConfigGolden(t *testing.T) {
	old := hookExe
	hookExe = func() (string, error) { return "/usr/local/bin/unsent", nil }
	t.Cleanup(func() { hookExe = old })
	t.Setenv("CLAUDE_CONFIG_DIR", "/cfg/claude")
	code, out, errOut := runCLI("hook", "claude")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	golden(t, "hook-claude-config.golden", out)
	if !strings.Contains(errOut, "/cfg/claude/settings.json") || strings.Count(errOut, "\n") > 2 {
		t.Fatalf("stderr %q", errOut)
	}
	var c hookConfig
	if err := json.Unmarshal([]byte(out), &c); err != nil {
		t.Fatal(err)
	}
	want := []hookMatcher{{Hooks: []hookCommand{{Type: "command", Command: "/usr/local/bin/unsent hook claude --run"}}}}
	if !reflect.DeepEqual(c.Hooks.SessionStart, want) {
		t.Fatalf("config %+v", c)
	}
}

// The real lookup gives the test binary's absolute path.
func TestHookExeIsAbsolute(t *testing.T) {
	p, err := hookExe()
	if err != nil || !filepath.IsAbs(p) {
		t.Fatalf("%q, %v", p, err)
	}
}

// An unsent on PATH that is a symlink to the running binary, as Homebrew's
// bin/unsent is to its versioned Cellar copy, is named by the link, which
// an upgrade keeps; the target it resolves to is removed by one.
func TestHookExeKeepsThePathSymlink(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	link := filepath.Join(bin, "unsent")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if p, err := hookExe(); err != nil || p != link {
		t.Fatalf("got %q, %v; want the link %q", p, err, link)
	}
	// An unsent on PATH that is another binary is not this one.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(link, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if p, err := hookExe(); err != nil || p == link {
		t.Fatalf("got %q, %v; want the running binary, not another unsent on PATH", p, err)
	}
}

// A path sh would split or expand is quoted, and sh gets it back as it is.
func TestShellQuote(t *testing.T) {
	for _, p := range []string{"/usr/local/bin/unsent", "/Users/a b/bin/unsent", "/o'neil/$HOME/`x`/unsent", ""} {
		out, err := exec.Command("sh", "-c", "printf %s "+shellQuote(p)).Output()
		if err != nil || string(out) != p {
			t.Errorf("%q: sh read %q, %v", p, out, err)
		}
	}
	if q := shellQuote("/usr/local/bin/unsent"); q != "/usr/local/bin/unsent" {
		t.Errorf("a plain path was quoted: %s", q)
	}
}

func TestHookUsage(t *testing.T) {
	for _, args := range [][]string{{"hook"}, {"hook", "codex"}, {"hook", "claude", "--bogus"}} {
		if code, _, errOut := runCLI(args...); code != 2 || !strings.Contains(errOut, "usage") {
			t.Errorf("%v: exit %d, %q", args, code, errOut)
		}
	}
}

// A draft the automatic paste gave up on still gets a note: it is the one
// the user most needs to hear about, and only the paste has a retry cap.
func TestHookClaudeNotesADraftThePasteGaveUpOn(t *testing.T) {
	st := testStore(t)
	work := realPath(t.TempDir())
	r := &record{ID: "gave-up", Agent: "claude", AgentSession: hookConv, Cwd: work, Draft: hookDraft, RestoreTries: maxRestoreTries}
	r.Format, r.Command, r.PID, r.Updated = recordFormat, []string{"claude"}, 4242, time.Now().Add(-time.Hour)
	r.Started, r.Ended = r.Updated, r.Updated
	if err := st.write(r); err != nil {
		t.Fatal(err)
	}
	if st.sessionOrphan("claude", work, hookConv) != nil {
		t.Fatal("the paste has not given up on it")
	}
	if why := checkHookOutput(runHook(t, hookJSON(hookConv, work, "resume"))); why != "" {
		t.Fatalf("no note for a draft the paste gave up on: %s", why)
	}
}
