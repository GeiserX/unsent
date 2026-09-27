package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Importing the shell logs. Every unsent command that opens the store first
// reads the logs the zsh hooks write (see zshHooks for the record format)
// and turns their records into drafts, history and sent logs, labelled with
// the shell's name and the folder each prompt ran in:
//
//   - a line cleared at its prompt (an i with no s before it), or replaced
//     wholesale (shellEdit says it is not an edit), goes to history, as an
//     agent draft does;
//   - a line run with Enter (an s with no c after it) follows the on-send
//     setting, delete by default for shells;
//   - the last line of a shell that is gone becomes an orphan draft;
//   - a forget mark (f) drops the line at the prompt, the one that gained
//     the leading space or came to match; an f marked new says the ignored
//     text replaced that line instead, so the line goes to history.
//
// The importer deletes a log only when nothing writes to it any more: a
// log the hooks moved aside (.done), or the log of a shell on this host
// whose pid is gone. A log named for another host is left to that host's
// importer unless it has not changed for shellForeignAge. A live shell's
// log is read to its last complete record and left in place, and the
// importer keeps how far it got, and the line in progress, in a state file
// per shell (shell/zsh-<host>-<pid>.import), so no record is imported twice.
// A shell's logs are one session from its first prompt to its end, across
// logs the hooks moved aside past 256 KB. They are read by stamp, except
// that the log the state names comes first: the importer removes a log it
// has read before it goes on to the next, so a log with an older stamp is
// one opened after the clock stepped back, and is read, never deleted.
//
// Every file the import writes is named after the log and the offset of the
// record that caused it, so an import that stops half way and runs again
// writes the same files, not a second copy. A sent log is the exception: a
// message appended just before such a stop is appended again.

const (
	// shellHistoryPrefix starts the name of a shell line in history/, so it
	// counts against shellHistoryLimit and not against the agents' cap.
	shellHistoryPrefix = "sh-"
	// shellLogFormat is the version the v record of a log this build reads
	// must carry.
	shellLogFormat = "1"
	// shellStateFormat is the layout of a state file.
	shellStateFormat = 1
	shellForeignAge  = 30 * 24 * time.Hour
)

// isShell reports whether an agent name is a shell's, whose on-send default
// is delete and whose lines have their own history cap.
func isShell(agent string) bool {
	return agent == "zsh"
}

// shellLog is one log file, parsed from its name
// zsh-<host>-<pid>-<stamp>.log, or .done once moved aside. The stamp is
// $EPOCHREALTIME with the dot taken out, a number too long for an int64.
type shellLog struct {
	path, stem, host, stamp string
	pid                     int
	done                    bool
}

// before reports whether stamp a is earlier than stamp b.
func before(a, b string) bool {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func parseShellLog(path string) (shellLog, bool) {
	l := shellLog{path: path}
	base := filepath.Base(path)
	stem, ok := strings.CutSuffix(base, ".log")
	if !ok {
		stem, ok = strings.CutSuffix(base, ".done")
		l.done = true
	}
	rest, isZsh := strings.CutPrefix(stem, "zsh-")
	if !ok || !isZsh {
		return l, false
	}
	l.stem = stem
	i := strings.LastIndexByte(rest, '-')
	if i < 0 {
		return l, false
	}
	stamp := rest[i+1:]
	if stamp == "" || strings.Trim(stamp, "0123456789") != "" {
		return l, false
	}
	j := strings.LastIndexByte(rest[:i], '-')
	if j <= 0 {
		return l, false
	}
	pid, err := strconv.Atoi(rest[j+1 : i])
	if err != nil || pid <= 0 {
		return l, false
	}
	l.host, l.pid, l.stamp = rest[:j], pid, stamp
	return l, true
}

// shellHost is this machine's name as the hooks write it into a log's name:
// $HOST with anything but letters, digits, dots and dashes turned into _.
func shellHost() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return strings.Map(func(r rune) rune {
		if r == '.' || r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			return r
		}
		return '_'
	}, h)
}

// pidAlive reports whether a process with that pid runs on this machine. A
// process of another user counts as alive: the doubt keeps the log.
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// shellState is how far the import got with one shell, and the line it had
// at its prompt then. Never rename or retype a field without bumping
// shellStateFormat.
type shellState struct {
	Format int `json:"format"`
	// Stem names the log the import read last, and Offset is where its next
	// record starts.
	Stem   string `json:"stem"`
	Offset int    `json:"offset"`
	// Line is the shell's session: its id, pid and first prompt, and the
	// line at the prompt now, with that prompt's folder.
	Line record `json:"line"`
	// Ran is set by an s record and cleared by a c after it: Enter ran the
	// line, unless it only opened a continuation prompt.
	Ran bool `json:"ran,omitempty"`
}

// shellGroup is every log and the state file of one shell, by host and pid.
type shellGroup struct {
	host  string
	pid   int
	logs  []shellLog
	state string
}

// importShells imports every shell log in the state folder. It is best
// effort: a log it cannot read stays for the next command.
func (s *store) importShells() {
	dir := filepath.Join(s.dir, "shell")
	names, _ := filepath.Glob(filepath.Join(dir, "zsh-*"))
	if len(names) == 0 {
		return
	}
	// Two unsent commands at once would import the same records twice.
	lock, err := os.OpenFile(filepath.Join(dir, ".import.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX) != nil {
		return
	}
	groups := map[string]*shellGroup{}
	group := func(host string, pid int) *shellGroup {
		key := host + "-" + strconv.Itoa(pid)
		if groups[key] == nil {
			groups[key] = &shellGroup{host: host, pid: pid}
		}
		return groups[key]
	}
	for _, n := range names {
		if l, ok := parseShellLog(n); ok {
			g := group(l.host, l.pid)
			g.logs = append(g.logs, l)
			continue
		}
		stem, ok := strings.CutSuffix(filepath.Base(n), ".import")
		rest, isZsh := strings.CutPrefix(stem, "zsh-")
		i := strings.LastIndexByte(rest, '-')
		if !ok || !isZsh || i <= 0 {
			continue
		}
		if pid, err := strconv.Atoi(rest[i+1:]); err == nil && pid > 0 {
			group(rest[:i], pid).state = n
		}
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	host := shellHost()
	for _, k := range keys {
		s.importShell(groups[k], host)
	}
	s.prune()
}

// importShell imports one shell's logs, oldest first.
func (s *store) importShell(g *shellGroup, host string) {
	statePath := g.state
	if statePath == "" {
		statePath = filepath.Join(s.dir, "shell", fmt.Sprintf("zsh-%s-%d.import", g.host, g.pid))
	}
	var st *shellState
	if data, err := os.ReadFile(statePath); err == nil {
		st = &shellState{}
		if json.Unmarshal(data, st) != nil {
			st = nil // read again from the start: the files it writes are the same
		} else if st.Format > shellStateFormat {
			return // a newer unsent's state
		}
	}
	foreign := g.host != host
	if foreign {
		// Another host's shell: its pid means nothing here.
		for _, l := range g.logs {
			if fi, err := os.Stat(l.path); err != nil || time.Since(fi.ModTime()) < shellForeignAge {
				return
			}
		}
	}
	sort.Slice(g.logs, func(i, j int) bool { return before(g.logs[i].stamp, g.logs[j].stamp) })
	if st != nil {
		if i := slices.IndexFunc(g.logs, func(l shellLog) bool { return l.stem == st.Stem }); i > 0 {
			l := g.logs[i]
			copy(g.logs[1:i+1], g.logs[:i])
			g.logs[0] = l
		}
	}
	alive := !foreign && pidAlive(g.pid)
	for i, l := range g.logs {
		if st == nil {
			st = s.newShellState(l)
		}
		from := 0
		if st.Stem == l.stem {
			from = st.Offset
		}
		end, ok := s.readShellLog(st, l, from)
		// Nothing new: the state file already says all this, and a write
		// costs two syncs per live shell on every unsent command.
		changed := end != from || st.Stem != l.stem
		st.Stem, st.Offset = l.stem, end
		if !ok {
			// A newer format or a broken record: leave it for a later unsent.
			if changed {
				s.saveShellState(statePath, st)
			}
			return
		}
		last := i == len(g.logs)-1
		if !l.done && alive && last {
			// The shell writes here still.
			if changed {
				s.saveShellState(statePath, st)
			}
			return
		}
		// Nothing writes to this log any more. A .log that is not the last
		// is a shell that ended, whose pid a newer shell took; the last log
		// of a dead shell ends it too. After a .done the shell goes on in its
		// next log, whose first prompt ends the line.
		if !l.done || last && !alive {
			if s.endShell(st) != nil {
				s.saveShellState(statePath, st)
				return
			}
			os.Remove(statePath)
			st = nil
		} else if s.saveShellState(statePath, st) != nil {
			return
		}
		if err := os.Remove(l.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			// The next import starts here again: at its end if the state
			// names it, or from the start, writing the same files.
			return
		}
	}
	if len(g.logs) == 0 && st != nil && !alive {
		// The logs went earlier and the shell has ended since.
		if s.endShell(st) == nil {
			os.Remove(statePath)
		}
	}
}

func (s *store) newShellState(l shellLog) *shellState {
	return &shellState{
		Format: shellStateFormat,
		Line: record{
			Format:  recordFormat,
			ID:      l.stem,
			Command: []string{"zsh"},
			Agent:   "zsh",
			PID:     l.pid,
		},
	}
}

func (s *store) saveShellState(path string, st *shellState) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return writeFileDurable(path, data)
}

// readShellLog applies the log's complete records from offset from on, and
// returns the offset after the last one it applied. It reports false when
// the log is in a newer format, a record is broken, or the store could not
// take a record; the records before it are applied.
func (s *store) readShellLog(st *shellState, l shellLog, from int) (int, bool) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return from, false
	}
	if from > len(data) {
		from = len(data)
	}
	at := from
	for at < len(data) {
		kind, when, text, n, err := nextShellRecord(data[at:])
		if err != nil {
			return at, false
		}
		if n == 0 {
			break // still being written, or torn by a failed write
		}
		if at == 0 && (kind != 'v' || text != shellLogFormat) {
			return at, false
		}
		if s.applyShellRecord(st, kind, when, text, fmt.Sprintf("%s-%09d", l.stem, at)) != nil {
			return at, false
		}
		at += n
	}
	return at, true
}

// nextShellRecord parses the record at the start of b: its kind, time and
// text, and its length, which is 0 when the record is not complete yet.
func nextShellRecord(b []byte) (kind byte, when time.Time, text string, n int, err error) {
	nl := bytes.IndexByte(b, '\n')
	if nl < 0 {
		return 0, when, "", 0, nil
	}
	f := strings.Fields(string(b[:nl]))
	if len(f) != 3 || len(f[0]) != 1 {
		return 0, when, "", 0, fmt.Errorf("bad record header %q", b[:nl])
	}
	secs, err1 := strconv.ParseInt(f[1], 10, 64)
	size, err2 := strconv.Atoi(f[2])
	if err1 != nil || err2 != nil || size < 0 {
		return 0, when, "", 0, fmt.Errorf("bad record header %q", b[:nl])
	}
	body := b[nl+1:]
	if len(body) < size+1 {
		return 0, when, "", 0, nil
	}
	if body[size] != '\n' {
		return 0, when, "", 0, fmt.Errorf("record %q does not end in a line break", b[:nl])
	}
	return f[0][0], time.Unix(secs, 0), string(body[:size]), nl + 1 + size + 1, nil
}

// applyShellRecord takes one record into the shell's state. at names the
// record, for the files it writes.
func (s *store) applyShellRecord(st *shellState, kind byte, when time.Time, text, at string) error {
	switch kind {
	case 'i': // a new prompt: the line before it ended
		if err := s.endLine(st, when, at); err != nil {
			return err
		}
		st.Line.Cwd = realPath(text)
		if st.Line.Started.IsZero() {
			st.Line.Started = when
		}
		st.Line.Updated = when
	case 'c': // a continuation prompt: Enter did not run the line
		st.Ran = false
	case 'b', 'h': // the line before a redraw; the line when the window closed
		return s.setLine(st, text, when, at)
	case 's': // Enter ended the line
		if err := s.setLine(st, text, when, at); err != nil {
			return err
		}
		st.Ran = st.Line.Draft != ""
	case 'f': // the line gained a leading space or came to match HISTORY_IGNORE
		return s.forgetLine(st, text == "new", at)
	}
	return nil
}

// setLine replaces the line at the prompt. The old line goes to history
// when the new one empties it or is not an edit of it, as when a widget
// such as atuin's writes a recalled line into the buffer. A line that
// starts with a space is never kept.
func (s *store) setLine(st *shellState, text string, when time.Time, at string) error {
	if strings.HasPrefix(text, " ") {
		return s.forgetLine(st, false, at)
	}
	old := st.Line.Draft
	if text == old {
		return nil
	}
	if strings.TrimSpace(old) != "" && (text == "" || !shellEdit(old, text)) {
		if err := s.keepLine(st, at); err != nil {
			return err
		}
	}
	st.Line.Draft = text
	st.Line.Updated = when
	return nil
}

// shellEdit reports whether the line b is an edit of the line a rather
// than another line: what they share at the start and end covers at least
// half of a, or b holds all of a. Unlike similar it holds at any length,
// since most command lines are shorter than similar's 24 bytes.
func shellEdit(a, b string) bool {
	if strings.Contains(b, a) {
		return true
	}
	p, q := sharedEnds(a, b)
	return 2*(p+q) >= len(a)
}

// endLine closes the line at the prompt: one Enter ran follows the on-send
// setting; any other goes to history.
func (s *store) endLine(st *shellState, when time.Time, at string) error {
	if st.Ran {
		if err := s.lineSent(st, when, at); err != nil {
			return err
		}
	} else if history, _ := keepOld(st.Line.Draft, "", false); history {
		if err := s.keepLine(st, at); err != nil {
			return err
		}
	}
	st.Line.Draft, st.Ran = "", false
	return nil
}

// keepLine writes the line at the prompt to history, named after the
// record that replaced or ended it.
func (s *store) keepLine(st *shellState, at string) error {
	id := shellHistoryPrefix + st.Line.Updated.Format("20060102-150405") + "-" + at
	return s.archiveAs(&st.Line, id)
}

// forgetLine takes a forget mark. It drops the line at the prompt, which
// the user edited into the text zsh ignores, unless replaced says the
// ignored text took the line's place instead, such as a recall of a line
// that starts with a space: then the line goes to history. Lines that went
// to history earlier at this prompt were other lines, and stay.
func (s *store) forgetLine(st *shellState, replaced bool, at string) error {
	if replaced && strings.TrimSpace(st.Line.Draft) != "" {
		if err := s.keepLine(st, at); err != nil {
			return err
		}
	}
	st.Line.Draft, st.Ran = "", false
	return nil
}

// lineSent follows the on-send setting for a line Enter ran: delete, the
// shells' default, keeps nothing; log appends it to the shell's sent log.
// A sent log that cannot be written sends the line to history instead.
func (s *store) lineSent(st *shellState, when time.Time, at string) error {
	if onSend(st.Line.agent()) == "delete" {
		return nil
	}
	if err := s.logSentAt(&st.Line, when); err != nil {
		s.warn(err)
		return s.keepLine(st, at)
	}
	return nil
}

// endShell closes a shell that is gone: a line Enter ran follows the
// on-send setting, and any other line left at the prompt becomes an orphan
// draft, as an agent's box does when its session ends.
func (s *store) endShell(st *shellState) error {
	if st.Ran {
		return s.lineSent(st, st.Line.Updated, st.Stem+"-end")
	}
	if strings.TrimSpace(st.Line.Draft) == "" {
		return nil
	}
	r := st.Line
	r.Ended = r.Updated
	return s.write(&r)
}
