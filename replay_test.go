package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/vt"
	"github.com/mattn/go-runewidth"
)

// Replays of real Claude Code captures in testdata/claude/<version>/ (see
// the README there). Each one goes through the same shadow screen, reader
// and stitcher as a live session, and the draft is checked against what
// Claude Code itself held.

// replayBytes feeds a byte capture, taken at cols x rows, through a session
// and saves after every synchronized-output frame, as a save that ran at
// that moment would. It returns each draft saved, in order.
func replayBytes(t *testing.T, cols, rows int, data []byte) []string {
	t.Helper()
	s := &session{
		screen: vt.NewEmulator(cols, rows),
		rec:    newRecord([]string{"claude"}, "/w"),
		store:  testStore(t),
		pastes: &pasteTracker{},
		prof:   &claude,
	}
	// Claude Code queries the terminal; the emulator's answers must drain.
	go io.Copy(io.Discard, s.screen)
	drafts := []string{""}
	for len(data) > 0 {
		n := len(data)
		if i := bytes.Index(data, frameEnd); i >= 0 {
			n = i + len(frameEnd)
		}
		s.write(data[:n])
		data = data[n:]
		s.save()
		if s.broken.Load() {
			t.Fatal("the session stopped saving")
		}
		if d := s.rec.Draft; d != drafts[len(drafts)-1] {
			drafts = append(drafts, d)
		}
	}
	return drafts
}

// replayRecord feeds a session recorded with `script -r` (keys in and
// output out, in the order they happened) through a session: keys go
// through its input path, output through the shadow screen, and a save
// runs after every output frame. At each Ctrl+G the draft saved must be
// the next editor copy, name.editor-N.txt.
func replayRecord(t *testing.T, name string) {
	t.Helper()
	data := readFixture(t, "2.1.282/"+name+".rec")
	s := &session{
		screen: vt.NewEmulator(120, 40),
		rec:    newRecord([]string{"claude"}, "/w"),
		store:  testStore(t),
		pastes: &pasteTracker{},
		prof:   &claude,
	}
	go io.Copy(io.Discard, s.screen)
	var now time.Time
	s.deletes.now = func() time.Time { return now }
	save := func() {
		if s.save(); s.broken.Load() {
			t.Fatal("the session stopped saving")
		}
	}
	checked := 0
	eachChunk(t, data, func(at time.Time, dir byte, chunk []byte) {
		now = at
		switch dir {
		case 'i':
			if string(chunk) == "\x07" {
				checked++
				want := string(readFixture(t, fmt.Sprintf("2.1.282/%s.editor-%d.txt", name, checked)))
				if s.rec.Draft != want {
					t.Fatalf("at Ctrl+G %d the draft differs from Claude Code's:\n got %q\nwant %q", checked, s.rec.Draft, want)
				}
			}
			s.input(chunk)
		case 'o':
			for len(chunk) > 0 {
				k := len(chunk)
				if i := bytes.Index(chunk, frameEnd); i >= 0 {
					k = i + len(frameEnd)
				}
				s.write(chunk[:k])
				chunk = chunk[k:]
				if !s.inFrame {
					save()
				}
			}
		}
	})
	files, _ := filepath.Glob(filepath.Join("testdata", "claude", "2.1.282", name+".editor-*.txt"))
	if checked == 0 || checked != len(files) {
		t.Fatalf("%d Ctrl+G checks for %d editor copies", checked, len(files))
	}
}

// eachChunk calls fn with each chunk of a `script -r` record, in order:
// when it happened, its direction ('i' keys in, 'o' output out, others
// for start and end) and its bytes.
func eachChunk(t *testing.T, data []byte, fn func(at time.Time, dir byte, chunk []byte)) {
	t.Helper()
	for len(data) > 0 {
		// Each chunk: its length, seconds, microseconds and direction.
		if len(data) < 24 {
			t.Fatal("truncated record")
		}
		n := binary.LittleEndian.Uint64(data)
		at := time.Unix(int64(binary.LittleEndian.Uint64(data[8:])), int64(binary.LittleEndian.Uint32(data[16:]))*1000)
		fn(at, data[20], data[24:24+n])
		data = data[24+n:]
	}
}

// Recorded with keys and output together:
//   - deletes: Backspace across a line break mid-box, Ctrl+K, Backspace at
//     the end of the draft, a new row and a Backspace in one burst (the
//     box scrolls a row out of sight above as the key deletes), Delete, and
//     Backspace over whole rows at the end, all with the box scrolled;
//   - bursts: a new row and Ctrl+W, a new row and Delete, Up and Delete,
//     and Up and Ctrl+K, each landing in one frame mid-box; the last scrolls
//     a row out of sight below as the key deletes;
//   - trailing-rows: Ctrl+K and Delete on the last rows with text, above
//     blank rows that stay;
//   - blank-lines: a draft that starts with a line break, then a blank line
//     scrolled onto the marker row, with text out of sight above;
//   - wide-run: runs of wide characters longer than a row, one after a
//     single narrow character (the row ends a cell short) and one after a
//     word (the row is full);
//   - word-deletes: last rows emptied as in trailing-rows, with other keys,
//     each in one burst: Ctrl+D across the last row with text, Alt+D
//     across the row above it, Alt+Backspace across the row above that;
//   - placeholders: a pasted image path, which shows as [Image #1], then a
//     long paste; a long paste pasted twice, which shows the whole text
//     and then cuts its middle out as [...Truncated text #4 +137 lines...];
//     after the editor returns, that middle shows as [Pasted text #4 +137
//     lines].
func TestReplayClaudeKeysAndOutput(t *testing.T) {
	for _, name := range []string{"deletes", "bursts", "trailing-rows", "blank-lines", "wide-run", "word-deletes", "placeholders"} {
		t.Run(name, func(t *testing.T) { replayRecord(t, name) })
	}
}

// sends is Claude Code answering the keys that send or clear a box, run on
// a dummy key so every send fails at once with a 401: Enter on a draft
// holding a paste placeholder, Ctrl+C, Esc Esc, Enter and Ctrl+C in one
// read, and Enter with the next message typed 0.2 s later (the agent had
// drawn the empty box by then), then cleared with Ctrl+C. It is replayed
// with a save after every output frame and with one every 0.4 s of
// recorded time, as a live session saves.
func TestReplayClaudeSends(t *testing.T) {
	sent := []string{
		"send this, with a paste: pasted line 1\npasted line 2\npasted line 3\npasted line 4\npasted line 5\npasted line 6\npasted line 7 and the end",
		"sent, then typing right away",
	}
	history := []string{"cleared with ctrl-c", "cleared with esc esc", "enter then ctrl-c at once", "next one"}
	for _, tick := range []time.Duration{0, saveInterval} {
		t.Run(fmt.Sprint("save every ", tick), func(t *testing.T) {
			s := newSendSession(t, &claude)
			s.screen = vt.NewEmulator(120, 40)
			go io.Copy(io.Discard, s.screen)
			var next time.Time
			eachChunk(t, readFixture(t, "2.1.282/sends.rec"), func(at time.Time, dir byte, chunk []byte) {
				for tick > 0 && !next.IsZero() && !at.Before(next) {
					s.save()
					next = next.Add(tick)
				}
				if next.IsZero() {
					next = at.Add(tick)
				}
				switch dir {
				case 'i':
					s.input(chunk)
				case 'o':
					for len(chunk) > 0 {
						k := len(chunk)
						if i := bytes.Index(chunk, frameEnd); i >= 0 {
							k = i + len(frameEnd)
						}
						s.write(chunk[:k])
						chunk = chunk[k:]
						if tick == 0 && !s.inFrame {
							s.save()
						}
					}
				}
			})
			s.save()
			s.expect(sent, history)
			if s.rec.Draft != "" {
				t.Fatalf("draft %q at the end", s.rec.Draft)
			}
		})
	}
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "claude", path))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Typed drafts that end with Ctrl+G, whose editor copy is the truth:
//   - tall-wrapped: 30 rows in a box of 15, with accents, an emoji and wide
//     characters at the wrap edge, rows that just miss and just fit the
//     width, and an edit made with the box scrolled, so text is out of
//     sight above and below;
//   - long-word: a word longer than a row, broken at the edge.
func TestReplayClaudeAgainstEditorCopy(t *testing.T) {
	for _, name := range []string{"tall-wrapped", "long-word"} {
		t.Run(name, func(t *testing.T) {
			drafts := replayBytes(t, 120, 40, readFixture(t, "2.1.282/"+name+".bin"))
			want := string(readFixture(t, "2.1.282/"+name+".editor.txt"))
			if got := drafts[len(drafts)-1]; got != want {
				t.Fatalf("saved draft differs from Claude Code's:\n got %q\nwant %q", got, want)
			}
			// No delete key was pressed, so no save may lose a word.
			for i := 1; i < len(drafts); i++ {
				if n := lostChars(drafts[i-1], drafts[i]); n > 0 {
					t.Fatalf("save %d lost %d characters:\n%q\n->\n%q", i, n, drafts[i-1], drafts[i])
				}
			}
		})
	}
}

// Typing, Ctrl+G to the editor (its screen shows no box, so the draft
// stays), and the repaint after it returns.
func TestReplayClaudeEditorRoundTrip(t *testing.T) {
	drafts := replayBytes(t, 120, 40, readFixture(t, "2.1.282/editor-probe.bin"))
	want := string(readFixture(t, "2.1.282/editor-probe.editor.txt"))
	if !slices.Equal(drafts, []string{"", want}) {
		t.Fatalf("drafts %q, want %q", drafts, []string{"", want})
	}
}

// Keystroke by keystroke: letters, a new line, a letter and Backspace, and
// a paste that becomes a placeholder. typing.bin was logged after the box
// was drawn, so the start-up paint of the same build at the same size
// comes first.
func TestReplayClaudeTyping(t *testing.T) {
	start := readFixture(t, "2.1.282/editor-probe.bin")
	start = start[:bytes.Index(start, frameEnd)+len(frameEnd)]
	data := append(start, readFixture(t, "2.1.282/typing.bin")...)
	want := []string{"", "h", "he", "hel", "hell", "hello", "hello\n", "hello\nx", "hello\n",
		"hello\n[Pasted text #14 +3 lines]"}
	if drafts := replayBytes(t, 120, 40, data); !slices.Equal(drafts, want) {
		t.Fatalf("drafts %q\nwant   %q", drafts, want)
	}
}

// Two start-ups at 100x30 from a configured install: an empty box that
// shows the dim hint, which must not be saved as a draft; and a draft kept
// through Ctrl+Z, the shell writing over the screen, fg and the repaint.
func TestReplayClaudeHintAndSuspend(t *testing.T) {
	if drafts := replayBytes(t, 100, 30, readFixture(t, "2.1.282/empty-hint.bin")); !slices.Equal(drafts, []string{""}) {
		t.Fatalf("the empty box's hint was saved: %q", drafts)
	}
	want := []string{"", "draft before ctrl-z", "draft before ctrl-z and after fg"}
	if drafts := replayBytes(t, 100, 30, readFixture(t, "2.1.282/suspend.bin")); !slices.Equal(drafts, want) {
		t.Fatalf("drafts %q\nwant   %q", drafts, want)
	}
}

// Screens copied out of tmux (capture-pane -e), one per state. ok false
// means the reader must not find a box: the draft is kept as it was.
var claudeScreens = []struct {
	file  string
	ok    bool
	draft string
}{
	{"2.1.282/screens/empty.txt", true, ""},
	{"2.1.282/screens/shell-empty.txt", true, ""},
	{"2.1.282/screens/typed.txt", true, "do nothing; reply ok (line one)\nline two\nline three after backslash-enter"},
	{"2.1.282/screens/newline-keys.txt", true, "do nothing; reply ok (line one)\nline two\nline three after backslash-enter\nafter ctrl-j\nafter csi-u shift-enter"},
	{"2.1.282/screens/up-in-draft.txt", true, "do nothing; reply ok (line one)\nline two\nline three after backslash-enter\nafter ctrl-j\nafter csi-u shift-enter"},
	{"2.1.282/screens/after-editor.txt", true, "do nothing; reply ok (line one)\nline two\nline three after backslash-enter\nafter ctrl-j\nafter csi-u shift-enter"},
	{"2.1.282/screens/history.txt", false, ""},
	{"2.1.282/screens/history-search.txt", false, ""},
	{"2.1.282/screens/slash-menu.txt", true, "/mode"},
	{"2.1.282/screens/shell.txt", true, "!echo harmless"},
	{"2.1.282/screens/paste-3-lines.txt", true, "paste3 line a\npaste3 line b\npaste3 line c"},
	{"2.1.282/screens/paste-4-lines.txt", true, "[Pasted text #1 +3 lines]"},
	{"2.1.282/screens/paste-5-lines.txt", true, "[Pasted text #2 +4 lines]"},
	{"2.1.282/screens/paste-6-lines.txt", true, "[Pasted text #3 +5 lines]"},
	{"2.1.282/screens/paste-12-lines.txt", true, "[Pasted text #4 +11 lines]"},
	{"2.1.282/screens/paste-before-again.txt", true, "[Pasted text #12 +4 lines]"},
	{"2.1.282/screens/paste-again.txt", true, "aaaa\nbbbb\ncccc\ndddd\neeee"},
	{"2.1.282/screens/tall.txt", true, "row 11\nrow 12\nrow 13\nrow 14\nrow 15\nrow 16\nrow 17\nrow 18\nrow 19\nrow 20\nrow 21\nrow 22\nrow 23\nrow 24\nrow 25"},
	{"2.1.282/screens/leading-break.txt", true, "\nafter a leading break"},
	{"2.1.282/screens/trust-dialog.txt", false, ""},
	{"2.1.282/screens/blank-top.txt", true, "\npara two line 01\npara two line 02\npara two line 03\npara two line 04\npara two line 05\npara two line 06\npara two line 07\npara two line 08\npara two line 09\npara two line 10\npara two line 11\npara two line 12\npara two line 13\npara two line 14"},
	{"2.1.280/screens/empty.txt", true, ""},
	{"2.1.280/screens/draft.txt", true, "do nothing; reply ok\ntwo[Pasted text #1 +3 lines]"},
	{"2.1.280/screens/scrolled.txt", true, "r8\nr9\nr10\nr11\nr12\nr13\nr14\nr15\nr16\nr17\nr18\nr19\nr20\nr21\nr22"},
}

func TestReplayClaudeScreens(t *testing.T) {
	for _, c := range claudeScreens {
		t.Run(c.file, func(t *testing.T) {
			text := strings.TrimSuffix(string(readFixture(t, c.file)), "\n")
			e := vt.NewEmulator(120, 40)
			go io.Copy(io.Discard, e)
			e.Write([]byte("\x1b[H\x1b[2J" + strings.ReplaceAll(text, "\n", "\r\n")))
			v, ok := claudeBox(snapshot(e))
			if ok != c.ok {
				t.Fatalf("box found %v, want %v (rows %q)", ok, c.ok, v.rows)
			}
			var st stitcher
			if got := st.update(v, claude.unwrap); ok && got != c.draft {
				t.Fatalf("draft %q, want %q", got, c.draft)
			}
		})
	}
	// Every screen in testdata is checked: a new one needs its draft here.
	files, _ := filepath.Glob(filepath.Join("testdata", "claude", "*", "screens", "*"))
	if len(files) != len(claudeScreens) {
		t.Fatalf("%d screens in testdata, %d checked", len(files), len(claudeScreens))
	}
}

// Replays of real Codex 0.158.0 captures in testdata/codex/0.158.0 (see
// the README there), through the same shadow screen, stitcher and paste
// tracker as a live session, drawn by Codex's profile: its wrap model,
// its scroll and its paste rule. At each Ctrl+G the draft must be the
// next editor copy.

// codexStandIn reads Codex 0.158.0's box for these replays until Codex's
// own reader lands with its profile: on the alternate screen, the rows
// from the lowest bold, not dim, not reversed › at column 0 down to the
// padding row above the two footer rows, from column 2. Rows wrap at the
// width minus 3, and the box stops growing at the height minus 4 rows
// (counted one lower, the safe side). It knows nothing of menus or turns:
// the replays below show neither with a draft in the box.
func codexStandIn(s *screen) (view, bool) {
	end := len(s.rows) - 3 // the padding row
	if !s.alt || end < 1 || s.rows[end].text() != "" {
		return view{}, false
	}
	for y := end - 1; y >= 0; y-- {
		r := s.rows[y]
		if r.cells[0] != "›" || !r.look[0].bold || r.look[0].reverse || r.faint[0] {
			continue
		}
		v := view{cursor: -1, width: s.cols - 3, under: -1, capped: end-y >= len(s.rows)-5}
		if end == y+1 && (r.textFrom(2) == "" || r.faintFrom(2)) {
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
		return v, true
	}
	return view{}, false
}

// codexShows reports whether got is Codex's draft truth as far as Codex's
// screen shows it: byte for byte, but for two things its rows cannot
// tell. A line break before a word that would not fit after the row reads
// as a wrap (byteExact's limit, here where a line starts with a word as
// wide as a row, or wider), and spaces at the very end of the draft are
// not drawn. Tolerated by Codex's measured drawing, not by the profile's
// model, so a wrong model cannot excuse itself.
func codexShows(got, truth string) bool {
	return got == truth || byteExact(got, strings.TrimRight(truth, " "), 117, wordWrap{endRow: true})
}

// replayCodex replays name.rec through a session with prof, read by
// codexStandIn while prof has no reader, and returns the first of the
// first checks Ctrl+G (all when 0) whose editor copy the saved draft
// differs from.
func replayCodex(t *testing.T, prof *profile, name string, checks int) error {
	t.Helper()
	if prof.read == nil {
		p := *prof
		p.read = codexStandIn
		prof = &p
	}
	folder := filepath.Join("testdata", "codex", "0.158.0")
	data, err := os.ReadFile(filepath.Join(folder, name+".rec"))
	if err != nil {
		t.Fatal(err)
	}
	s := &session{
		screen: vt.NewEmulator(120, 40),
		rec:    newRecord([]string{"codex"}, "/w"),
		store:  testStore(t),
		pastes: &pasteTracker{},
		prof:   prof,
	}
	go io.Copy(io.Discard, s.screen)
	var now time.Time
	s.deletes.now = func() time.Time { return now }
	checked := 0
	var failed error
	eachChunk(t, data, func(at time.Time, dir byte, chunk []byte) {
		now = at
		switch dir {
		case 'i':
			if k := keysIn(chunk); len(k) == 1 && k[0].key == ctrl('g') {
				checked++
				want, err := os.ReadFile(filepath.Join(folder, fmt.Sprintf("%s.editor-%d.txt", name, checked)))
				if err != nil {
					t.Fatal(err)
				}
				if failed == nil && (checks == 0 || checked <= checks) && !codexShows(s.rec.Draft, string(want)) {
					failed = fmt.Errorf("%s: at Ctrl+G %d the draft differs from Codex's:\n got %q\nwant %q", name, checked, s.rec.Draft, want)
				}
			}
			s.input(chunk)
		case 'o':
			for len(chunk) > 0 {
				k := len(chunk)
				if i := bytes.Index(chunk, frameEnd); i >= 0 {
					k = i + len(frameEnd)
				}
				s.write(chunk[:k])
				chunk = chunk[k:]
				if !s.inFrame {
					if s.save(); s.broken.Load() {
						t.Fatal("the session stopped saving")
					}
				}
			}
		}
	})
	copies, _ := filepath.Glob(filepath.Join(folder, name+".editor-*.txt"))
	if failed == nil && (checked == 0 || checked != len(copies)) {
		t.Fatalf("%s: %d Ctrl+G checks for %d editor copies", name, checked, len(copies))
	}
	return failed
}

// Recorded with keys and output together (testdata/codex/README.md):
//   - typed: one line;
//   - multiline: new lines by Ctrl+J, Alt+Enter and Shift+Enter, one blank;
//   - accents: accents precomposed and decomposed, emoji, a wide character
//     that does not fit at the edge of a word broken there, and a wrap at a
//     space after a word 115 columns wide;
//   - wrap: lines that end on a full row, each followed by Codex's empty
//     row: before a blank line, before a line, a row of words, a word two
//     rows long, and at the end of the draft;
//   - pastes: pastes inline at 3 lines and 1,000 characters, two of 1,001
//     characters as "[Pasted Content 1001 chars]" and "... #2", the box
//     after the editor round trip showing them whole, and one of 1,079
//     characters with 11 line breaks, sent with LF and with CR;
//   - tall: 45 lines in a box of 36 that scrolls a row at a time, edited 40
//     rows up and read back from the bottom, the edit out of sight. Only
//     its first Ctrl+G: before the second, Ctrl+W deletes a word 30 rows
//     up, which only Codex's delete keys, still to come with its profile,
//     tell from a word scrolled away.
var codexReplays = []struct {
	name   string
	checks int
}{{"typed", 0}, {"multiline", 0}, {"accents", 0}, {"wrap", 0}, {"pastes", 0}, {"tall", 1}}

func TestReplayCodex(t *testing.T) {
	for _, r := range codexReplays {
		t.Run(r.name, func(t *testing.T) {
			if err := replayCodex(t, &codex, r.name, r.checks); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A check that cannot fail is not a check: with Codex's wrap model swapped
// for character wrap, or for Claude Code's word wrap with no end row, some
// replay must go red.
func TestReplayCodexCatchesAWrongWrapModel(t *testing.T) {
	for _, m := range []struct {
		name string
		rule unwrapRule
	}{{"character wrap", charWrap{}}, {"word wrap with no end row", wordWrap{}}} {
		p := codex
		p.unwrap = m.rule
		var red []string
		for _, r := range codexReplays {
			if replayCodex(t, &p, r.name, r.checks) != nil {
				red = append(red, r.name)
			}
		}
		if len(red) == 0 {
			t.Errorf("with %s every Codex replay stayed green", m.name)
		}
		t.Logf("with %s, red: %s", m.name, strings.Join(red, ", "))
	}
}

// charWrap is character wrap, the model of aider and goose in docs/SPEC.md
// section 4 step 17: rows break at the width wherever it falls, and a full
// row runs on into the next with nothing between. Here it is only the
// mutant the Codex replays must catch.
type charWrap struct{}

func (charWrap) wrap(text string, width int) (rows []string, starts []int) {
	at := 0
	for _, line := range strings.Split(text, "\n") {
		for rest, pos := line, at; ; {
			head := rest
			if width > 0 && runewidth.StringWidth(rest) > width {
				if head = runewidth.Truncate(rest, width, ""); head == "" {
					_, n := utf8.DecodeRuneInString(rest)
					head = rest[:n]
				}
			}
			rows, starts = append(rows, head), append(starts, pos)
			if rest, pos = rest[len(head):], pos+len(head); rest == "" {
				break
			}
		}
		at += len(line) + 1
	}
	return rows, starts
}

func (charWrap) unwrap(rows []string, width int) string {
	var b strings.Builder
	for i, r := range rows {
		// A row a cell short of full is full too: a wide character did not
		// fit at its end.
		if i > 0 && runewidth.StringWidth(rows[i-1]) < width-1 {
			b.WriteByte('\n')
		}
		b.WriteString(r)
	}
	return b.String()
}
