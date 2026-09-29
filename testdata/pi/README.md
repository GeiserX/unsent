# pi captures

Real terminal traffic of pi (`@earendil-works/pi-coding-agent`), recorded for the pi profile. One folder per pi version. Every draft in them is dummy text typed for the capture. `agent_pi_test.go` replays every record with editor copies through pi's profile and checks the saved draft against each copy, and reads the dialogs and pickers as frames that hold no box (docs/research/pi.md, "Capture run on pi 0.87.1" and "Profile run on pi 0.87.1").

## What each file is

- `*.rec`: the keys sent and the bytes pi wrote, in the order they happened, as `script -r` records them (each chunk has a 24-byte header: length, seconds, microseconds, direction; `script -p` plays one back). All but one were recorded by `unsent capture pi`, which ran pi with no reader and passed every byte through. The input side is what reached pi, after tmux encoded the keys.
- `*.sizes.json`: the window size over time, as `unsent capture` writes it next to its record. Every capture is 120x40.
- `*.editor-N.txt`: the file pi handed to `$EDITOR` at the Nth Ctrl+G in the record, which is exactly the draft it held, placeholders expanded. This is the ground truth.
- `*.keys.json`: the tmux keys sent to make the capture, in order (`type` is literal text, `keys` are tmux key names, `paste` is text sent with `paste-buffer -p`, with `-r` when `raw` is set, `wait` is seconds, `until` waits for a pattern on the screen, `screen` saves a screen, `sh` runs a probe and saves its output in `sessions/`, `gap` is the pause after a step, 0.15 s when not set).
- `screens/*.txt`: one screen copied with `tmux capture-pane -p -e`, colours included. The `-exit` screens also hold the scrollback after pi quit.
- `sessions/*.txt`: what another process could see of the running session at that moment: the processes, the files the pi process held open under the scratch folder (`lsof`), every file under `PI_CODING_AGENT_DIR` with its size and time, and the first 240 characters of each line of each session file. `poll-*.txt` polls every 50 ms from before start and prints when the box, a picker, the pi process, a file it holds open and each session file first appear or change. `fork-session.jsonl` is one whole session file, the one `pi --fork` wrote.

## How they were made

pi 0.87.1 (the npm `latest` on 2026-09-29, installed with `npm install --ignore-scripts @earendil-works/pi-coding-agent@latest`, run by Node 26.0.0) ran on a Mac mini with macOS 26.6.1, in a private tmux 3.6b server (`tmux -L unsent-071 -f /dev/null`, `TERM=tmux-256color`) at 120x40, with `extended-keys on` and `extended-keys-format csi-u`. It ran under `env -i` with a scratch `HOME`, `PI_CODING_AGENT_DIR`, `TMPDIR` and XDG folders, `PI_OFFLINE=1`, `PI_SKIP_VERSION_CHECK=1`, `PI_TELEMETRY=0`, no provider key, no `GITHUB_TOKEN`, `GH_TOKEN` or `CI`, and a `PATH` holding only unsent, pi, node and the system folders (checked: no other agent on it). No model was configured, so every submit failed with `Error: No API key found for the selected model.` `$EDITOR` and `$VISUAL` were unsent's copy-and-exit script.

A pi session file is written only once the session has an assistant message, so with no model nothing is ever written and nothing can be resumed. The identity and resume captures therefore used a second scratch agent folder with one custom provider in its `models.json`: `baseUrl` `http://127.0.0.1:9/v1`, `api` `openai-completions`, `apiKey` `dummy`, one model `dummy-model`, with `HTTPS_PROXY`, `HTTP_PROXY` and `ALL_PROXY` at the same dead port. Every request failed on the machine with `Error: Connection error.`, pi retried three times, and the failure was recorded as an assistant error message. These captures are `identity`, `resume-c`, `slash-new`, `resume-picker`, `resume-session`, `slash-resume`, `fork`, `resume-unknown`, `resume-unsent-id` and `send-paste`, run in that order in one folder, so each one sees the sessions the ones before it left.

The records `wrap`, `paste-renumber`, `edge-deletes`, `scroll-steps`, `early-paste` and `early-paste-resume` came from a second run the same day, in a new scratch folder on the same machine with the same setup, under `unsent capture pi` built with the pi profile, so the reader ran while they were recorded. `early-paste-resume` was started with `pi -c` in a dead-port agent folder where one failed send had made a session file.

Differences from that setup:

- `suspend` ran `unsent capture pi` from a `zsh -f` in the pane, so Ctrl+Z had a shell to return to. `suspend-direct` ran pi under `script -q -r -t 0` from that shell, without unsent, to see pi's own Ctrl+Z; its record is `script`'s, so it has no sizes file.
- `resume-c` was started with `pi -c`, `slash-new` with `pi -c`, `resume-picker` with `pi -r`, `resume-session` with `pi --session 01a0ed72` (a prefix of the first session's id), `fork` with `pi --fork 01a0ed72`, `resume-unknown` with `pi --session` and a made-up id, and `resume-unsent-id` with `pi --session` and the id of a session that was open but never got a reply (the one `slash-resume` started before its `/resume`).

## Scenarios

| Record | What it holds |
| --- | --- |
| `typed` | the empty box, one typed line, Ctrl+G, Ctrl+C clear, Ctrl+D quitting on the empty box |
| `multiline` | new lines with Ctrl+J, Shift+Enter and `\` then Enter, one of them blank |
| `tall` | 45 lines, taller than the 12-row box; an edit 40 rows up with `↑ 4 more` and `↓ 29 more` on the rules, back down so it is out of sight, Ctrl+G; a Ctrl+W 30 rows up, back down, Ctrl+G |
| `accents` | Spanish accents precomposed and decomposed, emoji with a skin tone and a flag, a double-width character at the wrap edge, a word wrap at a space |
| `pastes` | a 3-line, a 10-line and a 1,000-character paste inline; an 11-line paste ending in a line break (`[paste #1 +12 lines]`), two 1,001-character pastes (`#2`, `#3`); Backspace over the first placeholder, which renumbers the others; a 12-line paste after it (`#3`); Ctrl+G; Backspace after the round trip; an 11-line paste sent with CR |
| `deletes` | Backspace, Ctrl+H, Ctrl+W, Alt+Backspace, Ctrl+Backspace, Delete, Ctrl+D, Alt+D, Ctrl+Delete, Alt+Delete, Ctrl+U, Ctrl+Y, Alt+Y, Ctrl+K, Ctrl+-, Ctrl+_, Shift+Backspace, Shift+Delete, then Ctrl+C and Ctrl+- bringing the cleared draft back, with a Ctrl+G after each group |
| `ctrlc-history` | Ctrl+C clear (not added to history), two submits, Up and Down through history, Up in a typed draft (first to the line start, then into history with the draft held aside), Down back to the draft, Ctrl+R, Ctrl+C and Ctrl+- |
| `dialogs` | the empty box, the `/` menu, `@`, Ctrl+O startup help, `/settings`, `/hotkeys`, `/login`, `/resume` with no sessions, `/model` and Ctrl+L (the model picker, empty), `/session`, `!` bash mode and Esc wiping it, `/tree`, `/trust` |
| `submit` | typing and Ctrl+C before any submit, Enter failing with no model, a draft typed after it, Alt+Enter, Esc then Enter, Ctrl+Enter and Ctrl+M, with a probe of the agent folder at each step |
| `kill9` | a two-line draft, then `kill -9` of the pi process |
| `killsession` | a two-line draft, then tmux `kill-session` |
| `suspend`, `suspend-direct` | Ctrl+Z through unsent and `fg`; Ctrl+Z to pi alone, which stops its screen and hangs until a SIGCONT the rig sent by hand, text typed meanwhile, then Ctrl+G |
| `identity` | dead port: the agent folder before any key, after typing, after Ctrl+C, 0.3 s after a submit, during and after the retries with a draft typed during the turn and Ctrl+G, `/session`, an Alt+Enter submit, Esc cancelling the retry |
| `resume-c`, `resume-picker`, `resume-session`, `fork` | the resume forms, each with `/session` showing which session it opened, and a submit in `resume-c` |
| `slash-new`, `slash-resume` | `/new` inside a resumed session, then a submit in the new one; `/resume` inside a new session and Enter on the first entry |
| `resume-unknown`, `resume-unsent-id` | `pi --session` of an id with no file: `No session found matching '<id>'`, exit status 1 |
| `send-paste` | dead port: a draft with a 1,001-character placeholder sent with Enter, Esc, then Up recalling it expanded, Ctrl+G |
| `wrap` | a word that ends at the wrap width with a space after it (pi moves it down), a word longer than a row, a row that starts with the space after one, a line that fills its row before a blank line, a placeholder moved whole to the next row; Ctrl+G |
| `paste-renumber` | two 1,001-character pastes, the first deleted with Backspace (the second becomes `#1`) and a third pasted after; then two again, the second deleted; Ctrl+G after each |
| `edge-deletes` | 20 rows in a box of 12, then Ctrl+W, Backspace, Alt+Backspace, Ctrl+U, Ctrl+K, Delete, Ctrl+D, Alt+D, Alt+Delete, Shift+Delete and Shift+Backspace on the last row, with a Ctrl+G after each group |
| `scroll-steps` | 20 rows, Up 16 times a key at a time into the rows out of sight, an edit, Down 16 times back; Ctrl+G |
| `early-paste` | a 1,367-character paste with 7 line breaks on the first frame that shows the box, sent with LF, then with CR; a paste with a tab, spaces at line ends and a final line break; a path pasted after a word; Ctrl+G after each |
| `early-paste-resume` | dead port, `pi -c`: the same long paste on the first frame of the resumed session; Ctrl+G |

## Adding one

`unsent capture pi` records a new `.rec` with its editor copies, named `capture-<time>`, into the folder of the running version; rename them for what they show. Keep each agent under 20 MB. Check every file for real prompt text, names, paths and keys before committing. These were grepped for the maintainer's user and host names and for home-folder paths; the only paths in them are the scratch folders under `/Volumes/Data`, and `USER`, `LOGNAME` and the shell prompt were set to `rig`.
