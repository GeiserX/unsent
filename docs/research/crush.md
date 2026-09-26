# Charm Crush (charmbracelet/crush v0.96.1): verified input-box profile for unsent

**Tags.** `[measured]` = seen by the research lane on screen, in a tmux pane capture or in the byte capture `/tmp/crush-lane/run/typescript` (21,482 bytes). `[source]` = code at tag `v0.96.1` (`17a62b7`) unless stated; main is `c9b4534` (2026-09-26, 13 commits past the tag). `[docs]` = README, help text or a protocol spec. `[guess]` = inference. Where the verifier re-checked a claim, the tag says so (`[measured, re-checked]`, `[source, re-checked]`).

**Setup the lane used.** Release binary from `crush_0.96.1_Darwin_arm64.tar.gz`; the tarball sha256 `6302f907…5b0867` matches the release `checksums.txt` (the checksum covers the tarball, not the extracted binary). Run under `env -i`, HOME/XDG/data dir in `/tmp/crush-lane/run`, `OPENAI_API_KEY=sk-fake-unsent-measure`, empty work dir, private tmux socket `-L unsent-crush-lane` (tmux 3.7c). No prompt was submitted. `COLORTERM` was unset, so the byte capture is in 256 colours. [measured]

---

## 1. TUI stack
- Go; `charm.land/bubbletea/v2 v2.0.9`, `charm.land/bubbles/v2 v2.2.1` (textarea), `charm.land/lipgloss/v2 v2.0.6`, `charmbracelet/ultraviolet` (pseudo-version `006e29f97886`), `charmbracelet/x/editor v0.2.0`. [source, re-checked `go.mod`]
- unsent's shadow screen uses `charmbracelet/x/vt` and ultraviolet from the same family, so it should render Crush faithfully. [guess]
- The renderer moves the cursor with HT and ECH: 3 HT bytes and 23 distinct `ESC[nX` forms in the byte capture. tmux `capture-pane` shows those tabs inside box rows. [measured, re-checked counts]

## 2. Screen mode and output framing
- Alternate screen: `ESC[?1049h` at start (3 in the capture: start plus two Ctrl+O returns), `ESC[?1049l` twice (the two Ctrl+O). `v.AltScreen = true` every frame at `internal/ui/model/ui.go:3634` (the lane cited 3641). [measured + source, re-checked]
- Startup queries, each once: `ESC[?2026$p`, `ESC[?2027$p`, `ESC[?u`, `ESC[>q`, `ESC[14t`, OSC 99, kitty graphics `ESC_G`. Enabled modes: `ESC[?2004h` (bracketed paste), `ESC[?1002h` + `ESC[?1006h` (mouse), `ESC[?1004h` (focus), `ESC[>4;2m` (modifyOtherKeys 2), `ESC[>1u` (push kitty flags 1), `ESC[1 q`, OSC 12 / OSC 11 colours. [measured, re-checked counts]
- Synchronized output: 136 `ESC[?2026h` and 136 `ESC[?2026l` in the capture, because tmux answered DECRQM 2026. [measured, re-checked] Whether they are omitted on a terminal that does not answer: [guess].

## 3. What the box looks like
No border, no rule lines. Top to bottom [measured at 100x30, 60x20, 140x45]:

```
row T-1 : attachments row (blank, or chips "  ≡  paste_1.txt  ✕ ")
row T   : " " + "  > " + text     first row, focused, normal mode
row T+1 : " " + "::: " + text     every other row (hard newline OR soft wrap)
...
row B   : last box row, B = T+h-1, h = 3..15
row B+1 : blank margin row
row B+2 : help bar ("… • ctrl+j newline • ctrl+c quit")
```

Real bytes for an empty focused box (256 colours) [measured, re-checked]:
`ESC[38;5;49;1m  > ESC[38;5;59;22mReady?` — prompt bold colour 49, placeholder colour 59 with SGR 22 (normal intensity), **not** SGR 2 faint.

- **Columns.** Column 0 is a margin space, columns 1–4 are the prompt (`SetPromptFunc(4, …)` in `setEditorPrompt`, `ui.go:4427-4443`), text starts at column 5. [measured + source, re-checked]
- **First-row prompt by mode** (styles in `internal/ui/styles/quickstyle.go:847-867`) [source, re-checked]:
  - Normal focused: `"  > "`, bold, `success` colour (truecolor `#00FFB2` measured by the lane).
  - Normal blurred: the first row is a plain `"::: "` (`normalPromptFunc`, `ui.go:4447-4459`). [source, re-checked; lane also measured cursor hidden]
  - Plan (v0.95.0+): `" ⏸ "` on the `primary` background plus a 1-cell right margin; other rows `":::"` + margin.
  - YOLO: `" ! "` on the `busy` background (`#E8FF27` measured) + margin; status row shows a `YOLO MODE` badge [measured].
  - Bang (`!` typed first): `" ! "` on the `primary` background (`#8B75FF` measured) + margin. The `!` is removed from the textarea value (`history.go`, `strings.TrimPrefix(val, "!")`), so the draft is `"!" + box text`. [source, re-checked; measured by lane]
  - Blurred plan/yolo/bang icons switch to an `fgMoreSubtle` background. [source]
- **Other rows** are `"::: "` in every mode, focused or blurred; only the colour changes. [source, re-checked]
- Soft wraps and hard newlines both show `:::`. [measured]
- The prompt function receives the *content* line number, so a box scrolled by even one row shows `:::` on every visible row; `  > ` on the top row means scroll offset 0. [measured]
- **Placeholder.** Grey foreground, not faint (see bytes above), so unsent's `faintFrom` test (`screen.go:39`, used at `extract.go:80`) would read `Ready?` as a draft. [measured, re-checked]
  - Ready list: `Ready!` `Ready...` `Ready?` `Ready for instructions`; working list: `Working!` `Working...` `Brrrrr...` `Prrrrrrrr...` `Processing...` `Thinking...` (`ui.go:4801-4815`, randomized in `randomizePlaceholders`, the lane cited 4837-4851). [source, re-checked]
  - Fixed: `Run a shell command` (bang), `Let's plan` (plan), `Go crazy` (yolo) (`ui.go:1519-1537`). [source, re-checked]
  - An empty box is 3 rows: placeholder row plus two `:::` rows. [measured]
- **Colours depend on theme.** Themes follow the provider and user config; compare colours relative to each other on one screen, never absolute values. [source per lane (commit f38b794); not re-checked]
- A `" ? "` question icon style exists, but it is used only by the question dialogs (`internal/ui/dialog/question_*.go`); their free-text editor is a separate textarea with prompt width 0, so it cannot mimic the `:::` run. [source, re-checked]

## 4. Wrapping, height, scrolling, cursor
- **Wrap width** = editor width minus the 4-column prompt:
  - landing and compact chat: **cols − 6** (94 at 100 cols, 54 at 60, 134 at 140) [measured];
  - chat with the 32-column sidebar (`sidebarWidth := 32`, `ui.go:4179`; breakpoints width 120 / height 30, `ui.go:70-73`): **cols − 38** (102 at 140x45). [measured + source, re-checked constants]
- **Wrap rule** (bubbles `textarea.go:2020` `wrap`) [source, re-checked]:
  - breaks between words; the separating spaces stay at the end of the row;
  - a word too long for the row is cut at the width (a 1000-char word at width 94: ten rows of 94 then 60 [measured]); double-width runes can cut one cell earlier;
  - the last row of each logical line gets one extra trailing space, and a line reaching the width exactly adds an empty visual row;
  - the sanitizer (`bubbles internal/runeutil`) turns CR and LF into newlines, tabs into spaces, drops other control characters.
- **Height:** `DynamicHeight`, `MinHeight 3`, `MaxHeight 15` (`ui.go:88-96, 444-452`), independent of window height. [source, re-checked] Grows upward; bottom stays one blank row above the help bar. [measured]
- **Line count** is not limited by MaxHeight because Crush's newline key calls `InsertRune('\n')` (`ui.go:3178-3180`), which skips the textarea's `atContentLimit` check. It is still capped at **10,000 logical lines** by the bubbles package constant `maxLines` (`textarea.go:37`, enforced at `:582`). The lane said "unlimited". [source, re-checked; corrected]
- Short windows overflow: at 60x20 only 12 of 15 rows were visible, the help bar drew over the rest, and the cursor sat below the help bar. [measured]
- **Scrolling:** moving the cursor scrolls one row at a time (`repositionView`, `textarea.go:1114`); Up at the start of the box runs history recall. [measured + source]
- **Cursor:** real terminal cursor at `x = 5 + column`, `y = box top + visual row`; virtual cursor off (`ta.SetVirtualCursor(false)`). Hidden when blurred, a dialog is open, or the compact details overlay shows. Empty box: cursor at (5, T), before the placeholder. [measured + source]
- **Cursor bug:** in a capped box whose first logical line wraps over 3 rows, Up to the top left the cursor 2 rows above the box (y=10, box top y=12) until the next edit. Never assume the cursor is inside the box. [measured by the lane; no capture file kept]
- Resize re-wraps to the new width. [measured]

## 5. Where the box is
- Landing (no session): logo header, info, then the box; editor spans columns 1..cols-2. [measured]
- Chat, wide: sidebar at columns ≥ cols-32 (logo, session title, cwd, model, Modified Files, LSPs, MCPs, Skills), to the right of the box rows; crop each box row to `[5, 5+width)`. [measured]
- Chat, compact (width < 120 or height < 30, or `tui.compact_mode`): one-line header `Charm™ CRUSH ╱╱╱ … • ctrl+d open` on row 1, box full width. [measured + source]
- **Next release:** main `c9b4534` binds Ctrl+B to "toggle sidebar" (`keys.go:222`, `ActionToggleCompactMode`); not in v0.96.1. Derive width from the screen, not from `cols ≥ 120`. [source, re-checked]
- Things that cover or replace the box: dialogs (Ctrl+P etc.) draw `╭─╮ │ ╰─╯` frames over it [measured]; `@` completion pops above it; inline editors (agent questions, plan handoff) replace the textarea; Ctrl+G expanded help pushes the box up. [source]

## 6. Pastes
- Bracketed paste on; `handlePasteMsg` at `ui.go:5723` (the lane cited 5823). It first rewrites only `\r\n` to `\n`. [source, re-checked]
- Threshold (`hasPasteExceededThreshold`, `ui.go:5786-5801`): split on `\n`, attachment when **more than 10 segments** (i.e. 10 or more LF) **or any segment longer than 1000 bytes** (`len()` counts bytes). [source, re-checked]
  - Lane measurements: 9 LF inline, 10 LF (10 lines + trailing LF) attachment, 1000-byte line inline, 1001-byte line attachment. [measured; fixtures `/tmp/crush-lane/p*.txt` present]
- **Only LF counts.** ultraviolet appends the raw paste bytes without normalizing CR (`terminal_reader.go:289-325` at `006e29f97886`), and Crush rewrites only `\r\n`. [source, re-checked] With tmux `paste-buffer` (which turns LF into CR) a 10-line paste stayed inline and the sanitizer turned the CRs into newlines. [measured]
  - **Correction/addition:** a CR-separated paste is one segment for the threshold, so any CR-separated paste **over 1000 bytes in total** becomes an attachment even if each line is short, while a CR-separated paste of many short lines under 1000 bytes stays inline. [source-derived; not measured]
  - Which real terminals send CR vs LF inside a bracketed paste (Ghostty, iTerm2, Terminal.app, kitty) is not established here. [guess: xterm-style terminals send CR]
- An attachment never enters the box. It becomes `message.Attachment{FileName: "paste_N.txt"}` in memory (over 5 MB is refused with "Paste is too big (>5mb)"); N = highest existing `paste_N` index + 1 (`pasteIdx`, `ui.go:5919-5932`). [source, re-checked]
  - Chip on the row above the box: `  ≡  paste_1.txt  ✕ ` (U+2261 icon, U+2715 remove). [measured] Image `■`, markdown/skill `▲`, names cut to 15 cells with `…`, overflow `N more…` (`internal/ui/attachments/attachments.go`). [source, not re-checked]
  - A paste that is a list of existing image paths becomes image attachments. [source, re-checked]
- Attachments are not part of the Ctrl+O text. [measured]
- Ctrl+R then a digit deletes attachment i; Ctrl+R then `r` deletes all (`keys.go:166-175`). [source, re-checked]
- **For unsent:** save the captured bracketed-paste bytes as a separate attachment keyed by the `paste_N` number; the screen shows only the chip. Apply Crush's own rule (LF count and per-segment byte length on the `\r\n`-normalized bytes) to decide whether a captured paste went into the box or into an attachment. [guess]

## 7. Keys (v0.96.1)
- **Send:** Enter. `exit`/`quit` + Enter opens the quit dialog. `/` on an empty box opens the command palette (`ui.go:3216`), so a draft cannot start with a typed `/`. [source, re-checked; measured]
- **Newline:** `shift+enter` or `ctrl+j` (`keys.go:139`). Shift+Enter only works where the terminal reports it distinctly; help text switches to "shift+enter" when it can (`ui.go:1121`). Alt+Enter not bound. [source, re-checked; Ctrl+J measured]
- **Backslash + Enter** only strips the trailing `\` (`ui.go:3105-3113`: comment says "add a newline", code only calls `SetValue(before)`); nothing is sent and no newline is added. [measured + source, re-checked]
- **External editor:** Ctrl+O (§9).
- **History:** Up at start / Down at end walks sent prompts; Esc returns to the draft. [source]
- Other: Ctrl+C quit dialog; Ctrl+Z suspend; Tab focus editor/chat; Shift+Tab cycles code → plan → code+YOLO (`toggleInputMode`); Ctrl+Y toggle YOLO; Ctrl+P / Ctrl+L / Ctrl+S dialogs, Ctrl+G help; Ctrl+N new session and Ctrl+F add attachment shadow the textarea's own bindings. [source, re-checked]
- **Delete keys** (bubbles `DefaultKeyMap`, `textarea.go:97-103`, plus Crush overrides) [source, re-checked; the lane measured Backspace, Ctrl+H, Delete, Ctrl+D, Ctrl+W, Alt+Backspace, Ctrl+K, Ctrl+U]:
  - one char back: Backspace `0x7f`, Ctrl+H `0x08`;
  - one char ahead: Delete `ESC[3~`, Ctrl+D `0x04` — **but in compact chat Ctrl+D toggles the details panel instead** (`ui.go:2925`, `keys.go:213`);
  - any amount back: Ctrl+W `0x17`, Alt+Backspace `ESC 0x7f`, Ctrl+Backspace, Ctrl+U `0x15` (to line start);
  - any amount ahead: Ctrl+K `0x0b`, Alt+D `ESC d`, Alt+Delete `ESC[3;3~`, Ctrl+Delete `ESC[3;5~`;
  - selections (Shift+arrows, mouse drag, Ctrl+Shift+A) then typing, Backspace or Ctrl+Shift+X delete any amount;
  - no undo key (`0x1f` / Ctrl+_ is unbound).
  - Versus unsent's current set (`wrap.go:506-510`, `deleteKeys` at `:514`: `0x7f 0x08 ESC[3~` one-char; `0x17 0x15 0x0b 0x1f` many; `ESC[3~ 0x0b 0x1f` ahead): a Crush set adds `0x04` (one-char ahead, non-compact only), `ESC 0x7f`, `ESC d`, `ESC[3;3~`, `ESC[3;5~`, and drops `0x1f`.
- **Key encodings change under Crush.** Bubble Tea v2 always writes `ESC[>4;2m` and pushes kitty flags with bit 1 set (`cursed_renderer.go:159-167`; `keyboardEnhancementsFlags` starts at `flags := 1`, `:895-910`). [source, re-checked; measured in capture]
  - In a terminal that honours kitty disambiguation, Ctrl+letter and Alt+key arrive as CSI-u: Ctrl+W `ESC[119;5u`, Ctrl+U `ESC[117;5u`, Ctrl+K `ESC[107;5u`, Ctrl+D `ESC[100;5u`, Ctrl+H `ESC[104;5u`, Alt+Backspace `ESC[127;3u`; plain Backspace stays `0x7f`. With xterm modifyOtherKeys 2 instead: `ESC[27;5;119~` style. [docs: kitty keyboard protocol spec; not measured through unsent]
  - The same applies to non-delete keys unsent may care about: Ctrl+J becomes `ESC[106;5u`, Shift+Enter `ESC[13;2u`, Enter stays `CR`. [docs; guess for which terminals]
  - Which terminals honour the push: kitty, Ghostty, WezTerm, foot are documented kitty-protocol terminals; iTerm2 support is version- and setting-dependent; Terminal.app honours neither. [guess; lane listed iTerm2 as CSI-u without evidence]
  - unsent's `deleteKeys` matches only legacy bytes, so in those terminals it would undercount Crush deletions.

## 8. Draft persistence and history
- No saved drafts: draft lives in `textarea.Model` and `promptHistory.draft` while browsing (`history.go:96-184`). Quitting loses the draft and all paste attachments. [source, re-checked]
- History = sent user messages from the project SQLite DB (`ListUserMessages` per session, or `ListAllUserMessages`), shell commands prefixed `!` (`history.go:27-42`), capped at the 200 most recent. [source, re-checked] The 200 cap arrived in commit `978d49d` (2026-09-15), first tagged in v0.96.0. [source, re-checked]
- A history recall replaces the whole box, so unsent's `keepOld` should treat it as "not similar" and move the old draft to history. [guess]

## 9. External editor (ground truth)
- Ctrl+O; refused while the agent is busy ("Agent is working, please wait..."). [source, re-checked]
- `openEditor` (`ui.go:4385-4425`): writes the value to `os.CreateTemp("", "msg_*.md")`, runs `x/editor` with the cursor position, reads back. [source, re-checked] The lane saw `/tmp/msg_*.md` paths (`run/dumps/args.last`: `/tmp/msg_2564505293.md`). [measured]
- `$EDITOR` only, no `$VISUAL`, fallback `nano`; for unknown editors only the path is passed. [source per lane, x/editor v0.2.0; not re-checked] [measured: args.last holds only the path]
- In: exact textarea value (with `!` prepended in bang mode, attachments excluded). The lane reports a 193-byte dump that byte-matched the draft; that dump no longer exists (the one remaining dump is 1,378 bytes), so the byte-match is [measured by the lane, capture not retained].
- Out: `strings.TrimSpace(file)`; an **empty** file (0 bytes) gives "Message is empty" and no change; a whitespace-only file sets the value to empty. [source, re-checked; refined]
- Test recipe: `EDITOR` = script that copies its last argument and exits 0 (`/tmp/crush-lane/run/dump-editor.sh`), then Ctrl+O. Do it last, or allow for the trim.

## 10. Detection recipe: `crushBox(s *screen)`
The screen snapshot must keep per-cell foreground/background, not only the faint flag.
1. **Candidate rows:** `cells[0] == " "` and `cells[1:5]` is `"  > "`, `" ⏸  "`, `" !  "` or `"::: "`. [source + measured]
2. **Bottom anchor:** scan up from the last row, skipping help bar and blank rows; the lowest candidate is `B`; require row `B+1` blank over `[0, 5+width)`. [measured layout; guess as a rule]
3. **Walk up** while `"::: "`, at most 15 rows; include and stop at a first-row prompt → `T`. A `":::"` top row with the cursor visible means scrolled; with the cursor hidden it may be a blurred box at offset 0 (ambiguous). [source + guess]
4. **Width:** `cols-38` when the sidebar is present (content at columns ≥ cols-32 on header rows, column cols-33 blank on box rows), else `cols-6`. Read `cells[5 : 5+width]`. [measured + guess]
5. **Empty:** h == 3, rows 2–3 blank, row 1 in the placeholder list, and row 1's foreground differs from typed text (e.g. equals the help-bar description colour) or the cursor sits at (5, T). A typed `Ready...` puts the cursor at column 13. Never use faint. [measured + guess]
6. **Capped:** h == 15. [source]
7. **Bang vs YOLO:** first-row `!` with no `YOLO MODE` badge = bang; with the badge, bang only if the prompt background differs from the badge background. Bang → prepend `!`. [measured + source]
8. **Reject** (`ok=false`) when a dialog frame crosses columns 5..5+width on box rows, no run is found (inline editor, onboarding), or the run exceeds 15 rows. [guess]
9. **Attachments:** parse row `T-1` for `≡ / ■ / ▲ name ✕` chips; pair `paste_N.txt` chips with captured pastes. [measured + guess]
10. **Unwrapping is ambiguous.** Soft wraps and hard newlines both render `:::`. The bubbles rule says a soft wrap happened when row i's content plus the next word plus its spaces would exceed `width`, but a hard newline followed by a long first word produces the same picture, and trailing spaces cannot be read from the screen once the row is right-trimmed (the lane's steps 4 and 10 contradicted each other on this). Use the heuristic only as a fallback; prefer tracking Ctrl+J / Shift+Enter (legacy and CSI-u forms) in the input stream to know where hard newlines are, and treat Ctrl+O as the exact answer in tests. [source + guess; corrected]

## 11. Stability across versions [source, re-checked by scanning tags]
- `"::: "` continuation prompt and prompt width 4 exist from v0.10.0 (2025-09-24, old `internal/tui/components/chat/editor/editor.go`) through v0.96.1 (2026-09-21).
- TUI rewrite from `internal/tui` to `internal/ui` happened between v0.30.0 (2025-12-27) and v0.40.0 (2026-02-09).
- `pasteLinesThreshold = 10` present by v0.40.0; `pasteColsThreshold = 1000` since **v0.51.3 (2026-03-24)**.
- **Growing box (DynamicHeight, 3..15) since v0.53.0 (2026-03-27).** v0.52.0 still set a fixed `SetHeight(layout.editor.Dy() - 2)`. The lane said v0.60.0; corrected.
- Bang prompt since **v0.78.0 (2026-06-19)**; plan prompt `⏸` since **v0.95.0 (2026-09-16)**. The lane said "bang by v0.80" and gave only a commit for plan.
- Release cadence: 7 tags from v0.93.1 to v0.96.1 in about a month; main already adds the Ctrl+B sidebar toggle.
- Changes that would break the reader: a new prompt glyph or width; sidebar width or the −6/−38 margins; MaxHeight; attachments-row position; placeholder list; an added border; replacing the textarea; the Ctrl+B toggle shipping; help-bar height; very short windows; a terminal drawing `⏸` (U+23F8) two cells wide. [guess]
- A reader that stops matching fails safe (`ok=false`), as with Claude. [guess]

## Risks
1. Placeholder is grey, not faint; unsent's `faintFrom` would save `Ready?` as a draft. Needs colour-aware snapshots or the placeholder list. [measured]
2. Hard newlines vs soft wraps are not reliably separable from the screen (§10.10). [source + guess]
3. Large pastes become in-memory attachments lost on quit; only the input stream has the bytes. With CR-separated pastes, the trigger is total size over 1000 bytes, not line count. [source + measured]
4. Kitty flag 1 and modifyOtherKeys 2 change delete, newline and Ctrl+J encodings in capable terminals; `deleteKeys` undercounts. [source + docs]
5. Ctrl+D is delete-forward only outside compact chat. [source]
6. Wrap width depends on layout; the Ctrl+B toggle on main makes `cols ≥ 120` unreliable. [source]
7. Cursor can sit outside the box after scrolling; short windows hide box rows. [measured]
8. Bang and YOLO share the `!` glyph; only background colour or the badge tells them apart. [measured]
9. Backslash+Enter behaviour may be "fixed" to insert a newline in a later release. [source]
10. Fast release cadence and recent layout changes (v0.53.0 height model, v0.95.0 plan mode) require re-checking the reader against real captures per release. [source]

## Open questions
- Do Ghostty, iTerm2 and Terminal.app send LF or CR inside bracketed pastes? That decides which threshold fires. [open]
- Through unsent's pty, does the outer terminal honour Crush's `ESC[>1u` push, so Ctrl+W arrives as `ESC[119;5u`? Measure in Ghostty through unsent. [open]
- Should unsent store Crush paste/image attachments next to the draft? [open]
- The box was not observed next to real chat content, a busy agent (`Working...` placeholders), or an inline question editor (the lane's chat session was a messageless row inserted into the scratch SQLite DB). [open]

## Verification notes
Checked directly (source clone `/tmp/crush-lane/crush` at `v0.96.1`, module cache `/tmp/crush-lane/gomod`, ultraviolet `terminal_reader.go` fetched from GitHub at `006e29f97886`, byte capture `/tmp/crush-lane/run/typescript`, unsent's `wrap.go` / `screen.go` / `extract.go`):
- **Held:** go.mod versions; paste thresholds and byte counting; attachment naming and `pasteIdx`; `\r\n`-only normalization; prompt strings and styles for all modes; blurred first row `"::: "`; placeholder lists; MinHeight 3 / MaxHeight 15 / DynamicHeight; backslash+Enter bug; `/` palette only on empty box; Ctrl+D compact override; bubbles delete key map; Bubble Tea's unconditional modifyOtherKeys 2 + kitty flag 1; `openEditor` temp file and TrimSpace; history 200 cap; Ctrl+B only on main; sidebar width 32 and breakpoints 120/30; tag dates. Byte capture confirmed every escape-sequence count the lane gave (136/136 sync pairs, `ESC[>4;2m`, `ESC[>1u`, `ESC[1 q`, etc.) and that the placeholder is `ESC[38;5;59;22m`, not faint.
- **Changed:**
  - "Line count unlimited" became "capped at 10,000 logical lines" (bubbles `maxLines`).
  - Height model change moved from v0.60.0 to v0.53.0. The 1000-byte threshold is dated v0.51.3, bang to v0.78.0, plan mode to v0.95.0.
  - Added that a CR-separated paste counts as one segment, so the 1000-byte rule fires on total size. This comes from the source; I did not measure it.
  - Refined the editor round trip: only a 0-byte file gives "Message is empty".
  - Flagged the lane's recipe contradiction (right-trim vs trailing-space unwrap). Marked unwrapping as ambiguous and recommended input-stream newline tracking.
  - Added CSI-u forms for Ctrl+J and Shift+Enter.
  - Downgraded iTerm2 CSI-u behaviour to guess.
  - Corrected the checksum wording: it covers the tarball, not the binary.
  - Fixed drifted line citations: AltScreen 3634, placeholders 4801-4815, paste handler 5723, threshold 5786-5801, pasteIdx 5919, prompt setup 4427-4459.
- **Not re-checked (kept with the lane's tag):** truecolor hex values; theme-per-provider commit; x/editor `$EDITOR`/nano behaviour; attachment chip glyph details beyond the lane's pane observation; the cursor-above-box bug and the 193-byte byte-match. Neither has a retained capture: the only surviving editor dump is a different 1,378-byte file.
- **Not done, by rule:** I did not run Crush (it is an agent CLI), so every correction above comes from source reading, not new measurement.
- **Dropped:** the lane's fifth open question about an unrelated relayed user message ("delete that warning"). It is outside this profile, so I left it to the orchestrator.
- **Cleanup:** `/tmp/crush-lane` (about 131 MB) is still on disk and can be deleted when no lane needs it.
