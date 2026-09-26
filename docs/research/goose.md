# goose CLI: input-box profile for unsent (verified)

Measured on **goose 1.52.0** (official `goose-aarch64-apple-darwin.tar.gz`, `/tmp/goose-prof/bin/goose --version` prints `1.52.0`; release published 2026-09-23T14:59Z). The repo is now `github.com/aaif-goose/goose`; `block/goose` redirects there.

Source read at main `04ed836` (2026-09-25, shallow clone in `/tmp/goose-prof/src`, which also carries tag `v1.52.0`). `git diff --stat v1.52.0 HEAD -- crates/goose-cli/src/session` touches only `output.rs` (7 lines, a tool-output filter), so the input path is identical to the release [source, re-checked]. `Cargo.lock` pins `rustyline 18.0.1` and `console 0.16.6` [source, re-checked]; rustyline 18.0.1 is unpacked in `/tmp/goose-prof/rl/`.

Tags:
- **[measured]**: seen on 1.52.0. Where the raw pty byte stream survives it is cited as `typescript` (session 1, 23,595 bytes) or `typescript2` (session 2, 857 bytes), both in `/tmp/goose-prof/`, captured with `script -q -F`. "(tmux only, capture not retained)" means only the lane's word backs it.
- **[source]**: read in goose at the cited path (under `crates/goose-cli/src/session/` unless stated), in rustyline 18.0.1, or in the other named crate.
- **[docs]**: project docs.
- **[guess]**: inference nobody verified.

Setup [measured]: `/tmp/goose-prof/run.sh` runs `goose session --no-profile` under `env -i` with `TERM=xterm-256color`, `HOME=/tmp/goose-prof/home`, `GOOSE_PATH_ROOT=/tmp/goose-prof/root`, `GOOSE_DISABLE_KEYRING=1`, `GOOSE_PROVIDER=openai`, `GOOSE_MODEL=gpt-4o`, `OPENAI_API_KEY=sk-fake-not-real`, `GOOSE_TELEMETRY_ENABLED=false`, and `EDITOR`/`VISUAL` = a copy-and-exit script. Private tmux server, 100x30, one resize to 60x20. Nothing was submitted: both captures contain exactly one `ESC[?2004h`/`ESC[?2004l` pair, closed by the exit path, `root/state` has no `history.txt`, `/tmp/goose-prof/home` is empty, and no `editor-copy.txt` exists (so `/edit` never ran). Session 1 ended with two Ctrl+C on an empty box, session 2 with Ctrl+D on an empty box.

---

## 1. TUI stack
- **rustyline 18 line editor, not a full-screen TUI.** `rustyline::Editor<GooseCompleter, DefaultHistory>` built in `mod.rs:645-660` with `CompletionType::Circular`; the prompt is `editor.readline("> ")` at `paste.rs:368` [source, re-checked].
- Styling comes from the `console` crate; `cliclack` draws tool approvals and config prompts during an agent turn, not at the draft prompt (`crates/goose-cli/Cargo.toml:35-36,51`) [source].
- The Ink/npm TUI (`@aaif/goose`) is deprecated and its source removed: `ui/text/` now holds only a README saying so [source, re-checked]. The desktop app is Electron. Only the rustyline CLI matters for unsent.
- Emacs mode by default; `EDIT_MODE=vi` switches to vi (`builder.rs:840-850`, `mod.rs:650-653`) [source, re-checked].

## 2. Screen mode
- **Main screen, no alternate screen.** 0 `ESC[?1049h` in either capture [measured, re-counted]; nothing in `goose-cli` emits it [source].
- The banner, earlier prompts and agent output stay in scrollback. unsent's recovery notice printed before goose starts stays visible [measured].
- **New:** at session start goose sets the terminal title with OSC 0: `ESC]0;🪿 <dir>BEL` (first bytes of `typescript`; `output.rs:1542-1553`) [measured + source]. It identifies goose but is not a prompt anchor.

## 3. What the box looks like
No box and no borders. Idle prompt, verbatim from `typescript` [measured]:
```
  \e[32m\e[2m╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\e[0m \e[2m0%\e[0m \e[2m0/128k\e[0m\r\n      <- context-usage bar
\e[?2004h\e[?2026h\r\e[K> \e[2mEnter to send · Ctrl+J newline\e[0m\r\e[2C\e[?2026l
```
(The lane's version dropped the leading two spaces and swapped the `32`/`2` order; corrected from the bytes.)

- **Prompt glyph.** ASCII `> ` (`0x3E 0x20`) at column 0, unstyled; `highlight_prompt` returns it unchanged (`completion.rs:545-551`) [measured + source].
- **Continuation rows.** No prefix, start at column 0. From `typescript`: `> do nothing; reply ok` / `second line of harmless draft` / `third xxx…` [measured].
- **Wrapping.** By character at the full terminal width, mid-word, no margin; row 0 holds `cols-2` draft characters. At 100 columns the 599-character paste `w000 … w119` took 7 rows with the cursor ending at column 1 (`\r\e[1C`), i.e. 601 cells = 6×100+1 [measured]. The 60-column case is tmux only (capture not retained). Wide graphemes that don't fit move to the next row (rustyline `tty/unix.rs:1110-1135`) [source]. When a row ends exactly at the last column, rustyline emits its own `\r\n` inside the refresh to place the cursor (`unix.rs:1075-1081`); seen after the 100-cell `third xxx…` row in `typescript` [source + measured].
- **Empty-box hint.** Shown only when the buffer is empty, dim (`highlight_hint` applies `console::Style::new().dim()`, `completion.rs:553-556`), written right after `> ` [measured + source]. Three strings (`completion.rs:512-541`) [source, re-checked]:
  - `Enter to send · Ctrl+J newline` (letter follows `GOOSE_CLI_NEWLINE_KEY`) [measured]
  - `Press Ctrl+C again to exit, or type new instructions to continue` [measured, `typescript` offsets 23066, 23444]
  - `Interrupted, what should goose work on instead?` (after Ctrl+C interrupts a turn) [source]
  Typing any character resets the status to Default (`completion.rs` `hint()`) [source]; seen in `typescript` (`x` typed after the MaybeExit hint, then cleared, shows the Default hint) [measured].
- **Row above the prompt.** Every loop iteration calls `display_context_usage()` right before `get_input` (`mod.rs:609-636`) [source, re-checked]. Format (`output.rs:1565-1613`) [source, re-checked]:
  - two spaces, a 20-cell bar of `━` (filled) / `╌` (empty), ` N%`, ` used/limit` with `k`/`M` suffixes (`0/128k`, `1.2M`); colour green+dim under 50 %, yellow under 85 %, red above.
  - context limit 0 → `  context usage unavailable (context limit is 0)`.
  - `GOOSE_CLI_SHOW_COST=true` → `Cost: $x.xxxx USD (N tokens: in …, out …)` via `eprintln!`, and only when price data exists for the model (`output.rs:1620-1642`, `mod.rs:1917-1923`) [source]. It lands between bar and prompt on the same pty.
  - `run_status_hook("waiting")` sits between the bar and `readline` but runs the hook with stdout/stderr null, so it prints nothing (`output.rs:275-300`) [source, new].
  - Nothing prints asynchronously while the prompt is open: the background extension loader only reports through `ensure_extensions_loaded`, which runs at the top of the loop and in `handle_input` (`mod.rs:1725-1750`) [source, new].
- **Below the draft.** Nothing; rustyline clears old rows on each full refresh [measured].
- **Completion.** `CompletionType::Circular` replaces text inline, no menu (`mod.rs:649`) [source].
- **Ctrl+R.** Prompt becomes `(reverse-i-search)`…': ` [source]. With empty history it returns early (rustyline `lib.rs:377`) [source, re-checked]; the lane's "does nothing" observation is tmux only (no bytes for it in the captures).

## 4. Cursor
- The terminal cursor sits at the insertion point while that point is on screen: `22,6`, `29,7`, `30,9` [measured, tmux only, capture not retained].
- **Draft taller than the screen, insertion point above the top:** rustyline positions the cursor with `ESC[nA` (`unix.rs:1084-1093`), which the terminal clamps at row 0. In `typescript`, after typing `Z` into line 2 of a 41-row draft, the refresh ends `\e[39A\r\e[7C`: 39 rows up from the bottom of a 30-row screen, clamped to row 0, column 7 [measured + source]. The screen keeps showing the bottom rows; the `Z` (`secondZ line …`) is written into scrollback, not onto the visible screen [measured].

## 5. Height and scrolling
- **No cap.** The draft grows to the full screen and then the terminal scrolls; the `> ` row and the bar leave the screen. The 41-row draft (40 logical lines, line 3 wraps) left `line 11 … line 40` visible in 30 rows [measured; consistent with the bytes, final screen tmux only].
- The box never scrolls to follow the cursor (section 4).
- Each full refresh of an overflowing draft rewrites it from its first row, pushing more copies into scrollback (`history_size` 72 → 83 after one keystroke [measured, tmux only]). Scrollback is useless for recovery.
- **New, load-bearing:** once a draft has reached the top of the screen, rustyline's own cursor-up moves are clamped for the rest of that prompt, so the `> ` row is redrawn at **screen row 0 with no context bar above it**. In `typescript`, Ctrl+C on the overflowing draft redraws `> Enter to send · Ctrl+J newline` at row 0; the three pastes and the kill-ring test that follow all draw from row 0 too [measured]. The bar and banner are only in scrollback. The lane noted the banner loss but not that it breaks the bar anchor (see recipe step 2).

## 6. Pastes
- **Unix: inline, no placeholder.** goose's paste-burst detector returns `console_pending_events() == 0` off Windows (`paste.rs:57-60`), so `capture_paste` never fires and rustyline's bracketed paste inserts the text (`Cmd::Insert`, `\r\n`/`\r` → `\n`, rustyline `keymap.rs:1093-1096`, `unix.rs:880-895`) [source, re-checked]. From the redraw bytes in `typescript` [measured]:
  - 3-line paste (`p3.txt`): `> paste one\r\npaste two\r\npaste three`, 3 rows starting on the `> ` row
  - 12-line paste (`p12.txt`): 12 rows
  - 599-character single line (`pl.txt`): 7 rows at 100 columns
- **Windows only:** bursts of ≥2 lines or ≥400 characters become `[Pasted N lines #id]` / `[Pasted N chars #id]`, expanded on submit by `expand_pastes` (`paste.rs:17-22,319-337,340-375`) [source]. Not reachable on unsent's Unix pty [source].
- unsent's placeholder expansion is not needed for goose; big pastes produce the overflow case instead [source].
- **Tabs.** A pasted `\t` is written raw; rustyline counts it to the next tab stop, so the reader sees blank cells (`unix.rs:1119-1121`) [source].

## 7. Keys

**Newline / submit:**
- **Ctrl+J** inserts a newline: goose binds `Ctrl+<get_newline_key()>` to `Cmd::Newline` (`input.rs:93-100,170-176`) [source, re-checked]; seen in `typescript` (`\r\n` then the next line echoed) [measured].
- `GOOSE_CLI_NEWLINE_KEY` changes the letter; `m` and `c` are rejected. If rebound, Ctrl+J falls back to rustyline's `E(K::Char('J'|'M'), M::CTRL) | E::ENTER => AcceptOrInsertLine { accept_in_the_middle: true }` (`keymap.rs:1059-1061`), and goose's `Validator` always returns `Valid` (`completion.rs:568-575`), so **Ctrl+J submits** [source, re-checked].
- **Esc+Enter / Alt+Enter**: unbound, ring BEL. `typescript2` shows `\x07` right after `do nothing; reply ok` was typed [measured].
- Enter / Ctrl+M submit through `PasteAwareEnterHandler` (`input.rs:150-167`) [source].
- Shift+Enter is indistinguishable from Enter without the kitty keyboard protocol, which rustyline does not use, so it submits [guess].

**Delete and restore keys** (Emacs mode; rustyline `keymap.rs:560-662,1030-1091`; goose `input.rs:55-85`):

| Key | Effect | Tag |
|---|---|---|
| Backspace (`^?`), Ctrl+H | delete char back | source |
| Delete (`ESC[3~`) | delete char forward | source |
| Ctrl+D | with text: delete char forward; **empty box: EOF, session ends** | measured (`typescript2`: `> o nothing; reply ok` after Ctrl+A, Ctrl+D; final `?2004l` then `session closed`) |
| Ctrl+W | kill back to whitespace | measured (`typescript2`: `\e[2D\e[K`, `\e[6D\e[K`, `\e[9D\e[K`) |
| Alt+Backspace / Esc Backspace | kill word back | source |
| Alt+D | kill word forward | source |
| Ctrl+U | kill to start of the current logical line | measured (`typescript`: `alpha beta ` removed as `\e[11D\e[K`) |
| Ctrl+K | kill to end of the logical line | measured (tmux only) |
| Ctrl+X Backspace | kill to line start | source |
| **Ctrl+C** (goose `CtrlCHandler`) | with text: `Cmd::Kill(WholeBuffer)`; empty: first `Repaint` with the MaybeExit hint, second `Cmd::Interrupt` → exit | source (`input.rs:50-86`) + measured (all inside one readline: no `?2004l` until the final exit) |
| Ctrl+_ / Ctrl+X Ctrl+U | undo | measured (tmux only; `typescript2` shows `o ` coming back after the Ctrl+W run, consistent) |
| Ctrl+Y / Alt+Y | yank / yank-pop; brings back Ctrl+C/W/U/K kills | measured (`typescript`: `do nothing; reply ok KILLRING` cleared by Ctrl+C, then redrawn whole) |
| Ctrl+T, Alt+T, Alt+U/L/C | transpose / change case | source |
| Up/Down | move one logical line; on the first/last line swap the buffer for a history entry, with the draft saved by `backup()` and restored by Down. No-op while history is empty (`edit.rs:702-716`) | source, re-checked |

Vi mode (`EDIT_MODE=vi`) brings the full vi command set (`x`, `dd`, `D`, `C`, `s`, `cw`, …) [source; not measured].

## 8. How updates reach the screen
- Full refreshes are wrapped in `ESC[?2026h … ESC[?2026l` (rustyline `unix.rs:39-40`): 61 balanced pairs in `typescript`, 7 in `typescript2` [measured, re-counted].
- **Many edits bypass the full refresh** [measured, bytes in both captures]: characters typed at the end of a line are echoed raw (`o nothing; reply ok`), Ctrl+W/Ctrl+U arrive as `\e[nD\e[K`, and cursor moves as bare `\e[A`/`\e[D`. The first character typed into an empty box goes through a full refresh because the hint must be erased. So "never read inside a pair" holds, but many states arrive with no pair.
- **Prompt open = between `ESC[?2004h` and `ESC[?2004l`** (rustyline `unix.rs:37-38`, toggled on entering/leaving raw mode for each `readline()`) [source]. Measured: one pair per session, each session held a single prompt, and in-prompt state changes (Ctrl+C clear, MaybeExit hint, Ctrl+Y) never emitted `?2004l` [measured]. The per-prompt toggle across a submitted turn is source only, since nothing was submitted.
- While the agent runs there is no editor; the tty is back in cooked mode, so type-ahead is probably echoed by the tty and read at the next prompt [guess].
- **Version note (new):** rustyline 15.0.0, used by goose ≤ v1.34.x (`Cargo.toml` at `v1.34.0`: `rustyline = "15.0.0"`), has the bracketed-paste constants but **no `?2026` synchronized-output marks** (`rustyline-15.0.0/src/tty/unix.rs`) [source]. The `?2026` rule only applies from v1.35.0.

## 9. Built-in persistence and recovery
- **Nothing on disk for an unsent draft.**
  - History file: `Paths::state_dir()/history.txt`, with `Paths::config_dir()/history.txt` as a legacy file read if the new one is missing and deleted after the first save (`mod.rs:188-232`) [source, re-checked].
  - Location: `$GOOSE_PATH_ROOT/state/history.txt` when `GOOSE_PATH_ROOT` is an absolute path; otherwise goose uses `etcetera::choose_app_strategy`, which is the **XDG strategy on macOS too** (etcetera 0.11.0 `app_strategy.rs:157-163`: `create_strategies!(Apple, Xdg)` = native Apple, app Xdg). So `$XDG_STATE_HOME/goose/history.txt`, default `~/.local/state/goose/history.txt`; legacy `~/.config/goose/history.txt` (`crates/goose/src/config/paths.rs:8-58`) [source; upgraded from guess]. The comment in `paths.rs` mentioning `~/Library/Application Support/Block/goose/` does not describe the macOS CLI path.
  - Entries are added only for submitted non-blank lines (`input.rs` after `readline` returns) [source]. With a draft typed and goose quit, no history file appeared under `root/state` [measured, re-checked: only `state/logs/`].
- **In-process recovery:** Ctrl+C with text sends the whole buffer to the kill ring and Ctrl+Y restores it [measured, `typescript` offsets 22745-22911]. It lasts until the next kill or process exit.
- **goose's own keys cannot exit while text is in the box**: Ctrl+C clears first, Ctrl+D deletes forward [source + measured]. A draft is lost by Ctrl+C followed by further kills, a closed terminal / SIGHUP / crash, an accidental Enter (or Ctrl+J after a rebind), and temporarily by Up-arrow history browsing (Down restores) [source].
- Sessions live in `data/sessions/sessions.db` (SQLite, WAL), created at startup, submitted messages only [measured: `root/data/sessions/sessions.db`, `-wal`, `-shm` present; contents not inspected].

## 10. External-editor escape
- **No keybinding** (rustyline has no Ctrl+X Ctrl+E; goose adds none) [source].
- **`/edit [text]`** slash command (`input.rs:236-237`): opens `GOOSE_PROMPT_EDITOR` → `$VISUAL` → `$EDITOR` (`editor.rs:13-38`; falls back to `vi` where a default is used) on a temp file prefixed `goose_prompt_` (`editor.rs:116-119`) holding a template with the prefill and earlier turns as `## User:` / `## Assistant:`; meaningful content on close is **submitted** [source]. Not measured (an unmodified prefill would submit).
- It cannot capture the current in-box draft; `/edit` itself must be typed and submitted, replacing the box [source].
- With `goose_prompt_editor` configured, every prompt goes to the editor and no inline box appears, unless `goose_prompt_editor_always=false`; empty editor content falls through to the inline prompt (`input.rs:103-139`) [source, re-checked].

## 11. Detection recipe for a `gooseBox` reader
1. **Is a prompt open?** Track `ESC[?2004h` (open) and `ESC[?2004l` (closed) in goose's output. Read only while open, and never inside an open `?2026` pair (v1.35+). While closed (agent turn, `/edit`, cliclack approval), keep the last draft.
2. **Find the prompt row.**
   - Normal case: the lowest row at or above the cursor row that starts with `> ` at column 0 **and** has, directly above it, a row matching `^  [━╌]{20} \d{1,3}% \S+/\S+$` or `^  context usage unavailable \(context limit is 0\)$` (optionally with a `^Cost: \$` row between). The bar pairing skips old submitted prompts, agent blockquotes, and draft lines that themselves begin with `> `.
   - **Pinned case (new):** if no bar pair is found, a prompt is open, and screen row 0 starts with `> `, treat row 0 as the prompt row. This is the state after any draft of this prompt reached the top of the screen (section 5). Risk: a draft line beginning with `> ` scrolled to row 0 of an overflowing draft looks identical; prefer the overflow rule (step 6) when the screen is full to the bottom.
3. **Empty box:** after `> `, only SGR 2 cells or blanks. Also match the three hint strings exactly as text, since `console` drops SGR 2 when colours are off (see section 13).
4. **Draft rows:** from the prompt row (drop 2 cells) down to the last non-blank row; rustyline clears everything below.
5. **Joining rows:** a row that fills the full width (row 0 counts the 2 prompt cells) is a soft wrap, join with the next; a shorter row ends a logical line (`\n`). Ambiguities: a logical line of exactly `cols` cells, trailing spaces (unless the shadow screen distinguishes written spaces), and one blank cell left when a wide character wraps.
6. **Overflow:** prompt open, no prompt row found by step 2, screen filled to the bottom → the visible rows are the draft's tail with the top unknown; stitch against the last known text. Do not trust the cursor on row 0.
7. **Keys for `deleteKeys`:** Backspace/^H, Delete, Ctrl+D (with text), Ctrl+W, Alt/Esc+Backspace, Alt+D, Ctrl+U, Ctrl+K, Ctrl+X Backspace, Ctrl+C (whole buffer). Restore keys: Ctrl+Y, Alt+Y, Ctrl+_. Up/Down at the first/last line swap the buffer for a history entry (not a deletion). Pastes are inline on Unix; nothing to expand.

## 12. Stability across versions
Prompt history, checked against GitHub (`aaif-goose/goose`) [source, re-checked]:

| Versions | Prompt | Evidence |
|---|---|---|
| ≤ v1.24.x | styled `( O)> ` | `input.rs` at `v1.24.0`: "goose face "( O)>" followed by a space" |
| v1.25.0 – v1.34.x | `🪿 ` (VTE plain fallback from `7d6e766027`, #8385, 2026-04-09) | `input.rs` at `v1.25.0` / `v1.34.0`; `85348d2745` (#7138, 2026-02-13) |
| **v1.35.0 (released 2026-05-22) onward** | **`> `** | `f41d45ff3a` (#9305, 2026-05-19); compare API: `v1.35.0` is ahead of it, `v1.34.0` diverged |

- rustyline 15 → 18 in `651a2022fc` (#8961, 2026-05-12), contained in v1.35.0 [source]. `v1.34.0` still pins `rustyline = "15.0.0"` [source, re-checked].
- Ctrl+C clear-then-exit behaviour from `0f334dbd45` (#6900, 2026-02-03) [source].
- The input files change every 1-3 weeks, mostly for slash commands [source, lane's reading of git history; not re-checked].
- Releases are weekly: 1.48.0 (08-27), 1.49.0 (09-03), 1.50.0 (09-08), 1.50.1 (09-14), 1.51.0 (09-17), 1.52.0 (09-23) [source, re-checked via releases API].
- Since v1.35 the draft area is plain text on the main screen: easier to read than Claude Code's, but with no border and no end marker.

## 13. What would break the reader
- The prompt glyph changing again (it changed twice in 2026) [source].
- The context-bar format changing or disappearing (it is the main anchor) [source].
- A move away from rustyline (reedline, ratatui, a revived Ink TUI) or onto the alternate screen [guess].
- **Colours off** [source; upgraded from guess]: `console 0.16.6` disables styling when `NO_COLOR` is set to any value, `CLICOLOR=0`, `TERM` is unset or `dumb`, or stdout is not a terminal; `CLICOLOR_FORCE≠0` re-enables it (`utils.rs:15-19`, `unix_term.rs:25-35`, `term.rs:572-590`). goose never calls `set_colors_enabled` or `force_styling` [source]. With colours off the hint is plain text and only exact string matching separates it from a draft.
- `EDIT_MODE=vi`: no mode indicator in the prompt, vi edit commands [source].
- A rebound `GOOSE_CLI_NEWLINE_KEY`: hint text changes and Ctrl+J submits [source].
- `/edit` or `goose_prompt_editor` mode: no inline box [source].
- Ctrl+R search replaces the prompt text [source].
- Pre-v1.35 builds: different glyph and no `?2026` marks; a reader needs a version gate or a glyph set [source].
- The pinned-at-row-0 state (section 5) removes the bar anchor for the rest of that prompt [measured].
- Agent output reproducing a bar line followed by `> ` [guess, unlikely; only matters if `?2004h` tracking is lost].

## Risks
- No top marker once the draft is taller than the screen; the cursor stays on row 0 while edits above the screen never appear. The reader must stitch from the visible tail [measured].
- After an overflow, the prompt row stays pinned at row 0 without a bar, even when the draft is later cleared or shrinks; a bar-only anchor misses the live prompt [measured].
- The glyph changed twice in 2026 and releases ship weekly; re-measure on prompt changes and fail safe [source].
- Character wrapping at the exact width makes a logical line of exactly `cols` cells look like a soft wrap; trailing spaces and tab/wide-char blank cells are ambiguous [source].
- Many edits arrive outside `?2026` pairs [measured].
- A rebound newline key turns Ctrl+J into submit [source].
- Up/Down at the first/last line show a history entry in place of the draft until Down; keepOld protects the real draft but the saved text is wrong in between [source].
- `goose_prompt_editor` removes the inline box entirely [source].

## Open questions
- Keys typed during an agent turn (cooked tty) were not measured; needs a submitted turn [guess].
- `/edit` behaviour is source only.
- Vi mode and a rebound `GOOSE_CLI_NEWLINE_KEY` were not measured.
- The history file's default macOS path is now source-backed (`~/.local/state/goose/history.txt`) but was not confirmed on disk.
- Whether rustyline redraws on SIGWINCH during an overflowing draft (the resize in session 1 left no refresh in the byte stream) was not examined.

## Verification notes
What I checked:
- Re-counted escape sequences in both surviving captures (`typescript`, `typescript2`): 0 `?1049h`; one `?2004h`/`?2004l` pair per session; 61 and 7 balanced `?2026` pairs; hint strings, SGR 2, BEL. Decoded the key sequences from the redraw bytes to confirm Ctrl+J, Esc+Enter (BEL), Ctrl+D with text, Ctrl+W, Ctrl+U, Ctrl+C clear, the MaybeExit hint and exit, Ctrl+Y restore, the 3/12-line and 599-char pastes, and the overflow cursor (`\e[39A\r\e[7C` = row 0, col 7).
- Confirmed `goose --version` = 1.52.0, the v1.52.0 → main diff (session dir: `output.rs` only), and the `rustyline`/`console` pins.
- Read in source: `mod.rs` prompt loop and editor config, `output.rs` context bar, cost line and status hook, `input.rs` Ctrl+C handler, newline key and editor-always logic, `completion.rs` hints/highlighter/validator, `paste.rs` Unix path, `editor.rs` editor order, `paths.rs`; rustyline 18.0.1 `keymap.rs:1059`, `edit.rs:702-716`, `unix.rs:37-40,1084-1093`, `lib.rs:372-380`.
- Fetched rustyline 15.0.0, etcetera 0.11.0 and console 0.16.6 from crates.io into `/tmp/goose-prof/{rl15,etc,con}` and read them.
- Checked the cited commits, tag containment and release dates through the GitHub API.

What I changed:
- Upgraded to [source]: default history path (etcetera uses XDG on macOS), colour-off behaviour (`console` rules), and rustyline 15 lacking `?2026` (closes a lane open question).
- Added: the pinned-at-row-0 state after overflow, which removes the bar anchor, plus a matching recipe branch and risk; the OSC 0 `🪿 <dir>` title; the silent status hook; the absence of async prints during the prompt.
- Corrected the idle-prompt byte sample (two leading spaces, SGR order) and the "one `?2004h` per prompt" claim, which was measured only for a single prompt per session; the across-turns toggle is source only.
- Marked tmux-only observations (cursor coordinates, 60-column wrap, `history_size` growth, Ctrl+K, Ctrl+R, undo) as "capture not retained".
- Dropped the lane's first open question (a relayed request about editing memory/CLAUDE.md), which is unrelated to this lane; nothing was acted on.
- Everything else held as stated.
