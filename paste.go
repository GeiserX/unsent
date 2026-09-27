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
	// rows is the window height, and heights holds it as each paste
	// ended: whether the agent collapses a paste can depend on it.
	rows    int
	heights []int
	// nums maps a placeholder's number to the paste that filled it, and
	// cuts holds the text behind each placeholder the agent put in place
	// of the middle of a long draft (see expand).
	nums map[int]int
	cuts map[int]string
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
			p.heights = append(p.heights, p.rows)
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

// resize notes the window height.
func (p *pasteTracker) resize(rows int) {
	p.mu.Lock()
	p.rows = rows
	p.mu.Unlock()
}

func (p *pasteTracker) reset() {
	p.mu.Lock()
	p.pastes, p.heights = nil, nil
	p.nums, p.cuts = nil, nil
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
// A paste then belongs to that placeholder's number, and never fills
// another number's. One that matches no paste stays as it is.
//
// A placeholder for the middle of a long draft (profile.truncated) stands
// for text the agent cut out of the box, typed or pasted. When prev, the
// draft saved before, reads the same as the new one on both sides of the
// placeholder, the middle is what prev holds between them: it is put back,
// and kept under its number for the saves after, also where a paste
// placeholder shows that number. Otherwise the placeholder stays, and
// prev, which no longer looks like the draft, goes to history (see
// keepOld).
func (p *pasteTracker) expand(draft, prev string, prof *profile) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	used := make([]bool, len(p.pastes))
	held := map[int]int{} // paste index to the number that holds it
	for n, i := range p.nums {
		held[i] = n
	}
	out := draft
	if prof.placeholder != nil {
		out = prof.placeholder.ReplaceAllStringFunc(draft, func(ph string) string {
			m := prof.placeholder.FindStringSubmatch(ph)
			num, _ := strconv.Atoi(m[1])
			want := 0
			if m[2] != "" {
				want, _ = strconv.Atoi(m[2])
			}
			if mid, ok := p.cuts[num]; ok {
				// Claude Code shows a cut middle as a paste after an
				// editor round trip.
				return mid
			}
			for i, text := range p.pastes {
				if n, ok := held[i]; used[i] || (ok && n != num) || strings.Contains(draft, text) ||
					(prof.collapses != nil && !prof.collapses(text, p.heights[i])) {
					// Already matched, or another number's paste, or a paste
					// the agent took in as typed text, or turned into
					// something else, such as an image.
					continue
				}
				nl := strings.Count(strings.TrimRight(text, "\n"), "\n")
				if nl == want || strings.Count(text, "\n") == want {
					used[i] = true
					if p.nums == nil {
						p.nums = map[int]int{}
					}
					p.nums[num] = i
					return text
				}
			}
			return ph
		})
	}
	if prof.truncated == nil {
		return out
	}
	if at := prof.truncated.FindAllStringSubmatchIndex(out, -1); len(at) == 1 {
		head, tail := out[:at[0][0]], out[at[0][1]:]
		num, _ := strconv.Atoi(out[at[0][2]:at[0][3]])
		want, _ := strconv.Atoi(out[at[0][4]:at[0][5]])
		if _, ok := p.cuts[num]; !ok && len(prev) > len(head)+len(tail) &&
			strings.HasPrefix(prev, head) && strings.HasSuffix(prev, tail) {
			mid := prev[len(head) : len(prev)-len(tail)]
			if strings.Count(mid, "\n") == want && !prof.truncated.MatchString(mid) {
				if p.cuts == nil {
					p.cuts = map[int]string{}
				}
				p.cuts[num] = mid
			}
		}
	}
	return prof.truncated.ReplaceAllStringFunc(out, func(ph string) string {
		num, _ := strconv.Atoi(prof.truncated.FindStringSubmatch(ph)[1])
		if mid, ok := p.cuts[num]; ok {
			return mid
		}
		return ph
	})
}
