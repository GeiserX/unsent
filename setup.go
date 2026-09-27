package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// unsent setup writes one marked block into the shell's rc file. At each
// shell start the block wraps every agent this build has a profile for, and
// finds on PATH, in a shell function that runs it through unsent. The list
// is baked in, so an upgrade that adds a profile needs setup again, but the
// PATH check runs in the shell, so an agent installed later is covered from
// the next shell. Nothing starts a process when the shell starts.
//
// The block defines each wrapper with `function name { }`, never
// `name() { }`: under an existing `alias claude=...` the second form fails
// to parse, and the rest of the rc file does not run. It leaves a function
// the user already has under the agent's name alone.
//
// setup --undo removes exactly the block. When setup was the only change,
// the rc file is byte for byte what it was.

const (
	rcBlockStart = "# >>> unsent >>>"
	rcBlockEnd   = "# <<< unsent <<<"
)

// setupShells are the shells setup writes a block for.
var setupShells = []string{"zsh", "bash"}

// agentCommands are the command names the block wraps: every name of every
// profile, in order.
func agentCommands() []string {
	var out []string
	for _, p := range profiles {
		for _, n := range p.names {
			if !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	return out
}

// rcFile is the file setup writes for a shell.
func rcFile(shell string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if shell == "zsh" {
		if d := os.Getenv("ZDOTDIR"); d != "" {
			home = d
		}
		return filepath.Join(home, ".zshrc"), nil
	}
	return filepath.Join(home, ".bashrc"), nil
}

// startupFiles are the files a shell reads when it starts, where setup looks
// for aliases and launchers that skip the wrappers.
func startupFiles(shell string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	names := []string{".bashrc", ".bash_profile", ".bash_login", ".profile", ".bash_aliases"}
	if shell == "zsh" {
		if d := os.Getenv("ZDOTDIR"); d != "" {
			home = d
		}
		names = []string{".zshenv", ".zprofile", ".zshrc", ".zlogin"}
	}
	var out []string
	for _, n := range names {
		out = append(out, filepath.Join(home, n))
	}
	return out
}

// setupBlock is the block for a shell, ending in a line break. Each wrapper
// is defined only when the agent is a program on PATH and no function of
// that name exists yet, and it falls back to the agent itself when unsent
// is gone.
func setupBlock(shell string) string {
	found := `type -P "$_unsent_a" >/dev/null && ! declare -F "$_unsent_a" >/dev/null`
	if shell == "zsh" {
		found = `whence -p "$_unsent_a" >/dev/null && ! typeset -f "$_unsent_a" >/dev/null`
	}
	return rcBlockStart + " written by `unsent setup`; `unsent setup --undo` removes it\n" +
		"for _unsent_a in " + strings.Join(agentCommands(), " ") + "; do\n" +
		"  if " + found + "; then\n" +
		`    eval "function $_unsent_a { if command -v unsent >/dev/null 2>&1; then unsent $_unsent_a \"\$@\"; else command $_unsent_a \"\$@\"; fi; }"` + "\n" +
		"  fi\n" +
		"done\n" +
		"unset _unsent_a\n" +
		rcBlockEnd + "\n"
}

// A span is one block's place in an rc file: from the start of its first
// line to the end of its last, the line break after it included.
type span struct{ start, end int }

// findBlocks finds the blocks in an rc file. A start line without its end
// line, or the other way round, is an error: setup never cuts text it
// cannot delimit.
func findBlocks(text string) ([]span, error) {
	var out []span
	open := -1
	line := 1
	for i := 0; i < len(text); {
		j := strings.IndexByte(text[i:], '\n')
		next := len(text)
		if j >= 0 {
			next = i + j + 1
		}
		l := strings.TrimRight(text[i:next], "\r\n")
		switch {
		case strings.HasPrefix(l, rcBlockStart):
			if open >= 0 {
				return nil, fmt.Errorf("line %d starts an unsent block inside another one", line)
			}
			open = i
		case strings.TrimRight(l, " \t") == rcBlockEnd:
			if open < 0 {
				return nil, fmt.Errorf("line %d ends an unsent block that never started", line)
			}
			out = append(out, span{open, next})
			open = -1
		}
		i = next
		line++
	}
	if open >= 0 {
		return nil, fmt.Errorf("the unsent block that starts at byte %d has no %q line", open, rcBlockEnd)
	}
	return out, nil
}

// withBlock returns text with block in it: in place of the first block
// already there, else at the end, so it runs after the lines that set PATH.
// A file whose last line has no line break gets one before the block, and
// the block then ends without one, so withoutBlocks can take out exactly
// what was added.
func withBlock(text, block string) (string, error) {
	spans, err := findBlocks(text)
	if err != nil {
		return "", err
	}
	if len(spans) == 0 {
		if text != "" && !strings.HasSuffix(text, "\n") {
			return text + "\n" + strings.TrimSuffix(block, "\n"), nil
		}
		return text + block, nil
	}
	first := spans[0]
	if !strings.HasSuffix(text[:first.end], "\n") {
		block = strings.TrimSuffix(block, "\n")
	}
	rest, err := withoutBlocks(text[first.end:])
	if err != nil {
		return "", err
	}
	return text[:first.start] + block + rest, nil
}

// withoutBlocks returns text with every block taken out, and nothing else.
// A block that ends the file with no line break after it also takes the
// line break before it, which setup added.
func withoutBlocks(text string) (string, error) {
	spans, err := findBlocks(text)
	if err != nil {
		return "", err
	}
	for _, s := range slices.Backward(spans) {
		start := s.start
		if s.end == len(text) && !strings.HasSuffix(text, "\n") && start > 0 {
			start--
		}
		text = text[:start] + text[s.end:]
	}
	return text, nil
}

// writeRC replaces an rc file's contents, through a symlink if it is one
// (a dotfiles folder), keeping its mode.
func writeRC(path, text string) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := writeFileDurable(path, []byte(text)); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func readRC(path string) (string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

func cmdSetup(args []string, stdout, stderr io.Writer) int {
	undo := false
	shell := ""
	for _, a := range args {
		switch {
		case a == "--undo":
			undo = true
		case slices.Contains(setupShells, a) && shell == "":
			shell = a
		case a == "fish":
			fmt.Fprintln(stderr, "unsent: fish is not supported yet; zsh and bash are")
			return 2
		default:
			fmt.Fprintln(stderr, "unsent: usage: unsent setup [zsh|bash] [--undo]")
			return 2
		}
	}
	if undo {
		return setupUndo(shell, stdout, stderr)
	}
	if shell == "" {
		shell = filepath.Base(os.Getenv("SHELL"))
		if !slices.Contains(setupShells, shell) {
			fmt.Fprintf(stderr, "unsent: your shell is %q; name one: unsent setup zsh, or unsent setup bash\n", os.Getenv("SHELL"))
			return 2
		}
	}
	rc, err := rcFile(shell)
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	old, err := readRC(rc)
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	text, err := withBlock(old, setupBlock(shell))
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %s: %v; fix it by hand, then run setup again\n", tilde(rc), err)
		return 1
	}
	switch {
	case text == old:
		fmt.Fprintf(stdout, "The unsent block in %s is up to date.\n", tilde(rc))
	default:
		if err := writeRC(rc, text); err != nil {
			fmt.Fprintf(stderr, "unsent: %v\n", err)
			return 1
		}
		if strings.Contains(old, rcBlockStart) {
			fmt.Fprintf(stdout, "Rewrote the unsent block in %s.\n", tilde(rc))
		} else {
			fmt.Fprintf(stdout, "Wrote the unsent block to %s.\n", tilde(rc))
		}
	}
	for _, a := range agentCommands() {
		if p, err := exec.LookPath(a); err == nil {
			fmt.Fprintf(stdout, "From the next shell, %s runs through unsent (%s).\n", a, tilde(p))
		} else {
			fmt.Fprintf(stdout, "%s is not on PATH now; the block wraps it from the first shell that finds it.\n", a)
		}
	}
	warnings := bypasses(startupFiles(shell), agentCommands())
	if shell == "bash" {
		if w := loginWarning(); w != "" {
			warnings = append(warnings, w)
		}
	}
	if len(warnings) > 0 {
		fmt.Fprintln(stdout, "\nWarning: these skip the wrapper, and setup leaves them as they are:")
		for _, w := range warnings {
			fmt.Fprintln(stdout, "  "+w)
		}
		fmt.Fprintln(stdout, "To protect a launcher, make it call `unsent <agent>`, for example `env ... unsent claude \"$@\"`.")
	}
	fmt.Fprintf(stdout, "\n`command %s` runs the agent without unsent once, UNSENT_OFF=1 for longer.\n", agentCommands()[0])
	fmt.Fprintln(stdout, "`unsent setup --undo` removes the block.")
	return 0
}

// setupUndo removes the block from one shell's rc file, or from every
// shell's when none is named. Drafts are the user's: it says where they
// are and never deletes them.
func setupUndo(shell string, stdout, stderr io.Writer) int {
	shells := setupShells
	if shell != "" {
		shells = []string{shell}
	}
	var none []string
	code := 0
	for _, sh := range shells {
		rc, err := rcFile(sh)
		if err != nil {
			fmt.Fprintf(stderr, "unsent: %v\n", err)
			return 1
		}
		old, err := readRC(rc)
		if err != nil {
			fmt.Fprintf(stderr, "unsent: %v\n", err)
			code = 1
			continue
		}
		text, err := withoutBlocks(old)
		if err != nil {
			fmt.Fprintf(stderr, "unsent: %s: %v; nothing removed, fix it by hand\n", tilde(rc), err)
			code = 1
			continue
		}
		if text == old {
			none = append(none, tilde(rc))
			continue
		}
		if err := writeRC(rc, text); err != nil {
			fmt.Fprintf(stderr, "unsent: %v\n", err)
			code = 1
			continue
		}
		fmt.Fprintf(stdout, "Removed the unsent block from %s. New shells start the agents directly; in this one, `unset -f %s` does it now.\n",
			tilde(rc), strings.Join(agentCommands(), " "))
	}
	if len(none) > 0 {
		fmt.Fprintf(stdout, "No unsent block in %s.\n", strings.Join(none, " or "))
	}
	if dir, err := stateDir(); err == nil {
		fmt.Fprintf(stdout, "Your drafts stay in %s; unsent never deletes them.\n", tilde(dir))
	}
	return code
}

// tilde writes a path under the home folder with ~.
func tilde(p string) string {
	return shortPath(p, len(p)+1)
}

// bypasses are the lines in a shell's startup files that start an agent
// without the wrapper: an alias of the agent's name that points at a path,
// and anything that starts it through env, command, exec or a full path,
// because those skip shell functions. Also a function of the agent's own
// name, which the block leaves in place. Each is file:line and why. It
// reads the files as text and runs nothing; a file they source is not read.
func bypasses(files, agents []string) []string {
	var out []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		inBlock := false
		lines := strings.Split(string(b), "\n")
		for i := 0; i < len(lines); i++ {
			n := i + 1
			l := strings.TrimRight(lines[i], "\r")
			// A line that ends in a backslash goes on in the next one.
			for strings.HasSuffix(l, "\\") && i+1 < len(lines) {
				i++
				l = l[:len(l)-1] + " " + strings.TrimRight(lines[i], "\r")
			}
			switch {
			case strings.HasPrefix(l, rcBlockStart):
				inBlock = true
			case strings.TrimRight(l, " \t") == rcBlockEnd:
				inBlock = false
			case !inBlock:
				if why := lineBypass(l, agents); why != "" {
					out = append(out, fmt.Sprintf("%s:%d: %s: %s", tilde(f), n, why, preview(l, 60)))
				}
			}
		}
	}
	return out
}

// funcHeader matches a function definition's first line and its name.
var funcHeader = regexp.MustCompile(`^\s*(?:function\s+([^\s(){}]+)|([^\s(){}=]+)\s*\(\s*\))`)

// lineBypass says why one line starts an agent without the wrapper, or "".
func lineBypass(line string, agents []string) string {
	if m := funcHeader.FindStringSubmatch(line); m != nil {
		name := m[1] + m[2]
		if slices.Contains(agents, name) {
			return fmt.Sprintf("defines its own %s function, which the block leaves alone, so it goes through unsent only if it calls `unsent %s`", name, name)
		}
	}
	for _, words := range shellCommands(line) {
		if why := commandBypass(words, agents); why != "" {
			return why
		}
	}
	return ""
}

// commandBypass says why one simple command starts an agent without the
// wrapper, or "".
func commandBypass(words []string, agents []string) string {
	for len(words) > 0 && (isAssignment(words[0]) || slices.Contains(shellKeywords, words[0])) {
		words = words[1:]
	}
	if len(words) == 0 {
		return ""
	}
	isAgent := func(w string) bool { return slices.Contains(agents, filepath.Base(w)) }
	switch filepath.Base(words[0]) {
	case "function", "for", "case", "select":
		return ""
	case "alias":
		for _, w := range words[1:] {
			name, value, ok := strings.Cut(w, "=")
			if !ok {
				continue
			}
			cmds := shellCommands(value)
			if slices.Contains(agents, name) && len(cmds) > 0 && strings.Contains(firstCommand(cmds[0]), "/") {
				return fmt.Sprintf("alias %s points at a path", name)
			}
			for _, c := range cmds {
				if why := commandBypass(c, agents); why != "" {
					return "alias " + name + " " + why
				}
			}
		}
		return ""
	case "env":
		// Skip env's variables, flags and expansions such as "${vars[@]}"
		// to the command it runs.
		rest := words[1:]
		for len(rest) > 0 {
			r := rest[0]
			if !isAssignment(r) && !strings.HasPrefix(r, "-") && (!strings.HasPrefix(r, "$") || isAgent(r)) {
				break
			}
			if r == "-u" || r == "-C" {
				rest = rest[1:]
			}
			if len(rest) > 0 {
				rest = rest[1:]
			}
		}
		if len(rest) > 0 && isAgent(rest[0]) {
			return "starts " + filepath.Base(rest[0]) + " through env"
		}
	case "command", "exec":
		rest := words[1:]
		for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
			if words[0] == "command" && strings.ContainsAny(rest[0], "vV") {
				return "" // command -v only asks where it is
			}
			if rest[0] == "-a" {
				rest = rest[1:]
			}
			if len(rest) > 0 {
				rest = rest[1:]
			}
		}
		if len(rest) > 0 && isAgent(rest[0]) {
			return "starts " + filepath.Base(rest[0]) + " through " + words[0]
		}
	}
	if strings.Contains(words[0], "/") && isAgent(words[0]) {
		return "starts " + filepath.Base(words[0]) + " by its full path"
	}
	return ""
}

// shellKeywords come before a command without being it.
var shellKeywords = []string{"if", "then", "else", "elif", "fi", "do", "done", "while", "until", "!", "time", "esac"}

var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?\+?=`)

func isAssignment(w string) bool { return assignment.MatchString(w) }

// firstCommand is a simple command's command word, past its assignments.
func firstCommand(words []string) string {
	for _, w := range words {
		if !isAssignment(w) {
			return w
		}
	}
	return ""
}

// shellCommands splits one line of shell into its simple commands, each a
// list of words with the quotes taken off. It is a rough reader, good for
// warnings only: it does not expand anything, and "$(...)" and "${...}"
// stay inside their word as written.
func shellCommands(line string) [][]string {
	var cmds [][]string
	var cur []string
	var w strings.Builder
	inWord := false
	endWord := func() {
		if inWord {
			cur = append(cur, w.String())
			w.Reset()
			inWord = false
		}
	}
	endCommand := func() {
		endWord()
		if len(cur) > 0 {
			cmds = append(cmds, cur)
			cur = nil
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && i+1 < len(line):
			i++
			w.WriteByte(line[i])
			inWord = true
		case c == '\'':
			j := strings.IndexByte(line[i+1:], '\'')
			if j < 0 {
				j = len(line) - i - 1
			}
			w.WriteString(line[i+1 : i+1+j])
			i += j + 1
			inWord = true
		case c == '"':
			j := i + 1
			for ; j < len(line) && line[j] != '"'; j++ {
				if line[j] == '\\' && j+1 < len(line) {
					j++
				}
				w.WriteByte(line[j])
			}
			i = j
			inWord = true
		case c == '$' && i+1 < len(line) && (line[i+1] == '{' || line[i+1] == '('):
			open, shut := line[i+1], byte('}')
			if open == '(' {
				shut = ')'
			}
			depth := 0
			j := i + 1
			for ; j < len(line); j++ {
				if line[j] == open {
					depth++
				} else if line[j] == shut {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			w.WriteString(line[i:min(j+1, len(line))])
			i = j
			inWord = true
		case c == ' ' || c == '\t':
			endWord()
		case c == '#' && !inWord:
			endCommand()
			return cmds
		case strings.IndexByte(";&|()", c) >= 0:
			endCommand()
		case (c == '{' || c == '}') && !inWord && (i+1 == len(line) || strings.IndexByte(" \t;", line[i+1]) >= 0):
			endCommand()
		default:
			w.WriteByte(c)
			inWord = true
		}
	}
	endCommand()
	return cmds
}

// loginWarning says when bash login shells, which macOS terminals start,
// never read ~/.bashrc, so the block would not load in them.
func loginWarning() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, n := range []string{".bash_profile", ".bash_login", ".profile"} {
		p := filepath.Join(home, n)
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), "bashrc") {
			return ""
		}
		return fmt.Sprintf("%s: bash login shells read this file, and it never sources ~/.bashrc; add `[ -f ~/.bashrc ] && . ~/.bashrc` to it", tilde(p))
	}
	return "bash login shells read ~/.bash_profile, which does not exist, so they never read ~/.bashrc; create it with `[ -f ~/.bashrc ] && . ~/.bashrc`"
}
