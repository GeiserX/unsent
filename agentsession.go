package main

import (
	"context"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
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
