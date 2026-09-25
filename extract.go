package main

import (
	"path/filepath"
	"strings"

	"github.com/mattn/go-runewidth"
)

// view is what an extractor sees of the input box on one screen.
type view struct {
	// rows are the box's visible rows, prefix stripped, trailing spaces trimmed.
	rows []string
	// cursor is the row index of the cursor inside rows, or -1, and
	// cursorEnd whether it sits after the last character of its row.
	cursor    int
	cursorEnd bool
	// width is the column at which the agent wraps a row.
	width int
	// capped is true when the box is at its maximum height, so it may be
	// scrolled and rows above or below may be out of sight.
	capped bool
	// empty is true when the box holds no draft (only a dim hint, or nothing).
	empty bool
	// deleted is how many characters the delete keys pressed since the
	// last view can have removed (unlimited after Ctrl+W, Ctrl+U, Ctrl+K
	// or undo), and deletedAhead whether one removes text after the cursor
	// (Delete, Ctrl+K, undo). The screen alone cannot tell text deleted at
	// the edge of the box from text moved out of sight.
	deleted      int
	deletedAhead bool
}

// An extractor finds the agent's input box on the screen. ok is false when
// the box is not visible (a menu, a permission prompt, a full-screen editor).
type extractor func(s *screen) (v view, ok bool)

func extractorFor(command string) extractor {
	switch filepath.Base(command) {
	case "claude":
		return claudeBox
	default:
		return nil
	}
}

// claudeBox reads Claude Code's input box. It is drawn as:
//
//	────────────────────
//	❯ first row of the draft
//	  following rows, indented two columns
//	────────────────────
//
// The marker is followed by a no-break space (U+00A0). An empty box shows a
// dim hint after it. In shell mode the marker is "!".
// The box grows up to half the window height minus five rows, then scrolls
// inside itself. Rows wrap at the window width minus four columns.
func claudeBox(s *screen) (view, bool) {
	for y := len(s.rows) - 2; y >= 1; y-- {
		first := s.rows[y].text()
		shell := hasMarker(first, "!")
		if !shell && !hasMarker(first, "❯") {
			continue
		}
		if !isRule(s.rows[y-1].text(), s.cols) {
			continue
		}
		end := -1
		for z := y + 1; z < len(s.rows); z++ {
			if isRule(s.rows[z].text(), s.cols) {
				end = z
				break
			}
		}
		if end < 0 {
			continue
		}
		v := view{cursor: -1, width: s.cols - 4}
		v.capped = end-y >= s.rows2cap()
		if (s.rows[y].textFrom(2) == "" || s.rows[y].faintFrom(2)) && end == y+1 {
			v.empty = true
			return v, true
		}
		for z := y; z < end; z++ {
			v.rows = append(v.rows, s.rows[z].textFrom(2))
		}
		if s.curY >= y && s.curY < end {
			v.cursor = s.curY - y
			v.cursorEnd = s.curX >= 2+runewidth.StringWidth(v.rows[v.cursor])
		}
		if shell {
			v.rows[0] = "!" + v.rows[0]
		}
		return v, true
	}
	return view{}, false
}

// hasMarker reports whether a row starts with marker and a space, or is
// only the marker.
func hasMarker(row, marker string) bool {
	rest, ok := strings.CutPrefix(row, marker)
	return ok && (rest == "" || strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\u00a0"))
}

// rows2cap is the height at which Claude Code's box stops growing. It is one
// row lower than the measured cap (rows/2 - 5): treating a box that is not
// scrolled as scrolled only costs a little precision, while the opposite
// would drop the rows scrolled out of sight.
func (s *screen) rows2cap() int {
	c := len(s.rows)/2 - 6
	if c < 1 {
		c = 1
	}
	return c
}

// isRule reports whether a row is a horizontal line across most of the window.
func isRule(t string, cols int) bool {
	n := 0
	for _, r := range t {
		if r != '─' {
			return false
		}
		n++
	}
	return n >= cols/2
}
