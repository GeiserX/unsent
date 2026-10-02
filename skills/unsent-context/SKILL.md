---
name: unsent-context
description: Use when the user asks why they answered something, wants the context of their sent prompts filled, or asks to run the gloss pass; also on a schedule. Fills each Claude Code message in unsent's sent log that replies to the agent with a one-line gloss of what was asked and what was chosen, and marks the replies unsent's own rule missed.
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
unsent log <agent_session> --json   # one conversation and its messages
```

Name a conversation by its `agent_session` (Claude Code's own session id), not by `n` or `id`: list numbers move when another session sends, and one run's `id` can name several conversations (after `/clear`), which makes the command exit 2. Fall back to `id` only when `agent_session` is empty. Only Claude Code conversations (`"agent": "claude"`) have context.

## 3. Sort replies from directives

Read each message in `messages` with a non-empty `asked` and no `gloss`. unsent sets `reply_to` when the message answers one of the agent's menu questions or when the agent's last paragraph holds a `?` outside a code block, so many replies still have it false. Decide from `asked` and `text` alone:

- **A reply**: the text answers, chooses, confirms or refuses something the agent said, or asks about it (`Delete which copies exactly?` after `...or keep them on disk?`).
- **A directive**: a new instruction unrelated to what the agent said. It gets no gloss and nothing is saved.

A message with `reply_to` true is a reply. When unsure, it is a directive.

## 4. Write the glosses

For each reply, write ONE line:

- At most 160 characters.
- What was asked, then what the person chose. For example: `Asked whether to keep the v1 flag or drop it; chose to drop it and bump the format.`
- Taken only from that message's `asked` and `text`. Never invent a reason that is not in those two fields. If the choice cannot be read from them, write what was asked and quote the reply.
- No names, paths or secrets beyond what `text` already holds.

Skip messages with an empty `asked` and entries that are `{"resumed": ...}` lines.

## 5. Save each gloss

```sh
unsent context add <agent_session> <uuid> --gloss "Asked whether to keep the v1 flag or drop it; chose to drop it." --reply
```

`--reply` sets the message's `reply_to` to true, so `unsent log` shows what it answered. Pass it for every reply; it is harmless when `reply_to` is already true.

`<uuid>` is the message's `uuid` from the `--json` output you just read. Use it whenever the message has one: a context pass can add an earlier message while you work, which moves every `n` after it. Only for a message with no `uuid`, pass its `n` instead, and read the conversation again right before, since numbers also shift when old messages are dropped. For text with quotes, pass `--gloss -` and the line on standard input.

## 6. Finish

Stop when every message with a non-empty `asked` and no `gloss` has been read once in this run; directives stay without a gloss, so do not read them again. Report how many conversations you read, how many glosses you wrote, and how many of those replies had `reply_to` false before.

## Installing

Symlink or copy this folder into `~/.claude/skills/`, or the skills folder of your agent:

```sh
ln -s "$PWD/skills/unsent-context" ~/.claude/skills/unsent-context   # from a checkout of unsent
```

To run it on a schedule, ask your agent to run the unsent-context skill from a cron job or a scheduled task.
