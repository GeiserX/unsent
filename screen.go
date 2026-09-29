package main

import (
	"bytes"
	"image/color"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// screen is a plain snapshot of the shadow terminal: the text of every row,
// how each cell is drawn, where the cursor is, and whether the agent is on
// the alternate screen.
type screen struct {
	rows       []screenRow
	cols       int
	curX, curY int
	alt        bool
}

// screenRow holds one string per terminal column. The right half of a wide
// character is an empty string, so column indexes stay true. faint marks
// the dim cells; look holds the rest of each cell's style, for readers that
// need more than dim to tell the box from what is drawn around it.
type screenRow struct {
	cells []string
	faint []bool
	look  []cellLook
}

// cellLook is how one cell is drawn, besides dim. Codex marks the chosen
// row of a menu with reverse video, starts its box with a bold glyph and,
// when the terminal tells it its colours, tints the box's background
// (docs/research/codex.md).
type cellLook struct {
	fg, bg        cellColor
	bold, reverse bool
}

// cellColor is a cell's colour: 0 for the terminal's default, 1<<24 plus
// the index for a palette colour (the 16 basic ones included), 2<<24 plus
// 0xRRGGBB for any other.
type cellColor uint32

func colorOf(c color.Color) cellColor {
	switch c := c.(type) {
	case nil:
		return 0
	case ansi.BasicColor:
		return 1<<24 | cellColor(c)
	case ansi.IndexedColor:
		return 1<<24 | cellColor(c)
	}
	r, g, b, _ := c.RGBA()
	return 2<<24 | cellColor(r>>8)<<16 | cellColor(g>>8)<<8 | cellColor(b>>8)
}

func (r screenRow) text() string {
	return strings.TrimRight(strings.Join(r.cells, ""), " ")
}

// textFrom returns the row's text from column x on.
func (r screenRow) textFrom(x int) string {
	if x >= len(r.cells) {
		return ""
	}
	return strings.TrimRight(strings.Join(r.cells[x:], ""), " ")
}

// faintFrom reports whether every visible character from column x on is
// dim. Agents draw their empty-box hint this way.
func (r screenRow) faintFrom(x int) bool {
	seen := false
	for i := x; i < len(r.cells); i++ {
		if strings.TrimSpace(r.cells[i]) == "" {
			continue
		}
		if !r.faint[i] {
			return false
		}
		seen = true
	}
	return seen
}

func snapshot(e *vt.Emulator) *screen {
	w, h := e.Width(), e.Height()
	s := &screen{rows: make([]screenRow, h), cols: w, alt: e.IsAltScreen()}
	// A save snapshots the screen after every frame: one array of each for
	// the whole screen, not three per row, keeps that cheap.
	cells, faint, look := make([]string, w*h), make([]bool, w*h), make([]cellLook, w*h)
	for y := 0; y < h; y++ {
		i, j := y*w, (y+1)*w
		row := screenRow{cells: cells[i:j:j], faint: faint[i:j:j], look: look[i:j:j]}
		for x := 0; x < w; x++ {
			c := e.CellAt(x, y)
			switch {
			case c == nil:
				row.cells[x] = " "
			case c.Width == 0:
				row.cells[x] = ""
			case c.Content == "":
				row.cells[x] = " "
			default:
				row.cells[x] = c.Content
				row.faint[x] = c.Style.Attrs&uv.AttrFaint != 0
			}
			// A blank cell has a look too: a tinted row is mostly blanks. Most
			// cells have none, so test the four fields the look keeps rather
			// than compare the whole style.
			if c == nil {
				continue
			}
			if st := &c.Style; st.Fg != nil || st.Bg != nil || st.Attrs&(uv.AttrBold|uv.AttrReverse) != 0 {
				row.look[x] = cellLook{
					fg:      colorOf(st.Fg),
					bg:      colorOf(st.Bg),
					bold:    st.Attrs&uv.AttrBold != 0,
					reverse: st.Attrs&uv.AttrReverse != 0,
				}
			}
		}
		s.rows[y] = row
	}
	p := e.CursorPosition()
	s.curX, s.curY = p.X, p.Y
	return s
}

func (s *screen) String() string {
	var b strings.Builder
	for _, r := range s.rows {
		b.WriteString(r.text())
		b.WriteByte('\n')
	}
	return b.String()
}

// screenFromText builds a screen from plain text, for tests and fixtures.
// Text between "⟦" and "⟧" is drawn dim and "▮" marks the cursor; the
// markers take no column.
func screenFromText(text string, cols int) *screen {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	s := &screen{cols: cols, curX: -1, curY: -1}
	for y, l := range lines {
		row := screenRow{cells: make([]string, cols), faint: make([]bool, cols), look: make([]cellLook, cols)}
		for i := range row.cells {
			row.cells[i] = " "
		}
		x, dim := 0, false
		for _, r := range l {
			switch r {
			case '⟦':
				dim = true
				continue
			case '⟧':
				dim = false
				continue
			case '▮':
				s.curX, s.curY = x, y
				continue
			}
			if x < cols {
				row.cells[x] = string(r)
				row.faint[x] = dim
			}
			x++
		}
		s.rows = append(s.rows, row)
	}
	return s
}

// stringSeqs strips terminal string sequences (OSC, DCS, APC, PM and SOS)
// from the agent's output before the shadow screen sees it. They carry
// window titles, links and queries, never text in cells. The emulator ends
// one at a 0x9c byte even inside a UTF-8 character, so the ✳ (e2 9c b3)
// Claude Code puts in its window title after a send printed the rest of
// the title into the box. The state carries over from chunk to chunk.
type stringSeqs struct {
	state int
}

const (
	seqText  = iota // outside a string sequence
	seqEsc          // after an Esc that may start one
	seqIn           // inside one
	seqInEsc        // after an Esc inside one, which may end it
)

func (f *stringSeqs) strip(b []byte) []byte {
	if f.state == seqText && bytes.IndexByte(b, 0x1b) < 0 {
		return b
	}
	out := make([]byte, 0, len(b)+1)
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch f.state {
		case seqText:
			if c == 0x1b {
				f.state = seqEsc
			} else {
				out = append(out, c)
			}
		case seqEsc:
			switch c {
			case ']', 'P', '_', '^', 'X':
				f.state = seqIn
			case 0x1b:
				out = append(out, 0x1b)
			default:
				out = append(out, 0x1b, c)
				f.state = seqText
			}
		case seqIn:
			switch c {
			case 0x07, 0x18, 0x1a: // BEL ends an OSC; CAN and SUB cancel any
				f.state = seqText
			case 0x1b:
				f.state = seqInEsc
			}
		case seqInEsc:
			if c == '\\' {
				f.state = seqText
				continue
			}
			// Any other byte after the Esc cancels the string and starts a
			// new sequence.
			f.state = seqEsc
			i--
		}
	}
	return out
}
