package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/charmbracelet/x/vt"
	"github.com/mattn/go-runewidth"
)

// agy's profile on real 1.2.13 captures (testdata/agy/1.2.13): the replays
// against agy's own editor copies, what its reader must never take for a
// draft, what it must go red on when it is mutated, its keys, pastes and
// wrap, and what it does at a closed window, a send and Ctrl+Z.

var agyCaptures = captureSet{folder: filepath.Join("testdata", "agy", "1.2.13"), command: "agy", shows: agyShows}

// agyShows reports whether got is agy's draft truth as far as agy's screen
// shows it: byte for byte, but for three things its rows cannot tell.
// Spaces at the end of a line are drawn as the blank cells after it (what
// Ctrl+W leaves in deletes); a combining mark never reaches the screen at
// all, since agy draws a decomposed accent as its bare letter while it
// holds the whole character (accents), so that much of such a draft is
// beyond any reader of agy's screen; and the one line break byteExact
// allows at a full row. Tolerated by agy's measured drawing, not by the
// profile's model, so a wrong model cannot excuse itself. Only the truth
// is brought down to what the screen can show: a reader that invented a
// combining mark or spaces at the end of a line must not pass, and none
// of the 50 checks in the replays needs got trimmed.
func agyShows(got, truth string) bool {
	lines := strings.Split(truth, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(agyDrawn(l), " ")
	}
	w := strings.Join(lines, "\n")
	return got == truth || byteExact(got, w, 117, agy.unwrap) || byteExact(got, agyJoined(w, 117), 117, agy.unwrap)
}

// agyJoined is text without the line breaks the screen hides altogether:
// one typed at the end of a row the text filled exactly, where the word
// after it would not have fitted on that row either. The two rows then
// look exactly like one word too long for a row, broken at the edge, and
// no reader can tell them apart (wrap, "w" x117 then a line break). Every
// other break comes back as itself or, at a full row, as a space, which
// byteExact allows.
func agyJoined(text string, width int) string {
	var b strings.Builder
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			rows, _ := agy.unwrap.wrap(b.String(), width)
			last := rows[len(rows)-1]
			word := last[strings.LastIndexByte(last, ' ')+1:]
			next, _, _ := strings.Cut(line, " ")
			if runewidth.StringWidth(last) != width || runewidth.StringWidth(word)+runewidth.StringWidth(next) <= width {
				b.WriteByte('\n')
			}
		}
		b.WriteString(line)
	}
	return b.String()
}

// agyDrawn is text as agy draws it: without the combining marks it leaves
// out (measured on 1.2.13, accents).
func agyDrawn(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.In(r, unicode.Mn) {
			return -1
		}
		return r
	}, s)
}

// replayAgy replays name.rec through a session with prof and returns the
// first Ctrl+G whose editor copy the saved draft differs from.
func replayAgy(t *testing.T, prof *profile, name string, recalled ...int) error {
	t.Helper()
	_, err := replayAgySession(t, prof, name, recalled...)
	return err
}

func replayAgySession(t *testing.T, prof *profile, name string, recalled ...int) (*session, error) {
	t.Helper()
	return replayCaptureSession(t, prof, agyCaptures, name, recalled...)
}

// Recorded with keys and output together (testdata/agy/README.md):
//   - typed, multiline, accents, tall, deletes: the box, its new-line keys,
//     accents, emoji and a wide character at the edge, 45 lines in a box of
//     20 edited out of sight, every delete key;
//   - scroll-steps: 27 lines, one of them three rows wide, Up, PageUp,
//     Ctrl+Home and Ctrl+End through the rows out of sight;
//   - pastes, pastes2: pastes inline and as placeholders of both forms, a
//     placeholder deleted with one Backspace, which renumbers nothing, the
//     numbering starting again after a Ctrl+G, a paste with a tab and one
//     whose line breaks came as CR;
//   - ctrlc-history: two sends, then Up and Down through agy's own history
//     (the first two Ctrl+G), and Up in a typed draft, which leaves it
//     alone (the third);
//   - submit: Enter sending into a dead endpoint, a draft typed during the
//     turn, then Alt+Enter, Shift+Enter, Ctrl+J and Esc then Enter, each
//     of which makes a new line instead;
//   - resume-c, resume-conversation: a draft in agy -c and in
//     agy --conversation <id>;
//   - suspend, suspend-legacy: Ctrl+Z as ESC[122;5u, which nothing acts
//     on, and as the lone byte, which unsent takes;
//   - wrap: a line that ends exactly at the wrap width, and a word that
//     would end exactly at it with a space after, which agy moves down;
//   - edge-deletes: Ctrl+W, Backspace, Alt+Backspace, Ctrl+U and Delete
//     on the last row of a box at its cap, where only the keys tell a
//     deletion from rows scrolled out of sight below;
//   - early-paste: a 20-line paste on the first frame that shows the box;
//   - restore-c, restore-long: the drafts orphan and orphan-long left at
//     a closed window, put back by unsent into agy -c, the long one as
//     agy's own placeholder.
var agyReplays = []struct {
	name     string
	recalled []int
}{{"typed", nil}, {"multiline", nil}, {"accents", nil}, {"tall", nil}, {"scroll-steps", nil},
	{"deletes", nil}, {"edge-deletes", nil}, {"wrap", nil}, {"pastes", nil}, {"pastes2", nil},
	{"early-paste", nil}, {"bash-mode", nil}, {"ctrlc-history", []int{1, 2}}, {"submit", nil},
	{"resume-c", nil}, {"resume-conversation", nil}, {"restore-c", nil}, {"restore-long", nil},
	{"suspend", nil}, {"suspend-legacy", nil}}

func TestReplayAgy(t *testing.T) {
	for _, r := range agyReplays {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			if err := replayAgy(t, &agy, r.name, r.recalled...); err != nil {
				t.Fatal(err)
			}
		})
		// Again with a save every 0.4 s of recorded time, as a live session
		// saves: keys typed in one save must still be read. Not deletes,
		// which presses Ctrl+U and Ctrl+G at once: the draft read before
		// the Ctrl+U is still the saved one as the editor opens, which
		// holds more than agy's copy, never less.
		if r.name == "deletes" {
			continue
		}
		t.Run(r.name+" save every "+saveInterval.String(), func(t *testing.T) {
			t.Parallel()
			set := agyCaptures
			set.tick = saveInterval
			if _, err := replayCaptureSession(t, &agy, set, r.name, r.recalled...); err != nil {
				t.Fatal(err)
			}
		})
	}
	// Every capture with editor copies is replayed: a new one needs a line
	// in agyReplays.
	replayed := map[string]bool{}
	for _, r := range agyReplays {
		replayed[r.name] = true
	}
	copies, _ := filepath.Glob(filepath.Join(agyCaptures.folder, "*.editor-1.txt"))
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

// firstRedAgyReplay replays agyReplays with prof in order and returns the
// name of the first that goes red, or "".
func firstRedAgyReplay(t *testing.T, prof *profile) string {
	t.Helper()
	for _, r := range agyReplays {
		if replayAgy(t, prof, r.name, r.recalled...) != nil {
			return r.name
		}
	}
	return ""
}

// A check that cannot fail is not a check: with agy's wrap model swapped
// for character wrap, or for Codex's word wrap with its end row, some
// replay must go red.
func TestReplayAgyCatchesAWrongWrapModel(t *testing.T) {
	for _, m := range []struct {
		name string
		rule unwrapRule
	}{{"character wrap", charWrap{}}, {"Claude Code's word wrap", wordWrap{}},
		{"Codex's word wrap, with its end row", wordWrap{endRow: true}}} {
		p := agy
		p.unwrap = m.rule
		red := firstRedAgyReplay(t, &p)
		if red == "" {
			t.Errorf("with %s every agy replay stayed green", m.name)
		}
		t.Logf("with %s, red: %s", m.name, red)
	}
}

// agyNotABox are frames that show no box to save into: agy's first-run
// screens, drawn on the alternate screen with none of the box's rules.
// Each must read as no box, so the draft is kept.
var agyNotABox = []struct{ record, shows string }{
	{"first-run", "Choose your color scheme"},
	{"first-run", "Terms of Service & Data Use"},
	{"first-run", "Do you trust the contents of this project?"},
}

// agyEmptyBox are frames where a panel, a picker or a mode hint is on
// screen and the box itself holds nothing: each must read as an empty box,
// never as a draft. agy draws its panels under the box's bottom rule, so
// the box stays in view while they are open. Each is the last frame that
// shows what names it, since the box below a panel is drawn a moment
// after the panel itself.
var agyEmptyBox = []struct {
	record string
	shows  []string
}{
	{"dialogs", []string{"Keyboard Shortcuts"}},              // ? for shortcuts
	{"dialogs", []string{"Available Command"}},               // its commands tab
	{"dialogs", []string{"Accept-edits mode: file edits"}},   // shift+tab, twice
	{"dialogs", []string{"Plan mode: research & plan only"}}, //
	{"dialogs", []string{"Settings"}},                        // /settings
	{"dialogs", []string{"Switch Model"}},                    // /model
	{"dialogs", []string{"No conversations available."}},     // /resume
	{"dialogs", []string{"Context Usage"}},                   // /context
	{"ctrlc-history", []string{"Artifacts"}},                 // ctrl+r
	{"first-run", []string{"? for shortcuts"}},               // the box after the trust dialog
	{"resume-unknown", []string{"not found"}},                // agy --conversation <unknown>
}

// agyMenus are frames with a menu or a picker under the box: only what was
// typed is the draft. Each is named by two things on it, since the
// shortcuts panel lists the same commands as the menu.
var agyMenus = []struct {
	record string
	shows  []string
	draft  string
}{
	{"dialogs", []string{"/add-dir", "enter Select · tab Complete"}, "/"},
	{"dialogs", []string{"/config (settings)", "enter Select · tab Complete"}, "/set"},
	{"dialogs", []string{"Directory", "enter Select · tab Complete"}, "@"},
	// agy's "!" is the box's glyph, not draft text: at Ctrl+G it hands the
	// editor the command alone (bash-mode).
	{"dialogs", []string{"activated bash mode", "! echo dummy"}, "echo dummy"},
}

// agyTyped is every draft each record shows in its box, in the order
// typed: no other text may ever be read from its frames.
var agyTyped = map[string][]string{
	"dialogs":        {"?", "/", "/set", "@", "echo dummy", "/settings", "/model", "/resume", "/context"},
	"first-run":      nil,
	"exit-ctrl-c":    {"a draft that the exit throws away"},
	"exit-ctrl-d":    nil,
	"kill9":          {"kill nine draft zebra\nsecond row of it"},
	"killsession":    {"kill session draft yankee\nsecond row of it"},
	"resume-unknown": nil,
	"ctrlc-history":  {"first sent prompt rig", "second sent prompt rig", "a draft, then Up"},
	"identity":       {"typed but never submitted, dummy", "second submit of the same conversation"},
}

// agyScreensCheck returns the first way agyBox misreads the frames above,
// or nil.
func agyScreensCheck(t *testing.T, frames frameSet) error {
	t.Helper()
	get := func(record string) []*screen { return frames.get(t, &agy, agyCaptures.folder, record) }
	for _, c := range agyNotABox {
		f := firstFrame(t, get(c.record), c.shows)
		if v, ok := agyBox(f); ok {
			return fmt.Errorf("%s, the frame showing %q: read as a box %+v", c.record, c.shows, v)
		}
	}
	for _, c := range agyEmptyBox {
		f := lastFrame(t, get(c.record), c.shows...)
		v, ok := agyBox(f)
		if !ok || !v.empty {
			return fmt.Errorf("%s, the frame showing %q: box %v, %+v, want an empty box", c.record, c.shows, ok, v)
		}
	}
	for _, c := range agyMenus {
		f := firstFrame(t, get(c.record), c.shows...)
		v, ok := agyBox(f)
		var st stitcher
		if got := st.update(v, agy.unwrap); !ok || got != c.draft {
			return fmt.Errorf("%s, the frame showing %q: draft %q (box %v), want %q", c.record, c.shows, got, ok, c.draft)
		}
	}
	for record, typed := range agyTyped {
		for _, f := range get(record) {
			v, ok := agyBox(f)
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

func TestAgyNegativeScreens(t *testing.T) {
	if err := agyScreensCheck(t, frameSet{}); err != nil {
		t.Fatal(err)
	}
}

// The frames of the empty box, a draft, a scrolled box and bash mode read
// as agy drew them: the rows, the cursor's row, and the rows the labels
// say are out of sight.
func TestAgyBoxFrames(t *testing.T) {
	empty := firstFrame(t, captureFrames(t, &agy, agyCaptures.folder, "typed"), "? for shortcuts")
	if v, ok := agyBox(empty); !ok || !v.empty || v.width != 117 {
		t.Fatalf("the first frame's box: %+v, %v", v, ok)
	}
	tall := captureFrames(t, &agy, agyCaptures.folder, "tall")
	f := firstFrame(t, tall, "↑ 4 more lines", "↓ 21 more lines")
	v, ok := agyBox(f)
	if !ok || len(v.rows) != 20 || !v.capped || v.width != 117 ||
		!strings.HasPrefix(v.rows[0], "row 05 of the tall draft") {
		t.Fatalf("the box scrolled up: %+v, %v", v, ok)
	}
	// The labels are not text: a box of 20 rows at the cap shows 20.
	for _, r := range v.rows {
		if strings.Contains(r, "more lines") {
			t.Fatalf("a scroll label was read as a draft row: %q", r)
		}
	}
	turn := lastFrame(t, captureFrames(t, &agy, agyCaptures.folder, "submit"), "Generating", "typed during the turn")
	if v, ok := agyBox(turn); !ok || len(v.rows) != 1 || v.rows[0] != "typed during the turn" {
		t.Fatalf("the box under a running turn: %+v, %v", v, ok)
	}
}

// A check that cannot fail is not a check: each part of the reader,
// mutated, turns a replay or a negative screen red. One red is the proof,
// so each mutant stops at the first: the screens, then the replays.
func TestAgyReaderMutations(t *testing.T) {
	good := agyLayout
	t.Cleanup(func() { agyLayout = good })
	frames := frameSet{}
	for _, m := range []struct {
		name   string
		mutate func(*agyBoxLayout)
	}{
		// A cap one row lower cuts the top rule of a full box out of the
		// reader's reach; one row higher only spares a box at its cap the
		// capped flag before anything is out of sight, which no capture
		// can tell, so it is not a mutant.
		{"a cap of one row less", func(b *agyBoxLayout) { b.cap = func(rows int) int { return rows/2 - 1 } }},
		{"wrap at the width", func(b *agyBoxLayout) { b.margin = 0 }},
		{"wrap at the width minus 2", func(b *agyBoxLayout) { b.margin = 2 }},
		{"wrap at the width minus 4", func(b *agyBoxLayout) { b.margin = 4 }},
		{"text from column 0", func(b *agyBoxLayout) { b.indent = 0 }},
		{"text from column 1", func(b *agyBoxLayout) { b.indent = 1 }},
		{"no scroll labels", func(b *agyBoxLayout) { b.labels = false }},
		{"no mode hints", func(b *agyBoxLayout) { b.hints = false }},
		{"any glyph", func(b *agyBoxLayout) { b.glyphs = nil }},
	} {
		t.Run(m.name, func(t *testing.T) {
			bad := good
			m.mutate(&bad)
			agyLayout = bad
			defer func() { agyLayout = good }()
			if err := agyScreensCheck(t, frames); err != nil {
				t.Logf("red: screens (%v)", err)
				return
			}
			if red := firstRedAgyReplay(t, &agy); red != "" {
				t.Logf("red: %s", red)
				return
			}
			t.Fatal("every agy replay and screen stayed green")
		})
	}
}

// The delete, recall and submit keys are what the captures need: without
// the keys that delete, text deleted where the box is at its cap reads as
// scrolled out of sight; without Up as a recall key, or with history
// recalled only into a box nothing was typed into since it emptied, an
// entry of agy's own history is saved as a draft; without Enter as a
// submit key the sends go to history, not the sent log.
func TestAgyKeyMutations(t *testing.T) {
	for _, m := range []struct {
		name string
		drop func(*keyset)
	}{
		{"keys that delete a word or more", func(k *keyset) { k.many = nil }},
		{"keys that delete one character", func(k *keyset) { k.one = nil }},
	} {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			p := agy
			m.drop(&p.keys)
			if red := firstRedAgyReplay(t, &p); red == "" {
				t.Fatal("every agy replay stayed green")
			} else {
				t.Logf("red: %s", red)
			}
		})
	}
	t.Run("recall keys", func(t *testing.T) {
		t.Parallel()
		p := agy
		p.keys.recall = nil
		if replayAgy(t, &p, "ctrlc-history", 1, 2) == nil {
			t.Fatal("a history entry brought back with Up was not saved as a draft")
		}
	})
	t.Run("recalling into any empty box", func(t *testing.T) {
		t.Parallel()
		p := agy
		p.keys.empties = false
		if replayAgy(t, &p, "ctrlc-history", 1, 2) == nil {
			t.Fatal("the entry Up brought back after an Esc was not saved as a draft")
		}
	})
	t.Run("submit key", func(t *testing.T) {
		t.Parallel()
		p := agy
		p.keys.submit = nil
		if agySendCheck(t, &p) == nil {
			t.Fatal("every send stayed in the sent log without Enter")
		}
	})
}

// agySendCheck replays the captures that send into the dead endpoint:
// every prompt Enter sent must be in the sent log, and the drafts cleared
// with Ctrl+U in history and not in the sent log. agy's other new-line
// keys (Alt+Enter, Shift+Enter, Ctrl+J, Esc then Enter) send nothing, so
// what they leave in the box is cleared, never sent.
func agySendCheck(t *testing.T, prof *profile) error {
	t.Helper()
	for _, c := range []struct {
		record        string
		sent, cleared []string
		recalled      []int
	}{
		// Each of the four prompts after the first is left in the box by a
		// new-line key and then cleared with Ctrl+U, whose first press
		// eats the line break it made, so the draft that reaches history
		// is the line alone.
		{"submit", []string{"dummy prompt one rig"},
			[]string{"typed during the turn", "second dummy prompt rig", "third dummy prompt rig",
				"fourth dummy prompt rig", "fifth dummy prompt rig"}, nil},
		{"identity", []string{"typed but never submitted, dummy", "second submit of the same conversation"}, nil, nil},
		{"ctrlc-history", []string{"first sent prompt rig", "second sent prompt rig"},
			[]string{"a draft, then Up"}, []int{1, 2}},
	} {
		s, err := replayAgySession(t, prof, c.record, c.recalled...)
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
			// In ctrlc-history the sent prompts come back from agy's own
			// history with Up and go through Ctrl+G, which makes them
			// drafts of unsent's; the Ctrl+U after that clears them into
			// history.
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

func TestAgySendGoesToTheSentLog(t *testing.T) {
	if err := agySendCheck(t, &agy); err != nil {
		t.Fatal(err)
	}
}

// The window closes, or agy is killed, with a draft in the box: agy keeps
// it in no file, and unsent keeps it as an orphan.
var agyCloses = []struct {
	name   string
	closed bool
	draft  string
}{
	{"kill9", false, "kill nine draft zebra\nsecond row of it"},
	{"killsession", true, "kill session draft yankee\nsecond row of it"},
}

func agyCloseCheck(t *testing.T, prof *profile) error {
	t.Helper()
	for _, c := range agyCloses {
		s, err := replayAgySession(t, prof, c.name)
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

func TestAgyWindowCloseKeepsTheDraft(t *testing.T) {
	if err := agyCloseCheck(t, &agy); err != nil {
		t.Fatal(err)
	}
	// A check that can fail: a reader that takes every box for empty loses
	// the drafts.
	empty := agy
	empty.read = func(scr *screen) (view, bool) {
		v, ok := agyBox(scr)
		v.rows, v.empty = nil, true
		return v, ok
	}
	if agyCloseCheck(t, &empty) == nil {
		t.Error("with every box read empty the close check stayed green")
	}
}

// Two Ctrl+C with a draft in the box quit agy, which keeps the draft
// nowhere: unsent keeps it, since no key of agy's clears the box.
func TestAgyExitKeepsTheDraft(t *testing.T) {
	s, err := replayAgySession(t, &agy, "exit-ctrl-c")
	if err != nil {
		t.Fatal(err)
	}
	s.finish()
	var kept []string
	for _, r := range s.store.orphans() {
		kept = append(kept, r.Draft)
	}
	if !slices.Equal(kept, []string{"a draft that the exit throws away"}) {
		t.Fatalf("orphans %q after the Ctrl+C exit", kept)
	}
}

// Ctrl+Z is unsent's, in the form tmux sent it (ESC[122;5u), and unsent
// writes nothing for agy as it suspends it: agy re-asserts bracketed
// paste and modifyOtherKeys while it is idle, but nothing sets the kitty
// flags again after the resume (suspend-legacy).
func TestAgySuspendPolicy(t *testing.T) {
	if at, end := findKey([]byte("ab\x1b[122;5ucd"), suspendKeys(&agy)); at != 2 || end != 10 {
		t.Fatalf("Ctrl+Z found at %d..%d", at, end)
	}
	if agy.suspended != nil {
		t.Fatalf("agy's suspend output %q: its repaint after a resume sets none of it again", agy.suspended)
	}
	data := readFixture(t, filepath.Join("..", "agy", "1.2.13", "suspend-legacy.rec"))
	// From the Ctrl+Z to the next key, a Ctrl+G typed once the box was
	// back: the stop, the continue, and agy's repaint.
	var out []byte
	stopped, done := false, false
	eachChunk(t, data, func(_ time.Time, dir byte, chunk []byte) {
		switch {
		case done:
		case dir == 'i' && bytes.Contains(chunk, []byte("\x1a")):
			stopped = true
		case dir == 'i' && stopped && bytes.Contains(chunk, []byte("\x1b[103;5u")):
			done = true
		case dir == 'o' && stopped:
			out = append(out, chunk...)
		}
	})
	if !done || !bytes.Contains(out, []byte("draft before the legacy suspend")) {
		t.Fatal("no repaint of the draft between the Ctrl+Z and the next key")
	}
	if bytes.Contains(out, []byte("\x1b[>1u")) {
		t.Fatal("agy set the kitty flags again after the resume; its suspend output can go in the profile")
	}
}

// agy is a profile: `agy` runs through its reader, and setup wraps it.
func TestAgyIsAProfile(t *testing.T) {
	if p := profileFor("/Users/rig/.local/bin/agy"); p != &agy {
		t.Fatalf("profile for agy: %v", p)
	}
	if !slices.Contains(agentCommands(), "agy") {
		t.Fatalf("setup wraps %q, not agy", agentCommands())
	}
}

// Saving stopped says which agy ran and which one the reader was last
// checked against; `agy --version` prints the bare version.
func TestAgySavingStoppedLine(t *testing.T) {
	s := &session{prof: &agy, version: make(chan string, 1)}
	s.typed.Store(true)
	s.version <- agentVersionOf("1.2.14\n")
	want := "unsent: could not read agy 1.2.14's box this session (last verified 1.2.13), nothing was saved"
	if got := s.exitLines(); !slices.Equal(got, []string{want}) {
		t.Fatalf("exit lines %q, want %q", got, want)
	}
}

// agy's keys, as the captures sent them in tmux with modifyOtherKeys 2 in
// its CSI-u form and as legacy bytes, each read as the profile says.
func TestAgyKeys(t *testing.T) {
	for _, c := range []struct {
		bytes         string
		chars         int64
		ahead         bool
		what          string
		submit, clear bool
	}{
		{bytes: "\x7f", chars: 1, what: "Backspace"},
		{bytes: "\x1b[104;5u", chars: 1, what: "Ctrl+H"},
		{bytes: "\x1b[3~", chars: 1, ahead: true, what: "Delete"},
		{bytes: "\x1b[119;5u", chars: unlimited, what: "Ctrl+W"},
		{bytes: "\x1b[127;3u", chars: unlimited, what: "Alt+Backspace"},
		{bytes: "\x1b[117;5u", chars: unlimited, what: "Ctrl+U"},
		{bytes: "\x1b[95;5u", chars: unlimited, ahead: true, what: "Ctrl+_, undo"},
		{bytes: "\x1f", chars: unlimited, ahead: true, what: "Ctrl+_ as legacy bytes"},
		{bytes: "\x1b[122;6u", chars: unlimited, ahead: true, what: "Ctrl+Shift+Z, redo"},
		{bytes: "\x1b[107;5u", what: "Ctrl+K, which does nothing"},
		{bytes: "\x1b[100;5u", what: "Ctrl+D, which does nothing in a draft"},
		{bytes: "\x1b[100;3u", what: "Alt+D, which does nothing"},
		{bytes: "\x1b[3;5~", what: "Ctrl+Delete, which does nothing"},
		{bytes: "\x1b[3;3~", what: "Alt+Delete, which does nothing"},
		{bytes: "\x1b[127;5u", what: "Ctrl+Backspace, which does nothing"},
		{bytes: "\x1b[127;2u", what: "Shift+Backspace, which does nothing"},
		{bytes: "\x1b[3;2~", what: "Shift+Delete, which does nothing"},
		{bytes: "\r", submit: true, what: "Enter"},
		{bytes: "\x1b[13;3u", what: "Alt+Enter, a new line"},
		{bytes: "\x1b[13;2u", what: "Shift+Enter, a new line"},
		{bytes: "\x1b[106;5u", what: "Ctrl+J, a new line"},
		{bytes: "\x1b\r", what: "Esc then Enter in one read, a new line"},
		{bytes: "\x1b[99;5u", what: "Ctrl+C, which keeps the draft"},
		{bytes: "\x1b", what: "Esc, which keeps the draft"},
	} {
		chars, ahead := agy.keys.deletes([]byte(c.bytes))
		kinds, _ := agy.keys.kinds([]byte(c.bytes))
		if chars != c.chars || ahead != c.ahead || slices.Contains(kinds, keySubmit) != c.submit ||
			slices.Contains(kinds, keyClear) != c.clear {
			t.Errorf("%s: deletes %d (ahead %v), kinds %v", c.what, chars, ahead, kinds)
		}
	}
	for _, k := range []string{"\x1b[A", "\x1b[B"} {
		if kinds, _ := agy.keys.kinds([]byte(k)); !slices.Equal(kinds, []keyKind{keyRecall}) {
			t.Errorf("%q: kinds %v, want a recall key", k, kinds)
		}
	}
}

// agy's paste rule on the placeholders it draws (pastes, pastes2): more
// than min(15, rows/2) lines as "+N lines", counting the pieces between
// line breaks, else a line over 1,000 characters as "N chars", counting
// the whole paste; both on the text agy keeps, with each tab as four
// spaces and one line break at the end dropped.
func TestPasteAgy(t *testing.T) {
	rule := agy.pastes[0]
	lines := func(n int) string {
		var l []string
		for i := range n {
			l = append(l, fmt.Sprintf("line %d", i))
		}
		return strings.Join(l, "\n")
	}
	for _, c := range []struct {
		paste, placeholder, wrong string
		rows                      int
	}{
		{paste: lines(15), rows: 40},
		{paste: lines(16), rows: 40, placeholder: "[Pasted text #1 +16 lines]", wrong: "[Pasted text #1 +15 lines]"},
		{paste: lines(10), rows: 20},
		{paste: lines(11), rows: 20, placeholder: "[Pasted text #1 +11 lines]", wrong: "[Pasted text #1 +10 lines]"},
		// A line break at the end is dropped, so 16 lines and a break are
		// 16 lines, not 17.
		{paste: lines(16) + "\n", rows: 40, placeholder: "[Pasted text #1 +16 lines]", wrong: "[Pasted text #1 +17 lines]"},
		{paste: strings.Repeat("a", 1000), rows: 40},
		{paste: strings.Repeat("a", 1001), rows: 40, placeholder: "[Pasted text #2 1001 chars]", wrong: "[Pasted text #2 1000 chars]"},
		// The count is the whole paste, line breaks included; the rule is
		// the longest line.
		{paste: "a\n" + strings.Repeat("b", 1200) + "\nc", rows: 40,
			placeholder: "[Pasted text #3 1204 chars]", wrong: "[Pasted text #3 1200 chars]"},
		{paste: strings.Repeat("é", 1001), rows: 40, placeholder: "[Pasted text #1 1001 chars]", wrong: "[Pasted text #1 2002 chars]"},
		// A tab is four spaces in agy's buffer, so 997 characters and one
		// pass 1,000.
		{paste: strings.Repeat("a", 996) + "\t", rows: 40},
		{paste: strings.Repeat("a", 997) + "\t", rows: 40, placeholder: "[Pasted text #1 1001 chars]", wrong: "[Pasted text #1 998 chars]"},
	} {
		if got := rule.collapses(c.paste, c.rows); got != (c.placeholder != "") {
			t.Errorf("%.20q (%d bytes) at %d rows: collapses %v", c.paste, len(c.paste), c.rows, got)
			continue
		}
		if c.placeholder == "" {
			continue
		}
		if !rule.fits(rule.placeholder.FindStringSubmatch(c.placeholder), c.paste) ||
			rule.fits(rule.placeholder.FindStringSubmatch(c.wrong), c.paste) {
			t.Errorf("%.20q: fits %s, or %s too", c.paste, c.placeholder, c.wrong)
		}
	}
	if got := agyPasteText("a\tb\r\nc\n"); got != "a    b\nc" {
		t.Errorf("agyPasteText: %q", got)
	}
}

// agy's numbers hold: a placeholder deleted leaves the others as they
// were and the next paste takes the next number, so a number names its
// paste for as long as the box shows it.
func TestPasteExpandAgyKeepsItsNumbers(t *testing.T) {
	a, b, c := strings.Repeat("alpha ", 200), strings.Repeat("bravo ", 200), strings.Repeat("carol ", 200)
	p := &pasteTracker{}
	p.rows = 40
	paste := func(s string) { p.feed([]byte("\x1b[200~" + s + "\x1b[201~")) }
	paste(a)
	paste(b)
	prev := p.expand("one [Pasted text #1 1200 chars] two [Pasted text #2 1200 chars] three", "", &agy)
	if want := "one " + a + " two " + b + " three"; prev != want {
		t.Fatalf("both: %.60q", prev)
	}
	// The first deleted: the second stays #2, and the next paste is #3.
	prev = p.expand("one  two [Pasted text #2 1200 chars] three", prev, &agy)
	if want := "one  two " + b + " three"; prev != want {
		t.Fatalf("the first deleted: %.60q", prev)
	}
	paste(c)
	if got := p.expand("one  two [Pasted text #2 1200 chars] three [Pasted text #3 1200 chars]", prev, &agy); got != "one  two "+b+" three "+c {
		t.Fatalf("pasted again: %.60q", got)
	}
}

// A check that cannot fail is not a check: without the paste rule, or with
// one that collapses nothing or keeps a paste as it came, some replay must
// go red. (Numbers that renumber, as pi's do, are not a mutant here: with
// agy's own numbering the two models pair every placeholder in these
// captures with the same paste.)
func TestAgyPasteMutations(t *testing.T) {
	for _, m := range []struct {
		name   string
		break_ func(*profile)
	}{
		{"no paste rule", func(p *profile) { p.pastes = nil }},
		{"a rule that collapses nothing", func(p *profile) {
			p.pastes = slices.Clone(agy.pastes)
			p.pastes[0].collapses = func(string, int) bool { return false }
		}},
		// What agy keeps of a paste (holds) is checked in TestPasteAgy:
		// none of these captures collapses a paste with a tab or a line
		// break at its end, the two things it changes.
	} {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			p := agy
			m.break_(&p)
			if red := firstRedAgyReplay(t, &p); red == "" {
				t.Fatal("every agy replay stayed green")
			} else {
				t.Logf("red: %s", red)
			}
		})
	}
}

// agy's wrap, measured on 1.2.13 (accents, tall, scroll-steps): rows join
// back as typed, a word longer than a row breaks at the edge with no
// space, and a wide character that would cross the edge moves whole to
// the next row.
func TestAgyWrap(t *testing.T) {
	for _, c := range []struct {
		text  string
		width int
		rows  []string
		back  string
	}{
		// A word that would end exactly at the edge moves down whole, since
		// the space after it has to fit on the row too (wrap).
		{"aaaa bbbb cc dd", 12, []string{"aaaa bbbb", "cc dd"}, ""},
		{"aaaa bbbb cc", 12, []string{"aaaa bbbb cc"}, ""},
		// A word longer than a row breaks at the edge, and the space after
		// it starts the next row when the break falls right before it.
		{"xxxxxxxxxxxxxxx tail", 12, []string{"xxxxxxxxxxxx", "xxx tail"}, ""},
		{"xxxxxxxxxxxx tail", 12, []string{"xxxxxxxxxxxx", " tail"}, ""},
		// A wide character that would cross the edge moves whole to the
		// next row, and the two halves of the word join with nothing
		// (accents, at 117 columns).
		{"aaaaaaaaaa漢字", 12, []string{"aaaaaaaaaa漢", "字"}, ""},
		{"xxxxxxxxxxx ñan", 12, []string{"xxxxxxxxxxx", "ñan"}, ""},
		{"one\n\ntwo", 12, []string{"one", "", "two"}, ""},
	} {
		rows, _ := agy.unwrap.wrap(c.text, c.width)
		if !slices.Equal(rows, c.rows) {
			t.Errorf("%q: rows %q, want %q", c.text, rows, c.rows)
		}
		want := c.back
		if want == "" {
			want = c.text
		}
		if got := agy.unwrap.unwrap(rows, c.width); got != want {
			t.Errorf("%q: unwrapped as %q", c.text, got)
		}
	}
}

// The session id is the conversation whose database the process holds
// open, read from its open files: from lsof on macOS, from /proc on
// Linux. This test's own process holds them.
func TestAgySessionReadsTheHeldConversation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".gemini", "antigravity-cli", "conversations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	hold := func(name string) *os.File {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return f
	}
	a := "bdf5e6ed-5ad6-4c78-90d0-5643c376ca46"
	// A conversation nobody holds names nothing.
	os.WriteFile(filepath.Join(dir, a+".db"), nil, 0o644)
	pid := os.Getpid()
	agyHeld = heldFiles{}
	if id, found := agySession(pid, time.Now()); found || id != "" {
		t.Fatalf("no conversation held: %q, %v", id, found)
	}
	hold(a + ".db")
	// Its -wal and -shm name the same conversation, and are not another.
	hold(a + ".db-wal")
	hold(a + ".db-shm")
	agyHeld = heldFiles{}
	if id, found := agySession(pid, time.Now()); !found || id != a {
		t.Fatalf("one conversation held: %q, %v, want %q", id, found, a)
	}
	if pids := agySessionPids(); !slices.Contains(pids, pid) {
		t.Fatalf("holders %v, want this process %d", pids, pid)
	}
	tr := newSessionTracker(agy.session, pid, time.Now())
	if got := tr.current(); got != a {
		t.Fatalf("the tracker names %q, want %q", got, a)
	}
	// Two conversations open name none, and the tracker lets go of the one
	// it named.
	hold("24b5c0f1-0000-4000-8000-000000000000.db")
	agyHeld = heldFiles{}
	if id, found := agySession(pid, time.Now()); !found || id != sessionUnsure {
		t.Fatalf("two conversations held: %q, %v, want %q", id, found, sessionUnsure)
	}
	if got := tr.current(); got != "" {
		t.Fatalf("the tracker names %q for two held conversations", got)
	}
}

// agy's chat starts: the bare command, -c and --conversation <id>. A
// prompt argument, -i and -p send on start, and its subcommands open no
// box.
func TestAgyChat(t *testing.T) {
	for _, c := range []struct {
		args []string
		chat bool
	}{
		{nil, true},
		{[]string{"-c"}, true},
		{[]string{"--continue"}, true},
		{[]string{"--conversation", "bdf5e6ed-5ad6-4c78-90d0-5643c376ca46"}, true},
		{[]string{"--conversation=bdf5e6ed"}, true},
		{[]string{"--model", "gemini-3-pro", "-c"}, true},
		{[]string{"--sandbox", "--add-dir", "/tmp"}, true},
		{[]string{"fix the tests"}, false},
		{[]string{"-p", "fix the tests"}, false},
		{[]string{"--print", "x"}, false},
		{[]string{"-i", "a first prompt"}, false},
		{[]string{"--prompt-interactive", "x"}, false},
		{[]string{"mcp", "list"}, false},
		{[]string{"update"}, false},
		{[]string{"--help"}, false},
		{[]string{"--"}, false},
	} {
		if got := agyChat(c.args); got != c.chat {
			t.Errorf("agyChat(%q) = %v, want %v", c.args, got, c.chat)
		}
	}
}

// A draft with a tab or a line break at its end cannot go back into agy's
// box as it is: agy turns the tab into four spaces and drops the break.
func TestAgyFaithful(t *testing.T) {
	for _, c := range []struct{ draft, why string }{
		{"plain text\nover two lines", ""},
		{"ñandú 👍🏽 " + strings.Repeat("x", 2000), ""},
		{"a\tb", "tab"},
		{"a line\n", "line break"},
	} {
		got := agy.restore.faithful(c.draft)
		if (got == "") != (c.why == "") || c.why != "" && !strings.Contains(got, c.why) {
			t.Errorf("%.20q: %q, want %q", c.draft, got, c.why)
		}
	}
}

// agyRestoreRig is a restore rig for agy at 120x40, with no settle delay,
// in conversation conv. agy draws inline on the main screen, where the row
// under the box belongs to whatever the terminal held before it, so the
// rig leaves the alternate screen the shared rig starts on.
func agyRestoreRig(t *testing.T, conv string) *restoreRig {
	t.Helper()
	p := agy
	caps := *agy.restore
	caps.settle = 0
	p.restore = &caps
	r := newRestoreRig(t, &p, conv)
	r.rec.Agent = "agy"
	r.resize(120, 40)
	r.quietUntil = time.Time{}
	r.write([]byte("\x1b[?1049l"))
	return r
}

func readAgyRec(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(agyCaptures.folder, name+".rec"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// agyLongDraft is the 20-line draft restore-long puts back, which agy
// shows as "[Pasted text #1 +20 lines]".
func agyLongDraft() string {
	b, err := os.ReadFile(filepath.Join(agyCaptures.folder, "restore-long.editor-1.txt"))
	if err != nil {
		panic(err)
	}
	return string(b)
}

// Restore-in-box through agy's own frames: the draft a closed window left
// in orphan goes back into the box of agy -c once the box has been read
// empty, and is read back from the frames agy drew for the paste; the
// orphan then moves to history. The recorded paste is unsent's own, so
// the replay drops it and the restore under test makes it again. Nothing
// is drawn on the row under the box: on the main screen that row is the
// terminal's, and the line after exit says it instead.
func TestAgyRestoreReplays(t *testing.T) {
	for _, c := range []struct{ record, conv, draft string }{
		{"restore-c", "31cd68fa-6ec5-4c28-8ea8-f4723962edf2",
			"orphan draft for the restore, ñandú\nsecond line of the orphan"},
		{"restore-long", "dbe6f9e6-5f73-4181-a85d-8ed9662f09dd", agyLongDraft()},
	} {
		t.Run(c.record, func(t *testing.T) {
			r := agyRestoreRig(t, c.conv)
			seedOrphan(t, r.store, "old", "agy", c.conv, c.draft)
			paste := string(pasteStart) + c.draft + string(pasteEnd)
			data := readAgyRec(t, c.record)
			at := bytes.Index(data, []byte(paste))
			if at < 0 {
				t.Fatal("no restore paste in the record")
			}
			r.replayRestore(recordUntilKey(t, data, at+len(paste)), paste)
			if got := r.pasted(); got != paste {
				t.Fatalf("pasted %q, want %q", got, paste)
			}
			if orphanAt(r.store, "old") != nil || !inHistory(r.store, c.draft) {
				t.Fatal("the orphan was not read back and moved to history")
			}
			if r.rec.Draft != c.draft {
				t.Fatalf("the session's draft is %q, want the one put back", r.rec.Draft)
			}
			if term := r.term.String(); strings.Contains(term, "unsent:") {
				t.Fatalf("a line was drawn over agy's inline screen: %q", term)
			}
			if lines := r.restoreLines(); len(lines) != 1 || !strings.HasPrefix(lines[0], "unsent: put back your draft") {
				t.Fatalf("the lines after exit: %q", lines)
			}
		})
	}
}

// No restore goes into a new chat, which agy names nowhere until its first
// submit, nor into a box that is not agy's (the first-run screens), nor
// into a conversation whose draft belongs to another one.
func TestAgyNoRestore(t *testing.T) {
	for _, c := range []struct{ record, conv, orphan, why string }{
		{"new-chat", "", "31cd68fa-6ec5-4c28-8ea8-f4723962edf2", "a new chat, which has no conversation yet"},
		{"new-chat", "another-conversation", "31cd68fa-6ec5-4c28-8ea8-f4723962edf2", "a draft of another conversation"},
		{"first-run", "31cd68fa-6ec5-4c28-8ea8-f4723962edf2", "31cd68fa-6ec5-4c28-8ea8-f4723962edf2", "the first-run screens"},
	} {
		t.Run(c.why, func(t *testing.T) {
			r := agyRestoreRig(t, c.conv)
			seedOrphan(t, r.store, "old", "agy", c.orphan, "keep me")
			data := readAgyRec(t, c.record)
			if c.record == "first-run" {
				data = agyUntilBox(t, data)
			}
			r.replayRestore(data, "")
			if got := r.pasted(); strings.Contains(got, "keep me") {
				t.Fatalf("%s: pasted %q", c.why, got)
			}
			if orphanAt(r.store, "old") == nil {
				t.Fatalf("%s: the orphan is gone", c.why)
			}
		})
	}
}

// A check that cannot fail is not a check: with a session match that is
// always true, the draft of another conversation is pasted into this one.
func TestAgyRestoreCatchesAWrongSession(t *testing.T) {
	good := sessionMatch
	t.Cleanup(func() { sessionMatch = good })
	sessionMatch = func(orphan, running string) bool { return true }
	r := agyRestoreRig(t, "another-conversation")
	seedOrphan(t, r.store, "old", "agy", "31cd68fa-6ec5-4c28-8ea8-f4723962edf2", "keep me")
	r.replayRestore(readAgyRec(t, "new-chat"), "")
	if got := r.pasted(); !strings.Contains(got, "keep me") {
		t.Fatal("the always-true session match pasted nothing, so the new-chat test proves nothing")
	}
}

// agyUntilBox is a record cut at the output chunk that first draws the
// box's footer, with every chunk and tick before it.
func agyUntilBox(t *testing.T, data []byte) []byte {
	t.Helper()
	hint := []byte("? for shortcuts")
	for off := 0; off+24 <= len(data); {
		n := int(binary.LittleEndian.Uint64(data[off:]))
		if dir := data[off+20]; dir == 'o' && bytes.Contains(data[off+24:off+24+n], hint) {
			if off == 0 {
				t.Fatal("the box is in the first chunk")
			}
			return data[:off]
		}
		off += 24 + n
	}
	t.Fatal("the record never draws the box")
	return nil
}

// agyPaintCheck replays every agy capture's output through a shadow
// screen on the record's own clock, reads the box at each chunk boundary,
// and reports how many reads prof's frame marks held back and the first
// one they held back that the settled read after it contradicts.
func agyPaintCheck(t *testing.T, prof *profile) (held int, torn string) {
	t.Helper()
	recs, err := filepath.Glob(filepath.Join(agyCaptures.folder, "*.rec"))
	if err != nil || len(recs) == 0 {
		t.Fatalf("no agy captures: %v", err)
	}
	read := func(s *session) (text string, box bool) {
		v, ok := prof.read(snapshot(s.screen))
		if !ok || v.empty {
			return "", ok
		}
		return strings.Join(v.rows, "\n"), true
	}
	for _, path := range recs {
		name := strings.TrimSuffix(filepath.Base(path), ".rec")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		s := &session{screen: vt.NewEmulator(120, 40), prof: prof}
		done := drainScreen(t, s.screen)
		var now time.Time
		s.now = func() time.Time { return now }
		var out []recChunk
		eachChunk(t, data, func(at time.Time, dir byte, chunk []byte) {
			if dir == 'o' {
				out = append(out, recChunk{at, chunk})
			}
		})
		// Every read the marks held back waits for the settled read after
		// it, which says what agy was drawing.
		type pending struct {
			text string
			box  bool
		}
		var waiting []pending
		for i, c := range out {
			now = c.at
			for chunk := c.b; len(chunk) > 0; {
				k := len(chunk)
				if j := bytes.Index(chunk, frameEnd); j >= 0 {
					k = j + len(frameEnd)
				}
				s.write(chunk[:k])
				chunk = chunk[k:]
			}
			if i+1 < len(out) {
				now = out[i+1].at
			} else {
				now = now.Add(time.Second)
			}
			text, box := read(s)
			if s.drawing() {
				held++
				waiting = append(waiting, pending{text, box})
				continue
			}
			for _, w := range waiting {
				// A held-back read is safe when agy had not drawn the box
				// yet (look keeps the draft it has), or when it shows what
				// the settled read shows, whole or as far as it got.
				if w.box && !strings.HasPrefix(text, w.text) && torn == "" {
					torn = fmt.Sprintf("%s: a read held back mid-paint shows %q, the settled read after it %q",
						name, w.text, text)
				}
			}
			waiting = waiting[:0]
		}
		done()
	}
	return held, torn
}

// The paint marks and the quiet gap are the one piece of shared read-path
// machinery agy's profile adds, so they get a check that can fail. agy
// sends no synchronized-output marks; it brackets each paint with
// ESC[?25l … ESC[?25h, and a read inside one lands on a screen it has not
// finished drawing. Over every capture, the reads the marks hold back are
// screens where agy has not drawn the box yet, never a torn draft.
func TestAgyPaintMarksHoldBackHalfDrawnScreens(t *testing.T) {
	held, torn := agyPaintCheck(t, &agy)
	if torn != "" {
		t.Fatal(torn)
	}
	if held == 0 {
		t.Fatal("the paint marks and the quiet gap held back no read in any capture")
	}
	t.Logf("%d reads held back across the captures", held)
}

// Both ends of the gap go red. With no paint marks, or with no gap to
// wait out, nothing is held back and every save reads the screen the
// instant a chunk lands. With a gap of 2 s, agy's cursor stays hidden
// while a dialog is on screen for longer than any pause in the record, so
// the frames the negative screens need never settle.
func TestAgyPaintMarkMutations(t *testing.T) {
	for _, m := range []struct {
		name   string
		mutate func(*profile)
	}{
		{"no paint marks", func(p *profile) { p.hidesCursor = false }},
		{"no quiet gap", func(p *profile) { p.quietGap = 0 }},
	} {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			p := agy
			m.mutate(&p)
			if held, _ := agyPaintCheck(t, &p); held != 0 {
				t.Fatalf("with %s, %d reads were still held back", m.name, held)
			}
		})
	}
	t.Run("a quiet gap of 2 s", func(t *testing.T) {
		t.Parallel()
		shows := func(prof *profile, want string) bool {
			for _, f := range captureFrames(t, prof, agyCaptures.folder, "dialogs") {
				if strings.Contains(f.String(), want) {
					return true
				}
			}
			return false
		}
		const panel = "Keyboard Shortcuts"
		if !shows(&agy, panel) {
			t.Fatalf("no frame of dialogs shows %q at agy's own gap", panel)
		}
		p := agy
		p.quietGap = 2 * time.Second
		if shows(&p, panel) {
			t.Fatalf("with a 2 s gap a frame of dialogs still shows %q", panel)
		}
	})
}

// The settle delay and the number of empty reads a restore waits for are
// measured (docs/research/agy.md, "Restore", and docs/research/codex.md).
// Both rigs set settle to 0 so their tests need no wall clock, so nothing
// else keeps these two from being whittled away, and they are what stops
// a paste landing in a box the user is already typing in.
func TestAgyAndCodexSettleKeepTheirMargins(t *testing.T) {
	for _, p := range []*profile{&agy, &codex} {
		if p.restore.settle < time.Second || p.restore.empties < 3 {
			t.Errorf("%s waits %v and %d empty reads", p.name, p.restore.settle, p.restore.empties)
		}
	}
}

// agy keeps one database per conversation for ever, so the folder has no
// bound. Asking lsof about each file in it answered "nobody holds
// anything" long before the folder was large: past this timeout from
// about 3,000 conversations on a slow disk, and past the kernel's
// argument limit from about 8,000 wherever they live. Either way a draft
// got no agent_session under a launcher that does not exec agy, and
// nothing said so.
func TestAgySessionPidsAtManyConversations(t *testing.T) {
	if testing.Short() {
		t.Skip("makes several thousand files")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".gemini", "antigravity-cli", "conversations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const conversations = 8000
	for i := range conversations {
		name := fmt.Sprintf("019a%04d-4d7e-7c3a-9f1b-%012d.db", i, i)
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	held := fmt.Sprintf("019a%04d-4d7e-7c3a-9f1b-%012d.db", conversations/2, conversations/2)
	f, err := os.Open(filepath.Join(dir, held))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	pid := os.Getpid()
	if pids := agySessionPids(); !slices.Contains(pids, pid) {
		t.Fatalf("with %d conversations, holders %v, want this process %d", conversations, pids, pid)
	}
}
