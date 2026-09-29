package main

import (
	"os"
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
	// last view can have removed (unlimited after a key that deletes a
	// word or more, such as Ctrl+W), and deletedAhead whether one removes
	// text after the cursor (such as Delete). The screen alone cannot tell
	// text deleted at the edge of the box from text moved out of sight.
	deleted      int
	deletedAhead bool
	// under is the screen row just below the box, where unsent may draw a
	// line of its own, or -1 when there is none.
	under int
}

// An extractor finds the agent's input box on the screen. ok is false when
// the box is not visible (a menu, a permission prompt, a full-screen editor).
type extractor func(s *screen) (v view, ok bool)

// A profile is everything unsent knows about one agent: the commands that
// start it, how to read its box, and which of its keys the save loop must
// know about. Adding an agent means adding a profile, in a file of its own.
type profile struct {
	// name is the agent's name in records and filters.
	name string
	// names are the command base names that start the agent.
	names []string
	read  extractor
	// unwrap is how the agent wraps the draft into rows, and scroll how
	// its box scrolls once it stops growing (stitch.go).
	unwrap unwrapRule
	scroll scrollModel
	// pastes are what the agent shows in place of a long paste, and how
	// each placeholder is paired with the paste behind it (paste.go).
	pastes []pasteRule
	// truncated matches what the agent shows in place of the middle of a
	// draft too long to show whole; its groups are the placeholder's number
	// and the middle's line count. Nil when the agent never does that.
	truncated *regexp.Regexp
	keys      keyset
	// suspended is what the agent writes to the terminal as it suspends
	// itself: its keyboard protocol, mouse reports and alternate screen
	// turned off. unsent writes it when it suspends the agent instead, so
	// the shell gets its keys as it expects them; the agent must turn them
	// on again as it repaints after the resume. Nil writes nothing.
	suspended []byte
	// verified is the agent version the reader was last checked against,
	// and version the arguments that make the agent print its own: when
	// the reader never finds the box, the user learns both.
	verified string
	version  []string
	// session reads the agent's own session id for the process unsent
	// started, which records and sent logs carry as agent_session; nil when
	// the profile cannot read one.
	session *sessionSource
	// restore is what putting a draft back in the box needs (restore.go);
	// nil turns restore-in-box off for the agent.
	restore *restoreCaps
}

// profiles are the agents unsent can read.
var profiles = []*profile{&claude}

// profileFor returns the profile of the agent a command starts, or nil.
func profileFor(command string) *profile {
	base := filepath.Base(command)
	for _, p := range profiles {
		if p.name == base || slices.Contains(p.names, base) {
			return p
		}
	}
	return nil
}

// agentFor names the agent a command starts: as (from --as), else the
// command's base name when a profile answers to it, else UNSENT_AGENT, else
// the base name. --as and UNSENT_AGENT are for commands whose name does not
// say it: npx, node cli.js, a renamed binary. A command that names a known
// agent keeps that name, so an exported UNSENT_AGENT cannot tag it as
// another agent.
func agentFor(as, command string) string {
	if as == "" {
		if p := profileFor(command); p != nil {
			return p.name
		}
	}
	for _, name := range []string{as, os.Getenv("UNSENT_AGENT"), filepath.Base(command)} {
		if name != "" {
			return agentName(name)
		}
	}
	return ""
}

// agentName is the name records and filters use: a name a profile answers
// to becomes the profile's name.
func agentName(name string) string {
	if p := profileFor(name); p != nil {
		return p.name
	}
	return name
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
