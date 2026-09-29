package main

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
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
	// restarted is set when the last view of a box at its cap lined up
	// with nothing known, so the draft is only what it shows.
	restarted bool
}

func (st *stitcher) reset() {
	*st = stitcher{}
}

// update merges a new view, whose rows the agent wrapped by rule u, and
// returns the draft's text.
func (st *stitcher) update(v view, u unwrapRule) string {
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
	st.restarted = false
	cur := u.unwrap(v.rows, v.width)
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
		st.restarted = true
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
	// undo remove. Those remove text from the cursor on, so words still in
	// view below the cursor's row mean the ones gone after them moved out
	// of sight (the cursor went up, taking the view with it). Or, with the
	// cursor at the very end of the view, text Backspace just took: as
	// long as no more vanished than was deleted.
	if b < st.b {
		gone := utf8.RuneCountInString(st.text[b:min(st.b, len(st.text))])
		atViewEnd := v.cursor == len(v.rows)-1 && v.cursorEnd
		cutAhead := v.deletedAhead && strings.TrimSpace(strings.Join(v.rows[v.cursor+1:], "")) == ""
		if cutAhead || (atViewEnd && gone <= v.deleted) {
			b = min(st.b, len(st.text))
		}
	}
	// An empty row at the top of the view is spacing the view itself
	// carries. Where the last view showed spacing at that spot, the new
	// view's replaces it rather than adding to it, or the line break would
	// double. Only as much as the view carries: a blank line that scrolled
	// out of sight above is real, and stays.
	lead := len(cur) - len(strings.TrimLeft(cur, " \n"))
	// (Only the same character: a space typed at the start of a row is not
	// the line break above it.)
	for ; lead > 0 && a > st.a && a > 0 && st.text[a-1] == cur[lead-1]; lead-- {
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
	// Where a stretch of old starting at word f sits. A word longer than a
	// row is cut by the top edge of the box, so a stretch that starts with
	// the visible part of one starts mid-word: without that, a view
	// scrolled a row into such a word looks a whole word away from where
	// the last one sat, and the tie between matching that word and taking
	// its visible part for a new one is broken the wrong way.
	startOf := func(f int) int {
		if len(seen) > 0 && cutAtEdge(seen[0].s, old[f].s, true, false, width) {
			return old[f].end - len(seen[0].s)
		}
		return old[f].start
	}
	gapFrom := func(f int) int {
		if f >= m {
			return abs(old[m-1].end - shownA)
		}
		return abs(startOf(f) - shownA)
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

// unwrapRule is how an agent wraps the lines of a draft into the rows of
// its box: its profile's wrap model (section 4 step 17 of docs/SPEC.md).
// The stitcher unwraps with it, and the fuzz draws the box with it.
type unwrapRule interface {
	// wrap draws text in rows width columns wide, the way the agent does,
	// and returns where each row starts in text. The insertion point at an
	// offset sits on the last row that starts at or before it.
	wrap(text string, width int) (rows []string, starts []int)
	// unwrap joins rows read off the screen back into the lines typed.
	unwrap(rows []string, width int) string
}

// wordWrap is word wrap with the space swallowed, as Claude Code and Codex
// draw it (measured; see CLAUDE.md and docs/research/codex.md sections 3
// and 10): greedy, at spaces, the space at a wrap left off both rows, and
// a word longer than a row broken at the edge, or one column short where a
// wide character would cross it.
//
// With endRow, a line that ends on a full row gets one more row, empty,
// where the insertion point sits (Codex 0.151.0 and 0.158.0). That row
// tells a line that ends at the edge from a wrap, which Claude Code's box
// cannot.
type wordWrap struct{ endRow bool }

func (w wordWrap) wrap(text string, width int) (rows []string, starts []int) {
	at := 0
	for _, line := range strings.Split(text, "\n") {
		rows, starts = wrapLine(rows, starts, line, at, width)
		if w.endRow && width > 0 && runewidth.StringWidth(rows[len(rows)-1]) == width {
			rows, starts = append(rows, ""), append(starts, at+len(line))
		}
		at += len(line) + 1
	}
	return rows, starts
}

func (w wordWrap) unwrap(rows []string, width int) string {
	if !w.endRow {
		return unwrap(rows, width)
	}
	// An empty row after a full one is where the line ended: drop it, and
	// the next row starts a line of its own.
	var lines []string
	from := 0
	for i := 0; i+1 < len(rows); i++ {
		if width > 0 && rows[i+1] == "" && runewidth.StringWidth(rows[i]) == width {
			lines = append(lines, unwrap(rows[from:i+1], width))
			from = i + 2
			i++
		}
	}
	if from < len(rows) || len(lines) == 0 {
		lines = append(lines, unwrap(rows[from:], width))
	}
	return strings.Join(lines, "\n")
}

// wrapLine appends the rows of one line, which starts at offset at of the
// text, to rows, and where each starts to starts.
func wrapLine(rows []string, starts []int, line string, at, width int) ([]string, []int) {
	if width <= 0 || runewidth.StringWidth(line) <= width {
		return append(rows, line), append(starts, at)
	}
	cur, curAt, before := "", at, len(rows)
	next := at // where the next word starts
	for _, word := range strings.Split(line, " ") {
		wordAt := next
		next += len(word) + 1
		broken := false
		for runewidth.StringWidth(word) > width {
			broken = true
			if cur != "" {
				rows, starts = append(rows, cur), append(starts, curAt)
				cur = ""
			}
			head := runewidth.Truncate(word, width, "")
			if head == "" {
				// A character wider than the row: it gets a row to itself.
				_, n := utf8.DecodeRuneInString(word)
				head = word[:n]
			}
			rows, starts = append(rows, head), append(starts, wordAt)
			word, wordAt = word[len(head):], wordAt+len(head)
		}
		if broken && word == "" {
			continue
		}
		switch {
		case cur == "":
			cur, curAt = word, wordAt
		case runewidth.StringWidth(cur)+1+runewidth.StringWidth(word) <= width:
			cur += " " + word
		default:
			rows, starts = append(rows, cur), append(starts, curAt)
			cur, curAt = word, wordAt
		}
	}
	if cur != "" || len(rows) == before {
		rows, starts = append(rows, cur), append(starts, curAt)
	}
	return rows, starts
}

// scrollModel is how a box at its height cap scrolls to keep the cursor in
// view: its profile's scroll model (section 4 step 17 of docs/SPEC.md).
// The stitcher needs none, since it lines each view up wherever it sits;
// the fuzz draws the box with it, and TestReplayScrollModel holds each
// profile's to where its agent's captures leave the cursor.
type scrollModel int

const (
	// scrollOneRow scrolls only as far as the cursor needs to stay in
	// view, a row for each row it moves past the edge (Codex 0.151.0 and
	// 0.158.0).
	scrollOneRow scrollModel = iota
	// scrollMidBox holds the cursor on the middle row while it moves over
	// rows out of sight, scrolling a row per key, and scrolls one row when
	// typing pushes past the edge (Claude Code 2.1.282).
	scrollMidBox
)

// unwrap joins rows that the agent wrapped back into the lines typed.
//
// A row was wrapped when the next row's first word would not have fitted
// after it; the space the wrap swallowed is put back. A word longer than a
// row is broken mid-way instead (see brokenWord), and joins with no space.
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
		case width > 0 && brokenWord(prev, next, pw, width):
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

// brokenWord reports whether the break between prev (pw columns wide) and
// next cuts one word longer than a row. Such a word fills the row, or all
// but one column when the next character is wide and did not fit. A word
// that fits in a row moves whole to the next one, so the two halves must
// add up to more than a row.
func brokenWord(prev, next string, pw, width int) bool {
	if next == "" || strings.HasSuffix(prev, " ") {
		return false
	}
	r, _ := utf8.DecodeRuneInString(next)
	if pw < width && !(pw == width-1 && runewidth.RuneWidth(r) == 2) {
		return false
	}
	last := prev[strings.LastIndexByte(prev, ' ')+1:]
	return runewidth.StringWidth(last)+runewidth.StringWidth(firstWord(next)) > width
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

// byteExact reports whether got is the draft byte for byte, allowing only
// the limit unwrap documents: a line break typed where the row before it
// is full comes back as a space. Full means the next line's first word
// would not have fitted after that row, so the screen shows a wrap; under
// Codex's end row, a line that ends on a full row is not, since that row
// shows where it ended. (The fuzz never types a list item or a code fence,
// which unwrap keeps apart.) It wraps with u.wrap, not u.unwrap, so a
// broken unwrap cannot excuse itself. The fuzz counts drafts with it, and
// a restore into the box is read back with it.
func byteExact(got, truth string, width int, u unwrapRule) bool {
	if len(got) != len(truth) {
		return false
	}
	lineStart := 0
	for i := 0; i < len(truth); i++ {
		if got[i] != truth[i] && (truth[i] != '\n' || got[i] != ' ' || !fullRowAt(truth, lineStart, i, width, u)) {
			return false
		}
		if truth[i] == '\n' {
			lineStart = i + 1
		}
	}
	return true
}

// A breakHider is a wrap rule that says itself which line breaks its
// agent's screen shows as a wrap at a space (pi's, whose rows break by
// other rules than the next word fitting).
type breakHider interface {
	hidesBreak(line, next string, width int) bool
}

// fullRowAt reports whether the line break at text[i], ending the line
// that starts at lineStart, looks like a wrap on screen.
func fullRowAt(text string, lineStart, i, width int, u unwrapRule) bool {
	next, _, _ := strings.Cut(text[i+1:], "\n")
	first, _, _ := strings.Cut(next, " ")
	if i == lineStart || next == "" {
		return false
	}
	if h, ok := u.(breakHider); ok {
		return h.hidesBreak(text[lineStart:i], next, width)
	}
	rows, _ := u.wrap(text[lineStart:i], width)
	return runewidth.StringWidth(rows[len(rows)-1])+1+runewidth.StringWidth(first) > width
}

// fitWrap is the wrap of an editor that keeps the space at a wrap on the
// upper row: greedy, over grapheme clusters, a paste placeholder as one
// (marks matches it). A row may break after the last run of spaces that
// leaves the rest of its word room on the next row, the spaces staying at
// the end of the upper row, where the screen shows none; or between two
// characters where either is CJK. Where no such break fits, the row
// breaks at the edge, also in the middle of a run of spaces, which then
// starts the next row. So a word that ends right at the edge and has a
// space after it moves to the next row, which Claude Code's wrap keeps on
// the full one. Rows are returned as the screen shows them, with spaces
// at their ends trimmed.
//
// It is pi's wrap (wordWrapLine in editor.js, 0.87.1) and agy's
// (measured on 1.2.13, the wrap capture: "a"x114 then " bb cc" at 117
// columns puts "bb cc" on the second row, where Claude Code's wrap keeps
// "bb" on the first). The CJK rule is pi's; for agy only a wide character
// at the edge of a row was measured.
type fitWrap struct{ marks *regexp.Regexp }

func (w fitWrap) wrap(text string, width int) (rows []string, starts []int) {
	at := 0
	for _, line := range strings.Split(text, "\n") {
		chunks := w.wrapLine(line, width)
		for _, c := range chunks {
			rows = append(rows, strings.TrimRight(line[c[0]:c[1]], " "))
			starts = append(starts, at+c[0])
		}
		at += len(line) + 1
	}
	return rows, starts
}

// wrapLine returns the byte ranges of the rows one line wraps into.
func (w fitWrap) wrapLine(line string, width int) [][2]int {
	if line == "" || width <= 0 || runewidth.StringWidth(line) <= width {
		return [][2]int{{0, len(line)}}
	}
	segs := w.segments(line)
	var chunks [][2]int
	cur, start := 0, 0
	opp, oppWidth := -1, 0
	for i, sg := range segs {
		g := line[sg[0]:sg[1]]
		gw := runewidth.StringWidth(g)
		marker := sg[2] == 1
		ws := !marker && wrapSpace(g)
		if cur+gw > width {
			switch {
			case opp >= 0 && cur-oppWidth+gw <= width:
				chunks = append(chunks, [2]int{start, opp})
				start = opp
				cur -= oppWidth
			case start < sg[0]:
				chunks = append(chunks, [2]int{start, sg[0]})
				start = sg[0]
				cur = 0
			}
			opp = -1
		}
		if gw > width {
			// Wider than a row (a placeholder in a narrow window): it takes
			// rows of its own, broken at the edge.
			sub := w.wrapLine(g, width)
			for _, c := range sub[:len(sub)-1] {
				chunks = append(chunks, [2]int{sg[0] + c[0], sg[0] + c[1]})
			}
			last := sub[len(sub)-1]
			start = sg[0] + last[0]
			cur = runewidth.StringWidth(g[last[0]:last[1]])
			opp = -1
			continue
		}
		cur += gw
		if i+1 >= len(segs) {
			continue
		}
		next := segs[i+1]
		ng := line[next[0]:next[1]]
		nextMarker := next[2] == 1
		switch {
		case ws && (nextMarker || !wrapSpace(ng)):
			opp, oppWidth = next[0], cur
		case !ws && !wrapSpace(ng) && ((!marker && wrapCJK(g)) || (!nextMarker && wrapCJK(ng))):
			opp, oppWidth = next[0], cur
		}
	}
	return append(chunks, [2]int{start, len(line)})
}

// segments splits a line into grapheme clusters, each paste placeholder
// kept as one: its byte range, and 1 in the third field for a placeholder.
func (w fitWrap) segments(line string) [][3]int {
	var segs [][3]int
	marks := w.marks.FindAllStringIndex(line, -1)
	pos := 0
	for pos < len(line) {
		if len(marks) > 0 && marks[0][0] == pos {
			segs = append(segs, [3]int{pos, marks[0][1], 1})
			pos = marks[0][1]
			marks = marks[1:]
			continue
		}
		end := len(line)
		if len(marks) > 0 {
			end = marks[0][0]
		}
		g, _, _, _ := uniseg.FirstGraphemeClusterInString(line[pos:end], -1)
		if g == "" {
			g = line[pos : pos+1]
		}
		segs = append(segs, [3]int{pos, pos + len(g), 0})
		pos += len(g)
	}
	return segs
}

// wrapSpace reports whether a grapheme holds white space, as JavaScript's
// \s finds it.
func wrapSpace(g string) bool {
	return strings.IndexFunc(g, func(r rune) bool { return unicode.IsSpace(r) || r == '\uFEFF' }) >= 0
}

// wrapCJK reports whether a grapheme holds a character after which, or
// before which, a row may break: Han, kana, Hangul or Bopomofo.
func wrapCJK(g string) bool {
	return strings.IndexFunc(g, func(r rune) bool {
		return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Bopomofo)
	}) >= 0
}

// unwrap joins rows pi wrapped back into the lines typed. Each row starts
// afresh, as pi's wrap does, so the break between two rows was a wrap
// exactly when wrapping the two joined gives them back: joined with the
// space the upper row hid, or with nothing (a word longer than a row, a
// CJK break, a row that starts with a space). A break both explain is a
// wrap at a space, unless a CJK character sits at it. Anything else is a
// line break typed there. Two or more spaces at a wrap come back as one.
func (w fitWrap) unwrap(rows []string, width int) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(rows[0])
	for i := 1; i < len(rows); i++ {
		b.WriteString(w.join(rows[i-1], rows[i], width))
		b.WriteString(rows[i])
	}
	return b.String()
}

// hidesBreak reports whether a line break between line and next reads
// as a wrap on pi's screen (see byteExact).
func (w fitWrap) hidesBreak(line, next string, width int) bool {
	a, _ := w.wrap(line, width)
	b, _ := w.wrap(next, width)
	return w.join(a[len(a)-1], b[0], width) == " "
}

// join is what stood between two rows on the screen: " " or "" for a
// wrap, "\n" for a line break.
func (w fitWrap) join(prev, next string, width int) string {
	if width <= 0 || prev == "" || next == "" {
		return "\n"
	}
	space, none := w.rewraps(prev+" "+next, prev, next, width), w.rewraps(prev+next, prev, next, width)
	switch {
	case space && none:
		last, _ := utf8.DecodeLastRuneInString(prev)
		first, _ := utf8.DecodeRuneInString(next)
		if wrapCJK(string(last)) || wrapCJK(string(first)) {
			return ""
		}
		return " "
	case space:
		return " "
	case none:
		return ""
	}
	return "\n"
}

// rewraps reports whether text wraps into exactly the rows a and b.
func (w fitWrap) rewraps(text, a, b string, width int) bool {
	c := w.wrapLine(text, width)
	return len(c) == 2 && strings.TrimRight(text[c[0][0]:c[0][1]], " ") == a && strings.TrimRight(text[c[1][0]:c[1][1]], " ") == b
}
