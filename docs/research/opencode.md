# opencode (sst/opencode 1.18.32): verified input-box profile for unsent

Tags: **measured** (from the lane's captures, re-read in this pass unless noted), **source** (file:line in `sst/opencode@b65de4d` at `/tmp/oc-profile/src`, or OpenTUI `v0.4.5` `0c8c4f7c` at `/tmp/oc-profile/opentui`, or unsent itself), **docs** (upstream README / GitHub metadata / protocol spec), **guess**.

## Summary

opencode 1.18.32 draws its TUI with OpenTUI 0.4.5 (Zig renderer, SolidJS reconciler, shipped as a Bun-compiled native binary). It runs on the alternate screen, and every frame sits inside a `?2026` synchronized-output pair. The input box is a textarea with a left `┃` bar and a filled background. It has no prompt glyph, and text starts 3 columns right of the bar. Under the draft come a blank `┃` row, a meta row (`┃  Build · <model> <provider>` or `┃  Shell`), and a `╹▀▀▀` bottom edge. In transparent themes that edge is blank.

The empty-box hint (`Ask anything… "<example>"`, home screen only) is drawn in truecolor `textMuted` (128,128,128 in the default theme), never SGR 2. unsent's `faintFrom` empty test cannot work here. Pastes of 3 or more lines, or over 150 characters, become an unindexed `[Pasted ~N lines]` token that Backspace deletes as one unit. opencode saves only some drafts itself: Ctrl+C-cleared drafts of 20 or more trimmed characters, or drafts with parts. A reader is feasible only if `screenRow` also carries per-cell foreground and background colour. Text alone cannot tell the prompt from transcript user messages.

## Method (measured)

- opencode `1.18.32` (npm `opencode-ai`, `opencode-darwin-arm64`) in `/tmp/oc-profile/npm`, run by `/tmp/oc-profile/scratch/run.sh` with HOME and all XDG dirs in `/tmp/oc-profile/scratch`, `EDITOR`/`VISUAL` set to a copy-and-exit script, `TERM=xterm-256color`, and API keys unset. The config used `enabled_providers: ["unsent-no-such-provider"]`, so nothing could reach a model. No prompt was submitted.
- Private tmux server `tmux -L ocprof` at 100x40, 140x40 and 140x24. Raw PTY bytes were logged with `pipe-pane` to `/tmp/oc-profile/scratch/raw-home.log` (109,772 B), `raw-session1.log` (71,340 B), `raw-session2.log` (42,932 B) and `raw.log` (22,727 B).
- To get the session screen, the lane created user messages through `opencode serve` with `noReply: true` (stored, no LLM call; lane cites `packages/opencode/src/session/prompt.ts:1069`, not re-checked).
- **Caveat (measured, new):** OpenTUI detected tmux and wrapped its capability queries in tmux DCS passthrough (`ESC P tmux; …`). Under unsent, with no tmux in between, the queries go to the real terminal directly, and the kitty-keyboard answer can differ.

## 1. TUI stack

- OpenTUI `@opentui/core`, `@opentui/solid` and `@opentui/keymap` pinned at 0.4.5, with SolidJS. **source** root `package.json:43-45`; `packages/tui/package.json:55-57` uses `catalog:`.
- The box is OpenTUI's `<textarea>` (EditBufferRenderable). Defaults are `wrapMode: "word"`, `scrollMargin: 0.2` and a transparent background. **source** `opentui packages/core/src/renderables/EditBufferRenderable.ts:101-110`.
- **Binary-name collision:** the archived Go `opencode-ai/opencode` (archived, last push 2025-09-18, continued as Charm's Crush) also installed a binary named `opencode`, via `brew install opencode-ai/tap/opencode` and `go install github.com/opencode-ai/opencode@latest`. **docs** (GitHub repo metadata and README). That it draws a different screen is **guess**: it was Bubble Tea, and not measured here. `extractorFor` switches on `filepath.Base(command)` (**source** unsent `extract.go:38-45`), so it could meet either binary.

## 2. Screen mode and terminal protocol

- **Alternate screen** `ESC[?1049h` at startup. **measured**: in all four logs, 1049 is set once at byte 225. In `raw-home.log`, `1049l`/`1049h` also appear around the external-editor suspend. OpenTUI's default is `"alternate-screen"`, and `OTUI_USE_ALTERNATE_SCREEN` set to a falsy value forces main-screen mode. **source** `opentui renderer.ts:335-343`.
- **Synchronized output:** every draw is inside `ESC[?2026h … ESC[?2026l`. **measured**, re-counted in this pass: 67/67 pairs (home), 41/41 (session1), 20/20 (session2; the lane said 16), 8/8 (raw.log). Bytes outside the pairs total 609, 449, 439 and 439, and all of them are queries, mode sets or titles.
- **Mode sets (measured):**
  - mouse `?1000h ?1002h ?1003h ?1006h` (1003 = any-motion);
  - bracketed paste `?2004h`;
  - `?2031h`;
  - xterm modifyOtherKeys `ESC[>4;1m`;
  - title `OSC 0;OpenCode`.

  The lane's cursor style `ESC[1 q` was not re-checked. On suspend the log shows `ESC[0 q`. Cursor style is configurable via `tui.json` `cursor`. **source** `index.tsx:1441`.
- **Startup queries (measured, in the tmux-wrapped form):**
  - OSC 10/11;
  - `ESC[>0q` (XTVERSION);
  - `ESC[6n` (CPR, three times);
  - XTGETTCAP;
  - DECRQM for 1016, 2027, 2031, 1004, 2004, 2026;
  - `ESC[?u`;
  - OSC 99 and OSC 1337 capability probes;
  - OSC 66 text-sizing probes;
  - `ESC[14t`;
  - OSC 4;0;
  - a kitty graphics query;
  - DA1.

  Several of these (`ESC[s`, `ESC[6n`, and OSC 66 at `ESC[H`) are sent **before** `?1049h`, so they hit the main screen. The theme-mode wait is `waitForThemeMode(1000)`. **source** `packages/tui/src/app.tsx:242`.
- **Duplicate replies are not an issue for unsent:** unsent's emulator replies are drained to `io.Discard`. **source** unsent `wrap.go:89-90`. This resolves one of the lane's open questions.
- **Kitty keyboard:** tmux did not answer `ESC[?u`, so no push was seen. **measured**. With a terminal that answers, OpenTUI sets flags disambiguate (0b1) | alternate-keys (0b100) = 5 by default, and opencode passes `useKittyKeyboard: {}`. **source** `opentui renderer.ts:533-590` (`buildKittyKeyboardFlags`) and `1057-1059`, opencode `app.tsx:199`. The lane cited `renderer.ts:620-640`, which is mouse-event code; corrected. Under kitty flags, modified keys arrive as CSI-u. **docs** (kitty keyboard protocol). The exact bytes per terminal are **not measured**.
- **modifyOtherKeys** `ESC[>4;1m` is on even without kitty. **measured**. Some modified keys (Ctrl+Backspace, Shift+Enter) may then arrive as `ESC[27;<mod>;<code>~` in xterm-style terminals. **guess**, not measured.

## 3. What the box looks like

### Session screen (measured at 100 columns)

```
  ┃                                          <- top padding row (paddingTop=1)
  ┃  do nothing; reply ok                    <- draft rows, text from column c+3
  ┃  after-alt-enter
  ┃                                          <- blank row (meta row has paddingTop=1)
  ┃  Build · No provider selected Connect a provider   <- meta row
  ╹▀▀▀▀▀▀▀▀▀▀ … ▀                            <- bottom edge
   /private/tmp/…/work              tab agents  ctrl+p commands   <- footer
```

- **Left bar:** `┃` (U+2503) at column c, where c = 2 (0-based) on the session screen. It is coloured by the agent: Build = 92,156,245 in the default theme (**measured**). Shell mode uses `theme.primary`, and a pending leader key uses `theme.border`. **source** `index.tsx:1288-1294`, `1350-1358`.
- **The bar cell's background is the page background (10,10,10), not the box's (measured, new).** The fill (`backgroundElement`, 30,30,30) starts at c+1. **measured** raw SGR: `ESC[38;2;92;156;245m ESC[48;2;10;10;10m┃ … ESC[48;2;30;30;30m  `. Default theme values: `background` #0a0a0a, `backgroundPanel` #141414, `backgroundElement` #1e1e1e, `textMuted` #808080, `text` #eeeeee. **source** `packages/tui/src/theme/assets/opencode.json`.
- **No glyph and no rules.** Text starts at c+3, after `paddingLeft={2}`, with `paddingRight={2}` on the other side. **source** `index.tsx:1360-1368`.
- **Bottom edge:** the left border is `╹` and the bottom is `▀` coloured `backgroundElement`, only when `backgroundElement.a != 0`. Otherwise both are spaces. **source** `index.tsx:1487-1511`. The count of `▀` equals the box's inner width, and wrap width = count − 4: 95→91 at 100 columns, 93→89 at 140 with the sidebar, 74→70 on home. **measured** by the lane from tmux snapshots. The raw log interleaves SGR, so this can't be re-counted from bytes, but the numbers agree with the layout arithmetic in §5.
- **Transparent themes:** `lucent-orng` has `background`, `backgroundPanel` and `backgroundElement` all `transparent`. **source** `theme/assets/lucent-orng.json`. The bottom row is then blank and the box has no fill. The lane reports this as measured; there is no separate capture for it in this pass.
- **Meta row:** `┃  <Agent>`, then optional `auto` (permission auto mode), then `· <model> <provider>`, then optional `· <variant>`. In shell mode it shows `Shell` only. **source** `index.tsx:1444-1484`. `Build` measured.
- **Footer:** `tab agents  ctrl+p commands` when idle, `esc interrupt` when busy, and shell-mode and usage variants. **source** `index.tsx:1513+`. The idle footer was measured; the others are source only.

### Home screen

- The same box, centred. Width = `tui.json prompt.max_width`, default 75, or `"auto"` = max(75, floor(0.7·cols)). **source** `routes/home.tsx:33-37`.
- At 100 columns: bar at 1-based column 14 (0-based 13), draft/placeholder at 1-based column 17 = c+3. **measured** raw `ESC[21;14H…┃ ESC[21;17H…Ask anything…`.
- The box moves up as it grows, because the content is vertically centred. **measured** by the lane: top row 19 → 18 → 14.
- Home also has replaceable plugin slots `home_logo` and **`home_prompt`**, the whole prompt. **source** `routes/home.tsx:76-84`. The lane named only `session_prompt`.

### Sidebar

- Shown by default when cols > 120, and it takes 42 columns. Session content width = cols − 42 − 4. **source** `routes/session/index.tsx:256-278`.
- Sidebar text shares rows with the box, so the reader must cut rows at the box's right edge. **measured** by the lane at 140 columns.

### Autocomplete popup

- Drawn above the box's top padding row, as `┃ /agents   Switch agent   …   ┃`: same left column, and a right-hand `┃`. **measured** by the lane; not re-checked.

### Transcript user messages

- Same layout: `┃`, blank padding row, `┃  text`, blank padding row, same bar colour.
- Background `backgroundPanel` (20,20,20), or `backgroundElement` (30,30,30) on mouse hover. **source** `routes/session/index.tsx:1411-1418`. **measured**: session1 has 189 cells of `48;2;20;20;20` against 289 of `30;30;30`.
- In transparent themes they cannot be told apart by colour. **source** (theme file).

### Dialogs dim everything (new)

- An open dialog lays a backdrop of `RGBA(0,0,0,150)` over the whole screen. **source** `ui/dialog.tsx:48`.
- **measured**: with the "Connect a provider" dialog open, the box row read bar 38,64,101 on 4,4,4 and fill 12,12,12 instead of 92,156,245 / 10,10,10 / 30,30,30.
- Any fixed-colour test (fill = 30,30,30, placeholder = 128,128,128) therefore fails while a dialog is up. The cursor gate (§10 step 5) must reject that state anyway, because the cursor moves into the dialog. **measured** by the lane: cursor (14,25).

## 4. Empty box and hint

- **Home:** `Ask anything… "<example>"`, with examples "Fix a TODO in the codebase", "What is the tech stack of this project?" and "Fix broken tests". Shell mode shows `Run a command… "<ls -la|git status|pwd>"`. **source** `index.tsx:1310-1319`, `routes/home.tsx:17-20`.
  - A `props.showPlaceholder === false` path returns no hint. **source** `index.tsx:1311`.
  - Placeholder lists come from props, so a plugin can change them. **source**.
- **Colour, not faint:** the placeholder is drawn with `ESC[38;2;128;128;128m` (`textMuted`). **measured** raw: `ESC[21;17H ESC[38;2;128;128;128m ESC[48;2;30;30;30mAsk anything…`. A full SGR parse of all four logs in this pass found only attributes 0, 1 and 49 (plus truecolor fg/bg); **SGR 2 never appears**. **measured**.
  - unsent's `faintFrom` (**source** unsent `screen.go:37-48`, fed by `uv.AttrFaint` at `screen.go:69`) can never fire for opencode.
  - unsent's `screenRow` holds only `cells` and `faint` (**source** `screen.go:20-23`), so colour has to be added.
- **Session screen:** no hint. The empty box is a single blank `┃` row with the cursor at c+3. **measured** by the lane. Source is consistent: session routes pass no placeholders. **guess** on the exact prop path; not traced.
- **Leader trap:** while the leader key (Ctrl+X) is pending, real draft text also turns `textMuted`. **source** `index.tsx:1373-1374`.
- The wording changed in 2026: #45126 (2026-09-03) "use unicode ellipses in interface text". **docs** (GitHub commits API, re-checked).

## 5. Wrapping, height, scrolling, cursor

- **Wrap width:**
  - session screen: cols − 9 (4 page padding, 1 bar, 2+2 box padding);
  - session with the sidebar: cols − 42 − 9;
  - home: max_width − 5.

  **source** arithmetic from `routes/session/index.tsx:278` and `index.tsx:1350-1368`, agreeing with the **measured** 91 / 89 / 70.
- **Word wrap;** an over-long word hard-breaks at the width. **measured** by the lane. Where the break falls next to `:` is not pinned (open).
- **Height cap:** `tui.json prompt.max_height`, else `max(6, floor(rows/3))`. **source** `index.tsx:1345`. **measured** 13 at 40 rows, 8 at 24.
- **Scrolling:** past the cap the box scrolls one row at a time with a 20% margin. **measured** by the lane, from Up-key behaviour. **source** OpenTUI `scrollMargin: 0.2`. There is no scroll indicator.
- **Cursor:** the real cursor sits at the insertion point, shown with `?25h`. **measured** raw: frames end `ESC[21;37H ESC[?25h ESC[?2026l`. With a dialog open, it moves into the dialog.

## 6. Pastes

- **Rule:** normalise CRLF/CR to LF, then **trim**, then count lines = LFs + 1. If lines ≥ 3 or length > 150, and `paste_summary_enabled` (kv, default `!experimental.disable_paste_summary`), insert the placeholder `[Pasted ~N lines]` plus one space as a virtual extmark. Otherwise insert the **untrimmed** normalised text inline. **source** `index.tsx:1183-1215`.
- **measured** raw: `[Pasted ~3 lines]` for a 3-line paste and `[Pasted ~1 lines]` for a 151-character one-liner. Style: bold, fg 10,10,10 on bg 245,167,66.
- **No index:** two pastes with the same line count look identical. The stored part text is the trimmed paste. **source** `index.tsx:1149-1180`.
- **File-path pastes:** a pasted path to a readable local file becomes `[SVG: name]` (text attachment), or an attachment token such as `[Image N]` / `[PDF N]` (binary). **source** `index.tsx:1186-1203`. The exact token wording for binary types was not re-checked in this pass.
- **Atomic delete:** Backspace next to a token removes all of it. **measured** by the lane: 17 characters in one key.
- **Editor expansion:** Ctrl+X e with a copy-and-exit `$EDITOR` got the draft with placeholders expanded (`pasteA:two line one\ntwo line two pasteB:three one\nthree two\nthree three pasteC:x…`), 410 bytes, with no trailing newline. **measured** `/tmp/oc-profile/scratch/editor-copy.txt`.
- **Fit with unsent:** its `pastePlaceholder` regex is Claude-specific (`\[Pasted text #\d+…\]`, **source** unsent `paste.go:14-16`). opencode needs its own matcher, and expansion has to pair tokens with captured bracketed pastes by order and line count, comparing trimmed text.

## 7. Keys (source `packages/tui/src/config/keybind.ts:161-200`; all remappable via `tui.json keybinds`)

- **Submit:** `return`.
- **Newline:** `shift+return, ctrl+return, alt+return, ctrl+j`. **measured**: Ctrl+J and Alt+Enter inserted newlines in tmux.
- **Delete keys:**
  - `backspace, shift+backspace`;
  - word back: `ctrl+w, ctrl+backspace, alt+backspace`;
  - `ctrl+u` (to line start);
  - `ctrl+k` (to line end);
  - `ctrl+shift+d` (line);
  - forward: `ctrl+d, delete, shift+delete`;
  - word forward: `alt+d, alt+delete, ctrl+delete`;
  - undo: `ctrl+-, super+z`;
  - redo: `ctrl+., super+shift+z`.
- **Clear:** `input_clear: ctrl+c`. **source** `keybind.ts:161`. It runs `clearPrompt()`. **source** `index.tsx:335-345`, `1272-1285`. `app_exit: ctrl+c,ctrl+d,<leader>q`. **source** `keybind.ts:48`. The lane measured that Ctrl+C clears a non-empty box and exits on an empty one. The precedence code was not traced in this pass.
- **Gap against unsent `deleteKeys` (source `wrap.go:505-528`, new):**
  - unsent counts only `0x7f`, `0x08` and `ESC[3~` as one-character deletes, and `0x17`, `0x15`, `0x0b`, `0x1f` as unlimited.
  - opencode's Ctrl+C (`0x03`, clears everything) and Ctrl+D (`0x04`, forward delete) are missing.
  - Alt+Backspace (`ESC 0x7f`) is counted as one character, but it deletes a word.
  - A Backspace next to a paste token removes the whole token (≥17 characters) but counts as one.
  - CSI-u and modifyOtherKeys encodings are not recognised at all.
  - A cleared box is still handled by `keepOld`, which moves the old draft to history when the draft becomes empty. **source** `wrap.go:537-545`.
- **History:** Up recalls only when the cursor is at offset 0. Down recalls only at the end. `move()` returns nothing when the current input is non-empty and differs from the history entry, so **a non-empty edited draft is not overwritten**. **source** `packages/tui/src/prompt/history.tsx` `move()` (the lane's path `component/prompt/history.tsx` is a one-line re-export), `index.tsx:862-925`.
- **Shell mode:** `!` at offset 0 enters shell mode without inserting `!` (**source** `index.tsx:831`; **measured** by the lane). Backspace at offset 0 leaves it.

## 8. External editor (ground truth)

- **Key:** `<leader>e` (Ctrl+X then e) or `/editor`. **source** `keybind.ts:77`.
- **Editor choice:** `$VISUAL || $EDITOR`, on the temp file `os.tmpdir()/<Date.now()>.md`; the file is deleted afterwards. **source** `packages/tui/src/editor.ts:26-56`.
- **Command parsing:** the editor string is split on spaces, with no shell on Unix. **source** `editor.ts:37-41`.
- **Output:** the draft with pastes expanded (**measured**, §6). After return, the box holds the expanded text. **measured** by the lane.

## 9. opencode's own draft persistence

- **File:** `$XDG_STATE_HOME/opencode/prompt-history.jsonl` (default `~/.local/state/opencode/`). **source** `packages/core/src/global.ts:3,14`, `prompt/history.tsx`.
- **What goes in:**
  - submitted prompts;
  - drafts cleared via `clearPrompt()` when the trimmed input is ≥ 20 characters or there are any parts. **source** `index.tsx:104`, `1272-1278`.
- **Limits:** 50 entries; a consecutive exact duplicate is not appended again. **source** `history.tsx` (`MAX_HISTORY_ENTRIES`, `isDuplicateEntry`).
- **measured:** the file holds one entry, `input` of 346 characters over **18** lines (the lane said "20-line"), with `parts: []` and `mode: "normal"`.
- **Pastes in the file:** `input` holds the placeholder text and `parts[].text` the real paste. **source** `index.tsx:1165-1178`; not measured.
- **Stash:** `prompt-stash.jsonl` via the palette commands "Stash prompt" / "Stash pop", with no default key. **source** `keybind.ts:156-158`, `prompt/stash.tsx`.
- **Across unmount:** if the input is non-empty, the draft goes to a module variable and is restored on mount. **source** `index.tsx:141`, `615-633`.
- **Not saved:**
  - drafts under 20 characters that have no parts, when cleared;
  - any draft lost on exit, SIGHUP, a crash or a closed terminal;
  - text deleted by hand.

  This is unsent's gap to fill. **source** (the absence of any other write path was not exhaustively proven; **guess** for "any draft on exit").

## 10. Detection recipe for an `opencodeBox` reader

**Precondition:** add per-cell fg and bg to `screenRow`. `uv.Cell.Style` is already read for `AttrFaint` at `screen.go:69`, so the same struct can supply colours. **source**. **Guess** that fg/bg are exposed there in the same form; check before implementing.

1. **Meta row M.** Scan up from the bottom for the lowest row M where cell c is `┃`, `row[M-1]` is `┃` followed by blanks, and `row[M+1][c]` is `╹` or blank (not `┃`). c is the leftmost `┃` column: 2 on the session screen, variable on home. The text at c+3 is the agent name, or `Shell`, which means prefix `!`. This rejects transcript user messages, because every text row of theirs has a `┃` row below it.
2. **Box fill B** = bg of `row[M][c+1]`. Do not use column c: the bar cell carries the page background (**measured**).
3. **Draft rows.** Walk up from M−2 while `row[y][c] == '┃'` and, if B is opaque, `bg(row[y][c+1]) == B`. Stop at a row whose last non-blank cell is `┃` (autocomplete) and after cap+1 rows. Drop the topmost row (padding). Text = `cells[c+3 : c+3+width]`.
4. **Width.**
   - If `row[M+1]` is `╹▀…`: count(`▀`) − 4.
   - Otherwise: the run of B on row M, minus 5.
   - Otherwise: the formula in §5.
   - Always cut at the box edge, because the sidebar shares these rows.
5. **Cursor gate.** Accept only if the cursor row is inside the draft rows and cursor col ≥ c+3. Otherwise `ok=false`: a dialog (which also dims every colour), a permission or question prompt, or a subagent view has focus.
6. **Empty.** A single draft row, cursor at c+3, and either the row is blank or it starts with `Ask anything… "` / `Run a command… "` in a foreground different from the draft text colour. Never rely on SGR 2. Prefer the text prefix over colour, because of the leader-key mute and the dialog backdrop.
7. **Capped** = visible rows ≥ cap. Scrolling is one row at a time with no indicator.
8. **Timing.** Read only outside `?2026` pairs. unsent already does this (**source** `wrap.go:212-216`), and every opencode draw is inside a pair (**measured**).
9. **Pastes.** Match `\[Pasted ~(\d+) lines\] ` and attachment tokens. There is no index, so pair tokens with captured pastes by order and trimmed line count.
10. **deleteKeys** has to add Ctrl+C (clear all), Ctrl+D, Alt+D, Alt+Backspace as a word delete, and whole-token deletes, and parse CSI-u and modifyOtherKeys forms (see §7).

## 11. Stability and breakers

- **Churn, corrected:** from 2026-04-01 to 2026-09-26 the prompt file had **62 commits at the old path** (`packages/opencode/src/cli/cmd/tui/component/prompt/index.tsx`, up to the package move on 2026-06-07) plus **14 at the new path**, about 76 in total. The lane said "about 50". **docs** (GitHub commits API, re-queried).
  - Churn has **slowed**: 14 commits in the 3.5 months since the move.
  - Cited PRs confirmed: #28255 (2026-05-25, responsive and configurable size), #31193 (2026-06-07, standalone `packages/tui`), #45126 (2026-09-03, ellipsis), #24869 (2026-04-29, paste-summary toggle).
- **Breakers:**
  - **Theme and colour:** transparent themes; custom themes; the dialog backdrop; user-message hover; mouse selection (**guess**); the leader-key mute.
  - **Layout config:** `prompt.max_height` / `max_width`; the sidebar.
  - **Plugins:** the `session_prompt` and `home_prompt` replace-slots (**source** `routes/session/index.tsx:1313-1317`, `routes/home.tsx:82-84`).
  - **Input handling:** keybind remaps; kitty and modifyOtherKeys key encodings; paste-summary format or thresholds.
  - **Environment and text:** `OTUI_USE_ALTERNATE_SCREEN`; placeholder wording.
- **The design has held steady in 2026** (left `┃` bar, filled box, meta row below). **guess**, from commit titles only. Pin facts per release and re-measure on each minor bump.

## Risks

1. Colour-dependent reader: `screenRow` has no colour today; the extension is a prerequisite.
2. Transparent themes remove the fill and bottom edge. Only the meta row and the cursor anchor the box then.
3. Plugin replace-slots (`session_prompt`, `home_prompt`) can swap the prompt out entirely, and the reader would silently stop matching. It should fail closed (`ok=false`).
4. Empty detection: the leader mute and the dialog dimming both change colours, so use the text prefix plus the cursor gate.
5. Unindexed paste tokens make expansion depend on order and line count.
6. `deleteKeys` undercounts opencode deletions (Ctrl+C clear, token deletes, word deletes, CSI-u), so the delete budget will misjudge losses.
7. The binary name `opencode` is shared with the archived Go project (**docs**). Its screen differs (**guess**).

## Open questions

- Which bytes do kitty-capable terminals (Ghostty, kitty, WezTerm) send for Backspace, Ctrl+W, Alt+Backspace and Shift+Enter under flags 5, and which does xterm send under modifyOtherKeys 1? This needs a run outside tmux.
- Word-wrap placement next to `:` and `/` in long tokens.
- The startup delay before keys are accepted, and the prompt's look while a response streams (no provider was allowed).
- The transcript directly touching the prompt in a scrolled-full session.
- Whether any path other than `clearPrompt()` / submit writes drafts (for example on exit). Source was not exhaustively traced.

## Verification notes

**Checked against source and captures, and held:**
- OpenTUI 0.4.5 pin, and textarea defaults (word wrap, 0.2 scroll margin).
- Alternate-screen default and the env override.
- `DRAFT_RETENTION_MIN_CHARS = 20` and the `clearPrompt` condition.
- Paste thresholds (≥3 lines or >150 characters, trimmed) and the placeholder format.
- Placeholder strings and the example list.
- `max(6, floor(rows/3))` height cap.
- Home `max_width` default 75 / `"auto"`.
- Sidebar at >120 columns taking 42.
- `session_prompt` replace-slot and user-message backgrounds.
- The full keybind list and the Ctrl+C clear/exit bindings.
- Editor `$VISUAL||$EDITOR` and the temp file.
- Leader-key muting.
- Default theme colours: #808080, #1e1e1e, #141414, #0a0a0a.
- lucent-orng being fully transparent.
- A full SGR parse of all four raw logs: no SGR 2.
- The `?1049h` and `?2026` pairs.
- Mode and query bytes outside frames.
- The placeholder's 128,128,128 SGR, and the paste-token SGR.
- `prompt-history.jsonl` contents and `editor-copy.txt`.
- The unsent identifiers the lane named (`extractorFor`, `screenRow`, `faintFrom`, `deleteKeys`, `deleteLog`, `paste.go` regex).
- Commit history and PR numbers via the GitHub API.
- The archived status of `opencode-ai/opencode`.

**Changed:**
- Kitty-flag citation: `renderer.ts:620-640` → `533-590`, `1057-1059`.
- History module path: `component/prompt/history.tsx` is a re-export; the real code is in `prompt/history.tsx`.
- session2 sync-pair count: 16 → 20.
- Cleared-draft capture: "20-line" → 18 lines, 346 characters.
- Commit churn: "about 50" → about 76 since April, with only 14 since the June package move (churn is slowing).
- Binary-name collision upgraded from guess to docs, for the name only.
- Added:
  - the bar cell uses the page background, so B must be read at c+1;
  - the dialog backdrop dims every colour (source and measured);
  - the `home_prompt` replace-slot;
  - the tmux-passthrough caveat on the measured queries;
  - pre-1049 queries;
  - modifyOtherKeys as a second key encoding;
  - the concrete gaps between opencode delete keys and unsent's `deleteKeys`;
  - history de-duplication;
  - the resolution of the duplicate-reply question (unsent drains emulator replies, `wrap.go:89-90`).
- Kept several tmux-snapshot-only facts as "measured by the lane", because their byte-level form was not re-derived: ▀ counts, autocomplete layout, scroll behaviour, the 17-character token delete, Ctrl+C-on-empty exit.
- Dropped the lane's "relayed user request" note: it is unrelated to opencode, and nothing was edited because of it.

**Not checked:**
- `packages/opencode/src/session/prompt.ts:1069` (`noReply`).
- The binary attachment token wording.
- The session route's missing-placeholder prop path.
- Whether `uv.Cell.Style` exposes fg/bg in a directly comparable form.
