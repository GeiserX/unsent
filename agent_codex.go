package main

import (
	"cmp"
	"regexp"
	"strconv"
	"unicode/utf8"
)

// codex is what unsent knows of the OpenAI Codex CLI so far: how it wraps
// and scrolls its box and how it shows a long paste, measured on 0.151.0
// and 0.158.0 (docs/research/codex.md sections 3, 4 and 10). It has no
// reader yet, so it is not in profiles, and unsent runs Codex without
// saving; the fuzz and the replays of testdata/codex use it.
var codex = profile{
	name:  "codex",
	names: []string{"codex"},
	// Rows wrap at spaces, the space hanging off the row, and a line that
	// ends on a full row gets an empty row after it for the insertion
	// point. A box at its cap scrolls just far enough to keep the cursor
	// in view.
	unwrap: wordWrap{endRow: true},
	scroll: scrollOneRow,
	// A paste over 1,000 characters shows as "[Pasted Content 1001 chars]",
	// whatever its line breaks, which count as characters (CR and CRLF
	// become LF first). A second paste of the same size still in the box
	// shows as "[Pasted Content 1001 chars] #2", then #3; once none of that
	// size is left, the plain label comes back. So the label names the
	// paste while the box shows it, the count pairs it with one, the
	// pastes of a size go to the plain label, #2, #3 in the order they
	// came, and a paste whose label left the box never comes back.
	pastes: []pasteRule{{
		placeholder: regexp.MustCompile(`\[Pasted Content (\d+) chars\](?: #(\d+))?`),
		collapses:   func(paste string, _ int) bool { return utf8.RuneCountInString(paste) > 1000 },
		id:          func(m []string) string { return m[1] + " chars #" + cmp.Or(m[2], "1") },
		fits:        func(m []string, paste string) bool { return strconv.Itoa(utf8.RuneCountInString(paste)) == m[1] },
		rank: func(m []string) int {
			n, _ := strconv.Atoi(m[2])
			return max(n, 1)
		},
		reuses: true,
	}},
}
