# How it works

## Why unsent exists

**AutoRecover for your AI agent prompts.**

`unsent` saves text you have typed and not sent yet, in two places: the input box of a coding agent that runs in your terminal, and the command line at your shell prompt. Today that means Claude Code, Codex, pi, agy and zsh. Other agents run through `unsent` unchanged, and nothing of theirs is saved yet. [Supported agents](supported-agents.md) says which version of each was last checked, and when.

Remember Word's AutoRecover? The laptop dies at 3 a.m. the night before the deadline, you reopen Word, and there it is on the left: *Document Recovery: thesis_FINAL_v3 (AutoRecovered)*. That little panel has saved more university projects than any all-nighter.

`unsent` brings that to the box where you type to your agent.

You spend ten minutes writing a careful prompt. Then the terminal window closes, the machine reboots for an update, the agent crashes, or you press Ctrl+C one time too many, and the prompt is gone. Claude Code, Codex, pi and agy save your *conversations*, not the message you haven't sent yet. The request to change that in Claude Code, [anthropics/claude-code#18755](https://github.com/anthropics/claude-code/issues/18755), was closed as not planned.

With `unsent`, the next time you start the agent in that folder you see this line, before it starts and again after it exits, because the agent's screen covers the first one while it runs:

```text
unsent: recovered a draft from 18:42 today, 23 lines. Run `unsent restore` to copy it.
```

Reopen the conversation you typed it in and you don't need the notice. In Claude Code that is `claude --resume`, `claude -c`, the resume picker or `/resume`; in Codex it is `codex resume <id>`, `codex resume --last` or the picker that `codex resume` opens; in agy it is `agy -c` or `agy --conversation <id>`. The draft goes back into the empty box on its own, as one paste, and a line under the box says `unsent: put back your draft from 18:42 today, not sent`. Nothing is sent: you decide what to do with it. A new chat is another conversation, so it gets the notice and never a paste. So do `/clear` and `--fork-session` in Claude Code, and `/new` and `codex fork` in Codex. pi doesn't say which conversation it has open, so a pi draft comes back through the notice and `unsent restore` ([What differs in pi](usage.md#what-differs-in-pi)).

## Works everywhere you type

`unsent` wraps the agent rather than the terminal, so it works in any terminal app. Ghostty, iTerm2, Terminal.app, Kitty, WezTerm, Alacritty, the VS Code and Cursor terminals, tmux and SSH sessions all work. You don't need tmux, a plugin or a config file.

## The mechanism

1. `unsent` starts the agent inside a pseudo-terminal and passes every byte through, untouched, in both directions.
2. It keeps a hidden copy of the screen and, every 0.4 seconds, reads the text in the agent's input box off it.
3. When the text changes, it writes it to disk: a temporary file, flushed with `fsync`, then renamed into place. A power cut leaves the previous save or the new one, never half a file.
4. When you send the box, the message goes to the session's sent log. When you clear it, the draft moves to history. When the agent exits with text still in the box, the draft stays for `unsent restore`.
5. Each draft remembers its conversation: Claude Code names it in `sessions/<pid>.json` under its config folder, and Codex by the lock file in `thread-writer-locks/` under `$CODEX_HOME` that its process holds open. When you reopen that conversation, `unsent` waits until the box has been on screen and empty for about two seconds, with nothing typed, then pastes the draft in. Any key you press first cancels it. It takes every escape code out of the draft, so nothing in it can end the paste and press Enter. Once a save reads the draft back from the box, the old copy moves to history; if it never reads back, the draft stays where it was. Two terminals that open one conversation get one paste between them.

**One promise.** `unsent` never throws away text you didn't delete. Text in view is read exactly. Text out of sight above or below the box is inferred, and an inference can be wrong, so while a draft is taller than the box every save that loses text first keeps the previous version. Whatever `unsent` gets wrong, a correct copy is in `unsent list --all`, marked "(earlier version)". Those copies are capped at 30 per session and deleted once the prompt is sent.

A few things make that more than a screenshot:

- **Long drafts.** Claude Code's box and agy's stop growing at half the window height, Codex's at the window height minus 4 rows and pi's at 30% of it (at least 5 rows), and then each scrolls inside itself, so the screen only shows part of a long prompt. `unsent` lines each view up, word by word, against what it has already seen, and keeps the parts above and below it. Edits you make after scrolling back up, and resizing the window, both work. The screen can't tell text you deleted from text that only moved out of sight. The delete keys you pressed in the last second can, so `unsent` counts them.
- **Big pastes.** Claude Code shows a paste of 4 or more lines, or over 800 characters, as `[Pasted text #1 +39 lines]`, Codex one over 1,000 characters as `[Pasted Content 1234 chars]`, pi one of more than 10 lines or over 1,000 characters as `[paste #1 +12 lines]`, and agy one of more than 15 lines or with a line over 1,000 characters as `[Pasted text #1 +16 lines]`. Terminals mark pasted text with special codes, so `unsent` records the paste as it goes in and puts the real text back in place of the placeholder.
- **Ctrl+Z.** Suspending works as usual: your shell shows the job as stopped, and `fg` brings the agent back. That includes tmux and Ghostty, where Claude Code switches the terminal to a keyboard mode that sends Ctrl+Z as an escape code. While the agent is stopped, your shell gets its keys in the usual form. Codex and pi are the exception: while one is stopped, your shell gets keys in the form it asked the terminal for, so Ctrl+C at that prompt can show as `^[[99;5u`. After `fg`, pasting works as before.
- **Pipes.** When input or output isn't a terminal (`git diff | claude -p "review"`, `claude -p x > out.md`), `unsent` hands over to the agent directly. There's no input box to save, so the wrapper is safe to keep.
- **Saying when it didn't save.** Claude Code draws on a screen of its own, which hides anything printed while it runs, so `unsent` speaks up after the agent exits. If it never found the input box in a session you typed in, which is what an agent update that changes the box looks like, it says so and names the version it last checked, for example `unsent: could not read claude 2.1.290's box this session (last verified 2.1.282), nothing was saved`. An agent it has no reader for is named before it starts and again after it exits. If `unsent` itself hits a bug, it stops saving, keeps the last draft it saved and passes every byte through for the rest of the session. The agent never notices.
- **Knowing what's dead.** Each session holds a file lock, and the operating system releases it when the process dies, whether it exits, crashes or the machine reboots. A process ID can't be trusted for this because a reboot reuses them. The lock can.

## Where your drafts live

Drafts live in `~/.local/state/unsent/`. Set `XDG_STATE_HOME` or `UNSENT_HOME` to move them. `drafts/` holds one file per session, and `history/` keeps the last 500 drafts you cleared, replaced or restored, plus the capped earlier versions. `sent/` holds one sent log per session, or per conversation for Claude Code and Codex. A log over 5 MB drops its oldest messages first, logs not written to for 90 days are deleted, and at most 2,000 are kept. Only you can read the folder, which is mode `0700` with `0600` files. Drafts are plain JSON and never leave your machine, since `unsent` makes no network connections.
