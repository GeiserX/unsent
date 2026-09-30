package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A sessionSource reads the agent's own session id (a record's
// agent_session) from what the agent keeps about process pid, an agent
// started no earlier than since. found is false when the agent keeps
// nothing for pid; id is "" when what it keeps names no session, such as a
// file cut short or of another shape. pids lists the processes the agent
// keeps something for, so a descendant of the process unsent started can
// be found when the agent is not that process itself.
type sessionSource struct {
	read func(pid int, since time.Time) (id string, found bool)
	pids func() []int
}

// sessionUnsure is what a sessionSource reads when the agent keeps more
// than one session for the process and nothing says which is current: the
// process is in no known session, rather than in the last one read.
const sessionUnsure = "?"

// sessionIDRE is what an agent session id may look like. The id comes from
// the agent's files and names a sent log file, so nothing that could reach
// outside the folder gets through.
var sessionIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// sessionTracker follows the session the agent unsent started is in. One
// process can change session while it runs (Claude Code's resume picker,
// /resume, /clear), so it is asked again at every save. It keeps the last
// id it read when the file of the process unsent started goes, since the
// agent removes it on its way out. A descendant's file going while that
// process has none is the end of the descendant's session: a launcher that
// does not exec the agent can start another one, in another conversation,
// so the tracker forgets the id and looks for the next one at once.
type sessionTracker struct {
	src   *sessionSource
	root  int       // the pid unsent started
	since time.Time // when it started
	pid   int       // the process whose file names the session: root, or a descendant
	id    string
	// next is when to look for a descendant again, and wait how long after
	// that: listing processes runs ps, so a launcher that never leads to a
	// file costs one run a minute at most.
	next time.Time
	wait time.Duration
	now  func() time.Time // time.Now; tests move time by hand
}

func newSessionTracker(src *sessionSource, root int, since time.Time) *sessionTracker {
	if src == nil || root <= 0 {
		return nil
	}
	return &sessionTracker{src: src, root: root, since: since, pid: root, wait: 2 * time.Second, now: time.Now}
}

// current reads the session id afresh and returns it, or the last one read
// when this read finds none, or "" once the session it named has ended.
func (t *sessionTracker) current() string {
	if t == nil {
		return ""
	}
	id, found := t.src.read(t.pid, t.since)
	if !found && t.pid != t.root {
		t.pid = t.root
		id, found = t.src.read(t.root, t.since)
		if !found {
			t.id, t.next, t.wait = "", t.now(), 2*time.Second
		}
	}
	if !found && !t.now().Before(t.next) {
		// No file for the pid unsent started: a launcher that does not exec
		// the agent runs it as a descendant.
		t.next, t.wait = t.now().Add(t.wait), min(2*t.wait, time.Minute)
		if pid := t.descendant(); pid > 0 {
			t.pid = pid
			id, found = t.src.read(pid, t.since)
		}
	}
	switch {
	case found && id == sessionUnsure:
		// The agent names several sessions for the process and none as
		// current: text typed from now on belongs to none of them.
		t.id = ""
	case found && sessionIDRE.MatchString(id):
		t.id = id
	}
	return t.id
}

// descendant is the process nearest to root, below it, that the agent keeps
// a session for, or 0.
func (t *sessionTracker) descendant() int {
	var have []int
	for _, p := range t.src.pids() {
		if p != t.root {
			have = append(have, p)
		}
	}
	if len(have) == 0 {
		return 0
	}
	parents := processParents()
	best, bestDepth := 0, 0
	slices.Sort(have)
	for _, p := range have {
		if d := depthBelow(parents, p, t.root); d > 0 && (best == 0 || d < bestDepth) {
			best, bestDepth = p, d
		}
	}
	return best
}

// depthBelow is how many generations pid is below root, or 0 when it is not
// a descendant.
func depthBelow(parents map[int]int, pid, root int) int {
	for d := 1; d <= len(parents); d++ {
		pp, ok := parents[pid]
		if !ok || pp <= 1 {
			return 0
		}
		if pp == root {
			return d
		}
		pid = pp
	}
	return 0
}

// processParents maps each running process to its parent, from ps, which
// macOS and Linux both have; empty when ps cannot run.
var processParents = func() map[int]int {
	// A stalled ps must not hold the save loop, which is what saves the
	// draft when the window closes.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=", "-o", "ppid=")
	cmd.WaitDelay = time.Second
	out, _ := cmd.Output()
	parents := map[int]int{}
	for line := range strings.SplitSeq(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 == nil && err2 == nil {
			parents[pid] = ppid
		}
	}
	return parents
}

// heldFiles keeps the last answer to which of an agent's files a process
// holds open, for the profiles whose agent names the session it is in by
// the file it holds (Codex's thread lock, agy's conversation database).
// Asking runs lsof on macOS, so it is asked again only when the folder
// changed (a file taken or let go) or recheck has passed.
type heldFiles struct {
	mu  sync.Mutex
	pid int
	mod time.Time
	at  time.Time
	ids []string
}

// of lists the ids, by idOf of each file's name, of the files in dir that
// process pid holds open.
func (h *heldFiles) of(pid int, dir string, recheck time.Duration, idOf func(name string) string) []string {
	fi, err := os.Stat(dir)
	if err != nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if pid == h.pid && fi.ModTime().Equal(h.mod) && time.Since(h.at) < recheck {
		return h.ids
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil
	}
	var ids []string
	for _, p := range openFiles(pid) {
		if filepath.Dir(p) != real {
			continue
		}
		if id := idOf(filepath.Base(p)); id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	h.pid, h.mod, h.at, h.ids = pid, fi.ModTime(), time.Now(), ids
	return ids
}

// procFS is where Linux keeps a folder per running process. A test points
// it somewhere of its own to read the way the other system reads: at a
// folder that is not there, so a Linux box takes macOS's path through
// lsof, and at a folder of made-up process files, so a Mac takes Linux's.
var procFS = "/proc"

// openFiles lists the paths of the files process pid holds open: from
// /proc where there is one (Linux), else from lsof (macOS). Empty when
// neither can tell, and never slower than a second.
var openFiles = func(pid int) []string {
	fds := filepath.Join(procFS, strconv.Itoa(pid), "fd")
	if entries, err := os.ReadDir(fds); err == nil {
		var out []string
		for _, e := range entries {
			if p, err := os.Readlink(filepath.Join(fds, e.Name())); err == nil {
				out = append(out, p)
			}
		}
		return out
	}
	if _, err := os.Stat(filepath.Join(procFS, "self", "fd")); err == nil {
		return nil // Linux, and the process is gone or not ours
	}
	out, _ := lsof("-n", "-P", "-w", "-Fn", "-p", strconv.Itoa(pid))
	var paths []string
	for line := range strings.SplitSeq(out, "\n") {
		if p, ok := strings.CutPrefix(line, "n"); ok && strings.HasPrefix(p, "/") {
			paths = append(paths, p)
		}
	}
	return paths
}

// holders lists the processes that hold open a file in dir whose name
// idOf reads an id from. It asks about the folder, never about each file
// in it: agy keeps one database per conversation for ever, so that list
// has no bound, and lsof given every path answers "nobody holds anything"
// long before the folder is big by any human measure. Measured on a Mac
// mini (macOS 26.6, lsof 4.91), one call per path list: 1,000 paths
// 0.14 s and right, 3,000 past the one-second timeout, 12,000 past the
// kernel's argument limit, each of the last two an error this code used
// to drop on the floor. The same folder of 5,000 through +d takes 0.2 s.
var holders = func(dir string, idOf func(name string) string) []int {
	var pids []int
	if procs, err := os.ReadDir(procFS); err == nil {
		for _, p := range procs {
			pid, err := strconv.Atoi(p.Name())
			if err != nil {
				continue
			}
			for _, f := range openFiles(pid) {
				if filepath.Dir(f) == dir && idOf(filepath.Base(f)) != "" {
					pids = append(pids, pid)
					break
				}
			}
		}
		return pids
	}
	// +d lists what is open in dir itself, one process set at a time:
	// "p<pid>", then "f<fd>" and "n<path>" for each file it holds.
	out, _ := lsof("-n", "-P", "-w", "+d", dir, "-Fpn")
	pid := 0
	for line := range strings.SplitSeq(out, "\n") {
		switch {
		case strings.HasPrefix(line, "p"):
			pid, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "n") && pid > 0 && idOf(filepath.Base(line[1:])) != "":
			if !slices.Contains(pids, pid) {
				pids = append(pids, pid)
			}
		}
	}
	return pids
}

// lsof runs lsof with args and returns what it printed. A stalled lsof
// must not hold the save loop.
func lsof(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, lsofPath(), args...)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	return string(out), err
}

// lsofPath is lsof on PATH, or where macOS keeps it when PATH leaves
// /usr/sbin out.
func lsofPath() string {
	if p, err := exec.LookPath("lsof"); err == nil {
		return p
	}
	return "/usr/sbin/lsof"
}
