package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/mattn/go-runewidth"
)

// claude is Claude Code's profile.
var claude = profile{
	name:  "claude",
	names: []string{"claude"},
	read:  claudeBox,
	// Rows wrap at spaces, and the box scrolls a row at a time holding the
	// cursor mid-box (CLAUDE.md, measured on 2.1.282).
	unwrap: wordWrap{},
	scroll: scrollMidBox,
	// A paste shows as "[Pasted text #2 +39 lines]", or "[Pasted text #2]"
	// when it has no line break; the count is of line breaks. Claude Code
	// first turns each tab into 4 spaces, then collapses a paste over 800
	// UTF-16 units, or with more than 2 line breaks, or than the window
	// height minus 10 when that is less. Anything shorter goes in as typed
	// text, and a pasted image path becomes "[Image #N]", so neither fills
	// a placeholder. "[Image #N]" and "[Audio #N]" hold no text and stay
	// as they are. N counts up across pastes, images and cut middles, so
	// it names the paste; the line breaks pair it with one.
	pastes: []pasteRule{{
		placeholder: regexp.MustCompile(`\[Pasted text #(\d+)(?: \+(\d+) lines?)?\]`),
		collapses: func(paste string, rows int) bool {
			paste = strings.ReplaceAll(paste, "\t", "    ")
			return len(utf16.Encode([]rune(paste))) > 800 || strings.Count(paste, "\n") > max(0, min(rows-10, 2))
		},
		id: func(m []string) string { return m[1] },
		fits: func(m []string, paste string) bool {
			want, _ := strconv.Atoi(m[2]) // no count: no line break
			return strings.Count(strings.TrimRight(paste, "\n"), "\n") == want || strings.Count(paste, "\n") == want
		},
	}},
	// A box over 10,000 characters keeps its first and last 500 and shows
	// the middle as "[...Truncated text #N +M lines...]".
	truncated: regexp.MustCompile(`\[\.\.\.Truncated text #(\d+) \+(\d+) lines\.\.\.\]`),
	// Backspace, Ctrl+H, Delete and Ctrl+D remove one character; Ctrl+W,
	// Ctrl+U, Ctrl+K, Alt+Backspace, Ctrl+Backspace, Alt+D and undo (Ctrl+_,
	// Ctrl+-) any amount. Delete, Ctrl+D, Ctrl+K, Alt+D and undo can remove
	// text after the cursor. Enter sends the box, and Ctrl+Enter sends it
	// now (Ctrl+X Enter queues it, which also ends in Enter); Esc+Enter and
	// Shift+Enter make a new line. Ctrl+C and Esc Esc clear it. Ctrl+Z is
	// unsent's: Claude Code's own suspend hangs in a pseudo-terminal
	// (docs/research/claude.md section 12). Ctrl+X Ctrl+S also sends now,
	// but Ctrl+S alone stashes the box, so a chord misread would count a
	// stash as a send: it is left out and reads as a clear. The defaults of
	// Claude Code's keybindings (docs/research/claude.md section 4).
	keys: keyset{
		one:     []key{plain(keyBackspace), ctrl('h'), plain(keyDelete), ctrl('d')},
		many:    append([]key{ctrl('w'), ctrl('u'), ctrl('k'), alt(keyBackspace), ctrl(keyBackspace), alt('d')}, claudeUndo...),
		ahead:   append([]key{plain(keyDelete), ctrl('d'), ctrl('k'), alt('d')}, claudeUndo...),
		submit:  [][]key{{plain(keyEnter)}, {ctrl(keyEnter)}},
		clear:   [][]key{{ctrl('c')}, {plain(keyEsc), plain(keyEsc)}},
		suspend: []key{ctrl('z')},
	},
	// Claude Code's own suspend writes this, measured on 2.1.283 in tmux
	// and Ghostty (testdata/keys), less bracketed paste and colour scheme
	// reports, which it does not turn on again with its repaint. The repaint
	// after unsent's resume sets the rest again: the alternate screen, kitty
	// flags 5, modifyOtherKeys 2, mouse and focus reports (measured on
	// 2.1.284 in tmux). Without it the shell got Ctrl+C as ESC[27;5;99~.
	suspended: []byte("\x1b[?1006l\x1b[?1003l\x1b[?1002l\x1b[?1000l\x1b[?1004l\x1b[<u\x1b[?1049l\x1b[>4m\x1b[<u\x1b[?25h"),
	// The fixtures in testdata/claude; `claude --version` prints
	// "2.1.282 (Claude Code)".
	verified: "2.1.282",
	version:  []string{"--version"},
	session:  &sessionSource{read: claudeSession, pids: claudeSessionPids},
	// Measured on 2.1.282 with a copy of a configured install's settings,
	// plugins and hooks (docs/research/claude.md section 11): a paste on the
	// first frame that shows the box, new chat or resumed, went in whole.
	// The settle delay is margin on that, and the three empty reads come on
	// top. Claude Code turns each tab in a paste into 4 spaces.
	restore: &restoreCaps{
		settle:  time.Second,
		empties: 3,
		chat:    claudeChat,
		faithful: func(draft string) string {
			if strings.Contains(draft, "\t") {
				return "has tabs, which Claude Code's box turns into spaces"
			}
			return ""
		},
	},
}

// claudeUndo is Claude Code's undo, Ctrl+_ and Ctrl+-: 0x1f as legacy
// bytes, and minus with Ctrl, or Ctrl and Shift, in the CSI forms.
var claudeUndo = []key{ctrl('_'), ctrl('-'), {'-', modCtrl | modShift}, {'_', modCtrl | modShift}}

// claudeChat reports whether Claude Code's arguments open the chat box with
// nothing sent on start: no prompt argument (claude "fix x" sends it), no
// subcommand (claude mcp), no --print, and no mode that leaves the terminal
// (--bg) or opens a remote session. The resume forms (--resume <id>, -c, the
// --resume picker) are chat starts. Anything that could be a prompt counts
// as one: an unknown option that takes a value makes its value read as a
// prompt, and that only costs a restore, which the notice after exit makes
// up for.
func claudeChat(args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" || !strings.HasPrefix(a, "-") || a == "-" {
			return false
		}
		name, _, inline := strings.Cut(a, "=")
		switch {
		case slices.Contains(claudeNotChat, name):
			return false
		case inline:
		case slices.Contains(claudeValue, name):
			i++
		case slices.Contains(claudeOptional, name):
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
		}
	}
	return true
}

// Claude Code 2.1.282's options (claude --help) that matter to claudeChat.
// An option that takes a list (--add-dir a b) is listed as taking one value:
// a second one reads as a prompt, the safe side.
var (
	claudeNotChat = []string{"-p", "--print", "-h", "--help", "-v", "--version", "--bg", "--background",
		"--cloud", "--environment", "--teleport"}
	claudeValue = []string{"--add-dir", "--agent", "--agents", "--allowedTools", "--allowed-tools",
		"--append-system-prompt", "--autocompact", "--betas", "--debug-file", "--disallowedTools",
		"--disallowed-tools", "--effort", "--fallback-model", "--file", "--input-format", "--json-schema",
		"--max-budget-usd", "--mcp-config", "--model", "-n", "--name", "--output-format",
		"--permission-mode", "--permission-prompts", "--plugin-dir", "--plugin-url",
		"--remote-control-session-name-prefix", "--session-id", "--setting-sources", "--settings",
		"--system-prompt", "--system-prompt-snapshot", "--tools"}
	claudeOptional = []string{"-r", "--resume", "-d", "--debug", "--from-pr", "--prompt-suggestions",
		"--remote-control", "-w", "--worktree"}
)

// claudeSessions is the folder where Claude Code keeps one file per
// running process, sessions/<pid>.json, under its config folder:
// $CLAUDE_CONFIG_DIR, or ~/.claude when that is unset.
func claudeSessions() string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, "sessions")
}

// claudeSession reads the session process pid is in from its
// sessions/<pid>.json. Claude Code writes that file at start, before any
// key, and rewrites it when the process changes session: a choice in the
// resume picker, /resume, /clear. While the --resume picker is open it
// names a fresh session that matches no conversation; the /resume picker
// keeps the current one until the choice. Only the pid's own .json file is
// opened, never the <pid>.<hash>.key peer token beside it. A file left by
// an earlier process with the same pid (after kill -9) names a start
// before since, and is not this process's.
func claudeSession(pid int, since time.Time) (string, bool) {
	dir := claudeSessions()
	if dir == "" {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(dir, strconv.Itoa(pid)+".json"))
	if err != nil {
		return "", false
	}
	var f struct {
		PID       int    `json:"pid"`
		SessionID string `json:"sessionId"`
		StartedAt int64  `json:"startedAt"` // Unix milliseconds
	}
	if json.Unmarshal(data, &f) != nil {
		return "", true
	}
	if (f.PID != 0 && f.PID != pid) || (f.StartedAt != 0 && time.UnixMilli(f.StartedAt).Before(since.Add(-time.Second))) {
		return "", false
	}
	return f.SessionID, true
}

// claudeSessionPids lists the pids that have a sessions/<pid>.json. Only
// names are read here.
func claudeSessionPids() []int {
	entries, _ := os.ReadDir(claudeSessions())
	var pids []int
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if pid, err := strconv.Atoi(name); ok && err == nil && pid > 0 && strconv.Itoa(pid) == name {
			pids = append(pids, pid)
		}
	}
	return pids
}

// claudeBox reads Claude Code's input box. It is drawn as:
//
//	────────────────────
//	❯ first row of the draft
//	  following rows, indented two columns
//	────────────────────
//
// The marker is followed by a no-break space (U+00A0). An empty box shows a
// dim hint after it, or a placeholder (claudePlaceholder). In shell mode the
// marker is "!". A named session has its name in the top rule
// (claudeTitledRule).
// The box grows up to half the window height minus five rows, then scrolls
// inside itself. Rows wrap at the window width minus four columns.
func claudeBox(s *screen) (view, bool) {
	for y := len(s.rows) - 2; y >= 1; y-- {
		first := s.rows[y].text()
		shell := hasMarker(first, "!")
		if !shell && !hasMarker(first, "❯") {
			continue
		}
		if top := s.rows[y-1].text(); !isRule(top, s.cols) && !claudeTitledRule(top, s.cols) {
			continue
		}
		end := -1
		for z := y + 1; z < len(s.rows); z++ {
			if isRule(s.rows[z].text(), s.cols) {
				end = z
				break
			}
		}
		if end < 0 {
			continue
		}
		v := view{cursor: -1, width: s.cols - 4, under: -1}
		if end+1 < len(s.rows) {
			v.under = end + 1
		}
		v.capped = end-y >= s.rows2cap()
		if (s.rows[y].textFrom(2) == "" || s.rows[y].faintFrom(2) || claudePlaceholder(s.rows[y])) && end == y+1 {
			v.empty = true
			return v, true
		}
		for z := y; z < end; z++ {
			v.rows = append(v.rows, s.rows[z].textFrom(2))
		}
		if s.curY >= y && s.curY < end {
			v.cursor = s.curY - y
			v.cursorEnd = s.curX >= 2+runewidth.StringWidth(v.rows[v.cursor])
		}
		if shell {
			v.rows[0] = "!" + v.rows[0]
		}
		return v, true
	}
	return view{}, false
}

// claudeTitledRule reports whether a row is the top rule of a named
// session's box: a rule with the name near its right end, as in
// "──────── calls ─". Claude Code 2.1.284 draws it from the /rename that
// names the session on, and from the first frame of a resumed named session
// (docs/research/claude.md section 13). The rule keeps the full width: a
// name as wide as the window minus three leaves no rule before it
// (" name ─"), and a wider one is cut with an ellipsis (" nam… ─"), so a
// row with no rule before the name counts only at the full width. The
// label of history browsing ("─── History 1/1 ───…") sits at the left end
// and is still no rule: the box shows an old message then, not the draft.
func claudeTitledRule(t string, cols int) bool {
	rest, ok := strings.CutSuffix(t, " ─")
	if !ok {
		return false
	}
	name, ok := strings.CutPrefix(strings.TrimLeft(rest, "─"), " ")
	w := runewidth.StringWidth(t)
	return ok && strings.TrimSpace(name) == name && name != "" && !strings.Contains(name, "─") &&
		(strings.HasPrefix(rest, "─") && w >= cols/2 || w == cols)
}

// claudePlaceholder reports whether the box's first row shows Claude Code's
// placeholder rather than text: the agents view (← on an empty box) draws
// "❯ describe a task for a new session" with the marker and the text in
// one grey, and its cursor on the first letter (docs/research/claude.md
// section 13). Text the user types is drawn in the default colour, and so
// is the marker of the main box, so neither matches.
func claudePlaceholder(r screenRow) bool {
	return len(r.look) > 3 && r.look[0].fg != 0 && r.fgFrom(3, r.look[0].fg)
}

// rows2cap is the height at which Claude Code's box stops growing. It is one
// row lower than the measured cap (rows/2 - 5): treating a box that is not
// scrolled as scrolled only costs a little precision, while the opposite
// would drop the rows scrolled out of sight.
func (s *screen) rows2cap() int {
	c := len(s.rows)/2 - 6
	if c < 1 {
		c = 1
	}
	return c
}
