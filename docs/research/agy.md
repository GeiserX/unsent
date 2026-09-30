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
- **Bash mode:** typing `!` as the first character turns the glyph into a bold `!` in #8AB4F8. The `!` is not part of the text, and the footer reads `activated bash mode · esc to cancel`. (Ctrl+G on 1.2.13 proves the first half. See [Bash mode, measured](#bash-mode-measured-2026-09-30).)
- **Mode hint:** after Shift+Tab, a gray hint sits inside the box.
  - Raw bytes: `ESC[38;2;138;180;248m>ESC[39m ESC[38;2;154;160;166mAccept-edits mode: file edits auto-approved (shift+tab to cycle)ESC[39m`.
  - The other hint is `Plan mode: research & plan only (shift+tab to cycle)`.
  - The hint is not SGR 2 (dim), and the cursor stays at column 2. The hint colour in the light theme (#5F6368) is from the lane and was not re-checked.
- **Draft colours:** typed text uses the default foreground. A leading `/command` is shown in #8AB4F8 and is still draft text. The lane did not keep a byte capture of the `/command` colour. (On 1.2.13 a `/command` in the box is **not** coloured: see "Profile run on agy 1.2.13".)
- **Wrapping:**
  - Text width is cols − 3.
  - Text wraps at word boundaries, and a single long token hard-breaks at cols − 3 (150 `x` split into 97 + 53).
  - Wide characters are never split (48 `漢` per row at 100 columns).
  - A word that would end exactly at the width moves to the next row, since the space after it has to fit there too, and a line that ends exactly at the width gets no row of its own [measured on 1.2.13; see "Profile run on agy 1.2.13"].
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
4. **Empty box.** The box is empty if its single row is blank, or holds nothing but text in a colour of agy's own (the mode hint); a draft, paste placeholders included, is the terminal's own foreground. (1.2.13 does use SGR 2, for the model name in the footer, and draws no `/command` colour in the box.)
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

## Capture run on agy 1.2.13 (measured 2026-09-29)

**Environment.** agy 1.2.13, published 2026-09-29 and the newest release that
day. The `agy_cli_mac_arm64.tar.gz` tarball was downloaded straight into a
scratch folder on a Mac mini with macOS 26.6.1, unpacked there and run by its
full path through a symlink named `agy`; `install.sh` was never run and
`~/.local/bin/agy` was never touched. It ran in a private tmux 3.6b server
(`tmux -L unsent-072 -f <scratch conf>`, `TERM=tmux-256color`, 120x40, one
capture at 120x20) with `extended-keys on` and `extended-keys-format csi-u`,
under `unsent capture agy` with `UNSENT_DEBUG_DIR` set. unsent has no agy
reader, so it printed its not-protected line and passed every byte through
while logging both directions; the debug folder stayed empty, and the capture's
own record is the raw log. The environment was `env -i` with a scratch `HOME`,
`TMPDIR` and XDG folders, `modelProvider: "gemini"` in the scratch
`~/.gemini/antigravity-cli/settings.json`, `GEMINI_API_KEY=fake-not-a-key`,
`GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:9`, `HTTPS_PROXY`, `HTTP_PROXY` and
`ALL_PROXY` on the same dead port, `AGY_CLI_DISABLE_AUTO_UPDATE=1`, no other
provider key, no `GITHUB_TOKEN`, `GH_TOKEN` or `CI`, and `USER` and `LOGNAME`
set to `rig`. `$EDITOR` and `$VISUAL` were unsent's copy-and-exit script. No
prompt ever reached a model: every submit failed on the dead endpoint. The 23
captures, 144 screens and 39 probes are in
[`testdata/agy/1.2.13/`](../../testdata/agy/1.2.13/), described in
[its README](../../testdata/agy/README.md); every conversation id below belongs
to a throwaway home.

**It did not replace itself.** The binary's SHA-256 is
`ef3728208834483754b06fd99963b4b6320740bf237212768dfed5256ae24ddb` before the
first run and after the last, with the same size and mtime, and no `.old` file
appeared beside it. The updater did try: `updater/update_status.json` reads
`{"success":false,"message":"Update failed, please install from website"}`, and
`updater/update.lock` was created. The dead proxy, not
`AGY_CLI_DISABLE_AUTO_UPDATE=1`, is what stopped it, so the research's warning
stands: block the network if the version has to stay put [measured, every
`sessions/*` probe].

### What 1.2.13 changes from the 1.2.11 profile above

| The profile above says | 1.2.13 does | Evidence |
| --- | --- | --- |
| It wraps redraws in `?2026h … ?2026l` | **No synchronized-output marks at all**: 0 in 23 records, although it still queries `ESC[?2026$p` and `ESC[?2027$p` once per start. tmux answered neither query through unsent. So an agy reader needs the quiet-gap read of [SPEC 2.11](../SPEC.md#211-performance) | every `.rec` |
| Ctrl+U deletes to the start of the line, Ctrl+K to the end, Alt+D and Ctrl+D delete a word or a character forward | Only Backspace, Ctrl+H, Ctrl+W, Alt+Backspace, Delete and Ctrl+U delete anything. **Ctrl+K, Ctrl+D, Alt+D, Ctrl+Delete, Alt+Delete, Ctrl+Backspace, Shift+Backspace and Shift+Delete do nothing** in the box; the shortcuts panel binds Ctrl+K to `Approve subagent fast` and Ctrl+D to `Exit` | `deletes.editor-1` to `-16` |
| A second Ctrl+C exits and the draft is lost | Still true, and **one Ctrl+D on an empty box only warns** `press ctrl+d again to exit`; the second quits with status 0. There is no key that clears the draft: Ctrl+C and Esc both keep it | `exit-ctrl-c`, `exit-ctrl-d`, `typed` |
| `[Pasted text #N +M lines]` and one Backspace deletes a placeholder whole | Both hold. Adds: **the numbering never renumbers** when an earlier placeholder is deleted, it keeps counting up within a session, and **the external editor round trip expands every placeholder into the box and resets the counter to 1** | `pastes`, `pastes2` |
| `conversationId` is a field of `history.jsonl` | It is, but **only from the second submit on**: the entry a new conversation's first submit writes has `display`, `timestamp` and `workspace` and no id, because the conversation is created by that submit | `identity`, `submit` |
| 1.2.11 draws inline by default, after an alternate-screen flash at start | Inline confirmed, in every probe taken with the box showing. The flash is **not always there**: 10 of 23 records enter and leave the alternate screen once, 13 never touch it | every `.rec`, every `sessions/*` probe |

Unchanged and re-seen: the two full-width U+2500 rules, the ASCII `>` and one
space, continuation rows indented two, text width `cols − 3`, word wrap with
wide characters never split, a cap of `floor(rows/2)` text rows, `↑ N more
lines` and `↓ N more lines` counting wrapped screen rows, the footer
`? for shortcuts` on an empty box and `Gemini 3.1 Pro · low` on the right, the
mode hints after Shift+Tab, bash mode's `!` glyph, and the paste thresholds
[measured, `tall`, `accents`, `scroll-steps`, `dialogs`, `pastes`].

### Screen, modes and frames

- **Inline on the main screen.** tmux's `alternate_on` was 0 in every probe
  taken with the box showing, and the not-protected line unsent prints before
  the start is still above the box on the screen and in the scrollback at exit
  [measured, every probe and every `-exit` screen].
- **The startup flash**, when it happens, is one
  `ESC[>4m ESC[?1049h ESC[?25l ESC[?5W ESC[?2004h ESC[>4;2m ESC[>1u ESC[?u ESC[H ESC[2J`,
  the logo, then `?1049l` and the inline frame. 10 records of 23 have exactly
  one `?1049h`/`?1049l` pair, and what decides it is not known [measured].
- **A first run stays on the alternate screen** for the colour-scheme picker,
  the terms screen and the trust dialog, and leaves it when the box is drawn
  [measured, `first-run`].
- **Input modes.** It asks for bracketed paste (`?2004h`), modifyOtherKeys 2
  (`ESC[>4;2m`) and kitty flags 1 (`ESC[>1u`), sends the kitty query `ESC[?u`,
  and re-asserts `?2004h ESC[>4;2m` while idle: 525 and 536 times in 23
  records. It turns on no mouse tracking at all. On exit it writes
  `ESC[>4m ESC[<1u`, then `\r\n\n ESC[J ESC[?2004l ESC[0 q`, which erases the
  rows below the box and leaves the box and the draft in the scrollback
  [measured].
- **The box is drawn about 0.1 s after start** once the config folder exists:
  the first frame holding `? for shortcuts` is at 0.09 s in `identity` and
  0.11 s in `typed` [measured]. A first run takes as long as the person
  answering the onboarding screens.
- **Reading a frame.** agy brackets each paint with `ESC[?25l` … `ESC[?25h`:
  1,235 paints in the 23 records, every one of them closed. 73 chunks continued
  a paint that had started, 12 of those being the idle mode re-assert and 61
  real output, and 18 of those 61 arrived more than 50 ms after the chunk
  before, up to 1.68 s, all of them inside a large panel such as the shortcuts
  list. So a plain quiet-gap read tears those panels, while `ESC[?25h` marks
  the end of every paint [measured, all records]. The quiet gap between one
  paint's end and the next paint's start has a median of 83 ms, but that
  measures the rig's typing pace, not agy.

### Box, wrapping and text

- Text width 117 at 120 columns: 115 `a`s and then `漢字` put `漢` at the edge
  and `字` on the next row, leaving nothing behind; a 114-`x` word followed by
  ` ñandú` wrapped at the space [measured, `accents`].
- Precomposed accents (`ñandú`, `¿qué pasó?`, `Pingüino`), decomposed ones
  (`e` + U+0301, `n` + U+0303, `u` + U+0308), `👍🏽` and the flag `🇪🇸` came
  back byte-identical through Ctrl+G [measured, `accents.editor-1.txt`].
- **Height cap `floor(rows/2)` text rows**, plus an indicator row above and
  below when there is more: at 40 rows a 45-line draft showed
  `> ↑ 4 more lines`, 20 text rows and `  ↓ 21 more lines` between the rules
  [measured, `tall`].
- **The indicators count wrapped screen rows, not logical lines**: a draft of
  27 lines, one of them 250 characters wide and so three rows, showed
  `↑ 9 more lines` with 20 rows visible, which is 29 − 20 [measured,
  `scroll-steps`].
- **The view follows the insertion point.** After 40 Up presses in a 45-line
  draft the cursor's row was the first visible one and the box had scrolled by
  exactly that much; Up, PageUp and Down inside the visible rows move nothing.
  Ctrl+Home jumps to the top (`↓ 9 more lines` under the last row) and
  Ctrl+End back to the bottom [measured, `tall`, `scroll-steps`].
- A 45-line draft keeps what is out of sight: an edit typed 40 rows up and a
  Ctrl+W 30 rows up were both in the editor copy taken after scrolling back
  down [measured, `tall.editor-1.txt`, `tall.editor-2.txt`].

### Keys as they reached agy in tmux (modifyOtherKeys 2, `extended-keys-format csi-u`)

| Key | Bytes | What agy did (editor copy or screen) |
| --- | --- | --- |
| Enter | `CR` | **submit** |
| Ctrl+J | `ESC[106;5u` | new line |
| Alt+Enter | `ESC[13;3u` | new line |
| Shift+Enter | `ESC[13;2u` | new line |
| Esc then Enter | `ESC CR` in one read | new line |
| `\` then Enter | `\`, then `CR` | new line, `\` dropped |
| Backspace | `0x7f` | one character back |
| Ctrl+H | `ESC[104;5u` | one character back |
| Ctrl+W | `ESC[119;5u` | one word back, the trailing space stays |
| Alt+Backspace | `ESC[127;3u` | one word back |
| Ctrl+Backspace | `ESC[127;5u` | nothing |
| Delete | `ESC[3~` | one character forward |
| Ctrl+D (in a draft) | `ESC[100;5u` | nothing |
| Alt+D, Ctrl+Delete, Alt+Delete | `ESC[100;3u`, `ESC[3;5~`, `ESC[3;3~` | nothing |
| Shift+Backspace, Shift+Delete | `ESC[127;2u`, `ESC[3;2~` | nothing |
| Ctrl+U | `ESC[117;5u` | kills to the start of the line; at column 0 it eats the newline, so it is the only way to clear a draft |
| Ctrl+K | `ESC[107;5u` | nothing (`Approve subagent fast`) |
| Ctrl+Y | `ESC[121;5u` | yanks the last Ctrl+U kill back |
| Ctrl+_ | `ESC[95;5u` | undo |
| Ctrl+Shift+Z | `ESC[122;6u` | redo |
| Ctrl+C | `ESC[99;5u` | keeps the draft, `press ctrl+c again to exit`; the second quits |
| Esc | `ESC` | keeps the draft; closes a panel; interrupts a running turn |
| Ctrl+D (empty box) | `ESC[100;5u` | `press ctrl+d again to exit`; the second quits, status 0 |
| Ctrl+G | `ESC[103;5u` | external editor |
| Ctrl+Z | `ESC[122;5u` | nothing (below) |
| Home, End, Ctrl+Home, Ctrl+End | `ESC[1~`, `ESC[4~`, `ESC[1;5H`, `ESC[1;5F` | move the insertion point |

[measured, `deletes.rec`, `multiline.rec`, `submit.rec`, `typed.rec`,
`exit-ctrl-c`, `exit-ctrl-d`; the effects from `deletes.editor-1` to `-16`,
`multiline.editor-1` and the screens]. tmux does not answer `ESC[?u`, so these
are tmux's modifyOtherKeys 2 encodings in its CSI-u form and no kitty release
event was seen.

**Enter is the only submit key.** Alt+Enter, Shift+Enter, Ctrl+J, Esc then
Enter and `\` then Enter all make a new line, each checked on a draft that was
then left in the box [measured, `submit`, `multiline`]. That matters for
[SPEC 2.13](../SPEC.md#213-sent-drafts-and-the-sent-log): agy's profile lists
one submit key, and the `ESC CR` pair that pi reads as a submit is a new line
here.

**Ctrl+Z.** The shortcuts panel binds it to `Suspend CLI`, but under tmux's
extended keys it arrives as `ESC[122;5u`: unsent does not match that form and
agy did not act on it either, so the draft stayed and the typing that followed
went into the box [measured, `suspend`]. As the lone byte `0x1a` unsent takes
it, writes nothing (agy has no profile yet) and asks the kernel to stop the
process group; in the rig that group has no job-control shell, so the kernel
dropped the SIGTSTP and agy repainted when unsent continued it, the same trap
pi's run hit [measured, `suspend-legacy`]. What a real terminal under a shell
does was not measured.

### Pastes

- **Line rule.** More than `min(15, floor(rows/2))` logical lines becomes
  `[Pasted text #N +M lines]`, where M is the line count. At 40 rows 15 lines
  stayed inline and 16 became a placeholder; at 20 rows 10 stayed inline and 11
  became one [measured, `pastes`, `pastes-20rows`].
- **Character rule.** A line over 1,000 characters becomes
  `[Pasted text #N M chars]`: 1,000 stayed inline and 1,001 gave
  `[Pasted text #1 1001 chars]` [measured, `pastes`, `pastes2`].
- **Numbering.** It counts up per session and does not renumber: with
  `#1 keep #2` in the box, one Backspace over `#1` left `#2` as `#2`, and the
  next paste took `#3`. Deleting a placeholder and pasting again from an empty
  box gave `#2` then `#3` [measured, `pastes`, `pastes2`]. **A Ctrl+G round
  trip resets the counter to 1**, because it leaves no placeholder in the box
  [measured, `pastes`].
- **One Backspace right after a placeholder deletes it whole**, for both forms
  [measured, `pastes2`: `chars-1001` then `after-backspace` empty, `lines-16`
  then `lines-16-after-backspace` empty].
- **Editing inside a placeholder works on the pasted text.** Ctrl+U at the end
  of a `+16 lines` placeholder killed it in one press; after a Ctrl+G had
  expanded the same paste into plain text, five Ctrl+U left 13 lines drawn
  inline [measured, `pastes`].
- **A trailing newline is dropped**: `one\ntwo\n` pasted gives the two lines and
  a 7-byte editor copy [measured, `pastes`, `pastes2`].
- **Line breaks arrive as `\n` either way**: a paste sent with CR (tmux's
  default) and one sent with LF (`paste-buffer -r`) both gave `one\ntwo`
  [measured, `pastes2`].
- **A tab becomes four spaces in agy's own buffer**: the editor copy of a paste
  holding `\t` has four spaces where the tab was [measured,
  `pastes2.editor-2.txt`]. A draft with a tab cannot go back into agy
  faithfully, which [SPEC 2.3](../SPEC.md#23-restore-in-box) condition 8 asks
  each profile to say.
- **The editor copy expands every placeholder**: a draft of
  `[Pasted text #1 1001 chars] middle [Pasted text #2 1001 chars]` gave exactly
  2,010 bytes [measured, `pastes.editor-4.txt`].

### External editor (ground truth)

Ctrl+G writes the draft to a file in `$TMPDIR` and runs `$EDITOR`. Every one of
the 42 copies in these captures is the exact draft, placeholders expanded, with
no trailing newline, and it works on a non-empty box and during a running turn
[measured, every `*.editor-*.txt`, `submit.editor-1.txt`]. After the editor
returns, the box holds the expanded text.

### A send that fails

Enter draws the prompt above the box under a shorter rule, with
`⡿  Generating...` under it and `esc to cancel` in the footer, and empties the
box at once. Against the dead endpoint it spins for at least 30 s with no error
message. Typing during the turn works and Ctrl+G copies that draft. Esc
interrupts and the transcript shows
`⎿  Interrupted · What should Antigravity CLI do instead?`, leaving the draft
in the box [measured, `submit`, `identity`, `ctrlc-history`].

### Session identity

| When | What is under `~/.gemini/antigravity-cli` | What another process can read | Tag |
| --- | --- | --- | --- |
| Start, before any key | `settings.json`, `installation_id`, `jetski_state.pbtxt`, `conversation_summaries.db`, an empty `conversations/` folder and a `log/cli-<time>.log`. No `history.jsonl`, no `cache/last_conversations.json` | the process holds its log, its crash log, `conversation_summaries.db` and the config folders open; nothing names a conversation | measured, `identity-id-0-start` |
| Typed, not sent; after Ctrl+C | nothing new | nothing new | measured, `id-1-typed`, `id-2-ctrl-c` |
| 0.3 s after the first submit | `conversations/<id>.db` with its `-wal` and `-shm`, `cache/last_conversations.json` mapping the working folder to `<id>`, and one `history.jsonl` line `{"display","timestamp","workspace"}` **with no `conversationId`** | the process holds `conversations/<id>.db` open, and the file name is the id | measured, `id-3-submit-0.3s`, `poll-identity.txt` |
| After a second submit | a second `history.jsonl` line, this one carrying `"conversationId":"<id>"` | the same | measured, `id-6-second-submit` |
| At exit | nothing more; the box and the draft stay in the scrollback | nothing | measured, `id-7-after-exit`, every `-exit` screen |

**So the running conversation's id can be read from outside**: list the files
the child pid holds open and take the `conversations/<id>.db`. For a new
conversation it appears with the first submit, 0.3 s after Enter; for a resumed
one it is open before the box is drawn [measured, `poll-resume-c.txt`,
`poll-resume-conversation.txt`]. `cache/last_conversations.json` holds the same
id keyed by the working folder, but it is shared by every agy in that folder,
so the open file is the one to read. Nothing is keyed by pid.

**Resume forms.**

| How it was opened | What it runs | What another process sees | Tag |
| --- | --- | --- | --- |
| `agy -c` (`--continue`) | the newest conversation of the folder, its earlier prompts redrawn above the box | its `conversations/<id>.db` open 0.37 s after start, before the box at 0.49 s | measured, `resume-c` |
| `agy --conversation <id>` | that conversation | its database open by the time the box is drawn, both at 0.11 s | measured, `resume-conversation` |
| `agy --conversation <unknown id>` | prints `warning: conversation "<id>" not found` above the banner and **starts a new conversation anyway**, which nothing on disk names until its first submit | nothing | measured, `resume-unknown` |
| `/resume` inside a session (aliases `switch`, `conversation`) | a picker drawn under the bottom rule, `Conversations`, with a `CLI` and an `Other` tab; on a home with no conversation it reads `No conversations available. Press Enter to return.` | not measured with a conversation to choose | measured, `dialogs` |

`agy --help` also lists `--new-project`, `--project`, `--agent`, `--model`,
`--effort`, `--mode`, `--sandbox`, `--add-dir`, `-p`/`--print` and `-i`, and the
subcommands `agent(s)`, `changelog`, `help`, `install`, `mcp`, `mic-serve`,
`models`, `plugin(s)`, `remote-control` and `update`. The chat starts are the
bare command, `-c`, `--conversation` and `-i`; the rest are not a chat box
[source, `agy --help`].

### Kill, close and what is never written

- `kill -9` of agy: unsent died with it, by the same signal, and the draft
  (`kill nine draft zebra`) is in no file under the scratch home or `TMPDIR`.
  unsent's exit lines printed over the draft's last row [measured, `kill9`].
- tmux `kill-session`: agy wrote its teardown on the hangup, both processes
  exited, none was left, and the draft is in no file [measured, `killsession`].
- Two Ctrl+C with a draft in the box: agy quits with status 0 and the draft is
  in no file [measured, `exit-ctrl-c`].
- **agy saves no unsent draft anywhere**, which is the whole reason unsent
  exists here: `history.jsonl` holds submitted prompts only, and a draft typed
  and never submitted left no trace in any of these runs [measured,
  `identity`, `kill9`, `killsession`, `exit-ctrl-c`].

### Negative screens

Each of these is a frame a reader must not take for a draft [measured,
`dialogs`, `first-run`, `ctrlc-history`]:

- the `?` shortcuts panel, drawn under the bottom rule with its own tabs
  (`Antigravity CLI`, `general`, `commands`, `shortcuts`);
- the `/` menu and the `@` file picker, both drawn **under** the bottom rule
  with rows that start `> `, while the box itself holds only the `/` or `@`
  that was typed;
- the mode hints inside the box after Shift+Tab
  (`Accept-edits mode: …`, `Plan mode: …`), which sit where the draft would be;
- bash mode: the glyph becomes `!` and the footer reads
  `activated bash mode · esc to cancel`. The box itself is a draft, and
  what it holds is the command without the glyph
  ([Bash mode, measured](#bash-mode-measured-2026-09-30));
- `/settings`, `/model`, `/context`, `/resume` and Ctrl+R (an `Artifacts`
  panel), all drawn under the bottom rule while the box stays empty;
- the first-run colour-scheme picker, the terms screen and the trust dialog,
  all on the alternate screen, none of which has the two rules.

### History browsing

Up on an empty box recalls the newest submitted prompt, Up again the one
before, and Down walks back; the entries come from `history.jsonl`, so they
include prompts from earlier sessions in the same home. **Up in a typed draft
leaves the draft alone** [measured, `ctrlc-history`, `editor-1` to `-3`].

### Open questions from above, answered where this run could

- **The default screen mode outside tmux:** still open. Inside tmux 1.2.13 is
  inline, with an alternate-screen flash at start in 10 of 23 records.
- **Where the box sits once a conversation exists:** in the same place,
  inline, under the redrawn prompts of the conversation [measured, `resume-c`].
- **`\`+Enter and `shift+enter` delivery:** both keyed, both make a new line.
- **Is the trailing-newline drop general:** yes in both cases tried.
- **Does placeholder numbering reset after a submit:** it resets after a Ctrl+G
  round trip; a submit was not tried with a placeholder still in the box.
- **16 lines at 80 rows:** not tried; 11 lines at 20 rows confirms the
  `floor(rows/2)` half of the rule.
- **Which value makes `AGY_CLI_DISABLE_AUTO_UPDATE` work:** still unknown. With
  `1` the updater still ran and only failed because the network was dead.
- **SPEC 2.3's agy row, "how the running session's id is read":** the
  `conversations/<id>.db` the process holds open (above).

### What this run could not capture

- A real terminal other than tmux, kitty flag-7 release events, and a Ctrl+V or
  right-click clipboard paste.
- A streaming reply, and the box's height cap during a turn: nothing can answer
  on the dead endpoint.
- The `/resume` picker with conversations in it, and `/fork`, `/new` and
  `/rename`.
- Vim mode, `SetVirtualCursor`, and the light theme's colours.
- The paste settle delay and a restore: unsent has no agy reader yet, so the
  first-frame paste and restore belong to the profile run.
- Every capture in this run ended with the rig closing the window, except
  `exit-ctrl-d`, `exit-ctrl-c`, `kill9` and `killsession`; the two exit
  captures are the ones that show agy's own quit.

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

## Profile run on agy 1.2.13 (measured 2026-09-30)

The same rig as the capture run above, on the same Mac mini and the same
binary, this time with agy's profile ([`agent_agy.go`](../../agent_agy.go))
built into unsent. Nine more captures went into
[`testdata/agy/1.2.13/`](../../testdata/agy/1.2.13/), on a fresh scratch
home and a fresh unsent state folder. **The binary still did not replace
itself.** Its SHA-256 is
`ef3728208834483754b06fd99963b4b6320740bf237212768dfed5256ae24ddb`
before the first run and after the last, and the updater's status still
reads `{"success":false,"message":"Update failed, please install from website"}`.

### Wrapping, corrected

- **A word that would end exactly at the wrap width moves to the next row**,
  because the space after it has to fit on that row too. At 117 columns,
  `"a"×114` then `" bb cc"` draws `aaaa…(114)` on one row and `bb cc` on
  the next, where Claude Code and Codex keep `bb` on the first [measured,
  `wrap`]. So agy's wrap is pi's ([`fitWrap`](../../stitch.go)), not
  Claude Code's. Every other capture reads the same under both models, which is
  why the first profile had the wrong one until this capture.
- **A line that ends exactly at the width gets no row of its own.**
  `"w"×117`, a new line and `after a full row` draw two rows with no empty
  one between them, so agy has nothing like Codex's end row [measured,
  `wrap`].
- That leaves one line break the screen cannot show at all: one typed at
  the end of a row the text filled exactly, before a word that would not
  have fitted on that row either. Those two rows are exactly what one word
  too long for a row, broken at the edge, draws, so the break comes back
  joined with nothing. Every other break comes back as itself, or as a
  space at a full row [measured, `wrap`; the limit is the stitcher's, not
  agy's].

### What the screen cannot carry

- **agy does not draw a combining mark.** `e` + U+0301, `n` + U+0303 and
  `u` + U+0308 typed into the box come back from Ctrl+G whole, and are
  written to the screen as `e n u`: the marks are in no cell [measured,
  `accents`, the bytes of the record]. A reader of agy's screen cannot
  save a decomposed accent, and unsent saves the bare letters.
- **SGR 2 exists in 1.2.13**, against the 1.2.11 note: the model name on
  the right of the footer is dim. It is drawn nowhere in the box.
- In a 256-colour terminal agy draws its rules, its hints, its scroll
  labels and the footer's left text in colour 90, and its `>` glyph in 94;
  a draft, paste placeholders included, is the terminal's own foreground.
  A `/command` typed into the box is **not** coloured there (the blue
  `/command` of the 1.2.11 notes is the menu's selected row, drawn under
  the bottom rule) [measured, `typed`, `dialogs`, `pastes`].

### Reading a frame

- Every paint is bracketed by `ESC[?25l` … `ESC[?25h`, and agy keeps the
  cursor hidden for as long as a dialog is on screen: the whole first run
  is one open paint. So an open paint alone cannot mean a half-drawn
  screen. unsent reads once the paint closes, or once the output has been
  quiet for 50 ms [measured, `first-run` and every record].
- Outside a paint, every chunk in the capture run followed the one before
  it by at least 1.78 s (the input modes agy re-asserts while idle), and
  the chunks of one unbracketed burst (the banner at start, the teardown
  at exit) arrive in the same millisecond.

### Restore-in-box (measured end to end)

| What was run | What happened |
| --- | --- |
| A draft left at a closed window in a conversation, then `agy -c` | unsent pasted it back into the empty box; Ctrl+G showed exactly the draft, `ñandú` included, and the orphan moved to history [`orphan`, `restore-c`] |
| A 20-line draft, then `agy -c` | the box showed `[Pasted text #1 +20 lines]` and Ctrl+G gave all 20 lines [`orphan-long`, `restore-long`] |
| A new chat in the same folder, with that draft waiting | no paste: a new agy conversation has no id until its first submit, so nothing matches the draft's. unsent's notice before the start named it [`new-chat`] |
| A 20-line paste on the first frame that showed the box | it landed whole, so the settle delay is a safety margin, not a need [`early-paste`] |
| The line under the box | agy draws inline on the main screen, where that row belongs to the terminal, so unsent draws nothing there and says it after exit instead: `unsent: put back your draft from 00:18 today, not sent` [`restore-long`, its exit screen] |

The restore caps ([SPEC 2.3](../SPEC.md#23-restore-in-box), item 4 of the
profile done definition): the conversation id comes from the
`conversations/<id>.db` the process holds open, the settle delay is 1 s
and three empty reads in a row, line breaks go in as `\n` inside a
bracketed paste, and a draft with a tab or a line break at its end goes to
the clipboard instead, since agy turns a tab into four spaces and drops
that break.

### The sent log

Two prompts sent in one conversation, both of which failed on the dead
endpoint: the first went to the run's log, because the conversation is
created by that first submit and nothing names it yet, and the second to
`sent/agy-<conversation id>.jsonl`. agy's own `history.jsonl` holds the
same two texts, with `conversationId` on the second only [measured,
`send`]. So a conversation's log is complete from its second message on,
and its first message is in the log of the run that sent it.

### Chat starts, corrected

`agy --help` on 1.2.13 lists `-i`/`--prompt-interactive` as "Run an
initial prompt interactively and continue the session": it sends on start,
so it is not a chat start for restore, and neither is `-p`/`--print`/
`--prompt`, a bare prompt argument, or any subcommand. The chat starts are
the bare command, `-c`/`--continue` and `--conversation <id>` [source,
`agy --help`; measured for `-c` and `--conversation`].

## Bash mode, measured (2026-09-30)

The same rig once more, on the same binary, for the one thing the profile
had taken from Codex rather than measured. One capture,
[`bash-mode`](../../testdata/agy/1.2.13/bash-mode.rec), with three Ctrl+G
copies.

- **The `!` is the glyph, not the text.** A bash-mode box holding
  `echo alpha … zulu` hands the editor `echo alpha … zulu` at Ctrl+G, 169
  bytes, with no `!` [measured,
  [`bash-mode.editor-1.txt`](../../testdata/agy/1.2.13/bash-mode.editor-1.txt)].
  agy differs from Codex here, whose `history.jsonl` keeps
  `!echo do-nothing` whole.
- **The command wraps at the same width as any other draft.** 130 `x`
  typed after the `!` draw 117 on the first row and 13 on the second, with
  the text from column 2 on both, so the glyph costs the first row
  nothing [measured,
  [`bash-mode.editor-3.txt`](../../testdata/agy/1.2.13/bash-mode.editor-3.txt)].
  A reader that splices the `!` into the first row therefore models a row
  one column narrower than agy draws. It re-wraps a bash draft one
  character early and reads a typed space at that boundary as a line
  break, which is what the first editor copy caught.
- **Ctrl+U empties the box but stays in bash mode**, glyph and footer and
  all, and **Esc is what leaves it** and puts `>` back. Ctrl+G does not
  leave it either, and the command comes back into a bash-mode box
  [measured, `bash-mode`].
- **The binary still did not replace itself.** Its SHA-256 is
  `ef3728208834483754b06fd99963b4b6320740bf237212768dfed5256ae24ddb`
  before and after, the same as the two runs above.
