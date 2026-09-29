package main

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// pi is the profile of pi, the coding agent of earendil-works/pi
// (npm @earendil-works/pi-coding-agent), measured on 0.87.1
// (docs/research/pi.md, "Capture run on pi 0.87.1", and the captures in
// testdata/pi).
var pi = profile{
	name:  "pi",
	names: []string{"pi"},
	read:  piBox,
	// Rows wrap at the last space that lets the rest fit, the space left
	// at the end of the upper row, or anywhere next to a CJK character,
	// and a paste placeholder moves whole (piWrap). A box at its cap
	// scrolls just far enough to keep the cursor in view.
	unwrap: piWrap{},
	scroll: scrollOneRow,
	// A paste of more than 10 lines, or over 1,000 characters, shows as
	// "[paste #1 +12 lines]" or "[paste #2 1001 chars]": lines are the
	// pieces between line breaks, characters UTF-16 units, both counted
	// on the text pi keeps (piPasteText). The number counts up from 1 in
	// the order the pastes came, and Backspace over a placeholder
	// renumbers every higher one, so a number names a paste only until
	// then (handleBackspace in editor.js, 0.87.1; measured in pastes).
	pastes: []pasteRule{{
		placeholder: piPastePlaceholder,
		collapses: func(paste string, _ int) bool {
			t := piPasteText(paste)
			return strings.Count(t, "\n")+1 > 10 || len(utf16.Encode([]rune(t))) > 1000
		},
		id: func(m []string) string { return m[1] },
		fits: func(m []string, paste string) bool {
			t := piPasteText(paste)
			lines := strings.Count(t, "\n") + 1
			switch {
			case m[2] != "":
				return lines > 10 && m[2] == strconv.Itoa(lines)
			case m[3] != "":
				return lines <= 10 && m[3] == strconv.Itoa(len(utf16.Encode([]rune(t))))
			}
			return true
		},
		rank: func(m []string) int {
			n, _ := strconv.Atoi(m[1])
			return n
		},
		renumbers: true,
		holds:     piPasteText,
	}},
	// Measured on 0.87.1 in tmux with modifyOtherKeys 2 (deletes, submit,
	// multiline, ctrlc-history in testdata/pi/0.87.1): Backspace, Shift+
	// Backspace, Delete, Shift+Delete and Ctrl+D (in a draft) remove one
	// character; Ctrl+W, Alt+Backspace, Alt+D, Alt+Delete, Ctrl+U and
	// Ctrl+K a word or more, and so do undo (Ctrl+-, 0x1f as legacy bytes)
	// and Alt+Y, which swaps the text Ctrl+Y put back for an older kill.
	// Delete, Shift+Delete, Ctrl+D, Alt+D, Alt+Delete, Ctrl+K and undo
	// remove text after the cursor. Ctrl+H, Ctrl+Backspace and
	// Ctrl+Delete do nothing. Enter sends the box, and so does Alt+Enter,
	// pi's follow-up key, when pi is idle; Esc then Enter in one read is
	// Alt+Enter, so it sends too, and is never a new line. Ctrl+J, Shift+
	// Enter and a backslash then Enter make a new line. Ctrl+C clears the
	// box (and quits when pressed twice); undo brings it back, which reads
	// as text put back in the empty box, a draft. Up and Down on an empty
	// box bring back an entry of pi's history, which is not a draft, also
	// after an Esc, which cancels a turn (send-paste). In a draft, Up first
	// moves the cursor to the start of the first row, and from there
	// brings history too, with the draft held aside; Down past the newest
	// entry puts it back (ctrlc-history). Up and Down change the box's text
	// in no other way.
	keys: keyset{
		one:     []key{plain(keyBackspace), {keyBackspace, modShift}, plain(keyDelete), {keyDelete, modShift}, ctrl('d')},
		many:    append([]key{ctrl('w'), alt(keyBackspace), alt('d'), alt(keyDelete), ctrl('u'), ctrl('k'), alt('y')}, piUndo...),
		ahead:   append([]key{plain(keyDelete), {keyDelete, modShift}, ctrl('d'), alt('d'), alt(keyDelete), ctrl('k')}, piUndo...),
		submit:  [][]key{{plain(keyEnter)}, {alt(keyEnter)}},
		clear:   [][]key{{ctrl('c')}},
		recall:  []key{plain(letterKey + 'A'), plain(letterKey + 'B')},
		browses: true,
		suspend: []key{ctrl('z')},
	},
	// Ctrl+Z is unsent's, and unsent writes nothing for pi as it suspends
	// it. pi's own suspend takes its modes down and stops its process
	// group, which the kernel drops in a pseudo-terminal of its own
	// session: pi hangs until a SIGCONT (suspend-direct). unsent's stop
	// works, and fg brings the box back, but pi's repaint after the resume
	// sets none of its modes again, not the kitty flags, modifyOtherKeys
	// or bracketed paste (suspend): writing its teardown here would leave
	// pi with Shift+Enter sent as a plain Enter, which sends the box. So
	// while it is stopped the shell gets keys in the form pi asked for,
	// and unsent turns bracketed paste on again as it resumes pi
	// (session.resuming).
	suspended: nil,
	// The fixtures in testdata/pi; `pi --version` prints "0.87.1".
	verified: "0.87.1",
	version:  []string{"--version"},
	// pi names no running session anywhere another process can read before
	// the session gets a reply: it holds no file open, keys nothing by pid,
	// and writes the session file only with its first assistant message; a
	// resume, the picker, /resume and /new write nothing until the next
	// entry (docs/research/pi.md, "Session identity"). So unsent reads no
	// session id for pi: its sent log is per run, and restore-in-box stays
	// off, since it needs the id before the first key. The notice after
	// exit and unsent restore reach the draft.
	session: nil,
	restore: nil,
}

// piUndo is pi's undo, Ctrl+-: 0x1f as legacy bytes, which the decoder
// reads as Ctrl+_, and minus with Ctrl in the CSI forms.
var piUndo = []key{ctrl('-'), ctrl('_')}

// piPastePlaceholder matches what pi shows for a collapsed paste; its
// groups are the number, the line count and the character count.
var piPastePlaceholder = regexp.MustCompile(`\[paste #(\d+)(?: (?:\+(\d+) lines|(\d+) chars))?\]`)

// piCSIuCtrl is a Ctrl+letter sent as CSI-u inside a paste, which pi
// turns back into its control byte before it cleans the paste.
var piCSIuCtrl = regexp.MustCompile(`\x1b\[(\d+);5u`)

// piPasteText is what pi keeps of a paste (handlePaste in editor.js,
// 0.87.1): CSI-u Ctrl+letters decoded, line breaks as LF, each tab as
// four spaces, and every control character but LF dropped. A paste that
// starts with /, ~ or . after a word character also gets a space in
// front, which only the text before the cursor tells; that one is not
// modelled.
func piPasteText(paste string) string {
	paste = piCSIuCtrl.ReplaceAllStringFunc(paste, func(m string) string {
		cp, _ := strconv.Atoi(piCSIuCtrl.FindStringSubmatch(m)[1])
		switch {
		case cp >= 'a' && cp <= 'z':
			return string(rune(cp - 96))
		case cp >= 'A' && cp <= 'Z':
			return string(rune(cp - 64))
		}
		return m
	})
	paste = strings.ReplaceAll(normalizeNewlines(paste), "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' {
			return -1
		}
		return r
	}, paste)
}

// piBox reads pi's input box. On 0.87.1 pi draws inline on the main
// screen, so the box follows the transcript and is not pinned to the
// bottom:
//
//	──────────────────────────────  (top rule, full width)
//	first row of the draft
//	following rows, no glyph and no indent
//	──────────────────────────────  (bottom rule)
//	/the/folder                     (footer, or the / and @ menus first)
//	0.0%/0 (auto)          model
//
// The cursor is pi's own, a reverse-video cell over the character at the
// insertion point, or a reverse-video space after a row's end; an empty
// box is that space alone at column 0. A box too tall for its cap shows
// " ↑ N more " centred on the top rule and " ↓ N more " on the bottom
// one, and while pi works the top rule reads "── ⠇ Retrying... ────".
// pi's pickers and dialogs (the model picker, /resume, /settings, /login,
// /trust, /tree) are drawn between the same rules in place of the box,
// with a search row that has a reverse-video cursor too; their rows mix
// colours, while a draft is drawn in one.
func piBox(s *screen) (view, bool) { return piLayout.read(s) }

// piLayout is what piBox reads by; agent_pi_test.go mutates each part and
// requires a replay or a negative screen to go red.
var piLayout = piBoxLayout{
	cap:     func(rows int) int { return max(5, rows*3/10) },
	margin:  1,
	labels:  true,
	status:  true,
	cursor:  true,
	uniform: true,
}

// piBoxLayout is the shape of pi's box.
type piBoxLayout struct {
	// cap is how many rows the box shows at most in a window rows high,
	// and margin how many columns less than the window its rows wrap at
	// (pi keeps the last column for its cursor).
	cap    func(rows int) int
	margin int
	// labels accepts the rules' " ↑ N more " and " ↓ N more ", status a
	// top rule with pi's working status in it.
	labels, status bool
	// cursor requires one reverse-video run in the box, pi's cursor, and
	// uniform that every character around it is drawn in one colour with
	// no background: pickers and dialogs mix colours.
	cursor, uniform bool
}

// piRule matches the rules of pi's box, plain or with a scroll label
// (createScrollBorder in editor.js): its groups are the arrow and the
// count of rows out of sight. piStatusRule matches the top rule while pi
// works ("── <spinner> <status> ───", renderTopBorder in
// custom-editor.js), which may hold the up label after the status.
var (
	piRuleRE       = regexp.MustCompile(`^─+(?: ([↑↓]) (\d+) more ─+)?$`)
	piStatusRuleRE = regexp.MustCompile(`^── \S.*? ─+(?: ↑ (\d+) more ─+)?$`)
)

// rule reads row r as one of the box's rules, the top one when top is
// set, and returns how many rows the label says are out of sight on its
// side.
func (b piBoxLayout) rule(r screenRow, cols int, top bool) (hidden int, ok bool) {
	t := r.text()
	if runewidth.StringWidth(t) != cols {
		return 0, false
	}
	if m := piRuleRE.FindStringSubmatch(t); m != nil {
		if m[1] == "" {
			return 0, true
		}
		if !b.labels || (m[1] == "↑") != top {
			return 0, false
		}
		n, _ := strconv.Atoi(m[2])
		return n, true
	}
	if m := piStatusRuleRE.FindStringSubmatch(t); top && b.status && m != nil {
		if m[1] != "" && !b.labels {
			return 0, false
		}
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	return 0, false
}

func (b piBoxLayout) read(s *screen) (view, bool) {
	n := len(s.rows)
	limit := b.cap(n)
	// The bottom rule is the lowest rule on the screen: only the menus and
	// the footer are drawn under the box.
	end := -1
	var below int
	for y := n - 1; y >= 1; y-- {
		if h, ok := b.rule(s.rows[y], s.cols, false); ok {
			end, below = y, h
			break
		}
	}
	if end < 0 {
		return view{}, false
	}
	top := -1
	var above int
	for y := end - 1; y >= 0 && y >= end-limit-1; y-- {
		if h, ok := b.rule(s.rows[y], s.cols, true); ok {
			top, above = y, h
			break
		}
	}
	if top < 0 || end-top-1 < 1 || end-top-1 > limit {
		return view{}, false
	}
	width := s.cols - b.margin
	v := view{cursor: -1, width: width, under: -1}
	if end+1 < n {
		v.under = end + 1
	}
	cy, cx, ok := b.cursorAt(s, top+1, end)
	if !ok {
		return view{}, false
	}
	for y := top + 1; y < end; y++ {
		row := s.rows[y].text()
		if runewidth.StringWidth(row) > width {
			// Wider than pi wraps: something narrows the box that was never
			// measured, such as editor padding.
			return view{}, false
		}
		v.rows = append(v.rows, row)
	}
	v.capped = above > 0 || below > 0 || len(v.rows) >= limit
	if cy >= 0 {
		v.cursor = cy - top - 1
		v.cursorEnd = cx >= runewidth.StringWidth(v.rows[v.cursor])
	}
	if len(v.rows) == 1 && v.rows[0] == "" && cx == 0 {
		v.rows = nil
		v.empty = true
	}
	return v, true
}

// cursorAt finds pi's cursor in rows from to to-1: the one run of
// reverse-video cells there, one character wide (two columns for a wide
// one, the whole placeholder over a paste). It also checks, when the
// layout asks, that every other character there is drawn in one colour
// with no background. cy is -1 when there is no cursor to go by.
func (b piBoxLayout) cursorAt(s *screen, from, to int) (cy, cx int, ok bool) {
	cy, cx = -1, -1
	runs, good := 0, true
	var fg cellColor
	seenFg, mixed := false, false
	for y := from; y < to; y++ {
		r := s.rows[y]
		start := -1
		for x := 0; x <= len(r.cells); x++ {
			if x < len(r.cells) && r.cells[x] == "" {
				continue // the right half of a wide character
			}
			if x < len(r.cells) && r.look[x].reverse {
				if start < 0 {
					start = x
					if runs++; runs == 1 {
						cy, cx = y, x
					}
				}
				continue
			}
			if start >= 0 {
				good = good && piCursorRun(r, start, x)
				start = -1
			}
			if x == len(r.cells) || strings.TrimSpace(r.cells[x]) == "" {
				continue
			}
			l := r.look[x]
			mixed = mixed || l.bg != 0 || (seenFg && l.fg != fg)
			fg, seenFg = l.fg, true
		}
	}
	if b.uniform && mixed {
		return 0, 0, false
	}
	if b.cursor && (runs != 1 || !good) {
		return 0, 0, false
	}
	return cy, cx, true
}

// piCursorRun reports whether the reverse-video cells from start to end-1
// of a row can be pi's cursor: one character, or a paste placeholder.
func piCursorRun(r screenRow, start, end int) bool {
	text := strings.Join(r.cells[start:end], "")
	if utf8.RuneCountInString(text) == 1 || uniseg.GraphemeClusterCount(text) == 1 {
		return true
	}
	return piPastePlaceholder.FindString(text) == text
}

// piWrap is pi's wrap (wordWrapLine in editor.js, 0.87.1): greedy, over
// grapheme clusters, a paste placeholder as one. A row may break after
// the last run of spaces that leaves the rest of its word room on the
// next row, the spaces staying at the end of the upper row, where the
// screen shows none; or between two characters where either is CJK.
// Where no such break fits, the row breaks at the edge, also in the middle
// of a run of spaces, which then starts the next row. So a word that
// ends right at the edge and has a space after it moves to the next row,
// which Claude Code's wrap keeps on the full one. Rows are returned as
// the screen shows them, with spaces at their ends trimmed.
type piWrap struct{}

func (piWrap) wrap(text string, width int) (rows []string, starts []int) {
	at := 0
	for _, line := range strings.Split(text, "\n") {
		chunks := piWrapLine(line, width)
		for _, c := range chunks {
			rows = append(rows, strings.TrimRight(line[c[0]:c[1]], " "))
			starts = append(starts, at+c[0])
		}
		at += len(line) + 1
	}
	return rows, starts
}

// piWrapLine returns the byte ranges of the rows one line wraps into.
func piWrapLine(line string, width int) [][2]int {
	if line == "" || width <= 0 || runewidth.StringWidth(line) <= width {
		return [][2]int{{0, len(line)}}
	}
	segs := piSegments(line)
	var chunks [][2]int
	cur, start := 0, 0
	opp, oppWidth := -1, 0
	for i, sg := range segs {
		g := line[sg[0]:sg[1]]
		gw := runewidth.StringWidth(g)
		marker := sg[2] == 1
		ws := !marker && piSpace(g)
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
			sub := piWrapLine(g, width)
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
		case ws && (nextMarker || !piSpace(ng)):
			opp, oppWidth = next[0], cur
		case !ws && !piSpace(ng) && ((!marker && piCJK(g)) || (!nextMarker && piCJK(ng))):
			opp, oppWidth = next[0], cur
		}
	}
	return append(chunks, [2]int{start, len(line)})
}

// piSegments splits a line into grapheme clusters, each paste placeholder
// kept as one: its byte range, and 1 in the third field for a placeholder.
func piSegments(line string) [][3]int {
	var segs [][3]int
	marks := piPastePlaceholder.FindAllStringIndex(line, -1)
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

// piSpace reports whether a grapheme holds white space, as JavaScript's
// \s finds it.
func piSpace(g string) bool {
	return strings.IndexFunc(g, func(r rune) bool { return unicode.IsSpace(r) || r == '\uFEFF' }) >= 0
}

// piCJK reports whether a grapheme holds a character after which, or
// before which, pi may break a row: Han, kana, Hangul or Bopomofo.
func piCJK(g string) bool {
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
func (piWrap) unwrap(rows []string, width int) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(rows[0])
	for i := 1; i < len(rows); i++ {
		b.WriteString(piJoin(rows[i-1], rows[i], width))
		b.WriteString(rows[i])
	}
	return b.String()
}

// hidesBreak reports whether a line break between line and next reads
// as a wrap on pi's screen (see byteExact).
func (w piWrap) hidesBreak(line, next string, width int) bool {
	a, _ := w.wrap(line, width)
	b, _ := w.wrap(next, width)
	return piJoin(a[len(a)-1], b[0], width) == " "
}

// piJoin is what stood between two rows on pi's screen: " " or "" for a
// wrap, "\n" for a line break.
func piJoin(prev, next string, width int) string {
	if width <= 0 || prev == "" || next == "" {
		return "\n"
	}
	space, none := piRewraps(prev+" "+next, prev, next, width), piRewraps(prev+next, prev, next, width)
	switch {
	case space && none:
		last, _ := utf8.DecodeLastRuneInString(prev)
		first, _ := utf8.DecodeRuneInString(next)
		if piCJK(string(last)) || piCJK(string(first)) {
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

// piRewraps reports whether pi wraps text into exactly the rows a and b.
func piRewraps(text, a, b string, width int) bool {
	c := piWrapLine(text, width)
	return len(c) == 2 && strings.TrimRight(text[c[0][0]:c[0][1]], " ") == a && strings.TrimRight(text[c[1][0]:c[1][1]], " ") == b
}
