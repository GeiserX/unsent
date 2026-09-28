package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Restore-in-box: when the agent is in a conversation that left a draft
// behind (an orphan whose agent_session is the running session's id), the
// draft goes back into the empty box as one bracketed paste, never sent.
// Every condition below must hold before a byte is written, and the orphan
// moves to history only once a save reads the draft back from the box.
//
//  1. The command line starts a chat (restoreCaps.chat): no prompt
//     argument, no subcommand.
//  2. The box was first found in this session at least settle ago, and read
//     empty on the last empties reads in a row.
//  3. Nothing was typed since the box was first found in this session:
//     keys typed while it was out of view (a resume picker, the /resume that
//     opened one) come before and do not count; focus and mouse reports are
//     not typing.
//  4. The agent has bracketed paste on, so a line break in the draft is not
//     Enter; every ESC is taken out of the draft (cleanPaste), so it cannot
//     end the paste early.
//  5. The screen is the box, not a dialog: the reader finds the box, and
//     empty.
//  6. The profile says the paste puts the draft back as it is
//     (restoreCaps.faithful); otherwise the draft goes to the clipboard.
//  7. This process claimed the orphan (store.claim): of two terminals that
//     open one conversation, one injects.

// restoreCaps is what a profile knows about putting a draft back in its box.
type restoreCaps struct {
	// settle is how long after the box first appears a paste can go in,
	// and empties how many reads in a row must find it empty before.
	settle  time.Duration
	empties int
	// chat reports whether a command line (the arguments after the
	// command) opens the agent's chat box with nothing sent on start.
	chat func(args []string) bool
	// faithful says why a paste would not put back draft as it is, or "".
	faithful func(draft string) string
}

// maxRestoreTries is how many restores of one orphan may fail to read back
// before unsent stops injecting it and leaves it for `unsent restore`.
const maxRestoreTries = 3

// verifyWait is how long a save has, after the paste, to read the draft
// back from the box before the restore counts as failed; tests shorten it.
var verifyWait = 5 * time.Second

// sessionMatch reports whether an orphan's session is the running one. A
// test swaps it for one that is always true, which must fail the new-chat
// test.
var sessionMatch = func(orphan, running string) bool { return orphan != "" && orphan == running }

// restoreState is one run's restore-in-box state. Only the save loop's
// goroutine touches it, except lastKey (the input side) and shown (the
// output side, under session.mu).
type restoreState struct {
	// on is set when this run can restore at all.
	on bool
	// lastKey is when the user last typed a key, in Unix nanoseconds.
	lastKey atomic.Int64
	// id is the session the rest is about, and since when its box was first
	// in view (zero until then); done is set once a restore fired, failed or
	// was cancelled in it; empties counts empty reads in a row.
	id      string
	since   time.Time
	done    bool
	empties int
	// inj is the restore waiting to be read back.
	inj *injection
	// back holds the drafts put back this run, for the lines after exit.
	back []string
}

// injection is a draft pasted into the box, claimed from its orphan file.
type injection struct {
	orphan *record
	claim  string
	text   string
	at     time.Time
}

// typedSince reports whether the user typed a key after t.
func (rs *restoreState) typedSince(t time.Time) bool {
	return rs.lastKey.Load() > t.UnixNano()
}

// notices reports whether the draft notices are on: UNSENT_NOTICE=0 turns
// off the notice before start and after exit, and the line under the box,
// for sessions nobody watches. Restore-in-box still runs.
func notices() bool {
	return os.Getenv("UNSENT_NOTICE") != "0"
}

// cleanPaste is the draft as it goes into the box: without ESC, so an
// ESC[201~ in it cannot end the paste and let a line break submit, and
// without C1 controls, which some parsers read as CSI.
func cleanPaste(draft string) string {
	return strings.Map(func(r rune) rune {
		if r == 0x1b || r >= 0x80 && r <= 0x9f {
			return -1
		}
		return r
	}, draft)
}

// watchBox runs on the output side, under s.mu, after each chunk: it notes
// when the box came into view, and forgets it while the box is out of view.
func (s *session) watchBox() {
	if !s.rs.on || s.inFrame {
		return
	}
	if _, ok := s.prof.read(snapshot(s.screen)); !ok {
		s.shown = time.Time{}
	} else if s.shown.IsZero() {
		s.shown = time.Now()
	}
}

// tryRestore runs at every tick. It checks the injection waiting to be read
// back, and otherwise whether every condition for a restore holds now; the
// session id it uses is the one read now.
func (s *session) tryRestore() {
	if !s.rs.on || s.broken.Load() {
		return
	}
	defer s.guard()
	now := time.Now()
	if inj := s.rs.inj; inj != nil {
		if now.Sub(inj.at) > verifyWait {
			s.rs.inj = nil
			s.store.unclaim(inj.claim, inj.orphan)
		}
		return
	}
	id := s.ids.current()
	if id != s.rs.id {
		s.rs.id, s.rs.since, s.rs.done, s.rs.empties = id, time.Time{}, false, 0
	}
	if id == "" || s.rs.done {
		return
	}
	scr, shown, bracketed := s.restoreView()
	if scr == nil {
		return
	}
	if s.rs.since.IsZero() {
		s.rs.since = shown
	}
	caps := s.prof.restore
	if s.rs.since.IsZero() || now.Before(s.rs.since.Add(caps.settle)) {
		return
	}
	if v, ok := s.prof.read(scr); ok && v.empty && bracketed && !shown.IsZero() {
		s.rs.empties++
	} else {
		s.rs.empties = 0
	}
	if s.rs.empties < caps.empties {
		return
	}
	// One try per session: from here on, whatever happens, this session's
	// box gets no second paste in this run.
	s.rs.done = true
	r := s.store.sessionOrphan(s.rec.agent(), s.rec.Cwd, id)
	if r == nil {
		return
	}
	claim, err := s.store.claim(r, s.rec.ID)
	if err != nil {
		return // another terminal took it
	}
	if !s.ptyMu.TryLock() {
		// The input side is passing bytes on right now, a key or only a
		// mouse report: look again at the next tick.
		s.store.unclaimAsIs(claim, r)
		s.rs.done = false
		return
	}
	if s.rs.typedSince(s.rs.since) {
		// Typed since this session's box appeared: the user is at it, and
		// this session gets no paste.
		s.ptyMu.Unlock()
		s.store.unclaimAsIs(claim, r)
		return
	}
	if why := caps.faithful(r.Draft); why != "" {
		s.ptyMu.Unlock()
		s.clipboardInstead(r, why, claim)
		return
	}
	text := cleanPaste(r.Draft)
	paste := append(append(append([]byte(nil), pasteStart...), text...), pasteEnd...)
	// The paste tracker sees it as a paste, so the placeholder the agent
	// shows for a long one reads back as the text.
	s.pastes.feed(paste)
	s.rs.inj = &injection{orphan: r, claim: claim, text: text, at: now}
	// The lock goes with the write: a key typed meanwhile follows the paste
	// and never lands inside it. Written apart from the loop, since a write
	// to an agent that is not reading would hold the loop.
	go func() {
		defer s.ptyMu.Unlock()
		s.toAgent(paste)
	}()
}

// restoreView is the screen as it stands, with when the box came into view
// and whether the agent has bracketed paste on; a nil screen while the
// agent is halfway through a frame.
func (s *session) restoreView() (*screen, time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inFrame {
		return nil, time.Time{}, false
	}
	return snapshot(s.screen), s.shown, s.bracketed
}

// clipboardInstead handles a draft the box would not take back as it is:
// it goes to the clipboard, the line under the box says so, and the orphan
// stays for `unsent restore`.
func (s *session) clipboardInstead(r *record, why, claim string) {
	s.store.unclaim(claim, r)
	msg := fmt.Sprintf("unsent: your draft from %s %s; copied it to the clipboard, not sent", when(r.Updated), why)
	if err := copyToClipboard(r.Draft); err != nil {
		msg = fmt.Sprintf("unsent: your draft from %s %s; `unsent restore` copies it", when(r.Updated), why)
	}
	s.note(msg)
	s.rs.back = append(s.rs.back, msg)
}

// verifyRestore runs at every save's read of the box: the first read that
// holds the pasted draft (the same, or with a line break read back as a
// space at a full row) moves the orphan to history and says it was put
// back.
func (s *session) verifyRestore(draft string, width int) {
	inj := s.rs.inj
	if inj == nil || draft != inj.text && !byteExact(draft, inj.text, width) {
		return
	}
	s.rs.inj = nil
	if err := s.store.keepRestored(inj.claim, inj.orphan); err != nil {
		s.store.warn(err)
		s.store.unclaim(inj.claim, inj.orphan)
		return
	}
	msg := fmt.Sprintf("unsent: put back your draft from %s, not sent", when(inj.orphan.Updated))
	s.note(msg)
	s.rs.back = append(s.rs.back, msg)
}

// note draws msg on the row under the box, once the agent's frame is
// complete, unless the notices are off.
func (s *session) note(msg string) {
	if !notices() || s.out == nil {
		return
	}
	s.mu.Lock()
	v, ok := s.prof.read(snapshot(s.screen))
	s.mu.Unlock()
	if !ok || v.under < 0 {
		return
	}
	s.out.show(fmt.Sprintf("\x1b7\x1b[%d;1H\x1b[0m\x1b[2K%s\x1b8", v.under+1, msg))
}

// endRestore runs when the agent exits: a paste not read back yet puts its
// orphan back.
func (s *session) endRestore() {
	if inj := s.rs.inj; inj != nil {
		s.rs.inj = nil
		s.store.unclaim(inj.claim, inj.orphan)
	}
}

// restoreLines say after exit what was put back while the agent ran, since
// the line under the box went with the agent's screen.
func (s *session) restoreLines() []string {
	if !notices() {
		return nil
	}
	return s.rs.back
}

// termOut is the user's terminal. It passes the agent's output on, and
// draws a line of unsent's own only between complete frames, never inside
// an escape sequence or a synchronized frame, so it never lands in the
// agent's box or in the middle of its drawing.
type termOut struct {
	mu      sync.Mutex
	w       io.Writer
	tail    []byte
	inFrame bool
	last    time.Time
	pending string
}

func (t *termOut) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n, err := t.w.Write(b)
	data := append(t.tail, b...)
	if i, j := bytes.LastIndex(data, frameBegin), bytes.LastIndex(data, frameEnd); i > j {
		t.inFrame = true
	} else if j > i {
		t.inFrame = false
	}
	if len(data) > 64 {
		data = data[len(data)-64:]
	}
	t.tail = append(t.tail[:0], data...)
	t.last = time.Now()
	if bytes.HasSuffix(b, frameEnd) {
		t.flushLocked()
	}
	return n, err
}

// show queues a line to draw at the next clean moment.
func (t *termOut) show(s string) {
	t.mu.Lock()
	t.pending = s
	t.mu.Unlock()
	t.tick()
}

// tick draws the queued line once the agent has been quiet a moment.
func (t *termOut) tick() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if time.Since(t.last) >= 50*time.Millisecond {
		t.flushLocked()
	}
}

func (t *termOut) flushLocked() {
	if t.pending == "" || t.inFrame || !groundState(t.tail) {
		return
	}
	io.WriteString(t.w, t.pending)
	t.pending = ""
}

// groundState reports whether output ending in tail leaves the terminal
// between sequences: not inside an escape sequence, a string or a UTF-8
// character.
func groundState(tail []byte) bool {
	if !endsWhole(tail) {
		return false
	}
	i := bytes.LastIndexByte(tail, 0x1b)
	if i < 0 {
		return true
	}
	seq := tail[i+1:]
	switch {
	case len(seq) == 0:
		return false
	case seq[0] == '[':
		// Parameters and intermediates, then a final byte.
		return bytes.IndexFunc(seq[1:], func(r rune) bool { return r >= 0x40 && r <= 0x7e }) >= 0
	case bytes.IndexByte([]byte("]P_^X"), seq[0]) >= 0:
		// A string ends with BEL, or with ESC \, whose ESC is the last.
		return bytes.IndexByte(seq, 0x07) >= 0
	case seq[0] >= 0x20 && seq[0] <= 0x2f:
		// An intermediate byte, then a final one.
		return len(seq) >= 2
	}
	return true
}

// endsWhole reports whether b does not end partway through a UTF-8
// character.
func endsWhole(b []byte) bool {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			return b[i] < utf8.RuneSelf || utf8.FullRune(b[i:])
		}
	}
	return true
}
