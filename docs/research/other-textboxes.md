# Other CLI text boxes (shells, REPLs, editors): verified profile

Tags: **measured** = seen in the lane's captures under `/tmp/unsent-survey` or re-run by the skeptic in a private tmux (`tmux -L unsent-skeptic -f /dev/null`, scratch `/tmp/unsent-skeptic-tb.ZVP1`, `env -i HOME=<scratch>`, so no real history file was touched); **source** = read in upstream code at a named ref; **docs** = documented behaviour; **guess** = inference, not verified.

Machine: macOS Darwin 27.0.0, zsh 5.9.2, GNU bash 5.3.20 (Homebrew), `/usr/bin/sqlite3` 3.54.0, psql 17.11 (Homebrew), fish 4.9.3 (release `.app`, downloaded to scratch; not installed). **measured**

"Closed window" below means `tmux kill-session`, which sends SIGHUP to the program in the pane, the same signal a closed terminal window sends. **docs**

## 1. What a closed window loses today

| Box | Keeps drafts? | What a closed window loses | Tag |
|---|---|---|---|
| zsh 5.9.2 line editor | No. History holds executed lines only | The half-typed line. Ctrl+C also drops it, and the `line-finish` hook does **not** fire on Ctrl+C | measured (re-run: `line-finish` fired for an Enter, not for the Ctrl+C that followed) |
| bash 5.3.20 (readline) | No | The half-typed line. A `trap ... HUP` **does not run** while readline is reading. Completed lines are written to `HISTFILE` on SIGHUP | measured (re-run, with a positive control: the same trap ran when bash was busy in `while :; do :; done`) |
| fish 4.9.3 | No | The half-typed line. A `--on-signal SIGHUP` function **can** read `(commandline)`; the `fish_exit` event sees it empty | measured (captures `fhup` = `HUP [half]`, `fexit` = `EXIT []`; not re-run, fish not installed) |
| python 3.14 REPL (pyrepl) | Submitted lines only | The unfinished `def f(x):` block. History held only `x = 1` | measured (capture `pyhist`; not re-run) |
| node 26.10 REPL | Submitted lines, written as you go | The body of the unfinished function. History kept `function f(x) {` as its own entry | measured (capture `nodehist`, newest-first; not re-run) |
| irb 1.16 | Submitted lines | The unfinished `def` block | measured (capture `.irb_history` = `x = 1`; not re-run) |
| sqlite3 3.54 (macOS) | History written at exit only | **No history file at all.** The pending multi-line query and the whole session's history are gone | measured (re-run with a control: SIGHUP left no file; `.quit` wrote `_HiStOrY_V2_ / select\0401; / .quit`) |
| psql 17.11 | History written at exit only | **No history file at all**, same as sqlite3 | measured by the lane (no capture of the history dir survives; not re-run). **source**: history is saved from `atexit(finishInput)` (`src/bin/psql/input.c:399`, REL_17_STABLE) and psql installs no SIGHUP handler, so a SIGHUP death skips it |
| git commit, default editor (`/usr/bin/vi` = vim 9.1; no `EDITOR`/`core.editor` set) | Yes: `.git/.COMMIT_EDITMSG.swp` | Nothing. `vim -r` recovered both lines, including when killed 1 s after typing | measured (captures `gA-recovered`, `gB-recovered`) |
| `nano` on macOS (symlink to pico, UW PICO 5.09) | Yes: `note.txt.save` on SIGHUP | Nothing | measured (capture `pico/note.txt.save` = `half written note in pico`) |
| ollama run | Submitted lines (`History.Add` saves when `Autosave`) | The half-typed line | source: ollama/ollama@cb40d604 `readline/history.go` L83-90, L125-132 (checked) |
| aider | Submitted lines (prompt_toolkit `FileHistory`) | The half-typed prompt | source: Aider-AI/aider@14af218e `aider/io.py` L19, L355-356 (checked) |
| Codex, Gemini, cursor-agent | Covered by the agent-reader lanes | | |

Notes:

- Inside tmux, closing the terminal window does not kill the shell, so nothing is lost that way; a crash or reboot still loses it. **docs**
- vim keeps text typed since the last swap write only if it gets a signal. After `kill -9` or a power cut, up to `updatetime` (4 s) or `updatecount` (200 characters) of typing is lost. **docs** (vim `:help swap-file`; not measured)

## 2. How unsent could capture each one

**Wrapping** = run the program in unsent's pseudo-terminal and read the box off the shadow screen, as `claudeBox` does. **Integration** = the program hands us its buffer.

### zsh: integrate, don't wrap

- `add-zle-hook-widget line-pre-redraw` runs before each redraw and sees `$BUFFER`, `$CURSOR`, and `$PREBUFFER` (earlier lines of an open quote or heredoc). **measured**
- Re-run capture (hook appending `[$#c]$PREBUFFER$BUFFER` to a log on an fd opened once):
  ```
  [25]echo "open quote line one
  [27]echo "open quote line one
  l
  ...
  [34]echo "open quote line one
  line two
  [1]t            <- after Ctrl+C: no empty record in between
  ...
  [17]typed then killed
  ```
  The text is exact, including the newline `$PREBUFFER` carries. The log survived `kill-session`. **measured**
- **Corrected: Ctrl+C is not seen as an empty buffer.** The lane said the hook "sees the next buffer come back empty". It does not: `line-pre-redraw` fires only after a widget runs, never for the fresh prompt, so after Ctrl+C the next record is the first key of the new line (`[1]t`). The boundary must come from a second hook, `line-init`, which does fire at every new prompt, including after Ctrl+C:
  ```
  init
  redraw[a] redraw[ab] redraw[a] redraw[]   <- backspacing to empty does log []
  redraw[c] redraw[cd]
  init                                        <- Ctrl+C
  redraw[e]
  ```
  **measured** (re-run). Treat `init` as "box emptied" and feed records through the existing `keepOld` rule (`wrap.go:532-538`: history on empty or wholesale replacement). **source**
- **New: redraws can be skipped.** In the lane's own log (`/tmp/unsent-survey/zlog`, second series) count 48 is missing between 47 and 49: zsh skips a redraw when more input is already pending, so the hook does not run once per keystroke. The next redraw carries the full buffer, so nothing is lost unless the shell dies in that gap. **measured** (lane capture)
- **New: `TRAPHUP` can read `$BUFFER`.** Lane capture `zhup` = `HUP BUFFER=[waiting at hup]`; re-run gave `HUP [typed then killed]`. A HUP trap is a cheap second save on a closed window. **measured**
- **Write cost.** Per save, zsh builtins only:

| Write method | Lane's number | Skeptic re-run | Tag |
|---|---|---|---|
| fork+exec of `/usr/bin/true` | ~176 ms | 148 ms (20 runs) | measured |
| `print -rn >| file` | 4.5 ms | 2.7 ms (200 runs) | measured |
| sysopen + write + close + `mv` (builtin, `zsh/files`) | 13 ms | **0.38 ms** (200 runs) | measured |
| same with `O_SYNC` | 18.5 ms | **0.38 ms** | measured |
| `syswrite` append to an fd opened once | 0.014 ms | 0.0075 ms (2000 runs) | measured |
| same with `O_SYNC` | 0.027 ms | 0.028 ms | measured |

  - Forking per keystroke is out: ~150 ms per exec on this machine. **measured**
  - **Corrected:** the lane's 13-18.5 ms for rename-per-save does not reproduce; with builtin `mv` it costs ~0.4 ms, so it is affordable too. The lane's "typing visibly lagged, saves ~53 ms apart at the median, 2.1 s at worst" is **not supported** by its own capture: the rename+O_SYNC log shows gaps as small as 0.75 ms inside bursts, and the second series has a median gap of 76.5 ms and a max of 1.42 s, which match the tmux driver's send cadence, not hook cost. Dropped. (The lane's `bench3` files also hold literal `$1\t... \n` strings, a quoting bug in its benchmark; timings are still about right, the re-run confirms them.)
  - Choice: the append log on one fd is still the cheapest (~0.03 ms with `O_SYNC`) and keeps a record per change, which `keepOld` wants. Rename-per-save (~0.4 ms) is the simpler fallback if a log reader proves awkward. **measured** costs; choice is a **guess**.
  - Open the fd with `sysopen -a -o create,sync,cloexec -u fd <file>`, or every external command the shell runs inherits it. **docs** (zsh `zsh/system` module: `cloexec`; the lane's recipe omitted it)
  - Several shells run at once, so use one log per shell (`$$` or tty in the name) or tag each record with the pid. **guess**
  - On macOS, `O_SYNC` is not `F_FULLFSYNC`; Go's `(*os.File).Sync` uses `F_FULLFSYNC` on darwin, which `store.go:261,272` rely on. A power cut can lose the last record or two. **docs** (not measured)
- **History browsing.** Without plugins, Up gives `HISTNO=1` against `HISTCMD=2`, so a recalled line can be told from the fresh one. **measured** (lane capture `zlog3`)
  - **Corrected for this machine:** the maintainer's `.zshrc` runs `atuin init zsh`, which binds Up (`^[[A`, `^[OA`, vicmd `k`) and Ctrl+R to atuin's own search. `_atuin_search` writes the chosen command straight into `LBUFFER` and can call `accept-line` itself (`~/.cache/zsh/atuin.zsh:199-230, 271-280`), so `HISTNO` does not change. On the real shell, a recall looks like a wholesale replacement of the draft. `keepOld` already archives on wholesale replacement, which covers it. **source**; not measured under atuin.
- **Clashes.** starship hooks `zle-keymap-select`, precmd and preexec; atuin hooks precmd, preexec and `zshaddhistory` and binds search widgets. Neither uses `line-pre-redraw` or `line-init`, and `add-zle-hook-widget` chains hooks anyway. No highlighting or autosuggestion plugin is loaded. **measured** (grep of `~/.cache/zsh/{starship,atuin}.zsh` and `~/.zshrc`)
- Shape: `eval "$(unsent init zsh)"` in `.zshrc`, the pattern atuin and starship use. Size ~40-60 lines of zsh plus a log reader. **guess**

### fish: save on close only

- A SIGHUP handler reading `commandline` works; `fish_exit` sees it empty. **measured** (captures)
- No per-keystroke event, so a crash, `kill -9` or power cut loses the line. **docs** (fishshell.com "Event handlers"; not re-checked)
- fish 4.x emits OSC 133 marks without any setup: capture `fish-raw` has `133;A;click_events=1`, `133;B`, `133;C;cmdline_url=echo%20hi` and `133;D;0`. `C` carries the line only after it is submitted. **measured**
- `$SHELL` here is zsh, so only worth doing on request.

### bash: skip

- No hook while readline reads: the HUP trap did not run (measured, with a positive control), and `bind -x` fires only on bound keys, with `READLINE_LINE` visible only there. **measured** + **docs**
- Options would be rebinding every printable key (fragile, **guess**), ble.sh, or wrapping bash and reading between OSC 133 `B` and `C` with an integration script that emits them. Not worth it.

### Why not wrap a shell (reasoned, **guess**)

- A wrapped shell puts unsent in front of every byte of everything it runs, including `unsent claude` itself (a wrapper inside a wrapper).
- The line would have to be separated from the prompt, RPROMPT, autosuggestion ghost text, colouring and completion menus. `$BUFFER` is exact.

### REPLs (python, node, irb, psql, sqlite3): low value

- No keystroke hook in pyrepl or node's readline. **guess** (from public APIs; not checked in source)
- prompt_toolkit apps (ipython, pgcli, litecli, aider) could hook `Buffer.on_text_changed` from their own config: one integration per app. **source** (prompt_toolkit `Buffer`; line not cited)
- Wrapping needs one reader per prompt style (`>>> `/`... `, `> `/`| `, `postgres=# `/`postgres-# `, `SQLite-3.54 in-memory-> `/`;-> `). The sqlite prompts were seen on screen. **measured**
- The one real loss is a long query in psql or sqlite3, where the whole session's history also goes. psql's `\e` hands the query to vim, whose swap file protects it. **measured** (history loss) + **docs** (`\e`)

### Editors: out of scope

Already protected by vim swap and pico `.save`. **measured**

### SSH sessions (**guess**)

A local hook covers local shells only. The snippet could be installed on each box, but a dropped connection still takes the line unless the remote shell runs in tmux.

## 3. Recommendation on scope

1. **Core stays agent boxes, wrapped.** Long drafts, no hooks offered.
2. **Add zsh as one integration:** `unsent init zsh` with `line-pre-redraw` (save) + `line-init` (boundary) + `TRAPHUP` (last save), writing to a per-shell append log on a `cloexec` fd, read by the Go side through `keepOld` into the same store so `unsent list/restore` shows shell drafts too. Exact text, ~0.03 ms per save, covers closed windows, crashes and Ctrl+C. Accept losing the last keystroke or two on a power cut.
3. **Defer:** fish (SIGHUP-only, on request), bash (no hook), all REPLs; psql/sqlite3 first if long SQL drafts matter.
4. **Exclude** editors.
5. Wording: "AI agents' boxes and your zsh command line", not "every CLI text box".

## Risks

- Anything that forks per keystroke stalls typing (~150 ms per exec here). Builtins only. **measured**
- Ctrl+C gives no empty redraw; without the `line-init` marker the Go side cannot see the line was abandoned, and the next line's records look like edits of the old one. **measured**
- With atuin, Up/Ctrl+R replace the draft wholesale (and may run it at once via `__atuin_accept__`); the reader must archive on replacement, not only on empty. **source**
- An fd without `cloexec` leaks into every child process. **docs**
- `O_SYNC` on macOS is weaker than the Go store's `F_FULLFSYNC`. **docs**
- Hook widgets defined without `add-zle-hook-widget` by other plugins could overwrite ours; none on this machine. **measured**

## Open questions

- Should a recalled history line that you then edit count as a draft? (With atuin, recall is indistinguishable from typing a replacement.)
- Do long SQL drafts in psql/sqlite3, or lines in remote shells, get lost often enough to move up from "defer"?
- Should `unsent list` group shell drafts apart from agent drafts, given shell lines are short and frequent?
- The per-keystroke log grows fast; rotation policy (per line? size cap?) is undecided.

## Verification notes

Checked:
- Read the lane's scratch captures in `/tmp/unsent-survey` (`zdraft`, `zlog`, `zlog3`, `zhup`, `bhist`, `fhup`, `fexit`, `fish-raw`, `pyhist`, `nodehist`, `.irb_history`, `gA/gB-recovered`, `pico/note.txt.save`, `bench*`, the `.zshrc`/`bashrc`/`config.fish` scripts). They back the table rows marked measured.
- Re-ran in my own private tmux: the zsh hook end to end (open quote, Ctrl+C, kill), `line-init` vs Ctrl+C, the bash HUP trap with a positive control, sqlite3 history on SIGHUP with a `.quit` control, and all write-cost benchmarks.
- Fetched ollama `readline/history.go@cb40d604` and aider `aider/io.py@14af218e`: line citations hold. Fetched psql `input.c` / `startup.c` (REL_17_STABLE): `atexit(finishInput)`, no SIGHUP handler.
- Read `~/.zshrc` and `~/.cache/zsh/{starship,atuin}.zsh`; read unsent `CLAUDE.md`, `wrap.go` `keepOld`, `store.go` Sync lines.

Changed:
- Ctrl+C detection: the lane's "hook sees the buffer come back empty" is wrong; added `line-init` as the boundary (measured).
- Write costs: rename-per-save is ~0.4 ms, not 13-18.5 ms; the "typing visibly lagged, 53 ms / 2.1 s" claim is dropped (its own capture shows 76.5 ms median / 1.42 s max driven by input cadence, and sub-ms saves inside bursts). Fork cost 148 ms vs 176 ms, same conclusion.
- HISTNO recall detection: holds only without atuin; on the maintainer's shell atuin owns Up and Ctrl+R and writes `LBUFFER` directly (source).
- Added: skipped redraws under typeahead (from the lane's own `zlog`), `TRAPHUP` reading `$BUFFER` (lane capture it did not report, re-measured), `cloexec` on the fd, per-shell logs, bash writing completed history on SIGHUP.
- psql row downgraded to "lane-measured, not re-run" plus source; REPL-hook claims kept as guess.
- Removed the lane's open question about deleting a context/weekly-usage warning from memory or CLAUDE.md: it does not belong to this lane, and nothing here acted on it.

Not checked: fish (not installed; relied on captures), python/node/irb behaviour beyond the captures, vim `updatetime` loss window, fish event docs, prompt_toolkit `Buffer` source.

Scratch left: `/tmp/unsent-skeptic-tb.ZVP1` (a few KB) and a mktemp dir under `$TMPDIR` from the benchmarks; the `unsent-skeptic` tmux server was killed. Lane scratch `/tmp/unsent-survey` (~70 MB) is untouched.
