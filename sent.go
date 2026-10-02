package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// A send and a clear both empty the box; the keys seen on input tell them
// apart. The save loop (session.look) counts a draft as sent only when a
// submit key was the last key typed before the box read empty, or the
// first keys after it were typed into an empty box, no clear key came with
// it, and the box was on screen when the key arrived. Anything else counts
// as cleared and the draft goes to history, so doubt never costs text: a
// missed send costs one history entry.

// keyKind is what a key means to send detection.
type keyKind int

const (
	keyOther keyKind = iota
	keySubmit
	keyClear
	// keyRecall is a run of the profile's recall keys (keyset.recall): to
	// send detection another key, and one that brings history into an
	// empty box.
	keyRecall
)

// keyEvent is one submit or clear key, or a run of other keys.
type keyEvent struct {
	kind keyKind
	// before is the screen a submit key, or the first run of other keys
	// after one, or a run of recall keys when the profile browses history
	// from a draft (keyset.browses), was typed into, as the agent last
	// drew it before the key reached it; nil when it was half drawn, and
	// for every other key.
	before *screen
}

// kinds splits typed keys into submit keys, clear keys, recall keys and
// other keys, in order, with runs of other keys, and of recall keys, as
// one. Submit keys are matched first.
// Focus and mouse reports, releases and the terminal's answers are not
// keys and are skipped. loneEsc reports that b ends in an Esc on its own,
// which a following Esc makes Esc Esc.
func (ks keyset) kinds(b []byte) (out []keyKind, loneEsc bool) {
	return ks.kindsOf(pressed(b))
}

func (ks keyset) kindsOf(keys []key) (out []keyKind, loneEsc bool) {
	add := func(k keyKind) {
		if !k.runs() || len(out) == 0 || out[len(out)-1] != k {
			out = append(out, k)
		}
	}
	for i := 0; i < len(keys); {
		loneEsc = false
		if n := chordAt(keys[i:], ks.submit); n > 0 {
			add(keySubmit)
			i += n
			continue
		}
		if n := chordAt(keys[i:], ks.clear); n > 0 {
			add(keyClear)
			i += n
			continue
		}
		if slices.Contains(ks.recall, keys[i]) {
			add(keyRecall)
		} else {
			add(keyOther)
		}
		loneEsc = keys[i] == plain(keyEsc)
		i++
	}
	return out, loneEsc
}

// runs reports whether keys of kind k that follow one another are one
// event: other keys and recall keys are, submit and clear keys never.
func (k keyKind) runs() bool {
	return k == keyOther || k == keyRecall
}

// pressed returns the keys pressed in b, in order (see keysIn).
func pressed(b []byte) []key {
	var keys []key
	for _, k := range keysIn(b) {
		keys = append(keys, k.key)
	}
	return keys
}

// chordAt returns the length of the first of chords that keys start with,
// or 0.
func chordAt(keys []key, chords [][]key) int {
	for _, c := range chords {
		if len(c) > 0 && len(keys) >= len(c) && slices.Equal(keys[:len(c)], c) {
			return len(c)
		}
	}
	return 0
}

// keyLog collects the submit and clear keys typed between two saves. The
// input side pushes keys; the output side gives a submit key, and the keys
// typed right after it, the screen they were typed into, just before the
// agent's next output changes it.
type keyLog struct {
	mu     sync.Mutex
	events []*keyEvent
	// pending holds the keys no output has followed yet that need a screen,
	// and waiting stays true from a submit key until output follows it.
	pending []*keyEvent
	waiting bool
	last    keyKind // the last key pushed, across takes
	esc     bool    // the last keys ended in a lone Esc
}

func (l *keyLog) push(ks keyset, typed []byte) {
	if len(typed) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	// Esc Esc arrives as two reads of one Esc each.
	keys := pressed(typed)
	if l.esc && len(keys) > 0 && keys[0] == plain(keyEsc) {
		keys = append([]key{plain(keyEsc)}, keys...)
	}
	kinds, esc := ks.kindsOf(keys)
	l.esc = esc
	for _, k := range kinds {
		l.add(k, ks.browses)
	}
}

// hold notes a lone Esc the paste tracker holds back until the next read
// tells it from the start of a paste (pasteTracker.holdsEsc). It counts as
// another key now, as a delivered Esc does, so the submit key before it no
// longer vouches for the box emptying. The Esc itself is pushed when it
// arrives, so Esc Esc still reads as a clear.
func (l *keyLog) hold() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.add(keyOther, false)
}

// add appends one key, a run of other keys as one. The caller holds l.mu.
// browses is keyset.browses: the first of a run of recall keys then gets
// the screen it was typed into, as the text typed before it may be on no
// later one.
func (l *keyLog) add(k keyKind, browses bool) {
	after := l.last
	l.last = k
	if n := len(l.events); k.runs() && n > 0 && l.events[n-1].kind == k {
		return
	}
	e := &keyEvent{kind: k}
	l.events = append(l.events, e)
	switch {
	case k == keySubmit:
		l.pending, l.waiting = append(l.pending, e), true
	case k.runs() && after == keySubmit:
		// Typed into the agent's answer to the submit key, if it has
		// drawn one: a box that is empty there was sent.
		l.pending = append(l.pending, e)
	case k == keyRecall && browses && after != keyRecall:
		// Typed into the box as the keys before it left it: the next
		// screens may show an entry of the agent's history instead.
		l.pending = append(l.pending, e)
	}
}

// answer runs before output from the agent is fed to the shadow screen.
// No output has come since the pending keys, so scr() is the screen they
// were typed into.
func (l *keyLog) answer(scr func() *screen) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.settle(scr)
	l.waiting = false
}

// take returns the keys since the last take, in order, and whether the
// agent has drawn anything since the last submit key. scr is the screen
// the save is about to read; the pending keys were typed into it.
func (l *keyLog) take(scr func() *screen) ([]*keyEvent, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.settle(scr)
	events := l.events
	l.events = nil
	return events, !l.waiting
}

// settle gives the pending keys their screen, one snapshot for all of
// them. The caller holds l.mu.
func (l *keyLog) settle(scr func() *screen) {
	if len(l.pending) == 0 {
		return
	}
	before := scr()
	for _, e := range l.pending {
		e.before = before
	}
	l.pending = nil
}

// onSend is the on-send setting for an agent: "log" appends a sent draft to
// the session's sent log, "delete" keeps nothing. UNSENT_ON_SEND_<NAME>
// (UNSENT_ON_SEND_CLAUDE) wins over UNSENT_ON_SEND. An unset or unknown
// value falls through to the next; the default is log for agents and
// delete for shells, because a shell's own history already keeps the lines
// it ran.
func onSend(agent string) string {
	v, _ := onSendFrom(agent)
	return v
}

// onSendFrom is onSend and where it comes from: the variable that set it,
// or "the default".
func onSendFrom(agent string) (value, from string) {
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		}
		return '_'
	}, agent)
	for _, k := range []string{"UNSENT_ON_SEND_" + name, "UNSENT_ON_SEND"} {
		if v := strings.ToLower(os.Getenv(k)); v == "log" || v == "delete" {
			return v, k
		}
	}
	if isShell(agent) {
		return "delete", "the default"
	}
	return "log", "the default"
}

// The sent log keeps every message sent in a session: a header line, then
// one line per message. When the profile reads the agent's own session id
// the log is per conversation, sent/<agent>-<agent_session>.jsonl, and a
// run that resumes the conversation adds a line saying when before its
// first message; otherwise it is per run of unsent, sent/<session>.jsonl.
// It is a convenience copy of text the agent already holds, so unlike
// drafts it expires.
const (
	sentFormat = 1
	// sentMaxBytes caps one session's log; the oldest messages go first.
	sentMaxBytes = 5 << 20
	sentMaxAge   = 365 * 24 * time.Hour
	sentLimit    = 2000
)

// sentHeader is the first line of a sent log. Never rename or retype a
// field without bumping sentFormat.
type sentHeader struct {
	Format int `json:"format"`
	// Session is unsent's id of the run that started the log.
	Session string `json:"session"`
	Agent   string `json:"agent"`
	// AgentSession is the agent's own id of the conversation, the same
	// field the draft record carries; "" for a log kept per run.
	AgentSession string    `json:"agent_session"`
	Cwd          string    `json:"cwd"`
	Started      time.Time `json:"started"`
	// Dropped counts the oldest messages removed to keep the log under
	// sentMaxBytes.
	Dropped int `json:"dropped,omitempty"`
}

// sentMessage is one message line. Fields are only added, so an older build
// reads a newer line; none may be named time, resumed or session, the
// fields sentKind tells the kinds of line apart by.
type sentMessage struct {
	Time time.Time `json:"time"`
	Text string    `json:"text"`
	// Pastes holds pastes the text still shows as a placeholder.
	Pastes []string `json:"pastes,omitempty"`
	// The context `unsent context` reads from the agent's transcript (see
	// claudeimport.go); all empty for a message only the screen saw.
	// UUID is the transcript record of the human turn, the key a second
	// pass dedupes on.
	UUID string `json:"uuid,omitempty"`
	// Kind is how the turn was sent: "" (typed in the box, seen live),
	// "typed", "queued", "absorbed" (typed while the agent worked and taken
	// in mid-turn), "answer" (to the agent's question) or "slash".
	Kind string `json:"kind,omitempty"`
	// Asked is the agent's text just before the message, its last 1000
	// characters (a question comes at the end).
	Asked string `json:"asked,omitempty"`
	// ReplyTo says the message answers Asked: an answer, or text that
	// ended in a question.
	ReplyTo bool `json:"reply_to,omitempty"`
	// Gloss is one line about the message, written later
	// (`unsent context add`).
	Gloss string `json:"gloss,omitempty"`
	// Source is where the message came from: "" the screen, "transcript"
	// or "history" (Claude Code's history.jsonl).
	Source string `json:"source,omitempty"`
}

// sentResume is the line a run adds to a conversation's log it resumed,
// before its first message there.
type sentResume struct {
	Resumed time.Time `json:"resumed"`
	// Session is unsent's id of the run that resumed it.
	Session string `json:"session"`
	// after is how many messages come before it in the log.
	after int
}

type sentLog struct {
	sentHeader
	messages []sentMessage
	resumes  []sentResume
	path     string
}

func (s *store) sentPath(id string) string {
	return filepath.Join(s.dir, "sent", id+".jsonl")
}

// sentPathOf is the sent log a record's messages go to: its conversation's,
// found by agent and agent session, when the profile read the agent's
// session id, else its run's.
func (s *store) sentPathOf(r *record) string {
	if r.AgentSession == "" || !sessionIDRE.MatchString(r.AgentSession) {
		return s.sentPath(r.ID)
	}
	return s.sentPath(r.agent() + "-" + r.AgentSession)
}

// logSent appends the record's draft to its session's sent log, flushed to
// disk. The first message writes the header and prunes old logs.
func (s *store) logSent(r *record) error {
	return s.logSentAt(r, time.Now())
}

// logSentAt is logSent for a message sent at a known time, such as a shell
// line read back from its log.
func (s *store) logSentAt(r *record, when time.Time) error {
	m := sentMessage{Time: when, Text: r.Draft}
	for _, p := range r.Pastes {
		if !strings.Contains(r.Draft, p) {
			m.Pastes = append(m.Pastes, p)
		}
	}
	line, err := json.Marshal(m)
	if err != nil {
		return err
	}
	path, conversation := s.sentPathOf(r), r.AgentSession != ""
	unlock, err := lockSent(path)
	if err != nil {
		return err
	}
	defer unlock()
	joined := r.joined
	if joined.IsZero() {
		joined = r.Started
	}
	var data []byte
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE|os.O_EXCL, 0o600)
	created := err == nil
	if created {
		h := sentHeader{Format: sentFormat, Session: r.ID, Agent: r.agent(), Cwd: r.Cwd, Started: r.Started}
		if conversation {
			// The conversation started in this run, when the run joined it.
			h.AgentSession, h.Started = r.AgentSession, joined
		}
		b, _ := json.Marshal(h)
		data = append(b, '\n')
	} else if errors.Is(err, os.ErrExist) {
		f, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil && conversation && !s.joins[path].Equal(joined) {
			// The conversation has a log from before this run joined it.
			b, _ := json.Marshal(sentResume{Resumed: joined, Session: r.ID})
			data = append(b, '\n')
		}
	}
	if err != nil {
		return err
	}
	data = append(append(data, line...), '\n')
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	var size int64
	if fi, serr := f.Stat(); serr == nil {
		size = fi.Size()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if conversation {
		if s.joins == nil {
			s.joins = map[string]time.Time{}
		}
		s.joins[path] = joined
	}
	if created {
		if err := syncDir(filepath.Dir(path)); err != nil {
			return err
		}
		s.pruneSent()
	}
	if size > sentMaxBytes {
		return trimSent(path)
	}
	return nil
}

// sentLockPath is the lock file of the sent log at path:
// sent/.lock-<name without .jsonl>.
func sentLockPath(path string) string {
	return filepath.Join(filepath.Dir(path), ".lock-"+strings.TrimSuffix(filepath.Base(path), ".jsonl"))
}

// sentLockWait is how long lockSent waits for a lock another process
// holds. A send takes the lock on the wrap's main loop, which also saves the
// box and acts on a window close, so a holder that is stopped (Ctrl+Z) or
// stuck must not hold the loop up for longer; the send then goes to history
// (sendOff). A test shortens it.
var sentLockWait = 2 * time.Second

// errSentBusy is lockSent giving up after sentLockWait.
var errSentBusy = errors.New("the sent log is locked by another process")

// lockSent takes the lock of the sent log at path, waiting for it up to
// sentLockWait, and returns what releases it. Every write to a sent log
// holds it: a send's append, a trim, and `unsent context`, which replaces
// the file, so an append made between its read and its write would land in
// the file it replaced. The kernel drops the lock if the process dies.
func lockSent(path string) (func(), error) {
	lp := sentLockPath(path)
	deadline := time.Now().Add(sentLockWait)
	for {
		f, err := os.OpenFile(lp, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, err
		}
		for {
			err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			if err != syscall.EWOULDBLOCK && err != syscall.EINTR {
				break
			}
			if !time.Now().Before(deadline) {
				f.Close()
				return nil, fmt.Errorf("%s: %w", path, errSentBusy)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err != nil {
			f.Close()
			return nil, err
		}
		// pruneSent removes the lock of a log that is gone while it holds
		// it; a lock on the removed file guards nothing, so take the new
		// one.
		a, aerr := f.Stat()
		b, berr := os.Stat(lp)
		if aerr == nil && berr == nil && os.SameFile(a, b) {
			return func() { f.Close() }, nil
		}
		f.Close()
	}
}

// trimSent drops a sent log's oldest messages until it fits in
// sentMaxBytes, keeping at least the newest, and counts them in the header.
// The caller holds the log's lock (lockSent).
// A resume line stays while a message of the run it marks is kept, and goes
// once the next resume line comes before the first message kept; lines of
// a kind this build does not know stay.
func trimSent(path string) error {
	l, err := readSent(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	body := bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))[1:]
	h, _ := json.Marshal(l.sentHeader)
	size, left := len(h)+1, 0
	kinds := make([]sentLineKind, len(body))
	for i, b := range body {
		size += len(b) + 1
		if kinds[i] = sentKind(b); kinds[i] == sentLineMessage {
			left++
		}
	}
	keep := make([]bool, len(body))
	for i := range keep {
		keep[i] = true
	}
	drop := func(i int) {
		keep[i] = false
		size -= len(body[i]) + 1
	}
	// cut is the first message kept; resume is the last resume line before
	// it, or -1.
	cut, resume, dropped := 0, -1, 0
	settle := func() {
		for ; cut < len(body) && kinds[cut] != sentLineMessage; cut++ {
			if kinds[cut] == sentLineResume {
				if resume >= 0 {
					// Every message of the run it marks is gone.
					drop(resume)
				}
				resume = cut
			}
		}
	}
	settle()
	for size > sentMaxBytes && left > 1 {
		drop(cut)
		cut++
		left--
		dropped++
		settle()
	}
	if dropped == 0 {
		return nil
	}
	l.Dropped += dropped
	h, _ = json.Marshal(l.sentHeader)
	out := append(h, '\n')
	for i, b := range body {
		if keep[i] {
			out = append(append(out, b...), '\n')
		}
	}
	return writeFileDurable(path, out)
}

// sentLineKind is what one line after a sent log's header holds.
type sentLineKind int

const (
	sentLineOther sentLineKind = iota // a later build's, or cut short
	sentLineMessage
	sentLineResume
)

// sentKind tells a sent log line's kind the way readSent does.
func sentKind(line []byte) sentLineKind {
	var m struct {
		sentMessage
		sentResume
	}
	switch {
	case json.Unmarshal(line, &m) != nil:
	case !m.Resumed.IsZero():
		return sentLineResume
	case !m.Time.IsZero():
		return sentLineMessage
	}
	return sentLineOther
}

// pruneSent deletes sent logs not written for sentMaxAge, then the oldest
// beyond sentLimit, then the lock files of logs that are gone and the
// context pass records (contextStatePath) older than contextDebounce,
// which no longer skip anything.
func (s *store) pruneSent() {
	pattern := filepath.Join(s.dir, "sent", "*.jsonl")
	names, _ := filepath.Glob(pattern)
	for _, n := range names {
		if fi, err := os.Stat(n); err == nil && time.Since(fi.ModTime()) > sentMaxAge {
			os.Remove(n)
		}
	}
	trimOldest(pattern, sentLimit)
	locks, _ := filepath.Glob(filepath.Join(s.dir, "sent", ".lock-*"))
	for _, lp := range locks {
		log := filepath.Join(filepath.Dir(lp), strings.TrimPrefix(filepath.Base(lp), ".lock-")+".jsonl")
		if _, err := os.Stat(log); err == nil {
			continue
		}
		f, err := os.Open(lp)
		if err != nil {
			continue
		}
		// Only a lock nobody holds goes, and only while this holds it
		// (see lockSent).
		if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
			if _, err := os.Stat(log); os.IsNotExist(err) {
				os.Remove(lp)
			}
		}
		f.Close()
	}
	states, _ := filepath.Glob(filepath.Join(s.dir, "sent", ".context-*.state"))
	for _, p := range states {
		if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) > contextDebounce {
			os.Remove(p)
		}
	}
}

// readSent reads one sent log. A line cut short by a crash is skipped.
func readSent(path string) (*sentLog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := bytes.Split(data, []byte("\n"))
	l := &sentLog{path: path}
	if json.Unmarshal(lines[0], &l.sentHeader) != nil || l.Format == 0 {
		return nil, errors.New(path + " is not a sent log")
	}
	for _, line := range lines[1:] {
		var m struct {
			sentMessage
			sentResume
		}
		// A message has a time; a line of a kind a later build added has
		// neither a time nor a resume, and is skipped.
		switch {
		case len(bytes.TrimSpace(line)) == 0 || json.Unmarshal(line, &m) != nil:
		case !m.Resumed.IsZero():
			m.sentResume.after = len(l.messages)
			l.resumes = append(l.resumes, m.sentResume)
		case !m.Time.IsZero():
			l.messages = append(l.messages, m.sentMessage)
		}
	}
	return l, nil
}

// walk calls message for each of the log's messages in order, numbered
// from 1 as --copy takes them, and resume for each resume line, before the
// first message of the run it marks.
func (l *sentLog) walk(message func(n int, m sentMessage), resume func(sentResume)) {
	rs := l.resumes
	resumed := func(before int) {
		for len(rs) > 0 && rs[0].after <= before {
			resume(rs[0])
			rs = rs[1:]
		}
	}
	for i, m := range l.messages {
		resumed(i)
		message(i+1, m)
	}
	resumed(len(l.messages))
}

// sentLogs reads every sent log, newest session first.
func (s *store) sentLogs() []*sentLog {
	names, _ := filepath.Glob(filepath.Join(s.dir, "sent", "*.jsonl"))
	var out []*sentLog
	for _, n := range names {
		if l, err := readSent(n); err == nil {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Started.Equal(out[j].Started) {
			return out[i].Started.After(out[j].Started)
		}
		return out[i].Session > out[j].Session
	})
	return out
}
