package main

import (
	"cmp"
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

// codex is the OpenAI Codex CLI's profile, measured on 0.158.0
// (docs/research/codex.md section 10, the captures in testdata/codex).
var codex = profile{
	name:  "codex",
	names: []string{"codex"},
	read:  codexBox,
	// Rows wrap at spaces, the space hanging off the row, and a line that
	// ends on a full row gets an empty row after it for the insertion
	// point. A box at its cap scrolls just far enough to keep the cursor
	// in view.
	unwrap: wordWrap{endRow: true},
	scroll: scrollOneRow,
	// A paste over 1,000 characters shows as "[Pasted Content 1001 chars]",
	// whatever its line breaks, which count as characters (CR and CRLF
	// become LF first). A second paste of the same size still in the box
	// shows as "[Pasted Content 1001 chars] #2", then #3: one above the
	// highest of that size still there, and the plain label once none is
	// left (next_large_paste_placeholder in chat_composer.rs, 0.158.0). So
	// the label names the paste while the box shows it, the count pairs it
	// with one, the labels of a size show their pastes in the order they
	// came, and a label deleted and given again stands for the newer paste,
	// never again for the older one.
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
	// Measured on 0.158.0 in tmux, in both key forms Codex asks for
	// (testdata/codex/0.158.0/deletes and deletes-xterm): Backspace,
	// Shift+Backspace, Ctrl+H, Delete, Shift+Delete and Ctrl+D remove one
	// character; Ctrl+W, Alt+Backspace, Ctrl+Backspace, Alt+D, Ctrl+Delete,
	// Alt+Delete, Ctrl+U and Ctrl+K a word or more, as do Ctrl+Shift+
	// Backspace, Ctrl+Alt+H and Ctrl+Shift+Delete, Codex's other defaults
	// (keymap.rs). Delete, Shift+Delete, Ctrl+D, Alt+D, Ctrl+Delete,
	// Alt+Delete, Ctrl+Shift+Delete and Ctrl+K remove text after the cursor.
	// Codex has no undo; Ctrl+Y puts back the last kill. Enter sends the
	// box and Tab queues it, which sends it when Codex is idle; Ctrl+J,
	// Alt+Enter and Shift+Enter make a new line. Ctrl+C clears it (and
	// quits on an empty box). Up and Down on an empty box bring back an
	// entry of Codex's history, which is not a draft.
	keys: keyset{
		one: []key{plain(keyBackspace), {keyBackspace, modShift}, ctrl('h'),
			plain(keyDelete), {keyDelete, modShift}, ctrl('d')},
		many: []key{ctrl('w'), alt(keyBackspace), ctrl(keyBackspace), {keyBackspace, modCtrl | modShift},
			{'h', modCtrl | modAlt}, alt('d'), ctrl(keyDelete), alt(keyDelete), {keyDelete, modCtrl | modShift},
			ctrl('u'), ctrl('k')},
		ahead: []key{plain(keyDelete), {keyDelete, modShift}, ctrl('d'), alt('d'), ctrl(keyDelete),
			alt(keyDelete), {keyDelete, modCtrl | modShift}, ctrl('k')},
		submit:  [][]key{{plain(keyEnter)}, {plain(keyTab)}},
		clear:   [][]key{{ctrl('c')}},
		recall:  []key{plain(letterKey + 'A'), plain(letterKey + 'B')},
		suspend: []key{ctrl('z')},
	},
	// Ctrl+Z is unsent's, and unsent writes nothing for Codex as it
	// suspends it. Codex's own suspend does not stop it in a pseudo-terminal
	// of its own session: it turns its modes off, the stop is dropped, and
	// it turns them on again (suspend-direct). unsent's stop works, and fg
	// brings the box back with the draft, but Codex's repaint after the
	// resume sets none of its modes again: not the alternate screen, the
	// kitty flags or bracketed paste (suspend). Writing its leave sequence
	// here would leave the box drawn on the main screen. So while it is
	// stopped the shell sits on Codex's screen, with its key modes on. The
	// shell turns bracketed paste off as it runs fg, and unsent turns it
	// on again as it resumes Codex (session.resuming), or a paste's line
	// breaks would reach Codex as Enter.
	suspended: nil,
	// The fixtures in testdata/codex; `codex --version` prints
	// "codex-cli 0.158.0".
	verified: "0.158.0",
	version:  []string{"--version"},
	session:  &sessionSource{read: codexSession, pids: codexSessionPids},
	restore: &restoreCaps{
		settle:   time.Second,
		empties:  3,
		chat:     codexChat,
		faithful: codexFaithful,
	},
}

// codexBox reads Codex's input box. On 0.158.0 Codex draws on the
// alternate screen, and its box sits at the bottom of it:
//
//	(a blank row, the box's top padding)
//	› first row of the draft
//	  following rows, indented two columns
//	(a blank row, the bottom padding)
//	  gpt-… default · /the/folder
//	  ? for shortcuts                         ⚠ 1 warning · f2 to view
//
// The › is bold, and neither dim (a message sent earlier, in history)
// nor reversed (the chosen row of a menu or dialog, drawn above the box).
// A dim › is a box that takes no input, such as the "› Shutting down..."
// Codex draws as it quits. In shell mode the glyph is "!",
// and at the top effort tiers "»". An empty box shows a dim hint after
// the glyph. Rows wrap at the window width minus 3 columns. The second
// footer row turns into "reverse-i-search:" while Ctrl+R searches
// history, which puts a history entry in the box: that is not a draft.
func codexBox(s *screen) (view, bool) { return codexLayout.read(s) }

// codexLayout is what codexBox reads by; replay_test.go mutates each part
// and requires a replay or a negative screen to go red.
var codexLayout = boxLayout{
	glyphs: []string{"›", "»", "!"},
	glyph: func(l cellLook, dim bool) bool {
		return l.bold && !l.reverse && !dim
	},
	indent: 2,
	margin: 3,
	footer: 2,
	// The box stops growing at the window height minus 4 text rows when
	// Codex is idle (36 at 40). A running turn puts its status and queued
	// messages in the bottom pane too and lowers that (source, never
	// measured), so a box within 4 more rows of it counts as at its cap:
	// taking a box that is not scrolled for one that is costs a little
	// precision, the other way round would drop the rows out of sight.
	capAt: 9,
	footerRow: func(r screenRow) bool {
		// The model and the folder, from the text column on.
		return len(r.cells) > 2 && blankCells(r, 0, 2) && strings.TrimSpace(r.cells[2]) != ""
	},
	search: "reverse-i-search:",
}

// boxLayout is the shape of a box drawn at the bottom of the screen, with
// no border: a blank row above and below it and footer rows under that.
type boxLayout struct {
	// glyphs start the box's first visible row, when glyph accepts how
	// the first cell is drawn.
	glyphs []string
	glyph  func(l cellLook, dim bool) bool
	// indent is the column the text starts in, margin how many columns
	// less than the window the rows wrap at, footer the rows under the
	// bottom padding, and capAt how many rows short of the window height
	// the box counts as at its cap.
	indent, margin, footer, capAt int
	// footerRow reports whether a row is the first footer row, which
	// tells the box's bottom padding from a blank line of the draft.
	footerRow func(screenRow) bool
	// search starts the last footer row while the agent searches its
	// history; the box then shows a history entry.
	search string
}

func (b boxLayout) read(s *screen) (view, bool) {
	n := len(s.rows)
	pad := n - 1 - b.footer
	if pad < 2 || s.rows[pad].text() != "" || !b.footerRow(s.rows[pad+1]) {
		return view{}, false
	}
	if b.search != "" && strings.HasPrefix(s.rows[n-1].textFrom(b.indent), b.search) {
		return view{}, false
	}
	// The first row above the padding with anything left of the text is
	// the glyph's: every other row of the box is indented.
	y := pad - 1
	for y >= 1 && blankCells(s.rows[y], 0, b.indent) {
		y--
	}
	r := s.rows[y]
	if y < 1 || !b.glyphRow(r) {
		return view{}, false
	}
	v := view{cursor: -1, width: s.cols - b.margin, under: pad + 1}
	v.capped = pad-y >= n-b.capAt
	if pad == y+1 && (r.textFrom(b.indent) == "" || r.faintFrom(b.indent)) {
		v.empty = true
		return v, true
	}
	for z := y; z < pad; z++ {
		row := s.rows[z].textFrom(b.indent)
		if runewidth.StringWidth(row) > v.width {
			// Wider than the agent wraps: something narrows the box that
			// was never measured (Codex's pet image, on the right).
			return view{}, false
		}
		v.rows = append(v.rows, row)
	}
	if s.curY >= y && s.curY < pad {
		v.cursor = s.curY - y
		v.cursorEnd = s.curX >= b.indent+runewidth.StringWidth(v.rows[v.cursor])
	}
	if r.cells[0] == "!" {
		v.rows[0] = "!" + v.rows[0]
	}
	return v, true
}

// glyphRow reports whether a row starts with one of the glyphs, drawn as
// the box draws it, and a space after it.
func (b boxLayout) glyphRow(r screenRow) bool {
	if len(r.cells) < 2 || !slices.Contains(b.glyphs, r.cells[0]) || r.cells[1] != " " {
		return false
	}
	return b.glyph(r.look[0], r.faint[0])
}

// blankCells reports whether columns from to to-1 of a row hold nothing.
func blankCells(r screenRow, from, to int) bool {
	for x := from; x < to && x < len(r.cells); x++ {
		if strings.TrimSpace(r.cells[x]) != "" {
			return false
		}
	}
	return true
}

// codexFaithful says why pasting draft back into Codex's box would not
// put it back as it is: never, as measured on 0.158.0 (paste-exact and
// early-paste in testdata/codex): a bracketed paste goes in as sent, tabs,
// spaces at line ends, blank lines, decomposed accents, a flag and a line
// break at the end included, and over 1,000 characters Codex holds the
// whole text behind its placeholder. A line break arrives as LF whether
// it was sent as LF or CR; a draft holds LF only.
func codexFaithful(string) string { return "" }

// codexChat reports whether Codex's arguments open its chat box with
// nothing sent on start: no prompt argument (codex "fix x" sends it), no
// subcommand but resume and fork, no image to attach to a first prompt.
// codex resume <id>, codex resume --last and the picker (codex resume)
// are chat starts; codex fork starts a new thread, which has no draft of
// its own. Anything that could be a prompt counts as one, the safe side:
// it only costs a restore, which the notice after exit makes up for.
func codexChat(args []string) bool {
	sub := ""
	positional := 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" || a == "-" {
			return false
		}
		if !strings.HasPrefix(a, "-") {
			switch {
			case sub == "" && (a == "resume" || a == "fork"):
				sub = a
			case sub != "" && positional == 0 && !slices.Contains(args[:i], "--last"):
				// The id of the thread to open. After --last the first
				// argument may be read as a prompt: not a chat start.
				positional++
			default:
				return false
			}
			continue
		}
		name, _, inline := strings.Cut(a, "=")
		switch {
		case slices.Contains(codexNotChat, name):
			return false
		case inline:
		case slices.Contains(codexValue, name):
			i++
		}
	}
	return true
}

// Codex 0.158.0's options (codex --help, codex resume --help) that matter
// to codexChat. -i attaches images to a first prompt, so it counts as one.
var (
	codexNotChat = []string{"-h", "--help", "-V", "--version", "-i", "--image"}
	codexValue   = []string{"-c", "--config", "--enable", "--disable", "--remote", "--remote-auth-token-env",
		"-m", "--model", "--local-provider", "-p", "--profile", "-s", "--sandbox", "-C", "--cd",
		"--add-dir", "-a", "--ask-for-approval"}
)

// codexHome is Codex's own folder: $CODEX_HOME, or ~/.codex.
func codexHome() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// codexLocks is the folder where Codex keeps a lock file per thread it
// writes, thread-writer-locks/<thread id>.lock. The process that writes a
// thread holds its file open, with an exclusive flock, from before the
// box is drawn until it exits, which removes the file (measured on
// 0.158.0, docs/research/codex.md section 10).
func codexLocks() string {
	home := codexHome()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "thread-writer-locks")
}

// codexSession reads the thread process pid is in: the one whose lock it
// holds open. That is how Codex runs when a -c, --enable, --disable,
// --search or --no-daemon option keeps it off its shared background
// server; under that server the process holds no lock, and no file names
// its thread. A process that holds more than one lock (after /new, or
// /resume of another thread, which keep the first thread's lock) names
// none of them as current, so it is in no known session.
func codexSession(pid int, _ time.Time) (string, bool) {
	ids := codexHeld.of(pid, codexLocks(), codexRecheck, codexLockID)
	switch len(ids) {
	case 0:
		return "", false
	case 1:
		return ids[0], true
	}
	return sessionUnsure, true
}

// codexSessionPids lists the processes that hold a thread's lock open.
func codexSessionPids() []int {
	dir, err := filepath.EvalSymlinks(codexLocks())
	if err != nil {
		return nil
	}
	return holders(dir, codexLockID)
}

// codexLockID is the thread id a lock file's name gives, or "".
func codexLockID(name string) string {
	id, ok := strings.CutSuffix(name, ".lock")
	if !ok || !sessionIDRE.MatchString(id) {
		return ""
	}
	return id
}

// codexHeld keeps the last answer to which locks a process holds. Asking
// runs lsof on macOS, so it is asked again only when the lock folder
// changed (a lock taken or let go makes or removes a file) or
// codexRecheck has passed.
var codexHeld heldFiles

const codexRecheck = 5 * time.Second
