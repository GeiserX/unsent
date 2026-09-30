# Using unsent

Start your agent through it:

```sh
unsent claude
unsent codex
unsent pi
```

Everything else stays the same. Same terminal, same keys, same arguments, so `unsent claude --resume` and `unsent codex resume --last` work too. To stop typing `unsent`, run setup once:

```sh
unsent setup          # from the next shell, `claude` runs through unsent
unsent status         # am I protected? one line per fact
unsent setup --undo   # remove the block again
```

Setup adds one marked block to `~/.zshrc` or `~/.bashrc`, picked from `$SHELL` (or `unsent setup bash`). It turns each agent `unsent` can read, today `claude`, `codex`, `pi` and `agy`, into a shell function that runs it through `unsent`. An alias such as `alias claude='claude --model opus'` keeps working, and a function of the same name is left alone. An alias that points at a path, and launchers that start the agent through `env`, `command`, `exec` or a full path, skip the wrapper: setup warns about them and never edits them. `unsent status` asks a new shell what each agent name runs and what bypasses it, without starting an agent or editing a file. Run setup again after an upgrade that adds agents. `--undo` removes the block and nothing else, and your drafts stay. To uninstall, run `unsent setup --undo` and `brew uninstall unsent`.

`UNSENT_NOTICE=0` turns off the notices and the line under the box, for sessions nobody watches. Drafts still go back into their conversation's box, and a line saying drafts are not being saved still shows.

To run the agent without `unsent` for a while, for example when an agent update confuses it, set `UNSENT_OFF=1`: `unsent` then hands over to the agent directly. `command claude` also skips the wrapper, for one run.

When the agent starts through a command that doesn't carry its name, such as `npx` or a renamed binary, name the agent: `unsent --as claude npx @anthropic-ai/claude-code`, or put the variable in front of that one command: `UNSENT_AGENT=claude unsent npx @anthropic-ai/claude-code`. Don't export `UNSENT_AGENT` in your shell profile: every wrapped command whose name unsent doesn't recognize would then be read as that agent.

After a crash, a closed window or a reboot:

```sh
unsent list          # drafts left behind, newest first
unsent restore       # copy the newest one to the clipboard, then paste it
unsent show 2        # print draft 2
unsent list --all    # also drafts you cleared or replaced
unsent list --here --agent claude   # only Claude Code's drafts from this folder
```

Each draft remembers the agent and the folder it came from. The notice only offers a draft back to that agent in that folder, and counts the ones left in subfolders. `unsent restore --agent claude` copies that agent's newest draft from this folder, and a number from `unsent list` reaches any draft.

For a script, a launcher or an agent looking for a lost prompt, `unsent list --json` prints the same list, with the same filters, as a JSON array, newest first. `unsent show N --json` prints one draft as an object with the same fields plus its text:

| Field | What it holds |
| --- | --- |
| `format` | the version of this shape, now `1` |
| `n` | the number `show` and `restore` take |
| `id` | unsent's id for the draft |
| `kind` | `orphan` (left behind), `history` (cleared or replaced), `version` (an earlier version) or `shell` (a shell's command line) |
| `agent`, `agent_session` | the agent, and its own id of the conversation, `""` when unknown |
| `folder` | the folder it was typed in, as a real path |
| `started`, `updated`, `ended` | RFC 3339 times with the offset, `""` when unknown |
| `lines`, `bytes`, `first_line` | its size, and its first line with text, cut to 100 characters |
| `text`, `pastes` | `show` only: the draft with pastes expanded, and the raw text of each paste |

Fields are only ever added. A renamed or retyped field bumps `format`.

## A note for Claude Code when you reopen a conversation

```sh
unsent hook claude    # prints a SessionStart hook for Claude Code's settings.json
```

It prints the JSON to add to `~/.claude/settings.json` (or `settings.json` in `$CLAUDE_CONFIG_DIR`), next to any hooks you already have. `unsent` never edits that file. When you reopen a conversation that left a draft, the hook gives Claude one line to read with your next message: `unsent` kept a draft for this conversation, from when, how many lines, and its first line, and Claude should ask you whether to continue with it, not act on it. That first line, cut to 100 characters, goes to Claude with whatever you send next; the rest of the draft never does. Nothing is drawn on screen and nothing is sent for you. A new chat, `/clear`, a compact, a fork or a conversation with no draft gets no note. `UNSENT_NOTICE=0` leaves the note on, since installing the hook is how you asked for it. The hook names `unsent` by its full path, so run `unsent hook claude` again if `unsent` moves.

## What differs in Codex

`unsent codex` works the way `unsent claude` does. Three things differ.

- **Reopening a conversation.** A draft goes back into the box of `codex resume <id>`, `codex resume --last` and the conversation you choose in the picker that `codex resume` opens. `codex fork` and `/new` start another conversation, so they get the notice. After `/new` or `/resume` inside a running Codex, that Codex holds two conversations and `unsent` can't tell which one is on screen, so a draft typed after it gets the notice too. A plain `codex` runs its conversations in a shared background server, and `unsent` can name the conversation only when that `codex` started the server; the [limits](limits.md) say more.
- **New lines and sends.** Ctrl+J makes a new line, and so do Shift+Enter and Alt+Enter in a terminal that sends them as keys of their own. Enter sends the box, and so does Tab, which queues the message while Codex is still working. Both count as a send in the sent log. Ctrl+C on a draft clears it into history, and on an empty box it quits Codex.
- **Long pastes.** Codex shows a paste over 1,000 characters as `[Pasted Content 1234 chars]`, and a second one of the same size as `[Pasted Content 1234 chars] #2`. The count includes line breaks. `unsent` saves the pasted text in its place. A draft over 1,000 characters that goes back into the box shows as that placeholder too, while Codex holds the whole text; Ctrl+G opens it in your editor.

## What differs in agy

`unsent agy` works the way `unsent claude` does. Six things differ.

- **Reopening a conversation.** A draft goes back into the box of `agy -c` and `agy --conversation <id>`. A new chat is another conversation, and agy gives one an id only when you send its first message, so a new chat gets the notice and never a paste. agy draws inline on the main screen, where the row under the box belongs to whatever your terminal held before it, so the line saying a draft was put back comes after agy exits rather than under the box.
- **New lines and sends.** Enter is the only key that sends. Ctrl+J, Alt+Enter, Shift+Enter, Esc then Enter and a backslash then Enter all make a new line. Ctrl+C and Esc keep your draft, and a second Ctrl+C quits. Ctrl+U is the only key that clears the box, one line at a time.
- **Bash mode.** `!` at the start of an empty box turns it on, and the `!` is agy's own mark rather than part of what you typed. At Ctrl+G agy hands your editor the command alone, so that is what `unsent` saves and what comes back. Turn bash mode on again yourself before you run it.
- **Long pastes.** agy shows a paste of more than 15 lines (or half the window's rows, if that is less) as `[Pasted text #1 +16 lines]`, and one with a line over 1,000 characters as `[Pasted text #2 1001 chars]`. `unsent` saves the pasted text in its place, with each tab as four spaces and a line break at the end dropped, as agy keeps it. A draft that goes back into the box shows as that placeholder too, while agy holds the whole text; Ctrl+G opens it in your editor. A draft with a tab, or one ending in a line break, goes to the clipboard instead, because agy would change it.
- **Accents typed as two characters.** agy never draws a combining mark. `é` typed as `e` and U+0301 reaches agy whole, and stands on the screen as `e`, so the draft `unsent` saves from the screen has the bare letter. Accents typed as one character, the usual case and what a Spanish keyboard sends, are saved as they are. The [limits](limits.md) say what else the screen cannot carry.
- **The sent log.** A conversation's log starts with its second message. agy creates the conversation with the first one, so nothing names it while that message is on its way, and it stays in the log of that run.

## What differs in pi

`unsent pi` saves the box the way `unsent claude` does. Four things differ.

- **Reopening a conversation.** pi doesn't write down which conversation a running pi has open: it keeps no file per process, and it writes a conversation's file only once a reply comes. So `pi -c`, `pi -r`, `pi --session` and `/resume` look the same from outside until the next message, and a pi draft never goes back into the box by itself. The notice before and after pi runs names it, and `unsent restore` copies it. For the same reason the sent log is one per run, not one per conversation.
- **New lines and sends.** Ctrl+J and Shift+Enter make a new line, and so does `\` then Enter. Enter sends, and so does Alt+Enter, pi's follow-up key, when pi is idle; Esc then Enter reads as Alt+Enter and sends too, so it is never a new line. Ctrl+C on a draft clears it into history, and pi's undo, Ctrl+-, brings it back, which `unsent` saves again. Up with the cursor at the start of a draft shows pi's history while pi keeps your draft aside, and `unsent` keeps the draft as it was. If you then edit or send the entry, pi drops your draft, and `unsent` puts it in history first.
- **Long pastes.** pi shows a paste of more than 10 lines as `[paste #1 +12 lines]` and one over 1,000 characters as `[paste #2 1001 chars]`. Deleting one with Backspace renumbers the ones after it; `unsent` follows the renumbering and saves the pasted text in each place, with each tab as four spaces, as pi keeps it.
- **Ctrl+Z.** As with Codex, while pi is stopped your shell gets keys in the form pi asked the terminal for, so Ctrl+C at that prompt can show as `^[[99;5u`. After `fg`, pasting works as before.
