package main

import (
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/mattn/go-runewidth"
)

// claude is Claude Code's profile.
var claude = profile{
	name:  "claude",
	names: []string{"claude"},
	read:  claudeBox,
	// A paste shows as "[Pasted text #2 +39 lines]", or "[Pasted text #2]"
	// when it has no line break; the count is of line breaks. Claude Code
	// first turns each tab into 4 spaces, then collapses a paste over 800
	// UTF-16 units, or with more than 2 line breaks, or than the window
	// height minus 10 when that is less. Anything shorter goes in as typed
	// text, and a pasted image path becomes "[Image #N]", so neither fills
	// a placeholder. "[Image #N]" and "[Audio #N]" hold no text and stay
	// as they are. N counts up across pastes, images and cut middles.
	placeholder: regexp.MustCompile(`\[Pasted text #(\d+)(?: \+(\d+) lines?)?\]`),
	collapses: func(paste string, rows int) bool {
		paste = strings.ReplaceAll(paste, "\t", "    ")
		return len(utf16.Encode([]rune(paste))) > 800 || strings.Count(paste, "\n") > max(0, min(rows-10, 2))
	},
	// A box over 10,000 characters keeps its first and last 500 and shows
	// the middle as "[...Truncated text #N +M lines...]".
	truncated: regexp.MustCompile(`\[\.\.\.Truncated text #(\d+) \+(\d+) lines\.\.\.\]`),
	// Backspace, Ctrl+H, Delete and Ctrl+D remove one character; Ctrl+W,
	// Ctrl+U, Ctrl+K, Alt+Backspace, Alt+D and undo (Ctrl+_) any amount.
	// Delete, Ctrl+D, Ctrl+K, Alt+D and undo can remove text after the
	// cursor. Enter sends the box (Ctrl+X Enter queues it, which also ends
	// in Enter); Esc+Enter makes a new line. Ctrl+C and Esc Esc clear it.
	keys: keyset{
		one:    [][]byte{{0x7f}, {0x08}, []byte("\x1b[3~"), {0x04}},
		many:   [][]byte{{0x17}, {0x15}, {0x0b}, {0x1f}, {0x1b, 0x7f}, []byte("\x1bd")},
		ahead:  [][]byte{[]byte("\x1b[3~"), {0x04}, {0x0b}, {0x1f}, []byte("\x1bd")},
		submit: [][]byte{{'\r'}},
		clear:  [][]byte{{0x03}, []byte("\x1b\x1b")},
	},
	// The fixtures in testdata/claude; `claude --version` prints
	// "2.1.282 (Claude Code)".
	verified: "2.1.282",
	version:  []string{"--version"},
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
