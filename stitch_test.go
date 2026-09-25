package main

import (
	"fmt"
	"strings"
	"testing"
)

// boxSim draws a draft the way Claude Code does: wrapped rows, a box that
// stops growing at cap rows and scrolls just enough to keep the cursor row
// in sight.
type boxSim struct {
	width, cap int
	top        int
}

func (b *boxSim) view(text string, cursorRow int) view {
	rows := wrapText(text, b.width)
	if cursorRow < 0 || cursorRow >= len(rows) {
		cursorRow = len(rows) - 1
	}
	h := min(len(rows), b.cap)
	if cursorRow < b.top {
		b.top = cursorRow
	}
	if cursorRow >= b.top+h {
		b.top = cursorRow - h + 1
	}
	if b.top+h > len(rows) {
		b.top = len(rows) - h
	}
	if b.top < 0 {
		b.top = 0
	}
	return view{
		rows:   append([]string(nil), rows[b.top:b.top+h]...),
		cursor: cursorRow - b.top,
		width:  b.width,
		capped: len(rows) >= b.cap,
	}
}

func lineList(from, to int) []string {
	var out []string
	for i := from; i <= to; i++ {
		out = append(out, fmt.Sprintf("line %d", i))
	}
	return out
}

func TestStitchTypingPastTheCap(t *testing.T) {
	sim := &boxSim{width: 60, cap: 10}
	var st stitcher
	var got string
	for n := 1; n <= 80; n++ {
		got = st.update(sim.view(strings.Join(lineList(1, n), "\n"), -1))
	}
	if want := strings.Join(lineList(1, 80), "\n"); got != want {
		t.Fatalf("got %d lines, want 80:\n%s", strings.Count(got, "\n")+1, got)
	}
}

func TestStitchEditWhileScrolledUp(t *testing.T) {
	sim := &boxSim{width: 60, cap: 10}
	var st stitcher
	lines := lineList(1, 40)
	for n := 1; n <= 40; n++ {
		st.update(sim.view(strings.Join(lines[:n], "\n"), -1))
	}
	// Walk the cursor up to line 5, one row per save.
	for row := 38; row >= 4; row-- {
		st.update(sim.view(strings.Join(lines, "\n"), row))
	}
	lines[4] += " edited"
	got := st.update(sim.view(strings.Join(lines, "\n"), 4))
	if want := strings.Join(lines, "\n"); got != want {
		t.Fatalf("got:\n%s", got)
	}
	// Insert a new line under line 5 while scrolled up.
	lines = append(lines[:5], append([]string{"inserted"}, lines[5:]...)...)
	got = st.update(sim.view(strings.Join(lines, "\n"), 5))
	if want := strings.Join(lines, "\n"); got != want {
		t.Fatalf("after insert got:\n%s", got)
	}
}

func TestStitchDeleteAtTheEnd(t *testing.T) {
	sim := &boxSim{width: 60, cap: 10}
	var st stitcher
	lines := lineList(1, 30)
	for n := 1; n <= 30; n++ {
		st.update(sim.view(strings.Join(lines[:n], "\n"), -1))
	}
	var got string
	for n := 29; n >= 25; n-- {
		got = st.update(sim.view(strings.Join(lines[:n], "\n"), -1))
	}
	if want := strings.Join(lines[:25], "\n"); got != want {
		t.Fatalf("got:\n%s", got)
	}
}

func TestStitchScrollDownKeepsRowsBelow(t *testing.T) {
	sim := &boxSim{width: 60, cap: 10}
	var st stitcher
	lines := lineList(1, 30)
	text := strings.Join(lines, "\n")
	for n := 1; n <= 30; n++ {
		st.update(sim.view(strings.Join(lines[:n], "\n"), -1))
	}
	for row := 29; row >= 0; row-- {
		st.update(sim.view(text, row))
	}
	var got string
	for row := 0; row < 20; row++ {
		got = st.update(sim.view(text, row))
	}
	if got != text {
		t.Fatalf("got:\n%s", got)
	}
}

func TestStitchEditOnWindowsFirstRow(t *testing.T) {
	sim := &boxSim{width: 60, cap: 10}
	var st stitcher
	lines := lineList(1, 30)
	for n := 1; n <= 30; n++ {
		st.update(sim.view(strings.Join(lines[:n], "\n"), -1))
	}
	for row := 28; row >= 20; row-- {
		st.update(sim.view(strings.Join(lines, "\n"), row))
	}
	lines[20] = "changed on the top row"
	got := st.update(sim.view(strings.Join(lines, "\n"), 20))
	if want := strings.Join(lines, "\n"); got != want {
		t.Fatalf("got:\n%s", got)
	}
}

func TestStitchResize(t *testing.T) {
	sim := &boxSim{width: 96, cap: 10}
	var st stitcher
	para := wordsFrom(0, 37)
	lines := append(lineList(1, 20), para)
	for n := 1; n <= len(lines); n++ {
		st.update(sim.view(strings.Join(lines[:n], "\n"), -1))
	}
	sim.width, sim.top = 66, 0
	var got string
	for i := 1; i <= 7; i++ {
		lines = append(lines, fmt.Sprintf("more %d", i))
		got = st.update(sim.view(strings.Join(lines, "\n"), -1))
	}
	if want := strings.Join(lines, "\n"); got != want {
		t.Fatalf("got:\n%s", got)
	}
}

func TestStitchEmptyResets(t *testing.T) {
	var st stitcher
	st.update(view{rows: []string{"a"}, width: 10})
	if got := st.update(view{empty: true}); got != "" {
		t.Fatalf("got %q", got)
	}
	if len(st.rows) != 0 {
		t.Fatalf("rows kept after empty: %q", st.rows)
	}
}

func TestStitchNothingLinesUp(t *testing.T) {
	st := stitcher{rows: []string{"a", "b", "c", "d"}, width: 10, top: 1, h: 2}
	got := st.update(view{rows: []string{"x", "y"}, width: 10, capped: true, cursor: 0})
	if want := "a\nx\ny\nd"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
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
}
