package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/term"
)

// rawLog records the bytes that pass between the terminal and the agent,
// keys in and output out, in the order they passed, the way `script -r`
// records a session: each chunk has a 24-byte header (length, seconds,
// microseconds, direction), so `script -p` plays one back and the replay
// tests read one as they read the fixtures. The window sizes, which the
// format has no room for, go to a JSON file next to it. Nothing is
// recorded unless UNSENT_DEBUG_DIR is set or `unsent capture` runs.
type rawLog struct {
	mu   sync.Mutex
	f    *os.File
	base string
	meta rawMeta
}

type rawMeta struct {
	Agent   string    `json:"agent"`
	Version string    `json:"version,omitempty"`
	Sizes   []rawSize `json:"sizes"`
}

// rawSize is the window size from a moment on, on the record's clock.
type rawSize struct {
	At   time.Time `json:"at"`
	Cols int       `json:"cols"`
	Rows int       `json:"rows"`
}

// openRaw starts a raw log in base.rec, with the sizes in base.json. Both
// files are 0600: they hold every key typed.
func openRaw(base, agent, version string) (*rawLog, error) {
	f, err := os.OpenFile(base+".rec", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	r := &rawLog{f: f, base: base, meta: rawMeta{Agent: agent, Version: version}}
	r.chunk('s', nil)
	return r, nil
}

// debugRaw opens the raw log UNSENT_DEBUG_DIR asks for, one per session, or
// returns nil when it is not set.
func debugRaw(id, agent string) *rawLog {
	dir := os.Getenv("UNSENT_DEBUG_DIR")
	if dir == "" {
		return nil
	}
	r, err := openRaw(filepath.Join(dir, "raw-"+id), agent, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "unsent: no raw log: %v\n", err)
		return nil
	}
	return r
}

// record appends keys (dir 'i') or output (dir 'o'). A nil log records
// nothing, and a write that fails is dropped: logging never gets in the
// way of the session.
func (r *rawLog) record(dir byte, b []byte) {
	if r == nil || len(b) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chunk(dir, b)
}

// chunk writes one chunk. The caller holds r.mu, or owns r alone.
func (r *rawLog) chunk(dir byte, b []byte) {
	if r.f == nil {
		return
	}
	now := time.Now()
	var h [24]byte
	binary.LittleEndian.PutUint64(h[0:], uint64(len(b)))
	binary.LittleEndian.PutUint64(h[8:], uint64(now.Unix()))
	binary.LittleEndian.PutUint32(h[16:], uint32(now.Nanosecond()/1000))
	h[20] = dir
	r.f.Write(append(h[:], b...))
}

// resize notes the window size from now on.
func (r *rawLog) resize(cols, rows int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.meta.Sizes = append(r.meta.Sizes, rawSize{time.Now(), cols, rows})
	b, _ := json.MarshalIndent(r.meta, "", "  ")
	os.WriteFile(r.base+".json", append(b, '\n'), 0o600)
}

// close ends the record the way `script` does, with an 'e' chunk.
func (r *rawLog) close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chunk('e', nil)
	r.f.Close()
	r.f = nil
}

// editorCopy is what `unsent capture` points EDITOR and VISUAL at: it keeps
// the file the agent hands its editor as the next numbered editor copy and
// returns at once, so the copy is exactly what the agent held.
const editorCopy = `#!/bin/sh
n=1
while [ -e "$UNSENT_CAPTURE_TO.editor-$n.txt" ]; do n=$((n+1)); done
for f; do :; done
umask 077
cp "$f" "$UNSENT_CAPTURE_TO.editor-$n.txt"
`

// cmdCapture runs an agent under unsent with its keys and output recorded
// into a fixture bundle, testdata/<agent>/<version>/capture-<time>.*, in
// the current folder: the raw record, the window sizes, and an editor copy
// for each time the agent hands its box to $EDITOR.
func cmdCapture(args []string, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "unsent: usage: unsent capture <agent> [args...]")
		return 2
	}
	if off() || !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(stderr, "unsent: capture needs a terminal, and UNSENT_OFF unset")
		return 2
	}
	bin, err := exec.LookPath(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 127
	}
	agent := agentFor("", args[0])
	ask := []string{"--version"}
	if p := profileFor(args[0]); p != nil {
		ask = p.version
	}
	version := agentVersion(bin, ask)
	if version == "" {
		version = "unknown"
	}
	dir := filepath.Join("testdata", agent, version)
	tmp, err := os.MkdirTemp("", "unsent-capture-")
	if err == nil {
		defer os.RemoveAll(tmp)
		err = os.MkdirAll(dir, 0o755)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(tmp, "editor"), []byte(editorCopy), 0o700)
	}
	base := filepath.Join(dir, "capture-"+time.Now().Format("20060102-150405"))
	var raw *rawLog
	if err == nil {
		raw, err = openRaw(base, agent, version)
	}
	if err != nil {
		fmt.Fprintf(stderr, "unsent: %v\n", err)
		return 1
	}
	for k, v := range map[string]string{"EDITOR": filepath.Join(tmp, "editor"), "VISUAL": filepath.Join(tmp, "editor"), "UNSENT_CAPTURE_TO": base} {
		os.Setenv(k, v)
	}
	code := wrap(agent, args, os.Stdin, os.Stdout, raw)
	copies, _ := filepath.Glob(base + ".editor-*.txt")
	fmt.Fprintf(os.Stderr, "unsent: captured %s %s in %s.rec (keys and output), %s.json (window sizes) and %d editor copies. Check every file for real prompt text, names, paths and keys before committing.\n",
		agent, version, base, base, len(copies))
	return code
}
