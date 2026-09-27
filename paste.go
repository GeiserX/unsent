package main

import (
	"bytes"
	"strconv"
	"strings"
	"sync"
)

var (
	pasteStart = []byte("\x1b[200~")
	pasteEnd   = []byte("\x1b[201~")
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
	// cuts holds the text behind each placeholder the agent put in place
	// of the middle of a long draft (see expand).
	cuts map[string]string
}

// feed takes keystrokes as they arrive, and returns the bytes that were
// typed rather than pasted.
func (p *pasteTracker) feed(b []byte) (typed []byte) {
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
			} else {
				typed = append(typed, data[:len(data)-keep]...)
			}
			p.pending = append(p.pending, data[len(data)-keep:]...)
			return typed
		}
		if p.in {
			p.cur = append(p.cur, data[:i]...)
			p.pastes = append(p.pastes, normalizeNewlines(string(p.cur)))
			p.cur = nil
		} else {
			typed = append(typed, data[:i]...)
		}
		p.in = !p.in
		data = data[i+len(marker):]
	}
	return typed
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
	p.cuts = nil
	p.mu.Unlock()
}

func (p *pasteTracker) all() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.pastes...)
}

// expand puts the pasted text back where the agent shows a placeholder
// (see profile.placeholder). Placeholders are matched to pastes in order,
// by line count, among the pastes the agent shows as a placeholder at all.
// One that matches no paste stays as it is.
//
// A placeholder for the middle of a long draft (profile.truncated) stands
// for text the agent cut out of the box, typed or pasted. When prev, the
// draft saved before, reads the same as the new one on both sides of the
// placeholder, the middle is what prev holds between them: it is put back,
// and kept for the saves after. Otherwise the placeholder stays, and prev,
// which no longer looks like the draft, goes to history (see keepOld).
func (p *pasteTracker) expand(draft, prev string, prof *profile) string {
	pastes := p.all()
	used := make([]bool, len(pastes))
	out := draft
	if prof.placeholder != nil {
		out = prof.placeholder.ReplaceAllStringFunc(draft, func(ph string) string {
			m := prof.placeholder.FindStringSubmatch(ph)
			want := 0
			if m[1] != "" {
				want, _ = strconv.Atoi(m[1])
			}
			for i, text := range pastes {
				if used[i] || strings.Contains(draft, text) || (prof.collapses != nil && !prof.collapses(text)) {
					// Already matched, or a paste the agent took in as typed
					// text, or turned into something else, such as an image.
					continue
				}
				nl := strings.Count(strings.TrimRight(text, "\n"), "\n")
				if nl == want || strings.Count(text, "\n") == want {
					used[i] = true
					return text
				}
			}
			return ph
		})
	}
	if prof.truncated == nil {
		return out
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if at := prof.truncated.FindAllStringSubmatchIndex(out, -1); len(at) == 1 {
		ph, head, tail := out[at[0][0]:at[0][1]], out[:at[0][0]], out[at[0][1]:]
		want, _ := strconv.Atoi(out[at[0][2]:at[0][3]])
		if _, ok := p.cuts[ph]; !ok && len(prev) > len(head)+len(tail) &&
			strings.HasPrefix(prev, head) && strings.HasSuffix(prev, tail) {
			mid := prev[len(head) : len(prev)-len(tail)]
			if strings.Count(mid, "\n") == want && !prof.truncated.MatchString(mid) {
				if p.cuts == nil {
					p.cuts = map[string]string{}
				}
				p.cuts[ph] = mid
				// The middle can come back as a paste placeholder: Claude
				// Code shows it as one after an editor round trip.
				p.pastes = append(p.pastes, mid)
			}
		}
	}
	return prof.truncated.ReplaceAllStringFunc(out, func(ph string) string {
		if mid, ok := p.cuts[ph]; ok {
			return mid
		}
		return ph
	})
}
