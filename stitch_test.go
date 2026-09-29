package main

import (
	"cmp"
	"fmt"
	"math/rand"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
)

// boxSim draws a draft the way a profile's models say its agent does:
// rows wrapped by its unwrap rule, and a box that stops growing at cap
// rows and scrolls to keep the cursor in sight. With jump set it puts the
// cursor mid-box when the cursor leaves the view; without, it scrolls one
// row at a time.
type boxSim struct {
	text       string
	cur        int // cursor, as a byte offset into text
	width, cap int
	top        int
	jump       bool
	wrap       unwrapRule
	// edit is the byte range of the last change; seen says whether the
	// last view showed all of it.
	editFrom, editTo int
	seen             bool
}

// rows wraps the text by the sim's rule and also returns where each row
// starts. Widths are screen columns: a wide character takes two.
func (b *boxSim) rows() ([]string, []int) {
	return b.wrap.wrap(b.text, b.width)
}

// rowAt returns the row the insertion point at offset off sits on.
func rowAt(starts []int, off int) int {
	cr := 0
	for i, s := range starts {
		if s <= off {
			cr = i
		}
	}
	return cr
}

func (b *boxSim) view(t *testing.T) view {
	rows, starts := b.rows()
	for i, r := range rows {
		if starts[i] < 0 || starts[i]+len(r) > len(b.text) || b.text[starts[i]:starts[i]+len(r)] != r ||
			(i > 0 && starts[i] < starts[i-1]) {
			t.Fatalf("row %d %q does not start at %d of the text:\n%q", i, r, starts[i], b.text[max(0, len(b.text)-80):])
		}
	}
	cr := rowAt(starts, b.cur)
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

// moveRows puts the cursor on a word end about d rows away. A row with no
// word end (a blank line) is stepped over, further the same way; with
// none left that way the cursor stays.
func (b *boxSim) moveRows(d int) {
	rows, starts := b.rows()
	cr := rowAt(starts, b.cur)
	step := 1
	if d < 0 {
		step = -1
	}
	ends := b.wordEnds()
	for target := clamp(cr+d, 0, len(rows)-1); target >= 0 && target < len(rows); target += step {
		for _, e := range ends {
			// (A word end that puts the cursor on another row, such as the
			// end of a full line, where Codex's insertion point sits on
			// the empty row after it, is not on this one.)
			if e >= starts[target] && e <= starts[target]+len(rows[target]) && rowAt(starts, e) == target {
				b.cur = e
				return
			}
		}
		if d == 0 {
			return
		}
	}
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
	// bytes counts saves that are the draft byte for byte (byteExact),
	// raw those with no allowance at all.
	bytes, raw int
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
	return rowAt(starts, b.cur)
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
	if byteExact(got, r.sim.text, r.sim.width, r.sim.wrap) {
		r.bytes++
	}
	if got == r.sim.text {
		r.raw++
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
	got := r.st.update(v, r.sim.wrap)
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
// it, and checks after every save that not a word was lost or doubled. The
// box is drawn by prof's models.
func fuzzStitch(t *testing.T, prof *profile, seed int64, vocab int, jump bool, mode string) *stitchRun {
	rng := rand.New(rand.NewSource(seed))
	word := func(n int) string {
		if vocab > 0 {
			return fmt.Sprintf("w%d", rng.Intn(vocab))
		}
		return fmt.Sprintf("w%d", n)
	}
	sim := &boxSim{width: 40 + rng.Intn(60), cap: 5 + rng.Intn(10), jump: jump, wrap: prof.unwrap}
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
			switch rng.Intn(3) {
			case 0:
				sim.insert("\n" + word(n))
				r.check("new line")
			case 1:
				sim.insert("\n\n" + word(n))
				r.check("blank line")
			default:
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
	return r
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
// save) are misread; the guarantee makes that safe. Exact has three
// floors: the same words in the same order; the same bytes (byteExact),
// which catches lost and doubled line breaks; and identical, with no
// allowance, which catches a line break at a full row the stitcher kept
// before and now turns into a space. The floors sit at the rates measured
// with blank-line edits, so they can only go up. Each profile's fuzz draws
// the box by its own models and has floors of its own, the lowest rate
// measured at 4 seeds (the race detector's), 50 (CI's) and 150. Codex's
// box scrolls a row at a time on every seed, and its end row keeps more
// line breaks: at 150 seeds 62% of its saves are identical, against 56%
// for Claude Code, but only 56% at 4 seeds. pi's box scrolls a row at a
// time too, with no end row; with repeated words 50% of its saves are
// identical at 4 seeds, against 59% at 50 and 150.
//
// Every one of these runs is independent: its own simulator, its own
// stitcher, its own seeds, and nothing shared but the profile it reads.
// So they run in parallel, both tests at once, and the fuzz costs the
// busiest core rather than the sum of all twelve.

// fuzzFloors are the floors each mode's rates must stay above.
type fuzzFloors struct {
	mode                       string
	floor, byteFloor, rawFloor float64
}

func TestStitchFuzzUniqueWords(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		prof   *profile
		floors []fuzzFloors
	}{
		{&claude, []fuzzFloors{{"exact", 0.995, 0.88, 0.56}, {"window", 0.99, 0.88, 0.56}, {"unlimited", 0.95, 0.86, 0.55}}},
		{&codex, []fuzzFloors{{"exact", 0.995, 0.88, 0.56}, {"window", 0.99, 0.88, 0.56}, {"unlimited", 0.96, 0.87, 0.56}}},
		{&pi, []fuzzFloors{{"exact", 0.995, 0.88, 0.56}, {"window", 0.99, 0.88, 0.56}, {"unlimited", 0.97, 0.87, 0.56}}},
	} {
		for _, m := range c.floors {
			t.Run(c.prof.name+"/"+m.mode, func(t *testing.T) {
				t.Parallel()
				fuzzRate(t, c.prof, 0, m)
			})
		}
	}
}

// With a 40-word vocabulary the same words recur constantly, the hard case
// for lining views up, and some edits are ambiguous on screen: typing a
// word identical to the next hidden word looks like that word having been
// there all along.
func TestStitchFuzzRepeatedWords(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		prof   *profile
		floors fuzzFloors
	}{
		{&claude, fuzzFloors{"window", 0.95, 0.84, 0.56}},
		{&codex, fuzzFloors{"window", 0.94, 0.84, 0.63}},
		{&pi, fuzzFloors{"window", 0.95, 0.85, 0.50}},
	} {
		t.Run(c.prof.name, func(t *testing.T) {
			t.Parallel()
			fuzzRate(t, c.prof, 40, c.floors)
		})
	}
}

// fuzzRate runs the fuzz for prof over every seed. A profile whose box
// scrolls mid-box is drawn both ways, alternating by seed: a jump that
// puts the cursor mid-box, where moving over hidden rows leaves it, and
// one row at a time, as typing past the edge scrolls. The simulator moves
// the cursor several rows between saves, so it cannot draw the one-row
// steps in between.
func fuzzRate(t *testing.T, prof *profile, vocab int, f fuzzFloors) {
	t.Helper()
	exact, bytes, raw, saves := 0, 0, 0, 0
	for seed := int64(1); seed <= fuzzSeeds(); seed++ {
		r := fuzzStitch(t, prof, seed, vocab, prof.scroll == scrollMidBox && seed%2 == 0, f.mode)
		exact, bytes, raw, saves = exact+r.exact, bytes+r.bytes, raw+r.raw, saves+r.exact+r.near
	}
	rate, byteRate, rawRate := float64(exact)/float64(saves), float64(bytes)/float64(saves), float64(raw)/float64(saves)
	t.Logf("%d of %d saves exact (%.2f%%), %d byte-exact (%.2f%%), %d identical (%.2f%%)",
		exact, saves, 100*rate, bytes, 100*byteRate, raw, 100*rawRate)
	if rate < f.floor {
		t.Fatalf("exact rate %.4f below %.4f", rate, f.floor)
	}
	if byteRate < f.byteFloor {
		t.Fatalf("byte-exact rate %.4f below %.4f", byteRate, f.byteFloor)
	}
	if rawRate < f.rawFloor {
		t.Fatalf("identical rate %.4f below %.4f", rawRate, f.rawFloor)
	}
}

func TestStitchLongParagraphEditedWhileScrolledUp(t *testing.T) {
	// The review's case: 80 columns, a box capped at 10 rows, one long
	// paragraph, a word inserted near the top while the bottom is hidden.
	sim := &boxSim{width: 76, cap: 10, jump: true, wrap: claude.unwrap}
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
	sim := &boxSim{width: 60, cap: 10, wrap: claude.unwrap}
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
	st.update(view{rows: []string{"a"}, width: 10}, claude.unwrap)
	if got := st.update(view{empty: true}, claude.unwrap); got != "" {
		t.Fatalf("got %q", got)
	}
	if st.text != "" {
		t.Fatalf("text kept after empty: %q", st.text)
	}
}

func TestStitchNothingLinesUpStartsOver(t *testing.T) {
	st := stitcher{text: "the draft we know about, long enough to anchor", a: 0, b: 10}
	got := st.update(view{rows: []string{"completely different text here"}, width: 80, capped: true, cursor: 0}, claude.unwrap)
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
	got := st.update(view{rows: []string{"xxxxx", "end of the draft here"}, width: 25, capped: true, cursor: 1}, claude.unwrap)
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
		{"next word fits exactly: a hand newline", []string{"aaaa", "bbbbb"}, 10, "aaaa\nbbbbb"},
		{"next word one column too wide: a wrap", []string{"aaaa", "bbbbbb"}, 10, "aaaa bbbbbb"},
		{"two long words wrapped at a space", []string{"aa bbbbbb", "cccccc"}, 10, "aa bbbbbb cccccc"},
		{"wide character that did not fit leaves the row a cell short", []string{"x漢漢漢漢", "字字字"}, 10, "x漢漢漢漢字字字"},
		{"wide run longer than a row starts after a word", []string{"ab 漢漢漢", "字字字字"}, 9, "ab 漢漢漢字字字字"},
		{"wide words wrapped at a space", []string{"ab 漢漢", "字字"}, 8, "ab 漢漢 字字"},
		{"a cell short before a narrow character is a wrap", []string{"x漢漢漢漢", "abc"}, 10, "x漢漢漢漢 abc"},
		{"no rows", nil, 10, ""},
	}
	for _, c := range cases {
		if got := unwrap(c.rows, c.width); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// byteExact is the fuzz's only check on unwrap's full-row boundary, so it
// is pinned here: a checker with the same off-by-one as unwrap would
// excuse it.
func TestByteExact(t *testing.T) {
	cases := []struct {
		name       string
		got, truth string
		want       bool
	}{
		{"identical", "aa\nbb", "aa\nbb", true},
		{"break joined at a full row", "aaaa bbbb cccc", "aaaa bbbb\ncccc", true},
		{"break joined where the next word fits exactly", "aaaa bbbbb", "aaaa\nbbbbb", false},
		{"break joined one column past the fit", "aaaa bbbbbb", "aaaa\nbbbbbb", true},
		{"a line of several rows is judged by its last row", "aaaa bbbb cccc dddd", "aaaa bbbb cccc\ndddd", false},
		{"blank line lost", "aaaa bbbb\ncccc", "aaaa bbbb\n\ncccc", false},
		{"line break doubled", "aa\n\nbb", "aa\nbb", false},
		{"break before a blank line never joins", "aaaa bbbbb \ncccc", "aaaa bbbbb\n\ncccc", false},
		{"break ending an empty line never joins", "aa\n cccccccccc", "aa\n\ncccccccccc", false},
		{"a changed word", "aa\nbc", "aa\nbb", false},
	}
	for _, c := range cases {
		if got := byteExact(c.got, c.truth, 10, claude.unwrap); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	// A line that ends on a full row: Claude Code's screen shows a wrap
	// there, Codex's the empty row that ends the line.
	full, joined := "aaaaaaaaaa\nbb", "aaaaaaaaaa bb"
	if !byteExact(joined, full, 10, claude.unwrap) || byteExact(joined, full, 10, codex.unwrap) {
		t.Errorf("a break joined after a full line: excused for Claude %v, for Codex %v; want true, false",
			byteExact(joined, full, 10, claude.unwrap), byteExact(joined, full, 10, codex.unwrap))
	}
}

// wrapText wraps text the way Claude Code does: wordWrap's rows.
func wrapText(text string, width int) []string {
	rows, _ := wordWrap{}.wrap(text, width)
	return rows
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

// Codex's wrap, measured on 0.158.0 (testdata/codex/0.158.0/wrap.rec): a
// line that ends on a full row, a word two rows long included, gets one
// empty row more; one a cell short, or with a word left to wrap, does not.
func TestWordWrapEndRow(t *testing.T) {
	q, s := strings.Repeat("q", 10), strings.Repeat("s", 20)
	for _, c := range []struct {
		text   string
		rows   []string
		starts []int
		back   string // what the rows unwrap to, when not the text
	}{
		{q, []string{q, ""}, []int{0, 10}, ""},
		{q + "\n\nnext", []string{q, "", "", "next"}, []int{0, 10, 11, 12}, ""},
		{q + "\nnext", []string{q, "", "next"}, []int{0, 10, 11}, ""},
		{"abcd efghi\nx", []string{"abcd efghi", "", "x"}, []int{0, 10, 11}, ""},
		{s + "\nafter", []string{s[:10], s[10:], "", "after"}, []int{0, 10, 20, 21}, ""},
		{strings.Repeat("a", 9) + "漢", []string{strings.Repeat("a", 9), "漢"}, []int{0, 9}, ""},
		// A word that fills a row, then a wrap, reads as one word broken
		// at the edge: the screen is the same (the known limit).
		{q + " next", []string{q, "next"}, []int{0, 11}, q + "next"},
		{"short\nx", []string{"short", "x"}, []int{0, 6}, ""},
	} {
		rows, starts := codex.unwrap.wrap(c.text, 10)
		if !slices.Equal(rows, c.rows) || !slices.Equal(starts, c.starts) {
			t.Errorf("%q: rows %q at %v, want %q at %v", c.text, rows, starts, c.rows, c.starts)
		}
		for _, r := range rows {
			if runewidth.StringWidth(r) > 10 {
				t.Errorf("%q: row %q wider than the box", c.text, r)
			}
		}
		if want := cmp.Or(c.back, c.text); codex.unwrap.unwrap(rows, 10) != want {
			t.Errorf("%q: unwrapped as %q, want %q", c.text, codex.unwrap.unwrap(rows, 10), want)
		}
	}
	// Without the end row, Claude Code's rows join a full line with the
	// next: the limit Codex's screen does not have.
	if got := claude.unwrap.unwrap([]string{q, "next"}, 10); got != q+"next" {
		t.Errorf("Claude Code's rows %q", got)
	}
}

func TestStitchWideCharacterText(t *testing.T) {
	// A paragraph with no spaces, scrolled through a 5-row box: the review
	// found the word stitcher kept only what was on screen.
	var text strings.Builder
	for i := 0; i < 260; i++ {
		text.WriteRune(rune(0x4e00 + i))
	}
	sim := &boxSim{width: 20, cap: 5, wrap: claude.unwrap}
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
	st.update(v, claude.unwrap)
	st.text = "changed behind its back"
	if got := st.update(v, claude.unwrap); got != "changed behind its back" {
		t.Fatalf("an unchanged view was merged again: %q", got)
	}
}

func TestStitchEnterThenPauseKeepsSpacingExact(t *testing.T) {
	// Found in tmux, which sends Enter and the next letters separately: a
	// save between them saw an empty last row, and the line break doubled.
	sim := &boxSim{width: 60, cap: 6, wrap: claude.unwrap}
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

func TestStitchEmptyTopRowReplacesTheSameBreak(t *testing.T) {
	// The last view began with a blank row; this one too, with a word typed
	// below it. The blank row's line break is the one already in the text,
	// not a new one.
	st := stitcher{text: "one two\n\nthree four", a: len("one two\n"), b: len("one two\n\nthree four")}
	got := st.update(view{rows: []string{"", "three four five"}, width: 40, capped: true, cursor: 1, cursorEnd: true}, claude.unwrap)
	if want := "one two\n\nthree four five"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
