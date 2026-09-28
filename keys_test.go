package main

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDecodeKey(t *testing.T) {
	up, delete := plain(letterKey+'A'), plain(keyDelete)
	cases := []struct {
		in   string
		want key
		n    int
		tok  token
	}{
		// Legacy bytes.
		{"a", plain('a'), 1, tokKey},
		{"é", plain('é'), 2, tokKey},
		{"\x17", ctrl('w'), 1, tokKey},
		{"\x08", ctrl('h'), 1, tokKey},
		{"\x1a", ctrl('z'), 1, tokKey},
		{"\x00", ctrl(' '), 1, tokKey},
		{"\x1f", ctrl('_'), 1, tokKey},
		{"\x7f", plain(keyBackspace), 1, tokKey},
		{"\r", plain(keyEnter), 1, tokKey},
		{"\t", plain(keyTab), 1, tokKey},
		{"\x1b", plain(keyEsc), 1, tokKey},
		{"\x1b\x1b", plain(keyEsc), 1, tokKey}, // Esc, then Esc again
		// ESC-prefixed Alt keys.
		{"\x1bd", alt('d'), 2, tokKey},
		{"\x1b\x7f", alt(keyBackspace), 2, tokKey},
		{"\x1b\r", alt(keyEnter), 2, tokKey},
		{"\x1b\x17", key{'w', modCtrl | modAlt}, 2, tokKey},
		{"\x1bé", alt('é'), 3, tokKey},
		{"\x1bOA", up, 3, tokKey}, // SS3
		{"\x1bO", alt('O'), 2, tokKey},
		// Kitty CSI-u.
		{"\x1b[119;5u", ctrl('w'), 8, tokKey},
		{"\x1b[122;5u", ctrl('z'), 8, tokKey},
		{"\x1b[122;69u", ctrl('z'), 9, tokKey},       // Caps Lock (+64)
		{"\x1b[122;133u", ctrl('z'), 10, tokKey},     // Num Lock (+128)
		{"\x1b[122;197u", ctrl('z'), 10, tokKey},     // both
		{"\x1b[122;5:1u", ctrl('z'), 10, tokKey},     // a press
		{"\x1b[122;5:2u", ctrl('z'), 10, tokKey},     // a repeat counts as a press
		{"\x1b[122;5:3u", ctrl('z'), 10, tokRelease}, // a release is not a key
		{"\x1b[122:90;6u", key{'z', modCtrl | modShift}, 11, tokKey},
		{"\x1b[122:90:122;69:2;26u", key{'z', modCtrl}, 21, tokKey}, // every sub-field
		{"\x1b[97;;97u", plain('a'), 9, tokKey},                     // text as code points, no modifiers
		{"\x1b[27u", plain(keyEsc), 5, tokKey},
		{"\x1b[13u", plain(keyEnter), 5, tokKey},
		{"\x1b[13;5u", ctrl(keyEnter), 7, tokKey},
		{"\x1b[13;2u", key{keyEnter, modShift}, 7, tokKey},
		{"\x1b[127;3u", alt(keyBackspace), 8, tokKey},
		// Functional keys, with modifiers and events.
		{"\x1b[3~", delete, 4, tokKey},
		{"\x1b[3;5~", key{keyDelete, modCtrl}, 6, tokKey},
		{"\x1b[3;1:3~", delete, 8, tokRelease},
		{"\x1b[A", up, 3, tokKey},
		{"\x1b[1;5A", key{letterKey + 'A', modCtrl}, 6, tokKey},
		{"\x1b[Z", key{keyTab, modShift}, 3, tokKey},
		// modifyOtherKeys, as tmux sends it.
		{"\x1b[27;5;119~", ctrl('w'), 11, tokKey},
		{"\x1b[27;3;127~", alt(keyBackspace), 11, tokKey},
		{"\x1b[27;2;13~", key{keyEnter, modShift}, 10, tokKey},
		{"\x1b[27;5;122~", ctrl('z'), 11, tokKey},
		{"\x1b[27;5;90~", ctrl('z'), 10, tokKey}, // Z with Caps Lock
		{"\x1b[27;69;122~", ctrl('z'), 12, tokKey},
		// Reports, never keys.
		{"\x1b[<35;10;5M", key{}, 11, tokReport},
		{"\x1b[<0;1;1m", key{}, 9, tokReport},
		{"\x1b[I", key{}, 3, tokReport},
		{"\x1b[O", key{}, 3, tokReport},
		{"\x1b[M !!", key{}, 6, tokReport},
		{"\x1b[32;10;5M", key{}, 10, tokReport},
		{"\x1b[?5u", key{}, 5, tokReport},
		{"\x1b[?62;22;52c", key{}, 12, tokReport},
		{"\x1b[?2026;2$y", key{}, 11, tokReport},
		{"\x1b[2026;2$y", key{}, 10, tokReport},
		{"\x1b[6;17;8t", key{}, 9, tokReport},
		{"\x1bP>|ghostty 1.3.1\x1b\\", key{}, 19, tokReport},
		{"\x1b_Gi=31;OK\x1b\\", key{}, 12, tokReport},
		{"\x1b]11;rgb:0/0/0\x07", key{}, 15, tokReport},
		{"\x1b]11;rgb:0/0/0", key{}, 14, tokReport}, // cut short: the rest of the read
		// Cut short, or broken.
		{"\x1b[", key{}, 2, tokKey},
		{"\x1b[1", key{}, 3, tokKey},
		{"\x1b[?1", key{}, 4, tokReport},
		{"\x1b[1\x1b[A", key{}, 3, tokKey}, // the next sequence stays whole
	}
	for _, c := range cases {
		k, n, tok := decodeKey([]byte(c.in))
		if k != c.want || n != c.n || tok != c.tok {
			t.Errorf("decodeKey(%q) = %+v %d %d, want %+v %d %d", c.in, k, n, tok, c.want, c.n, c.tok)
		}
	}
}

// The decoder never panics, never stalls and never reads past its input,
// whatever the bytes: it runs on every read before the agent gets it.
func TestDecodeKeyAnyBytes(t *testing.T) {
	alphabet := []byte("\x1b[O];:<>?$0123456789uM~mtyIPAZ\x07\\\x1a\x7fa\xc3\xa9")
	r := rand.New(rand.NewPCG(1, 2))
	for range 200000 {
		b := make([]byte, 1+r.IntN(16))
		for i := range b {
			b[i] = alphabet[r.IntN(len(alphabet))]
		}
		for i := 0; i < len(b); {
			_, n, _ := decodeKey(b[i:])
			if n < 1 || i+n > len(b) {
				t.Fatalf("decodeKey(%q) at %d: length %d", b, i, n)
			}
			i += n
		}
	}
}

// Ctrl+Z is found in any form, anywhere in a read; its release is not a
// press, and a mouse report whose numbers hold 26 is not Ctrl+Z.
func TestFindCtrlZ(t *testing.T) {
	z := []key{ctrl('z')}
	cases := []struct {
		in      string
		at, end int
	}{
		{"\x1a", 0, 1},
		{"ab\x1acd", 2, 3},
		{"abc\x1b[122;5udef", 3, 11},
		{"\x1b[<26;26;26M\x1b[27;5;122~", 12, 23},
		{"x\x1b[122;69:2u", 1, 12},
		{"\x1b[122;5:3u", -1, -1},
		{"\x1b[<26;1;1M\x1b[26;5u", -1, -1},
		{"\x1b\x1a", -1, -1}, // Ctrl+Alt+Z
		{"\x1b[122;6u", -1, -1},
		{"plain text", -1, -1},
	}
	for _, c := range cases {
		if at, end := findKey([]byte(c.in), z); at != c.at || end != c.end {
			t.Errorf("findKey(%q) = %d %d, want %d %d", c.in, at, end, c.at, c.end)
		}
	}
	if at, _ := findKey([]byte("\x1a"), nil); at != -1 {
		t.Error("a profile with no suspend keys suspended")
	}
	if !slices.Equal(suspendKeys(nil), z) || !slices.Equal(suspendKeys(&claude), z) {
		t.Error("Ctrl+Z does not suspend an agent with no profile, or Claude Code")
	}
}

// Each delete, submit and clear key counts the same in every form a
// terminal sends it: the stitcher and the send check see one key.
func TestKeysInEveryForm(t *testing.T) {
	forms := []struct {
		name   string
		legacy string
		csi    []string
	}{
		{"Ctrl+W", "\x17", []string{"\x1b[119;5u", "\x1b[27;5;119~", "\x1b[119;69u"}},
		{"Ctrl+U", "\x15", []string{"\x1b[117;5u", "\x1b[27;5;117~", "\x1b[117;133:2u"}},
		{"Ctrl+K", "\x0b", []string{"\x1b[107;5u", "\x1b[27;5;107~"}},
		{"Ctrl+D", "\x04", []string{"\x1b[100;5u", "\x1b[27;5;100~"}},
		{"Ctrl+H", "\x08", []string{"\x1b[104;5u", "\x1b[27;5;104~"}},
		{"Ctrl+_", "\x1f", []string{"\x1b[45;5u", "\x1b[45:95;6u", "\x1b[27;6;95~"}},
		{"Alt+Backspace", "\x1b\x7f", []string{"\x1b[127;3u", "\x1b[27;3;127~"}},
		{"Alt+D", "\x1bd", []string{"\x1b[100;3u", "\x1b[27;3;100~"}},
		{"Backspace", "\x7f", []string{"\x1b[127u"}},
		{"Delete", "\x1b[3~", []string{"\x1b[3;1~", "\x1b[3;65~"}},
		{"Enter", "\r", []string{"\x1b[13u"}},
		{"Ctrl+C", "\x03", []string{"\x1b[99;5u", "\x1b[27;5;99~"}},
		{"Esc Esc", "\x1b\x1b", []string{"\x1b[27u\x1b[27u"}},
	}
	for _, f := range forms {
		chars, ahead := claude.keys.deletes([]byte(f.legacy))
		kinds, _ := claude.keys.kinds([]byte(f.legacy))
		for _, c := range f.csi {
			if ch, ah := claude.keys.deletes([]byte(c)); ch != chars || ah != ahead {
				t.Errorf("%s as %q deletes %d %v, as legacy bytes %d %v", f.name, c, ch, ah, chars, ahead)
			}
			if k, _ := claude.keys.kinds([]byte(c)); !slices.Equal(k, kinds) {
				t.Errorf("%s as %q is %v, as legacy bytes %v", f.name, c, k, kinds)
			}
			// Released, it is nothing at all.
			if rel := releaseOf(c); rel != "" {
				if ch, ah := claude.keys.deletes([]byte(rel)); ch != 0 || ah || typedKeys([]byte(rel)) {
					t.Errorf("%s released (%q) counts as a key", f.name, rel)
				}
			}
		}
	}
	// Ctrl+Enter sends the box; Shift+Enter and Alt+Enter make a new line.
	for in, want := range map[string]keyKind{
		"\x1b[13;5u": keySubmit, "\x1b[27;5;13~": keySubmit,
		"\x1b[13;2u": keyOther, "\x1b[27;2;13~": keyOther, "\x1b\r": keyOther, "\x1b[13;3u": keyOther,
	} {
		if k, _ := claude.keys.kinds([]byte(in)); !slices.Equal(k, []keyKind{want}) {
			t.Errorf("%q is %v, want %v", in, k, want)
		}
	}
}

// releaseOf turns a kitty CSI-u key into its release event, or "".
func releaseOf(csi string) string {
	if !strings.HasSuffix(csi, "u") || strings.Count(csi, "\x1b") != 1 {
		return ""
	}
	body := strings.TrimSuffix(strings.TrimPrefix(csi, "\x1b["), "u")
	code, mods, _ := strings.Cut(body, ";")
	mods, _, _ = strings.Cut(mods, ":")
	if mods == "" {
		mods = "1"
	}
	return "\x1b[" + code + ";" + mods + ":3u"
}

// Esc Esc in kitty form arrives as two reads, one ESC[27u each.
func TestKeyLogJoinsKittyEscEsc(t *testing.T) {
	var l keyLog
	l.push(claude.keys, []byte("\x1b[27u"))
	l.push(claude.keys, []byte("\x1b[27u"))
	events, _ := l.take(func() *screen { return nil })
	if len(events) != 2 || events[1].kind != keyClear {
		t.Fatalf("events %+v", events)
	}
}

// chunkLabel says what one read of real input is to unsent: a suspend, a
// submit or clear key, a delete key and how much it removes, typing, or a
// report that is none of these.
func chunkLabel(ks keyset, b []byte) string {
	if at, _ := findKey(b, ks.suspend); at >= 0 {
		return "suspend"
	}
	kinds, _ := ks.kinds(b)
	switch {
	case slices.Contains(kinds, keySubmit):
		return "submit"
	case slices.Contains(kinds, keyClear):
		return "clear"
	}
	chars, ahead := ks.deletes(b)
	label := ""
	switch {
	case chars >= unlimited:
		label = "many"
	case chars > 0:
		label = "one"
	}
	switch {
	case ahead:
		return label + " ahead"
	case label != "":
		return label
	case typedKeys(b):
		return "text"
	}
	return "report"
}

// labels runs chunkLabel over chunks, one run of text or reports as one.
func labels(ks keyset, chunks [][]byte) []string {
	var out []string
	for _, c := range chunks {
		l := chunkLabel(ks, c)
		if n := len(out); n > 0 && out[n-1] == l && (l == "text" || l == "report") {
			continue
		}
		out = append(out, l)
	}
	return out
}

// The keys Claude Code got under unsent in real terminals
// (testdata/keys/README.md): every delete key, every submit key and Ctrl+Z
// is recognised, in the form each terminal sent it, and the terminal's
// answers and focus reports are not typing. Warp has no log yet (see
// TestSynthesizedKittyKeys).
func TestReplayKeyCaptures(t *testing.T) {
	cases := []struct {
		file string
		want []string
	}{
		// Keys in modifyOtherKeys form: Ctrl+W, Ctrl+U, typing, Ctrl+K,
		// Alt+Backspace, Ctrl+D, Ctrl+U, Shift+Enter (a new line, not a
		// send), Ctrl+Z, fg, Enter, Ctrl+C.
		{"tmux-extkeys/keys", []string{"report", "text", "many", "many", "text", "many ahead", "many", "one ahead",
			"many", "text", "suspend", "text", "submit", "clear"}},
		// Kitty CSI-u, after a focus report and the terminal's answers.
		// Option+Backspace is a plain Backspace under Ghostty's defaults.
		{"ghostty/keys", []string{"report", "text", "many", "many", "text", "many ahead", "one", "one ahead",
			"many", "text", "suspend", "text", "submit", "clear"}},
		// Nothing pushed: legacy bytes, and Shift+Enter a plain Enter.
		{"tmux-extkeys/no-tmux-env", []string{"report", "text", "many", "many", "text", "many ahead", "many",
			"one ahead", "submit", "suspend"}},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "keys", c.file+".rec"))
			if err != nil {
				t.Fatal(err)
			}
			var chunks [][]byte
			eachChunk(t, data, func(_ time.Time, dir byte, chunk []byte) {
				if dir == 'i' {
					chunks = append(chunks, chunk)
				}
			})
			if got := labels(claude.keys, chunks); !slices.Equal(got, c.want) {
				t.Fatalf("keys read as\n%q\nwant\n%q", got, c.want)
			}
		})
	}
}

// Synthesized, not captured: there is no Warp log (testdata/keys/README.md),
// and Warp is on Claude Code's push list, so these are the key forms of the
// kitty keyboard protocol spec (sketch.kitty.dev keyboard-protocol) under
// its fullest flags, 15: every key as an escape code, release and repeat
// events, alternate keys, and the lock bits a terminal adds while Caps Lock
// or Num Lock is on.
var synthesizedKitty = []struct{ in, want string }{
	{"\x1b[97u\x1b[97;1:3u", "text"}, // a, pressed and released
	{"\x1b[98;65u", "text"},          // b with Caps Lock
	{"\x1b[119;5u", "many"},          // Ctrl+W
	{"\x1b[119;5:3u", "report"},      // its release alone
	{"\x1b[119;69u", "many"},         // Ctrl+W with Caps Lock
	{"\x1b[117;133u", "many"},        // Ctrl+U with Num Lock
	{"\x1b[107;5:2u", "many ahead"},  // Ctrl+K held down
	{"\x1b[127;3u", "many"},          // Alt+Backspace
	{"\x1b[127u", "one"},             // Backspace
	{"\x1b[3~\x1b[3;1:3~", "one ahead"},
	{"\x1b[100;5u", "one ahead"},       // Ctrl+D
	{"\x1b[100;3u", "many ahead"},      // Alt+D
	{"\x1b[45;5u", "many ahead"},       // Ctrl+-, undo
	{"\x1b[13;2u", "text"},             // Shift+Enter, a new line
	{"\x1b[13u", "submit"},             // Enter as an escape code
	{"\x1b[13;5u", "submit"},           // Ctrl+Enter
	{"\x1b[27u\x1b[27u", "clear"},      // Esc Esc
	{"\x1b[99;5u", "clear"},            // Ctrl+C
	{"\x1b[122;5u", "suspend"},         // Ctrl+Z
	{"\x1b[122;69:1u", "suspend"},      // Ctrl+Z with Caps Lock
	{"\x1b[122;5:2u", "suspend"},       // Ctrl+Z held down
	{"\x1b[122;5:3u", "report"},        // its release alone
	{"\x1b[<35;10;5M\x1b[I", "report"}, // mouse and focus
}

func TestSynthesizedKittyKeys(t *testing.T) {
	for _, c := range synthesizedKitty {
		if got := chunkLabel(claude.keys, []byte(c.in)); got != c.want {
			t.Errorf("%q reads as %q, want %q", c.in, got, c.want)
		}
	}
}

// Mouse and focus reports and the terminal's answers are never typing and
// never a delete, whatever numbers they carry.
func TestReportsAreNotKeys(t *testing.T) {
	for _, r := range []string{"\x1b[<0;127;23M", "\x1b[<64;8;4m", "\x1b[I", "\x1b[O", "\x1b[M\x7f\x17\x1a",
		"\x1b[?5u", "\x1b[6;17;8t", "\x1bP>|tmux 3.6b\x1b\\"} {
		if typedKeys([]byte(r)) {
			t.Errorf("%q counts as typing", r)
		}
		if chars, ahead := claude.keys.deletes([]byte(r)); chars != 0 || ahead {
			t.Errorf("%q counts as a delete: %d %v", r, chars, ahead)
		}
		if at, _ := findKey([]byte(r), claude.keys.suspend); at >= 0 {
			t.Errorf("%q suspends", r)
		}
		if k, _ := claude.keys.kinds([]byte(r)); len(k) != 0 {
			t.Errorf("%q is %v", r, k)
		}
	}
}

// agentInput has the fake agent keep every byte it reads, and returns a
// reader for them, to call once the run is over. The box cannot tell: the
// shadow screen shows nothing for an escape code or a lone 0x1a.
func agentInput(t *testing.T) func() string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "input")
	t.Setenv("UNSENT_FAKE_INPUT", f)
	return func() string {
		b, _ := os.ReadFile(f)
		return string(b)
	}
}

// ctrlZForms are Ctrl+Z as a lone byte, in kitty form, as modifyOtherKeys
// sends it, and in kitty form with Caps Lock on.
var ctrlZForms = []string{"\x1a", "\x1b[122;5u", "\x1b[27;5;122~", "\x1b[122;69u"}

// Ctrl+Z in any form, in the middle of a longer read, suspends unsent
// once. The agent gets the keys before it, never the key itself, and not
// the rest of that read either: those keys were typed for the shell, and
// the kernel flushes them on its own suspend key too.
func TestWrapCSIuCtrlZInALongerRead(t *testing.T) {
	for _, z := range ctrlZForms {
		t.Run(fmt.Sprintf("%q", z), func(t *testing.T) {
			stops := 0
			old := stopSelf
			stopSelf = func() { stops++ }
			defer func() { stopSelf = old }()
			got := agentInput(t)
			_, st := runWrapped(t, func(type_ func(string)) {
				type_("before")
				type_(" x" + z + "y ")
				type_("after")
				type_("\x04")
			})
			if stops != 1 {
				t.Fatalf("stopped %d times", stops)
			}
			if in := got(); in != "before xafter\x04" {
				t.Fatalf("the agent got %q", in)
			}
			if rs := st.orphans(); len(rs) != 1 || rs[0].Draft != "before xafter" {
				t.Fatalf("orphans %+v", rs)
			}
		})
	}
}

// An Enter typed for the shell in the same read as Ctrl+Z never reaches
// the agent, so the draft is not sent once the shell resumes unsent.
func TestWrapCtrlZDropsTheShellsKeys(t *testing.T) {
	for _, z := range ctrlZForms {
		t.Run(fmt.Sprintf("%q", z), func(t *testing.T) {
			old := stopSelf
			stopSelf = func() {}
			defer func() { stopSelf = old }()
			got := agentInput(t)
			_, st := runWrapped(t, func(type_ func(string)) {
				type_("my draft")
				type_(z + "ls\r")
				type_("\x04")
			})
			if in := got(); in != "my draft\x04" {
				t.Fatalf("the agent got %q", in)
			}
			for _, l := range st.sentLogs() {
				if len(l.messages) != 0 {
					t.Fatalf("sent %+v", l.messages)
				}
			}
			if rs := st.orphans(); len(rs) != 1 || rs[0].Draft != "my draft" {
				t.Fatalf("orphans %+v", rs)
			}
		})
	}
}

// Inside a paste Ctrl+Z is text, in any form: no suspend, and the agent
// gets the paste byte for byte.
func TestWrapCSIuCtrlZInsideAPaste(t *testing.T) {
	stops := 0
	old := stopSelf
	stopSelf = func() { stops++ }
	defer func() { stopSelf = old }()
	got := agentInput(t)
	paste := "\x1b[200~a\x1b[122;5ub\x1a\x1b[27;5;122~c\x1b[201~"
	runWrapped(t, func(type_ func(string)) {
		type_(paste)
		type_("\x04")
	})
	if stops != 0 {
		t.Fatalf("stopped %d times inside a paste", stops)
	}
	if in := got(); in != paste+"\x04" {
		t.Fatalf("the agent got %q", in)
	}
}

// Suspending hands the terminal back as Claude Code's own suspend does:
// keyboard protocol, mouse and focus reports off, and the alternate screen
// left, only when the agent drew on it. Without it the shell got Ctrl+C as
// ESC[27;5;99~ (docs/research/claude.md section 12).
func TestWrapSuspendHandsTheTerminalBack(t *testing.T) {
	for _, main := range []bool{false, true} {
		t.Run(fmt.Sprintf("main screen %v", main), func(t *testing.T) {
			if main {
				t.Setenv("UNSENT_FAKE_MAIN_SCREEN", "1")
			}
			stops := 0
			old := stopSelf
			stopSelf = func() { stops++ }
			defer func() { stopSelf = old }()
			_, _, shown, _ := runWrappedOut(t, "claude", []string{"claude"}, func(type_ func(string)) {
				type_("x")
				type_("\x1b[122;5u")
				type_("\x04")
			})
			want := string(claude.suspended)
			if main {
				want = strings.Replace(want, string(leaveAlt), "", 1)
			}
			if stops != 1 || !strings.Contains(shown, want) {
				t.Fatalf("stopped %d times; the terminal got %q, want %q in it", stops, shown, want)
			}
			if main && strings.Contains(shown, string(leaveAlt)) {
				t.Fatal("left an alternate screen the agent never used")
			}
		})
	}
}
