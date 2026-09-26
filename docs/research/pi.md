# pi (earendil-works/pi, formerly badlogic/pi-mono): verified input-box profile

Tags: **measured** = seen in the lane's captures (`/tmp/pi-profile/raw.log`, `raw-fs.log`, `editor-grab.txt`, tmux screens) or re-counted by the skeptic; **source** = read in the code (clone HEAD `2b0a123d`, 2026-09-26, unless a tag is named); **docs** = project docs or registry metadata; **guess** = inference.

Version note (source): the lane measured npm **0.87.1** but cited line numbers from HEAD, which is 50 commits later. Between `v0.87.1` and HEAD, `editor.ts` changed only in the autocomplete trigger regex (6 lines), and `terminal.ts` only in keyboard-protocol reply bookkeeping. The editor behaviour below is the same on both. `editor.ts` line numbers after line ~255 sit 2 lower in 0.87.1 than at HEAD.

## Which project
- `https://github.com/badlogic/pi-mono` answers `HTTP/2 301 location: https://github.com/earendil-works/pi`. **measured** (re-checked).
- npm `@earendil-works/pi-coding-agent` dist-tags: `latest: 0.87.1`, `legacy-node20: 0.74.2`. `@mariozechner/pi-coding-agent` stops at `0.73.1`, modified 2026-05-07. **measured** (re-checked with `npm view`).
- Tag `v0.87.1` is dated 2026-09-22 and `v0.84.4` 2026-08-28, so releases come about weekly. **source** (git tags).
- The binary is `pi`. The editor is `packages/tui/src/components/editor.ts`, and the app wrapper is `packages/coding-agent/src/modes/interactive/components/custom-editor.ts`. **source**
- That this is the "pi" the user means. **guess**

## TUI stack
- pi uses its own library, `@earendil-works/pi-tui` (`packages/tui`), in TypeScript on Node, with line-diff rendering. It does not use Ink, Bubble Tea, ratatui or OpenTUI. **source**

## Screen mode
- The default is `tuiMode: "regular"` (`settings-manager.ts:159,1266`), which renders inline on the main screen. **source**. `raw.log` has 0 × `ESC[?1049h`. **measured** (re-counted).
- On the main screen the box follows the transcript: it sits mid-screen, with blank rows below it, until the transcript fills the window. **measured** (tmux screens, not re-checked)
- Fullscreen can be turned on by the setting `tuiMode: "fullscreen"` **or by the CLI flag `--tui-mode fullscreen`** (`cli/args.ts:215`). **source**. Fullscreen uses the alternate screen: `raw-fs.log` has 1 × `?1049h`. **measured**. The lane saw the box drawn the same way, pinned above the footer. **measured** (screen not re-checked)
- **New:** fullscreen also enables mouse and focus reporting (`?1000h ?1002h ?1004h ?1006h`, `tui-alt-screen.ts:65-66`). unsent's input parser will then see SGR mouse reports and `ESC[I`/`ESC[O` focus events, and must not count them as edits. **source**
- Every redraw is wrapped in `ESC[?2026h … ESC[?2026l`: 69 matched pairs in `raw.log`. **measured** (re-counted). Hardware-cursor-only moves can be written outside a pair (`positionHardwareCursor`). **source**
- A width change, a height change (except under Termux), or a first changed line above the viewport triggers a full redraw, `ESC[2J ESC[H ESC[3J`, which also clears scrollback (`tui-main-screen.ts:283,337-349`). **source**. `raw.log` has 2 × `ESC[2J` and 2 × `ESC[3J`, both right after a Ctrl+G. **measured** (re-counted and located)
- At startup pi writes `ESC[?2004h ESC[>7u ESC[?u ESC[c ESC[?25l ESC]11;? BEL ESC[>4;2m`. That is bracketed paste, a kitty flag push, a kitty query, a DA1 query, cursor hide, a background-colour query and modifyOtherKeys 2. **measured** (first bytes of `raw.log`). Under unsent the real terminal must answer these queries, or pi waits and falls back.
- Every rendered row ends with `ESC]8;; BEL` (an OSC 8 hyperlink close). unsent's emulator must parse and ignore it. **measured** (`raw.log`)

## What the box looks like
Measured at 100×30, paddingX 0:
```
────────────────────────────────────────────────────  (top rule, full width, SGR 38;5;239 by default)
do nothing; reply ok. alpha … mu nu xi                 (content rows, no glyph, no side borders)
omicron pi rho sigma tau upsilon
────────────────────────────────────────────────────  (bottom rule)
/private/tmp/pi-profile/scratch                        (footer line 1: cwd, plus git branch / session name)
0.0%/0 (auto)                                   unknown (footer line 2: stats … model)
```
- **No prompt glyph and no side border.** **measured**
- **Empty box.** One row with `ESC[7m ESC[0m`, a reverse-video space at column 0, then blank padding. **measured** (`raw.log`; corrected: the lane wrote `ESC[7m ESC[39m ESC[0m`). The Editor has no placeholder at all (`layoutText` empty branch, `editor.ts:1000-1007`). **source**
  - So unsent's dim-hint rule does not apply: all-blank content rows mean an empty draft.
  - A draft of only spaces also has blank rows, but the fake cursor then sits at column N, not 0. That tells the two cases apart. **source** (render path), not measured
- **Fake cursor.** `ESC[7m<grapheme>ESC[0m` on the grapheme under the insertion point, or `ESC[7m ESC[0m` at end of line (`editor.ts:575-600`). It is drawn whether or not the editor has focus. On a wide (CJK) grapheme it covers **2 cells**, so a validator must accept "one contiguous SGR-7 run of 1–2 cells", not "exactly one cell". **source** (corrected)
- **Rule colour** follows the thinking level (`getThinkingBorderColor`), `!` bash mode (`getBashModeBorderColor`) and the theme (`interactive-mode.ts:4357-4367`). **source**. `raw.log` has 42 × `38;5;239` and 2 × `38;5;143` (bash mode). **measured** (re-counted). Never key on colour.
- **Scroll labels** are ` ↑ N more ` on the top rule and ` ↓ N more ` on the bottom rule, **centred** (`createScrollBorder`, `editor.ts:278-293`). When the rule is too narrow for a centred label they fall back to a left `─── ↑ N more ─…`, and then to a truncated `...`. **source**. `raw.log` shows a centred ` ↑ 16 more `. **measured**
  - **History corrected:** the lane said commit `37bedb7f` (2026-07-23) moved the label to the centre. It did not: that commit added `createScrollBorder` still left-aligned, with truncation. Centring came with `1d9787c1` (2026-09-03, "prettier Working... spinner", first in **v0.85.0**). Versions before 0.85.0 draw `─── ↑ N more ───…` on the left. **source** (`git log -S`)
- **Working status (while the agent runs).** The default editor is built with `embedWorkingStatus: true` (`interactive-mode.ts:610`), so the top rule becomes `── <status> ────…`. It can also carry the centred ` ↑ N more ` label. When the window is narrow it degrades to `─{0..3}<spinner>─…`, with fewer than 3 leading dashes (`custom-editor.ts:36-78`). **source only.** Not measured, because triggering it needs a submit. The lane's regex `^──\s.+\s─+$` misses the narrow form.
- **Horizontal padding.** Content rows are padded by `editorPaddingX` spaces on each side. The default is 0. **source**. With `editorPaddingX: 2`, rows start with two spaces. **measured** (lane, not re-checked)
  - **Corrected:** the 0–3 clamp is only in the settings-UI setter (`settings-manager.ts:1384`). The getter returns the raw value, and the editor only floors it and caps it at `floor((width-1)/2)` (`editor.ts:377-378,521-522`). A hand-edited value of 5 is honoured. **source**
  - **New:** settings are global (`$PI_CODING_AGENT_DIR` or `~/.pi/agent/settings.json`) deep-merged with project `<cwd>/.pi/settings.json` (`settings-manager.ts:238,350`). To read the padding, unsent needs both files. Inferring it from the cursor column on an empty box is simpler. **source**
  - Padding cannot be told apart from leading spaces in the draft. **source**
- **Below the bottom rule:** the slash or @ autocomplete list, then `widgetContainerBelow`, then the footer (usually 2 lines). The mount order is document, pendingMessages, status, widgetsAbove, **editor**, widgetsBelow, footer (`interactive-mode.ts:939-947`). **source**. An autocomplete list `→ settings … (1/25)` was seen under the rule. **measured**

## Wrapping, height, scrolling
- **Wrap width** is `width-1` when paddingX is 0, otherwise `width-2*paddingX` (`editor.ts:520-527`). **source**. A 150-character word broke at 99/51 at width 100. **measured**
- Word wrap breaks after the last space, which stays invisible at the end of the upper row. CJK can break anywhere. A paste marker is not split unless it is wider than the row (`wordWrapLine`). Nothing marks a soft wrap versus a hard newline. **source**
- **Height** is `max(5, floor(rows*0.3))` content rows (`editor.ts:537`). This is unchanged since v0.50.0 (`git show v0.50.0:…editor.ts:320`). **source**. 9 rows at 30 lines, 5 at 12. **measured** (lane; consistent with the formula)
- **Scrolling** moves one row at a time and keeps the cursor on the edge (`editor.ts:543-552`). **source**. From line 15 of 15, 9 × Up gave `↑ 5 more` / `↓ 1 more` with the cursor on the top row. **measured**. I recomputed this from the code and got the same numbers.
- N counts hidden **visual** rows exactly, so total visual height = N_above + visible + N_below. **source**
- **Hardware cursor** is placed at the insertion point but hidden (`ESC[?25l`; 72 × in `raw.log`) unless `showHardwareCursor` or `PI_HARDWARE_CURSOR=1` is set (`settings-manager.ts:1370`). The zero-width `CURSOR_MARKER` that positions it is only emitted while the editor has focus. **source + measured**

## Pastes
- Bracketed paste is on (`ESC[?2004h`). **measured**
- **Threshold.** A paste becomes a marker when `split("\n").length > 10 || chars > 1000`, counted after normalisation (`editor.ts:1300-1313` at HEAD). **source**. The lane's measurements: 3 lines inline; 10 lines without a trailing newline inline; 1000 chars inline; 11 lines plus a trailing newline gave `[paste #1 +12 lines]`; 1001 chars gave `[paste #2 1001 chars]`. **measured** (both markers are in `raw.log`; the fixture files `p3/p10n/p11/c1000/c1001.txt` are still in `/tmp/pi-profile`)
- **Format.** `[paste #N +M lines]` or `[paste #N C chars]`, regex `/\[paste #(\d+)( (\+\d+ lines|\d+ chars))?\]/g` (`editor.ts:31`). Unchanged since v0.50.0. **source**
- **IDs are not stable. Corrected and expanded:**
  - `setText` (Ctrl+C, submit, Ctrl+G, history recall) clears the paste map and resets the counter to 0 (`editor.ts:1114-1126`). **source**
  - **Backspace over any live marker** (not only the newest) deletes its entry, decrements the counter, and **renumbers every higher marker in the text**: `[paste #3 …]` becomes `[paste #2 …]` (`editor.ts:1393-1418`; commit `8a2ce5a5`, 2026-07-07). The rewrite hits every regex match with a higher id, including a typed look-alike. paste.go must renumber too, or keep its own mapping from marker text to content, rebuilt after each backspace over a marker. **source**
  - Forward delete over a marker removes it without renumbering. **source**
  - A marker is atomic only for ids still in the paste map (`validPasteIds`). **source**
- **Normalisation before storing** (`handlePaste`, `editor.ts:1259-1298`): **source**
  1. CSI-u Ctrl+letter (`ESC[<cp>;5u`) is decoded back to a control byte.
  2. `normalizeText` turns CRLF/CR into LF and a tab into 4 spaces.
  3. Characters below 0x20 other than `\n` are dropped.
  4. A leading space is added when the text starts with `/`, `~` or `.` and the character before the cursor is a word character.
- **Ctrl+V** (`app.clipboard.pasteImage`; `alt+v` on Windows/WSL) reads the clipboard in-process. An image is written to `$TMPDIR/pi-clipboard-<uuid>.<ext>` and its path is inserted. Otherwise the text is inserted through `insertTextAtCursor`: inline, never a marker, and it **never crosses the PTY** (`interactive-mode.ts:3041-3063`). **source**
- **New:** in fullscreen mode, a **right-click** also pastes from the clipboard in-process (`handleRightClickPaste`, `interactive-mode.ts:3027`; `tui-alt-screen.ts:1000`). It is wrapped as a bracketed paste and fed to `handleInput`, so it **can** produce a marker whose content unsent never saw. **source**

## Keys (defaults: `packages/tui/src/keybindings.ts:115-146`, `packages/coding-agent/src/core/keybindings.ts:75-150`)
- **Newline:** `shift+enter`, `ctrl+j`. **source**. tmux `send-keys C-j` inserted a newline. **measured**. `\` followed by Enter also gives a newline (`editor.ts:890-921`). **source**
- **Trap:** `alt+enter` (`ESC\r`) is `app.message.followUp`. When the agent is idle, `handleFollowUp` calls `setText("")` then `onSubmit(text)`, so it **submits** (`interactive-mode.ts:4317-4346`). On Windows/WSL the key is `ctrl+q`. **source**. Never use tmux `Escape Enter` for a newline with pi.
- **Submit:** `enter`. While streaming, Enter queues a steering message and clears the box. Esc while streaming aborts and puts queued messages back into the editor (`restoreQueuedMessagesToEditor`), and so does `alt+up` (`app.message.dequeue`). That puts text in the box the user did not just type. **source**
- **Delete before the cursor:** backspace, **shift+backspace** (new), ctrl+w, alt+backspace, ctrl+u. **source**
- **Delete after the cursor:** delete, ctrl+d (exits when the box is empty), **shift+delete** (new), alt+d, alt+delete, ctrl+k. **source**
- **Whole-draft wipes:**
  - `ctrl+c` (`app.clear` → `clearEditor` → `setText("")`). A second press within 500 ms exits (`interactive-mode.ts:4118-4126`). **source**. One Ctrl+C emptied the box. **measured**
  - Esc in `!` bash mode (`interactive-mode.ts:2963-2966`). **source**
  - **New:** both wipes are **undoable**. `setText` pushes a `structuredClone` undo snapshot that includes the paste map (`editor.ts:1119-1121,2115-2125`; `undo-stack.ts`). So `ctrl+-` (`ctrl+z` on win32, `alt+z` on WSL) brings back the whole draft, markers included. A loss rule that treats Ctrl+C as final will be wrong when the user undoes it straight away. **source** (not measured)
- **Restore:** undo `ctrl+-`, yank `ctrl+y`, yank-pop `alt+y`. **source**
- All of these can be rebound in `~/.pi/agent/keybindings.json`. **source**
- **Keyboard protocol.** pi pushes kitty flags 7 (disambiguate + event types + alternate keys) and sends modifyOtherKeys 2 at startup. If the terminal reports non-zero kitty flags, it turns modifyOtherKeys off again (`terminal.ts`). **source + measured** (both sequences are in `raw.log`). Under unsent, delete keys can reach the PTY as `ESC[119;5u`, `ESC[27;5;119~`, or with release events (`…:3u`). This was not measured under a real kitty-capable terminal. **guess** from source
- **Terminal.app:** when `TERM_PROGRAM=Apple_Terminal` on darwin, pi asks a native helper whether Shift is physically held. If it is, a plain `\r` becomes Shift+Enter, a newline (`terminal.ts:37,64,355-359`). A bare `\r` on the PTY is therefore not always a submit. **source**

## Draft persistence and history
- There is no unsent-draft persistence. A `draft` grep over `coding-agent/src` and `tui/src` finds only unrelated Immer `draft` callbacks. A draft is lost on exit, a crash or a double Ctrl+C. **source**
- Prompt history keeps only submitted prompts: in memory, capped at 100, browsed with Up/Down on the first or last row (`editor.ts:425-470`). `pi -c`/resume refills it from the session (`populateHistory`). Since `e4907b3b` (2026-06-09) the draft comes back when you leave history browsing, in memory only. **source**

## External editor (ground truth)
- Ctrl+G (`app.editor.external`) takes `getExpandedText()`, writes it to `$TMPDIR/pi-editor-*/prompt.md`, prints `Launching external editor: <cmd>` / `Pi will resume when the editor exits.` to the screen, runs the editor (the command string is split on spaces), and on exit 0 `setText`s the file back minus one trailing `\n`. The temp dir is then deleted. On a non-zero exit the draft is unchanged (`external-editor.ts`; `interactive-mode.ts:4438-4454`). **source**. The launch lines and the full redraw that follows are in `raw.log`. **measured**
- **Downgraded:** the lane said the grabbed copy was byte-exact with the r1…r11 paste intact. The surviving `/tmp/pi-profile/editor-grab.txt` comes from a **later** Ctrl+G and holds `q1…q10 E` plus 1000 `y`. Both of those pastes were under the threshold and went in inline, so the file proves no marker expansion. The marker expansion itself is visible on screen: after the first Ctrl+G the box showed `↑ 16 more` and the 1001-char `z…` text where `[paste #1 +12 lines] C [paste #2 1001 chars]` had been. So "expanded" is **measured** (on screen). "Byte-exact file" is **source** only; the capture was overwritten.
- Side effects: afterwards the box holds the expanded text, the markers are gone, the paste map is reset, and a `2J/3J` full redraw clears scrollback. A Ctrl+G check changes the state it is checking. **measured + source**

## Detection recipe for a `piBox` reader
1. Read only between sync pairs, not inside one. Scan the whole shadow screen; on the main screen the box is not pinned to the bottom.
2. From the bottom up, find the **bottom rule**: a full-width row of `─`, optionally with one ` ↓ N more ` label (centred on ≥0.85.0, left `─── ↓ N more` on older versions or narrow widths).
3. Search upward, at most `max(5,floor(rows*0.3))+1` rows, for the **top rule**, which is full width and one of:
   - `^─+$`
   - `^─*\s?↑\s(\d+)\smore\s─*$` (centred or left form)
   - the working form: `^─{0,3}.+─+$` that begins with `─` and whose non-rule part is a status/spinner, which can also contain ` ↑ N more `
4. Check that it is the editor:
   - The content row count is 1…`max(5,floor(rows*0.3))`.
   - Exactly one contiguous SGR-7 run (1–2 cells) sits in the content rows.
   - The row right after the bottom rule is the autocomplete list or the footer cwd row.
   - Otherwise (selector, extension editor via `setEditorComponent`, dialog) fail safe and keep the last draft.
5. Strip `paddingX` leading spaces from each row: learn it from the cursor column on an empty box, or read global+project settings. Trim trailing spaces.
6. All content rows blank and the cursor at column `paddingX` means empty.
7. Use `↑ N`/`↓ N` as the exact hidden visual-row counts for stitch.go.
8. Expand markers with paste.go, after pi's normalisation, **tracking renumbering on backspace**, and treating in-process pastes (Ctrl+V, fullscreen right-click) as unrecoverable content.

## Stability across versions
- Stable since v0.50.0 (`git show v0.50.0:…editor.ts`): the 30% height formula, the paste marker format and thresholds, and the `─` rules with no side border. **source**. The width-1 layout and the SGR-7 fake cursor are **source** at HEAD; the lane's claim that they are unchanged since v0.50.0 was not re-checked.
- Changed:
  - vertical scroll and `editorPaddingX` (2026-01; lane, not re-checked)
  - label truncation (`37bedb7f`, 2026-07-23, v0.82.0)
  - label centring and the working status in the top rule (`1d9787c1`, 2026-09-03, v0.85.0)
  - paste-id renumbering (`8a2ce5a5`, 2026-07-07)
  - **source**. `editor.ts` has 104 commits. **source**
- What would break the reader: weekly 0.x releases, `setEditorComponent` extensions, `editorPaddingX`, fullscreen mode (position, plus mouse/focus bytes), widgets under the editor, `showHardwareCursor`, a marker rename, and a future placeholder (it would look like draft text). **source/guess**

## Measurement setup (lane)
- Installed with `npm install --prefix /tmp/pi-profile/npm --ignore-scripts @earendil-works/pi-coding-agent@0.87.1` (version re-checked from the installed package.json). It ran under `env -i` with `HOME` and `PI_CODING_AGENT_DIR` pointed into `/tmp/pi-profile`, `PI_OFFLINE=1`, no API keys, and `EDITOR=/tmp/pi-profile/grab-editor.sh` (`run-pi.sh`). A private tmux socket was used. Nothing was submitted, and with no models configured nothing could reach an LLM. **measured**
- `/tmp/pi-profile/agentdir/settings.json` was left at `tuiMode: "fullscreen"` from the last run. It is the lane's scratch config, not the user's.

## Risks (ranked)
1. Alt+Enter (`ESC\r`) submits when idle. Harnesses must use Ctrl+J for a newline. **source**
2. Paste-marker ids renumber on backspace. A paste.go keyed by id will expand the wrong content. **source**
3. Ctrl+C and Esc in bash mode wipe the draft, but Ctrl+- undoes both. The loss rule must allow for a restore. **source**
4. Ctrl+V and fullscreen right-click pastes never cross the PTY. A right-click paste can create a marker whose content unsent does not have. **source**
5. The kitty protocol or modifyOtherKeys changes how delete keys are encoded, and fullscreen adds mouse and focus reports. **source**
6. On Terminal.app a bare `\r` can be a newline. **source**
7. Ctrl+G ground truth changes the state under test and clears scrollback. **measured**
8. Selectors and extension editors can draw look-alike `─` rules. **source/guess**

## Open questions
- The top rule during a real agent turn (source only; needs a submit).
- Delete-key bytes under Ghostty or iTerm2 with kitty flags 7 active (measured only in tmux without extended keys).
- Whether fullscreen with a long transcript changes anything besides where the box sits.
- How common extension editors (`setEditorComponent`) are among users.
- Whether unsent should capture undo (`ctrl+-`) as a draft-restoring key.

## Verification notes
Checked against the lane's scratch dir `/tmp/pi-profile` (clone at `2b0a123d`, raw logs, fixtures, grab file) plus `npm view` and `curl`:
- **Re-counted in `raw.log` / `raw-fs.log` and confirmed:** 0 vs 1 × `?1049h`; 69/69 `?2026` pairs; 2 × `2J`/`3J`, both after Ctrl+G; `>7u` and `>4;2m`; `38;5;239`/`143`; both paste markers.
- **Confirmed in source:**
  - height formula, wrap width, scroll behaviour
  - paste threshold and normalisation
  - Alt+Enter submit, Ctrl+C clear and 500 ms exit, bash-mode Esc wipe
  - Ctrl+V inline insert
  - mount order, external-editor flow, `useWindowsKeybindings`, Terminal.app Shift detection
  - the 100-entry history cap
  - the npm dist-tags and the GitHub 301
- **Corrected:**
  1. The scroll-label centring came in `1d9787c1`/v0.85.0, not `37bedb7f`.
  2. Paste ids renumber when **any** marker is backspaced, not only the newest.
  3. The `editorPaddingX` 0–3 clamp is only in the setter, and project `.pi/settings.json` merges over global settings.
  4. The empty-box bytes are `ESC[7m ESC[0m`.
  5. The fake cursor spans 2 cells on wide graphemes.
  6. The working-status regex misses the narrow form.
  7. The HEAD-vs-0.87.1 line-number drift is now flagged.
- **Downgraded:** the byte-exact Ctrl+G claim about the r1…r11 paste. The grab file was overwritten by a later run that has no markers. Expansion stays measured on screen only.
- **Added, all from source:**
  1. Ctrl+C and bash-mode wipes are undoable with Ctrl+-, paste map included.
  2. Fullscreen right-click pastes in-process and can create markers.
  3. Fullscreen enables mouse and focus reporting.
  4. The `--tui-mode fullscreen` CLI flag.
  5. The shift+backspace and shift+delete keys.
  6. The startup terminal queries (DA1, OSC 11) and the OSC 8 row terminators.
- **Not re-checked** (kept at the lane's tag): the tmux-screen observations (padding 2, the 9×Up scroll result, fullscreen box appearance) and the v0.50.0 stability of the width-1 layout.
