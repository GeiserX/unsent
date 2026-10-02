# Context

A message in the [sent log](sent-messages.md) says what you sent. Context says what it was sent in reply to. "Yes, the second one" means nothing a week later; next to the question it answered, it is a decision you can read back.

Context is read from Claude Code's own transcript of the conversation, after the fact. The box is still where the text comes from while you type; the transcript only adds what the box cannot know, such as the question on screen when you pressed Enter.

## What a message carries

Each message in a sent log can carry these fields. A message with none of them is a plain directive, saved as it was sent.

| Field | What it holds |
| --- | --- |
| `asked` | the agent's text right before your message: every text block of its last reply, keeping the last 1,000 characters, since a question comes at the end. For an `answer`, the questions it answers. `""` when your message followed another of yours with no reply between them. |
| `reply_to` | `true` when the message answers the agent: an answer to one of its questions, or `asked` ends with a line that ends in `?` |
| `kind` | how the message reached the agent, below. `""` for a message `unsent` read off the box live until a pass matches it to its turn; `source` `""` is what marks a message `unsent` saw in the box. |
| `uuid` | the id of the turn in Claude Code's transcript. `unsent` uses it so a turn is never added twice. |
| `gloss` | one line saying what was asked and what you chose, written later by the [skill](#the-skill) through `unsent context add` |
| `source` | `""` when `unsent` saw the message in the box, `transcript` when it came from the transcript, `history` when it came from Claude Code's `history.jsonl` |

The values of `kind`:

| `kind` | The message was |
| --- | --- |
| `typed` | typed in the box and sent while the agent waited |
| `queued` | typed in the box and sent while the agent worked, queued for its next turn |
| `absorbed` | typed while the agent worked and taken into the turn it was already running |
| `answer` | an answer to one of the agent's questions, picked from its menu. The text holds each question and its answer as `<question> → <answer>`, joined by `; `, and `asked` holds the questions. |
| `slash` | a slash command, such as `/review 42` |

## Where a Claude Code turn comes from

`unsent` reads your turns from three places in the transcript.

1. **The box.** What you typed and sent, including slash commands.
2. **Prompts absorbed while the agent worked.** Claude Code can take a message you send mid-turn into the turn it is running. That message never reaches the box's history the usual way, so the live sent log can miss it.
3. **Answers to the agent's questions.** When Claude Code asks you to pick from a menu, your choice is a tool result, not a message in the box. A question nobody answered, or one you dismissed to type something else, is not a turn; what you typed instead is.

Messages that Claude Code or another program sends on your behalf are not your turns and are skipped: compaction summaries, command output, hook notes, task notifications, messages from other sessions and from subagents.

## Commands

```sh
unsent context                       # add context to every Claude Code sent log that exists
unsent context --session <session>   # one conversation: a number from unsent log, its id or Claude Code's session id
unsent context --transcript <path>   # one transcript file
unsent import claude                 # also create logs for conversations unsent never saw
unsent import claude --history       # and for conversations only history.jsonl remembers
unsent context add 1 4 --gloss "Asked whether to keep the old flag; chose to drop it"
```

`unsent context` matches each logged message to its turn in the transcript by its text, with spaces collapsed, and fills in `uuid`, `kind`, `asked` and `reply_to`. A paste counts as the text it holds, whether the transcript wraps it in `<pasted_content>` tags or the log still shows it as `[Pasted text #1]` with the paste beside it. A message and a turn more than an hour apart are never matched, and when several messages share a text, such as `yes`, each turn takes the one nearest to it in time. Turns that are not in the log, usually answers and absorbed prompts, are added with `source` `transcript`. A gloss already written is kept. It prints one line, such as `3 logs updated, 12 messages added, 87 matched`. `--json` prints the same counts as an object, with `skipped` added: turns older than 365 days or with no uuid, and transcripts it could not read.

```json
{
  "logs": 3,
  "added": 12,
  "matched": 87,
  "skipped": 0
}
```

`--session` exits with status 2 when no conversation by that name is found, and when the conversation has a transcript but no sent log yet; `unsent import claude` makes that log.

`--config-dir DIR` adds a Claude Code config folder to read, and can be given more than once. Without it, `unsent` reads `$CLAUDE_CONFIG_DIR` when it is set, and `~/.claude`. Two config folders that share one `projects` folder through a symlink are read once.

`unsent import claude` is the one-off backfill. It does what `unsent context` does and also creates a log for every transcript that has none, so your past conversations show in `unsent log`. With `--history`, a session found only in Claude Code's `history.jsonl`, whose transcript is gone, gets a log too: its messages are `typed`, with `source` `history` and no `asked`, since that file keeps no reply. Turns older than the 365 days a log is kept are skipped. When `UNSENT_ON_SEND` or `UNSENT_ON_SEND_CLAUDE` is `delete`, it imports nothing, says so and exits with status 2.

`unsent context add <session> <n|uuid> --gloss TEXT` sets the gloss of one message, named by its `uuid` or by `n`. Prefer the `uuid`: it never changes, while `n` is the message's place in the log as it is now, and a pass that adds an earlier turn, such as one the Stop hook starts while you read, moves every message after it. `<session>` is what `unsent log <session>` takes: the number from the list, unsent's id or Claude Code's session id. `--gloss -` reads the text from standard input. It exits with status 2 when the session or the message is not there.

`unsent import` and `unsent context` take the place of any program with that name, such as ImageMagick's `import`. To run such a program through `unsent`, use `unsent -- import`.

## Reading it back

`unsent log <n>` prints up to two lines under a message that has context:

```text
4  2026-09-30 10:41
the second one, and keep the tests
  ↳ answering: Should I split the parser into its own package, or keep it in main?
  ↳ why: Asked whether to split the parser out; chose its own package, tests kept
```

The `answering` line shows the last line of `asked` that has text, where the question is, cut to 200 characters, and only when `reply_to` is true. The `why` line shows the gloss. `unsent log <n> --json` carries all six fields on each message, each left out when empty. [Sent messages](sent-messages.md) has the rest of the shape.

## The Stop hook

`unsent hook claude` prints a Stop hook next to the SessionStart hook described in [Using unsent](usage.md#a-note-for-claude-code-when-you-reopen-a-conversation). Add both to `settings.json`; `unsent` never edits that file.

Claude Code runs the Stop hook each time it finishes a reply. The hook reads the transcript path Claude Code hands it, checks that it is a `.jsonl` file in a `projects` folder, starts `unsent context --transcript <path>` in the background and exits at once. Claude Code never waits for it. Passes the hook starts on one transcript run at least 10 minutes apart: a reply that ends sooner after the last pass starts one that waits until the 10 minutes are up, and replies that end while it waits leave their turns to it. So the newest turns reach the log at most 10 minutes after the reply, even when the conversation ends in the meantime. Run `unsent context` yourself when you want them at once. When on send is `delete` for Claude Code, the hook does nothing.

Without the hook, run `unsent context` yourself, or let the skill run it.

## Retention

A sent log is kept for 365 days after its last message. A log that `unsent context` or `unsent import claude` writes gets its file time set to its last message, so a backfill of an old conversation does not keep it longer than if `unsent` had logged it live.

## What it does not do

- Compactions are not recorded. A compaction summary repeats your earlier messages, so it is never read as a turn. A `/compact` you type never gets context either: the transcript keeps it only as a bare `/compact` record, without what you typed after it, and that record is not read as a turn.
- Only Claude Code's transcripts are read. Messages sent to Codex, pi and agy have no context yet.
- A message sent while on send was `delete` is never imported later. The setting means keep nothing, and it holds for the transcript too.
- A conversation that is not in the transcript or in `history.jsonl` cannot be backfilled.

## Privacy

`unsent` reads a transcript only for your own turns and for the agent's text right before each of them. Tool calls, tool output and the rest of the conversation are skipped. It writes only to its own folder, never to Claude Code's, and makes no network connections. The gloss is written by an agent you run, through `unsent context add`; `unsent` itself never calls a model.

## The skill

The repo ships a skill, [skills/unsent-context](https://github.com/GeiserX/unsent/tree/main/skills/unsent-context), that runs the pass for you: it runs `unsent context`, then writes a gloss for each answer that has none, from `asked` and the message alone. Copy or symlink the folder into `~/.claude/skills` (or the skills folder of your agent) and ask for it, or run it on a schedule.

```sh
ln -s "$PWD/skills/unsent-context" ~/.claude/skills/unsent-context   # from a checkout of unsent
```
