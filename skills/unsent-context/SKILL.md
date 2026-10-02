---
name: unsent-context
description: Use when the user asks why they answered something, wants the context of their sent prompts filled, or asks to run the gloss pass; also on a schedule. Fills each Claude Code message in unsent's sent log that answers the agent with a one-line gloss of what was asked and what was chosen.
---

# unsent context: the gloss pass

unsent keeps every message the user sent to Claude Code in a sent log. `unsent context` adds what the agent had just said (`asked`) and whether the message answered it (`reply_to`). This pass adds the last field, `gloss`: one line a person can read a month later.

## 1. Bring the logs up to date

```sh
unsent context
```

The first time on a machine, run the import instead. It creates logs for past conversations, including ones only Claude Code's `history.jsonl` remembers:

```sh
unsent import claude --history
```

Exit status 2 with "on send is delete for claude" means the user keeps no sent messages. Stop and say so.

## 2. List the conversations

```sh
unsent log --json            # every conversation, newest first
unsent log <id> --json       # one conversation and its messages
```

Use `id` from the list, not `n`: list numbers move when another session sends. Only Claude Code conversations (`"agent": "claude"`) have context.

## 3. Write the glosses

For each message in `messages` with `reply_to` true and no `gloss`, write ONE line:

- At most 160 characters.
- What was asked, then what the person chose. For example: `Asked whether to keep the v1 flag or drop it; chose to drop it and bump the format.`
- Taken only from that message's `asked` and `text`. Never invent a reason that is not in those two fields. If the choice cannot be read from them, write what was asked and quote the reply.
- No names, paths or secrets beyond what `text` already holds.

Messages with `reply_to` false are bare directives. They get no gloss. Skip entries that are `{"resumed": ...}` lines.

## 4. Save each gloss

```sh
unsent context add <id> <n> --gloss "Asked whether to keep the v1 flag or drop it; chose to drop it."
```

`<n>` is the message's `n` from the `--json` output you just read. Read a conversation and write its glosses in one go: numbers shift when old messages are dropped, so never reuse an `n` from an earlier read. For text with quotes, pass `--gloss -` and the line on standard input.

## 5. Finish

Stop when no message with `reply_to` true is left without a gloss. Report how many conversations you read and how many glosses you wrote.

## Installing

Symlink or copy this folder into `~/.claude/skills/`, or the skills folder of your agent:

```sh
ln -s "$PWD/skills/unsent-context" ~/.claude/skills/unsent-context   # from a checkout of unsent
```

To run it on a schedule, ask your agent to run the unsent-context skill from a cron job or a scheduled task.
