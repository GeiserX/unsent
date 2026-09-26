# Amp CLI (`amp`): verified input-box profile for unsent

Lane: `amp`. Source lane output: `.omc/spec/research/amp.json`. Verified 2026-09-26.

Tags on every claim:
- **[measured]**: seen in a real Amp run. "lane" means the research lane's tmux run. "re-checked" means I replayed the saved raw pty capture (`/tmp/amp-lane/raw*.log`) through the same emulator unsent uses (`charmbracelet/x/vt`) and saw it myself.
- **[source]**: read in the minified JS bundle pulled out of the binary. I re-read every source claim marked "re-checked" at the given offset. Offsets refer to `/tmp/amp-lane/amp.js` (the `__BUN` section of `@ampcode/cli-darwin-arm64@0.0.1790424028-ge9aad5`, 6.9 MB).
- **[docs]**: ampcode.com (`/docs/cli/keybindings`) or the kitty keyboard protocol spec.
- **[guess]**: inference with no direct evidence.

Build measured: `0.0.1790424028-ge9aad5`. The number is a Unix time, so the build is from 2026-09-26. npm `latest` had already moved to `0.0.1790438457` when I checked. [measured, re-checked: npm registry]

## Summary

Amp draws the draft inside a full-width rounded box (`╭╮╰╯`) at the bottom of the alternate screen. Each text row is `│ ` + text + ` │`, and text wraps at width minus 4. The empty box has no hint text. The insertion point is one reverse-video (SGR 7) cell that Amp paints itself. The real cursor stays hidden, but Amp moves it onto that cell with CUP (the cursor-position escape) at the end of each frame. The cell is not painted, and the CUP is skipped, while the terminal reports focus lost. The box is 3 rows tall when empty and grows to `floor(rows/3)` text rows. After that it scrolls one row at a time. Pastes always show inline, with no placeholder. Every frame is inside a `?2026` synchronized-output pair. Amp keeps no local copy of an unsent draft: history is written only on submit, and thread drafts sync to Amp's server.

## How the lane reached the box without an account [measured, lane; harness re-checked]

- Install into a throwaway prefix: `npm install --prefix /tmp/amp-lane/pfx @sourcegraph/amp@<ver>`. The npm package is now a stub. The real program is `@ampcode/cli-darwin-arm64/amp`, a 76 MB Mach-O compiled with Bun 1.4.1. Older builds (01-15, 04-01) are Node bundles (`dist/main.js`). [measured; re-checked: the old prefixes hold `dist/main.js`, the newer ones the binary]
- With no key, Amp prompts `Would you like to log in to Amp? [(y)es, (n)o]` (re-checked in `raw1.log`). With a fake `AMP_API_KEY` it exits with `Error: Invalid or missing API key. Run 'amp login' to authenticate.` (`raw2.log`). A stub that returns 404 gives `API request for getUserInfo failed: 404` (`raw3.log`). [measured, re-checked]
- The lane's harness is `/tmp/amp-lane/run-amp.sh` plus `/tmp/amp-lane/fake.py`, pointed at by `AMP_URL=http://127.0.0.1:18555`. The stub answers every `POST /api/internal?<method>` with `{"ok":true,"result":{}}`. `getUserInfo` gets a fake user; `getThread` gets `{"ok":false,...}`. `fake.log` shows the calls Amp makes at start: `getUserInfo`, `user-actor-credentials`, `listAgentModes`, `loadPlugins`, `loadSkills`, and more. [measured, re-checked]
- The first run shows a 5-page welcome carousel. After it is dismissed, `session.json` holds `neoWelcomeDismissed: true` (the key exists in source; I did not see the file, because the lane's scratch HOME is now empty). [measured: lane only; source: key re-checked]
- HOME and every XDG dir pointed at `/tmp/amp-lane/home`. No prompt was ever submitted. [measured, lane]

## TUI stack [source]

- Amp uses its own retained-mode widget framework written in TypeScript, modelled on Flutter (`createState`, `build`, `initState`, `RenderBox`, `RenderFlex`). The prompt is a `TextField` whose key handler is near @5905553. [source, re-checked]
- It is not Ink, Bubble Tea, ratatui, prompt_toolkit or OpenTUI. [source, lane; not re-checked]

## What the box looks like (current build, 100x30)

Replayed from `raw5.log`, frame 5 (empty box). `█` = an SGR 7 cell, `~` = a dim (SGR 2) cell. [measured, re-checked]
```
25|╭───────────────────────────────────────────────────────────────────────────── medium ─╮|
26|│ █                                                                                     │|
27|│                                                                                       │|
28|│                                                                                       │|
29|╰───────────────────────────────────────────────────────── ~~~~~~~~~~~~~~~~~~~~~~~~~~ ─╯|
```
(The middle is shortened here; the real rows are 100 cells wide. The dim run on the bottom border is the cwd `/private/tmp/amp-lane/work`.)

- **Borders:** rounded `╭ ─ ╮ │ ╰ ╯`, spanning columns 0 to W-1. In every current-build frame I replayed, the bottom border sits on the last screen row. [measured, re-checked]
- **Labels in the border:** top-right shows the agent mode (` medium `). Bottom-right shows the cwd in **dim (SGR 2)**. [measured, re-checked] There are four overlay slots (topLeft, topRight, bottomLeft, bottomRight). When messages are queued, the topRight label moves into the queued-messages panel (`topRight:s?void 0:c.topRight` @6621847). [source, re-checked] Inside a thread, the slots also carry a usage gauge, status items, hints, and an "Uploading" spinner. [source, lane; the "Uploading" string is present]
- **Text area:** the editor has one column of padding each side (`padding:Kt.horizontal(1)` @6621847) inside the border, so text is in columns 2..W-3 and wraps at W-4 = 96. [measured + source, re-checked]
- **No prompt glyph.** A leading `$` gets no styling (`$ echo hi` in `raw6.log` frame 266 is drawn as plain text). The editor passes `prompts:this.props.shellPromptRules`, but nothing in the bundle ever sets a `shellPromptRules:` value. [measured + source, re-checked]
- **Empty box:** 3 blank rows plus the SGR 7 cell at column 2 of the first row. There is no placeholder text in the current build (and, per the lane, none in the 06-26 and 08-26 builds either). A placeholder, if one is ever set, is painted in `placeholderColor = mutedForeground`, not with SGR 2 (@6506511). [measured, re-checked for the current build; source, re-checked]
  - So "all text is dim" cannot be the empty signal the way it is for Claude Code. Also, dim cells do appear in the box, in the cwd label on the bottom border. A reader that borrows Claude Code's dim test must look only at the text rows.
- **Cursor:**
  - `_paintSoftwareCursor` (@5919620) paints the cell at the insertion point with `reverse:!0` and then calls `setCursorPositionHint`. It returns early when `Q_()` is false. `Q_()` is the terminal-focus flag (`q6`, starts `true`, updated from focus events, mode 1004). [source, re-checked]
  - The real cursor is hidden throughout: `raw4.log` and `raw5.log` have 0 `?25h` and 5,074 / 1,500 `?25l`. Most current-build frames end with `CUP row;col` + `?25l` inside the pair. [measured, re-checked]
  - In every replayed frame, the real cursor position equals the `█` cell (for example `raw5.log` frame 750: cursor 75,19 on the SGR 7 cell). [measured, re-checked]
  - When the terminal reports focus-out (`ESC[O`), no SGR 7 cell is drawn and no cursor hint is sent. [measured, lane; source, re-checked]
  - When the mouse wheel scrolls the insertion point out of view (`_allowCursorOffscreen`), there is no cursor cell either. [measured, lane; source identifier re-checked]
  - The 2026-01-15 build has no `setCursorPositionHint` at all, so its real cursor never follows the insertion point. The 04-01 build has it. [source, re-checked: 0 vs 4 occurrences]
  - Rule for the reader: trust the insertion point only when there is exactly one SGR 7 cell in the text rows.

## Wrapping, height, scrolling

- **Word wrap at W-4.** Replayed rows of `z01846 … z01858` are 13 seven-character words; the next word would pass column 96, so it wraps. [measured, re-checked] A single word longer than W-4 breaks at exactly 96. [measured, lane only]
- **Spaces at a soft wrap are not shown** (`a×96 + ' bbb'` shows no space; `a×95 + '   bbb'` shows one). Double spaces in the middle of a row are kept (`second line  with  double spaces` in `raw6.log` frame 233). [measured: lane for the wrap case; mid-row case re-checked]
- **Height:** `maxBodyRows: Math.floor(size.height/3)` (@6648650, @6713856), passed to the box as `maxBodyRows: t + d`, where `d` is 4 when images are attached and 0 otherwise. The minimum is 3 (`minBodyRows:3`, `initialRows:3`). [source, re-checked] Measured at 24/30/31/45 rows: 8/10/10/15 (lane). At 30 rows, a 10-row box re-checked in `raw5.log`. [measured]
- **Manual resize:** dragging the top border switches to a fixed height, and a double-click toggles `auto-min` / `manual-max` (@6621847). [source, re-checked] The glyph used while the border is hovered is not captured. [open]
- **Scrolling is one row at a time.** `raw5.log` frames 734→745 (a 14,000-character draft at the end, then Up presses): the cursor climbs rows 28→19 inside a fixed view, then each further Up moves the view exactly one layout row (`z01872` → `z01859` → `z01846` at the top). There is no "more above" marker and no scrollbar. [measured, re-checked]
- **The mouse wheel scrolls 3 rows** without moving the insertion point. [measured, lane only]
- **Typing or pasting at the end:** the view follows the end of the text (frame 734). [measured, re-checked]

## Pastes

- Amp enables bracketed paste (`?2004h`, seen in every capture). Pastes are **always inline**. [measured, re-checked]
  - 3 lines: `raw4.log` last frame shows `paste line one/two/three` inline.
  - 20 lines: `raw5.log` frame 731 shows `pl10 … pl19` inline, with the view at the end.
  - 13,999-character single line (`p12k.txt`): `raw5.log` frame 734 ends with `z01999█`.
  - The string `Pasted` appears nowhere in the bundle or in any capture.
- `stripTrailingPasteNewline` exists on the TextField; the lane says the prompt turns it on. [source: option re-checked, value not]
- So `paste.go`'s placeholder expansion is not needed for Amp. The captured paste text is still the only ground truth for the rows that scroll out of view.
- **Images:** Ctrl+V can paste an image. It shows as an `Images: [Image 1]` row, and **Backspace at position 0 removes the last image** (`onBackspaceWhenEmpty`). That Backspace deletes no text. [source, re-checked]

## Keys (TextField handler @5905553, re-checked unless noted)

| Key (legacy bytes) | Effect | Tag |
| --- | --- | --- |
| Enter (CR) | submit (`submitKey` default `{character:"Enter"}`) | source + docs |
| Ctrl+J (0x0a) | newline | source + measured (lane) + docs |
| Alt+Enter (ESC CR) | newline (`y.altKey && isMultiline`) | source + measured (lane) |
| Shift+Enter (`ESC[13;2u`) | newline | measured (lane) + docs |
| `\` then Enter | newline | docs (not tested) |
| Tab | not inserted (the draft `…spacesand tab` in `edcopy.txt` shows the typed Tab vanished) | measured, re-checked |
| Backspace (0x7f), Ctrl+H (0x08) | delete 1 before (or the selection) | source |
| Alt+Backspace (ESC 0x7f), Ctrl+W (0x17) | delete word left | source |
| Ctrl+U (0x15) | delete to start of the logical line; **at a line start it deletes the preceding `\n` (joins lines)** | source |
| Ctrl+K (0x0b) | delete to end of the logical line; **at a line end it deletes the `\n` (joins lines)** | source |
| Delete (`ESC[3~`), Ctrl+D (0x04) | delete 1 after | source |
| Ctrl+Y (0x19) | yank the kill buffer (re-inserts text) | source |
| Ctrl+P / Ctrl+N (0x10 / 0x0e) | move a line up/down; when the cursor cannot move, history previous/next **replaces the whole text** (the draft is kept in memory as `historyDraft` and returns on Ctrl+N) | source |
| Ctrl+R | history picker | source + docs |
| ↑ at the very start of the first line | the TextField returns "ignored" and the key goes to the parent; the docs say ↑/↓ "move to queued and previous messages so you can edit them" | source + docs |
| Tab / Shift+Tab | "navigate messages" (shortcuts help text `lLe` in source) | source |
| Ctrl+G | open the draft in `$EDITOR` | source + docs + measured |
| selection + typing/Backspace | replace/delete the selection (mouse drag, double-click, triple-click) | source, lane |

Measured by the lane (not in saved captures): Ctrl+U, Ctrl+A then Ctrl+K, and Backspace behaved as listed. Esc Esc does not clear an idle draft. One Ctrl+C with a draft keeps it and opens a small `Ctrl+C Quit / Esc …` box; a second Ctrl+C quits. [measured; the Ctrl+C box re-checked in `raw6.log` frame 392, drawn at columns 76..99 just above the prompt box, with the draft `look at @` still in the box]

The lane's "a line-level Ctrl+U/K leaves the other lines alone" holds only in the middle of a line. At a line boundary, the source shows both keys delete the newline. [source, corrected]

### Keyboard protocols

- Amp sends `ESC[?u` (kitty query) at start. Only if the terminal reports kitty support does it push `ESC[>5u` (disambiguate + report alternate keys; `D8t`, called with no options by the TUI @983649 and @986812). It always sends modifyOtherKeys `ESC[>4;1m` and pops kitty with `ESC[<u` on exit. [source, re-checked] In all the lane's tmux captures `?u` and `>4;1m` appear but `>5u` never does, because tmux did not answer the query. So **the kitty path was never measured**. [measured, re-checked]
- Under `>5u` (kitty spec, disambiguate): Ctrl+W `ESC[119;5u`, Ctrl+U `ESC[117;5u`, Ctrl+K `ESC[107;5u`, Ctrl+H `ESC[104;5u`, Ctrl+D `ESC[100;5u`, Ctrl+Y `ESC[121;5u`, Alt+Backspace `ESC[127;3u`, Ctrl+J `ESC[106;5u`, Alt+Enter `ESC[13;3u`, Shift+Enter `ESC[13;2u`, Esc `ESC[27u`. Plain Enter, Tab and Backspace keep their legacy bytes. The modifier field also carries Caps Lock (+64) and Num Lock (+128), so Ctrl+W with Caps Lock on is `ESC[119;69u`. With the report-alternate-keys flag, fields can carry `:`-separated sub-values. A decoder must mask lock bits and skip sub-fields. [docs: kitty spec; not measured]
- The Easter-egg "end credits" screen pushes kitty flags 31 (all keys as escape codes, `dXt` @1406352) while it is up. The box is not on screen then. [source, re-checked; low impact]
- Whether the outer terminal ever sees `?u` depends on unsent: if unsent answers terminal queries itself, the kitty path may never turn on. The spec should decide this explicitly. [guess]
- **Mouse input:** Amp enables 1002/1003/1004/1006 (button, any-motion, focus, SGR mouse). The input stream is full of `ESC[<…M/m` reports and `ESC[I`/`ESC[O` focus events, which must not count as keys. [measured, re-checked]

## Screen mode and timing [measured, re-checked unless noted]

- **Alternate screen.** At start Amp enables mouse, focus and paste modes, then probes: `?1049h ESC[H OSC 66 ESC[6n ?1049l`, followed by colour queries (OSC 10/11/12 and OSC 4;0..7). Then it enters for real. It also enables 2027 (grapheme clusters), 2048 (in-band resize) and 2031 (colour-scheme updates). On exit it resets everything, including `?1016l`, `>4;0m`, `ESC[0 q` and `?25h`, leaves the alternate screen, and resets the title with `OSC 0`.
- **Synchronized output:** frames are wrapped in `?2026h … ?25l ?2026l`. Pair counts: `raw4.log` 2,537/2,537, `raw5.log` 750/750, `raw6.log` 395 h / 396 l. (The lane reported 747/747 and 395/395; the extra `l` is a mode reset on exit.) Outside the pairs there is only the startup probe and the suspend/exit mode resets. **Never read the screen with a pair open.**
- **Idle and per-keystroke output:** `raw7.log` is one keystroke, one 262-byte frame (`x` + SGR 7 space + CUP + `?25l`). Idle output of 0 bytes in 5 s after the logo animation settles is lane-measured only.
- **Ctrl+G:** leaves the alternate screen and runs `$EDITOR` on `$TMPDIR/amp-edit-XXXXXX/message.amp.md` (`edargs.txt`: `/var/folders/…/T/amp-edit-myoeSG/message.amp.md`). On return it re-enters with `?1049h … ESC[2J` and redraws everything (the same sequence as in `raw6.log`'s suspend chunk). The box is off screen while the editor runs, so the reader must keep the last draft.

## Existing recovery in Amp

- **Prompt history:** `$XDG_DATA_HOME/amp/history.jsonl` (@6840102), capped at 2 MiB (`ikr=2097152`). It is appended only in `onPromptSubmitted` → `history.add` (@6763202), which the submit path calls (@6687981). [source, re-checked]
- **Thread drafts:** `syncEditorTextToThreadDraft` is an editor listener (@6820253). Drafts go to the server through `setDraft` and `client_update_prompt_draft`, and remote changes reload into the editor. A new thread has no ID until its first message, so its draft has nowhere to go. [source, re-checked for the identifiers; "in-memory Map + debounce" is lane reading, not re-checked]
- **After quit:** a typed draft and Ctrl+C Ctrl+C left no local file with the text; `history.jsonl` was not created. [measured, lane only; the scratch HOME is now empty, so I could not re-check]
- **Ctrl+G is exact ground truth:** `edcopy.txt` is 60 bytes, `do nothing; reply ok\nsecond line  with  double spacesand tab`, `\n` between lines, no trailing newline. It matches what the box showed in `raw6.log` frame 233. [measured, re-checked]

## Screen stability across versions

| Build (Unix-time version) | Box | Labels | Real cursor follows insertion point |
| --- | --- | --- | --- |
| 2026-01-15 (Node) | same rounded box, SGR 7 cursor | mode top-left `╭─medium───…╮`; cwd glued `…/work─╯` | no (source: no `setCursorPositionHint`) |
| 2026-04-01 (Node) | same | top-right hint glued with `─` (`opt-d … think more …`, source builds `${key} to think more`); ` ? for shortcuts` line **below** the box (source: `" for shortcuts"` shown when the text is empty) | yes |
| 2026-06-26, 08-26, 09-26 (Bun) | same | ` medium ─╮`, ` /cwd ─╯` | yes |

- Tags: box shape and labels per build [measured, lane only; no raw captures of the old builds were kept]. Cursor column [source, re-checked]. The 04-01 hint strings [source, re-checked].
- Release pace: `@ampcode/cli` 1,096 versions since 2026-05-12 (about 8 a day); `@sourcegraph/amp` 4,335 since 2025-04-11. [measured, re-checked: npm registry]
- A reader anchored on the box shape and ignoring border text should survive routine releases. [guess]

## What would break the reader

- **Overlays on the top border:** the `@` mention popup draws over the top border (`╭─╰─────╯───… medium ─╮`). [measured, lane] Never parse the top border's middle.
- **Boxes near the prompt:** the Ctrl+C confirm box ends at column W-1 right above the prompt (`raw6.log` frame 392) [measured, re-checked]. The queued-messages panel is inset one column (`Kt.horizontal(1)`) [source, re-checked]. Both are rejected by requiring `╰` at column 0 **and** `╯` at W-1 on the same row.
- **Shortcuts help (`?` in an empty box):** drawn inside the box, ending in an inner rule `│ ─{W-4} │`. At max height it pushes the editor out of view. [measured, lane] Treat an inner rule as "box not readable".
- **↑ at the top of the draft / Tab / Shift+Tab inside a real thread** move focus to previous or queued messages. What the prompt box shows then is unmeasured. The stub cannot create a thread. [source + docs; effect unknown]
- **History navigation (Ctrl+P/N, Ctrl+R):** the box text is replaced with no delete keys. If a save happens then, it would look like the whole draft was lost. unsent's `keepOld` safety copy covers this, but the stitcher should treat a whole-box replacement as a swap, not as edits. [source; guess on the unsent impact]
- **Unfocused terminal, or a wheel scroll past the cursor:** no SGR 7 cell, so the insertion point is unknown. [measured, lane; source, re-checked]
- **Manual resize:** height no longer follows the text. [source]
- **Soft-wrap spaces** are invisible, and soft and hard breaks can only be told apart by a fill heuristic. [measured, lane]
- **Selection** is painted with a background colour, not SGR 7. Copy-on-select is on. [source, lane]
- **Terminal size:** under `script(1)` inside tmux, resizes did not reach Amp, which left stale rows. Without `script`, resizes worked. unsent must keep setting the pty size. [measured, lane]
- **One unexplained Ctrl+U miss** after `$ echo hi`, not reproduced in 3 retries. [measured, lane, not reproduced] (`raw6.log` frames 266–281 show `$ echo hi` several times; I did not isolate the miss.)
- **`amp.keymap`** can remap commands and the submit key; the docs describe chords and arrays. The TextField's delete keys are hard-coded in the handler. [docs + source]

## Detection recipe (corrected)

0. Read only on the alternate screen, with no `?2026h` pair open. If no box is found (Ctrl+G, suspend, the startup probe, after quit), return "not found" and keep the last draft.
1. Scan up from the last row. Take the first row `r_b` whose column 0 is `╰` **and** whose column W-1 is `╯`. Do not parse between them: labels change between builds, and the cwd label is dim. Rows below `r_b` are allowed (the 04-01 build put a line there).
2. Walk up from `r_b-1` while column 0 is `│` and column W-1 is `│`. Stop at a row with `╭` at column 0 and `╮` at W-1. Do not require `─` between them (popups overwrite it). Require ≥ 3 text rows. More than `floor(H/3)+4` means a manual resize: accept it, but flag it.
3. Reject the view if any text row matches `^│ ─{W-4} │$` (shortcuts help).
4. Row text = columns 2..W-3. Trim trailing spaces (they cannot be recovered at a soft wrap). There is no glyph or indent to strip.
5. Insertion point = the single SGR 7 cell in the text rows. With zero cells (unfocused, wheel-scrolled) or more than one, the insertion point is unknown; do not fall back to the real cursor.
6. Empty box = every text row blank except the SGR 7 space. Do not use "dim" as the empty signal.
7. Stitcher: the view scrolls one row at a time, with the cursor pinned to the top or bottom edge row. The wheel moves the view 3 rows and leaves the cursor. There is no overflow marker.
8. Input side:
   - Pastes are shown inline; record the bracketed paste text as ground truth.
   - Delete keys, before the cursor: `0x7f`, `0x08`, `ESC 0x7f`, `0x17`, `0x15`. After the cursor: `0x0b`, `0x04`, `ESC[3~`. Kitty forms: `ESC[127;3u`, `ESC[119;5u`, `ESC[117;5u`, `ESC[104;5u` (before) and `ESC[107;5u`, `ESC[100;5u` (after). Mask lock bits (+64/+128) from the modifier and ignore `:` sub-fields.
   - Ctrl+U and Ctrl+K at a line boundary delete one `\n`.
   - Backspace at position 0 with images attached deletes an image, not text.
   - Keys that restore or replace text: `0x19` / `ESC[121;5u` (yank), `0x10`/`0x0e` and their CSI-u forms (history), Ctrl+R, undo/redo.
   - Newline keys: `0x0a`, `ESC CR`, `ESC[13;2u`, `ESC[13;3u`, `ESC[106;5u`, and `\` + CR.
   - Skip SGR mouse reports (`ESC[<…M/m`) and focus reports (`ESC[I`, `ESC[O`).
9. Real-app tests: `/tmp/amp-lane/run-amp.sh` + `fake.py` (`AMP_URL`, fake `AMP_API_KEY`, `AMP_SKIP_UPDATE_CHECK=1`, scratch HOME/XDG), with `session.json` seeded with `neoWelcomeDismissed:true`. Ground truth comes from `EDITOR=<copy script>` + Ctrl+G, which gives `message.amp.md`, byte-exact with no trailing newline. To exercise the kitty path, the test harness must answer `ESC[?u` (tmux does not).

## Risks

- Amp ships about 8 builds a day. The border labels changed three times in 9 months. Anchor on the box shape only.
- No cursor cell while unfocused or wheel-scrolled.
- Soft-wrap spaces are invisible. With no paste placeholder, a large paste scrolls most of the draft out of view at once; the stitcher depends on captured paste text.
- The kitty and CSI-u delete encodings are unmeasured. Counting only legacy bytes would miss Ctrl+W/U/K under a kitty-capable outer terminal.
- History navigation and message navigation replace or hide the box text with no delete keys.
- Source facts come from a minified bundle with obfuscated names.
- Housekeeping: the lane ran `amp --help` once without the scratch HOME, which created `~/.cache/amp/logs/cli.log` (0 bytes, 2026-09-26 17:32). I confirmed it still exists and left it, since nothing may be deleted without confirmation.

## Open questions

- What the box shows during an active turn: steer and queue hints, the queued panel, and what happens to the prompt when ↑ at the top, or Tab/Shift+Tab, moves focus to an earlier message. This needs a real thread.
- Whether Amp prints anything on the normal screen after quitting a real thread. The stub run printed nothing, so unsent's recovery notice would stay visible.
- The glyph used for the hovered or dragged top border.
- Whether unsent answers or forwards `ESC[?u`, which decides if the kitty path is ever on.
- How reliable the server-side draft sync is after a crash (needs an account).
- The lane's note about a relayed request to delete a warning from a memory or CLAUDE.md is outside this lane; it is left for the main session.

## Verification notes

What I checked:
- Replayed the lane's saved pty captures (`raw1`–`raw7.log`) through a throwaway Go tool (`/tmp/amp-verify/replay`, built against unsent's own `charmbracelet/x/vt` pin, offline). I confirmed the empty box, the 3-row minimum, the 10-row maximum at 30 rows, wrap at 96, SGR 7 cursor = real cursor position, the dim cwd label, inline 3-line / 20-line / 13,999-char pastes, one-row scrolling, the Ctrl+C confirm box, and dropped Tab.
- Counted escape sequences in the captures: `?2026` pairs, `?25h`/`?25l`, the mode set, `?u` and `>4;1m` present, `>5u` absent.
- Re-read the bundle at the cited offsets: `maxBodyRows` (floor(h/3), +4 with images, min 3), `_paintSoftwareCursor` (reverse + focus gate `Q_()`, initial `q6=!0`), the `shellPromptRules` wiring, `stripTrailingPasteNewline`, `history.jsonl` path and cap, `onPromptSubmitted`, draft sync identifiers, the kitty and modifyOtherKeys escapes, and the TextField key handler including `deleteToLineStart`/`deleteToLineEnd`.
- Grepped the two old Node bundles for `setCursorPositionHint` (0 in 01-15, 4 in 04-01) and the 04-01 hint strings.
- Fetched `ampcode.com/docs/cli/keybindings` and the npm registry.

What I changed:
- The real cursor is not just "hidden": Amp moves it onto the SGR 7 cell with CUP each frame, and both the cell and the CUP are gated on terminal focus (source, `Q_()`).
- Ctrl+U / Ctrl+K join lines at a line boundary. The lane's "leaves other lines alone" was only true mid-line.
- Added keys the recipe missed: Ctrl+Y yank and the Ctrl+P/N history keys as text-replacing keys, plus the kitty forms of Ctrl+J, Alt+Enter, Ctrl+Y and Esc. Added lock-bit masking and `:` sub-field handling. Added Backspace-at-0 removing an image. Added focus reports to the skip list.
- The kitty path is marked **unmeasured**: `>5u` never appears in any capture because tmux did not answer `?u`. Added the Easter-egg flags-31 push as a low-impact note.
- Added that ↑ at the start of the draft falls through to message navigation (source + docs). The lane had only Tab/Shift+Tab, which are backed by the in-app shortcuts text (source), not by the docs page.
- Corrected the sync-pair counts (750/750, and 395/396 in `raw6`). Release pace is about 8 a day, not 10.
- Noted that the cwd label is SGR 2 dim, so a dim-based empty test borrowed from Claude Code must look only at the text rows.
- Downgraded to "lane only" the claims with no saved capture: the old-build screen shapes, soft-wrap space loss, the long-word break, the wheel scroll step, the focus-out capture, "no local file after quit", and the `@` popup and `?` help overlays.
