# agy (Google Antigravity CLI): verified input-box profile

Tags: **measured** = seen on the real binary (in the lane's tmux captures, kept in the lane transcript, or re-checked in the files under `/tmp/agy-lane`); **source** = read in a file (binary strings, changelog, unsent code, install script); **docs** = antigravity.google docs; **guess** = inference nobody has checked.

Versions: every screen fact below is for **1.2.11** unless it says otherwise. See "Version comparison" for how thin the evidence on older versions is.

## What `agy` is

- `agy` is the Antigravity CLI, Google's terminal client for the Antigravity agent. `install.sh` puts it at `~/.local/bin/agy` (`TARGET_DIR="$HOME/.local/bin"`, `BINARY_PATH="$TARGET_DIR/agy"`). [source: install.sh, downloaded, not run]
- https://github.com/google-antigravity/antigravity-cli has no source code. The top level holds `.github`, `CHANGELOG.md`, `README.md`, `agy-cli-demo.gif` and `examples`, and HEAD is `6dadd6227a49905f475d22b7f0afe59493229595`. [source: re-checked with `gh api`]
- Release pace: 54 GitHub releases, from 1.0.4 (2026-06-01) to 1.2.11 (2026-09-25), so about one every 2 days. [source: re-checked with `gh api .../releases`]
- the maintainer has an old install. `~/.gemini/antigravity-cli/` is dated July 2026 and `~/.local/bin/agy` no longer exists. [measured: re-checked with `ls`]
- The oh-my-claudecode plugin calls it as `agy` (`run-provider-advisor.js:12,47`). [source, not re-checked]

## How the lane measured it

- Binaries:
  - 1.2.11 is at `/tmp/agy-lane/pkg/antigravity`, run through a symlink at `/tmp/agy-lane/bin/agy`.
  - The throwaway HOME is `/tmp/agy-lane/home`.
  - Everything ran in a private tmux server (`tmux -L agylane`), under `script -q -F` so the raw bytes were recorded.
- Getting past login:
  - `modelProvider: "gemini"`, `GEMINI_API_KEY=fake-not-a-key` and `GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:9` reach the input box without a login.
  - CHANGELOG 1.1.13 documents this path. [source; measured]
  - `trustedWorkspaces` was pre-set, so the trust prompt never needed an Enter.
- No prompt was submitted. [measured, per the transcript]
- **Most box measurements ran with `altScreenMode: "always"` set** (tmux `alt=1`). Inline mode (`alt=0`) was covered for these:
  - first-row scroll indicators
  - resize
  - Ctrl+G
  - history recall
  - mode hints
  - Ctrl+C
  - the final default-mode checks

  The lane says the box looks the same in both modes. That holds for the cases it checked in both. [measured]
- The captures are **not saved as files**. `raw.typescript` was overwritten on every launch, so each directory keeps only its last session. The pane captures exist only as tool output in the lane transcript (`.../wf_8ad8dc33-6ac/agent-aeaeb5e34100b981c.jsonl`). Keep that in mind before quoting a number as reproducible. [measured]

## TUI stack

- It is a Go binary built with `go1.28-20260721-RC03`, from google3 paths under `jetski/cli/...`. [measured: `go version -m`]
- Libraries found in the binary's strings [measured, re-checked in `strings.txt`]:
  - `charm_land/bubbletea/v2` (including `cursed`, and `googleCursedRenderer` appears 26 times)
  - `charm_land/bubbles/v2` (`textarea`, `textinput`, `viewport`, `cursor`)
  - `lipgloss/v2`
  - `charmbracelet/ultraviolet` (`uv.TerminalRenderer`, `uv.ScreenBuffer`)
  - `charmbracelet/x/ansi`
- The prompt editor is Google's own `jetski/cli/editing.EditArea`. It has these methods: `SetPromptFunc`, `SetVirtualCursor`, `SetAtomicRanges`, `SetValueWithUndo`, `UndoDepth`/`RedoDepth`, `EnableVim`/`SetVimMode`/`SetVimInsertFirst`. [measured: symbols re-checked]
  - The lane listed `OpenEditor` as an `EditArea` method. `OpenEditor`/`OpenEditorAtLine` do exist as symbols, but not on `EditArea`. [measured; corrected]
- unsent's `screen.go:69` reads only `uv.AttrFaint`. It does not keep any foreground colour today, so an `agyBox` reader needs `screen.go` to also record each cell's `Style.Fg`. [source: screen.go:55-73]

## Screen mode

- **1.2.11 draws inline on the main screen by default**, in every case tried: tmux 3.7c, `TERM=xterm-256color`, `TERM_PROGRAM` set to ghostty, iTerm.app, WarpTerminal or Apple_Terminal, and `TMUX` unset. The log still printed `Terminal detection: ... altScreen=true ssh=false`. [measured, 1.2.11 only; see the correction under "Version comparison"]
  - The docs say the default is adaptive: "Uses Alt-screen on advanced local terminals and degrades to inline mode over SSH". [docs, re-fetched]
  - All of these runs happened *inside tmux*, and faking `TERM_PROGRAM` does not change the terminal that actually answers the queries (tmux). The lane's idea of a server-side flag is one explanation. Another is detection based on the real terminal's replies. Only a check in a real Ghostty or iTerm2 window outside tmux can tell. [guess]
- `altScreenMode: "always"` gives `ESC[?1049h` plus mouse modes `?1002h ?1006h`. `"never"` gives inline. [measured for `always`; docs for `never`]
- **Startup flash:** before the inline UI, it enters the alternate screen (`?1049h`), draws the "not signed in / Press ctrl+c or ctrl+d twice to exit" screen, then sends `ESC[>4m ESC[<1u ESC[?1049l` and draws the inline frame after `\r ESC[J`. [measured: re-checked in the byte stream of the surviving 1.2.11 typescripts]
- **Inline exit:** it sends `\r ESC[J`, which erases the frame and the box with it. The transcript stays in scrollback. [measured]
- **Synchronized output:**
  - It queries `ESC[?2026$p` and `ESC[?2027$p`, then wraps redraws in `?2026h … ?2026l`.
  - The raw `h` and `l` counts in one session were 8–13 against 13–14. The lane's regex counted some `h` marks as separate tokens, so the pairing looks strict but was never proven.
  - A reader should wait for "no open `?2026h`" and not count pairs.
  - In the run with `SSH_CONNECTION` set, neither the query nor any mark appeared.

  [measured]
- **Input modes it requests:** bracketed paste `?2004h`, modifyOtherKeys 2 `ESC[>4;2m`, and kitty keyboard flags 1 `ESC[>1u` (plus the query `ESC[?u`). It re-sends `?2004h ESC[>4;2m` on nearly every redraw. The binary has an `AGY_CLI_DISABLE_INPUT_MODE_REASSERT` switch that turns this off. [measured; source]

## The input box (1.2.11, 100 columns, dark theme)

```
────────────────────────────────────────────────  (U+2500 across every column, fg #3C4043; light #DADCE0)
> do nothing; reply ok                            ('>' fg #8AB4F8 (light #4285F4) + ASCII space)
  line two                                        (continuation: 2-space indent)
────────────────────────────────────────────────
? for shortcuts                     Gemini 3.1 Pro · low   (footer, fg #9AA0A6)
```

Raw bytes of an empty box (from the surviving typescript) [measured]:

```
ESC[38;2;60;64;67m────…ESC[m
ESC[38;2;138;180;248m>ESC[m
ESC[38;2;60;64;67m────…ESC[m
ESC[38;2;154;160;166m? for shortcutsESC[m … ESC[38;2;154;160;166mGemini 3.1 Pro · lowESC[m
```

Every screen fact below is **measured on 1.2.11** unless its line says otherwise.

- **Rules:** both rules are full-width U+2500. The banner above them has no full-width rule.
- **Glyph:** an ASCII `>` followed by a normal space. This differs from Claude Code, which uses an NBSP.
- **Bash mode:** typing `!` as the first character turns the glyph into a bold `!` in #8AB4F8. The `!` is not part of the text, and the footer reads `activated bash mode · esc to cancel`.
- **Mode hint:** after Shift+Tab, a gray hint sits inside the box.
  - Raw bytes: `ESC[38;2;138;180;248m>ESC[39m ESC[38;2;154;160;166mAccept-edits mode: file edits auto-approved (shift+tab to cycle)ESC[39m`.
  - The other hint is `Plan mode: research & plan only (shift+tab to cycle)`.
  - The hint is not SGR 2 (dim), and the cursor stays at column 2. The hint colour in the light theme (#5F6368) is from the lane and was not re-checked.
- **Draft colours:** typed text uses the default foreground. A leading `/command` is shown in #8AB4F8 and is still draft text. The lane did not keep a byte capture of the `/command` colour.
- **Wrapping:**
  - Text width is cols − 3.
  - Text wraps at word boundaries, and a single long token hard-breaks at cols − 3 (150 `x` split into 97 + 53).
  - Wide characters are never split (48 `漢` per row at 100 columns).
- **Height cap:** the box shows at most floor(rows/2) text rows. Checked pairs: 20→10, 30→15, 50→25, 80→40.
- **Scroll indicators:**
  - Once past the cap, the first row reads `> ↑ N more lines` and the last reads `  ↓ N more lines`. Both are #9AA0A6. Raw bytes: `ESC[38;2;138;180;248m>ESC[39m ESC[38;2;154;160;166m↑ 16 more linesESC[39m`.
  - N counts wrapped screen rows, not logical lines. 1 line + a 159-char line (2 rows) + 30 lines made 33 rows, and the box showed 15 of them with `↑ 18`.
  - It scrolls one row at a time.
  - Only the English wording `more lines` was observed, and it is plural even for 1 (`↑ 1 more lines`).
- **Cursor:** the real terminal cursor sits at the insertion point.
  - While `press ctrl+c again to exit` is showing, the cursor moves to the footer row.
  - `SetVirtualCursor` exists as a symbol, but nobody knows what turns it on. [source; trigger unknown]
- **Resize:** after shrinking a running inline session from 30 to 20 rows, the bottom rule stayed off-screen. A fresh start at 20 rows fit.
- **Rows that look like the box:**
  - The `/` dropdown and the `@` file picker draw rows starting with `> ` below the bottom rule.
  - `?` on an empty box replaces the screen with a shortcuts panel.
  - `AGY_CLI_FAST_PROMPT_EDITOR=1` makes no visible difference.

## Pastes (bracketed, via `tmux paste-buffer -p`)

Everything in this section is **measured**, except where a line is marked as an inference.

- **Line-count rule:** a paste with more than min(15, floor(rows/2)) logical lines becomes `[Pasted text #N +M lines]`.
  - Checked points: at 20 rows, 10 lines stay inline and 11 become a placeholder. At 30 rows, 15 inline and 16 placeholder. At 50 rows, 15 inline and 16 placeholder. At 80 rows, 15 inline.
  - **16 lines at 80 rows was not tried.** The 15 cap at 80 rows is inferred from 50 rows.
  - 8 lines of 150 characters stayed inline, so logical lines count, not screen rows.
- **Character rule:** if any single line is longer than 1000 characters, the whole paste becomes `[Pasted text #N M chars]`, where M is the total character count including newlines.
  - 1000 characters stayed inline and 1001 gave a placeholder.
  - `é`×600 (1200 bytes) stayed inline, and `é`×1001 gave `1001 chars`. So it counts characters, not bytes.
  - `a\n`+`b`×1200+`\nc` gave `1204 chars`.
  - Same result at 200 columns.
- **No total-size limit:** 15 lines of 900 characters stayed inline (`↑ 135 more lines`).
- **Placeholder behaviour:**
  - It uses the default foreground (`ESC[38;2;138;180;248m>ESC[39m [Pasted text #1 +200 lines]`).
  - Placeholders are numbered per session.
  - One Backspace deletes a placeholder whole.
  - A trailing newline in a paste was dropped in one case (`a\nb\n`).
- **Format strings in the binary:** exactly two, `[Pasted text #%d +%d lines]` and `[Pasted text #%d %d chars]`. [source: re-checked in strings]
- **unsent gap:**
  - `paste.go:16` matches `\[Pasted text #\d+(?: \+(\d+) lines?)?\]`.
  - It does **not** match the `M chars` form, so that placeholder would be saved unexpanded until the regex is widened.

  [source]

## Keys (1.2.11)

- **Newline:**
  - `ctrl+j` and `Esc Enter` (alt+enter). [measured]
  - `shift+enter`: in the default keybindings and the docs, but not keyed through tmux. [source + docs]
  - `\`+Enter: "The CLI automatically removes the backslash and inserts a newline". [docs, re-fetched; not keyed]
  - Apple Terminal: `Option+Enter`. [docs]
- **Remapping:** `prompt.insert_newline` in `keybindings.json` (the maintainer's July file: `["alt+enter", "ctrl+j", "shift+enter"]`). [source]
- **Deleting before the cursor:**
  - Backspace and Ctrl+H delete one character.
  - Ctrl+W and Alt+Backspace delete one word, and the trailing space stays. Since 1.2.6 they stop at punctuation (CHANGELOG 1.2.6).
  - Ctrl+U deletes to the start of the line. At column 0 it removes the previous newline: the cursor moved from col 2 to col 12 on the same screen row and the hidden count fell by 1.

  [measured; the column-0 case measured indirectly]
- **Deleting after the cursor:** Delete and Ctrl+D (on non-empty text) delete one character, Alt+D deletes one word, and Ctrl+K deletes to end of line. [measured, alt-screen session]
  - In the maintainer's July keybindings `ctrl+k` is also bound to `subagent.approve_fast`. It probably only applies while a subagent is waiting. [source; context guess]
- **Ctrl+D on an empty box exits** (`cli.exit: ["ctrl+d"]`). [source + docs; not keyed]
- **Keys that bring text back:**
  - Undo `ctrl+_` / `ctrl+shift+-`, redo `ctrl+shift+z`, yank `ctrl+y`. [source: keybindings.json]
  - Vim mode (off by default) was added in **1.1.11**; counted operators came in **1.2.9**. [source: CHANGELOG; corrected from "1.2.8"]
- **Esc and a single Ctrl+C keep the draft.** A second Ctrl+C exits and the draft is lost. [measured]
- **Kitty and modifyOtherKeys:** agy requests both. Whether the user's outer terminal then sends Ctrl+W as `CSI 119;5u` into unsent's pty depends on that terminal and on whether unsent forwards the request. [measured request; the rest is guess]
- **unsent `deleteKeys` gaps for agy** [source: wrap.go:503-529]:
  - Alt+D (`ESC d`) and Ctrl+D (`0x04`) delete text and are not listed.
  - Alt+Backspace (`ESC 0x7f`) is counted as one character, but it deletes a word.
  - CSI-u and modifyOtherKeys encodings are not parsed.

## External editor (ground truth)

- Ctrl+G runs `$EDITOR`/`$VISUAL` on `$TMPDIR/jetski-prompt-<n>.txt`. It works on a non-empty box, even though the docs say "inside the empty prompt panel". [measured; docs re-fetched]
- The file is the exact draft, with no trailing newline and paste placeholders expanded. [measured, re-checked]
  - `/tmp/agy-lane/editor-copy.txt` = `head ` + 200 pasted lines + ` tail`: 199 newlines, and it ends in `r200 tail` with no final `\n`.
- After the editor returns, the box holds the expanded text and the temp file is deleted. [measured]

## Built-in persistence

- `~/.gemini/antigravity-cli/history.jsonl` stores **submitted** prompts, one per line: `{"display", "timestamp", "workspace"[, "conversationId"]}`. [measured: format re-checked on the maintainer's file, contents not read]
- Up and Down recall history only on an empty box. With a draft, they leave it untouched. [measured, inline]
- **Unsent drafts are not saved anywhere.** After exiting with a 37-row draft and with a 200-line pasted draft, a grep of the throwaway HOME found no draft text. [measured]

## Version comparison (corrected)

- The lane downloaded 1.1.13 and 1.2.0. **Each auto-updated itself to 1.2.11 on its first launch, even with `AGY_CLI_DISABLE_AUTO_UPDATE=1` exported** [measured, re-checked]:
  - The logs show `auto_updater.go:305] Spawned background update process` at 18:04:18 and 18:04:48.
  - `updater/update_status.json` reads `"Update successful, restart CLI to use"`.
  - The originals were renamed to `antigravity.<ns>.old`.
  - `old/1.1.13/antigravity` and `old/1.2.0/antigravity` now have the same SHA-1 as 1.2.11 (`bb119dc8…`).
  - The second launches, at 18:05:08 and 18:05:19, logged `Language server version: 1.2.11`, and their typescripts show the `Antigravity CLI 1.2.11` banner.
- What this means:
  - **Holds:** one session per old version, in `altScreenMode: always`, at 100×30, showed the same box. Rules, `> `, 2-column indent, a 15-row cap and `↑ N more lines` (1.1.13: `↑ 18` with 15 rows visible). The footer read `Gemini 3.1 Pro` in 1.1.13 and `Gemini 3.1 Pro · low` in 1.2.0. [measured]
  - **Invalid:** "1.1.13 default: alt=0, 1.2.0 default: alt=0". Those checks ran on the auto-updated 1.2.11 binary. The default screen mode of the old versions is unknown.
  - Paste thresholds, keys, colours and Ctrl+G were measured on 1.2.11 only.
- **The kill switch did not work as the lane used it.** The binary contains the name `AGY_CLI_DISABLE_AUTO_UPDATE`, but with value `1` the update still ran on 1.1.13 and 1.2.0. The value it accepts, if any, is unknown. The lane's claim that the variable "turns it off" is withdrawn. [measured; source]
- History of the placeholder, long-line guard and Vim mode in the changelog [source: CHANGELOG, re-checked; the lane's version numbers corrected]:
  - `[Pasted text #X +Y lines]` first appears in the **1.0.1** notes (the lane said 1.0.2).
  - The long-line paste guard came in **1.0.8**.
  - Atomic placeholder delete came in 1.1.9, and cursor misalignment on wrap was fixed in 1.1.17.

## Detection recipe for an `agyBox` reader

1. **Find the box.**
   - Scan upward for an all-`─` (U+2500) row R whose next row starts with `>` or a bold `!` in col 0 and a space in col 1.
   - Find the next all-`─` row B below it. The box is rows R+1 … B−1.
   - Ignore `> ` rows below B (dropdowns).
   - If there is no B, return no match and keep the last draft.
2. **Text position.** Text starts at column 2 on every row, and continuation rows have spaces in columns 0–1.
3. **Hint colour.** Read hintFg from the footer's `? for shortcuts` or status text. Do not hard-code it: it is #9AA0A6 in the dark theme and #5F6368 in the light theme (lane).
   - First row in hintFg matching `↑ (\d+) more lines?`: rows hidden above, and the row holds no text.
   - Last row matching `↓ (\d+) more lines?`: rows hidden below.
   - Both counts are wrapped screen rows.
4. **Empty box.** The box is empty if its single row is blank, or holds nothing but hintFg text (the mode hint). There is no SGR 2 anywhere. Keep default-fg text and a blue leading `/command` as draft.
5. **Join rows.** Rejoin rows at wrap width cols−3: word wrap, long tokens hard-broken, wide characters never split.
6. **Bash mode.** A bold `!` glyph means the draft is `!` + text.
7. **Placeholders.** Match `\[Pasted text #(\d+) \+(\d+) lines\]` and `\[Pasted text #(\d+) (\d+) chars\]`, then expand each from the captured bracketed paste.
8. **Screen mode.**
   - Inline by default in 1.2.11 in tmux. Handle `?1049h` for `altScreenMode: always` or an adaptive default.
   - Allow for the startup alternate-screen flash.
   - Never read while a `?2026h` is open.
9. **Delete keys.** Add `ESC d`, `0x04` and word-sized `ESC 0x7f` to `deleteKeys`, and parse the CSI-u and `CSI 27;m;k~` forms.
10. **Ground truth for tests.** Set `EDITOR` to a copy-and-exit script and press Ctrl+G. The file holds the exact draft, placeholders expanded, with no trailing newline.

## Risks

- **Closed source, fast releases, aggressive auto-update.**
  - Each fact has to be measured again on the real binary. Releases come about every 2 days.
  - The auto-update runs in the background on launch and replaced the binary within seconds.
  - `AGY_CLI_DISABLE_AUTO_UPDATE=1` did not stop it.

  [measured]
- **Colour is the only way to tell hint from draft.**
  - There is no SGR 2 dim; hints and indicators are truecolor gray that changes with the theme.
  - A leading `/command` is coloured but is still draft text.
  - unsent does not record foreground colour today.

  [measured; source]
- **The `> ` glyph is plain ASCII** and also starts dropdown rows, so the reader has to anchor on the two rules. [measured]
- **Inline mode:** exiting erases the frame, and a live shrink can push the bottom rule off-screen. [measured]
- **Terminal key encodings:** kitty and modifyOtherKeys forms can hide delete keys from `deleteKeys`, and Alt+D and Ctrl+D are missing from it. [source; the encoding part is guess]
- **No built-in draft save:** exiting loses the draft. [measured]
- **Housekeeping:**
  - `/tmp/agy-lane` is **1.1 GB**, not the ~560 MB the lane reported. It holds five agy binaries (two auto-updated copies plus two `.old` files), the tarballs and a 36 MB `strings.txt`. [measured: `du -sh`]
  - The lane also reported that the login keychain's mtime moved once during its runs, and that it could not attribute this to agy. Not re-checked.

## Open questions

- Is the default alternate screen real in a logged-in Ghostty or iTerm2 session outside tmux? And what was the old versions' default, now that the lane's "old default" checks turned out to be 1.2.11?
- Where does the box sit once a conversation exists, in both modes?
- `\`+Enter and `shift+enter` delivery were not keyed.
- Is the trailing-newline drop general? Does placeholder numbering reset after a submit? What happens with 16 lines at 80 rows?
- What turns on `SetVirtualCursor`, and does Vim mode change the glyph or indent?
- Which value, if any, makes `AGY_CLI_DISABLE_AUTO_UPDATE` work?

## Verification notes

Checked:

- **Old-version binaries** (`old/*/antigravity`, `*.old`):
  - Read their SHA-1s, `go version -m`, the updater status files and the `cli-*.log` files.
  - This showed that both "old" binaries were replaced by 1.2.11 during their first launch, despite `AGY_CLI_DISABLE_AUTO_UPDATE=1`.
  - It also showed that the lane's "1.1.13/1.2.0 default: alt=0" results came from 1.2.11.
- **The lane transcript** (`wf_8ad8dc33-6ac/agent-aeaeb5e34100b981c.jsonl`):
  - Read the pane captures behind: the paste thresholds (20/30/50 rows; 80 rows only at 15 lines), the 1000/1001-char rule, `é` counting, `1204 chars`, the delete-key probes, history recall, the mode-hint bytes, the old-version sessions and the TERM_PROGRAM sweep.
  - Found that most box measurements ran with `altScreenMode: always`.
- **Binary strings:** confirmed the Bubble Tea v2, bubbles v2, lipgloss v2, ultraviolet and `EditArea` symbols. Confirmed the two placeholder format strings, the `%d more lines` string and the `AGY_CLI_*` env names. `OpenEditor` is not an `EditArea` method.
- **CHANGELOG sections:** corrected the placeholder origin to 1.0.1, Vim mode to 1.1.11 and counted operators to 1.2.9. Confirmed `GEMINI_API_KEY` in 1.1.13 and punctuation word-delete in 1.2.6.
- **Docs, re-fetched:** confirmed the adaptive altScreen quote, the `\`+Enter wording, the `ctrl+g` "empty prompt panel" wording and Option+Enter. The docs say nothing about disabling auto-update.
- **GitHub:** confirmed HEAD `6dadd62…`, no source in the repo, and 54 releases from 2026-06-01 to 2026-09-25.
- **Files:** confirmed the Ctrl+G output file (`editor-copy.txt`, no trailing newline, placeholder expanded). Confirmed the maintainer's `keybindings.json` and the `history.jsonl` format, without reading the prompt text.
- **unsent code:** `screen.go:69` reads faint only. `paste.go:16` misses the `chars` form. `wrap.go` `deleteKeys` misses Alt+D, Ctrl+D and word-sized Alt+Backspace.

Changed:

- Withdrew the old-version default-mode result and the claim that `AGY_CLI_DISABLE_AUTO_UPDATE` disables updates.
- Narrowed the version-stability claim to one alt-screen session per old version.
- Marked the 80-row paste cap as inferred.
- Corrected three changelog version numbers and the disk size (1.1 GB).
- Noted that most measurements used alt-screen mode and that captures live only in the transcript.
- Said not to rely on strict `?2026` pairing.
- Added the unsent-side gaps (fg colour, the `chars` regex, missing delete keys) and the `ctrl+k` subagent binding.
- Dropped the lane's first open question, a user message the harness relayed that has nothing to do with agy. The parent session should handle it.

Not re-checked:

- The light-theme colours, and the `/command` colour bytes.
- The stale-frame-on-resize capture, beyond the transcript.
- The SSH-run sync result, and the keychain mtime note.

Nothing was launched. No agy or other agent CLI was run for this verification.
