package main

import (
	"bytes"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// The snapshot's cell looks, checked on real Codex 0.158.0 frames
// (testdata/codex/0.158.0): what docs/research/codex.md says a Codex
// reader tells the box, its hint, a menu and a sent message apart by.

// codexFrames replays the output of a Codex record through the shadow
// screen and returns a snapshot at the end of every frame, in order.
func codexFrames(t *testing.T, name string) []*screen {
	t.Helper()
	return captureFrames(t, &codex, codexCaptures.folder, name)
}

// frameSet holds the frames of the captures one test reads, so a record
// several checks share is decoded once. It is a field of the test, not a
// package cache: a run holds hundreds of frames of a 120x40 screen, and
// keeping every capture's for the whole suite took the race run's peak
// memory from 2.2 GB to 5.0 GB.
// Frames are read-only to their holder, which clones one before restyling
// it, and decoding is the emulator's work alone: no reader, and no layout
// a mutant changes, is involved, so the mutation tests may share one set
// across their mutants.
type frameSet map[string][]*screen

// get returns folder/name.rec's frames, decoding them the first time.
func (f frameSet) get(t *testing.T, prof *profile, folder, name string) []*screen {
	t.Helper()
	if f[name] == nil {
		f[name] = captureFrames(t, prof, folder, name)
	}
	return f[name]
}

// captureFrames replays folder/name.rec's output through a shadow screen
// and returns the screen after every complete frame.
func captureFrames(t *testing.T, prof *profile, folder, name string) []*screen {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(folder, name+".rec"))
	if err != nil {
		t.Fatal(err)
	}
	s := &session{screen: vt.NewEmulator(120, 40), prof: prof}
	defer drainScreen(t, s.screen)()
	var frames []*screen
	// The record's own clock, and each chunk read at the moment the next
	// one arrives, as a save loop between two chunks does: a profile that
	// reads after a quiet gap (agy) then sees the gaps the agent left.
	var now time.Time
	s.now = func() time.Time { return now }
	var out []recChunk
	eachChunk(t, data, func(at time.Time, dir byte, chunk []byte) {
		if dir == 'o' {
			out = append(out, recChunk{at, chunk})
		}
	})
	for i, c := range out {
		now = c.at
		chunk := c.b
		for len(chunk) > 0 {
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
		if !s.drawing() {
			frames = append(frames, snapshot(s.screen))
		}
	}
	if s.broken.Load() {
		t.Fatal("the shadow screen broke")
	}
	return frames
}

// recChunk is one chunk of a record: when it arrived and its bytes.
type recChunk struct {
	at time.Time
	b  []byte
}

// lastFrame returns the last frame whose text holds every want.
func lastFrame(t *testing.T, frames []*screen, want ...string) *screen {
	t.Helper()
	for i := len(frames) - 1; i >= 0; i-- {
		text, all := frames[i].String(), true
		for _, w := range want {
			all = all && strings.Contains(text, w)
		}
		if all {
			return frames[i]
		}
	}
	t.Fatalf("no frame shows %q", want)
	return nil
}

// codexAnchor returns the lowest row that starts with Codex's › glyph.
func codexAnchor(t *testing.T, s *screen) int {
	t.Helper()
	for y := len(s.rows) - 1; y >= 0; y-- {
		if s.rows[y].cells[0] == "›" {
			return y
		}
	}
	t.Fatalf("no › row:\n%s", s)
	return -1
}

func palette(i uint8) cellColor { return colorOf(ansi.IndexedColor(i)) }

// With the terminal's colours known (tmux answered OSC 10 and 11), Codex
// tints its box: the top padding, every text row and the bottom padding
// share one background from edge to edge, and the rows around it have
// none. The glyph is bold and not dim, the hint dim and not bold.
func TestSnapshotCodexTintedBox(t *testing.T) {
	frames := codexFrames(t, "tinted")
	tint := palette(237) // ESC[48;5;237m, screens/tinted.txt
	edges := func(s *screen, y int) (cellColor, cellColor) {
		return s.rows[y].look[0].bg, s.rows[y].look[s.cols-1].bg
	}
	tinted := func(s *screen, top, bottom int) {
		t.Helper()
		for y := top; y <= bottom; y++ {
			if l, r := edges(s, y); l != tint || r != tint {
				t.Errorf("row %d %q: background %#x and %#x at its edges, want the tint %#x", y, s.rows[y].text(), l, r, tint)
			}
		}
		for _, y := range []int{top - 1, bottom + 1, bottom + 2} {
			if l, r := edges(s, y); l != 0 || r != 0 {
				t.Errorf("row %d %q outside the box: background %#x and %#x, want none", y, s.rows[y].text(), l, r)
			}
		}
	}
	glyph := func(s *screen, y int) {
		t.Helper()
		if l := s.rows[y].look[0]; !l.bold || l.reverse || s.rows[y].faint[0] {
			t.Errorf("the box's › is %+v, dim %v; want bold, not reversed, not dim", l, s.rows[y].faint[0])
		}
	}

	empty := lastFrame(t, frames, "Ask Codex to do anything")
	y := codexAnchor(t, empty)
	glyph(empty, y)
	tinted(empty, y-1, y+1)
	if hint := empty.rows[y]; !hint.faintFrom(2) || hint.look[2].bold || hint.look[2].bg != tint {
		t.Errorf("hint row %q: dim %v, look %+v; want dim, not bold, on the tint", hint.text(), hint.faintFrom(2), hint.look[2])
	}
	if !empty.alt {
		t.Error("Codex 0.158.0 draws its box on the alternate screen")
	}

	draft := lastFrame(t, frames, "tinted draft, dummy text only\n  second row")
	y = codexAnchor(t, draft)
	glyph(draft, y)
	tinted(draft, y-1, y+2)
	for _, r := range draft.rows[y : y+2] {
		if r.faintFrom(2) || r.look[2].bold {
			t.Errorf("draft row %q is drawn as a hint: %+v", r.text(), r.look[2])
		}
	}

	if last := frames[len(frames)-1]; last.alt {
		t.Error("Codex left the alternate screen on exit")
	}
}

// Without the terminal's colours (tmux answered neither OSC 10 nor 11),
// nothing is tinted; the glyph and hint keep their looks, and the footer
// keeps its colours.
func TestSnapshotCodexUntintedBox(t *testing.T) {
	s := lastFrame(t, codexFrames(t, "typed"), "Ask Codex to do anything")
	y := codexAnchor(t, s)
	if l := s.rows[y].look[0]; !l.bold || s.rows[y].faint[0] || !s.rows[y].faintFrom(2) {
		t.Errorf("glyph %+v, hint dim %v", l, s.rows[y].faintFrom(2))
	}
	for i, r := range s.rows {
		for x, l := range r.look {
			if l.bg != 0 {
				t.Fatalf("row %d col %d has background %#x with no tint", i, x, l.bg)
			}
		}
	}
	// ESC[38;5;223m model, ESC[38;5;151m folder (screens/typed-empty.txt).
	for _, c := range []struct {
		text string
		fg   cellColor
	}{{"GPT-6-Astra", palette(223)}, {"/Volumes/", palette(151)}} {
		found := false
		for _, r := range s.rows[y+1:] {
			if x := strings.Index(strings.Join(r.cells, ""), c.text); x >= 0 {
				found = true
				// The row is ASCII up to here but for the one-cell "·".
				x = len([]rune(strings.Join(r.cells, "")[:x]))
				if r.look[x].fg != c.fg {
					t.Errorf("%q in the footer is colour %#x, want %#x", c.text, r.look[x].fg, c.fg)
				}
			}
		}
		if !found {
			t.Errorf("no %q in the footer", c.text)
		}
	}
}

// A menu's chosen row starts with the same bold › as the box, reversed;
// a sent message in history with a bold › that is also dim.
func TestSnapshotCodexMenuAndHistory(t *testing.T) {
	menu := lastFrame(t, codexFrames(t, "dialogs"), "› /model")
	chosen, box := -1, codexAnchor(t, menu)
	for y, r := range menu.rows {
		if strings.HasPrefix(r.text(), "› /model") {
			chosen = y
		}
	}
	if chosen < 0 || chosen >= box {
		t.Fatalf("menu row %d, box row %d:\n%s", chosen, box, menu)
	}
	if l := menu.rows[chosen].look[0]; !l.bold || !l.reverse {
		t.Errorf("chosen menu row's › is %+v, want bold and reversed", l)
	}
	if l := menu.rows[box].look[0]; !l.bold || l.reverse {
		t.Errorf("box's › under the menu is %+v, want bold, not reversed", l)
	}

	turn := lastFrame(t, codexFrames(t, "submit"), "› dummy prompt that must fail", "Ask Codex to do anything")
	sent, box := -1, codexAnchor(t, turn)
	for y, r := range turn.rows {
		if strings.HasPrefix(r.text(), "› dummy prompt that must fail") {
			sent = y
		}
	}
	if sent < 0 || sent >= box {
		t.Fatalf("sent row %d, box row %d:\n%s", sent, box, turn)
	}
	if r := turn.rows[sent]; !r.look[0].bold || !r.faint[0] {
		t.Errorf("sent message's › is %+v, dim %v; want bold and dim", r.look[0], r.faint[0])
	}
	if r := turn.rows[box]; !r.look[0].bold || r.faint[0] {
		t.Errorf("box's › during the turn is %+v, dim %v; want bold, not dim", r.look[0], r.faint[0])
	}
}

func TestColorOf(t *testing.T) {
	for _, c := range []struct {
		in   color.Color
		want cellColor
	}{
		{nil, 0},
		{ansi.BasicColor(6), 1<<24 | 6},
		{ansi.IndexedColor(6), 1<<24 | 6}, // ESC[36m and ESC[38;5;6m are one colour
		{ansi.IndexedColor(237), 1<<24 | 237},
		{color.RGBA{R: 0x39, G: 0x39, B: 0x47, A: 0xff}, 2<<24 | 0x393947}, // ESC[48;2;57;57;71m
		{color.RGBA{A: 0xff}, 2 << 24},                                     // black is not the default
	} {
		if got := colorOf(c.in); got != c.want {
			t.Errorf("colorOf(%v) = %#x, want %#x", c.in, got, c.want)
		}
	}
}
