package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// Claude Code keeps every conversation in
// <config dir>/projects/<slug>/<sessionId>.jsonl, one JSON record per line.
// readClaudeTranscript reads the messages the human wrote from one of
// those files, with what the agent had said just before each. The facts it
// relies on were measured on real files from 2.1.108 to 2.1.287:
//
//   - A human turn is a user record (typed, queued, or a slash command), a
//     queued_command attachment (a prompt typed while the agent worked and
//     absorbed mid-turn), or the tool result of an AskUserQuestion call.
//   - Since 2.1.183 a typed record says so (origin.kind "human", or
//     promptSource "typed" or "queued"); before that nothing does, and a
//     plain record that is not one of the harness's own is a turn. Files
//     mix versions, so each record is judged by its own version field.
//   - Records form a tree through parentUuid, and logicalParentUuid where
//     a compaction cut the chain. File order is not conversation order: a
//     rewind leaves the abandoned branch in the file, and concurrent
//     writers make timestamps run backwards.
//   - One API response is split over several records sharing message.id,
//     one content block each.

// claudeTurn is one message the human sent, as the transcript has it.
type claudeTurn struct {
	UUID      string    // record uuid; for "absorbed" attachments the attachment record's uuid
	Kind      string    // "typed" | "queued" | "absorbed" | "answer" | "slash"
	Time      time.Time // the record's timestamp, UTC
	Text      string
	Asked     string // joined assistant text, its last claudeAskedMax runes, "" if none
	ReplyTo   bool
	Cwd       string
	SessionID string
	Version   string
}

// claudeAskedMax is how many runes of the agent's text a turn keeps: the
// last ones, since a question comes at the end.
const claudeAskedMax = 1000

// claudeTypedSince is the first Claude Code version that marks typed
// records with origin or promptSource.
const claudeTypedSince = "2.1.183"

// claudeNotTyped are the starts of user records the harness writes itself:
// notifications, local command output, hook text, continuation and peer
// messages. None is a turn.
var claudeNotTyped = []string{
	"/compact",
	"<task-notification>",
	"<local-command-",
	"<command-name>",
	"<bash-input>",
	"<bash-stdout>",
	"<bash-stderr>",
	"<system-reminder>",
	"[Request interrupted",
	"This session is being continued",
	"Another Claude session sent a message",
	"Caveat:",
	"<agent-message",
	"<teammate-message",
}

// ctBlock is one content block. Only the small fields are decoded: a tool
// result's content and a tool call's input, which can be megabytes, are
// skipped.
type ctBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	ToolUseID string `json:"tool_use_id"`
	IsError   bool   `json:"is_error"`
}

// ctContent is a message's content: a string, or a list of blocks. A value
// of any other shape reads as empty, never as an error.
type ctContent struct {
	str    string
	blocks []ctBlock
}

func (c *ctContent) UnmarshalJSON(b []byte) error {
	b = bytes.TrimLeft(b, " \t\r\n")
	if len(b) == 0 {
		return nil
	}
	switch b[0] {
	case '"':
		_ = json.Unmarshal(b, &c.str)
	case '[':
		_ = json.Unmarshal(b, &c.blocks)
	}
	return nil
}

// text joins the text blocks; images and tool blocks add nothing.
func (c ctContent) text() string {
	if c.blocks == nil {
		return c.str
	}
	var parts []string
	for _, b := range c.blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (c ctContent) has(kind string) bool {
	for _, b := range c.blocks {
		if b.Type == kind {
			return true
		}
	}
	return false
}

type ctOrigin struct {
	Kind string `json:"kind"`
}

type ctMessage struct {
	ID      string    `json:"id"`
	Content ctContent `json:"content"`
}

// ctAsk is an AskUserQuestion result. Other tools' results, of any shape,
// read as not ok.
type ctAsk struct {
	ok        bool
	Questions []struct {
		Question string `json:"question"`
	} `json:"questions"`
	Answers     map[string]string `json:"answers"`
	Annotations map[string]struct {
		Notes string `json:"notes"`
	} `json:"annotations"`
}

func (a *ctAsk) UnmarshalJSON(b []byte) error {
	b = bytes.TrimLeft(b, " \t\r\n")
	if len(b) == 0 || b[0] != '{' || !bytes.Contains(b, []byte(`"answers"`)) {
		return nil
	}
	type plain ctAsk
	var p plain
	_ = json.Unmarshal(b, &p)
	*a = ctAsk(p)
	a.ok = true
	return nil
}

type ctAttachment struct {
	Type        string    `json:"type"`
	CommandMode string    `json:"commandMode"`
	Prompt      ctContent `json:"prompt"`
	Origin      *ctOrigin `json:"origin"`
	IsMeta      bool      `json:"isMeta"`
}

type ctRecord struct {
	Type              string       `json:"type"`
	UUID              string       `json:"uuid"`
	ParentUUID        string       `json:"parentUuid"`
	LogicalParentUUID string       `json:"logicalParentUuid"`
	IsMeta            bool         `json:"isMeta"`
	IsCompactSummary  bool         `json:"isCompactSummary"`
	IsSidechain       bool         `json:"isSidechain"`
	Origin            *ctOrigin    `json:"origin"`
	PromptSource      string       `json:"promptSource"`
	Entrypoint        string       `json:"entrypoint"`
	Version           string       `json:"version"`
	Timestamp         string       `json:"timestamp"`
	Cwd               string       `json:"cwd"`
	SessionID         string       `json:"sessionId"`
	Message           ctMessage    `json:"message"`
	ToolUseResult     ctAsk        `json:"toolUseResult"`
	Attachment        ctAttachment `json:"attachment"`
}

// up is the record the parent walk goes to next.
func (r *ctRecord) up() string {
	if r.ParentUUID != "" {
		return r.ParentUUID
	}
	return r.LogicalParentUUID
}

// What a record is to the parent walk.
const (
	ctPass      uint8 = iota // attachments, system records, tool results, harness text
	ctHuman                  // a human turn: the walk stops with nothing asked
	ctAssistant              // stops at it when it holds text
)

// ctNode is all that is kept of a record: enough to walk the tree.
type ctNode struct {
	up   string
	role uint8
	msg  string   // the key of the assistant text it belongs to, "" when it holds none
	asks []string // ids of its AskUserQuestion calls
}

// ctCandidate is a turn waiting for the whole file, since a parent can
// come later in file order than its child.
type ctCandidate struct {
	turn     claudeTurn
	start    string // where the walk starts
	fallback string // the last assistant text before it in file order
	// For an answer: the call it answers and the record holding it.
	toolUseID string
	asked     string
}

// readClaudeTranscript streams one transcript. Turns come back in file
// order, deduped by uuid. A torn last line is not an error.
func readClaudeTranscript(r io.Reader) ([]claudeTurn, error) {
	nodes := map[string]ctNode{}
	texts := map[string]string{} // assistant text by message id
	var cands []ctCandidate
	lastText := ""

	br := bufio.NewReaderSize(r, 1<<20)
	var line []byte
	for {
		var err error
		line, err = readLine(br, line[:0])
		if len(bytes.TrimSpace(line)) > 0 {
			var rec ctRecord
			if jerr := json.Unmarshal(line, &rec); jerr == nil || errors.As(jerr, new(*json.UnmarshalTypeError)) {
				if rec.UUID != "" {
					if _, dup := nodes[rec.UUID]; !dup {
						n, c := classify(&rec)
						if n.msg != "" {
							t := rec.Message.Content.text()
							if prev := texts[n.msg]; prev != "" {
								t = prev + "\n" + t
							}
							// Only the tail is ever read (asked, and whether its
							// last paragraph asks something); a margin covers the
							// trailing space trimmed before the cut.
							texts[n.msg] = lastRunes(t, 2*claudeAskedMax)
							lastText = n.msg
						}
						nodes[rec.UUID] = n
						if c != nil {
							c.fallback = lastText
							cands = append(cands, *c)
						}
					}
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	// An answer counts once the call it answers is found.
	kept := cands[:0]
	for _, c := range cands {
		if c.turn.Kind == "answer" {
			p, ok := nodes[c.start]
			if !ok || !slices.Contains(p.asks, c.toolUseID) {
				continue
			}
			n := nodes[c.turn.UUID]
			n.role = ctHuman
			nodes[c.turn.UUID] = n
		}
		kept = append(kept, c)
	}

	turns := make([]claudeTurn, 0, len(kept))
	for _, c := range kept {
		t := c.turn
		full := c.asked
		if t.Kind != "answer" {
			full = strings.TrimSpace(texts[walkAsked(nodes, c.start, c.fallback)])
		}
		t.Asked = lastRunes(full, claudeAskedMax)
		t.ReplyTo = t.Kind == "answer" || asksSomething(full)
		turns = append(turns, t)
	}
	return turns, nil
}

// readLine reads one line of any length into buf, without its newline
// limit: a record can be megabytes long.
func readLine(br *bufio.Reader, buf []byte) ([]byte, error) {
	for {
		chunk, err := br.ReadSlice('\n')
		buf = append(buf, chunk...)
		if err != bufio.ErrBufferFull {
			return buf, err
		}
	}
}

// classify reads one record into its node and, when it is a human turn or
// may be an answer, its candidate.
func classify(r *ctRecord) (ctNode, *ctCandidate) {
	n := ctNode{up: r.up()}
	turn := func(kind, text string) *ctCandidate {
		ts, _ := time.Parse(time.RFC3339Nano, r.Timestamp)
		return &ctCandidate{
			turn: claudeTurn{UUID: r.UUID, Kind: kind, Time: ts.UTC(), Text: text,
				Cwd: r.Cwd, SessionID: r.SessionID, Version: r.Version},
			start: n.up,
		}
	}
	switch r.Type {
	case "assistant":
		n.role = ctAssistant
		for _, b := range r.Message.Content.blocks {
			if b.Type == "tool_use" && b.Name == "AskUserQuestion" && b.ID != "" {
				n.asks = append(n.asks, b.ID)
			}
		}
		if strings.TrimSpace(r.Message.Content.text()) != "" {
			n.msg = "m:" + r.Message.ID
			if r.Message.ID == "" {
				n.msg = "u:" + r.UUID
			}
		}
		return n, nil

	case "attachment":
		a := r.Attachment
		if a.Type != "queued_command" || a.CommandMode != "prompt" || r.IsMeta || a.IsMeta || r.IsSidechain {
			return n, nil
		}
		text := stripPastedContent(a.Prompt.text())
		if a.Origin != nil {
			if a.Origin.Kind != "human" {
				return n, nil
			}
		} else if harnessText(text) {
			return n, nil
		}
		return n, turn("absorbed", text)

	case "user":
		if r.IsSidechain {
			return n, nil
		}
		c := r.Message.Content
		if c.has("tool_result") {
			return n, answer(r, turn)
		}
		if r.IsMeta || r.IsCompactSummary || r.Entrypoint == "sdk-cli" ||
			r.PromptSource == "system" || r.PromptSource == "sdk" ||
			(r.Origin != nil && r.Origin.Kind != "human") {
			return n, nil
		}
		text := stripPastedContent(c.text())
		// A slash command record is a turn on every version, marked or
		// not: 2.1.287 writes a custom command with neither origin nor
		// promptSource.
		if t := strings.TrimLeft(text, " \t\r\n"); strings.HasPrefix(t, "<command-message>") {
			n.role = ctHuman
			return n, turn("slash", slashText(t))
		}
		typed := r.Origin != nil || r.PromptSource == "typed" || r.PromptSource == "queued"
		if !typed && !claudeVersionBefore(r.Version, claudeTypedSince) {
			return n, nil
		}
		if text == "" && !c.has("image") {
			return n, nil
		}
		if harnessText(text) {
			return n, nil
		}
		kind := "typed"
		if r.PromptSource == "queued" {
			kind = "queued"
		}
		n.role = ctHuman
		return n, turn(kind, text)
	}
	return n, nil
}

// answer reads an AskUserQuestion result into a candidate: nil when it is
// another tool's result, was rejected, or nobody answered. Whether the
// call it answers is AskUserQuestion is checked once the file is read.
func answer(r *ctRecord, turn func(kind, text string) *ctCandidate) *ctCandidate {
	a := r.ToolUseResult
	if !a.ok || len(a.Answers) == 0 {
		return nil
	}
	var id string
	for _, b := range r.Message.Content.blocks {
		if b.Type == "tool_result" {
			if b.IsError {
				return nil
			}
			if id == "" {
				id = b.ToolUseID
			}
		}
	}
	var pairs, questions []string
	done := map[string]bool{}
	add := func(q string) {
		ans, ok := a.Answers[q]
		if !ok || done[q] {
			return
		}
		done[q] = true
		p := q + " → " + ans
		if note := strings.TrimSpace(a.Annotations[q].Notes); note != "" {
			p += " (" + note + ")"
		}
		pairs = append(pairs, p)
	}
	for _, q := range a.Questions {
		questions = append(questions, q.Question)
		add(q.Question)
	}
	// Answers to questions the list does not hold, in a stable order.
	var rest []string
	for q := range a.Answers {
		if !done[q] {
			rest = append(rest, q)
		}
	}
	slices.Sort(rest)
	for _, q := range rest {
		add(q)
	}
	c := turn("answer", strings.Join(pairs, "; "))
	c.toolUseID = id
	c.start = r.ParentUUID
	c.asked = strings.Join(questions, "\n")
	return c
}

// walkAsked walks up from start to the nearest assistant record holding
// text and returns its text key. It returns "" when an earlier human turn,
// or the root, comes first, and fallback when the chain breaks (a parent
// missing from the file, or a loop).
func walkAsked(nodes map[string]ctNode, start, fallback string) string {
	seen := map[string]bool{}
	for id := start; id != ""; {
		if seen[id] {
			return fallback
		}
		seen[id] = true
		n, ok := nodes[id]
		if !ok {
			return fallback
		}
		switch {
		case n.role == ctHuman:
			return ""
		case n.role == ctAssistant && n.msg != "":
			return n.msg
		}
		id = n.up
	}
	return ""
}

// harnessText reports text the harness wrote, not the human.
func harnessText(text string) bool {
	t := strings.TrimLeft(text, " \t\r\n")
	for _, p := range claudeNotTyped {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// slashText turns a slash command record back into what was typed:
// "/name args".
func slashText(t string) string {
	tag := func(name string) string {
		open, end := "<"+name+">", "</"+name+">"
		i := strings.Index(t, open)
		if i < 0 {
			return ""
		}
		s := t[i+len(open):]
		if j := strings.Index(s, end); j >= 0 {
			s = s[:j]
		}
		return strings.TrimSpace(s)
	}
	name := strings.TrimPrefix(tag("command-name"), "/")
	if name == "" {
		name = tag("command-message")
	}
	out := "/" + name
	if args := tag("command-args"); args != "" {
		out += " " + args
	}
	return out
}

// claudeVersionBefore reports whether version v is older than want. A
// version that does not parse counts as older: only old builds wrote none.
func claudeVersionBefore(v, want string) bool {
	a, b := versionParts(v), versionParts(want)
	if a == nil {
		return true
	}
	for i := range max(len(a), len(b)) {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func versionParts(v string) []int {
	var out []int
	for _, p := range strings.Split(strings.TrimSpace(v), ".") {
		end := 0
		for end < len(p) && p[end] >= '0' && p[end] <= '9' {
			end++
		}
		if end == 0 {
			return nil
		}
		n, _ := strconv.Atoi(p[:end])
		out = append(out, n)
		if end < len(p) {
			break // "183-beta": what follows the number is not compared
		}
	}
	return out
}

// cutRunes keeps the first n runes of s.
func cutRunes(s string, n int) string {
	i := 0
	for count := 0; i < len(s) && count < n; count++ {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}

// lastRunes keeps the last n runes of s.
func lastRunes(s string, n int) string {
	i := len(s)
	for count := 0; i > 0 && count < n; count++ {
		_, size := utf8.DecodeLastRuneInString(s[:i])
		i -= size
	}
	return s[i:]
}

// asksSomething reports whether the last paragraph of s, the text after
// its last blank line, holds a question mark anywhere. An agent often puts
// its question mid paragraph and ends on a default ("Delete them? My
// default is to keep them."), and a question in an earlier paragraph is
// usually answered by the paragraphs after it.
func asksSomething(s string) bool {
	lines := strings.Split(strings.TrimRight(s, " \t\r\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			break
		}
		if strings.Contains(lines[i], "?") {
			return true
		}
	}
	return false
}

// pastedContentRE is the tag Claude Code wraps a pasted part of a message
// in since 2.1.277 (seen through 2.1.287), `<pasted_content id="1">` and a
// bare `<pasted_content>`, with its close. The box shows the paste without
// it, so the live sent log does too.
var pastedContentRE = regexp.MustCompile(`</?pasted_content(?:\s[^>]*)?>`)

// stripPastedContent is s with the paste tags taken out and the paste left
// in place: what the user sent.
func stripPastedContent(s string) string {
	if !strings.Contains(s, "pasted_content") {
		return s
	}
	return pastedContentRE.ReplaceAllString(s, "")
}

// normaliseSent is the form two copies of one message are compared in: paste
// tags taken out, tabs as four spaces, every run of whitespace as one space,
// trimmed. What unsent read off the screen and what the transcript holds
// differ in exactly those ways.
func normaliseSent(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(stripPastedContent(s), "\t", "    ")), " ")
}

// claudeSlug is the projects folder name Claude Code uses for a cwd: every
// UTF-16 unit that is not an ASCII letter or digit becomes '-', and a name
// over 200 units is cut to 200 with '-' and a base36 hash of the path
// after it (read from the 2.1.287 binary).
func claudeSlug(cwd string) string {
	units := utf16.Encode([]rune(cwd))
	b := make([]byte, len(units))
	for i, u := range units {
		switch {
		case u >= 'a' && u <= 'z', u >= 'A' && u <= 'Z', u >= '0' && u <= '9':
			b[i] = byte(u)
		default:
			b[i] = '-'
		}
	}
	if len(b) <= 200 {
		return string(b)
	}
	var h int32
	for _, u := range units {
		h = (h << 5) - h + int32(u)
	}
	abs := int64(h)
	if abs < 0 {
		abs = -abs
	}
	return string(b[:200]) + "-" + strconv.FormatInt(abs, 36)
}

// claudeConfigDirs returns the extra config dirs, $CLAUDE_CONFIG_DIR (if
// set) and ~/.claude, existing ones only, in that order. Several config
// dirs can share one projects folder through a symlink, so a dir whose
// projects folder resolves to one already listed is dropped; a dir with no
// projects folder is kept once, for its history.jsonl.
func claudeConfigDirs(extra []string) []string {
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
		if d == "" {
			continue
		}
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			continue
		}
		key := ""
		if p, err := filepath.EvalSymlinks(filepath.Join(d, "projects")); err == nil {
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				key = "projects:" + p
			}
		}
		if key == "" {
			real, err := filepath.EvalSymlinks(d)
			if err != nil {
				continue
			}
			key = "dir:" + real
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	return out
}
