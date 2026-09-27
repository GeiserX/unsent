package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// unsent status answers "am I protected?" for one shell, one fact per line.
// A process cannot see its parent shell's functions, so status asks the
// shell itself: it starts that shell as a login, interactive shell with this
// environment, so it reads the same startup files a new terminal would
// (terminals on macOS and tmux start login shells), and has it say what
// each agent name resolves to, and in zsh whether the command-line hooks
// loaded. It never starts an agent. Aliases and
// launchers that skip the wrapper come from the same text scan setup warns
// with; status only reports them.

// statusProbeTimeout bounds the interactive shell status asks.
var statusProbeTimeout = 10 * time.Second

// statusEnv is the environment of the shell status asks: this process's,
// so PATH is this shell's. Tests replace it with an empty one.
var statusEnv = os.Environ

// probeMark starts status's own output in the probe shell's stdout, after
// anything the rc files printed.
const probeMark = "@@unsent-status"

// probeScript prints, for each agent name, what the shell runs for it: the
// kind (alias, function, file, builtin, or nothing), the file on PATH, the
// alias, the function's body; for zsh, whether the command-line hooks
// loaded, which sets _unsent_fd whether the setup block or an
// eval "$(unsent init zsh)" line ran them; then where unsent is on PATH.
// The hooks open no log here: a shell run with -c never shows a prompt.
func probeScript(shell string, agents []string) string {
	kind, path, fn, hooks := `type -t "$_unsent_a"`, `type -P "$_unsent_a"`, `declare -f "$_unsent_a"`, ""
	if shell == "zsh" {
		kind, path, fn = `whence -w "$_unsent_a"`, `whence -p "$_unsent_a"`, `typeset -f "$_unsent_a"`
		hooks = `printf '@@hooks %s\n' "${+_unsent_fd}"` + "\n"
	}
	return "printf '\\n%s\\n' '" + probeMark + "'\n" +
		"for _unsent_a in " + strings.Join(agents, " ") + "; do\n" +
		"  _unsent_k=$(" + kind + " 2>/dev/null); _unsent_k=${_unsent_k##*: }\n" +
		`  printf '@@kind %s %s\n' "$_unsent_a" "$_unsent_k"` + "\n" +
		`  printf '@@path %s %s\n' "$_unsent_a" "$(` + path + ` 2>/dev/null)"` + "\n" +
		`  if [ "$_unsent_k" = alias ]; then printf '@@alias %s %s\n' "$_unsent_a" "$(alias "$_unsent_a")"; fi` + "\n" +
		"  if " + fn + " >/dev/null 2>&1; then\n" +
		`    printf '@@body %s\n' "$_unsent_a"; ` + fn + "; printf '\\n@@end\\n'\n" +
		"  fi\n" +
		"done\n" +
		hooks +
		`printf '@@unsent %s\n' "$(command -v unsent 2>/dev/null)"` + "\n"
}

// resolved is what the probe shell runs for one agent name.
type resolved struct {
	kind, path, alias, body string
}

// probe is the probe shell's answer: each agent's resolution, whether the
// zsh command-line hooks loaded, and where unsent is on its PATH.
type probe struct {
	agents map[string]resolved
	hooks  bool
	unsent string
}

// runProbe starts the shell as a new terminal would and reads its answer.
// The shell gets a session of its own: with no controlling terminal it
// cannot take the terminal from the job status runs in (`unsent status |
// less`), and on timeout one kill reaches everything the startup files
// started. WaitDelay stops the wait soon after the shell is gone, though a
// background job it started still holds stdout open.
func runProbe(shellPath, shell string, agents []string) (probe, error) {
	ctx, cancel := context.WithTimeout(context.Background(), statusProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shellPath, "-l", "-i", "-c", probeScript(shell, agents))
	cmd.Env = statusEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if p, ok := parseProbe(string(out)); ok {
		return p, nil
	}
	if ctx.Err() != nil {
		return probe{}, fmt.Errorf("%s did not answer within %v", shellPath, statusProbeTimeout)
	}
	if err == nil {
		err = fmt.Errorf("no answer")
	}
	return probe{}, fmt.Errorf("%s: %v", shellPath, err)
}

// parseProbe reads the lines after probeMark, up to the @@unsent line that
// ends a whole answer. Without that line the answer does not count.
func parseProbe(out string) (probe, bool) {
	i := strings.Index("\n"+out, "\n"+probeMark+"\n")
	if i < 0 {
		return probe{}, false
	}
	p := probe{agents: map[string]resolved{}}
	lines := strings.Split(out[i+len(probeMark)+1:], "\n")
	for j := 0; j < len(lines); j++ {
		tag, rest, _ := strings.Cut(lines[j], " ")
		switch tag {
		case "@@unsent":
			p.unsent = rest
			return p, true
		case "@@hooks":
			p.hooks = rest == "1"
			continue
		}
		name, value, _ := strings.Cut(rest, " ")
		r := p.agents[name]
		switch tag {
		case "@@kind":
			r.kind = value
		case "@@path":
			r.path = value
		case "@@alias":
			r.alias = value
		case "@@body":
			var body []string
			for j++; j < len(lines) && lines[j] != "@@end"; j++ {
				body = append(body, lines[j])
			}
			r.body = strings.Join(body, "\n")
		default:
			continue
		}
		p.agents[name] = r
	}
	return probe{}, false
}

// aliasValue is the text an alias stands for, from `alias name` as bash
// (`alias name='value'`) or zsh (`name='value'`) prints it.
func aliasValue(printed string) string {
	cmds := shellCommands(strings.TrimPrefix(printed, "alias "))
	if len(cmds) == 0 || len(cmds[0]) == 0 {
		return ""
	}
	_, value, _ := strings.Cut(cmds[0][0], "=")
	return value
}

// callsUnsent reports whether shell text runs `unsent <agent>` or
// `unsent --as`, directly or through env.
func callsUnsent(text, agent string) bool {
	for _, line := range strings.Split(text, "\n") {
		for _, words := range shellCommands(line) {
			for i, w := range words {
				if filepath.Base(w) == "unsent" && i+1 < len(words) && (words[i+1] == agent || words[i+1] == "--as") {
					return true
				}
			}
		}
	}
	return false
}

// route says whether an agent name runs through unsent in the probe shell,
// and why.
func route(a string, r resolved, unsentOnPath bool) (through bool, why string) {
	viaFunction := func() (bool, string) {
		switch {
		case !callsUnsent(r.body, a):
			return false, fmt.Sprintf("runs directly: your own %s function does not call `unsent %s`", a, a)
		case r.path == "":
			return false, "not on PATH; the wrapper covers it once it is"
		case !unsentOnPath:
			return false, "runs directly: unsent is not on this shell's PATH, so the wrapper falls back to the agent"
		}
		return true, "goes through unsent (the " + a + " function), runs " + tilde(r.path)
	}
	switch r.kind {
	case "alias":
		v := aliasValue(r.alias)
		cmds := shellCommands(v)
		first := ""
		if len(cmds) > 0 {
			first = firstCommand(cmds[0])
		}
		switch {
		case callsUnsent(v, a) && unsentOnPath:
			return true, fmt.Sprintf("goes through unsent (alias %s='%s')", a, v)
		case callsUnsent(v, a):
			return false, fmt.Sprintf("fails: alias %s='%s' calls unsent, which is not on this shell's PATH", a, v)
		case first == a && r.body != "":
			return viaFunction()
		case first == a && r.path == "":
			return false, "not on PATH"
		case first == a:
			return false, fmt.Sprintf("runs directly: alias %s='%s', and no wrapper function; run unsent setup", a, v)
		}
		return false, fmt.Sprintf("runs directly: alias %s='%s' skips the wrapper", a, v)
	case "function":
		return viaFunction()
	case "", "none":
		return false, "not on PATH"
	}
	return false, "runs directly: no wrapper function in this shell; run unsent setup"
}

// statusShell picks the shell status reports on: the one named, else
// $SHELL's; and the binary to ask.
func statusShell(args []string, stderr io.Writer) (shell, bin string, code int) {
	switch {
	case len(args) > 1, len(args) == 1 && !slices.Contains(setupShells, args[0]):
		fmt.Fprintln(stderr, "unsent: usage: unsent status [zsh|bash]")
		return "", "", 2
	case len(args) == 1:
		shell = args[0]
	default:
		shell = filepath.Base(os.Getenv("SHELL"))
		if !slices.Contains(setupShells, shell) {
			fmt.Fprintf(stderr, "unsent: your shell is %q; name one: unsent status zsh, or unsent status bash\n", os.Getenv("SHELL"))
			return "", "", 2
		}
	}
	if s := os.Getenv("SHELL"); filepath.Base(s) == shell {
		return shell, s, 0
	}
	bin, err := exec.LookPath(shell)
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %s is not on PATH\n", shell)
		return "", "", 1
	}
	return shell, bin, 0
}

// blockState says whether an rc file holds the block, and whether it is
// the one this build writes.
func blockState(shell, rc string) string {
	text, err := readRC(rc)
	if err != nil {
		return fmt.Sprintf("%s: %v", tilde(rc), err)
	}
	spans, err := findBlocks(text)
	switch {
	case err != nil:
		return fmt.Sprintf("%s: %v; fix it by hand", tilde(rc), err)
	case len(spans) == 0:
		return fmt.Sprintf("none in %s; run unsent setup %s", tilde(rc), shell)
	}
	got := strings.TrimSuffix(text[spans[0].start:spans[0].end], "\n")
	if len(spans) > 1 || got != strings.TrimSuffix(setupBlock(shell), "\n") {
		return fmt.Sprintf("in %s, older than this unsent; run unsent setup %s again", tilde(rc), shell)
	}
	return "in " + tilde(rc) + ", up to date"
}

func cmdStatus(args []string, stdout, stderr io.Writer) int {
	shell, bin, code := statusShell(args, stderr)
	if code != 0 {
		return code
	}
	rc, err := rcFile(shell)
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	agents := agentCommands()
	var b bytes.Buffer
	fmt.Fprintf(&b, "shell: %s (%s)\n", shell, tilde(bin))
	fmt.Fprintf(&b, "setup block: %s\n", blockState(shell, rc))
	p, perr := runProbe(bin, shell, agents)
	for _, a := range agents {
		if perr != nil {
			fmt.Fprintf(&b, "%s: unknown, could not ask the shell: %v\n", a, perr)
		} else {
			_, why := route(a, p.agents[a], p.unsent != "")
			fmt.Fprintf(&b, "%s: %s\n", a, why)
		}
		if pr := profileFor(a); pr != nil {
			fmt.Fprintf(&b, "%s profile: %s, reader last verified on version %s\n", a, pr.name, pr.verified)
		}
	}
	warnings := bypasses(startupFiles(shell), agents)
	if shell == "bash" {
		if w := loginWarning(); w != "" {
			warnings = append(warnings, w)
		}
	}
	fmt.Fprintf(&b, "command line: %s\n", commandLine(shell, p, perr))
	for _, w := range warnings {
		fmt.Fprintf(&b, "bypass: %s\n", w)
	}
	if len(warnings) == 0 {
		fmt.Fprintln(&b, "bypass: none found in the startup files")
	}
	if off() {
		fmt.Fprintf(&b, "saving: off, UNSENT_OFF=%s runs every agent directly\n", os.Getenv("UNSENT_OFF"))
	} else {
		fmt.Fprintln(&b, "saving: on")
	}
	var seen []string
	for _, a := range agents {
		name := agentName(a)
		if slices.Contains(seen, name) {
			continue
		}
		seen = append(seen, name)
		v, from := onSendFrom(name)
		fmt.Fprintf(&b, "on send, %s: %s (%s)\n", name, v, from)
	}
	if shell == "zsh" {
		v, from := onSendFrom(shell)
		fmt.Fprintf(&b, "on send, %s: %s (%s)\n", shell, v, from)
	}
	stdout.Write(b.Bytes())
	return 0
}

// commandLine says whether the shell saves its command line: in zsh, when
// the hooks loaded in the shell status asked.
func commandLine(shell string, p probe, perr error) string {
	switch {
	case shell != "zsh":
		return "not saved; unsent saves only zsh's so far"
	case perr != nil:
		return fmt.Sprintf("unknown, could not ask the shell: %v", perr)
	case !p.hooks:
		return "not saved, the hooks do not load in this shell; run unsent setup zsh"
	}
	return "saved as you type; a line left unrun shows in unsent list"
}
