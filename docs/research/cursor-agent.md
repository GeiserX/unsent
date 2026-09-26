# Cursor CLI agent (`cursor-agent`): verified input-box profile for unsent

Build examined: `cursor-agent 2026.07.20-8cc9c0b`, the only version directory under `~/.local/share/cursor-agent/versions/`.

Tags on each claim:

- **[measured]**: seen on a live screen or in a raw byte capture from this lane.
- **[source]**: read in the shipped minified bundle and re-checked by the skeptic, with file and character offset. Offsets only hold for this build.
- **[source, lane]**: the lane cites the bundle and the skeptic did not re-open that spot. Treat it as likely, not confirmed.
- **[docs]**: from published docs.
- **[guess]**: inference, unchecked.

**Bottom line.** The only live measurement is the login gate. Nothing about the input box has been seen on a screen. The box facts below come from the bundle; the skeptic re-read most of the load-bearing ones and they hold. Before a `cursorBox` reader ships, someone has to log in and capture a real box.

---

## 0. What was actually measured

- `~/.local/bin/cursor-agent` links to `~/.local/share/cursor-agent/versions/2026.07.20-8cc9c0b/cursor-agent`, a launcher that runs the bundled `node` on `index.js`. **[measured]**
- The lane started it in a private tmux server (`tmux -L unsent-spec-cursor-agent -f /dev/null`, 120x40, in `/tmp/unsent-spec-cursor`). It stopped at a login gate and no key was sent. **[measured]**
- Raw bytes of the gate (`/tmp/unsent-spec-cursor-raw.typescript`, 326 bytes; the skeptic re-read the file): **[measured]**
  ```
  ESC[2J ESC[H ESC[2K ESC[G  (\r\n x19)
  <47 spaces>ESC[1mCursor AgentESC[22m\r\n
  <47 spaces>ESC[2mv2026.07.20-8cc9c0bESC[22m\r\n
  <47 spaces>ESC[32mPress any key to log in...ESC[39m  (\r\n x19)
  ESC[?25l
  ```
  There is no `ESC[?1049h`. The gate runs on the main screen, clears the visible screen, and hides the cursor.
- Stderr said ``Tip: You can start the Cursor CLI with `agent` (same as `cursor-agent`).`` in cyan. **[measured]**
- `cursor-agent status` prints `✓ Login successful!` / `Logged in (unable to fetch user details)` (`/tmp/unsent-spec-cursor-status.txt`), yet the TUI asks for a login. **[measured]** Why is unknown; a stale cached token is a **[guess]**.
- Settings on this machine (`~/.cursor/cli-config.json`): `editor.vimMode=false`, `display.zenMode=true`. **[measured]**

## 1. TUI stack

- Ink is vendored under `../ink/build/*` inside `index.js`, together with its own `log-update.js`. The dependency set is React 19.2.4 + yoga-layout, and the renderer uses ansi-escapes 7.3.0 and cli-cursor 4.0.0. **[source]** (`index.js` ~4715856, module paths in the pnpm store names)
- Main render options: `maxFps:30, incrementalRendering:false` (`index.js` ~4744350). The CLI's own `render(...)` call (`371.index.js` ~792135) passes an `onFullScreenClear` hook and does not override `incrementalRendering`. **[source]**
- **The redraw is different from what the lane said.** In non-incremental mode, log-update finds the *first* line that changed. It erases from there to the bottom (`ESC[2K` + `ESC[1A` per line, then `ESC[G`) and rewrites *every* line from that point down, all in one `write()`. It does not rewrite only the changed rows. **[source]** (`index.js` log-update module ~4715870)
- **Full clear when the live region is taller than the terminal.** If the lines to redraw exceed `process.stdout.rows`, it writes `ESC[2J ESC[3J ESC[H` and repaints. The CLI logs this as `"ink full-screen clear (live region exceeds terminal height)"`. `ESC[3J` also **erases the scrollback**, so unsent's recovery notice can be wiped mid-session and not only at start-up. **[source]** (ansi-escapes `Ik` in `index.js`; the message at `371.index.js` ~792135)
- No synchronized output in the main render path. `?2026h` / `?2026l` occur exactly 3 times each, all in `371.index.js` (137390/137721, 148817/151744, 152029/153271), inside the diff/review pager. **[source]** (the skeptic counted occurrences in all `*.js`)
- Where the code lives: `7910.index.js` holds `./src/components/text-input.tsx`, `./src/hooks/use-text-input.ts` and `./src/components/text-layout.ts`; `371.index.js` holds the prompt bar (`promptSymbol` default at 335078). **[source]**

## 2. Screen modes

- The chat is inline on the main screen (Ink + log-update). **[source]** Whether the chat view clears the screen at start the way the login gate does is **unmeasured**.
- `ESC[?1049h` occurs 4 times: `371.index.js` 116962 (the diff/review pager, followed by `?25l` at 116982) and 478609 (the resume picker), `4926.index.js` 2203 (another pager), `5723.index.js` 32278 (worker mode). **[source]** The prompt bar is hidden while one of these is open. **[source, lane]**
- Modes it sets: bracketed paste `ESC[?2004h` (mount/unmount of text-input, `7910.index.js` 16147); `?2031h` (`7910.index.js` 27981); focus reporting `?1004h` (`371.index.js` 744438). **[source]** The OSC 11 background query is **[source, lane]**.
- **Kitty keyboard protocol**: `process.stdout.write("\x1b[>1u")` runs at start-up only when `stdin.isTTY` (`371.index.js` 788947). It is popped with `\x1b[<u` on SIGINT, on normal exit (792616) and on suspend, and pushed again on resume (960640/960677). **[source]**

## 3. What the box looks like

All **[source]** unless tagged otherwise.

- **Container** `Ae` (`371.index.js` ~377119): `Box marginX:1, marginY:(half?0:1), width:zs`, where `zs = columns − 2` (369556). Inside it: `Box backgroundColor:<bar colour>, paddingLeft:1, paddingY:(half?0:1)`. There are no box-drawing rules.
- **Bar colour**: `eo = Po(detectedBg, light ? uN : ge)`; for dark, `ge = {tintHex:"#505050", mixRatio:.95, fallbackHex:"#151515", fallbackAnsi256:233}` (`index.js` `./src/constants.ts`). It is emitted as SGR 48, truecolor or 256-colour. The escape form is **[source, lane]**.
- **Two edge layouts.** `half` is true only when all of these hold: `AGENT_CLI_DISABLE_HALF_BLOCK_PROMPT_BAR` is not truthy; stdout is a TTY; `TERM` ≠ `dumb`; colour support ≠ `none`; a UTF-8 locale; and `terminalSupportsPromptBarHalfPadding()`. That last check tests the program name against this set: `alacritty, ghostty, gnome-terminal, hyper, iterm2, kitty, konsole, mintty, rio, tabby, terminator, tilix, vscode, warp, wezterm, windows-terminal`.
  - Half-block: a `▄` × (cols−2) row above and a `▀` × (cols−2) row below. Each is drawn as a foreground-coloured Text in the bar colour, starting at col 1, with no margin rows.
  - Padded: one blank margin row, one bar-background pad row, the text rows, another pad row, one blank margin row.
- **Correction: the terminal is detected from env keys first, not from `TERM_PROGRAM`.** `detectTerminalProgram` (`index.js`, `./src/utils/terminal-environment.ts`) checks these first: `GHOSTTY_RESOURCES_DIR`, `KITTY_WINDOW_ID`, `ALACRITTY_*`, `WARP_IS_LOCAL_SHELL_SESSION`/`WARP_HONOR_PS1`, `ITERM_SESSION_ID`/`ITERM_PROFILE`, `WT_SESSION`, `KONSOLE_VERSION`, `VTE_VERSION`, and others. Only after those does it fall back to `TERM_PROGRAM` (with `tmux → "tmux"`), then `LC_TERMINAL`. **[source]** Consequences:
  - The lane said "padded inside tmux". That holds only when none of those env keys reach the process. tmux started from Ghostty, iTerm2, kitty or Warp usually carries them, so the layout is half-block there too. **[guess, on tmux env inheritance]**
  - Under unsent the env passes straight through, so the layout follows the user's real terminal. This machine's shell has `TERM_PROGRAM=WarpTerminal`, which maps to `warp` and is in the set, so it will most likely draw the **half-block layout**. **[measured env; layout is source-derived]**
  - The reader must handle both layouts either way.
- **Text row geometry** (0-based cols): col 0 is the margin, col 1 the bg pad, col 2 the glyph, col 3 a space, and text runs from col 4. The text width is `Ds = columns − Re` with `Re = 7` (`371.index.js` 369448, 377634), so text ends at col cols−4. Cols cols−3..cols−2 are bar background and col cols−1 is the margin. Continuation rows have blank cols 2–3.
- **Glyph row** (`371.index.js` ~370544): `Text color = cloud ? magenta : shell ? yellow : "foreground"`, `dimColor = !cloud && !cloudSetup && !shell && value.length===0`, and the content is `& ` / `! ` / `promptSymbol + " "`. The `promptSymbol` default is `→` (335078); it is `>` when a local agent runs (880471). **The glyph is dim exactly when the draft is empty in normal mode.** Confirmed.
- **The glyph row can be hidden.** It has `display:none` while a sudo-password prompt, decision dropdown, plan suggestion, debug-action suggestion or interaction prompt is up. The whole bar has `display:none` during the last four of those, and whenever `AGENT_CLI_HIDE_PROMPT_BAR=true` with no pending decisions (~370030). The reader must fail safe and keep the last draft.
- **Placeholders** (~371342–371537): `Plan, search, build anything` (empty chat); `Add a follow-up`; `Add a follow-up — /plan to review and build`; in shell mode `Run a command — e.g., git status` (`ls` outside a repo), or `Run a command` below 50 columns. There are mode-specific texts for ask-question, plan-revision, rejection-reason and similar modes. A right-aligned dim `ctrl+c to stop` appears when the box is empty and the agent is processing.
- **Placeholder style** (`7910.index.js` ~3100): when focused, it is `cursorStyle(first char) + dim(rest)`, so col 4 is inverse and **not dim**, and the rest is dim.
- **Text inside a draft that is not the draft (new finding).** A slash-command **ghost text** is drawn dim right after the value when the cursor is at the end and a matching palette item has `ghostText` (`7910.index.js` ~4020 `xt=r&&st.length>0&&Q===st.length`; `371.index.js` `Tr`, which is `" " + item.ghostText` for a single `/cmd` word). **The reader must drop trailing dim cells on the cursor row**, otherwise the hint is saved as draft text.
- **Draft styling**: `[Pasted text #N …]` tokens are dim cyan on dark and dim gray on light, and known `/commands` are bold cyan / bold blue (`7910.index.js` ~1447). So "any dim cell means empty" is wrong. Use the glyph's dim bit.

## 4. Cursor

- log-update hides the real cursor through cli-cursor, and `?25l` was seen at start-up. **[measured + source]**
- The insertion point is a styled cell: `chalk.inverse` normally, `bgGray` in vim normal mode, `bgBlue.white` in vim visual mode. At the end of the text an inverse space is appended (`7910.index.js` ~3100–3700). **[source]** The terminal cursor position means nothing here.
- **unsent's `screen.go` records only text, faint (dim) and the cursor position (`screen.go` 69, 74). It records no reverse-video and no background.** The cells in the `charmbracelet/x/vt` / ultraviolet layer already hold both, so exposing them is plumbing work, and nothing outside unsent blocks it. **[source: unsent code]**

## 5. Wrapping, height, scrolling

- Wrap width is `cols − 7`, word wrap with hard breaks (`text-layout.ts` `Vz`). Wide ranges count 2, combining and zero-width count 0 (`7910.index.js` ~4500). **[source]** The tab-as-4 in layout is **[source, lane]**. A typed or pasted tab becomes two spaces before it reaches the value (`7910.index.js`, in the bracketed-paste branch and elsewhere). **[source]**
- **Max rows = 6.** In the text input, `if(t.lines.length > u.V1) Kk(t, cursor, V1, scrollOffset, focused)`, and `constants.ts` exports `V1: () => Je` with `Je = 6`. **[source]** This path only runs when the value contains `\n` or is longer than the width. It is fixed and does not depend on terminal height.
- **Scrolling** (`Kk` in `text-layout.ts`): if `cursorLine < top` then `top = cursorLine`; else if `cursorLine >= top + 6` then `top = cursorLine − 5`. The window moves the minimum amount, one row at a time, and only while the input is focused. `hasAbove`/`hasBelow` are computed but used nowhere in the bundle, so **there is no more-above/below marker**. **[source]**

## 6. Pastes

- In the prompt bar's `onPaste` (`371.index.js` 373040): tabs become 2 spaces; **if `length > 800` the text becomes `[Pasted text #N +M lines]`**, where `Ie = (e,t) => "[Pasted text #"+e+" +"+(newlines+1)+" lines]"` (334660). A one-line 900-char paste therefore shows `+1 lines`. **[source]**
- The decode regex is `\[Pasted text #(\d+)(?: \+\d+(?: lines)?)?\]`. `Me()` expands tokens from the in-memory map at submit. **[source]**
- ≤ 800 chars is inserted inline, whatever the line count. **[source]** This differs from Claude Code, which switches to a placeholder at 6+ lines.
- The body of a >800 paste is written at paste time via `(0,C.w)(Ve, map)` to `<configDir>/chats/<md5(resolve(cwd))>/<chatId>/pasted_text.json`. The path helper `./src/state/index.ts` does `createHash("md5").update(resolve(cwd))` under `join(WI(),"chats")` (`6937.index.js` 61425 and duplicates). On this machine the config dir is `~/.cursor` (`~/.cursor/chats/<md5>/<uuid>/` exists). **[source + measured dir]** That the file keeps `{entries:{N:text}}` capped at 500, and that N continues from the stored max, are **[source, lane]**.
- The image-path branch is `Oe()` (≤ 4096 chars, image extensions or `file://`). **[source]** The `[Image #N]` token is **[source, lane]**.
- Unbracketed input: the 75 ms debounce constant is `j=75` (`7910.index.js` 10035). **[source]** Its use as an "aggregated" paste is **[source, lane]**.

## 7. Keys

From the `use-text-input.ts` handler (`7910.index.js`, offsets relative to the module start at 901). The skeptic read the raw bytes with `repr`. All **[source]** unless tagged.

**Newline** (only when `allowExplicitNewline`):

- `Ctrl+J`, or a lone `\n`/`\r` arriving without `key.return`.
- `Shift+Enter` as `ESC[13;2u`, `ESC[27;2;13~` or `ESC[13;2~`.
- `Meta+Enter` (`key.meta && key.return`).
- `\` then `Enter`: the backslash is replaced by `\n`.
- Three or more keys under 35 ms apart ending in Enter insert a newline, **but only when the box is non-empty**; on an empty box the burst submits.
- That tmux `send-keys Escape Enter` produces Meta+Enter is **[guess]**.

**Submit**: `Enter`.

**Delete keys** (bytes checked):

| key | bytes | effect |
|---|---|---|
| Backspace / Ctrl+H | `0x7f`, `0x08` | 1 char before |
| Delete | `ESC[3~` (without meta) | 1 char after |
| Ctrl+D | `0x04`, parsed ctrl+d | 1 char after, **only when the box has text**. The prompt bar passes `onCtrlD` only when the value is empty (`_r=0===e.length`, `371.index.js` 351287); that arms "Press Ctrl+D again to exit" |
| Ctrl+U, Ctrl+Backspace | starts with `0x15`, `ESC[127;5u`, parsed ctrl+u, backspace+ctrl | back to `T(value, cursor, width)`, the start of the **visual** row per the lane **[source, lane on `T`]** |
| Ctrl+K | `0x0b`, parsed ctrl+k | to the end of the logical line (`GD`); at the end of a line it joins the next line |
| Ctrl+W, Alt+W | `0x17`, `ESC w`, parsed ctrl/meta+w | word before |
| Alt+Backspace | `ESC 0x7f`, `ESC 0x08`, `ESC[8;3~`, `ESC[127;3u`, backspace+meta/ctrl | word before |
| Alt+D, Alt+Delete | `ESC d`, `ESC[3;3~`, meta+`ESC[3~` | word after |
| Ctrl+C | parsed ctrl+c | see below |
| vim normal mode | letters, Ctrl+W | many; cannot be classified from bytes |

- **Ctrl+C, corrected.** The prompt bar passes `clearOnCtrlC:!1` (`371.index.js` 371931). The text input therefore does not clear on its own; the bar's `Or` handler (350064) decides:
  - agent **processing** → it stops the agent and arms exit; **the draft is kept**;
  - idle with text → it clears the draft (`xr("")`) and arms "Press Ctrl+C again to exit";
  - shell, cloud, plan-revision and rejection-reason modes → it leaves the mode and clears the value;
  - exit already armed → it clears and exits.

  The lane's "clears the whole draft when it has text" is only true when idle.
- There is no undo: no `0x1f`/Ctrl+_ handler and no "undo" string in `7910.index.js`. `Ctrl+T` transposes (`0x14`). Ctrl+O and Ctrl+L are swallowed. Home/End/Ctrl+A/E/Alt+B/F and word-arrows move the cursor.
- History on Up/Down, `draftBeforeHistory`, and the Ctrl+Y snapshot are **[source, lane]**.

**Kitty keyboard protocol.** The CLI pushes `ESC[>1u` (disambiguate). Per the kitty spec, a terminal that honours it sends Ctrl+letter as `CSI <code>;5u` (Ctrl+W `ESC[119;5u`, Ctrl+U `ESC[117;5u`, Ctrl+K `ESC[107;5u`, Ctrl+D `ESC[100;5u`, Ctrl+H `ESC[104;5u`) and Alt+letter as `CSI <code>;3u`. Unmodified Enter, Tab and Backspace stay legacy. **[docs]** https://sw.kovidgoyal.net/kitty/keyboard-protocol/
- unsent passes the child's output to the outer terminal, so the push reaches the user's terminal. Whether Warp, Ghostty or iTerm2 honour it under unsent is **unmeasured**.
- unsent today: `oneCharKeys = {0x7f, 0x08, ESC[3~}`, `manyKeys = {0x17, 0x15, 0x0b, 0x1f}`, `aheadKeys = {ESC[3~, 0x0b, 0x1f}` (`wrap.go` 507–509). None of the Alt or CSI-u forms and no `0x04` are in there. **[source: unsent code]** `0x1f` is Claude Code's undo and harmless here.

## 8. External editor (ground truth for real-app tests)

- The editor function (`371.index.js` ~1007137–1007788) picks `$VISUAL`, then `$EDITOR`. It writes the value to `mkdtemp("cursor-agent-edit-prompt-")/prompt.md`, spawns the editor with stdio inherited, and returns `nextValue`, which the bar puts back with `xr(t.nextValue)`. With neither variable set it shows `Set $VISUAL or $EDITOR to edit your prompt in an external editor`. **[source]**
- It is gated by a shortcut-context check (`Qr = e => V(Hr()) && Gr(e)`). The Ctrl+G byte binding is **[source, lane]**.
- The value handed over is the raw box value, so `[Pasted text #N …]` tokens are probably not expanded. No `Me()` call sits on that path. **[guess]**

## 9. Persistence inside cursor-agent

- `prompt_history.json` in the same per-chat dir keeps **submitted** prompts, capped at 500 (`371.index.js` 601360, `const l=500`). **[source]**
- `pasted_text.json` holds bodies of pastes over 800 chars, written at paste time. **[source]**
- An unsent draft is not written to disk anywhere. **[source, lane]** The skeptic found no draft store in the prompt-bar or text-input paths he read, but did not audit the whole bundle.

## 10. Detection recipe (`cursorBox`), corrected

1. If a pager/picker `?1049h` is active, or the bar is hidden (no candidate row found), keep the last draft.
2. Candidate first row `y`, scanning bottom-up: cols 0–1 blank, col 2 ∈ {`→`, `!`, `&`, `>`}, col 3 space. Require the bar background on cols 1..cols−2 once unsent's screen exposes background. Without that check, `→` in transcript text is a false positive.
3. Bottom edge: the first later row that is either `▀` × (cols−2) from col 1 (half-block, the likely layout under Warp, Ghostty, iTerm2 and the others) or a pad row of pure bar background (padded layout). Top edge check: row y−1 is `▄` × (cols−2) or a pad row.
4. **Empty** ⇔ the col-2 glyph is faint (normal mode). In shell or cloud mode the glyph is never dim; there, empty ⇔ col 4 is reverse-video and the rest of the row is dim placeholder text.
5. Draft rows = cols 4..cols−4 of each row, right-trimmed. **Drop the trailing dim run on the cursor row (slash-command ghost text).** Keep dim-cyan `[Pasted text …]` tokens. Prefix `!` in shell mode.
6. Cursor = the reverse-video cell (vim: grey or blue bg). Never use the terminal cursor.
7. Un-wrap at `width = cols − 7`. `capped` = 6 rows; the window follows the cursor one row at a time and there are no overflow markers.
8. Paste tokens: `\[Pasted text #(\d+)(?: \+\d+(?: lines)?)?\]` (over 800 chars) and `\[Image #\d+\]`.
9. `deleteKeys` additions:
   - word/any-amount before: `ESC w`, `ESC 0x7f`, `ESC 0x08`, `ESC[8;3~`, `ESC[127;3u`, `ESC[127;5u`, `ESC[119;5u`, `ESC[117;5u`, `ESC[119;3u`;
   - after the cursor: `ESC d`, `ESC[3;3~`, `ESC[100;3u`, `ESC[107;5u`, plus `0x04` / `ESC[100;5u` (1 char, only meaningful with text);
   - one-char: `ESC[104;5u`.

   Ctrl+C with text while idle is a full clear. While the agent is processing it is not.
10. There are no `?2026` frame marks. Debounce on output quiet time. Frames go out in one `write()` at up to 30 fps, so torn reads need a split pty read **[guess]**. Expect `ESC[2J ESC[3J ESC[H` repaints whenever the live region outgrows the screen.

## 11. Stability and what breaks the reader

- Date-stamped builds sit under `versions/`, and only one is on disk (dir dated Jul 23). **[measured]** Self-update behaviour is **[guess]**.
- Fragile constants: `Re=7`, `V1=6`, the 800 threshold, the `Pasted text` wording, the `→` default and the prop overrides, the half-block terminal set, the dim-glyph rule, and the inverse fake cursor. Any of them can move in a new build. The reader must fail safe.
- `AGENT_CLI_DISABLE_HALF_BLOCK_PROMPT_BAR` forces the padded layout and `AGENT_CLI_HIDE_PROMPT_BAR=true` hides the bar. **[source]** unsent could set the first to get one layout, at the cost of changing the user's UI.
- A bordered `┌─…─┐` "For <file>" comment box in the diff pager (`371.index.js` ~140073, which also draws its own `→`) must not be mistaken for the prompt. **[source]**
- `display.zenMode` (true here) only switches tool calls to compact one-line rows (`371.index.js` 73393, 522557). No prompt-bar code path reads it. **[source]** This answers one of the lane's open questions.

## Open questions (still unmeasured)

1. A live login is needed (`cursor-agent login` or `CURSOR_API_KEY`) to capture: the empty box, a multi-line draft, 3- and 12-line pastes under 800 chars, a 900-char paste, the 6-row cap and scroll, and the edge layout under Warp directly and under tmux.
2. Does the chat view clear the screen at start-up? And how often does the `ESC[3J` full clear fire in a normal session (it wipes unsent's notice)?
3. Does the outer terminal (Warp especially) honour `ESC[>1u` through unsent, and which bytes arrive for Ctrl/Alt deletes?
4. Does Ctrl+G hand `$EDITOR` unexpanded `[Pasted text #N]` tokens?
5. Why does `status` say logged in while the TUI asks for a login?

## Verification notes

What was checked, against the bundle on disk (`~/.local/share/cursor-agent/versions/2026.07.20-8cc9c0b/`), unsent's source, and the lane's scratch files:

- **Held, now [source]**:
  - the `promptSymbol` default `→` / `>`, and the glyph `dimColor` expression;
  - the placeholder strings and the `ctrl+c to stop` hint;
  - the `Ae` container (marginX 1, marginY/paddingY 0 or 1, `▄`/`▀` rows of width cols−2), `Re=7` / `Ds=cols−7`, `zs=cols−2`;
  - `V1=Je=6` and the minimal-scroll `Kk` with unused `hasAbove`/`hasBelow`;
  - the `length>800` paste rule and the exact `[Pasted text #N +M lines]` builder;
  - the md5-of-cwd chat dir, `prompt_history.json` capped at 500;
  - the `?2026h` count and places, the `?1049h` places, the `?2004h`/`?2031h`/`?1004h` writes, and the `[>1u` push/pop places;
  - the Ctrl+G `$VISUAL`/`$EDITOR` flow and `prompt.md` temp file;
  - the delete-key byte forms, no undo, the 35 ms / 75 ms constants, and the Shift+Enter forms;
  - the dark-theme bar colour constants, and unsent's `wrap.go` 507–509 key lists.
- **Held, [measured]**: the login-gate bytes (the skeptic re-read the 326-byte capture), the stderr tip, the `status` output, and the single version dir.
- **Corrected**:
  1. Terminal detection reads env keys (`GHOSTTY_RESOURCES_DIR`, `ITERM_SESSION_ID`, `WARP_*`, …) before `TERM_PROGRAM`, so "padded inside tmux" is not reliable. On this machine (Warp) half-block is the likely layout.
  2. The renderer rewrites from the first changed line to the bottom, not only the changed rows. Renders are throttled to 30 fps.
  3. The full clear is `ESC[2J ESC[3J ESC[H`, which erases scrollback. The lane did not note this; it affects unsent's recovery notice.
  4. Ctrl+C: the prompt bar sets `clearOnCtrlC:false`. The draft is kept while the agent is processing and cleared only when idle, or when leaving shell or other modes.
  5. An Enter burst on an empty box submits rather than inserting a newline.
- **Added**:
  - dim slash-command ghost text after the cursor inside a non-empty draft, which the reader must strip;
  - the glyph row and the whole bar hide in several decision states;
  - `zenMode` does not touch the prompt bar;
  - unsent's `screen.go` exposes neither reverse-video nor background today (the vt layer has both).
- **Downgraded to [source, lane]** (not re-opened): `T()` = start of the visual row for Ctrl+U; N continuing from the stored max; `pasted_text.json` entry cap; `[Image #N]`; the aggregated-paste path; the history and Ctrl+Y snapshots; the Ctrl+G byte binding; "no draft written to disk" (plausible, not audited bundle-wide).
- **Removed**: the lane's first open question, about a relayed request to edit memory/CLAUDE.md. It is unrelated to this profile and carries no spec content.
- **Summary field**: the lane's JSON has `measured: true`. Only the login gate was measured; every input-box fact is source-derived.
