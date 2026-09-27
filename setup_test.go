package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// setupHome points HOME, ZDOTDIR and the state folder at a scratch folder,
// so no test reads or writes a real rc file, and puts a fake claude and a
// fake unsent on PATH. The fakes print their arguments, one per line.
type setupHome struct {
	home, agentBin, unsentBin string
}

func newSetupHome(t *testing.T) setupHome {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", home)
	t.Setenv("UNSENT_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("SHELL", "/bin/zsh")
	h := setupHome{home: home, agentBin: filepath.Join(home, "agent-bin"), unsentBin: filepath.Join(home, "unsent-bin")}
	writeScript(t, filepath.Join(h.agentBin, "claude"), `printf 'claude[%s]\n' "$@"`)
	writeScript(t, filepath.Join(h.unsentBin, "unsent"), `printf 'unsent[%s]\n' "$@"`)
	t.Setenv("PATH", h.agentBin+":"+h.unsentBin+":/usr/bin:/bin")
	for _, sh := range setupShells {
		rc, err := rcFile(sh)
		if err != nil || !strings.HasPrefix(rc, home) {
			t.Fatalf("rc file %q is outside the scratch home %q (%v)", rc, home, err)
		}
	}
	return h
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (h setupHome) rc(t *testing.T, shell string) string {
	t.Helper()
	rc, err := rcFile(shell)
	if err != nil {
		t.Fatal(err)
	}
	return rc
}

func runSetup(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = run(append([]string{"setup"}, args...), &o, &e)
	return code, o.String(), e.String()
}

func mustSetup(t *testing.T, args ...string) string {
	t.Helper()
	code, out, errOut := runSetup(t, args...)
	if code != 0 {
		t.Fatalf("unsent setup %v: exit %d\n%s%s", args, code, out, errOut)
	}
	return out
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// testShells are the real shells on this machine, one per binary: bash 3.2
// and zsh on macOS, Homebrew's bash 5 where it is installed, and the Linux
// ones in CI. A shell that is missing is skipped.
func testShells(t *testing.T) []string {
	var out, seen []string
	for _, p := range []string{
		"/bin/zsh", "/usr/bin/zsh", "/opt/homebrew/bin/zsh", "/usr/local/bin/zsh",
		"/bin/bash", "/usr/bin/bash", "/opt/homebrew/bin/bash", "/usr/local/bin/bash",
	} {
		real, err := filepath.EvalSymlinks(p)
		if err != nil || slices.Contains(seen, real) {
			continue
		}
		if fi, err := os.Stat(real); err != nil || fi.Mode()&0o111 == 0 {
			continue
		}
		seen = append(seen, real)
		out = append(out, p)
	}
	if len(out) == 0 {
		t.Skip("no zsh or bash on this machine")
	}
	return out
}

func shellKind(sh string) string {
	if strings.Contains(filepath.Base(sh), "zsh") {
		return "zsh"
	}
	return "bash"
}

// interactive runs script in an interactive shell that reads the scratch rc
// file, with an empty environment but for HOME, ZDOTDIR and path: the same
// as env -i. No real agent is on that PATH.
func (h setupHome) interactive(t *testing.T, sh, path, script string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sh, "-i", "-c", script)
	cmd.Env = []string{"HOME=" + h.home, "ZDOTDIR=" + h.home, "PATH=" + path, "TERM=dumb"}
	cmd.Dir = h.home
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		t.Fatalf("%s timed out; stderr:\n%s", sh, stderr.String())
	}
	_ = err // an interactive shell without a terminal may exit non-zero
	t.Logf("%s stderr: %s", sh, stderr.String())
	return string(out)
}

// wherePath prints where the shell finds a program on PATH, ignoring
// aliases and functions.
func wherePath(sh, name string) string {
	if shellKind(sh) == "zsh" {
		return "whence -p " + name
	}
	return "type -P " + name
}

// shellVersion is a shell's version line, for the test log.
func shellVersion(sh string) string {
	out, _ := exec.Command(sh, "--version").Output()
	first, _, _ := strings.Cut(string(out), "\n")
	return first
}

// TestSetupInRealShells runs the block in each real shell: with an alias of
// the agent's name before it, the wrapper still runs the agent through
// unsent with the alias's flag, and the lines after the block still run.
func TestSetupInRealShells(t *testing.T) {
	for _, sh := range testShells(t) {
		t.Run(sh, func(t *testing.T) {
			t.Log(shellVersion(sh))
			h := newSetupHome(t)
			kind := shellKind(sh)
			rc := h.rc(t, kind)
			path := h.agentBin + ":" + h.unsentBin + ":/usr/bin:/bin"

			if err := os.WriteFile(rc, []byte("alias claude='claude --flag'\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			mustSetup(t, kind)
			f, err := os.OpenFile(rc, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			f.WriteString("UNSENT_TAIL=ran\n")
			f.Close()

			script := wherePath(sh, "claude") + "; claude hello 'two words'; echo \"tail=$UNSENT_TAIL\""
			got := h.interactive(t, sh, path, script)
			want := h.agentBin + "/claude\nunsent[claude]\nunsent[--flag]\nunsent[hello]\nunsent[two words]\ntail=ran\n"
			if got != want {
				t.Fatalf("with an alias before the block:\ngot:\n%s\nwant:\n%s", got, want)
			}

			// Sourcing the rc file again keeps the wrapper.
			got = h.interactive(t, sh, path, ". "+rc+"; . "+rc+"; claude again")
			if want := "unsent[claude]\nunsent[--flag]\nunsent[again]\n"; got != want {
				t.Fatalf("after sourcing the rc file twice:\ngot:\n%s\nwant:\n%s", got, want)
			}

			// With unsent gone from PATH, the wrapper runs the agent itself.
			got = h.interactive(t, sh, h.agentBin+":/usr/bin:/bin", "claude hi")
			if want := "claude[--flag]\nclaude[hi]\n"; got != want {
				t.Fatalf("without unsent on PATH:\ngot:\n%s\nwant:\n%s", got, want)
			}

			// With the agent not installed, no wrapper is defined.
			got = h.interactive(t, sh, h.unsentBin+":/usr/bin:/bin", "typeset -f claude >/dev/null && echo wrapped || echo none")
			if got != "none\n" {
				t.Fatalf("without claude on PATH: got %q, want none", got)
			}
		})
	}
}

// TestSetupKeepsTheUsersOwnFunction checks the block leaves a function of
// the agent's name alone, and that setup says it does.
func TestSetupKeepsTheUsersOwnFunction(t *testing.T) {
	for _, sh := range testShells(t) {
		t.Run(sh, func(t *testing.T) {
			h := newSetupHome(t)
			kind := shellKind(sh)
			rc := h.rc(t, kind)
			if err := os.WriteFile(rc, []byte("function claude { printf 'mine[%s]\\n' \"$@\"; }\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			out := mustSetup(t, kind)
			if !strings.Contains(out, ":1: defines its own claude function") {
				t.Errorf("setup did not warn about the user's claude function:\n%s", out)
			}
			got := h.interactive(t, sh, h.agentBin+":"+h.unsentBin+":/usr/bin:/bin", "claude x")
			if got != "mine[x]\n" {
				t.Fatalf("the user's function was replaced: got %q", got)
			}
		})
	}
}

// TestSetupShellChecksCanFail proves the shell checks above go red: the
// `name() { }` form under an alias, and a block that replaces the user's
// own function, both fail them.
func TestSetupShellChecksCanFail(t *testing.T) {
	for _, sh := range testShells(t) {
		t.Run(sh, func(t *testing.T) {
			h := newSetupHome(t)
			kind := shellKind(sh)
			rc := h.rc(t, kind)
			path := h.agentBin + ":" + h.unsentBin + ":/usr/bin:/bin"

			block := setupBlock(kind)
			paren := strings.Replace(block, "function $_unsent_a {", "$_unsent_a() {", 1)
			if paren == block {
				t.Fatal("the mutation found nothing to change")
			}
			os.WriteFile(rc, []byte("alias claude='claude --flag'\n"+paren+"UNSENT_TAIL=ran\n"), 0o644)
			got := h.interactive(t, sh, path, "claude hello; echo \"tail=$UNSENT_TAIL\"")
			if strings.HasPrefix(got, "unsent[claude]\nunsent[--flag]\nunsent[hello]\ntail=ran") {
				t.Fatalf("the name() form passed the alias check: %q", got)
			}

			guard := strings.Replace(block, " && ! ", " && : ", 1)
			if guard == block {
				t.Fatal("the mutation found nothing to change")
			}
			os.WriteFile(rc, []byte("function claude { printf 'mine[%s]\\n' \"$@\"; }\n"+guard), 0o644)
			if got := h.interactive(t, sh, path, "claude x"); got == "mine[x]\n" {
				t.Fatalf("a block that replaces the user's function passed: %q", got)
			}
		})
	}
}

// TestSetupUndoRestoresTheFile checks setup twice leaves one block, and
// undo gives back the file byte for byte, mode included.
func TestSetupUndoRestoresTheFile(t *testing.T) {
	for _, shell := range setupShells {
		for name, orig := range map[string]string{
			"empty":             "",
			"one line":          "export A=1\n",
			"no final newline":  "export A=1",
			"alias and blanks":  "alias claude='claude --flag'\n# mine\n\n",
			"crlf":              "export A=1\r\n",
			"two blank endings": "x\n\n\n",
		} {
			t.Run(shell+"/"+name, func(t *testing.T) {
				h := newSetupHome(t)
				rc := h.rc(t, shell)
				if err := os.WriteFile(rc, []byte(orig), 0o640); err != nil {
					t.Fatal(err)
				}
				mustSetup(t, shell)
				once := readFile(t, rc)
				if strings.Count(once, rcBlockStart) != 1 || !strings.HasPrefix(once, orig) {
					t.Fatalf("after setup:\n%q", once)
				}
				mustSetup(t, shell)
				if twice := readFile(t, rc); twice != once {
					t.Fatalf("setup twice changed the file:\n%q\n%q", once, twice)
				}
				mustSetup(t, "--undo", shell)
				if got := readFile(t, rc); got != orig {
					t.Fatalf("undo left %q, want %q", got, orig)
				}
				fi, _ := os.Stat(rc)
				if fi.Mode().Perm() != 0o640 {
					t.Errorf("mode %v, want 0640", fi.Mode().Perm())
				}
			})
		}
	}
}

// TestSetupUndoKeepsTheUsersLines checks undo removes only the block when
// the user wrote around it, and that a second setup rewrites the block in
// place.
func TestSetupUndoKeepsTheUsersLines(t *testing.T) {
	h := newSetupHome(t)
	rc := h.rc(t, "zsh")
	os.WriteFile(rc, []byte("before\n"), 0o644)
	mustSetup(t, "zsh")
	f, _ := os.OpenFile(rc, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("after\n")
	f.Close()

	// An older block, in the middle of the file, is rewritten where it is.
	old := readFile(t, rc)
	stale := strings.Replace(old, "for _unsent_a in claude;", "for _unsent_a in claude oldagent;", 1)
	os.WriteFile(rc, []byte(stale), 0o644)
	out := mustSetup(t, "zsh")
	if got := readFile(t, rc); got != old {
		t.Fatalf("setup did not rewrite the block in place:\n%q\nwant\n%q", got, old)
	}
	if !strings.Contains(out, "Rewrote the unsent block") {
		t.Errorf("setup output: %s", out)
	}

	out = mustSetup(t, "--undo")
	if got := readFile(t, rc); got != "before\nafter\n" {
		t.Fatalf("undo left %q", got)
	}
	if !strings.Contains(out, "No unsent block in ~/.bashrc") || !strings.Contains(out, "Your drafts stay in ~/state") {
		t.Errorf("undo output: %s", out)
	}
	// Undo again finds nothing and changes nothing.
	out = mustSetup(t, "--undo", "zsh")
	if !strings.Contains(out, "No unsent block in ~/.zshrc") {
		t.Errorf("second undo: %s", out)
	}
}

// TestSetupUndoCleansEveryShell checks undo with no shell named removes the
// block from both rc files, and that a missing rc file is created.
func TestSetupUndoCleansEveryShell(t *testing.T) {
	h := newSetupHome(t)
	mustSetup(t, "zsh")
	mustSetup(t, "bash")
	for _, sh := range setupShells {
		if !strings.Contains(readFile(t, h.rc(t, sh)), rcBlockStart) {
			t.Fatalf("no block in the %s rc file", sh)
		}
	}
	mustSetup(t, "--undo")
	for _, sh := range setupShells {
		if got := readFile(t, h.rc(t, sh)); got != "" {
			t.Fatalf("the %s rc file kept %q", sh, got)
		}
	}
}

// TestSetupDefaultsToShell checks setup picks the rc file from SHELL.
func TestSetupDefaultsToShell(t *testing.T) {
	h := newSetupHome(t)
	t.Setenv("SHELL", "/opt/homebrew/bin/bash")
	out := mustSetup(t)
	if !strings.Contains(readFile(t, h.rc(t, "bash")), rcBlockStart) {
		t.Fatal("no block in ~/.bashrc")
	}
	if !strings.Contains(out, "From the next shell, claude runs through unsent (~/agent-bin/claude)") {
		t.Errorf("setup output: %s", out)
	}
	t.Setenv("PATH", h.unsentBin+":/usr/bin:/bin")
	if out := mustSetup(t, "bash"); !strings.Contains(out, "claude is not on PATH now") {
		t.Errorf("setup without claude on PATH: %s", out)
	}
}

func TestSetupArguments(t *testing.T) {
	newSetupHome(t)
	for _, c := range []struct {
		shell string
		args  []string
		want  string
	}{
		{"/bin/zsh", []string{"fish"}, "fish is not supported yet"},
		{"/bin/zsh", []string{"tcsh"}, "usage: unsent setup"},
		{"/bin/zsh", []string{"zsh", "bash"}, "usage: unsent setup"},
		{"/bin/sh", nil, `your shell is "/bin/sh"`},
		{"", nil, `your shell is ""`},
	} {
		t.Setenv("SHELL", c.shell)
		code, _, errOut := runSetup(t, c.args...)
		if code != 2 || !strings.Contains(errOut, c.want) {
			t.Errorf("setup %v with SHELL=%q: exit %d, %q", c.args, c.shell, code, errOut)
		}
	}
}

// TestSetupRefusesABrokenBlock checks a start line without its end line,
// or an end line alone, stops setup and undo before they change anything.
func TestSetupRefusesABrokenBlock(t *testing.T) {
	h := newSetupHome(t)
	rc := h.rc(t, "zsh")
	for _, text := range []string{
		"a\n" + rcBlockStart + "\nb\n",
		"a\n" + rcBlockEnd + "\n",
		rcBlockStart + "\n" + rcBlockStart + "\n" + rcBlockEnd + "\n",
	} {
		os.WriteFile(rc, []byte(text), 0o644)
		for _, args := range [][]string{{"zsh"}, {"--undo", "zsh"}} {
			code, _, errOut := runSetup(t, args...)
			if code != 1 || !strings.Contains(errOut, "fix it by hand") {
				t.Errorf("setup %v on %q: exit %d, %q", args, text, code, errOut)
			}
			if got := readFile(t, rc); got != text {
				t.Fatalf("setup %v changed a broken file: %q", args, got)
			}
		}
	}
}

// TestSetupWritesThroughASymlink checks an rc file kept in a dotfiles folder
// stays a symlink.
func TestSetupWritesThroughASymlink(t *testing.T) {
	h := newSetupHome(t)
	rc := h.rc(t, "zsh")
	target := filepath.Join(h.home, "dotfiles", "zshrc")
	os.MkdirAll(filepath.Dir(target), 0o755)
	os.WriteFile(target, []byte("export A=1\n"), 0o600)
	if err := os.Symlink(target, rc); err != nil {
		t.Fatal(err)
	}
	mustSetup(t, "zsh")
	if fi, err := os.Lstat(rc); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the rc file is no longer a symlink: %v", err)
	}
	if !strings.Contains(readFile(t, target), rcBlockStart) {
		t.Fatal("the symlink's target has no block")
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
	mustSetup(t, "--undo", "zsh")
	if got := readFile(t, target); got != "export A=1\n" {
		t.Fatalf("undo left %q", got)
	}
}

// TestSetupWarnsAboutBypasses checks setup names every alias and launcher
// that starts the agent without the wrapper, and nothing else.
func TestSetupWarnsAboutBypasses(t *testing.T) {
	h := newSetupHome(t)
	rc := h.rc(t, "zsh")
	lines := []string{
		`alias claude=~/.local/bin/claude`,                          // 1: an alias to a path
		`cc() { env "${_cc_clean_env[@]}" FOO=1 claude --x "$@"; }`, // 2: env
		`alias cx='command claude'`,                                 // 3: command
		`function ccr { /usr/local/bin/claude "$@"; }`,              // 4: a full path
		`run_it() {`,                   // 5
		`  exec claude "$@"`,           // 6: exec
		`}`,                            // 7
		`alias claude='claude --flag'`, // 8: fine
		`if command -v claude >/dev/null; then echo yes; fi`, // 9: fine, only asks
		`export CLAUDE_BIN=$HOME/.local/bin/claude`,          // 10: fine, no command
		`alias claude="unsent claude"`,                       // 11: fine
		`# env claude in a comment`,                          // 12: fine
		`ok() { claude "$@"; }`,                              // 13: fine, goes through the function
		`alias cl2='env -u X \`,                              // 14: env, over two lines
		`  claude'`,                                          // 15
		`alias other=/usr/bin/true`,                          // 16: fine, not an agent
		`alias claude2="$HOME/.local/bin/claude"`,            // 17: a full path
	}
	os.WriteFile(rc, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	os.WriteFile(filepath.Join(h.home, ".zprofile"), []byte("export PATH=/x\nalias cz='env claude'\n"), 0o644)
	out := mustSetup(t, "zsh")
	want := map[string]string{
		".zshrc:1:":    "alias claude points at a path",
		".zshrc:2:":    "starts claude through env",
		".zshrc:3:":    "alias cx starts claude through command",
		".zshrc:4:":    "starts claude by its full path",
		".zshrc:6:":    "starts claude through exec",
		".zshrc:14:":   "alias cl2 starts claude through env",
		".zshrc:17:":   "alias claude2 starts claude by its full path",
		".zprofile:2:": "alias cz starts claude through env",
	}
	for at, why := range want {
		if !strings.Contains(out, "~/"+at+" "+why) {
			t.Errorf("no warning %q %q in:\n%s", at, why, out)
		}
	}
	if got := strings.Count(out, "\n  ~/"); got != len(want) {
		t.Errorf("%d warnings, want %d:\n%s", got, len(want), out)
	}
	// The block itself is never a bypass.
	if out := mustSetup(t, "zsh"); strings.Count(out, "\n  ~/") != len(want) {
		t.Errorf("a second setup warns about its own block:\n%s", out)
	}
}

// TestSetupWarnsWhenBashLoginSkipsBashrc checks the warning for a login
// shell that never reads ~/.bashrc.
func TestSetupWarnsWhenBashLoginSkipsBashrc(t *testing.T) {
	h := newSetupHome(t)
	if out := mustSetup(t, "bash"); !strings.Contains(out, "~/.bash_profile, which does not exist") {
		t.Errorf("no warning without a profile:\n%s", out)
	}
	profile := filepath.Join(h.home, ".bash_profile")
	os.WriteFile(profile, []byte("export A=1\n"), 0o644)
	if out := mustSetup(t, "bash"); !strings.Contains(out, "~/.bash_profile: bash login shells read this file, and it never sources ~/.bashrc") {
		t.Errorf("no warning for a profile without bashrc:\n%s", out)
	}
	os.WriteFile(profile, []byte("[ -f ~/.bashrc ] && . ~/.bashrc\n"), 0o644)
	if out := mustSetup(t, "bash"); strings.Contains(out, "Warning") {
		t.Errorf("warned for a profile that sources bashrc:\n%s", out)
	}
	if out := mustSetup(t, "zsh"); strings.Contains(out, "bash_profile") {
		t.Errorf("zsh setup warned about bash:\n%s", out)
	}
}
