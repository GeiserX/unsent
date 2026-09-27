package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

// runStatus runs `unsent status` for one real shell binary, asking it with
// an empty environment but for HOME, ZDOTDIR, TERM and path, as env -i
// does, so no real rc file or agent is reached.
func (h setupHome) runStatus(t *testing.T, sh, path string) string {
	t.Helper()
	orig := statusEnv
	t.Cleanup(func() { statusEnv = orig })
	statusEnv = func() []string {
		return []string{"HOME=" + h.home, "ZDOTDIR=" + h.zdotdir, "PATH=" + path, "TERM=dumb"}
	}
	t.Setenv("SHELL", sh)
	var o, e bytes.Buffer
	if code := run([]string{"status", shellKind(sh)}, &o, &e); code != 0 {
		t.Fatalf("unsent status: exit %d\n%s%s", code, o.String(), e.String())
	}
	return o.String()
}

// statusLine is the one line of status output that starts with label.
func statusLine(t *testing.T, out, label string) string {
	t.Helper()
	var found []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, label+": ") {
			found = append(found, strings.TrimPrefix(l, label+": "))
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d lines for %q in:\n%s", len(found), label, out)
	}
	return found[0]
}

// loginReadsRC gives bash's login shell, which status asks, a
// ~/.bash_profile that reads ~/.bashrc, as setup tells bash users to.
func (h setupHome) loginReadsRC(t *testing.T, kind string) {
	t.Helper()
	if kind == "bash" {
		os.WriteFile(filepath.Join(h.home, ".bash_profile"), []byte(". ~/.bashrc\n"), 0o644)
	}
}

// skipIfSystemAgents skips a test that needs a PATH with no claude and no
// unsent when the system's own login files (path_helper on macOS) put one
// there, so no expected line depends on what the machine has installed.
func (h setupHome) skipIfSystemAgents(t *testing.T, sh string) {
	t.Helper()
	orig := statusEnv
	defer func() { statusEnv = orig }()
	statusEnv = func() []string {
		return []string{"HOME=" + h.home, "ZDOTDIR=" + h.zdotdir, "PATH=/usr/bin:/bin", "TERM=dumb"}
	}
	p, err := runProbe(sh, shellKind(sh), []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}
	if c := p.agents["claude"].path; c != "" || p.unsent != "" {
		t.Skipf("the system login files put claude (%q) or unsent (%q) on PATH", c, p.unsent)
	}
}

// TestStatusInRealShells asks each real shell, through the scratch rc file,
// what claude runs: nothing wraps it before setup, the block's function
// after, and each way it can still skip unsent is named.
func TestStatusInRealShells(t *testing.T) {
	for _, sh := range testShells(t) {
		t.Run(sh, func(t *testing.T) {
			h := newSetupHome(t)
			h.skipIfSystemAgents(t, sh)
			kind := shellKind(sh)
			rc := h.rc(t, kind)
			h.loginReadsRC(t, kind)
			path := h.agentBin + ":" + h.unsentBin + ":/usr/bin:/bin"
			noAgent := h.unsentBin + ":/usr/bin:/bin"

			// Nothing named claude at all: zsh says "none", bash nothing.
			if got := statusLine(t, h.runStatus(t, sh, noAgent), "claude"); got != "not on PATH" {
				t.Errorf("claude with no agent and no block: %q", got)
			}
			out := h.runStatus(t, sh, path)
			if got := statusLine(t, out, "shell"); got != kind+" ("+sh+")" {
				t.Errorf("shell: %q", got)
			}
			if got := statusLine(t, out, "setup block"); got != "none in "+tilde(rc)+"; run unsent setup "+kind {
				t.Errorf("setup block before setup: %q", got)
			}
			if got := statusLine(t, out, "claude"); !strings.HasPrefix(got, "runs directly: no wrapper function") {
				t.Errorf("claude before setup: %q", got)
			}
			if got := statusLine(t, out, "claude profile"); got != "claude, reader last verified on version "+claude.verified {
				t.Errorf("profile: %q", got)
			}
			notSaved := "not saved; unsent saves only zsh's so far"
			if kind == "zsh" {
				notSaved = "not saved, the hooks do not load in this shell; run unsent setup zsh"
			}
			if got := statusLine(t, out, "command line"); got != notSaved {
				t.Errorf("command line before setup: %q", got)
			}

			// An alias of its own name and no block yet.
			os.WriteFile(rc, []byte("alias claude='claude --flag'\n"), 0o644)
			out = h.runStatus(t, sh, path)
			if got := statusLine(t, out, "claude"); got != "runs directly: alias claude='claude --flag', and no wrapper function; run unsent setup" {
				t.Errorf("claude under an alias of its own name, before setup: %q", got)
			}
			if got := statusLine(t, h.runStatus(t, sh, noAgent), "claude"); got != "not on PATH" {
				t.Errorf("claude under an alias of its own name, no agent: %q", got)
			}
			mustSetup(t, kind)
			out = h.runStatus(t, sh, path)
			if got := statusLine(t, out, "setup block"); got != "in "+tilde(rc)+", up to date" {
				t.Errorf("setup block after setup: %q", got)
			}
			if got := statusLine(t, out, "claude"); got != "goes through unsent (the claude function), runs ~/agent-bin/claude" {
				t.Errorf("claude after setup, under an alias of its own name: %q", got)
			}
			saved := notSaved
			if kind == "zsh" {
				saved = "saved as you type; a line left unrun shows in unsent list"
			}
			if got := statusLine(t, out, "command line"); got != saved {
				t.Errorf("command line after setup: %q", got)
			}
			if l := h.liveLogs(t); len(l) > 0 {
				t.Errorf("asking the shell opened a log: %v", l)
			}

			// unsent missing from the shell's PATH: the wrapper falls back.
			out = h.runStatus(t, sh, h.agentBin+":/usr/bin:/bin")
			if got := statusLine(t, out, "claude"); !strings.Contains(got, "unsent is not on this shell's PATH") {
				t.Errorf("claude without unsent on PATH: %q", got)
			}
			// The agent not installed.
			out = h.runStatus(t, sh, noAgent)
			if got := statusLine(t, out, "claude"); got != "not on PATH; the wrapper covers it once it is" {
				t.Errorf("claude not installed: %q", got)
			}
			// An rc file that ends in output with no line break.
			appendLine(t, rc, "printf 'welcome'")
			out = h.runStatus(t, sh, path)
			if got := statusLine(t, out, "claude"); got != "goes through unsent (the claude function), runs ~/agent-bin/claude" {
				t.Errorf("claude after an rc file that prints with no line break: %q", got)
			}

			// An alias after the block that points at a path wins over the
			// function, and the scan names the line too.
			appendLine(t, rc, "alias claude="+h.agentBin+"/claude")
			line := strings.Count(readFile(t, rc), "\n")
			out = h.runStatus(t, sh, path)
			if got := statusLine(t, out, "claude"); !strings.HasPrefix(got, "runs directly: alias claude=") || !strings.HasSuffix(got, "/agent-bin/claude' skips the wrapper") {
				t.Errorf("claude under an alias to a path: %q", got)
			}
			if !strings.Contains(out, fmt.Sprintf("bypass: %s:%d: alias claude points at a path", tilde(rc), line)) {
				t.Errorf("no bypass line for the alias:\n%s", out)
			}

			// The user's own function, which the block leaves alone.
			os.WriteFile(rc, []byte("function claude { printf 'mine[%s]\\n' \"$@\"; }\n"), 0o644)
			mustSetup(t, kind)
			out = h.runStatus(t, sh, path)
			if got := statusLine(t, out, "claude"); got != "runs directly: your own claude function does not call `unsent claude`" {
				t.Errorf("claude under the user's own function: %q", got)
			}
			// One that calls unsent goes through it.
			os.WriteFile(rc, []byte("function claude { unsent claude --model x \"$@\"; }\n"), 0o644)
			mustSetup(t, kind)
			out = h.runStatus(t, sh, path)
			if got := statusLine(t, out, "claude"); got != "goes through unsent (the claude function), runs ~/agent-bin/claude" {
				t.Errorf("claude under the user's function that calls unsent: %q", got)
			}
			// An alias to unsent itself.
			os.WriteFile(rc, []byte("alias claude='unsent claude'\n"), 0o644)
			out = h.runStatus(t, sh, path)
			if got := statusLine(t, out, "claude"); got != "goes through unsent (alias claude='unsent claude')" {
				t.Errorf("claude as an alias to unsent: %q", got)
			}
			// The same alias with unsent missing runs nothing at all.
			out = h.runStatus(t, sh, h.agentBin+":/usr/bin:/bin")
			if got := statusLine(t, out, "claude"); got != "fails: alias claude='unsent claude' calls unsent, which is not on this shell's PATH" {
				t.Errorf("claude as an alias to unsent, with no unsent on PATH: %q", got)
			}
		})
	}
}

// TestStatusReadsLoginFiles checks status asks the shell a new terminal
// starts, a login shell: a function in ~/.zlogin replaces the block's, and a
// ~/.bash_profile that never reads ~/.bashrc leaves the block unread.
func TestStatusReadsLoginFiles(t *testing.T) {
	for _, sh := range testShells(t) {
		t.Run(sh, func(t *testing.T) {
			h := newSetupHome(t)
			kind := shellKind(sh)
			path := h.agentBin + ":" + h.unsentBin + ":/usr/bin:/bin"
			mustSetup(t, kind)
			if kind == "zsh" {
				os.WriteFile(filepath.Join(h.zdotdir, ".zlogin"), []byte("function claude { command claude \"$@\"; }\n"), 0o644)
				out := h.runStatus(t, sh, path)
				if got := statusLine(t, out, "claude"); got != "runs directly: your own claude function does not call `unsent claude`" {
					t.Errorf("claude under a function in .zlogin: %q", got)
				}
				return
			}
			os.WriteFile(filepath.Join(h.home, ".bash_profile"), nil, 0o644)
			out := h.runStatus(t, sh, path)
			if got := statusLine(t, out, "claude"); strings.HasPrefix(got, "goes through unsent") {
				t.Errorf("claude with a .bash_profile that never reads .bashrc: %q", got)
			}
			if !strings.Contains(out, "bypass: ~/.bash_profile: bash login shells read this file, and it never sources ~/.bashrc") {
				t.Errorf("no bypass line for .bash_profile:\n%s", out)
			}
			h.loginReadsRC(t, kind)
			if got := statusLine(t, h.runStatus(t, sh, path), "claude"); !strings.HasPrefix(got, "goes through unsent") {
				t.Errorf("claude with a .bash_profile that reads .bashrc: %q", got)
			}
		})
	}
}

// TestStatusProbeIsBounded checks status returns soon after its timeout
// even when the startup files leave a job running with stdout open, and
// that an answer the shell printed in full counts though the shell then
// hangs or leaves such a job behind.
func TestStatusProbeIsBounded(t *testing.T) {
	sh := testShells(t)[0]
	noRC := "-f"
	if shellKind(sh) == "bash" {
		noRC = "--norc --noprofile"
	}
	// Each fake runs the probe script, its last argument, in the real
	// shell with no rc file.
	answer := `for a; do s=$a; done; ` + sh + ` ` + noRC + ` -c "$s"`
	for _, c := range []struct {
		name, script, want string
	}{
		{"hangs, a job holds stdout", "sleep 30 & sleep 30", "unknown, could not ask the shell: {fake} did not answer within 1s"},
		{"answers, then a job holds stdout", "sleep 30 & " + answer, "runs directly: no wrapper function in this shell; run unsent setup"},
		{"answers, then hangs", answer + "; sleep 30", "runs directly: no wrapper function in this shell; run unsent setup"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newSetupHome(t)
			orig := statusProbeTimeout
			t.Cleanup(func() { statusProbeTimeout = orig })
			statusProbeTimeout = time.Second
			fake := filepath.Join(h.home, "fake-shell", filepath.Base(sh))
			writeScript(t, fake, c.script)
			start := time.Now()
			out := h.runStatus(t, fake, h.agentBin+":"+h.unsentBin+":/usr/bin:/bin")
			if d := time.Since(start); d > 5*time.Second {
				t.Errorf("status took %v", d)
			}
			if got, want := statusLine(t, out, "claude"), strings.ReplaceAll(c.want, "{fake}", fake); got != want {
				t.Errorf("claude: %q, want %q", got, want)
			}
		})
	}
}

// TestStatusProbeTakesNoTerminal runs status in a terminal of its own and
// checks the shell it asks cannot open that terminal, so it cannot take it
// from whatever status runs beside (`unsent status | less`). A plain child
// of the same terminal can, which shows the check can fail.
func TestStatusProbeTakesNoTerminal(t *testing.T) {
	sh := testShells(t)[0]
	h := newSetupHome(t)
	kind := shellKind(sh)
	h.loginReadsRC(t, kind)
	tryTTY := func(file string) string {
		return "if { : </dev/tty; } 2>/dev/null; then echo has >" + file + "; else echo none >" + file + "; fi\n"
	}
	inTerminal := func(cmd *exec.Cmd) {
		t.Helper()
		ptmx, err := pty.Start(cmd)
		if err != nil {
			t.Fatal(err)
		}
		defer ptmx.Close()
		go io.Copy(io.Discard, ptmx)
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	control := filepath.Join(h.home, "control-tty")
	inTerminal(exec.Command("/bin/sh", "-c", tryTTY(control)))
	if got := strings.TrimSpace(readFile(t, control)); got != "has" {
		t.Fatalf("a plain child of the terminal: %q", got)
	}

	probed := filepath.Join(h.home, "probe-tty")
	os.WriteFile(h.rc(t, kind), []byte(tryTTY(probed)), 0o644)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "status", kind)
	cmd.Env = []string{"UNSENT_TEST_RUN=1", "HOME=" + h.home, "ZDOTDIR=" + h.zdotdir, "UNSENT_HOME=" + filepath.Join(h.home, "state"),
		"PATH=" + h.agentBin + ":" + h.unsentBin + ":/usr/bin:/bin", "TERM=dumb", "SHELL=" + sh}
	inTerminal(cmd)
	if got := strings.TrimSpace(readFile(t, probed)); got != "none" {
		t.Fatalf("the shell status asks opened its terminal: %q", got)
	}
}

// TestStatusChecksCanFail proves the "goes through unsent" line goes red:
// a block whose wrapper does not call unsent, and one that is not the block
// this build writes, both fail it.
func TestStatusChecksCanFail(t *testing.T) {
	for _, sh := range testShells(t) {
		t.Run(sh, func(t *testing.T) {
			h := newSetupHome(t)
			kind := shellKind(sh)
			rc := h.rc(t, kind)
			h.loginReadsRC(t, kind)
			block := setupBlock(kind)
			broken := strings.Replace(block, `then unsent $_unsent_a`, `then command $_unsent_a`, 1)
			if broken == block {
				t.Fatal("the mutation found nothing to change")
			}
			os.WriteFile(rc, []byte(broken), 0o644)
			out := h.runStatus(t, sh, h.agentBin+":"+h.unsentBin+":/usr/bin:/bin")
			if got := statusLine(t, out, "claude"); strings.HasPrefix(got, "goes through unsent") {
				t.Fatalf("a wrapper that never calls unsent passed: %q", got)
			}
			if got := statusLine(t, out, "setup block"); !strings.Contains(got, "older than this unsent") {
				t.Fatalf("a changed block passed as up to date: %q", got)
			}
		})
	}
}

// TestStatusSettings checks the saving and on-send lines follow UNSENT_OFF
// and the UNSENT_ON_SEND variables, and say which one set the value.
func TestStatusSettings(t *testing.T) {
	sh := testShells(t)[0]
	h := newSetupHome(t)
	path := h.agentBin + ":" + h.unsentBin + ":/usr/bin:/bin"
	for _, c := range []struct {
		off, all, one  string
		saving, onSend string
	}{
		{"", "", "", "on", "log (the default)"},
		{"0", "", "", "on", "log (the default)"},
		{"1", "", "", "off, UNSENT_OFF=1 runs every agent directly", "log (the default)"},
		{"", "delete", "", "on", "delete (UNSENT_ON_SEND)"},
		{"", "delete", "log", "on", "log (UNSENT_ON_SEND_CLAUDE)"},
		{"", "nonsense", "DELETE", "on", "delete (UNSENT_ON_SEND_CLAUDE)"},
	} {
		t.Setenv("UNSENT_OFF", c.off)
		t.Setenv("UNSENT_ON_SEND", c.all)
		t.Setenv("UNSENT_ON_SEND_CLAUDE", c.one)
		out := h.runStatus(t, sh, path)
		if got := statusLine(t, out, "saving"); got != c.saving {
			t.Errorf("%+v: saving %q", c, got)
		}
		if got := statusLine(t, out, "on send, claude"); got != c.onSend {
			t.Errorf("%+v: on send %q", c, got)
		}
	}
}

// TestStatusNamesLaunchers checks status lists every launcher that skips
// the wrapper, as setup warns about them, and says so when there is none.
func TestStatusNamesLaunchers(t *testing.T) {
	sh := testShells(t)[0]
	h := newSetupHome(t)
	kind := shellKind(sh)
	path := h.agentBin + ":" + h.unsentBin + ":/usr/bin:/bin"
	mustSetup(t, kind)
	h.loginReadsRC(t, kind)
	if out := h.runStatus(t, sh, path); statusLine(t, out, "bypass") != "none found in the startup files" {
		t.Errorf("bypass with no launcher:\n%s", out)
	}
	rc := h.rc(t, kind)
	appendLine(t, rc, `cc() { env FOO=1 claude "$@"; }`)
	appendLine(t, rc, `alias ccr='command claude'`)
	out := h.runStatus(t, sh, path)
	for _, want := range []string{"starts claude through env", "alias ccr starts claude through command"} {
		if !strings.Contains(out, "bypass: "+tilde(rc)) || !strings.Contains(out, want) {
			t.Errorf("no bypass line %q in:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "\nbypass: "); n != 2 {
		t.Errorf("%d bypass lines, want 2:\n%s", n, out)
	}
	// The wrapper itself still wins for plain claude.
	if got := statusLine(t, out, "claude"); !strings.HasPrefix(got, "goes through unsent") {
		t.Errorf("claude: %q", got)
	}
}

// TestStatusWhenTheShellCannotAnswer checks a shell that prints no answer
// makes status say so instead of guessing.
func TestStatusWhenTheShellCannotAnswer(t *testing.T) {
	h := newSetupHome(t)
	fake := filepath.Join(h.home, "fake-shell", "zsh")
	writeScript(t, fake, "echo 'rc noise'; exit 3")
	out := h.runStatus(t, fake, "/usr/bin:/bin")
	if got := statusLine(t, out, "claude"); !strings.HasPrefix(got, "unknown, could not ask the shell: "+fake) {
		t.Errorf("claude: %q", got)
	}
	if got := statusLine(t, out, "command line"); !strings.HasPrefix(got, "unknown, could not ask the shell: "+fake) {
		t.Errorf("command line: %q", got)
	}
	// The facts that need no shell are still there.
	statusLine(t, out, "claude profile")
	statusLine(t, out, "saving")
	statusLine(t, out, "on send, zsh")
}

// TestStatusShellOnSend checks the on-send line for zsh's command lines
// follows UNSENT_ON_SEND_ZSH, then UNSENT_ON_SEND, then the shells'
// default, and that the command line in an rc file kept by hand, with
// eval "$(unsent init zsh)", counts as saved. bash, which saves no line,
// gets no on-send line for one.
func TestStatusShellOnSend(t *testing.T) {
	sh := testZsh(t)
	h := newSetupHome(t)
	path := h.agentBin + ":" + h.unsentBin + ":/usr/bin:/bin"
	for _, c := range []struct{ all, zsh, want string }{
		{"", "", "delete (the default)"},
		{"log", "", "log (UNSENT_ON_SEND)"},
		{"log", "delete", "delete (UNSENT_ON_SEND_ZSH)"},
		{"", "LOG", "log (UNSENT_ON_SEND_ZSH)"},
	} {
		t.Setenv("UNSENT_ON_SEND", c.all)
		t.Setenv("UNSENT_ON_SEND_ZSH", c.zsh)
		if got := statusLine(t, h.runStatus(t, sh, path), "on send, zsh"); got != c.want {
			t.Errorf("%+v: on send, zsh %q", c, got)
		}
	}
	// The fake unsent on PATH prints the hooks, as unsent init zsh does.
	writeScript(t, filepath.Join(h.unsentBin, "unsent"), "cat <<'UNSENT_HOOKS'\n"+zshHooks+"UNSENT_HOOKS")
	os.WriteFile(h.rc(t, "zsh"), []byte(`eval "$(unsent init zsh)"`+"\n"), 0o644)
	if got := statusLine(t, h.runStatus(t, sh, path), "command line"); got != "saved as you type; a line left unrun shows in unsent list" {
		t.Errorf("command line with eval \"$(unsent init zsh)\": %q", got)
	}
	for _, b := range testShells(t) {
		if shellKind(b) == "bash" {
			if out := h.runStatus(t, b, path); strings.Contains(out, "on send, zsh") {
				t.Errorf("status bash names zsh's on-send setting:\n%s", out)
			}
			break
		}
	}
}

func TestStatusArguments(t *testing.T) {
	newSetupHome(t)
	for _, c := range []struct {
		shell string
		args  []string
		want  string
	}{
		{"/bin/zsh", []string{"fish"}, "usage: unsent status"},
		{"/bin/zsh", []string{"zsh", "bash"}, "usage: unsent status"},
		{"/bin/sh", nil, `your shell is "/bin/sh"`},
	} {
		t.Setenv("SHELL", c.shell)
		var o, e bytes.Buffer
		if code := run(append([]string{"status"}, c.args...), &o, &e); code != 2 || !strings.Contains(e.String(), c.want) {
			t.Errorf("status %v with SHELL=%q: exit %d, %q", c.args, c.shell, code, e.String())
		}
	}
}

// TestParseProbeSkipsRcOutput checks what the rc files print before the
// probe's mark, even a line that looks like its own, is ignored.
func TestParseProbeSkipsRcOutput(t *testing.T) {
	out := "@@kind claude alias\nhello from rc\n" + probeMark + "\n@@kind claude function\n@@path claude /x/claude\n@@body claude\nclaude () {\n unsent claude \"$@\"\n}\n\n@@end\n@@unsent /u/unsent\n"
	p, ok := parseProbe(out)
	r := p.agents["claude"]
	if !ok || r.kind != "function" || r.path != "/x/claude" || !callsUnsent(r.body, "claude") || p.unsent != "/u/unsent" {
		t.Fatalf("parseProbe: %+v %v", p, ok)
	}
	// An answer cut off before its last line does not count.
	if _, ok := parseProbe(out[:strings.Index(out, "@@unsent ")]); ok {
		t.Fatal("parsed an answer with no @@unsent line")
	}
	// Lines a logout file prints after the answer are not read.
	if p, ok := parseProbe(out + "@@kind claude file\n"); !ok || p.agents["claude"].kind != "function" {
		t.Fatalf("read past the answer: %+v %v", p, ok)
	}
	if _, ok := parseProbe("no mark here\n"); ok {
		t.Fatal("parsed output with no mark")
	}
}

func TestAliasValue(t *testing.T) {
	for printed, want := range map[string]string{
		`alias claude='claude --flag'`:           "claude --flag",
		`claude='claude --flag'`:                 "claude --flag",
		`claude=/usr/local/bin/claude`:           "/usr/local/bin/claude",
		`claude='env '\''A=1'\'' claude'`:        "env 'A=1' claude",
		`alias claude='unsent claude --model x'`: "unsent claude --model x",
	} {
		if got := aliasValue(printed); got != want {
			t.Errorf("aliasValue(%q) = %q, want %q", printed, got, want)
		}
	}
}
