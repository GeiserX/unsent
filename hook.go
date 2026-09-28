package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// `unsent hook claude` prints a Claude Code SessionStart hook for the user
// to add to their settings.json; unsent never edits that file. The hook
// runs `unsent hook claude --run`, which tells the model of a resumed
// conversation that unsent kept a draft for it, and asks it to check with
// the user. It never sends anything, never uses initialUserMessage, and
// names the draft without carrying its text beyond the first line.

// hookInput is the part of Claude Code's hook input the note needs
// (docs/research/claude.md section 10).
type hookInput struct {
	SessionID     string `json:"session_id"`
	Cwd           string `json:"cwd"`
	Source        string `json:"source"`
	HookEventName string `json:"hook_event_name"`
}

// hookNote is the whole output of a hook that has a note: one field, the
// additionalContext Claude Code hands the model with the next message.
type hookNote struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// hookConfig is the settings.json fragment the user pastes in.
type hookConfig struct {
	Hooks struct {
		SessionStart []hookMatcher `json:"SessionStart"`
	} `json:"hooks"`
}

type hookMatcher struct {
	Hooks []hookCommand `json:"hooks"`
}

type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// hookExe is the running unsent binary as an absolute path, so the shell
// Claude Code runs the hook in needs no unsent on its PATH. It keeps
// symlinks: Homebrew's bin/unsent survives an upgrade, while the versioned
// Cellar path it points at is removed. When the unsent on PATH is this same
// binary, that path wins, since on Linux os.Executable has already resolved
// the link. A test swaps it for a fixed path.
var hookExe = func() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if q, err := exec.LookPath("unsent"); err == nil && filepath.IsAbs(q) {
		if a, err := os.Stat(p); err == nil {
			if b, err := os.Stat(q); err == nil && os.SameFile(a, b) {
				return q, nil
			}
		}
	}
	return p, nil
}

// hookInputMax caps how much of the hook's stdin is read.
const hookInputMax = 1 << 20

func cmdHook(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 1 && args[0] == "claude":
		return printClaudeHook(stdout, stderr)
	case len(args) == 2 && args[0] == "claude" && args[1] == "--run":
		runClaudeHook(stdin, stdout)
		return 0
	}
	fmt.Fprintln(stderr, "unsent: usage: unsent hook claude (other agents are not supported yet)")
	return 2
}

// printClaudeHook prints the hook config on stdout and where it goes on
// stderr, so `unsent hook claude > file` holds only the JSON.
func printClaudeHook(stdout, stderr io.Writer) int {
	exe, err := hookExe()
	if err != nil {
		fmt.Fprintf(stderr, "unsent: cannot find the unsent binary: %v\n", err)
		return 1
	}
	var c hookConfig
	c.Hooks.SessionStart = []hookMatcher{{Hooks: []hookCommand{{Type: "command", Command: shellQuote(exe) + " hook claude --run"}}}}
	if code := printJSON(stdout, stderr, c); code != 0 {
		return code
	}
	settings := shortPath(filepath.Join(filepath.Dir(claudeSessions()), "settings.json"), 200)
	fmt.Fprintf(stderr, "unsent: add this to %s, next to any hooks already there; unsent never edits that file.\n", settings)
	fmt.Fprintln(stderr, "unsent: it names the unsent binary by its path, so run this again if unsent moves.")
	return 0
}

// runClaudeHook prints the note for a resumed conversation that left a
// draft, and nothing in every other case: another source, no draft, input
// or a store it cannot read, any error. The hook must never block or slow
// Claude Code, so it reads no terminal, runs no command and always exits 0.
func runClaudeHook(stdin io.Reader, stdout io.Writer) {
	defer func() { recover() }()
	if f, ok := stdin.(*os.File); ok {
		if fi, err := f.Stat(); err != nil || fi.Mode()&os.ModeCharDevice != 0 {
			return // a terminal: nobody piped the hook's input in
		}
	}
	data, err := io.ReadAll(io.LimitReader(stdin, hookInputMax))
	if err != nil {
		return
	}
	var in hookInput
	if json.Unmarshal(data, &in) != nil || in.HookEventName != "SessionStart" || in.Source != "resume" || in.SessionID == "" || in.Cwd == "" {
		return
	}
	st := existingStore()
	if st == nil {
		return
	}
	r := st.sessionOrphan("claude", in.Cwd, in.SessionID)
	if r == nil {
		return
	}
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	enc.Encode(hookOutput(claudeNote(r)))
}

// hookOutput is what the hook prints for a note: additionalContext and
// nothing else. A test swaps it for one that adds initialUserMessage, which
// the output check must catch.
var hookOutput = func(note string) any {
	var out hookNote
	out.HookSpecificOutput.HookEventName = "SessionStart"
	out.HookSpecificOutput.AdditionalContext = note
	return out
}

// claudeNote names the draft for the model: when, how long, how it starts.
// It asks the model to check with the user, never to act on the draft: a
// half-typed thought acted on alone is worse than a lost one.
func claudeNote(r *record) string {
	return fmt.Sprintf("unsent kept a draft for this conversation from %s, %s, starting \"%s\". "+
		"It may already be back in the input box. Ask the user whether to continue with it; do not act on it.",
		when(r.Updated), lines(r.Draft), firstLine(r.Draft))
}

// existingStore is the store as it is on disk, or nil when there is none:
// the hook runs at every session start, so it makes no folder and imports
// no shell logs.
func existingStore() *store {
	dir, err := stateDir()
	if err != nil {
		return nil
	}
	if fi, err := os.Stat(filepath.Join(dir, "drafts")); err != nil || !fi.IsDir() {
		return nil
	}
	return &store{dir: dir}
}

// shellQuote quotes p for sh when it holds anything but plain path
// characters.
func shellQuote(p string) string {
	if p != "" && strings.Trim(p, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._+-@%:,=") == "" {
		return p
	}
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}
