package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
)

// keyEvent is one submit or clear key, or a run of other keys.
type keyEvent struct {
	kind keyKind
	// before is the screen a submit key, or the first run of other keys
	// after one, was typed into, as the agent last drew it before the key
	// reached it; nil when it was half drawn, and for every other key.
	before *screen
}

// kinds splits typed keys into submit keys, clear keys and other keys, in
// order, with runs of other keys as one. Submit keys are matched first.
// Focus and mouse reports are not keys and are skipped. loneEsc reports
// that b ends in an Esc on its own, which a following Esc makes Esc Esc.
func (ks keyset) kinds(b []byte) (out []keyKind, loneEsc bool) {
	add := func(k keyKind) {
		if k != keyOther || len(out) == 0 || out[len(out)-1] != keyOther {
			out = append(out, k)
		}
	}
	for i := 0; i < len(b); {
		loneEsc = false
		if n := prefixOf(b[i:], ks.submit); n > 0 {
			add(keySubmit)
			i += n
			continue
		}
		if n := prefixOf(b[i:], ks.clear); n > 0 {
			add(keyClear)
			i += n
			continue
		}
		n, key := keyLen(b[i:])
		if key {
			add(keyOther)
		}
		loneEsc = n == 1 && b[i] == 0x1b
		i += n
	}
	return out, loneEsc
}

// prefixOf returns the length of the first of keys that b starts with, or 0.
func prefixOf(b []byte, keys [][]byte) int {
	for _, k := range keys {
		if bytes.HasPrefix(b, k) {
			return len(k)
		}
	}
	return 0
}

// keyLen returns the length of the key that starts b, and false when it is
// a focus or mouse report rather than a key: Claude Code turns on focus
// events and all-motion mouse tracking, so these arrive with every move.
func keyLen(b []byte) (int, bool) {
	if b[0] != 0x1b || len(b) == 1 || b[1] == 0x1b {
		return 1, true
	}
	switch b[1] {
	case 'O': // SS3: F1 to F4, and arrows in application mode
		return min(3, len(b)), true
	case '[':
	default: // Alt and a key
		return 2, true
	}
	j := 2
	for j < len(b) && b[j] >= 0x20 && b[j] <= 0x3f {
		j++
	}
	if j == len(b) {
		return j, true
	}
	params, final := b[2:j], b[j]
	switch {
	case len(params) > 0 && params[0] == '<': // SGR mouse
		return j + 1, false
	case len(params) == 0 && (final == 'I' || final == 'O'): // focus in, out
		return j + 1, false
	case len(params) == 0 && final == 'M': // X10 mouse: three more bytes
		return min(j+4, len(b)), false
	}
	return j + 1, true
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
	if l.esc && typed[0] == 0x1b && (len(typed) == 1 || typed[1] == 0x1b) {
		typed = append([]byte{0x1b}, typed...)
	}
	kinds, esc := ks.kinds(typed)
	l.esc = esc
	for _, k := range kinds {
		after := l.last
		l.last = k
		if n := len(l.events); k == keyOther && n > 0 && l.events[n-1].kind == keyOther {
			continue
		}
		e := &keyEvent{kind: k}
		l.events = append(l.events, e)
		switch {
		case k == keySubmit:
			l.pending, l.waiting = append(l.pending, e), true
		case k == keyOther && after == keySubmit:
			// Typed into the agent's answer to the submit key, if it has
			// drawn one: a box that is empty there was sent.
			l.pending = append(l.pending, e)
		}
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

// The sent log keeps every message sent in a session, in sent/<session>.jsonl:
// a header line, then one line per message. It is a convenience copy of
// text the agent already holds, so unlike drafts it expires.
const (
	sentFormat = 1
	// sentMaxBytes caps one session's log; the oldest messages go first.
	sentMaxBytes = 5 << 20
	sentMaxAge   = 90 * 24 * time.Hour
	sentLimit    = 2000
)

// sentHeader is the first line of a sent log. Never rename or retype a
// field without bumping sentFormat.
type sentHeader struct {
	Format  int       `json:"format"`
	Session string    `json:"session"`
	Agent   string    `json:"agent"`
	Cwd     string    `json:"cwd"`
	Started time.Time `json:"started"`
	// Dropped counts the oldest messages removed to keep the log under
	// sentMaxBytes.
	Dropped int `json:"dropped,omitempty"`
}

type sentMessage struct {
	Time time.Time `json:"time"`
	Text string    `json:"text"`
	// Pastes holds pastes the text still shows as a placeholder.
	Pastes []string `json:"pastes,omitempty"`
}

type sentLog struct {
	sentHeader
	messages []sentMessage
	path     string
}

func (s *store) sentPath(id string) string {
	return filepath.Join(s.dir, "sent", id+".jsonl")
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
	path := s.sentPath(r.ID)
	var data []byte
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE|os.O_EXCL, 0o600)
	created := err == nil
	if created {
		h, _ := json.Marshal(sentHeader{Format: sentFormat, Session: r.ID, Agent: r.agent(), Cwd: r.Cwd, Started: r.Started})
		data = append(h, '\n')
	} else if errors.Is(err, os.ErrExist) {
		f, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
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

// trimSent drops a sent log's oldest messages until it fits in
// sentMaxBytes, keeping at least the newest, and counts them in the header.
func trimSent(path string) error {
	l, err := readSent(path)
	if err != nil {
		return err
	}
	h, _ := json.Marshal(l.sentHeader)
	var lines [][]byte
	size := len(h) + 1
	for _, m := range l.messages {
		b, _ := json.Marshal(m)
		lines = append(lines, b)
		size += len(b) + 1
	}
	drop := 0
	for drop < len(lines)-1 && size > sentMaxBytes {
		size -= len(lines[drop]) + 1
		drop++
	}
	if drop == 0 {
		return nil
	}
	l.Dropped += drop
	h, _ = json.Marshal(l.sentHeader)
	data := append(h, '\n')
	for _, b := range lines[drop:] {
		data = append(append(data, b...), '\n')
	}
	return writeFileDurable(path, data)
}

// pruneSent deletes sent logs not written for sentMaxAge, then the oldest
// beyond sentLimit.
func (s *store) pruneSent() {
	pattern := filepath.Join(s.dir, "sent", "*.jsonl")
	names, _ := filepath.Glob(pattern)
	for _, n := range names {
		if fi, err := os.Stat(n); err == nil && time.Since(fi.ModTime()) > sentMaxAge {
			os.Remove(n)
		}
	}
	trimOldest(pattern, sentLimit)
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
		var m sentMessage
		if len(bytes.TrimSpace(line)) > 0 && json.Unmarshal(line, &m) == nil {
			l.messages = append(l.messages, m)
		}
	}
	return l, nil
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
