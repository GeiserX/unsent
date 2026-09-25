package main

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// stitcher rebuilds a draft that is taller than the input box.
//
// A box at its maximum height scrolls: the screen shows only part of the
// draft. Each view is lined up, word by word, against the text already
// known: it replaces the stretch of old words it is the fewest word edits
// away from, and the text above and below, out of sight, is kept.
//
// Words, not rows or characters: typing in the middle of a paragraph
// reflows every row below it, including hidden ones, but leaves the words
// alone. A wrap and a typed line break both just separate two words.
type stitcher struct {
	text string // the whole draft as far as we know it
	a, b int    // where the last view sat in text
	last *view  // the last view merged, to skip work when nothing changed
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
	if st.last != nil && sameView(*st.last, v) {
		// The agent redrew (a spinner, a status line) but the box did not
		// change: nothing to line up.
		return st.text
	}
	st.last = &v
	cur := unwrap(v.rows, v.width)
	if !v.capped || st.text == "" {
		// The whole draft is on screen.
		return st.replace(cur)
	}
	old, seen := words(st.text), words(cur)
	if len(old) == 0 || len(seen) == 0 {
		return st.replace(cur)
	}
	p, q, shared := align(old, seen, st.a, st.b, v.width, v.deleted)
	if q == p || shared == 0 || (len(seen) >= 2*minShared && shared < minShared) {
		// Next to nothing in common: a different text, such as a prompt
		// recalled from history. Start over from the screen; the caller
		// archives the old draft. (A view full of new words is fine: a
		// paste or fast typing, as long as some known text lines up.)
		return st.replace(cur)
	}
	a, b := old[p].start, old[q-1].end

	// A word longer than a row is broken across rows, so the box can cut
	// it at its top or bottom edge: keep the part out of sight.
	long := func(w word) bool { return runewidth.StringWidth(w.s) > v.width }
	if f := seen[0].s; long(old[p]) && f != old[p].s && strings.HasSuffix(old[p].s, f) {
		a = old[p].end - len(f)
	}
	if l := seen[len(seen)-1].s; long(old[q-1]) && l != old[q-1].s && strings.HasPrefix(old[q-1].s, l) {
		b = old[q-1].start + len(l)
	}
	// Words the last view showed and this one does not were deleted, or
	// only moved out of sight. The keys pressed decide.
	//
	// Above the view: Backspace deletes before the cursor, so words gone
	// from the top were deleted when the cursor sits on the top row. With
	// the cursor elsewhere the view scrolled (typing, then deleting, within
	// one save).
	if v.deleted > 0 && a > st.a && a <= st.b && v.cursor == 0 {
		a = st.a
	}
	// Below the view: text after the cursor, which only Delete, Ctrl+K or
	// undo remove. Or, with the cursor at the very end of the view, text
	// Backspace just took: as long as no more vanished than was deleted.
	if b < st.b {
		gone := utf8.RuneCountInString(st.text[b:min(st.b, len(st.text))])
		atViewEnd := v.cursor == len(v.rows)-1 && v.cursorEnd
		if v.deletedAhead || (atViewEnd && gone <= v.deleted) {
			b = min(st.b, len(st.text))
		}
	}
	// An empty row at the top of the view is spacing the view itself
	// carries. Where the last view showed spacing at that spot, the new
	// view's replaces it rather than adding to it, or the line break would
	// double. Only as much as the view carries: a blank line that scrolled
	// out of sight above is real, and stays.
	lead := len(cur) - len(strings.TrimLeft(cur, " \n"))
	for ; lead > 0 && a > st.a && a > 0 && isSpace(st.text[a-1]); lead-- {
		a--
	}
	// At the end, spacing that ran to the very end of the last view (an
	// empty last row then) is now carried by this view, as trailing spacing
	// or as the break before newly typed words. Spacing followed by more
	// words the last view showed stays: those words scrolled away below.
	if end := min(st.b, len(st.text)); b < end && strings.TrimLeft(st.text[b:end], " \n") == "" {
		b = end
	}
	st.text = st.text[:a] + cur + st.text[b:]
	st.a, st.b = a, a+len(cur)
	return st.text
}

// lostChars counts the characters (not bytes: a delete key removes one
// character) of the words in old that are not in
// new, ignoring spacing. It compares only the stretch where the two differ
// (trimming the words they share at the start and the end), so a word is
// judged against its own surroundings. There, a word that was typed into
// ("hel", now "hello"; "helo", now "hello") is not lost; any other change
// counts, since counting a real loss as none would skip the copy that
// keeps it, while the opposite only adds a copy.
func lostChars(old, new string) int {
	var o, n []string
	for _, w := range words(old) {
		o = append(o, w.s)
	}
	for _, w := range words(new) {
		n = append(n, w.s)
	}
	for len(o) > 0 && len(n) > 0 && o[0] == n[0] {
		o, n = o[1:], n[1:]
	}
	for len(o) > 0 && len(n) > 0 && o[len(o)-1] == n[len(n)-1] {
		o, n = o[:len(o)-1], n[:len(n)-1]
	}
	have := map[string]int{}
	for _, w := range n {
		have[w]++
	}
	lost := 0
	for k, w := range o {
		switch {
		case have[w] > 0:
			have[w]--
		case k < len(n) && grewFrom(n[k], w):
			// Typed into, in place.
		default:
			lost += utf8.RuneCountInString(w)
		}
	}
	return lost
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\n'
}

// replace makes the view the whole draft.
func (st *stitcher) replace(cur string) string {
	st.text, st.a, st.b = cur, 0, len(cur)
	return cur
}

func sameView(a, b view) bool {
	return a.width == b.width && a.cursor == b.cursor && a.cursorEnd == b.cursorEnd &&
		a.capped == b.capped && a.empty == b.empty && slices.Equal(a.rows, b.rows)
}

// word is one word of a text and where it sits.
type word struct {
	s          string
	start, end int
}

// words splits text on spaces and line breaks. Each wide character
// (Chinese, Japanese, Korean) is a word of its own: those scripts use no
// spaces, and a whole paragraph as one "word" could never be lined up.
func words(text string) []word {
	var out []word
	start := -1
	flush := func(i int) {
		if start >= 0 {
			out = append(out, word{text[start:i], start, i})
			start = -1
		}
	}
	for i, r := range text {
		switch {
		case r == ' ' || r == '\n':
			flush(i)
		case runewidth.RuneWidth(r) == 2:
			flush(i)
			n := utf8.RuneLen(r)
			out = append(out, word{text[i : i+n], i, i + n})
		case start < 0:
			start = i
		}
	}
	flush(len(text))
	return out
}

// minShared is how many words a view must share with the known text to be
// lined up against it rather than taken as a different text.
const minShared = 4

// align finds the stretch old[p:q] that seen is the fewest word edits
// away from, and how many words the two share along the way. The stretch
// may start and end anywhere, so scrolling costs nothing. The last view
// showed old text from byte shownA to shownB.
//
// Edits are weighed by the keys that could have made them. Typing a word,
// or into one, is one edit. Deleting a word, or changing one, needs
// deleted characters (deleted is how many the keys could remove); an
// explanation that needs more than that costs as much as three edits.
//
// Choosing where the stretch ends, a word the last view showed and this
// one should still show counts one edit when left out below it: it cannot
// silently have moved out of sight ("line" backspaced to "l" is that word
// changed, not "l" typed with "line" hidden below).
//
// Ties go to the stretch starting nearest where the last view started,
// then ending nearest where it ended. Ties come from edits, not scrolling
// (a scrolled view matches exactly), and an edit leaves the end of the
// view where it was: typing into the last word in view ("hel" becoming
// "hello") rather than a new word beside a stale one, a word typed next to
// a hidden one rather than the hidden one edited, and a word deleted at
// the end rather than its neighbour retyped.
func align(old, seen []word, shownA, shownB, width, deleted int) (p, q, shared int) {
	m := len(old)
	gapFrom := func(f int) int {
		if f >= m {
			return abs(old[m-1].end - shownA)
		}
		return abs(old[f].start - shownA)
	}
	drop := func(w string) int {
		if utf8.RuneCountInString(w) <= deleted {
			return 1
		}
		return impossible
	}
	change := func(s, old string, first, last bool) int {
		switch {
		case grewFrom(s, old) || cutAtEdge(s, old, first, last, width):
			return 1
		case deleted > 0 && utf8.RuneCountInString(old)-utf8.RuneCountInString(s) <= deleted && removed(s, old) <= deleted:
			// (removed is at least the length difference: check that first,
			// it is free.)
			return 2
		}
		return impossible
	}
	// cost[j] is the cheapest way to turn the first i seen words into a
	// stretch of old ending at word j; from[j] is where that stretch
	// starts, and hits[j] how many words it matches unchanged.
	cost, from, hits := make([]int, m+1), make([]int, m+1), make([]int, m+1)
	prevCost, prevFrom, prevHits := make([]int, m+1), make([]int, m+1), make([]int, m+1)
	for j := range prevFrom {
		prevFrom[j] = j
	}
	for i := 1; i <= len(seen); i++ {
		cost[0], from[0], hits[0] = i, 0, 0
		for j := 1; j <= m; j++ {
			c, f, h := prevCost[j-1], prevFrom[j-1], prevHits[j-1]
			if seen[i-1].s == old[j-1].s {
				h++
			} else {
				c += change(seen[i-1].s, old[j-1].s, i == 1, i == len(seen))
			}
			for _, o := range [2][3]int{
				{prevCost[j] + 1, prevFrom[j], prevHits[j]},
				{cost[j-1] + drop(old[j-1].s), from[j-1], hits[j-1]},
			} {
				if o[0] < c || (o[0] == c && gapFrom(o[1]) < gapFrom(f)) {
					c, f, h = o[0], o[1], o[2]
				}
			}
			cost[j], from[j], hits[j] = c, f, h
		}
		cost, prevCost = prevCost, cost
		from, prevFrom = prevFrom, from
		hits, prevHits = prevHits, hits
	}
	// endsBy(x) counts the old words ending at or before byte x.
	endsBy := func(x int) int {
		return sort.Search(m, func(k int) bool { return old[k].end > x })
	}
	best, bestGapEnd := 1<<30, 0
	for j := 1; j <= m; j++ {
		f := prevFrom[j]
		if f >= j {
			continue
		}
		// Words from j on that the last view showed and this one, starting
		// at word f, should still show. A view that moved up may have lost
		// rows at the bottom (how many depends on row lengths it cannot
		// see), so only one that did not move up counts them.
		hidden := 0
		if old[f].start >= shownA {
			hidden = max(0, endsBy(shownB)-max(j, f))
		}
		c := prevCost[j] + hidden
		ge := abs(old[j-1].end - shownB)
		if c < best || (c == best && (gapFrom(f) < gapFrom(p) || (gapFrom(f) == gapFrom(p) && ge < bestGapEnd))) {
			p, q, shared, best, bestGapEnd = f, j, prevHits[j], c, ge
		}
	}
	return p, q, shared
}

// impossible is the cost of an edit the keys pressed could not have made.
const impossible = 3

// cutAtEdge reports whether s can be the visible part of old, a word longer
// than a row that the top or bottom edge of the box cuts.
func cutAtEdge(s, old string, first, last bool, width int) bool {
	if runewidth.StringWidth(old) <= width {
		return false
	}
	return (first && strings.HasSuffix(old, s)) || (last && strings.HasPrefix(old, s))
}

// grewFrom reports whether typing into old could have made s: old's
// characters appear in s, in order.
func grewFrom(s, old string) bool {
	i := 0
	for j := 0; j < len(s) && i < len(old); j++ {
		if s[j] == old[i] {
			i++
		}
	}
	return i == len(old)
}

// removed is how many of word's characters must have been deleted to turn
// it into s by deleting and typing: those not in their longest common
// subsequence.
func removed(str, word string) int {
	s, old := []rune(str), []rune(word)
	if len(s)*len(old) > 1<<16 {
		return len(old) // too long to compare cheaply; words rarely are
	}
	prev, cur := make([]int, len(old)+1), make([]int, len(old)+1)
	for i := 1; i <= len(s); i++ {
		for j := 1; j <= len(old); j++ {
			switch {
			case s[i-1] == old[j-1]:
				cur[j] = prev[j-1] + 1
			default:
				cur[j] = max(prev[j], cur[j-1])
			}
		}
		prev, cur = cur, prev
	}
	return len(old) - prev[len(old)]
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
