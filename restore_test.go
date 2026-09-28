package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

// quickClaude is Claude Code's profile with no settle delay, so a restore
// needs only its three empty reads.
func quickClaude() *profile {
	p := claude
	caps := *claude.restore
	caps.settle = 0
	p.restore = &caps
	return &p
}

// restoreRig is a session on a hand-drawn screen whose agent is in session
// *id, with bracketed paste on. What the restore writes to the agent and to
// the user's terminal is kept.
type restoreRig struct {
	sendSession
	id     *string
	sentMu sync.Mutex
	sent   []byte
	term   lockedBuffer
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func newRestoreRig(t *testing.T, prof *profile, id string) *restoreRig {
	t.Helper()
	return newRestoreRigIn(t, prof, id, "")
}

// rigs numbers the rigs' records, so rigs sharing a state folder never
// share a record id or its lock.
var rigs atomic.Int64

// newRestoreRigIn is newRestoreRig with the state folder dir, so rigs can
// share one ("" for a folder of its own).
func newRestoreRigIn(t *testing.T, prof *profile, id, dir string) *restoreRig {
	t.Helper()
	r := &restoreRig{sendSession: newSendSession(t, prof), id: &id}
	if dir != "" {
		r.store = &store{dir: dir}
	}
	r.rec.ID = fmt.Sprintf("%s-rig%d", r.rec.ID, rigs.Add(1))
	r.rs.on = true
	fakeIDs(r.session, r.id)
	r.toAgent = func(b []byte) {
		r.sentMu.Lock()
		r.sent = append(r.sent, b...)
		r.sentMu.Unlock()
	}
	r.out = &termOut{w: &r.term}
	if err := r.store.hold(r.rec); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.store.release)
	// The alternate screen, where the line under the box is drawn, and
	// bracketed paste on.
	r.write([]byte("\x1b[?1049h\x1b[?2004h"))
	return r
}

// agentWriter is the agent's side of the pseudo-terminal for keys the
// rig forwards: they land where the restore's paste does.
type agentWriter struct{ r *restoreRig }

func (w agentWriter) Write(b []byte) (int, error) {
	w.r.sentMu.Lock()
	defer w.r.sentMu.Unlock()
	w.r.sent = append(w.r.sent, b...)
	return len(b), nil
}

// ticks runs n ticks of the save loop: a save, then the restore check.
func (r *restoreRig) ticks(n int) {
	r.t.Helper()
	for range n {
		r.save()
		r.tryRestore()
	}
}

// pasted is what the restore wrote to the agent, once the write is done.
func (r *restoreRig) pasted() string {
	r.ptyMu.Lock()
	r.ptyMu.Unlock()
	r.sentMu.Lock()
	defer r.sentMu.Unlock()
	return string(r.sent)
}

// seedOrphan writes a draft a dead Claude Code session left in /work, in
// conversation session.
func seedOrphan(t *testing.T, st *store, id, agent, session, draft string) *record {
	t.Helper()
	r := newRecord([]string{agent}, "/work")
	r.ID, r.Agent, r.AgentSession, r.Draft, r.Ended = id, agent, session, draft, time.Now()
	r.Updated = time.Now().Add(-time.Hour)
	if err := st.write(r); err != nil {
		t.Fatal(err)
	}
	// The lock file its session held, unlocked since it died.
	if err := os.WriteFile(st.lockPath(id), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return r
}

// orphanAt reads the orphan file id, or nil when it is gone.
func orphanAt(st *store, id string) *record {
	for _, r := range st.orphans() {
		if r.ID == id {
			return r
		}
	}
	return nil
}

func inHistory(st *store, draft string) bool {
	for _, r := range st.load(true) {
		if r.Draft == draft && !isDraftFile(st, r) {
			return true
		}
	}
	return false
}

// The draft of the running session goes back into the empty box as one
// bracketed paste, after three empty reads; once a save reads it back, the
// orphan moves to history and a line under the box says it was not sent.
func TestRestorePastesTheSessionsDraft(t *testing.T) {
	r := newRestoreRig(t, quickClaude(), "conv-a")
	draft := "left in conv a\nsecond line"
	seedOrphan(t, r.store, "old-a", "claude", "conv-a", draft)
	r.draw("")
	r.ticks(2)
	if got := r.pasted(); got != "" {
		t.Fatalf("pasted after two reads: %q", got)
	}
	r.ticks(1)
	if got := r.pasted(); got != "\x1b[200~"+draft+"\x1b[201~" {
		t.Fatalf("pasted %q", got)
	}
	if orphanAt(r.store, "old-a") != nil {
		t.Fatal("the claimed orphan is still offered")
	}
	r.draw(draft)
	r.ticks(1)
	if orphanAt(r.store, "old-a") != nil || !inHistory(r.store, draft) {
		t.Fatal("the orphan did not move to history once read back")
	}
	if names, _ := filepath.Glob(filepath.Join(r.store.dir, "drafts", "*"+claimMark+"*")); len(names) != 0 {
		t.Fatalf("claims left: %v", names)
	}
	if _, err := os.Stat(r.store.lockPath("old-a")); err == nil {
		t.Fatal("the orphan's lock file is left behind")
	}
	if r.rec.Draft != draft || r.rec.AgentSession != "conv-a" {
		t.Fatalf("the session's own record: %q in %q", r.rec.Draft, r.rec.AgentSession)
	}
	r.expect(nil, []string{draft})
	if got := r.term.String(); !strings.Contains(got, "unsent: put back your draft from") || !strings.HasPrefix(got, "\x1b7\x1b[") || !strings.HasSuffix(got, "not sent\x1b8") {
		t.Fatalf("line under the box %q", got)
	}
	if got := r.restoreLines(); len(got) != 1 || !strings.Contains(got[0], "put back your draft") {
		t.Fatalf("lines after exit %q", got)
	}
	// One restore per session: an empty box later gets nothing.
	r.draw("")
	r.ticks(5)
	if got := r.pasted(); strings.Count(got, "\x1b[200~") != 1 {
		t.Fatalf("pasted again: %q", got)
	}
}

// A long draft goes back as one paste, and the placeholder the agent shows
// for it reads back as the draft.
func TestRestoreLongDraftReadsBackThroughThePlaceholder(t *testing.T) {
	r := newRestoreRig(t, quickClaude(), "conv-a")
	draft := strings.Repeat("a line of a long draft\n", 7) + "the end"
	seedOrphan(t, r.store, "old-a", "claude", "conv-a", draft)
	r.draw("")
	r.ticks(3)
	r.draw("[Pasted text #1 +7 lines]")
	r.ticks(1)
	if orphanAt(r.store, "old-a") != nil || r.rec.Draft != draft {
		t.Fatalf("not read back: record %q", r.rec.Draft)
	}
}

// An ESC[201~ and a line break inside the draft can never end the paste
// and submit it: every ESC is taken out, so only unsent's own markers are
// escape sequences.
func TestRestoreDraftCannotEndThePaste(t *testing.T) {
	r := newRestoreRig(t, quickClaude(), "conv-a")
	seedOrphan(t, r.store, "old-a", "claude", "conv-a", "first\x1b[201~\rsecond\x1b[201~\n\u009b201~")
	r.draw("")
	r.ticks(3)
	got := r.pasted()
	if got != "\x1b[200~first[201~\rsecond[201~\n201~\x1b[201~" || strings.Count(got, "\x1b") != 2 {
		t.Fatalf("pasted %q", got)
	}
}

// A paste the box never reads back leaves the orphan where it was, with
// the failed try counted; after three, it is left for unsent restore.
func TestRestoreUnverifiedKeepsTheOrphan(t *testing.T) {
	old := verifyWait
	verifyWait = 10 * time.Millisecond
	t.Cleanup(func() { verifyWait = old })
	dir := testStore(t).dir
	seedOrphan(t, &store{dir: dir}, "old-a", "claude", "conv-a", "never shown")
	for try := 1; try <= maxRestoreTries+1; try++ {
		r := newRestoreRigIn(t, quickClaude(), "conv-a", dir)
		r.draw("")
		r.ticks(3)
		if pasted := r.pasted() != ""; pasted != (try <= maxRestoreTries) {
			t.Fatalf("try %d: pasted %v", try, pasted)
		}
		// The agent draws the box without the draft.
		r.draw("")
		r.ticks(1)
		time.Sleep(20 * time.Millisecond)
		r.ticks(1)
		o := orphanAt(r.store, "old-a")
		if o == nil || o.Draft != "never shown" || o.RestoreTries != min(try, maxRestoreTries) {
			t.Fatalf("try %d: orphan %+v", try, o)
		}
		if inHistory(r.store, "never shown") {
			t.Fatalf("try %d: an unverified restore went to history", try)
		}
	}
}

// A key typed after the box appeared and before the restore cancels it,
// even one that leaves the box empty.
func TestRestoreCancelledByAKeypress(t *testing.T) {
	prof := quickClaude()
	prof.restore.settle = time.Hour
	r := newRestoreRig(t, prof, "conv-a")
	seedOrphan(t, r.store, "old-a", "claude", "conv-a", "keep me")
	r.draw("")
	r.ticks(1)
	r.input([]byte("\x1b[A"))
	prof.restore.settle = 0
	r.ticks(5)
	if got := r.pasted(); got != "" {
		t.Fatalf("pasted after a keypress: %q", got)
	}
	if o := orphanAt(r.store, "old-a"); o == nil || o.RestoreTries != 0 {
		t.Fatalf("orphan %+v", o)
	}
}

// Focus reports, mouse reports and the terminal's answers to the agent's
// queries are not typing: tmux answers Claude Code's cell size query right
// after its box is drawn.
func TestRestoreIgnoresFocusAndMouseReports(t *testing.T) {
	r := newRestoreRig(t, quickClaude(), "conv-a")
	seedOrphan(t, r.store, "old-a", "claude", "conv-a", "keep me")
	r.draw("")
	r.ticks(1)
	r.input([]byte("\x1b[O"))
	r.input([]byte("\x1b[I\x1b[<35;10;5M"))
	r.input([]byte("\x1bP>|tmux 3.6b\x1b\\\x1b[?1;2;4c"))
	r.input([]byte("\x1b[6;32;16t\x1b[?1;2;4c"))
	r.ticks(2)
	if got := r.pasted(); got != "\x1b[200~keep me\x1b[201~" {
		t.Fatalf("pasted %q", got)
	}
}

// Keys pressed in a resume picker, where the box is out of view, come
// before the chosen session's box and do not cancel its restore; nor does
// the /resume typed in the session before.
func TestRestorePickerKeysDoNotCancel(t *testing.T) {
	r := newRestoreRig(t, quickClaude(), "conv-z")
	seedOrphan(t, r.store, "old-a", "claude", "conv-a", "keep me")
	r.draw("")
	// Idle long enough for conv-z's one try, which finds no orphan: the
	// switch gives conv-a a try of its own.
	r.ticks(5)
	r.input([]byte("/resume"))
	r.draw("/resume")
	r.input([]byte("\r"))
	r.write([]byte("\x1b[H\x1b[2JResume session\r\n   ❯ a conversation\r\n"))
	r.ticks(1)
	r.input([]byte("\x1b[B"))
	r.input([]byte("\r"))
	*r.id = "conv-a"
	r.draw("")
	r.ticks(3)
	if got := r.pasted(); got != "\x1b[200~keep me\x1b[201~" {
		t.Fatalf("pasted %q", got)
	}
}

// Two terminals open one conversation: one of them claims the orphan and
// pastes it, the other finds nothing to paste.
func TestRestoreTwoTerminalsInjectOnce(t *testing.T) {
	a := newRestoreRig(t, quickClaude(), "conv-a")
	b := newRestoreRigIn(t, quickClaude(), "conv-a", a.store.dir)
	seedOrphan(t, a.store, "old-a", "claude", "conv-a", "only once")
	a.draw("")
	b.draw("")
	for range 3 {
		a.ticks(1)
		b.ticks(1)
	}
	if n := strings.Count(a.pasted()+b.pasted(), "only once"); n != 1 {
		t.Fatalf("pasted %d times: %q and %q", n, a.pasted(), b.pasted())
	}
}

// The claim itself: of two processes that found the orphan, one rename
// succeeds. A claim whose session is gone puts the orphan back.
func TestStoreClaim(t *testing.T) {
	st := testStore(t)
	r := seedOrphan(t, st, "old-a", "claude", "conv-a", "claimed")
	live := newRecord([]string{"claude"}, "/work")
	live.ID = "live"
	if err := st.hold(live); err != nil {
		t.Fatal(err)
	}
	path, err := st.claim(r, "live")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.claim(r, "other"); err == nil {
		t.Fatal("a second claim succeeded")
	}
	if orphanAt(st, "old-a") != nil || len(st.orphans()) != 0 {
		t.Fatal("a claimed orphan is listed")
	}
	st.release()
	if o := orphanAt(st, "old-a"); o == nil || o.Draft != "claimed" {
		t.Fatalf("a stale claim was not put back: %+v", o)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the stale claim file is still there")
	}
}

// Claude Code's trust dialog draws the rule and the ❯ a box has; it never
// gets a restore.
func TestRestoreNeverIntoTheTrustDialog(t *testing.T) {
	r := newRestoreRig(t, quickClaude(), "conv-a")
	seedOrphan(t, r.store, "old-a", "claude", "conv-a", "keep me")
	text := strings.TrimSuffix(string(readFixture(t, "2.1.282/screens/trust-dialog.txt")), "\n")
	e := []byte("\x1b[H\x1b[2J" + strings.ReplaceAll(text, "\n", "\r\n"))
	r.resize(120, 40)
	r.write(e)
	r.ticks(5)
	if got := r.pasted(); got != "" {
		t.Fatalf("pasted into the trust dialog: %q", got)
	}
}

// Only this agent's draft, from this folder and this conversation, with
// tries left, and only with bracketed paste on.
func TestRestoreOnlyTheRightDraft(t *testing.T) {
	for name, c := range map[string]struct {
		agent, session, cwd string
		tries               int
		noPaste             bool
	}{
		"another agent":       {agent: "codex", session: "conv-a", cwd: "/work"},
		"another folder":      {agent: "claude", session: "conv-a", cwd: "/elsewhere"},
		"another session":     {agent: "claude", session: "conv-b", cwd: "/work"},
		"no session":          {agent: "claude", session: "", cwd: "/work"},
		"tries used up":       {agent: "claude", session: "conv-a", cwd: "/work", tries: maxRestoreTries},
		"no bracketed paste":  {agent: "claude", session: "conv-a", cwd: "/work", noPaste: true},
		"the right one works": {agent: "claude", session: "conv-a", cwd: "/work"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRestoreRig(t, quickClaude(), "conv-a")
			o := seedOrphan(t, r.store, "old-a", c.agent, c.session, "keep me")
			o.Cwd, o.RestoreTries = c.cwd, c.tries
			r.store.write(o)
			if c.noPaste {
				r.write([]byte("\x1b[?2004l"))
			}
			r.draw("")
			r.ticks(4)
			want := ""
			if name == "the right one works" {
				want = "\x1b[200~keep me\x1b[201~"
			}
			if got := r.pasted(); got != want {
				t.Fatalf("pasted %q, want %q", got, want)
			}
		})
	}
}

// A draft the box would change (Claude Code turns a tab into spaces) goes
// to the clipboard instead, and the line under the box says so; with no
// clipboard, it names unsent restore. The orphan stays.
func TestRestoreTabsGoToTheClipboard(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	r := newRestoreRig(t, quickClaude(), "conv-a")
	seedOrphan(t, r.store, "old-a", "claude", "conv-a", "col\tcol")
	r.draw("")
	r.ticks(4)
	if got := r.pasted(); got != "" {
		t.Fatalf("pasted %q", got)
	}
	if o := orphanAt(r.store, "old-a"); o == nil || o.RestoreTries != 1 {
		t.Fatalf("orphan %+v", o)
	}
	// The line under the box is cut to the width, so it never wraps onto
	// the agent's next row; what to do comes first. The line after exit is
	// whole.
	got := r.term.String()
	line, ok := strings.CutPrefix(got, "\x1b7\x1b[")
	if _, line, _ = strings.Cut(line, "\x1b[2K"); !ok || !strings.HasSuffix(line, "\x1b8") {
		t.Fatalf("line under the box %q", got)
	}
	line = strings.TrimSuffix(line, "\x1b8")
	if !strings.HasPrefix(line, "unsent: `unsent restore` copies your draft from") || utf8.RuneCountInString(line) > 99 {
		t.Fatalf("line under the box %q, %d wide at 100 columns", line, utf8.RuneCountInString(line))
	}
	if l := r.restoreLines(); len(l) != 1 || !strings.HasSuffix(l[0], "has tabs, which Claude Code's box turns into spaces") {
		t.Fatalf("lines after exit %q", l)
	}
}

// An agent on the main screen draws below whatever the terminal held, so
// the shadow screen's rows are not the terminal's: no line under the box,
// where it could land on the draft; the line after exit says it.
func TestRestoreNoteOnlyOnTheAlternateScreen(t *testing.T) {
	r := newRestoreRig(t, quickClaude(), "conv-a")
	r.write([]byte("\x1b[?1049l"))
	seedOrphan(t, r.store, "old-a", "claude", "conv-a", "back in the box")
	r.draw("")
	r.ticks(3)
	r.draw("back in the box")
	r.ticks(1)
	if !inHistory(r.store, "back in the box") {
		t.Fatal("not read back")
	}
	time.Sleep(60 * time.Millisecond)
	r.out.tick()
	if got := r.term.String(); got != "" {
		t.Fatalf("drew %q on the main screen", got)
	}
	if got := r.restoreLines(); len(got) != 1 || !strings.Contains(got[0], "put back your draft") {
		t.Fatalf("lines after exit %q", got)
	}
}

// /resume <id> switches the conversation with the box in view the whole
// time: the keys that asked for it do not cancel the restore, a key typed
// after the switch does.
func TestRestoreAfterResumeWithAnID(t *testing.T) {
	for _, after := range []string{"", "\x1b[A"} {
		t.Run(fmt.Sprintf("key after %q", after), func(t *testing.T) {
			r := newRestoreRig(t, quickClaude(), "conv-z")
			seedOrphan(t, r.store, "old-a", "claude", "conv-a", "keep me")
			r.draw("")
			r.ticks(5)
			r.input([]byte("/resume conv-a"))
			r.draw("/resume conv-a")
			r.ticks(1)
			r.input([]byte("\r"))
			*r.id = "conv-a"
			r.draw("")
			r.ticks(1)
			if after != "" {
				r.input([]byte(after))
			}
			r.ticks(4)
			want := "\x1b[200~keep me\x1b[201~"
			if after != "" {
				want = ""
			}
			if got := r.pasted(); got != want {
				t.Fatalf("pasted %q, want %q", got, want)
			}
		})
	}
}

// No paste before the settle delay, however many empty reads; the three
// empty reads count only after it.
func TestRestoreWaitsTheSettleDelay(t *testing.T) {
	prof := quickClaude()
	prof.restore.settle = 300 * time.Millisecond
	r := newRestoreRig(t, prof, "conv-a")
	seedOrphan(t, r.store, "old-a", "claude", "conv-a", "keep me")
	r.draw("")
	r.ticks(4)
	if got := r.pasted(); got != "" {
		t.Fatalf("pasted inside the settle delay: %q", got)
	}
	time.Sleep(350 * time.Millisecond)
	r.ticks(2)
	if got := r.pasted(); got != "" {
		t.Fatalf("pasted after two empty reads past the delay: %q", got)
	}
	r.ticks(1)
	if got := r.pasted(); got != "\x1b[200~keep me\x1b[201~" {
		t.Fatalf("pasted %q", got)
	}
}

// The measured settle delay keeps its margin (docs/research/claude.md
// section 11).
func TestClaudeSettleKeepsItsMargin(t *testing.T) {
	if claude.restore.settle < time.Second || claude.restore.empties < 3 {
		t.Fatalf("Claude Code's settle %v and %d empty reads", claude.restore.settle, claude.restore.empties)
	}
}

// Keys on their way to the agent hold ptyMu: a restore that finds it held
// writes nothing, puts the orphan back untouched, and tries again at the
// next tick.
func TestRestoreWaitsForKeysInFlight(t *testing.T) {
	r := newRestoreRig(t, quickClaude(), "conv-a")
	seedOrphan(t, r.store, "old-a", "claude", "conv-a", "keep me")
	r.draw("")
	r.ptyMu.Lock()
	r.ticks(4)
	r.sentMu.Lock()
	sent := string(r.sent)
	r.sentMu.Unlock()
	o := orphanAt(r.store, "old-a")
	r.ptyMu.Unlock()
	if sent != "" || o == nil || o.RestoreTries != 0 || r.rs.done {
		t.Fatalf("with keys in flight: pasted %q, orphan %+v, done %v", sent, o, r.rs.done)
	}
	r.ticks(1)
	if got := r.pasted(); got != "\x1b[200~keep me\x1b[201~" {
		t.Fatalf("pasted %q", got)
	}
}

// A key typed while the paste is being written follows it, never lands
// inside it.
func TestRestoreKeyFollowsThePaste(t *testing.T) {
	r := newRestoreRig(t, quickClaude(), "conv-a")
	release := make(chan struct{})
	write := r.toAgent
	r.toAgent = func(b []byte) {
		<-release
		write(b)
	}
	seedOrphan(t, r.store, "old-a", "claude", "conv-a", "keep me")
	r.draw("")
	r.ticks(3)
	done := make(chan struct{})
	go func() {
		r.forward([]byte("x"), agentWriter{r})
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	close(release)
	<-done
	if got := r.pasted(); got != "\x1b[200~keep me\x1b[201~x" {
		t.Fatalf("agent got %q", got)
	}
}

// UNSENT_NOTICE=0 draws no line under the box and none after exit; the
// restore still runs.
func TestRestoreNoticeOff(t *testing.T) {
	t.Setenv("UNSENT_NOTICE", "0")
	r := newRestoreRig(t, quickClaude(), "conv-a")
	seedOrphan(t, r.store, "old-a", "claude", "conv-a", "quietly back")
	r.draw("")
	r.ticks(3)
	r.draw("quietly back")
	r.ticks(1)
	if orphanAt(r.store, "old-a") != nil || !inHistory(r.store, "quietly back") {
		t.Fatal("no restore with the notices off")
	}
	if got := r.term.String(); got != "" {
		t.Fatalf("drew %q", got)
	}
	if got := r.restoreLines(); len(got) != 0 {
		t.Fatalf("lines after exit %q", got)
	}
}

// A line break the read back shows as a space at a full row is the
// reader's known limit, and still counts as read back.
func TestRestoreReadBackAllowsAJoinAtAFullRow(t *testing.T) {
	r := newRestoreRig(t, quickClaude(), "conv-a")
	o := seedOrphan(t, r.store, "old-a", "claude", "conv-a", "aaaa bbbb\ncccc")
	claim, err := r.store.claim(o, r.rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.rs.inj = &injection{orphan: o, claim: claim, text: o.Draft, at: time.Now()}
	r.verifyRestore("aaaa bbbb cccc dddd", 10)
	if r.rs.inj == nil {
		t.Fatal("a different draft counted as read back")
	}
	r.verifyRestore("aaaa bbbb cccc", 10)
	if r.rs.inj != nil || !inHistory(r.store, "aaaa bbbb\ncccc") {
		t.Fatal("the join at a full row did not count as read back")
	}
}

// An exit before the read back puts the orphan back.
func TestRestoreExitBeforeTheReadBack(t *testing.T) {
	r := newRestoreRig(t, quickClaude(), "conv-a")
	seedOrphan(t, r.store, "old-a", "claude", "conv-a", "keep me")
	r.draw("")
	r.ticks(3)
	r.endRestore()
	if o := orphanAt(r.store, "old-a"); o == nil || o.RestoreTries != 1 {
		t.Fatalf("orphan %+v", o)
	}
}

func TestClaudeChat(t *testing.T) {
	for _, c := range []struct {
		args []string
		want bool
	}{
		{nil, true},
		{[]string{"--resume", "0b4f7f49-5c6a-4e1c-9a53-1f1f5c1f0a11"}, true},
		{[]string{"-r"}, true},
		{[]string{"--resume"}, true},
		{[]string{"-c"}, true},
		{[]string{"--continue", "--model", "opus"}, true},
		{[]string{"--model=opus", "-c"}, true},
		{[]string{"--resume", "x", "--fork-session"}, true},
		{[]string{"--dangerously-skip-permissions", "--permission-mode", "plan"}, true},
		{[]string{"-d", "api,hooks"}, true},
		{[]string{"fix the build"}, false},
		{[]string{"-c", "fix the build"}, false},
		{[]string{"--model", "opus", "fix the build"}, false},
		{[]string{"--resume", "x", "fix the build"}, false},
		{[]string{"--add-dir", "a", "b"}, false},
		{[]string{"--", "-c"}, false},
		{[]string{"mcp"}, false},
		{[]string{"-p", "hi"}, false},
		{[]string{"--print"}, false},
		{[]string{"--version"}, false},
		{[]string{"--bg"}, false},
		{[]string{"--unknown-flag", "value"}, false},
	} {
		if got := claudeChat(c.args); got != c.want {
			t.Errorf("claudeChat(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}

func TestCleanPaste(t *testing.T) {
	if got := cleanPaste("a\x1b[201~\rb\u009b201~ é\t\n"); got != "a[201~\rb201~ é\t\n" {
		t.Fatalf("%q", got)
	}
}

// The line under the box is drawn between the agent's frames, never inside
// one and never inside an escape sequence or a character.
func TestTermOutDrawsBetweenFrames(t *testing.T) {
	var b lockedBuffer
	o := &termOut{w: &b}
	o.Write([]byte("\x1b[?2026hpart of a frame"))
	o.show("NOTE")
	time.Sleep(60 * time.Millisecond)
	o.tick()
	if strings.Contains(b.String(), "NOTE") {
		t.Fatal("drawn inside a frame")
	}
	o.Write([]byte(" the rest\x1b[?2026l"))
	if got := b.String(); !strings.HasSuffix(got, "\x1b[?2026lNOTE") {
		t.Fatalf("not drawn right after the frame: %q", got)
	}
	for _, tail := range []string{"\x1b[3", "\x1b", "\x1b]0;title", "é"[:1], "\x1b("} {
		var b lockedBuffer
		o := &termOut{w: &b}
		o.Write([]byte("text" + tail))
		o.show("NOTE")
		time.Sleep(60 * time.Millisecond)
		o.tick()
		if strings.Contains(b.String(), "NOTE") {
			t.Errorf("drawn after %q", tail)
		}
	}
	for _, tail := range []string{"\x1b[31m", "\x1b7", "\x1b]0;title\x07", "\x1b]0;t\x1b\\", "é", "\x1b(B", "plain"} {
		var b lockedBuffer
		o := &termOut{w: &b}
		o.Write([]byte("text" + tail))
		o.show("NOTE")
		time.Sleep(60 * time.Millisecond)
		o.tick()
		if !strings.HasSuffix(b.String(), "NOTE") {
			t.Errorf("not drawn after %q", tail)
		}
	}
}

// End to end, the agent's session reopened each way it can be: the draft
// goes back into its box, is not sent, and the line after exit says so.
func TestWrapRestoreOnReopen(t *testing.T) {
	quickRestore(t)
	for _, c := range []struct {
		name       string
		argv       []string
		start, nxt string
		keys       []string
		// idle waits in the first conversation until it had its own try,
		// as a new chat left alone does, before the keys.
		idle bool
	}{
		{"--resume <id>", []string{"claude", "--resume", "conv-a"}, "", "", nil, false},
		{"-c", []string{"claude", "-c"}, "conv-a", "", nil, false},
		{"picker", []string{"claude", "--resume"}, "conv-a", "", []string{"\x1b[B", "\r"}, false},
		{"/resume", []string{"claude"}, "conv-z", "conv-a", []string{"/resume", "\r", "\x1b[B", "\r"}, true},
		{"/resume conv-a", []string{"claude"}, "conv-z", "", []string{"/resume conv-a", "\r"}, true},
		{"a launcher, --as", []string{"--as", "claude", "launcher", "--resume", "conv-a"}, "", "", nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			home, st := wrapWithOrphan(t, "left in conv a\nsecond line")
			t.Setenv("UNSENT_FAKE_SESSION", c.start)
			t.Setenv("UNSENT_FAKE_SESSION_NEXT", c.nxt)
			command := "claude"
			if c.argv[0] == "--as" {
				command = c.argv[2]
			}
			_, st, screen, stderr := runWrappedWith(t, home, command, c.argv, func(w *wrapRun) {
				if c.idle {
					w.waitDecided()
				}
				for _, k := range c.keys {
					w.type_(k)
				}
				waitFor(t, "the restore to read back", func() bool { return inHistory(st, "left in conv a\nsecond line") })
				w.waitShown("unsent: put back your draft from") // the line under the box
				w.type_("\x04")
			})
			if orphanAt(st, "old-a") != nil {
				t.Fatal("the orphan is still there")
			}
			rs := st.orphans()
			if len(rs) != 1 || rs[0].Draft != "left in conv a\nsecond line" || rs[0].AgentSession != "conv-a" {
				t.Fatalf("orphans after exit %+v", rs)
			}
			for _, l := range st.sentLogs() {
				for _, m := range l.messages {
					if strings.Contains(m.Text, "conv a") {
						t.Fatalf("the draft was sent: %q", m.Text)
					}
				}
			}
			if !strings.Contains(screen, "unsent: put back your draft from") || !strings.Contains(stderr, "unsent: put back your draft from") {
				t.Fatalf("no word of the restore: stderr %q", stderr)
			}
		})
	}
}

// End to end on the main screen, as Claude Code's default renderer draws:
// the restore runs, nothing is drawn under the box, and the line after
// exit says it.
func TestWrapRestoreOnTheMainScreen(t *testing.T) {
	quickRestore(t)
	t.Setenv("UNSENT_FAKE_MAIN_SCREEN", "1")
	home, st := wrapWithOrphan(t, "back on the main screen")
	_, st, screen, stderr := runWrappedWith(t, home, "claude", []string{"claude", "--resume", "conv-a"}, func(w *wrapRun) {
		waitFor(t, "the restore to read back", func() bool { return inHistory(st, "back on the main screen") })
		// By the next save the restore's line was drawn, if it were going
		// to be.
		w.waitSave()
		w.type_("\x04")
	})
	if strings.Contains(screen, "unsent: put back") || !strings.Contains(stderr, "unsent: put back your draft from") {
		t.Fatalf("screen %q, stderr %q", screen, stderr)
	}
}

// A launcher started with what could be a prompt gets no restore, as
// claude would: its arguments are read as Claude Code's own.
func TestWrapNoRestoreThroughALauncherWithAPrompt(t *testing.T) {
	quickRestore(t)
	home, _ := wrapWithOrphan(t, "left in conv a")
	t.Setenv("UNSENT_FAKE_SESSION", "conv-a")
	_, st, _, stderr := runWrappedWith(t, home, "launcher", []string{"--as", "claude", "launcher", "fix the build"}, func(w *wrapRun) {
		w.waitDecided()
		w.type_("\x04")
	})
	if o := orphanAt(st, "old-a"); o == nil || o.RestoreTries != 0 || strings.Contains(stderr, "put back") {
		t.Fatalf("restored through a launcher given a prompt: %+v, %q", o, stderr)
	}
}

// A draft still in the box when the agent exits was not sent, under
// UNSENT_ON_SEND=delete too: a submit answered by a menu, then the box
// again, and a menu opened with no submit key, both leave it an orphan.
func TestWrapDraftInTheBoxAtExitIsKept(t *testing.T) {
	for name, keys := range map[string][]string{
		"a menu after a submit, then the box": {"/tasks", "\r", "\x14", "keep me", "\x04"},
		"a menu with no submit":               {"keep me", "\x14", "\x04"},
	} {
		t.Run(name, func(t *testing.T) {
			claudeConfig(t)
			t.Setenv("UNSENT_FAKE_SESSION", "conv-a")
			t.Setenv("UNSENT_ON_SEND", "delete")
			_, st, _, _ := runWrappedOut(t, "claude", []string{"claude"}, func(type_ func(string)) {
				for _, k := range keys {
					type_(k)
				}
			})
			if rs := st.orphans(); len(rs) != 1 || rs[0].Draft != "keep me" {
				t.Fatalf("orphans after exit %+v", rs)
			}
		})
	}
}

// End to end, a conversation other than the draft's: a new chat, /clear, a
// fork, and a start with a prompt get no paste, and the notice after exit
// names the draft.
func TestWrapNoRestoreOutsideTheSession(t *testing.T) {
	quickRestore(t)
	for _, c := range noRestoreCases {
		t.Run(c.name, func(t *testing.T) {
			pasted, stderr := runNoRestore(t, c)
			if pasted {
				t.Fatal("the draft was pasted")
			}
			if !strings.Contains(stderr, "unsent: recovered a draft from") {
				t.Fatalf("no notice after exit: %q", stderr)
			}
		})
	}
}

var noRestoreCases = []struct {
	name, start string
	argv, keys  []string
}{
	{"new chat", "conv-new", []string{"claude"}, nil},
	{"/clear", "conv-z", []string{"claude"}, []string{"/clear", "\r"}},
	{"--fork-session", "", []string{"claude", "--resume", "conv-a", "--fork-session"}, nil},
	{"a prompt argument", "conv-a", []string{"claude", "fix the build"}, nil},
}

// runNoRestore runs one of noRestoreCases until the session the agent ends
// in had its one try at a restore, or cannot have one, and reports whether
// the draft was pasted.
func runNoRestore(t *testing.T, c struct {
	name, start string
	argv, keys  []string
}) (pasted bool, stderr string) {
	t.Helper()
	home, _ := wrapWithOrphan(t, "left in conv a")
	t.Setenv("UNSENT_FAKE_SESSION", c.start)
	_, st, screen, stderr := runWrappedWith(t, home, "claude", c.argv, func(w *wrapRun) {
		for _, k := range c.keys {
			w.type_(k)
		}
		w.waitDecided()
		w.type_("\x04")
	})
	o := orphanAt(st, "old-a")
	return o == nil || o.RestoreTries != 0 || strings.Contains(screen, "left in conv a") || strings.Contains(stderr, "put back"), stderr
}

// A check that cannot fail is not a check: with a session match that is
// always true, the new chat must get the paste.
func TestRestoreNewChatTestCatchesAnAlwaysTrueMatch(t *testing.T) {
	quickRestore(t)
	old := sessionMatch
	sessionMatch = func(string, string) bool { return true }
	t.Cleanup(func() { sessionMatch = old })
	if pasted, _ := runNoRestore(t, noRestoreCases[0]); !pasted {
		t.Fatal("the new-chat test would stay green with a session match that is always true")
	}
}

// End to end: a draft holding ESC[201~ and a line break goes in whole and
// is never sent.
func TestWrapRestoredDraftNeverSubmits(t *testing.T) {
	quickRestore(t)
	old := verifyWait
	verifyWait = time.Hour
	t.Cleanup(func() { verifyWait = old })
	home, st := wrapWithOrphan(t, "first\x1b[201~\rsecond")
	_, st, screen, _ := runWrappedWith(t, home, "claude", []string{"claude", "--resume", "conv-a"}, func(w *wrapRun) {
		waitFor(t, "the paste", func() bool { return orphanAt(st, "old-a") == nil })
		// The paste drawn in the box, and a save that read it.
		w.waitShown("second")
		w.waitSave()
		w.type_("\x04")
	})
	if logs := st.sentLogs(); len(logs) != 0 {
		t.Fatalf("sent: %+v", logs[0].messages)
	}
	if !strings.Contains(screen, "first[201~") || !strings.Contains(screen, "second") {
		t.Fatalf("the draft is not in the box: %q", screen)
	}
	var drafts []string
	for _, r := range st.orphans() {
		drafts = append(drafts, r.Draft)
	}
	if !slices.Contains(drafts, "first[201~\nsecond") || !slices.Contains(drafts, "first\x1b[201~\rsecond") {
		t.Fatalf("drafts after exit %q: the box's and the unverified orphan", drafts)
	}
}

// UNSENT_NOTICE=0, end to end: no notice before start or after exit, no
// line under the box, and the restore still runs.
func TestWrapRestoreWithNoticesOff(t *testing.T) {
	quickRestore(t)
	t.Setenv("UNSENT_NOTICE", "0")
	home, st := wrapWithOrphan(t, "quietly back")
	_, st, screen, stderr := runWrappedIn(t, home, "claude", []string{"claude", "--resume", "conv-a"}, func(type_ func(string)) {
		waitFor(t, "the restore to read back", func() bool { return inHistory(st, "quietly back") })
		type_("\x04")
	})
	if stderr != "" || strings.Contains(screen, "unsent:") {
		t.Fatalf("stderr %q, screen %q", stderr, screen)
	}
	if rs := st.orphans(); len(rs) != 1 || rs[0].Draft != "quietly back" {
		t.Fatalf("orphans %+v", rs)
	}
}

// /exit and Enter end the agent without an empty box: the draft was sent,
// so it is not left behind to be put back into the box next time.
func TestWrapExitCommandIsSent(t *testing.T) {
	claudeConfig(t)
	t.Setenv("UNSENT_FAKE_SESSION", "conv-a")
	_, st, _, _ := runWrappedOut(t, "claude", []string{"claude"}, func(type_ func(string)) {
		type_("/exit")
		type_("\r")
	})
	if rs := st.orphans(); len(rs) != 0 {
		t.Fatalf("left behind: %q", rs[0].Draft)
	}
	l, err := readSent(st.sentPath("claude-conv-a"))
	if err != nil || len(l.messages) != 1 || l.messages[0].Text != "/exit" {
		t.Fatalf("sent log %+v, %v", l, err)
	}
}

// quickRestore takes Claude Code's settle delay out for the test.
func quickRestore(t *testing.T) {
	t.Helper()
	claudeConfig(t)
	old := claude.restore
	claude.restore = quickClaude().restore
	t.Cleanup(func() { claude.restore = old })
}

// wrapWithOrphan makes a state folder holding one draft a dead Claude Code
// session left in conversation conv-a, in a scratch folder that is now the
// working directory.
func wrapWithOrphan(t *testing.T, draft string) (string, *store) {
	t.Helper()
	t.Chdir(t.TempDir())
	cwd, _ := os.Getwd()
	home := t.TempDir()
	st := &store{dir: home}
	for _, sub := range []string{"drafts", "history", "sent"} {
		os.MkdirAll(filepath.Join(home, sub), 0o700)
	}
	r := newRecord([]string{"claude"}, realPath(cwd))
	r.ID, r.Agent, r.AgentSession, r.Draft, r.Ended = "old-a", "claude", "conv-a", draft, time.Now()
	r.Updated = time.Now().Add(-time.Hour)
	if err := st.write(r); err != nil {
		t.Fatal(err)
	}
	return home, st
}

// waitFor waits up to 10 s for cond.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !cond(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}
