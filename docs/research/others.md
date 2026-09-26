# Other interactive agent CLIs: verified input-box profiles

Covers Kimi Code, Cline 3, Letta Code, Mistral Vibe, OpenHands, Kilo, Grok Build, Auggie, CodeBuddy and Warp. Other lanes cover Claude, Codex, Gemini, OpenCode, Copilot, Cursor, Aider, Amp, Qwen, Crush, Goose, Droid, pi and agy.

Tags:
- **measured**: seen in the lane's captures under `/tmp/unsent-other`, or re-counted by the skeptic. The lane ran a private tmux (`tmux -L unsent-other -f /dev/null`) at 100x40 with TERM=xterm-256color and HOME=/tmp/unsent-other/home. Raw pane bytes are in `raw/<agent>.log`, and screen grabs are in `k.cap` (Kilo), `l.cap` (Letta), `v.cap` (Vibe) and `oh.cap` (OpenHands).
- **source**: read in code. The clones are `src/kimi-code` at `be7d5f5` (2026-09-24), `src/cline` at `29896ec` (2026-09-25, `apps/cli/package.json` = 3.0.65) and `src/letta-code` at `1157790` (2026-09-26, 0.33.2). Installed packages: `uvtools/mistral-vibe` (Vibe 2.25.8), `uvtools/openhands` (openhands_cli 1.16.0), and `npm/node_modules/@augmentcode/auggie/augment.mjs` (0.36.0) and `@tencent-ai/codebuddy-code/dist/codebuddy.js` (2.158.0). Grok has binary strings in `grok.strings` and bundled docs in `home/.grok/docs/user-guide` (1.0.41).
- **docs**: vendor docs or registry metadata.
- **guess**: inference.

Nothing was submitted to any agent.

## Ranking by likely user base

The skeptic re-fetched npm counts for 2026-09-18..24 from `api.npmjs.org`, PyPI counts from `pypistats.org/api/.../recent`, and stars with `gh api`, all on 2026-09-26. **measured**

| # | Agent (binary) | Weekly installs | Stars | Box reachable without login? | Readability |
|---|---|---|---|---|---|
| 1 | Mistral Vibe (`vibe`) | PyPI `mistral-vibe` 747,330 (rolling last week; may include CI) | 5.0k | yes (fake API key accepted) | good |
| 2 | Letta Code (`letta`) | npm 140,142 | 3.4k | yes (`--backend local`) | hard (no height cap, ↵ glyphs) |
| 3 | Grok Build, xAI (`grok`) | npm `@xai-official/grok` 73,380, plus a curl installer | closed | no (device login) | unknown |
| 4 | CodeBuddy Code, Tencent (`codebuddy`/`cbc`) | npm 67,124 | closed | no (login menu) | probably like Claude |
| 5 | Cline CLI 3 (`cline`) | npm **66,123** (the lane said 54k) | 69.4k (mostly the IDE extension) | yes (Ollama with a fake model) | good |
| 6 | Auggie (`auggie`) | npm 37,007 | closed | no (login) | unknown |
| 7 | Kimi Code (`kimi`) | npm 36,083; the legacy **PyPI** `kimi-cli` is 12,266 (the npm `kimi-cli` is 44) | 7.7k (kimi-code) / 11.4k (kimi-cli, archived) | yes | best |
| 8 | Kilo CLI (`kilo`), an OpenCode fork | npm `@kilocode/cli` 34,068 | 27.4k | yes | same as OpenCode |
| 9 | OpenHands CLI (`openhands`) | PyPI 8,391 | 0.26k | yes (fake LLM env) | medium, two modes |
| 10 | Continue `cn` | npm 4,553 | 36k | not tried | Ink **guess** |
| — | Warp | n/a | — | n/a | out of reach of a PTY wrapper |

- npm and PyPI counts are not comparable. PyPI counts are inflated by CI. The skeptic did not verify that the PyPI `recent` endpoint excludes mirrors, despite the lane's claim. **docs/guess**
- The lane judged three agents too small to be worth a reader: superagent `grok-cli` (907/wk, and it clashes with xAI's `grok` binary name), iFlow CLI (583/wk) and Qodo Command (198/wk). **measured by lane, not re-checked**

## Changes that affect unsent as a whole

1. **The empty-box test.** `screen.go` records only `faint` (SGR 2) per cell (`screen.go:22,69,114`). **source**
   - The hint colours are:
     - Cline: fg 131,137,140 (`cline.log`). **measured**
     - OpenHands: fg 122,122,122 on bg 34,34,34 (`oh.log`). **measured**
     - Letta: SGR 90, with the first character in SGR 7 (`letta.log`). **measured**
     - Kilo: fg 166,160,155 (`k.cap`). **measured**
   - Vibe and Kimi show no hint at all. **measured**
   - So the snapshot needs the foreground colour per cell, or a per-agent list of hint strings. The hint strings are more robust, because themes change the colours. **guess**
2. **Where the cursor is.**
   - The hardware cursor sits at the insertion point in Kimi, Cline and Kilo. Kimi hides it but still positions it. **measured**
   - Letta, Vibe and OpenHands hide it and draw a reverse-video (SGR 7) cell instead. **measured by lane**
   - So the snapshot needs an SGR 7 flag per cell.
3. **Synchronized output.**
   - Kimi, Cline, Kilo, Vibe and OpenHands wrap redraws in `?2026h/l`. Counts of `ESC[?2026h`: Kimi 109, Cline 115, Kilo 53, Vibe 1893, OpenHands 324. **measured** (re-counted)
   - **Letta has 0.** Ink redraws are not framed, so a snapshot can land mid-redraw. Letta needs a settle delay or a double read. **measured** (count); consequence **guess**
4. **Name clashes in `extractorFor`.** `extract.go:38` switches only on `filepath.Base(command)`. **source**
   - `grok` can be xAI's Grok Build or the superagent CLI. `kimi` can be Kimi Code or the archived Python `kimi-cli`. **docs**
   - Each reader must confirm the agent from the screen.
5. **Keys that clear the whole draft.**
   - One Ctrl+C clears everything in Kimi, Cline, Letta, Vibe and Grok. **source** for all five, **measured** for Kimi.
   - Esc Esc clears in Letta, Grok and Auggie. **source/docs**
   - A whole-draft clear must count as an unlimited delete. Otherwise unsent's stitcher will think the text scrolled out of sight.

## Profiles

### Kimi Code 2.1.1 (`@moonshot-ai/kimi-code`)
- **TUI stack.** A vendored pi-tui in `packages/pi-tui`, plus Kimi's own post-processing in `apps/kimi-code/src/tui/components/editor/custom-editor.ts`. **source**. The legacy Python `kimi-cli` is archived. **docs**
- **Screen.** Main screen: 0 × `?1049h` in `kimi.log`. Modes seen: `?2004h ?2026h/l ?1004h ?2031h ?25l`. **measured** (re-counted)
  - The alternate screen needs `tui_mode="fullscreen"` or `KIMI_CODE_TUI_FULL_SCREEN=1`. The env var is read at `apps/kimi-code/src/tui/config.ts:22` (the lane cited `config.ts:22`), and `ENTER_ALT_SCREEN` is at `packages/pi-tui/src/tui-alt-screen.ts:61`. **source**
- **Box.** pi-tui draws only the two horizontal rules. `wrapWithSideBorders` (`custom-editor.ts:861-897`) turns the first rule into `╭…╮` and the last into `╰…╯`, and puts `│` on the outer columns of content rows. **source**
  - Border colour gray rgb(90,90,90); rows look like ` │ > text…   │`. **measured by lane**
  - **Correction:** while the `/btw` side panel is open, the top corners are **`├` and `┤`**, not `╭╮` (`connectedAbove`, `custom-editor.ts:871-872`; set in `controllers/btw-panel.ts:145`). The lane's recipe only accepts `╭`. **source**
  - The prompt glyph is added by `injectPromptSymbol` (`custom-editor.ts:832-843`). It rewrites the first content row to `"  > " + line.slice(4)`, and only when the row's first four characters are spaces. The screen positions are 0-based: `│` at col 1, `>` at col 3, text from col 5. **source + measured by lane**
  - In shell mode the glyph is `!` and the top border reads `╭ ! shell mode ───╮`. The label is only drawn when the top rule is plain `─`, so it disappears while a `↑ N more` label is showing (`custom-editor.ts:876`). **source**
- **Empty box.** `> ` then an SGR 7 block, with no hint text. **measured by lane**
- **Wrapping.** Word wrap at cols−10 (90 at 100 cols). **measured by lane**
- **Height.** `max(5, floor(rows*0.3))` visual rows (`packages/pi-tui/src/components/editor.ts:620`), then the box scrolls one row at a time with the cursor (`editor.ts:626-630`). **source**
- **Scroll labels.** They come from `createScrollBorder` (`editor.ts:286-300`). **source**
  - The label ` ↑ N more ` / ` ↓ N more ` is **centred** in the rule.
  - When the rule is too narrow, it falls back to a left `─── ↑ N more ─…`, and then to `...`.
  - N counts hidden visual rows, so the scroll offset is exact.
  - `kimi.log` holds `↑ 1 more` through `↑ 25 more`, and 7 lines with `↓ N more`. **measured** (re-counted). The "37 rows, 12 visible, ↑ 25 more" case matches the formula at 40 rows.
- **Cursor.** Hidden but positioned at the insertion point. The visible cursor is a drawn SGR 7 cell. **measured by lane**
- **Newline.** Shift+Enter and Ctrl+J (`packages/pi-tui/src/keybindings.ts:143`). **source**. Esc+Enter and Ctrl+J. **measured by lane**
- **Delete keys** (`keybindings.ts`). **source**
  - Backspace; Delete and Ctrl+D (forward).
  - Ctrl+W and Alt+Backspace; Alt+D and Alt+Delete.
  - Ctrl+U; Ctrl+K.
  - Ctrl+- (undo); Ctrl+Y (yank) and Alt+Y (yank-pop).
- **Ctrl+C** clears the whole draft (`clearEditorTextIfPresent`, `apps/kimi-code/src/tui/controllers/editor-keyboard.ts:533-538`). **source + measured by lane**
- **Paste.** Bracketed. **source**
  - More than 10 lines becomes `[paste #N +M lines]`; otherwise more than 1000 characters becomes `[paste #N C chars]` (`editor.ts:1421-1434`).
  - When a marker is removed, the ids of the later markers are renumbered (`editor.ts:1518-1537`).
  - `kimi.log` holds `[paste #1 +30 lines]` and `[paste #1 12000 chars]`. **measured** (re-counted). 6 lines stayed inline. **measured by lane**
  - There is also a timing-based fallback for unbracketed pastes (`paste-burst.ts`). **source**
- **External editor.** Ctrl+G, using `$VISUAL`/`$EDITOR` or a configured command. **source**. Not exercised. This is the ground-truth hook for tests.
- **Persistence.**
  - Only submitted prompts are saved, in `<share_dir>/user-history/<md5(cwd)>.jsonl` (`apps/kimi-code/src/utils/paths.ts:141-144`). **source**. The share dir was `~/.kimi-code` in this run. **measured** (the directory exists in the test home)
  - Input history is capped at 100 entries (`editor.ts:482`). **source**
  - Up on an empty box did not bring back a draft cleared with Ctrl+C. **measured by lane**
- **Detection recipe.**
  1. Bottom-up, find row y matching `^ ?│ [>!] `, with a row starting `╭` **or `├`** above it and a `╰` row below.
  2. Take the text from col 5 to cols−5.
  3. Parse `↑ (\d+) more` on the top rule and `↓ (\d+) more` on the bottom rule, anywhere in the row, since the label is centred.
- **Stability.** Medium. The TypeScript rewrite is new, and the box is Kimi's post-processing on top of pi-tui. **guess**

### Cline CLI 3.0.65 (`cline`)
- **TUI stack.** A Bun-compiled binary on OpenTUI React 0.4.3. **measured by lane** (binary strings). Source is `apps/cli`, and the clone's `package.json` says 3.0.65, the same version as the one measured. **source**
- **Screen.** Alternate screen: 1 × `?1049h` in `cline.log`. **measured** (re-counted). It also turns on mouse tracking `1000/1002/1003/1006` and `?2004h`, and uses `?2026` framing. **measured**
- **Box.** Two `─` rules (rgb 99,99,99), no side borders. **measured by lane**
  - Home view: the box is centred and `min(cols,68)` wide (`HOME_VIEW_MAX_WIDTH=68`, `types.ts:253`, used at `views/home-view.tsx:77,135`). **source**. At 100 cols it starts at col 16. **measured by lane**
  - Chat view: full width. **guess**. The home-only use of the constant suggests it, but the chat view was not measured because reaching it needs a submit.
  - The prompt is a bold `❯` in rgb(121,184,255), then a space; continuation rows are indented 2. **measured**
- **Empty box.** The placeholder "What can I do for you?" in fg 131,137,140, not dim. **measured** (`cline.log`). In plan mode the placeholder is "Plan something...". **source per lane**
  - **New trap:** on the home view, the same string "What can I do for you?" is also drawn as a **bold white heading above the box**: `ESC[1m` at row 19, col 40 in `cline.log`. **measured**. A placeholder match must be limited to the row inside the rules.
- **Height.** `maxHeight={5}` with `wrapMode="word"` (`components/input-bar.tsx:235-236`). **source**. Past 5 rows it scrolls, with `❯` on the first visible row and no "more" marker. **measured by lane**
- **Cursor.** The real cursor is visible at the insertion point. **measured by lane**
- **Newline.** Shift+Enter and Ctrl+Enter (`input-bar.tsx:238-241`); Meta+Enter. **source**. Esc+Enter and Ctrl+J. **measured by lane**
- **Delete keys.** From the OpenTUI Textarea defaults (`src/opentui-textarea.ts`). **source per lane, not re-checked**
  - Ctrl+W, Ctrl+Backspace, Alt+Backspace.
  - Alt+D, Ctrl+Delete, Ctrl+Shift+D (delete line).
  - Ctrl+K, Ctrl+U, Delete.
  - Ctrl+- and Cmd+Z (undo).
- **Ctrl+C and Ctrl+D** (`hooks/use-root-keyboard.ts`). **source**
  - Ctrl+C with text clears the draft (`setInputValue("")`). Ctrl+C on an empty box calls `requestExit` (lines 87-95).
  - The root handler also takes Ctrl+D: it exits when the box is empty and the session is idle, and otherwise just returns (lines 271-276). Whether Ctrl+D still reaches the Textarea as forward-delete is **guess**.
  - Esc Esc restores a checkpoint. **source per lane**
- **Paste** (`utils/pasted-snippets.ts`). **source**
  - The threshold is `countPastedTextLines >= 5`, and a trailing newline is not counted. There is no character threshold.
  - The marker is `[<preview>... Pasted +N lines]`. The preview is trimmed, whitespace-collapsed and cut to 48 characters. The `... ` is added even when the preview is shorter than that.
  - A repeat of the same text gets ` #2`, ` #3` and so on.
  - `cline.log` holds `[q1 q2 q3 q4 q5 q6... Pasted +6 lines]`. **measured**. 4 lines, and a 12,000-character single line, stayed inline. **measured by lane**
- **Persistence.** Input history is persisted (`use-input-history.ts`). **source per lane**. No external editor was found. **guess**
- **Detection recipe.**
  1. Find a row whose first non-space character is `❯` (col c), with a `─` run starting at col c directly above.
  2. The box ends at the next `─` run at col c. Text starts at c+2, and the box holds at most 5 rows.
  3. The box is empty when its text equals the current placeholder. Only compare rows inside the rules.
- **Traps.** **measured by lane, not re-checked**
  - On macOS the darwin-arm64 binary was SIGKILLed for an invalid signature until it was re-signed ad hoc.
  - After exit it left a `--cline-hub-daemon` process running.

### Letta Code 0.33.2 (`letta`)
- **TUI stack and screen.** Ink 5.2.1 (`package.json`). **source**. Main screen: 0 × `?1049h`. Modes seen: `?2004h ?25l`. **No `?2026` framing.** **measured** (re-counted)
- **Box.** Full-width dim `─` rules above and below. The prompt is `›` (U+203A) plus a space; continuation rows are indented 2. **measured** (`l.cap`)
  - Under the bottom rule is a right-aligned status line (`Tutor · Claude Opus 4.6 (US)`). **measured**
- **Empty box.** `› ` + `ESC[7mT ESC[27m ESC[90mry "explain what this function does"`: the first hint character is the drawn cursor. **measured** (`letta.log`)
- **Wrapping.** cols−2. **measured by lane**
- **Height: no cap.** A 45-row draft in a 40-row window pushed `›` and the first rows into scrollback. **measured by lane**. No `maxHeight` exists on the input in `InputRich.tsx`. **source** (grep)
- **Cursor.** Hidden, and not at the insertion point. **measured by lane**
- **Newline.** Esc+Enter and Ctrl+J. **measured by lane**
- **Clearing.** **source**
  - The first Ctrl+C wipes the draft. A second Ctrl+C within 1 s exits (`InputRich.tsx:1363-1385`). While a bash command runs, Ctrl+C interrupts it instead.
  - Esc Esc clears a non-empty draft (`InputRich.tsx:1321-1328`). The window constant is `ESC_CLEAR_WINDOW_MS = 2500` (line 75).
- **Paste** (`components/PasteAwareTextInput.tsx`). **source**
  - More than 5 lines or more than 500 characters becomes `[Pasted text #N +M lines]` (lines 281-284). A single long line says `+1 lines`.
  - Smaller pastes are inserted inline through `sanitizeForDisplay` (lines 59-61, used at 296 and 690), which **replaces every newline with `↵`**.
  - `letta.log` holds `p1↵p2↵p3`, `[Pasted text #1 +6 lines]` and `[Pasted text #2 +1 lines]`. **measured** (re-counted)
  - **New:** unbracketed pastes are also caught, by a heuristic: an addition of more than 500 characters or more than 5 lines at least 1 s after the last paste (lines 636-647). **source**
  - The paste counter kept counting across Ctrl+C. **measured by lane**
  - A reader must map `↵` back to newline. A literally typed `↵` cannot be told apart. **guess**
- **Detection recipe.**
  1. Find a dim full-width rule, a `› ` row, then a rule below. Text runs from col 2 with width cols−2.
  2. Map `↵` to a newline.
  3. If no rule is visible above the box and the box reaches row 0, flag the top as out of sight.
  4. Empty is the SGR 90 hint `Try "…"`.
  5. Because redraws are not framed, read twice or wait for quiet before trusting a snapshot. **guess**

### Mistral Vibe 2.25.8 (`vibe`)
- **TUI stack and screen.** Textual, alternate screen (`?1049h` present in `vibe.log`). **measured**
- **Box.** Two full-width `─` rules. The top rule ends in a right-aligned mode title (`── accept edits ─`). The prompt is a bold orange `>` at col 0 plus a space; continuation rows are indented 2. **measured** (`v.cap`)
- **Empty box.** `>` alone, plus **two blank rows**: the `#input` style has `min-height: 3` (`vibe/cli/textual_ui/app.tcss:320-323`). **source + measured** (`v.cap`)
- **Wrapping.** cols−4. **measured by lane**
- **Height.** `max-height: 50vh` (`app.tcss:323`). **source**. That was 20 rows in a 40-row window; then the box scrolls with `>` on the first visible row. **measured by lane**
- **Keys** (`widgets/chat_input/text_area.py`). **source**
  - Newline: Shift+Enter and Ctrl+J (lines 55-61). Ctrl+J was also measured by the lane.
  - Shift+Backspace and Shift+Delete delete a character.
  - Ctrl+G opens the external editor.
- **Ctrl+C** follows a ladder (`app.py:6097-6127`). **source**
  - A non-empty draft is cleared first. A second press quits after confirmation.
  - **Edge case:** when there is no app server, Ctrl+C quits at once (lines 6100-6102).
- **Paste.** Inline, with no placeholder. **source**
  - **Correction/new:** a paste that is entirely a path, or a newline-separated list of paths, is **rewritten to `@path` mentions joined by spaces** (`maybe_prepend_at_for_path`, `widgets/chat_input/paste_path.py:27-40`, called from `text_area.py:~383`). So on-screen text is not always the pasted bytes. **source**
- **Persistence.** Submitted prompts go to `~/.vibe/vibehistory`. **source per lane**
- **Detection recipe.**
  1. Find a rule row, which may end in a mode title.
  2. Then a row matching `^> `, then a closing rule.
  3. Width cols−4; capped at floor(rows/2).
  4. Empty means the `>` row holds only the prompt and the other box rows are blank.

### OpenHands CLI 1.16.0 (`openhands`)
- **TUI stack and screen.** Textual, alternate screen, with mouse modes `?1000/1003/1006/1015h` and `?2026`. **measured**
- **Box.** A rounded `╭…╮` box in the theme colour, with no prompt glyph. Text starts at col 3. **measured** (`oh.cap`)
- **Empty box.** Placeholder "Type your message, @mention a file, or / for commands" in fg 122,122,122. **measured**
- **Two input modes** (`openhands_cli/tui/widgets/user_input/input_field.py`). **source**
  - **Single-line** (default): auto height, `max-height: 8` including the border, so 1 to 6 text rows (lines 105-112). Enter submits (`single_line_input.py:71`), and there is no newline key.
  - **Multi-line**: toggled with Ctrl+L (line 89), or switched on automatically by a multi-line paste (lines 368-378). It is fixed at `height: 6`, so 4 text rows with a scrollbar (line 125). Ctrl+J submits (line 90).
  - Toggling copies the text from one widget into a **different widget** (lines 317-328).
  - Submit strips whitespace, clears the box and **switches back to single-line** (lines 332-344).
  - The status line shows the mode: `[Ctrl+L for multi-line • Ctrl+X for custom editor]` in single-line mode. **measured** (`oh.cap`)
- **Paste.** Inline. **source per lane**
- **External editor.** Ctrl+X, a priority binding (lines 91-93). **source**
- **Startup** took 40–90 s because of LiteLLM cost-map fetch timeouts. **measured by lane**
- **Detection recipe.**
  1. Find a `╭…╮` box in the lower part of the screen. The text is at col 3.
  2. Tell the mode from the status line.
  3. Cap at 6 rows in single-line mode and 4 in multi-line mode.
  4. Empty is the placeholder string.

### Kilo CLI 7.8.1 (`kilo`)
- **TUI stack and screen.** OpenTUI, alternate screen (`?1049h` in `kilo.log`). **measured**. It is an OpenCode fork. **docs**
- **Box.** A left `┃` bar in fg 166,160,155 on a filled panel (bg 41,37,36), closed at the bottom by `╹▀▀▀`. There is no prompt glyph. A model/mode line (`Code · <model> <provider>`) sits inside the panel under the text. **measured** (`k.cap`)
- **Empty box.** Placeholder `Ask anything... "<random example>"` in fg 166,160,155. Two different examples appear in `kilo.log`. **measured**
- **Newline.** Ctrl+J and Esc+Enter. **measured by lane**
- **Paste.** `[Pasted ~N lines]`. A 12,000-character single line became `[Pasted ~1 lines]`. **measured**: `kilo.log` holds `[Pasted ~6 lines]` and `[Pasted ~1 lines]`.
- **Height and cursor.** Up to 13 text rows at 40 rows; the cursor is visible at the insertion point. **measured by lane**
- **Reuse.** Share the OpenCode reader, but key on the placeholder text and the extra model line inside the panel. **guess**

### Grok Build 1.0.41 (`@xai-official/grok`): login wall
- **Version.** `home/.grok/version.json` = 1.0.41. **measured**
- **TUI stack.** Rust. The binary contains `ratatui-0.29.0`, `ratatui-core-0.1.0` and `crossterm-0.28.1`. **measured** (`grok.strings`)
- **Screen.**
  - The **login screen** runs on the alternate screen (1 × `?1049h`, plus `?1004h ?2004h`). **measured**. The prompt box itself was never reached.
  - Fullscreen is the default. `--minimal` or `[ui] screen_mode="minimal"` gives inline rendering with no border, and it falls back to minimal under tmux control mode and under Zellij (`21-terminal-support.md`, `04-slash-commands.md`). **docs** (bundled)
- **Newline.** Shift+Enter or Alt+Enter. Under tmux the footer prefers Alt+Enter (`03-keyboard-shortcuts.md:451,470`). **docs**
  - `/multiline` (Ctrl+M with the prompt focused) swaps the keys: Enter becomes newline and Shift+Enter/Alt+Enter sends (lines 250, 323). **docs**
- **Clearing** (lines 229-237, 262, 271). **docs**
  - Idle with a non-empty prompt, Esc Esc within 800 ms clears **and stashes** the draft. Ctrl+S, Alt+S, or Ctrl+Z right after restores it.
  - Esc Esc on an empty prompt opens the rewind picker.
  - Ctrl+C clears in one press and discards the draft; Ctrl+Z still undoes that, text only.
  - While a draft is stashed the top border reads `Stashed`. The stash is kept in memory only.
- **External editor.** Ctrl+G in minimal mode. The fullscreen TUI uses the command palette ("Edit Prompt in External Editor"). `/edit-prompt` starts from an empty draft. Drafts holding chips are refused. **docs** (`04-slash-commands.md:150`, `03-keyboard-shortcuts.md:273`)
- **Box height/scroll.** The lane said the box "grows to a cap and then scrolls". **Downgraded to guess**: no bundled doc line says so.
- **Paste.** Text paste chips exist, but their format was not found. Images show as `[Image #N]`. **docs**. Nothing matching `Pasted …` is in `grok.strings` apart from an image message. **measured**

### Auggie 0.36.0 (`@augmentcode/auggie`): login wall
- **TUI stack and screen.** React with a yoga-based Ink-like renderer, on the main screen: 0 × `?1049h` in `auggie.log`. **measured**. It switches to the alternate screen only while an external editor runs. **source per lane**
- **Box.** Bordered, with the border colour changing by mode. Prompt prefixes: `›` normal, `/` command, `!` bash, `#` ask, `∷` prompt enhancer. **source per lane, not re-checked**
- **Empty box.** Placeholder "Try 'how do I log an error?' or type / for commands". **source** (re-found in `augment.mjs`). In ask mode it is **"Ask a question..."**. **source** (new)
- **Newline.** Ctrl+J, Option+Enter, and Shift+Enter in Ghostty. **docs**
- **Paste** (`augment.mjs`). **source**
  - Collapsing is off by default: `collapseLongPastedText ?? false`, with a threshold of `numPastedLinesToCollapse ?? 10`.
  - When it is on, a paste shows as `[Pasted text "…summary..." (N lines)]`. Expanded, it is a `┌─ Pasted text (N lines)` … `└─ End of pasted text` block.
  - Ctrl+E toggles a block. Ctrl+O on an expanded block **edits the paste block**.
- **Other keys.**
  - Esc Esc clears. Ctrl+S stashes and Ctrl+T recalls. Ctrl+/ undoes. **source per lane**
  - The external editor appears as both "Ctrl+O to open external editor" (tips list and ask-mode help) and `CTRL_G_OPEN_EDITOR: "Ctrl+G" "Open in Editor"`. **source**. Which one applies to the main prompt is open.
- **Unknown.** Height cap and wrap width.

### CodeBuddy Code 2.158.0 (`@tencent-ai/codebuddy-code`)
- **Layout.** A close copy of Claude Code's layout. Main screen (0 × `?1049h` in `cbc.log`), a welcome box, then a login menu. **measured**
- **Paste.** The placeholder is `[Pasted text #N: M lines]`, with a **colon**, unlike Claude (`buildPastePlaceholder` in `dist/codebuddy.js`). **source** (re-found)
  - **New:** collapse happens when `text.length > 800 || text.split("\n").length > 2`, so 3 or more lines (`shouldCollapsePaste`). **source**
- **Newline.** `/terminal-setup` configures the newline key; the welcome box says so. **measured**

### Warp: out of reach of a PTY wrapper
- Warp's agent input is drawn by the Warp app itself, not by a program on a pseudo-terminal. Its `warp`/`oz` CLI is headless and takes the prompt as an argument. **docs** ([Warp CLI quickstart](https://docs.warp.dev/reference/cli/quickstart))
- This is a limit of unsent's design, which reads a child's screen through a PTY. It is not a fundamental one: support would need a Warp-side API or integration, and none is known. **guess**
- unsent still protects Claude Code and other agents running inside Warp's terminal.

## How stable the screens are across versions
- Kimi (a new rewrite), Cline 3 (moved to OpenTUI in 2026) and Grok (weekly releases) are the most likely to change. **guess**
- Every reader should key on the prompt glyph plus the rule or border shape, and fail safe when either is missing.
- Known breakers:
  - the box width changing (Cline home vs chat)
  - mode labels and corners changing on the rules (Vibe mode title, Kimi `! shell mode` and `├┤`, Grok `Stashed`)
  - theme colours (Cline, OpenHands, Kilo)
  - Letta's unframed redraws

## Side effects and leftovers of the lane's run (measured by lane)
- Grok Build's and Letta's login screens start device-code logins and may have opened browser tabs. Nothing was approved.
- The first Kilo run inherited `GITHUB_TOKEN` and AWS variables from the tmux server and auto-selected Copilot, then Bedrock (`k.cap` shows "Claude Sonnet 4.6 (US) Amazon Bedrock"). No prompt was sent.
- The throwaway Cline binary was re-signed ad hoc, and its leftover hub daemon was killed.
- `/tmp/unsent-other` is **4.1 GB** on the internal disk (re-measured with `du`). It holds a fake Mistral key and a fake Ollama config. Ask before deleting it; it is the only copy of these captures.

## Open questions
1. Grok's text-paste chip format and its box geometry (height cap, wrap, glyph). These need a logged-in session.
2. Auggie's border, height cap and wrap width, and which of Ctrl+O and Ctrl+G opens the editor on the main prompt.
3. Cline's chat-view width, which needs one submitted prompt.
4. The multi-line mode of OpenHands, and Ctrl+G in Kimi and Vibe, were read in source only. They need a harmless `EDITOR`-script test like the one for Claude.
5. Whether the Ctrl+D binding in Cline's root keyboard handler swallows the Textarea's forward-delete.

## Verification notes
The skeptic checked against the lane's scratch dir `/tmp/unsent-other` (raw logs, `.cap` screens, source clones, installed packages, Grok's bundled docs) and re-fetched registry data.

- **Re-counted in raw logs and confirmed:**
  - `?1049h` presence per agent: Kimi, Letta, Auggie and CodeBuddy are 0; Cline, Grok, Kilo, Vibe and OpenHands are ≥1.
  - Kimi's `?1004h`/`?2031h`, and its `↑ 1..25 more` / `↓ N more` labels.
  - The paste markers of Kimi, Cline, Letta (including `p1↵p2↵p3`) and Kilo.
  - The hint colours of Cline, OpenHands, Letta and Kilo.
  - The Letta, Vibe and OpenHands box shapes, from the `.cap` files.
- **Confirmed in source:**
  - Kimi: the height formula, paste thresholds, keybindings, Ctrl+C clear, history path and fullscreen env.
  - Cline: `maxHeight=5`, `HOME_VIEW_MAX_WIDTH`, the paste threshold and format, and Ctrl+C.
  - Letta: the paste threshold, `↵` substitution, Esc Esc and Ctrl+C.
  - Vibe: the newline keys, 50vh, the Ctrl+C ladder and Ctrl+G.
  - OpenHands: the heights, Ctrl+L, Ctrl+J and Ctrl+X.
  - Auggie: the placeholder and the paste-collapse default of 10.
  - CodeBuddy: the colon placeholder.
  - Grok: the key docs.
- **Corrected:**
  1. Cline's weekly npm count is 66,123, not 54k.
  2. The 12k legacy `kimi-cli` count is PyPI, not npm.
  3. Kimi's top corners become `├┤` while the /btw panel is open, and the recipe now accepts them.
  4. Kimi's scroll labels are centred, with left and `...` fallbacks.
  5. Kimi's fullscreen env is in `tui/config.ts`.
  6. Grok's "grows to a cap then scrolls" is downgraded to guess.
  7. Warp is reframed from "can't be supported" to a design limit of a PTY wrapper.
- **Added:**
  1. Letta has no `?2026` framing, and it has an unbracketed-paste heuristic.
  2. Cline's placeholder string also appears as a heading outside the box, and the Ctrl+D root binding.
  3. Vibe rewrites path pastes to `@path`, its empty box has 3 rows, and Ctrl+C force-quits when there is no app server.
  4. OpenHands switches back to single-line on submit, strips whitespace, and uses two separate widgets.
  5. Grok's Esc Esc stash/restore and rewind-on-empty.
  6. Auggie's ask-mode placeholder and Ctrl+O "edit paste block".
  7. CodeBuddy's collapse threshold (>800 chars or 3+ lines).
- **Dropped:** the lane's open question about a relayed request to edit memory/CLAUDE.md. It is unrelated to this profile.
- **Not re-checked** (kept at the lane's tag):
  - the wrap widths
  - Kimi's 6-line inline paste
  - Letta's 45-row overflow
  - Vibe's measured 20-row cap
  - Kilo's 13-row cap and newline keys
  - Cline's OpenTUI delete-key defaults
  - the codesign/daemon traps
  - the small-agent download counts
  - the cursor observations
