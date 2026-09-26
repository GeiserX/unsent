# Claude Code captures

Real output of Claude Code, replayed by `replay_test.go` through the shadow screen, the reader and the stitcher. One folder per Claude Code version. Every draft in them is dummy text typed for the capture.

## What each file is

- `*.bin`: the raw bytes Claude Code wrote to its terminal. The test saves after every synchronized-output frame (`ESC[?2026l`) and checks the drafts saved.
- `*.editor.txt`: the file Claude Code handed to `$EDITOR` on Ctrl+G, which is exactly the draft it held. This is the ground truth.
- `*.keys.json`: the tmux keys sent to make the capture, in order (`type` is literal text, `keys` are tmux key names, `wait` is seconds).
- `screens/*.txt`: one screen copied with `tmux capture-pane -p -e`, colours included (`scrolled.txt` without them).

## How they were made

Claude Code ran in a private tmux server at 120x40 with a scratch `CLAUDE_CONFIG_DIR`, a scratch `HOME` and a dummy `ANTHROPIC_API_KEY`, so nothing reached a model. `tall-wrapped.bin` and `long-word.bin` were recorded with `script -q -F`, from start-up to the repaint after the editor returned.

Three files differ from that:

- `typing.bin` was logged after the box was already drawn, so the test first replays the start-up paint from `editor-probe.bin` (same build, same size, same settings).
- `empty-hint.bin` and `suspend.bin` are 100x30 start-ups from a configured install, which shows the dim `Try "…"` hint in an empty box. Their header row's model and plan label was replaced with `model and plan label removed`; nothing the reader looks at was changed.

## Adding one

Keep each agent under 20 MB, trimmed or compressed. Check every file for real prompt text, names, paths and keys before committing. A new screen needs its expected draft in `claudeScreens`, or `TestReplayClaudeScreens` fails.
