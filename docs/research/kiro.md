# Kiro CLI: input-box profile for unsent (verified)

Measured on Kiro CLI 2.24.1 (manifest `https://prod.download.cli.kiro.dev/stable/latest/manifest.json` says `2.24.1`; changelog date 2026-09-24). Kiro CLI is the renamed Amazon Q Developer CLI. The v2 terminal UI is closed source.

Tags:
- **[measured]**: seen by the research lane in a private tmux server (`tmux -L kirolane -f /dev/null`, 120x40 unless stated). Where a raw capture in `/tmp/kiro-lane/` backs it, the file is named. **[measured, re-checked]** means I replayed that capture through unsent's own emulator (`charmbracelet/x/vt` at the version in unsent's `go.mod`, frame by frame at each `ESC[?2026l`) or counted the bytes myself. "(capture not retained)" means only the lane's word backs it.
- **[source]**: read in the unpacked UI bundle `/tmp/kiro-lane/tui.js` (13.3 MB minified, sha256 prefix `b893ab8ab0e5086b`, same as the `tui.js.sha256` Kiro wrote on disk). Minified names such as `xet()` are good for 2.24.1 only. Also the legacy Q CLI source (`aws/amazon-q-developer-cli` at `15cc8f3`, 2026-04-23) and CAO (`awslabs/cli-agent-orchestrator` at `5471d8f`, `src/cli_agent_orchestrator/providers/kiro_cli.py`).
- **[docs]**: `https://kiro.dev/docs/cli/terminal-ui/` and `https://kiro.dev/changelog/cli/` (saved as `terminal-ui.txt`, `cl.txt`, `changelog.txt`).
- **[guess]**: inference nobody verified.

## How it was measured
- DMG sha256 starts `b024a670ef1a1f88`, matching the lane's claim. It was mounted read-only under `/tmp/kiro-lane/mnt` (now detached). [measured, re-checked]
- `run.sh` runs `kiro-cli-chat chat` under `env -i` with `HOME=/tmp/kiro-lane/home`, `PATH=/usr/bin:/bin`, `TERM=xterm-256color`, `KIRO_DISABLE_TELEMETRY=1`, `Q_DISABLE_TELEMETRY=1`, `KIRO_NO_AUTO_UPDATE=1`, `KIRO_API_KEY=fake-not-a-key`. `runl.sh` is the same with `--legacy-ui`. [measured, re-checked]
- **This environment matters for later claims.** `TMUX`, `TERM_PROGRAM`, `ZELLIJ` and `KITTY_WINDOW_ID` were all unset in the child. Kiro changes its cursor, keyboard protocol and sync output based on exactly those variables (see below). Under unsent the child inherits the user's environment, so a real user in tmux, Ghostty, kitty, WezTerm or iTerm2 gets different behaviour from what was captured.
- The dummy API key gets past the login gate (`You are not logged in. Login now?`) and draws the real box, plus notices such as `● failed to retrieve governance settings — MCP and web tools disabled`. [measured, re-checked in `raw1.bin`/`raw2.bin`]
- **Nothing was submitted.** All six session files under `home/.kiro/sessions/cli/` have empty `.jsonl` transcripts and `"user_turn_metadatas": []`. [measured, re-checked]
- Captures kept: `raw1.bin` (inline, 331 frames: typing, wrap, long-token test, 50-line draft, offscreen edit, resize), `raw2.bin` (inline, 213 frames: pastes of 3, 10, 11 lines and 500 chars), `raw3.bin` (legacy UI), `raw4.bin` (fullscreen, 344 frames), `before.txt`/`after.txt`/`after2.txt` (tmux snapshots around the offscreen edit), paste inputs `p3.txt`, `p10.txt`, `p11.txt`, `c500.txt`, `c501.txt`.

## TUI stack
- `kiro-cli-chat chat` (Rust) unpacks an embedded `bun` and `tui.js` into `~/Library/Application Support/kiro-cli/` and runs the UI on Bun. Both files and their `.sha256` are present in the isolated HOME. [measured, re-checked on disk] The UI talks ACP to a `kiro-cli-chat acp` child. [measured (process tree not retained)]
- The UI is React with Yoga layout, drawn by its own inline diff renderer with `TWINKI_*` switches: `TWINKI_HARDWARE_CURSOR`, `TWINKI_NO_SYNC`, `TWINKI_CLEAR_ON_SHRINK`, `TWINKI_WRITE_LOG`. It is not Ink. Input decoding is its own: a stdin buffer, a kitty keyboard push or query, and a modifyOtherKeys fallback. [source]
- **UI engine choice.** The docs give the order flag (`--tui` / `--classic`) > `KIRO_CHAT_UI` > setting `chat.ui` > default `tui`. [docs] `--legacy-ui` also works; `raw3.bin` came from it. [measured]
- The UI is also selected by engine: V2 (default) and V3 (`--v3`, "early release"), plus a `lite` mode. 2.24.1 prints `An early release of Kiro CLI V3 is now available!` on start, and the docs already carry a "What's new in 3.0" page. Only the default V2 TUI was measured. [measured + docs]

## What the box looks like (default TUI, inline)
Real capture, `raw2.bin` frame 205, 120 columns, replayed (trailing spaces trimmed):
```
────────────────────────────────── … (full width U+2500)         <- rule; a "What's new" panel above sits between two such rules
kiro_default · ◔ 3%                         /private/tmp/kiro-lane/scratch   <- status row, cwd right-aligned
                                                                             <- blank row
› do nothing; reply ok p1 one Press Tab to expand                   <- "›" U+203A at col 0, space, text from col 2; the hint is SGR 2
  p1 two                                                             <- continuation rows: 2-space indent
  p1 three
  ten 1
  …
  ten 10
   11 lines ▸                                                        <- paste chip, own background
```
- No border and no rule below the box. Above it: rule, status row, blank row, then the `›` row. [measured, re-checked]
- **The status row is not fixed text.** Seen in the captures: cwd only (boot), `kiro_default` + cwd, and `kiro_default · ◔ 3%` + cwd. [measured, re-checked] CAO matches `<agent> · <model> · ◔ N%`, and the 2.24.1 changelog changed when a `thinking` chip shows there. [source + docs] Anchor on "row between a rule and a blank row", not on its text.
- Colours in the default theme: rule 38;5;235, agent name and cwd 38;5;141, `·` 38;5;244, `◔ 3%` 38;5;42. Theme-dependent; do not key on them. [measured, re-checked]
- **Boot.** A dim (SGR 2) `⠋ Launching...` spinner is drawn first, then the screen is cleared. During init there is a `● Initializing...` row above the rule and the hint reads `Initializing · type to queue a message`. [measured, re-checked] Typing during boot is kept. [measured (capture not retained)] CAO saw a paste dropped while the older `M of N mcp servers initialized. ctrl-c to start chatting now` screen was up. [source, CAO]
- **Prompt glyph** `›` U+203A. `KIRO_ASCII_MODE=1` or `chat.allowAsciiArt=false` switches glyphs to ASCII. [source: `i1()`] The ASCII prompt glyph itself was not seen. [guess that it is `>`]
- **Shell mode:** a leading `!` is drawn in 38;5;141 inside the draft; the glyph stays `›`. [measured (capture not retained)] Slash commands and mentions are also coloured draft text. [source]
- **Rows below the box:** a right-aligned grey `/copy to clipboard` tip under the empty box; the slash menu with its own `─` rule and `esc to cancel · ↑↓ to navigate · ↵ to select`. [measured: the tip is in `raw1.bin`/`raw2.bin`; the menu (capture not retained)]
- **iTerm2 left padding.** `paddingLeft:1` appears in three places in `tui.js`. The one I read is a scrolling list panel, not the chat input. Whether iTerm2 moves `›` to col 1 is unproven. [guess]

## Empty box and hints
- Empty row: `› ` + one SGR 7 space (the fake cursor) + hint in **38;5;247**, not SGR 2. Bytes: `› \e[7m \e[0m\e[38;5;247mask a question or describe a task ↵`. [measured, re-checked in `raw1.bin`] The same pattern holds for the boot hint. [measured, re-checked]
- So unsent's Claude rule ("dim means empty") does not carry over. An empty box is "SGR 7 space at col 2, then non-default-fg text from col 3, and no continuation rows".
- Hint text has changed over time: CAO's comments record `ask a question, or describe a task` (older, comma), a capitalised form (said to be v1.29+), `Kiro is working` replaced by `Thinking...` in 2.11, and `Initializing · type to queue a message` in 2.8. 2.24.1 shows lowercase with no comma. [source, CAO + measured] Treat hint strings as unstable.
- **Paste hint.** After a collapsed paste, ` Press Tab to expand` in **SGR 2** is appended to the end of the **first** draft row (`\e[2m Press Tab to expand\e[0m`). [measured, re-checked `raw2.bin`] A reader must drop SGR 2 runs from draft rows.

## Wrap and width
- Words wrap to (cols - 2) content columns after the 2-column prefix. At 120 columns a 117-character line fitted on one row (`alpha … psi`, `raw1.bin` frame 181), and a following word moved down. [measured, re-checked]
- **Width overflow: two measured shapes, not one.**
  1. **Status row spills by one column, draft intact.** A 118-character token at 120 columns (`raw1.bin` frame 187) drew the token plus the fake cursor on the next row, and the renderer itself wrote the status row as `…/scratc` with `h` on the following row, which is where the blank row normally is. Draft rows kept their indent and text. The same spill happened with a **500-character inline paste of short words** (`c500.txt`, 100 × `abcd `) at 120 columns (`raw2.bin` frame 208), so it is not only a long-token bug. [measured, re-checked; the lane reported only the long-token case]
  2. **Whole frame corrupted.** A 120-character token at 120 columns (`raw1.bin` frame 183): the status row splits (`scrat`/`ch`), `›` is alone on its row, every draft row loses its 2-space indent, and the digit at the wrap edge is lost (the next row starts ` 2345…`). [measured, re-checked] It recovers when the token is deleted (frame 188). [measured, re-checked]
- The lane says this was also seen at 100 columns. [measured (capture not retained)]
- Setting `chat.disableWrap` exists and is passed to the renderer as `wrapDisabled`. [source] Not measured.

## Height, scrolling, cursor
- **No height cap, no internal scroll.** A 50-line draft rendered in full; the top went into terminal scrollback and the view stayed pinned to the bottom. [measured, re-checked: `raw1.bin` final frame and `before.txt` show only the tail `line 15 … line 50`] In fullscreen the same happens on the alternate screen, where there is no scrollback: `raw4.bin` ends with only `  line 9 … line 45` visible and no rule, status row or `›`. [measured, re-checked]
- **The view does not follow the cursor.** `before.txt`, `after.txt` and `after2.txt` (tmux snapshots before and after an edit above the screen) are byte-identical. In `raw1.bin` the edited row `  lineZZ` first appears only in the full redraw after the resize (byte 81878, after the first `ESC[3J` at 77106). [measured, re-checked]
- **Resize clears scrollback.** Each resize redraw sends `ESC[2J` and `ESC[3J` (4 × `ESC[3J` in `raw1.bin`, frames 329-330) and re-prints the transcript. [measured, re-checked] That is the default `preserveScrollbackOnRedraw=false`; the `chat.preserveScrollback` setting (added in 2.20) changes it. [source + docs] Not measured with the setting on.
- **Cursor.** The renderer sends `ESC[?25l` constantly (410 times in `raw1.bin`, 0 × `?25h`) and draws a fake cursor as an SGR 7 cell. After each frame it parks the hidden cursor at the insertion point (for example `\e[3G` with an empty draft, `\e[120G` after a 118-char token). [measured, re-checked] When the insertion point is above the screen the hidden cursor clamps to row 0 (`raw4.bin` final: `(8,0)`). [measured, re-checked]
- **Hardware cursor mode** is chosen by `pD()`: `TWINKI_HARDWARE_CURSOR=1` forces it on, `=0` forces it off, otherwise it is on when `TMUX` or `ZELLIJ` is set. [source] So **a user running unsent inside tmux gets the real cursor and no SGR 7 cell**, the opposite of the lane's captures. The reader must handle both. The lane measured the visible cursor with `TWINKI_HARDWARE_CURSOR=1`. [measured (capture not retained)]
- The fake cursor can sit on a real character, including a real space mid-line: `  \e[7m \e[0m6` in `raw1.bin`. [measured, re-checked] A reverse-video space at the end of a line is ambiguous: it is either the cursor after the text or the cursor on a real trailing space. Dropping it can lose at most one trailing space. [guess, from the byte pattern]
- Blank draft lines render as empty rows; trailing ones look like the footer rows unless the cursor is on them. [measured]

## Screen modes and escapes
- **Inline by default:** no `?1049h` in `raw1.bin`/`raw2.bin`. [measured, re-checked]
- **Startup** (`raw2.bin`, in order): `\r\e[K\e[2J\e[H`, a mouse blip `?1000h ?1003h ?1006h` then `?1006l ?1003l ?1000l`, `\e]11;?\a` (background query), `?2004h`, `\e[?u` (kitty query), `\e[>4;1m` (modifyOtherKeys), `?25l`, `\e[6n`, and later `?1004h`. [measured, re-checked] The `ESC[2J` wipes the visible screen, so **unsent's pre-launch recovery notice is erased**, not just hidden as with Claude Code. [measured, re-checked] (`raw1.bin` lacks the `]11;?` query; I did not chase why.)
- **Fullscreen** (`/fullscreen` or `chat.startFullscreen`): `?1049h`, and after the startup blip `?1000h ?1003h ?1006h` again with no matching `l` in the capture, so any-motion mouse tracking stays on. [measured, re-checked `raw4.bin`] The input stream then carries mouse reports that `deleteKeys` must skip.
- **Synchronized output:** 213 matched `?2026h/l` pairs in `raw2.bin`, 330 in `raw1.bin`, 344 in `raw4.bin`; nearly every frame. [measured, re-checked] The renderer emits them unless `TWINKI_NO_SYNC` is set. [source: `emitBsu=!process.env.TWINKI_NO_SYNC`] The docs say `KIRO_NO_SYNCHRONIZED=1` disables sync output. [docs] But the terminal-detection code that reads it already returns "no sync" for the measured environment (no `TERM_PROGRAM`, no `TMUX`), and the pairs were still emitted, so `KIRO_NO_SYNCHRONIZED` probably does not stop the renderer's pairs. [guess, from source + measurement] unsent must read only outside pairs.

## Pastes
- **Collapse rule** (`xet()`): collapse when `content.split(/\r\n|\r|\n/).length > 10` **or** `content.length > 500`. [source, re-checked] Measured: 3 and 10 lines inline, 11 lines → ` 11 lines ▸ `, 500 chars inline, 501 chars → chip. [measured; 3/10/11 lines and 500 chars re-checked in `raw2.bin`, 501 (capture not retained)]
- Two details the lane did not state [source]: `length` is JavaScript UTF-16 length, so an emoji counts as 2; and a trailing newline adds a line, so 10 lines plus a final newline is 11 and collapses. unsent's `paste.go` must use the same arithmetic to predict which pastes become chips. The content passes through two normalising functions before the check (`bge()`, `er()`); I did not read what they strip. [guess that they normalise line endings]
- **Chip bytes:** `\e[48;5;141m\e[38;5;235m 11 lines ▸ \e[39m\e[49m`, i.e. fg 235 on **bg 48;5;141** (the lane wrote "bg 38;5;141"). Label ` N lines ▸ ` or ` N chars ▸ `, one space each side, **no index number**. [measured, re-checked] One Backspace deletes a chip; Tab expands it into editable text. [measured (capture not retained) + docs]
- Image chips (`name (W×H size)`) and `@file` chips also exist. [source]
- Legacy UI: pastes arrive inline (rustyline). [source, Q CLI]

## Keys that matter to unsent
- **Newline:** Ctrl+J; Esc+Enter / Alt+Enter; Shift+Enter (needs kitty keyboard or modifyOtherKeys). The Enter handler `ttt()` returns `newline` when `meta` or `shift` is set and otherwise submits; there is **no backslash-continuation case**, so `\` + Enter submits. [source, re-checked; docs list the same newline keys] Ctrl+J and Esc+Enter were measured. [measured (capture not retained)]
- While the agent works, Enter queues a non-empty draft (`submit-during-processing`). [source, re-checked]
- **Keyboard protocol depends on the terminal, decided from environment variables** (`queryAndEnableKittyProtocol`): when `TERM_PROGRAM` is `WezTerm`, `iTerm.app` or `ghostty`, or `TERM` is `xterm-kitty`/`xterm-ghostty`, or `KITTY_WINDOW_ID` is set, Kiro pushes kitty flags `ESC[>1u` directly without asking. Otherwise it sends the `ESC[?u` query plus `ESC[>4;1m`. [source, re-checked] With kitty flag 1 the kitty spec encodes Ctrl and Alt combinations as CSI-u: Ctrl+W as `ESC[119;5u`, Ctrl+U `ESC[117;5u`, Ctrl+C `ESC[99;5u`, Alt+Backspace `ESC[127;3u`. [docs, kitty keyboard protocol; not measured with Kiro] **On Ghostty, kitty and WezTerm, which honour the push, `deleteKeys` sees those forms, not the bytes below.** Whether iTerm2 honours it depends on its settings. [guess] The lane's captures ran with none of these variables set, so none of them shows this.
- **Delete before the cursor (legacy bytes):** Backspace `0x7f`/`0x08`, Ctrl+W `0x17`, Alt+Backspace `ESC 0x7f`, Ctrl+U `0x15`. **After the cursor:** Delete `ESC[3~`, Ctrl+K `0x0b`, Alt+D `ESC d`, Ctrl+D `0x04`. [source + docs] Ctrl+D on an empty box counts towards exit (`dCn`). [source, re-checked]
- **Ctrl+C clears the whole draft only when idle.** Handler `uCn`: if a voice capture is running it cancels that; else if the agent is processing it **cancels the response and leaves the draft**; else if there is input it clears the input; else it counts towards exit (two presses within 2 s exit). [source, re-checked] The lane measured the idle case: one press, draft gone, Up did not restore it. [measured (capture not retained)] The key is the `quit` binding, default `ctrl+c`, remappable with `chat.keybindings.quit`. [source + docs] So `deleteKeys` must treat Ctrl+C as a whole-draft delete only when the box is idle, and should not assume the key is Ctrl+C when the user has remapped it.
- **Up on the first row / Ctrl+P** swaps in a history entry; the draft is held as a string in `savedInput` and Down past the newest entry restores it. [source, re-checked] Whether chips survive that round trip is unknown. [guess that they are flattened]
- Escape keeps the draft. [measured (capture not retained)]
- **Ctrl+Z needs two presses within 2 s** (`armSuspend`, then `suspendProcess`). Disabled while the subagent panel is open. [source, re-checked] Relevant to unsent's suspend handling.
- Other editing keys (transpose, case, yank, undo `Ctrl+_`) change text without a delete key. [source + docs]
- **Legacy UI** (`raw3.bin`): prompt `4% ` in SGR 32 then `> ` in **38;5;93** (the lane wrote plain 93), empty-box hint `Use @file or @dir to include file contents inline` in 38;5;240, continuation rows unindented (`second`, `third` at col 0). [measured, re-checked] In the Q CLI source, a trailing `\` or an odd number of fences keeps the line open (`prompt.rs:401-422`), and Alt+Enter / Ctrl+J insert a newline (`prompt.rs:608-618`). [source, Q CLI `15cc8f3`; Kiro's current legacy build is closed, so this may have drifted]

## Draft persistence, history, editor
- No saved draft. History (class `yr`) keeps the last 1000 **submitted** prompts, newlines escaped as `\n`, per session (`sessions/cli/<id>.history`) or global (`.cli_bash_history`). [source, re-checked; docs confirm the per-session default]
- `/editor [text]` opens `$VISUAL`/`$EDITOR` on a temp `kiro-editor-*/prompt.md` seeded with the **command's argument** (`initialContent: r || ""`), not the current draft, and on exit **sends** any non-empty result (`n.sendMessage(o.content)`). [source, re-checked] It cannot be a non-submitting ground truth like Claude's Ctrl+G. Ctrl+G opens the crew monitor. [docs]
- Ground truth for real-app tests: the known typed input, or render logs (`KIRO_RENDER_LOG_FILE`, `KIRO_RENDER_LOG=1`, `TWINKI_WRITE_LOG`). [source for the variables; whether they log frame text is a guess]

## Stability
- Releases: 2.16.0 (31 Jul 2026) through 2.24.0 (23 Sep) and 2.24.1 (24 Sep), with patch releases between. [docs, re-checked]
- 2.24.1 itself changed the status row (`thinking` chip). [docs] Hints have changed several times (see above). V3 is shipping as an early release and has its own docs. [measured + docs]
- The structure (rule, status row, blank row, `›`, 2-space indent) was only checked on 2.24.1, V2 engine, default theme.
- Not measured: V3, `lite`, non-default themes, `chat.disableWrap`, `chat.preserveScrollback`, a logged-in session while the agent works (queue tray, permission bar), iTerm2/Ghostty/kitty/WezTerm keyboard encodings, tmux-inherited hardware cursor.

## Detection recipe (`kiroBox`, default V2 TUI, inline and fullscreen)
0. **Box not live.** Return "no box" (keep the last draft) when the screen shows `You are not logged in`, or the list-selector footer `esc to cancel · ↑↓ to navigate · ↵ to select` in the bottom rows (menu or dialog open), or the boot spinner `Launching...`.
1. **Anchor.** Scan up from the bottom for the last row P whose col 0 is `›` (or `>` in ASCII mode) and col 1 a space. Walk up: row P-1 is blank **or** a short right-aligned fragment (the one-column cwd spill), then the status row, then a full-width run of `─`. Accept the rule at P-3 or P-4 for that reason. Do not match status text or colours.
2. **Corruption guard.** If row P is exactly `›` with nothing after it and the next row is not indented by 2, the whole frame has overflowed (shape 2). Return "unreliable" and keep the last draft. A status-row spill alone (shape 1) is **not** a reason to reject: the draft rows are intact.
3. **Empty box.** Row P col 2 holds an SGR 7 space (fake cursor) or a plain space (hardware-cursor mode, cursor at col 2), text from col 3 is in a non-default foreground and not SGR 2, and no indented rows follow. Treat known hint prefixes (`ask a question`, `Initializing · type to queue`, `Thinking...`, `Kiro is working`, `running shell command`) as supporting evidence only.
4. **Draft rows.** Line 0 is row P from col 2. Then take following rows while they start with two spaces (content from col 2), allowing blank rows between them. Stop at the first non-blank row that does not start with two spaces (slash menu, second rule, right-aligned tip). Trailing blank rows count only up to the cursor row: the hardware cursor (always at the insertion point) or the SGR 7 cell.
5. **Cell cleanup.** Drop SGR 2 runs (` Press Tab to expand`). Drop an SGR 7 space at the end of a row (fake cursor; may cost one real trailing space). Keep an SGR 7 cell over a real character or mid-line space. Keep coloured cells (`!`, slash commands, mentions).
6. **Chips.** A run with a non-default background shaped ` N lines ▸ `, ` N chars ▸ `, an image label or a file label is a chip. Match paste chips to captured bracketed pastes **by order**, predicting collapse with `lines > 10 || utf16Length > 500` (trailing newline counts as a line). One Backspace removes a chip; Tab expands it.
7. **Un-wrap.** Word wrap at (cols - 2) content columns, 2-space continuation indent, same idea as Claude's un-wrap at a different width.
8. **Offscreen.** No `›` visible but bottom rows start with two spaces: only the tail is visible; hand it to the stitcher. Hidden cursor at row 0 with no SGR 7 cell visible (or hardware cursor at row 0): the insertion point is above the screen and edits there do not show; unchanged rows are not proof that nothing changed. Expect `ESC[3J` on resize to wipe scrollback.
9. **Timing.** Read only outside `ESC[?2026h … l`.
10. **`deleteKeys`.** Before the cursor: `0x7f`, `0x08`, `0x17`, `ESC 0x7f`, `0x15`. After: `ESC[3~`, `0x0b`, `ESC d`, `0x04`. Also the kitty CSI-u forms (`ESC[119;5u`, `ESC[117;5u`, `ESC[127;3u`, `ESC[99;5u`, …) and modifyOtherKeys `ESC[27;mod;code~`; these are the normal case in Ghostty, kitty and WezTerm. Whole draft: Ctrl+C (legacy `0x03` or `ESC[99;5u`) **only when the box is idle** (not processing; the hint row is the practical signal). Recoverable replace: Up on the first row / Ctrl+P. Skip SGR mouse reports (`ESC[<…M/m`) in fullscreen.
11. **Child environment (optional).** `TWINKI_HARDWARE_CURSOR=1` gives one cursor model regardless of tmux. The legacy UI needs its own reader: prefix `[agent] N% ↯ !> `-style, unindented continuation rows.

## Risks
- Weekly releases of a freshly minified bundle; hints and status-row content already change between patch releases. Key on structure only.
- Width overflow: a status-row spill from long tokens or long pastes (shape 1) would make a strict anchor fail and save nothing; full overflow (shape 2) makes the screen wrong. Both measured at 120 columns.
- Edits above the screen are invisible until a full redraw, and a resize clears scrollback with `ESC[3J`.
- Paste chips carry no index; order is the only link to the captured paste.
- The empty hint is coloured, not dim, while the paste hint is dim: the Claude "dim means empty" rule is wrong here.
- Environment decides cursor model (tmux/zellij), keyboard encoding (Ghostty/kitty/WezTerm/iTerm2), and sync gating. The lane's captures cover only the bare case.
- Startup `ESC[2J` erases unsent's recovery notice.
- Ctrl+C is a silent whole-draft delete when idle and a harmless cancel when busy; `/editor` submits. No safe ground-truth escape.

## Open questions
- Does V3 or `lite` draw the same box?
- What does iTerm2 do with `ESC[>1u` by default, and does the iTerm2 `paddingLeft:1` touch the chat input?
- What sits above the box while the agent works (queue tray, permission bar)? CAO reports 2.11 needed two Enters after a bracketed paste to submit.
- Does `KIRO_NO_SYNCHRONIZED=1` actually stop the renderer's `?2026` pairs?
- Can `KIRO_RENDER_LOG_FILE` serve as a non-submitting ground truth?
- Should unsent set `TWINKI_HARDWARE_CURSOR=1` for the child to get one cursor model?

## Verification notes
What I checked:
- Replayed `raw1.bin`, `raw2.bin`, `raw3.bin`, `raw4.bin` through `charmbracelet/x/vt` (unsent's emulator version, built offline in `/tmp/kiro-verify`) and dumped frames; counted escape sequences byte by byte. Confirmed layout, 38;5;247 hint with SGR 7 cursor, SGR 2 `Press Tab to expand`, chip bytes, 213 sync pairs, 4 × `ESC[3J`, startup order, fullscreen mouse left on, offscreen edit invisible until resize, legacy prompt and unindented continuation.
- Read in `tui.js`: `xet()` thresholds, `pD()`, `ttt()` Enter handler, Ctrl+C handler `uCn`, Ctrl+D `dCn`, Ctrl+Z `armSuspend`, history class `yr`, `/editor` `promptEditor`, keyboard-protocol selection, sync emission, keybinding defaults, `KIRO_ASCII_MODE`. Read Q CLI `prompt.rs` at `15cc8f3`, CAO `kiro_cli.py` at `5471d8f`, the saved docs and changelog. Checked the DMG and `tui.js` hashes and that no prompt was submitted.

What I changed:
- **Ctrl+C:** the lane said it wipes the draft with no qualification. Source shows it clears only when idle; while processing it cancels the response and keeps the draft; the key is remappable (`chat.keybindings.quit`). Recipe step 10 updated.
- **Overflow bug:** the lane had one shape tied to long tokens. The captures show a second, milder shape (status row spills one column, draft intact) caused both by a 118-char token and by a 500-char inline paste of short words. The lane's guard would have rejected those readable frames; recipe steps 1-2 relaxed.
- **Keyboard protocol:** added that Kiro pushes kitty flags unconditionally on Ghostty, kitty, WezTerm and iTerm2 (by environment variable), so Ctrl/Alt delete keys arrive as CSI-u there. The lane said only "when the outer terminal supports them".
- **Cursor model:** stressed that `TMUX` inherited from the user's shell switches Kiro to the hardware cursor, the opposite of all captures.
- **Paste arithmetic:** added UTF-16 length and trailing-newline line count.
- **Sync output:** `KIRO_NO_SYNCHRONIZED` downgraded to "probably no effect on the renderer's pairs"; only `TWINKI_NO_SYNC` is shown by source to gate them.
- **Scrollback:** noted `ESC[3J` on resize and the `chat.preserveScrollback` setting; noted `chat.disableWrap`.
- Small fixes: chip background is `48;5;141`, not `38;5;141`; legacy `> ` colour is `38;5;93`; status row text varies (cwd only, agent only, agent + meter, per CAO also model); the iTerm2 `paddingLeft:1` I found is on a list panel, so downgraded to guess; `/editor` and Ctrl+G roles confirmed.
- Removed from the profile: the lane's first open question, which was about an unrelated relayed request, not Kiro.
- Kept as the lane's word only (no capture retained): Ctrl+C wipe measured when idle, Escape keeps the draft, Ctrl+J / Esc+Enter newline, the 501-char chip, `TWINKI_HARDWARE_CURSOR=1` visible cursor, 100-column overflow, the process tree.
