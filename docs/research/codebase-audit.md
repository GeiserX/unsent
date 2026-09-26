# unsent codebase audit (verified): what must change to support many agents

Lane: `codebase-audit`. Source: `.omc/spec/research/codebase-audit.json`, checked against the repo at `064c7f4` (HEAD, clean tree) and against the verified Claude Code profile (`.omc/spec/verified/claude.md`).

Tags on every claim:
- **measured**: someone ran it. "(re-run)" means the verifier reproduced it this pass. "(no capture)" means the lane reports it and no capture survives.
- **source**: read in the repo at `064c7f4`, or in the Claude Code binary as cited by the claude lane.
- **docs**: an external doc.
- **guess**: inference, not verified.

## Summary

Only a few pieces of unsent are specific to Claude Code: the box reader, the box cap, the wrap and un-wrap rule, the paste placeholder regex, the delete-key byte list, and the binary-name lookup [source]. The `view` struct is already agent-neutral and can be the reader interface [source], so the refactor is small: one `profile` struct holding those values, with no behaviour change [guess, design].

The load-bearing measurements, all reproduced this pass:
1. CI takes about 10 minutes because `-race` and `-coverprofile` run in the same step, which makes even the 4-seed fuzz about 8x slower [measured (re-run)]. Splitting it into a race run without the fuzz and a coverage run without race fixes it [measured (re-run)].
2. The fuzz counts a save as exact when `strings.Fields` matches, so it cannot see line-break errors. The byte-exact rate is 61.31%, and every miss is a lost line break [measured (re-run)].
3. The fuzz simulator hangs forever once blank lines are fuzzed [measured (re-run)]. With the lane's patches, blank-line edits push the repeated-words rate to 96.27%, under its 0.97 floor [measured, capture `/tmp/unsent-audit-p1.txt`, not re-run].

Claude Code's terminal modes (kitty keyboard flags, modifyOtherKeys) are **contested**: this lane saw them pushed at the trust dialog on the real config, and the claude lane saw no push at the main prompt with a throwaway config. The code that pushes them exists in the binary [source, claude lane].

Plan order, smallest first: CI split, honest fuzz metrics, profile extraction, agent override and agent field, key decoding (after measuring), notice after exit, line-break memory, restore-in-box, real-app harness, second agent.

---

## 1. What is Claude-specific today

| Fact | Where | Tag |
|---|---|---|
| Binary lookup by basename, only `"claude"`, so `npx …`, `node cli.js` and renamed binaries get no reader | `extract.go:38-45` `extractorFor` | source |
| Box shape: `─` rule directly above the marker row and a rule below; `❯` or `!` marker plus space or U+00A0; 2-column indent (`textFrom(2)`); blank or faint after column 2 on a one-row box means empty | `extract.go:58-104` `claudeBox`, `hasMarker` | source |
| Wrap width `cols-4` | `extract.go:78` | source |
| Box cap `rows/2-6`, one below the measured `rows/2-5` on purpose | `extract.go:106-116` `rows2cap` | source |
| Un-wrap rule: word wrap, the swallowed space goes back; a full row with no space is a long word broken at `width`; markdown block starts (`- * + > # \|`, `1.`, fences) force a break | `stitch.go:357-394` `unwrap`, `startsBlock`; long-word edge logic `stitch.go:64-72` | source |
| Paste placeholder `[Pasted text #N +M lines]` / `[Pasted text #N]` | `paste.go:16` (lane said :15), `expand` at `paste.go:105` | source |
| Claude Code's own placeholder regex also has `[Image #N]`, `[Audio #N]`, `[...Truncated text #N +M lines...]`, which unsent does not know | claude lane, binary strings | source |
| Delete keys as raw bytes: one char `0x7f 0x08 ESC[3~`; any amount `0x17 0x15 0x0b 0x1f`; after the cursor `ESC[3~ 0x0b 0x1f` | `wrap.go:503-529` | source |
| Missing delete keys: Ctrl+D (`0x04`, one char ahead), `ESC d` (word ahead); `ESC 0x7f` counted as one char but kills a word | claude lane, binary keybinding tables | source |
| Ctrl+Z is seen only when a stdin read is exactly the one byte `0x1a` outside a paste | `wrap.go:115` | source |
| Synchronized output `?2026h/l` | markers `wrap.go:215-216`, `trackFrames` `wrap.go:272-285` (lane cited 212-222). Harmless for an agent that never sends them | source |
| Recovery notice printed to stderr before the agent starts | `main.go:198-217` `noticeOrphans`, called at `wrap.go:52` | source |
| Restore = clipboard only (prints the draft if there is no clipboard) | `main.go:168-179` | source |
| The fake agent is shaped like Claude Code and reached through a `claude` symlink on PATH | `wrap_test.go:18-104`, symlink `wrap_test.go:118` | source |
| The fuzz simulator is the Claude wrap model (`boxSim.rows`, `wrapText`); the cap rule is repeated in `boxSim.view` (`capped: h >= b.cap-1`) | `stitch_test.go:17-123`; cap at `stitch_test.go:118` (lane said 639, which is `wrapText`) | source |
| Package doc names Claude Code | `main.go:1-3` | source |

**Already agent-neutral** [source]: `view` (`extract.go:10-32`: `rows, cursor, cursorEnd, width, capped, empty, deleted, deletedAhead`) is the reader interface; the stitcher only consumes it. `screen.go`, `store.go`, the pty passthrough, signals, suspend, the frame wait, `keepOld` and `lostChars` do not depend on the agent. The record keeps `Command` but no agent name (`store.go:25-40`).

### Claude Code 2.1.282 start-up: what was seen, and the conflict

This lane's run [measured (no capture); the lane removed its scratch dir]: private tmux socket `-f /dev/null`, fresh empty dir, `script -q raw.log claude` on the **real** Claude config, 100x30, only Escape sent.
- First bytes: `ESC[?2004h` (bracketed paste), `ESC[?2031h`, `ESC[?1004h` (focus), `ESC[<u` then `ESC[>5u` (kitty keyboard push, flags 5 = disambiguate + report alternate keys), `ESC[>4;2m` (modifyOtherKeys level 2). On exit it popped them and turned mouse modes off.
- The workspace **trust dialog** comes before the box, on the normal screen (no `?1049h` in that run). It draws a rule and `❯ No, exit` / `Yes, I trust this folder`.
- Esc on the trust dialog exits Claude Code.

The claude lane's run [measured, capture `raw-start.bin`]: tmux, a throwaway `CLAUDE_CONFIG_DIR`, main prompt reached. Start-up had `?2004h`, `?2031h`, `?1004h`, `?1049h` and mouse modes, a kitty **query** `ESC[?u` and two `ESC[>4m` **resets**, and **no** `>5u` push and no `>4;2m`.

What that leaves:
- Bracketed paste (`?2004h`) at start-up: seen by both lanes [measured].
- Alternate screen for the main prompt: `?1049h` in the claude lane's capture [measured], so the recovery notice printed before start-up is hidden, as CLAUDE.md says.
- Kitty / modifyOtherKeys push: the binary has code that pushes `CSI <u` + `CSI >5u` (or `>1u`) + `CSI >4;2m`, gated on an extended-keys capability with a terminal allow-list that includes `WarpTerminal` and `tmux` [source, claude lane]. One run saw it at the trust dialog, one run did not see it at the main prompt. **Whether the main prompt pushes it in the user's terminal (Warp) on the real config is unmeasured.** The lane's summary stated it as a fact for "the first frame"; that is downgraded here.
- Trust dialog vs `claudeBox`: `claudeBox` needs a rule on the row directly above the marker row and a rule below it (`extract.go:62-77`). The lane reports the trust dialog does not match [source + measured (no capture)]. There is no captured trust screen to turn into a negative fixture yet.

**Consequence if the push reaches the user's terminal** [docs + guess]: unsent passes the agent's output to the real terminal, so the terminal switches its keyboard encoding and unsent's stdin receives the new forms. Under the kitty protocol with the disambiguate flag, Ctrl+letter keys arrive as `CSI <code>;5u` (Ctrl+W = `ESC[119;5u`, Ctrl+Z = `ESC[122;5u`) [docs, https://sw.kovidgoyal.net/kitty/keyboard-protocol/]. Then `deleteKeys` misses Ctrl+W/U/K/_ (a smaller delete budget for the stitcher; `keepOld` still keeps the no-loss promise) [source + guess], and the suspend path at `wrap.go:115` never fires, so Ctrl+Z goes to the agent instead [guess].

---

## 2. Tests: what they cover and what they miss

### The fuzz is blind to line breaks [measured (re-run)]

`checkKeys` counts a save as exact when `slices.Equal(strings.Fields(got), strings.Fields(r.sim.text))` (`stitch_test.go:279`) [source]. Line breaks and blank lines never count.

Re-run this pass (30 seeds, unique words, "exact" mode, a byte-exact counter added in a scratch copy `/tmp/unsent-verify.L1la`):

```
word-exact 11813 byte-exact 7243 (61.31%) misses: ambiguous-full-unwrap 4308 lost 4496 extra 0 blank 74 other 0
```

- Every miss has fewer line breaks than the truth (4496, plus 74 saves taken while the true text held the transient `\n\n` of the "Enter, then a pause" step). None has an extra break. Same total as the lane (4570).
- **Cause** [measured (re-run)]: in 4308 of the 4570 misses (94%), `unwrap(wrapText(truth, width), width) != truth`, so even a perfect view of the whole draft cannot give the line breaks back. That is the documented ambiguity (`stitch.go:363-366`; test "full row then hand newline joins (known limit)", `stitch_test.go:604`) [source]. The remaining 262 are consistent with the lane's "the error sticks" (a break lost earlier, kept after the layout changed) [guess, not traced].
- Sample miss (lane capture `/tmp/unsent-audit-p0.txt`): got `… w128 w129`, want `… w128\nw129`.
- **Caveat** [guess]: 61% is a figure for the fuzz model, not for real typing. The fuzz inserts `"\n"+word` in one step, so no save ever sees the empty row between Enter and the next word. A person typing, with saves every 400 ms, often does produce that intermediate view. Line-break memory (plan step 7) depends on exactly that.
- This contradicts the "text in view is exact" line in CLAUDE.md's promise for line breaks: words in view are exact, line breaks at a full row are not [measured (re-run) + source].

### Blank lines are never fuzzed, and the simulator hangs on them [measured (re-run)]

- The fuzz inserts `"\n"+word`, or `"\n"` then a word after one save (`stitch_test.go:384-399`), never `"\n\n"` [source]. A blank row exists only for one save, with the cursor on it, in the "Enter, then a pause" step, and `deleteWord` never empties a line (`stitch_test.go:148-158`) [source].
- Re-run: adding `sim.insert("\n\n"+word(n))` to case 3 hangs until the timeout; the stack ends in the "scroll up to see an edit" loop in `checkKeys` (`stitch_test.go:264-266`). `boxSim.moveRows` (`stitch_test.go:161-179`) leaves `b.cur` unchanged when the target row has no word end (a blank row), so `for r.sim.moveRows(-1); r.sim.cur > 0 && r.sim.rowOf() > 0; …` never advances. **A test-harness bug, not production.**
- With two scratch patches (the cursor may sit on a blank row; no leading space when inserting at a line start) at 30 seeds [measured, capture `/tmp/unsent-audit-p1.txt`; not re-run this pass]:
  - unique words: exact/window 100%, unlimited 97.61%; **the strict no-loss check held**.
  - repeated words: `11923 of 12385 saves exact (96.27%)`, **below the 0.97 floor: FAIL**. The same seeds without blank lines give 99.43% (`/tmp/unsent-audit-p0.txt`).
  - byte-exact about 60% (capture: 60.18% over all modes; the lane's 59.68% is exact mode only). The lane's breakdown (158 wrong blank-line count, 4410 lost break, 92 extra, 71 other) has no surviving capture [measured (no capture)].
- **Caveat** [guess]: the 96.27% depends on how often the patched fuzz picks a blank-line edit and on the lane's simulator patch. It shows the floor can fail on blank lines; it is not a real-world rate.

### Fake agent [source]

`fakeAgent` (`wrap_test.go:28-104`) redraws the whole screen on every read, treats `ESC\r` as a newline and a paste with more than 2 `\r` as a placeholder. It never scrolls (no cap), never sends `?2004h`, `?2026h` or kitty flags, and has no trust dialog. Good for passthrough and store tests; it pins nothing per agent.

### CI time [measured (re-run)]

CI run 36228371761 (main, `064c7f4`), step times from the GitHub API:

| Step | macOS | ubuntu |
|---|---|---|
| `go test -race -count=1 -coverprofile=coverage.out ./...` | 497 s | 512 s |
| `go test -count=1 -run Fuzz ./...` (50 seeds) | 89 s | 109 s |
| Whole run (jobs in parallel) | 07:58:11 → 08:08:54, about 10.7 min | |

Local (Apple silicon, Go 1.27.1). Under `-race`, `fuzzSeeds()` drops to 4 (`stitch_test.go:444-456`) [source].

| Run | Time | Who |
|---|---|---|
| `go test -race` (no coverage), whole package | 77 s; the two fuzz tests 29 s | lane, `/tmp/unsent-audit-race.json` |
| `go test -race -coverprofile`, whole package | 275 s; fuzz 227 s (147.5 + 79.8); coverage 94.2% | lane, `/tmp/unsent-audit-racecov.json` |
| `TestStitchFuzzRepeatedWords`, `-race`, 4 seeds | 10.7 s | re-run |
| same, `-race -coverprofile` | 83.8 s (7.8x) | re-run |
| same, no race, `-covermode=atomic`, 150 seeds | 76.3 s (the no-coverage figure is 44.9 s, lane `/tmp/unsent-audit-fuzz.json`) | re-run |
| `go test -race -skip Fuzz ./...` | 33 s (lane: 45 s) | re-run |
| `UNSENT_FUZZ_SEEDS=50 go test -coverprofile ./...` (no race) | 87.5 s, coverage 94.2% | re-run (lane: 93 s) |

- **Cause** [measured (re-run) + docs]: with `-race`, `go test` sets `-covermode=atomic` (`go help testflag`). Atomic coverage alone costs about 1.7x on the fuzz; race alone about 5x; together they multiply to about 8x over race alone. The lane's "the counters sit in `align`'s O(n·m) inner loop" is a plausible location [guess, not profiled].
- The store tests are slow on real fsyncs: `TestStoreArchiveAndPrune` 8.4 s and `TestStoreVersionsGlobalCap` 8.4 s under race, writing past `historyLimit = 500` and `versionsLimit = 300` (`store.go:15,21`) [measured by the lane + source].
- **Fix** [measured (re-run)]: step A `go test -race -count=1 -skip Fuzz ./...` (only the two `TestStitchFuzz*` tests match `Fuzz`); step B `UNSENT_FUZZ_SEEDS=50 go test -count=1 -coverprofile=coverage.out ./...`, uploading coverage from B. B replaces today's separate no-race fuzz step, which already exists. Locally about 120-140 s against 275 + 58 s today. A CI estimate of about 4 minutes per job (the jobs already run in parallel per OS) is extrapolated from the CI-to-local ratio [guess]. Optional: make the two store caps package variables so the store tests use 20 (saves about 16 s under race) [guess].

---

## 3. Save and recovery flow today [source]

`wrap()` → `noticeOrphans` (stderr, before the pty starts, `wrap.go:52`) → pty + raw mode → `session.save` every 400 ms (`saveInterval`, `wrap.go:25`), waiting up to 100 ms for an open `?2026` frame (`maxFrameWait`, `wrap.go:222`) and 150 ms quiet after a resize (`resizeQuiet`, `wrap.go:297`) → read the screen → `deletes.recent()` → `stitch.update` → `pastes.expand` → `keepOld` (archive / keepVersion) → `store.write` (fsync file + dir). On exit (`finish`, `wrap.go:379`) an empty draft removes the file; a non-empty one stays as an orphan. Liveness comes from an `flock` (`store.go:115,139`). `restore` copies to the clipboard, then archives and removes the draft file (`main.go:168-179`).

Gaps for many agents:
- `noticeOrphans` and `show`/`restore` pick drafts by **cwd only** (`main.go:152,203`), so a Codex draft would be offered inside a Claude Code session.
- The record has no `Agent` field (`store.go:25-40`).

---

## 4. Known open items

1. **Restore-in-box** (paste the recovered draft into the empty box, never send it). Not implemented; restore is clipboard only [source]. Safety requirements [guess, design, derived from the code and the measurements]:
   - Inject only after the reader returns `ok && v.empty` for several consecutive saves, after an agent-specific settle delay, and only if the user has typed nothing. The settle delay is config-dependent: CLAUDE.md's ~9 s Esc+Enter warm-up was measured on the real config, the claude lane saw about 1.2 s on a clean config [measured (no capture), claude lane]; whether a paste has the same warm-up is unmeasured.
   - Inject only if the agent turned on bracketed paste: track `?2004h`/`?2004l` in the output the way `trackFrames` tracks `?2026`. Both lanes saw Claude Code send `?2004h` at start-up [measured]. Without it a newline in the draft is Enter, and Enter submits.
   - Strip every `ESC[201~` (ideally every ESC) from the draft before wrapping it in `ESC[200~ … ESC[201~`, or an embedded end marker followed by `\r` would submit.
   - Feed the injected text to `pasteTracker` too (for example `s.pastes.feed(wrapped)`): unsent's tracker only sees stdin (`wrap.go:122`), and Claude Code turns a paste of **4+ lines or more than 800 characters** into a placeholder [measured, claude lane; the lane said 6+, from the older CLAUDE.md line]. Without the tracker entry the new session saves the placeholder instead of the text.
   - Archive the orphan only after a save of the new session **verifies** the box holds it (expanded draft equals the orphan draft). Otherwise leave it and keep the notice.
   - Never fire inside the trust dialog or another `❯` menu. Capture the trust screen and add it as a `claudeBox` negative fixture (no capture exists yet).
   - Newline encoding inside the paste (`\n` vs `\r`) for Claude Code: unmeasured.
2. **Line breaks and blank lines.** `064c7f4` fixed the "Enter, then a pause" double break (`stitch.go:93-110`) [source]. Still open: blank-line edits are not fuzzed, the simulator hangs on them, and once patched the repeated-words floor fails [measured]. Separately, 39% of fuzz saves lose a single break because of the un-wrap ambiguity [measured (re-run)], a fuzz-model rate (section 2 caveat).
3. **Recovery notice hidden** by the alternate screen [measured, claude lane `raw-start.bin` has `?1049h`]. Cheapest fix: print the notice again after the agent exits when an orphan for this cwd/agent was not restored [guess, design]. Restore-in-box makes it mostly redundant.
4. **CI about 10 minutes.** Root cause measured (section 2).

---

## 5. Refactor plan, smallest first [guess, design; each step one PR, `go test` green]

1. **CI split (yaml only).** Step A and B from section 2; drop the separate fuzz step. Optional store-cap package vars.
2. **Honest fuzz (tests only).**
   - Fix `boxSim.moveRows` for blank rows and `insert` at a line start.
   - Add `"\n\n"+word` edits.
   - Add a byte-exact counter with its own floor, starting at what it measures now (about 0.60), so it can only go up.
   - Prove it can fail: a mutation that drops `\n` in `unwrap` must turn it red.
   - Decide the repeated-words floor: re-tune to the measured value with blank lines, or fix the stitcher first (step 7). Steps 2 and 7 are coupled.
3. **`profile` extraction (no behaviour change).** A new `agent_claude.go`:
   ```go
   type profile struct {
       name        string
       names       []string                  // argv0 basenames
       read        extractor                 // claudeBox
       unwrap      func([]string, int) string // today's unwrap
       placeholder *regexp.Regexp            // paste.go:16
       keys        keyset                    // oneChar, many, ahead [][]byte (wrap.go:507-509)
       restore     restoreCaps               // needs2004, settle time.Duration
   }
   ```
   `session` takes `prof *profile`; `pasteTracker.expand` and `deleteLog.add` take their tables from it; `extractorFor` becomes `profileFor`; `view` stays the reader interface. Update CLAUDE.md's "adding an agent = one reader in extract.go" to "one profile file plus fixtures".
4. **Agent identity.** `UNSENT_AGENT=<name>` (or `unsent --as <name> <cmd…>`) for `npx`/`node` launches; an `Agent` field in `record`, falling back to `filepath.Base(Command[0])` for old files; filter `noticeOrphans`/`restore` by cwd **and** agent.
5. **Key encodings.** Measure first: a `script` log of stdin in Warp and one kitty-protocol terminal (Ghostty, kitty, WezTerm) with the real config. Then decode kitty `CSI <cp>;<mods>u` and modifyOtherKeys `CSI 27;<mods>;<cp>~` for Ctrl+W/U/K/_/Z/H/D before matching, detect Ctrl+Z anywhere in a read (not only `n == 1`), and add Ctrl+D and `ESC d`.
6. **Notice after exit.** A few lines in `wrap()`/`finish`.
7. **Line-break memory in the stitcher.** When `unwrap` guesses at a junction (the full-row case, `stitch.go:379-382`) and the known text already has the same two words with a `\n` between them, keep the `\n`. The byte-exact counter from step 2 measures the gain. Untried; the gain is a guess, and in the current fuzz (which never shows the intermediate empty row) it may be near zero.
8. **Restore-in-box** with the safety list in section 4.1: `?2004` tracking next to `trackFrames`, a restore state machine in `session.save`, fake-agent support for `?2004h` plus a trust-dialog screen. Tests: a draft containing `ESC[201~\r` never submits; an unverified restore leaves the orphan; typing before the box settles cancels the restore.
9. **Real-app harness (opt-in, not in CI).** Private `tmux -L` socket, a throwaway `CLAUDE_CONFIG_DIR` (the claude lane used one), profile-driven key scripts, `UNSENT_DEBUG_DIR` capture, ground truth from Ctrl+G with `EDITOR` set to a copy script, quit only with Ctrl+C on an empty box, and a negative control.
10. **Second agent.** A profile plus `testdata/<agent>/*.txt` captures (the `screenFromText` format), the fake agent parameterized as `UNSENT_FAKE_AGENT=<profile>`, and `wrapText`/`boxSim` taking the profile's wrap rule so the fuzz runs once per profile. The simulator's `jump` scroll mode should also come from the profile: the claude lane saw one-row scrolling with the cursor held mid-box, which conflicts with CLAUDE.md's "jumps" [measured (no capture), claude lane].

---

## Risks

- Restore-in-box can submit a prompt if the agent has not turned on bracketed paste, or if the draft contains `ESC[201~` followed by a newline. Gate on `?2004` and sanitise [guess, design].
- Restore-in-box can type into the trust dialog or another `❯` menu if the empty-box test ever gives a false positive. Needs a captured negative fixture [guess].
- If Claude Code pushes the kitty flags in the user's terminal, Ctrl+Z suspend (`wrap.go:115`) and many-char delete detection (`wrap.go:507-529`) silently stop working. `keepOld` keeps the no-loss promise; stitching gets less exact [source + guess; the push itself is contested].
- The fuzz floors cannot catch line-break or blank-line regressions: the green gate cannot fail on spacing [measured (re-run)].
- Fixing `boxSim` for blank lines fails `TestStitchFuzzRepeatedWords` at once (96.27% vs 0.97) [measured, capture], so plan steps 2 and 7 are coupled.
- Adding an `Agent` field and filtering by it changes which folder sessions see old drafts; the fallback derives it from `Command[0]` [guess].

## Open questions

1. Restore-in-box: on by default or opt-in? Only drafts from the same agent, or any agent in the same folder?
2. Does Claude Code's main prompt push kitty / modifyOtherKeys in Warp on the real config? One run saw the push at the trust dialog, one did not see it at the main prompt. Highest priority before touching `deleteKeys`.
3. Newline inside a bracketed paste for Claude Code (`\n` or `\r`), and does a paste in the first seconds get dropped the way early Esc+Enter does?
4. Real-app harness: a throwaway `CLAUDE_CONFIG_DIR` holding auth, so the harness never touches the real config.
5. Byte-exact floor and the repeated-words floor after blank lines: re-tune to measured values, or fix the stitcher first?
6. The lane also carried an unrelated relayed request (delete a "context or weekly" warning). Out of scope here; route it separately.

---

## Verification notes

What I checked:
- **Every source citation** in section 1 against `064c7f4` (clean tree). Fixed line numbers: `paste.go:15` → `:16`; `trackFrames` is `wrap.go:272-285`, not 212-222; `capped: h >= b.cap-1` is `stitch_test.go:118`, not 639; `extractorFor` 38-45; the full-row case is `stitch.go:379-382`.
- **CI step times** via the GitHub API for run 36228371761: 497 s / 512 s for race+coverage, 89 s / 109 s for the fuzz step. Matches the lane.
- **Race + coverage slowdown**, re-run in a clean `git archive` copy: `TestStitchFuzzRepeatedWords` 10.7 s under `-race`, 83.8 s under `-race -coverprofile`. Added a control the lane lacked: atomic coverage without race is 1.7x, so the 8x comes from the combination.
- **Proposed split**, re-run: `-race -skip Fuzz` 33 s; `UNSENT_FUZZ_SEEDS=50 -coverprofile` 87.5 s at 94.2% coverage. Confirmed `-skip Fuzz` skips only the two `TestStitchFuzz*` tests.
- **Byte-exact rate**, re-run with my own counter: 7243/11813 = 61.31%, identical to the lane; every miss is a lost break. Added a cause check the lane lacked: 94% of misses are un-wrap ambiguous even from a full view of the draft.
- **Blank-line hang**, re-run: reproduced; the stack ends in the scroll-up sweep of `checkKeys`, matching the `moveRows` explanation.
- **Lane evidence files** in `/tmp`: `unsent-audit-race.json`, `-racecov.json`, `-fuzz.json`, `-p0.txt`, `-p1.txt`, `-hang.txt` exist and match the quoted numbers. The scratch copy `/tmp/unsent-audit` and the Claude `raw.log` do not survive.
- **Cross-checked** the Claude Code facts against `.omc/spec/verified/claude.md`.

What I changed:
- **Kitty / modifyOtherKeys push**: downgraded from "Claude Code turns them on at its first frame" to contested. This lane saw them at the trust dialog (no capture); the claude lane's capture of the main prompt has no push. The code exists in the binary with an allow-list that includes Warp and tmux.
- **Paste placeholder threshold**: corrected from 6+ lines to 4+ lines or more than 800 characters (claude lane, measured).
- **Settle delay**: the ~9 s is config-dependent (about 1.2 s on a clean config); marked as such.
- **Byte-exact 61%**: kept, and added that it is a fuzz-model rate (the fuzz never shows the empty row between Enter and the next word), that it contradicts CLAUDE.md's "text in view is exact" for line breaks, and that plan step 7 may gain little inside the current fuzz.
- **96.27% floor failure and the blank-line error breakdown**: kept as measured from the lane's capture (96.27% is in `p1.txt`); the per-category breakdown has no capture and is marked so; byte-exact with blank lines corrected to about 60% (60.18% in the capture across modes).
- **Local timings**: kept the lane's figures with their capture files, and added the re-run figures (45 s → 33 s, 93 s → 87.5 s for the split steps).
- **Mechanism of the slowdown** ("atomic counters in `align`'s inner loop"): marked guess; not profiled.
- **Missing delete keys** (Ctrl+D, `ESC d`, `ESC 0x7f`) and Claude Code's extra placeholder kinds: added from the claude lane.
- **Simulator scroll mode**: noted the conflict between CLAUDE.md's "jumps" and the claude lane's one-row scrolling, relevant to `boxSim.jump`.
- **Refactor plan**: kept as design (tagged guess), with key decoding reordered to measure in the user's terminal first.

Scratch copies from this pass (probe patches in `stitch_test.go`, repo untouched): `/tmp/unsent-verify.L1la`, `/tmp/unsent-verify2.*`.
