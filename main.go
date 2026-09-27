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
                             from unsent log, or its id); --copy N copies
                             message N to the clipboard
  unsent forget --log <id>
                             delete one session's sent log (id: the session id
                             unsent log shows, never a number, which can move)
  unsent version

A message you send goes to its session's sent log, not to history.
UNSENT_ON_SEND=delete keeps nothing of it; UNSENT_ON_SEND_CLAUDE=delete does
that for one agent, and wins over UNSENT_ON_SEND.

Put "alias claude='unsent claude'" in your shell profile to never think
about it again. Use "unsent -- list" to run a program called list.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
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
		return wrap(agentFor(as, cmd[0]), cmd, os.Stdin, os.Stdout)
	case "--":
		args = args[1:]
		if len(args) == 0 {
			fmt.Fprint(stderr, usage)
			return 2
		}
	}
	return wrap(agentFor("", args[0]), args, os.Stdin, os.Stdout)
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
	l := findSent(logs, o.rest[0])
	if l == nil {
		fmt.Fprintf(stderr, "unsent: no sent log %q; see unsent log\n", o.rest[0])
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
	fmt.Fprintf(stdout, "%s in %s, started %s, session %s\n", l.Agent, shortPath(l.Cwd, 60), when(l.Started), l.Session)
	if l.Dropped > 0 {
		fmt.Fprintf(stdout, "(%d earlier messages were dropped to keep the log under %d MB)\n", l.Dropped, sentMaxBytes>>20)
	}
	for i, m := range l.messages {
		fmt.Fprintf(stdout, "\n%d  %s\n%s\n", i+1, when(m.Time), m.Text)
		for _, p := range m.Pastes {
			fmt.Fprintf(stdout, "--- a paste that could not be placed in the message ---\n%s\n", p)
		}
	}
	return 0
}

// findSent picks a sent log by its number in unsent log, or its session id.
func findSent(logs []*sentLog, arg string) *sentLog {
	if n, err := strconv.Atoi(arg); err == nil {
		if n >= 1 && n <= len(logs) {
			return logs[n-1]
		}
		return nil
	}
	for _, l := range logs {
		if l.Session == arg {
			return l
		}
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
	l := findSent(st.sentLogs(), args[1])
	if l == nil {
		fmt.Fprintf(stderr, "unsent: no sent log %q; see unsent log\n", args[1])
		return 2
	}
	if err := os.Remove(l.path); err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	fmt.Fprintf(stderr, "Deleted the sent log of the %s session started %s (%d messages).\n", l.Agent, when(l.Started), len(l.messages))
	return 0
}

// noticeOrphans tells the user, before the agent starts, that a draft this
// agent left in this folder is waiting, the way a word processor offers
// recovered files. A draft is only offered back to the agent it came from,
// and one left in a subfolder is counted, not offered.
func noticeOrphans(st *store, cwd, agent string) {
	var here []*record
	var first *record // this folder's newest, of any agent: what a plain restore picks
	sub := 0
	for _, r := range st.orphans() {
		if samePath(r.Cwd, cwd) && first == nil {
			first = r
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
	if len(here) == 0 {
		if subs != "" {
			fmt.Fprintf(os.Stderr, "unsent: %s of this folder: `unsent list`.\n", subs)
		}
		return
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
	fmt.Fprintf(os.Stderr, "unsent: recovered a draft from %s, %s%s. Run `%s` to copy it.%s\n",
		when(r.Updated), lines(r.Draft), more, restore, subs)
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
