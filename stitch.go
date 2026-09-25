package main

import (
	"regexp"
	"strings"

	"github.com/mattn/go-runewidth"
)

// stitcher rebuilds a draft that is taller than the input box.
//
// A box at its maximum height scrolls: the screen shows only a window of
// the draft's rows. Each new window is lined up against the rows already
// known, and replaces the part it covers. Rows above and below it that are
// out of sight are kept.
type stitcher struct {
	rows  []string // every row of the draft known so far, at width
	width int
	top   int // where the last window started in rows
	h     int // the last window's height
}

func (st *stitcher) reset() {
	*st = stitcher{}
}

// update merges a new view and returns the draft's text.
func (st *stitcher) update(v view) string {
	if v.empty {
		st.reset()
		return ""
	}
	if st.width != 0 && v.width != st.width && len(st.rows) > 0 {
		// The window was resized: rows wrap differently now. Re-wrap what we
		// know so it can still be lined up against the new windows.
		st.rows = wrapText(unwrap(st.rows, st.width), v.width)
		st.top = 0
	}
	st.width = v.width
	if !v.capped || len(st.rows) == 0 {
		// The whole draft is on screen.
		st.rows = append([]string(nil), v.rows...)
		st.top, st.h = 0, len(v.rows)
		return unwrap(st.rows, st.width)
	}
	p, q := st.align(v)
	merged := make([]string, 0, p+len(v.rows)+len(st.rows)-q)
	merged = append(merged, st.rows[:p]...)
	merged = append(merged, v.rows...)
	merged = append(merged, st.rows[q:]...)
	st.rows, st.top, st.h = merged, p, len(v.rows)
	return unwrap(st.rows, st.width)
}

// align finds which known rows the window replaces: rows[p:q].
//
// Rows above the edit in the window match known rows from p on; rows below
// it match known rows ending at q. The pair that explains the most rows
// wins, and a tie goes to the spot nearest the previous window.
func (st *stitcher) align(v view) (int, int) {
	w, n, h := v.rows, len(st.rows), len(v.rows)
	bestP, bestQ, best, bestDist := st.top, -1, 0, 1<<30

	consider := func(p, q, score int) {
		d := abs(p - st.top)
		if score > best || (score == best && score > 0 && d < bestDist) {
			bestP, bestQ, best, bestDist = p, q, score, d
		}
	}
	for p := 0; p < n; p++ {
		if st.rows[p] != w[0] {
			continue
		}
		head := 0
		for head < h && p+head < n && st.rows[p+head] == w[head] {
			head++
		}
		q, tail := st.tailFrom(p+head, w[head:])
		if tail == 0 {
			q = min(n, p+h)
		}
		consider(p, q, head+tail)
	}
	// The edit may be on the window's first row: anchor on the rows below it.
	for q := n; q > 0; q-- {
		if st.rows[q-1] != w[h-1] {
			continue
		}
		tail := 0
		for tail < h && q-1-tail >= 0 && st.rows[q-1-tail] == w[h-1-tail] {
			tail++
		}
		consider(max(0, q-h), q, tail)
	}
	if bestQ < 0 {
		// Nothing lines up (a big edit reflowed the whole window): assume
		// the window did not move.
		bestP = min(st.top, n)
		bestQ = min(n, bestP+st.h)
	}
	// The window moved up while the cursor stayed on its last row: rows at
	// the end of the draft were deleted, not scrolled past.
	if v.cursor == h-1 && bestP < st.top {
		bestQ = max(bestQ, min(n, st.top+st.h))
	}
	return bestP, bestQ
}

// tailFrom lines up the end of rest against known rows at or after from and
// returns the end of the match and how many rows matched.
func (st *stitcher) tailFrom(from int, rest []string) (int, int) {
	if len(rest) == 0 {
		return from, 0
	}
	last := rest[len(rest)-1]
	bestQ, best := from, 0
	for q := from + 1; q <= len(st.rows); q++ {
		if st.rows[q-1] != last {
			continue
		}
		k := 0
		for k < len(rest) && q-1-k >= from && st.rows[q-1-k] == rest[len(rest)-1-k] {
			k++
		}
		if k > best {
			bestQ, best = q, k
		}
	}
	return bestQ, best
}

// unwrap joins rows that the agent wrapped back into the lines typed.
//
// A row was wrapped when the next row's first word would not have fitted
// after it; the space the wrap swallowed is put back. A row that fills the
// whole width with no space in it was a long word broken mid-way.
//
// The screen cannot tell a wrap from a line the user ended by hand right
// where the row happened to be full. That case comes back joined with a
// space, unless the next row starts a list item, heading, quote or code
// fence.
func unwrap(rows []string, width int) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(rows[0])
	for i := 1; i < len(rows); i++ {
		prev, next := rows[i-1], rows[i]
		pw := runewidth.StringWidth(prev)
		switch {
		case width > 0 && pw >= width && !strings.Contains(prev, " ") && next != "":
			// A word longer than a row, broken at the edge.
		case width > 0 && next != "" && prev != "" && !startsBlock(next) &&
			pw+1+runewidth.StringWidth(firstWord(next)) > width:
			b.WriteByte(' ')
		default:
			b.WriteByte('\n')
		}
		b.WriteString(next)
	}
	return b.String()
}

// wrapText wraps text the way the agent does: greedy, at word boundaries,
// breaking words longer than a row.
func wrapText(text string, width int) []string {
	var rows []string
	for _, line := range strings.Split(text, "\n") {
		if width <= 0 || runewidth.StringWidth(line) <= width {
			rows = append(rows, line)
			continue
		}
		cur := ""
		for _, word := range strings.Split(line, " ") {
			for runewidth.StringWidth(word) > width {
				if cur != "" {
					rows = append(rows, cur)
					cur = ""
				}
				head := runewidth.Truncate(word, width, "")
				rows = append(rows, head)
				word = word[len(head):]
			}
			switch {
			case cur == "":
				cur = word
			case runewidth.StringWidth(cur)+1+runewidth.StringWidth(word) <= width:
				cur += " " + word
			default:
				rows = append(rows, cur)
				cur = word
			}
		}
		rows = append(rows, cur)
	}
	return rows
}

var blockStart = regexp.MustCompile("^(?:[-*+>#|] |\\d+[.)] |```|#+ )")

func startsBlock(row string) bool {
	return blockStart.MatchString(row)
}

func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return s
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
