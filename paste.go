package main

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var (
	pasteStart = []byte("\x1b[200~")
	pasteEnd   = []byte("\x1b[201~")
	// Claude Code shows a long paste as "[Pasted text #2 +39 lines]".
	pastePlaceholder = regexp.MustCompile(`\[Pasted text #\d+(?: \+(\d+) lines?)?\]`)
)

// pasteTracker keeps the text of every paste since the box was last empty.
// Terminals wrap a paste in bracketed-paste markers, so it can be picked out
// of the keystrokes exactly, whatever the agent then shows on screen.
type pasteTracker struct {
	mu      sync.Mutex
	pastes  []string
	cur     []byte
	in      bool
	pending []byte // a marker split across two reads
}

func (p *pasteTracker) feed(b []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	data := append(p.pending, b...)
	p.pending = nil
	for len(data) > 0 {
		marker := pasteStart
		if p.in {
			marker = pasteEnd
		}
		i := bytes.Index(data, marker)
		if i < 0 {
			// Keep a tail that could be the start of a marker cut in two.
			keep := partialSuffix(data, marker)
			if p.in {
				p.cur = append(p.cur, data[:len(data)-keep]...)
			}
			p.pending = append(p.pending, data[len(data)-keep:]...)
			return
		}
		if p.in {
			p.cur = append(p.cur, data[:i]...)
			p.pastes = append(p.pastes, normalizeNewlines(string(p.cur)))
			p.cur = nil
		}
		p.in = !p.in
		data = data[i+len(marker):]
	}
}

// partialSuffix returns how many bytes at the end of data are a proper
// prefix of marker.
func partialSuffix(data, marker []byte) int {
	for k := min(len(marker)-1, len(data)); k > 0; k-- {
		if bytes.HasSuffix(data, marker[:k]) {
			return k
		}
	}
	return 0
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// inPaste reports whether a paste has started and not yet ended.
func (p *pasteTracker) inPaste() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.in
}

func (p *pasteTracker) reset() {
	p.mu.Lock()
	p.pastes = nil
	p.mu.Unlock()
}

func (p *pasteTracker) all() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.pastes...)
}

// expand puts the pasted text back where the agent shows a placeholder.
// Placeholders are matched to pastes in order, by line count. One that
// matches no paste stays as it is.
func (p *pasteTracker) expand(draft string) string {
	pastes := p.all()
	used := make([]bool, len(pastes))
	return pastePlaceholder.ReplaceAllStringFunc(draft, func(ph string) string {
		m := pastePlaceholder.FindStringSubmatch(ph)
		want := -1
		if m[1] != "" {
			want, _ = strconv.Atoi(m[1])
		}
		for i, text := range pastes {
			if used[i] || strings.Contains(draft, text) {
				// Already matched, or a short paste the agent typed in as is.
				continue
			}
			nl := strings.Count(strings.TrimRight(text, "\n"), "\n")
			if want < 0 || nl == want || strings.Count(text, "\n") == want {
				used[i] = true
				return text
			}
		}
		return ph
	})
}
