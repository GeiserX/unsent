// Command unsent is AutoRecover for AI agent prompts: it keeps the text you
// are typing into Claude Code saved on disk, so a closed window, a crash or
// a reboot never takes an unsent message with it.
package main

import (
	"encoding/json"
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
  unsent list [--all] [--here] [--agent <agent>] [--json]
                             list drafts left behind (--all adds cleared ones
                             and earlier versions; --here keeps this folder's,
                             --agent one agent's; --json prints a JSON array,
                             its shape in the README)
  unsent show [--agent <agent>] [--json] [N]
                             print draft N (default: this folder's newest)
  unsent restore [--agent <agent>] [N]
                             copy draft N to the clipboard and mark it restored
  unsent log [--here] [--agent <agent>] [--json]
                             list sessions with sent messages, newest first
                             (--json prints a JSON array, its shape in the
                             README)
  unsent log <session> [--copy N | --json]
                             print a session's sent messages (session: a number
                             from unsent log, its id, or the agent's own session
                             id); --copy N copies message N to the clipboard
  unsent context [--config-dir DIR]... [--session ID | --transcript PATH] [--json]
                             give the messages of each Claude Code conversation's
                             sent log their context from Claude Code's transcript:
                             what the agent had just said, and the answers and
                             messages typed while it worked, which the screen
                             never showed as sent
  unsent context add <session> <n|uuid> --gloss TEXT [--reply]
                             set a one-line note on message n of a sent log
                             (TEXT - reads stdin); unsent log shows it;
                             --reply also marks the message a reply to what
                             the agent asked
  unsent import claude [--config-dir DIR]... [--history] [--json]
                             the same over every Claude Code transcript, making
                             the sent logs that are missing (the last 365 days);
                             --history adds conversations whose transcript is
                             gone from history.jsonl
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
  unsent hook claude         print Claude Code hooks for your settings.json:
                             reopening a conversation that left a draft then
                             tells Claude about it, to ask you before it goes
                             on, and the end of each turn runs unsent context
                             for that conversation; unsent never edits the file
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
list; the same goes for import (ImageMagick ships one) and context.
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
	case "context":
		return cmdContext(args[1:], os.Stdin, stdout, stderr)
	case "import":
		return cmdImport(args[1:], stdout, stderr)
	case "capture":
		return cmdCapture(args[1:], stderr)
	case "setup":
		return cmdSetup(args[1:], stdout, stderr)
	case "status":
		return cmdStatus(args[1:], stdout, stderr)
	case "init":
		return cmdInit(args[1:], stdout, stderr)
	case "hook":
		return cmdHook(args[1:], os.Stdin, stdout, stderr)
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
// behind, then (with all) drafts that were cleared or replaced, and the
// earlier versions of sessions that ended. A version is listed even while
// its session's draft is still an orphan: that draft may be the one that
// lost the text the version holds.
func candidates(st *store, all bool) []*record {
	out := st.orphans()
	if all {
		for _, r := range st.load(true) {
			switch {
			case contains(out, r):
			case r.Version:
				if !st.alive(&record{ID: r.session}) {
					out = append(out, r)
				}
			case !st.alive(r) && !isDraftFile(st, r):
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
	json      bool   // list, show and log --json
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
		case "--json":
			o.json = true
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

// keepsSent is keeps for a sent log.
func (o options) keepsSent(l *sentLog, cwd string) bool {
	return (o.agent == "" || l.Agent == o.agent) && (!o.here || samePath(l.Cwd, cwd))
}

func cmdList(args []string, stdout, stderr io.Writer) int {
	o, err := parseOptions(args, "--all", "--here", "--agent", "--json")
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
	if o.json {
		items := []draftJSON{}
		for _, i := range shown {
			items = append(items, jsonOf(st, rs[i], i+1))
		}
		return printJSON(stdout, stderr, items)
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
	allowed := []string{"--agent"}
	if !restore {
		allowed = append(allowed, "--json")
	}
	o, err := parseOptions(args, allowed...)
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
	if o.json {
		pastes := r.Pastes
		if pastes == nil {
			pastes = []string{}
		}
		return printJSON(stdout, stderr, showJSON{jsonOf(st, r, n), r.Draft, pastes})
	}
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

// draftFormat is the version of the shape list --json and show --json
// print, documented in the README. Fields are only added; a rename or a
// retype bumps it, the rule the records follow.
const draftFormat = 1

// draftJSON is one draft as list --json prints it. Times are RFC 3339 with
// the offset they were saved with, "" when unknown.
type draftJSON struct {
	Format       int    `json:"format"`
	N            int    `json:"n"` // the number show and restore take
	ID           string `json:"id"`
	Kind         string `json:"kind"` // orphan, history, version or shell
	Agent        string `json:"agent"`
	AgentSession string `json:"agent_session"`
	Folder       string `json:"folder"`
	Started      string `json:"started"`
	Updated      string `json:"updated"`
	Ended        string `json:"ended"`
	Lines        int    `json:"lines"`
	Bytes        int    `json:"bytes"`
	FirstLine    string `json:"first_line"`
}

// showJSON is one draft as show --json prints it: the list's fields, the
// draft with its pastes expanded, and the raw paste texts kept with it.
type showJSON struct {
	draftJSON
	Text   string   `json:"text"`
	Pastes []string `json:"pastes"`
}

// firstLineMax caps first_line, in characters.
const firstLineMax = 100

func jsonOf(st *store, r *record, n int) draftJSON {
	kind := "history"
	switch {
	case isShell(r.agent()):
		kind = "shell"
	case r.Version:
		kind = "version"
	case isDraftFile(st, r):
		kind = "orphan"
	}
	return draftJSON{
		Format: draftFormat, N: n, ID: r.ID, Kind: kind,
		Agent: r.agent(), AgentSession: r.AgentSession, Folder: r.Cwd,
		Started: rfc3339(r.Started), Updated: rfc3339(r.Updated), Ended: rfc3339(r.Ended),
		Lines: strings.Count(r.Draft, "\n") + 1, Bytes: len(r.Draft), FirstLine: firstLine(r.Draft),
	}
}

// firstLine is a draft's first line with text, trimmed and cut to
// firstLineMax characters: list --json's first_line, and what the Claude
// Code hook's note quotes.
func firstLine(draft string) string {
	for l := range strings.SplitSeq(draft, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if c := []rune(l); len(c) > firstLineMax {
				return string(c[:firstLineMax])
			}
			return l
		}
	}
	return ""
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// printJSON writes v indented, with <, > and & left as they are.
func printJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
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
	o, err := parseOptions(args, "--here", "--agent", "--copy", "--json")
	switch {
	case err != nil:
	case len(o.rest) > 1:
		err = fmt.Errorf("unexpected argument %q", o.rest[1])
	case o.copy > 0 && len(o.rest) == 0:
		err = fmt.Errorf("--copy needs a session: unsent log <session> --copy N")
	case o.copy > 0 && o.json:
		err = fmt.Errorf("--copy and --json do not go together")
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
			if o.keepsSent(l, cwd) {
				shown = append(shown, i)
				width = max(width, len(l.Agent))
			}
		}
		if o.json {
			items := []sentLogJSON{}
			for _, i := range shown {
				items = append(items, sentJSONOf(logs[i], i+1))
			}
			return printJSON(stdout, stderr, items)
		}
		if len(shown) == 0 {
			fmt.Fprintln(stdout, "No sent messages.")
			return 0
		}
		for _, i := range shown {
			// The row previews the last message's first raw line, as it did
			// before log --json; first_line is the JSON's own field.
			l, j := logs[i], sentJSONOf(logs[i], i+1)
			last := ""
			if n := len(l.messages); n > 0 {
				last, _, _ = strings.Cut(l.messages[n-1].Text, "\n")
			}
			fmt.Fprintf(stdout, "%3d  %s  %-*s  %s  %-24s  %4d sent  %s\n", j.N, j.ID, width, j.Agent, when(l.Started), shortPath(l.Cwd, 24), j.Messages, preview(last, 40))
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
	if o.json {
		msgs := []any{}
		l.walk(func(n int, m sentMessage) {
			pastes := m.Pastes
			if pastes == nil {
				pastes = []string{}
			}
			msgs = append(msgs, sentMessageJSON{
				N: n, Sent: rfc3339(m.Time), Text: m.Text, Pastes: pastes,
				UUID: m.UUID, Kind: m.Kind, Asked: m.Asked, ReplyTo: m.ReplyTo, Gloss: m.Gloss, Source: m.Source,
			})
		}, func(r sentResume) {
			msgs = append(msgs, sentResumeJSON{Resumed: rfc3339(r.Resumed)})
		})
		return printJSON(stdout, stderr, sentShowJSON{sentJSONOf(l, slices.Index(logs, l)+1), msgs})
	}
	fmt.Fprintf(stdout, "%s in %s, started %s, session %s", l.Agent, shortPath(l.Cwd, 60), when(l.Started), l.Session)
	if l.AgentSession != "" {
		fmt.Fprintf(stdout, ", %s session %s", l.Agent, l.AgentSession)
	}
	fmt.Fprintln(stdout)
	if l.Dropped > 0 {
		fmt.Fprintf(stdout, "(%d earlier messages were dropped to keep the log under %d MB)\n", l.Dropped, sentMaxBytes>>20)
	}
	l.walk(func(n int, m sentMessage) {
		fmt.Fprintf(stdout, "\n%d  %s\n%s\n", n, when(m.Time), m.Text)
		if m.ReplyTo && m.Asked != "" {
			fmt.Fprintf(stdout, "  ↳ answering: %s\n", answeringLine(m.Asked))
		}
		if m.Gloss != "" {
			fmt.Fprintf(stdout, "  ↳ why: %s\n", m.Gloss)
		}
		for _, p := range m.Pastes {
			fmt.Fprintf(stdout, "--- a paste that could not be placed in the message ---\n%s\n", p)
		}
	}, func(r sentResume) {
		fmt.Fprintf(stdout, "\n(resumed %s, session %s)\n", when(r.Resumed), r.Session)
	})
	return 0
}

// sentJSONFormat is the version of the shape log --json and log <session>
// --json print, documented in the README; the rule of draftFormat holds.
const sentJSONFormat = 1

// sentLogJSON is one session as log --json prints it.
type sentLogJSON struct {
	Format       int    `json:"format"`
	N            int    `json:"n"`  // the number log <session> takes
	ID           string `json:"id"` // unsent's id of the run that started the log
	Agent        string `json:"agent"`
	AgentSession string `json:"agent_session"`
	Folder       string `json:"folder"`
	Started      string `json:"started"`
	Updated      string `json:"updated"`    // the last send
	Messages     int    `json:"messages"`   // how many
	FirstLine    string `json:"first_line"` // of the last message
	Dropped      int    `json:"dropped"`    // oldest messages trimmed away
}

// sentShowJSON is one session as log <session> --json prints it: the
// list's fields, with messages holding the messages themselves, in order,
// and a sentResumeJSON where the conversation was reopened.
type sentShowJSON struct {
	sentLogJSON
	Messages []any `json:"messages"`
}

// sentMessageJSON is one sent message: n is the number --copy takes, text
// has its pastes expanded, and pastes holds the ones it could not place.
// The rest is the context unsent context adds, each field left out when
// empty (sentMessage says what they hold).
type sentMessageJSON struct {
	N       int      `json:"n"`
	Sent    string   `json:"sent"`
	Text    string   `json:"text"`
	Pastes  []string `json:"pastes"`
	UUID    string   `json:"uuid,omitempty"`
	Kind    string   `json:"kind,omitempty"`
	Asked   string   `json:"asked,omitempty"`
	ReplyTo bool     `json:"reply_to,omitempty"`
	Gloss   string   `json:"gloss,omitempty"`
	Source  string   `json:"source,omitempty"`
}

// answeringMax caps the agent's line unsent log quotes above a reply, in
// characters.
const answeringMax = 200

// answeringLine is the line unsent log quotes above a reply: the last line
// of asked with text outside a code block, trimmed and cut to answeringMax
// characters. Fences are walked the way asksSomething walks them, so a
// question above a trailing block is quoted, never the block's last line.
func answeringLine(asked string) string {
	lines := strings.Split(asked, "\n")
	fenced := false
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		switch {
		case strings.HasPrefix(l, "```"):
			fenced = !fenced
		case fenced:
		case l != "":
			return cutRunes(l, answeringMax)
		}
	}
	return ""
}

// sentResumeJSON marks where a later run reopened the conversation.
type sentResumeJSON struct {
	Resumed string `json:"resumed"`
}

func sentJSONOf(l *sentLog, n int) sentLogJSON {
	j := sentLogJSON{
		Format: sentJSONFormat, N: n, ID: l.Session, Agent: l.Agent, AgentSession: l.AgentSession,
		Folder: l.Cwd, Started: rfc3339(l.Started), Messages: len(l.messages), Dropped: l.Dropped,
	}
	if k := len(l.messages); k > 0 {
		j.Updated, j.FirstLine = rfc3339(l.messages[k-1].Time), firstLine(l.messages[k-1].Text)
	}
	return j
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
	// Under the log's lock: a context pass between its read and its write
	// would put the whole log back.
	unlock, err := lockSent(l.path)
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	err = os.Remove(l.path)
	unlock()
	if err != nil {
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
