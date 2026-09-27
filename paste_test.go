package main

import (
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

func TestClaudeCollapses(t *testing.T) {
	for _, c := range []struct {
		paste string
		want  bool
	}{
		{"a\nb\nc", false},
		{"a\nb\nc\nd", true},
		{strings.Repeat("x", 800), false},
		{strings.Repeat("x", 801), true},
		{strings.Repeat("é", 801), true},
		{strings.Repeat("😀", 401), true}, // two UTF-16 units each
		{strings.Repeat("😀", 400), false},
	} {
		if got := claude.collapses(c.paste); got != c.want {
			t.Errorf("collapses(%d bytes, %d breaks) = %v, want %v", len(c.paste), strings.Count(c.paste, "\n"), got, c.want)
		}
	}
}

// Claude Code shows a draft over 10,000 characters with its middle cut
// out. The middle comes back from the draft saved before when both sides
// match it, and stays for the saves after.
func TestPasteExpandPutsBackATruncatedMiddle(t *testing.T) {
	var p pasteTracker
	head, tail := "the start\n", "\nthe end"
	mid := strings.Repeat("middle words ", 800) + "\nline two\nline three"
	prev := head + mid + tail
	ph := "[...Truncated text #4 +2 lines...]"
	if got := p.expand(head+ph+tail, prev, &claude); got != prev {
		t.Fatalf("got %q", got)
	}
	if got := p.expand(head+ph+tail+" and more", prev, &claude); got != prev+" and more" {
		t.Fatalf("an edit after the cut: got %q", got)
	}
	p.reset()
	if got := p.expand(head+ph+tail, "", &claude); got != head+ph+tail {
		t.Fatalf("a reset kept the middle: %q", got)
	}
	// The middle is not put back from a draft that does not match it.
	for _, old := range []string{
		"other start\n" + mid + tail,          // the start differs
		head + mid + "\nanother end",          // the end differs
		head + "one\ntwo\nthree\nfour" + tail, // another line count
		head + ph + tail,                      // the placeholder itself
	} {
		if got := p.expand(head+ph+tail, old, &claude); got != head+ph+tail {
			t.Fatalf("put back %q from %q", got, old)
		}
	}
}
