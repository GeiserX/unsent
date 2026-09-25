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
	got := p.expand(draft)
	want := "before short " + big + " mid " + strings.Repeat("x", 2000) + " after [Pasted text #3 +5 lines]"
	if got != want {
		t.Fatalf("got %q", got)
	}
}
