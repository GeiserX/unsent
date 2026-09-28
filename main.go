// Command unsent is AutoRecover for AI agent prompts: it keeps the text you
// are typing into Claude Code saved on disk, so a closed window, a crash or
// a reboot never takes an unsent message with it.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"
)

var version = "dev"

const usage = `unsent: AutoRecover for AI agent prompts.

Usage:
  unsent <agent> [args...]   run the agent with its input box saved as you type
  unsent --as <agent> <command> [args...]
                             the same, for a command whose name does not say
                             which agent it starts (npx, node cli.js, a renamed
                             binary); UNSENT_AGENT=<agent> does the same
  unsent list [--all] [--here] [--agent <agent>]
                             list drafts left behind (--all adds cleared ones
                             and earlier versions; --here keeps this folder's,
                             --agent one agent's)
  unsent show [--agent <agent>] [N]
                             print draft N (default: this folder's newest)
  unsent restore [--agent <agent>] [N]
                             copy draft N to the clipboard and mark it restored
  unsent log [--here] [--agent <agent>]
                             list sessions with sent messages, newest first
  unsent log <session> [--copy N]
                             print a session's sent messages (session: a number
                             from unsent log, its id, or the agent's own session
                             id); --copy N copies message N to the clipboard
  unsent forget --log <id>
                             delete one session's sent log (id: the session id
                             unsent log shows, never a number, which can move)
  unsent setup [zsh|bash]    wrap every agent unsent can read in a shell
                             function that runs it through unsent, with one
                             marked block in your shell's rc file
  unsent setup --undo [zsh|bash]
                             remove that block (from every shell's rc file
                             when none is named); drafts stay
  unsent init zsh            print the hooks that save zsh's command line as
                             you type it, for an rc file you keep by hand:
                             eval "$(unsent init zsh)"; unsent setup zsh
                             already writes them
  unsent status [zsh|bash]   which agents this shell runs through unsent,
                             which profile each gets, what skips the wrapper,
                             whether the command line is saved, and the
                             saving and on-send settings
  unsent capture <agent> [args...]
                             run the agent with every key and all its output
                             recorded into testdata/<agent>/<version>/ here, as
                             a test fixture; $EDITOR only copies the file the
                             agent hands it
  unsent version

A message you send goes to its session's sent log, not to history. A Claude
Code conversation keeps one log across every run that resumes it.
UNSENT_ON_SEND=delete keeps nothing of it; UNSENT_ON_SEND_CLAUDE=delete does
that for one agent, and wins over UNSENT_ON_SEND. A shell line you run keeps
nothing, since the shell's own history has it; UNSENT_ON_SEND_ZSH=log logs
it. A shell line you clear goes to history, and the last line of a closed
shell waits in unsent list.

Reopen a Claude Code conversation that left a draft (--resume, -c, the resume
picker, /resume) and the draft goes back into its empty box, not sent.
UNSENT_NOTICE=0 turns off the draft notices and the line under the box; the
draft still goes back.

UNSENT_OFF=1 runs the agent directly, as if unsent were not there.
UNSENT_DEBUG_DIR=<folder> logs every key, all output and each save's view
there, for debugging; the files hold everything typed.

Run "unsent setup" once to never think about it again; "command claude"
skips the wrapper for one run. Use "unsent -- list" to run a program called
list.
`

func main() {
	exitAs(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "version", "--version":
		fmt.Fprintln(stdout, "unsent", buildVersion())
		return 0
	case "list", "ls":
		return cmdList(args[1:], stdout, stderr)
	case "show":
		return cmdShow(args[1:], stdout, stderr, false)
	case "restore":
		return cmdShow(args[1:], stdout, stderr, true)
	case "log":
		return cmdLog(args[1:], stdout, stderr)
	case "forget":
		return cmdForget(args[1:], stdout, stderr)
	case "capture":
		return cmdCapture(args[1:], stderr)
	case "setup":
		return cmdSetup(args[1:], stdout, stderr)
	case "status":
		return cmdStatus(args[1:], stdout, stderr)
	case "init":
		return cmdInit(args[1:], stdout, stderr)
	case "--as":
		if len(args) < 3 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		as, cmd := args[1], args[2:]
		if cmd[0] == "--" {
			cmd = cmd[1:]
		}
		if as == "" || len(cmd) == 0 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		return wrap(agentFor(as, cmd[0]), cmd, os.Stdin, os.Stdout, nil)
	case "--":
		args = args[1:]
		if len(args) == 0 {
			fmt.Fprint(stderr, usage)
			return 2
		}
	}
	return wrap(agentFor("", args[0]), args, os.Stdin, os.Stdout, nil)
}

// buildVersion is the release version stamped in by the release build, or
// the module version when installed with go install.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

// candidates are the drafts the commands work on, newest first: drafts left
// behind, then (with all) drafts that were cleared or replaced.
func candidates(st *store, all bool) []*record {
	out := st.orphans()
	if all {
		for _, r := range st.load(true) {
			if !contains(out, r) && !st.alive(r) && !isDraftFile(st, r) {
				out = append(out, r)
			}
		}
	}
	return out
}

func isDraftFile(st *store, r *record) bool {
	_, err := os.Stat(st.draftPath(r.ID))
	return err == nil
}

func contains(rs []*record, r *record) bool {
	for _, x := range rs {
		if x.ID == r.ID {
			return true
		}
	}
	return false
}

// options are the flags list, show, restore and log take.
type options struct {
	all, here bool
	agent     string // "" for every agent
	copy      int    // log --copy N; 0 when not given
	rest      []string
}

// parseOptions reads the flags in allowed out of args; the rest are
// arguments.
func parseOptions(args []string, allowed ...string) (options, error) {
	var o options
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			o.rest = append(o.rest, a)
			continue
		}
		if a == "-a" {
			a = "--all"
		}
		if !slices.Contains(allowed, a) {
			return o, fmt.Errorf("unknown option %q", args[i])
		}
		switch a {
		case "--all":
			o.all = true
		case "--here":
			o.here = true
		case "--agent":
			if i+1 >= len(args) || args[i+1] == "" {
				return o, fmt.Errorf("--agent needs an agent name")
			}
			i++
			o.agent = agentName(args[i])
		case "--copy":
			n := 0
			if i+1 < len(args) {
				n, _ = strconv.Atoi(args[i+1])
			}
			if n < 1 {
				return o, fmt.Errorf("--copy needs a message number")
			}
			i++
			o.copy = n
		}
	}
	return o, nil
}

// keeps reports whether a record passes the agent and --here filters.
func (o options) keeps(r *record, cwd string) bool {
	return (o.agent == "" || r.agent() == o.agent) && (!o.here || samePath(r.Cwd, cwd))
}

func cmdList(args []string, stdout, stderr io.Writer) int {
	o, err := parseOptions(args, "--all", "--here", "--agent")
	if err == nil && len(o.rest) > 0 {
		err = fmt.Errorf("unexpected argument %q", o.rest[0])
	}
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 2
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	cwd, _ := os.Getwd()
	// Filtered rows keep their numbers, the ones show and restore take.
	var shown []int
	width := 0
	rs := candidates(st, o.all)
	for i, r := range rs {
		if o.keeps(r, cwd) {
			shown = append(shown, i)
			width = max(width, len(r.agent()))
		}
	}
	if len(shown) == 0 {
		fmt.Fprintln(stdout, "No drafts to recover.")
		return 0
	}
	for _, i := range shown {
		r := rs[i]
		mark := ""
		if r.Version {
			mark = "(earlier version) "
		}
		fmt.Fprintf(stdout, "%3d  %-*s  %s  %-24s  %s%s\n", i+1, width, r.agent(), when(r.Updated), shortPath(r.Cwd, 24), mark, preview(r.Draft, 60-len(mark)))
	}
	return 0
}

func cmdShow(args []string, stdout, stderr io.Writer, restore bool) int {
	o, err := parseOptions(args, "--agent")
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 2
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	rs := candidates(st, true)
	if len(rs) == 0 {
		fmt.Fprintln(stderr, "unsent: no drafts to recover")
		return 1
	}
	n := 0
	if len(o.rest) > 0 {
		// A number reaches any draft, whatever its agent or folder.
		n, err = strconv.Atoi(o.rest[0])
		if err != nil || n < 1 || n > len(rs) {
			fmt.Fprintf(stderr, "unsent: no draft %q; see unsent list --all\n", o.rest[0])
			return 2
		}
	} else {
		// The recovery notice promised this folder's draft: prefer it, from
		// the agent asked for.
		cwd, _ := os.Getwd()
		for i, r := range rs {
			if !o.keeps(r, cwd) {
				continue
			}
			if n == 0 {
				n = i + 1
			}
			if isDraftFile(st, r) && samePath(r.Cwd, cwd) {
				n = i + 1
				break
			}
		}
		if n == 0 {
			fmt.Fprintf(stderr, "unsent: no drafts from %s to recover\n", o.agent)
			return 1
		}
	}
	r := rs[n-1]
	if !restore {
		fmt.Fprintln(stdout, r.Draft)
		for _, p := range r.Pastes {
			if !strings.Contains(r.Draft, p) {
				fmt.Fprintf(stdout, "\n--- a paste that could not be placed in the draft ---\n%s\n", p)
			}
		}
		return 0
	}
	if err := copyToClipboard(r.Draft); err != nil {
		// No clipboard (SSH, a bare Linux console): print it instead.
		fmt.Fprintln(stdout, r.Draft)
		fmt.Fprintf(stderr, "unsent: no clipboard (%v); printed the draft instead\n", err)
		return 0
	}
	// Leave the draft in place unless its copy in history is safely written.
	if isDraftFile(st, r) && st.archive(r) == nil {
		st.remove(r)
	}
	fmt.Fprintf(stderr, "Copied %s from %s to the clipboard. Paste it into the agent.\n",
		lines(r.Draft), when(r.Updated))
	if n := unplaced(r); n > 0 {
		// Restoring moved the draft to history, so its number changed.
		fmt.Fprintf(stderr, "%d paste(s) could not be put back in place: find the draft in `unsent list --all`, and `unsent show <number>` prints them.\n", n)
	}
	return 0
}

// unplaced counts a record's pastes that are not in its draft.
func unplaced(r *record) int {
	n := 0
	for _, p := range r.Pastes {
		if !strings.Contains(r.Draft, p) {
			n++
		}
	}
	return n
}

// cmdLog lists the sessions with a sent log, or prints one session's
// messages, or copies one of them.
func cmdLog(args []string, stdout, stderr io.Writer) int {
	o, err := parseOptions(args, "--here", "--agent", "--copy")
	switch {
	case err != nil:
	case len(o.rest) > 1:
		err = fmt.Errorf("unexpected argument %q", o.rest[1])
	case o.copy > 0 && len(o.rest) == 0:
		err = fmt.Errorf("--copy needs a session: unsent log <session> --copy N")
	}
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 2
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	st.pruneSent()
	logs := st.sentLogs()
	if len(o.rest) == 0 {
		cwd, _ := os.Getwd()
		// Filtered rows keep their numbers, the ones log <session> takes.
		var shown []int
		width := 0
		for i, l := range logs {
			if (o.agent == "" || l.Agent == o.agent) && (!o.here || samePath(l.Cwd, cwd)) {
				shown = append(shown, i)
				width = max(width, len(l.Agent))
			}
		}
		if len(shown) == 0 {
			fmt.Fprintln(stdout, "No sent messages.")
			return 0
		}
		for _, i := range shown {
			l := logs[i]
			last := ""
			if n := len(l.messages); n > 0 {
				last, _, _ = strings.Cut(l.messages[n-1].Text, "\n")
			}
			fmt.Fprintf(stdout, "%3d  %s  %-*s  %s  %-24s  %4d sent  %s\n", i+1, l.Session, width, l.Agent, when(l.Started), shortPath(l.Cwd, 24), len(l.messages), preview(last, 40))
		}
		return 0
	}
	l := oneSent(logs, o.rest[0], stderr)
	if l == nil {
		return 2
	}
	if o.copy > 0 {
		if o.copy > len(l.messages) {
			fmt.Fprintf(stderr, "unsent: that session has %d messages, not %d\n", len(l.messages), o.copy)
			return 2
		}
		text := l.messages[o.copy-1].Text
		if err := copyToClipboard(text); err != nil {
			fmt.Fprintln(stdout, text)
			fmt.Fprintf(stderr, "unsent: no clipboard (%v); printed the message instead\n", err)
			return 0
		}
		fmt.Fprintf(stderr, "Copied message %d, %s, to the clipboard.\n", o.copy, lines(text))
		return 0
	}
	fmt.Fprintf(stdout, "%s in %s, started %s, session %s", l.Agent, shortPath(l.Cwd, 60), when(l.Started), l.Session)
	if l.AgentSession != "" {
		fmt.Fprintf(stdout, ", %s session %s", l.Agent, l.AgentSession)
	}
	fmt.Fprintln(stdout)
	if l.Dropped > 0 {
		fmt.Fprintf(stdout, "(%d earlier messages were dropped to keep the log under %d MB)\n", l.Dropped, sentMaxBytes>>20)
	}
	resumes := l.resumes
	resumed := func(before int) {
		for len(resumes) > 0 && resumes[0].after <= before {
			fmt.Fprintf(stdout, "\n(resumed %s, session %s)\n", when(resumes[0].Resumed), resumes[0].Session)
			resumes = resumes[1:]
		}
	}
	for i, m := range l.messages {
		resumed(i)
		fmt.Fprintf(stdout, "\n%d  %s\n%s\n", i+1, when(m.Time), m.Text)
		for _, p := range m.Pastes {
			fmt.Fprintf(stdout, "--- a paste that could not be placed in the message ---\n%s\n", p)
		}
	}
	resumed(len(l.messages))
	return 0
}

// findSent picks sent logs by their number in unsent log, unsent's session
// id, or the agent's own session id. One run of unsent can start the logs
// of several conversations (the agent's /resume or /clear), so an id of
// unsent's can name more than one.
func findSent(logs []*sentLog, arg string) []*sentLog {
	if n, err := strconv.Atoi(arg); err == nil {
		if n >= 1 && n <= len(logs) {
			return logs[n-1 : n]
		}
		return nil
	}
	var out []*sentLog
	for _, l := range logs {
		if l.Session == arg || l.AgentSession == arg {
			out = append(out, l)
		}
	}
	return out
}

// oneSent is the one sent log arg names, or nil, having said why on stderr.
func oneSent(logs []*sentLog, arg string, stderr io.Writer) *sentLog {
	found := findSent(logs, arg)
	switch len(found) {
	case 0:
		fmt.Fprintf(stderr, "unsent: no sent log %q; see unsent log\n", arg)
		return nil
	case 1:
		return found[0]
	}
	fmt.Fprintf(stderr, "unsent: %q started %d conversations' sent logs; name one by the agent's session id:\n", arg, len(found))
	for _, l := range found {
		fmt.Fprintf(stderr, "  %s  %s, %d sent\n", l.AgentSession, when(l.Started), len(l.messages))
	}
	return nil
}

// cmdForget deletes one session's sent log, named by its session id. A
// number from unsent log is refused: a session that sends its first message
// in between shifts the numbers, and the wrong log would be gone for good.
func cmdForget(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 || args[0] != "--log" {
		fmt.Fprintln(stderr, "unsent: usage: unsent forget --log <id>")
		return 2
	}
	if _, err := strconv.Atoi(args[1]); err == nil {
		fmt.Fprintf(stderr, "unsent: forget --log takes the session id that unsent log shows, not a number: the numbers move when another session sends its first message\n")
		return 2
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	st.pruneSent()
	l := oneSent(st.sentLogs(), args[1], stderr)
	if l == nil {
		return 2
	}
	if err := os.Remove(l.path); err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	fmt.Fprintf(stderr, "Deleted the sent log of the %s session started %s (%d messages).\n", l.Agent, when(l.Started), len(l.messages))
	return 0
}

// orphanNotice is the line that tells the user a draft this agent left in
// this folder is waiting, the way a word processor offers recovered files,
// or "" when there is none. A draft is only offered back to the agent it
// came from, and one left in a subfolder is counted, not offered. A shell
// line typed in this folder and never run, by a shell that is gone, gets a
// line of its own.
func orphanNotice(st *store, cwd, agent string) string {
	var here, left []*record // this agent's drafts here; shell lines left here
	var first *record        // this folder's newest, of any agent: what a plain restore picks
	sub := 0
	for _, r := range st.orphans() {
		if samePath(r.Cwd, cwd) && first == nil {
			first = r
		}
		if isShell(r.agent()) && r.agent() != agent && samePath(r.Cwd, cwd) {
			left = append(left, r)
		}
		switch {
		case r.agent() != agent:
		case samePath(r.Cwd, cwd):
			here = append(here, r)
		case below(r.Cwd, cwd):
			sub++
		}
	}
	subs := ""
	switch {
	case sub == 1:
		subs = "1 draft waits in a subfolder"
	case sub > 1:
		subs = fmt.Sprintf("%d drafts wait in subfolders", sub)
	}
	shell := ""
	switch {
	case len(left) == 1:
		shell = fmt.Sprintf("unsent: a %s line you typed here and did not run is saved: `unsent restore --agent %s` copies it.\n", left[0].agent(), left[0].agent())
	case len(left) > 1:
		shell = fmt.Sprintf("unsent: %d shell lines you typed here and did not run are saved: `unsent list --here`.\n", len(left))
	}
	if len(here) == 0 {
		if subs != "" {
			return fmt.Sprintf("unsent: %s of this folder: `unsent list`.\n", subs) + shell
		}
		return shell
	}
	r := here[0]
	more := ""
	if len(here) > 1 {
		more = fmt.Sprintf(" (and %d more)", len(here)-1)
	}
	restore := "unsent restore"
	if first != r {
		// Another agent's draft here is newer, and a plain restore takes it.
		restore += " --agent " + agent
	}
	if subs != "" {
		subs = " " + subs + ": `unsent list`."
	}
	return fmt.Sprintf("unsent: recovered a draft from %s, %s%s. Run `%s` to copy it.%s\n",
		when(r.Updated), lines(r.Draft), more, restore, subs) + shell
}

// realPath is a folder's resolved real path, so /tmp and /private/tmp on
// macOS are one folder. A path that cannot be resolved stays as it is.
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// samePath compares two folders by their real paths.
func samePath(a, b string) bool {
	return realPath(a) == realPath(b)
}

// below reports whether folder a is inside folder b, at any depth.
func below(a, b string) bool {
	rel, err := filepath.Rel(realPath(b), realPath(a))
	return err == nil && rel != "." && filepath.IsLocal(rel)
}

func copyToClipboard(text string) error {
	var tries [][]string
	switch runtime.GOOS {
	case "darwin":
		tries = [][]string{{"pbcopy"}}
	case "windows":
		tries = [][]string{{"clip.exe"}}
	default:
		tries = [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}, {"clip.exe"}}
	}
	for _, t := range tries {
		if _, err := exec.LookPath(t[0]); err != nil {
			continue
		}
		cmd := exec.Command(t[0], t[1:]...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	return fmt.Errorf("none of pbcopy, wl-copy, xclip, xsel, clip.exe found")
}

func when(t time.Time) string {
	now := time.Now()
	switch {
	case now.Sub(t) < 24*time.Hour && now.Day() == t.Day():
		return t.Format("15:04") + " today"
	case now.Sub(t) < 48*time.Hour && now.AddDate(0, 0, -1).Day() == t.Day():
		return t.Format("15:04") + " yesterday"
	default:
		return t.Format("2006-01-02 15:04")
	}
}

func lines(s string) string {
	n := strings.Count(s, "\n") + 1
	if n == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", n)
}

func preview(s string, width int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > width {
		return string(r[:width-1]) + "…"
	}
	return s
}

func shortPath(p string, width int) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			p = filepath.Join("~", rel)
		}
	}
	if r := []rune(p); len(r) > width {
		return "…" + string(r[len(r)-width+1:])
	}
	return p
}
