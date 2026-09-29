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
	return replayBytesAs(t, &claude, cols, rows, data)
}

// replayBytesAs is replayBytes with the session drawn by prof.
func replayBytesAs(t *testing.T, prof *profile, cols, rows int, data []byte) []string {
	t.Helper()
	s := &session{
		screen: vt.NewEmulator(cols, rows),
		rec:    newRecord([]string{"claude"}, "/w"),
		store:  replayStore(t),
		pastes: &pasteTracker{},
		prof:   prof,
	}
	drainScreen(t, s.screen)
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

// drainScreen reads and drops what the emulator answers the agent's
// terminal queries with, as the real terminal does in a session, until the
// test ends. Then it closes the emulator's input pipe, so the reader
// returns and the emulator (4 MB of parse buffer alone) can be freed: a
// reader left blocked holds it for the rest of the run, and with a few
// hundred replays the race run's peak memory passed 10 GB. The pipe is
// closed, not the emulator, whose Close writes a flag its Read reads
// unguarded, a data race. The returned func closes it sooner, for a
// helper done with the emulator before its test ends.
func drainScreen(t *testing.T, e *vt.Emulator) (stop func()) {
	t.Helper()
	go io.Copy(io.Discard, e)
	stop = func() { e.InputPipe().(io.Closer).Close() }
	t.Cleanup(stop)
	return stop
}

// replayRecord feeds a session recorded with `script -r` (keys in and
// output out, in the order they happened) through a session: keys go
// through its input path, output through the shadow screen, and a save
// runs after every output frame. At each Ctrl+G the draft saved must be
// the next editor copy, name.editor-N.txt.
func replayRecord(t *testing.T, name string) {
	t.Helper()
	replayRecordAs(t, &claude, name)
}

// replayRecordAs is replayRecord with the session drawn by prof.
func replayRecordAs(t *testing.T, prof *profile, name string) {
	t.Helper()
	data := readFixture(t, "2.1.282/"+name+".rec")
	s := &session{
		screen: vt.NewEmulator(120, 40),
		rec:    newRecord([]string{"claude"}, "/w"),
		store:  replayStore(t),
		pastes: &pasteTracker{},
		prof:   prof,
	}
	drainScreen(t, s.screen)
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
			drainScreen(t, s.screen)
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

// replayRun feeds a whole run recorded by unsent's raw log (keys in and
// output out, `script -r` format) through a session at 120x40: keys through
// its input path, output through the shadow screen, with a save after every
// output frame (tick 0) or one every tick of recorded time, as a live
// session saves. closed says the window closed at the end: the close comes
// just before Claude Code's teardown (mouse reports off) that answers it.
// Otherwise the agent left on the keys. It returns the finished session and
// every draft saved on the way.
func replayRun(t *testing.T, file string, tick time.Duration, closed bool) (sendSession, []string) {
	t.Helper()
	s := newSendSession(t, &claude)
	s.screen = vt.NewEmulator(120, 40)
	go io.Copy(io.Discard, s.screen)
	drafts := []string{""}
	save := func() {
		s.save()
		if d := s.rec.Draft; d != drafts[len(drafts)-1] {
			drafts = append(drafts, d)
		}
	}
	var next time.Time
	eachChunk(t, readFixture(t, file), func(at time.Time, dir byte, chunk []byte) {
		for tick > 0 && !next.IsZero() && !at.Before(next) {
			save()
			next = next.Add(tick)
		}
		if next.IsZero() {
			next = at.Add(tick)
		}
		switch dir {
		case 'i':
			s.input(chunk)
		case 'o':
			if closed && !s.closed && bytes.Contains(chunk, []byte("\x1b[?1006l")) {
				s.closing()
			}
			for len(chunk) > 0 {
				k := len(chunk)
				if i := bytes.Index(chunk, frameEnd); i >= 0 {
					k = i + len(frameEnd)
				}
				s.write(chunk[:k])
				chunk = chunk[k:]
				if tick == 0 && !s.inFrame {
					save()
				}
			}
		}
	})
	if closed && !s.closed {
		t.Fatal("no teardown to close the window on")
	}
	save()
	s.finish()
	return s, drafts
}

// Runs of Claude Code 2.1.284 with slash commands and a named session,
// recorded by unsent's raw log in a scratch config (docs/research/claude.md
// section 13). After /rename, Claude Code writes the session's name into
// the box's top rule; a reader that took only a plain rule for the box lost
// it for the rest of the run, kept the /rename line as the draft and left
// it behind as an orphan, and saved nothing typed after it. Each case says
// what the store holds once the agent is gone: the sent log, history, and
// the draft left behind (the orphan, "" for none).
//   - rename-enter: /rename calls, Enter, Ctrl+C twice;
//   - rename-after-esc: /rename, Esc closes the command menu, " calls",
//     Enter, then /exit and Enter;
//   - title-in-rule: /rename alpha-notes, Enter, Ctrl+C twice;
//   - rename-then-typed: a prompt typed under the titled rule, then Ctrl+C
//     twice, which clears it first;
//   - rename-main-screen: the same on the main-screen renderer, the window
//     closed with the prompt half typed;
//   - resumed-named: claude --resume alpha-notes, whose box has the titled
//     rule from its first frame, a prompt typed and Ctrl+C twice;
//   - resume-picker-close: /resume, a search typed into the picker, Esc,
//     the window closed with the picker open;
//   - slash-clear, slash-help, slash-model: the command, Enter, Esc, /exit;
//   - agents-view: /rename alpha-notes, Enter, ← to the agents view, whose
//     box shows a grey placeholder, Esc back, a message sent, ← again and
//     Ctrl+C twice; its output also sets the window title "✳ alpha-notes";
//   - rename-long-name: /rename with a name wider than the window, which
//     the rule shows cut with an ellipsis and no rule before it, then a
//     prompt typed under it and Ctrl+C twice, which clears it first;
//   - resume-picker-ctrlc: a message sent, /resume, Ctrl+C twice, which
//     leaves the picker open, the window closed;
//   - model-ctrlc: /model, Ctrl+C twice, which leaves the dialog open, the
//     window closed;
//   - control: a prompt half typed, the window closed.
func TestReplayClaudeSlashCommandsAndNames(t *testing.T) {
	for _, c := range []struct {
		name    string
		closed  bool
		sent    []string
		history []string
		left    string
	}{
		{"rename-enter", false, []string{"/rename calls"}, nil, ""},
		{"rename-after-esc", false, []string{"/rename calls", "/exit"}, nil, ""},
		{"title-in-rule", false, []string{"/rename alpha-notes"}, nil, ""},
		{"rename-then-typed", false, []string{"/rename calls"}, []string{"a real prompt typed after the rename"}, ""},
		{"rename-main-screen", true, []string{"/rename alpha-notes"}, nil, "half typed after rename"},
		{"resumed-named", false, nil, []string{"typed in the resumed named session"}, ""},
		{"resume-picker-close", true, nil, []string{"/resume"}, ""},
		{"slash-clear", false, []string{"/clear", "/exit"}, nil, ""},
		{"slash-help", false, []string{"/exit"}, []string{"/help"}, ""},
		{"slash-model", false, []string{"/exit"}, []string{"/model"}, ""},
		{"agents-view", false, []string{"/rename alpha-notes", "hello there"}, nil, ""},
		{"rename-long-name", false, []string{"/rename " + strings.Repeat("long-name-", 14) + "end"}, []string{"typed after long rename"}, ""},
		{"resume-picker-ctrlc", true, []string{"hello"}, []string{"/resume"}, ""},
		{"model-ctrlc", true, nil, []string{"/model"}, ""},
		{"control", true, nil, nil, "please refactor the parser so that"},
	} {
		for _, tick := range []time.Duration{0, saveInterval} {
			t.Run(fmt.Sprint(c.name, " save every ", tick), func(t *testing.T) {
				s, drafts := replayRun(t, "2.1.284/"+c.name+".rec", tick, c.closed)
				s.expect(c.sent, c.history)
				var left []string
				for _, r := range s.store.load(false) {
					left = append(left, r.Draft)
				}
				if want := []string{c.left}; c.left == "" && len(left) > 0 || c.left != "" && !slices.Equal(left, want) {
					t.Errorf("left behind %q, want %q", left, c.left)
				}
				// Nothing but typed text is ever a draft: not the session's
				// name, from the rule or the window title, not a picker's
				// search, not a placeholder.
				for _, d := range drafts {
					switch d := strings.TrimSpace(d); {
					case d == "calls", d == "alpha-notes", d == "alph", d == "describe a task for a new session",
						strings.HasSuffix(d, "…"):
						t.Errorf("saved %q as a draft (all: %q)", d, drafts)
					}
				}
				if l := s.exitLines(); l != nil {
					t.Errorf("exit lines %q", l)
				}
			})
		}
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
	{"2.1.284/screens/titled-empty.txt", true, ""},
	{"2.1.284/screens/titled-draft.txt", true, "a real prompt typed after the rename"},
	{"2.1.284/screens/titled-draft-main.txt", true, "half typed after rename"},
	{"2.1.284/screens/resume-picker.txt", false, ""},
	{"2.1.284/screens/agents-view.txt", true, ""},
	{"2.1.280/screens/empty.txt", true, ""},
	{"2.1.280/screens/draft.txt", true, "do nothing; reply ok\ntwo[Pasted text #1 +3 lines]"},
	{"2.1.280/screens/scrolled.txt", true, "r8\nr9\nr10\nr11\nr12\nr13\nr14\nr15\nr16\nr17\nr18\nr19\nr20\nr21\nr22"},
}

func TestReplayClaudeScreens(t *testing.T) {
	for _, c := range claudeScreens {
		t.Run(c.file, func(t *testing.T) {
			text := strings.TrimSuffix(string(readFixture(t, c.file)), "\n")
			e := vt.NewEmulator(120, 40)
			drainScreen(t, e)
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
// the README there), through the same shadow screen, reader, stitcher and
// paste tracker as a live session, with Codex's profile. At each Ctrl+G
// the draft must be the next editor copy.

// codexShows reports whether got is Codex's draft truth as far as Codex's
// screen shows it: byte for byte, but for two things its rows cannot
// tell. A line break before a word that would not fit after the row reads
// as a wrap (byteExact's limit, here where a line starts with a word as
// wide as a row, or wider), spaces at the end of a line are drawn as the
// blank cells after it (the end of the draft in pastes, a word deleted
// with Ctrl+W at the end of a line in tall), and a tab is drawn as one
// space (paste-exact). Tolerated by Codex's measured drawing, not by the
// profile's model, so a wrong model cannot excuse itself.
func codexShows(got, truth string) bool {
	lines := strings.Split(strings.ReplaceAll(truth, "\t", " "), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return got == truth || byteExact(got, strings.Join(lines, "\n"), 117, wordWrap{endRow: true})
}

// replayCodex replays name.rec through a session with prof and returns
// the first Ctrl+G whose editor copy the saved draft differs from. At the
// Ctrl+G numbers in recalled the box holds an entry of Codex's history
// brought back with Up, which is not a draft: nothing new may be saved
// there, so the draft must be the empty box's.
func replayCodex(t *testing.T, prof *profile, name string, recalled ...int) error {
	t.Helper()
	_, err := replayCodexSession(t, prof, name, recalled...)
	return err
}

// replayCodexSession is replayCodex, and also returns the session.
func replayCodexSession(t *testing.T, prof *profile, name string, recalled ...int) (*session, error) {
	t.Helper()
	return replayCaptureSession(t, prof, codexCaptures, name, recalled...)
}

// A captureSet is one agent version's captures made with unsent capture
// (testdata/<agent>/<version>): where they are, the command they ran, and
// how the agent's screen shows a draft (shows: whether got is the editor
// copy truth as far as the screen can tell).
type captureSet struct {
	folder, command string
	shows           func(got, truth string) bool
	// saved, when set, sees the session after every save.
	saved func(*session)
	// tick, when set, saves once every tick of recorded time, as a live
	// session saves, instead of after every output frame.
	tick time.Duration
}

var codexCaptures = captureSet{folder: filepath.Join("testdata", "codex", "0.158.0"), command: "codex", shows: codexShows}

// replayCaptureSession replays name.rec of set through a session with prof,
// with a save after every output frame (or every set.tick), and returns the session and the
// first Ctrl+G whose editor copy the saved draft differs from. At the
// Ctrl+G numbers in recalled the box holds an entry of the agent's history
// brought back with Up, which is not a draft: the draft must be the empty
// box's.
func replayCaptureSession(t *testing.T, prof *profile, set captureSet, name string, recalled ...int) (*session, error) {
	t.Helper()
	folder := set.folder
	data, err := os.ReadFile(filepath.Join(folder, name+".rec"))
	if err != nil {
		t.Fatal(err)
	}
	s := &session{
		screen: vt.NewEmulator(120, 40),
		rec:    newRecord([]string{set.command}, "/w"),
		store:  replayStore(t),
		pastes: &pasteTracker{},
		prof:   prof,
	}
	drainScreen(t, s.screen)
	var now time.Time
	s.deletes.now = func() time.Time { return now }
	checked := 0
	var failed error
	save := func() {
		if s.save(); s.broken.Load() {
			t.Fatal("the session stopped saving")
		}
		if set.saved != nil {
			set.saved(s)
		}
	}
	var next time.Time
	eachChunk(t, data, func(at time.Time, dir byte, chunk []byte) {
		for set.tick > 0 && !next.IsZero() && !at.Before(next) {
			now = next
			save()
			next = next.Add(set.tick)
		}
		if next.IsZero() {
			next = at.Add(set.tick)
		}
		now = at
		switch dir {
		case 'i':
			if k := keysIn(chunk); len(k) == 1 && k[0].key == ctrl('g') {
				checked++
				want, err := os.ReadFile(filepath.Join(folder, fmt.Sprintf("%s.editor-%d.txt", name, checked)))
				if err != nil {
					t.Fatal(err)
				}
				switch {
				case failed != nil:
				case slices.Contains(recalled, checked):
					if s.rec.Draft != "" {
						failed = fmt.Errorf("%s: at Ctrl+G %d the box holds a history entry, saved as the draft %q", name, checked, s.rec.Draft)
					}
				case !set.shows(s.rec.Draft, string(want)):
					failed = fmt.Errorf("%s: at Ctrl+G %d the draft differs from %s's:\n got %q\nwant %q", name, checked, set.command, s.rec.Draft, want)
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
				if set.tick == 0 && !s.inFrame {
					save()
				}
			}
		}
	})
	copies, _ := filepath.Glob(filepath.Join(folder, name+".editor-*.txt"))
	if failed == nil && checked != len(copies) {
		t.Fatalf("%s: %d Ctrl+G checks for %d editor copies", name, checked, len(copies))
	}
	return s, failed
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
//     rows up and read back from the bottom, the edit out of sight, then a
//     word deleted with Ctrl+W 30 rows up, which only Codex's delete keys
//     tell from a word scrolled away;
//   - deletes, deletes-xterm: every delete key, in the CSI-u forms Codex
//     asks for and as legacy bytes;
//   - placeholder: Backspace right after a placeholder, then a placeholder
//     cleared with Ctrl+C and brought back with Up (the second Ctrl+G);
//   - ctrlc-history: a draft cleared with Ctrl+C and brought back with Up
//     (the first Ctrl+G), then a draft typed over the history, Up in it
//     and Ctrl+R;
//   - submit: a draft typed while a send that fails retries;
//   - suspend: Ctrl+Z, fg and more typing, through unsent;
//   - suspend-direct: Ctrl+Z to Codex alone, which does not stop, so the
//     fg typed next lands in its box;
//   - resume-id, resume-last, fork-last: a draft in a resumed or forked
//     thread;
//   - tinted: a draft in a box drawn on a background colour;
//   - paste-exact: pastes with tabs, spaces at line ends, blank lines,
//     decomposed accents and a flag, sent with LF and with CR, and one
//     ending in a line break;
//   - orphan: a send that fails, then a draft left at a window close;
//   - restore-id, restore-last, restore-picker: that draft put back by
//     unsent in codex resume <id>, codex resume --last and the picker;
//   - restore-long: a draft of 1,259 characters on 18 lines put back,
//     which Codex shows as "[Pasted Content 1259 chars]";
//   - early-paste: a paste on the first frame that shows the box;
//   - slash-new: /new through its dialog, and a draft in the new thread;
//   - edge-deletes: Ctrl+W, Backspace, Alt+Backspace, Ctrl+U, Ctrl+K,
//     Delete and Ctrl+D on the last row of a box at its cap, where only
//     the keys tell a deletion from rows scrolled out of sight below;
//   - recall-deleted: a draft cleared with Ctrl+C, another emptied with
//     Backspace, then Up brings the first back (the first Ctrl+G);
//   - suspend-paste: Ctrl+Z, fg, then a paste of three lines, which goes
//     in whole because unsent turns bracketed paste back on.
var codexReplays = []struct {
	name     string
	recalled []int
}{{"typed", nil}, {"multiline", nil}, {"accents", nil}, {"wrap", nil}, {"pastes", nil}, {"tall", nil},
	{"deletes", nil}, {"deletes-xterm", nil}, {"placeholder", []int{2}}, {"ctrlc-history", []int{1}},
	{"submit", nil}, {"suspend", nil}, {"suspend-direct", nil}, {"resume-id", nil}, {"resume-last", nil},
	{"fork-last", nil}, {"tinted", nil}, {"paste-exact", nil}, {"orphan", nil}, {"restore-id", nil},
	{"restore-last", nil}, {"restore-picker", nil}, {"restore-long", nil}, {"early-paste", nil}, {"slash-new", nil},
	{"edge-deletes", nil}, {"recall-deleted", []int{1}}, {"suspend-paste", nil}}

func TestReplayCodex(t *testing.T) {
	for _, r := range codexReplays {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			if err := replayCodex(t, &codex, r.name, r.recalled...); err != nil {
				t.Fatal(err)
			}
		})
	}
	// With --no-alt-screen Codex draws its box inline, under what the
	// terminal held, with one footer row: a layout never read, so the
	// reader must find no box in it at all, and the user learns at exit
	// that nothing was saved.
	t.Run("inline", func(t *testing.T) {
		t.Parallel()
		s, err := replayCodexSession(t, &codex, "inline", 1)
		if err != nil || s.matched {
			t.Fatalf("the inline box was read: %v, matched %v", err, s.matched)
		}
	})
	// Every capture with editor copies is replayed: a new one needs a line
	// in codexReplays.
	replayed := map[string]bool{"inline": true}
	for _, r := range codexReplays {
		replayed[r.name] = true
	}
	copies, _ := filepath.Glob(filepath.Join("testdata", "codex", "0.158.0", "*.editor-1.txt"))
	var missing []string
	for _, c := range copies {
		if name := strings.TrimSuffix(filepath.Base(c), ".editor-1.txt"); !replayed[name] {
			missing = append(missing, name)
		}
	}
	if len(copies) == 0 || len(missing) > 0 {
		t.Fatalf("%d captures with editor copies, not replayed: %s", len(copies), strings.Join(missing, ", "))
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
		red := firstRedReplay(t, &p)
		if red == "" {
			t.Errorf("with %s every Codex replay stayed green", m.name)
		}
		t.Logf("with %s, red: %s", m.name, red)
	}
}

// firstRedReplay replays codexReplays with prof in order and returns the
// name of the first that goes red, or "" when all stay green. A mutant
// needs one red replay to be caught; replaying the rest costs a full set
// of captures per mutant and proves nothing more.
func firstRedReplay(t *testing.T, prof *profile) string {
	t.Helper()
	for _, r := range codexReplays {
		if replayCodex(t, prof, r.name, r.recalled...) != nil {
			return r.name
		}
	}
	return ""
}

// Each profile's scroll model must be the one its agent's captures show:
// every time the box scrolled, the cursor landed on a row the model puts
// it on. The model only draws the fuzz's box (stitch_test.go), where a
// wrong one would make the fuzz easier or harder than the agent without a
// test noticing. The captures scroll both ways: Codex's tall moves 40 rows
// up and back down a row at a time, pi's scroll-steps 16, a key at a time,
// and Claude Code's tall-wrapped,
// deletes and bursts move up over rows out of sight mid-box, and
// trailing-rows deletes rows at the end. So, as a check that can fail,
// each profile with the other model must go red.
func TestReplayScrollModel(t *testing.T) {
	codexTall := func(t *testing.T, p *profile) { replayCodex(t, p, "tall") }
	piRec := func(name string) func(*testing.T, *profile) {
		return func(t *testing.T, p *profile) { replayPi(t, p, name) }
	}
	claudeBin := func(name string) func(*testing.T, *profile) {
		return func(t *testing.T, p *profile) { replayBytesAs(t, p, 120, 40, readFixture(t, "2.1.282/"+name+".bin")) }
	}
	claudeRec := func(name string) func(*testing.T, *profile) {
		return func(t *testing.T, p *profile) { replayRecordAs(t, p, name) }
	}
	for _, c := range []struct {
		prof     *profile
		other    scrollModel
		captures []func(*testing.T, *profile)
	}{
		{&codex, scrollMidBox, []func(*testing.T, *profile){codexTall}},
		{&pi, scrollMidBox, []func(*testing.T, *profile){piRec("scroll-steps"), piRec("tall"), piRec("edge-deletes")}},
		{&claude, scrollOneRow, []func(*testing.T, *profile){
			claudeBin("tall-wrapped"), claudeRec("deletes"), claudeRec("bursts"), claudeRec("trailing-rows")}},
	} {
		t.Run(c.prof.name, func(t *testing.T) {
			t.Parallel()
			var all []boxScroll
			for _, run := range c.captures {
				all = append(all, scrollsIn(replayViews(t, c.prof, run))...)
			}
			if len(all) == 0 {
				t.Fatal("no capture scrolled the box")
			}
			wrong := 0
			for _, sc := range all {
				if !scrollLands(c.prof.scroll, sc) {
					t.Errorf("the box scrolled %+d rows with the cursor on row %d of %d, where %s's model never puts it", sc.by, sc.cursor, sc.rows, c.prof.name)
				}
				if !scrollLands(c.other, sc) {
					wrong++
				}
			}
			if wrong == 0 {
				t.Errorf("with the other scroll model all %d scrolls still land", len(all))
			}
			t.Logf("%d scrolls; %d of them where the other model never puts the cursor", len(all), wrong)
		})
	}
}

// boxScroll is one scroll of a box at its height cap between two reads:
// by rows (the text moved up by, when positive), leaving the cursor on
// row cursor of rows.
type boxScroll struct{ by, cursor, rows int }

// scrollLands reports whether a box drawn by model m, as boxSim draws it,
// can leave the cursor where sc did. Typing or moving past the bottom
// scrolls just far enough, so the cursor is on the last row, in both
// models; rows deleted at the end of the draft bring the box's end up
// with the cursor on the last row too. Past the top, one row at a time
// leaves the cursor on the first row; mid-box holds it on the middle row
// over rows out of sight, both ways.
func scrollLands(m scrollModel, sc boxScroll) bool {
	switch {
	case sc.cursor == sc.rows-1:
		return true
	case m == scrollOneRow:
		return sc.by < 0 && sc.cursor == 0
	default:
		return sc.cursor == sc.rows/2
	}
}

// replayViews runs a replay with prof and returns every view with text
// its reader read, in order.
func replayViews(t *testing.T, prof *profile, run func(*testing.T, *profile)) []view {
	t.Helper()
	var views []view
	p := *prof
	read := p.read
	p.read = func(s *screen) (view, bool) {
		v, ok := read(s)
		if ok && !v.empty {
			views = append(views, v)
		}
		return v, ok
	}
	run(t, &p)
	return views
}

// scrollsIn lists the scrolls between consecutive views of a box at its
// cap: the one shift under which all rows the two views share read the
// same, but for one row an edit changed, when it is not zero.
func scrollsIn(views []view) []boxScroll {
	var out []boxScroll
	for n := 1; n < len(views); n++ {
		a, b := views[n-1], views[n]
		h := len(b.rows)
		if len(a.rows) != h || h < 3 || !a.capped || !b.capped || b.cursor < 0 {
			continue
		}
		best, most, tie := 0, -1, false
		for d := -(h - 1); d <= h-1; d++ {
			same, shared := 0, 0
			for i := max(0, -d); i < min(h, h-d); i++ {
				shared++
				if a.rows[i+d] == b.rows[i] && b.rows[i] != "" {
					same++
				}
			}
			switch {
			case shared < 2 || same < shared-1:
			case same > most:
				best, most, tie = d, same, false
			case same == most:
				tie = true
			}
		}
		if most >= 0 && !tie && best != 0 {
			out = append(out, boxScroll{best, b.cursor, h})
		}
	}
	return out
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
