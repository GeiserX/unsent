# GitHub Copilot CLI: input-box profile for `unsent` (verified)

Versions: **0.0.422** (the Homebrew cask installed here, `/opt/homebrew/bin/copilot`) and **1.0.88** (latest on npm on 2026-09-25, installed by the lane into `/tmp/unsent-spec-copilot/npm`).

Tags on every claim:
- **measured**: seen in the lane's private tmux server (`tmux -L unsent-spec-copilot -f /dev/null`, 120x40, `TERM=xterm-256color`, tmux 3.7c). The captures are kept in `/tmp/unsent-spec-copilot/` (`cap-*.raw` with SGR, `cap-*.txt` plain, `stream*.bin` raw output). Where I re-read a capture during verification, the file is named.
- **source**: read from the shipped JS bundle. 0.0.422 is `~/.copilot/pkg/darwin-arm64/0.0.422/index.js`; 1.0.88 is `/tmp/unsent-spec-copilot/home2/Library/Caches/copilot/pkg/darwin-arm64/1.0.88/app.js`. `@N` is a character offset in that file.
- **docs**: the bundled `changelog.json`.
- **guess**: not backed by a capture or a source line.

unsent today has no Copilot reader: `extractorFor` returns `claudeBox` only for `claude` and `nil` for everything else (`extract.go:38-45`) [source, unsent].

---

## 1. What the box looks like

### 0.0.422, default inline mode

Empty box, `cap-empty.raw` rows 17-21 (ESC shown as `␛`) [measured, re-read]:
```
17| ␛[37m/private/tmp/unsent-spec-copilot/work␛[39m
18|␛[2m␛[37m────────…(120 cols)
19|␛[0m␛[37m❯ ␛[7m␛[39m ␛[0m␛[37mType @ to mention files, # for issues/PRs, / for commands, or ? for shortcuts␛[39m
20|␛[2m␛[37m────────…(120 cols)
21|␛[0m ␛[1m␛[37mshift+tab␛[0m␛[37m switch mode␛[39m ​      <- ends " " + U+200B
```
- **Rules**: full-width `─` (U+2500), SGR `2;37` (dim grey) [measured]. They are an Ink box with `borderStyle:"single"`, `borderLeft:!1,borderRight:!1`, and `borderDimColor` only in interactive mode (`W==="interactive"`) [source @15576553, re-read]. Side borders were removed in 0.0.329 [docs].
- **Marker**: `❯` (U+276F) + ASCII space 0x20, no NBSP [measured, re-read: `cap-empty.txt` row 19 code points `0x276f 0x20`]. Shell mode uses `! ` [source: `Q=W==="shell"?"! ":"❯ "` @15575771]. The lane reports shell-mode rules and marker in SGR 33 and not dim [measured by lane, no capture kept]; this matches the source, where only interactive mode dims the border. Plan and autopilot modes change the colour (`selected` / `statusSuccess`) [source].
- **Empty hint**: col 2 is a reverse-video space (the fake cursor), and the hint starts at col 3 in **SGR 37, not dim** [measured, re-read].
- **Draft text**: default foreground (`␛[39m`); continuation rows are indented 2 columns [measured, re-read `cap-draft2.raw`]:
```
19|␛[0m␛[37m❯ ␛[39mdo nothing; reply ok
20|  second line of a harmless draft␛[7m ␛[0m
21|␛[2m␛[37m────…
22|␛[0m ␛[1m␛[37mshift+tab␛[0m␛[37m switch mode␛[39m ␛[37m·␛[39m ␛[1m␛[37mctrl+s␛[0m␛[37m run command␛[39m ​
```
- **Footer**: the row under the bottom rule changes with state ("ctrl+s run command" once there is text) [measured, re-read]. A completion list sits under the box and pushes it up [measured, re-read `cap-ghost.raw`: box at rows 34-36, completion at 37]. The box has no fixed row; find it by the marker.
- **Ghost text**: slash-command completions draw the rest of the command after the cursor in SGR 37, and the cell *under* the cursor can hold a ghost character drawn exactly like a typed one (reverse video, default fg). `cap-ghost.raw` row 35 [measured, re-read]:
```
␛[0m␛[37m❯ ␛[39m/cle␛[7ma␛[0m␛[37mr␛[39m
```
  Here `r` is visibly ghost (SGR 37), but nothing marks whether `a` was typed or is ghost. The lane's stronger claim, that `/clea` + ghost `r` was byte-identical to a typed `/clear` with the cursor on `r`, has no kept capture; the kept one shows the same ambiguity. Only drafts that start with `/` are affected.

### 0.0.422 with the alt screen (`alt_screen:true`, `--alt-screen`, or `--experimental`)

Same glyphs and colours, box anchored at the bottom [measured, re-read `cap-altempty.raw` rows 35-38]. The reverse-video cursor cell is still drawn (`cap-altdraft.raw` row 36: `␛[7me␛[0mnd`) [measured, re-read].

### 1.0.88: two looks

**Fallback rules** (no themed background, e.g. bare tmux) [measured, re-read `cap-new-draft.raw`, `cap-new-empty.raw`]:
```
35|␛[2m────…            <- dim, no colour
36|␛[0m❯ do nothing; reply ok
37|  second line
38|␛[2m────…
39|␛[0m ␛[1m@␛[0m files · ␛[1m#␛[0m issues
```
The empty box is a bare `❯` row: no hint, no reverse cell [measured, re-read]. That run was logged out. Whether a hint returns when logged in is unknown; I found no `Type @ to mention` string in the 1.0.88 bundle, and the hints moved to the footer, so a missing hint is the likelier case [guess].

**Decorative "rail" frame** [measured, re-read `cap-frame2.raw`, `cap-frame-empty.raw`; tmux made to answer the background query with `window-style bg=#101010`]:
```
35|␛[38;2;129;139;152m╻␛[38;2;20;27;34m▄▄▄▄…(119)
36|␛[38;2;129;139;152m┃␛[39m␛[48;2;20;27;34m do nothing; reply ok
37|␛[38;2;129;139;152m␛[49m┃␛[39m␛[48;2;20;27;34m second
38|␛[38;2;129;139;152m␛[49m╹␛[38;2;20;27;34m▀▀▀▀…(119)
```
No `❯` or `!`; the mode shows only as the rail colour; all truecolor; an empty box is `┃` alone [measured].

**When the rail is drawn** [source, corrected]: `Pd(e)` is true only when the theme has both `backgroundPrimary` and `backgroundSecondary` **and** `Rbt(e, terminalType)` is true (`app.js` @4807370). `Rbt` returns true if `COPILOT_PROMPT_FRAME=1`, false if `COPILOT_PROMPT_FRAME=0` or `!e`, and otherwise true for any terminal type except `apple-terminal` (@390288). The theme backgrounds appear to come from the terminal's OSC 11 answer; the one measurement (tmux answering vs not) is consistent with that, but I did not trace the theme code [guess, single measurement]. Two corrections to the lane:
- `COPILOT_PROMPT_FRAME=1` alone does not force the rail; `Pd` still needs the theme backgrounds [source].
- **`COPILOT_PROMPT_FRAME=0` forces the fallback rules** in any terminal [source; the debug screen at @6906091 says "Set COPILOT_PROMPT_FRAME=1 or =0 to override terminal detection"]. unsent controls the child's environment, so setting `COPILOT_PROMPT_FRAME=0` could remove the rail variant from the reader entirely. Not measured.

## 2. Alternate screen
- 0.0.422: **off by default**. `altScreen = t.altScreen ?? (o || (n.alt_screen ?? false))`, where `o` is `--experimental` [source @16157537, re-read]. Measured `alternate_on`=0 by default and 1 with `alt_screen:true`, where `ESC[?1049h` is written right after `ESC[?u` [measured; `stream2.bin` starts `␛[?u␛[?1049h`, re-read].
- 1.0.88: **on by default** [measured by lane; `stream4.bin` and `stream6.bin` both write `␛[?1049h` in the first bytes, re-read].
- 0.0.422 alt mode enables `?2004h ?1004h ?1002h ?1006h` and queries OSC 10/11 and `ESC[?u` [measured, re-read `stream2.bin`]. **Correction:** 0.0.422 *inline* mode also enables bracketed paste (`?2004h` ×2) and focus events (`?1004h`) [measured, re-read `stream3.bin`]. 1.0.88 enables `?2004h ?1004h ?1003h ?1006h` (any-motion mouse, not `?1002h`), sets modifyOtherKeys `ESC[>4;2m`, and queries OSC 4 palette entries and DECRQM `?12$p`, `?1007$p` [measured, re-read `stream4.bin`].

## 3. Redraw and synchronized output
- **0.0.422 inline**: Ink erase-and-rewrite (`ESC[2K` ×135 in `stream3.bin`), **zero `?2026` marks** in the whole 21.6 KB stream [measured, re-read]. A torn frame is possible, so read only after output goes quiet. Ink throttles to about 30 fps [source, lane @15148596; not re-read]. The idle gap to use is not known; 40 ms is a guess.
- **0.0.422 alt**: a row-diff renderer wraps frames in `ESC[?2026h … ESC[?2026l` (33 matched pairs in `stream2.bin`) [measured, re-read; the lane said 16], and then places the real cursor on the **last** row containing `ESC[7m` (`Doi()` scans bottom-up) [source @15220740, re-read].
- **1.0.88 (correction, the lane did not cover this)**: the renderer wraps a frame in `?2026h/l` only when a flag `T` is set (`app.js` @359622), and a DECRQM probe `?2026` counts as supported when the terminal answers mode 1 or 2 (@7597692) [source]. In the tmux runs `stream4.bin` and `stream6.bin` contain **zero** `?2026` marks and bracket updates with `?25l … ?25h` instead (87/86 and 30/29) [measured, re-read]. So under a terminal that answers the probe (most modern ones, which unsent passes queries to) frames are likely synchronized, and in bare tmux they are not [guess for the real-terminal case]. The reader cannot rely on sync marks for 1.0.88 and needs the same idle-gap rule as 0.0.422 inline.

## 4. Cursor
- 0.0.422 inline: the real cursor is parked at column 0 below the footer; the insertion point is the only SGR 7 cell in the box [measured by lane, positions from `tmux display`; the reverse cell is re-read in every 0.0.422 capture].
- 0.0.422 alt: real cursor at the insertion point [measured by lane] and the reverse cell is also drawn [measured, re-read `cap-altdraft.raw`].
- 1.0.88: no reverse cell; real cursor at the insertion point [measured by lane; no reverse cell in `cap-new-draft.raw`, re-read].

## 5. Wrapping
- 0.0.422: text width **W−4** (116 at 120). Words that fit move to the next row with the separating space dropped; a longer word hard-breaks at exactly 116 [measured, re-read `cap-wrap.txt`: row 21 holds 115 text chars and the next 4-char word wraps; row 23 holds 116 chars of one word]. Source: `U=t-rci-Q.length` with `Q` the 2-char marker [source @15576367, re-read; `rci=nci*2` with `nci=1`, so 2].
- 1.0.88 rail: **W−3** after `┃ ` [measured by lane; not re-derived].
- 1.0.88 fallback: not measured; W−4 is a guess.
- **The screen drops characters.** `eci()` strips ANSI, turns TAB into 4 spaces and deletes `[─-╿▀-▟]` [source @15570118, re-read]. Measured: the draft `tab<TAB>here and a rule ─── and block █ end` shows as `tab    here and a rule  and block  end` [measured, re-read `cap-altdraft.raw` row 36]. A draft containing those characters can never be read back exactly from the screen.

## 6. Height and scrolling
- 0.0.422: hard cap of **10 visible rows** (`maxLines:10` passed by `InputArea`) whatever the window size [source @15577131, re-read; measured 10 rows at 40 rows, `cap-tall.raw` rows 21-30 show L3-L12]. The `❯` marks the first *visible* row [measured, re-read]. Scrolling is minimal: the offset moves only as far as needed to keep the cursor line visible (`ve<et&&(It=ve)`, `ve>et+g-1&&(It=ve-g+1)`) [source, re-read], so one cursor step moves the view one row; a larger cursor jump moves it further [source].
- 1.0.88: `Ee=Math.max(3,Math.floor((t-ybo)/2))` with `ybo=4` and `t` the terminal height [source @6561343 and @6562837, re-read]; measured 18 rows at 40 [measured, re-read `cap-new-tall.raw` rows 20-37, L3-L20].

## 7. Pastes
- 0.0.422: `UUr` @15225561 [source, re-read]. A paste goes inline if `split("\n").length <= 10`, else becomes `[Paste #N - M lines]` (`esi=10`, `tsi`); a trailing newline counts as a line [source]. Measured: 3 lines inline (`cap-paste3.raw`), 10 lines inline, 11 and 12 lines as `B:[Paste #1 - 11 lines] C:[Paste #2 - 12 lines]` (`cap-paste12.raw` rows 21-30) [measured, re-read].
- **Correction for 1.0.88**: not unchanged. It trims one trailing newline before counting and before inserting (`ee=Lee(Y)`, `Lee` drops a final `\n`) [source @5973843]. So a 10-line paste with a trailing newline is inline in 1.0.88 but a token in 0.0.422. Same regexes and the same threshold `ioo=10` [source @5971769].
- **Over 20480 bytes**: `[Saved pasted content to workspace (24.4 KB) id=N]` [measured, re-read `cap-pastebig.raw` row 30]. The text goes to `<config>/session-state/<session>/files/paste-<epoch ms>.txt`, mode 600 [measured, re-read: `-rw------- 25000 cfg/session-state/40cf300a-…/files/paste-1790437163429.txt`]. The threshold is `a_o()`: `COPILOT_LARGE_OUTPUT_THRESHOLD_BYTES` or 20480 [source @8345923, re-read]. The 0.0.397 changelog says "over 30 KB"; the threshold changed since [docs vs source].
- Regexes: `/\[Paste #(\d+)(?: - \d+ lines)?\]/g` and `/\[Saved pasted content to workspace \([^)]+\) id=(\d+)\]/g` [source, re-read]. N counts from 1 and resets on send or clear [measured by lane, not re-checked].
- **Non-bracketed input treated as a paste** (0.0.422): a raw input chunk that is 32+ characters, or contains `\n` and is longer than 1, takes the paste path (`G.length>=Vrn||G.includes("\n")&&G.length>1`, `Vrn=32`) [source @15230372, re-read], unless the draft starts with `/` [source @15227368]. 200 ms debounce (`$oi=200`) [source].
- `compact_paste:false` disables the tokens [source: bundled help text @16132017; docs 0.0.410 per lane]. Shell mode pastes raw text [docs per lane, not re-checked].
- One Backspace, Delete or arrow key acts on a whole token (`handleBackspaceIntoPasteToken`, `handleDeleteIntoPasteToken`) [source, re-read]. For unsent this means **one delete key can remove 20+ visible characters**, so a byte count of delete keys undercounts the loss.
- unsent's `pastePlaceholder` matches only `[Pasted text #N +M lines]` / `[Pasted text #N]` (`paste.go:16`) and would expand neither Copilot format [source, unsent].

## 8. Keys

0.0.422 splits every input chunk into tokens with `/\x1b\[[^@-~]*[@-~]|\x1b[\x08\x1b\x20-\x7e\x7f]|[\x00-\x1f\x7f]|[^\x00-\x1f\x7f\x1b]+/` [source @15230372, re-read]. `\x1b\r` is not in the ESC+char class, so ESC and CR always arrive as two keys.

| Action | Keys | Evidence |
|---|---|---|
| New line | Ctrl+J (0x0a): a lone `\n` chunk becomes a newline with `return` cleared | measured by lane; source re-read |
| New line | `\` then Enter at end of line or before `\n` (the backslash is removed) | measured by lane; source re-read (`st==="\\"&&dt`) |
| New line | Shift+Enter or Meta+Enter, only when the terminal encodes them as one key (kitty CSI-u, modifyOtherKeys) | source re-read (`(ve.shift\|\|ve.meta)&&ve.return`); docs 0.0.410 |
| **Submits (trap)** | Esc then Enter (tmux `send-keys Escape Enter`) | **measured**: the "not authorized" reply in `cap-paste12.raw` row 17 and the lone `fourth via esc-enter` draft in `cap-draft4.raw` fit a submit; source: the tokenizer splits ESC CR, Escape is ignored (`ve.escape` returns early), then Return submits |
| Submits (likely) | Option/Alt+Enter from a terminal that sends ESC CR in one write | source (same tokenizer); not measured |
| Submit | Enter; Ctrl+Q (queue); Ctrl+D with the cursor at the end of the text (queue); Ctrl+Enter with kitty | source re-read (`ve.ctrl&&qe==="q"\|\|It&&ve.ctrl&&qe==="d"\|\|n&&ve.ctrl&&ve.return`) |
| Exit | Ctrl+D on an empty box (missing from the lane's table) | source re-read (`!e.text&&et&&h`); docs ("Exit CLI with ctrl+d on empty prompt") |
| Delete before cursor | Backspace 0x7f, Ctrl+H; Ctrl+W, Ctrl+Backspace and Meta+Backspace delete a word; Ctrl+U clears before the cursor (`clearBeforeCursor`; whether it stops at the logical line is not verified) | source re-read @15249010 |
| Delete after cursor | Delete `ESC[3~`, Ctrl+D (not at the end), Ctrl+K (`clearAfterCursor`), Meta+Delete deletes a word | source re-read |
| Clear everything | Ctrl+C (a second one within the window exits); Ctrl+L submits `/clear` | measured by lane (Ctrl+C); source per lane (Ctrl+L, not re-read) |
| History | Up on the first row or Down on the last replaces the box | measured by lane |
| External editor | Ctrl+G | measured (`edargs.txt`); docs 0.0.419 |
| Other | Ctrl+S runs a command and keeps the input; `?` on an empty box opens help | measured by lane (`cap-help.raw` exists) |

Key encodings change with the terminal: 0.0.422 queries `ESC[?u` and decodes kitty CSI-u when enabled [source, per lane; the `ESC[?u` query is measured in `stream2/3.bin`], and 1.0.88 sets modifyOtherKeys `ESC[>4;2m` [measured, re-read `stream4.bin`]. With those, Ctrl+W arrives as `ESC[119;5u` or `ESC[27;5;119~` [guess from the protocols; not captured].

**unsent's `deleteKeys` against Copilot** (`wrap.go:506-529`) [source, unsent; lane corrected]:
- 0x08 is **already** in `oneCharKeys`; the lane said it had to be added.
- Missing: 0x04 (Ctrl+D, deletes ahead mid-text but submits at the end), 0x03 (Ctrl+C clears everything).
- `ESC 0x7f` (Meta+Backspace, a word) is counted as one character because it contains 0x7f; it needs to be unlimited.
- CSI-u and modifyOtherKeys forms are not matched at all.
- A single Backspace or Delete can remove a whole paste token (section 7).

## 9. Built-in recovery
- Ctrl+C clear, same session: the draft came back after Up then Down [measured by lane]. Memory only.
- Across restarts: none; nothing on disk held a draft left in the box at exit [measured by lane].
- Submitted prompts: `~/.copilot/command-history-state.json` as `{"commandHistory":[…]}` [measured by lane]. It ignored `--config-dir` on 0.0.422 [measured by lane].
- Large pastes: saved on disk (section 7).

## 10. Ctrl+G, a byte-exact reference for tests
Ctrl+G runs `$VISUAL`/`$EDITOR` on `$TMPDIR/copilot-edit-XXXXXX/COPILOT_PROMPT.md` [measured, re-read `edargs.txt`: `/var/folders/…/T/copilot-edit-F1FU0a/COPILOT_PROMPT.md`]. The file holds the draft exactly, including TAB, `───` and `█` that the screen hides, and paste tokens stay unexpanded [measured, re-read `edcopy.txt`: `tab<TAB>here and a rule ─── and block █ end P:[Paste #1 - 12 lines]`]. That the directory is deleted afterwards is the lane's observation [measured by lane].

## 11. TUI stack
React on a vendored Ink with Yoga layout [source]. 0.0.422 adds a custom alt-screen row-diff renderer [source @15220740]. Ships as a Node v24.11.1 single-executable binary that unpacks JS into `~/.copilot/pkg/<platform>/<version>/` (0.0.422) or `~/Library/Caches/copilot/pkg/…` (1.0.88) [measured: log line `Node.js version: v24.11.1`; both unpack trees exist].

## 12. Detection recipe for a `copilotBox` reader
1. **Remove the rail variant first if possible**: start the child with `COPILOT_PROMPT_FRAME=0` [source; not measured]. If that holds, only the rules layout remains in both versions.
2. **Rules layout**: scan bottom-up for a row that starts with `❯ ` or `! `, or is only `❯`, with an all-`─` row of at least cols/2 directly above; the box ends at the next all-`─` row; content from col 2 on every row. `isRule` and `hasMarker` already accept this (ASCII space is allowed) [source, unsent `extract.go:101-128`].
3. **Rail layout** (if not suppressed): top row `╻` + `▄…`, rows `┃ ` + text (from col 2, width W−3), bottom row `╹` + `▀…`. No marker; shell mode shows only as rail colour [measured].
4. **Empty test**:
   - 0.0.422: one row whose only non-SGR-37 cell is the SGR 7 cursor cell at col 2. `screen.go` records only dim (`faint`) and the cursor, so it must also record reverse video and non-default foreground. **Reusing `claudeBox` as is would save the hint** `Type @ to mention files…` as a draft: the row matches the marker, `textFrom(2)` is not empty and `faintFrom(2)` is false because the hint is SGR 37, not dim [source, unsent `extract.go:80`; measured hint styling].
   - 1.0.88: nothing after the marker or rail [measured].
5. **Cursor**: 0.0.422 inline, use the SGR 7 cell and ignore the real cursor. 0.0.422 alt and 1.0.88, use the real cursor [measured by lane].
6. **Ghost text**: drop SGR 37 cells after the cursor on the last row. The character under the cursor can be ghost and is indistinguishable; only drafts starting with `/` are affected [measured].
7. **Width**: W−4 for 0.0.422 rules (measured) and, by guess, for 1.0.88 rules; W−3 for the rail (measured by lane).
8. **Cap**: 10 rows on 0.0.x; `max(3, floor((rows−4)/2))` on 1.x [source + measured]. Following unsent's convention, treat one row below the cap as capped. 0.0.422's scroll is minimal (cursor-following), unlike Claude Code's jump.
9. **Timing**: 0.0.422 inline and 1.0.88 in bare tmux have no sync marks, so read after an idle gap (length unknown; 40 ms is a guess). When sync marks appear, never read inside a pair.
10. **Pastes**: add expanders for `[Paste #N - M lines]` (from the bracketed paste bytes unsent already captures; inline mode does enable `?2004h`) and `[Saved pasted content … id=N]`. Count lines the way the version does (1.0.88 drops one trailing newline).
11. **Delete keys**: add 0x04 (ahead), 0x03 (unlimited), `ESC 0x7f` (unlimited), CSI-u and modifyOtherKeys forms, and treat any delete key next to a paste token as able to remove the whole token.

## 13. Is the screen stable enough to read?
Not without version gating. The 0.0.422 `changelog.json` has 104 entries from 0.0.328 to 0.0.421 (74 distinct stable versions; the rest are `-N` prereleases) [docs, re-counted; the lane said 105 releases]; the 1.0.88 changelog has 369 entries [docs, re-counted]. Box-relevant entries include the 10-line scrollable input and removed side borders (0.0.329), pasted content saved to workspace files (then "over 30 KB"), Ctrl+D exit on an empty prompt, kitty Shift+Enter, and Ctrl+G external editor (0.0.419) [docs; version numbers other than 0.0.329 and 0.0.419 are the lane's and were not re-checked]. Between 0.0.422 and 1.0.88 the default screen mode, cursor rendering, hint, height formula, footer and frame all changed, and a top tab bar appeared [measured].

What breaks a reader: the rail frame (unless `COPILOT_PROMPT_FRAME=0` works), `/theme` and truecolor themes, `--screen-reader`, `--no-color`, vim mode (feature-flagged in 1.0.88 per lane), menus and permission prompts replacing the box, completion lists moving it, kitty or modifyOtherKeys changing key bytes, and the renderer dropping TAB and box-drawing characters. A reader that stops matching fails safe (keeps the last draft).

## 14. Test-harness traps
- **Esc+Enter submits** in Copilot where it makes a new line in Claude Code. Use Ctrl+J or `\`+Enter [measured].
- **Copilot writes to the real `~/.copilot`** even with `--config-dir` (config.json and command history on 0.0.422) [measured by lane; source per lane `xin()`/`fr.writeKey`, not re-read]. Always run tests with a scratch `HOME`.
- **Copilot finds credentials by itself**: it reads `GITHUB_TOKEN`, `GH_TOKEN`, `COPILOT_GITHUB_TOKEN` and friends, calls `gh auth token --hostname <host>`, and has keytar (macOS keychain) support; the hidden `--no-auto-login` flag disables "stored OAuth tokens and gh CLI" detection [source @1022218, @11422680, @11423518, @16150829]. This is the likely answer to the lane's open question about where the `Welcome GeiserX!` login came from (the gh CLI's github.com login is GeiserX) [guess, not traced at runtime]. On this Mac the shell's exported enterprise token produces `Classic PATs are not supported` in every later log [measured, `~/.copilot/logs/*.log`]. Tests should use a scratch `HOME` plus `--no-auto-login` and unset the token variables, or a real submit can reach GitHub.

## 15. Side effects
Reported by the lane (not independently checkable after the fact):
- The first 0.0.422 run used the real HOME, rewrote `~/.copilot/config.json` (added `last_logged_in_user`) and created `~/.copilot/command-history-state.json`. The lane says it restored config.json to its original 131 bytes and deleted the history file.
- One harmless prompt was submitted by the Esc+Enter test and refused with "You are not authorized to use this Copilot feature…" [measured, visible in `cap-paste12.raw` row 17].
- A 137 MB 1.0.88 unpack under `~/Library/Caches/copilot` was removed [verified: the directory does not exist now].

**Found during verification (not the lane's doing, by timestamps):** 0.0.422 ran again under the real HOME after the lane's last capture (17:51 local). `~/.copilot/logs/` has process logs starting at 16:02Z, 16:24Z, 16:26Z (×2) and 16:47Z (18:02-18:47 local), one last written at 19:00 local; `~/.copilot/session-state/` has four session directories from 18:02-18:47; and `~/.copilot/config.json` is now **160 bytes**, modified 18:47, with keys `firstLaunchAt`, `last_logged_in_user`, `banner`. So the lane's "restored to 131 bytes" cannot be confirmed any more, and something else (another lane or a person) has been running `copilot` against the real config since. No `command-history-state.json` exists now. No copilot process was running when I checked.

---

## Open questions
- Does `COPILOT_PROMPT_FRAME=0` give the rules layout in a real terminal (iTerm2, Ghostty) under unsent? Source says yes; not measured.
- Does 1.0.88 emit `?2026` pairs under a terminal that answers the DECRQM probe, and does that reach unsent's shadow screen?
- 1.0.88 fallback wrap width (W−4 is a guess) and whether the empty hint exists when logged in.
- Safe idle gap for unsynchronized redraws.
- Does a `[Paste #N]` token still expand at submit after a Ctrl+G round trip? Untested because testing means submitting.
- Which Copilot version to target: 0.0.422 is installed, 1.0.88 is current and draws a different box.
- Who ran `copilot` under the real HOME between 18:02 and 18:47 local (section 15).

## Verification notes

What I checked (all without launching any agent CLI; only reading captures, the two installed bundles and unsent's source):
- Re-read captures `cap-empty`, `cap-draft2`, `cap-ghost`, `cap-altempty`, `cap-altdraft`, `cap-tall`, `cap-wrap`, `cap-bs`, `cap-draft4`, `cap-paste3`, `cap-paste12`, `cap-pastebig`, `cap-new-empty`, `cap-new-draft`, `cap-new-tall`, `cap-frame`, `cap-frame2`, `cap-frame-empty`, plus `edargs.txt`, `edcopy.txt`, the saved paste file and the first-run log. Counted control sequences in `stream2/3/4/6.bin`.
- Re-read source at the cited offsets for: `maxLines:10`, the border props and wrap width, the scroll-offset effect, `alt_screen` default, the paste thresholds and regexes (`esi`, `Vrn`, `ZD`/`a_o`), `eci()`, the stdin tokenizer, the submit/newline key handler, the common delete-key handler, `Doi()` and the `?2026` constants (0.0.422); `ybo`, `Rbt`, `Pd`, the paste code, and the `?2026` renderer and DECRQM probe (1.0.88).
- Checked unsent's `extract.go`, `wrap.go` (`deleteKeys`) and `paste.go` against the lane's claims about them.

What held: box glyphs and colours, ASCII-space marker, SGR 37 hint, W−4 wrap, 10-row cap and 1.0.88 formula, paste token formats and thresholds, the 20480-byte file save, character stripping, Ctrl+G reference copy, Esc+Enter submitting, alt-screen defaults.

What I changed:
- `deleteKeys` already counts 0x08; removed it from the "must add" list and added that Meta+Backspace is undercounted as one char and that one key can delete a whole paste token.
- 1.0.88 synchronized output: the lane implied alt mode always has `?2026` pairs; 1.0.88 only emits them when a DECRQM probe succeeds, and the tmux streams have none.
- 0.0.422 inline mode also enables bracketed paste and focus events (the lane listed them for alt mode only).
- 1.0.88 paste line count is not unchanged: it drops a trailing newline first.
- Rail gate: `COPILOT_PROMPT_FRAME=1` does not force it alone; added that `COPILOT_PROMPT_FRAME=0` forces the fallback, a possible big simplification.
- Esc+Enter: added the source reason (tokenizer splits ESC CR) and that a real Alt+Enter likely submits too.
- Added Ctrl+D on an empty box exits; Ctrl+U/Ctrl+K scope marked unverified.
- Ghost text: downgraded "byte-identical" (no kept capture); the kept capture shows the same ambiguity with different characters.
- Changelog: 104 entries / 74 stable versions, not 105 releases; added that the 0.0.397 note said 30 KB while the current threshold is 20 KB.
- `claudeBox` is not used for Copilot today; reworded the hint risk as a risk of reusing it.
- `?2026` pair count in 0.0.422 alt: 33 in `stream2.bin`, not 16.
- Credentials: answered from source (env tokens, `gh auth token`, keytar, `--no-auto-login`), tagged guess for what actually happened.
- Side effects: `~/.copilot/config.json` is now 160 bytes and was modified at 18:47, after the lane ended, alongside new logs and session directories, so the restore claim can't be confirmed and someone else ran copilot under the real HOME.
- Dropped the lane's open question about an unrelated relayed user message; it is not part of this profile.
