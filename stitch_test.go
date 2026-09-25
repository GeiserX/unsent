package main

import (
	"fmt"
	"math/rand"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// boxSim draws a draft the way Claude Code does: wrapped rows, a box that
// stops growing at cap rows and scrolls to keep the cursor in sight. With
// jump set it scrolls the way the real one was measured to, putting the
// cursor mid-box; without, one row at a time.
type boxSim struct {
	text       string
	cur        int // cursor, as a byte offset into text
	width, cap int
	top        int
	jump       bool
	// edit is the byte range of the last change; seen says whether the
	// last view showed all of it.
	editFrom, editTo int
	seen             bool
}

// rows wraps the text like wrapText and also returns where each row
// starts. Widths are screen columns: a wide character takes two.
func (b *boxSim) rows() ([]string, []int) {
	width := runewidth.StringWidth
	var rows []string
	var starts []int
	lineStart := 0
	for _, line := range strings.Split(b.text, "\n") {
		if width(line) <= b.width {
			rows, starts = append(rows, line), append(starts, lineStart)
			lineStart += len(line) + 1
			continue
		}
		rowStart, rowEnd := lineStart, lineStart
		for i := 0; i <= len(line); i++ {
			if i < len(line) && line[i] != ' ' {
				continue
			}
			wordEnd := lineStart + i
			switch {
			case rowEnd == rowStart && width(b.text[rowStart:wordEnd]) > b.width:
				// A word wider than a row: break it at the row's width.
				for width(b.text[rowStart:wordEnd]) > b.width {
					head := runewidth.Truncate(b.text[rowStart:wordEnd], b.width, "")
					if head == "" {
						_, n := utf8.DecodeRuneInString(b.text[rowStart:])
						head = b.text[rowStart : rowStart+n]
					}
					rows, starts = append(rows, head), append(starts, rowStart)
					rowStart += len(head)
				}
				rowEnd = wordEnd
			case width(b.text[rowStart:wordEnd]) <= b.width:
				rowEnd = wordEnd
			default:
				rows, starts = append(rows, b.text[rowStart:rowEnd]), append(starts, rowStart)
				rowStart = rowEnd + 1
				rowEnd = wordEnd
			}
		}
		rows, starts = append(rows, b.text[rowStart:rowEnd]), append(starts, rowStart)
		lineStart += len(line) + 1
	}
	return rows, starts
}

func (b *boxSim) view(t *testing.T) view {
	rows, starts := b.rows()
	if want := wrapText(b.text, b.width); !slices.Equal(rows, want) {
		i := 0
		for i < len(rows) && i < len(want) && rows[i] == want[i] {
			i++
		}
		t.Fatalf("simulator rows differ from wrapText at row %d of %d/%d:\nsim  %q\nwrap %q\ntext %q",
			i, len(rows), len(want), rows[i:min(len(rows), i+3)], want[i:min(len(want), i+3)], b.text[max(0, len(b.text)-80):])
	}
	cr := 0
	for i, s := range starts {
		if s <= b.cur {
			cr = i
		}
	}
	h := min(len(rows), b.cap)
	if cr < b.top || cr >= b.top+h {
		if b.jump {
			b.top = cr - h/2
		} else if cr < b.top {
			b.top = cr
		} else {
			b.top = cr - h + 1
		}
	}
	b.top = clamp(b.top, 0, len(rows)-h)
	bottom := len(b.text)
	if b.top+h < len(rows) {
		bottom = starts[b.top+h]
	}
	b.seen = b.editFrom >= starts[b.top] && b.editTo <= bottom
	b.editFrom, b.editTo = len(b.text)+1, -1
	return view{
		rows:      append([]string(nil), rows[b.top:b.top+h]...),
		cursorEnd: b.cur >= starts[cr]+len(rows[cr]),
		cursor:    cr - b.top,
		width:     b.width,
		// The extractor counts a box one row short of the cap as capped.
		capped: h >= b.cap-1,
		empty:  b.text == "",
	}
}

// wordEnds lists the offsets right after each word: where the simulated
// cursor may sit.
func (b *boxSim) wordEnds() []int {
	var out []int
	for i := 1; i <= len(b.text); i++ {
		if b.text[i-1] != ' ' && b.text[i-1] != '\n' && (i == len(b.text) || b.text[i] == ' ' || b.text[i] == '\n') {
			out = append(out, i)
		}
	}
	return out
}

func (b *boxSim) insert(s string) {
	b.text = b.text[:b.cur] + s + b.text[b.cur:]
	b.mark(b.cur, b.cur+len(s))
	b.cur += len(s)
}

// mark records that text[from:to] changed.
func (b *boxSim) mark(from, to int) {
	b.editFrom, b.editTo = min(b.editFrom, from), max(b.editTo, to)
}

// deleteWord removes the word before the cursor and the space before it,
// unless it starts a line, and returns how many Backspaces that took.
func (b *boxSim) deleteWord() int {
	i := strings.LastIndexAny(b.text[:b.cur], " \n")
	if i < 0 || b.text[i] == '\n' {
		return 0
	}
	n := b.cur - i
	b.text = b.text[:i] + b.text[b.cur:]
	b.cur = i
	b.mark(max(0, i-1), i+1)
	return n
}

// moveRows puts the cursor on a word end about d rows away.
func (b *boxSim) moveRows(d int) {
	rows, starts := b.rows()
	cr := 0
	for i, s := range starts {
		if s <= b.cur {
			cr = i
		}
	}
	target := clamp(cr+d, 0, len(rows)-1)
	ends := b.wordEnds()
	best := b.cur
	for _, e := range ends {
		if e >= starts[target] && e <= starts[target]+len(rows[target]) {
			best = e
			break
		}
	}
	b.cur = best
}

type stitchRun struct {
	t        *testing.T
	sim      *boxSim
	st       stitcher
	steps    int
	sweeping bool
	// strict checks the no-loss guarantee on every save. It needs every
	// word to be unique: with repeats, counting cannot tell which "w19" a
	// save kept, so only the exact rate is measured.
	strict bool
	// mustBeExact fails on any inexact save (the fixed scenarios).
	mustBeExact bool
	stitched    bool
	// mode is how delete keys reach the stitcher: "exact" (this save's
	// keys only), "window" (Backspaces through the session's deleteLog, as
	// in production) or "unlimited" (Ctrl+W instead of Backspaces).
	mode  string
	log   *deleteLog
	clock time.Time
	// last is the previous draft; kept holds the versions the session
	// would have archived because a save lost text nobody deleted.
	last        string
	kept        []string
	exact, near int
	// trail describes recent saves, for failure messages.
	trail []string
}

// budget turns this save's delete count into what the stitcher is told,
// through the session's own deleteLog on a simulated clock: saves 0.4 s
// apart, keys pressed just before each save.
func (r *stitchRun) budget(deleted int) int {
	if r.mode == "exact" || r.mode == "" {
		return deleted
	}
	if r.log == nil {
		r.clock = time.Unix(0, 0)
		r.log = &deleteLog{now: func() time.Time { return r.clock }}
	}
	r.clock = r.clock.Add(saveInterval)
	if deleted > 0 {
		if r.mode == "unlimited" {
			r.log.push(unlimited, false) // Ctrl+W
		} else {
			r.log.push(int64(deleted), false)
		}
	}
	n, _ := r.log.recent()
	return n
}

// rowOf returns the row the cursor is on.
func (b *boxSim) rowOf() int {
	_, starts := b.rows()
	cr := 0
	for i, s := range starts {
		if s <= b.cur {
			cr = i
		}
	}
	return cr
}

func (r *stitchRun) check(what string) {
	r.t.Helper()
	r.checkKeys(what, 0)
}

// checkDeleted is check after Backspace was pressed n times.
func (r *stitchRun) checkDeleted(what string, n int) {
	r.t.Helper()
	r.checkKeys(what, n)
}

func (r *stitchRun) checkKeys(what string, deleted int) {
	r.t.Helper()
	r.steps++
	v := r.sim.view(r.t)
	v.deleted = deleted
	got := r.save(v, what)
	if !r.sim.seen {
		// Part of the edit landed out of sight: no screen shows it. Scroll
		// through the whole draft, as the user would to see it.
		r.sweeping = true
		for r.sim.moveRows(-1); r.sim.cur > 0 && r.sim.rowOf() > 0; r.sim.moveRows(-1) {
			r.check("scroll up to see an edit")
		}
		for last := -1; r.sim.rowOf() != last; r.sim.moveRows(1) {
			last = r.sim.rowOf()
			r.check("scroll down to see an edit")
		}
		r.sweeping = false
		r.sim.cur = len(r.sim.text)
		got = r.save(r.sim.view(r.t), "back to the end")
	}
	if r.sweeping {
		return
	}
	if slices.Equal(strings.Fields(got), strings.Fields(r.sim.text)) {
		r.exact++
		return
	}
	r.near++
	if r.mustBeExact {
		r.t.Fatalf("step %d (%s): got %q\nwant %q", r.steps, what, got, r.sim.text)
	}
}

// save runs one save the way the session does: stitch the view, and keep
// the previous version when keepOld says so. Then it checks the guarantee:
// every word the previous save held that is still in the true draft is in
// this save or a kept version.
func (r *stitchRun) save(v view, what string) string {
	r.t.Helper()
	v.deleted = r.budget(v.deleted)
	prev := r.last
	got := r.st.update(v)
	r.stitched = r.stitched || v.capped
	history, version := keepOld(prev, got, r.stitched)
	if history || version {
		r.kept = append(r.kept, prev)
	}
	if r.log != nil {
		r.log.spend(shrunk(prev, got))
	}
	r.last = got
	r.trail = append(r.trail, fmt.Sprintf("step %d %s: deleted=%d lost=%d kept=%v", r.steps, what, v.deleted, lostChars(prev, got), history || version))
	if len(r.trail) > 8 {
		r.trail = r.trail[1:]
	}
	if missing := r.lostEverywhere(prev, got); r.strict && len(missing) > 0 {
		o, n, tr := strings.Fields(prev), strings.Fields(got), strings.Fields(r.sim.text)
		i := 0
		for i < len(o) && i < len(n) && o[i] == n[i] {
			i++
		}
		r.t.Fatalf("step %d (%s): words lost with no copy kept: %q\n%s\nfrom word %d:\nprev:  %q\ngot:   %q\ntruth: %q\nview:  %q cursor %d",
			r.steps, what, missing, strings.Join(r.trail, "\n"), i,
			o[max(0, i-4):min(len(o), i+10)], n[max(0, i-4):min(len(n), i+10)], tr[max(0, i-4):min(len(tr), i+10)], v.rows, v.cursor)
	}
	return got
}

// lostEverywhere returns the words the previous save held, and the true
// draft still holds, that are now neither in got nor in any kept version
// (each counted as often as it occurs). A word no save caught is a
// misread, not a loss: it counts against exactness instead.
func (r *stitchRun) lostEverywhere(prev, got string) []string {
	count := func(text string) map[string]int {
		n := map[string]int{}
		for _, w := range strings.Fields(text) {
			n[w]++
		}
		return n
	}
	best := count(got)
	for _, k := range r.kept {
		for w, c := range count(k) {
			best[w] = max(best[w], c)
		}
	}
	before, truth := count(prev), count(r.sim.text)
	var missing []string
	for w, c := range before {
		if need := min(c, truth[w]); need > best[w] {
			missing = append(missing, fmt.Sprintf("%s (held %d, still true %d, now at most %d)", w, c, truth[w], best[w]))
		}
	}
	return missing
}

// fuzzStitch types a draft past the cap, then makes random edits all over
// it, and checks after every save that not a word was lost or doubled.
func fuzzStitch(t *testing.T, seed int64, vocab int, jump bool, mode string) (exact, near int) {
	rng := rand.New(rand.NewSource(seed))
	word := func(n int) string {
		if vocab > 0 {
			return fmt.Sprintf("w%d", rng.Intn(vocab))
		}
		return fmt.Sprintf("w%d", n)
	}
	sim := &boxSim{width: 40 + rng.Intn(60), cap: 5 + rng.Intn(10), jump: jump}
	r := &stitchRun{t: t, sim: sim, strict: vocab == 0, mode: mode}
	n := 0
	sim.insert(word(n))
	r.check("first word")
	for len(sim.rows0()) < sim.cap*3 {
		n++
		if rng.Intn(12) == 0 {
			sim.insert("\n" + word(n))
		} else {
			sim.insert(" " + word(n))
		}
		r.check("type at the end")
	}
	for i := 0; i < 150; i++ {
		n++
		switch rng.Intn(9) {
		case 0, 1:
			sim.insert(" " + word(n))
			r.check("insert a word")
		case 2:
			r.checkDeleted("delete a word", sim.deleteWord())
		case 3:
			if rng.Intn(2) == 0 {
				sim.insert("\n" + word(n))
				r.check("new line")
			} else {
				// Enter at the end of a line, a pause long enough for a
				// save, then the word.
				if i := strings.IndexByte(sim.text[sim.cur:], '\n'); i >= 0 {
					sim.cur += i
				} else {
					sim.cur = len(sim.text)
				}
				sim.insert("\n")
				r.check("Enter, then a pause")
				sim.insert(word(n))
				r.check("type the new line")
			}
		case 4, 5:
			sim.moveRows(-1 - rng.Intn(3))
			r.check("move up")
		case 6:
			sim.moveRows(1 + rng.Intn(3))
			r.check("move down")
		case 7:
			if rng.Intn(2) == 0 {
				sim.cur = len(sim.text)
				r.check("jump to the end")
			} else {
				// Type into the word before the cursor, a few letters at a
				// time: the half-typed word must not survive.
				// Letters, not digits: "w9" plus "7" would collide with w97.
				sim.insert(string(rune('a' + rng.Intn(26))))
				r.check("type into a word")
			}
		case 8:
			if rng.Intn(3) == 0 {
				// Type a word, then take some of it back, within one save.
				sim.insert(" " + word(n) + "x")
				sim.text = sim.text[:sim.cur-1] + sim.text[sim.cur:]
				sim.cur--
				r.checkDeleted("type, then backspace", 1)
			} else if rng.Intn(4) == 0 {
				sim.width = 40 + rng.Intn(60)
				r.check("resize")
			} else {
				sim.insert(" " + word(n) + " " + word(n+1))
				r.check("insert two words")
			}
		}
	}
	return r.exact, r.near
}

func (b *boxSim) rows0() []string {
	rows, _ := b.rows()
	return rows
}

// fuzzSeeds is how many random runs each fuzz test makes
// (UNSENT_FUZZ_SEEDS overrides it).
func fuzzSeeds() int64 {
	if n, err := strconv.ParseInt(os.Getenv("UNSENT_FUZZ_SEEDS"), 10, 64); err == nil && n > 0 {
		return n
	}
	if raceEnabled || testing.Short() {
		// The race detector looks for concurrency bugs, and the stitcher
		// has none; CI runs the full fuzz without it.
		return 4
	}
	return 150
}

// The fuzz tests check two things at every save. Hard (unique words, so
// every word can be told apart): no word the previous save held is gone
// from both the live draft and the versions the session archives, unless
// the user deleted it. Measured: how often the live draft is exactly
// right, which must stay above a floor. Views are partial, so a few rare
// edits (typing, deleting and scrolling at the box edge within one 0.4 s
// save) are misread; the guarantee makes that safe.

func TestStitchFuzzUniqueWords(t *testing.T) {
	for _, m := range []struct {
		mode  string
		floor float64
	}{{"exact", 0.995}, {"window", 0.99}, {"unlimited", 0.95}} {
		t.Run(m.mode, func(t *testing.T) { fuzzRate(t, 0, m.mode, m.floor) })
	}
}

// With a 40-word vocabulary the same words recur constantly, the hard case
// for lining views up, and some edits are ambiguous on screen: typing a
// word identical to the next hidden word looks like that word having been
// there all along.
func TestStitchFuzzRepeatedWords(t *testing.T) {
	fuzzRate(t, 40, "window", 0.97)
}

func fuzzRate(t *testing.T, vocab int, mode string, floor float64) {
	t.Helper()
	exact, near := 0, 0
	for seed := int64(1); seed <= fuzzSeeds(); seed++ {
		e, n := fuzzStitch(t, seed, vocab, seed%2 == 0, mode)
		exact, near = exact+e, near+n
	}
	rate := float64(exact) / float64(exact+near)
	t.Logf("%d of %d saves exact (%.2f%%)", exact, exact+near, 100*rate)
	if rate < floor {
		t.Fatalf("exact rate %.4f below %.4f", rate, floor)
	}
}

func TestStitchLongParagraphEditedWhileScrolledUp(t *testing.T) {
	// The review's case: 80 columns, a box capped at 10 rows, one long
	// paragraph, a word inserted near the top while the bottom is hidden.
	sim := &boxSim{width: 76, cap: 10, jump: true}
	r := &stitchRun{t: t, sim: sim, strict: true, mustBeExact: true}
	for i := 0; i < 150; i++ {
		sim.insert(fmt.Sprintf("word%03d ", i))
		r.check("type")
	}
	for i := 0; i < 20; i++ {
		sim.moveRows(-1)
		r.check("move up")
	}
	sim.insert(" INSERTED")
	r.check("insert")
	for i := 0; i < 12; i++ {
		sim.insert(fmt.Sprintf(" more%02d", i))
		r.check("insert more")
	}
	for i := 0; i < 6; i++ {
		r.checkDeleted("delete", sim.deleteWord())
	}
}

func TestStitchDeleteAtTheEnd(t *testing.T) {
	sim := &boxSim{width: 60, cap: 10}
	r := &stitchRun{t: t, sim: sim, strict: true, mustBeExact: true}
	for i := 1; i <= 30; i++ {
		if i > 1 {
			sim.insert("\n")
		}
		sim.insert(fmt.Sprintf("typed line %d", i))
		r.check("type")
	}
	for i := 0; i < 5; i++ {
		j := strings.LastIndex(sim.text, "\n")
		n := len(sim.text) - j
		sim.text, sim.cur = sim.text[:j], j
		r.checkDeleted("delete the last line", n)
	}
}

func TestStitchEmptyResets(t *testing.T) {
	var st stitcher
	st.update(view{rows: []string{"a"}, width: 10})
	if got := st.update(view{empty: true}); got != "" {
		t.Fatalf("got %q", got)
	}
	if st.text != "" {
		t.Fatalf("text kept after empty: %q", st.text)
	}
}

func TestStitchNothingLinesUpStartsOver(t *testing.T) {
	st := stitcher{text: "the draft we know about, long enough to anchor", a: 0, b: 10}
	got := st.update(view{rows: []string{"completely different text here"}, width: 80, capped: true, cursor: 0})
	if got != "completely different text here" {
		t.Fatalf("got %q", got)
	}
}

func TestAlignPrefersTheStretchNearTheLastView(t *testing.T) {
	old := words("a b c x a b c y a b c")
	seen := words("a b c")
	if p, q, n := align(old, seen, old[4].start, old[6].end, 80, 0); p != 4 || q != 7 || n != 3 {
		t.Fatalf("p=%d q=%d shared=%d", p, q, n)
	}
	if p, _, _ := align(old, seen, 0, old[2].end, 80, 0); p != 0 {
		t.Fatalf("p=%d", p)
	}
}

func TestStitchKeepsHiddenPartOfALongWord(t *testing.T) {
	long := strings.Repeat("x", 30)
	st := stitcher{text: "start " + long + " end of the draft here", a: 0, b: 12}
	// Rows are 25 wide: the long word breaks after 25, and the view begins
	// with its last 5 characters.
	got := st.update(view{rows: []string{"xxxxx", "end of the draft here"}, width: 25, capped: true, cursor: 1})
	if want := "start " + long + " end of the draft here"; !slices.Equal(strings.Fields(got), strings.Fields(want)) {
		t.Fatalf("got %q", got)
	}
}

func clamp(x, lo, hi int) int {
	return max(lo, min(x, hi))
}

func wordsFrom(from, to int) string {
	var w []string
	for i := from; i < to; i++ {
		w = append(w, fmt.Sprintf("word%03d", i))
	}
	return strings.Join(w, " ")
}

func TestUnwrap(t *testing.T) {
	cases := []struct {
		name  string
		rows  []string
		width int
		want  string
	}{
		{"soft wrap puts the space back", []string{"aaaa bbbb", "cccc"}, 10, "aaaa bbbb cccc"},
		{"short row is a hard newline", []string{"aa", "bbbb"}, 10, "aa\nbbbb"},
		{"long word broken at the edge", []string{"xxxxxxxxxx", "xxx"}, 10, "xxxxxxxxxxxxx"},
		{"list item starts a new line", []string{"aaaa bbbb", "- cc"}, 10, "aaaa bbbb\n- cc"},
		{"numbered item starts a new line", []string{"aaaa bbbb", "2. cc"}, 10, "aaaa bbbb\n2. cc"},
		{"code fence starts a new line", []string{"aaaa bbbb", "```go"}, 10, "aaaa bbbb\n```go"},
		{"blank row is kept", []string{"aaaa bbbb", "", "cc"}, 10, "aaaa bbbb\n\ncc"},
		{"full row then hand newline joins (known limit)", []string{"aaaa bbbb", "cccc"}, 10, "aaaa bbbb cccc"},
		{"no rows", nil, 10, ""},
	}
	for _, c := range cases {
		if got := unwrap(c.rows, c.width); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestWrapTextMatchesClaudeCode(t *testing.T) {
	// Measured in Claude Code 2.1.282 at 100 columns: rows wrap at 96.
	para := wordsFrom(0, 40)
	rows := wrapText(para, 96)
	if len(rows) != 4 || rows[0] != wordsFrom(0, 12) || rows[3] != wordsFrom(36, 40) {
		t.Fatalf("rows: %q", rows)
	}
	long := wrapText(strings.Repeat("c", 130), 56)
	if len(long) != 3 || len(long[0]) != 56 || len(long[2]) != 18 {
		t.Fatalf("long word rows: %q", long)
	}
	if got := wrapText("a\n\nb", 10); strings.Join(got, "|") != "a||b" {
		t.Fatalf("blank lines: %q", got)
	}
	if got := unwrap(wrapText(para, 96), 96); got != para {
		t.Fatalf("round trip: %q", got)
	}
	// A character wider than the row used to loop forever.
	if got := wrapText("漢字", 1); strings.Join(got, "|") != "漢|字" {
		t.Fatalf("wide chars at width 1: %q", got)
	}
}

// wrapText wraps text the way Claude Code does (measured; see CLAUDE.md): greedy, at word boundaries,
// breaking words longer than a row.
func wrapText(text string, width int) []string {
	var rows []string
	for _, line := range strings.Split(text, "\n") {
		if width <= 0 || runewidth.StringWidth(line) <= width {
			rows = append(rows, line)
			continue
		}
		cur, before := "", len(rows)
		for _, word := range strings.Split(line, " ") {
			broken := false
			for runewidth.StringWidth(word) > width {
				broken = true
				if cur != "" {
					rows = append(rows, cur)
					cur = ""
				}
				head := runewidth.Truncate(word, width, "")
				if head == "" {
					// A character wider than the row: it gets a row to itself.
					_, n := utf8.DecodeRuneInString(word)
					head = word[:n]
				}
				rows = append(rows, head)
				word = word[len(head):]
			}
			if broken && word == "" {
				continue
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
		if cur != "" || len(rows) == before {
			rows = append(rows, cur)
		}
	}
	return rows
}

func TestStitchWideCharacterText(t *testing.T) {
	// A paragraph with no spaces, scrolled through a 5-row box: the review
	// found the word stitcher kept only what was on screen.
	var text strings.Builder
	for i := 0; i < 260; i++ {
		text.WriteRune(rune(0x4e00 + i))
	}
	sim := &boxSim{width: 20, cap: 5}
	r := &stitchRun{t: t, sim: sim, strict: true, mustBeExact: true}
	for _, ch := range text.String() {
		sim.insert(string(ch))
		r.check("type")
	}
	for i := 0; i < 20; i++ {
		sim.moveRows(-1)
		r.check("move up")
	}
}

func TestStitchSkipsAnUnchangedView(t *testing.T) {
	var st stitcher
	v := view{rows: []string{"a b c"}, width: 10}
	st.update(v)
	st.text = "changed behind its back"
	if got := st.update(v); got != "changed behind its back" {
		t.Fatalf("an unchanged view was merged again: %q", got)
	}
}

func TestStitchEnterThenPauseKeepsSpacingExact(t *testing.T) {
	// Found in tmux, which sends Enter and the next letters separately: a
	// save between them saw an empty last row, and the line break doubled.
	sim := &boxSim{width: 60, cap: 6}
	r := &stitchRun{t: t, sim: sim, strict: true, mustBeExact: true}
	for i := 1; i <= 20; i++ {
		if i > 1 {
			sim.insert("\n")
			r.check("Enter")
		}
		sim.insert(fmt.Sprintf("typed line %d", i))
		r.check("type")
	}
	if got := r.st.text; got != sim.text {
		t.Fatalf("spacing drifted:\ngot  %q\nwant %q", got, sim.text)
	}
	// Enter at the end of a line whose next line is out of sight adds a
	// real blank line: it must survive.
	sim.cur = strings.Index(sim.text, "typed line 3") + len("typed line 3")
	for i := 0; i < 10; i++ {
		sim.moveRows(0)
		r.check("move")
	}
}
