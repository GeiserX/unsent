# Codex captures

Real terminal traffic of the OpenAI Codex CLI, recorded for the Codex profile. One folder per Codex version. Every draft in them is dummy text typed for the capture. `screen_test.go` replays `tinted`, `typed`, `dialogs` and `submit` to check the snapshot's cell looks; no reader test replays them yet, since `replay_test.go` reads only `testdata/claude`.

## What each file is

- `*.rec`: the keys sent and the bytes Codex wrote, in the order they happened, as `script -r` records them (each chunk has a 24-byte header: length, seconds, microseconds, direction; `script -p` plays one back). All but one were recorded by `unsent capture codex`, which ran Codex with no reader and passed every byte through. The input side is what reached Codex, after tmux encoded the keys.
- `*.sizes.json`: the window size over time, as `unsent capture` writes it next to its record. Every capture is 120x40.
- `*.editor-N.txt`: the file Codex handed to `$EDITOR` at the Nth Ctrl+G in the record, which is exactly the draft it held. This is the ground truth.
- `*.keys.json`: the tmux keys sent to make the capture, in order (`type` is literal text, `keys` are tmux key names, `paste` is text sent with `paste-buffer -p`, with `-r` when `raw` is set, `wait` is seconds, `until` waits for a pattern on the screen, `screen` saves a screen, `sh` runs a probe and saves its output in `sessions/`).
- `screens/*.txt`: one screen copied with `tmux capture-pane -p -e`, colours included. The `-exit` screens also hold the scrollback after Codex quit.
- `sessions/*.txt`: what another process could see of the running thread at that moment: the processes, the files under `CODEX_HOME` the Codex process held open (`lsof`), the `threads` table of `state_5.sqlite`, `sessions/`, `history.jsonl` and `thread-writer-locks/`. `flock-*.txt` tries an exclusive lock on each lock file; `poll-*.txt` polls every 50 ms from start and prints when the box, the held lock and the open rollout first appear. `history-*.jsonl` are the two scratch `history.jsonl` files at the end.

## How they were made

Codex 0.158.0 (`codex-cli 0.158.0`, the native arm64 build) ran on a Mac mini with macOS 26.6.1, in a private tmux 3.6b server (`tmux -L unsent-07 -f /dev/null`, `TERM=tmux-256color`) at 120x40, with `extended-keys on` and `extended-keys-format csi-u`, so tmux sent keys in the form Codex asked for. It ran under `env -i` with a scratch `HOME`, `CODEX_HOME`, `TMPDIR` and XDG folders, `OPENAI_API_KEY=dummy`, `OPENAI_BASE_URL=http://127.0.0.1:9/v1`, and `HTTPS_PROXY`, `HTTP_PROXY` and `ALL_PROXY` pointed at the same dead port. Codex ignores `OPENAI_BASE_URL` for its responses websocket (it tried `wss://api.openai.com/v1/responses`), so the proxy is what kept every request on the machine; each one failed with `Connection refused`. `check_for_update_on_startup = false` was set in the scratch `config.toml`, and every run passed `-c sandbox_mode="danger-full-access"` so Ctrl+G used `CODEX_HOME/editor`.

The first run (`dialogs`) answered the sign-in screen with the detected dummy key and trusted the scratch folder; every later run started straight at the box.

Differences from that setup:

- `deletes-xterm` ran with `extended-keys-format xterm`, where Codex asks for fewer key reports and tmux sends legacy bytes. tmux has no legacy form for Ctrl+Backspace, so that key arrived as the literal text `C-BSpace`, which is in the draft.
- `daemon-quit` and `daemon-killsession` ran without `-c` (the sandbox setting went into the scratch `config.toml` instead), which is how Codex starts its shared background server.
- `suspend` ran `unsent` from a `zsh -f` in the pane, so Ctrl+Z had a shell to return to. `suspend-direct` ran Codex under `script -q -r -t 0` from that shell, without unsent, to see Codex's own Ctrl+Z. Codex did not stop, so the `fg` and Enter typed next went into its box and were sent; that send failed on the dead port.
- `tinted` ran with `window-style "fg=#cdd6f4,bg=#1e1e2e"` set in the tmux server, so tmux answered `OSC 10;?` and `OSC 11;?` and Codex tinted its box (`ESC[48;5;237m`). An earlier try with only `bg` set got an answer to `OSC 11` alone, and Codex drew no tint.
- `picker` was started with `codex resume` and its keys begin with the picker open. `resume-id`, `resume-unsent-id`, `resume-last` and `fork-last` were started with `codex resume <id>`, `codex resume <id of a thread that never sent>`, `codex resume --last` and `codex fork --last`.

## Scenarios

| Record | What it holds |
| --- | --- |
| `dialogs` | sign-in screen, API key screen, trust dialog, first empty box with its logo, warnings view (F2), `/` menu, `?` shortcuts, `@` mentions, Ctrl+C on the empty box quitting |
| `typed` | one typed line, Ctrl+G, Ctrl+C clear |
| `tinted` | the empty box and a two-line draft with the box tinted, Ctrl+G, Ctrl+C clear |
| `multiline` | new lines with Ctrl+J, Alt+Enter and Shift+Enter, one of them blank |
| `accents` | Spanish accents precomposed and decomposed, emoji with a skin tone and a flag, a double-width character at the wrap edge, a word wrap at a space |
| `tall` | 45 lines, taller than the box; an edit 40 rows up that scrolls the box, then back down so it is out of sight; a word deleted with Ctrl+W 30 rows up |
| `pastes` | a 3-line paste and a 1,000-character paste inline, two 1,001-character pastes (placeholder and `#2`), Backspace after the editor round trip, a 1,079-character 12-line paste sent with LF and with CR |
| `placeholder` | Backspace right after a placeholder, then Ctrl+C with a placeholder in the box and Up to recall it |
| `deletes`, `deletes-xterm` | Backspace, Ctrl+H, Ctrl+W, Alt+Backspace, Ctrl+Backspace, Delete, Ctrl+D, Alt+D, Ctrl+Delete, Alt+Delete, Ctrl+U, Ctrl+Y, Ctrl+K, Ctrl+_, Shift+Backspace, Shift+Delete, with a Ctrl+G after each group |
| `ctrlc-history` | Ctrl+C clear, Up and Down through history, Up inside a typed draft, Ctrl+R |
| `submit` | Ctrl+C before any send, Enter on a prompt that fails on the dead port while Codex retries, a draft typed during the retries, Ctrl+C |
| `kill9` | a two-line draft, then `kill -9` of the Codex process |
| `killsession` | a two-line draft, then tmux `kill-session` |
| `resume-id`, `resume-last`, `fork-last`, `picker` | the resume forms, `/new`, `/resume` and `/status` inside a session |
| `resume-unsent-id` | `codex resume` of a thread that never sent anything: an error and exit status 1 |
| `daemon-quit`, `daemon-killsession` | the shared background server: a quit, and a draft followed by `kill-session` |
| `suspend`, `suspend-direct` | Ctrl+Z through unsent and `fg`; Ctrl+Z to Codex alone |

## Adding one

`unsent capture codex` records a new `.rec` with its editor copies, named `capture-<time>`, into the folder of the running version; rename them for what they show. Keep each agent under 20 MB. Check every file for real prompt text, names, paths and keys before committing: `ls -l` output in a probe names the file owner, so the `sessions/` files were rewritten to say `rig`.
