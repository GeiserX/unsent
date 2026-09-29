# OpenAI Codex CLI 0.151.0: input-box profile for unsent (verified)

Tags on every claim:
- **[measured]** seen in the lane's private tmux run (120x40 unless noted), backed by a capture or raw log in `/tmp/unsent-spec-codex/` where one survives;
- **[source]** openai/codex at tag `rust-v0.151.0` (commit `78c290807ce710180111df227df3b7a4fe845452`), paths relative to `codex-rs/tui/src/`;
- **[docs]** protocol spec (kitty keyboard protocol, xterm);
- **[guess]** inference nobody checked.

## Summary

Sections 1 to 9 describe 0.151.0. Section 10 is a capture run on 0.158.0 and corrects them where that version differs: it draws on the alternate screen, has a two-row footer, quits on one Ctrl+C, runs a shared background server by default, and holds a lock file that names the running thread [measured].

Codex draws its main screen **inline** (not on the alternate screen); the alternate screen is used only for overlays such as the Ctrl+T transcript [measured]. The box has no border lines: 1 padding row, a bold `›` at column 0 of the first visible text row, continuation rows indented 2 columns, 1 padding row, then the footer [measured, source]. An empty box shows a dim hint [measured]. Rows wrap at `cols - 3` [measured, source]; a logical line that exactly fills that width gets an extra empty continuation row the reader must drop [measured, source]. The box grows to `rows - 4` text rows and then scrolls one row at a time to keep the cursor visible [measured, source]. Pastes over 1000 characters become `[Pasted Content N chars]` [measured, source]. Ctrl+C on a draft moves it into `$CODEX_HOME/history.jsonl`; killing Codex loses the draft [measured]. Codex pushes the kitty keyboard protocol, so in modern terminals the delete keys unsent counts arrive as CSI-u sequences, not the legacy bytes `wrap.go` matches today [measured push, docs encoding; not measured end to end].

## How it was measured (lane's setup, re-checked)

- Binary `codex-cli 0.151.0`, Mach-O arm64 [measured].
- Private tmux server `tmux -L unsent-spec-codex -f /dev/null`, 120x40, empty cwd `/tmp/unsent-spec-codex/work`, `CODEX_HOME=/tmp/unsent-spec-codex/home`, dummy API key typed into the sign-in screen, `EDITOR` = a copy script [measured].
- Nothing was submitted to a model: the temp home has no `sessions/` directory [re-checked in verification]. Every draft was cleared with Ctrl+C; the 18 entries in `home/history.jsonl` are those clears [re-checked].
- Raw output bytes were recorded with `script` into `raw-run{1,2,3}.log` [re-checked: files present].
- A fresh CODEX_HOME shows a sign-in screen, then a directory-trust prompt, before the box [measured].

## 1. What the box looks like

### Empty box [measured, `cap-empty.txt`, SGR kept]
```
^[[1m›^[[0m ^[[2mAsk Codex to do anything^[[0m
                                                   <- bottom padding row
  ^[[38;2;246;226;183mgpt-5.6-sol default^[[2m^[[39m · ^[[0m^[[38;2;171;223;167m/private/tmp/unsent-spec-codex/work^[[39m
```
Above the `›` row: one blank top-padding row, then one blank spacer row, then history. History at startup contains dim rounded-corner cards (`╭─…╮`, the session header and an "Update available" notice) [measured, `cap-empty.txt`]. A reader must not treat those `─` rules as a box border.

### Layout
- The composer rect is: 1 top padding row, text rows inset 2 columns left (`LIVE_PREFIX_COLS = 2`, `ui_consts.rs:10`) and 1 column right, 1 bottom padding row; footer or popup below [source `bottom_pane/chat_composer.rs` `layout_areas_with_textarea_right_reserve`, ~line 1020].
- **Remote image rows** (`[Image #N]` rehydrated from backtrack or the app server) sit *inside* the composer rect above the text rows, followed by a blank separator row [source `chat_composer.rs:1027-1046`, module doc line 131]. Not measured. The `›` then sits below them, not directly under the top padding.
- **Right reserve**: when an ambient "pet" image is shown, the text area loses `image_columns + gap` more columns on the right, so the wrap width shrinks [source `chatwidget/rendering.rs:21,86`, `chatwidget/pets.rs:102`]. Not measured; off unless a pet image is enabled [guess].

### Prompt glyph at column 0 of the first *visible* text row [source `chat_composer.rs:4867-4890`]
| State | Glyph and style |
| --- | --- |
| normal | `›` (U+203A) **bold**, default fg [measured `ESC[1m›`] |
| shell mode | `!` bold light red [measured `ESC[1mESC[38;5;9m!`]; footer right shows `Shell mode` [measured] |
| effort tier Max / Ultra | `›` / `»` (U+00BB), bold, truecolour [source `bottom_pane/effort_ignition.rs:105-110,147`] |
| input disabled | `›` **dim**, not bold [source] |
| 0.157.1 "luna reserve" | `›` bold gold [source, `/tmp/unsent-spec-codex/cc157.rs:4928-4932`] |

- The glyph is followed by a plain space, not U+00A0 [measured].
- The `!` is not in the textarea; `current_text()` adds it back [source].
- **Submitted user messages in history** also start with `› `, but styled **bold + dim**, with a tinted blank row above and below [source `history_cell/messages.rs:249-270`]. The lane listed this as a guess; the source settles it. The composer glyph is bold and *not* dim, so dim is the discriminator; "pick the lowest row" is a second guard. Not measured (nothing was submitted).
- Selection lists (trust prompt, approvals) use a cyan, non-bold `›` [measured `ESC[38;5;6m› 1. Yes, continue`].

### Hint text
Dim (SGR 2) right after `› ` [measured]. Possible texts [source]: `Ask Codex to do anything` (`chatwidget.rs:2009`), `Ask a follow-up question` (`chatwidget.rs:2010`), plus disabled-input hints (lane cites `Viewing sub-agent — direct input is disabled` and `Input disabled.`; not re-checked). An empty shell-mode box shows no hint [measured]. Detect an empty box by the dim attribute, not the words.

### Background tint
- The whole composer rect (both padding rows, full width) is filled with `user_message_style()` [source `chat_composer.rs:4860-4861`].
- That style only has a background when Codex learned the terminal background via `OSC 11;?` [source reference by the lane; the OSC 10/11 queries are measured in all three raw logs].
- Lane measured `ESC[48;2;57;57;71m` on the rect with the pane bg set to `#1e1e2e`, footer untinted (`ESC[49m`) [measured, capture not retained in the scratch dir]. With no OSC 11 reply (default detached tmux) there is no tint [measured].
- Under unsent, the real terminal's reply reaches Codex only if unsent forwards it on stdin [guess].

### Multi-line draft (Ctrl+J) [measured]
```
^[[1m›^[[0m do nothing; reply ok
  second line
  third line
                                                   <- padding
  gpt-5.6-sol default · ...
```

### Popups [measured]
- `/` opens the command list below the bottom padding, replacing the footer (`  /model         choose what model …`).
- `@` opens a file/skill list, selected row `> ` in cyan.
- `?` on an empty box shows shortcut help: `ctrl + j for newline`, `tab to submit message`, `ctrl + g to edit in external editor`, `esc esc to edit previous message`, `ctrl + r search history`, `ctrl + c to exit`.

## 2. Screen mode

- Main chat is inline: tmux `#{alternate_on}` = 0 the whole session [measured]. `raw-run2.log` and `raw-run3.log` contain zero `ESC[?1049h` [re-checked].
- Ctrl+T (transcript) enters the alternate screen and `q` returns with the draft intact [measured; `raw-run1.log` has exactly one `?1049h`/`?1049l` pair, re-checked].
- Overlays that use the alternate screen: transcript, diff, Esc-Esc backtrack, resume picker, model migration [source, lane's citations `tui.rs:831 enter_alt_screen` (re-checked), others not re-checked]. `--no-alt-screen` or `tui.alternate_screen = "never"` disables them [source `lib.rs:1795-1808`, re-checked].
- The first frame is `ESC[?2026h ESC[1;1H ESC[J …` [re-checked in all three logs]. In tmux the cursor was already at 1;1, so whether Codex clears text unsent printed before it (the recovery notice) in a normal terminal is unknown [guess]. Codex sends `ESC[6n` at startup and places its inline viewport from the reply [measured query; placement is a guess].
- As the box grows, rows above are pushed into scrollback (tmux `history_size` 1 → 38) [measured]. History is scrolled with scroll regions (`ESC[1;7r`, `ESC[1;40r`; 4-5 per log) [measured, re-checked counts].
- History re-wraps on resize [measured].

## 3. Rows, wrapping, height, cursor

- **Wrap width W = cols − 3** (minus any right reserve): 117 at 120 cols, 57 at 60 cols [measured]; `COLS_WITH_MARGIN = LIVE_PREFIX_COLS + 1` [source `chat_composer.rs:4561-4563`, re-checked].
- Word wrap via `textwrap 0.16.2` [source `Cargo.toml:460`]. A word longer than W breaks at exactly W [measured].
- **Spaces at a wrap point are not on screen** [measured: `a*110 + " sierra   tango x"` lost all three spaces from view]. Source agrees: "At soft word breaks, interior separators hang off the preceding row" [source `bottom_pane/textarea.rs:13-15`]. The count of spaces at a wrap is not recoverable; assume one.
- **A logical line that exactly fills W gets an empty continuation row** [measured: `q*117 + "\n\nnext"` drew `qqq…`, empty, empty, `  next`; Ctrl+G copy is exactly `q…q\n\nnext`, 123 bytes, re-checked with `od`]. Source: "full logical lines receive a continuation row so their insertion point stays visible" [source `textarea.rs:13, ~1988`]. 116 `w`s gave no extra row [measured]. **The reader must drop one empty row after a row of display width W.**
- **Max height = rows − 4 text rows** with nothing else in the bottom pane [measured: 36 at 40 rows (`cap-p45.txt`, re-checked: `›` on row 3, text through row 38, padding row 39, footer row 40); 20 at 24 rows]. A running turn adds a status line and queued-message preview and lowers the cap [source reference by lane, not measured].
- **Scrolling** is one row at a time, just enough to keep the cursor visible [measured; source `textarea.rs` `effective_scroll`: "Cursor is always on screen. No scrolling if content fits"]. `›` marks the first *visible* row (`› tall row 10` with rows 01-09 out of sight) [measured, `cap-p45.txt`].
- Once maxed, the inline area fills the screen and stays there; after a clear the box redraws at the top [measured].
- **The real cursor is at the insertion point** (e.g. `12,20` at the end of "third line") [measured]. The lane also saw the cursor at column 118 of 120 while stepping through hidden wrap-point spaces [measured]; its reading ("after a full-width row") conflicts with the source rule that a full line moves the insertion point to a continuation row, so treat the exact column as unexplained and clamp `curX` to the text area [verification note].
- **Synchronized output**: every frame is in `ESC[?2026h … ESC[?2026l`, always paired: 111, 175, 30 pairs [measured, re-checked].
- **Startup sequences**, in order [measured, re-checked byte-for-byte at the start of all three logs]: `ESC[?2004h` (bracketed paste), `ESC[>4;0m` (modifyOtherKeys off), `ESC[>5u` (kitty push), `ESC[?1004h` (focus), `ESC[6n`, `OSC 10;?`, `OSC 11;?`, `ESC[?u`, `ESC[c`. The kitty flags pop is `ESC[<1u` [measured]; the push repeats after an external editor returns [measured: 2 pushes in runs 1 and 2].
- Typing works ~200 ms after launch through a provisional composer (`early one` / `early two` landed correctly and are in `history.jsonl`) [measured, re-checked]; source: `startup_draft.rs` "Display an editable, non-submitting composer while startup work continues" [source].

## 4. Pastes

- **Threshold 1000 characters**: `LARGE_PASTE_CHAR_THRESHOLD: usize = 1000`, test `char_count > 1000` [source `chat_composer.rs:341,1191`, re-checked]. 3-line, 12-line and 1000-char pastes stayed inline; 1001 chars became a placeholder [measured, paste files `p3/p12/p1000/p1001.txt` present]. Line count does not matter [measured].
- **Format** `[Pasted Content N chars]` in cyan (`ESC[38;5;6m`); a second pending paste of the same size gets ` #2`, then `#3`; once all of a size are gone the base label is reused [measured for #2; source `chat_composer.rs:124-128,1913`]:
```
^[[1m›^[[0m before ^[[38;5;6m[Pasted Content 1199 chars]^[[39m after^[[38;5;6m[Pasted Content 1199 chars] #2^[[39m
```
- N counts characters after CRLF→LF and sanitizing [source, lane's reading of `handle_paste`; not re-checked].
- One Backspace deletes the whole placeholder [measured]. unsent must count it as a many-character delete.
- A pasted image path becomes `[Image #N]` [source, not re-checked]. Non-bracketed fast input is treated as a paste burst (Enter becomes newline) [source `paste_burst.rs`, not measured].
- Format and threshold unchanged at `rust-v0.30.0`, `rust-v0.46.0` and 0.157.1 [source, re-checked].

## 5. Keys

Defaults [source `keymap.rs:1414-1485`, re-checked]:

| Action | Default keys |
| --- | --- |
| submit | Enter |
| queue (submits when idle) | Tab |
| newline | Ctrl+J [measured], Alt+Enter `ESC CR` [measured], Shift+Enter, Ctrl+M (both need enhanced keys to differ from Enter) |
| delete back 1 | Backspace (0x7f), Shift+Backspace, Ctrl+H (0x08) |
| delete back word | Alt+Backspace (`ESC 0x7f`), Ctrl+Backspace, Ctrl+Shift+Backspace, Ctrl+W (0x17), Ctrl+Alt+H |
| kill to line start | Ctrl+U (0x15) |
| delete forward 1 | Delete (`ESC[3~`), Shift+Delete, Ctrl+D (0x04; on an empty box it quits [lane, source]) |
| delete forward word | Alt+Delete, Ctrl+Delete, Ctrl+Shift+Delete, Alt+D (`ESC d`) |
| kill to line end | Ctrl+K (0x0b) |
| kill whole line | unbound |
| yank | Ctrl+Y (restores the last killed span only) [source `textarea.rs` module doc] |
| whole draft | Ctrl+C clears it [measured] and appends it to history (section 6) |

- The editor keymap's newline list also contains plain Enter; the composer's submit binding wins [source; behaviour measured: Enter was never used, drafts were cleared, so not measured].
- **No undo binding** [source: no `undo` in `keymap.rs` or `bottom_pane/textarea.rs`, re-checked]. unsent's `0x1f` (Ctrl+_) entry in `manyKeys`/`aheadKeys` (`wrap.go:508-509`) is Claude-only.
- **Vim mode** (`/vim`, off by default) adds `x`, `d{motion}`, `D`, `c…`, `s`, `.` deletes [source `keymap.rs` `vim_normal`]. unsent's key log cannot see these.
- **Every binding can be remapped** under `[tui.keymap]` in `config.toml` [source `keymap.rs:619+`, resolve calls at 683-720 re-checked].

### Keyboard protocol (corrected)
- Codex writes `ESC[>4;0m` (modifyOtherKeys off) then pushes kitty flags [source `tui/keyboard_modes.rs:120-170`, re-checked].
- **The flags depend on the terminal** [source `keyboard_modes.rs:152-169`]: base = `DISAMBIGUATE_ESCAPE_CODES | REPORT_ALTERNATE_KEYS` (= 5). On Ghostty, iTerm2, or tmux with `extended-keys-format xterm`, that is all (`ESC[>5u`, what the lane measured in tmux). **Everywhere else** (kitty, WezTerm, …) Codex adds `REPORT_EVENT_TYPES` → `ESC[>7u`. The lane reported `>5u` as universal; that was only its tmux case.
- Inside tmux with `extended-keys-format csi-u`, Codex also enables modifyOtherKeys mode 2 [source `keyboard_modes.rs:180-190`].
- `CODEX_TUI_DISABLE_KEYBOARD_ENHANCEMENT=1` turns the push off entirely [source `keyboard_modes.rs:18-39`]. unsent could set it to keep legacy bytes, at the cost of Shift+Enter and Ctrl+M newlines for the user [guess on the trade-off].
- Encodings under disambiguate [docs, kitty protocol]: Ctrl+W `ESC[119;5u`, Ctrl+U `ESC[117;5u`, Ctrl+K `ESC[107;5u`, Ctrl+H `ESC[104;5u`, Ctrl+D `ESC[100;5u`, Alt+D `ESC[100;3u`, Ctrl+Backspace `ESC[127;5u`, Alt+Backspace `ESC[127;3u`; functional keys keep tilde form with modifiers (Ctrl+Delete `ESC[3;5~`, Alt+Delete `ESC[3;3~`). Plain Backspace (0x7f) and plain Delete stay legacy.
- With `REPORT_EVENT_TYPES` (flag 2) the terminal also sends **repeat** (`…:2u`) and **release** (`…:3u`) events [docs]. unsent must count a delete once per press/repeat and never on release.
- None of this was measured through unsent: tmux did not negotiate the protocol with the outer terminal [measured by lane]. `wrap.go` `deleteKeys` currently matches only `0x7f 0x08 ESC[3~ 0x17 0x15 0x0b 0x1f` [source, unsent `wrap.go:506-509`, re-checked], so under Codex in kitty/Ghostty/WezTerm/iTerm2 it would miss Ctrl+W, Ctrl+U, Ctrl+K and Ctrl+H.

### External editor (ground truth)
- Ctrl+G opens `$VISUAL`/`$EDITOR` on `<dir>/editor/.tmpXXXXXX.md` [measured: `editor-args.txt` = `/private/tmp/unsent-spec-codex/home/editor/.tmp4asQ17.md`].
- Contents are exactly the draft with **no trailing newline** [measured, `editor-copy.txt` re-checked with `od`: 123 bytes, ends `next`].
- `<dir>` is the first of CODEX_HOME, `~/.codex`, `<cwd>/.codex` that the sandbox policy does **not** make writable [source `external_editor.rs:60-192`, re-checked candidate order and "editor directory must not be writable"]. In tests pass `-c sandbox_mode="danger-full-access"` so CODEX_HOME is used; otherwise it falls back to the real `~/.codex/editor` [measured side effect, see section 9].

## 6. Draft persistence today

- **Ctrl+C on a non-empty box** clears it and appends `{"session_id","ts","text"}` to `$CODEX_HOME/history.jsonl` [measured: 18 entries, including `"x\n\n"` and `"!echo do-nothing"`, re-checked]. Source: `bottom_pane/mod.rs:893 clear_composer_for_ctrl_c` → `AppEvent::AppendMessageHistoryEntry` **only if a thread id exists**; otherwise it logs a warning and the text is not saved [source, re-checked; the lane omitted this condition].
- Pasted content is stored only as placeholder text (`"before [Pasted Content 1199 chars] after[Pasted Content 1199 chars] #2"`) [measured, re-checked], so the pasted text itself is lost after the session.
- Up / Ctrl+R recall history [measured help, source]. Up/Down replace a non-empty draft only when it equals the last recalled entry and the cursor is at the start or end [source `chat_composer_history.rs:374-400`, re-checked].
- **Killing Codex loses the draft** [measured: `tmux kill-server` with `tinted draft\nrow two` in the box; re-checked that the string appears in no file under the temp CODEX_HOME, including the `*.sqlite` and WAL files]. This is the gap unsent fills.
- Two Ctrl+C presses on an empty box quit Codex [measured, by accident].
- The temp home has `queue_1.sqlite` with `queued_items`; queued (Tab) messages may persist on disk [guess, not checked].
- Whether `history.jsonl` writing can be turned off by config was not checked [open].

## 7. TUI stack [source, `codex-rs/Cargo.toml`, re-checked]
- Rust; `ratatui 0.30.2`; `crossterm 0.29.0` patched to `openai-oss-forks/crossterm@45fecb95`; `textwrap 0.16.2`.
- Custom inline viewport (`custom_terminal.rs`, `insert_history.rs`) that inserts history with scroll regions [source per lane; scroll regions measured].

## 8. Stability across versions [source]

| Tag | Box | Paste placeholder, threshold | Tint |
| --- | --- | --- | --- |
| `rust-v0.30.0` | **left border** block (`Borders::LEFT`, `BorderType::QuadrantOutside`), no `›` [re-checked; lane said "none found"] | `[Pasted Content {n} chars]`, 1000 [re-checked] | none |
| `rust-v0.46.0` | bold `›` [re-checked] | same [re-checked] | `user_message_style` [re-checked] |
| 0.80.0 – 0.140.0 | bold `›`, dim `›` when disabled | same | yes [lane, not re-checked] |
| `rust-v0.157.1` | same `›` code plus gold "luna reserve" `›` [re-checked `cc157.rs:4928`] | same, threshold 1000 [re-checked `cc157.rs:440`] | yes |

0.157.1 also adds composer mouse selection and a `warning_area` above the box [lane, not re-checked]. The installed 0.151.0 prompts "Update available! 0.151.0 -> 0.157.1" [measured, `cap-empty.txt`]. Churn is in colour and decoration, not structure, back to 0.46.

## Reader recipe: `codexBox(s)` for 0.151.x

1. **Anchor.** Scan rows bottom-up for the lowest row y whose column 0 is `›`, `»` or `!`, **bold and not dim**, not cyan (38;5;6), and whose column 1 is a space or end of row. Bold+dim `›` is a submitted message in history; dim-only `›` is a disabled composer (return ok=false). `!` means shell mode: prepend `!` to line 0.
2. **Above.** Row y−1 must be blank (top padding, or the separator under remote image rows).
3. **Bottom edge, tinted.** If row y−1 has a non-default background B at column 0 and the last column, the composer is the contiguous run of rows with background B; its last row is the bottom padding; text rows are y .. last−1.
4. **Bottom edge, untinted.** Let F be the last non-blank row. Readable only if F−1 is blank and F has styled text (the footer); text rows are y..F−2. If the rows after the padding look like a popup (start `  /` or `> `, or more than one styled row), return ok=false and keep the last draft.
5. **Read.** Each row from column 2, right-trimmed. Row y dim at column 2 = empty box (match dim, not words). A draft of blank lines has no dim text.
6. **Un-wrap** at W = cols − 3 (a right reserve for a pet image makes W smaller; if any row is wider than cols − 3, fail safe). Drop one empty row that follows a row of display width exactly W. Join word-wrapped rows with one space. A word longer than W breaks at exactly W.
7. **Cursor.** Row = curY − y when inside the text rows. `cursorEnd` = `curX >= 2 + width(row)`; clamp curX to the text area.
8. **Height cap.** Treat as possibly scrolled when text rows ≥ rows − 5 (measured cap rows − 4, one lower to fail safe) or no history is visible above. Scrolling is one row at a time; `›` marks the first visible row.
9. **Frames and overlays.** Never read inside `ESC[?2026h … ESC[?2026l`. On the alternate screen (`?1049h`), return ok=false and keep the last draft.
10. **Pastes.** Expand `[Pasted Content N chars]` and `[Pasted Content N chars] #K` (threshold > 1000 chars). One Backspace can delete a whole placeholder.
11. **Delete-key log.** Add Ctrl+D (0x04), `ESC d`, `ESC 0x7f`, `ESC[3;2~` (Shift+Delete), `ESC[3;3~`, `ESC[3;5~`, `ESC[3;6~`; the CSI-u forms of Ctrl+W/U/K/H/D, Alt+D, Ctrl/Alt+Backspace, Ctrl+Shift+Backspace (`ESC[127;6u`) and Ctrl+Alt+H (`ESC[104;7u`) (with optional `:1`/`:2` event type; ignore `:3` releases). Shift+Backspace stays 0x7f under disambiguate, so the legacy match covers it [docs, kitty protocol]. Ctrl+C clears the whole box. Drop `0x1f` for Codex. Or set `CODEX_TUI_DISABLE_KEYBOARD_ENHANCEMENT=1` and keep legacy matching.

**Real-app test ground truth:** `CODEX_HOME=<tmp> EDITOR=<copy script> codex -c sandbox_mode="danger-full-access"`, type with Ctrl+J or Alt+Enter (works ~200 ms after launch), press Ctrl+G, compare the copy byte for byte (no trailing newline). Drafts cleared with Ctrl+C land in `<tmp>/history.jsonl`.

## Risks
- Keeping the empty continuation row after a width-W row invents a blank line [measured behaviour].
- Several spaces at a wrap point come back as one [measured].
- Without tint, a blank last draft line is indistinguishable from the bottom padding; the footer fallback fails when a popup replaces the footer [measured layout, inference].
- Kitty-protocol keys (flags 5 or 7 depending on terminal) bypass `deleteKeys`; flag 7 adds release events that must not be double-counted [source, docs].
- One Backspace on a paste placeholder removes >1000 characters [measured].
- Remapped keys and vim mode delete text unsent's key log cannot see [source].
- Submitted messages in history also start with `›`; a reader that ignores the dim attribute can latch onto one [source].
- Ctrl+C twice on an empty box quits Codex (matters for scripted tests) [measured].
- If the sandbox makes CODEX_HOME writable, Ctrl+G uses the real `~/.codex/editor` [measured once].

## Open questions
- Box look and height cap while a turn runs (status line, queued preview, approvals): needs a real submit.
- CSI-u delete keys end to end through unsent in Ghostty, kitty, WezTerm and iTerm2, including release events under flag 7.
- Does the real terminal's OSC 11 reply reach Codex under unsent, so the box is always tinted? If not, unsent could answer OSC 11 itself.
- Does text unsent prints before launch (recovery notice) survive Codex's first frame in a normal terminal?
- Remote image rows and the pet right-reserve: seen only in source.
- 0.157.1 mouse selection and warning row: measure before the reader ships.
- Can `history.jsonl` writing be disabled by config?

## 9. Side effects of the lane's run
- The first Ctrl+G test (workspace-write sandbox) created an empty `~/.codex/editor/`; the lane removed it with `rmdir` [re-checked: absent].
- Scratch stays under `/tmp/unsent-spec-codex/` (captures, raw logs, source clone, temp CODEX_HOME with a dummy key).

## 10. Capture run on Codex 0.158.0 (measured 2026-09-29)

**Environment.** `codex-cli 0.158.0`, the native arm64 build, copied from the MacBook to a Mac mini with macOS 26.6.1. It ran in a scratch folder under `/Volumes/Data`, in a private tmux 3.6b server (`tmux -L unsent-07 -f /dev/null`, `TERM=tmux-256color`, 120x40) with `extended-keys on` and `extended-keys-format csi-u`, under `unsent capture codex` with `UNSENT_DEBUG_DIR` set. unsent has no Codex reader, so it printed its not-protected line and passed every byte through while logging both directions. The environment was `env -i` with a scratch `HOME`, `CODEX_HOME`, `TMPDIR` and XDG folders, `OPENAI_API_KEY=dummy`, `OPENAI_BASE_URL=http://127.0.0.1:9/v1`, `HTTPS_PROXY`, `HTTP_PROXY` and `ALL_PROXY` at the same dead port, no other provider key, no `GITHUB_TOKEN`, `GH_TOKEN` or `CI`, and `check_for_update_on_startup = false` in the scratch `config.toml`. Every run passed `-c sandbox_mode="danger-full-access"` except the two daemon runs below. `$EDITOR` was unsent's copy-and-exit script. The captures, screens and probes are in [`testdata/codex/0.158.0/`](../../testdata/codex/0.158.0/), described in [its README](../../testdata/codex/README.md); every session id below is from that throwaway home.

**A rig trap. `OPENAI_BASE_URL` is not enough on 0.158.0.** Codex's own log (`logs_2.sqlite`) shows it opening its responses websocket at `wss://api.openai.com/v1/responses` on every start and every send, with the base URL set [measured, 66 log rows in one home]. Each attempt ended in `Connection refused (os error 61)`, which is the dead proxy. A connection to the real host would not be refused. So the proxy variables, not the base URL, kept this run on the machine. The rig must set them, or `openai_base_url` in the scratch `config.toml` [guess for the config key; its name is in the binary's strings, not tried].

### What 0.158.0 changed from the 0.151.0 profile above

| 0.151.0 profile says | 0.158.0 does | Tag | Evidence |
| --- | --- | --- | --- |
| The main screen is inline; only overlays use the alternate screen | The whole TUI runs on the **alternate screen**: `ESC[?1049h` 0.27 s after start, tmux `#{alternate_on}` = 1 with the box showing, plus `ESC[?1007h` (alternate scroll). It leaves the alternate screen around Ctrl+G and on exit | measured | every `.rec`; `sessions/suspend-alt.txt` |
| One footer row under the bottom padding | **Two footer rows**: `model · cwd`, then `? for shortcuts` with `⚠ 1 warning · f2 to view` on the right. While the box holds text, the left of the second row is blank. During a turn the first row ends in a spinner, and Ctrl+R shows `reverse-i-search:` there | measured | `screens/empty.txt`, `typed.txt`, `submit-4s.txt`, `ctrlc-history-ctrl-r.txt` |
| An empty box sits under history | An empty box has a large braille logo and a greeting line above it; typing removes the logo. A resumed thread shows a rounded header card instead (`╭─…╮`, `>_ OpenAI Codex (v0.158.0)`, model, directory) | measured | `screens/empty-welcome.txt`, `typed-empty.txt`, `resume-id.txt` |
| Selection lists use a cyan, non-bold `›` | The trust dialog, the `/` menu and the `@` list mark the chosen row with **bold and reverse video** (`ESC[1;7m› `). The composer `›` is bold only, and a sent message in history is bold and dim (`ESC[1;2m› `), as the source said | measured | `screens/trust-dialog.txt`, `slash-menu.txt`, `at-popup.txt`, `submit-4s.txt` |
| The `/` popup replaces the footer below the box | The `/` menu is drawn **above** the box (rows 28 to 35, box on row 37); the `@` list too | measured | `screens/slash-menu.txt`, `at-popup.txt` |
| Two Ctrl+C on an empty box quit | **One** Ctrl+C on an empty box quits. During a turn, Ctrl+C on an empty box interrupts the turn (`■ Conversation interrupted`) and does not quit | measured | `dialogs.rec`, `submit` |
| Ctrl+C on a draft saves it only once a thread exists; nothing under `sessions/` without a submit | A **thread exists from start**: Ctrl+C before any send wrote `{"session_id":"01a0eb0f-027c-…","ts":…,"text":"typed but not submitted, dummy"}`, and quitting prints `Session ID: <id>` even when nothing was sent | measured | `sessions/id-2-ctrlc.txt`, every `-exit` screen |
| A cleared paste is stored in `history.jsonl` as its placeholder | Ctrl+C with a placeholder in the box stored the **full pasted text**. Up recalled it as the placeholder again, and Ctrl+G on the recalled entry gave the full 1,509 characters | measured | `placeholder`, `sessions/history-captures.jsonl` |
| (not covered) | After Ctrl+G returns, the box holds the editor file's text: every placeholder is replaced by its full text, and trailing whitespace is gone. `keep ` came back as `keep`, `omicron ` as `omicron`, and a draft ending in an empty line lost the final line break. The editor copy itself is exact | measured | `placeholder.editor-1.txt` vs the next screen; `deletes.editor-2` to `-5` |
| Kitty flags: 5 in tmux-xterm, 7 elsewhere, plus modifyOtherKeys 2 in tmux-csi-u (source) | Confirmed on the wire: with `csi-u`, Codex writes `ESC[>4;0m ESC[>7u ESC[>4;2m`; with `xterm`, `ESC[>5u` and no `ESC[>4;2m`. Keys then arrive as the table below shows | measured | `deletes.rec`, `deletes-xterm.rec` |
| Codex runs in the process unsent starts | Without `-c`, `--enable`, `--disable` or `--search`, Codex installs a copy of itself under `$CODEX_HOME/packages/app-server-daemon/` and starts a **shared background server** (`codex app-server --listen unix:// --managed-daemon`, plus `codex app-server daemon pid-update-loop`). The thread lives there, and the server keeps running after the TUI exits (parent pid 1). With `-c`, the warning `Running without the shared background server: command-line configuration overrides … requires embedded mode` appears under F2 and the thread lives in the TUI process | measured | `daemon-quit`, `daemon-killsession`, `sessions/daemon-killsession.txt` |

Unchanged and re-seen: the bold `›` at column 0 of the first visible row, continuation rows indented 2, one blank row above and one below the text, a dim `Ask Codex to do anything` hint, `rows − 4` text rows at most (36 at 40 rows: rows 2 to 37, padding on 38, footer on 39 and 40), one-row scrolling that keeps the cursor visible with `›` on the first visible row, and the paste threshold [measured, `tall`, `pastes`]. No background tint: tmux answered DSR and DA but not `OSC 10;?`/`OSC 11;?` [measured, first input chunk of every `.rec`].

### Box, wrapping and text

- Wrap width 117 at 120 columns, as before: 116 `a`s and then `漢字` put `漢` on the next row, leaving the first row one cell short; a 115-`x` word followed by ` ñandú` wrapped at the space [measured, `accents`]. The exact-width continuation row was not re-measured.
- Precomposed accents (`ñandú`, `¿qué pasó?`, `Pingüino`), decomposed ones (`e` + U+0301, `n` + U+0303), `👍🏽` and the flag `🇪🇸` came back byte-identical through Ctrl+G [measured, `accents.editor-1.txt` against the typed bytes].
- A 45-line draft: an edit 40 rows up scrolled the box to rows 05 to 40; going back down scrolled it to rows 10 to 45, and the editor copy still held the edit, now out of sight. A Ctrl+W 30 rows up changed only that row [measured, `tall.editor-1.txt`, `tall.editor-2.txt`].

### Keys as they reached Codex in tmux

| Key | Bytes, `csi-u` (flags 7 and modifyOtherKeys 2) | Bytes, `xterm` (flags 5) | What Codex did (editor copy) |
| --- | --- | --- | --- |
| Ctrl+J | `ESC[106;5u` | `LF` | new line |
| Alt+Enter | `ESC[13;3u` | not sent | new line |
| Shift+Enter | `ESC[13;2u` | not sent | new line |
| Backspace, Shift+Backspace | `0x7f`, `ESC[127;2u` | `0x7f`, `0x7f` | one character back |
| Ctrl+H | `ESC[104;5u` | `0x08` | one character back |
| Ctrl+W | `ESC[119;5u` | `0x17` | one word back |
| Alt+Backspace | `ESC[127;3u` | `ESC 0x7f` | one word back |
| Ctrl+Backspace | `ESC[127;5u` | tmux cannot send it (typed as text) | one word back |
| Delete | `ESC[3~` | `ESC[3~` | one character forward |
| Ctrl+D (in a draft) | `ESC[100;5u` | `0x04` | one character forward |
| Alt+D, Ctrl+Delete, Alt+Delete | `ESC[100;3u`, `ESC[3;5~`, `ESC[3;3~` | `ESC d`, `ESC[3;5~`, `ESC[3;3~` | one word forward each |
| Shift+Delete | `ESC[3;2~` | `ESC[3;2~` | forward (nothing at the end of the text) |
| Ctrl+U | `ESC[117;5u` | `0x15` | kills to the start of the current line only |
| Ctrl+K | `ESC[107;5u` | `0x0b` | kills to the end of the line |
| Ctrl+Y | `ESC[121;5u` | `0x19` | yanks the last kill back |
| Ctrl+_ | `ESC[95;5u` | `0x1f` | nothing: no undo |
| Ctrl+C | `ESC[99;5u` | `0x03` | clears a draft; quits on an empty box |
| Ctrl+G | `ESC[103;5u` | `0x07` | external editor |
| Ctrl+Z | `ESC[122;5u` | not run | see below |
| Up, Home, End | `ESC[A`, `ESC[1~`, `ESC[4~` | the same | |

[measured, `deletes.rec`, `deletes-xterm.rec`, `multiline.rec`, `suspend-direct.rec`; the effects from `deletes.editor-1` to `-8`, identical in both runs except where the literal `C-BSpace` text sits]. tmux sends no key release or repeat events, so the flag-7 release form was not seen. The encodings are those of kitty's disambiguate mode for these keys, but they came from tmux's modifyOtherKeys 2, not from kitty, Ghostty, WezTerm or iTerm2.

**Ctrl+Z.** Codex handles it itself. Run alone under `script` (its own session), Ctrl+Z made it write `ESC[<1u ESC[?1007l ESC[?1049l ESC[2;1H ESC[?25h ESC[<1u ESC[>4;0m ESC[?2004l ESC[?1004l ESC[0 q ESC[?25h`, then, when the kernel dropped the stop, set every mode again, asked `ESC[6n`, and repainted the same box with the draft intact [measured, `suspend-direct.rec` at 3.39 s]. Through unsent, which takes Ctrl+Z for an agent with no profile, zsh printed `suspended ./start.sh`, Codex showed state `T`, and `fg` brought the box back with the draft; the next Ctrl+G copy matched it [measured, `suspend`]. While stopped, the terminal stayed on Codex's alternate screen with its key modes on, because unsent has no Codex `suspended` bytes to write; the sequence above is what a Codex profile would write [measured screen; design for the fix].

**History.** Up on an empty box recalled the newest entry, from any earlier session in the same `CODEX_HOME`; Down went back to the empty box. Up in a typed draft changed nothing. Ctrl+R opens `reverse-i-search:` in the footer [measured, `ctrlc-history`].

### Pastes

- A 3-line paste and a 1,000-character paste went in as text; 1,001 characters became `[Pasted Content 1001 chars]` in colour 6, and a second one of the same size `[Pasted Content 1001 chars] #2` [measured, `pastes`].
- A 1,079-character paste with 11 line breaks became `[Pasted Content 1079 chars]`, so N counts line breaks as characters. Sent with LF (`paste-buffer -r`) and with CR (tmux's default), both came back with LF only [measured, `pastes.editor-4.txt`: 22 LF, no CR].
- One Backspace right after `[Pasted Content 1001 chars]` removed all of it: the editor copy was `keep ` [measured, `placeholder.editor-1.txt`].
- Ctrl+G opens the full text, never the placeholder [measured, `pastes.editor-2.txt`, 2,016 characters for `before `, two 1,001-character pastes and ` after `].

### A send that fails

Enter sent the prompt. It moved into history as a bold and dim `› ` row, the box went back to the empty hint at once, and a status row above the box showed `◦ Reconnecting... 2/5`, then `5/5`, then `Reconnecting... waiting for network (33s • esc to interrupt)` with the cause below it; after 30 s it was still retrying [measured, `screens/submit-*.txt`]. The box stayed on row 37 during the turn; the height cap during a turn was not measured. Typing during the turn worked, and Ctrl+C cleared that draft into `history.jsonl`; the next Ctrl+C interrupted the turn [measured, `submit`]. With a draft in the box during a turn the footer offers `tab to queue message` [measured, `screens/suspend-direct-typed-after.txt`].

### Session identity

| When | `history.jsonl` | `sessions/` | What another process can read | Tag |
| --- | --- | --- | --- | --- |
| Start, before any key | no file | none | the Codex process (unsent's child, with `-c`) holds `$CODEX_HOME/thread-writer-locks/<thread id>.lock` open, with an exclusive `flock` (a second `flock` fails with `EAGAIN`). `state_5.sqlite` has no `threads` row | measured, `sessions/id-0-start.txt`, `flock-resume-id.txt` |
| Typed, not sent | no file | none | the same lock, nothing new | measured, `id-1-typed.txt` |
| Ctrl+C on the draft | `{"session_id":"<thread id>","ts":…,"text":"typed but not submitted, dummy"}` | none | the same lock | measured, `id-2-ctrlc.txt` |
| 0.5 s after a send that fails | plus the sent text, same `session_id` | `sessions/2026/09/29/rollout-2026-09-29T04-47-04-<thread id>.jsonl` (local time), held open by the process; its first line is `session_meta` with the id; a `threads` row with the path | lock and rollout, both naming the id | measured, `id-3-submit-0.5s.txt` |
| After the turn is interrupted and Ctrl+C on a new draft | plus that draft | the rollout ends in `turn_aborted` | the same | measured, `id-6-ctrlc-after-submit.txt` |

The lock is held by the time the box is drawn: a 50 ms poll of `codex resume <id>` saw the box, the lock and the open rollout in the same pass, 0.33 s after start [measured, `sessions/poll-resume-id.txt`]. A clean exit removes the lock file; after `kill -9` it stays until a later start clears it [measured, `thread-writer-locks/` listings]. `logs_2.sqlite` also maps `process_uuid` `pid:<pid>:<uuid>` to `thread_id`, but its rows arrive in batches: none had appeared 10 s after that resume, and a send showed rows only after several seconds, one of them for a second thread id that is in no `threads` row [measured]. It is a log, not a place to read the running thread from.

**Resume forms.**

| How it was opened | Thread the process then holds | Tag |
| --- | --- | --- |
| `codex resume <id>` | the same id: its lock and its rollout; the box was empty | measured, `resume-id` |
| `codex resume --last` | the same id as `codex resume <id>` here, the only thread of the folder with a send | measured, `resume-last` |
| `codex resume` (the picker, on the alternate screen, `Resume a previous session`) | nothing while the picker is open (no lock); the chosen id after Enter. The picker listed one thread, the one with a send; the fork and the threads that never sent were not in it | measured, `picker`, `sessions/id-picker-chosen.txt` |
| `codex fork --last` | a new id, whose rollout exists at once; the screen says `• Thread forked from <id>` | measured, `fork-last` |
| `/new` inside a session | a new id, **and the old thread's lock and rollout stay open** | measured, `sessions/id-after-new.txt` |
| `/resume` inside a session, then Enter | the chosen id, with the other lock still held | measured, `id-slash-resume-chosen.txt` |
| `codex resume <id>` of a thread that never sent | `ERROR: No saved session found with ID <id>. Run codex resume without an ID to choose from existing sessions.`, exit status 1. Through the shared server: `no rollout found for thread id <id> (code -32600)` | measured, `resume-unsent-id`, `screens/daemon-resume-unsubmitted.txt` |

`/status` shows `Session: <id>` in a card, and every exit prints `Session ID: <id>`, or `To continue this session, run: codex resume <id>` once the thread has a send [measured, `screens/status.txt`, `-exit` screens]. Neither is readable without the screen.

So the running thread's id can be read from outside when Codex runs embedded (with a `-c` flag): list the files the child pid holds open and take the `thread-writer-locks/<id>.lock`. It is unambiguous until `/new`, `/resume` or a fork inside the session, after which the process holds several locks and none of them marks which is current [measured]. Under the shared server, which is what a plain `codex` starts, the TUI process holds no lock and no rollout; one server process holds the locks of every thread of every TUI on that `CODEX_HOME`, and keeps them after the TUI exits [measured, `sessions/id-daemon-draft.txt`, `daemon-killsession.txt`]. There the child's pid names no thread, and how unsent could read it is still open: the server's JSON-RPC on `$CODEX_HOME/app-server-control/app-server-control.sock` was not tried, and the binary has a `--no-daemon` switch [source, strings only] that would be reconfiguring the agent, which decision 3 rules out.

### Kill, close, synchronized output

- `kill -9` of Codex: the draft (`kill nine draft zebra`) is in no file under `CODEX_HOME`, SQLite files and WAL included. The terminal stays on the alternate screen with kitty flags and modifyOtherKeys 2 on, and unsent's exit lines are printed over Codex's last frame [measured, `kill9`, `screens/kill9-after.txt`].
- tmux `kill-session`: both Codex and unsent exit, no process is left, and the draft is in no file [measured, `sessions/ks-kill.txt`]. Under the shared server the TUI exits, the server keeps running with the thread's lock, and the draft is in no file either [measured, `sessions/daemon-killsession.txt`]. The rig has to kill that server itself.
- **Codex sends synchronized-output marks.** 5,627 `ESC[?2026h`/`ESC[?2026l` pairs in 22 records, balanced in every record. Every paint of the box was inside a pair. Outside the pairs there were only mode switches (bracketed paste, kitty flags, focus, alternate screen, cursor), the startup queries, the window title (`OSC 0;work`, with a spinner during a turn) and the exit text. The median time between the ends of two frames was 35 ms [measured, all records]. Codex needs no quiet-gap read.
- Text printed before launch survives: unsent's not-protected line printed before Codex started was still in the scrollback above `Session ID:` after exit, because Codex draws on the alternate screen [measured, every `-exit` screen].

### Open questions from above, answered where this run could

- **Box and height cap while a turn runs:** the look is measured (status rows above the box, spinner in the footer); the cap is not.
- **CSI-u delete keys end to end through unsent:** measured through unsent in tmux with modifyOtherKeys 2, table above. Real Ghostty, kitty, WezTerm and iTerm2 were not run, and release events under flag 7 were not seen.
- **Does the real terminal's OSC 11 reply reach Codex under unsent?** Not answered: tmux sent no reply, so there was no tint.
- **Does text printed before launch survive the first frame?** Yes on 0.158.0, because of the alternate screen.
- **Remote image rows and the pet reserve:** not measured.
- **0.157.1 mouse selection and warning row:** warnings now sit on the second footer row (`⚠ 1 warning · f2 to view`), and F2 opens a `Warnings · 1 of 1` view; mouse selection was not tried.
- **Can `history.jsonl` writing be disabled by config?** Not checked.
- **SPEC 2.3's Codex row, "whether a thread exists before the first submit":** yes, from start [measured].

### What this run could not capture

- The height cap and box during a running turn, and a Tab-queued message.
- Real terminals other than tmux, and flag-7 release events.
- A paste on the first frame of the box (the settle delay) and a restore; unsent has no Codex reader yet.
- A way to name the running thread under the shared server.
- One inadvertent send: in `suspend-direct` Codex did not stop on Ctrl+Z, so the `fg` and Enter meant for the shell were sent from its box. That send failed on the dead proxy like the others.

## Verification notes

What I checked:
- Scratch artefacts exist: `cap-empty.txt`, `cap-p12.txt`, `cap-p45.txt`, `raw-run{1,2,3}.log`, `editor-args.txt`, `editor-copy.txt`, paste inputs, temp home; the source clone is at tag `rust-v0.151.0`, commit `78c2908`.
- Raw logs: counted `?2026h/l` pairs (111/175/30), `?1049h` (1/0/0), `>5u` pushes, `<1u` pops, `>4;0m`, OSC 10/11, `6n`, DA, scroll regions; confirmed the startup byte order.
- Captures: empty box glyph and dim hint; 45-line paste gives 36 text rows at 40 rows with `›` on the first visible row; 12-line paste inline.
- Ctrl+G copy: 123 bytes, `q*117 \n\n next`, no trailing newline.
- `history.jsonl` has 18 entries including the placeholder-only paste; `tinted draft` appears nowhere under the temp home (SQLite and WAL included); no `sessions/`; `~/.codex/editor` absent.
- Source: `LIVE_PREFIX_COLS`, `COLS_WITH_MARGIN`, paste threshold and format, hint constants, continuation-row and hanging-separator comments, `effective_scroll`, glyph rendering, effort tier glyphs, default keymap, no undo, `clear_composer_for_ctrl_c`, history recall guard, external-editor directory order, alt-screen mode, keyboard enhancement flags, `Cargo.toml` versions.
- Version table: fetched `chat_composer.rs` at `rust-v0.30.0` and `rust-v0.46.0` from GitHub; checked the saved 0.157.1 copy.
- unsent side: `wrap.go:502-531` `deleteKeys` byte lists.

What I changed:
- **Kitty flags**: `>5u` is not universal. Codex pushes 5 only on Ghostty, iTerm2 or tmux-xterm, and 7 (adds release/repeat events) elsewhere; tmux-csi-u also gets modifyOtherKeys 2. Added the `CODEX_TUI_DISABLE_KEYBOARD_ENHANCEMENT` opt-out and the "ignore release events" rule.
- **History `›`** moved from guess to source: bold+dim. The recipe now rejects dim anchors instead of relying only on "lowest row".
- **Ctrl+C → history** holds only when a thread id exists (source); the lane stated it unconditionally.
- Added two layout cases from source that the lane missed: remote image rows inside the composer, and the pet right-reserve that narrows W.
- Corrected the 0.30.0 row (left-border box, not "none found").
- Downgraded the "cursor at cols−2 after a full-width row" reading; it conflicts with the continuation-row rule, so the recipe clamps instead.
- The tint measurement's capture was not kept; tagged it as measured without capture and backed it with source.
- Dropped the lane's first open question: it quoted an unrelated message asking to delete memory or CLAUDE.md text. It is outside this spec, and nothing here acts on it.
- Minor: `wrap.go:507` citation is `wrap.go:508`.

Not re-checked (kept with the lane's tag): 0.80–0.140 table rows, 0.157.1 mouse selection and warning row, `handle_paste` sanitising, `[Image #N]` on path paste, `paste_burst.rs`, the disabled-input hint strings, and the running-turn height cap.
