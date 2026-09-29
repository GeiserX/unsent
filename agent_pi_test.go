package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// pi's profile on real 0.87.1 captures (testdata/pi/0.87.1): the replays
// against pi's own editor copies, what its reader must never take for a
// draft, what it must go red on when it is mutated, its keys, pastes and
// wrap, and what it does at a closed window, a send and Ctrl+Z.

var piCaptures = captureSet{folder: filepath.Join("testdata", "pi", "0.87.1"), command: "pi", shows: piShows}

// piShows reports whether got is pi's draft truth as far as pi's screen
// shows it: byte for byte, but for spaces at the end of a line, which pi
// draws as the blank cells after it (a word deleted with Ctrl+W at the end
// of a line; the draft keeps those of lines out of sight, which the
// stitcher knew from before), and the one line break byteExact allows. Tolerated by pi's
// measured drawing, not by the profile's model, so a wrong model cannot
// excuse itself.
func piShows(got, truth string) bool {
	trim := func(s string) string {
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			lines[i] = strings.TrimRight(l, " ")
		}
		return strings.Join(lines, "\n")
	}
	return got == truth || byteExact(trim(got), trim(truth), 119, piWrap{})
}

// replayPi replays name.rec through a session with prof and returns the
// first Ctrl+G whose editor copy the saved draft differs from.
func replayPi(t *testing.T, prof *profile, name string, recalled ...int) error {
	t.Helper()
	_, err := replayPiSession(t, prof, name, recalled...)
	return err
}

func replayPiSession(t *testing.T, prof *profile, name string, recalled ...int) (*session, error) {
	t.Helper()
	return replayCaptureSession(t, prof, piCaptures, name, recalled...)
}

// Recorded with keys and output together (testdata/pi/README.md):
//   - typed, multiline, accents, tall, deletes: the box, its new-line keys,
//     accents, emoji and a wide character at the edge, 45 rows in a box of
//     12 edited out of sight, every delete key;
//   - wrap: a word that ends at the edge with a space after it (pi moves
//     it to the next row), a word longer than a row, a row that starts
//     with the space after one, a line that fills its row before a blank
//     line, and a placeholder moved whole to the next row;
//   - pastes: pastes inline and as placeholders, the first placeholder
//     deleted, which renumbers the others, and a paste sent with CR;
//   - scroll-steps: 20 rows, Up one key at a time into the rows out of
//     sight above, an edit there, and Down one key at a time back;
//   - edge-deletes: Ctrl+W, Backspace, Alt+Backspace, Ctrl+U, Ctrl+K,
//     Delete, Ctrl+D, Alt+D, Alt+Delete, Shift+Delete and Shift+Backspace
//     on the last row of a box at its cap, where only the keys tell a
//     deletion from rows scrolled out of sight below;
//   - paste-renumber: two placeholders of one size, one deleted, and a
//     third of that size pasted after: first the first one deleted (the
//     second becomes #1), then the second;
//   - early-paste, early-paste-resume: a long paste on the first frame that
//     shows the box, in a new chat and in pi -c, sent with LF and with CR,
//     then a paste with a tab and one of a path after a word;
//   - ctrlc-history: Ctrl+C, history browsed from an empty box and from a
//     draft, which pi holds aside, and Ctrl+- bringing a cleared draft back;
//   - submit, identity: drafts typed after a failed send and during pi's
//     retries, with its status in the top rule;
//   - suspend, suspend-direct: Ctrl+Z through unsent and to pi alone;
//   - send-paste: a send with a placeholder, then Up bringing it back from
//     history (the first Ctrl+G).
var piReplays = []struct {
	name     string
	recalled []int
}{{"typed", nil}, {"multiline", nil}, {"accents", nil}, {"wrap", nil}, {"tall", nil}, {"pastes", nil},
	{"scroll-steps", nil}, {"paste-renumber", nil}, {"deletes", nil}, {"edge-deletes", nil}, {"ctrlc-history", nil}, {"submit", nil}, {"suspend", nil},
	{"suspend-direct", nil}, {"identity", nil}, {"send-paste", []int{1}}, {"early-paste", nil},
	{"early-paste-resume", nil}}

func TestReplayPi(t *testing.T) {
	for _, r := range piReplays {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			if err := replayPi(t, &pi, r.name, r.recalled...); err != nil {
				t.Fatal(err)
			}
		})
		// Again with a save every 0.4 s of recorded time, as a live session
		// saves: keys typed in one save, such as ctrlc-history's "typed
		// draft" and the Up right after it, must still be read. Not
		// deletes, which presses Ctrl+U and Ctrl+G at once: the draft read
		// before the Ctrl+U is still the saved one as the editor opens,
		// which holds more than pi's copy, never less.
		if r.name == "deletes" {
			continue
		}
		t.Run(r.name+" save every "+saveInterval.String(), func(t *testing.T) {
			t.Parallel()
			set := piCaptures
			set.tick = saveInterval
			if _, err := replayCaptureSession(t, &pi, set, r.name, r.recalled...); err != nil {
				t.Fatal(err)
			}
		})
	}
	// Every capture with editor copies is replayed: a new one needs a line
	// in piReplays.
	replayed := map[string]bool{}
	for _, r := range piReplays {
		replayed[r.name] = true
	}
	copies, _ := filepath.Glob(filepath.Join(piCaptures.folder, "*.editor-1.txt"))
	var missing []string
	for _, c := range copies {
		if name := strings.TrimSuffix(filepath.Base(c), ".editor-1.txt"); !replayed[name] {
			missing = append(missing, name)
		}
	}
	if len(copies) == 0 || len(missing) > 0 {
		t.Fatalf("%d captures with editor copies, not replayed: %s", len(copies), strings.Join(missing, ", "))
	}
}

// firstRedPiReplay replays piReplays with prof in order and returns the
// name of the first that goes red, or "".
func firstRedPiReplay(t *testing.T, prof *profile) string {
	t.Helper()
	for _, r := range piReplays {
		if replayPi(t, prof, r.name, r.recalled...) != nil {
			return r.name
		}
	}
	return ""
}

// A check that cannot fail is not a check: with pi's wrap model swapped for
// character wrap, Claude Code's word wrap or Codex's (with its end row),
// some replay must go red.
func TestReplayPiCatchesAWrongWrapModel(t *testing.T) {
	for _, m := range []struct {
		name string
		rule unwrapRule
	}{{"character wrap", charWrap{}}, {"Claude Code's word wrap", wordWrap{}}, {"Codex's word wrap", wordWrap{endRow: true}}} {
		p := pi
		p.unwrap = m.rule
		red := firstRedPiReplay(t, &p)
		if red == "" {
			t.Errorf("with %s every pi replay stayed green", m.name)
		}
		t.Logf("with %s, red: %s", m.name, red)
	}
}

// After Ctrl+G pi puts the text behind each placeholder in the box itself,
// and a long one scrolls the box: the draft must stay whole. Every draft a
// record's Ctrl+C clears must be what pi held at the Ctrl+G before it, less
// the one line break at its end pi drops as it reads the file back, as
// nothing edits it in between in early-paste.
func TestPiRoundTripKeepsTheDraft(t *testing.T) {
	s, err := replayPiSession(t, &pi, "early-paste")
	if err != nil {
		t.Fatal(err)
	}
	copies, _ := filepath.Glob(filepath.Join(piCaptures.folder, "early-paste.editor-*.txt"))
	var want []string
	for _, c := range copies {
		b, _ := os.ReadFile(c)
		want = append(want, string(b))
	}
	n := 0
	for _, h := range s.store.load(true) {
		if h.ID == s.rec.ID {
			continue
		}
		n++
		if !slices.ContainsFunc(want, func(w string) bool { return piShows(h.Draft, strings.TrimSuffix(w, "\n")) }) {
			t.Errorf("cleared a draft pi never held, %d bytes: %.80q", len(h.Draft), h.Draft)
		}
	}
	if n < len(want) {
		t.Fatalf("%d drafts in history for %d cleared", n, len(want))
	}
}

// piNotABox are frames that show no box to save into: pi's pickers and
// dialogs, drawn between the box's own rules in place of the box. Each must
// read as no box, so the draft is kept.
var piNotABox = []struct{ record, shows string }{
	{"dialogs", "No matching models"},                     // /model and Ctrl+L
	{"dialogs", "Type to search · Enter/Space to change"}, // /settings
	{"dialogs", "Select authentication method"},           // /login
	{"dialogs", "No sessions in current folder"},          // /resume, no sessions
	{"dialogs", "Session Tree"},                           // /tree
	{"dialogs", "Project trust"},                          // /trust
	{"resume-picker", "Resume Session (Current Folder)"},  // pi -r
	{"slash-resume", "Resume Session (Current Folder)"},   // /resume
}

// piMenus are frames with a menu under the box, or help printed above it:
// only what was typed is the draft.
var piMenus = []struct{ record, shows, draft string }{
	{"dialogs", "(1/25)", "/"},
	{"dialogs", "!echo dummy", "!echo dummy"},
}

// piTyped is every draft each record shows in its box, in the order typed:
// no other text may ever be read from its frames.
var piTyped = map[string][]string{
	"dialogs": {"/", "@", "/settings", "/hotkeys", "/login", "/resume", "/model", "/session", "!echo dummy",
		"/tree", "/trust"},
	"resume-picker": {"/session"},
	"slash-resume":  {"/session", "/resume"},
	"slash-new":     {"/new", "/session", "prompt in the new session, dummy"},
	"identity": {"typed but not submitted, dummy", "dummy prompt that must fail on the dead port",
		"a draft typed during the retries", "/session", "second prompt for the dead port, dummy"},
	"ctrlc-history": {"a draft to clear with ctrl c\nits second line", "first sent prompt, dummy",
		"second sent prompt, dummy", "typed draft"},
}

// piScreensCheck returns the first way piBox misreads the frames above, or
// nil.
func piScreensCheck(t *testing.T) error {
	t.Helper()
	frames := map[string][]*screen{}
	get := func(record string) []*screen {
		if frames[record] == nil {
			frames[record] = captureFrames(t, piCaptures.folder, record)
		}
		return frames[record]
	}
	for _, c := range piNotABox {
		f := firstFrame(t, get(c.record), c.shows)
		if v, ok := piBox(f); ok {
			return fmt.Errorf("%s, the frame showing %q: read as a box %+v", c.record, c.shows, v)
		}
	}
	// Synthesized from a real frame: the draft with its cursor gone, which
	// is how pi's rules around something that is not its editor look, and
	// with one character in another colour, as a picker's rows are.
	typed := lastFrame(t, get("typed"), "hello from the rig")
	y := piAnchor(t, typed, "hello from the rig")
	for name, restyle := range map[string]func(*screenRow){
		"no cursor":       func(r *screenRow) { clear(r.look) },
		"a second colour": func(r *screenRow) { r.look[0].fg = 1<<24 | 109 },
	} {
		f := cloneScreen(typed)
		restyle(&f.rows[y])
		if v, ok := piBox(f); ok {
			return fmt.Errorf("the box with %s (synthesized): read as a box %+v", name, v)
		}
	}
	for _, c := range piMenus {
		f := firstFrame(t, get(c.record), c.shows)
		v, ok := piBox(f)
		var st stitcher
		if got := st.update(v, pi.unwrap); !ok || got != c.draft {
			return fmt.Errorf("%s, the frame showing %q: draft %q (box %v), want %q", c.record, c.shows, got, ok, c.draft)
		}
	}
	for record, typed := range piTyped {
		for _, f := range get(record) {
			v, ok := piBox(f)
			if !ok || v.empty {
				continue
			}
			text := strings.Join(v.rows, "\n")
			if !slices.ContainsFunc(typed, func(d string) bool { return strings.HasPrefix(d, text) }) {
				return fmt.Errorf("%s: read %q from a frame, which was never typed there:\n%s", record, text, f)
			}
		}
	}
	return nil
}

// firstFrame returns the first frame whose text holds every want. On the
// main screen what pi drew stays on the rows above once it draws the box
// again, so the last frame showing a picker's text can be the box's.
func firstFrame(t *testing.T, frames []*screen, want ...string) *screen {
	t.Helper()
	for _, f := range frames {
		text, all := f.String(), true
		for _, w := range want {
			all = all && strings.Contains(text, w)
		}
		if all {
			return f
		}
	}
	t.Fatalf("no frame shows %q", want)
	return nil
}

// piAnchor is the row of s that starts with text.
func piAnchor(t *testing.T, s *screen, text string) int {
	t.Helper()
	for y, r := range s.rows {
		if strings.HasPrefix(r.text(), text) {
			return y
		}
	}
	t.Fatalf("no row starts with %q", text)
	return -1
}

func TestPiNegativeScreens(t *testing.T) {
	if err := piScreensCheck(t); err != nil {
		t.Fatal(err)
	}
}

// The frames of the empty box, a draft and a scrolled box read as pi drew
// them: the cursor's row, the empty box, and the rows the labels say are
// out of sight.
func TestPiBoxFrames(t *testing.T) {
	empty := firstFrame(t, captureFrames(t, piCaptures.folder, "typed"), "(auto)")
	if v, ok := piBox(empty); !ok || !v.empty {
		t.Fatalf("the first frame's box: %+v, %v", v, ok)
	}
	tall := captureFrames(t, piCaptures.folder, "tall")
	f := lastFrame(t, tall, "↑ 4 more", "↓ 29 more")
	v, ok := piBox(f)
	if !ok || len(v.rows) != 12 || !v.capped || v.cursor != 0 || v.width != 119 ||
		v.rows[0] != "tall row 05 dummy words here EDITED" {
		t.Fatalf("the box scrolled up: %+v, %v", v, ok)
	}
	retry := lastFrame(t, captureFrames(t, piCaptures.folder, "identity"), "Retrying", "a draft typed during")
	if v, ok := piBox(retry); !ok || v.rows[0] != "a draft typed during the retries" {
		t.Fatalf("the box under pi's working status: %+v, %v", v, ok)
	}
}

// A check that cannot fail is not a check: each part of the reader,
// mutated, turns a replay or a negative screen red. One red is the proof,
// so each mutant stops at the first: the screens, then the replays.
func TestPiReaderMutations(t *testing.T) {
	good := piLayout
	t.Cleanup(func() { piLayout = good })
	for _, m := range []struct {
		name   string
		mutate func(*piBoxLayout)
	}{
		{"cap a row lower", func(b *piBoxLayout) { b.cap = func(rows int) int { return max(5, rows*3/10) - 1 } }},
		{"wrap at the width", func(b *piBoxLayout) { b.margin = 0 }},
		{"wrap at the width minus 2", func(b *piBoxLayout) { b.margin = 2 }},
		{"no scroll labels", func(b *piBoxLayout) { b.labels = false }},
		{"no working status", func(b *piBoxLayout) { b.status = false }},
		{"no cursor check", func(b *piBoxLayout) { b.cursor = false }},
		{"no colour check", func(b *piBoxLayout) { b.uniform = false }},
	} {
		t.Run(m.name, func(t *testing.T) {
			bad := good
			m.mutate(&bad)
			piLayout = bad
			defer func() { piLayout = good }()
			if err := piScreensCheck(t); err != nil {
				t.Logf("red: screens (%v)", err)
				return
			}
			if red := firstRedPiReplay(t, &pi); red != "" {
				t.Logf("red: %s", red)
				return
			}
			t.Fatal("every pi replay and screen stayed green")
		})
	}
}

// The delete, recall and submit keys are what the captures need: without
// the keys that delete, text deleted on the last row of a box at its cap
// reads as scrolled out of sight below; without Up as a
// recall key a history entry is saved as a draft, and with history browsed
// only from an empty box so are the entry Up brings into a draft and the
// one it brings back after the Esc that cancelled a send; without Enter or
// Alt+Enter as a submit key, the sends go to history, not the sent log.
func TestPiKeyMutations(t *testing.T) {
	for _, m := range []struct {
		name string
		drop func(*keyset)
	}{
		{"keys that delete a word or more", func(k *keyset) { k.many = nil }},
		{"keys that delete one character", func(k *keyset) { k.one = nil }},
		{"keys that delete after the cursor", func(k *keyset) { k.ahead = nil }},
	} {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			p := pi
			m.drop(&p.keys)
			if replayPi(t, &p, "edge-deletes") == nil {
				t.Fatal("edge-deletes stayed green")
			}
		})
	}
	t.Run("recall keys", func(t *testing.T) {
		t.Parallel()
		p := pi
		p.keys.recall = nil
		if piBrowseCheck(t, &p) == nil || replayPi(t, &p, "send-paste", 1) == nil {
			t.Fatal("a history entry brought back with Up was not saved as a draft")
		}
	})
	t.Run("browsing from a draft", func(t *testing.T) {
		t.Parallel()
		p := pi
		p.keys.browses = false
		if piBrowseCheck(t, &p) == nil || replayPi(t, &p, "send-paste", 1) == nil {
			t.Fatal("the entry Up brought into a draft, or after an Esc, was not saved as the draft")
		}
	})
	for _, drop := range []int{0, 1} {
		t.Run(fmt.Sprintf("submit key %d", drop), func(t *testing.T) {
			t.Parallel()
			p := pi
			p.keys.submit = slices.Delete(slices.Clone(pi.keys.submit), drop, drop+1)
			if piSendCheck(t, &p) == nil {
				t.Fatal("every send stayed in the sent log without that submit key")
			}
		})
	}
}

// piBrowseCheck replays ctrlc-history, where Up in a typed draft moves the
// cursor to its start, and Up again brings the newest entry of pi's
// history while pi holds the draft aside, until Down puts it back: the
// entry, a prompt sent earlier, must never be the saved draft after it
// was sent.
func piBrowseCheck(t *testing.T, prof *profile) error {
	t.Helper()
	set := piCaptures
	var typed, sent bool
	var saved error
	const entry = "second sent prompt, dummy"
	set.saved = func(s *session) {
		switch {
		case s.rec.Draft == entry && sent && saved == nil:
			saved = fmt.Errorf("the history entry %q was saved as the draft", entry)
		case s.rec.Draft == entry:
			typed = true // the next empty box is its send
		case s.rec.Draft == "" && typed:
			sent = true
		}
	}
	if _, err := replayCaptureSession(t, prof, set, "ctrlc-history"); err != nil {
		return err
	}
	if !sent && saved == nil {
		return fmt.Errorf("%q was never typed and sent", entry)
	}
	return saved
}

// piSendCheck replays submit (no model, so each send fails at once),
// identity (a dead port, so pi retries) and send-paste (a send with a
// placeholder): every prompt sent with Enter, Alt+Enter or Esc then Enter
// must be in the sent log, pastes expanded, and not in history, and the
// drafts cleared with Ctrl+C in history and not in the sent log.
func piSendCheck(t *testing.T, prof *profile) error {
	t.Helper()
	for _, c := range []struct {
		record        string
		sent, cleared []string
		recalled      []int
	}{
		{"submit", []string{"dummy prompt that must fail with no model", "submitted with alt enter, dummy",
			"submitted with esc then enter, dummy"},
			[]string{"typed but not submitted, dummy", "a draft typed after the failed submit",
				"submitted with ctrl enter, dummyctrl m, dummy"}, nil},
		{"identity", []string{"dummy prompt that must fail on the dead port", "/session",
			"second prompt for the dead port, dummy"},
			[]string{"typed but not submitted, dummy", "a draft typed during the retries"}, nil},
		{"send-paste", []string{piSendPaste()}, nil, []int{1}},
	} {
		s, err := replayPiSession(t, prof, c.record, c.recalled...)
		if err != nil {
			return err
		}
		l, _ := readSent(s.store.sentPath(s.rec.ID))
		var sent []string
		if l != nil {
			for _, m := range l.messages {
				sent = append(sent, m.Text)
			}
		}
		if !slices.Equal(sent, c.sent) {
			return fmt.Errorf("%s: sent log %q, want %q", c.record, sent, c.sent)
		}
		for _, d := range c.sent {
			// In send-paste the sent message comes back from history with Up
			// and goes through Ctrl+G, which makes it pi's draft; the Ctrl+C
			// after it clears that into history.
			if c.recalled == nil && inHistory(s.store, d) {
				return fmt.Errorf("%s: the sent %q is in history too", c.record, d)
			}
		}
		for _, d := range c.cleared {
			if !inHistory(s.store, d) {
				return fmt.Errorf("%s: the cleared %q is not in history", c.record, d)
			}
		}
	}
	return nil
}

// piSendPaste is the message send-paste sends: text around a paste of
// 1,001 characters, which pi shows as "[paste #1 1001 chars]".
func piSendPaste() string {
	b, err := os.ReadFile(filepath.Join(piCaptures.folder, "send-paste.editor-1.txt"))
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestPiSendGoesToTheSentLog(t *testing.T) {
	if err := piSendCheck(t, &pi); err != nil {
		t.Fatal(err)
	}
}

// pi names no session another process can read, so unsent reads none: its
// sent log is one per run, and restore-in-box is off.
func TestPiHasNoSessionAndNoRestore(t *testing.T) {
	if pi.session != nil || pi.restore != nil {
		t.Fatal("pi's profile reads a session id or restores; pi names its session nowhere unsent can read")
	}
	s, err := replayPiSession(t, &pi, "identity")
	if err != nil {
		t.Fatal(err)
	}
	if s.rec.AgentSession != "" {
		t.Fatalf("a session id %q for pi", s.rec.AgentSession)
	}
	if _, err := os.Stat(s.store.sentPath(s.rec.ID)); err != nil {
		t.Fatalf("no sent log for the run: %v", err)
	}
}

// The window closes, or pi is killed, with a draft in the box: pi keeps it
// in no file, and unsent keeps it as an orphan. pi drew nothing after the
// kill or the closed window: the captures end at the draft.
var piCloses = []struct {
	name   string
	closed bool
	draft  string
}{
	{"kill9", false, "kill nine draft zebra\nsecond line of it"},
	{"killsession", true, "kill session draft yak\nsecond line of it"},
}

func piCloseCheck(t *testing.T, prof *profile) error {
	t.Helper()
	for _, c := range piCloses {
		s, err := replayPiSession(t, prof, c.name)
		if err != nil {
			return err
		}
		if c.closed {
			s.closing()
		}
		s.save()
		s.finish()
		var kept []string
		for _, r := range s.store.orphans() {
			kept = append(kept, r.Draft)
		}
		if !slices.Equal(kept, []string{c.draft}) {
			return fmt.Errorf("%s: orphans %q, want %q", c.name, kept, c.draft)
		}
	}
	return nil
}

func TestPiWindowCloseKeepsTheDraft(t *testing.T) {
	if err := piCloseCheck(t, &pi); err != nil {
		t.Fatal(err)
	}
	// A check that can fail: a reader that takes every box for empty loses
	// the drafts.
	empty := pi
	empty.read = func(scr *screen) (view, bool) {
		v, ok := piBox(scr)
		v.rows, v.empty = nil, true
		return v, ok
	}
	if piCloseCheck(t, &empty) == nil {
		t.Error("with every box read empty the close check stayed green")
	}
}

// Ctrl+Z is unsent's, in the form tmux sent it (ESC[122;5u), and unsent
// writes nothing for pi as it suspends it: pi's repaint after the resume
// turns none of its modes on again (suspend), so writing its teardown
// would leave it without the kitty flags and modifyOtherKeys, and
// Shift+Enter would send the box.
func TestPiSuspendPolicy(t *testing.T) {
	if at, end := findKey([]byte("ab\x1b[122;5ucd"), suspendKeys(&pi)); at != 2 || end != 10 {
		t.Fatalf("Ctrl+Z found at %d..%d", at, end)
	}
	if pi.suspended != nil {
		t.Fatalf("pi's suspend output %q: its repaint after a resume sets none of it again", pi.suspended)
	}
	data, err := os.ReadFile(filepath.Join(piCaptures.folder, "suspend.rec"))
	if err != nil {
		t.Fatal(err)
	}
	// From the Ctrl+Z to the next key, a Ctrl+G typed once the box was
	// back: the stop, fg, and pi's repaint.
	var out []byte
	stopped, done := false, false
	eachChunk(t, data, func(_ time.Time, dir byte, chunk []byte) {
		switch {
		case done:
		case dir == 'i' && bytes.Contains(chunk, []byte("\x1b[122;5u")):
			stopped = true
		case dir == 'i' && stopped && bytes.Contains(chunk, []byte("\x1b[103;5u")):
			done = true
		case dir == 'o' && stopped:
			out = append(out, chunk...)
		}
	})
	if !done || !bytes.Contains(out, []byte("second line")) {
		t.Fatal("no repaint of the draft between the Ctrl+Z and the next key")
	}
	for _, mode := range []string{"\x1b[>7u", "\x1b[>4;2m", "\x1b[?2004h"} {
		if bytes.Contains(out, []byte(mode)) {
			t.Fatalf("pi set %q again after the resume; its suspend output can go in the profile", mode)
		}
	}
}

// pi is a profile: `pi` runs through its reader, and setup wraps it.
func TestPiIsAProfile(t *testing.T) {
	if p := profileFor("/opt/homebrew/bin/pi"); p != &pi {
		t.Fatalf("profile for pi: %v", p)
	}
	if !slices.Contains(agentCommands(), "pi") {
		t.Fatalf("setup wraps %q, not pi", agentCommands())
	}
}

// Saving stopped says which pi ran and which one the reader was last
// checked against; `pi --version` prints the bare version.
func TestPiSavingStoppedLine(t *testing.T) {
	s := &session{prof: &pi, version: make(chan string, 1)}
	s.typed.Store(true)
	s.version <- agentVersionOf("0.88.0\n")
	want := "unsent: could not read pi 0.88.0's box this session (last verified 0.87.1), nothing was saved"
	if got := s.exitLines(); !slices.Equal(got, []string{want}) {
		t.Fatalf("exit lines %q, want %q", got, want)
	}
}

// pi's keys, as the captures sent them in tmux with modifyOtherKeys 2 and
// as legacy bytes, each read as the profile says.
func TestPiKeys(t *testing.T) {
	for _, c := range []struct {
		bytes         string
		chars         int64
		ahead         bool
		kind          keyKind
		what          string
		submit, clear bool
	}{
		{bytes: "\x7f", chars: 1, what: "Backspace"},
		{bytes: "\x1b[127;2u", chars: 1, what: "Shift+Backspace"},
		{bytes: "\x1b[3~", chars: 1, ahead: true, what: "Delete"},
		{bytes: "\x1b[100;5u", chars: 1, ahead: true, what: "Ctrl+D"},
		{bytes: "\x1b[119;5u", chars: unlimited, what: "Ctrl+W"},
		{bytes: "\x1b[127;3u", chars: unlimited, what: "Alt+Backspace"},
		{bytes: "\x1b[100;3u", chars: unlimited, ahead: true, what: "Alt+D"},
		{bytes: "\x1b[3;3~", chars: unlimited, ahead: true, what: "Alt+Delete"},
		{bytes: "\x1b[117;5u", chars: unlimited, what: "Ctrl+U"},
		{bytes: "\x1b[107;5u", chars: unlimited, ahead: true, what: "Ctrl+K"},
		{bytes: "\x1b[45;5u", chars: unlimited, ahead: true, what: "Ctrl+-"},
		{bytes: "\x1f", chars: unlimited, ahead: true, what: "Ctrl+- as legacy bytes"},
		{bytes: "\x1b[104;5u", what: "Ctrl+H, which does nothing"},
		{bytes: "\x1b[127;5u", what: "Ctrl+Backspace, which does nothing"},
		{bytes: "\x1b[3;5~", what: "Ctrl+Delete, which does nothing"},
		{bytes: "\r", submit: true, what: "Enter"},
		{bytes: "\x1b[13;3u", submit: true, what: "Alt+Enter"},
		{bytes: "\x1b\r", submit: true, what: "Esc then Enter in one read"},
		{bytes: "\x1b[99;5u", clear: true, what: "Ctrl+C"},
		{bytes: "\x1b[106;5u", what: "Ctrl+J, a new line"},
		{bytes: "\x1b[13;2u", what: "Shift+Enter, a new line"},
		{bytes: "\x1b[13;5u", what: "Ctrl+Enter, which does nothing"},
	} {
		chars, ahead := pi.keys.deletes([]byte(c.bytes))
		kinds, _ := pi.keys.kinds([]byte(c.bytes))
		if chars != c.chars || ahead != c.ahead || slices.Contains(kinds, keySubmit) != c.submit ||
			slices.Contains(kinds, keyClear) != c.clear {
			t.Errorf("%s: deletes %d (ahead %v), kinds %v", c.what, chars, ahead, kinds)
		}
	}
	if kinds, _ := pi.keys.kinds([]byte("\x1b[A")); !slices.Equal(kinds, []keyKind{keyRecall}) {
		t.Errorf("Up: kinds %v, want a recall key", kinds)
	}
}

// pi's paste rule on the placeholders it draws (editor.js, 0.87.1; the
// pastes capture): more than 10 lines as "+N lines", counting the pieces
// between line breaks, else over 1,000 UTF-16 units as "N chars"; the text
// pi keeps has tabs as four spaces and no control characters.
func TestPastePi(t *testing.T) {
	rule := pi.pastes[0]
	lines := func(n int) string {
		var l []string
		for i := range n {
			l = append(l, fmt.Sprintf("line %d", i))
		}
		return strings.Join(l, "\n")
	}
	for _, c := range []struct {
		paste, placeholder, wrong string
	}{
		{lines(10), "", ""},
		{lines(11), "[paste #1 +11 lines]", "[paste #1 +10 lines]"},
		{lines(11) + "\n", "[paste #1 +12 lines]", "[paste #1 +11 lines]"},
		{strings.Repeat("a", 1000), "", ""},
		{strings.Repeat("a", 1001), "[paste #1 1001 chars]", "[paste #1 1000 chars]"},
		{strings.Repeat("😀", 501), "[paste #1 1002 chars]", "[paste #1 501 chars]"}, // UTF-16 units
		{strings.Repeat("a", 996) + "\t", "", ""},                                   // 1,000 once the tab is 4 spaces
		{strings.Repeat("a", 997) + "\t", "[paste #1 1001 chars]", "[paste #1 998 chars]"},
		{strings.Repeat("a", 1002) + "\x01", "[paste #1 1002 chars]", "[paste #1 1003 chars]"},
	} {
		if got := rule.collapses(c.paste, 40); got != (c.placeholder != "") {
			t.Errorf("%.20q (%d bytes): collapses %v", c.paste, len(c.paste), got)
			continue
		}
		if c.placeholder == "" {
			continue
		}
		if !rule.fits(rule.placeholder.FindStringSubmatch(c.placeholder), c.paste) ||
			rule.fits(rule.placeholder.FindStringSubmatch(c.wrong), c.paste) {
			t.Errorf("%.20q (%d bytes): fits %s, or %s too", c.paste, len(c.paste), c.placeholder, c.wrong)
		}
	}
	if got := piPasteText("a\tb\r\nc\x01d\x1b[106;5ue"); got != "a    b\ncd\ne" {
		t.Errorf("piPasteText: %q", got)
	}
}

// A placeholder deleted with Backspace renumbers the ones above it, so a
// number names a paste only in the read that shows it: the placeholders,
// lowest first, pair with the pastes in the order they came, and between
// pastes of one size the one whose place in the draft saved before
// matches.
func TestPasteExpandPiRenumbered(t *testing.T) {
	a, b, c := strings.Repeat("alpha ", 167), strings.Repeat("bravo ", 167), strings.Repeat("carol ", 167)
	for _, x := range []string{a, b, c} {
		if len(x) != 1002 {
			t.Fatal("pastes of one size")
		}
	}
	p := &pasteTracker{}
	paste := func(s string) { p.feed([]byte("\x1b[200~" + s + "\x1b[201~")) }
	paste(a)
	paste(b)
	prev := p.expand("one [paste #1 1002 chars] two [paste #2 1002 chars] three", "", &pi)
	if prev != "one "+a+" two "+b+" three" {
		t.Fatalf("both: %.60q", prev)
	}
	// The first deleted: b is #1 now.
	if got := p.expand("one  two [paste #1 1002 chars] three", prev, &pi); got != "one  two "+b+" three" {
		t.Fatalf("the first deleted: %.60q", got)
	}
	prev = "one  two " + b + " three"
	paste(c)
	if got := p.expand("one  two [paste #1 1002 chars] three four [paste #2 1002 chars]", prev, &pi); got != "one  two "+b+" three four "+c {
		t.Fatalf("pasted again: %.60q", got)
	}
	// The second deleted: a stays #1.
	q := &pasteTracker{}
	q.feed([]byte("\x1b[200~" + a + "\x1b[201~\x1b[200~" + b + "\x1b[201~"))
	prev = q.expand("five [paste #1 1002 chars] six [paste #2 1002 chars] seven", "", &pi)
	if got := q.expand("five [paste #1 1002 chars] six  seven", prev, &pi); got != "five "+a+" six  seven" {
		t.Fatalf("the second deleted: %.60q", got)
	}
	// Forward delete takes a placeholder without renumbering: #1 and #3
	// are left, still in the order the pastes came.
	r := &pasteTracker{}
	r.feed([]byte("\x1b[200~" + a + "\x1b[201~\x1b[200~" + b + "\x1b[201~\x1b[200~" + c + "\x1b[201~"))
	prev = r.expand("[paste #1 1002 chars] [paste #2 1002 chars] [paste #3 1002 chars]", "", &pi)
	if got := r.expand("[paste #1 1002 chars]  [paste #3 1002 chars]", prev, &pi); got != a+"  "+c {
		t.Fatalf("the middle one deleted forward: %.60q", got)
	}
	// A placeholder no paste fits stays as it is.
	if got := r.expand("[paste #1 +12 lines]", "", &pi); got != "[paste #1 +12 lines]" {
		t.Fatalf("no paste of 12 lines: %q", got)
	}
}

// A check that cannot fail: with ids that hold, as Claude Code's do, the
// paste deleted first still fills #1 after the renumbering.
func TestPasteExpandPiCatchesHeldIDs(t *testing.T) {
	p := pi
	p.pastes = slices.Clone(pi.pastes)
	p.pastes[0].renumbers = false
	a, b := strings.Repeat("alpha ", 167), strings.Repeat("bravo ", 167)
	tr := &pasteTracker{}
	tr.feed([]byte("\x1b[200~" + a + "\x1b[201~\x1b[200~" + b + "\x1b[201~"))
	prev := tr.expand("one [paste #1 1002 chars] two [paste #2 1002 chars] three", "", &p)
	if got := tr.expand("one  two [paste #1 1002 chars] three", prev, &p); got == "one  two "+b+" three" {
		t.Fatal("held ids expanded the renumbered placeholder right")
	}
}

// pi's wrap, measured on 0.87.1 (wrap and accents in testdata/pi/0.87.1):
// each row back from the screen joins as typed.
func TestPiWrap(t *testing.T) {
	for _, c := range []struct {
		text  string
		width int
		rows  []string
		back  string
	}{
		// A word that ends at the edge with a space after it moves down.
		{"aaaa bbbb cc dd", 12, []string{"aaaa bbbb", "cc dd"}, ""},
		// At the end of the line it stays.
		{"aaaa bbbb cc", 12, []string{"aaaa bbbb cc"}, ""},
		// A word longer than a row breaks at the edge, and the space after
		// it starts the next row when the break falls right before it.
		{"xxxxxxxxxxxxxxx tail", 12, []string{"xxxxxxxxxxxx", "xxx tail"}, ""},
		{"xxxxxxxxxxxx tail", 12, []string{"xxxxxxxxxxxx", " tail"}, ""},
		// Next to a CJK character a row breaks with no space.
		{"aaaaaaaaaaa漢字", 12, []string{"aaaaaaaaaaa", "漢字"}, ""},
		// A placeholder moves whole, spaces and all.
		{"aaaa bbbb [paste #1 1001 chars]", 25, []string{"aaaa bbbb", "[paste #1 1001 chars]"}, ""},
		// A line that fills its row, then a blank line.
		{"aaaa bbbb cc\n\ndd", 12, []string{"aaaa bbbb cc", "", "dd"}, ""},
		// Two spaces at a wrap come back as one.
		{"aaaa bbbb  cc dd", 12, []string{"aaaa bbbb", "cc dd"}, "aaaa bbbb cc dd"},
	} {
		rows, _ := piWrap{}.wrap(c.text, c.width)
		if !slices.Equal(rows, c.rows) {
			t.Errorf("%q: rows %q, want %q", c.text, rows, c.rows)
		}
		want := c.back
		if want == "" {
			want = c.text
		}
		if got := (piWrap{}).unwrap(rows, c.width); got != want {
			t.Errorf("%q: unwrapped as %q", c.text, got)
		}
	}
}

// drawPiBox is pi's screen with one row of text in its box and the
// cursor at its end, drawn the way pi 0.87.1 draws it (typed in
// testdata/pi/0.87.1): full-width rules, a reverse-video cursor, the
// footer under the bottom rule.
func drawPiBox(cols int, text string) string {
	rule := strings.Repeat("─", cols)
	return "\x1b[H\x1b[2J" + rule + "\r\n" + text + "\x1b[7m \x1b[0m\r\n" + rule + "\r\n/w\r\n0.0%/0 (auto)"
}

// piAsideCheck drives pi's box by hand through what ctrlc-history does not
// show: in a saved draft, Up moves the cursor to its start, and Up again
// shows the newest entry of pi's history while pi holds the draft aside,
// in memory only, and drops it once the entry is edited or sent. The
// draft must then be in history, short and alike as the two are; coming
// back down to it costs nothing. Text typed in the same save as the Up
// that follows it, into a draft or an empty box, is saved from the screen
// the Up was typed into, and is still there when the window closes.
func piAsideCheck(t *testing.T, prof *profile) error {
	t.Helper()
	const up, down = "\x1b[A", "\x1b[B"
	start := func(draft string) sendSession {
		s := newSendSession(t, prof)
		s.rec.Agent = "pi"
		if draft != "" {
			s.input([]byte(draft))
			s.write([]byte(drawPiBox(100, draft)))
			s.save()
		}
		return s
	}
	browse := func(s sendSession) {
		s.input([]byte(up))
		s.write([]byte(drawPiBox(100, "fix the bug"))) // the cursor at the start
		s.save()
		s.input([]byte(up))
		s.write([]byte(drawPiBox(100, "run tests"))) // the entry, the draft aside
		s.save()
	}
	for _, c := range []struct {
		name          string
		keys, box     string
		sent, history []string
		draft         string
	}{
		{"entry edited", "x", "run testsx", nil, []string{"fix the bug"}, "run testsx"},
		{"entry sent", "\r", "", []string{"run tests"}, []string{"fix the bug"}, ""},
		{"back down to the draft", "", "", nil, nil, "fix the bug!"},
	} {
		s := start("fix the bug")
		browse(s)
		if c.keys == "" {
			s.input([]byte(down))
			s.write([]byte(drawPiBox(100, "fix the bug")))
			s.save()
			c.keys, c.box = "!", "fix the bug!"
		}
		s.input([]byte(c.keys))
		s.write([]byte(drawPiBox(100, c.box)))
		s.save()
		if got := s.sent(); !slices.Equal(got, c.sent) {
			return fmt.Errorf("%s: sent log %q, want %q", c.name, got, c.sent)
		}
		if got := s.history(); !slices.Equal(got, c.history) {
			return fmt.Errorf("%s: history %q, want %q", c.name, got, c.history)
		}
		if s.rec.Draft != c.draft {
			return fmt.Errorf("%s: draft %q, want %q", c.name, s.rec.Draft, c.draft)
		}
	}
	for _, c := range []struct{ name, saved, typed string }{
		{"typed into a draft, then Up", "line one", "line one and the rest"},
		{"typed into the empty box, then Up", "", "typed then up"},
	} {
		s := start(c.saved)
		s.input([]byte(strings.TrimPrefix(c.typed, c.saved)))
		s.write([]byte(drawPiBox(100, c.typed)))
		s.input([]byte(up)) // in the same save: the cursor goes to the start
		s.write([]byte(drawPiBox(100, c.typed)))
		s.closing()
		s.finish()
		var kept []string
		for _, r := range s.store.orphans() {
			kept = append(kept, r.Draft)
		}
		if !slices.Equal(kept, []string{c.typed}) {
			return fmt.Errorf("%s: orphans %q at the close, want %q", c.name, kept, c.typed)
		}
	}
	return nil
}

func TestPiHistoryBrowsing(t *testing.T) {
	if err := piBrowseCheck(t, &pi); err != nil {
		t.Fatal(err)
	}
	if err := piAsideCheck(t, &pi); err != nil {
		t.Fatal(err)
	}
	// A check that can fail: without browsing from a draft, the entry
	// replaces the draft with no copy.
	p := pi
	p.keys.browses = false
	if piAsideCheck(t, &p) == nil {
		t.Error("with browses off the held-aside check stayed green")
	}
}

// piHeldPasteCheck expands pi's placeholders for pastes pi keeps other
// than they came (piPasteText): a paste that collapses only once its tab
// is four spaces and its CRLF one LF fills "[paste #1 1002 chars]" with
// the text pi holds; after the editor round trip the box shows that text
// as typed, and a new paste of the same size, #1 again, is the new paste,
// not the one already in the box.
func piHeldPasteCheck(prof *profile) error {
	a := strings.Repeat("x", 990) + "\tend\r\nnext" // 1,000 bytes as pasted
	held := strings.Repeat("x", 990) + "    end\nnext"
	b := strings.Repeat("y", 1002)
	p := &pasteTracker{}
	p.feed([]byte("\x1b[200~" + a + "\x1b[201~"))
	prev := p.expand("before [paste #1 1002 chars] after", "", prof)
	if want := "before " + held + " after"; prev != want {
		return fmt.Errorf("expanded %.40q, want %.40q", prev, want)
	}
	p.feed([]byte("\x1b[200~" + b + "\x1b[201~"))
	if got, want := p.expand("before "+held+" after [paste #1 1002 chars]", prev, prof), "before "+held+" after "+b; got != want {
		return fmt.Errorf("the second paste: %.40q, want %.40q", got[len(got)-min(len(got), 40):], want[len(want)-40:])
	}
	return nil
}

func TestPasteExpandPiHeldText(t *testing.T) {
	if len("\t\r\n")+990+len("endnext") != 1000 || len(piPasteText(strings.Repeat("x", 990)+"\tend\r\nnext")) != 1002 {
		t.Fatal("the paste must collapse only as pi holds it")
	}
	if err := piHeldPasteCheck(&pi); err != nil {
		t.Fatal(err)
	}
	// A check that can fail: with the paste as it came, the tab and the CR
	// come back, and the paste already in the box fills the new placeholder.
	p := pi
	p.pastes = slices.Clone(pi.pastes)
	p.pastes[0].holds = nil
	if piHeldPasteCheck(&p) == nil {
		t.Error("with holds unset the held-text check stayed green")
	}
}
