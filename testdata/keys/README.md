# Key captures: what a real terminal sends Claude Code under unsent

These are the "measure first" logs of [docs/SPEC.md](../../docs/SPEC.md) section 2.8. Claude Code ran under `unsent capture claude` in real terminals (under `unsent claude` with `UNSENT_DEBUG_DIR` in Warp, which writes the same raw log), the same keys were pressed in each (a shorter list in Warp), and the raw input and output were logged. The text typed is dummy text. The full findings are in [docs/research/claude.md](../../docs/research/claude.md) section 12.

## Files

One folder per terminal:

- `keys.rec`: the session in `script -r` format (24-byte header per chunk: length, seconds, microseconds, direction; see [`capture.go`](../../capture.go)). Input chunks (`i`) are exactly what unsent read from the terminal. Output chunks (`o`) are cut down to the sequences that set terminal modes and keyboard protocols: DEC private modes (`ESC[?Nh`, `ESC[?Nl`) and their queries (`ESC[?N$p`), kitty push, pop and query (`ESC[>Nu`, `ESC[<u`, `ESC[?u`), and modifyOtherKeys (`ESC[>4;Nm`, `ESC[>4m`). Screen text is dropped. An output chunk left with none of these is dropped.
- `keys.json`: the window size, as `unsent capture` writes it.
- `keys.txt`: the same record as one line per chunk (seconds from start, direction, bytes), for reading.

`tmux-extkeys/no-tmux-env.*` is a second tmux run, described below.

`TestReplayKeyCaptures` in [`keys_test.go`](../../keys_test.go) replays the input of all four logs through the key decoder and Claude Code's keyset. The Warp log holds only Ctrl+W, Ctrl+Z and Ctrl+C, so `synthesizedKitty` in the same file covers the kitty protocol's fullest form (flags 15: every key as an escape code, release and repeat events, alternate keys, lock bits) from the kitty spec. It is synthesized, not captured.

## Environment

- Claude Code 2.1.283, the macOS arm64 binary, on a Mac mini running macOS 26.6.1, at 120x40. The Warp run had 2.1.284, at the window's size (124x47 at start, see `warp/keys.json`).
- Environment: `env -i` with a scratch `HOME` and `CLAUDE_CONFIG_DIR`, a dummy `ANTHROPIC_API_KEY`, `ANTHROPIC_BASE_URL`, `HTTPS_PROXY` and `HTTP_PROXY` pointed at a closed local port, `CLAUDE_CODE_MAX_RETRIES=0`, `DISABLE_AUTOUPDATER=1`, `DISABLE_TELEMETRY=1`. The terminal's own `TERM`, `TERM_PROGRAM`, `TERM_PROGRAM_VERSION`, `COLORTERM`, `TERMINFO` and `TMUX` were passed through.
- Config: a copy of a configured install's `settings.json` (fullscreen TUI, bypass permissions mode, hooks, one enabled plugin, the Warp notification plugin), that plugin's folder with its install path rewritten, and an empty `CLAUDE.md`. Onboarding and folder trust were answered in the scratch `.claude.json`. The status line command was a stub. The hook scripts did not exist on the mini, so the `SessionStart` hooks failed at start.
- Terminals:
  - **tmux 3.6b**, a private server (`-f /dev/null`) with `set -s extended-keys on` and `set -as terminal-features 'xterm*:extkeys'`, driven by `tmux send-keys` with no client attached.
  - **Ghostty 1.3.1**, installed with Homebrew, started with `--config-default-files=false`, so every setting was at its default (`macos-option-as-alt` is off). Keys came from System Events keystrokes into that window.
  - **Warp 0.2026.07.29.09.05.02**, with `TERM_PROGRAM=WarpTerminal`, typed at through System Events. Its config was a `settings.json` with no plugin and no hooks. A first attempt stopped at Warp's first-run screens; the run below is a later one, described in [The Warp run](#the-warp-run).

## Keys, in order

1. type `alpha bravo charlie delta`
2. Ctrl+W
3. Ctrl+U
4. type `echo foxtrot golf`
5. Ctrl+K
6. Alt+Backspace (Option+Backspace in Ghostty, `M-BSpace` in tmux)
7. Ctrl+D
8. Ctrl+U, which is not in the task's list: it empties the box so that Shift+Enter cannot send anything if it arrives as a plain Enter (it did, in `no-tmux-env`)
9. Shift+Enter
10. Ctrl+Z
11. `fg` and Enter
12. Ctrl+C, twice

The first key came about 10 s after the box appeared (22 s in Ghostty), and the keys 1 to 1.3 s apart.

## What arrived

| Key | tmux 3.6b, extended keys on | Ghostty 1.3.1, defaults | Warp 0.2026.07.29 | tmux 3.6b, no `TMUX` or `TERM_PROGRAM` |
| --- | --- | --- | --- | --- |
| text | plain bytes | plain bytes | plain bytes | plain bytes |
| Ctrl+W | `ESC[27;5;119~` | `ESC[119;5u` | `ESC[119;5u` | `0x17` |
| Ctrl+U | `ESC[27;5;117~` | `ESC[117;5u` | not pressed | `0x15` |
| Ctrl+K | `ESC[27;5;107~` | `ESC[107;5u` | not pressed | `0x0b` |
| Alt+Backspace | `ESC[27;3;127~` | `0x7f`, a plain Backspace | not pressed | `ESC 0x7f` |
| Ctrl+D | `ESC[27;5;100~` | `ESC[100;5u` | not pressed | `0x04` |
| Shift+Enter | `ESC[27;2;13~` | `ESC[13;2u` | not pressed | `0x0d`, a plain Enter, which sent the box (step 8 was added after this run) |
| Ctrl+Z | `ESC[27;5;122~` | `ESC[122;5u` | `ESC[122;5u` | `0x1a` |
| `fg`, Enter, Ctrl+C after Ctrl+Z | `fg`, `0x0d`, `0x03` | `f` `g`, `0x0d`, `0x03` | `fg` went to the shell; Ctrl+C `0x03` | not reached |

So tmux sends modifyOtherKeys (`ESC[27;<mods>;<code>~`), Ghostty and Warp send kitty CSI-u (`ESC[<code>;<mods>u`), and the legacy bytes arrive only when Claude Code pushes nothing. No key arrived ESC-prefixed except Alt+Backspace in the run without the push.

**The push.** In both real runs Claude Code wrote `ESC[<u ESC[>5u ESC[>4;2m` (pop, then kitty flags 5, then modifyOtherKeys level 2) 0.25 s (tmux) and 0.46 s (Ghostty) after start, before its first frame. It wrote them again right after `ESC[?1049h` (alternate screen), and again with the first key typed. It sent the kitty query `ESC[?u` after the push, not before. Ghostty answered `ESC[?5u`, so flags 5 were on. tmux did not answer the query, and still sent every key in modifyOtherKeys form. In the run with no `TMUX` or `TERM_PROGRAM`, Claude Code sent only the `ESC[?u` query and pushed nothing.

**Ctrl+Z.** In both real runs Ctrl+Z did not reach unsent as the lone byte `0x1a`, so unsent did not suspend. Claude Code took it itself. It popped the flags (`ESC[<u`, `ESC[>4m`), left the alternate screen, turned mouse and focus reporting off, and printed `Claude Code has been suspended. Run `fg` to bring Claude Code back.` But it did not stop: `ps` showed it `Ss+` and unsent `S+`, and the shell never got the terminal back. `fg` and Enter went to Claude Code, which ignored them. The next Ctrl+C, now legacy `0x03`, ended Claude Code and unsent with it; the second Ctrl+C reached the shell. In the run with no push, `0x1a` arrived and unsent suspended itself (`zsh: suspended`), as [`wrap.go`](../../wrap.go) intends.

**Other input.** Ghostty sent a focus report `ESC[I` at start, and answered Claude Code's queries on input: XTVERSION (`ESC P>|ghostty 1.3.1 ESC \`), `ESC[?5u`, two DA1 (`ESC[?62;22;52c`), DECRPM for modes 2026 and 1016 (`ESC[?2026;2$y`, `ESC[?1016;2$y`), a kitty graphics reply (`ESC _Gi=31;OK ESC \`) and a cell size report (`ESC[6;17;8t`). tmux answered with XTVERSION and DA1 only.

## The run without `TMUX`

`no-tmux-env.*` was the first tmux run. The pane's shell was started under `env -i`, which removed `TMUX`, `TMUX_PANE`, `TERM_PROGRAM` and `TERM_PROGRAM_VERSION`, so Claude Code saw only `TERM=tmux-256color`. A real tmux pane always has them. I kept the run as the control, because it shows the same keys arriving as legacy bytes when nothing is pushed. Its Shift+Enter arrived as `0x0d` and sent `echo foxtrot ` to the closed port. The request failed at once (`ECONNREFUSED`) and nothing left the machine. It is cut at 20 s, before the kill that ended it.

## The Warp run

`warp/` is a later run, at the v0.6 gate, in Warp 0.2026.07.29.09.05.02 with Claude Code 2.1.284 under `unsent claude`, the key decoder and unsent's own suspend in place. The environment was the one above, plus `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`. The keys were fewer:

1. type `alpha bravo charlie delta`
2. Ctrl+W
3. Ctrl+Z
4. `fg` and Enter, at the shell, so not in the log
5. type ` echo`
6. Ctrl+C, twice

The first key came 17 s after start.

**The push.** Claude Code wrote `ESC[<u ESC[>5u ESC[>4;2m` 0.61 s after start, again right after `ESC[?1049h`, and again with the first key typed, as in Ghostty. It sent XTVERSION, the kitty query and DA1 together (`ESC[>0q ESC[?u ESC[c`), and Warp answered `ESC P>|Warp(v0.2026.07.29.09.05.stable_02) ESC \`, `ESC[?5u` (flags 5 on) and `ESC[?62c`. Later it answered DECRPM for mode 2026 (`ESC[?2026;2$y`), the kitty graphics query (`ESC _Gi=31;OK ESC \`) and DA1 again. It did not answer the query for mode 1016 or the cell size query `ESC[16t`, and sent no focus report at start.

**The keys.** Ctrl+W came as `ESC[119;5u` and Ctrl+Z as `ESC[122;5u`, kitty CSI-u. Ctrl+C came as the legacy byte `0x03` both times, with flags 5 pushed again at the key typed just before; Claude Code read the first as "Press Ctrl-C again to exit" and the second ended it. unsent read Ctrl+Z and suspended itself and Claude Code: the shell printed `zsh: suspended  unsent claude`, `ps` showed both stopped (`T`, `Ts+`), and `fg` brought Claude Code back with `alpha bravo charlie` in the box. After the resume Claude Code pushed again with `ESC[?1049h`, and bracketed paste came back with the next key.

The log is not cut: it ends at Claude Code's exit.
