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

// historyLimit caps how many archived drafts are kept.
const historyLimit = 500

// Safety copies (see keepVersion) are capped separately, so they never push
// real history out: this many per session, and this many in all.
const (
	versionsPerSession = 30
	versionsLimit      = 300
)

// record is one wrapped session and the draft its input box last held.
type record struct {
	ID      string    `json:"id"`
	Command []string  `json:"command"`
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
}

func newRecord(args []string, cwd string) *record {
	now := time.Now()
	return &record{
		ID:      fmt.Sprintf("%s-%d", now.Format("20060102-150405"), os.Getpid()),
		Command: args,
		Cwd:     cwd,
		PID:     os.Getpid(),
		Started: now,
		Updated: now,
	}
}

// store is the state directory: one file per live or unrecovered session in
// drafts/, and every draft that left an input box in history/.
type store struct {
	dir    string
	warned bool
	lock   *os.File
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

func openStore() (*store, error) {
	dir, err := stateDir()
	if err != nil {
		return nil, err
	}
	for _, sub := range []string{"drafts", "history"} {
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
	a := *r
	a.ID = fmt.Sprintf("%s-%d", time.Now().Format("20060102-150405.000000"), r.PID)
	if err := writeDurable(filepath.Join(s.dir, "history", a.ID+".json"), &a); err != nil {
		s.warn(err)
		return err
	}
	s.prune()
	return nil
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

func (s *store) prune() {
	names, _ := filepath.Glob(filepath.Join(s.dir, "history", "*.json"))
	var real []string
	for _, n := range names {
		if !strings.HasPrefix(filepath.Base(n), "v-") {
			real = append(real, n)
		}
	}
	sort.Strings(real)
	for len(real) > historyLimit {
		os.Remove(real[0])
		real = real[1:]
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
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
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
			out = append(out, &r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

// orphans are drafts whose session is gone: the window closed, the agent or
// the machine crashed, or the agent exited with text still in the box. Dead
// sessions that left an empty box are cleaned up on the way.
func (s *store) orphans() []*record {
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
	for _, sub := range []string{"drafts", "history"} {
		names, _ := filepath.Glob(filepath.Join(s.dir, sub, ".tmp-*"))
		for _, n := range names {
			if fi, err := os.Stat(n); err == nil && time.Since(fi.ModTime()) > time.Hour {
				os.Remove(n)
			}
		}
	}
}
