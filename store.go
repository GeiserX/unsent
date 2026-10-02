package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// historyLimit caps how many archived agent drafts are kept, and
// shellHistoryLimit how many shell lines. Shell lines are short and many,
// so they count against their own cap and never push agent drafts out.
const (
	historyLimit      = 500
	shellHistoryLimit = 500
)

// Safety copies (see keepVersion) are capped separately, so they never push
// real history out: this many per session, and this many in all.
const (
	versionsPerSession = 30
	versionsLimit      = 300
)

// recordFormat is the layout of a record file. Files written before the
// field existed read as 0 and have the same layout as format 1. Never rename
// or retype a field without bumping this and adding a migration test: a
// renamed field would read back as an empty draft.
const recordFormat = 1

// record is one wrapped session and the draft its input box last held.
type record struct {
	Format  int      `json:"format"`
	ID      string   `json:"id"`
	Command []string `json:"command"`
	// Agent names the agent the draft came from (see agentFor), so it is
	// only offered back to that agent.
	Agent string `json:"agent"`
	// AgentSession is the agent's own id of the conversation the draft was
	// typed in, as the profile read it at the save (see sessionTracker);
	// "" when unknown, and in files written before the field existed. The
	// sent log's header carries the same field.
	AgentSession string `json:"agent_session"`
	// joined is when this run entered AgentSession: the run's start for
	// the first id, the switch for a later one. A resumed conversation's
	// sent log says so with this time.
	joined time.Time
	// Cwd is the folder the agent ran in, as a resolved real path. Files
	// written before that may hold a path through a symlink.
	Cwd     string    `json:"cwd"`
	PID     int       `json:"pid"`
	Started time.Time `json:"started"`
	Updated time.Time `json:"updated"`
	Ended   time.Time `json:"ended,omitzero"`
	Draft   string    `json:"draft"`
	// Pastes holds the raw text of every paste in the draft, in case the
	// agent showed one as a placeholder that could not be matched.
	Pastes []string `json:"pastes,omitempty"`
	// Version marks a safety copy: an earlier version of a live draft, kept
	// because a later save lost text out of sight.
	Version bool `json:"version,omitempty"`
	// session is, for a safety copy load read, the id of the session it
	// was kept from: the id its file holds. load gives the copy its file
	// name as ID, so every copy is a draft of its own.
	session string
	// RestoreTries counts the restores of this orphan into its reopened
	// session that were not read back (see restore.go).
	RestoreTries int `json:"restore_tries,omitempty"`
}

// agent is the agent the draft came from. Files written before records named
// it fall back to the base name of the command.
func (r *record) agent() string {
	if r.Agent != "" {
		return r.Agent
	}
	if len(r.Command) == 0 {
		return ""
	}
	return agentName(filepath.Base(r.Command[0]))
}

func newRecord(args []string, cwd string) *record {
	now := time.Now()
	return &record{
		Format:  recordFormat,
		ID:      fmt.Sprintf("%s-%d", now.Format("20060102-150405"), os.Getpid()),
		Command: args,
		Cwd:     cwd,
		PID:     os.Getpid(),
		Started: now,
		Updated: now,
	}
}

// store is the state directory: one file per live or unrecovered session in
// drafts/, drafts that were cleared or replaced in history/, and each
// session's sent messages in sent/ (see sent.go).
type store struct {
	dir    string
	warned bool
	lock   *os.File
	// joins holds, per conversation sent log this process appended to, the
	// joined time of the record it last appended for (see logSentAt).
	joins map[string]time.Time
}

func stateDir() (string, error) {
	if d := os.Getenv("UNSENT_HOME"); d != "" {
		return d, nil
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "unsent"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "unsent"), nil
}

// openStore opens the store with the shell logs imported: every command
// that reads the store sees the shells' lines too.
func openStore() (*store, error) {
	s, err := makeStore()
	if err != nil {
		return nil, err
	}
	s.importShells()
	return s, nil
}

// makeStore makes the store's folders, private, and imports nothing: wrap
// learns whether the store can open before it starts the agent, and
// imports once the agent runs.
func makeStore() (*store, error) {
	dir, err := stateDir()
	if err != nil {
		return nil, err
	}
	return makeStoreAt(dir)
}

// makeStoreAt is makeStore in dir.
func makeStoreAt(dir string) (*store, error) {
	for _, sub := range []string{"drafts", "history", "sent"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return nil, err
		}
	}
	// MkdirAll leaves an existing folder's mode alone; drafts are private.
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	return &store{dir: dir}, nil
}

func (s *store) draftPath(id string) string {
	return filepath.Join(s.dir, "drafts", id+".json")
}

// write saves the record so it survives a crash or a power cut: a temporary
// file is flushed to disk, then renamed over the old one.
func (s *store) write(r *record) error {
	return writeDurable(s.draftPath(r.ID), r)
}

func (s *store) lockPath(id string) string {
	return filepath.Join(s.dir, "drafts", id+".lock")
}

// hold takes an exclusive lock that lives as long as this process. The
// kernel drops it on any exit, crash or reboot, which is what makes a
// session's liveness reliable where a PID check is not (PIDs are reused).
func (s *store) hold(r *record) error {
	f, err := os.OpenFile(s.lockPath(r.ID), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return err
	}
	s.lock = f
	return nil
}

// release drops the session lock. Exiting does the same; this is for
// callers that keep running.
func (s *store) release() {
	if s.lock != nil {
		s.lock.Close()
		s.lock = nil
	}
}

// alive reports whether the wrapper that owns the record still runs.
func (s *store) alive(r *record) bool {
	f, err := os.Open(s.lockPath(r.ID))
	if err != nil {
		return false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return true
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

func (s *store) remove(r *record) {
	os.Remove(s.draftPath(r.ID))
	os.Remove(s.lockPath(r.ID))
}

// archive copies the record's current draft into history/, so a box that was
// cleared by mistake can still be recovered.
func (s *store) archive(r *record) error {
	if strings.TrimSpace(r.Draft) == "" {
		return nil
	}
	id := fmt.Sprintf("%s-%d", time.Now().Format("20060102-150405.000000"), r.PID)
	if isShell(r.agent()) {
		id = shellHistoryPrefix + id
	}
	if err := s.archiveAs(r, id); err != nil {
		s.warn(err)
		return err
	}
	s.prune()
	return nil
}

// archiveAs writes the record's draft into history/ under id. Shell lines
// take ids that start with shellHistoryPrefix, and names sort by time.
func (s *store) archiveAs(r *record, id string) error {
	if strings.TrimSpace(r.Draft) == "" {
		return nil
	}
	a := *r
	a.ID = id
	return writeDurable(filepath.Join(s.dir, "history", a.ID+".json"), &a)
}

// keepVersion saves the record's current draft as a safety copy, named
// after its session so the copies can be capped and cleared per session.
func (s *store) keepVersion(r *record) error {
	if strings.TrimSpace(r.Draft) == "" {
		return nil
	}
	a := *r
	a.Version = true
	name := fmt.Sprintf("v-%s-%s", r.ID, time.Now().Format("20060102-150405.000000"))
	if err := writeDurable(filepath.Join(s.dir, "history", name+".json"), &a); err != nil {
		s.warn(err)
		return err
	}
	trim(filepath.Join(s.dir, "history", "v-"+r.ID+"-*.json"), versionsPerSession)
	trimOldest(filepath.Join(s.dir, "history", "v-*.json"), versionsLimit)
	return nil
}

// dropVersions removes a session's safety copies once they protect nothing:
// its draft was sent, or cleared and archived.
func (s *store) dropVersions(r *record) {
	names, _ := filepath.Glob(filepath.Join(s.dir, "history", "v-"+r.ID+"-*.json"))
	for _, n := range names {
		os.Remove(n)
	}
}

// prune keeps the newest historyLimit agent drafts and, apart from them,
// the newest shellHistoryLimit shell lines. Safety copies have caps of
// their own (keepVersion).
func (s *store) prune() {
	names, _ := filepath.Glob(filepath.Join(s.dir, "history", "*.json"))
	var agents, shells []string
	for _, n := range names {
		switch b := filepath.Base(n); {
		case strings.HasPrefix(b, "v-"):
		case strings.HasPrefix(b, shellHistoryPrefix):
			shells = append(shells, n)
		default:
			agents = append(agents, n)
		}
	}
	for _, c := range []struct {
		names []string
		limit int
	}{{agents, historyLimit}, {shells, shellHistoryLimit}} {
		sort.Strings(c.names)
		for len(c.names) > c.limit {
			os.Remove(c.names[0])
			c.names = c.names[1:]
		}
	}
}

// trim keeps the newest limit files matching pattern; names sort by time.
func trim(pattern string, limit int) {
	names, _ := filepath.Glob(pattern)
	sort.Strings(names)
	for len(names) > limit {
		os.Remove(names[0])
		names = names[1:]
	}
}

// trimOldest keeps the limit most recently written files matching pattern.
func trimOldest(pattern string, limit int) {
	names, _ := filepath.Glob(pattern)
	trimOldestOf(names, limit)
}

// trimOldestOf keeps the limit most recently written of the files names.
func trimOldestOf(names []string, limit int) {
	if len(names) <= limit {
		return
	}
	mod := map[string]time.Time{}
	for _, n := range names {
		if fi, err := os.Stat(n); err == nil {
			mod[n] = fi.ModTime()
		}
	}
	sort.Slice(names, func(i, j int) bool { return mod[names[i]].Before(mod[names[j]]) })
	for _, n := range names[:len(names)-limit] {
		os.Remove(n)
	}
}

// warn reports a failed save once per session, on stderr, so a full disk is
// not silent but does not flood the agent's screen either.
func (s *store) warn(err error) {
	if s.warned {
		return
	}
	s.warned = true
	fmt.Fprintf(os.Stderr, "\r\nunsent: could not save the draft: %v\r\n", err)
}

func writeDurable(path string, r *record) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return writeFileDurable(path, data)
}

// writeFileDurable replaces path with data so that a crash or a power cut
// leaves the old file or the new one, never half of one.
func writeFileDurable(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	// A failed folder sync means the rename may not survive a power cut:
	// the write is not durable, and a caller about to remove the old copy
	// (restore, or a save replacing a draft) must not.
	return syncDir(filepath.Dir(path))
}

// syncDir flushes a folder's entries, so a new or renamed file in it
// survives a power cut.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// load reads every record in drafts/ (and history/ when withHistory is set),
// newest first.
func (s *store) load(withHistory bool) []*record {
	subs := []string{"drafts"}
	if withHistory {
		subs = append(subs, "history")
	}
	var out []*record
	for _, sub := range subs {
		names, _ := filepath.Glob(filepath.Join(s.dir, sub, "*.json"))
		for _, n := range names {
			data, err := os.ReadFile(n)
			if err != nil {
				continue
			}
			var r record
			if json.Unmarshal(data, &r) != nil || strings.TrimSpace(r.Draft) == "" {
				continue
			}
			if r.Version {
				r.session = r.ID
				r.ID = strings.TrimSuffix(filepath.Base(n), ".json")
			}
			out = append(out, &r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

// orphans are drafts whose session is gone: the window closed, the agent or
// the machine crashed, or the agent exited with text still in the box. Dead
// sessions that left an empty box are cleaned up on the way, and so are the
// claims of sessions that died restoring an orphan.
func (s *store) orphans() []*record {
	s.dropStaleClaims()
	var out []*record
	names, _ := filepath.Glob(filepath.Join(s.dir, "drafts", "*.json"))
	for _, n := range names {
		data, err := os.ReadFile(n)
		if err != nil {
			continue
		}
		var r record
		if json.Unmarshal(data, &r) != nil {
			continue
		}
		// A newer unsent wrote this file. Its draft may sit in a field this
		// build does not know, so an empty Draft here proves nothing.
		if r.Format > recordFormat {
			continue
		}
		// The file name, not the content, says which files belong to it.
		r.ID = strings.TrimSuffix(filepath.Base(n), ".json")
		if s.alive(&r) {
			continue
		}
		if strings.TrimSpace(r.Draft) == "" {
			s.remove(&r)
			continue
		}
		out = append(out, &r)
	}
	s.sweepTemp()
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

// sweepTemp removes temporary files left by a crash in the middle of a
// write. A live write finishes in milliseconds; an hour is plenty.
func (s *store) sweepTemp() {
	for _, sub := range []string{"drafts", "history", "sent"} {
		names, _ := filepath.Glob(filepath.Join(s.dir, sub, ".tmp-*"))
		for _, n := range names {
			if fi, err := os.Stat(n); err == nil && time.Since(fi.ModTime()) > time.Hour {
				os.Remove(n)
			}
		}
	}
}

// claimMark sits between an orphan's file name and the id of the session
// that claimed it for a restore: drafts/<id>.json.claim-<session>. The name
// is outside drafts/*.json, so no other process lists or restores it while
// the claim holds.
const claimMark = ".json.claim-"

// claim takes an orphan for a restore into the box, atomically: of two
// processes that open one conversation, the rename succeeds for one. by is
// the claiming session's record id, whose lock tells whether it still runs.
func (s *store) claim(r *record, by string) (string, error) {
	path := filepath.Join(s.dir, "drafts", r.ID+claimMark+by)
	if err := os.Rename(s.draftPath(r.ID), path); err != nil {
		return "", err
	}
	return path, nil
}

// unclaim puts a claimed orphan back, counting a restore that failed.
func (s *store) unclaim(path string, r *record) {
	r.RestoreTries++
	writeDurable(path, r)
	s.unclaimAsIs(path, r)
}

// unclaimAsIs puts a claimed orphan back as it was: nothing was pasted.
func (s *store) unclaimAsIs(path string, r *record) {
	if err := os.Rename(path, s.draftPath(r.ID)); err != nil {
		s.warn(err)
	}
}

// keepRestored moves a claimed orphan to history once the box read it
// back: the draft lives on in the reopened session's own record.
func (s *store) keepRestored(path string, r *record) error {
	if err := s.archive(r); err != nil {
		return err
	}
	os.Remove(s.lockPath(r.ID))
	return os.Remove(path)
}

// dropStaleClaims puts back the orphans claimed by sessions that are gone:
// one that died between the claim and the read back must not hide the
// draft.
func (s *store) dropStaleClaims() {
	names, _ := filepath.Glob(filepath.Join(s.dir, "drafts", "*"+claimMark+"*"))
	for _, n := range names {
		id, by, _ := strings.Cut(filepath.Base(n), claimMark)
		if s.alive(&record{ID: by}) {
			continue
		}
		if _, err := os.Stat(s.draftPath(id)); err == nil {
			continue
		}
		os.Rename(n, s.draftPath(id))
	}
}

// sessionOrphan is the newest orphan an agent left in folder cwd in the
// conversation id, that restores have not given up on; nil when there is
// none.
func (s *store) sessionOrphan(agent, cwd, id string) *record {
	if r := s.conversationOrphan(agent, cwd, id); r != nil && r.RestoreTries < maxRestoreTries {
		return r
	}
	return nil
}

// conversationOrphan is the newest orphan an agent left in folder cwd in
// the conversation id, whether or not restores gave up on it: a draft
// still waits for the user after its last paste failed.
func (s *store) conversationOrphan(agent, cwd, id string) *record {
	for _, r := range s.orphans() {
		if r.agent() == agent && samePath(r.Cwd, cwd) && sessionMatch(r.AgentSession, id) {
			return r
		}
	}
	return nil
}
