# Qwen Code (`qwen`) input-box profile, verified (0.24.6)

Tags: **measured** = a capture exists or I re-ran the replay; **measured (lane)** = the lane measured it but kept no output file I could re-read; **source** = checked in the qwen-code tree at `3d49ece1166f` (packages/cli 0.24.6, paths relative to `packages/cli/src/`); **docs** = external spec; **guess** = inference nobody measured.

## Bottom line for unsent

- The empty-box hint is drawn in a truecolor foreground, never SGR 2 dim. `claudeBox`'s `faintFrom` test returns false on it, so reusing it would save "Type your message or @path/to/file" as a draft. A Qwen reader needs per-cell foreground color in `screenRow` (today it only has `faint`), with the placeholder strings as the fallback. **measured** (vt replay below) + **source**.
- The box is a full-width `─` rule, rows starting `> ` (ASCII `>` plus U+0020), a bottom `─` rule, no side borders. **measured**.
- Wrap width is `floor(cols*0.9) - 6`, the box stops at a fixed 10 rows and scrolls one row at a time. **source** + **measured (lane)**.
- Ground truth for real-app tests: Ctrl+X hands the exact buffer (placeholders unexpanded, no trailing newline) to `$EDITOR`. **measured**.
- Correction from the lane: Ink itself wraps frames in `ESC[?2026h`/`l` on any TTY, so frames are very likely bracketed in tmux and Terminal.app too; the lane's "no pairs there, debounce instead" is contradicted by the source. **source**, not measured.

## How it was measured (lane setup, re-checked)

- Install: `npm install --prefix /tmp/qwenprof/prefix @qwen-code/qwen-code@0.24.6`; npm has 135 stable versions, 12 of them dated 2026-09-03 to 2026-09-26 (0.23.0 to 0.24.6). **measured** (I re-ran `npm view ... time`).
- No login: isolated `HOME`/`QWEN_HOME=/tmp/qwenprof/home/.qwen`, `settings.json` with `security.auth.selectedType: "openai"`, `model.name: "dummy-model"`, telemetry and auto-update off, `ui.autoModeAcknowledged: true`; env `OPENAI_API_KEY=dummy`, `OPENAI_BASE_URL=http://127.0.0.1:9/v1`. **measured** (files present).
- Harnesses: a private tmux server (100x30, killed), and a Python pty harness `rawcap.py` with `TERM_PROGRAM=iTerm.app` and `TMUX`/`SSH_*` removed, whose bytes are in `/tmp/qwenprof/raw.bin` (193,741 bytes). Replay into `charmbracelet/x/vt` (`vt@v0.0.0-20260924144451`) via `/tmp/qwenprof/vtprobe/main.go`. **measured**.
- Nothing submitted: `tmp/<hash>/logs.json` is `[]`; `~/.qwen` does not exist. **measured** (re-checked).
- Note: `rawcap.py` sets `EDITOR` but the default launch was in **Auto** approval mode (footer "Auto mode (shift + tab to cycle)"), so the prefix color below is Auto's, not Default's. **measured**.

## TUI stack

- Ink 7.0.3 + React ^19.2.4 (`packages/cli/package.json:76,89`). **source**.
- Ink renders with `alternateScreen: useVP` and `maxFps: 60` when VP is on (`ui/startInteractiveUI.tsx:328-339`), so redraws are throttled to about 16 ms. **source**.
- Opt-in OpenTUI renderer: `QWEN_TUI_RENDERER=opentui` with Bun >= 1.3.0 or Node >= 26.4.0 (`ui/opentui/renderer-selection.ts:24,54-55`). This machine has Node 26.10.0. Its input claims the same chrome (`ui/opentui/input-prompt.tsx`, not re-read by me). Never captured. **source** / **guess** for "same chrome".

## Screen mode and terminal traffic

- Alternate screen by default: one `ESC[?1049h` at byte 192, one `?1049l` at byte 193,697. **measured**. Default comes from `shouldUseVirtualViewport` = interactive && `(useTerminalBuffer ?? true)` && !screenReader (`ui/utils/terminal-buffer.ts:44-51`; schema default `true`, `config/settingsSchema.ts:1267-1272`). **source**.
- Mouse and focus tracking on: `?1002h` (byte 6049), `?1006h` (6057), `?1004h` (6074). **measured**. `ui.mouseTracking` default `true` (`settingsSchema.ts:1287-1292`); clicks in the prompt move the cursor. **source**. unsent's stdin passthrough will carry `ESC[<b;x;yM/m` and `ESC[I`/`ESC[O`.
- Startup queries: `ESC[?u` (byte 0), `ESC[c` (byte 4), `ESC]11;?` (byte 7). **measured**. If the terminal answers the kitty query, qwen pushes `ESC[>1u` (`ui/utils/kittyProtocolDetector.ts:14,117`) and re-pushes it after entering the alt screen (`startInteractiveUI.tsx:340-350`). **source**. My pty did not answer, so 0 pushes in the capture. **measured**. Under unsent the real terminal answers through the passthrough, so on kitty/Ghostty/WezTerm-style terminals delete keys arrive as CSI-u (e.g. Ctrl+W = `ESC[119;5u`); unmodified Enter/Tab/Backspace stay legacy. **docs** (kitty keyboard protocol, flag 1 "disambiguate").

### Synchronized output (corrected)

Two independent emitters, **source**:
1. Ink 7's own `bsu`/`esu` around every throttled frame, enabled by `shouldSynchronize(stream) = stream.isTTY && !isInCi` (bundled `chunks/chunk-XZQH4UBR.js`, Ink `log-update` path). No terminal or TMUX check.
2. qwen's stdout wrapper (`ui/utils/synchronizedOutput.ts:34-120`), which opens a frame on the first write of a tick and closes it on a microtask. It is on only when `QWEN_CODE_FORCE_SYNCHRONIZED_OUTPUT=1` or `QWEN_CODE_SYNCHRONIZED_OUTPUT=1`, or when `TMUX`, `SSH_TTY`, `SSH_CLIENT` are unset and `TERM_PROGRAM` is WezTerm / iTerm.app / WarpTerminal / ghostty or the terminal is kitty. `QWEN_CODE_DISABLE_SYNCHRONIZED_OUTPUT=1` or `QWEN_CODE_SYNCHRONIZED_OUTPUT=0` turn it off.

In the iTerm capture the markers come **nested**: the sequence is `hhll` repeated 29 times then `hlhl` (60 begins, 60 ends). **measured**. Each outer pair is the inner pair plus 8 bytes, so nothing sits between the inner and outer `l`. Only 200 bytes fall outside any pair (the startup queries and the title OSC before the first frame, and `OSC 0/2` + `?25h` at exit). **measured**. The lane's "40 pairs for 20 keystrokes (2 per key)" is one frame per key, double-wrapped. unsent's `trackFrames` (last-marker-wins, `wrap.go:273-286`) handles this nesting correctly.

In tmux or Terminal.app, wrapper 2 is off but Ink's pair should remain, so frames should still arrive as single pairs. **source**, not measured. The lane's claim that there are no pairs there had no capture behind it and I removed it. If `CI` is set, Ink's pairs go away too. **source**.

- Redraw style: Ink log-update erases and rewrites the frame with `ESC[2K` + cursor-up; `ESC[H` appears once (byte 208, startup). About 6.85 KB per keystroke frame at 100x30. **measured**.

## What the box looks like (Ink, default)

vt replay of `raw.bin` at byte 189,615 (empty box after Ctrl+C), rows 10-12 of 30, 100 columns. **measured** (re-run):
```
10 "────────────────────────────────────────────────────────────────────────────────────────────────────"
11 ">   Type your message or @path/to/file                                                              "
12 "────────────────────────────────────────────────────────────────────────────────────────────────────"
cursor 2 11
cell(0,11)=">"  fg={137 180 250}   (Auto-mode prefix color)
cell(1,11)=" "  fg={137 180 250}
cell(2,11)=" "  fg=<nil> bg={212 212 212}   (software cursor cell)
cell(3,11)=" "  fg={108 112 134}
cell(4,11)="T"  fg={108 112 134}  faint=false
... every hint cell fg={108 112 134}, faint=false
```
Below it the footer rows `  ➜ scratch · dummy-model` and the mode line. **measured**.

Typed state at byte 182,761 (after typing `do nothing; reply ok`, Ctrl+J, `abc`). **measured** (re-run):
```
10 "────…────"
11 "> do nothing; reply ok"
12 "  abc ​"
13 "────…────"
```
And one character in (byte 38,251): `cell(2)="d" fg=<nil>`, `cell(3)=" " bg={212 212 212}` (cursor), `cell(4)="​" width=0`. **measured**.

Faint count in the whole stream: 0 matches for `ESC[2m` or any `;2m` SGR. The hint color `38;2;108;112;134` appears 457 times. **measured**.

Structure (`ui/components/BaseTextInput.tsx:347-414`). **source**, consistent with the capture:
- Top rule is its own `<Text>`: `'─'.repeat(columns)`, or with a label `'─'×(columns-labelWidth) + ' ' + label + ' ──'`. The label is truncated to fit (`truncateToWidth`, added in a369b4fac6, 2026-08-24). The label is `voiceStatusLabel ?? uiState.sessionName` (`ui/components/InputPrompt.tsx:2394`); `sessionName` is set by the title-recorded callback of the chat-recording service (auto-title, /rename, resume) (`ui/AppContainer.tsx:1834-1850`, also `:1255`). The labelled form was never captured. **source**.
- Body: Ink `Box` with `borderStyle="single"`, bottom border only, no sides.
- Prefix, `InputPrompt.tsx:2327-2359` and `approvalModeVisuals.ts:29-45`. **source**; `>` and `!` measured (lane):
  - `>` in default, plan, auto-edit and auto modes; `*` in YOLO; `!` in shell mode; `(r:) ` + ` ` (6 columns) in reverse search or command search.
  - Always followed by a plain U+0020.
  - Color: approval-mode color (`theme.text.link` for Auto, `status.warningDim` auto-edit, `status.errorDim` YOLO), status color in shell/voice, else `theme.text.accent`.
- `>` is on the first *visible* row only; continuation rows are indented 2 columns. **measured**.
- Attachments render as `Attachments: [file]` above the top rule; suggestions below the bottom rule. **source**.
- The box is not rendered unless input is active: config initialized, no init error, not processing (unless compression pending), and stream Idle or Responding (`AppContainer.tsx:338-358`). It vanishes during tool confirmation and slash-command processing. **source**. The reader must keep the last draft then.

### Empty-box hint

- Text: `'  ' + t('Type your message or @path/to/file')`; vim mode `'  ' + t("Press 'i' for INSERT mode and 'Esc' for NORMAL mode.")` (`ui/components/Composer.tsx:141-145`). **source**.
- Render: when `showCursor`, the first character (a space) goes through `renderSoftwareCursor`, the rest in `theme.text.secondary`; when `showCursor` is false the whole placeholder is `text.secondary` with no cursor cell (`BaseTextInput.tsx:381-390`). **source**. So an emptiness test must not require the cursor cell (lane recipe did).
- Localized in 8 languages besides English: ca, de, fr, ja, pt, ru, zh, zh-TW (9 locale files including `en.js`). **source**. Lane said "9 locales"; corrected.
- A prompt suggestion takes the same slot in the same style: `placeholder={availableSuggestion ?? placeholder}` (`InputPrompt.tsx:2390`). **source**. Reading it as empty is right: it is not user text.
- Theme dependence: `108;112;134` is the default theme's `text.secondary` here. **measured**. Other themes and NO_COLOR change or drop the color. **source (lane)**, not re-checked.

### Typed text, ghost text, cursor

- Plain typed text is the terminal default fg in the default theme (`fg=<nil>`). **measured**. `/commands` and `@paths` use `theme.text.accent`; ghost completion text after the cursor uses `theme.text.secondary` (`InputPrompt.tsx:2022-2127`). **source (lane)**, not re-read by me.
- The real cursor is placed at the insertion point: `x = boxLeft + prefixWidth + stringWidth(textBeforeCursor)`, `y = boxTop + relativeRow + 1` (the +1 skips the top rule) (`BaseTextInput.tsx:175-195`). **source**; measured at (2,11) empty and (3,11) after one character.
- Software cursor: `chalk.bgHex(...)` block, or `chalk.underline` when `TMUX` is set without forced sync output, or on win32 (`ui/utils/software-cursor.ts:89-110`). **source**; bg `212;212;212` measured.
- At end of line the cursor cell is followed by U+200B, which x/vt stores as its own width-0 cell (`cell(4,11)="​" width=0`). **measured**. `snapshot` maps width-0 cells to `""` (`screen.go`), so it drops out. The ZWSP does take a column index in x/vt, which is harmless only because nothing follows it on that row.

## Wrapping, height, scrolling

- Wrap width: `max(2, floor(cols*0.9) - 4 - 2)` (`ui/utils/layoutUtils.ts:18-39`, used at `AppContainer.tsx:1490-1492,1543`). The lane cited `utils/layoutUtils.ts`; the real path is `ui/utils/`. **source**. At 100 columns a 100-character word broke at 84. **measured (lane)**.
- Word wrap breaks at the last space in the chunk and skips one space after the chunk (`ui/components/shared/text-buffer.ts:955-1060`); a word longer than the width hard-breaks at exactly the width. **source**. Open: a row of exactly `width` characters can be a hard break or a full chunk followed by a swallowed space; the un-wrapper has to decide. **guess**, needs a capture.
- Height: `viewport: { height: 10, width: inputWidth }` (`AppContainer.tsx:1543`), fixed, not window-dependent. **source**; 12 visual rows showed 10. **measured (lane)**.
- Scroll: cursor above window sets scroll = cursor row; below sets `cursor - height + 1`, clamped (`text-buffer.ts:2188-2209`). One row at a time, unlike Claude's jump. **source**; 10×Up moved the window 1 row. **measured (lane)**.

## Keys

All **source** from `config/keyBindings.ts:124-256`, `ui/components/BaseTextInput.tsx:250-300`, `text-buffer.ts:2659-2706`, unless marked.

- Submit: bare Enter (no ctrl/cmd/shift/paste). While a completion list is open, bare Enter and Tab accept the suggestion instead (`ACCEPT_SUGGESTION`).
- Newline: Ctrl+Enter, Cmd+Enter, Shift+Enter, Ctrl+J, Enter inside a paste; also Meta+Enter (Esc CR) and `\` then Enter. Ctrl+J, Esc+Enter and `\`+Enter measured in tmux, including immediately after startup. **measured (lane)**.
- Delete before the cursor: Backspace/`0x7f`, Ctrl+H; word left: Ctrl+W, Alt/Ctrl+Backspace, Cmd+Backspace, `0x1f`; Ctrl+U to line start. Backspace right after a paste placeholder removes the whole placeholder. **source (lane)** for the placeholder rule.
- Delete after the cursor: Delete, Ctrl+D; word right: Alt+D, Ctrl/Alt+Delete; Ctrl+K to line end.
- Whole buffer: Ctrl+C when non-empty (`buffer.setText('')`), which also arms "Press Ctrl+C again to exit"; Esc twice within 500 ms (`InputPrompt.tsx:1314-1325`). Both **measured (lane)** too.
- Text that comes back without typing: Ctrl+Z undo / Ctrl+Shift+Z redo; Up/Down or Ctrl+P/Ctrl+N at the edge replace the text with a submitted prompt and restore the original on the way back (`ui/hooks/useInputHistory.ts`); Up at the top with queued messages pops the queue into the box (`InputPrompt.tsx:1675-1684`); Esc while Responding pulls the prompt back (`AppContainer.tsx:3477-3540`, lane citation, not re-read). Added Ctrl+P/N and the queue pop, which the lane missed.
- Ctrl+S (`SHOW_MORE_LINES`) with text stashes the prompt without clearing it (`InputPrompt.tsx:1121-1136`).
- External editor: Ctrl+X alone (`keyBindings.ts:234-237`); the `BaseTextInput` comment says "Ctrl+X Ctrl+E", which is stale. Qwen writes a temp `buffer.txt` and runs `preferredEditor` / `$VISUAL` / `$EDITOR` / `vi`. **source (lane)** for the editor order. `/tmp/qwenprof/edcopy.txt` is 57 bytes, `[Pasted Content 1001 chars][Pasted Content 1001 chars] #2`, no trailing newline. **measured**.

## Pastes

- Placeholder `[Pasted Content N chars]`, then ` #2` etc. for another live paste with the same N; smallest free id per N (`InputPrompt.tsx:597-614`; parse regex at `:347`). **source**.
- N = code points after CRLF/CR → LF; placeholder when `N > 1000` or `split('\n').length > 10` (`InputPrompt.tsx:232-233,1083-1106`). A trailing newline counts as an extra line. **source**.
- Lane measurements consistent with that rule: 10 lines inline, 11 lines/21 chars placeholder, 1000 chars inline, 1001 placeholder, second 1001 got ` #2` (the last is also in `edcopy.txt`). **measured (lane)** / **measured**.
- Doubtful row: the lane reported `seq 1 12` as `[Pasted Content 63 chars]`. `seq 1 12` with its trailing LF is 27 characters, and nothing in the code path adds characters. The number is unexplained; do not build a test on it. **flagged**.
- A paste of only image paths becomes attachment chips, not text (`InputPrompt.tsx:1093-1101`). **source**.
- unsent's `paste.go` needs the regex `\[Pasted Content (\d+) chars\](?: #(\d+))?` matched to captured bracketed pastes by normalized code-point count and order; Claude's `[Pasted text #N +M lines]` does not apply. **source**.

## Built-in persistence

- No automatic draft saving: grep for every typed test string under `/tmp/qwenprof/home/.qwen` finds nothing (positive control: `dummy` matches `settings.json`), although `edcopy.txt` proves the text was in the buffer. **measured**.
- Ctrl+S stash: `<project dir>/prompt-stash.json` as `{version:1,text}` with placeholders expanded (`services/prompt-stash.ts:15-47`), restored into the box at startup (`AppContainer.tsx:1553-1565`). **source**. A restored stash means the first frame is not the placeholder.
- History (`logs.json`) holds only submitted prompts. **source (lane)**.

## Startup timing

**measured (lane)**, no outputs saved; `early.py`/`afterbox.py` exist but only print a line each run:
- Box drew 2 to 9 s after launch.
- Keys sent 0.1 or 0.5 s after the placeholder appeared were kept 6/6.
- Keys sent before the box drew were sometimes dropped (lane: 5 of 11 tries in roughly 0.5 to 3 s; in tmux, 0.5 s and 2 s both dropped). The lane also says keys at 0.3 to 1.8 s were "usually kept" by `utils/earlyInputCapture.ts`; the two ranges overlap, so the only safe reading is "before the box, unreliable".
- Rule for scripted tests: wait for the placeholder, then about 1 s. No need for Claude's 9 s wait.

## Detection recipe (corrected)

`qwenBox(s *screen)`; needs per-cell fg (and ideally bg/underline) in `screenRow`.

1. Scan bottom-up for row `y` whose text starts with `> `, `* ` or `! ` (U+0020), or is only the marker. Rows starting `(r:)  ` are search mode: not a draft, keep the last draft.
2. Row `y-1` must be the top rule: `^─+$`, or `^─+ .+ ──$` (session title or voice label, possibly truncated), spanning about `cols`.
3. The first all-`─` row `z > y` is the bottom rule; box rows are `y..z-1`, at most 10.
4. Text starts at column `prefixWidth` (2, or 6 in search). Wrap width `max(2, floor(cols*0.9)-6)`. Un-wrap by re-inserting one space at soft breaks; a width-long row with no space is a hard break; a width-long row with spaces is ambiguous (see Wrapping).
5. Empty: `z == y+1` and either (a) every non-blank cell from column 2 on has the same non-default fg (the hint color), ignoring a cursor cell that has bg set or underline, or (b) `textFrom(2)` equals the English, vim or one of the 8 localized placeholders (NO_COLOR fallback). Do not require the cursor cell: with `showCursor` false the hint has none.
6. Ghost text: on the cursor row, drop cells at or after the cursor column whose fg equals the hint color. Width-0 U+200B already becomes `""`.
7. Cursor: row = `curY - y`, col = `curX - prefixWidth`.
8. Placeholders: see Pastes.
9. Never read inside `ESC[?2026h…l`. Expect nested pairs on iTerm/WezTerm/Warp/Ghostty/kitty and single Ink pairs elsewhere (not measured outside iTerm). unsent's existing tracker needs no change; keep the 100 ms wait cap.
10. deleteKeys for Qwen: before cursor `0x7f`, Ctrl+H, Ctrl+W, Alt/Ctrl/Cmd+Backspace, `0x1f`, Ctrl+U; after cursor Delete, Ctrl+D, Alt+D, Ctrl/Alt+Delete, Ctrl+K; whole buffer Ctrl+C with text, Esc Esc within 500 ms. Text can return via Ctrl+Z/Ctrl+Shift+Z, history (Up/Down, Ctrl+P/N) and the queue pop. Accept kitty CSI-u forms; ignore SGR mouse and focus reports, and remember a mouse click can move the cursor with no key.
11. Real-app ground truth: `EDITOR=<copy script>` + Ctrl+X, after waiting for the placeholder plus about 1 s.

## What would break the reader

1. Release cadence: 12 stable releases in 23 days; `BaseTextInput.tsx` has 17 commits from 89f8751233 (2026-03-10, rule-style box) to e88f37f85d (2026-09-01). **measured** (GitHub commits API re-run). The separate labelled top rule came in **0c423deedf (2026-04-22, session rename/auto-title #3093)**, not 7a1b244d34 as the lane said; label truncation came in a369b4fac6 (2026-08-24). **source** (commit patches).
2. Labelled top rule once a session has a title. **source**, not captured.
3. Theme, NO_COLOR, locale and vim mode change the hint's color or text. **source**.
4. The 0.9 fraction, the 6-column overhead and the 10-row viewport are plain constants. **source**.
5. Ghost text styled like the hint; without fg data it would be saved. **source**.
6. The box unmounts during dialogs and slash commands. **source**.
7. `ui.useTerminalBuffer: false` renders on the main screen with scrollback; stale frames above could match a bottom-up scan. **source** / **guess** for the stale-frame risk.
8. OpenTUI renderer: different paint path, never captured. **guess** that the chrome matches.
9. Kitty mode changes delete-key bytes on terminals that answer `ESC[?u`. **source** + **docs**.

## Open questions

- Capture the labelled top rule after `/rename` (needs a real session; this lane never submits).
- Capture in tmux and Terminal.app to confirm Ink's single `?2026` pairs.
- Capture OpenTUI (`QWEN_TUI_RENDERER=opentui`, Node >= 26.4).
- Delete-key bytes on a terminal that answers `ESC[?u`.
- The `seq 1 12` → 63 chars result.
- A width-long row followed by a space: which way does it wrap?
- Hint color strategy: learn it from the first empty frame (fails when a stash is restored), map known theme colors, or match strings.

## Verification notes

What I checked:
- Re-read `/tmp/qwenprof`: `raw.bin`, `edcopy.txt` (xxd), `rawcap.py`, `early.py`, `afterbox.py`, `run.sh`, `ed.sh`, `home/.qwen` contents, `install.log`.
- Re-counted escape sequences in `raw.bin` (1049h/l, 1002/1006/1004, 2026 h/l with order and bytes outside pairs, DA1, OSC 11, kitty query/push, faint SGR, hint color, `ESC[H`).
- Re-ran the vt replay (`vtprobe`) at bytes 38,251, 182,761 and 189,615 and read cell styles for the prompt row.
- Read in the source clone (confirmed at `3d49ece`, version 0.24.6, ink 7.0.3): `layoutUtils.ts`, `AppContainer.tsx` (viewport, stash restore, `isInputActiveForState`, sessionName), `BaseTextInput.tsx` (cursor, top rule, placeholder render, Ctrl+C/K/U/W/X), `InputPrompt.tsx` (prefix, prefix width, placeholder slot, paste thresholds and ids, Ctrl+S, history, queue pop, Ctrl+C), `Composer.tsx`, `approvalModeVisuals.ts`, `software-cursor.ts`, `synchronizedOutput.ts`, `terminal-buffer.ts`, `kittyProtocolDetector.ts`, `renderer-selection.ts`, `keyBindings.ts`, `text-buffer.ts` (wrap and key map), `settingsSchema.ts` defaults, `i18n/locales`.
- Grepped the installed bundle for Ink's `?2026` emitters and its `shouldSynchronize` condition.
- Re-ran `npm view ... time` and the GitHub commits API for `BaseTextInput.tsx`, and fetched patches for 0c423deedf, 7a1b244d34 and a369b4fac6.
- Did not launch qwen or any other agent CLI.

What I changed:
- Synchronized output: the pairs are nested (qwen wrapper outside Ink's own pair), and Ink emits pairs on any non-CI TTY, so the lane's "no pairs in tmux/Terminal.app, debounce instead" is contradicted by source; downgraded to unmeasured with the opposite expectation. "2 pairs per key" restated as one double-wrapped frame per key.
- Emptiness test no longer requires the cursor cell (the placeholder renders without one when `showCursor` is false). Cursor column uses `prefixWidth`, not a fixed 2.
- Commit history: labelled top rule dates to 0c423deedf (2026-04-22), not 7a1b244d34.
- Locales: 8 translations plus English, not 9 translations.
- Path fix: `ui/utils/layoutUtils.ts`; wrap formula includes the `max(2, …)` floor.
- Added missed key facts: Enter/Tab accept a completion while the list is open; Ctrl+P/Ctrl+N history; Up pops queued messages into the box; `QWEN_CODE_FORCE/DISABLE_SYNCHRONIZED_OUTPUT`; Ink `maxFps: 60`.
- Flagged the `seq 1 12` → 63 chars row as inconsistent with the code (27 chars expected) and the overlapping early-input timing ranges as unreliable; startup timing, wrap-at-84, 10-row cap and scroll-by-one are measured by the lane with no saved output.
- Dropped the lane's first open question (about a relayed user request unrelated to this profile).
- Kept: alternate screen, mouse/focus tracking, startup queries, box structure and glyphs, hint color and zero faint, ZWSP width-0 cell, cursor placement, paste format and thresholds, Ctrl+X ground truth, no auto-save, stash behavior, release cadence numbers.
