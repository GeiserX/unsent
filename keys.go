package main

import (
	"slices"
	"unicode/utf8"
)

// A terminal sends the same key in several forms, depending on what the
// agent asked for: legacy bytes (Ctrl+W is 0x17, Alt+D is ESC d), kitty
// CSI-u (ESC[119;5u), modifyOtherKeys (ESC[27;5;119~), and functional keys
// with modifiers (ESC[3;5~). Claude Code pushes kitty flags 5 and
// modifyOtherKeys level 2 in tmux and Ghostty, so there every Ctrl key
// arrives in one of the CSI forms (docs/research/claude.md section 12).
// unsent decodes each form into one key before it matches a profile's
// keys. The decoder only reads: every byte still reaches the agent as it
// came, except a suspend key unsent takes for itself.

// A key is one key press. code is the key's Unicode code point, lower case
// for a letter pressed with Ctrl or in a CSI form, or one of the codes
// below for a key that has none; mods are the modifiers held, Caps Lock
// and Num Lock left out.
type key struct {
	code rune
	mods keyMods
}

// keyMods are the kitty protocol's modifier bits, which modifyOtherKeys and
// the functional keys use too: the number sent is 1 plus these.
type keyMods uint8

const (
	modShift keyMods = 1 << iota
	modAlt
	modCtrl
	modSuper
	modHyper
	modMeta
)

// lockMods are Caps Lock (64) and Num Lock (128): the kitty protocol
// reports them with every key, and they change no key's meaning here.
const lockMods = 64 | 128

const (
	keyTab       = 9
	keyEnter     = 13
	keyEsc       = 27
	keyBackspace = 127
	// Keys with no code point, past the end of Unicode: CSI <n> ~ is
	// tildeKey+n (Delete is CSI 3 ~), and CSI or SS3 with a letter is
	// letterKey+letter (Up is CSI A).
	tildeKey  = 0x110000
	letterKey = 0x110100
	keyDelete = tildeKey + 3
)

func plain(code rune) key { return key{code: code} }
func ctrl(code rune) key  { return key{code, modCtrl} }
func alt(code rune) key   { return key{code, modAlt} }

// A token is what decodeKey found at the start of its input.
type token int

const (
	tokKey     token = iota // a key pressed, or held down (a repeat)
	tokRelease              // a key let go (kitty event type 3): not typing
	tokReport               // a mouse or focus report, or the terminal answering the agent
)

// decodeKey decodes the key at the start of b (never empty), and returns
// it, its length and what it is. A sequence cut short at the end of b is a
// key of code 0, to be safe, unless its start says it is a terminal's
// answer.
func decodeKey(b []byte) (key, int, token) {
	if b[0] != 0x1b {
		k, n := legacyKey(b)
		return k, n, tokKey
	}
	if len(b) == 1 || b[1] == 0x1b {
		return plain(keyEsc), 1, tokKey
	}
	switch b[1] {
	case '[':
		return decodeCSI(b)
	case 'O': // SS3: F1 to F4, and arrows and keypad keys in application mode
		if len(b) > 2 && b[2] >= 0x40 && b[2] <= 0x7e {
			return plain(letterKey + rune(b[2])), 3, tokKey
		}
	case ']', 'P', '_', '^', 'X': // OSC, DCS, APC, PM, SOS: only terminals send these
		return key{}, stringLen(b), tokReport
	}
	// Alt and a key: ESC, then the key as it comes without Alt.
	k, n := legacyKey(b[1:])
	k.mods |= modAlt
	return k, 1 + n, tokKey
}

// legacyKey decodes one key sent as plain bytes: a C0 control byte is Ctrl
// and a key (0x08 is Ctrl+H, whatever the agent makes of it), 0x7f is
// Backspace, anything else is the character.
func legacyKey(b []byte) (key, int) {
	switch c := b[0]; {
	case c == keyTab || c == keyEnter || c == keyEsc || c == keyBackspace:
		return plain(rune(c)), 1
	case c == 0:
		return ctrl(' '), 1
	case c < 0x1b:
		return ctrl(rune('a' + c - 1)), 1
	case c < 0x20:
		return ctrl(rune(`\]^_`[c-0x1c])), 1
	case c < 0x80:
		return plain(rune(c)), 1
	}
	r, n := utf8.DecodeRune(b)
	return plain(r), n
}

// stringLen returns the length of the string sequence (OSC, DCS and co.)
// that starts b: up to BEL or ESC \, or all of b when it is cut short.
func stringLen(b []byte) int {
	for i := 2; i < len(b); i++ {
		switch b[i] {
		case 0x07:
			return i + 1
		case 0x1b:
			if i+1 < len(b) && b[i+1] == '\\' {
				return i + 2
			}
			return i // cancelled by the next sequence
		}
	}
	return len(b)
}

// decodeCSI decodes a CSI sequence (ESC [ ...) typed or sent by the
// terminal: kitty CSI-u keys, modifyOtherKeys, functional keys, and the
// reports that are not keys.
func decodeCSI(b []byte) (key, int, token) {
	j := 2
	for j < len(b) && b[j] >= 0x20 && b[j] <= 0x3f {
		j++
	}
	private := j > 2 && (b[2] == '?' || b[2] == '>' || b[2] == '=')
	if j == len(b) {
		if private {
			return key{}, j, tokReport
		}
		return key{}, j, tokKey
	}
	final := b[j]
	if final < 0x40 || final > 0x7e {
		// Not a whole CSI: an unknown key, and the byte that broke it
		// starts afresh (it may be the ESC of the next sequence).
		return key{}, j, tokKey
	}
	params, n := b[2:j], j+1
	switch {
	case private: // DA, kitty flags (?5u), DECRPM (?2026;2$y), XTVERSION's kin
		return key{}, n, tokReport
	case len(params) > 0 && params[0] == '<': // SGR mouse, ESC[<b;x;yM and ...m
		return key{}, n, tokReport
	case len(params) == 0 && (final == 'I' || final == 'O'): // focus in, out
		return key{}, n, tokReport
	case final == 'M': // X10 mouse (three more bytes), or urxvt's with numbers
		if len(params) == 0 {
			return key{}, min(n+3, len(b)), tokReport
		}
		return key{}, n, tokReport
	case final == 't': // window and cell size reports (ESC[6;17;8t)
		return key{}, n, tokReport
	case final == 'y' && len(params) > 0 && params[len(params)-1] == '$': // mode reports
		return key{}, n, tokReport
	}
	first, _ := csiParam(params, 0)
	mods, event := csiParam(params, 1)
	var k key
	switch {
	case final == 'u': // kitty: CSI code[:shifted[:base]] [; mods[:event] [; text]] u
		k.code = lowerASCII(rune(first))
	case final == '~' && first == 27: // modifyOtherKeys: CSI 27 ; mods ; code ~
		code, _ := csiParam(params, 2)
		k.code = lowerASCII(rune(code))
	case final == '~': // Delete is CSI 3 ~, Ctrl+Delete CSI 3;5 ~
		k.code = tildeKey + rune(first)
	case final == 'Z': // Shift+Tab
		k.code, mods = keyTab, 2
	default: // arrows, Home, End, F1 to F4: CSI 1;5 A is Ctrl+Up
		k.code = letterKey + rune(final)
	}
	if mods > 0 {
		k.mods = keyMods((mods - 1) &^ lockMods & 0x3f)
	}
	if event == 3 {
		return k, n, tokRelease
	}
	return k, n, tokKey
}

// csiParam returns the i-th ;-separated field of a CSI's parameters and
// the field's second :-separated part: the number and the kitty event type
// in "5:3". A missing or empty part is 0. Parts past the second (kitty's
// base layout key) are skipped.
func csiParam(params []byte, i int) (value, sub int) {
	for ; i > 0; i-- {
		at := slices.Index(params, ';')
		if at < 0 {
			return 0, 0
		}
		params = params[at+1:]
	}
	if at := slices.Index(params, ';'); at >= 0 {
		params = params[:at]
	}
	part := 0
	for _, c := range params {
		switch {
		case c == ':':
			part++
		case c < '0' || c > '9':
			return value, sub
		case part == 0 && value < 1<<21:
			value = 10*value + int(c-'0')
		case part == 1 && sub < 1<<21:
			sub = 10*sub + int(c-'0')
		}
	}
	return value, sub
}

// lowerASCII lower-cases an ASCII letter: modifyOtherKeys sends Ctrl+Z as
// 90 ('Z') with Caps Lock on.
func lowerASCII(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 'a' - 'A'
	}
	return r
}

// keyAt is a key press found in a chunk of input, with the bytes it spans.
type keyAt struct {
	key
	at, end int
}

// keysIn decodes b and returns its key presses in order. Releases, mouse
// and focus reports and the terminal's answers are left out: they are not
// typing, and never a delete.
func keysIn(b []byte) []keyAt {
	var keys []keyAt
	for i := 0; i < len(b); {
		k, n, t := decodeKey(b[i:])
		if t == tokKey {
			keys = append(keys, keyAt{k, i, i + n})
		}
		i += n
	}
	return keys
}

// typedKeys reports whether b holds a key the user typed, not only focus
// and mouse reports, releases, or the terminal's answers to the agent's
// queries: a string (OSC, DCS, APC, PM, SOS), a CSI with a private marker
// (?, >, =), a window report (CSI ... t, the answer to the cell size query
// Claude Code sends at start) or a mode report (CSI ... $ y).
func typedKeys(b []byte) bool {
	for i := 0; i < len(b); {
		_, n, t := decodeKey(b[i:])
		if t == tokKey {
			return true
		}
		i += n
	}
	return false
}

// suspendKeys are the keys unsent suspends on for an agent: its profile's,
// and Ctrl+Z for an agent with no profile. The agent runs in a terminal
// session of its own, where the kernel drops its own suspend.
func suspendKeys(p *profile) []key {
	if p == nil {
		return []key{ctrl('z')}
	}
	return p.keys.suspend
}

// findKey returns where the first of keys pressed in b starts and ends, or
// -1, -1.
func findKey(b []byte, keys []key) (at, end int) {
	if len(keys) == 0 {
		return -1, -1
	}
	for i := 0; i < len(b); {
		k, n, t := decodeKey(b[i:])
		if t == tokKey && slices.Contains(keys, k) {
			return i, i + n
		}
		i += n
	}
	return -1, -1
}
