package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// agy is the profile of agy, Google's Antigravity CLI, measured on 1.2.13
// (docs/research/agy.md, "Capture run on agy 1.2.13", and the captures in
// testdata/agy).
var agy = profile{
	name:  "agy",
	names: []string{"agy"},
	read:  agyBox,
	// Rows wrap at the last space that leaves the space itself room on the
	// upper row, so a word that would end exactly at the edge moves down
	// whole (fitWrap, measured in the wrap capture); a word longer than a
	// row breaks at the edge, a wide character is never split, and a line
	// that ends on a full row gets no row of its own, as Codex's does. A
	// box at its cap scrolls just far enough to keep the insertion point
	// in view.
	unwrap: fitWrap{agyPastePlaceholder},
	scroll: scrollOneRow,
	// A paste of more than min(15, rows/2) lines shows as
	// "[Pasted text #1 +16 lines]", and one with a line over 1,000
	// characters as "[Pasted text #2 1001 chars]", where the count is the
	// whole paste, line breaks included. Both count on the text agy keeps
	// (agyPasteText). The number counts up from 1 within a session and
	// never renumbers: a placeholder deleted leaves the others as they
	// were, and the next paste takes the next number. Ctrl+G, which puts
	// every paste in the box as plain text, starts the numbering again at
	// 1, and then the pastes behind the old numbers are in the draft
	// itself, so they fill no placeholder again (pastes, pastes2).
	pastes: []pasteRule{{
		placeholder: agyPastePlaceholder,
		collapses: func(paste string, rows int) bool {
			t := agyPasteText(paste)
			return strings.Count(t, "\n")+1 > min(15, rows/2) || agyLongestLine(t) > 1000
		},
		id: func(m []string) string { return m[1] },
		fits: func(m []string, paste string) bool {
			t := agyPasteText(paste)
			switch {
			case m[2] != "":
				return m[2] == strconv.Itoa(strings.Count(t, "\n")+1)
			case m[3] != "":
				return m[3] == strconv.Itoa(utf8.RuneCountInString(t))
			}
			return true
		},
		rank: func(m []string) int {
			n, _ := strconv.Atoi(m[1])
			return n
		},
		holds: agyPasteText,
	}},
	// Measured on 1.2.13 in tmux with modifyOtherKeys 2 in its CSI-u form
	// (deletes, multiline, submit, typed in testdata/agy/1.2.13):
	// Backspace, Ctrl+H and Delete remove one character, Delete the one
	// after the cursor; Ctrl+W, Alt+Backspace and Ctrl+U a word or more,
	// Ctrl+U to the start of the line and through the line break before
	// it, which makes it the only key that clears a draft. Undo (Ctrl+_)
	// and redo (Ctrl+Shift+Z) change the draft anywhere, so both count as
	// deletes of any amount on either side of the cursor. Ctrl+K, Ctrl+D,
	// Alt+D, Ctrl+Delete, Alt+Delete, Ctrl+Backspace, Shift+Backspace and
	// Shift+Delete do nothing at all. Enter is the only submit key:
	// Ctrl+J, Alt+Enter, Shift+Enter, Esc then Enter in one read and a
	// backslash then Enter all make a new line. No key clears the box:
	// Ctrl+C and Esc both keep the draft, and a second Ctrl+C quits. Up
	// and Down on an empty box bring back an entry of agy's own history,
	// which is not a draft; in a draft they only move the insertion point.
	keys: keyset{
		one:     []key{plain(keyBackspace), ctrl('h'), plain(keyDelete)},
		many:    append([]key{ctrl('w'), alt(keyBackspace), ctrl('u')}, agyUndo...),
		ahead:   append([]key{plain(keyDelete)}, agyUndo...),
		submit:  [][]key{{plain(keyEnter)}},
		recall:  []key{plain(letterKey + 'A'), plain(letterKey + 'B')},
		empties: true,
		// The shortcuts panel binds Ctrl+Z to "Suspend CLI", and agy did
		// not act on the CSI-u form tmux sends (suspend), so unsent takes
		// it, as it does for every other profile.
		suspend: []key{ctrl('z')},
	},
	// unsent writes nothing for agy as it suspends it. agy re-asserts
	// bracketed paste and modifyOtherKeys while it is idle, about every
	// two seconds, but its repaint after the resume sets the kitty flags
	// again nowhere (suspend-legacy), so writing its teardown here would
	// leave it with keys it did not ask for. unsent turns bracketed paste
	// on again as it resumes agy (session.resuming).
	suspended: nil,
	// agy sends no synchronized-output marks at all on 1.2.13, not one in
	// any of the 33 captures, though it still queries for them. It
	// brackets each of its paints with ESC[?25l … ESC[?25h instead, and
	// closed every one, so those marks are what says a paint is open.
	// They cannot say on their own that the screen is half drawn, since
	// agy keeps the cursor hidden for as long as a dialog is on screen, so
	// the gap decides (session.drawing): while a paint is open the screen
	// is read once the bytes have stopped coming for 50 ms, and output
	// outside a paint is read as it lands. What that buys is the dense
	// part of a paint, where the chunks arrive in the same millisecond:
	// 57 reads across the captures fall inside one, every one of them on a
	// screen agy has not drawn the box into yet
	// (TestAgyPaintMarksHoldBackHalfDrawnScreens).
	// It never holds a save for long: a paint whose next chunk is seconds
	// away stops blocking after the gap, and take caps any wait at
	// maxFrameWait whatever the profile says.
	hidesCursor: true,
	quietGap:    50 * time.Millisecond,
	// The fixtures in testdata/agy; `agy --version` prints "1.2.13".
	verified: "1.2.13",
	version:  []string{"--version"},
	session:  &sessionSource{read: agySession, pids: agySessionPids},
	restore: &restoreCaps{
		settle:   time.Second,
		empties:  3,
		chat:     agyChat,
		faithful: agyFaithful,
	},
}

// agyUndo is agy's undo and redo: Ctrl+_ (0x1f as legacy bytes, which the
// decoder reads as Ctrl+_ too) and Ctrl+Shift+Z.
var agyUndo = []key{ctrl('_'), {'-', modCtrl | modShift}, {'z', modCtrl | modShift}}

// agyPastePlaceholder matches what agy shows for a collapsed paste; its
// groups are the number, the line count and the character count.
var agyPastePlaceholder = regexp.MustCompile(`\[Pasted text #(\d+)(?: (?:\+(\d+) lines|(\d+) chars))?\]`)

// agyPasteText is what agy keeps of a paste (pastes2): line breaks as LF
// whether they came as CR or LF, each tab as four spaces, and one line
// break at the end dropped.
func agyPasteText(paste string) string {
	return strings.ReplaceAll(strings.TrimSuffix(normalizeNewlines(paste), "\n"), "\t", "    ")
}

// agyLongestLine is the longest line of text in characters, which is what
// agy's 1,000-character paste rule counts.
func agyLongestLine(text string) int {
	n := 0
	for line := range strings.SplitSeq(text, "\n") {
		n = max(n, utf8.RuneCountInString(line))
	}
	return n
}

// agyFaithful says why pasting draft back into agy's box would not put it
// back as it is (measured on 1.2.13, pastes2): agy turns a tab in a paste
// into four spaces, and drops one line break at the end of it. Everything
// else went in as sent, accents precomposed and decomposed, emoji and a
// flag included, and over the placeholder thresholds agy holds the whole
// text behind "[Pasted text #N …]", which Ctrl+G opens in the editor.
func agyFaithful(draft string) string {
	switch {
	case strings.Contains(draft, "\t"):
		return "holds a tab, which agy would turn into four spaces"
	case strings.HasSuffix(draft, "\n"):
		return "ends in a line break, which agy would drop"
	}
	return ""
}

// agyChat reports whether agy's arguments open its chat box with nothing
// sent on start: no prompt, and no subcommand. The chat starts are the
// bare command, -c/--continue and --conversation <id>; -i and -p send a
// prompt on start, and every subcommand (agent, changelog, help, install,
// mcp, mic-serve, models, plugin, remote-control, update) is not a chat
// box (agy --help, 1.2.13). Anything that could be a prompt counts as
// one, the safe side: it only costs a restore, which the notice after
// exit makes up for.
func agyChat(args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" || a == "--" {
			return false
		}
		name, _, inline := strings.Cut(a, "=")
		switch {
		case slices.Contains(agyNotChat, name):
			return false
		case inline:
		case slices.Contains(agyValue, name):
			i++
		}
	}
	return true
}

// agy 1.2.13's options (agy --help) that matter to agyChat: the ones that
// send something on start or open no chat box, and the ones whose value
// is the next argument.
var (
	agyNotChat = []string{"-h", "--help", "-p", "--print", "--prompt", "-i", "--prompt-interactive",
		"--input-format", "--output-format", "--json-schema", "--print-timeout"}
	agyValue = []string{"--add-dir", "--agent", "--conversation", "--effort", "--log-file",
		"--mode", "--model", "--project"}
)

// agyBox reads agy's input box. On 1.2.13 agy draws inline on the main
// screen, so the box follows the transcript and the rows above it stay in
// the scrollback:
//
//	────────────────────────────────  (top rule, full width)
//	> first row of the draft          ('>' in a colour of its own, then a space)
//	  following rows, indented two
//	────────────────────────────────  (bottom rule)
//	? for shortcuts        Gemini 3.1 Pro · low
//
// A box taller than its cap shows "> ↑ N more lines" as its first row and
// "  ↓ N more lines" as its last, both in the colour agy draws its hints
// in, and N counts wrapped screen rows. The empty box is the glyph alone,
// or the glyph and one of agy's mode hints, which sits where the draft
// would be. In bash mode the glyph is "!", and it is not draft text: agy
// hands the editor the command alone at Ctrl+G, draws it from column 2 as
// it draws every other draft, and wraps it at the same width, so the "!"
// is read as the glyph it is (measured on 1.2.13, bash-mode). Ctrl+U in
// bash mode leaves the glyph with an empty box, and Esc puts ">" back.
// The transcript above the box has rules of its
// own, half the window wide, and the slash menu, the file picker and
// every panel agy opens are drawn under the bottom rule, some of them with
// rows that start "> " too: the box is the one between the lowest two
// full-width rules.
func agyBox(s *screen) (view, bool) { return agyLayout.read(s) }

// agyLayout is what agyBox reads by; agent_agy_test.go mutates each part
// and requires a replay or a negative screen to go red.
var agyLayout = agyBoxLayout{
	cap:    func(rows int) int { return rows / 2 },
	glyphs: []string{">", "!"},
	indent: 2,
	margin: 3,
	labels: true,
	hints:  true,
}

// agyBoxLayout is the shape of agy's box.
type agyBoxLayout struct {
	// cap is how many text rows the box shows at most in a window rows
	// high, glyphs what its first row may start with, indent the column
	// its text starts in, and margin how many columns less than the window
	// its rows wrap at.
	cap            func(rows int) int
	glyphs         []string
	indent, margin int
	// labels accepts the "↑ N more lines" and "↓ N more lines" rows, which
	// are not text, and hints an empty box drawn as one of agy's mode
	// hints. Both are told from a draft by their colour: agy draws a draft
	// in the terminal's own foreground and everything else in a colour.
	labels, hints bool
}

// agyLabelRE matches the rows agy puts at the ends of a box too tall to
// show whole; its groups are the arrow and the count of rows out of sight.
var agyLabelRE = regexp.MustCompile(`^([↑↓]) (\d+) more lines?$`)

// rule reports whether a row is one of the box's rules: U+2500 across the
// whole window. The rules of the transcript above the box are half the
// window wide.
func (b agyBoxLayout) rule(r screenRow, cols int) bool {
	t := r.text()
	if runewidth.StringWidth(t) != cols {
		return false
	}
	for _, c := range t {
		if c != '─' {
			return false
		}
	}
	return true
}

// label reads row r as one of the box's scroll labels on the side arrow
// says, and returns how many rows it says are out of sight.
func (b agyBoxLayout) label(r screenRow, arrow string) (hidden int, ok bool) {
	m := agyLabelRE.FindStringSubmatch(r.textFrom(b.indent))
	if !b.labels || m == nil || m[1] != arrow || !agyColoured(r, b.indent) {
		return 0, false
	}
	n, _ := strconv.Atoi(m[2])
	return n, true
}

func (b agyBoxLayout) read(s *screen) (view, bool) {
	n := len(s.rows)
	limit := b.cap(n)
	// The bottom rule is the lowest full-width rule on the screen: the
	// menus, the pickers, the panels and the footer are drawn under the
	// box, and the transcript's own rules are above it and narrower.
	end := -1
	for y := n - 2; y >= 1; y-- {
		if b.rule(s.rows[y], s.cols) {
			end = y
			break
		}
	}
	if end < 0 {
		return view{}, false
	}
	// The top rule is the next one above it, within the cap and the two
	// rows the scroll labels take.
	top := -1
	for y := end - 1; y >= 0 && y >= end-limit-3; y-- {
		if b.rule(s.rows[y], s.cols) {
			top = y
			break
		}
	}
	if top < 0 || end-top < 2 {
		return view{}, false
	}
	rows := s.rows[top+1 : end]
	if !b.glyphRow(rows[0]) {
		return view{}, false
	}
	for _, r := range rows[1:] {
		if !blankCells(r, 0, b.indent) {
			return view{}, false
		}
	}
	first := top + 1
	above, below := 0, 0
	if h, ok := b.label(rows[0], "↑"); ok {
		above, rows, first = h, rows[1:], first+1
	}
	if len(rows) > 0 {
		if h, ok := b.label(rows[len(rows)-1], "↓"); ok {
			below, rows = h, rows[:len(rows)-1]
		}
	}
	if len(rows) == 0 || len(rows) > limit {
		return view{}, false
	}
	v := view{cursor: -1, width: s.cols - b.margin, under: end + 1}
	v.capped = above > 0 || below > 0 || len(rows) >= limit
	if len(rows) == 1 && (rows[0].textFrom(b.indent) == "" || b.hints && agyColoured(rows[0], b.indent)) {
		// Nothing typed, or one of agy's mode hints where the draft would
		// be (dialogs).
		v.empty = true
		return v, true
	}
	for _, r := range rows {
		t := r.textFrom(b.indent)
		if runewidth.StringWidth(t) > v.width {
			// Wider than agy wraps: something narrows the box that was
			// never measured.
			return view{}, false
		}
		v.rows = append(v.rows, t)
	}
	if s.curY >= first && s.curY < first+len(v.rows) {
		v.cursor = s.curY - first
		v.cursorEnd = s.curX >= b.indent+runewidth.StringWidth(v.rows[v.cursor])
	}
	return v, true
}

// glyphRow reports whether a row starts with one of the box's glyphs,
// drawn in a colour of its own, and a space after it.
func (b agyBoxLayout) glyphRow(r screenRow) bool {
	if len(r.cells) < 2 || !slices.Contains(b.glyphs, r.cells[0]) || r.cells[1] != " " {
		return false
	}
	l := r.look[0]
	return l.fg != 0 && !l.reverse
}

// agyColoured reports whether every visible character from column x on is
// drawn in a colour of agy's own rather than the terminal's foreground,
// and there is one. agy draws a draft, its paste placeholders included, in
// the terminal's foreground, and its hints and scroll labels in a colour
// (measured on 1.2.13 in a 256-colour terminal, and on 1.2.11 in a
// truecolor one, where the colours differ but the rule holds).
func agyColoured(r screenRow, x int) bool {
	seen := false
	for i := x; i < len(r.cells); i++ {
		if strings.TrimSpace(r.cells[i]) == "" {
			continue
		}
		if r.look[i].fg == 0 {
			return false
		}
		seen = true
	}
	return seen
}

// agyHome is agy's own folder, ~/.gemini/antigravity-cli. agy 1.2.13 has
// no variable that moves it (its strings hold none), so it follows HOME.
func agyHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gemini", "antigravity-cli")
}

// agyConversations is the folder where agy keeps one database per
// conversation, conversations/<id>.db. The process that is in a
// conversation holds that file open, with its -wal and -shm, from the
// first submit of a new conversation (0.3 s after Enter) or from before
// the box is drawn of a resumed one, until it exits (measured on 1.2.13,
// docs/research/agy.md, "Session identity").
func agyConversations() string {
	home := agyHome()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "conversations")
}

// agySession reads the conversation process pid is in: the one whose
// database it holds open. A process that holds more than one names none of
// them as current, so it is in no known session. cache/last_conversations.json
// holds the same id keyed by the working folder, but every agy in that
// folder shares it, so the open file is the one to read.
func agySession(pid int, _ time.Time) (string, bool) {
	ids := agyHeld.of(pid, agyConversations(), agyRecheck, agyConversationID)
	switch len(ids) {
	case 0:
		return "", false
	case 1:
		return ids[0], true
	}
	return sessionUnsure, true
}

// agySessionPids lists the processes that hold a conversation's database
// open. The folder is asked about as a whole: it holds every conversation
// this home has ever had, and there is no cap on how many that is.
func agySessionPids() []int {
	dir, err := filepath.EvalSymlinks(agyConversations())
	if err != nil {
		return nil
	}
	return holders(dir, agyConversationID)
}

// agyConversationID is the conversation id a file name in the
// conversations folder gives, or "". The -wal and -shm files beside a
// database name the same conversation, and are left out so a process that
// holds one conversation is not read as holding three.
func agyConversationID(name string) string {
	id, ok := strings.CutSuffix(name, ".db")
	if !ok || !sessionIDRE.MatchString(id) {
		return ""
	}
	return id
}

// agyHeld keeps the last answer to which conversation databases a process
// holds open.
var agyHeld heldFiles

const agyRecheck = 5 * time.Second
