package main

import (
	"strconv"
	"strings"
	"testing"
)

const rule = "────────────────────────────────────────────────────────────────────────────────────────────────────"

// Screens below are copied from Claude Code 2.1.282 at 100x30. "⟦…⟧" marks
// dim text and "▮" the cursor.
func claudeScreen(box ...string) string {
	head := []string{
		" ▐▛███▛█   Claude Code v2.1.282",
		"▝▜██████▀  Opus 5.5 · Claude Max",
		" ▝▝   ▝▝   ~/code/project",
		"",
		"❯ an earlier prompt in the transcript",
		"",
		"● An earlier answer.",
		"",
		rule,
	}
	tail := []string{rule, "  ⏵⏵ bypass permissions on (shift+tab to cycle)"}
	lines := append(append(head, box...), tail...)
	for len(lines) < 30 {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func TestClaudeBoxEmptyHint(t *testing.T) {
	s := screenFromText(claudeScreen("❯ ▮⟦Try \"how do I log an error?\"⟧"), 100)
	v, ok := claudeBox(s)
	if !ok || !v.empty {
		t.Fatalf("ok=%v empty=%v rows=%q", ok, v.empty, v.rows)
	}
}

func TestClaudeBoxTypedText(t *testing.T) {
	s := screenFromText(claudeScreen("❯ hello from unsent", "  second line▮"), 100)
	v, ok := claudeBox(s)
	if !ok || v.empty {
		t.Fatalf("ok=%v empty=%v", ok, v.empty)
	}
	if strings.Join(v.rows, "|") != "hello from unsent|second line" {
		t.Fatalf("rows %q", v.rows)
	}
	if v.cursor != 1 || !v.cursorEnd || v.width != 96 || v.capped {
		t.Fatalf("cursor=%d end=%v width=%d capped=%v", v.cursor, v.cursorEnd, v.width, v.capped)
	}
	s = screenFromText(claudeScreen("❯\u00a0hello▮ world"), 100)
	if v, _ := claudeBox(s); v.cursor != 0 || v.cursorEnd {
		t.Fatalf("cursor mid-row read as at the end: %+v", v)
	}
}

func TestClaudeBoxTypedTextIsNotAHint(t *testing.T) {
	// Real text that is only partly dim (a highlighted @file) is still a draft.
	s := screenFromText(claudeScreen("❯ look at ⟦@main.go⟧▮"), 100)
	v, ok := claudeBox(s)
	if !ok || v.empty || v.rows[0] != "look at @main.go" {
		t.Fatalf("ok=%v empty=%v rows=%q", ok, v.empty, v.rows)
	}
}

func TestClaudeBoxCapped(t *testing.T) {
	var box []string
	for i := 41; i <= 50; i++ {
		prefix := "  "
		if i == 41 {
			prefix = "❯ "
		}
		box = append(box, prefix+"typed line "+strconv.Itoa(i))
	}
	s := screenFromText(claudeScreen(box...), 100)
	v, ok := claudeBox(s)
	if !ok || !v.capped || len(v.rows) != 10 || v.rows[0] != "typed line 41" {
		t.Fatalf("ok=%v capped=%v rows=%q", ok, v.capped, v.rows)
	}
	if v.cursor != -1 {
		t.Fatalf("cursor %d without a cursor on screen", v.cursor)
	}
}

func TestClaudeBoxShellMode(t *testing.T) {
	s := screenFromText(claudeScreen("! ls -la▮"), 100)
	v, ok := claudeBox(s)
	if !ok || v.empty || v.rows[0] != "!ls -la" {
		t.Fatalf("ok=%v rows=%q", ok, v.rows)
	}
	s = screenFromText(claudeScreen("!\u00a0▮⟦Try \"fix lint errors\"⟧"), 100)
	if v, ok := claudeBox(s); !ok || !v.empty {
		t.Fatalf("empty shell box: ok=%v empty=%v rows=%q", ok, v.empty, v.rows)
	}
}

func TestClaudeBoxNotOnScreen(t *testing.T) {
	trust := strings.Join([]string{
		rule,
		" Accessing workspace:",
		" /private/tmp/unsent-probe/cwd",
		" Security guide",
		" ❯ No, exit",
		"   Yes, I trust this folder",
		" Enter to confirm · Esc to cancel",
	}, "\n")
	if _, ok := claudeBox(screenFromText(trust, 100)); ok {
		t.Fatal("found a box in the trust dialog")
	}
	noEnd := strings.Join([]string{rule, "❯ typed", "  more"}, "\n")
	if _, ok := claudeBox(screenFromText(noEnd, 100)); ok {
		t.Fatal("found a box with no closing rule")
	}
}

func TestExtractorFor(t *testing.T) {
	if extractorFor("/usr/local/bin/claude") == nil {
		t.Fatal("no extractor for claude")
	}
	if extractorFor("vim") != nil {
		t.Fatal("extractor for vim")
	}
}

func TestIsRuleAndMarker(t *testing.T) {
	if isRule("────", 100) || !isRule(rule, 100) || isRule(rule+"x", 100) {
		t.Fatal("isRule")
	}
	if !hasMarker("❯", "❯") || !hasMarker("❯ x", "❯") || hasMarker("❯x", "❯") {
		t.Fatal("hasMarker")
	}
	small := &screen{rows: make([]screenRow, 4)}
	if small.rows2cap() != 1 {
		t.Fatalf("cap %d", small.rows2cap())
	}
}

func TestScreenText(t *testing.T) {
	s := screenFromText("ab⟦cd⟧▮\n", 6)
	if s.String() != "abcd\n" || s.curX != 4 || s.curY != 0 {
		t.Fatalf("%q %d %d", s.String(), s.curX, s.curY)
	}
	if s.rows[0].faintFrom(0) || !s.rows[0].faintFrom(2) || s.rows[0].faintFrom(4) {
		t.Fatal("faintFrom")
	}
	if s.rows[0].textFrom(10) != "" {
		t.Fatal("textFrom past the end")
	}
}
