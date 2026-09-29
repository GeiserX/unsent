# Codex captures

Real terminal traffic of the OpenAI Codex CLI, recorded for the Codex profile. One folder per Codex version. Every draft in them is dummy text typed for the capture. `screen_test.go` replays `tinted`, `typed`, `dialogs` and `submit` to check the snapshot's cell looks. `replay_test.go` replays every capture that has editor copies through Codex's profile (its reader, wrap, scroll, paste rule and keys) and checks each draft against the editor copy, and `inline` to check that its box is never read. `agent_codex_test.go` reads the dialogs, menus and history browsing in `dialogs`, `picker`, `slash-new` and `ctrlc-history` as negative screens, checks the sent log in `orphan` and `recall-send`, and puts the drafts of `restore-id`, `restore-last`, `restore-picker` and `restore-long` back through its own restore.

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

A second run on 2026-09-29, the same setup and the same Codex 0.158.0, ran unsent with the Codex profile, so it saved drafts, kept a sent log and put drafts back: `paste-exact`, `orphan`, `restore-id`, `restore-last`, `restore-picker`, `restore-long`, `early-paste`, `slash-new`, `edge-deletes`, `recall-send`, `daemon-draft` and `inline`. The restores in them are unsent's own pastes, logged as keys. Differences:

- `restore-id` and `restore-long` were started with `codex resume <id>`, `restore-last` and `early-paste` with `codex resume --last`, `restore-picker` with `codex resume`, `slash-new` with `codex resume --last`, and `inline` with `--no-alt-screen`.
- `daemon-draft` ran without `-c`, with the full Codex package (`codex-package.json`, `codex-resources`, `codex-path`) beside the binary, which the shared background server needs: with the bare binary Codex quits with `this CLI has no complete local package`.
- `slash-new` began in a thread that had an orphan: the restore went in 2.4 s after the box, and the draft cleared with Ctrl+C is the restored one.

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
| `wrap` | lines that end on a full row (117 columns at 120): before a blank line, before a line, a row of words, a word two rows long, and at the end of the draft; each gets an empty row after it. Recorded on its own on 2026-09-29 with a fresh scratch home, the same setup |
| `paste-exact` | pastes with tabs, spaces at line ends, a blank line, decomposed accents, emoji and a flag, sent with LF and with CR, an 853-character one with tabs, and one ending in a line break; each editor copy is byte for byte what was pasted |
| `orphan` | a prompt sent with Enter that fails on the dead port, Ctrl+C to interrupt the turn, then a two-line draft left in the box at a window close |
| `restore-id`, `restore-last`, `restore-picker` | that draft, and the next ones left the same way, put back by unsent into the empty box of `codex resume <id>`, `codex resume --last` and the picker's choice, Ctrl+G, Ctrl+C |
| `restore-long` | a draft of 1,259 characters on 18 lines put back, shown as `[Pasted Content 1259 chars]`; the editor copy is the whole draft |
| `early-paste` | a paste sent 0.30 s after start, on the first frame that shows the box, in a resumed thread with an orphan: the paste goes in whole and unsent's restore does not go in after it |
| `slash-new` | `/new` opens a dialog, `Where should the new conversation run?`; Enter picks the current checkout, and the process then holds two thread locks; a draft typed in the new thread |
| `edge-deletes` | 45 lines, the box at its cap with the cursor on its last row: Ctrl+W, Backspace, Alt+Backspace, Ctrl+U, Left, Ctrl+K, Delete and Ctrl+D there |
| `recall-send` | a prompt cleared with Ctrl+C, brought back with Up and sent with Enter |
| `daemon-draft` | a draft under the shared background server, which Codex starts as its own child a moment after it draws the box |
| `inline` | `--no-alt-screen`: the box drawn under the terminal's text with one footer row, a draft, Ctrl+G |

## Adding one

`unsent capture codex` records a new `.rec` with its editor copies, named `capture-<time>`, into the folder of the running version; rename them for what they show. Keep each agent under 20 MB. Check every file for real prompt text, names, paths and keys before committing: `ls -l` output in a probe names the file owner, so the `sessions/` files were rewritten to say `rig`.
