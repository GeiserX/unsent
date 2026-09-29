# Your half-typed zsh command line

A long command typed at the zsh prompt and not yet run is lost the same way: the window closes, or Ctrl+C clears it. `unsent setup zsh` also saves that line as you type it, with a few zsh builtins in the same block, so no process starts with the shell and nothing wraps it.

```sh
unsent list --agent zsh     # the line a closed window took
unsent list --all --agent zsh   # also lines you cleared with Ctrl+C
unsent restore --agent zsh  # copy the newest one from this folder
```

A line you run with Enter keeps nothing, because zsh's own history has it (`UNSENT_ON_SEND_ZSH=log` keeps it). Only the prompt's own line is saved: never a password typed at a `sudo`, `ssh` or `read -s` prompt, which zsh's line editor never sees, never text typed at `vared`, never a line that starts with a space or matches your `HISTORY_IGNORE`, and never a line recalled with Up or Ctrl+R that you didn't edit. `unsent status zsh` says whether the hooks load. If you keep your `.zshrc` by hand, add `eval "$(unsent init zsh)"` to it instead of running setup; that costs one process start per shell. bash and fish don't have this yet.
