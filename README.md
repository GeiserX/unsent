<p align="center">
  <img src="docs/images/banner.svg" alt="unsent" width="900">
</p>

<p align="center">
  <a href="https://github.com/GeiserX/unsent/releases"><img src="https://img.shields.io/github/v/release/GeiserX/unsent" alt="Release"></a>
  <a href="https://github.com/GeiserX/unsent/actions/workflows/ci.yml"><img src="https://github.com/GeiserX/unsent/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/GeiserX/unsent" alt="License"></a>
  <a href="https://codecov.io/gh/GeiserX/unsent"><img src="https://codecov.io/gh/GeiserX/unsent/branch/main/graph/badge.svg" alt="Coverage"></a>
</p>

# unsent

**AutoRecover for your AI agent prompts.**

`unsent` saves text you have typed and not sent yet, in two places: the input box of a coding agent that runs in your terminal, and the command line at your shell prompt. Today that means Claude Code, Codex, pi, agy and zsh. Those agents save your *conversations*, not the message you haven't sent yet, so a closed window, a reboot, a crash or one Ctrl+C too many loses the prompt.

The next time you start the agent in that folder, `unsent` tells you what it kept:

```text
unsent: recovered a draft from 18:42 today, 23 lines. Run `unsent restore` to copy it.
```

## Features

- Saves the agent's input box as you type, every 0.4 seconds, with atomic writes that survive a power cut.
- Puts a draft back into the box when you reopen its Claude Code, Codex or agy conversation. Nothing is sent for you.
- Recovers long drafts that scroll inside the box, and big pastes shown as placeholders.
- Keeps a log of the messages you sent, and a history of drafts you cleared or replaced.
- Saves your half-typed zsh command line too.
- `list --json`, `show --json` and `log --json` for scripts and agents.
- Works in any terminal app, tmux and SSH. Drafts stay on your machine; `unsent` makes no network connections.
- Says so when it could not read an agent's box, instead of failing silently.

## Quick start

```sh
brew tap GeiserX/unsent && brew trust --formula GeiserX/unsent/unsent && brew install unsent   # or: go install github.com/GeiserX/unsent@latest
unsent setup      # from the next shell, your agents run through unsent
unsent restore    # after a crash: copy the newest draft to the clipboard
```

Release binaries and the first run in detail: [Getting started](https://geiserx.github.io/unsent/getting-started/).

## Documentation

The full documentation is at [geiserx.github.io/unsent](https://geiserx.github.io/unsent/).

- [Getting started](https://geiserx.github.io/unsent/getting-started/): Homebrew, Go, release binaries, and the first run
- [Configuration](https://geiserx.github.io/unsent/configuration/): every environment variable, and how to turn `unsent` off for a while
- [Using unsent](https://geiserx.github.io/unsent/usage/): setup, recovering drafts, JSON output, the Claude Code hook, what differs in Codex, agy and pi
- [Sent messages](https://geiserx.github.io/unsent/sent-messages/): the sent log and its JSON shape
- [Your zsh command line](https://geiserx.github.io/unsent/zsh/): the half-typed command line, and what is never saved
- [Supported agents](https://geiserx.github.io/unsent/supported-agents/): which agent versions were checked, and how
- [How it works](https://geiserx.github.io/unsent/how-it-works/): the mechanism, the one promise, and where drafts live
- [Limits](https://geiserx.github.io/unsent/limits/): what the screen cannot carry, agent by agent
- [Development](https://geiserx.github.io/unsent/development/): tests, capturing fixtures, releasing and the spec

## License

[GPL-3.0-or-later](LICENSE)
