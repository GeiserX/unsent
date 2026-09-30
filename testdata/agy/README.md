# agy captures

Real terminal traffic of agy (Google's Antigravity CLI), recorded for the agy
profile. One folder per agy version. Every draft in them is dummy text typed
for the capture, and every conversation id in them belongs to a throwaway home
that no longer exists.

## What each file is

- `*.rec`: the keys sent and the bytes agy wrote, in the order they happened,
  as `script -r` records them (each chunk has a 24-byte header: length,
  seconds, microseconds, direction; `script -p` plays one back). All of them
  were recorded by `unsent capture agy`, which ran agy with no reader and
  passed every byte through.  The input side is what reached agy, after tmux
  encoded the keys.
- `*.sizes.json`: the window size over time, as `unsent capture` writes it next
  to its record. Every capture is 120x40 but `pastes-20rows`, which is 120x20.
- `*.editor-N.txt`: the file agy handed to `$EDITOR` at the Nth Ctrl+G in the
  record, which is exactly the draft it held, placeholders expanded. This is
  the ground truth.
- `*.keys.json`: the tmux keys sent to make the capture, in order, and the log
  of when each step ran (`type` is literal text, `keys` are tmux key names,
  `paste` is text sent with `paste-buffer -p`, with `-r` when `raw` is set,
  `wait` is seconds, `until` waits for a pattern on the screen, `screen` saves
  a screen, `sh` runs a probe and saves its output in `sessions/`, `bg` starts
  a poller, `kill` and `killsession` end the run the hard way, `gap` is the
  pause after a step, 0.15 s when not set).
- `screens/*.txt`: one screen copied with `tmux capture-pane -p -e`, colours
  included. The `-exit` screens also hold the scrollback after agy quit.
- `sessions/*.txt`: what another process could see at that moment: the
  processes, the files the agy process held open under the scratch home
  (`lsof`), the files in its config folder with their sizes, `history.jsonl`,
  `cache/last_conversations.json`, the updater's status and the checksum of the
  agy binary. `poll-*.txt` polls every 50 ms from before start and prints when
  the box, the agy process, the conversation database it holds open, and each
  of those files first appears or changes.

## How they were made

agy 1.2.13 (released 2026-09-29, the newest at the time) ran on a Mac mini with
macOS 26.6.1. The release tarball `agy_cli_mac_arm64.tar.gz` was unpacked into
a scratch folder and run from there by its full path, through a symlink named
`agy` on a `PATH` holding only unsent, agy and the system folders; the
installer was never run.

It ran in a private tmux 3.6b server (`tmux -L unsent-072 -f <scratch conf>`,
`TERM=tmux-256color`) at 120x40, with `extended-keys on` and
`extended-keys-format csi-u`, under `unsent capture agy` with `UNSENT_DEBUG_DIR`
set. unsent has no agy reader, so it printed its not-protected line and passed
every byte through.

The environment was `env -i` with a scratch `HOME`, `TMPDIR` and XDG folders,
`modelProvider: "gemini"` in the scratch
`~/.gemini/antigravity-cli/settings.json`, `GEMINI_API_KEY=fake-not-a-key`,
`GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:9`, `HTTPS_PROXY`, `HTTP_PROXY` and
`ALL_PROXY` on the same dead port, `AGY_CLI_DISABLE_AUTO_UPDATE=1`, no other
provider key, no `GITHUB_TOKEN`, `GH_TOKEN` or `CI`, and `USER` and `LOGNAME`
set to `rig`. `$EDITOR` and `$VISUAL` were unsent's copy-and-exit script. That
path reaches the input box without a login, so nothing was ever sent to a
model: every submit failed on the dead endpoint.

`first-run` ran on a home with nothing but that settings file, so it holds the
first-run screens. Every other capture ran on a home that had been through
those screens once, with `trustedWorkspaces` already holding the working
folder. `typed` to `submit` and `scroll-steps` share one home in the order
below, so each sees the prompt history the ones before it left; `identity` and
the three resume captures share a second home, in that order.

## Scenarios

| Record | What it holds |
| --- | --- |
| `typed` | the empty box, one typed line, Ctrl+G, Ctrl+C and Esc both keeping the draft, the draft cleared with Ctrl+U, a second draft and its Ctrl+G |
| `multiline` | new lines with Ctrl+J, Alt+Enter, Shift+Enter, Esc then Enter and `\` then Enter, one of them blank, then Ctrl+G |
| `tall` | 45 lines, taller than the 20-row box; an edit 40 rows up with `↑ 4 more lines` and `↓ 21 more lines` on the rules, back down so it is out of sight, Ctrl+G; a Ctrl+W 30 rows up, back down, Ctrl+G |
| `accents` | Spanish accents precomposed and decomposed, emoji with a skin tone and a flag, a double-width character at the wrap edge, a word wrap at a space, Ctrl+G |
| `pastes` | a 3-line paste inline; 15 lines inline and 16 as `[Pasted text #1 +16 lines]`; 1,000 characters inline and 1,001 as `[Pasted text #1 1001 chars]`; two placeholders in one draft; Backspace over the first, which does not renumber the second; a third paste taking `#3`; a paste ending in a line break; Ctrl+U inside a placeholder; a Ctrl+G after each |
| `pastes2` | the same thresholds again, each from a box the screens show is empty: 1,000 and 1,001 characters, a placeholder deleted whole with one Backspace, the next paste taking the next number, a 16-line placeholder deleted whole, a paste holding a tab, and a paste whose line breaks tmux sent as CR |
| `pastes-20rows` | the line threshold at 20 rows: 10 lines inline, 11 as `[Pasted text #1 +11 lines]` |
| `deletes` | Backspace, Ctrl+H, Ctrl+W, Alt+Backspace, Ctrl+Backspace, Delete, Ctrl+D, Alt+D, Ctrl+Delete, Alt+Delete, Shift+Backspace, Shift+Delete, Ctrl+K, Ctrl+U, Ctrl+Y, Ctrl+_ and Ctrl+Shift+Z, with a Ctrl+G after each group |
| `dialogs` | the empty box, the `?` shortcuts panel and its command tab, the `/` menu and a filtered `/set`, the `@` file picker, the three Shift+Tab modes, `!` bash mode and Esc wiping it, `/settings`, `/model`, `/resume` with no conversations, `/context` |
| `ctrlc-history` | two submits that fail, then Up and Down through the prompt history, which holds entries from the earlier captures of the same home, Up in a typed draft leaving it alone, Ctrl+R opening the artifacts panel |
| `submit` | Enter sending and failing on the dead endpoint, the box during the turn, a draft typed during it and its Ctrl+G, Esc interrupting, then Alt+Enter, Shift+Enter, Ctrl+J and Esc then Enter, each of which makes a new line instead, with a probe of the config folder at each step |
| `scroll-steps` | 27 lines, one of them 250 characters wide, so `↑ 9 more lines` counts wrapped rows; Up, PageUp, Ctrl+Home and Ctrl+End; Ctrl+G |
| `identity` | the config folder before any key, after typing, after Ctrl+C, 0.3 s and 4 s after a submit, after Esc and after a second submit, with a 50 ms poll of the whole run |
| `resume-c`, `resume-conversation` | `agy -c` and `agy --conversation <id>`, each with a poll showing the conversation database held open before the box is drawn, a draft and its Ctrl+G |
| `resume-unknown` | `agy --conversation` of an id with no conversation: `warning: conversation "<id>" not found`, and a new conversation starts anyway |
| `kill9` | a two-line draft, then `kill -9` of the agy process |
| `killsession` | a two-line draft, then tmux `kill-session` |
| `suspend` | Ctrl+Z as `ESC[122;5u`, which neither unsent nor agy acts on, then a SIGCONT the rig sent by hand, and Ctrl+G |
| `suspend-legacy` | Ctrl+Z as the lone byte `0x1a`, which unsent takes; the rig's process group has no job-control shell, so the kernel drops the stop |
| `exit-ctrl-d` | one Ctrl+D warning `press ctrl+d again to exit`, the second quitting with status 0 |
| `exit-ctrl-c` | one Ctrl+C keeping the draft and warning, the second quitting, and the draft in no file afterwards |
| `first-run` | the colour-scheme picker, the terms screen with its checkbox turned off, and the trust dialog, all on the alternate screen, then the inline box |

## The profile run (2026-09-30)

Nine more captures, made the same way with the agy profile built into
unsent, on a fresh scratch home and a fresh unsent state folder. The `.rec`
files of `restore-c` and `restore-long` hold the paste unsent itself made
to put a draft back; the tests drop it from the record and make it again.

| Record | What it holds |
| --- | --- |
| `wrap` | a line that ends exactly at the wrap width, and a word that would end exactly at it with a space after, which agy moves to the next row |
| `edge-deletes` | Ctrl+W, Backspace, Alt+Backspace, Ctrl+U and Delete on the last row of a box at its cap, where only the keys tell a deletion from rows scrolled out of sight below |
| `early-paste` | a 20-line paste on the first frame that shows the box |
| `orphan`, `orphan-long` | a prompt sent so the conversation exists, then a draft (2 lines, and 20) left in the box at a `kill-session` |
| `new-chat` | a bare `agy` in the same folder with that draft waiting: a new conversation, so no paste |
| `restore-c`, `restore-long` | `agy -c` reopening that conversation: unsent puts the draft back, the long one as `[Pasted text #1 +20 lines]`, and Ctrl+G says what agy holds |
| `send` | two prompts sent in one conversation, for the sent log and agy's own `history.jsonl` |

## The bash-mode run (2026-09-30)

One more capture, made the same way on the same binary, for the one thing
the profile had taken from Codex rather than measured. Is agy's `!` part
of the draft? It is not. This one was driven by hand from
`tmux send-keys` rather than by the rig, so
[`bash-mode.keys.json`](1.2.13/bash-mode.keys.json) has the steps but no
timing log. What it settled is in
[docs/research/agy.md](../../docs/research/agy.md#bash-mode-measured-2026-09-30).

| Record | What it holds |
| --- | --- |
| `bash-mode` | `!` then a command that wraps over two rows and then three, and 130 `x` in one token, with a Ctrl+G after each: agy hands the editor the command alone, and wraps it at the same `cols - 3` on every row, the first included |

## Adding one

`unsent capture agy` records a new `.rec` with its editor copies, named
`capture-<time>`, into the folder of the running version; rename them for what
they show. Keep each agent under 20 MB. Check every file for real prompt text,
names, paths and keys before committing. These were grepped for the
maintainer's user and host names and for home-folder paths; the only paths in
them are the scratch folders under `/Volumes/Data`, `USER` and `LOGNAME` were
set to `rig`, and the owner column of the `ls` output in `sessions/` was
rewritten to `rig`.
