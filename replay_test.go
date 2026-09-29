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

	"github.com/charmbracelet/x/vt"
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
					switch strings.TrimSpace(d) {
					case "calls", "alpha-notes", "alph", "describe a task for a new session":
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
			go io.Copy(io.Discard, e)
			e.Write([]byte("\x1b[H\x1b[2J" + strings.ReplaceAll(text, "\n", "\r\n")))
			v, ok := claudeBox(snapshot(e))
			if ok != c.ok {
				t.Fatalf("box found %v, want %v (rows %q)", ok, c.ok, v.rows)
			}
			var st stitcher
			if got := st.update(v); ok && got != c.draft {
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
