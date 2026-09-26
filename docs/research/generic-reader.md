# Generic input-box reader for agent CLIs with no profile: verified

Tags:
- **measured**: backed by a capture or log the lane kept in `/tmp/ugl-*`, re-read or re-run in this pass unless noted.
- **measured (no capture)**: the lane reports running it, but no artifact survives to re-check.
- **source**: `file:line` in unsent at `064c7f4` (`the repo checkout`) or in a Go module, re-read in this pass.
- **docs**: vendor documentation, not re-verified here.
- **guess**: inference, untested.

## Summary

We tried one approach on six agents: Codex 0.151.0, Copilot CLI 0.0.422, Gemini CLI 0.61.0, opencode 1.18.32, Crush 0.96.1 and aider 0.86.2. The reader learns the box layout from the screen where typed text first appears: the text column, the marker on the first row, the prefix on the rows below, and the colour of typed text. On every later screen it reads that layout, anchored on the cursor, and hands the rows to unsent's unchanged stitcher. The prototype is `/tmp/ugl-proto/generic.go`, 294 lines. Replayed through unsent's own `charmbracelet/x/vt` emulator, it reached the exact typed draft (2 long lines plus 30 short ones) on all six agents (**measured**, re-run in this pass).

What the replay did not test:
- **Calibration input.** Calibration was given the known typed text as input, and it ran on the screen that already showed the whole 150-character first line. Calibrating from the key stream after about 6 characters, as the lane describes, is **untested**. At that point there is no wrapped second row, so the continuation prefix and aider's character wrap cannot be learned yet.
- **Delete keys.** No delete keys were fed, so the stitcher's delete path never ran.
- **Real tick timing.** Reads happened only at quiet points (gaps over 30 ms), not on unsent's real 400 ms tick.
- **Assertions.** The tests print results and assert nothing. They pass even when the reader breaks.

Detecting boxes by shape alone is unsafe. Copilot's trust dialog is a box (**measured**), and the lane reports that Gemini's API-key dialog is an input box showing the key (**measured (no capture)**).

None of the agents keeps a live draft file. Codex and opencode write the draft to history only when Ctrl+C clears it.

In tmux with `extended-keys on`, Ctrl+W, Shift+Enter, Meta+Enter and Ctrl+] arrive as `ESC[27;…~` sequences (**measured**). By the same rule Ctrl+Z, Ctrl+U and Ctrl+K almost certainly do too (**guess**, not sent). unsent's delete-key list and its Ctrl+Z check miss these forms. GNU screen 4.00.03 drops bracketed paste, and zellij keeps it (**measured**). Native Windows does not build (**measured**).

## 1. Setup (lane, measured (no capture) except where noted)

- The lane used a private tmux server, `tmux -L ugl -f /dev/null`, at 100x30.
- Gemini, opencode, Crush and aider were installed under `/tmp` (`/tmp/ugl-npm`, `/tmp/ugl-uv`), with `HOME` and `XDG_*` pointed at `/tmp/ugl-home`. That directory exists and holds `.gemini`, `.aider`, `.config`, `.local` and `.cache` (**measured**).
- Codex ran from `/private/tmp`, which was already trusted in `~/.codex/config.toml`. The lane checked that config by hash before and after the run.
- cursor-agent was not logged in, so it was not measured. Claude Code was not re-run.
- Output was recorded with `tmux pipe-pane` into a timestamped stream, `/tmp/ugl-rec/<agent>/stamped`. For Codex the recordings are per-scene raw files, `/tmp/ugl-rec/codex/{0-empty,1-long,…,raw}`. All of these exist (**measured**).
- The recordings are replayed through the same emulator unsent uses. The prototype directory is a copy of unsent at `064c7f4`, matching the repo HEAD (**measured**).

## 2. What each agent's box looks like

The table comes from the lane's captures. This pass re-checked the rows marked ✓: sync-mark counts and the empty-state screens for aider, plus the placeholder and calibration outputs printed by the replay. The other cells are **measured (no capture re-read)**.

| Agent (framework) | Screen | First-row marker / later rows | Real cursor | Empty box shows | Height cap at 30 rows | `?2026` marks | Paste placeholder |
|---|---|---|---|---|---|---|---|
| Codex 0.151.0 (ratatui) | inline | `› ` / 2 spaces ✓ | visible, at insertion ✓ | dim "Ask Codex to do anything" ✓ | ~26 rows ✓ (26-row read) | yes: 95 `?2026h` in the recordings ✓ (lane: 94 pairs) | `[Pasted Content 2110 chars]` |
| Copilot CLI 0.0.422 (Ink) | inline | `❯ ` / 2 spaces ✓ | hidden, parked below the box; fake cursor is an SGR 7 cell ✓ (reverse-cell anchor) | plain-colour hint ✓ (read as placeholder) | 10 rows | 0 ✓ | `[Paste #1 - 41 lines]` |
| Gemini CLI 0.61.0 (Ink) | inline | ` > ` / 3 spaces ✓ | hidden at the insertion point; fake SGR 7 cell ✓ | grey text, not dim ✓ | 10 rows | 0 ✓ | `[Pasted Text: 41 lines]` |
| opencode 1.18.32 (opentui) | alt screen | `┃  ` on every row ✓ (textCol 16) | visible, at insertion | other-colour hint | 10 rows | yes: 68 `?2026h` ✓ (lane: 67) | `[Pasted ~40 lines]` |
| Crush 0.96.1 (Bubble Tea) | alt screen | `   > ` / ` ::: ` ✓ | visible, at insertion | "Ready?" | ~26 rows | yes: 81 `?2026h` ✓ (lane: 78) | none: a paste becomes an attachment chip |
| aider 0.86.2 (prompt_toolkit) | inline | `> ` on every row ✓ | visible, at insertion | nothing ✓ | fills the screen | 0 ✓ | none (inline) |

The `?2026` counts from this pass count `?2026h` across the whole recording files. That is not the lane's "pairs" measure, so small differences are expected. The zero/non-zero split matches exactly, and that split is the part that matters.

More facts from the lane:
- **Wrapping.** Five agents wrap at word boundaries. aider breaks mid-word at full width, and its wrapped rows carry the same `> ` prefix (**measured**: the calibration learned `charWrap` for aider only).
- **Dead copies.** After Ctrl+C, aider leaves the old draft above a fresh prompt (**measured**, `/tmp/ugl-rec/aider/5-cleared.txt`). The old copy is separated from the new `> ` prompt by blank rows, `^C again to exit` and a `─` rule, which is why the anchor-based read did not pick it up.
- **Keys typed during start-up** go to dialogs. Crush's "initialize now?" dialog ate `do n`, and Codex under `script(1)` showed its sign-in screen (**measured (no capture)**: the Crush dialog is not in the stamped file).
- **Copilot's trust dialog** ("Confirm folder trust… Do you trust the files in this folder?") is in the recording (**measured**).
- **Cursor visibility.** Copilot and Gemini hide the cursor and never re-show it (**measured (no capture re-counted)**). The replay's anchor reports `reverse-cell` for both, consistent with this.
- **Keyboard modes.** Codex sends `ESC[>4;0m` then `ESC[>5u`, Crush sends `ESC[>1u` and `ESC[>4;2m`, and opencode sends `ESC[>4;1m`. All six enable `?2004h` (**measured (no capture re-counted)**).
- **Gemini ignores SIGHUP.** Its processes survived their panes being killed, and the lane killed four pairs by path (**measured (no capture)**). None remain now.

## 3. Approaches

### A. Calibrate from the typing, then read the learned layout (recommended)

Code: `/tmp/ugl-proto/generic.go`. Replay tests: `prog_test.go`, `neg_test.go` and `codex_test.go`.

**Calibration**, as implemented (**source** `/tmp/ugl-proto/generic.go:144-184`):
1. Find the row whose text, from some column `x`, does not start with a space, is at least 6 characters long, and is a substring of the typed text. Pick the one whose substring comes earliest in the typed text. This tolerates lost first keys: Crush calibrated on text starting at `othing; reply…`.
2. Learn from that row:
   - `textCol = x`;
   - `marker` = columns `[0, x)`;
   - the style of the first cell;
   - the placeholder, read from the pre-typing screen at the same spot.
3. If the next row is also part of the typed text, learn its prefix as `cont`. If the first row does not end in a space and row + next row appears in the typed text, set character wrap and its width (aider).

How it was exercised (**measured**, re-read in `prog_test.go` and `neg_test.go`):
- Calibration ran on the first quiet screen containing `sierra tango`, the end of the 150-character first line. It was passed the known typed string, not a string derived from keystrokes.
- The "about 6 characters" in the lane's write-up is the minimum length check in code, not the calibration point that was tested.
- Calibrating early, on a single short row, cannot learn `cont` or `charWrap` (**source**, lines 173-181, which need a second row). A production design needs either a later calibration point or a second step that learns `cont` (**guess**).

Learned values (**measured**, re-printed by the replay in this pass):

| Agent | textCol | marker | cont | typed style |
|---|---|---|---|---|
| Codex | 2 | `› ` | 2 spaces | default |
| Copilot | 2 | `❯ ` | 2 spaces | default |
| Gemini | 3 | ` > ` | 3 spaces | white |
| opencode | 16 | `             ┃  ` | same | eeeeee |
| Crush | 5 | `   > ` | ` ::: ` | eeeeee |
| aider | 2 | `> ` | `> ` | green, character wrap |

This pass printed the Codex values directly. The others come from the lane and are consistent with the replay reading every agent correctly.

**Reading on each save** (**source** `generic.go:199-280`):
1. **Anchor.** Use the real cursor. If it is hidden, use the single reverse-video cell. Otherwise use the row nearest the last known top.
2. **Top row.** Take the nearest `marker` row at or above the anchor. If no marker row exists but the anchor row carries `cont`, the box has scrolled (Crush): take the run of `cont` rows. If `marker == cont` (aider, opencode), walk up from the anchor while rows carry the prefix.
3. **Extend down** while rows carry `cont`.
4. **Keep only typed-style text.** Foreign style on the first row means the placeholder, so the box is empty. Foreign style on a later row is chrome, so stop.
5. **Hand off** to the stitcher with `capped = true`.

`capped = true` always:
- The `rows2cap` comment says treating an unscrolled box as scrolled "only costs a little precision" (**source** `extract.go:106-109`). That comment is about Claude's one-row margin.
- Applying it to every read of every agent extrapolates that comment (**guess**). It held in these replays, but they fed no delete keys, so its cost when text is deleted is **untested**.

**Results** (**measured**, re-run in this pass: `go test -run 'TestStampedProgressive|TestNegative|TestCodexFrames|TestGenericReplay'` in `/tmp/ugl-proto`):

| Agent | Reads | "Box not found" | Exact full 32-line draft reached? |
|---|---|---|---|
| Codex | 63 frames (one read after each closed `?2026` frame) | 0 | yes |
| Copilot | 66 | 0 | yes |
| Gemini | 65 | 0 | yes |
| opencode | 65 | 0 | yes |
| Crush | 64 of 65 bursts | 1 (quit dialog) | yes, **minus the first 4 characters `do n`** |
| aider | 65 | 0 | yes |

Caveats this pass found in the results:
- **Crush's "exact" allows a missing prefix.** The test accepts `"do n"+draft == full` (**source** `prog_test.go`), because the start-up dialog ate `do n` and those characters never reached the box. The reader recovered exactly what the box held, which is correct, but it is not the full typed text.
- **Every agent prints `exact=false` for the final pre-clear draft.** The final draft is 373 characters against 366 wanted: it still holds `\nline30`, which six Backspaces had deleted. The replay fed no keys, so the stitcher kept text it could no longer see. That is the designed "never lose" behaviour, not a read error. The same run shows Crush with `len(got)=0` there, because Crush never showed an empty box (Ctrl+C opened a quit dialog), so the test's "last before clear" slot stayed empty. It is a test-bookkeeping artifact, not a lost draft.
- **Codex is covered only by `TestCodexFrames`.** It has no `stamped` file, so the negative controls below cover five agents, not six.
- **The tests assert nothing.** All four `PASS` whatever they print. A broken reader also shows `PASS`.

**Negative controls** (**measured**, re-run; `neg_test.go`, five agents: aider, Copilot, Crush, Gemini, opencode):
- **Pre-typing screens.** No non-empty draft came out of any screen before typing: 1 to 9 screens per agent (Crush 1, aider 3, Gemini 4, opencode 2, Copilot 9). The lane said "3 to 9".
- **Cross-agent.** No other agent's calibration read a draft from this agent's screens: 0 of 4 for each of the five.
- **Crush quit dialog.** The last screen, the Ctrl+C quit dialog, gave `marker not found (hidden-cursor(rev=0))`. The stitcher is not updated on a miss, so the last draft stays.

**Positive control: mutation** (**measured**, new in this pass, on a copy at `/tmp/ugl-verify`). Appending `"x"` to the learned `marker`:
- **Codex, Copilot, Crush, Gemini:** the exact-draft line went `false`.
- **aider, opencode:** the exact draft was **still reached**. With a broken marker, the read falls through to the "scrolled box" branch, which accepts any run of `cont` rows at the cursor. For agents where `marker == cont`, the marker adds nothing.

So the lane's recipe step 3 ("the exact-draft line should go false") holds for only 4 of 6 agents. For aider and opencode, the only thing checked is "the cursor row carries the learned prefix". That is a weaker guard against reading the wrong region (**measured** that it holds on these recordings; the broader risk is a **guess**).

**Where it fails, and how to fail safe:**
- **Calibrating on the wrong box** (a login or API-key field). The options are:
  - (a) Strict: confirm the calibration only after a full cycle of type, Enter, empty box, type again in the same layout, and save nothing before that. The session's first draft goes uncovered.
  - (b) Permissive: refuse when the box shows words like `API key`, `password`, `token` or `passphrase`, or when the input is key-shaped (no spaces, more than 20 characters).
  - Both are **guess**. The Gemini dialog behind this risk has no surviving capture: the stamped Gemini recording only shows `Authenticated with gemini-api-key`.
- **Half-drawn frames on agents without `?2026`** (Copilot, Gemini, aider; **measured** 0 marks). Only quiet points were replayed. A quiet-gap guard of 30 to 50 ms, capped like `maxFrameWait`, is **guess**/untested. unsent's real reads come on a 400 ms tick (**source** `wrap.go:25`), not at quiet points.
- **Wrong "empty".** Accept an empty reading only after a key that can empty the box (Enter, Ctrl+C, Ctrl+U, Esc) within `deleteWindow`, and with the anchor on the region's first row. The rule is a **guess**. The Crush quit-dialog miss is **measured**.
- **Soft wrap vs typed newline.** Without a profile, the width is the widest row seen: a lower bound. **Measured** widths: Codex 94, Copilot 94, Gemini 89, opencode 66, Crush 90 (Codex re-seen as 94 in this pass). A wrong guess changes line breaks, never words (**guess**, from how `unwrap` works, **source** `stitch.go:367`). In these replays the final 32-line drafts had correct breaks (**measured**).
- **Transcript copies of sent prompts** (Codex, Copilot, Gemini). Not measured: no prompt was ever sent.
- **Layout change mid-session.** The marker is not found, so the last draft is kept and nothing new is saved until re-calibration (**guess**).

### B. Keystroke ledger: a verifier, not a source (design only, guess)

Replaying keys through a model line editor breaks on history recall, completion, slash menus, `@` chips, vim modes, mouse clicks, Crush's paste-to-attachment and paste placeholders. Use it for three things:
1. the calibration input, which the prototype took from an oracle string instead;
2. an echo check: recent typed words must appear near the anchor, or the save is skipped;
3. the newline-vs-wrap decision.

It must parse the CSI key encodings in section 5.

### C. Detecting box shapes

Shape alone cannot pick the box:
- Copilot's trust dialog is a box (**measured**, in the recording).
- Gemini draws a `─` rule above its hint line, and its API-key dialog is a rounded input box (**measured (no capture)**).

Use shapes only as a tie-breaker after calibration (**guess**).

### D. Agents' own draft and history files

| Agent | Live draft file? | Saves on Ctrl+C clear? | Saves when killed? |
|---|---|---|---|
| Codex 0.151.0 | no | **yes**, `~/.codex/history.jsonl`; a paste is stored as its placeholder only | **no** (unique-word test) |
| opencode 1.18.32 | no | **yes**, `$XDG_STATE_HOME/opencode/prompt-history.jsonl` | not tested |
| Copilot CLI | no | no | not tested |
| Gemini CLI | no | no | not tested |
| aider | no | no (`.aider.input.history` holds sent prompts only) | not tested |
| Crush | no | no (not in `.crush/crush.db*`) | not tested |

Evidence:
- **Codex (measured):** `~/.codex/history.jsonl` holds 2 lines matching `do nothing; reply ok` and 1 matching `[Pasted Content 2110 chars]`, the 3 entries the lane reported.
- **Other rows:** **measured (no capture)**.
- **opencode:** its verified profile narrows the rule. opencode saves a Ctrl+C-cleared draft only when it has 20 or more trimmed characters or has parts (see `verified/opencode.md`).

Verdict: no agent keeps a file that covers a crash.

### E. Anchoring on the cursor (measured)

The replay's anchor was `cursor` for Codex and `reverse-cell` for Copilot and Gemini (the Copilot and Gemini reads in the mutation run name it). Copilot parks the hidden cursor below the box, so its reverse-video cell is the only usable anchor. With several reverse cells (a highlighted menu), the reader should not guess (**guess**).

## 4. Generic paste placeholders

unsent's regex matches only Claude's form: `\[Pasted text #\d+(?: \+(\d+) lines?)?\]` (**source** `paste.go:16`; the lane cited `:15`, the comment line).

The other formats (**measured (no capture re-read)**):
- `[Pasted Content 2110 chars]` (Codex; also **measured** in `~/.codex/history.jsonl`);
- `[Paste #1 - 41 lines]` (Copilot);
- `[Pasted Text: 41 lines]` (Gemini);
- `[Pasted ~40 lines]` (opencode);
- Crush: an attachment chip.

A generic form is `\[Paste[^\]]{0,40}\]`, matched to a captured paste by line count ±1 or by character count (**guess**). Unmatched pastes still sit in `record.Pastes`, and `unsent show` prints them (**source** `main.go:159-165`).

## 5. Multiplexers, terminals, SSH, Windows

**Where unsent sits.** unsent passes every byte between the terminal and the agent untouched and reads the agent's output into its own emulator (**source** `wrap.go:27-29`). A multiplexer outside unsent does not change the screen unsent reads. It changes the key bytes unsent receives, `TERM`, and how the window closes.

**Key bytes** received by an app inside the pane (**measured**, re-decoded from `/tmp/ugl-bin/{tmux,tmux-ext,zellij,screen}.log`). The keys were sent with `tmux send-keys` from `/tmp/ugl-bin/keys.sh`, not typed on a real keyboard.

| Key | tmux 3.7c default | tmux `extended-keys on`, app asked `ESC[>4;2m` | zellij 0.45.1 (inside tmux) | GNU screen 4.00.03 (inside tmux) |
|---|---|---|---|---|
| Backspace | `7f` | `7f` | `7f` | `7f` |
| Delete | `ESC[3~` | same | same | same |
| Ctrl+W | `17` | `ESC[27;5;119~` | `17` | `17` |
| Shift+Enter | `0d` | `ESC[27;2;13~` | `0d` | `0d` |
| Meta+Enter | `1b0d` | `ESC[27;3;13~` | `1b0d` | `1b0d` |
| Ctrl+] | `1d` | `ESC[27;5;93~` | `1d` | `1d` |
| Bracketed paste `p1\np2\n` | `ESC[200~p1\rp2\rESC[201~` | same | same, start marker in its own read | `p1\rp2\r`, **no markers** |

Not sent, so **not measured**: Ctrl+Z, Ctrl+U, Ctrl+K, Alt+Backspace, and kitty `CSI u` forms. Codex requests the kitty protocol (`ESC[>5u`). Whether tmux 3.7c honours that request was not tested.

What follows for unsent:
- **Delete keys.** `deleteKeys` knows only legacy bytes: `oneCharKeys = {7f, 08, ESC[3~}` and `manyKeys = {17, 15, 0b, 1f}` (**source** `wrap.go:507-509`, function at `:514`; the lane cited `:487-512`). CSI forms of Ctrl+W, Ctrl+U and Ctrl+K are not counted. This is a hint only, so it costs stitch precision, never text (**source** `wrap.go:459-461`). Alt+Backspace (`1b7f`) is also absent. That Codex binds Alt+Backspace is a **guess**.
- **Ctrl+Z.** Suspend fires only on `n == 1 && buf[0] == ctrlZ` (**source** `wrap.go:115`; the lane cited `:108`). Under extended keys, Ctrl+Z should arrive as `ESC[27;5;122~` by the same rule as the measured Ctrl+W (**guess**, strong). Only a lone byte triggers suspend, so a CSI Ctrl+Z would reach the agent inside its own session: the hang the wrapper exists to prevent (**source**, the comment at `wrap.go:161-164`; the hang itself was not reproduced). This also affects the existing Claude reader, but only if Claude Code requests extended keys, which was not checked (**guess**).
- **tmux.** Without `extended-keys`, Shift+Enter equals Enter (**measured**).
- **zellij.** It keeps bracketed paste but splits the start marker into its own read (**measured**). `paste.go` handles split markers (**source**, per the lane; not re-read in this pass).
- **GNU screen 4.00.03** (the macOS build) strips bracketed paste (**measured**). A multi-line paste reaches the agent as lines ending in CR, which can submit the first line, and unsent's paste tracker sees nothing. Newer screen releases: **guess**.
- **A multiplexer inside unsent** (`unsent tmux`). `extractorFor` switches on the basename and returns `nil` for anything but `claude` (**source** `extract.go:38-45`), so there is no reader today. How a generic reader would behave there is a **guess**.
- **SSH.** `unsent ssh host claude` also gets no reader today (**source**, same switch). `restore` prints the draft when there is no clipboard (**source** `main.go:168-172`; the lane cited `:170-175`). mosh's predicted echo: **guess**.
- **VS Code / Cursor terminals.** Shift+Enter needs a keybinding, and persistent sessions revive the process instead of sending SIGHUP (**docs**, not re-verified).
- **WSL.** A Linux build (lane: cross-compile OK, **measured (no capture)**). `copyToClipboard` falls back to `clip.exe` (**source** `main.go:237,239`; the lane cited `:196`).
- **Native Windows.**
  - `GOOS=windows go vet ./...` fails with `store.go:115:20: undefined: syscall.Flock` (**measured**, re-run). The lane also lists `store.go:139,142` and `syscall.SIGWINCH` at `wrap.go:134` (**source**, lines confirmed; the compiler stops at the first error).
  - `creack/pty@v1.1.24/start_windows.go:18` returns `ErrUnsupported` (**source**).
  - `.goreleaser.yaml` builds `goos: [darwin, linux]` only (**source**).
  - `charmbracelet/x/windows v0.2.2` is an indirect dependency in `go.mod` (**source**).
  - What ConPTY does to `?2026` marks and SGR (**docs**/**guess**), and the console-close handler (**docs**), were not verified.

## 6. An agent that ignores the hang-up

On a signal, unsent saves first, forwards the signal plus SIGCONT (**source** `wrap.go:150-157`), and then keeps waiting on `cmd.Wait()` (**source** `wrap.go:140`). Gemini ignored SIGHUP (**measured (no capture)**), so unsent would sit with no terminal attached. The draft is already saved. A timeout that kills the process group after a forwarded SIGHUP is a **guess**.

## 7. Proposed shape of the change (guess, for the spec)

- `extractorFor` returns a stateful generic reader for unknown names, with an opt-out. Records carry `Reader: "generic"`, and the notice says "best effort".
- Hard rules:
  - `capped = true`;
  - never accept "empty" without an emptying key and the anchor in the box;
  - skip the save when the echo check fails;
  - a quiet-gap guard for agents without `?2026`;
  - a guard against calibrating on a secret field.
- Calibration must take its typed text from the key stream, not an oracle, and must learn `cont` after the draft first wraps or spans two rows. The prototype never exercised either.
- Tests must **assert**, not print:
  - exact draft reached per agent;
  - no draft from pre-typing or dialog screens;
  - a mutated calibration must fail **for every agent**. That includes aider and opencode, which need a mutation of `cont` or of the fallback branch, because marker mutation alone does not fail them.
- Feed the recorded keystrokes into the replay, so the delete path and `capped = true` are tested together.
- Add the CSI-u and modifyOtherKeys forms to `deleteKeys` and to the Ctrl+Z check, and add Ctrl+Z, Ctrl+U and Ctrl+K to the key-byte capture.

## 8. Side effects of the lane's run

- `~/.codex/history.jsonl` gained 3 harmless entries (**measured** in this pass, count only).
- Codex, Copilot and Cursor configs were unchanged by hash, and no keychain item was added (**measured (no capture)**).
- Left in `/tmp`: `ugl-proto`, `ugl-rec`, `ugl-bin` (recorders, key logger, a zellij binary), `ugl-npm`, `ugl-uv` and `ugl-home` (throwaway installs with dummy keys). This pass added `/tmp/ugl-verify`, a mutated copy of `ugl-proto`.

## Open questions

- Calibration guard: strict (the first draft goes unsaved) or permissive (keyword and key-shape refusal)?
- On by default for unknown agents, or opt-in (`unsent --generic <agent>`) until `UNSENT_DEBUG_DIR` logs back it?
- Not measured:
  - a sent prompt echoed in the transcript;
  - cursor-agent;
  - Claude Code under the generic reader;
  - early calibration from keystrokes;
  - replay with delete keys;
  - real 400 ms tick timing;
  - Ctrl+Z, Ctrl+U and Ctrl+K bytes under extended keys;
  - a live kitty-protocol terminal;
  - real WSL and VS Code;
  - GNU screen 5.x;
  - CJK/IME input.

## Verification notes

What I checked and what held:
- **Replay re-run.** `go test -count=1 -run 'TestStampedProgressive|TestNegative|TestCodexFrames|TestGenericReplay' -v .` in `/tmp/ugl-proto`: all six agents print `saw-exact-full-30-line-draft=true`, and Codex shows 63 frames. The negative controls print 0 garbage drafts and 0 of 4 cross-agent reads, and Crush's final read is `marker not found`. Read counts match the lane's table.
- **Test and prototype code.** I read `prog_test.go`, `neg_test.go`, `codex_test.go` and `generic.go`: calibration, the read path and the fallback branches.
- **Mutation control, new.** `g.marker += "x"` on a copy (`/tmp/ugl-verify`).
- **Key-byte logs.** I decoded the four logs byte by byte against the table, and read `keys.sh` to see which keys were actually sent.
- **Sync marks.** Counted `?2026h` in the recordings: zero for Copilot, Gemini and aider, non-zero for Codex, opencode and Crush.
- **Codex history side effect.** 2 + 1 entries in `~/.codex/history.jsonl`, count only.
- **unsent source citations.**
  - `paste.go:16`;
  - `wrap.go:25`, `27-29`, `115`, `134`, `140`, `150-157`, `459-461`, `507-514`;
  - `extract.go:38-45`, `106-109`;
  - `main.go:159-172`, `231-249`;
  - `store.go:115`, `139`, `142`;
  - `.goreleaser.yaml` goos;
  - `go.mod` (`creack/pty v1.1.24`, `x/windows v0.2.2`);
  - `creack/pty@v1.1.24/start_windows.go:18`.
- **Windows build failure.** Re-run with `GOOS=windows go vet`.
- **Captures.** The aider cleared screen shows the dead draft separated from the new prompt, and Copilot's trust dialog is in its recording.

What I changed:
- **Calibration.** It was tested with the known typed text as input and at the screen showing the full first line, not from keystrokes after about 6 characters. Early calibration cannot learn `cont` or `charWrap`. Downgraded from "measured" to untested.
- **Tests.** They assert nothing; `PASS` is unconditional. Flagged, and "must assert" added to the proposed change.
- **Mutation.** The lane's recipe claimed a mutated marker makes the exact-draft line go false. That holds for 4 of 6: aider and opencode still pass through the scrolled-box fallback. Added as a measured finding and a weakness.
- **Crush.** Its "exact" draft is the typed text minus `do n`; the test allows that prefix. Stated explicitly.
- **Negative controls** cover 5 agents, not 6: Codex has no stamped file. The pre-typing screen range is 1 to 9, not 3 to 9.
- **`exact=false` / `len(got)=0` lines.** Explained as no delete keys fed (all agents) and a test-bookkeeping artifact (Crush), not read errors.
- **Ctrl+Z.** The lane called its CSI encoding "measured bytes", but Ctrl+Z (and Ctrl+U and Ctrl+K) were never sent. Downgraded to a strong guess.
- **No surviving capture.** The Gemini API-key dialog, the Crush init dialog, Gemini ignoring SIGHUP, and the history files of agents other than Codex are tagged "measured (no capture)".
- **`capped = true` for every read.** The `rows2cap` comment is Claude-specific, so applying it everywhere is a guess, untested with deletes.
- **Line citations.** Corrected drifted numbers: `wrap.go:108` to `115`; `deleteKeys` `487-512` to `507-514`; `main.go:170-175` to `168-172`; `main.go:196` to `237,239`; `paste.go:15` to `16`.
- **Sync-mark counts.** Mine are raw `?2026h` counts (95/68/81), not the lane's pairs (94/67/78). Kept the lane's numbers for reference; the zero/non-zero split, which is what matters, matches.
- **Dropped.** The lane's first open question relayed an unrelated request to delete a memory or CLAUDE.md warning. It is outside this lane, carries no user authority here, and was not acted on.
