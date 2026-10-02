# Configuration

`unsent` has no config file. It reads a few environment variables, and `unsent setup` writes one marked block to your shell's rc file. `unsent status` prints what the current shell has: whether saving is on, and for each agent whether a sent message is logged and which variable decided it.

## Environment variables

| Variable | Unset means | What it does |
| --- | --- | --- |
| `UNSENT_OFF` | `unsent` saves as usual | Any value but `0` runs every agent directly, as if `unsent` were not there. The way out when an agent update confuses it, with no file to edit. `command claude` does the same for one run. |
| `UNSENT_NOTICE` | notices on | `0` turns off the draft notices and the line under the box, for sessions nobody watches. Drafts still go back into their conversation's box, the line saying drafts are not being saved still shows, and the [Claude Code hook](usage.md#a-note-for-claude-code-when-you-reopen-a-conversation) stays on. |
| `UNSENT_AGENT` | the agent is read from the command's name | Names the agent for a command whose name doesn't say it, such as `npx` or a renamed binary: `UNSENT_AGENT=claude unsent npx @anthropic-ai/claude-code`. The same as `--as`. Put it in front of one command; exported, every wrapped command `unsent` doesn't recognize is read as that agent. |
| `UNSENT_ON_SEND` | agents: `log`; zsh: `delete` | `log` keeps what you send in the [sent log](sent-messages.md); `delete` keeps nothing. Set to `log`, it logs the zsh lines you run too. |
| `UNSENT_ON_SEND_CLAUDE`, `UNSENT_ON_SEND_CODEX`, `UNSENT_ON_SEND_PI`, `UNSENT_ON_SEND_AGY`, `UNSENT_ON_SEND_ZSH` | `UNSENT_ON_SEND` decides | The same for one agent or for zsh, and it wins over `UNSENT_ON_SEND`. `UNSENT_ON_SEND_ZSH=log` keeps the [zsh lines](zsh.md) you run. |
| `UNSENT_HOME` | `$XDG_STATE_HOME/unsent` | The folder that holds drafts, history and sent logs. |
| `XDG_STATE_HOME` | `~/.local/state` | Read when `UNSENT_HOME` is unset. [Where your drafts live](how-it-works.md#where-your-drafts-live) says what is inside. |
| `UNSENT_DEBUG_DIR` | nothing logged | Logs every key, all output and each save's view into that folder, for any session. The files are `0600` and hold everything you type: check them before you share them. See [Development](development.md). |

The `UNSENT_ON_SEND` variables take `log` or `delete`, in any case; any other value counts as unset.

## Read from the agents

`unsent` finds which conversation a draft belongs to in the agents' own folders, and follows the variables that move them:

| Variable | Unset means | What `unsent` reads there |
| --- | --- | --- |
| `CLAUDE_CONFIG_DIR` | `~/.claude` | Claude Code's `sessions/<pid>.json`, which names the open conversation. `unsent hook claude` prints the hooks for `settings.json` in the same folder. `unsent context`, `unsent import claude` and the Stop hook also read the transcripts in `projects/<slug>/<session>.jsonl` and the prompt history in `history.jsonl`, for [context](context.md); nothing else does. |
| `CODEX_HOME` | `~/.codex` | The lock files in `thread-writer-locks/` that a running Codex holds open. |

## Your shell's rc file

`unsent setup` adds one marked block to `~/.zshrc` (`$ZDOTDIR/.zshrc` when `ZDOTDIR` is set) or `~/.bashrc`, picked from `$SHELL`, or the one you name (`unsent setup bash`). The block turns `claude`, `codex`, `pi` and `agy` into shell functions that run through `unsent`, and in zsh it saves the [command line](zsh.md) too. `unsent setup --undo` removes the block and nothing else. If you keep your `.zshrc` by hand, add `eval "$(unsent init zsh)"` to it instead. [Using unsent](usage.md) covers what setup leaves alone and what bypasses it.
