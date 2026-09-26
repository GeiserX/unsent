# Claude Code 2.1.282: verified input-box profile for unsent

Lane: `claude`. Source lane output: `.omc/spec/research/claude.json`. Captures live in `/tmp/unsent-spec-claude/` (scratch, not durable: copy what matters before `/tmp` is cleaned).

**Tags.** `measured` = seen on screen or in bytes, with the capture file named. `measured (no capture)` = the lane reports it, but no capture survives to re-check. `source` = read in the 2.1.282 binary's JS (strings dump `/tmp/unsent-spec-claude/strings.txt`). `docs` = public docs. `guess` = inference.

**Environment of every measurement.** tmux 3.7c at 120x40, `env -i` with `TERM=xterm-256color`, throwaway `CLAUDE_CONFIG_DIR`, a dummy `ANTHROPIC_API_KEY` (so a 401 banner on screen), `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`, `DISABLE_TELEMETRY=1`, no plugins or hooks, empty non-git cwd (`start.sh`). The `TMUX` variable was stripped by `env -i`. Anything about hints, start-up timing or keyboard protocol may differ in the maintainer's real terminal and config. Nothing was submitted.

## 1. The box

| Fact | Tag | Evidence |
| --- | --- | --- |
| Two full-width `─` (U+2500) rules, SGR `38;5;244`; pink `38;5;211` in shell mode | measured | `cap-empty.txt`, `cap-shell.txt` |
| First row `❯` + U+00A0 + text; shell mode `!` + U+00A0 | measured | `cap-keys.txt`, `cap-shell.txt` |
| Continuation rows indented 2 columns | measured | `cap-keys.txt` |
| Empty box = `❯\xa0` + a reverse-video space (`ESC[7m ESC[0m`), a fake cursor drawn by Claude Code | measured | `cap-empty.txt`, `cap-escesc.txt`, `cap-shell-empty.txt` |
| The fake cursor cell is a space, so `textFrom(2)` trims it and unsent's empty test still passes | source (unsent `screen.go:30`, `extract.go:80`) | |
| No dim `Try "…"` hint appeared in this environment | measured | `cap-empty.txt`, `cap280-empty.txt` |
| The hint exists: `vjr()` returns `Try "fix lint errors"` etc., shown only when `v<1&&!D&&be` | source | strings dump |
| Top rule carries `─── History n/m ───` (label dim) while browsing history | measured | `cap-up2.txt` |
| The label is produced only while the history index is non-zero (`"History"` or `` `History ${n}/${m}` ``) | source | strings dump |
| Footer below the box: `⏸ manual mode on · ? for shortcuts · ← for agents` when empty; `⏸ manual mode on` with a draft; `paste again to expand` after a placeholder paste; `! for shell mode` in shell mode | measured | `cap-empty.txt`, `cap-keys.txt`, `cap-paste4.txt`, `cap-shell.txt` |
| A right-aligned dim hint row sits above the top rule at times (`ctrl+g to edit in Grab-editor.sh`, `◐ medium · /effort`) | measured | `cap-up2.txt`, `cap280-empty.txt` |
| The slash menu is drawn above the top rule; the box stays intact and holds `/mode` as the draft | measured | `cap-slash.txt` |
| Ctrl+R replaces the box with a search panel; its list `❯` is indented 2 columns, so `hasMarker` does not match and the reader returns not-ok | measured | `cap-ctrlr.txt` |

The empty-box hint is **not** proven stale. CLAUDE.md's dim hint was measured on the maintainer's real config; this lane ran with non-essential traffic disabled and a dummy key, and the hint's gate (`be`) was not decoded. The reader must keep handling both shapes: blank after the marker, and all-dim after the marker. It does today.

Key captures (`capture-pane -p -e`, row index, runs of `─` collapsed):

```
# empty (cap-empty.txt)
36 '\x1b[38;5;244m─x120'
37 '\x1b[39m❯\xa0\x1b[7m \x1b[0m'
38 '\x1b[38;5;244m─x120'
39 '\x1b[39m  \x1b[38;5;246m⏸ manual mode on · ? for shortcuts · ← for agents\x1b[39m'
# draft built with Esc+Enter, \+Enter, Ctrl+J, CSI 13;2u (cap-keys.txt)
32 '\x1b[38;5;244m─x120'
33 '\x1b[39m❯\xa0do nothing; reply ok (line one)'
34 '  line two'
35 '  line three after backslash-enter'
36 '  after ctrl-j'
37 '  after csi-u shift-enter\x1b[7m \x1b[0m'
38 '\x1b[38;5;244m─x120'
# history browsing (cap-up2.txt)
32 '\x1b[38;5;244m───\x1b[39m \x1b[2mHistory 1/1\x1b[0m \x1b[38;5;244m─x104'
# shell mode (cap-shell.txt)
36 '\x1b[38;5;211m─x120'
37 '!\xa0\x1b[39mecho harmless\x1b[7m \x1b[0m'
# 4-line paste (cap-paste4.txt)
37 '\x1b[39m❯\xa0[Pasted text #1 +3 lines]\x1b[7m \x1b[0m'
39 '\x1b[39m  \x1b[38;5;246mpaste again to expand\x1b[39m'
# 25 typed lines at 40 rows (cap-tall.txt): rows 23..37 = 15 rows, draft lines 11..25
22 '\x1b[38;5;244m─x120'
23 '\x1b[39m❯\xa0row 11'
37 '  row 25\x1b[7m \x1b[0m'
38 '\x1b[38;5;244m─x120'
```

## 2. Screen mode, cursor, frames

- **Alternate screen** [measured, `raw-start.bin`]: start-up bytes are `ESC7 ESC[r ESC8 ESC[?25h ESC[?2004h ESC[?2031h ESC[?1004h OSC0;✳ Claude Code BEL ESC[?1049h ESC[2J ESC[H ESC[?1000h ESC[?1002h ESC[?1003h ESC[?1006h ESC[?25l`. So: bracketed paste, colour-scheme reports, focus events, all-motion SGR mouse tracking, hidden cursor. Onboarding runs on the normal screen [measured (no capture)].
- **Terminal queries** [measured, `raw-start.bin`]: `ESC[?u` (kitty keyboard query) is sent once; `ESC[>4m` (modifyOtherKeys reset) twice. No kitty push (`CSI >1u`/`>5u`) and no `CSI >4;2m` appeared under tmux.
- **Real cursor at the insertion point** [measured, `raw-typing.bin`]: each typing frame ends with a CUP to the insertion point (typing `h` in an empty box ends `ESC[38;4H`, row 38 col 4, 1-based), then the cursor stays hidden (DECTCEM off). CLAUDE.md's "the real cursor sits at the insertion point" holds; it is just invisible.
- **Fake cursor vs real cursor mid-row** [guess]: the lane said the reverse-video cell was drawn at row end "not under the real cursor" after Up. `cap-up.txt` shows Up moving from a 24-char row to a 12-char row, where the cursor clamps to the row end anyway, so the capture does not show a mismatch. Keep reading the real cursor, as unsent does.
- **Synchronized output** [measured, `raw-typing.bin`]: typing 9 keys produced 9 matched `ESC[?2026h … ESC[?2026l` pairs, one per frame. The first paint at start-up is not wrapped (`raw-start.bin`: paint starts straight after `ESC[?25l`). CLAUDE.md's "6 pairs, not one per keystroke" came from a different session shape; on this build and this input, every keystroke frame is wrapped. `maxFrameWait` (100 ms, `wrap.go:222`) still bounds the wait.
- **External editor** [measured (no capture) for the escape sequences; measured `cap-after-editor.txt` for the repaint]: Ctrl+G turns off mouse, focus and bracketed paste, sends `ESC[>4m`, shows the cursor, clears the screen and runs `$EDITOR` on the same alternate screen; on return Claude Code re-sends `ESC[?1049h` and repaints the same draft. Temp file `/tmp/claude-501/claude-prompt-<uuid>.md` [measured (no capture)]. `ctrl+x ctrl+e` and `ctrl+g` are both bound to `chat:externalEditor` [source].

## 3. Pastes

- **Threshold** [measured]: a paste becomes a placeholder at **4+ lines** or **more than 800 characters**.
  - 3 lines stay inline (`cap-paste3.txt`; history entries `a\nb\ncx`, `a\n\nc`).
  - 4, 5, 6 lines become `+3`, `+4`, `+5 lines` (`cap-paste4/5/6.txt`).
  - One-line pastes of 790 and 800 characters stay inline; a longer one becomes `[Pasted text #9]` (history entries of length 790 and 800, then `[Pasted text #9]`). The 801 exact figure is the lane's; the 800-inline side is on disk.
  - 3 lines × 300 characters becomes `[Pasted text #8 +2 lines]` (history).
  - CLAUDE.md's "3 inline, 6+ placeholder" is true but incomplete: 4 and 5 are placeholders too.
- **Format** [source]: `Y9(id,n)` returns `[Pasted text #id]` when `n===0`, else `[Pasted text #id +n lines]`; `n` is `YD()`, the count of `\r\n|\r|\n`, so **M = line breaks, not lines**. unsent's `expand` already accepts either count (`paste.go:119`).
- **Claude Code's own placeholder regex** [source]: `/\[(?:Pasted text #\d+(?: \+\d+ lines)?|Image #\d+|Audio #\d+|\.\.\.Truncated text #\d+ \+\d+ lines\.\.\.)\]/g`. unsent's `pastePlaceholder` (`paste.go:16`) knows only `Pasted text`.
- **Truncation** [source]: when the input value exceeds `1e4` characters (`wFe`), a hook (`Cse` → `CFe`) keeps the first 500 and last 500 characters (`oM=1000`, halved) and replaces the middle with `[...Truncated text #N +M lines...]`, moving the middle into `pastedContents`. This applies to the box value, so it can hit typed text or text returned from `$EDITOR`, not just pastes. Not measured.
- **N is per process** [measured]: history shows `#1`…`#13` in one session; the 2.1.280 run restarts at `#1` (`cap280-draft.txt`).
- **Paste again to expand** [measured, `cp.txt` then `cap-paste-again.txt`]: pasting the same 5-line text a second time turns `[Pasted text #12 +4 lines]` into the inline text, shown once. unsent's tracker then holds two identical pastes; `expand` skips a paste whose text is already in the draft (`paste.go:112`), so this is harmless [source].
- **Backspace removes a whole placeholder** [measured (no capture)].
- **Editor ground truth** [measured (no capture) for the placeholder case]: the lane reports that `before [Pasted text #13 +4 lines] after` came out of Ctrl+G as `before q1\nq2\nq3\nq4\nq5 after`. The surviving `editor-copy.txt` is from a later probe (`editor probe text`, 17 bytes, no trailing newline), which confirms only the no-trailing-newline part.

## 4. Keys

**Newline** [measured, `cap-keys.txt`]: `Esc Enter`, `\` then Enter (backslash removed), Ctrl+J (0x0A) and `CSI 13;2u` all insert a line break. [source] Default Chat bindings: `enter: chat:submit`, `ctrl+j: chat:newline`, `ctrl+enter` and `ctrl+x ctrl+s: chat:sendNow`, `ctrl+x enter: chat:queueSubmit`. Ctrl+J is the safest scripted newline under default bindings; a user remap can change that.

**Warm-up** [measured (no capture)]: Esc+Enter made a new line about 1.2 s after the box appeared, in the clean environment. This does not refute CLAUDE.md's ~9 s: that was measured with the maintainer's plugins and hooks loading. Keep the 9 s wait in real-app tests until it is re-measured on the real config.

**Delete and clear keys** (default bindings):

| Bytes | Action | Tag |
| --- | --- | --- |
| 0x7F, 0x08 | Backspace; a placeholder goes as one unit | source (text input); measured (no capture) for placeholders |
| Ctrl+D (0x04) | Delete forward; exits only when the box is empty (`if(w.text===F&&e==="")…"Ctrl-D"…return w.del()`) | source |
| `ESC[3~` | Delete forward | source |
| Ctrl+K (0x0B) | Delete to line end (`deleteToLineEnd`) | source |
| Ctrl+U (0x15) | Delete to line start; one logical line per press | source; measured (no capture) |
| Ctrl+W (0x17) | Delete WORD before (`deleteWORDBefore`, whitespace-delimited) | source |
| meta/ctrl+Backspace (e.g. `ESC 0x7F`) | Delete word before (`backwardKillWord`) | source |
| meta+d (`ESC d`) | Delete word forward (`killWord`) | source |
| Ctrl+_ (0x1F), Ctrl+- | Undo (`chat:undo`) | source |
| Esc Esc | Clear the box, draft written to `history.jsonl` | measured (`cfg/history.jsonl`: ~20 cleared drafts, nothing submitted) |
| Ctrl+C | Global `app:interrupt`; on a draft it clears the box with no history entry and nothing in the Ctrl+Y ring | source (binding); measured (no capture) for the clear |
| Ctrl+S | `chat:stash`: box empties, `› stashed` hint; Ctrl+S on an empty box restores; memory only | source (binding); measured (no capture), `cfg/.claude.json` has `hasUsedStash` |
| Ctrl+L | Bound to `chat:clearInput`, but did not clear the box | source; measured (`cap-ctrll.txt` shows the draft intact) |
| Ctrl+Y / meta+Y | Yank / yank-pop | source |

**unsent's `deleteKeys` gaps** [source, `wrap.go:507-509`]: `oneCharKeys` = 0x7F, 0x08, `ESC[3~`; `manyKeys` = 0x17, 0x15, 0x0B, 0x1F. Missing: Ctrl+D (0x04, one char ahead), `ESC d` (word ahead), `ESC 0x7F` (counted as one char because it contains 0x7F, but kills a word). The keys that empty the box (Esc Esc, Ctrl+C) need no entry: `keepOld` archives any draft that goes to empty (`wrap.go:542`). The gaps only lower stitch accuracy; the no-loss promise rests on `keepOld`, not on `deleteKeys`.

**Other keys** [source]: Up/Down move within the draft and browse history at the first/last line (`history:previous`/`next`); the draft comes back on the way down [measured, `cap-up2.txt` → lane]. Ctrl+R is `history:search`. Left arrow on an empty box leads to the agents view (footer `← for agents`; `leftArrowDetachAvailable` in the binary).

**Kitty keyboard / modifyOtherKeys** [source, upgraded from the lane's guess]: the binary has `XYe(r){return y()?CSI<u+(r?.legacyKitty?CSI>1u:CSI>5u)+CSI>4;2m:""}`, gated on an `extendedKeys` capability, with a terminal allow-list `["iTerm.app","kitty","WezTerm","ghostty","tmux","windows-terminal","WarpTerminal"]`. So Claude Code **does** carry code that pushes kitty keyboard flags and modifyOtherKeys level 2. The one call site found is an attach path (`Attaching…`); whether the main REPL pushes it in Ghostty, kitty, WezTerm or iTerm2 was not measured, and under tmux nothing was pushed [measured, `raw-start.bin`]. If it is pushed, Ctrl+W/U/K/D/_ arrive as `CSI <code>;5u` (or `CSI 27;5;<code>~`) and unsent's C0-byte matching misses them [guess]. Also present: `CLAUDE_CODE_BS_AS_CTRL_BACKSPACE` env var [source].

**Remapping** [source]: bindings are data (`{context:"Chat",bindings:{…}}`) and user-overridable via `~/.claude/keybindings.json` (`$docs: https://code.claude.com/docs/en/keybindings` in the binary). Every key above is a default, not a fact.

## 5. Height, scrolling, wrapping

- **Cap** [measured, `cap-tall.txt`]: 15 rows at 40 rows = `rows/2 - 5`; same on 2.1.280 [measured (no capture)]. unsent's `rows2cap` uses `rows/2 - 6` on purpose (`extract.go:110`), which errs toward "capped".
- **Scrolling** [measured (no capture) for the step sequence; `c.txt` (2.1.280) shows a mid-scroll view `r8…r22`]: with Up/Down the cursor walks to the middle row, then each further press scrolls the view one row with the cursor held mid-box; no scroll indicator. This conflicts with CLAUDE.md's "the box jumps … it does not scroll one row at a time". The two may describe different moves (line-by-line Up/Down vs large jumps such as start of buffer or a mouse click); treat both as possible until re-measured with a per-step capture. The stitcher must not assume either.
- **Wrap** [measured (no capture); history holds the 150-digit and `word00…` test lines]: rows wrap at `cols-4` (116 at 120 cols); words wrap at spaces; a word longer than the width breaks hard at 116. Matches CLAUDE.md.

## 6. What Claude Code saves on its own

- **Esc Esc** [measured]: writes `{"display":…,"pastedContents":{},"timestamp":…,"project":…,"sessionId":…}` to `$CLAUDE_CONFIG_DIR/history.jsonl`.
- **New finding** [measured, `cfg/history.jsonl`]: every cleared placeholder draft was saved with `pastedContents: {}` and `display` holding only the placeholder (`[Pasted text #4 +11 lines]`, `before [Pasted text #13 +4 lines] after`). So on disk, Claude Code's history does **not** keep the pasted text of a cleared draft. Whether an in-memory or `paste-cache` copy lets Up restore it was not tested (`paste-cache` appears in the binary [source]; no such dir was created).
- **Ctrl+S stash**: memory only [measured (no capture)].
- **The REPL draft is not written to disk while typing** [measured (no capture)]. [source] A debounced draft store exists at `jobs/.draft-<sha256(cwd)[:8]>` (1-day TTL, `l=86400000`), tied to job/launcher drafts (`jobDraft`), not shown to back the REPL box. [source] An in-process composer API exists (`composer:{read:()=>readDraft(),write:…fillDraft,propose:…suggestDraft}`) and a per-session UDS messaging socket (`cc-socks`); no evidence that `readDraft` is reachable from outside.
- **Loss vectors unsent still covers** [measured/guess]: Ctrl+C on a draft, any cleared paste draft (history keeps only the placeholder), a crash or kill, the terminal closing.

## 7. TUI stack

[source] Bun single-file executable (`/$bunfs/root/chunk-*.js`), React rendered through Claude Code's own Ink-derived renderer. Keymap is data and remappable (above).

## 8. Detection recipe for unsent's reader

1. **Is it Claude Code?** Command basename `claude` (as now). Optional confirmation: `OSC 0 ;✳ Claude Code` and `ESC[?1049h` in the first output bytes [measured].
2. **Find the box.** Bottom-up, a row starting with `❯` or `!` followed by U+00A0, a space or end of row, with a rule directly above; then the next rule below. Rule = every cell `─`, length ≥ cols/2. This is exactly `claudeBox` today [source].
3. **Labelled top rule** (`─── History n/m ───`): keep rejecting it. It means the user is browsing history; the reader returns not-ok and the last draft is kept.
4. **Rows.** Strip 2 columns, trim trailing spaces (drops the fake cursor). In shell mode prefix `!`.
5. **Empty.** One row whose text after column 2 is blank or all dim. Cross-check if wanted: the footer row contains `? for shortcuts` only when the box is empty [measured].
6. **Cursor.** Use the terminal cursor even though it is hidden. Never read inside a `?2026` pair; every keystroke frame is wrapped on this build.
7. **Capped.** Rows between rules ≥ `rows2cap()` (keep `-6`).
8. **Width.** `cols - 4`, word wrap, hard break for longer words.
9. **Pastes.** Extend the placeholder regex to Claude Code's own (section 3). `Image` and `Audio` placeholders hold no text to recover. `Truncated` needs the text from typed/pasted input, which unsent may not have (see risks).
10. **Ground truth in tests.** `EDITOR` = a copy script, press Ctrl+G; the copy is the expanded draft with no trailing newline.

## 9. Stability and what would break the reader

**Stable** [measured]: 2.1.280 and 2.1.282 draw the same box: rules, `❯\xa0`, fake cursor, footer, `+M lines`, `paste again to expand` (`cap280-empty.txt`, `cap280-draft.txt` vs `cap-empty.txt`, `cap-paste4.txt`). Only the logo row differs.

**Drift against CLAUDE.md, graded:**

| CLAUDE.md claim | Verdict |
| --- | --- |
| Empty box shows a dim hint | Not reproduced in a clean env; not refuted for the real config. Keep both handled. |
| Real cursor at the insertion point | Holds; the cursor is hidden (measured). |
| 6 sync pairs in one session, not one per keystroke | This build wraps every keystroke frame (measured, 9/9). Update the doc. |
| Box jumps, does not scroll one row at a time | Contradicted for step-by-step Up/Down (lane, no per-step capture). Re-measure. |
| 3 inline, 6+ placeholder | True but incomplete: 4+ lines or >800 chars (measured). Update the doc. |
| ~9 s before Esc+Enter works | Not reproduced in a clean env (~1.2 s); likely config-dependent. Keep the test wait. |

**What breaks the reader** [guess, ranked]:

1. Rule glyph or width changes (rounded box, `╌`, rules not full width).
2. Marker changes (`❯` → `>`, U+00A0 dropped; `hasMarker` already accepts a space).
3. A label written into the top rule in the normal state (today only while browsing history).
4. Continuation indent ≠ 2 or wrap ≠ cols-4: breaks un-wrapping and stitching silently, without failing safe.
5. A higher cap than `rows/2 - 5`: `capped` would read false on a scrolled box (dangerous direction).
6. A renderer that stops parking the terminal cursor at the insertion point.
7. The kitty/modifyOtherKeys push reaching the main REPL in the user's terminal (code exists, section 4).
8. User keybinding remaps.

## Risks

- `pastePlaceholder` misses `[...Truncated text #N +M lines...]`, `[Image #N]`, `[Audio #N]`. A box value over 10,000 characters is shown with its middle collapsed [source]; if that text was typed or came back from `$EDITOR`, unsent's paste tracker never saw it as a paste. The previous full draft is still archived by `keepOld` (the new text shares only 1,000 characters with it, so `similar` fails and it goes to history) [source, `wrap.go:538`], but the current draft saves with the literal placeholder [guess].
- `deleteKeys` misses Ctrl+D, `ESC d`, and undercounts `ESC 0x7F`; under a kitty/modifyOtherKeys push it would miss every Ctrl-key delete. Stitch accuracy only; the promise rests on `keepOld`.
- Ctrl+C and Esc Esc on a paste draft leave no recoverable pasted text on Claude Code's side (history keeps the placeholder only) [measured]. unsent is the only safety net, so the empty-box → history path in `keepOld` must not regress.
- Editing a recalled history entry: the `History n/m` label may stay while the user edits, so unsent saves nothing new until the label goes; a crash then loses the edit [guess; label gating is source].
- All-motion mouse tracking floods stdin with SGR mouse reports (`ESC[<b;x;yM`) [measured, `raw-start.bin`]. These contain only digits, `;`, `<`, `M`/`m`, so they cannot match any `deleteKeys` byte [source]; the lane's "must not count them as keys" is already true. Passthrough is unaffected.
- All measurements used a clean config; the maintainer's real setup may add rows around the box, hints and start-up delay.

## Open questions

1. Does the main REPL push kitty keyboard / modifyOtherKeys in Ghostty, kitty, WezTerm, iTerm2? Measure raw output outside tmux. Highest priority for `deleteKeys`.
2. When does the dim `Try "…"` hint show? Needs the real config (or decoding `be`).
3. Does the `History n/m` label stay after editing a recalled entry?
4. Does the box ever jump on large moves (start/end of buffer, mouse click) as CLAUDE.md says? Needs a per-step capture.
5. Is the ~9 s Esc+Enter warm-up caused by plugins/hooks?
6. Why does Ctrl+L not clear despite `chat:clearInput`?
7. Does Up restore the pasted text of a cleared placeholder draft (in-memory or `paste-cache`), given `history.jsonl` stores `pastedContents: {}`?
8. Is the composer API reachable over the per-session socket?

## Verification notes

**What I checked.**
- Re-read every surviving capture in `/tmp/unsent-spec-claude/` with the lane's `dump.py`: `cap-empty`, `cap-keys`, `cap-up`, `cap-up2`, `cap-shell`, `cap-shell-empty`, `cap-paste3/4/5/6/12`, `cap-paste-again`, `cp.txt`, `c.txt`, `cap-tall`, `cap-escesc`, `cap-ctrll`, `cap-ctrlr`, `cap-slash`, `cap-after-editor`, `cap280-empty`, `cap280-draft`. Box shape, marker, fake cursor, footer, history label, shell mode, paste placeholders, cap of 15 and 2.1.280 parity all match the lane.
- Parsed `raw-start.bin` and `raw-typing.bin`: start-up sequence confirmed byte for byte; 9 of 9 typing frames wrapped in `?2026`; each frame ends with a CUP to the insertion point; no kitty push, one `?u` query, two `>4m` resets.
- Read `cfg/history.jsonl`: confirmed paste thresholds (790 and 800 inline, 3 lines inline, 4+ placeholders, `+M` = breaks) and found `pastedContents: {}` on every placeholder entry.
- Grepped the 2.1.282 strings dump for: the placeholder regex, `Y9`/`YD`, the `wFe=1e4, oM=1000` truncation hook, `vjr` hint and its gate, the Chat and Global keybinding tables, the text-input handlers for Ctrl+D, meta+d, meta/ctrl+Backspace, Ctrl+K/U/W, the `History n/m` label function, `XYe`/`A7n` extended-keys code, `.draft-`, `readDraft`, `cc-socks`, `keybindings.json`.
- Read unsent's `extract.go`, `screen.go`, `paste.go`, `wrap.go` to check every claim about `claudeBox`, `isRule`, `hasMarker`, `rows2cap`, `textFrom`/`faintFrom`, `pastePlaceholder`, `expand`, `deleteKeys`, `keepOld`.
- `start.sh` shows the environment (`env -i`, dummy key, `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`, no `TMUX`), which the lane under-reported.

**What I changed.**
- Downgraded "the empty-box dim hint is stale" to "not reproduced in a clean environment". CLAUDE.md measured it on the real config, and the lane's env disables non-essential traffic.
- Downgraded "no 9 s warm-up" the same way: config-dependent, not a contradiction.
- Downgraded "fake cursor drawn at row end, not under the real cursor" to a guess: `cap-up.txt` shows a clamp to a shorter row, which puts both at the row end.
- Upgraded kitty/modifyOtherKeys from guess to source: the binary has an explicit push (`CSI <u` + `CSI >5u`/`>1u` + `CSI >4;2m`) with a terminal allow-list. Whether the main REPL uses it is still unmeasured.
- Tagged scrolling, wrap, Ctrl+C, Ctrl+S, Ctrl+U, placeholder-backspace, warm-up and the expanded-editor-copy result as "measured (no capture)": the lane reports them, but no capture survives (`editor-copy.txt` was overwritten by a later probe).
- Added a new measured finding: Claude Code's history stores cleared paste drafts with `pastedContents: {}`, so its own history cannot restore their pasted text from disk.
- Corrected the truncation claim's scope: it applies to the box value over 10,000 characters (typed or editor-returned text too), not only to pastes; added how `keepOld` handles it.
- Dropped the mouse-tracking risk to low: SGR mouse bytes cannot match `deleteKeys`.
- Replaced the lane's "drift" list with a graded table against CLAUDE.md.
- Removed the lane's "Heads-up" about a relayed user request ("why do you care about the context or weekly? … delete that warning"). It is unrelated to this profile; the lane did not act on it, and the orchestrator should handle it on the main thread.

**Not done (rules of this task).** No agent CLI was launched, so nothing was re-measured live; all checks are against the lane's own captures, the binary's strings and unsent's source.
