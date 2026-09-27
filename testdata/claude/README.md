# Claude Code captures

Real output of Claude Code, replayed by `replay_test.go` through the shadow screen, the reader and the stitcher. One folder per Claude Code version. Every draft in them is dummy text typed for the capture.

## What each file is

- `*.bin`: the raw bytes Claude Code wrote to its terminal. The test saves after every synchronized-output frame (`ESC[?2026l`) and checks the drafts saved.
- `*.rec`: the keys sent and the bytes Claude Code wrote, in the order they happened, as `script -r` records them (each chunk has a 24-byte header: length, seconds, microseconds, direction; `script -p` plays one back). The test feeds keys through the session's input path, so the delete keys reach the stitcher, and saves after every output frame.
- `*.editor.txt`: the file Claude Code handed to `$EDITOR` on Ctrl+G, which is exactly the draft it held. This is the ground truth.
- `*.editor-N.txt`: the same for a `.rec`, one per Ctrl+G in it. At the Nth Ctrl+G the saved draft must match the Nth copy.
- `*.keys.json`: the tmux keys sent to make the capture, in order (`type` is literal text, `keys` are tmux key names, `paste` is text sent with `paste-buffer -p`, `wait` is seconds).
- `screens/*.txt`: one screen copied with `tmux capture-pane -p -e`, colours included (`scrolled.txt` without them).

## How they were made

Claude Code ran in a private tmux server at 120x40 with a scratch `CLAUDE_CONFIG_DIR`, a scratch `HOME` and a dummy `ANTHROPIC_API_KEY`, so nothing reached a model. `tall-wrapped.bin` and `long-word.bin` were recorded with `script -q -F`, from start-up to the repaint after the editor returned.
The `.rec` files were recorded with `script -q -r -t 0` the same way, and their editor script kept one numbered copy per Ctrl+G.

`word-deletes.rec` and `placeholders.rec` were recorded with `unsent capture claude` in the same setup. `placeholders.rec` pastes the path of a 4x4 PNG that existed only for the capture.

`sends.rec` was recorded the same way with `CLAUDE_CODE_MAX_RETRIES=0`, so each send fails at once with a 401 on the dummy key and Claude Code is idle again for the next key.

Three files differ from that:

- `typing.bin` was logged after the box was already drawn, so the test first replays the start-up paint from `editor-probe.bin` (same build, same size, same settings).
- `empty-hint.bin` and `suspend.bin` are 100x30 start-ups from a configured install, which shows the dim `Try "…"` hint in an empty box. Their header row's model and plan label was replaced with `model and plan label removed`; nothing the reader looks at was changed.

## Adding one

`unsent capture claude` records a new `.rec` with its editor copies, named `capture-<time>`, into the folder of the running version; rename them for what they show. Keep each agent under 20 MB, trimmed or compressed. Check every file for real prompt text, names, paths and keys before committing. A new screen needs its expected draft in `claudeScreens`, or `TestReplayClaudeScreens` fails.
