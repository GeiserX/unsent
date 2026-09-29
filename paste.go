package main

import (
	"bytes"
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// pasteRule is how an agent shows a paste it collapses, and how unsent
// pairs each placeholder with the paste behind it: the profile's paste
// rule (section 4 step 16 of docs/SPEC.md). Each rule counts the way its
// agent does.
type pasteRule struct {
	// placeholder matches what the agent shows in place of a paste.
	placeholder *regexp.Regexp
	// collapses reports whether the agent shows a paste as a placeholder
	// at all, in a window rows high: one it took in as typed text never
	// fills a placeholder.
	collapses func(paste string, rows int) bool
	// id names the placeholder match m stands for. While the box holds it
	// the same id stands for the same paste, and a paste paired with an id
	// fills no other. Ids are distinct across a profile's rules.
	id func(m []string) string
	// fits reports whether paste can be the one behind match m, by what
	// the placeholder says of it (its line breaks, its length).
	fits func(m []string, paste string) bool
	// rank orders the placeholders not yet paired, lowest first, when the
	// agent numbers them in the order it took the pastes; they are paired
	// with the pastes in that order. Nil takes them in the draft's order.
	rank func(m []string) int
	// reuses is true when the agent gives a deleted placeholder's id to
	// the next paste that needs it (Codex). A label held by a paste may
	// then stand for a paste that came after the last read that showed
	// it; the older paste is spent once a newer one takes its id, and
	// fills nothing after. A placeholder missing from one read, the box
	// scrolled or the stitcher starting over, keeps its paste.
	reuses bool
}

// pasteKey is a placeholder's id under the rule, by its index in the
// profile's pastes, that matched it.
type pasteKey struct {
	rule int
	id   string
}

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
	// nums maps a placeholder's id (pasteRule.id) to the paste that filled
	// it, spent holds the pastes whose id a newer paste took under a
	// reusing rule (pasteRule.reuses), and shown holds, for each id such a
	// rule paired, how many pastes had come when a read last showed it.
	// cuts holds the text behind each placeholder the agent put in place
	// of the middle of a long draft, by its number (see expand).
	nums  map[pasteKey]int
	spent map[int]bool
	shown map[pasteKey]int
	cuts  map[string]string
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

// holdsEsc reports whether feed is holding back a lone Esc typed outside a
// paste: the Esc key, or the start of a paste marker, which only the next
// read tells apart.
func (p *pasteTracker) holdsEsc() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.in && len(p.pending) == 1 && p.pending[0] == 0x1b
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
	p.nums, p.spent, p.shown, p.cuts = nil, nil, nil, nil
	p.mu.Unlock()
}

func (p *pasteTracker) all() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.pastes...)
}

// expand puts the pasted text back where the agent shows a placeholder
// (see profile.pastes). Placeholders are paired with pastes by the rule's
// count, in the rule's order, among the pastes the agent shows as a
// placeholder at all. A paste then belongs to that placeholder's id, and
// never fills another id's. One that matches no paste stays as it is.
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
	held := map[int]pasteKey{} // paste index to the id that holds it
	for key, i := range p.nums {
		held[i] = key
	}
	out := draft
	for r, rule := range prof.pastes {
		out = p.fill(out, draft, r, rule, used, held)
	}
	if prof.truncated == nil {
		return out
	}
	if at := prof.truncated.FindAllStringSubmatchIndex(out, -1); len(at) == 1 {
		head, tail := out[:at[0][0]], out[at[0][1]:]
		num := out[at[0][2]:at[0][3]]
		want, _ := strconv.Atoi(out[at[0][4]:at[0][5]])
		if _, ok := p.cuts[num]; !ok && len(prev) > len(head)+len(tail) &&
			strings.HasPrefix(prev, head) && strings.HasSuffix(prev, tail) {
			mid := prev[len(head) : len(prev)-len(tail)]
			if strings.Count(mid, "\n") == want && !prof.truncated.MatchString(mid) {
				if p.cuts == nil {
					p.cuts = map[string]string{}
				}
				p.cuts[num] = mid
			}
		}
	}
	return prof.truncated.ReplaceAllStringFunc(out, func(ph string) string {
		if mid, ok := p.cuts[prof.truncated.FindStringSubmatch(ph)[1]]; ok {
			return mid
		}
		return ph
	})
}

// fill replaces the placeholders of rule, profile.pastes[r], in out with
// the pastes behind them. draft is the text as read, before any rule
// filled it. The caller holds p.mu.
func (p *pasteTracker) fill(out, draft string, r int, rule pasteRule, used []bool, held map[int]pasteKey) string {
	at := rule.placeholder.FindAllStringSubmatchIndex(out, -1)
	ms, fills, order := make([][]string, len(at)), make([]string, len(at)), make([]int, len(at))
	got := make([]int, len(at)) // the paste behind each placeholder, or -1
	for k, loc := range at {
		m := make([]string, len(loc)/2)
		for g := range m {
			if loc[2*g] >= 0 {
				m[g] = out[loc[2*g]:loc[2*g+1]]
			}
		}
		ms[k], fills[k], order[k], got[k] = m, m[0], k, -1
	}
	if rule.rank != nil {
		slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(rule.rank(ms[a]), rule.rank(ms[b])) })
	}
	for _, k := range order {
		id := rule.id(ms[k])
		key := pasteKey{r, id}
		if mid, ok := p.cuts[id]; ok {
			// Claude Code shows a cut middle as a paste after an editor
			// round trip.
			fills[k] = mid
			continue
		}
		for i, text := range p.pastes {
			if n, ok := held[i]; used[i] || p.spent[i] || (ok && n != key) || strings.Contains(draft, text) ||
				(rule.collapses != nil && !rule.collapses(text, p.heights[i])) || !rule.fits(ms[k], text) {
				// Already paired, or spent, or another placeholder's paste,
				// or a paste the agent took in as typed text, or turned into
				// something else, such as an image, or not this one by its
				// count.
				continue
			}
			used[i], got[k] = true, i
			if p.nums == nil {
				p.nums = map[pasteKey]int{}
			}
			p.nums[key] = i
			fills[k] = text
			break
		}
	}
	if rule.reuses {
		p.reuse(draft, r, rule, ms, order, got, fills, used, held)
	}
	var b strings.Builder
	last := 0
	for k, loc := range at {
		b.WriteString(out[last:loc[0]])
		b.WriteString(fills[k])
		last = loc[1]
	}
	b.WriteString(out[last:])
	return b.String()
}

// reuse finds the placeholders a reusing agent gave again (pasteRule.reuses).
// Codex labels a paste with the plain label when no paste of its size is
// left in the box, else with one number above the highest still there. So
// the labels in the box show their pastes in the order the pastes came,
// and a label still held by a paste stands for a newer one when that label
// was deleted and the newer paste took it: a paste that came after the
// last read that showed the label, and that got no label of its own in
// this one. Such a paste takes the label, the newest the highest, and the
// older paste is spent. From the screen alone this reads the same as that
// newer paste deleted before the read; the rule takes the newer text, as
// Codex would have, when a label was deleted and pasted again. The caller
// holds p.mu.
func (p *pasteTracker) reuse(draft string, r int, rule pasteRule, ms [][]string, order, got []int, fills []string, used []bool, held map[int]pasteKey) {
	// free reports whether paste i is paired with no placeholder and was
	// collapsed into one.
	free := func(i int) bool {
		_, ok := held[i]
		text := p.pastes[i]
		return !used[i] && !p.spent[i] && !ok && !strings.Contains(draft, text) &&
			(rule.collapses == nil || rule.collapses(text, p.heights[i]))
	}
	upper := len(p.pastes)
	for n := len(order) - 1; n >= 0; n-- {
		k := order[n]
		key := pasteKey{r, rule.id(ms[k])}
		if h, ok := held[got[k]]; ok && h == key {
			i := got[k]
			for j := upper - 1; j >= p.shown[key]; j-- {
				if free(j) && rule.fits(ms[k], p.pastes[j]) {
					if p.spent == nil {
						p.spent = map[int]bool{}
					}
					p.spent[i], used[j], got[k] = true, true, j
					p.nums[key], fills[k] = j, p.pastes[j]
					break
				}
			}
		}
		if got[k] >= 0 {
			upper = min(upper, got[k])
			if p.shown == nil {
				p.shown = map[pasteKey]int{}
			}
			p.shown[key] = len(p.pastes)
		}
	}
}
