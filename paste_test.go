package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestPasteTrackerSplitMarkers(t *testing.T) {
	var p pasteTracker
	input := "typed\x1b[200~line one\r\nline two\rline three\x1b[201~more\x1b[200~second\x1b[201~"
	// Feed it one byte at a time: markers arrive cut in every possible place.
	for i := 0; i < len(input); i++ {
		p.feed([]byte{input[i]})
	}
	got := p.all()
	if len(got) != 2 || got[0] != "line one\nline two\nline three" || got[1] != "second" {
		t.Fatalf("pastes %q", got)
	}
	p.reset()
	if len(p.all()) != 0 {
		t.Fatal("reset kept pastes")
	}
}

// A lone Esc at the end of a read is held back, outside a paste only, and
// only while it is alone: the next read tells an Esc key from a paste.
func TestPasteTrackerHoldsALoneEsc(t *testing.T) {
	var p pasteTracker
	for _, c := range []struct {
		in    string
		typed string
		holds bool
	}{
		{"text\x1b", "text", true},
		{"x", "\x1bx", false},
		{"\x1b", "", true},
		{"[200~pasted\x1b", "", false}, // a paste after all, and inside it
		{"[201~\x1b[", "", false},
		{"A", "\x1b[A", false},
	} {
		if typed := string(p.feed([]byte(c.in))); typed != c.typed || p.holdsEsc() != c.holds {
			t.Fatalf("after %q: typed %q, holds an Esc %v; want %q, %v", c.in, typed, p.holdsEsc(), c.typed, c.holds)
		}
	}
	if got := p.all(); len(got) != 1 || got[0] != "pasted" {
		t.Fatalf("pastes %q", got)
	}
}

func TestPasteExpand(t *testing.T) {
	var p pasteTracker
	forty := make([]string, 40)
	for i := range forty {
		forty[i] = "pasted"
	}
	big := strings.Join(forty, "\n")
	p.feed([]byte("\x1b[200~short\x1b[201~"))
	p.feed([]byte("\x1b[200~" + big + "\x1b[201~"))
	p.feed([]byte("\x1b[200~" + strings.Repeat("x", 2000) + "\x1b[201~"))

	draft := "before short [Pasted text #1 +39 lines] mid [Pasted text #2] after [Pasted text #3 +5 lines]"
	got := p.expand(draft, "", &claude)
	want := "before short " + big + " mid " + strings.Repeat("x", 2000) + " after [Pasted text #3 +5 lines]"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestPasteFeedReturnsOnlyTypedBytes(t *testing.T) {
	var p pasteTracker
	typed := p.feed([]byte("ab\x1b[200~x\x0by\x1b[201~\x7f"))
	if string(typed) != "ab\x7f" {
		t.Fatalf("typed %q", typed)
	}
}

// A paste Claude Code took in as typed text, or turned into an image, never
// fills a placeholder: a pasted image path shows as "[Image #N]", and a
// short paste can be edited until the draft no longer holds it.
func TestPasteExpandSkipsPastesShownAsSomethingElse(t *testing.T) {
	var p pasteTracker
	long := strings.Repeat("y", 900)
	p.feed([]byte("\x1b[200~/tmp/screenshot.png\x1b[201~"))
	p.feed([]byte("\x1b[200~short one\x1b[201~"))
	p.feed([]byte("\x1b[200~" + long + "\x1b[201~"))
	draft := "look [Image #1] and [Audio #2], short two, [Pasted text #3]"
	want := "look [Image #1] and [Audio #2], short two, " + long
	if got := p.expand(draft, "", &claude); got != want {
		t.Fatalf("got %q", got)
	}
}

// Claude Code turns tabs into 4 spaces before it measures a paste, and
// lets fewer line breaks through in a window under 12 rows.
func TestClaudeCollapses(t *testing.T) {
	for _, c := range []struct {
		paste string
		rows  int
		want  bool
	}{
		{"a\nb\nc", 40, false},
		{"a\nb\nc\nd", 40, true},
		{"a\nb\nc", 12, false},
		{"a\nb\nc", 11, true},
		{"a\nb", 11, false},
		{"a\nb", 10, true},
		{"a", 5, false},
		{strings.Repeat("x", 800), 40, false},
		{strings.Repeat("x", 801), 40, true},
		{strings.Repeat("é", 801), 40, true},
		{strings.Repeat("😀", 401), 40, true}, // two UTF-16 units each
		{strings.Repeat("😀", 400), 40, false},
		{strings.Repeat("cell\t", 150), 40, true}, // 750 characters, 1,200 with the tabs as spaces
		{strings.Repeat("cell ", 150), 40, false},
	} {
		if got := claude.pastes[0].collapses(c.paste, c.rows); got != c.want {
			t.Errorf("collapses(%d bytes, %d breaks, %d rows) = %v, want %v", len(c.paste), strings.Count(c.paste, "\n"), c.rows, got, c.want)
		}
	}
}

// Whether a paste fills a placeholder depends on the window height when it
// was pasted, not on the height now.
func TestPasteExpandUsesTheHeightAtPasteTime(t *testing.T) {
	var p pasteTracker
	p.resize(11)
	p.feed([]byte("\x1b[200~one\ntwo\nthree\x1b[201~"))
	p.resize(40)
	if got := p.expand("[Pasted text #1 +2 lines]", "", &claude); got != "one\ntwo\nthree" {
		t.Fatalf("got %q", got)
	}
	p.reset()
	p.feed([]byte("\x1b[200~one\ntwo\nthree\x1b[201~"))
	if got := p.expand("[Pasted text #1 +2 lines]", "", &claude); got != "[Pasted text #1 +2 lines]" {
		t.Fatalf("a paste Claude Code shows inline in 40 rows filled a placeholder: %q", got)
	}
}

// Claude Code shows a draft over 10,000 characters with its middle cut
// out. The middle comes back from the draft saved before when both sides
// match it, and stays for the saves after.
func TestPasteExpandPutsBackATruncatedMiddle(t *testing.T) {
	var p pasteTracker
	head, tail := "the start\n", " and so the end"
	mid := strings.Repeat("middle words ", 800) + "\nline two\nline three"
	prev := head + mid + tail
	ph := "[...Truncated text #4 +2 lines...]"
	if got := p.expand(head+ph+tail, prev, &claude); got != prev {
		t.Fatalf("got %q", got)
	}
	if got := p.expand(head+ph+tail+" and more", prev, &claude); got != prev+" and more" {
		t.Fatalf("an edit after the cut: got %q", got)
	}
	// Deleting the first word after the cut keeps the middle as it was:
	// the words deleted are not taken into it.
	if got := p.expand(head+ph+" so the end", prev, &claude); got != head+mid+" so the end" {
		t.Fatalf("a delete after the cut: got %q", got)
	}
	if n := len(p.all()); n != 0 {
		t.Fatalf("the middle was kept as %d pastes", n)
	}
	p.reset()
	if got := p.expand(head+ph+tail, "", &claude); got != head+ph+tail {
		t.Fatalf("a reset kept the middle: %q", got)
	}
	// The middle is not put back from a draft that does not match it.
	for _, old := range []string{
		"other start\n" + mid + tail,          // the start differs
		head + mid + " and another end",       // the end differs
		head + "one\ntwo\nthree\nfour" + tail, // another line count
		head + ph + tail,                      // the placeholder itself
		"the start\n and so the end",          // shorter than both sides
	} {
		if got := p.expand(head+ph+tail, old, &claude); got != head+ph+tail {
			t.Fatalf("put back %q from %q", got, old)
		}
	}
	// Both sides can overlap in a draft shorter than the two together.
	cut := "aaa\n[...Truncated text #5 +0 lines...]\naaa"
	if got := p.expand(cut, "aaa\naaa", &claude); got != cut {
		t.Fatalf("put back %q from overlapping sides", got)
	}
}

// After Ctrl+G the cut middle shows as [Pasted text #N +M lines], with the
// number of the cut. It is filled from the cut, not from an earlier paste
// with the same line count, whose start and end the box may not show.
func TestPasteExpandFillsACutMiddleByNumber(t *testing.T) {
	var p pasteTracker
	var lines []string
	for i := 0; i < 138; i++ {
		lines = append(lines, fmt.Sprintf("line %03d %s", i, strings.Repeat("word ", 14)))
	}
	// No line break in the first or last 500 characters.
	text := strings.Repeat("start ", 100) + strings.Join(lines, "\n") + strings.Repeat(" end", 150)
	p.feed([]byte(pasteStart))
	p.feed([]byte(text))
	p.feed([]byte(pasteEnd))
	if got := p.expand("[Pasted text #1 +137 lines]", "", &claude); got != text {
		t.Fatalf("the paste: got %q", got)
	}
	// Pasting it again shows it whole, then Claude Code cuts it.
	p.feed([]byte(string(pasteStart) + text + string(pasteEnd)))
	if got := p.expand(text, text, &claude); got != text {
		t.Fatalf("the paste again: got %q", got)
	}
	head, tail := text[:500], text[len(text)-500:]
	if got := p.expand(head+"[...Truncated text #2 +137 lines...]"+tail, text, &claude); got != text {
		t.Fatalf("the cut: got %d characters", len(got))
	}
	if got := p.expand(head+"[Pasted text #2 +137 lines]"+tail, text, &claude); got != text {
		t.Fatalf("after the editor: got %d characters, want %d", len(got), len(text))
	}
}

// Claude Code puts a paste back in the text when it cuts a middle holding
// its placeholder, and leaves one in the last 500 characters as it is. A
// placeholder keeps the paste it was first filled with, so the one left
// is not filled with the one folded into the middle.
func TestPasteExpandKeepsEachNumbersPaste(t *testing.T) {
	var p pasteTracker
	a, b := strings.Repeat("a", 2000), strings.Repeat("b", 900)
	p.feed([]byte(string(pasteStart) + a + string(pasteEnd)))
	p.feed([]byte(string(pasteStart) + b + string(pasteEnd)))
	before, between := strings.Repeat("typed ", 100), strings.Repeat("words ", 1700)
	prev := p.expand(before+"[Pasted text #1]"+between+"[Pasted text #2]", "", &claude)
	if prev != before+a+between+b {
		t.Fatalf("got %q", prev)
	}
	shown := before + a + between + "[Pasted text #2]"
	cut := shown[:500] + "[...Truncated text #3 +0 lines...]" + shown[len(shown)-500:]
	if got := p.expand(cut, prev, &claude); got != prev {
		t.Fatalf("the cut: got %d characters, want %d", len(got), len(prev))
	}
}

// A paste whose placeholder was deleted does not fill the placeholder of
// a later paste with the same line count.
func TestPasteExpandSkipsAnotherNumbersPaste(t *testing.T) {
	var p pasteTracker
	a, b := strings.Repeat("a", 2000), strings.Repeat("b", 900)
	p.feed([]byte(string(pasteStart) + a + string(pasteEnd)))
	if got := p.expand("[Pasted text #1]", "", &claude); got != a {
		t.Fatalf("got %q", got)
	}
	p.feed([]byte(string(pasteStart) + b + string(pasteEnd)))
	if got := p.expand("typed [Pasted text #2]", "typed", &claude); got != "typed "+b {
		t.Fatalf("got %q", got)
	}
}

// Codex's rule (docs/research/codex.md sections 4 and 10): a paste over
// 1,000 characters shows as "[Pasted Content N chars]", N counting every
// character with line breaks as LF, and a second one of the same size
// still in the box as "... #2". Placeholders pair with pastes by that
// count, the plain label with the first paste of its size and #2 with the
// next, wherever each sits in the draft.
func TestPasteExpandCodex(t *testing.T) {
	a, b := strings.Repeat("a", 1001), strings.Repeat("b", 1001)
	accents := strings.Repeat("é", 1001) // 2,002 bytes
	lines := strings.Repeat(strings.Repeat("l", 89)+"\r", 11) + strings.Repeat("l", 89)
	pasted := func() *pasteTracker {
		var p pasteTracker
		for _, text := range []string{strings.Repeat("s", 1000), a, b, accents, lines} {
			p.feed([]byte(string(pasteStart) + text + string(pasteEnd)))
		}
		return &p
	}
	long := strings.ReplaceAll(lines, "\r", "\n") // 1,079 characters
	for _, c := range []struct{ draft, want string }{
		{"before [Pasted Content 1001 chars] after [Pasted Content 1001 chars] #2", "before " + a + " after " + b},
		{"[Pasted Content 1001 chars] #2 then [Pasted Content 1001 chars]", b + " then " + a},
		{"[Pasted Content 1079 chars]", long},
		{"[Pasted Content 1000 chars] stays: that paste went in as text", "[Pasted Content 1000 chars] stays: that paste went in as text"},
		{"[Pasted Content 999 chars] matches no paste", "[Pasted Content 999 chars] matches no paste"},
	} {
		if got := pasted().expand(c.draft, "", &codex); got != c.want {
			t.Errorf("%q: got %.60q…", c.draft, got)
		}
	}
	// The é paste is 1,001 characters, so it is one of the 1001s: third.
	p := pasted()
	if got := p.expand("[Pasted Content 1001 chars] [Pasted Content 1001 chars] #2 [Pasted Content 1001 chars] #3", "", &codex); got != a+" "+b+" "+accents {
		t.Errorf("three of a size: got %.60q…", got)
	}
	// Once paired, a label keeps its paste: with the plain one deleted, #2
	// is still b.
	if got := p.expand("only [Pasted Content 1001 chars] #2", "", &codex); got != "only "+b {
		t.Errorf("after a delete: got %.60q…", got)
	}
}

// Codex gives the plain label back once no paste of that size is left in
// the box: the next paste of that size shows under it, and the paste whose
// label it took fills nothing again.
func TestPasteExpandCodexReusedLabel(t *testing.T) {
	var p pasteTracker
	a, b := strings.Repeat("a", 1001), strings.Repeat("b", 1001)
	ph := "[Pasted Content 1001 chars]"
	p.feed([]byte(string(pasteStart) + a + string(pasteEnd)))
	if got := p.expand("x "+ph, "", &codex); got != "x "+a {
		t.Fatalf("the first paste: got %.60q…", got)
	}
	if got := p.expand("x ", "x "+a, &codex); got != "x " {
		t.Fatalf("its label deleted: got %.60q…", got)
	}
	p.feed([]byte(string(pasteStart) + b + string(pasteEnd)))
	if got := p.expand("x "+ph, "x ", &codex); got != "x "+b {
		t.Fatalf("the label back for the next paste: got %.60q…, want the b paste", got)
	}
}

// Codex gives a deleted label to the next paste that needs it, also
// between two reads: the draft must hold the paste the box shows, not the
// one deleted. A label missing from one read and back in the next, with
// no paste since, still holds its paste. Codex numbers a new paste one
// above the highest label of its size in the box, so the plain label and
// #2 still shown keep theirs when a new one shows as #2 or #3.
func TestPasteExpandCodexLabelGivenAgain(t *testing.T) {
	a, b, c := strings.Repeat("a", 1001), strings.Repeat("b", 1001), strings.Repeat("c", 1001)
	ph, ph2 := "[Pasted Content 1001 chars]", "[Pasted Content 1001 chars] #2"
	paste := func(p *pasteTracker, text string) { p.feed([]byte(string(pasteStart) + text + string(pasteEnd))) }
	expand := func(p *pasteTracker, draft, prev, want, what string) {
		t.Helper()
		if got := p.expand(draft, prev, &codex); got != want {
			t.Fatalf("%s: got %.80q…, want %.80q…", what, got, want)
		}
	}

	// Deleted and pasted again, same size, before the next read.
	var p pasteTracker
	paste(&p, a)
	expand(&p, "x "+ph, "", "x "+a, "the first paste")
	paste(&p, b)
	expand(&p, "x "+ph, "x "+a, "x "+b, "the label given again within one read")
	expand(&p, "x "+ph, "x "+b, "x "+b, "the read after")

	// The label missing from one read (scrolled out, or the stitcher
	// starting over) and back, no paste since.
	p = pasteTracker{}
	paste(&p, a)
	expand(&p, "x "+ph, "", "x "+a, "the first paste")
	expand(&p, "x ", "x "+a, "x ", "the label out of the draft")
	expand(&p, "x "+ph, "x ", "x "+a, "the label back")

	// A second paste of the size, the first still in the box: #2.
	p = pasteTracker{}
	paste(&p, a)
	expand(&p, ph, "", a, "the first paste")
	paste(&p, b)
	expand(&p, ph+" "+ph2, a, a+" "+b, "a second label")

	// #2 deleted and given again, the plain label kept.
	paste(&p, c)
	expand(&p, ph+" "+ph2, a+" "+b, a+" "+c, "#2 given again")

	// The plain label deleted with #2 in the box: the new paste is #3.
	p = pasteTracker{}
	paste(&p, a)
	paste(&p, b)
	expand(&p, ph+" "+ph2, "", a+" "+b, "two pastes")
	paste(&p, c)
	expand(&p, ph2+" "+ph+" #3", a+" "+b, b+" "+c, "#3 after the plain label went")
}
