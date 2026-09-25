package main

import (
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
