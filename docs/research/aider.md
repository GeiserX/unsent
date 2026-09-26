# aider: input-box profile for unsent (verified)

Measured on aider-chat 0.86.2 (PyPI upload 2026-02-12, still the latest release on 2026-09-26) with prompt_toolkit 3.0.52, which aider pins exactly (`Requires-Dist: prompt-toolkit==3.0.52` in the dist-info METADATA).

Tags:
- **[measured]**: seen by the research lane in a private tmux server (`tmux -L aiderprof -f /dev/null`, 100x30 unless stated). Where a capture survives in `/tmp/aider-prof/` it is named. "(capture not retained)" means only the lane's word backs it.
- **[source]**: read in the installed package (`/tmp/aider-prof/venv/lib/python3.12/site-packages/aider/` or `.../prompt_toolkit/`), in the per-version `io.py` files under `/tmp/aider-prof/wheels/<ver>/`, or on GitHub at the named tag.
- **[docs]**: aider's `website/docs/usage/commands.md` and `_includes/multi-line.md`.
- **[guess]**: inference nobody verified.

Setup [measured]: `/tmp/aider-prof/run.sh` runs aider under `env -i` with `HOME=/tmp/aider-prof/home`, cwd `/tmp/aider-prof/work`, `TERM=xterm-256color`, `EDITOR`/`VISUAL` = `/tmp/aider-prof/grab.sh` (copies its last argument to `editor-grab.txt`), and `--model gpt-4o --openai-api-key sk-dummy-not-real --no-git --no-check-update --no-show-release-notes --analytics-disable --no-show-model-warnings`. Nothing was submitted: `work/` holds no `.aider.input.history`, and `work/.aider.chat.history.md` has 0 lines starting `####` (re-checked).

Surviving captures: `raw.bin` (23.7 KB of raw pane output covering startup through the 60-column resize and the 10-line paste; it does not cover the completion-menu, 500-line paste or Ctrl+L tests), `big.txt` / `editor-grab.txt` (the 500-line editor ground truth), `paste.txt`.

## 1. TUI stack
- prompt_toolkit `PromptSession.prompt()` in inline (non-full-screen) mode; `rich` prints output, the rule and the file list. `io.py:343-362,656-666` [source]
- `PromptSession` is created without `multiline=`, so it is single-line mode as far as prompt_toolkit is concerned; aider adds newlines itself through key bindings. [source]
- The same session also draws yes/no confirmations (`confirm_ask`) with a question instead of the `> ` prefix. [source]

## 2. What the box looks like
```
────────────────────────────────────  <- rich console.rule(), full width U+2500, SGR 38;5;40
Readonly: ref.md                      <- optional file list, part of the prompt message, no prefix
Editable: notes.txt
> first line of the draft
> second line, and a very long line wraps per charac
> ter with the SAME prefix on the wrapped row
```
Real capture (`raw.bin`, 60 columns, after the resize, text stripped of escapes):
```
> pasted line 10 do nothing plus a longer tail so that this
> line wraps at the new narrower width ok
^C again to exit
────────────────────────────────────────────────────────────
>
```
- No bottom rule and no side border. Below the box are blank rows or the completion menu. [measured]
- **Prefix.** `io.py:545-553` builds `[edit_format][" " if edit_format]["multi"]"> "`. `edit_format` is shown only when it differs from the main model's default (`base_coder.py:902`; gpt-4o's default is `diff`, per the startup banner in `raw.bin`). Measured: `> ` and `ask multi> ` (`--chat-mode ask --multiline`). From source: `multi> `, `ask> `, `architect> `, `code> `, `help> `, `context> `, `whole> `, `diff-fenced> `, `udiff> `, `editor-diff> `, and so on. The separator is ASCII space. [measured + source]
- **Every row repeats the prefix**, hard newlines and soft wraps alike. aider's `get_continuation(width, line_number, is_soft_wrap)` returns `self.prompt_prefix` unconditionally (`io.py:653-654`), and prompt_toolkit calls it for every row after the first, including wrap rows (`shortcuts/prompt.py:1332-1350`, `layout/containers.py` wrap loop). A draft line starting with `> ` renders as `> > ...`. Strip exactly one prefix per row. [measured + source]
- **Numeric-argument exception (new).** In single-line mode, while an emacs numeric argument is pending (Esc then a digit), prompt_toolkit replaces the first row's prompt with `(arg: N) ` (`shortcuts/prompt.py:1343-1345`). A reader keyed on the prefix will miss row 0 for that moment. [source]
- **Empty box:** just the prefix (`>` after trimming), cursor at column len(prefix). No hint or placeholder text, dim or otherwise. [measured, `raw.bin` last rows]
- **Colour:** prefix, draft text and rule all use `--user-input-color` (default `#00cc00`, emitted as `ESC[0;38;5;40m`: 370 occurrences in `raw.bin`). The Pygments Markdown lexer can restyle parts (strings get `bold italic`, `io.py:400-411`). Colour cannot separate prefix from text, and there are no faint cells in a live box. [measured + source]
- **Abandoned-draft repaint (new).** When a prompt ends (Ctrl+C, submit, exit), prompt_toolkit repaints the box once in its "done" style. For Ctrl+C that is `class:aborting` = `#888888` (`styles/defaults.py:99`), emitted as `ESC[0;38;5;102m`: `raw.bin` shows the 30-row draft repainted grey row by row just before `^C again to exit`. The old rows keep the `> ` prefix. [measured + source]
- `--no-pretty`: the rule becomes a blank line (`io.py:509-514`) and there is no colour; the prefix is unchanged. [source; measured (capture not retained)]
- Rows are padded with coloured spaces; a row's trailing spaces cannot be told from padding. [measured, `raw.bin`]

## 3. Screen mode and escape inventory
- **No alternate screen.** Private modes in `raw.bin`: `?25h/l`, `?7h/l`, `?12l`, `?2004h/l` (6 pairs), `?1l`. No `?1049h`, no mouse modes, no synchronized output (`?2026`). This covers the `raw.bin` segment only; the lane reports the same for the whole session (capture not retained for the rest). Old prompts scroll into normal scrollback, so unsent's recovery notice stays visible. [measured]
- **Cursor position request `ESC[6n`** at each prompt start: 6 in `raw.bin` (the lane said 5). The real terminal answers. unsent's own emulator also answers into its pipe, but `wrap.go:87-90` drains those replies to `io.Discard`, so the child never gets a second reply. This closes the lane's open question. [measured + source]
- Bracketed paste: `?2004h` while the prompt is up, `?2004l` when it returns. [measured, `raw.bin`]

## 4. Wrap, growth, scroll, cursor
- **Wrap width is `cols - len(prefix)`, character-level, no word wrap.** prompt_toolkit wraps when `x + char_width > width` with `x` counting the prefix (`layout/containers.py`, `_copy_body` loop). Measured: 150 digits at 100 columns with `> ` broke after 98; 58 at 60 columns; 89 with `ask multi> `. [source; measured (capture not retained), 60-col row consistent in `raw.bin`]
- **Wide characters (new, was an open question):** a double-width character that would cross the edge moves to the next row and leaves the last column blank, so that row is `width-1` cells. A reader's "row is full, so it was a soft wrap" test fails there. [source]
- A break that lands on a space leaves the space as the row's last cell; the snapshot trims it (the 60-column row above is 57 visible characters plus that space). [measured, `raw.bin`]
- **Growth:** the box grows downward; at the bottom the terminal scrolls, pushing banner and rule into scrollback. Once the box's top reaches row 0 it fills the whole screen and scrolls internally; rows scrolled out that way are not in terminal scrollback, only in prompt_toolkit's buffer (34-line draft on 30 rows: rows 0-29 all `> ...`, tmux history held only the banner). [measured (capture not retained)]
- **Scrolling is one row at a time;** Up on the top visible row scrolls exactly one row, cursor stays on row 0. [measured (capture not retained)]
- **The terminal cursor sits at the insertion point** (e.g. `(54,13)` = column 2 + 52 characters). [measured (capture not retained)]
- Resize reflows the box at the new width. [measured, `raw.bin` 60-column section]
- Up on the first line / Down on the last switches to input history (`auto_up`/`auto_down`, `key_binding/bindings/basic.py:207-212`), replacing the buffer wholesale. [source]

## 5. Keys (emacs mode, the default)
- **Submit:** Enter; **Ctrl+J** (fed as `c-m`, `basic.py:195-202`); **Ctrl+O** (`operate-and-get-next` calls `validate_and_handle()`, `emacs.py` + `named_commands.py:665-675`; new). Tests must never send C-j or C-o. [source]
- **Newline:** Alt+Enter / Esc Enter (`io.py:626-634`), measured with `send-keys Escape Enter`. With `--multiline` the two swap. Shift+Enter is not supported. [measured + source + docs]
- **`{` blocks:** a line that is exactly `{` or `{tag` starts a block ended by `}` / `tag}`. Each block line is its own prompt call; `rule()` runs only once per `get_input`, so earlier block lines stay on screen above the live row with the same prefix and **no rule between them**, but have already left prompt_toolkit's buffer (`io.py:694-728`). [source]
- **No warm-up wait:** letters typed about 1 s after launch were echoed by the tty, then delivered into the box once the prompt appeared. [measured (capture not retained)] Esc+Enter during startup: not measured. [guess: works, typeahead is replayed as keys]
- **Delete before the cursor:** Backspace (`\x7f`), Ctrl+H (`\x08`), Ctrl+W (unix-word-rubout, `\x17`), Esc Backspace (`\x1b\x7f`), Ctrl+U (`\x15`, to line start). **After the cursor:** Delete (`ESC[3~`), Ctrl+D (`\x04`, only when there is text *before* the cursor: `basic.py` filter `has_text_before_cursor`; the lane said "when there is text"), Ctrl+K (`\x0b`, to line end), Esc d (`\x1bd`, kill-word), Ctrl+Delete (`ESC[3;5~`, kill-word in emacs mode, `emacs.py:71`). **Undo:** Ctrl+_ (`\x1f`), Ctrl+X Ctrl+U (`\x18\x15`). **Selection cut:** Ctrl+W or Delete with a selection active (`emacs.py:262`, `basic.py:214-217`), unlimited. [source]
- **Numeric arguments multiply deletes (new):** Esc 5 0 Backspace deletes 50 characters. A per-byte count of Backspace undercounts. [source, prompt_toolkit `event.arg`]
- Text-changing keys that are not deletes: Esc c / Esc l / Esc u (case), Ctrl+T (transpose), Esc . and Esc _ (yank-last-arg), Ctrl+Y / Esc y (yank). Ctrl+Space inserts a space (aider override, `io.py:582`). [source]
- **Ctrl+C throws the whole draft away:** prints `^C again to exit`, a new rule and an empty prompt; the old rows stay above, repainted grey (section 2). [measured, `raw.bin`] **A second Ctrl+C within 2 s exits aider** (`base_coder.py:986-1000`, `thresh = 2`; new). [source]
- **Ctrl+D on an empty buffer exits aider** (`shortcuts/prompt.py:841-854`). [measured + source]
- **Ctrl+L** clears the screen (`basic.py` `clear-screen`); the rule disappears and the box redraws at row 0. One `ESC[2J` appears in `raw.bin`. [measured + source]
- `--vim`: vi bindings, cursor shape via `ModalCursorShapeConfig` (`io.py:353-354`). vi deletes are plain letters (`x`, `dd`, `D`, `c...`) and cannot be counted from bytes. [source; not measured]
- Ctrl+Z suspends (`io.py:577-580`). [source]

## 6. Pastes
- **Always inline, never a placeholder.** 10 lines (`paste.txt`, rendered in `raw.bin`) and 500 lines were inserted in full, each row prefixed; the 500-line paste took about 3.5 s to render. prompt_toolkit converts `\r\n` and `\r` to `\n` (`basic.py:234-247`). No threshold. [measured (10-line in `raw.bin`; 500-line capture not retained) + source]
- `--copy-paste`: a clipboard watcher polls every 0.5 s; on a change it calls `interrupt_input()` (which saves the buffer into `io.placeholder`) and then **overwrites** `placeholder` with the clipboard text, wrapped in leading and trailing `\n` when it has several lines (`copypaste.py:24-31`). The next prompt opens with the clipboard text; the old draft is gone with no delete key pressed. [source]
- `/paste` pastes the clipboard as a command. [docs]

## 7. Completion menu (the main hazard for a reader)
- `complete_while_typing=True`, `CompleteStyle.MULTI_COLUMN`, `reserve_space_for_menu=4` (`io.py:656-666`). Typing `/cmd` or a known file name opens a menu drawn as a float over other rows. [source]
- Measured: typing `/ad` on line 1 of a 4-line draft turned line 2 into `> sec /addine reply ok`, menu cells styled `ESC[38;5;16m ESC[48;5;250m`. [measured (capture not retained; `raw.bin` has no `48;5;` sequence)] Those colours match prompt_toolkit's default `completion-menu` style `bg:#bbbbbb #000000` (`styles/defaults.py:65`); the selected item uses `reverse` (`defaults.py:69`), so a background check must also treat reverse-video cells as menu cells. aider leaves the menu colours unset by default (`--completion-menu-*` default `None`, `io.py:415-431`); with them set the background is still non-default. [source]
- **Placement (was a guess, now source):** the float goes one row below the cursor. If it does not fit and there is more room above than below, prompt_toolkit moves it **above the cursor**, over the draft rows there (`layout/containers.py:929-947`). So once the box fills the screen and the cursor is in its lower half, the menu covers rows above the cursor, not below. [source; not measured]

## 8. Draft persistence and history
- **No draft persistence.** Only submitted input goes to `.aider.input.history` (git root, or cwd without git; `args.py:271-275`), FileHistory format; `.aider.chat.history.md` also records submitted input only. A crash, a closed terminal or Ctrl+C loses the draft. [source + measured: no history file after all tests]
- In-process rescue: `interrupt_input()` (from `--watch-files` AI comments or the clipboard watcher) stores the buffer in `io.placeholder`; the next prompt reopens with it as `default=` (`io.py:516-521,641-644`). The clipboard watcher then overwrites it (section 6). [source]
- History recall: Ctrl+Up/Down (aider bindings), Ctrl+P/N, PageUp/PageDown, Ctrl+R search, Esc < / Esc >. [source + docs]

## 9. External editor (ground truth for real-app tests)
- **Ctrl+X Ctrl+E** calls `pipe_editor(input_data=current_text, suffix="md")` **without** passing `editor=` (`io.py:597-610`), so it uses `$VISUAL`, then `$EDITOR`, then `vim` on macOS / `vi` elsewhere (`editor.py:20-21,85,89-110`). **Correction:** the lane said `--editor` is tried first; only `/editor` passes `--editor` (`commands.py:1554`). [source]
- Measured: the copy script received the exact buffer: 34 logical lines, and separately 500 lines, no trailing newline added. `big.txt` (the pasted source, 500 lines, no final newline) and `editor-grab.txt` are byte-identical (`cmp`, re-checked). [measured]
- On return the buffer becomes `edited.rstrip("\n")`, so trailing newlines in a draft are dropped. This matches the `EDITOR`-grab method in unsent's CLAUDE.md. [source]

## 10. Startup
- Cold first run in a fresh HOME: about 90 s to the prompt; warm: 7.0 s. [measured (capture not retained)] Cold cost is likely `.pyc` compilation plus litellm's first import. [guess]

## 11. Version stability
| version | date (PyPI) | prompt | following rows |
|---|---|---|---|
| 0.50.0 – 0.74.0 | 2024-08 → 2025-02-06 | `> ` | **no prefix**: no `prompt_continuation`, and prompt_toolkit only pads with spaces when `multiline=True` (`shortcuts/prompt.py:1325-1328`) |
| 0.70.0 | 2024-12-26 | `multi` mode and the `escape enter` binding first appear | — |
| 0.75.0 – 0.78.0 | 2025-02-24 → 2025-03 | `> ` | **`. `** |
| 0.79.0 → 0.86.2 | 2025-03-25 → 2026-02-12 | `[fmt][ multi]> ` | **same as the prompt prefix** |
| 0.80.0 | 2025-03-31 | Ctrl+X Ctrl+E added | — |

[source: `wheels/<ver>/io.py` for 0.50/0.60/0.65/0.70/0.75-0.80/0.84; the 0.71-0.74 gap closed on GitHub tags `v0.71.0`-`v0.74.0` (no `continuation` in `aider/io.py`); dates from the PyPI JSON API]. Upstream `aider/io.py` last changed 2025-06-25 (`14af218ea2`), the repo's latest commit is 2026-05-22 (`5dc9490bb3`) (`gh api`, re-checked). The screen format has been stable for about 18 months and rides on the exact `prompt-toolkit==3.0.52` pin. [source]

## 12. What would break the reader
1. A change to `get_continuation` (changed twice already) or the prefix format. On aider < 0.79 the reader must fail safe (`ok=false`).
2. A prompt_toolkit bump that changes wrapping or continuation handling.
3. The completion float covering draft rows **below or above** the cursor (section 7). It fails silently with plausible text.
4. Ctrl+L or `--no-pretty` removing the rule, so the reader cannot require one.
5. `--no-fancy-input` or `TERM=dumb`: plain `input()`, cooked tty, no prefix on following rows, no bracketed paste (`io.py:340-367`).
6. The clipboard or file watchers replacing the buffer with no delete key.
7. **Soft wrap vs hard newline is ambiguous in two directions (widened).** A row of exactly `width-1` characters ending in a trimmed space may be a wrap; a row of exactly `width` characters may be a whole logical line followed by a hard newline, which a "full row means wrap" join glues together wrongly. A wide character at the edge makes a wrapped row `width-1` cells.
8. `{` blocks: earlier block lines sit above the live row with the same prefix and no rule, so a walk-up reads them as box rows. They are unsent text, but the cursor and delete-key logic only apply to the last row, and once they scroll off they exist nowhere.
9. The grey (`38;5;102`) abandoned draft left above after Ctrl+C looks like box rows; the walk-up must stop at `^C again to exit` and the rule, which it does in the recipe below.
10. The `(arg: N) ` prompt replacement while a numeric argument is pending.

## Detection recipe (corrected)
Proposed `aiderBox(s *screen) (view, bool)` in `extract.go`, with `extractorFor` matching `filepath.Base(command) == "aider"`.

1. Prefix from the cursor row: `row := s.rows[s.curY].text()`; `P` = `^(?:[a-z][a-z-]*(?: multi)?|multi)?> ` matched against `row + " "` (so a trimmed empty `>` still matches). No match: `ok=false` (confirm prompts, streaming output, an editor on the alternate screen, Ctrl+R search, a pending `(arg: N)`). [guess, design]
2. Row membership: `text == strings.TrimRight(P, " ")` or `strings.HasPrefix(text, P)`.
3. Walk up from `curY` while rows belong; stop at the `─` rule, file-list rows, `^C again to exit`, a blank row (`--no-pretty`) or row 0. Also stop at a row whose cells are grey `38;5;102` (needs a foreground-colour or "not the live style" test; optional given the `^C` line). Walk down while rows belong; stop at the first blank row or a menu row.
4. `v.rows[i] = textFrom(len(P))`; `v.width = s.cols - runewidth(P)`; `v.cursor = curY - top`; `v.cursorEnd = curX >= len(P) + width(row)`.
5. `v.empty = top == bot && textFrom(len(P)) == ""`. No dim hint; do not use `faintFrom`.
6. `v.capped = top == 0`. The box scrolls one row at a time, so the stitcher's overlap alignment applies.
7. **Completion-menu guard (corrected):** `screenRow` today carries only `faint` (`screen.go:20-22`), so this needs a new per-cell flag for non-default background **or reverse video**. Because the menu can sit above the cursor as well as below, cutting `bot` to `curY` is not enough; return `ok=false` whenever any flagged cell lies in the box region (or on any screen row, which is simpler). [source for placement; design guess for the fix]
8. Unwrap: a new `unwrapChars(rows, width)`. Join with no separator when `runewidth(prev) == width`; everything else emits `\n`. Accept that a logical line of exactly `width` characters followed by a hard newline will be joined wrongly, and that a space or wide character at the edge produces a spurious `\n`; the Esc/Ctrl+X Ctrl+E ground truth in real-app tests will show how often this bites. The existing word-wrap `unwrap()` must not be reused.
9. Delete keys: unsent's `deleteKeys` is one global list today (`wrap.go:507-509`: `\x7f`, `\x08`, `ESC[3~`; unlimited `\x17 \x15 \x0b \x1f`). For aider add `\x1b\x7f` (unlimited), `\x1bd` (unlimited, ahead), `ESC[3;5~` (unlimited, ahead), `\x04` (one char, ahead), and `\x03` (Ctrl+C, unlimited both sides). `\x18\x15` is already covered by `\x15`. Treat any Esc-digit prefix as making the following delete unlimited. vi mode cannot be covered by byte matching.
10. Real-app test harness: newline with `send-keys Escape Enter` (never C-j or C-o, both submit); paste with `paste-buffer -p` (arrives inline); ground truth with `send-keys C-x C-e` and `VISUAL`/`EDITOR` set to a copy script (not `--editor`, which Ctrl+X Ctrl+E ignores); trailing newlines are stripped when the buffer comes back. Never send two Ctrl+C within 2 s. Wait for a row matching `^> *$` (warm start about 7 s). Launch with `--no-check-update --analytics-disable --no-show-release-notes`, a dummy key, and a scratch HOME/cwd.

## Risks
- The completion float overwrites draft rows with plausible text, above or below the cursor; without a background/reverse check the reader saves corrupted text as exact. [measured + source]
- Char-level wrap makes unwrap ambiguous at full-width and width-1 rows. [source + measured]
- aider < 0.79 draws following rows with no prefix (< 0.75) or `. ` (0.75-0.78); the reader must fail safe there. [source]
- Ctrl+J and Ctrl+O submit; two Ctrl+C within 2 s exit. [source]
- `--copy-paste` and `--watch-files` replace or interrupt the buffer with no delete key; only `keepOld`'s safety copy guards it. [source]
- The `prompt-toolkit==3.0.52` pin is the stability anchor; any aider release that bumps it needs a re-measure. [source]
- The lane's scratch install is still at `/tmp/aider-prof` (venv, 11 wheels, a partial clone in `src/`, uv cache). Harmless; delete when the spec is done.

## Open questions
- Menu placement above the cursor is read from source, not measured on a full-screen box.
- Esc+Enter typed during startup was not measured.
- Vi mode and `--no-fancy-input` screens: source only.
- How often the full-width hard-newline case happens in real drafts: unknown.
- The lane relayed an unrelated message ("delete that warning", options 1/2) at the top of its task and did not act on it; the main session should check where that reply belonged.

## Verification notes
Checked (all against the lane's still-present install at `/tmp/aider-prof`, prompt_toolkit 3.0.52 source, GitHub and PyPI):
- Version 0.86.2 (`_version.py`), the exact prompt_toolkit pin (METADATA), PyPI upload dates for 0.70-0.80, 0.86.1 and 0.86.2, and upstream `io.py`/repo commit dates via `gh api`: all hold.
- `io.py` prefix construction, `get_continuation`, Enter/Esc-Enter bindings, `rule()`, `interrupt_input`, the `{` block loop, `_get_style`: hold.
- The version table from the extracted wheels: holds. The lane skipped 0.71-0.74; I fetched those tags from GitHub and moved the "no prefix" row's end from 0.70 to 0.74.
- prompt_toolkit char-level wrap, continuation on wrap rows, `(arg: N)` prompt, float placement, key bindings, default styles: read directly.
- Captures: `raw.bin` escape inventory re-counted, `cmp big.txt editor-grab.txt` identical, chat history has 0 `####`, no input-history file.
- unsent side: `wrap.go:87-90` (emulator replies dropped), `wrap.go:507-509` (global delete keys), `screen.go:20-22` (no background flag).

Changed:
- **Corrected:** Ctrl+X Ctrl+E ignores `--editor` (only `/editor` uses it). Ctrl+D deletes only with text before the cursor. `ESC[6n` count is 6 in the capture, not 5, and the escape inventory is backed only for the `raw.bin` segment. Version-table boundary 0.70 -> 0.74.
- **Upgraded guess/open question to source:** menu placement (it can cover rows **above** the cursor, so the recipe's "cut bot to curY" guard was wrong and is now `ok=false`); wide characters at the edge; the double-CPR question (unsent drops its emulator's replies).
- **Added (source):** Ctrl+O submits; double Ctrl+C within 2 s exits; numeric-argument prefix multiplies deletes and swaps the prompt for `(arg: N)`; selection cuts; Ctrl+Delete is kill-word; menu selection uses reverse video; the clipboard watcher overwrites the saved draft rather than keeping it; the full-width-row/hard-newline unwrap ambiguity; `{` block rows join the walk-up.
- **Added (measured, from `raw.bin`):** after Ctrl+C the abandoned draft is repainted grey `38;5;102` (`class:aborting`).
- **Downgraded to "measured (capture not retained)":** completion-menu screen and colours, 500-line paste timing, scroll-one-row, cursor position, Ctrl+L redraw, `--no-pretty`, startup times, the 34-line fill. The tmux server is gone and `raw.bin` does not cover them; the source agrees with each of them.
