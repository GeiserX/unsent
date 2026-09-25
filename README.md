<p align="center">
  <img src="docs/images/banner.svg" alt="unsent: AutoRecover for your AI agent prompts" width="900">
</p>

<p align="center">
  <a href="https://github.com/GeiserX/unsent/actions/workflows/ci.yml"><img src="https://github.com/GeiserX/unsent/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://codecov.io/gh/GeiserX/unsent"><img src="https://codecov.io/gh/GeiserX/unsent/branch/main/graph/badge.svg" alt="Coverage"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-GPL--3.0-blue" alt="License: GPL-3.0"></a>
</p>

# unsent

**AutoRecover for your AI agent prompts.**

Remember Word's AutoRecover? The laptop dies at 3 a.m. the night before the deadline, you reopen Word, and there it is on the left: *Document Recovery: thesis_FINAL_v3 (AutoRecovered)*. That little panel has saved more university projects than any all-nighter.

`unsent` brings that to the box where you type to Claude Code.

You spend ten minutes writing a careful prompt. Then the terminal window closes, the machine reboots for an update, Claude Code crashes, or you press Ctrl+C one time too many, and the prompt is gone. Claude Code saves your *conversations*, not the message you haven't sent yet. The request to change that, [anthropics/claude-code#18755](https://github.com/anthropics/claude-code/issues/18755), was closed as not planned.

With `unsent`, the next time you start Claude Code in that folder you see:

```text
unsent: recovered a draft from 18:42 today, 23 lines. Run `unsent restore` to copy it.
```

## Install

```sh
go install github.com/GeiserX/unsent@latest
```

Or grab a binary for macOS or Linux from the [releases page](https://github.com/GeiserX/unsent/releases).

## Use

Start Claude Code through it:

```sh
unsent claude
```

Everything else stays the same. Same terminal, same keys, same arguments, so `unsent claude --resume` works too. To stop thinking about it, add this to your `~/.zshrc` or `~/.bashrc`:

```sh
alias claude='unsent claude'
```

After a crash, a closed window or a reboot:

```sh
unsent list          # drafts left behind, newest first
unsent restore       # copy the newest one to the clipboard, then paste it
unsent show 2        # print draft 2
unsent list --all    # also drafts you cleared or sent
```

## Works everywhere you type

`unsent` wraps the agent rather than the terminal, so it works in any terminal app. Ghostty, iTerm2, Terminal.app, Kitty, WezTerm, Alacritty, the VS Code and Cursor terminals, tmux and SSH sessions all work. You don't need tmux, a plugin or a config file.

| Agent | Status |
| --- | --- |
| Claude Code | Supported |
| Codex CLI, Gemini CLI | Planned: each needs a small reader for its input box |

Anything else runs through `unsent` unchanged; it just isn't saved yet.

## How it works

1. `unsent` starts the agent inside a pseudo-terminal and passes every byte through, untouched, in both directions.
2. It keeps a hidden copy of the screen and, every 0.4 seconds, reads the text in the agent's input box off it.
3. When the text changes, it writes it to disk: a temporary file, flushed with `fsync`, then renamed into place. A power cut leaves the previous save or the new one, never half a file.
4. When the box empties because you sent or cleared it, the draft moves to history. When the agent exits with text still in the box, the draft stays for `unsent restore`.

A few things make that more than a screenshot:

- **Long drafts.** Claude Code's box stops growing at half the window height and scrolls inside itself, so the screen only shows part of a long prompt. `unsent` lines each view up against what it has already seen and stitches the full draft together, including edits you make after scrolling back up.
- **Big pastes.** Claude Code shows a long paste as `[Pasted text #1 +39 lines]`. Terminals mark pasted text with special codes, so `unsent` records the paste as it goes in and puts the real text back in place of the placeholder.
- **Ctrl+Z.** Suspending works as usual: your shell shows the job as stopped, and `fg` brings the agent back.
- **Knowing what's dead.** Each session holds a file lock, and the operating system releases it when the process dies, whether it exits, crashes or the machine reboots. A process ID can't be trusted for this because a reboot reuses them. The lock can.

## Where your drafts live

Drafts live in `~/.local/state/unsent/`. Set `XDG_STATE_HOME` or `UNSENT_HOME` to move them. `drafts/` holds one file per session and `history/` keeps the last 500 drafts you cleared, sent or restored. Only you can read the folder, which is mode `0700` with `0600` files. Drafts are plain JSON and never leave your machine, since `unsent` makes no network connections.

## Limits

- It only saves agents started through `unsent`. It can't see the Claude Desktop app or the VS Code extension's chat panel, which don't run in a terminal.
- Pasted images show as `[Image #1]` and come back as that placeholder.
- A screen can't tell a wrapped line from a line you ended by hand exactly where the row was full. In that one case the line break comes back as a space. Lines that start a list item, heading, quote or code block are kept on their own line.
- If a whole long draft lands in the box between two saves, only the part on screen is recovered. Typing never goes that fast.
- macOS and Linux, including WSL. Native Windows has no pseudo-terminals of this kind.

## Development

```sh
go test -race ./...
```

The tests include a fake agent that draws its box the way Claude Code does. Typing, pasting, Ctrl+C, Ctrl+Z and a closed window all run end to end without Claude Code installed.

## License

[GPL-3.0](LICENSE)
