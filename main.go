// Command unsent is AutoRecover for AI agent prompts: it keeps the text you
// are typing into Claude Code saved on disk, so a closed window, a crash or
// a reboot never takes an unsent message with it.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

var version = "dev"

const usage = `unsent: AutoRecover for AI agent prompts.

Usage:
  unsent <agent> [args...]   run the agent with its input box saved as you type
  unsent list [--all]        list drafts left behind (--all adds cleared ones
                             and earlier versions)
  unsent show [N]            print draft N (default: this folder's newest)
  unsent restore [N]         copy draft N to the clipboard and mark it restored
  unsent version

Put "alias claude='unsent claude'" in your shell profile to never think
about it again. Use "unsent -- list" to run a program called list.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "version", "--version":
		fmt.Fprintln(stdout, "unsent", buildVersion())
		return 0
	case "list", "ls":
		return cmdList(args[1:], stdout, stderr)
	case "show":
		return cmdShow(args[1:], stdout, stderr, false)
	case "restore":
		return cmdShow(args[1:], stdout, stderr, true)
	case "--":
		args = args[1:]
		if len(args) == 0 {
			fmt.Fprint(stderr, usage)
			return 2
		}
	}
	return wrap(args, os.Stdin, os.Stdout)
}

// buildVersion is the release version stamped in by the release build, or
// the module version when installed with go install.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

// candidates are the drafts the commands work on, newest first: drafts left
// behind, then (with all) drafts that were cleared or sent.
func candidates(st *store, all bool) []*record {
	out := st.orphans()
	if all {
		for _, r := range st.load(true) {
			if !contains(out, r) && !st.alive(r) && !isDraftFile(st, r) {
				out = append(out, r)
			}
		}
	}
	return out
}

func isDraftFile(st *store, r *record) bool {
	_, err := os.Stat(st.draftPath(r.ID))
	return err == nil
}

func contains(rs []*record, r *record) bool {
	for _, x := range rs {
		if x.ID == r.ID {
			return true
		}
	}
	return false
}

func cmdList(args []string, stdout, stderr io.Writer) int {
	st, err := openStore()
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	all := len(args) > 0 && (args[0] == "--all" || args[0] == "-a")
	rs := candidates(st, all)
	if len(rs) == 0 {
		fmt.Fprintln(stdout, "No drafts to recover.")
		return 0
	}
	for i, r := range rs {
		mark := ""
		if r.Version {
			mark = "(earlier version) "
		}
		fmt.Fprintf(stdout, "%3d  %s  %-24s  %s%s\n", i+1, when(r.Updated), shortPath(r.Cwd, 24), mark, preview(r.Draft, 60-len(mark)))
	}
	return 0
}

func cmdShow(args []string, stdout, stderr io.Writer, restore bool) int {
	st, err := openStore()
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	rs := candidates(st, true)
	if len(rs) == 0 {
		fmt.Fprintln(stderr, "unsent: no drafts to recover")
		return 1
	}
	n := 1
	if len(args) > 0 {
		n, err = strconv.Atoi(args[0])
		if err != nil || n < 1 || n > len(rs) {
			fmt.Fprintf(stderr, "unsent: no draft %q; see unsent list --all\n", args[0])
			return 2
		}
	}
	if len(args) == 0 {
		// The recovery notice promised this folder's draft: prefer it.
		cwd, _ := os.Getwd()
		for i, r := range rs {
			if isDraftFile(st, r) && samePath(r.Cwd, cwd) {
				n = i + 1
				break
			}
		}
	}
	r := rs[n-1]
	if !restore {
		fmt.Fprintln(stdout, r.Draft)
		for _, p := range r.Pastes {
			if !strings.Contains(r.Draft, p) {
				fmt.Fprintf(stdout, "\n--- a paste that could not be placed in the draft ---\n%s\n", p)
			}
		}
		return 0
	}
	if err := copyToClipboard(r.Draft); err != nil {
		// No clipboard (SSH, a bare Linux console): print it instead.
		fmt.Fprintln(stdout, r.Draft)
		fmt.Fprintf(stderr, "unsent: no clipboard (%v); printed the draft instead\n", err)
		return 0
	}
	// Leave the draft in place unless its copy in history is safely written.
	if isDraftFile(st, r) && st.archive(r) == nil {
		st.remove(r)
	}
	fmt.Fprintf(stderr, "Copied %s from %s to the clipboard. Paste it into the agent.\n",
		lines(r.Draft), when(r.Updated))
	if n := unplaced(r); n > 0 {
		fmt.Fprintf(stderr, "%d paste(s) could not be put back in place; `unsent show %s` prints them.\n", n, n0(args))
	}
	return 0
}

// unplaced counts a record's pastes that are not in its draft.
func unplaced(r *record) int {
	n := 0
	for _, p := range r.Pastes {
		if !strings.Contains(r.Draft, p) {
			n++
		}
	}
	return n
}

// n0 is the draft number the user asked for, for messages.
func n0(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

// noticeOrphans tells the user, before the agent starts, that a draft from
// this folder is waiting, the way a word processor offers recovered files.
func noticeOrphans(st *store, cwd string) {
	var here []*record
	for _, r := range st.orphans() {
		if samePath(r.Cwd, cwd) {
			here = append(here, r)
		}
	}
	if len(here) == 0 {
		return
	}
	r := here[0]
	more := ""
	if len(here) > 1 {
		more = fmt.Sprintf(" (and %d more)", len(here)-1)
	}
	fmt.Fprintf(os.Stderr, "unsent: recovered a draft from %s, %s%s. Run `unsent restore` to copy it.\n",
		when(r.Updated), lines(r.Draft), more)
}

// samePath compares two folders after resolving symlinks, so /tmp and
// /private/tmp on macOS count as one.
func samePath(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return a == b
}

func copyToClipboard(text string) error {
	var tries [][]string
	switch runtime.GOOS {
	case "darwin":
		tries = [][]string{{"pbcopy"}}
	case "windows":
		tries = [][]string{{"clip.exe"}}
	default:
		tries = [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}, {"clip.exe"}}
	}
	for _, t := range tries {
		if _, err := exec.LookPath(t[0]); err != nil {
			continue
		}
		cmd := exec.Command(t[0], t[1:]...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	return fmt.Errorf("none of pbcopy, wl-copy, xclip, xsel, clip.exe found")
}

func when(t time.Time) string {
	now := time.Now()
	switch {
	case now.Sub(t) < 24*time.Hour && now.Day() == t.Day():
		return t.Format("15:04") + " today"
	case now.Sub(t) < 48*time.Hour && now.AddDate(0, 0, -1).Day() == t.Day():
		return t.Format("15:04") + " yesterday"
	default:
		return t.Format("2006-01-02 15:04")
	}
}

func lines(s string) string {
	n := strings.Count(s, "\n") + 1
	if n == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", n)
}

func preview(s string, width int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > width {
		return string(r[:width-1]) + "…"
	}
	return s
}

func shortPath(p string, width int) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			p = filepath.Join("~", rel)
		}
	}
	if r := []rune(p); len(r) > width {
		return "…" + string(r[len(r)-width+1:])
	}
	return p
}
