package main

import (
	"bytes"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

// screen is a plain snapshot of the shadow terminal: the text of every row,
// which cells are drawn dim, and where the cursor is.
type screen struct {
	rows       []screenRow
	cols       int
	curX, curY int
}

// screenRow holds one string per terminal column. The right half of a wide
// character is an empty string, so column indexes stay true.
type screenRow struct {
	cells []string
	faint []bool
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
	s := &screen{rows: make([]screenRow, h), cols: w}
	for y := 0; y < h; y++ {
		row := screenRow{cells: make([]string, w), faint: make([]bool, w)}
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
		row := screenRow{cells: make([]string, cols), faint: make([]bool, cols)}
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
