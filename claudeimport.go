package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// `unsent context` gives the messages of a Claude Code conversation's sent
// log their context, read from Claude Code's own transcript: the record of
// each human turn (its uuid, how it was sent) and the agent's text just
// before it. It only reads, in the config folders it is given or one
// transcript named on the command line, and only writes into unsent's own
// sent logs. `unsent import claude` runs the same pass over every
// transcript and makes the logs that are missing, and with --history makes
// logs from history.jsonl for conversations whose transcript is gone.
// `unsent context add` sets the one-line gloss a skill writes later.

// turnReader reads one transcript's human turns (readClaudeTranscript); a
// test passes fake turns.
type turnReader func(io.Reader) ([]claudeTurn, error)

// claudeTurns is the reader the commands use.
var claudeTurns turnReader = readClaudeTranscript

// contextResult is what one pass did, the summary line and its --json.
type contextResult struct {
	Logs    int `json:"logs"`    // sent logs written
	Added   int `json:"added"`   // messages added from the transcript or history
	Matched int `json:"matched"` // messages of the log given their turn
	// Skipped counts turns and history entries older than sentMaxAge or
	// without a uuid, and transcripts that could not be read.
	Skipped int `json:"skipped"`
}

func (r *contextResult) add(o contextResult) {
	r.Logs += o.Logs
	r.Added += o.Added
	r.Matched += o.Matched
	r.Skipped += o.Skipped
}

// sentMessageFields are the JSON names of sentMessage's fields: a rewritten
// line keeps every other field, a later build's, as it was.
var sentMessageFields = map[string]bool{
	"time": true, "text": true, "pastes": true, "uuid": true, "kind": true,
	"asked": true, "reply_to": true, "gloss": true, "source": true,
}

// encodeSentMessage is m as a log line, with the fields of raw, the line it
// replaces, that this build does not know.
func encodeSentMessage(m sentMessage, raw []byte) []byte {
	b, _ := json.Marshal(m)
	var have map[string]json.RawMessage
	if raw == nil || json.Unmarshal(raw, &have) != nil {
		return b
	}
	var extra []string
	for k := range have {
		if !sentMessageFields[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		key, _ := json.Marshal(k)
		b = append(append(append(append(b[:len(b)-1], ','), key...), ':'), have[k]...)
		b = append(b, '}')
	}
	return b
}

// sentLine is one line after a sent log's header, as a rewrite keeps it.
type sentLine struct {
	raw   []byte
	kind  sentLineKind
	msg   sentMessage
	at    time.Time // where it sorts: a message's time, a resume line's, the line before's
	dirty bool      // msg changed: encode it again
}

// splitSentLog cuts a sent log into its header line and the lines after.
func splitSentLog(data []byte) ([]byte, []*sentLine, error) {
	head, body, _ := bytes.Cut(data, []byte("\n"))
	var h sentHeader
	if json.Unmarshal(head, &h) != nil || h.Format == 0 {
		return nil, nil, errors.New("not a sent log")
	}
	var out []*sentLine
	var at time.Time
	for raw := range bytes.SplitSeq(body, []byte("\n")) {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		l := &sentLine{raw: raw, kind: sentKind(raw)}
		switch l.kind {
		case sentLineMessage:
			json.Unmarshal(raw, &l.msg)
			at = l.msg.Time
		case sentLineResume:
			var r sentResume
			json.Unmarshal(raw, &r)
			at = r.Resumed
		}
		l.at = at
		out = append(out, l)
	}
	return head, out, nil
}

// joinSentLog is the log's bytes: the header, then the lines in order.
func joinSentLog(head []byte, lines []*sentLine) []byte {
	out := append(append([]byte{}, head...), '\n')
	for _, l := range lines {
		if l.dirty {
			l.raw = encodeSentMessage(l.msg, l.raw)
		}
		out = append(append(out, l.raw...), '\n')
	}
	return out
}

// mergeHook runs between a merge's read and its write; a test appends
// there, as a send would, to check the lock keeps it.
var mergeHook func(path string)

// contextMatchWindow is how far apart in time a logged message and a turn
// with the same text may be and still be one message. A turn is recorded
// when the agent takes it, a queued or absorbed one up to about a minute
// after it was sent; a conversation resumed over months repeats short
// texts ("yes", a slash command) days apart, and those must not pair up.
const contextMatchWindow = time.Hour

// mergeSent merges messages from a transcript or history into the sent log
// at path, under its lock. A message with a uuid the log already holds is
// skipped. The rest are paired with the messages of the log that have no
// uuid and the same text (sameSent) within contextMatchWindow, the
// nearest pairs first across the whole log, so an old turn never takes the
// live message of a later one; a paired message gets the turn's uuid, kind,
// asked and reply_to, and the turns left over are added. Messages from
// history carry no uuid and are only written into a log this makes. When
// the log is missing, head is its header, or with head nil nothing is
// done. The lines end sorted by time and the file's time is its last
// message's, so a backfill does not reset retention. A pass that changes
// nothing writes nothing.
func mergeSent(path string, head *sentHeader, msgs []sentMessage) (contextResult, error) {
	var res contextResult
	unlock, err := lockSent(path)
	if err != nil {
		return res, err
	}
	defer unlock()
	data, err := os.ReadFile(path)
	created := errors.Is(err, os.ErrNotExist)
	switch {
	case created && head == nil:
		return res, nil
	case created:
		data, _ = json.Marshal(head)
	case err != nil:
		return res, err
	}
	hl, lines, err := splitSentLog(data)
	if err != nil {
		return res, fmt.Errorf("%s: %w", path, err)
	}
	if mergeHook != nil {
		mergeHook(path)
	}
	have := map[string]bool{}
	for _, l := range lines {
		if l.kind == sentLineMessage && l.msg.UUID != "" {
			have[l.msg.UUID] = true
		}
	}
	var turns, plain []sentMessage
	for _, m := range msgs {
		switch {
		case m.UUID == "":
			if created {
				plain = append(plain, m)
			}
		case !have[m.UUID]:
			have[m.UUID] = true
			turns = append(turns, m)
		}
	}
	paired := pairTurns(lines, turns)
	for i, m := range turns {
		if l := paired[i]; l != nil {
			l.msg.UUID, l.msg.Kind, l.msg.Asked, l.msg.ReplyTo = m.UUID, m.Kind, m.Asked, m.ReplyTo
			l.dirty = true
			res.Matched++
			continue
		}
		plain = append(plain, m)
	}
	for _, m := range plain {
		lines = append(lines, &sentLine{kind: sentLineMessage, msg: m, at: m.Time, dirty: true})
		res.Added++
	}
	if res.Added == 0 && res.Matched == 0 {
		return res, nil
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].at.Before(lines[j].at) })
	if err := writeFileDurable(path, joinSentLog(hl, lines)); err != nil {
		return res, err
	}
	res.Logs = 1
	if fi, err := os.Stat(path); err == nil && fi.Size() > sentMaxBytes {
		if err := trimSent(path); err != nil {
			return res, err
		}
	}
	var last time.Time
	for _, l := range lines {
		if l.kind == sentLineMessage && l.msg.Time.After(last) {
			last = l.msg.Time
		}
	}
	if !last.IsZero() {
		os.Chtimes(path, last, last)
	}
	return res, nil
}

// pairTurns pairs turns with the messages of lines that have no uuid: a
// pair is a message and a turn of the same text (sameSent) at most
// contextMatchWindow apart, and pairs are taken nearest in time first, each
// message and each turn once. paired[i] is turn i's message, nil for none.
func pairTurns(lines []*sentLine, turns []sentMessage) []*sentLine {
	paired := make([]*sentLine, len(turns))
	type pair struct {
		turn, line int
		gap        time.Duration
	}
	var pairs []pair
	byText := map[string][]int{} // messages without a paste placeholder
	var withPastes []int
	for j, l := range lines {
		if l.kind != sentLineMessage || l.msg.UUID != "" {
			continue
		}
		if len(l.msg.Pastes) > 0 && pastedTextRE.MatchString(l.msg.Text) {
			withPastes = append(withPastes, j)
			continue
		}
		k := normaliseSent(l.msg.Text)
		byText[k] = append(byText[k], j)
	}
	for i, m := range turns {
		want := normaliseSent(m.Text)
		try := func(j int) {
			if d := lines[j].msg.Time.Sub(m.Time).Abs(); d <= contextMatchWindow {
				pairs = append(pairs, pair{i, j, d})
			}
		}
		for _, j := range byText[want] {
			try(j)
		}
		for _, j := range withPastes {
			if sameSent(lines[j].msg, want) {
				try(j)
			}
		}
	}
	sort.SliceStable(pairs, func(a, b int) bool { return pairs[a].gap < pairs[b].gap })
	used := map[int]bool{}
	for _, p := range pairs {
		if paired[p.turn] == nil && !used[p.line] {
			paired[p.turn] = lines[p.line]
			used[p.line] = true
		}
	}
	return paired
}

// sameSent reports whether the logged message m and a turn whose
// normalised text is want are one message. m's text can still show a
// paste as a placeholder, with the paste in m.Pastes, while the transcript
// holds it in place: then each placeholder, in order, is filled with a
// paste of m.Pastes, each once, and the result compared. The numbers on
// the placeholders do not say which paste is which, so every assignment
// is tried, up to sameSentTries.
func sameSent(m sentMessage, want string) bool {
	if normaliseSent(m.Text) == want {
		return true
	}
	holes := pastedTextRE.FindAllStringIndex(m.Text, -1)
	if len(holes) == 0 || len(holes) > len(m.Pastes) {
		return false
	}
	used := make([]bool, len(m.Pastes))
	tries := 0
	var fill func(k int, b *strings.Builder) bool
	fill = func(k int, b *strings.Builder) bool {
		from := 0
		if k > 0 {
			from = holes[k-1][1]
		}
		if k == len(holes) {
			tries++
			return normaliseSent(b.String()+m.Text[from:]) == want
		}
		for i, p := range m.Pastes {
			if used[i] || tries >= sameSentTries {
				continue
			}
			var next strings.Builder
			next.WriteString(b.String())
			next.WriteString(m.Text[from:holes[k][0]])
			next.WriteString(p)
			used[i] = true
			ok := fill(k+1, &next)
			used[i] = false
			if ok {
				return true
			}
		}
		return false
	}
	return fill(0, &strings.Builder{})
}

// sameSentTries caps the paste assignments sameSent tries for one message.
const sameSentTries = 120

// turnMessage is a turn as a sent log message.
func turnMessage(t claudeTurn) sentMessage {
	return sentMessage{
		Time: t.Time, Text: t.Text, UUID: t.UUID, Kind: t.Kind,
		Asked: t.Asked, ReplyTo: t.ReplyTo, Source: "transcript",
	}
}

// claudeLogPath is the sent log of Claude Code conversation id, the one a
// live run writes (sentPathOf).
func (s *store) claudeLogPath(id string) string {
	return s.sentPath("claude-" + id)
}

// transcriptFile is one transcript and the conversation its name says.
type transcriptFile struct {
	path, session string
}

// claudeTranscripts lists the transcripts in the projects folders of dirs,
// <dir>/projects/<slug>/<session>.jsonl and never deeper (the folders
// beside them hold subagents and tool results), regular files only, each
// once by its real path, by session.
func claudeTranscripts(dirs []string) map[string][]transcriptFile {
	out := map[string][]transcriptFile{}
	seen := map[string]bool{}
	for _, d := range dirs {
		names, _ := filepath.Glob(filepath.Join(d, "projects", "*", "*.jsonl"))
		sort.Strings(names)
		for _, n := range names {
			id := strings.TrimSuffix(filepath.Base(n), ".jsonl")
			fi, err := os.Lstat(n)
			if err != nil || !fi.Mode().IsRegular() || !sessionIDRE.MatchString(id) {
				continue
			}
			if r := realPath(n); !seen[r] {
				seen[r] = true
				out[id] = append(out[id], transcriptFile{n, id})
			}
		}
	}
	return out
}

// groupTurns reads files and sorts their turns by conversation: the turn's
// own session id, else the one the file's name says. A turn is taken once
// by uuid, the first read; one with no uuid, or older than cutoff, is
// skipped and counted.
func groupTurns(files []transcriptFile, read turnReader, cutoff time.Time) (map[string][]claudeTurn, []string, int) {
	groups := map[string][]claudeTurn{}
	var order []string
	seen := map[string]bool{}
	skipped := 0
	for _, f := range files {
		fh, err := os.Open(f.path)
		if err != nil {
			skipped++
			continue
		}
		turns, err := read(bufio.NewReaderSize(fh, 1<<20))
		fh.Close()
		if err != nil && len(turns) == 0 {
			skipped++
			continue
		}
		for _, t := range turns {
			switch {
			case t.UUID == "" || t.Time.Before(cutoff):
				skipped++
				continue
			case seen[t.UUID]:
				continue
			}
			seen[t.UUID] = true
			id := t.SessionID
			if !sessionIDRE.MatchString(id) {
				id = f.session
			}
			if _, ok := groups[id]; !ok {
				order = append(order, id)
			}
			groups[id] = append(groups[id], t)
		}
	}
	return groups, order, skipped
}

// mergeTurns merges each conversation's turns into its sent log; with
// create, a conversation with no log gets one.
func (s *store) mergeTurns(groups map[string][]claudeTurn, order []string, create bool) (contextResult, error) {
	var res contextResult
	for _, id := range order {
		turns := groups[id]
		var head *sentHeader
		if create {
			first := turns[0]
			for _, t := range turns {
				if t.Time.Before(first.Time) {
					first = t
				}
			}
			cwd := ""
			for _, t := range turns {
				if cwd = t.Cwd; cwd != "" {
					break
				}
			}
			head = &sentHeader{Format: sentFormat, Session: "claude-" + id, Agent: "claude", AgentSession: id, Cwd: cwd, Started: first.Time}
		}
		msgs := make([]sentMessage, len(turns))
		for i, t := range turns {
			msgs[i] = turnMessage(t)
		}
		r, err := mergeSent(s.claudeLogPath(id), head, msgs)
		if err != nil {
			return res, err
		}
		res.add(r)
	}
	return res, nil
}

// claudeHistoryDirs are the config folders whose history.jsonl is read:
// each its own, even when several share one projects folder, so they are
// told apart by their own real path, not their projects folder's.
func claudeHistoryDirs(extra []string) []string {
	cands := append([]string{}, extra...)
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		cands = append(cands, d)
	}
	if home, err := os.UserHomeDir(); err == nil {
		cands = append(cands, filepath.Join(home, ".claude"))
	}
	var out []string
	seen := map[string]bool{}
	for _, d := range cands {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() || seen[realPath(d)] {
			continue
		}
		seen[realPath(d)] = true
		out = append(out, d)
	}
	return out
}

// claudeHistoryEntry is one line of Claude Code's history.jsonl.
type claudeHistoryEntry struct {
	Display        string `json:"display"`
	PastedContents map[string]struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	} `json:"pastedContents"`
	Project   string `json:"project"`
	SessionID string `json:"sessionId"`
	Timestamp int64  `json:"timestamp"`
}

// pastedTextRE is the placeholder Claude Code shows for a paste, in the box
// and in history.jsonl.
var pastedTextRE = regexp.MustCompile(`\[Pasted text #(\d+)(?: \+\d+ lines?)?\]`)

// text is the entry's display with each placeholder whose paste the entry
// holds put back.
func (e claudeHistoryEntry) text() string {
	return pastedTextRE.ReplaceAllStringFunc(e.Display, func(p string) string {
		id := pastedTextRE.FindStringSubmatch(p)[1]
		if c, ok := e.PastedContents[id]; ok && c.Type == "text" && c.Content != "" {
			return c.Content
		}
		return p
	})
}

// historyLogs makes a sent log for each conversation that history.jsonl
// names and that has neither a transcript nor a log already: kind typed,
// source history, no asked. An entry is read once across config folders.
func (s *store) historyLogs(dirs []string, known map[string]bool, cutoff time.Time) (contextResult, error) {
	var res contextResult
	type key struct {
		session string
		at      int64
		display string
	}
	seen := map[key]bool{}
	groups := map[string][]claudeHistoryEntry{}
	var order []string
	for _, d := range dirs {
		f, err := os.Open(filepath.Join(d, "history.jsonl"))
		if err != nil {
			continue
		}
		br := bufio.NewReaderSize(f, 1<<20)
		for {
			line, err := br.ReadBytes('\n')
			var e claudeHistoryEntry
			if len(bytes.TrimSpace(line)) > 0 && json.Unmarshal(line, &e) == nil &&
				sessionIDRE.MatchString(e.SessionID) && strings.TrimSpace(e.Display) != "" && !known[e.SessionID] {
				k := key{e.SessionID, e.Timestamp, e.Display}
				switch {
				case seen[k]:
				case time.UnixMilli(e.Timestamp).Before(cutoff):
					seen[k] = true
					res.Skipped++
				default:
					seen[k] = true
					if _, ok := groups[e.SessionID]; !ok {
						order = append(order, e.SessionID)
					}
					groups[e.SessionID] = append(groups[e.SessionID], e)
				}
			}
			if err != nil {
				break
			}
		}
		f.Close()
	}
	for _, id := range order {
		es := groups[id]
		sort.SliceStable(es, func(i, j int) bool { return es[i].Timestamp < es[j].Timestamp })
		head := &sentHeader{Format: sentFormat, Session: "claude-" + id, Agent: "claude", AgentSession: id, Cwd: es[0].Project, Started: time.UnixMilli(es[0].Timestamp).UTC()}
		msgs := make([]sentMessage, len(es))
		for i, e := range es {
			msgs[i] = sentMessage{Time: time.UnixMilli(e.Timestamp).UTC(), Text: e.text(), Kind: "typed", Source: "history"}
		}
		r, err := mergeSent(s.claudeLogPath(id), head, msgs)
		if err != nil {
			return res, err
		}
		res.add(r)
	}
	return res, nil
}

// contextOptions are the flags context and import take.
type contextOptions struct {
	configDirs []string
	session    string
	transcript string
	history    bool
	json       bool
	fromHook   bool // the Stop hook started the pass (contextDebounced)
}

func parseContextOptions(args []string, allowed ...string) (contextOptions, error) {
	var o contextOptions
	for i := 0; i < len(args); i++ {
		a := args[i]
		ok := false
		for _, x := range allowed {
			ok = ok || x == a
		}
		if !ok {
			return o, fmt.Errorf("unexpected argument %q", a)
		}
		value := func() (string, error) {
			if i+1 >= len(args) || args[i+1] == "" {
				return "", fmt.Errorf("%s needs a value", a)
			}
			i++
			return args[i], nil
		}
		var err error
		switch a {
		case "--json":
			o.json = true
		case "--history":
			o.history = true
		case "--from-hook":
			o.fromHook = true
		case "--config-dir":
			var d string
			d, err = value()
			o.configDirs = append(o.configDirs, d)
		case "--session":
			o.session, err = value()
		case "--transcript":
			o.transcript, err = value()
		}
		if err != nil {
			return o, err
		}
	}
	if o.session != "" && o.transcript != "" {
		return o, errors.New("--session and --transcript do not go together")
	}
	if o.fromHook && o.transcript == "" {
		return o, errors.New("--from-hook needs --transcript")
	}
	return o, nil
}

// printContextResult prints the summary line, or its JSON.
func printContextResult(o contextOptions, res contextResult, stdout, stderr io.Writer) int {
	if o.json {
		return printJSON(stdout, stderr, res)
	}
	fmt.Fprintf(stdout, "%d logs updated, %d messages added, %d matched\n", res.Logs, res.Added, res.Matched)
	return 0
}

// cmdContext is `unsent context`: the pass over the Claude Code
// conversations that already have a sent log, or `unsent context add`.
func cmdContext(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "add" {
		return cmdContextAdd(args[1:], stdin, stdout, stderr)
	}
	o, err := parseContextOptions(args, "--config-dir", "--session", "--transcript", "--json", "--from-hook")
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 2
	}
	if onSend("claude") == "delete" {
		fmt.Fprintln(stderr, "unsent: on send is delete for claude, nothing reconciled")
		return 2
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	cutoff := time.Now().Add(-sentMaxAge)
	var files []transcriptFile
	only := ""
	switch {
	case o.transcript != "":
		fi, err := os.Stat(o.transcript)
		if err != nil || !fi.Mode().IsRegular() {
			fmt.Fprintf(stderr, "unsent: %s is not a transcript\n", o.transcript)
			return 2
		}
		files = []transcriptFile{{o.transcript, strings.TrimSuffix(filepath.Base(o.transcript), ".jsonl")}}
		if o.fromHook && st.contextDebounced(o.transcript, time.Now()) {
			return printContextResult(o, contextResult{}, stdout, stderr)
		}
	default:
		all := claudeTranscripts(claudeConfigDirs(o.configDirs))
		if o.session != "" {
			only = o.session
			if l := findSent(st.sentLogs(), o.session); len(l) == 1 && l[0].Agent == "claude" && l[0].AgentSession != "" {
				only = l[0].AgentSession
			}
			if !sessionIDRE.MatchString(only) {
				fmt.Fprintf(stderr, "unsent: no Claude Code conversation %q\n", o.session)
				return 2
			}
			files = all[only]
			break
		}
		// Only the transcripts of conversations that have a log.
		for _, l := range st.sentLogs() {
			if l.Agent == "claude" && l.AgentSession != "" && l.path == st.claudeLogPath(l.AgentSession) {
				files = append(files, all[l.AgentSession]...)
			}
		}
	}
	groups, order, skipped := groupTurns(files, claudeTurns, cutoff)
	if only != "" {
		order = []string{only}
		if groups[only] == nil {
			order = nil
		}
	}
	res, err := st.mergeTurns(groups, order, false)
	res.Skipped += skipped
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	if o.transcript != "" {
		st.markContextPass(o.transcript, time.Now())
	}
	return printContextResult(o, res, stdout, stderr)
}

// contextDebounce is how long after a pass over a transcript a pass the
// Stop hook starts on it is skipped: a busy conversation ends a turn every
// few seconds, and each pass reads the whole transcript.
const contextDebounce = 10 * time.Minute

// contextStatePath is the file that records the last pass over transcript:
// sent/.context-<its name without .jsonl>.state.
func (s *store) contextStatePath(transcript string) string {
	return filepath.Join(s.dir, "sent", ".context-"+strings.TrimSuffix(filepath.Base(transcript), ".jsonl")+".state")
}

// contextDebounced reports whether a pass over transcript ran less than
// contextDebounce before now. A state file that does not parse, or a time
// after now (a clock step back), never skips.
func (s *store) contextDebounced(transcript string, now time.Time) bool {
	b, err := os.ReadFile(s.contextStatePath(transcript))
	if err != nil {
		return false
	}
	last, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(b)))
	if err != nil {
		return false
	}
	age := now.Sub(last)
	return age >= 0 && age < contextDebounce
}

// markContextPass records a pass over transcript at now.
func (s *store) markContextPass(transcript string, now time.Time) {
	p := s.contextStatePath(transcript)
	if os.MkdirAll(filepath.Dir(p), 0o700) == nil {
		writeFileDurable(p, []byte(now.UTC().Format(time.RFC3339Nano)+"\n"))
	}
}

// cmdImport is `unsent import claude`: the one-off backfill of every
// Claude Code transcript, and with --history of history.jsonl.
func cmdImport(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "claude" {
		fmt.Fprintln(stderr, "unsent: usage: unsent import claude [--config-dir DIR]... [--history] [--json]")
		return 2
	}
	o, err := parseContextOptions(args[1:], "--config-dir", "--history", "--json")
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 2
	}
	if onSend("claude") == "delete" {
		fmt.Fprintln(stderr, "unsent: on send is delete for claude, nothing imported")
		return 2
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	cutoff := time.Now().Add(-sentMaxAge)
	all := claudeTranscripts(claudeConfigDirs(o.configDirs))
	ids := make([]string, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var files []transcriptFile
	for _, id := range ids {
		files = append(files, all[id]...)
	}
	groups, order, skipped := groupTurns(files, claudeTurns, cutoff)
	res, err := st.mergeTurns(groups, order, true)
	res.Skipped += skipped
	if err == nil && o.history {
		known := map[string]bool{}
		for id := range all {
			known[id] = true
		}
		for id := range groups {
			known[id] = true
		}
		for _, l := range st.sentLogs() {
			if l.Agent == "claude" && l.AgentSession != "" {
				known[l.AgentSession] = true
			}
		}
		var h contextResult
		h, err = st.historyLogs(claudeHistoryDirs(o.configDirs), known, cutoff)
		res.add(h)
	}
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	st.pruneSent()
	return printContextResult(o, res, stdout, stderr)
}

// cmdContextAdd is `unsent context add <session> <n> --gloss TEXT`: it
// sets message n's gloss, one line, TEXT - read from stdin, and keeps the
// log's time.
func cmdContextAdd(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var rest []string
	gloss, set := "", false
	for i := 0; i < len(args); i++ {
		if args[i] == "--gloss" && i+1 < len(args) {
			gloss, set = args[i+1], true
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	n, err := 0, error(nil)
	if len(rest) == 2 {
		n, err = strconv.Atoi(rest[1])
	}
	if !set || len(rest) != 2 || err != nil || n < 1 {
		fmt.Fprintln(stderr, "unsent: usage: unsent context add <session> <n> --gloss TEXT (TEXT - reads stdin)")
		return 2
	}
	if gloss == "-" {
		b, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
		if err != nil {
			fmt.Fprintf(stderr, "unsent: %v\n", err)
			return 1
		}
		gloss = string(b)
	}
	gloss = strings.Join(strings.Fields(gloss), " ")
	st, err := openStore()
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	l := oneSent(st.sentLogs(), rest[0], stderr)
	if l == nil {
		return 2
	}
	switch err := setGloss(l.path, n, gloss); {
	case errors.Is(err, errNoMessage):
		fmt.Fprintf(stderr, "unsent: that session has no message %d\n", n)
		return 2
	case err != nil:
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	return 0
}

var errNoMessage = errors.New("no such message")

// setGloss sets the gloss of message n (from 1, as unsent log numbers
// them) of the sent log at path, under its lock, and keeps the file's
// time.
func setGloss(path string, n int, gloss string) error {
	unlock, err := lockSent(path)
	if err != nil {
		return err
	}
	defer unlock()
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	head, lines, err := splitSentLog(data)
	if err != nil {
		return err
	}
	k := 0
	for _, l := range lines {
		if l.kind != sentLineMessage {
			continue
		}
		if k++; k == n {
			if l.msg.Gloss == gloss {
				return nil
			}
			l.msg.Gloss, l.dirty = gloss, true
			if err := writeFileDurable(path, joinSentLog(head, lines)); err != nil {
				return err
			}
			return os.Chtimes(path, fi.ModTime(), fi.ModTime())
		}
	}
	return errNoMessage
}
