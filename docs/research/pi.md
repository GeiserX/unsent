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

## Capture run on pi 0.87.1 (measured 2026-09-29)

**Environment.** `@earendil-works/pi-coding-agent` 0.87.1, still the npm `latest` on 2026-09-29 (the same version the research above measured), installed with `npm install --ignore-scripts` into a scratch folder on a Mac mini with macOS 26.6.1 and run by Node 26.0.0. It ran in a private tmux 3.6b server (`tmux -L unsent-071 -f /dev/null`, `TERM=tmux-256color`, 120x40) with `extended-keys on` and `extended-keys-format csi-u`, under `unsent capture pi` with `UNSENT_DEBUG_DIR` set. unsent has no pi reader, so it printed its not-protected line and passed every byte through while logging both directions; the debug folder stayed empty, and the capture's own record is the raw log. The environment was `env -i` with a scratch `HOME`, `PI_CODING_AGENT_DIR`, `TMPDIR` and XDG folders, `PI_OFFLINE=1`, `PI_SKIP_VERSION_CHECK=1`, `PI_TELEMETRY=0`, no provider key, no `GITHUB_TOKEN`, `GH_TOKEN` or `CI`, and a `PATH` with only unsent, pi, node and the system folders (checked: no other agent resolves on it). `$EDITOR` and `$VISUAL` were unsent's copy-and-exit script. The captures, screens and probes are in [`testdata/pi/0.87.1/`](../../testdata/pi/0.87.1/), described in [its README](../../testdata/pi/README.md); every session id below is from that throwaway home.

**A rig trap: with no model, pi writes no session, so nothing can be resumed.** A session file is created only once the session holds an assistant message (`_persist` in `session-manager.js` [source, 0.87.1 dist]), and with no model a submit ends in `Error: No API key found for the selected model.` before any assistant message exists [measured, `submit`, `sessions/id-3-submit-0.5s.txt`]. So the box scenarios ran with no model, as planned, and the identity and resume scenarios ran with a second scratch agent folder holding one custom provider in `models.json` (`baseUrl` `http://127.0.0.1:9/v1`, `api` `openai-completions`, `apiKey` `dummy`, model `dummy-model`) and `HTTPS_PROXY`, `HTTP_PROXY` and `ALL_PROXY` at the same dead port. Every request failed on the machine with `Error: Connection error.`; pi retried three times and recorded each failure as an assistant error message [measured, `identity`]. This is the "unreachable base URL" path of [SPEC 5.2](../SPEC.md#52-the-real-app-test-rig), not the no-model path the lane used.

### What this run corrects or adds to the profile above

| The profile above says | 0.87.1 does | Tag | Evidence |
| --- | --- | --- | --- |
| Under unsent the terminal must answer pi's startup queries, or pi waits and falls back | tmux answered only DA1 (`ESC[?1;2;4c`), not the kitty query `ESC[?u` nor `OSC 11;?`. pi did not wait: the first frame, box and footer included, came 2 ms after the queries, 0.19 s after start. With no kitty answer pi kept modifyOtherKeys 2, and tmux sent keys in CSI-u form (table below) | measured | the first chunks of every `.rec` |
| (not covered) | pi sends its whole terminal setup again after every Ctrl+G and on SIGCONT (`ESC[?2004h ESC[>7u ESC[?u ESC[c ESC[?25l … ESC[>4;2m`), and takes it down before the editor, on its own Ctrl+Z and on exit (`ESC[?25h ESC[?2004l ESC[<u ESC[>4;0m`) | measured | 49 kitty pushes and 48 pops in 23 records; `suspend-direct.rec` at 2.9 s and 7.1 s |
| Ctrl+G's full redraw clears scrollback | Confirmed: each of the 10 Ctrl+G in `deletes` wrote `Launching external editor: …` outside a frame, then one `ESC[2J ESC[H ESC[3J` frame | measured | `deletes.rec` |
| On exit 0 the editor file is set back minus one trailing `\n` | Confirmed, and it changes the draft: a draft ending in a line break (`… mu\n`, the editor copy) came back without it, so a Ctrl+Y after the round trip joined the yanked line onto `mu` | measured | `deletes.editor-4.txt`, `screens/deletes-yank.txt` |
| Byte-exact editor copy is source only (the lane's file was overwritten) | **Byte-exact, measured.** Every editor copy equals the typed and pasted text, placeholders expanded in the right order after renumbering, no trailing newline added: `typed`, `multiline` (a blank line; `\` then Enter left no `\`), `accents`, `tall` (both), `pastes` (all three), `send-paste` | measured | the copies against the key scripts, compared by program |
| Ctrl+C and bash-mode wipes are undoable with Ctrl+- (source) | **Measured for Ctrl+C.** Ctrl+C emptied the box, and Ctrl+- (`ESC[45;5u`) brought the whole draft back, in `deletes` and in `ctrlc-history`. Esc in `!` bash mode emptied the box too [measured, `screens/bash-mode-esc.txt`]; its undo was not tried | measured | `deletes.editor-10.txt`, `ctrlc-history.editor-2.txt` |
| Paste ids renumber on Backspace over any marker (source) | **Measured.** `[paste #1 +12 lines] B [paste #2 1001 chars] C [paste #3 1001 chars]`, then Ctrl+A, Right (one press stepped over the whole marker) and Backspace (removed it whole) gave ` B [paste #1 1001 chars] C [paste #2 1001 chars]`; the next paste was `[paste #3 +12 lines]`. The editor copy put each text where its old id was | measured | `screens/pastes-*.txt`, `pastes.editor-2.txt` |
| Prompt history is browsed with Up on the first row | Up browses only when the box is empty, already browsing, or the cursor is at column 0 of the first row (`editor.js`). In a typed draft the first Up moved the cursor to the line start and the second showed the newest entry, **with the draft held aside in memory**; Down put the draft back. An edit or a send of the entry drops the draft: every edit calls `exitHistoryBrowsing`, which sets `historyDraft` to null, and only the undo snapshot `navigateHistory` pushed first still has it (source, `editor.js`; not measured). Ctrl+C does not add the cleared draft to history; a submit that fails with no model does | measured + source | `ctrlc-history`, `screens/ctrlc-history-up-in-draft.txt`, `…-up-again-in-draft.txt`, `…-down-in-draft.txt` |
| The working status in the top rule is source only | **Measured** during retries: `── ⠇ Retrying (1/3) in 1s... (escape to cancel) ────…`, the spinner changing each frame; after the last retry `Error: Retry failed after 3 attempts: Connection error.` above the box and the plain rule back. The box took typing during the turn, one row high | measured | `screens/identity-retry.txt`, `identity-draft-during-turn.txt`, `identity-after-retries.txt` |
| (Ctrl+Z not covered; undo is `ctrl+z` only on win32) | Ctrl+Z is `app.suspend`: pi takes down its screen and sends SIGTSTP to its own process group. See "Ctrl+Z" below | measured | `suspend`, `suspend-direct` |

Unchanged and re-seen: the inline main screen (0 `ESC[?1049h` in 23 records), the full-width `─` rules with no side border, 12 content rows at 40 rows (`max(5, floor(rows*0.3))`), centred ` ↑ N more ` and ` ↓ N more ` labels whose counts add up with the visible rows to the draft's rows (4 + 12 + 29 = 45), the reverse-video fake cursor (`ESC[7m ESC[0m` on an empty box, raw bytes), the hardware cursor hidden, `ESC]8;; BEL` at the end of every row, and the rule colour `38;5;239` [measured, `typed`, `tall`].

### Box, wrapping and text

- Wrap width 119 at 120 columns: 118 `a`s and then `漢字` put `漢` on the next row, leaving the first row one cell short of the width limit; 117 `x`s followed by ` ñandú` wrapped at the space [measured, `accents`, `screens/accents.txt`].
- Precomposed accents (`ñandú`, `¿qué pasó?`, `Pingüino`), decomposed ones (`e` + U+0301, `n` + U+0303), `👍🏽` and the flag `🇪🇸` came back byte-identical through Ctrl+G [measured, `accents.editor-1.txt`].
- A 45-line draft: 40 Up from the last row scrolled the box to rows 05 to 16 (`↑ 4 more`, `↓ 29 more`), with the cursor on the top row; an edit there, then 40 Down back to rows 34 to 45 (`↑ 33 more`), and the editor copy held the edit out of sight. After Ctrl+G the cursor sat at the end of the text; 30 Up and a Ctrl+W on row 15 changed only that row [measured, `tall`, `tall.editor-1.txt`, `tall.editor-2.txt`].
- A blank line in the draft is a blank content row; `\` then Enter gives a new line and the `\` is not kept, even with the two keys in separate reads 0.16 s apart [measured, `multiline`].

### Pastes

- A 3-line paste, a 10-line paste and a 1,000-character paste went in as text. An 11-line paste ending in a line break became `[paste #1 +12 lines]` (the count is `split("\n").length`), 1,001 characters `[paste #2 1001 chars]`, an 11-line paste with no final break sent with CR `[paste #1 +11 lines]` [measured, `pastes`].
- CR line breaks (tmux's default `paste-buffer`) came back as LF [measured, `pastes.editor-3.txt`: 10 LF, no CR].
- After Ctrl+G the box holds the expanded text, the markers are gone, and one Backspace removes one character (`dummy` to `dumm`) [measured, `screens/pastes-after-editor.txt`, `pastes-bspace-after-editor.txt`].
- A sent message is stored expanded: the session file's user message was the 1,021 characters typed and pasted, the transcript showed them expanded, and Up recalled them expanded [measured, `send-paste`, `sessions/id-send-paste.txt`, `screens/send-paste-up.txt`].

### Keys as they reached pi in tmux (modifyOtherKeys 2, `extended-keys-format csi-u`)

| Key | Bytes | What pi did (editor copy or screen) |
| --- | --- | --- |
| Enter | `CR` | submit |
| Alt+Enter | `ESC[13;3u` | submit (idle) |
| Esc then Enter (`send-keys Escape Enter`) | `ESC CR` in one read | submit (read as Alt+Enter) |
| Ctrl+Enter, Ctrl+M | `ESC[13;5u`, `ESC[109;5u` | nothing |
| Ctrl+J | `ESC[106;5u` | new line |
| Shift+Enter | `ESC[13;2u` | new line |
| `\` then Enter | `\`, then `CR` | new line, `\` dropped |
| Backspace, Shift+Backspace | `0x7f`, `ESC[127;2u` | one character back |
| Ctrl+H | `ESC[104;5u` | nothing |
| Ctrl+W, Alt+Backspace | `ESC[119;5u`, `ESC[127;3u` | one word back |
| Ctrl+Backspace | `ESC[127;5u` | nothing (bound only in the session picker) |
| Delete, Ctrl+D (in a draft) | `ESC[3~`, `ESC[100;5u` | one character forward |
| Alt+D, Alt+Delete | `ESC[100;3u`, `ESC[3;3~` | one word forward |
| Ctrl+Delete | `ESC[3;5~` | nothing |
| Shift+Delete | `ESC[3;2~` | nothing at the end of the text (not tried elsewhere) |
| Ctrl+U | `ESC[117;5u` | kills to the start of the current line |
| Ctrl+K | `ESC[107;5u` | kills to the end of the line |
| Ctrl+Y, Alt+Y | `ESC[121;5u`, `ESC[121;3u` | yank, then yank-pop (the kill ring held `pha beta`, two forward kills joined) |
| Ctrl+- | `ESC[45;5u` | undo, including a Ctrl+C clear |
| Ctrl+_ | `ESC[95;5u` | nothing |
| Ctrl+C | `ESC[99;5u` | clears a draft; Ctrl+D then quits on the empty box |
| Ctrl+D (empty box) | `ESC[100;5u` | quits |
| Ctrl+G | `ESC[103;5u` | external editor |
| Ctrl+R | `ESC[114;5u` | nothing in the main box |
| Ctrl+Z | `ESC[122;5u` | pi's suspend (below); through unsent, unsent's |
| Esc | `ESC` | closes a dialog or picker; during a turn cancels the retry (`Retry cancelled`) |

[measured, `deletes.rec`, `submit.rec`, `multiline.rec`, `ctrlc-history.rec`, `identity.rec`; effects from `deletes.editor-1` to `-10` and the screens]. Two effects are paired by source, not by the capture: in the group Alt+D, Ctrl+Delete, Alt+Delete the copy lost `pha` and ` beta`, and the keybindings put the word forward on Alt+D and Alt+Delete only. The encodings are tmux's for modifyOtherKeys 2 under its csi-u format; no kitty-capable terminal was run, so flag-7 release events were not seen.

**Submit keys: Enter, Alt+Enter, and Esc then Enter when the two arrive in one read. New line: Ctrl+J, Shift+Enter, `\` then Enter.** A submit empties the box at once; with no model the prompt is not drawn in the transcript, only the error, and with a model it is drawn above the box [measured, `submit`, `identity`, `send-paste`].

**Ctrl+Z.** pi binds it to `app.suspend`: `ui.stop()`, then `process.kill(0, "SIGTSTP")`, and it redraws when a SIGCONT comes (`handleCtrlZ`, `interactive-mode.js` [source]). Run alone under `script` (its own terminal session, so an orphaned process group), Ctrl+Z made pi write `ESC[4B CR CR LF ESC[?25h ESC[?2004l ESC[<u ESC[>4;0m`: the cursor below the box, cursor on, bracketed paste off, kitty pop, modifyOtherKeys off. The kernel dropped the SIGTSTP, so pi kept running (state `S`, not `T`) with its screen stopped: text typed then was echoed by the terminal and went into the box when the rig sent SIGCONT by hand, after which pi set its modes again and redrew with `ESC[2J ESC[H ESC[3J` [measured, `suspend-direct.rec` at 2.9 s and 7.1 s, `sessions/suspend-direct-ps.txt`, `screens/suspend-direct-after-cont.txt`]. Without that SIGCONT pi would stay like that: the same hang unsent's suspend exists for. Through unsent, which takes Ctrl+Z for an agent with no profile, zsh printed `suspended  unsent capture pi`, unsent showed state `T`, and `fg` brought the box back with the draft, which the next Ctrl+G copy matched [measured, `suspend`]. So pi's profile keeps Ctrl+Z as unsent's. That its `suspended` bytes should be the teardown above does not hold: see "Profile run" below, where pi's repaint after `fg` sets none of its modes again, so the profile writes nothing.

### A send that fails

- With no model: Enter empties the box, draws `Error: No API key found for the selected model.` and the `/login` hint above it, and writes nothing to disk. The prompt goes into the in-memory history (Up recalls it) [measured, `submit`, `ctrlc-history`].
- With the dead-port model: Enter draws the prompt above the box, then `Error: Connection error.`, and the top rule carries `Retrying (n/3) in Ns... (escape to cancel)` for about 12 s; a draft typed during the retries stays in the box and Ctrl+G copies it. Alt+Enter submits the same way. Esc cancels the retry (`Retry failed after 1 attempts: Retry cancelled`) and leaves the box empty [measured, `identity`].

### Session identity

| When | What is under `PI_CODING_AGENT_DIR` | What another process can read | Tag |
| --- | --- | --- | --- |
| Start, before any key | `settings.json` (`{"lastChangelogVersion": "0.87.1"}`), `auth.json` and `models-store.json` (`{}`), and an empty `sessions/--<cwd with / as ->--/` folder, all written at start. No session file | the pi process holds only its working folder open; no pid-keyed file; the process title is `pi` | measured, `sessions/id-0-start.txt`, `id-dp-0-start.txt` |
| Typed, not sent; after Ctrl+C | nothing new | nothing new | measured, `id-1-typed.txt`, `id-2-ctrlc.txt`, `id-dp-1`, `id-dp-2` |
| After a failed submit, no model | nothing new, then or at exit | nothing | measured, `id-3-submit-0.5s.txt`, `id-6-end.txt` |
| 0.3 s after a failed submit, dead port | `sessions/--<cwd>--/<start time>_<id>.jsonl`, named with the time pi **started** (`2026-09-29T13-55-21-526Z`, 6 s before the submit). Its lines: `{"type":"session","version":3,"id":"<id>",…,"cwd":…}`, `model_change`, `thinking_level_change`, a `system` message, the `user` message, then one `assistant` error message and one `context_edit` per retry | the file, held open only for the instant of an append (one poll caught it open, the next one no longer); its name and first line carry the id | measured, `id-dp-3-submit-0.3s.txt`, `poll-identity.txt` |
| After Esc cancels a retry, after a later Ctrl+C | appended entries, same file | the same | measured, `id-dp-5`, `id-dp-6` |
| At exit, once the session has a file | the screen prints `To resume this session: pi --session <id>`; nothing is written | nothing | measured, every `-exit` screen of the dead-port runs |

`/session` shows the session file and `ID: <id>` from start, before the file exists: a UUIDv7 (`01a0ed72-d735-77d5-…`), the file named `<start time>_<id>.jsonl` [measured, `screens/session-info.txt`, `identity-session.txt`]. That is the only place the id shows while nothing has been written, and reading it takes typing a command.

**Resume forms.**

| How it was opened | Session it then runs (from `/session`) | What changed on disk at the start | Tag |
| --- | --- | --- | --- |
| `pi -c` | the newest session of the folder, same id and same file; later entries appended to it | nothing, until the next submit (the file stayed 9,517 bytes until then) | measured, `resume-c`, `poll-resume-c.txt` |
| `pi -r` (the picker, drawn between the `─` rules in place of the box, `Resume Session (Current Folder)`, newest first) | the chosen id; Down then Enter picked the older one | nothing while open or after the choice | measured, `resume-picker`, `poll-resume-picker.txt` |
| `pi --session <id prefix>` | that id | nothing | measured, `resume-session`, `poll-resume-session.txt` |
| `/resume` inside a session, then Enter | the chosen id. The new session pi had started with (`01a0ed74-7e72-…`) was never written | nothing | measured, `slash-resume` |
| `/new` inside a session | a new id (`✓ New session started`), with no file until its first submit | nothing, then a new file | measured, `slash-new` |
| `pi --fork <id>` | a new id, whose file exists at start with `"parentSession": "<path of the old file>"` in its header | a new file | measured, `fork`, `sessions/fork-session.jsonl` |
| `pi --session <id>` of a session that never got a reply, or of an unknown id | `No session found matching '<id>'`, exit status 1 | nothing | measured, `resume-unsent-id`, `resume-unknown` |

The picker lists only sessions with a file. `pi --session-id <id>` ("use exact project session ID, creating it if missing") also exists [source, the help text in `cli/args.js`], not tried.

**So the running session's id cannot be read from outside while pi runs, until pi appends to the session file.** pi holds no file open between appends, writes nothing keyed by pid, and a resume, the picker, `/resume` and `/new` touch no file until the next entry [measured, the polls and `id-*` probes above]. What another process can read:

- after the running pi's first append (a reply, an error reply, a model change), the `sessions/--<cwd>--/` file whose size grew since the child started: its name and first line hold the id. Two pi processes in the same folder make that ambiguous [guess];
- a fork, whose new file exists from start [measured];
- the screen: a resumed session redraws its earlier prompts above the box (`resume-c` shows both sent prompts), which could be matched against the user messages of each session file [design, not tried].

Reading the id from `pi -c`, `--session` or `--fork` on the command line would miss the picker and `/resume`, and [SPEC 2.3](../SPEC.md#23-restore-in-box) rules the command line out; passing `--session-id` ourselves would be reconfiguring the agent, which [decision 3](../SPEC.md#8-decisions) rules out. For restore this also means: a pi draft typed in a session that never got a reply has no file, cannot be resumed, and can only come back through the notice and `unsent restore`, as for Claude Code.

### Kill, close, synchronized output

- `kill -9` of pi: unsent exited with it, by the same signal, the draft (`kill nine draft zebra`) is in no file of the scratch folders, and the terminal kept bracketed paste and modifyOtherKeys 2 on, with unsent's exit lines printed on the draft's last row [measured, `kill9`, `sessions/id-k9-after.txt`, `screens/kill9-after.txt`].
- tmux `kill-session`: pi wrote its teardown on the hangup, pi and unsent exited, no process was left, and the draft is in no file [measured, `killsession.rec` at 3.1 s, `sessions/ks-kill.txt`].
- **pi sends synchronized-output marks.** 664 `ESC[?2026h`/`ESC[?2026l` pairs in 23 records, balanced in every record, and every paint of the box inside a pair. Outside the pairs there were only hardware-cursor moves (`ESC[nG`, `ESC[nA`, `ESC[nB`, `ESC[?25l`), the mode setups and teardowns, the startup queries, the window title (`OSC 0;π - work`), the `Launching external editor:` lines, the exit line, and the terminal's echo of text typed while pi was stopped. The median time between the ends of two frames was 82 ms, the shortest 0.7 ms [measured, all records]. pi needs no quiet-gap read.

### Negative screens

Each of these draws the same two full-width `─` rules with something that is not the draft between them, in place of the box [measured, `dialogs`, `resume-picker`, `slash-resume`]:

- `/model` and Ctrl+L: the model picker, a `> ` search row with the reverse-video cursor, `No matching models`, `Model catalogs refreshed.`, a key hint row;
- `/resume` and `pi -r`: `Resume Session (Current Folder)`, two hint rows, the same `> ` search row with the cursor, then the sessions or `No sessions in current folder. Press Tab to view all.`;
- `/settings`: a list of 31 settings with `→` on the chosen row and a `> ` search row with the cursor;
- `/login`: `Select authentication method:` with `→`; `/trust`: `Project trust` with `→`; `/tree`: `Session Tree` with its own rules inside;
- the `/` menu (`→ settings …`, `(1/25)`) draws under the bottom rule, with the box itself holding `/`; `@` in the empty scratch folder drew no list;
- `!` bash mode: the box holds `!echo dummy` with the rule in another colour, and Esc empties it.

A reader that takes "two rules, content rows, one reverse-video cursor" as the box would read the pickers' `> ` rows as a draft of `>`: the rows under the top rule, a row starting `> ` with hint rows around it, and a content height the box would not have tell them apart. `/hotkeys` and `/session` print into the transcript and leave the box as it is.

### Open questions from above, answered where this run could

- **The top rule during a real agent turn:** measured with a retrying request (above). A streaming reply was not seen, since nothing can answer.
- **Delete-key bytes under Ghostty or iTerm2 with kitty flags 7:** not run. tmux does not answer `ESC[?u`, so the table is modifyOtherKeys 2 in tmux's CSI-u form.
- **Fullscreen with a long transcript:** not run.
- **Extension editors:** not run; none is installed in a scratch home.
- **Whether unsent should capture undo (`ctrl+-`) as a draft-restoring key:** Ctrl+- brings back a Ctrl+C clear in full, markers included, so a profile that treats Ctrl+C as a clear must also expect the cleared text back in the box with no recall key [measured]; how the profile handles it is a design choice.
- **SPEC 2.3's pi row, "how the running session's id is read":** no out-of-process source until the first append (above).

### What this run could not capture

- A kitty-capable terminal, flag-7 release events, fullscreen mode, a Ctrl+V or right-click clipboard paste, and a streaming reply.
- The paste settle delay and a restore: unsent had no pi reader yet (the profile run below measured the paste on the first frame; restore stays off).
- The resume forms with no model: no session file is ever written then, so they ran on the dead-port provider instead.
- History recalled in a resumed session (`populateHistory`): not tried.

## Profile run on pi 0.87.1 (measured 2026-09-29)

**Environment.** The setup of the capture run above, same machine, pi, Node, tmux and scratch layout, in a new scratch folder, with unsent built from the branch that adds the pi profile. Six more captures were recorded with `unsent capture pi`, now running pi's reader too: `wrap`, `paste-renumber`, `edge-deletes`, `scroll-steps`, `early-paste` (no model) and `early-paste-resume` (`pi -c` on the dead-port provider, after one failed send made a session file). Two live runs went through `unsent pi`: a draft left at tmux `kill-session`, and a send scenario on the dead port. All are in [`testdata/pi/0.87.1/`](../../testdata/pi/0.87.1/) but the two live runs, whose results are below.

### What this run corrects or adds

| Said above | Measured | Evidence |
| --- | --- | --- |
| The profile's `suspended` bytes are pi's own Ctrl+Z teardown | Between unsent's Ctrl+Z and the next key after `fg`, pi repainted the box (a full `ESC[2J ESC[H ESC[3J` frame, from the size nudge) and sent no `ESC[>7u`, `ESC[>4;2m` or `ESC[?2004h`. Written at the suspend, the teardown would leave pi without its kitty flags and modifyOtherKeys after `fg`, where Shift+Enter arrives as a plain `CR` and sends the box. So unsent writes nothing, as for Codex, and turns bracketed paste on again as it resumes pi | `suspend.rec`, 2.9 s to 7.7 s; `TestPiSuspendPolicy` |
| Word wrap "breaks after the last space" (source) | A word that ends exactly at the wrap width with a space after it moves to the next row: 12 nine-letter words, 119 columns, then ` and these words…` put `edgewordC` on row 2. A word longer than a row breaks at the edge, and when the edge falls right before a space, that space starts the next row (` tail after a long word`). A placeholder that does not fit moves whole, spaces and all. A line that fills the row exactly gets no extra row | `wrap`, `screens/wrap.txt`, `wrap.editor-1.txt` |
| Paste ids renumber on Backspace (measured) | Also for two placeholders of the same size: deleting the first renumbers the second to `#1`, deleting the second leaves the first as it was, and the next paste gets `#2`; the editor copy put each text where it was | `paste-renumber`, `screens/renumber-*.txt` |
| Ctrl+G sets the file back "minus one trailing `\n`" | And the box then shows the expanded text: a 1,367-character paste became 16 rows in a box of 12, scrolled to its end with `↑ 4 more`. unsent's stitcher knew only the placeholder by then, and until the fix in this branch the live draft kept only the rows in view (the whole text went to history). It now lines such a view up against the saved draft | `early-paste`, `TestPiRoundTripKeepsTheDraft` |
| (not measured) Delete keys at the edge of a box at its cap | Ctrl+W, Backspace, Alt+Backspace, Ctrl+U, Ctrl+K, Delete, Ctrl+D, Alt+D, Alt+Delete, Shift+Delete and Shift+Backspace on the last row of a 20-row draft; every editor copy matched | `edge-deletes` |
| Scrolling keeps the cursor on the edge (source; one lane run) | Up one key at a time, 16 times from the last of 20 rows: the cursor walked to the top row, then each Up scrolled one row with the cursor on it (`↑ 3 more`, `↓ 5 more`); Down back the same way | `scroll-steps`, `screens/steps-up.txt` |

### Restore caps (SPEC 2.3), each as measured

- **How the running session's id is read:** it cannot be, before the first append (above, "Session identity"). The profile reads none, its sent log is per run, and restore-in-box is off.
- **Resume forms:** `pi -c`, `pi -r` (a picker), `pi --session <id or prefix>`, `/resume` inside a session; `pi --fork <id>` and `/new` start another session. None writes anything that names the session until the next entry.
- **Settle delay:** 0 at the resolution of the rig. A 1,367-character paste with 7 line breaks and accents, sent as a bracketed paste on the first frame that shows the box (9 ms after pi first drew its cursor in a new chat, 25 ms after in `pi -c`), went in whole, and the Ctrl+G copy was byte-identical.
- **Empty reads:** nothing pi-specific was seen; three would do, as for Claude Code and Codex.
- **Newline encoding inside a bracketed paste:** LF and CR both arrive as LF; the copies were byte-identical either way.
- **What cannot go back faithfully:** a tab becomes four spaces and control characters other than LF are dropped. A paste that starts with `/`, `~` or `.` right after a word character gets a space in front (`see` then a pasted `/tmp/x.txt` gave `see /tmp/x.txt`), which cannot happen in an empty box. Spaces at the ends of lines and a final line break stay.
- **Whether the editor copy holds placeholders:** no, every placeholder is expanded, and the copy is exact.

So items 4 and 6 of the profile done definition are measured, and restore-in-box stays off for pi because item 4's first answer is that there is no session id to key a restore to. That is hard, not impossible: pi writing its session id to a file keyed by its pid, as Claude Code does, or naming the new session's file at start, would be enough. Reading the session file whose name carries the child's start time would name a new chat's session once it has a reply, but not a resumed one, so it was not built.

### The live runs

- **Window closed.** `unsent pi`, two lines typed (`live draft kept at a window close, ñandú`), tmux `kill-session`: `unsent list` then showed the orphan, and `unsent show 1` printed the two lines exactly.
- **Send scenario (SPEC 5.2).** On the dead port: `before ` plus a 1,001-character paste plus ` after, dummy`, sent with Enter; a second message sent with Alt+Enter; a third cleared with Ctrl+C. The sent log held the first two messages, the paste expanded, and they equal the `user` messages of pi's own session file byte for byte; the third was in history. No key was missing from a logged message.

### Open questions left

- A kitty-capable terminal (flag-7 release events), fullscreen mode, a Ctrl+V or right-click clipboard paste, a streaming reply, and a queued message given back by Esc during streaming: not run.
- `editorPaddingX` above 0: not run; the reader would save the padding as leading spaces.

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
