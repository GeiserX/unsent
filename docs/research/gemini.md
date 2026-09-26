# Gemini CLI (`gemini`): input-box profile for unsent (verified)

**Tags.** **measured**: seen on Gemini CLI 0.61.0 (npm, published 2026-09-24 00:04 UTC, GitHub release dated 2026-09-23) in a private tmux server (`tmux -L gemprof -f /dev/null`, tmux 3.7c, 100x40 / 100x30, `TERM=tmux-256color`, `COLORTERM=truecolor`), throwaway `HOME`, dummy API key. "measured (capture)" means a raw `script` log in `/tmp/gem-prof/raw*.log` still shows it; "measured (lane only)" means the lane reported it but kept no capture. **source**: read in `google-gemini/gemini-cli` at main `2fe7c2d3f065` (2026-09-25, 0.63.0-nightly.20260923), paths relative to `packages/cli/src/`; "(+0.61.0)" means also found in the installed 0.61.0 bundle. **docs**: settings schema descriptions. **guess**: inference.

Captures on disk: `/tmp/gem-prof/raw.log` and `raw4.log` (default, main screen), `raw3.log` (`ui.useAlternateBuffer=true`), `editor-copy.txt` (Ctrl+G output). Nothing was submitted; the keychain was not written.

## 1. TUI stack and screen mode

- Ink, Google fork `"ink": "npm:@jrichman/ink@6.6.9"`, plus React. **source** `packages/cli/package.json:52`.
- **Main screen by default.** `ui.useAlternateBuffer` default `false` **source** `config/settingsSchema.ts:798-806`. `raw.log`/`raw4.log`: no `ESC[?1049h`, no `ESC[2J`, 46 / 34 `ESC[nA` cursor-up moves (Ink log-update redraw). Banner and history print above as static output and scroll into scrollback. **measured (capture)**
- **Alternate screen is opt-in** via `ui.useAlternateBuffer=true`. `raw3.log`: one `ESC[?1049h`, one `ESC[2J`, `ESC[?1002h`/`ESC[?1006h` mouse tracking (twice each), `ESC[?7l` (autowrap off), and **17** matched `ESC[?2026h`/`l` synchronized-output pairs. Default mode: 0 sync pairs, and `ESC[?1002l`/`ESC[?1006l` once. **measured (capture)** (the lane said 3 sync pairs; the capture has 17.)
- `ui.terminalBuffer=true` makes the UI *size* itself as in alternate-buffer mode (`isAlternateBufferEnabled = useAlternateBuffer || useTerminalBuffer`, `ui/hooks/useAlternateBuffer.ts:15`), but the `alternateBuffer` flag handed to Ink comes only from `useAlternateBuffer` and is forced off in screen-reader mode (`interactiveCli.tsx:69-72,161`). Whether Ink's `terminalBuffer` mode itself switches to the alternate screen was not checked. **source**; the effect on 1049h is **unverified**.
- Startup queries: `ESC[8m`, `ESC[?u` (kitty keyboard query), `ESC]11;?` (background colour), `ESC[>q`, `ESC[>4;?m`, `ESC[c`; then `ESC[?1004h` (focus events), `ESC[?2004h` (bracketed paste). **measured (capture)**
- Window title via OSC 0, e.g. `◇  Ready (scratch)`. **measured (capture)** Controlled by `ui.dynamicWindowTitle` (default true). **docs**

## 2. What the box looks like

Layout: `ui/components/InputPrompt.tsx:1791-1929`, `ui/components/shared/HalfLinePaddedBox.tsx`. **source** Screen at 100 columns, default settings, truecolor:

```
────────────────────────────  <- SGR 2 dim, fg 135,135,135: status separator, NOT the box
 Shift+Tab to accept edits    <- approval-mode hint line
▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄  <- 100 x U+2584, fg 95,95,95 (= input background)
 > do nothing; reply ok. w1…  <- col0 ' ', col1 marker, col2 ' ', text from col 3; bg 95,95,95
   w7-abcdefgh w8-abcdefgh …  <- continuation rows: 3 spaces
▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀  <- 100 x U+2580, fg 95,95,95
 workspace (/directory)   sandbox   /model   <- footer
```

Frame glyphs, dim separator, marker and hint colours are **measured (capture)** in `raw.log`; the two text rows are **measured (lane only)**. In alternate-buffer mode (`raw3.log`) the `▄`/`▀` rows are still 100 wide. **measured (capture)**

The RGB values are the default theme on this terminal. The input background is `theme.background.input` blended with the theme's primary background (#18802), so other themes and terminal backgrounds give other values. **source / guess**: do not hardcode RGB.

**Frame variants** (from `InputPrompt.tsx:1735,1798,1916` and `HalfLinePaddedBox.tsx`, **source**):

1. **Default** (`ui.useBackgroundColor=true`, truecolor): `▄` row, content rows with background, `▀` row. **measured (capture)**
2. **`ui.useBackgroundColor=false`**: `HalfLinePaddedBox` returns its children bare, and two full-width `─` rules (Ink `round` border, top/bottom only, no corners) are drawn. Colour = `statusColor ?? theme.ui.focus` when the input has focus, else `theme.border.default`; so it changes with shell / YOLO / plan / auto-edit mode. Measured once as fg 215,255,215, not dim. **measured (lane only)** + **source**
3. **`NO_COLOR` set** (and `useBackgroundColor` left true): `useLineFallback = !!process.env['NO_COLOR']` adds the `─` rules, but `HalfLinePaddedBox` does not look at `NO_COLOR`, so the `▄`/`▀` rows are still drawn, inside the rules. **source (+0.61.0)**. This explains most of the lane's "unexplained" observation (`▄`/`▀` still drawn under `NO_COLOR=1`). The lane did not record whether the `─` rules also appeared, and whether colour escapes are suppressed depends on Ink/chalk's colour detection. **guess**; re-measure.
4. **No truecolor** (`supportsTrueColor()` false), background on: `paddingY=1`, i.e. one blank background-coloured row above and below, **no glyph**. **source**; not reproduced under tmux (tmux passed `COLORTERM=truecolor`).
5. **Screen-reader mode** (`ui.accessibility.screenReader`, default false, or `--screen-reader`): `HalfLinePaddedBox` returns children bare (`isScreenReaderEnabled`), Ink renders in screen-reader mode, and the alternate screen is never entered. No frame glyphs unless `NO_COLOR`/`useBackgroundColor=false` adds rules. **source**; not measured. The lane missed this variant; the reader should return `ok=false` here.

**Markers at col 1**, followed by a space; text starts at col 3 (`InputPrompt.tsx:1820-1845`, **source**):

- `>` normal mode, fg 215,175,255. **measured (capture)**
- `!` shell mode, fg 135,215,215. **measured (lane only)** Typing `!` in an empty box toggles shell mode; the `!` is not in the buffer. The hint line becomes `shell mode enabled (esc to disable)`. **measured (lane only)**
- `*` YOLO mode. **source** (Ctrl+Y did not show it in an untrusted folder. **measured (lane only)**)
- `(r:) ` reverse search: in shell mode (followed by the marker's own space, so `(r:)  `) or command search. The box then holds a search query, not the draft. **source**
- Voice mode (`experimental voiceMode`, default false **docs**): `🎤 ` or a listening indicator is drawn before the marker, shifting every column. **source** `InputPrompt.tsx:1813-1819`.

**Empty box.** One row: ` > `, an inverse-video cell at col 3 (the first placeholder character, a space), then ` Type your message or @path/to/file` in fg 175,175,175 (`theme.text.secondary`). Byte sequence in `raw.log`: `ESC[48;2;95;95;95m ESC[38;2;215;175;255m> ESC[7m ESC[39m ESC[27m ESC[38;2;175;175;175m Type your message…`. **measured (capture)** It is a colour, **not SGR 2 dim**, so unsent's `faintFrom` test would read the hint as a draft. Placeholder default `'  Type your message or @path/to/file'` **source (+0.61.0)** `InputPrompt.tsx:213,1848-1863`. Without focus the whole placeholder is secondary colour with no inverse cell. **source** Voice mode swaps the placeholder text. **source** `InputPrompt.tsx:372`.

**Other noise near the box.** `Press Ctrl+O to expand pasted text` above the frame after a large paste. **measured (lane only)** Suggestions list above or below the box. **source**

**Ghost text.** Rendered via `getGhostTextLines` (`InputPrompt.tsx:1430`) from `completion.promptCompletion`. LLM prompt completion is hard-disabled: `const isPromptCompletionEnabled = false` in `ui/hooks/usePromptCompletion.ts:47` **source (+0.61.0)**. Ghost text therefore comes only from shell-mode path/command completion (`ui/hooks/useCommandCompletion.tsx:253-300`). **source** Ghost rows count toward the box height (`scrollableData`). **source**

## 3. Width, wrapping, height, scrolling, cursor

- **Wrap width** = `mainAreaWidth − 6` (`calculatePromptWidths`, `InputPrompt.tsx:142-155`: border+padding 4, prefix 2). **source** `mainAreaWidth = cols − 1` when `isAlternateBufferEnabled` (alt buffer *or* terminalBuffer), else `cols` (`ui/utils/ui-sizing.ts`). **source**
  - Main screen, 100 cols: 120-char word broke 94 + 26. **measured (lane only)**
  - Alternate buffer, 100 cols: 93 + 27. **measured (capture)** `raw3.log` (`y`×93 then `y`×27).
  - terminalBuffer mode adds a scrollbar gutter to the list width (#26882, `SCROLLBAR_GUTTER_WIDTH`) and may not be on the alternate screen; its wrap width is **unverified**.
- Word wrap keeps the break space at the end of the upper row. **measured (lane only)**
- **Height cap is a constant 10 rows**: `viewport: { height: 10, width: inputWidth }` at `ui/AppContainer.tsx:618`. **source (+0.61.0)** 15 lines at 40 rows showed 10. **measured (lane only)**
- **Scrolls one row at a time** (unlike Claude Code's jump); the marker stays on the first visible row. **measured (lane only)** + **source** (`buffer.visualScrollRow` slice, `InputPrompt.tsx:1893-1897`). No scrollbar outside terminalBuffer mode (#25320, merged 2026-04-13). **source**
- **Cursor.** Terminal cursor hidden (`ESC[?25l`, never `ESC[?25h` in any capture) but moved to the insertion point after each frame: `raw.log` has `ESC[4A ESC[4G` (empty box, col 3 zero-based), `ESC[4A ESC[10G`, `ESC[4A ESC[24G`. **measured (capture)** The visible cursor is an inverse-video cell. **measured (capture)** So x = 3 + display column of the insertion point.
- **Resize** leaves rows of the old frame below the new footer (100→70 columns left three old footer rows). **measured (lane only)**
- **First frame** is drawn near the top (rows 4-10) before the banner prints, then moves down. **measured (lane only)**

## 4. Pastes

- **Threshold**: placeholder when lines > 5 or length > 500: `LARGE_PASTE_LINE_THRESHOLD = 5`, `LARGE_PASTE_CHAR_THRESHOLD = 500` (`ui/components/shared/text-buffer.ts:37-38`). **source (+0.61.0)** 3 and 5 lines inline, 500 chars inline, 501 chars and 6 lines placeholder. **measured (lane only)**; the Ctrl+G copy (section 6) is consistent with it.
- **Format** `[Pasted Text: 6 lines]`, `[Pasted Text: 501 chars]`; a colliding label gets ` #2`, ` #3` … Lines win when both thresholds are crossed. `generatePastedTextId`, `text-buffer.ts:1595-1612`; regex `/\[Pasted Text: \d+ (?:lines|chars)(?: #\d+)?\]/g` at `:41-42`. **source**
  - The suffix counts collisions with labels currently in `pastedContent`; orphans are pruned (`pruneOrphanedPastedContent`), so numbers are reused. Not a global counter like Claude Code's `#N`. **source**
  - Line count = `payload.split('\n').length` after `\r\n|\r → \n` (a trailing newline adds a line); chars = JS `.length` (UTF-16 units) of the normalised payload. **source** `text-buffer.ts:1846-1856`.
- Placeholders wrap across rows (`[Pasted Text:` / `6 lines #2]`). **measured (lane only)**
- Inline (below-threshold) pastes go through `stripUnsafeCharacters`, so they can differ from the pasted bytes. **source** `text-buffer.ts:1874`.
- Ctrl+O expands/collapses a placeholder in place; editing inside an expanded paste detaches it into plain text. **source**
- **Paste of an existing file path** (≥ 3 chars, not shell mode, every segment an existing regular file) is rewritten to `@escaped/path ` with a trailing space. `parsePastedPaths` / `isValidFilePath` (`existsSync && isFile`), `ui/utils/clipboardUtils.ts:496-529`; `escapePastedPaths: true` at `AppContainer.tsx:621`. **source**
- **Image paths display differently**: `@dir/x.png` shows as `[Image x.png]` (base name truncated with `...`), expanding when the cursor enters it. **source** `text-buffer.ts:~890-935`.
- **Enter within 40 ms of a paste becomes a newline** when the kitty keyboard protocol is off (tmux): `setRecentUnsafePasteTime`, cleared by a 40 ms timer. **source** `InputPrompt.tsx:817-845,1261-1272`. The lane omitted the 40 ms window.

## 5. Keys

Defaults from `ui/key/keyBindings.ts:256-411`, platform undo/redo at `:779-807`. **source** Users can rebind them.

- **Submit**: Enter (only when `buffer.text.trim()` is non-empty). A `\` before the cursor turns Enter into backspace + newline. **source** `InputPrompt.tsx:1258-1282`.
- **Tab while the model is generating** queues the message (`QUEUE_MESSAGE`), which empties the box without Enter. **source** `InputPrompt.tsx:756-770`.
- **Newline**: Ctrl+Enter, Cmd+Enter, Alt+Enter, Shift+Enter, Ctrl+J. **source** tmux `send-keys C-j` and `send-keys Escape Enter` both made a newline. **measured (lane only)**
- **Delete before the cursor**: Backspace, Ctrl+H (char); Ctrl+W, Alt+Backspace, Ctrl+Backspace (word); Ctrl+U (to line start).
- **Delete after the cursor**: Delete, Ctrl+D (char; Ctrl+D on an empty box is EXIT); Alt+D, Alt+Delete, Ctrl+Delete (word); Ctrl+K (to line end).
- **Clear all**: Ctrl+C with text (`CLEAR_INPUT`, same key as QUIT). **source**; cleared the box. **measured (lane only)** Esc Esc within 500 ms clears a non-empty box; on an empty box with history it submits `/rewind`. **source** `InputPrompt.tsx:264-286`.
- **Undo**: macOS Cmd+Z, Alt+Z; Linux Alt+Z, Cmd+Z, Ctrl+Z; Windows Ctrl+Z, Alt+Z. **Redo**: Ctrl+Shift+Z, Cmd+Shift+Z, Alt+Shift+Z. **source** Redo brings deleted text back.
- Ctrl+Z is also `SUSPEND_APP` (on Linux it shares the key with undo). **source**
- Vim mode (`general.vimMode`, default false) adds normal-mode deletes (`x`, `dd`, `dw`, …). **source / docs**
- **History**: Ctrl+P/Ctrl+N, Up/Down at the box edges; Ctrl+R reverse search. While browsing, the box shows a history entry; the unsubmitted draft is held in memory (index −1) and restored on returning down. **source** `ui/hooks/useInputHistory.ts:33-92`. A reader would see the history entry as the "draft" meanwhile.
- The lane listed "DEL (0x7f) inside inserted text" as a delete key; not re-checked. **source (unverified)**

## 6. External editor (ground truth)

- **Ctrl+G** (also Ctrl+Shift+G; Ctrl+X is the deprecated binding, switched in #24861, merged 2026-04-08) writes the buffer, paste labels expanded (`expandPastePlaceholders`), to `$TMPDIR/gemini-edit-*/buffer.txt`, runs `general.preferredEditor`, else `$VISUAL ?? $EDITOR`, else `vi`; reads the file back and re-collapses unchanged pastes. **source** `text-buffer.ts:3316-3370`, `ui/utils/editorUtils.ts:~20-110`.
- `editor-copy.txt` is 1054 bytes, 15 lines: `a`…`e` (5-line inline paste), then on the `e` line 500 `0`s (inline), 501 `0`s (the expanded `[Pasted Text: 501 chars]`), then two identical 6-line pastes (` do nothing`,`2`…`6`). **measured (capture)** The box came back with its labels collapsed. **measured (lane only)** This is a byte-exact ground truth, like Claude Code's Ctrl+G.

## 7. Built-in persistence

- **Unsent drafts are not saved.** The only `draft` hits in `cli/src` and `core/src` are JSON-schema "draft-07/2020-12" and prompt text. **source** `~/.gemini/tmp/scratch/logs.json` is `[]` in all three throwaway HOMEs; `grep -r 'do nothing'` over them finds nothing, while the positive control `session_context` matches `chats/session-*.jsonl`. **measured (capture)** (re-run during verification)
- Submitted prompts go to `logs.json` and feed Up-arrow history across sessions (`useInputHistoryStore`, `core/src/core/logger.ts`). **source** Chat session files are created at startup. **measured (capture)**

## 8. Startup and timing (for real-app tests)

- Box appeared after 14-24 s (dummy key, untrusted folder; a parent `node` relaunches a child with `--max-old-space-size=12288`). **measured (lane only)**
- Keys typed at 2 s were lost. **measured (lane only)**
- Once, two sessions started together showed typed text doubled; not reproduced. **measured once, cause unknown**
- Killing the tmux session left both node processes running, re-parented to PID 1. Kill the process tree on teardown. **measured (lane only)**
- First run shows a folder-trust dialog, then an auth dialog; the auth dialog offers to store the key in the system keychain (answer with Esc). **measured (lane only)**

## 9. Stability across versions

- Frame history of `HalfLinePaddedBox.tsx` (GitHub commits API, re-fetched): #16563 (2026-01-26) added the background box with `useBackgroundColor` **default true**; #17634 (01-27) alt-buffer backgrounds; #17914 (01-30) screen-reader mode; #18802 (02-12) blend with theme; #25339 (04-22) "removed background color for input": despite the title it dropped the low-colour fallback and the left/right `│` borders of the line variant and introduced `useLineFallback = !!NO_COLOR`; it did **not** change the default; #29347 (09-17) negative-dimension guard. **source**
- The default is `true` in main (`settingsSchema.ts:826-834`). The lane's claim that the default "flipped at least twice in 2026" is **not supported**; nothing found shows it ever defaulting to false.
- #26882 (2026-05-11) added a scrollbar gutter to the width calculation (only applied in terminalBuffer mode today). **source**
- `InputPrompt.tsx` has 66 commits since 2026-01-01. **source** (re-counted)
- Stable releases weekly: v0.58.0 09-01, v0.59.0 09-08, v0.60.0 09-15, v0.61.0 09-23; plus previews and nightlies. **source**
- Verdict: readable (deterministic layout, truthful cursor, fixed height cap), but InputPrompt churns often, so re-measure on each minor release. **guess**

## 10. Detection recipe for a `geminiBox` extractor

1. **Width**: `w = cols − 6`, or `cols − 7` when the shadow emulator is on the alternate screen. terminalBuffer mode and screen-reader-plus-alt-buffer break this mapping; treat as unsupported (**guess**).
2. **Marker row**: scan bottom-up for col0 `' '`, col1 ∈ {`>`,`!`,`*`}, col2 `' '`. Allow a `🎤 `/listening prefix (shifted columns) or return `ok=false` for it. A row starting ` (r:) ` is a search query: `ok=false`.
3. **Top frame** (row y−1): a full-width run of `▄`; or a full-width `─` rule without SGR 2 (the dim `─` two rows higher is the status separator); or, if background colour is recorded, a blank row carrying the input background. Under `NO_COLOR` expect `─` then `▄` (variant 3).
4. **Bottom frame**: walk down through rows that start with 3 spaces to a full-width `▀` row (or the matching `─` / background-blank row). None found: `ok=false`.
5. **Empty box**: exactly one content row and every non-space cell from col 4 on is drawn in a non-default foreground equal to the hint colour (theme secondary, not SGR 2). `screen.go` must record per-cell foreground (at least "non-default fg") besides `AttrFaint`. Matching the literal `Type your message or @path/to/file` is a fallback only; it misreads a draft typed as that text.
6. **Rows**: `textFrom(3)` of each content row. `capped = rowCount >= 10` (constant). Cursor row = `curY − y`, `cursorEnd = curX >= 3 + width(row)`.
7. **Shell mode**: prefix `!` to `rows[0]`, as `claudeBox` does.
8. **Ghost text**: only in shell mode (completion); drop secondary-coloured text after the cursor and secondary-coloured rows below it.
9. **Pastes**: in `paste.go`, match `\[Pasted Text: (\d+) (lines|chars)(?: #(\d+))?\]` on unwrapped text. Map to captured bracketed pastes by kind and count computed Gemini's way (CR/CRLF→LF, `split('\n')` lines, UTF-16 length), then by order of live collisions.
10. **deleteKeys**: before cursor = Backspace, Ctrl+H, Ctrl+W, Alt+Backspace, Ctrl+Backspace, Ctrl+U; after cursor = Delete, Ctrl+D, Alt+D, Alt+Delete, Ctrl+Delete, Ctrl+K; unlimited = Ctrl+C, Esc Esc (≤ 500 ms), undo (Cmd+Z/Alt+Z on macOS; Ctrl+Z also on Linux). Redo can restore text.
11. **Real-app tests**: wait for the box (14-24 s here); newlines via `send-keys C-j` or `Escape Enter`; pastes via `paste-buffer -p`, and wait > 40 ms before Enter; ground truth from Ctrl+G with a copy script in `VISUAL`; kill the process tree on teardown.

## Risks

- Hint is coloured, not dim: the current `faintFrom` empty test saves the hint as a draft. **measured**
- Five frame variants (half-block, `─` rules, both under `NO_COLOR`, glyph-less padding, none in screen-reader mode). **source**
- RGB values depend on theme and terminal background. **source / guess**
- Screen text can differ from the buffer: `[Image …]`, `@path` rewrites of pasted existing files, stripped control characters, shell-mode ghost text. **source**
- Esc Esc and Ctrl+C wipe the draft; Esc Esc on an empty box sends `/rewind`; Tab while generating queues (empties) the box. **source**
- History browsing puts a history entry in the box; the reader would save it over the draft. **source**
- Stale rows below the footer after a resize could anchor a bottom-up scan. **measured (lane only)**
- Main-screen rendering: the box moves (top first, then bottom). **measured (lane only)**
- Frequent releases (weekly stable, 66 InputPrompt commits in 2026). **source**

## Open questions

- Under `NO_COLOR=1`, do the `─` rules appear together with `▄`/`▀`, and are colours still emitted? Source says rules + half-blocks; needs one capture.
- No-truecolor variant (blank padding rows) still unmeasured; needs a run outside tmux or with `COLORTERM` unset in the child.
- Does Ink's `terminalBuffer` mode use the alternate screen, and what is its wrap width?
- Screen-reader mode layout (marker text, frame) is unmeasured.
- Can unsent's vt emulator expose per-cell foreground colour and the alternate-screen flag?
- The doubled-text startup glitch: cause unknown.

## Verification notes

What I checked, against the lane's own scratch (`/tmp/gem-prof`: 0.61.0 install, `src` clone at `2fe7c2d3f065`, raw logs, `editor-copy.txt`, throwaway HOMEs) and the GitHub API. I launched no agent CLI.

Confirmed:
- Paste thresholds, label format, collision suffix, line/char counting (`text-buffer.ts`), also in the 0.61.0 bundle.
- The 10-row height cap (`AppContainer.tsx:618`, also in 0.61.0).
- The `cols − 6` width formula and `cols − 1` main area in alt mode; `raw3.log` shows the 93 + 27 split.
- Placeholder string and colours (the hint is fg 175,175,175, not SGR 2).
- Frame glyphs and colour, marker colour, hidden-but-positioned cursor, startup query sequences, the no-1049h default.
- The whole keybinding table, the Esc-Esc and backslash-Enter logic, and the Ctrl+G editor flow.
- The 1054-byte editor copy decomposition.
- No persisted drafts: I re-ran the negative grep with its positive control.
- The PR numbers and dates, the 66-commit count, and the weekly release cadence.

Changed:
- Sync-output pairs in alt mode: 17, not 3 (`raw3.log`).
- #25339 did not remove or flip the background default. The "default flipped at least twice" risk is withdrawn: the default has been `true` since #16563.
- `NO_COLOR` does not replace the half-block frame. Source draws `─` rules *and* `▄`/`▀`, which explains most of the lane's "unexplained" measurement.
- `ui.terminalBuffer` changes sizing (`cols − 1`), but the alternate-screen switch comes only from `useAlternateBuffer`. I downgraded the "cols − 7 ⇔ alternate screen" rule to unverified for terminalBuffer.
- Added a missed frame variant: screen-reader mode draws no frame.
- Added the paste-guard window (40 ms) and Tab-queues-message while generating.
- Paste-path rewriting only applies to existing regular files.
- Inline pastes are passed through `stripUnsafeCharacters`.
- History browsing replaces the visible box text.
- Redo restores text.
- The `─` rule colour follows the approval/shell mode.
- Resolved the ghost-text question: LLM prompt completion is hard-disabled (`isPromptCompletionEnabled = false`, main and 0.61.0), so ghost text is shell-mode completion only.
- Recipe step 10 now lists the before-cursor delete keys too; the lane's recipe listed only after-cursor keys and clears.
- Empty-box detection no longer hardcodes RGB.
- Tagged every "measured" claim as backed by a kept capture or lane-only.
- Dropped the lane's first open question, a stray relayed user request unrelated to this lane.
- Corrected minor line citations (placeholder render `:1848-1863`, `editorUtils.ts` range, `useBackgroundColor` schema at `:826`).

Not checked:
- Lane-only measurements with no capture: main-screen 94 + 26 wrap, one-row scrolling, resize leftovers, startup timings, the orphaned processes, and shell-mode colour.
- The DEL 0x7f claim.
