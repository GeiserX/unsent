package main

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// view is what an extractor sees of the input box on one screen.
type view struct {
	// rows are the box's visible rows, prefix stripped, trailing spaces trimmed.
	rows []string
	// cursor is the row index of the cursor inside rows, or -1, and
	// cursorEnd whether it sits after the last character of its row.
	cursor    int
	cursorEnd bool
	// width is the column at which the agent wraps a row.
	width int
	// capped is true when the box is at its maximum height, so it may be
	// scrolled and rows above or below may be out of sight.
	capped bool
	// empty is true when the box holds no draft (only a dim hint, or nothing).
	empty bool
	// deleted is how many characters the delete keys pressed since the
	// last view can have removed (unlimited after Ctrl+W, Ctrl+U, Ctrl+K
	// or undo), and deletedAhead whether one removes text after the cursor
	// (Delete, Ctrl+K, undo). The screen alone cannot tell text deleted at
	// the edge of the box from text moved out of sight.
	deleted      int
	deletedAhead bool
}

// An extractor finds the agent's input box on the screen. ok is false when
// the box is not visible (a menu, a permission prompt, a full-screen editor).
type extractor func(s *screen) (v view, ok bool)

// A profile is everything unsent knows about one agent: the commands that
// start it, how to read its box, and which of its keys the save loop must
// know about. Adding an agent means adding a profile, in a file of its own.
type profile struct {
	// names are the command base names that start the agent.
	names []string
	read  extractor
	// placeholder matches what the agent shows in place of a long paste;
	// its first group, when it matches, is the paste's line count.
	placeholder *regexp.Regexp
	keys        keyset
}

// profiles are the agents unsent can read.
var profiles = []*profile{&claude}

// profileFor returns the profile of the agent a command starts, or nil.
func profileFor(command string) *profile {
	for _, p := range profiles {
		if slices.Contains(p.names, filepath.Base(command)) {
			return p
		}
	}
	return nil
}

// hasMarker reports whether a row starts with marker and a space, or is
// only the marker.
func hasMarker(row, marker string) bool {
	rest, ok := strings.CutPrefix(row, marker)
	return ok && (rest == "" || strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\u00a0"))
}

// isRule reports whether a row is a horizontal line across most of the window.
func isRule(t string, cols int) bool {
	n := 0
	for _, r := range t {
		if r != '─' {
			return false
		}
		n++
	}
	return n >= cols/2
}
