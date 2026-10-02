# Sent messages

Every message you send goes to that session's sent log, in order, with its time and with pastes expanded. For Claude Code, Codex and agy the log belongs to the conversation (pi's is one per run): resume it (`claude --resume`, `claude -c`, the resume picker or `/resume`; `codex resume <id>`, `codex resume --last` or its picker; `agy -c` or `agy --conversation <id>`) and new messages join the same log, after a line saying when it was resumed. `unsent log <id>` also takes the agent's own id for the conversation: Claude Code's session id, Codex's thread id, or agy's conversation id. An agy conversation is created by its own first message, so that message is in the log of the run that sent it and the conversation's log starts with the second. After you start a new session, the old one's messages are one command away:

```sh
unsent log                 # sessions with sent messages, newest first
unsent log 1               # every message sent in session 1
unsent log 1 --copy 3      # copy message 3 of it to the clipboard
unsent log --here --agent claude   # only Claude Code's sessions in this folder
unsent log --json          # the same list as a JSON array, for a script or an agent
unsent log 1 --json        # session 1 and all its messages as one JSON object
unsent forget --log 20260927-101500-4242   # delete one session's sent log, by the id unsent log shows
```

`unsent log --json` prints the same list, with the same filters, newest first. `unsent log N --json` prints one session with the same fields, except that `messages` holds the messages themselves instead of their count. Nothing but the JSON goes to the output. The rules of `list --json` hold: fields are only ever added, and a renamed or retyped field bumps `format`.

| Field | What it holds |
| --- | --- |
| `format` | the version of this shape, now `1` |
| `n` | the number `unsent log` takes |
| `id` | unsent's id for the run that started the log, or `claude-<session id>` for a log that `unsent import claude` made |
| `agent`, `agent_session` | the agent, and its own id of the conversation, `""` when unknown |
| `folder` | the folder the session ran in, as a real path |
| `started`, `updated` | when the session started and when its last message was sent, RFC 3339 times with the offset |
| `messages` | in the list, how many messages the session sent. For one session, its messages in order, each with `n` (the number `--copy` takes), `sent` (the time), `text` (pastes expanded) and `pastes` (the raw text of each paste that could not be placed), plus the context fields below when they are set. An entry `{"resumed": <time>}` marks where the conversation was reopened. |
| `first_line` | the last message's first line with text, cut to 100 characters |
| `dropped` | how many of the oldest messages were removed to keep the log under 5 MB, `0` when none. The kept messages are numbered from 1. |

A message of a Claude Code conversation can also carry its [context](context.md), each field left out when empty:

| Field | What it holds |
| --- | --- |
| `uuid` | the id of the turn in Claude Code's transcript |
| `kind` | `typed`, `queued`, `absorbed`, `answer` or `slash`; `""` for a message read off the box live until `unsent context` matches it to its turn (`source` `""` marks a message read off the box) |
| `asked` | the agent's text right before the message, its last 1,000 characters |
| `reply_to` | `true` when the message answers the agent: an answer to one of its questions, a message after a reply whose last paragraph holds a `?`, or one the context skill marked with `unsent context add --reply` |
| `gloss` | one line on what was asked and what was chosen, set by `unsent context add` |
| `source` | `""` when read off the box, `transcript` or `history` when backfilled |

`unsent forget --log` takes the session id, not the number: the numbers move when another session sends its first message.

A send and a clear both empty the box, and the key you pressed tells them apart. A draft counts as sent only when Enter was the last key before the box emptied, with no Ctrl+C or Esc Esc next to it, and the box was on screen when you pressed it. Typing the next message into the emptied box doesn't change that. When `unsent` can't be sure, it treats the draft as cleared and keeps it in history. A missed send costs one history entry, never text.

To keep nothing of what you send, set `UNSENT_ON_SEND=delete`. `UNSENT_ON_SEND_CLAUDE=delete` does the same for Claude Code alone, `UNSENT_ON_SEND_CODEX=delete` for Codex, `UNSENT_ON_SEND_PI=delete` for pi and `UNSENT_ON_SEND_AGY=delete` for agy; each wins over `UNSENT_ON_SEND`. Turning it off doesn't delete existing logs; `unsent forget --log` does.
