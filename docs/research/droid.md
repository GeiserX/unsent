# Factory droid CLI 0.228.0: input-box profile for unsent (verified)

Tags on every claim:
- **[measured]** seen by the lane in droid 0.228.0 inside a private tmux (`-L droidprobe`, `-f /dev/null`, 100x30 unless noted). Where the bytes survive, the capture is `/tmp/droid-probe/raw.log` (388,817 bytes, one `script(1)` session at 100 columns). Claims marked **[measured, no capture]** were seen in tmux sessions whose output was not saved.
- **[source]** the JS embedded in the 0.228.0 Bun binary. Offsets are byte offsets into `/tmp/droid-probe/all.txt` (strings of the `__BUN` segment). Names are minified and hold for 0.228.0 only.
- **[docs]** docs.factory.ai, or the kitty keyboard protocol spec.
- **[guess]** inference nobody checked.

## Summary

droid is Ink 6 on React, drawn on the main screen (no alternate screen) [measured, source]. The input box is a full-width rounded box (`╭─╮ │ │ ╰─╯`, border fg `38;2;135;135;135`) [measured]. The first visible content row starts `│ > ` with the text at col 4; bash mode puts ` ! ` at cols 2-4 [measured]. The real cursor is hidden; the insertion point is one reverse-video (SGR 7) cell, which disappears when the terminal loses focus [measured, source]. Rows wrap at `cols - 10` [measured, source]. The idle box shows at most `rows - 13` content rows (for rows up to about 110) and then shows faint `↑ N more` / `↓ M more` rows with exact hidden counts plus a faint `Ctrl+P to open in editor` row [measured, source]. Every box frame is drawn inside a `?2026h/l` pair [measured]. A paste over 400 characters or 9+ lines becomes `[<first 50 chars>... N lines]` [measured, source]. Enter, Ctrl+Enter and Esc+Enter all submit; newline is Shift+Enter (kitty or xterm form), `\`+Enter, or Ctrl+J when no changelog is showing [measured, source, docs]. Drafts are never written to disk; only submitted text reaches `~/.factory/state/history.json` [measured].

## How it was measured (re-checked)

- **Package.** `@factory/cli@0.228.0` (npm, published 2026-09-26T02:26Z [measured: `npm view time`]) is a small shim that pulls `@factory/cli-darwin-arm64@0.228.0`, a Bun-compiled Mach-O [measured; tarballs in `/tmp/droid-probe/`].
- **Login wall.** A plain start asks for a Factory login. `FACTORY_AIRGAP_ENABLED=true` skips it: `airgapEnabled===!0` appears in the credential path at offset 5765769 [source]. The chat UI then shows the banner "Airgap Mode is enabled and no BYOK custom model is configured" (139 hits in `raw.log`) and submits fail locally [measured].
- **Isolation.** `run.sh` runs `env HOME=/tmp/droid-probe/home EDITOR=VISUAL=/tmp/droid-probe/cap.sh TERM=xterm-256color FACTORY_AIRGAP_ENABLED=true FACTORY_DROID_AUTO_UPDATE_ENABLED=false script -q raw.log droid` [re-checked]. The real `~/.factory` still has mtime May 23 and no droid process is running [re-checked].
- **Submits during the lane.** `home/.factory/state/history.json` holds **three** submitted drafts, not two as the lane said: `"do nothing; reply ok\nline two\nthree kitty\nfour xterm"`, `"do nothing; reply ok\nK\nX"`, `"do nothing\nafter ctrl j"` [re-checked]. All were refused locally in airgap mode. They also prove that kitty Shift+Enter, xterm Shift+Enter and Ctrl+J inserted `\n` at the time [measured].

## TUI stack

- **Ink 6.x on React**, with Ink's `kitty-keyboard`, `write-synchronized`, `log-update`, `input-parser` modules in the bundle; custom chat input, not ink-text-input [source].
- **Main screen.** No `ESC[?1049h` in `raw.log` (0 hits) [measured]. The box is redrawn in place by erase-and-rewrite (`ESC[2K ESC[1A` 1,995 times in `raw.log`) [measured].
- **Synchronized output is on.** 138 `ESC[?2026h` and 138 `ESC[?2026l` in `raw.log`; all 136 `╭` in the log fall inside a pair, 0 outside [measured, re-counted]. Gate: `Uu(n){return n.isTTY===!0&&!ze}` where `ze` is `CI` or `CONTINUOUS_INTEGRATION` set to anything but `0`/`false` (offset 8553073, 8374174) [source]. So under unsent's pty it is on, **unless the user's environment has `CI` set**, in which case there are no pairs at all [source].
- **Terminal modes seen at start:** `?2004h` bracketed paste, `?1004h` focus reporting, `?25l` hidden cursor, `ESC[>4;1m` modifyOtherKeys (3 hits, droid ran under tmux), and one `ESC[?u` kitty query [measured].
- **Kitty keyboard.** Ink's `initKittyKeyboard` writes `ESC[?u`, waits 200 ms, and on a reply writes `ESC[>{flags}u` with default flags `disambiguateEscapeCodes` (=1); a separate module also writes `ESC[>1u` and pops with `ESC[<u` (offset 6623643) [source]. tmux did not answer, so this was not measured. Under flag 1, Ctrl+letter and Alt+key presses arrive as CSI-u (e.g. Ctrl+W as `ESC[119;5u`), while plain Backspace, Enter and Tab stay legacy [docs: kitty keyboard protocol]. Upgraded from the lane's [guess]: in Ghostty, kitty, WezTerm and similar terminals, unsent's `deleteKeys` must decode CSI-u Ctrl+W/U/K/H/D/C and Alt+Backspace.

## What the box looks like

Captured frame from `raw.log` at 100 columns (SGR shown as `^[`):

```
^[[38;2;135;135;135m╭────…(98)…────╮^[[39m
^[[38;2;135;135;135m│^[[39m ^[[38;2;215;95;0m> ^[[39m^[[7mT^[[27m^[[2mry "Write unit tests for this file"^[[22m   …   ^[[38;2;135;135;135m│^[[39m
^[[38;2;135;135;135m╰────…(98)…────╯^[[39m
```

- **Borders.** Top `╭` + `─`×(cols-2) + `╮`, sides `│` at col 0 and col cols-1, bottom `╰…╯` [measured; only 100-col frames are in `raw.log`].
- **Borderless variant.** `jD(t)` returns `hasBorder:!t`; it is called with `windowsLike=Qs()`, and `Qs()` returns `Gc()`, which is hard-wired to `!1` (offset 8930758) [source]. So the border is always on today; the switch exists for a Windows-like mode.
- **Prefixes.** `{normal:"> ",bash:" ! ",executing:" ⏳ ",continuation:"  ",spec:"> "}` (offset 15358126) [source].
  - normal: `│ ` + orange `> ` (fg `215;95;0`), text from col 4; continuation rows `│ ` + orange `  ` [measured].
  - bash: `!` in fg `175;175;95`, text from col 5 on the first row, col 4 on continuations [measured, no capture].
  - executing ` ⏳ ` is two columns wide, so text would start at col 5 or 6 [source; not measured].
  - All prefix spaces are U+0020, no NBSP [measured].
- **The `> ` marks the first visible row**, not the first draft row: `│   ↑ 8 more` is followed by `│ > L09` [measured, `raw.log`].
- **Selection menus use the same orange `> `** outside the box (e.g. `> 1. Trust this folder` under the trust dialog) [measured]. A reader must only look inside a qualified box.
- **Empty box.**
  - Before the first message, focused: `> ` + `ESC[7m` on the first hint character + `ESC[27m` + `ESC[2m` for the rest [measured, `raw.log`]. The lane wrote `ESC[0;2m`; the bytes are `ESC[27m ESC[2m` (0 hits for `ESC[0;2m`). The hint is random from `common:placeholders.promptN` (function `aO`, offset ~8931900) [source].
  - Terminal unfocused (after `ESC[O`): whole hint dim, no reverse cell [measured, no capture; consistent with `h9` source].
  - After a message has been submitted: no hint, `> ` + reverse space [measured, no capture]. Rule in the placeholder chooser: `if(!B)return Z??""` (offset ~15359443) [source].
  - Other hints: spec mode, mission mode, and `Enter to steer · Ctrl+Enter to queue` while the agent runs [source; i18n `chatInput.*`].
- **Keys that type nothing on an empty box.** `?` opens help, `!` toggles bash mode (`if(It==="?"&&Zt.current.length===0)…if(It==="!"&&…)`, offset 15353179) [source]; `!` also [measured, no capture]. Backspace on an empty bash-mode box leaves bash mode [measured, no capture].

## Wrapping

- **Width = cols - 10.** `oO(t){return t?Math.max(t-10,1):80}` (offset 8932178), called as `up({value, maxLineWidth:oO(ht)})` where `ht` is the terminal width (offset 15339049) [source]. Measured 90 at 100 cols and 50 at 60 cols [measured; the 60-col run has no capture].
- **No dropped characters.** Display rows are consecutive substrings of the buffer; `hp()` maps the cursor through `lineMapping[s].bufferStart + row.length` (offset 8936738) [source]. Joining a raw line's rows with nothing between them gives the line back.
- **Where the separator space goes.** It stays at the row end if it fits, else it starts the next row: a paste placeholder split as `…nine 3 nine` / ` 4 nine 5…` in `raw.log` [measured]; the lane's `…mu nu ` / `xi` and `…zeta eta` / ` theta` cases [measured, no capture].
- **Consequence for the stitcher.** A row that ends in a space loses it when trailing spaces are trimmed; a row that begins with a space keeps it. Join rows of one raw line verbatim, from untrimmed cells [guess, follows from the above].
- **Runs of several spaces** at a wrap point were not measured [open].

## Height and scrolling

- **Cap formula.** `lU()` (offset ~8932400; the lane cited ~15366000, which is wrong) with `Te=8, Re=2, Ie=2, Me=3, be=1, _e=0.33, Ce=0.9, we=10, Oe=1, We=3`, `ky=6` [source]:
  `c = (suggestions?10 : commands?6 : loadingSuggestions?1 : 0) + 1 + (images?3:0)`
  `cap = max(1, max(3, min(H - 10 - c, floor(H * (agentActive ? 0.33 : 0.9)))) - 2)`
  - Idle, no menus: `cap = min(H - 13, floor(0.9H) - 2)`, which is `H - 13` up to H≈110 [source]. Measured 17 at H=30 [measured, `raw.log`] and 11 at H=24 [measured, no capture].
  - Agent running: `floor(0.33H) - 2`, e.g. 7 at H=30 [source only; needs a model].
- **Overflow rows** sit inside the box, dim, and do not count toward the cap [measured, `raw.log`]:
  - `│ ` + `ESC[2m  ↑ N more` above the content (the two leading spaces are inside the dim run),
  - `│   ↓ M more` below it,
  - `│   ` + `ESC[2mCtrl+P to open in editor` last while clipped (the three leading spaces are **not** dim).
  - N and M are exact (`FD()` returns `hiddenAbove`/`hiddenBelow`, offset ~8937048) [source]. `raw.log` holds `↑ 1…13 more` and `↓ 1…5 more` [measured].
- **Scroll is stateless and keeps the cursor on the bottom visible row** once it is past the first `cap` rows: `start = max(0, min(total-cap, cursorLine-cap+1))` [source]. Captured frame after moving up from the end of a 30-line draft: `↑ 8 more`, `> L09` … `L25` with the reverse cell on `L25`, `↓ 5 more`, `Ctrl+P to open in editor` (22 rows including borders) [measured, `raw.log`]. This differs from Claude Code, which jumps the cursor to mid-box.
- **Position.** The box is not pinned to the bottom: the status row (`? for help … TMUX ⧉`) and, when open, the slash/@ suggestion list sit below it [measured]. After the box shrinks there can be blank rows under it [measured, no capture].
- **Resize.** Re-lays out on SIGWINCH [measured, no capture]; BSD `script(1)` in the middle did not forward the resize (a harness artifact).

## Cursor

- **The terminal cursor is hidden** (`?25l`; tmux `cursor_flag=0`) and parked at col 0 below the frame [measured]. It says nothing about the insertion point.
- **The insertion point is one SGR 7 cell**: the character under the cursor, or a space at the row end. `h9()` renders `inverse(P.charAt(_)||" ")` (offset ~15359700) [source]; `ESC[7m ESC[27m` on the cursor row in every captured frame [measured].
- **No cursor cell when the terminal or the component is unfocused.** `h9`: `if(z===!1||V===!1)` renders plain text with `dim: F||V===!1` and no inverse cell, where `z` = terminal focused, `V` = component focused [source]. Focus-out removes the cell [measured, no capture].
- **So a real draft can be dim.** When the component itself is unfocused (another pane or dialog has focus), the draft is SGR 2 [source]. Terminal focus-out alone does not dim it [source]. "Dim means empty" is not safe for droid.

## Pastes

- **Threshold.** `Cr(text, isPaste, framed)`: `if(ld>400||vp>8)` where `ld` = length in UTF-16 units and `vp` = `split("\n").length` (offset 15342070) [source]. So more than 400 characters or 9+ lines. Measured: 3, 6 and 8 lines and a 400-char line stayed inline; 9 lines and 401 chars collapsed [measured; the 9-line and 401-char placeholders are in `raw.log`].
- **Format.** `` `[${first50}... ${n} lines]` ``, and when that exact string is already in the draft's map, `` `[${first50}... ${n} lines, paste ${k}]` `` with a per-input counter reset on submit (offset 15337255) [source]. `first50 = l9(text).substring(0,50)` with `\n` and `\t` turned into spaces, then `.trim()` [source]. `l9` drops C0 controls 0x00-0x08, 0x0E-0x1F and 0x7F first; `O2` normalises `\v` and `\f` to `\n` before that (offset 15334829) [source]. Captured: `[nine 1 nine 2 nine 3 nine 4 nine 5 nine 6 nine 7 n... 9 lines]` then `[… 9 lines, paste 1]` for the same paste again [measured, `raw.log`].
- **The count is display rows, not newlines.** Each line is wrapped at `ht`, the full terminal width (`Tu(line, ht)`), not the box width [source]. Measured `5 lines` for 401 chars and `146 lines` for a 14,189-char one-line word stream at 100 cols [measured, `raw.log`].
- **The placeholder is editable text.** It wraps like text (`raw.log` shows one split across two rows) [measured]. If an edit breaks it, its map entry is dropped (`Oce()`) [source]. Alt+Shift+V (`paste-expansion`) expands placeholders inline; placeholders are expanded before submit and before the external editor (`ea()`) [source].
- **Unframed input collapses too.** In the input handler, any chunk longer than 1 character with no ESC, no `[200~`/`[201~`, not starting with `[`, and not a 2-char `\x` pair goes through `ps(O2(chunk))` with `framedPaste=false`, and `ps` calls `Cr(text, true, false)` (offsets 15353884, 15342482) [source]. So a fast unbracketed burst over 400 chars or 8 newlines becomes a placeholder that unsent's `paste.go` never saw [source; not measured].
- **Images** show as `[Image N]` with a "+N more pasted images..." line [source, i18n `imageAttachment.*`].

## Keys

**Newline:**
- Shift+Enter as kitty `ESC[13;2u` and as xterm `ESC[27;2;13~` [measured: history.json entry 1]. The handler: `if(It.return){… if(It.shift){insert "\n"}` (offset 15350249) [source]. Docs: "Shift + Enter Insert a newline in the chat input", with `/terminal-setup` to make it reliable [docs: docs.factory.ai/reference/cli-reference].
- Ctrl+J (LF): `if(ao===ke.NEWLINE)return Cr("\n")` [source]; [measured: history.json entry 3]. **But** the docs list Ctrl+J as "Toggle the changelog display", and the source has a layer-0 Ctrl+J subscriber that toggles the changelog when one is showing (`if(!Yo(nt,"ctrl-j"))return;if(AO())IU();else if(_O()){…}`) [docs, source]. The lane missed this. Treat Ctrl+J as a newline only when no changelog is on screen (e.g. first run after an update) [guess on exact conditions].
- `\` then Enter: the backslash is removed and a newline inserted, unless the key is Ctrl+Enter [source].

**Submit:**
- Enter [measured].
- **Ctrl+Enter (`ESC[13;5u`) submits**; with the agent running it queues (`re(ss,{queuePlacement:"end_of_loop"})`) [source; measured accidental submit]. The built-in help table lists Ctrl+Enter as newline; the handler disagrees [source].
- **Esc then Enter submits** (not a newline, unlike Claude Code) [measured: history.json entry 3 was submitted this way per the lane].

**Deleting** (built-in help table, offset ~2946443) [source]:
- before the cursor: Backspace, Ctrl+H, Ctrl+W, Option+Backspace (`ESC DEL`), Ctrl+U (line prefix)
- after the cursor: Delete (`ESC[3~`, and `ESC[3;1u` = `MAC_FN_DELETE_KITTY`), Ctrl+D (first clears draft attachments if there are any), Option+D (`ESC d`), Ctrl+Delete, Ctrl+K (line suffix)
- Note `ke.BACKSPACE` and `ke.DELETE` are both `\x7F` [source].
- No undo handler found in the input [source; absence claim, lower confidence].
- By hand: Ctrl+U deleted only the current line's prefix in a two-line draft; Ctrl+K from Home cleared the line; `ESC DEL` and Ctrl+W deleted one word per press [measured, no capture].

**Whole-draft clearers** (matter for `deleteLog`):
- **Ctrl+C** clears the whole draft and shows "Press Ctrl+C again to exit" (10 hits in `raw.log`); a second Ctrl+C in the window exits [measured].
- **Esc Esc** on a non-empty box stashes the draft in memory and clears it; a third Esc restores it and fires the rewind callback (`Ma()`: `It===2` stash+clear, `It>=3` restore, offset 15343000) [source]. Docs: "Second press clears the input draft; a third Escape opens the rewind menu" [docs]. Measured the same [measured, no capture].
- Esc Esc on an empty box opens rewind: "No previous user messages available for rewind" [measured, `raw.log`].

**Other keys:** Up/Down move through rows, then history from `history.json` [measured, docs]. Ctrl+G pulls a queued message back into the input; Ctrl+R reviews queued messages [source].

## External editor (ground truth)

- **Ctrl+P** opens the draft in `$VISUAL`, then `$EDITOR`, then the system default (`Nf()` / `DN()`) [source]; the overflow hint row says so [measured]. The docs page does not list Ctrl+P [docs, checked].
- **The file** is `$TMPDIR/factory-input-edit-XXXX/prompt.md` (`command.sh` in bash mode), with placeholders expanded [source: `factory-input-edit` at offset 15348878].
- **Round trip.** `cap.sh` copied `$1` to `/tmp/droid-probe/editor-copy.txt`, which holds exactly `L01\n…\nL30` with no trailing newline (119 bytes) [measured, re-checked]. The lane reports the editor ran about 2.6 s after the key; poll for the copy [measured, no capture].
- **Sync vs async editors.** Terminal editors and unrecognised commands run synchronously (TUI suspended, file read back on exit); GUI editors without `--wait` go async and droid asks "press Enter here to load changes (Esc to cancel)" [source]. Use a script EDITOR in tests.

## Persistence and recovery already in droid

- **Unsent drafts are never written to disk.** "keepme draft unsent" typed, then Ctrl+C Ctrl+C; nothing under the scratch HOME contains `keepme` [measured; re-checked `grep -r keepme` finds nothing]. The draft is lost.
- **Submitted text goes to `~/.factory/state/history.json`** as `{command, timestamp, type, mode}`, including submits the backend refused [measured, re-checked].
- **The Esc-Esc stash lives only in memory** and is dropped when the Esc sequence times out (`Ya()` clears it) [source].
- **Keys typed before the UI is up are kept.** "early typed" sent 0.25 s after launch appeared in the box [measured, no capture]. The early-input shell hands `initialDraft` to the app; `FACTORY_DISABLE_EARLY_INPUT_SHELL` turns it off (offset 2050545) [source]. The early shell draws its own bordered composer (`vW()` with the same `jD`/`oO` helpers, offset 8944188) [source], so the reader sees a box of the same shape from the first frame [guess].

## Detection recipe for a `droidBox` reader

1. `extractorFor`: `filepath.Base(command) == "droid"`. The npm `droid` is a node shim that spawns the platform binary; the curl installer path and binary name were not checked [guess].
2. Scan rows bottom-up for a top border: `╭` + `─`×(cols-2) + `╮`, exactly screen width.
3. Walk down to the matching `╰─…─╯`. Every row between must have `│` at col 0 and at col cols-1.
4. **Qualify the box.** It is the chat input only if its first inner row is either a dim `↑ \d+ more` row followed by a prefixed row, or a row with `> ` at cols 2-3, ` ! ` at cols 2-4, or ` ⏳ ` from col 2. This rejects the welcome card and the "Trust this folder?" dialog, which use the same 135-grey rounded border (captured in `raw.log`) [measured].
5. **Sort the inner rows.** A row whose non-space cells are all dim and match `[↑↓] (\d+)` gives hiddenAbove / hiddenBelow. Do not require the leading spaces to be dim: on the Ctrl+P hint row they are not. Dim rows after the content are hints (`Ctrl+P to open in editor`, `Enter to steer · Ctrl+Enter to queue`, editor guidance, or two joined with ` · `): drop them. Match the arrows and digits, not English words, because these strings are localised [source]. Everything else is content: cols 4..cols-2 (col 5 on a bash first row).
6. **Empty box.** Exactly one content row, and after the prefix it is empty, a lone reverse space, or a reverse first character followed by a dim rest (the placeholder signature). Never call a box empty on dimness alone when there are several content rows or an indicator.
7. **Cursor.** The single SGR 7 cell in the content area; none after terminal focus-out or when the component is unfocused. Ignore the real terminal cursor.
8. **Wrap and cap.** `width = cols - 10`; `capped = hiddenAbove > 0 || hiddenBelow > 0`; the counts are exact, so the stitcher knows the total row count (`hiddenAbove + visible + hiddenBelow`). Scrolling keeps the cursor on the bottom visible row.
9. **When to read.** Only outside `?2026h/l` pairs (already how unsent works). If `CI` is set in the environment droid emits no pairs [source].
10. **`deleteKeys`.** Before the cursor: `\x7f`, Ctrl+H, Ctrl+W, `ESC DEL`, Ctrl+U. After: `ESC[3~`, `ESC[3;1u`, Ctrl+D, `ESC d`, Ctrl+Delete, Ctrl+K. Whole draft: Ctrl+C and Esc Esc. Add the CSI-u forms of the Ctrl and Alt keys, since droid pushes kitty flag 1 when the terminal supports it [source, docs].
11. **Paste expansion.** Match `\[(.{0,50})\.\.\. (\d+) lines(?:, paste (\d+))?\]`. Pair group 1 with a captured paste whose text, after dropping C0 controls (except `\t\n\v\f\r`), turning `\v`/`\f` into `\n`, taking the first 50 UTF-16 units, turning `\n`/`\t` into spaces and trimming, equals group 1. Ignore the count. Also capture unframed multi-character bursts, which droid collapses too.
12. **Test harness.** Newline = `send-keys -H 1b 5b 31 33 3b 32 75` (kitty Shift+Enter) or `\`+Enter; Ctrl+J only when no changelog is showing. Never `Escape Enter` and never `ESC[13;5u`: both submit. Ground truth = Ctrl+P with a copy-and-exit `$EDITOR`, polling for the copy. Login-free = `FACTORY_AIRGAP_ENABLED=true`, scratch `HOME`, `FACTORY_DROID_AUTO_UPDATE_ENABLED=false`.

## Stability across versions

- **Release pace.** 235 versions from 0.1.0 (2025-08-07) to 0.228.0 (2026-09-26); 13 published since 2026-09-17 [measured: `npm view @factory/cli time`, re-run]. Auto-update is on by default [source, lane; not re-checked].
- **Same in 0.200.0 (2026-08-20) and 0.228.0** [source, re-checked by regex over both string dumps]: the prefix map, the `>400||>8` threshold, the placeholder template, the `-10` wrap, the `8,2,2,3,1,0.33,0.9` height constants, `hiddenAbove`, and the Ctrl+P hint.
- **Different in 0.150.0 (2026-06-16):** prefix map, threshold, placeholder template and `-10` wrap were already there, but the height constants, `hiddenAbove` and the Ctrl+P hint were not [source, re-checked]. So the in-box viewport with `↑ N more` arrived between June and August 2026. (0.150.0 does contain two `↑ {{count}} more` strings, used elsewhere in the UI [source].)
- **Localisation.** Indicators and hints are i18n strings (it, ja, ko, zh locales present) [source].

## What would break the reader

- Border colour or theme change (only matters if the reader keys on colour; it should not).
- `Gc()` returning true (Windows-like mode): no border, different padding [source].
- A new prefix string or wrap offset.
- Dim drafts when the input component is unfocused [source]; whether a permission dialog unfocuses the box or replaces it is unmeasured [open].
- Terminal focus-out: no cursor cell [measured].
- Agent running: cap drops to about `0.33H - 2`, a steer hint row appears, the prefix may become ` ⏳ ` [source].
- Suggestion menus change the cap through `c` [source].
- Kitty keyboard push in modern terminals: Ctrl/Alt delete keys arrive as CSI-u [source, docs].
- Unframed paste collapse [source].
- Locale (arrow rows and hints change wording) [source].
- `CI` set in the environment: no synchronized-output pairs [source].

A reader that stops matching fails safe under unsent's rules. Re-measure on any droid release that touches `chatInput`, `wrapText.ts`, `viewportBudget.ts` or `inputLayout.ts` (file names present in the bundle) [source, lane].

## Open questions

- Agent-running state (0.33 cap, steer hint, ` ⏳ ` prefix) and permission dialogs need a model; a BYOK custom model pointed at a local stub server would allow measuring them without real submits.
- Does a permission or tool-approval dialog hide the box or only unfocus it (dim draft)?
- Real kitty-protocol terminal (not tmux): confirm droid pushes `ESC[>1u` and what Backspace/Ctrl+W/Ctrl+C bytes then look like.
- Exact conditions under which Ctrl+J toggles the changelog instead of inserting a newline.
- Curl installer path and binary name.
- Runs of several spaces at a wrap boundary (the wrapper tokenises `\S+|\s+`).

## Verification notes

Checked against the lane's surviving artifacts in `/tmp/droid-probe/` (`raw.log`, `all.txt`, `old-0.150.0/s.txt`, `old-0.200.0/s.txt`, `home/`, `editor-copy.txt`, `run.sh`, `cap.sh`), against npm, and against docs.factory.ai/reference/cli-reference. droid was not run.

Held up:
- Source strings and logic at the cited places: prefix map, `oO` wrap (`t-10`), `lU` cap formula (worked through for H=24/30: 11 and 17), `FD` stateless scroll, `h9` cursor and dim rule, paste threshold and template, unframed-burst routing, Enter/Shift/Ctrl+Enter/backslash handler, `Ma` Esc stash, airgap early return, `FACTORY_DISABLE_EARLY_INPUT_SHELL`, sync-output gating.
- `raw.log`: 0 `?1049h`; 138/138 sync pairs with all 136 box frames inside; `↑ 8 more` / `L09..L25` cursor on `L25` / `↓ 5 more` / Ctrl+P hint frame; `[… 9 lines]`, `[… 9 lines, paste 1]`, `5 lines`, `146 lines` placeholders; trust dialog in the same border; rewind message.
- `history.json` proves the kitty and xterm Shift+Enter and Ctrl+J newlines; no `keepme` anywhere in the scratch HOME; `editor-copy.txt` is the exact 30-line draft; real `~/.factory` untouched; no droid process left.
- Version comparison (regex over the three string dumps, minified names differ) and npm release dates.

Changed:
- Submitted drafts: three in `history.json`, not two.
- Empty-hint SGR is `ESC[7m`X`ESC[27m ESC[2m`…, not `ESC[0;2m`.
- `lU()` offset is ~8932400, not ~15366000; the idle cap is `min(H-13, floor(0.9H)-2)`, so `H-13` holds only up to H≈110.
- Ctrl+J: docs and a source subscriber say it toggles the changelog when one is showing; downgraded from an unconditional newline key and removed from the recommended test newline.
- Kitty keyboard: upgraded from [guess] to [source] (Ink `initKittyKeyboard` and an explicit `ESC[>1u` push), with CSI-u consequences for `deleteKeys` [docs].
- Paste pairing: added the `l9` control-character strip and `\v`/`\f` normalisation; the count is rows wrapped at the full terminal width `ht` (source), replacing the lane's "about 98-100".
- Ctrl+P hint row: its leading spaces are not dim, so "fully faint row" matching must look at non-space cells only.
- Added the `CI` environment caveat for synchronized output, and that selection menus use the same orange `> ` outside the box.
- Claims seen only in unsaved tmux sessions (60-col wrap, H=24 cap, bash-mode colours, focus-out, early typing, hand-tested delete keys, 2.6 s editor delay) are now tagged [measured, no capture].
- Dropped the lane's first open question: it relayed an unrelated user request about a warning and memory, which has nothing to do with droid.
