# unsent: spec for the next version

Status: draft. The maintainer's decisions are in section 8. Nothing here is built yet.

This spec lists what unsent should do, in priority order, and how we prove each part works. Every fact about an agent comes from the verified profiles in [`research/`](research/). Each claim carries a tag in square brackets:

- **measured**: someone saw it on a real screen or in a byte capture. "measured (no capture)" means someone saw it but the capture did not survive; "measured (lane)" means only the research notes back it.
- **source**: read in the agent's code or shipped bundle, never seen live.
- **docs**: the vendor's documentation only.
- **guess**: inference nobody checked.
- **design**: a number or rule this spec chooses. It is a starting value, not a finding.

A tag like [measured, codex §3] points at section 3 of [`research/codex.md`](research/codex.md). "Unmeasured" means what it says.

## Terms, defined once

- **Agent**: an interactive AI coding CLI such as Claude Code, Codex or Gemini CLI.
- **Box**: the text field in the agent's terminal UI where the user types a prompt before sending it.
- **Draft**: the text in the box that has not been sent.
- **Shadow screen**: unsent's private copy of the agent's screen, built by feeding the agent's output through the same terminal emulator library a real terminal would use (`charmbracelet/x/vt`).
- **Reader**: code that finds the box on the shadow screen and returns its rows, the cursor position and whether the box is empty.
- **Profile**: everything unsent knows about one agent. That is the reader, the wrap rule, the paste placeholder format, the delete keys, the submit keys, the Ctrl+Z policy and what restore-in-box needs.
- **Stitcher**: the code that rebuilds a draft taller than the box from the part on screen plus what it saw before ([`stitch.go`](../stitch.go)).
- **Session**: one conversation of an agent, named by the agent's own session id. The same session can be opened again later, from a new terminal and a new process, through the agent's resume (for Claude Code: `claude --resume`, `claude -c`, the resume picker, `/resume` inside a running session, or a launcher that does one of these).
  One run of `unsent <agent>` also has an id of its own, "unsent's session id", and the sent log (2.13) and the per-session caps (2.5) count by run [source, [`store.go:35`](../store.go#L35)]. A resumed conversation is a new run with the same agent session id [design].
- **Orphan**: a draft left on disk by an agent process that ended with text still in the box.
- **Send**: the user hands the box to the agent with a submit key, or runs a shell line. The box then empties. A send is not a deletion: the agent, or the shell, now holds the text.
- **Submit key**: a key that sends the box in a given agent, for example Enter. Each profile lists its own (section 2.13).
- **On-send setting**: what unsent does with a draft once it is sent. `log` appends it to the session's sent log; `delete` removes it and keeps nothing (section 2.13).
- **Sent log**: one file per session holding every message the user sent in it, in order, each with a timestamp (section 2.13).
- **Shell line**: a command typed at the shell prompt and not yet run. unsent saves it the way it saves an agent draft, labelled with the shell's name (section 2.12).
- **Folder**: the resolved real path of the working directory (`filepath.EvalSymlinks`), so `/tmp` and `/private/tmp` on macOS are one folder.
- **Restore-in-box**: when a session that left an orphan is opened again, unsent pastes the orphan into that session's empty box. It never presses Enter.
- **Placeholder**: the short token some agents show instead of a long paste, for example `[Pasted text #1 +39 lines]`.
- **Alternate screen**: a second terminal screen that full-screen programs switch to (`ESC[?1049h`). Anything printed before the switch is hidden until the program exits.
- **Synchronized output**: the marks `ESC[?2026h` and `ESC[?2026l` around a redraw. Between them the screen is half drawn, so unsent never reads it there.
- **Kitty keyboard protocol**: a terminal mode an agent can ask for (`ESC[>1u`, `ESC[>5u`, `ESC[>7u`). If the outer terminal honours it, Ctrl and Alt keys reach unsent as `CSI` sequences such as `ESC[119;5u` for Ctrl+W instead of the single byte `0x17`. We call these **CSI-u** forms.
- **modifyOtherKeys**: an older xterm mode with the same effect (`ESC[>4;2m`), giving forms like `ESC[27;5;119~`.
- **Ground truth**: the text the agent holds, taken from the agent itself. Most agents hand the box to `$EDITOR` on a key such as Ctrl+G. With `EDITOR` set to a script that copies its file argument, the copy is what the agent holds. For most agents that is the draft with pastes expanded. For Copilot and Qwen the copy keeps the paste placeholders unexpanded [measured, copilot §10; qwen Keys], so it is not the text unsent saves. Section 5.2 lists per agent how much of this was measured.

## 1. What unsent is for

People lose prompts. A terminal window closes, the machine reboots, the agent crashes, or one Ctrl+C too many clears the box. None of the agents we checked writes the draft to a local file while you type [measured (no capture) or source, generic-reader §3.D and each profile's persistence section]. Loss on kill was tested directly only for Codex [measured, generic-reader §3.D] and droid (a double Ctrl+C left nothing on disk) [measured, droid]. For the others it follows from finding no draft file or no write path in source. Two partial exceptions: Amp syncs the draft of an existing thread to its server, but a new thread's draft has nowhere to go, and how the sync behaves after a crash is unmeasured [source; measured (lane) for the new-thread case, amp §Existing recovery]. For opencode, "nothing is written on exit" is a guess, because the source was not traced exhaustively [guess, opencode §9].

unsent runs the agent inside a pseudo-terminal, passes every byte through untouched, reads the box off its shadow screen every 0.4 s, and writes the draft to disk with `fsync`. The next time that conversation is opened, however it is opened, the draft comes back into its box, ready to edit or send [design, 2.3]. A new conversation in the same folder gets a one-line notice instead, because the draft belongs to the other one [design].

The same loss happens at the shell prompt. A command typed but not yet run is gone when the window closes, and Ctrl+C drops it too; zsh, bash and fish keep only lines that ran [measured, other-textboxes §1]. unsent saves that line as well, through a hook in the shell that sees the line editor's buffer, not by wrapping the shell (section 2.12).

A second gap comes after a send. When a session ends and the user starts a new one, what they told the old session sits in the agent's own files, in a different place and format for each agent, and some keep pastes only as placeholders [measured, codex §6]. unsent keeps a sent log per session, so `unsent log` shows every message sent in a session, in order (section 2.13).

**The promise.** No text the user did not delete is ever lost. Text on screen is read exactly. Text scrolled out of sight is inferred, and an inference can be wrong, so before any save that would lose characters unsent keeps the previous version in history. A send is not a deletion, but after a send the text lives in the agent or the shell, so the on-send setting may drop unsent's own copy (section 2.13). Until unsent is sure a box was sent, it treats the emptied box as cleared and keeps the draft.

There are two known kinds of error, both in line breaks. Characters are never dropped by either. A line break can come back as a space, and a blank line can come back doubled. In the stitcher's fuzz test without blank lines, 61% of saves are identical to the draft and every miss is a lost line break [measured, codebase-audit §2]. 94% of the misses have a known cause: a line ends exactly at the edge of the box, and the screen cannot show whether the user pressed Enter there or the text wrapped. The other 6% look like a break lost earlier and kept after the layout changed, which was not traced [guess, codebase-audit §2]. With blank lines fuzzed (step 2), 56% of saves are identical at 150 seeds, and 88% are byte-exact once a line break joined into a space at a full row is allowed [measured, `TestStitchFuzz*`]. In window mode at 50 seeds, 1128 of the byte-exact misses are longer than the draft, and 1115 of those hold a doubled blank line; 60 are shorter and 708 join a line break at a row that is not full [measured, `TestStitchFuzz*`]. The doubling happens at the bottom edge of the view. With `w1\nw2\nw3\n\nw4` known and the capped view showing `w2`, `w3`, an empty row and `w4`, the user types `\nw5` after `w3`; the next view shows `w2`, `w3`, `w5` and an empty row, with `w4` scrolled out of sight below. The view's empty last row is spacing the text kept below already holds, and the stitcher adds it again: it saves `w1\nw2\nw3\nw5\n\n\nw4` for `w1\nw2\nw3\nw5\n\nw4` [measured, `stitcher.update`]. The fix is the bottom-edge twin of the top-edge rule in `stitcher.update`, which already stops an empty row at the top of the view from doubling the break above it; the floors go up when it lands. These rates are for the fuzz model, not for real typing [guess, codebase-audit §2 caveat]; the rig in section 5 measures the real rate.

Some agents also draw text differently from what they hold. Copilot 0.0.422 turns a tab into 4 spaces and deletes box-drawing characters on screen [source + measured, copilot §5; 1.0.88 not checked]. For those, the screen alone cannot be exact, and the spec says so per agent.

## 2. Features

Priorities: **Must** ships in this version. **Should** ships in this version if the musts are done. **Could** is written down so we do not forget it; it does not block anything.

### 2.1 Per-agent support

- **Must. One profile per agent.** Adding an agent means one profile file plus test fixtures from real captures. No agent-specific code anywhere else.
- **Must. Profile done definition.** A profile is done, and its agent may be called supported, only when it has all of these:
  1. the reader, with fixtures replayed from real captures of a named version;
  2. delete keys, the newline key, the submit keys, and the Ctrl+Z policy (sections 2.8 and 2.13);
  3. the paste rule: placeholder format, threshold, pairing (section 4 step 16);
  4. restore caps: how the running session's id is read, settle delay, number of empty reads, newline encoding inside a bracketed paste, what cannot go back faithfully, and whether the editor copy holds placeholders (section 2.3);
  5. the quiet gap, if the agent sends no synchronized-output marks (section 2.11);
  6. negative fixtures: empty box, trust and login dialogs, menus, history browsing;
  7. a mutation test that goes red (section 5.1);
  8. one rig run (section 5.2) and a row in the [README](../README.md) with version and date (section 5.3).

  Restore-in-box is switched on for an agent only when items 4 and 6 are done.
- **Must. Tier 1 agents:** Claude Code, Codex, Gemini CLI, GitHub Copilot CLI, opencode, pi and agy. **cursor-agent** joins this list after the capture run of decision 7: nothing about its box has been seen on screen yet [source, cursor-agent §0].
- **Should. Tier 2 agents:** Amp, droid, Qwen Code, Crush, Kiro, goose, aider, Kimi Code, Mistral Vibe, Cline and Kilo. Kilo's box looks like opencode's plus a model line inside the panel [measured, others: Kilo]; that it can share opencode's reader is a guess [guess, others: Kilo].
- **Could. Tier 3:** Letta Code, OpenHands, Grok Build, Auggie, CodeBuddy, Continue `cn`. The input boxes of Grok, Auggie and CodeBuddy sit behind a login wall and were never seen [measured, others]; Continue `cn` was not tried [others ranking].
- **Must. Which agents count.** An agent gets a profile when it has more than about 1,000 weekly downloads or the maintainer uses it [design]. Dropped for low downloads: superagent `grok-cli` (907 a week), iFlow CLI (583) and Qodo Command (198) [measured (lane), others]. Never surveyed: Atlassian Rovo Dev CLI, Google Jules CLI, gptme, Open Interpreter, plandex, `llm chat`. They are unmeasured, not excluded.
- **Must. Fail safe.** When a reader cannot find the box, unsent keeps the last good draft and saves nothing new. It never saves a guess over a good draft.
- **Must. Say when saving stopped.** If a reader never matched during a session in which the user typed, unsent prints one line on exit, naming the running version and the last verified one: `unsent: could not read codex 0.152.0's box this session (last verified 0.151.0), nothing was saved`. Today a reader that stops matching after an agent update fails silently. Agents release fast: Amp about 8 builds a day and Qwen 12 releases in 23 days [measured, amp and qwen stability sections]; agy about one release every 2 days [source, GitHub releases API, agy]. Silence is the worst failure here.
- **Must. Say when an agent is not protected.** Today unsent prints `unsent: no reader for "X" yet, running it without saving drafts` before starting an agent it has no reader for [source, [`wrap.go:76`](../wrap.go#L76)]. On the alternate screen that line is hidden at once, so unsent prints it again on exit.
- **Must. Agent identity.** Each saved draft records which agent it came from. The agent is found by the command's base name, with `UNSENT_AGENT=<name>` or `unsent --as <name> <command>` for `npx`, `node cli.js` and renamed binaries [codebase-audit §5 step 4]. Where a base name is shared by two different programs (`opencode`, `grok`, `kimi`), the reader must also confirm the agent from the screen before saving [docs, opencode §1; others "Name clashes"].
- **Must. Accents, emoji and wide characters.** The maintainer writes in Spanish, so accented text is the normal case. The fuzz and the fixtures include accented Latin text in both precomposed and decomposed forms, emoji, and a double-width character at the wrap edge. A double-width character that would cross the edge moves to the next row and leaves that row one cell short, so "a full row means a soft wrap" fails there [source, aider §2; crush §4]. CJK and IME input are unmeasured [generic-reader open questions].
- **Should. `unsent capture <agent>`** writes a fixture bundle into `testdata/<agent>/<version>/`: raw output bytes, raw input bytes, and the editor copy when the profile has an editor key. This turns "add an agent" into a PR instead of a research lane. `UNSENT_DEBUG_DIR` already logs views; it gains raw byte logs for this.

### 2.2 Generic reader

Not in this version (decision 4). What we learned stays here for when it is picked up.

- **Could. A best-effort reader for agents with no profile.** It learns the box layout from the first screen where typed text appears: text column, first-row marker, continuation prefix, typed-text colour. It then reads that layout at the cursor on every save. A prototype, replayed through unsent's emulator, reached the exact 32-line draft on six agents (Codex, Copilot 0.0.422, Gemini, opencode, Crush, aider) [measured, generic-reader §3.A].
- That result is weaker than it sounds:
  - Crush's "exact" draft was missing its first 4 characters, because a start-up dialog ate them, and the test allowed it [measured, generic-reader §3.A].
  - Codex was covered only by a frame test, so the negative controls covered five agents, not six [measured, generic-reader §3.A].
  - It calibrated from the known typed text, not from keystrokes, and its tests printed results instead of asserting.
  - A deliberately broken marker still passed on aider and opencode, through the scrolled-box fallback [measured, generic-reader verification notes].

  The production version must calibrate from the key stream, assert, and pass a mutation test on every agent, including a mutation of the fallback branch.
- **Must, if the generic reader ships. Secret guard.** Never calibrate on a field that looks like a login or API-key prompt. Gemini's API-key dialog is an input box showing the key [measured (no capture), generic-reader §3.A]. Refuse when the screen shows `API key`, `password`, `token` or `passphrase`, or the typed text has no spaces and is over 20 characters [guess, design]. The second rule also refuses long paths and URLs, so it ships with a false-positive fixture set. Neither rule catches a short or spaced secret in an unlabelled field, so nothing is saved until the calibration is confirmed by a full cycle in the same layout: type, Enter, empty box, type again [guess, generic-reader §3.A "Where it fails", option (a)]. The session's first draft goes uncovered. The fixtures include a short secret in an unlabelled field, which must not be saved.
- Drafts from the generic reader are labelled "best effort" in `unsent list`. Whether it is on by default is decided when it is picked up.

### 2.3 Restore-in-box

Restore pastes the recovered draft into the empty box and never sends it. A draft belongs to the conversation it was typed in, so restore is per session. It fires when that same session is opened again, however it is opened (decision 1).

- **Must. A draft is keyed to its session.** Each record holds the agent, and the agent's own session id in the field `agent_session`. It is the same field the sent log's header carries (2.13), read the same way [design]. The profile reads it at every save, because one process can change session: `/resume` and `/clear` inside a running Claude Code switch the id of the same pid [measured, claude §10]. A save records the id that was current when it read the box.
- **Must. Automatic restore on reopening the session.** When the running agent is in a session that has an orphan, unsent pastes that orphan into the box once the box is ready. The user sees their draft and decides what to do with it. It fires on its own, with no key or command, whether the session came back through a resume flag, `-c`, a picker chosen inside the terminal UI, `/resume` inside a running session, or a launcher (decision 1).
- **Must. Detection never relies on the command line.** A picker chooses the session inside the terminal UI, after unsent has seen the arguments, so unsent reads which session the running process is in, the way the profile says. For Claude Code that is `sessionId` in `$CLAUDE_CONFIG_DIR/sessions/<pid>.json`, where `<pid>` is the pid unsent started [measured, claude §10]:
  - the file exists from start, before any key and before any transcript;
  - `--resume <id>` and `-c` show the resumed id, and after `--resume <id>` the new messages went to the same transcript;
  - while a picker is open the file holds a fresh id that matches no conversation, and it switches to the chosen id by the time the box is drawn (same 20 ms poll, 67 ms after Enter);
  - Claude Code holds neither this file nor the transcript open, so `lsof` on the child cannot name the session, and unsent reads the file instead;
  - unsent reads only the `sessionId` field, never the `.key` file beside it (2.7).

  The id used for a restore is the one read when the safety conditions below pass, never one read earlier [design]. Without `CLAUDE_CONFIG_DIR`, the folder is `~/.claude/sessions/` [guess, claude §10]. If no file matches the child's pid, which a launcher that does not `exec` the agent could cause [guess, claude open question 9], unsent looks for a file whose pid is a descendant of its child [design]; if none matches, no automatic restore, and the notice after exit names the draft.
- **Must. A new chat gets the notice, not a paste.** A brand-new chat in the same folder is another conversation. So is `/clear`, and so is `--fork-session`, which gets a new id [measured, claude §10]. None of them gets an automatic paste. They get the one-line notice (2.4), and `unsent list` and `unsent restore` reach the draft. Cross-agent restore stays off: a Claude conversation cannot be resumed by Codex (decision 2).
- **Must. Chat starts only.** Restore never fires when the command carries a prompt argument (`claude "fix x"` sends on start), or on subcommands that are not a chat box (`codex login`, `claude mcp`, `gemini extensions`) [design]. Resume forms are starts where restore does fire. Each profile lists its non-chat subcommands and its resume forms.
- **Must. Each profile says how its session id is read**, or says "to measure". Restore-in-box is off for a profile until it can:

  | Agent | Running session's id | Resume forms | Tag |
  |---|---|---|---|
  | Claude Code | `sessionId` in `sessions/<pid>.json` | `--resume <id>`, `-c`, `--resume` picker, `/resume` | measured, claude §10 |
  | Codex | the one `thread-writer-locks/<id>.lock` the process holds open, from before the box is drawn; with the shared background server, the server this Codex started holds it; two held (after `/new`) name none [measured, codex §10, §11]. `history.jsonl` gets its `session_id` only at a Ctrl+C or a send, from any terminal [measured, codex §10] | `codex resume <id>`, `codex resume --last`, the picker (`codex resume`) [measured, codex §11] | measured, codex §11 |
  | Gemini CLI | a `chats/session-*.jsonl` file created at start [measured, gemini §7]; which field names the session: to measure | to measure | |
  | agy | the one `conversations/<id>.db` the process holds open: from the first submit of a new conversation (0.3 s after Enter) or from before the box is drawn of a resumed one; `history.jsonl` carries `conversationId` only from the second submit on [measured, agy "Session identity"] | `agy -c`, `agy --conversation <id>`; `-i` and `-p` send on start, so they are not [measured, agy "Resume forms" and "Chat starts, corrected"] | measured, agy |
  | Kiro | the `<id>` of `sessions/cli/<id>.history`, whose session files exist before any submit [measured, kiro §How it was measured; source, kiro §Draft persistence]; which one is the running process's: to measure | to measure | |
  | pi | none another process can read before the session's first reply: no file keyed by pid, nothing held open, and the session file is written with the first assistant message; so restore stays off [measured, pi "Session identity", "Profile run"] | `pi -c`, `pi -r`, `pi --session <id>`, `/resume`, none of which writes anything until the next entry [measured, pi "Resume forms"] | measured, pi |
  | cursor-agent | the `<chatId>` folder of `chats/<md5(cwd)>/<chatId>/` [source, cursor-agent §6] | a resume picker [source, cursor-agent §2] | |
  | Copilot, opencode and every tier 2 and 3 agent | to measure | to measure | |

- **Could not be resumed, so never restored in the box.** A Claude Code session that was typed in but never sent has no transcript, is not in the picker, and `claude --resume <id>` answers `No conversation found with session ID: <id>` [measured, claude §10]. Its draft can only come back through the notice and `unsent restore`. Section 8 lists this as open.
- **Must. Safety conditions, all of them, before unsent writes a single byte** [codebase-audit §4.1 unless noted]:
  1. After the profile's settle delay, the reader has returned "empty box" on 3 saves in a row (1.2 s at the 0.4 s tick) [design; the count is per profile]. Settle delays are measured for three agents only, and none of them is a paste delay:
     - Claude Code: Esc+Enter made a new line about 1.2 s after the box appeared on a clean config [measured (no capture), claude §4]. The repo's [CLAUDE.md](../CLAUDE.md) gives about 9 s on the maintainer's config; that plugins and hooks cause the difference is unverified [claude open question 5].
     - Qwen: the placeholder plus about 1 s [measured (lane), qwen startup].
     - Gemini: the box itself took 14 to 24 s to appear [measured (lane), gemini §8]. That is start-up time, not a delay after the box appears.

     For Claude Code a 1,391-character paste sent on the first frame that showed the empty box, 0.21 s after start, landed whole [measured, one run on a clean config, claude §10]; the maintainer's config was not tried. For every other agent, whether a paste in the first seconds is dropped the way an early Esc+Enter is, is unmeasured [codebase-audit §4.1]. Each profile measures its paste settle delay before restore is switched on.
  2. The user has typed nothing since the reader first found the box in the current session. Keys pressed in a resume picker, or the `/resume` typed to open one, come before the box of the chosen session is drawn, so they do not count [measured for the `--resume` picker: no box while it is open, claude §10; unmeasured for `/resume`; design for the rule]. Focus reports, mouse reports and resizes do not count as typing.
  3. The agent has turned on bracketed paste (`ESC[?2004h`) and not turned it off. unsent tracks this in the output the same way it already tracks `?2026`. Without bracketed paste a newline in the draft is Enter, and Enter sends.
  4. The draft is cleaned first: every `ESC` byte is removed, so an embedded `ESC[201~` followed by a newline can never end the paste early and submit.
  5. The newline encoding inside the paste (`\n` or `\r`) is the one the profile measured. For Claude Code both arrive as `\n`, and the Ctrl+G copy was byte-identical to the draft either way [measured, claude §10], so unsent sends the draft's own `\n` [design]. It is unmeasured for every other agent [codebase-audit §4.1].
  6. The box is in insert mode. Agents with a vim-style editing mode may treat a paste in normal mode as commands; for Claude Code's vim mode this is unmeasured [guess]. Until measured, a profile with such a mode does not restore while it is on.
  7. The screen is not a dialog. Trust dialogs and selection menus draw the same markers as boxes: Copilot's trust dialog is a box [measured, generic-reader §2], droid's trust dialog uses the same rounded border and orange `>` [measured, droid §What the box looks like], Codex's trust list uses a `›` [measured, codex §1]. Each profile needs a captured dialog as a negative test before its restore is switched on.
  8. The profile says restore is safe for this draft. Some agents change pasted text on the way in, so a paste would not put back what the user had:
     - Crush turns a paste with 10 or more line feeds, or one line over 1000 bytes, into an attachment outside the box [source, crush §6].
     - Gemini and Mistral Vibe rewrite a paste of existing file paths to `@path` mentions [source, gemini §4; others: Vibe].
     - Letta shows inline pastes with every newline as `↵` [measured, others: Letta].
     - pi turns tabs into 4 spaces and drops control characters [source, pi §Pastes].
     - Copilot saves a paste over 20,480 bytes to a workspace file [measured, copilot §7].
     - Amp drops a typed Tab [measured, amp keys table]. What it does with a Tab inside a bracketed paste is unmeasured, so a draft with a Tab is not restored into Amp until it is.

     When the profile says a draft cannot go back faithfully, unsent copies it to the clipboard instead and says so.
- **Must. Claim the orphan first.** The same session can be opened in two terminals at once, and each would inject the same orphan; whether Claude Code allows that was not measured [claude §10]. Before injecting, a process claims the orphan atomically (rename it to a claimed name in the same folder) [design]. If verification fails, the claim is released.
- **Must. Record the paste as a paste.** unsent feeds the injected text to its own paste tracker. Most agents turn a long paste into a placeholder, and without the tracker entry the reopened session would save the placeholder instead of the text [codebase-audit §4.1].
- **Must. Verify before archiving.** The orphan moves to history only after a save of the reopened session reads back the same draft. "The same" means equal, or different only by a line break read back as a space where the read-back row was full (the known error in section 1) [design]. If the read-back fails, the orphan stays. After 3 failed restores of one orphan, unsent stops injecting it and leaves it for `unsent restore` [design].
- **Must. Cancel on typing.** Any key the user presses before the injection cancels it.
- **Must. A long draft goes back as one bracketed paste, placeholder and all.** In Claude Code a paste of 4 or more lines, or over 800 characters, shows as `[Pasted text #N +M lines]`, or `[Pasted text #N]` with no line break [measured, claude §3]. The placeholder loses nothing: a 1,391-character draft with 7 line breaks and accents, pasted into a resumed session and into a new one, and a 900-character line pasted into a resumed session, came out of Ctrl+G byte-identical to the draft, twice in a row for the line [measured, one run each on a clean config, claude §10]. So Claude Code holds the exact text, and Ctrl+G opens all of it in `$EDITOR` for editing. Backspace removes the whole placeholder [measured (no capture), claude §3]. We do not paste twice to expand it inline ("paste again to expand" [measured, claude §3]): a second paste that did not expand would leave the draft in the box twice [guess] (decision 13).
- **Must. Say it was put back, not sent.** After a restore verifies, unsent writes one line over the row under the box: `unsent: put back your draft from 18:42, not sent` [design]. It writes after a complete frame, never inside a `?2026` pair, with the cursor saved and restored [design]. Whether Claude Code's renderer repaints that row on its next frame or leaves the line until the row changes is unmeasured [guess]; if the line corrupts the frame in the rig, it moves to the notice after exit only. The notice after exit says the same (2.4). `UNSENT_NOTICE=0` turns this line off (2.4).

### 2.4 Recovery when restore-in-box does not apply

- **Must. Notice after exit.** On the alternate screen, the notice unsent prints before starting the agent is hidden: Claude Code, opencode, Amp, Crush, Copilot 1.0.88, Qwen and Cline all start with `ESC[?1049h` [measured, each profile's screen mode]. Kiro clears the screen at start with `ESC[2J` [measured, kiro §Screen modes]. cursor-agent can wipe scrollback mid-session with `ESC[3J` [source, cursor-agent §1]. So when an orphan was not restored, unsent prints the notice again after the agent exits. In a new chat, the notice names the drafts this agent left in this folder under other sessions, since none of them is pasted there (2.3) [design].
- **Must. `unsent restore`** keeps working as today: clipboard, or print when there is no clipboard, `clip.exe` on WSL [source, generic-reader §5; [`main.go:231-249`](../main.go#L231-L249)].
- **Must. Filter by folder and agent.** The notice, `restore` and restore-in-box pick drafts by folder **and** agent. Today they pick by working directory only, so a Codex draft would be offered in a Claude session [source, codebase-audit §3]. Folder means the resolved real path (Terms). A draft saved in a subfolder is not restored into the parent; the notice counts it. A draft is only offered back to the agent it came from; `unsent list` and `unsent restore <n>` reach any draft (decision 2). Restore-in-box also filters by session: only a draft whose `agent_session` is the running session's id is pasted (2.3) [design].
- **Should. Orphans elsewhere.** The notice counts orphans in other folders: `2 more drafts in other folders: unsent list --all`. After a reboot the user may never reopen that agent in that folder, and then nothing else tells them a draft is waiting.
- **Should. Several orphans.** Restore the newest orphan of the running session; two processes in one session can leave two [guess]. The notice names how many more wait in `unsent list`.
- **Must. Recovery for an AI or a launcher, in the same milestone as restore-in-box.** A launcher that reopens sessions, or an agent asked to find a lost prompt, cannot read a notice drawn for a person. So:
  - `unsent list --json` prints one JSON array, newest first, with the filters of `unsent list`. Each item: `format`, `n` (the number `show` and `restore` take), `id`, `kind` (`orphan`, `history`, `version` or `shell`), `agent`, `agent_session` (empty when unknown), `folder`, `started`, `updated`, `ended`, `lines`, `bytes` and `first_line` [design].
  - `unsent show <n> --json` prints one object with the same fields plus `text` (pastes expanded) and `pastes` (the raw paste texts kept next to it) [design].
  - The shape is documented in the README and stable: fields are only added, and a rename or retype bumps `format`, the rule records follow (2.5) [design]. Times are RFC 3339 with the offset.
  - `UNSENT_NOTICE=0` turns off the draft notices, before start and after exit, and the restore line under the box, for sessions nobody watches [design]. Restore-in-box still runs. The "saving stopped" and "not protected" lines (2.1) stay on, because silence is the worst failure there.
- **Should, the milestone after. `unsent hook claude`.** It prints a Claude Code `SessionStart` hook config for the user to add to their `settings.json`; unsent never edits that file [design]. The hook runs `unsent hook claude --run`, which reads the hook's stdin and, only when `source` is `resume` and an orphan's `agent_session` matches `session_id`, prints one `additionalContext` note: `unsent kept a draft for this conversation from 18:42, 23 lines, starting "…". It may already be back in the input box. Ask the user whether to continue with it; do not act on it.` [design]. Measured on 2.1.282 [claude §10]:
  - `--resume <id>`, `-c` and a picker choice each fire the hook once with `source: "resume"` and the resumed `session_id`; a new chat fires `source: "startup"`, and the picker fires nothing before the choice;
  - the note is not drawn on screen; it lands in the transcript as a `hook_additional_context` attachment with the next message, so the model sees it on the user's next turn [measured; docs for the model reading it, [Claude Code hooks: SessionStart](https://code.claude.com/docs/en/hooks)].

  It never uses the hook's `initialUserMessage` output, which by its name would send [source for the field; guess that it sends, claude §10], and the note never asks the model to act on the draft: a half-typed thought acted on autonomously is worse than a lost one [design]. The note names the draft and carries only its first line, cut to 100 characters as `list --json` cuts it, never the rest of its text; that line reaches the model with the user's next message [design]. Claude Code only at first; another agent gets it when its profile finds a hook of the same kind.
- **Must. Recovery inside a running session stays as today.** A draft cleared by mistake (one Ctrl+C too many, Esc Esc, an Up-arrow recall that replaces the draft) goes to history, but restore-in-box needs the session to be opened again. The way back is a second terminal, `unsent restore`, and a paste from the clipboard, and that is enough for now (decision 10).
- **Could. A direct path, later.** `unsent restore --into` from another pane, reaching the live session over a unix socket in the state folder (local, not network), or a reserved key chord, which takes a key away from the agent. The safety conditions of 2.3 would apply, with "typed nothing" replaced by "the box reads empty now". Not in this version.

### 2.5 History and versions

- **Must. Keep what agents throw away.** A draft cleared with Ctrl+C or Esc Esc goes to unsent's history. Agents mostly do not keep these. Claude Code's own history stores a cleared paste draft as the placeholder only, with `pastedContents: {}` [measured, claude §6]. Codex saves a Ctrl+C-cleared draft only when a thread exists, and stores pastes as placeholders [source, codex §6]. opencode keeps a cleared draft only when it has 20 or more trimmed characters or holds a paste [source, opencode §9].
- **Must. Format version on every record, and a migration rule.** A renamed field in the draft file would unmarshal as an empty draft, and `orphans()` would then delete the file [source, distribution §4]. That is a direct break of the promise. Rule: add a `format` field now; never rename or retype a field without a version bump and a migration test.
- **Must. A sent draft is not a cleared draft.** Today a box that empties goes to history whether it was sent or cleared [source, [`wrap.go:538`](../wrap.go#L538) `keepOld`]. From step 7 on, a draft unsent knows was sent follows the on-send setting (2.13) and does not go to history, so history holds cleared and replaced drafts only. When unsent cannot tell a send from a clear, the draft goes to history, as today.
- **Must. Existing caps stay:** 500 history entries and 300 safety versions overall [source, codebase-audit §2], 30 safety versions per session [source, [`store.go:20`](../store.go#L20)]. These caps evict the oldest copy even when it is the only one holding text the stitcher dropped, so a session with more than 30 lossy saves, or 300 across sessions, can lose such text [source, [`store.go:180-181`](../store.go#L180-L181)]. That is a known gap in the promise; closing it without unbounded growth is open.
- **Must. Shell lines have their own cap.** Shell lines are short and many, so cleared shell lines count against a separate cap of 500 and never push agent drafts out of history [design; other-textboxes open questions]. `unsent list` shows them under the shell's name, and `--agent zsh` filters them.
- **Must. Orphans never expire on their own.** Deleting one automatically would break the promise [design]. The user removes them with `unsent forget`.
- **Must. `unsent forget <n>`** deletes one draft or version, and `unsent forget --log <session>` deletes one session's sent log (2.13). A user who typed a secret into a box needs a way to remove it, and orphans have no expiry. This is the user deleting, so the promise allows it.
- **Should. `unsent list`** shows agent, folder, time, size and first line; `--agent`, `--here` and `--all` filter it.
- **Images stay placeholders.** `[Image #N]` comes back as the placeholder, because image bytes never cross the pseudo-terminal. The README says so today; nobody should plan to fix it.
- **Could. `unsent search <words>`** across history.
- **Could. Paste attachments.** Crush keeps big pastes as in-memory attachments that die on quit [source, crush §8]. unsent already has the paste bytes from the input side and could store them next to the draft.

### 2.6 Setup and uninstall

- **Must. `unsent setup [zsh|bash|fish]`** writes one marked block to the shell's rc file. The block lists every agent name this build has a profile for. At each shell start it defines a `function <agent> { … }` wrapper for each listed name that has no function of that name yet. The wrapper looks the agent up on `PATH` when it is called, not when the block runs, so an agent installed later, or put on `PATH` by a line after the block, is covered from the next shell. Each wrapper falls back to the real agent when `unsent` is missing, and to the shell's own "command not found" when the agent is [measured in bash 3.2, bash 5.3 and zsh 5.9, distribution §3]. After an unsent upgrade that adds profiles, the user runs setup again, which rewrites the block.
- `unsent init zsh` prints only the command-line hooks (2.12), not the wrappers, for a user who keeps the rc file by hand: an `eval "$(unsent init zsh)"` line keeps them current across upgrades, but costs one process start per shell, about 100 to 150 ms on this Mac under load [measured (lane), distribution §3 trap 8]. The static block costs nothing measurable, so it stays the default. Hooks loaded both ways run once.
- The block must use `function name { }`, never `name() { }`. With an existing `alias claude=...` the second form fails to parse and **the rest of the rc file does not run** in all three shells [measured, distribution §3 trap 1].
- **Must. No PATH shims and never replace the agent binary.** A shim makes unsent find itself again and loop [measured (lane), distribution trap 5].
- **Must. Warnings.** Setup warns about aliases that point at a path and about functions that start the agent through `env`, `command` or a full path, because those skip the wrapper. A launcher that calls `env ... claude` would run unprotected [source, distribution trap 4]. unsent only warns and flags them in `unsent status`; it never edits a launcher (decision 8). Anything that runs the agent without the user's interactive rc file also skips it: scripts, `exec claude`, and probably `tmux new-window claude` and zellij layouts [guess].
- **Must. The zsh block also installs the command-line hooks** (2.12), written into the same static block, so no process starts with the shell. `setup --undo` removes them with the rest. bash and fish get theirs in the same way when they ship.
- **Must. `unsent status`.** Lists which agent commands in this shell go through unsent, which profile and last verified version each gets, which aliases or launchers bypass it, whether this shell's command line is saved, and the on-send setting per agent (2.13). Setup warns once; status answers "am I protected?" at any time.
- **Must. Off switch.** `UNSENT_OFF=1` makes `unsent <agent>` exec the agent directly. When a reader misbehaves after an agent update, the user needs a way out that does not mean editing rc files. `command claude` also skips the wrapper; the README says both.
- **Must. `unsent setup --undo`** removes the block. Uninstall is `setup --undo` then `brew uninstall unsent`, in either order [measured, distribution trap 7]. Drafts are user data: uninstall prints where they are and never deletes them.
- **Should. fish** through `~/.config/fish/conf.d/unsent.fish` [docs only; fish is not installed on this Mac].
- Pipes and non-terminal calls already pass straight to the agent, so the wrappers are safe for `git diff | claude -p` [source, distribution §3]. Claude Code copies user functions into its Bash tool; that the Bash tool never runs with a terminal, and so always passes straight through, is unchecked [guess, distribution trap 6].

### 2.7 Privacy

- **Must. No network, ever.** No update check, no telemetry. This rules out a self-updater [source, distribution §4]. A unix socket in the state folder (2.4) is local and allowed.
- **Must. Files stay private:** folder mode `0700`, files `0600`, as today.
- **Must. The debug log is opt-in.** `UNSENT_DEBUG_DIR` writes every view with its text, and with 2.1's capture change, raw input and output bytes. It stays off unless set, and writes `0600` files.
- **Must. Secret guard** for the generic reader, if it ships later (2.2).
- **Must. Nothing unsent ships contains a real key.** Test fixtures come from runs with dummy keys only.
- **Must. The sent log is private, bounded and can be turned off.** Files `0600` in the `0700` state folder, the retention and size caps of 2.13, and `UNSENT_ON_SEND=delete` to keep no text unsent recognized as sent, once imported. Two exceptions: a send unsent cannot tell from a clear goes to history (2.13), and a shell's raw log holds every change until import (2.12). Of the agent's own files, unsent reads only the session id field (2.3, 2.13), never the transcript, and never Claude Code's `sessions/<pid>.<hash>.key` peer token [design].
- **Must. Shell privacy** (2.12). unsent saves only the line typed at the shell's own prompt, so a password typed at a `sudo`, `ssh` or `read -s` prompt never reaches it [guess until the rig checks it, 5.2]. A line that starts with a space, or matches the shell's own ignore pattern (`HISTORY_IGNORE` in zsh, `HISTIGNORE` in bash), is never saved. Run shell lines are deleted by default, because the shell's history already keeps them.

### 2.8 Keyboard protocols

This is the largest hidden gap today. unsent counts delete keys to tell text the user deleted from text that scrolled out of sight, and it catches Ctrl+Z to suspend. Both match only legacy byte sequences (`0x17`, `0x1a`, `ESC[3~`, …), and Ctrl+Z only as a lone byte [source, codebase-audit §1].

Many agents ask for the kitty protocol or modifyOtherKeys. unsent forwards the agent's output untouched, so the request reaches the outer terminal, and unsent's own emulator replies are dropped [source, opencode §2, [`wrap.go:89-90`](../wrap.go#L89-L90)]. Whether Warp, Ghostty or iTerm2 then honour the request under unsent is unmeasured [cursor-agent §7; crush §7]. Terminal.app honours neither, and iTerm2 depends on its version and settings [guess, crush §7].

Pushes seen in captures:

- Codex `ESC[>5u` in tmux; `ESC[>7u` on other terminals such as kitty and WezTerm [measured push in tmux; source for the terminal split, codex §5].
- pi `ESC[>7u` plus `ESC[>4;2m` [measured, pi §Screen mode].
- agy `ESC[>1u` plus `ESC[>4;2m` [measured, agy §Screen mode].
- Crush `ESC[>1u` plus `ESC[>4;2m` [measured, crush §2].
- Copilot 1.0.88 `ESC[>4;2m` [measured, copilot §2]; Copilot 0.0.422 sends the kitty query `ESC[?u` [measured] and decodes CSI-u when enabled [source, copilot §2].
- opencode, Amp, Kiro and droid `ESC[>4;1m` [measured: opencode §2, amp §Keys, kiro startup, droid terminal modes].

Kitty pushes that only happen when the terminal answers the query, or when environment variables name a kitty-capable terminal, are in source for cursor-agent, Kiro, droid, Qwen, Amp and opencode [source, each profile]. Claude Code has push code with an allow-list that includes Warp and tmux. One run saw the push at the trust dialog [measured (no capture), codebase-audit §1]; no run saw it at the main prompt. This is contested.

If the outer terminal honours the push, unsent would miss Ctrl+W, Ctrl+U and Ctrl+K deletes. That costs the stitcher accuracy, not text, because the safety copy still guards the promise. The worse effect: Ctrl+Z would arrive as `ESC[122;5u`, unsent would not suspend, and the agent's own suspend would hang inside the pseudo-terminal [source + docs + guess, generic-reader §5; not measured end to end in any terminal, codex open questions].

- **Must, before any decoder work. Measure first.** Owner: the maintainer, because Warp is his terminal. We prepare the build. Run Claude Code on the real config under unsent with raw input logging on (2.1, `unsent capture`), in Warp and in Ghostty. Press Ctrl+W, Ctrl+U, Ctrl+K, Alt+Backspace, Ctrl+D, Ctrl+Z and Shift+Enter. The logs land in `testdata/keys/<terminal>/`. This settles the contested Claude push and gives the decoder its first real fixtures [codebase-audit open question 2].
- **Must. Decode every key form before matching:**
  - legacy bytes and `ESC`-prefixed Alt keys;
  - kitty CSI-u, masking the Caps Lock (+64) and Num Lock (+128) bits, skipping `:` sub-fields, and ignoring release events (`:3`) under flag 7 [docs, kitty spec; codex §5];
  - functional keys with modifiers (`ESC[3;5~`);
  - modifyOtherKeys (`ESC[27;<mods>;<code>~`), which tmux with `extended-keys on` also sends [measured, generic-reader §5].
- **Must. Skip mouse and focus reports** (`ESC[<…M`, `ESC[I`, `ESC[O`). Claude Code, Amp, opencode, Qwen and Kiro in fullscreen turn on mouse tracking [measured]. These reports never count as typing (2.3).
- **Must. Per-profile delete keys.** Before the cursor, after the cursor, any amount, whole draft. Today's list is Claude's, and it misses Ctrl+D, `ESC d`, and counts `ESC 0x7f` (a whole word) as one character [source, claude §4]. Codex has no undo, so `0x1f` is Claude-only [source, codex §5].
- **Must. Per-profile Ctrl+Z policy.** Catch Ctrl+Z in any encoding and anywhere in a read, not only as a lone byte. Then act per profile: suspend on the first press (Claude Code), suspend on the second press within 2 s (Kiro [source, kiro §Keys]), or pass it to the agent where it means undo (Qwen [source, qwen §Keys]). On Linux, Gemini binds Ctrl+Z to both undo and suspend, and which one wins was not read [source, gemini §5]; passing it through could suspend Gemini inside the pseudo-terminal, so Gemini keeps unsent's suspend until measured. Today unsent steals every lone Ctrl+Z [source, [`wrap.go:115`](../wrap.go#L115)]. The policy per agent is [guess, design] until measured.
- **Should not.** Setting env vars such as `CODEX_TUI_DISABLE_KEYBOARD_ENHANCEMENT=1` to keep legacy bytes. The likely cost is Shift+Enter and Ctrl+M newlines for the user [guess, codex §5]. unsent must not change how the agent behaves; readers handle the agent as it ships (decision 3).

### 2.9 Multiplexers and terminals

unsent wraps the agent, not the terminal, so any terminal works. What changes is which key bytes arrive and how the window closes.

- **Must. tmux.** Decode `extended-keys` forms (above). Kiro switches to a real cursor when `TMUX` is set, the opposite of every capture, so its reader must handle both cursor models [source, kiro §Height].
- **Must. zellij.** It splits the paste start marker into its own read [measured, generic-reader §5]; [`paste.go`](../paste.go) must keep handling that.
- **Should. Document GNU screen.** The macOS build (4.00.03) strips the bracketed-paste markers, so the paste tracker sees nothing [measured, generic-reader §5]. A multi-line paste then reaches the agent as lines ending in CR, which could submit the first line [guess; no agent was run under screen]. That is screen's behaviour, and unsent cannot fix it. The README says so.
- **Should. Agents that ignore hang-up.** Gemini survived SIGHUP [measured (no capture), generic-reader §6]. After forwarding SIGHUP, unsent waits 5 s, then kills the agent's process group [design]. Test: a fake agent that ignores SIGHUP is gone after the wait. The draft is saved before this, as today.
- **Could. SSH.** Install unsent on the remote box and run the agent there under it. `unsent ssh host claude` has no reader and never will be exact.
- **Could. OSC 52 clipboard.** Over SSH `unsent restore` prints the draft because no clipboard tool is found [source, `main.go:231-249`]. OSC 52 is an escape sequence written to the local terminal, not network access, and would copy over SSH.
- **Out of reach.** Warp's built-in agent input is drawn by the Warp app, not by a program on a pseudo-terminal [docs, others: Warp]. This is a limit of unsent's design, not a fundamental one; it would need a Warp API. Agents run inside Warp's terminal are covered.

### 2.10 Platforms

- **Must.** macOS and Linux, arm64 and amd64, and WSL, as today.
- **Could. Native Windows.** It does not build: `GOOS=windows go vet` stops at `syscall.Flock` [measured, generic-reader §5]; behind it, `syscall.SIGWINCH` and `creack/pty` also have no Windows path [source, generic-reader §5]. A ConPTY port is hard, not impossible: it needs a Windows lock, a resize path and ConPTY output checked for `?2026` and SGR. Not in this version.

### 2.11 Performance

- **Must. The user never waits on unsent.** Passthrough stays a byte copy. Reading and saving run off the input path, on the existing 0.4 s tick.
- **Must. Measure the new costs.** Per-cell colour in the snapshot, CSI-u decoding and heavy redraw traffic: Mistral Vibe emitted 1,893 synchronized frames in one captured session [measured, others §3], Amp 2,537 [measured, amp §Screen mode]. Targets: added keystroke latency p99 under 2 ms, and idle CPU under 0.5% of one core [design]. Both are unmeasured; the rig (section 5) records them.
- **Must. A reader panic never ends the session.** The save loop recovers from a panic in any reader or in the stitcher, logs it, and falls back to plain passthrough for the rest of the session. Test: a reader that panics on a crafted screen leaves the fake agent running and typing still works. If unsent itself dies, the pseudo-terminal closes and the agent gets SIGHUP; nothing can prevent that, so the passthrough path gets a test that it cannot panic.
- **Must. Agents without synchronized output.** For these profiles unsent reads only after output has been quiet for a gap. The gap is measured per profile (profile done definition); 30 to 50 ms is a starting guess [guess, generic-reader §3.A]. The agents:
  - Gemini on the main screen, Copilot 0.0.422 inline, aider and Letta [measured];
  - Copilot 1.0.88 in tmux, which sent zero `?2026` marks; under a terminal that answers its probe it probably does send them [measured in tmux, guess otherwise, copilot §3];
  - cursor-agent, whose main render path has no marks [source, cursor-agent §1];
  - goose, whose full refreshes are synchronized but whose many small edits arrive outside any pair [measured, goose §8];
  - droid and Qwen when `CI` is set in the environment [source, droid; qwen].
- **Must. CI under 5 minutes.** Today it takes about 10.7 minutes because `-race` and `-coverprofile` run together and slow the fuzz about 8x [measured, codebase-audit §2]. Splitting them takes the local run from 275 s plus 58 s to 33 s plus 88 s [measured, codebase-audit §2]; about 4 minutes per CI job is an extrapolation [guess, codebase-audit §2].

### 2.12 Shell command lines

The shell prompt is in scope, not optional (decision 5). A command typed at the prompt but not yet run is saved and recoverable like an agent draft. zsh comes first, because it is the maintainer's shell; bash and fish come after.

- **Must. Integrate, never wrap.** unsent does not run the shell in its pseudo-terminal. A wrapped shell would put unsent in front of every byte of every program the shell runs, `unsent claude` included, and the line would have to be told apart from the prompt, the right prompt, suggestions and completion menus. The line editor's own buffer is exact [guess, other-textboxes §2 "Why not wrap a shell"].
- **Must. zsh hooks**, written by `unsent setup zsh` into the static block (2.6) and printed by `unsent init zsh`:
  - `line-pre-redraw` appends `$PREBUFFER$BUFFER` to a per-shell log before each redraw. The text is exact, including the earlier lines of an open quote or heredoc [measured, other-textboxes §2].
  - `line-init` marks each new prompt. It is the only way to see a Ctrl+C: `line-pre-redraw` does not fire for the fresh prompt [measured, other-textboxes §2].
  - `line-finish` marks a send. It fires when Enter runs the line and not on Ctrl+C [measured, other-textboxes §1].
  - `TRAPHUP` writes a last record when the window closes; it can read `$BUFFER` [measured, other-textboxes §2].
- **Must. Builtins only, one log per shell.** Each record is a `syswrite` append to a descriptor opened once with `sysopen -a -o create,sync,cloexec`, about 0.03 ms per save. A fork per keystroke costs about 150 ms and is out [measured, other-textboxes §2]. Without `cloexec` every command the shell runs inherits the descriptor [docs, other-textboxes §2]. The log name carries the shell's pid, so parallel shells never share one [guess, other-textboxes §2], and the host name, because a home folder shared between machines or bind-mounted into a container puts pids from separate pid spaces in one folder [design]. It lives in the state folder, mode `0600`. A write that fails, such as on a full disk, moves the log aside at once, so a torn record is only ever the end of a log nothing writes to, and the next prompt opens a fresh one [design].
- **Must. Import into the same store.** Every `unsent` command first imports the shell logs. Records follow the rule agent saves follow: a cleared line, or one replaced wholesale, goes to history. The replacement test holds at any length, because most command lines are shorter than the 24 bytes below which `keepOld` takes any text for an edit [design]. zsh's own Up and Ctrl+R put a history entry in the buffer and set `$HISTNO` to it; while `$BUFFER` still equals `${history[$HISTNO]}`, the hook saves nothing, since the shell keeps that line, and a copy of every entry browsed past would fill the 500-line cap [measured]. The line typed before the recall stays the last one saved, as zsh keeps it too, and if Enter runs the recalled line, the typed one goes to history. With atuin, Up and Ctrl+R write the buffer directly, so a recall looks like a replacement and is covered [source, other-textboxes §2]. A sent line follows the on-send setting (2.13). The last line of a shell whose pid is gone becomes an orphan; `unsent list` shows it, and the notice of an agent started in that folder names it. `unsent list`, `show`, `restore` and `forget` then treat shell lines like agent drafts. A shell is one session from its first prompt to its end, across the logs the hook moves aside. The importer deletes a log only when nothing writes to it any more: a log the hook has moved aside, or the log of a shell on the importer's own host whose pid is gone. A log named for another host is left to that host's importer, unless it has not changed for 30 days [design]. A live shell's current log is read to its end and left in place, and the importer keeps the offset it reached so no record is imported twice; when that read finds nothing new, it writes nothing [design]. The importer removes a log it has read before it goes on to the next, and reads the log its state names first and the rest by stamp, so a log opened after the clock stepped back is read, never deleted unread [design]. If a live shell's log is gone at a prompt, the hook opens a fresh one. That covers an importer that took a live shell for dead, such as one in a container with its own pid space that shares the home folder and the host name: it costs at most the line in progress, which that importer kept as an orphan [design]. When a live shell's log passes 256 KB, the hook moves it aside with the builtin `mv` at the next prompt and opens a new one, so nothing is lost before import [design].
- **Must. Privacy rules** (2.7):
  - Save only at the shell's own prompt and its continuation lines: the hook writes only when the line editor's `$CONTEXT` is `start` or `cont`, never `vared` or `select` [docs, zsh line editor]. `sudo`, `ssh` and `read -s` read a password from the terminal themselves with echo off and never run the line editor, so the hooks never see it [guess; the rig checks it with a positive control, 5.2].
  - A line that starts with a space is never saved, whether or not the shell's own `HIST_IGNORE_SPACE` is set [design]. The hook writes nothing while the buffer starts with a space. If a space is added at the start later, the hook appends a forget mark and the importer drops that line. It drops only that line: lines cleared or replaced earlier at the same prompt stay in history. When the ignored text does not hold the first half of the last line saved, it replaced that line rather than editing it, as a recall of a line with a leading space does, so the hook marks the forget `new` and the importer keeps the replaced line as history [design].
  - A line that matches zsh's `HISTORY_IGNORE` pattern is dropped the same way [docs, zsh parameters; design]. The pattern lives in the shell, not in unsent's environment, so the hook matches it with zsh's own matcher and the user's `extended_glob`: it writes nothing while the line matches, and a forget mark when the line comes to match, which the importer drops. A pattern zsh cannot parse matches nothing.
  - Until import, the raw per-shell log holds every change, including lines later sent or opted out. That is the price of builtins only. The log is `0600` and the importer deletes it.
- **Must. Limits, stated.** zsh skips a redraw when more input is already pending, so a shell that dies in that gap loses the last keystrokes [measured (lane capture), other-textboxes §2]. `O_SYNC` is not a full flush on macOS, so a power cut can lose the last record or two [docs, other-textboxes §2]. A local hook covers local shells only; a remote shell needs unsent installed on that box [guess, other-textboxes §2]. The hooks write only into a folder the shell's user owns, so a root shell that reads the user's rc file, as `sudo -s` does on macOS where it keeps `HOME`, saves nothing [design]. A shell with no `unsent` on its `PATH` at a prompt saves nothing either, as the wrappers fall back then, so `brew uninstall unsent` before `setup --undo` leaves no log that nothing will import [design]. A shell keeps the host name it started with, so after the machine's name changes the importer treats that shell's logs as another host's, and a dead shell's line shows only once its log has not changed for 30 days [design]. A list-form HUP trap in the rc file (`trap '...' HUP`, or an empty one that ignores HUP) keeps its say, because a `TRAPHUP` function would replace it; that shell gets no last record when the window closes, and its last redraw's record stands [measured].
- **Should. Offer the line back at the next prompt.** This is the shell's restore-in-box. A new zsh in the same folder finds a dead shell's unsent line with builtins only (a glob and `kill -0`), claims it with the builtin `mv` as in 2.3, and puts it in the buffer with `print -z`, which never runs it [docs, zsh builtins; design]. It fires once per orphan, and never over a line the user has started typing.
- **Should. fish, after zsh.** fish has no per-keystroke event, but a `--on-signal SIGHUP` handler can read `(commandline)`, so fish gets a save when the window closes. A crash, `kill -9` or a power cut still loses the line [measured, other-textboxes §1 and §2]. The leading-space rule applies.
- **Should. bash, after zsh, once a way in is measured.** bash runs no trap while readline reads, and `bind -x` sees `READLINE_LINE` only on bound keys [measured + docs, other-textboxes §2]. That makes bash hard, not impossible. The candidates are ble.sh, a line editor for bash with its own hooks, or binding every printable key to a save [guess, other-textboxes §2]; a measurement lane picks one before any code. The leading-space rule and `HISTIGNORE` apply. Until bash ships, `unsent status` says its command line is not saved.
- **Out.** REPLs and editors: vim and pico already keep their own recovery files [measured, other-textboxes §1].

### 2.13 Sent drafts and the sent log

- **Must. Detect a send, per profile.** Both a send and a clear empty the box; the key seen on input tells them apart. A draft counts as sent only when all of these hold [design]:
  1. the last good draft was not empty after trimming;
  2. a submit key of the profile was the last key seen on input before the box read empty, and no clear key (Ctrl+C, Esc Esc, the profile's whole-draft keys) came after it;
  3. the next read shows the box empty, or holding only text typed after the submit key.

  Anything else counts as cleared, and the draft goes to history as today: an unknown key form, a submit key and a clear key between the same two reads, a profile with no submit keys. A missed send costs one history entry. A clear mistaken for a send under `delete` would lose text, so doubt always resolves to "cleared". For shells the send is `line-finish` (2.12), not a key.
- **Must. Log the text the agent got.** The 0.4 s tick can miss the last keys before a quick Enter. When the input side sees a submit key it sets a flag, and the output side reads the box from the last complete frame before it feeds the agent's next output into the shadow screen. The read then shows the box as the agent last drew it before reacting to the key [design]. Keys the agent had not drawn yet can still be missing; the rig measures how often (5.2). Pastes are expanded the same way as in drafts, and when a placeholder cannot be matched the raw paste text is kept next to the message, as drafts keep it today.
- **Must. Submit keys**, one profile field each, from the research:

  | Agent | Submit keys | Tag |
  |---|---|---|
  | Claude Code | Enter; Ctrl+Enter and Ctrl+X Ctrl+S (send now); Ctrl+X Enter (queue) | source, claude §4 |
  | Codex | Enter; Tab queues, and sends when idle | source, codex §5 |
  | Gemini CLI | Enter, only when the trimmed text is not empty | source, gemini §5 |
  | Copilot CLI | Enter; Ctrl+Q (queue); Ctrl+D with the cursor at the end; Ctrl+Enter under kitty; Esc then Enter | source, copilot §8; Esc then Enter measured |
  | opencode | Enter | source, opencode §7 |
  | pi | Enter; Alt+Enter when idle; Esc then Enter in one read | measured, pi "Keys as they reached pi" |
  | droid | Enter; Ctrl+Enter; Esc then Enter | source, droid Keys; Esc then Enter measured |
  | aider | Enter, Ctrl+J, Ctrl+O | source, aider §5 |
  | Qwen Code | Enter, except while a completion list is open | source, qwen Keys |
  | Kiro | Enter, including `\` then Enter; Enter while the agent works queues | source, kiro Keys |
  | Amp | Enter by default; `amp.keymap` can remap it | source + docs, amp Keys |
  | goose | Enter, Ctrl+M | source, goose §7 |
  | cursor-agent | Enter; a burst of fast keys ending in Enter on an empty box | source, cursor-agent §7 |

  The other agents have no submit keys in their research yet; until a profile lists them, their emptied boxes go to history. Three traps:
  - A queued message (Codex Tab, Claude Code's queue key, Copilot Ctrl+Q, Kiro and pi while the agent works) empties the box and counts as sent. pi's Esc while streaming puts queued messages back into the box [source, pi Keys], so the log can hold a message the agent never ran.
  - Claude Code, Codex, opencode and Amp let the user remap keys [source or docs, each profile's keys section]. A profile's list is the default, not a fact about the user's setup; a remapped submit key reads as a clear, which is the safe side.
  - Plain Enter stays `CR` under kitty flags 1, 5 and 7, the pushes seen in captures; only flag 8 turns it into a CSI form [measured pushes, 2.8; docs, kitty spec]. Ctrl+Enter and Alt+Enter need the decoder of step 10, so before it they read as clears.
- **Must. The on-send setting.** Two values:
  - `log`: the sent text is appended to that session's sent log. Default for agents.
  - `delete`: the draft is removed and unsent keeps nothing. Default for shells, because the shell's own history already keeps the lines it ran.

  `UNSENT_ON_SEND=log|delete` replaces both defaults. `UNSENT_ON_SEND_<NAME>` sets one agent or shell (`UNSENT_ON_SEND_CLAUDE=delete`, `UNSENT_ON_SEND_ZSH=log`) and wins over the global value [design]. These are environment variables, like `UNSENT_HOME` and `UNSENT_OFF`, so no config file is added; the setup block can export them. In both modes the session's safety versions are dropped on a send, as they are today [source, [`store.go:187`](../store.go#L187) `dropVersions`].
- **Must. A sent log per session**, for every agent whose profile has submit keys, and for a shell set to `log`. One file per session in `sent/` in the state folder, JSON lines [design]:
  - a header: format version, agent, folder (the resolved real path), session start, and the agent's own session id once the profile has read it, as `agent_session`, the same field the draft record carries (2.3) [design];
  - then one line per message, in order: timestamp and text, pastes expanded.

  Each append is flushed with `fsync`; sends are rare, so the cost does not matter [guess]. For a shell, the session is the shell process.

  When the profile reads the agent's session id, the log is kept per conversation: a resumed conversation adds to the log it already has, found by agent and `agent_session`, so `unsent log` shows everything sent in it across every run [design]. The header's session start stays the first run's; each run adds a line saying when it resumed [design]. Without an agent session id, the log is per unsent run, as before.
- **Must. The agent's session id, where a profile can read it.** Claude Code's profile reads `sessionId` from `sessions/<pid>.json` of the running process, the reader restore uses (2.3) [measured, claude §10]. It is the id `history.jsonl` and the transcript carry [measured, claude §10], and unlike the newest `history.jsonl` entry it cannot belong to a parallel session in another terminal, because the file is named by the pid [guess]. Codex's profile reads the thread lock its process holds, the reader restore uses (2.3) [measured, codex §11]. The other profiles read, after a send, the newest entry of the agent's own prompt history and keep only the id field: `conversationId` in agy's `history.jsonl` [measured format, agy]; the `<id>` in Kiro's `sessions/cli/<id>.history` [source, kiro §Draft persistence]. The profile honours the agent's own config-folder variables (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`). Other profiles record no agent session id.
- **Must. `unsent log`** lists sessions that have a sent log, newest first: agent, folder, start time, message count, and the first line of the last message. `--agent <name>` and `--here` filter it. **`unsent log <session>`** prints that session's messages in order, numbered, with times. `<session>` is the number from the list, unsent's session id, or the agent's own session id. `--copy N` puts message N on the clipboard, with the same rules as `unsent restore` (print when there is no clipboard, `clip.exe` on WSL).
- **Must. Retention.** The sent log is a convenience copy of text the agent already holds, so unlike orphans it expires [design]:
  - a session log over 5 MB drops its oldest messages first and records how many at the top;
  - logs older than 90 days are deleted, and at most 2,000 session logs are kept, oldest deleted first;
  - `unsent forget --log <session>` deletes one at once. Here `<session>` is unsent's session id, which `unsent log` shows on every row, never the list number: a session that sends its first message in between shifts the numbers, and the deletion cannot be undone [design].
- **Must. Off.** `UNSENT_ON_SEND=delete` keeps no text recognized as sent for any agent; the per-name variable does it for one. Turning it off does not delete existing logs; `unsent forget --log` does.

### 2.14 Distribution

- **Must.** A Homebrew formula in `GeiserX/homebrew-unsent`, plus the existing `go install` and release tarballs. Formula binaries are not quarantined, so no Apple signing is needed [measured, distribution §2].
- **Should.** `.deb` and `.rpm` from goreleaser. homebrew-core once the repo is 30 days old, from about 2026-10-25, and has enough stars [docs, distribution §2].
- **Not now.** npm, curl-pipe-sh, a Homebrew cask and notarization. Casks are quarantined, so they would need notarization first [measured, distribution §2].
- Pushing to the tap needs a token that can write to that repo; the workflow's own token cannot [docs].

### 2.15 Command surface

One list, so the plan has one source. `UNSENT_NOTICE=0` (2.4) joins `UNSENT_OFF`, `UNSENT_ON_SEND` and `UNSENT_AGENT` as an environment switch.

| Command | What it does | Status |
|---|---|---|
| `unsent <agent> [args]` | Run the agent under unsent | Today; gains `--as <name>` |
| `unsent list` | Drafts and shell lines: agent or shell, folder, time, size, first line; `--agent`, `--here`, `--all` | Today; filters are Should |
| `unsent list --json` | The same list as a stable JSON array (2.4) | Must, v0.5 |
| `unsent show <n>` | Print one draft | Today |
| `unsent show <n> --json` | One draft as a stable JSON object with its text (2.4) | Must, v0.5 |
| `unsent restore [n]` | Copy a draft to the clipboard, or print it | Today; `--into` is not in this version (decision 10) |
| `unsent forget <n>`, `--log <session>` | Delete one draft or version, or one session's sent log | Must |
| `unsent log` | Sessions with a sent log, newest first; `--agent`, `--here` | Must |
| `unsent log <session>` | That session's sent messages in order; `--copy N` | Must |
| `unsent setup [shell]`, `--undo` | Write or remove the rc block, zsh command-line hooks included | Must |
| `unsent status` | What is protected in this shell, what bypasses it, the on-send setting | Must |
| `unsent capture <agent>` | Record a fixture bundle | Should |
| `unsent init zsh` | Print the zsh command-line hooks that setup writes | Must |
| `unsent hook claude` | Print a Claude Code `SessionStart` hook config that notes a resumed session's draft (2.4) | Should, v0.5.1 |
| `unsent version` | Version | Today |

## 3. Support matrix

**Evidence** means how the box itself is known. **measured** = the box was seen on a real screen or replayed from a byte capture. **source** = read in code or a bundle only. **docs** = vendor docs only. Versions are the ones profiled.

| Agent, version | Evidence | What makes the box readable | Main risk |
|---|---|---|---|
| Claude Code 2.1.282 | measured; reader ships today | Two full-width `─` rules, first row `❯` + no-break space, 2-column indent, wrap `cols-4`, cap `rows/2-5` [claude §1, §5] | Kitty push contested at the main prompt; the box scrolls one row at a time for Up and Down, and no key tried made it jump [measured, claude §5]; `[Image #N]` and `[...Truncated text #N +M lines...]` placeholders unknown to unsent; a box over 10,000 characters collapses its middle even for typed text [source, claude §3] |
| Codex 0.151.0 | measured + source | Bold, not dim, `›` at column 0; dim hint when empty; wrap `cols-3`; cap `rows-4`; scrolls one row at a time [codex §1, §3] | Kitty push measured in tmux; `>7u` with release events on other terminals is source and docs only [codex §5]; an extra empty row after a line that fills the width; history messages also start with `›`, told apart only by dim [codex §3] |
| Gemini CLI 0.61.0 | measured + source | Half-block `▄`/`▀` frame, ` > ` marker at column 1, text from column 3, wrap `cols-6`, fixed cap of 10 rows [gemini §2, §3] | Empty hint is a colour, not dim; five frame variants; no sync marks on the main screen; weekly releases with 66 prompt commits this year [gemini §2, §9] |
| Copilot CLI 0.0.422 and 1.0.88; only 1.x is supported (decision 6) | measured, both | `─` rules with `❯ ` + ASCII space; 1.0.88 can draw a "rail" frame instead [copilot §1] | Two versions draw different boxes; 0.0.422 shows tabs as 4 spaces and deletes box-drawing characters on screen (1.0.88 unchecked); editor copy keeps placeholders; no sync marks in tmux on either; Esc+Enter submits; it writes to the real `~/.copilot` even with `--config-dir` [copilot §3, §5, §8, §10, §14] |
| cursor-agent 2026.07.20 | **source only**; login gate measured | Background-coloured bar, glyph `→` dim when empty, fake inverse cursor, fixed cap of 6 rows, wrap `cols-7` [cursor-agent §3, §5] | Nothing about the box seen live; needs background and reverse-video per cell; no sync marks; `ESC[2J ESC[3J` clears wipe scrollback mid-session [cursor-agent §1] |
| opencode 1.18.32 | measured + source | Left `┃` bar on a filled panel, meta row `Build · …` below, `╹▀▀▀` edge; alternate screen; every frame synchronized [opencode §3] | Needs per-cell foreground and background; transparent themes remove the fill; plugins can replace the prompt; paste tokens have no index; the name `opencode` also belongs to an archived Go project [opencode §1, §6, §11] |
| pi 0.87.1 | measured + source | Two `─` rules, no glyph, fake reverse-video cursor, exact ` ↑ N more ` counts on the rules, cap `max(5, floor(rows*0.3))` [pi §What the box looks like] | Alt+Enter submits; paste ids renumber when one is deleted; Ctrl+V and right-click pastes never cross the pseudo-terminal; kitty flags 7 pushed; editor round trip resets the paste map [pi §Pastes, §Keys, §External editor] |
| agy 1.2.13 | measured; reader ships | Two full-width `─` rules, `> ` ASCII glyph in a colour of its own, 2-column indent, wrap `cols - 3`, cap `floor(rows/2)`, exact `↑ N more lines` counts in the hint colour [agy §The input box, §Profile run] | Closed source, a release about every 2 days, and auto-update runs unless the network is blocked; hint and draft are told apart by colour alone; no synchronized-output marks, so frames are the cursor's hide and show plus a quiet gap; combining marks are never drawn, so a decomposed accent cannot be read off the screen [agy §Risks, §Profile run] |
| aider 0.86.2 | measured + source | `> ` prefix repeated on every row, including wrapped rows [aider §2] | Completion menu overwrites draft rows with plausible text; character wrap makes line breaks ambiguous; a wide character at the edge leaves a row one cell short; Ctrl+J and Ctrl+O submit [aider §2, §5, §7] |
| Amp 0.0.1790424028 | measured against a stub server + source | Full-width rounded box `╭╮╰╯`, text in columns 2 to `W-3`, one reverse-video cursor cell [amp §What the box looks like] | About 8 builds a day; no cursor cell when the terminal loses focus; pastes always inline, so a big paste scrolls the draft out of view at once [amp §Risks] |
| Crush 0.96.1 | measured + source | `  > ` then `::: ` prompts in columns 1 to 4, text from column 5, cap 15 rows [crush §3, §4] | Soft wraps and typed newlines both show `:::`; big pastes become attachments outside the box; hint is a colour; editor round trip trims whitespace [crush §6, §9, §10] |
| Qwen Code 0.24.6 | measured by replay + source | `─` rules with `> `, fixed cap of 10, wrap `floor(cols*0.9)-6` [qwen §What the box looks like] | Hint is a colour; the top rule gains a session title; 12 releases in 23 days; box vanishes during dialogs; editor copy keeps placeholders [qwen §What would break, Keys] |
| goose 1.52.0 | measured + source | `> ` at column 0 under a context-usage bar; `?2004h`/`?2004l` bound the open prompt [goose §3, §8] | No height cap: a draft taller than the screen shows only its tail, and edits above it never appear; many edits arrive outside sync marks; glyph changed twice in 2026 [goose §5, §12] |
| Kiro 2.24.1 | measured + source; closed source | Rule, status row, blank row, then `›` with 2-column indent [kiro §What the box looks like] | No height cap and the view does not follow the cursor; a 120-character unbroken token at 120 columns corrupts the whole frame, while a 118-character token or a long paste only spills the status row and leaves the draft intact [kiro §Wrap and width]; cursor model and key encoding depend on `TMUX` and `TERM_PROGRAM`; `/editor` submits, so no safe ground truth [kiro §Risks] |
| droid 0.228.0 | measured + source | Rounded box, `│ > ` with text from column 4, exact `↑ N more` counts, wrap `cols-10` [droid §What the box looks like] | Esc+Enter and Ctrl+Enter submit; unbracketed fast input also collapses into placeholders unsent never saw; a real draft is dim when the box loses focus; no sync marks when `CI` is set [droid §Pastes, §Cursor, TUI stack] |
| Kimi Code 2.1.1 | measured + source | pi's editor inside `╭╮ │ ╰╯` borders, exact `↑ N more` counts [others: Kimi] | New TypeScript rewrite; corners change to `├┤` while a side panel is open; editor key never exercised |
| Mistral Vibe 2.25.8 | measured + source | `─` rules, bold `>`, cap `50vh` [others: Vibe] | Pastes of paths rewritten to `@path`; Ctrl+C quits at once with no app server |
| Cline 3.0.65 | measured + source | `─` rules, `❯`, fixed cap of 5 [others: Cline] | Placeholder text also appears as a heading outside the box; chat-view width unmeasured |
| Kilo 7.8.1 | measured box look; reader reuse is a guess | Same as opencode, plus a model line inside the panel [others: Kilo] | Shares all of opencode's risks |
| Letta 0.33.2 | measured + source | `─` rules, `› ` [others: Letta] | No height cap, no sync marks, inline pastes show newlines as `↵` |
| OpenHands 1.16.0 | measured + source | Rounded box, text at column 3 [others: OpenHands] | Two input modes in two different widgets; 40 to 90 s start-up |
| Grok Build 1.0.41 | docs only; login wall | Unknown | Box never reached |
| Auggie 0.36.0 | source; login wall | Unknown | Box never reached |
| CodeBuddy 2.158.0 | box: never seen; welcome box and login menu measured | Probably Claude Code's layout [guess, others ranking] | Box never reached; paste token `[Pasted text #N: M lines]`, with a colon [source] |
| Continue `cn` | not tried | Unknown | Never run [others ranking] |

Readers for agents marked "source only" or worse are not written until a real capture exists.

## 4. Design changes to the code, smallest first

Each step is one PR with `go test -race ./...` green. Steps 0 to 5 change no behaviour a user can see. Section 7 places every step in a release.

0. **Save the evidence first.** The research captures live in `/tmp` and will vanish; `/tmp/unsent-other` alone is 4.1 GB on the MacBook's internal disk and is the only copy of its captures [measured, others: side effects]. Move all capture folders to `/Volumes/Data` on a box of the fleet before any other work. Nothing is deleted from `/tmp` without the maintainer's yes.
1. **CI split.** Step A runs `go test -race -skip Fuzz -coverprofile=coverage-race.out ./...`; step B runs `UNSENT_FUZZ_SEEDS=50 go test -run Fuzz -coverprofile=coverage-fuzz.out ./...`. Each test runs once, and both profiles are uploaded as the coverage. The wrapper tests wait for the save that reads each key's answer instead of a fixed 1.2 s. About 66 s and 68 s on a Mac mini, down from 275 s plus 58 s [measured, codebase-audit §2; Mac mini, 2026-09-27]. Since 0.7.1 the race step measures no coverage at all: a coverage counter is an atomic add, the race detector instruments every one of them, and together they cost six times the CPU, 292 s against 199 s on a Mac mini [measured, Mac mini, 2026-09-29]. A third job runs the same tests once more without the detector, and its profile is the one uploaded beside the fuzz's. The fuzz's runs, one per profile and mode (twelve at v0.7.1, four more with agy), are independent, so they run in parallel: 233 s to 82 s on a Mac mini [measured]. That run measures no coverage either, and for the same reason: the counters belong to the code under test, so running it on several cores makes those cores write the same counters. The parallel fuzz costs 367 s of CPU with `-coverprofile` against 232 s without, and the Linux job that measured it took 9m27s against 4m01s on macOS, which does not [measured, CI and Mac mini, 2026-09-29]. Coverage is free in a sequential run, so the cover job is the only one that measures anything.
2. **Honest fuzz.** Fix the simulator hang on blank lines, add blank-line edits, and add a byte-exact counter with its own floor. With blank-line edits the byte-exact rate is about 60% (60.18% across modes), so the floor starts at 0.60, not at the 61% measured without blank lines [measured, codebase-audit §2]. As built, there are two counters: byte-exact allows a line break joined into a space at a full row and sits at 0.86 to 0.88 by mode (0.84 with repeated words); identical allows nothing and sits at 0.55 to 0.56 [measured, `TestStitchFuzz*` at 150 seeds]. Each has its own floor at those values. Prove it can fail with a mutation that drops `\n` in `unwrap`. Blank lines also push the repeated-words rate to 96.27%, below its 0.97 floor [measured, codebase-audit §2]. Decision: re-tune that floor to the measured value with blank lines in this step, so it can only go up. Step 15 may raise it later; its gain inside the fuzz may be near zero [guess, codebase-audit §5 step 7].
3. **Copy the evidence into the repo.** From the saved captures, copy the ones tests replay into `testdata/<agent>/<version>/`: raw byte streams, screen captures and editor copies. Budget: under 20 MB per agent, trimmed or compressed [design]. They hold dummy keys only; check each before committing.
4. **Record format version.** Add `format` to `record`, with a test that an old file still loads and is never treated as empty (section 2.5).
5. **Profile extraction.** Move everything Claude-specific into one `profile` value, today's behaviour unchanged [codebase-audit §5 step 3]:

   ```go
   type profile struct {
       name     string
       names    []string          // command base names
       nonChat  [][]string        // subcommands and flags where restore never fires
       confirm  func(*screen) bool // screen check for shared names
       read     func(*screen, *readState) (view, bool)
       unwrap   unwrapRule        // word wrap, space kept or swallowed; char wrap
       scroll   scrollModel       // jump, one row, or no cap with a visible tail
       pastes   []pasteRule       // placeholder regex plus pairing rule
       keys     keyset            // before, after, any amount, whole draft, restore, newline, submit
       ctrlZ    suspendPolicy     // first press, second press, pass through
       restore  restoreCaps       // settle delay, empty reads, paste newline, lossy transforms
       editor   editorCaps        // key, and whether the copy keeps placeholders
       session  sessionSource     // how the running process's session id is read (Claude: sessions/<pid>.json)
       noSync   bool              // read after a quiet gap instead of after ?2026l
       quietGap time.Duration
   }
   ```

   `view` stays the reader's output and gains two optional fields: exact hidden-row counts above and below, for agents that print them (pi, agy, droid, Kimi), and a shell-mode prefix.
6. **Agent and folder identity.** `Agent` and `agent_session` in `record` (step 7 writes the same `agent_session` field into the sent log's header; it stays empty in both until the session reader lands with step 13), falling back to the base name of `Command[0]` for old files; `UNSENT_AGENT` and `--as`; folder as the resolved real path; notice, `restore` and restore-in-box filter by folder and agent.
7. **Sent detection, the on-send setting and the sent log** (2.13). Early, because it reuses today's archive path: where `keepOld` sends an emptied box to history ([`wrap.go:350`](../wrap.go#L350)), the save loop now asks one more question, sent or cleared, and answers it from the submit and clear keys seen on input since the last save. The input side already scans every read for delete keys (`deleteLog`); the submit keys join that scan. Needs step 6 for the log header and the `submit` list in step 5's keyset. No new reader. Starts with Claude Code's Enter; other key forms come with step 10 and other agents with their profiles. Adds `UNSENT_ON_SEND`, the `sent/` folder with its retention, `unsent log`, `unsent log <session>` and `unsent forget --log`.
8. **Safety of unsent itself.** Panic recovery in the save loop with fallback to passthrough, `UNSENT_OFF=1`, the "saving stopped" and "not protected" lines on exit (2.1, 2.6, 2.11).
9. **Raw input and output logging** under `UNSENT_DEBUG_DIR`, then `unsent capture` on top of it (2.1).
10. **Key decoder** (section 2.8) and per-profile keysets and Ctrl+Z policy. Only after the Warp and Ghostty measurement.
11. **Notice after exit.** A few lines in `wrap()`.
12. **Richer snapshot.** Per cell: foreground, background, reverse video, bold, as well as today's dim flag. Also the alternate-screen flag and the bracketed-paste state. The emulator cells already hold all of this; [`screen.go`](../screen.go) drops it [source, cursor-agent §4]. Without this step, readers for Gemini, opencode, Qwen, Crush, agy, Kiro, Copilot 0.0.422, cursor-agent, Amp, pi and droid cannot tell a hint from a draft or find the cursor [each profile's detection recipe]. Built with the first reader that needs it.
13. **Restore-in-box per session** with every condition in section 2.3, the session reader, `unsent list --json`, `unsent show --json` and `UNSENT_NOTICE`. Tests: a draft containing `ESC[201~\r` never submits; an unverified restore leaves the orphan; a keypress after the box appears and before it settles cancels the restore; a focus report does not cancel it; keys pressed in a resume picker do not cancel it; two processes opening one session inject once; a captured trust dialog never gets a restore; `claude --resume <id>`, `claude -c`, a picker choice and `/resume` of the draft's session each get the restore; a new chat, `/clear` and `--fork-session` in the same folder get the notice and no paste; `claude "prompt"` never gets a restore; an orphan from another agent is never offered; the `--json` output matches a golden file; `UNSENT_NOTICE=0` prints no notice and still restores. Mutation: a session match that is always true must turn the new-chat test red. The session reader is tested on fixture `sessions/` folders copied from the 2026-09-28 run [claude §10].
14. **Shell command lines** (2.12). zsh first: the hook code that `unsent init zsh` prints and `unsent setup zsh` writes, the importer that feeds each shell log through `keepOld` into the store, the separate history cap, the privacy rules, `line-finish` wired to the on-send setting of step 7, and the offer at the next prompt. Needs setup (v0.3). Then fish, then bash once its measurement lane has picked a way in.
15. **Line-break memory.** When `unwrap` has to guess at a full row and the known text already has a `\n` between the same two words, keep the `\n`. The gain is a guess; the identical counter from step 2 measures it. The byte-exact counter cannot, because it already allows a break joined at a full row.
16. **Paste rules per profile.** A placeholder regex plus how to pair it with a captured paste: by number (Claude, agy), by number that renumbers on delete (pi, Kimi), by order and line count because there is no number (opencode, Kiro), by the first 50 characters (droid), or not at all because pastes stay inline (Amp, goose, aider). Each rule uses the agent's own counting: UTF-16 length for Gemini, Kiro and droid, line breaks rather than lines for Claude, a trailing newline counted as a line for Gemini, Qwen and Kiro.
17. **Unwrap and scroll models.** Word wrap with the space swallowed (Claude, Codex, Amp); word wrap with the space at the row end when it fits (Gemini, Crush); droid keeps the space at the row end if it fits, else starts the next row with it, so rows are joined verbatim from untrimmed cells [measured + guess, droid wrapping]; character wrap (aider, goose). Scroll by jump, by one row, or no cap where only the tail is visible (goose, Kiro, Letta). Claude's scroll model is settled first by a per-step capture (section 7, v0.2). The fuzz simulator takes these from the profile so it runs once per profile.
18. **Tier 1 profiles**, one PR each, each meeting the profile done definition (2.1).
19. **Generic reader.** Deferred: not in this version (decision 4).

## 5. How "works with agent X" is proven

A claim that unsent works with an agent names a version and a date, and rests on three things.

### 5.1 CI replays real captures

- Every profile has fixtures from real runs of that agent version: raw output bytes, and the keystrokes that produced them. CI replays the bytes through unsent's own pipeline (emulator, reader, stitcher, paste tracker) and asserts the exact draft at each step. This is new: the Amp, Kiro, Qwen and generic-reader verification runs replayed captures through `charmbracelet/x/vt` and dumped or read frames [measured, amp, kiro, qwen and generic-reader verification notes], but only the generic prototype ran a reader and the stitcher, and none asserted drafts from unsent's real pipeline.
- The fake agent in [`wrap_test.go`](../wrap_test.go) becomes a replayer of recorded frames, chosen by `UNSENT_FAKE_AGENT=<profile>`, instead of a hand-drawn imitation of Claude Code.
- Negative fixtures for every profile: the empty box, trust and login dialogs, menus, history browsing. None may yield a draft.
- Fixtures include accented text, emoji and a wide character at the wrap edge (2.1).
- **A check that cannot fail is not a check.** Every profile's tests must go red when its reader is mutated: marker, indent, wrap width, and the fallback branches. The generic prototype's marker mutation left aider and opencode green [measured, generic-reader §3.A]; that must not happen again.
- **Sent log and on-send setting** (2.13), replayed from a rig run that submits into a dummy key (5.2):
  - Enter on a non-empty box puts the exact text in the sent log, pastes expanded, and nothing in history.
  - Ctrl+C and Esc Esc put the draft in history and never in the sent log.
  - A submit key and Ctrl+C between the same two reads put the draft in history.
  - A key typed just before Enter, inside one tick, is in the logged text.
  - Under `delete`, nothing of the message remains in `sent/`, `history/` or `drafts/`.
  - A profile with no submit keys never writes a sent log.
  - `UNSENT_ON_SEND_CLAUDE` wins over `UNSENT_ON_SEND`; files are `0600`; retention trims at 5 MB, 90 days and 2,000 logs; `unsent log <session> --copy N` copies message N.
  - Mutation: a submit match that is always true must turn the Ctrl+C test red.
- **Shell command lines** (2.12), on a real zsh with only the hooks sourced (`zsh -f`), driven through a pseudo-terminal; CI installs zsh where the runner lacks it:
  - a line typed then SIGHUP, and one typed then `kill -9`, both show in `unsent list` (the `kill -9` case up to the last redraw);
  - Ctrl+C puts the line in shell history; Enter keeps nothing by default, and puts the line in the sent log under `UNSENT_ON_SEND_ZSH=log`;
  - a line that starts with a space, one that gains a space at the start later, and one matching `HISTORY_IGNORE` never reach the store;
  - text typed at `read -s` and at `vared` never reaches the raw log, and the same text typed at the prompt does (the positive control);
  - a child process does not inherit the log's descriptor;
  - the save cost per keystroke stays under 1 ms [design; about 0.03 ms measured, other-textboxes §2];
  - mutations: removing the `$CONTEXT` check turns the `vared` test red; removing the `line-init` mark turns the Ctrl+C test red.

### 5.2 The real-app test rig

We call this the rig, not "the real-app harness", because the harness means the agent runtime.

It is opt-in and never runs in CI. Per run:

- A private tmux server, `tmux -L unsent-rig-<agent> -f /dev/null`, fixed size, launched under `env -i` with a scratch `HOME`, scratch XDG directories and the agent's own config directory pointed into the scratch space.
- A login-free path per agent, all found during research: dummy API keys for Claude Code, Codex, Gemini, aider, Crush and Kiro; `OPENAI_BASE_URL=http://127.0.0.1:9/v1` for Qwen; a fake `GEMINI_API_KEY` and an unreachable base URL for agy; an unusable provider for opencode; `PI_OFFLINE=1` for pi; `FACTORY_AIRGAP_ENABLED=true` for droid; a local stub server via `AMP_URL` for Amp; `GOOSE_DISABLE_KEYRING=1` and a fake key for goose. cursor-agent has none; the maintainer logs it in once in a scratch config for its capture run (decision 7).
- **Ground truth from the agent's own external-editor key** with `EDITOR` and `VISUAL` set to a copy-and-exit script. Keys: Ctrl+G for Claude Code, Codex, Gemini, Copilot, pi, agy, Amp and Kimi; Ctrl+X e for opencode; Ctrl+X Ctrl+E for aider; Ctrl+O for Crush; Ctrl+X for Qwen; Ctrl+P for droid. What was measured about the copy:
  - Exact draft, pastes expanded, no trailing newline: Codex, agy, droid, aider, opencode, Amp [measured, each profile's external editor section].
  - Claude Code: pastes expanded, byte-identical to the pasted draft behind a placeholder [measured, claude §10]; no trailing newline [measured, claude §3].
  - Gemini: a 1,054-byte copy with pastes expanded; the trailing newline was not recorded [measured, gemini §6].
  - Copilot and Qwen: exact buffer, but paste placeholders stay unexpanded [measured, copilot §10; qwen Keys]. The rig compares their copies against unsent's save with placeholders collapsed back, or on scenarios without collapsed pastes.
  - pi: exact draft, pastes expanded, no trailing newline [measured, pi "Capture run on pi 0.87.1"]. Crush: a byte match backed only by research notes, and its round trip trims whitespace [measured (lane), source, crush §9]. Kimi: never exercised [source, others: Kimi].
  - goose and Kiro have no non-submitting editor key, so their ground truth is the typed input plus the screen [source, goose §10, kiro §Draft persistence].

  Traps: Codex needs `-c sandbox_mode="danger-full-access"` or it writes to the real `~/.codex/editor` [measured, codex §5]; pi's round trip resets its paste map and clears scrollback [measured, pi §External editor].
- Scenarios per agent: type; multi-line draft; accented text; paste below and above the placeholder threshold; draft taller than the box with scrolling and edits out of sight; every delete key; Ctrl+C clear; kill the agent with `kill -9`; close the window with `kill-session`; restore-in-box into the reopened session, and the notice with no paste in a new chat; a trust dialog on start. Each ends with the saved draft compared to ground truth byte for byte. Lost line breaks are reported as a count, never hidden, and this count is the real-world rate section 1 asks for.
- Newline keys per agent, because the wrong one sends the prompt: Esc+Enter submits in Copilot, pi and droid; Ctrl+J and Ctrl+O submit in aider; Ctrl+Enter submits in droid; Ctrl+J toggles the changelog in droid when one is showing [measured or source in each profile]. The rig reads the newline key from the profile.
- **Kitty and modifyOtherKeys runs.** Agents pushed kitty flags in the captures, but tmux does not answer the kitty query, so no capture shows key bytes arriving in CSI-u form [measured, amp §Keys; codex §5]. The rig gets a second driver in Go that plays the outer terminal: it owns the pseudo-terminal in front of unsent, answers `ESC[?u` with flags, and sends keys in legacy, CSI-u or modifyOtherKeys form. Whether that matches a real Ghostty is unmeasured, so the Warp and Ghostty key logs from 2.8 back it up.
- **Send scenario** (2.13). Type a draft with a paste, send it with each submit key the profile lists, then compare the sent log with the agent's own history of submitted prompts, for example Codex's and agy's `history.jsonl` or droid's `history.json`, which also records submits the backend refused [measured, droid]. Claude Code keeps cleared pastes there as placeholders [measured, claude §6] and Codex keeps pastes as placeholders [measured, codex §6], so those compare with placeholders collapsed. The rig reports how many logged messages miss keys typed just before the submit; that is the real rate for the limit in 2.13.
- **Shell scenario** (2.12), in the same private tmux with `zsh -f` and only the hooks sourced: type, Ctrl+C, Enter, a leading space, `kill -9` and `kill-session`. For privacy it types a dummy string at a `read -s` prompt and at a `sudo -k; sudo true` password prompt, then presses Ctrl+C before Enter; neither string may reach the raw log, while the same string typed at the prompt does.
- The rig records typing latency and CPU for the performance targets in 2.11.
- **Cadence.** Support claims go stale within days: Amp ships about 8 builds a day. The rig runs weekly on a Mac mini of the fleet, never the MacBook, against the latest release of every supported agent, and commits the result to the README table [design].

**Hard rules for the rig**, each learned from a side effect during the research:

- Never use the real `HOME`. Copilot wrote to the real `~/.copilot` [measured (lane), copilot §15; later writes by another process were found and not attributed]; Codex created `~/.codex/editor` [measured, codex §9]; Amp wrote `~/.cache/amp/logs` [measured, amp §Risks].
- Never start a shell with `-i` or rc files. During verification, a `zsh -i` put `~/.local/bin` first on `PATH` and launched the real Claude Code twice [measured, distribution verification notes]. Use `env -i` with `zsh -f` or `bash --norc`, and a positive control that no real agent is on the rig's `PATH`.
- Unset `GITHUB_TOKEN`, `GH_TOKEN`, AWS and every provider key. Kilo picked up an inherited `GITHUB_TOKEN` and AWS variables and chose real providers [measured, others: side effects]; Copilot also calls `gh auth token` unless started with `--no-auto-login` [source, copilot §14].
- Unset `CI`, or droid and Qwen stop sending sync marks [source, droid; qwen].
- Never submit to a model. The send scenario submits only through the login-free path above, where the request fails on a dummy key, an unreachable base URL or a stub server. Quit with the agent's own exit key on an empty box, then kill the whole process tree: Gemini survived its pane being killed [measured (no capture), gemini §8].
- Turn auto-update off where it can be turned off, and record the version actually run. agy updated itself to 1.2.11 despite the switch [measured, agy §Version comparison].

### 5.3 What the README says

The support table lists, per agent, the version and date of the last rig run and the evidence level. An agent without a rig run is not called supported. When the installed version is newer than the last verified one and the reader fails, the "saving stopped" line (2.1) names both versions.

## 6. Naming: keep `unsent`

Decided: keep `unsent` (decision 9). The evidence, all from [`research/distribution.md`](research/distribution.md) §1:

- **The command is `unsent` either way.** Agent CLIs name the package `X-cli` and the binary `X`: `@google/gemini-cli` installs `gemini`, the `copilot-cli` cask installs `copilot` [measured]. A suffix would change only the repo and package names.
- **The suffix makes the one real collision worse.** An email product called Unsent ([unsent.dev](https://unsent.dev)) ships SDKs in 8 languages and has no CLI [measured]. `unsent-cli` is the exact name their missing CLI would take [guess].
- **Every channel we would use is free for `unsent`:** Homebrew core and taps, unscoped npm, GitHub, Debian, AUR, nixpkgs [measured]. It is taken on crates.io, PyPI and RubyGems, which a Go binary does not use [measured].
- **Renaming is cheap only now.** 8 release downloads and 1 star so far [measured]. A later rename changes the Go module path, and `go install github.com/GeiserX/unsent@latest` would break for existing users [docs].
- Trademarks were not checked.

## 7. Milestones in shipping order

Each milestone is a release on its own, leaves main working, and closes only on its exit gate.

0. **Now, no release: save the evidence** (step 0). Exit gate: every capture folder the specs cite exists on `/Volumes/Data` on a box, with a file count that matches `/tmp`.
1. **v0.2: foundations, Claude only.** Steps 1 to 6, 8, 9 and 11: CI split, honest fuzz with re-tuned floors, fixtures in the repo, record format version, profile extraction, agent and folder identity, reader panic guard and `UNSENT_OFF`, the "saving stopped" and "not protected" lines, raw byte logging, notice after exit. Claude fixes that need no new machinery: Ctrl+D, `ESC d` and word-sized `ESC 0x7f` as delete keys; the `[Image #N]`, `[Audio #N]` and `[...Truncated text …]` placeholders. A per-step capture of Claude's scroll model, and CLAUDE.md and README corrected to "4+ lines or over 800 characters" for the paste threshold [measured, claude §3]. Visible to users: the notice after exit, drafts filtered by agent, the new exit lines.
   Exit gate: fixtures replay green; a mutated reader goes red; a mutation dropping `\n` in `unwrap` turns the byte-exact floor red; a real Claude Code session on the alternate screen shows the notice after exit; CI under 5 minutes on the run page.
2. **v0.2.1: the sent log for Claude Code** (step 7). It needs no new reader, so it can ship inside v0.2 if it is ready by then. Visible to users: `unsent log`, `unsent log <session>`, `UNSENT_ON_SEND`, and sent drafts no longer in history; the README line that says history keeps sent drafts is corrected.
   Exit gate: the sent-log tests of 5.1 green, the always-true submit mutation red; in a real Claude Code session on a dummy key, Enter puts the message in the sent log and Ctrl+C puts the draft in history.
3. **v0.3: setup and the tap.** `unsent setup`, `--undo` and `unsent status`; the Homebrew tap. These are measured, need no reader work, and are what put unsent in front of every agent.
   Exit gate: setup and undo pass in bash 3.2, bash 5.3 and zsh 5.9, including an rc file with `alias claude=...` before the block; `brew install` from the tap runs on a clean Mac mini.
4. **v0.4: zsh command lines** (step 14), right after setup, because the hooks live in the setup block. v0.4.1 adds fish, with its setup through `conf.d` (2.6). bash follows once its measurement lane has picked a way in (2.12).
   Exit gate: the shell tests of 5.1 green, both mutations red; a zsh killed with SIGHUP and one killed with `kill -9` both leave the typed line in `unsent list`; a line run with Enter leaves nothing under the default; the rig's shell privacy scenario passes.
5. **v0.5: per-session restore-in-box for Claude Code, with `--json` and `UNSENT_NOTICE`** (step 13, every safety condition of 2.3). The draft keyed to its session, the `sessions/<pid>.json` reader, the paste and its line under the box, the notice in a new chat, `unsent list --json`, `unsent show --json` and `UNSENT_NOTICE=0` (2.4). The paste newline and the placeholder are measured on a clean config [claude §10]; the settle delay still needs measuring on the real config first.
   Exit gate: all step 13 tests green and the always-true session mutation red; the rig's restore scenario passes on Claude Code through `--resume <id>`, `-c` and a picker choice, and a new chat in the same folder gets the notice and no paste; the trust dialog negative fixture yields no restore; a restored draft over 800 characters reads back byte-identical through Ctrl+G.
6. **v0.5.1: `unsent hook claude`** (2.4). Claude Code only; other agents get it when their profile finds a hook.
   Exit gate: on a throwaway config, a resumed session with an orphan gets exactly one `hook_additional_context` note naming it, a resumed session without one and a new chat get none, and the hook's output never holds `initialUserMessage`.
7. **v0.6: keys.** The Warp and Ghostty key logs (2.8) first. Then step 10: the key decoder, per-profile keysets, the Ctrl+Z policy. This fixes a real hang today if Ctrl+Z arrives in CSI-u form, and lets Ctrl+Enter and Alt+Enter count as submit keys.
   Exit gate: the recorded Warp and Ghostty key logs replay through the decoder with every delete key, every submit key and Ctrl+Z recognised; in a real Ghostty session Ctrl+Z suspends and `fg` returns to the agent.
8. **Rig track, no release of its own.** The tmux part lands before v0.4, whose shell scenario runs in it, and serves v0.5's restore tests. The Go outer-terminal driver that answers the kitty query lands before v0.7, because Codex pushes the kitty protocol.
9. **v0.7: Codex; v0.7.1 pi; v0.7.2 agy.** Box shapes close to Claude's. Brings step 12 (richer snapshot: pi and agy need reverse video and colour), step 16 (paste rules) and step 17 (unwrap and scroll models), which the first non-Claude agent needs. Codex needs the decoder from v0.6 because it pushes the kitty protocol. Each profile brings its submit keys and the way it reads the running session's id (2.3). Its release measures that id source, because the profile done definition requires it (item 4), and restore-in-box switches on with the release. The README is rewritten here with decision 12's scope.
   Exit gate per release: the profile done definition (2.1), restore and the send scenario included.
10. **v0.8: Gemini; v0.8.1 Copilot 1.x (decision 6); v0.8.2 opencode, with Kilo.** The colour-dependent readers and the quiet-gap read for agents without sync marks.
   Exit gate per release: the profile done definition.
11. **v0.9: cursor-agent**, after the maintainer's one logged-in capture run in a scratch config (decision 7). Exit gate: the profile done definition.
12. **v0.10.x: tier 2**, one agent per release in this order: Amp, droid, Qwen, Crush, Kiro, goose, aider, Kimi, Vibe, Cline. Exit gate per release: the profile done definition.
13. **Later:** step 15 (line-break memory) once the identical counter shows room for it, `.deb`/`.rpm`, homebrew-core, `unsent search`, paste attachments, OSC 52, `unsent restore --into`, tier 3 agents, and the generic reader (step 19, decision 4).

## 8. Decisions

The maintainer's answers to the questions this spec raised, each with what it changes in the build.

1. **Restore-in-box is per session and fires automatically** when the session the draft was typed in is opened again, however it is opened: a resume flag, `-c`, a picker chosen inside the terminal UI, `/resume`, or a launcher. A brand-new chat in the same folder is another conversation and gets the notice, not a paste. So a draft is keyed to (agent, the agent's session id), the session is read from the running process and never from the command line, there is no key or command to trigger a restore, and the safety conditions of 2.3 carry the whole risk (2.3).
2. **No cross-agent drafts:** a draft is only offered back to the agent it came from, and `unsent list` / `unsent restore <n>` reach any draft; so the notice, `restore` and restore-in-box filter by agent as well as folder (step 6).
3. **Agents are not reconfigured to be easier to read** (no environment variables such as `COPILOT_PROMPT_FRAME`), and readers handle the agent as it ships; so each layout variant, such as Copilot's rail frame [source, copilot §1], Kiro's two cursor models [source, kiro §Height] and cursor-agent's two layouts [source, cursor-agent §11], needs its own reader branch and fixtures.
4. **Generic reader: not in this version;** so step 19 stays deferred, and its secret guard waits with it (2.2).
5. **Shell command lines are in scope, not optional:** zsh first, bash and fish after; so step 14 and v0.4 are no longer conditional (2.12).
6. **Copilot: 1.x only,** 0.0.422 is not supported; so there is one Copilot reader, not two, and no fixtures for 0.0.422's inline box [measured, copilot §13].
7. **cursor-agent: the maintainer logs it in for one capture run in a scratch config;** so v0.9 can start as soon as that capture exists.
8. **Launchers that start the agent through `env` or a full path:** setup warns and `unsent status` flags them, and unsent does not edit them; so the build has detection code and no rc or launcher rewriting.
9. **Name: keep `unsent`;** so the tap, the module path and the binary name stay as they are (section 6).
10. **Recovery inside a running session:** today's path (another terminal, `unsent restore`, paste) is enough for now; so no socket and no reserved key chord in this version (2.4).
11. **Restore fires on resume.** Resume is the main way work is reopened. The same conversation comes back later, in a new terminal. A draft left in a session is wanted back in that session, and a new chat is where it does not belong. So each profile lists its resume forms among the starts where restore fires, and says how the running session's id is read (2.3).
12. **README scope: coding agents' input boxes and the shell command line;** so the README rewrite at v0.7 says exactly that, and the shell part is documented from v0.4.
13. **A long restored Claude draft shows as Claude Code's placeholder.** It goes back as one bracketed paste; a draft of 4 or more lines or over 800 characters shows as `[Pasted text #N +M lines]` while Claude Code holds the exact text, which Ctrl+G opens in `$EDITOR` [measured, claude §10]. unsent adds its one line under the box, with the draft's time, saying it was put back and not sent. So no clipboard fallback for long Claude drafts, and no second paste to expand them (2.3).
14. **Recovery for an AI or a launcher:** `unsent list --json` and `unsent show --json` with a stable, documented shape, and `UNSENT_NOTICE=0`, ship with restore-in-box in v0.5; `unsent hook claude`, a `SessionStart` note that names a resumed session's draft and asks the user, never acting on it, ships in v0.5.1 (2.4).

Still open: a Claude Code session that was typed in but never sent cannot be resumed (`No conversation found with session ID`), so under decision 1 its draft never goes back into a box; only the notice and `unsent restore` reach it [measured, claude §10]. That is the lost first prompt of a new chat. One option: a draft whose session has no transcript belongs to the next new chat of the same agent in the same folder, since no other conversation can claim it [design, not adopted]. Until answered, such a draft gets the notice only.

## Summary

- The spec turns unsent from a Claude Code tool into one that saves the box of every major agent CLI, and the shell command line: zsh first, then fish and bash. Each agent gets a profile, and restore-in-box pastes a lost draft back into the empty box of the same conversation when it is reopened, however it is reopened, without sending it. A new chat in the same folder gets a notice instead.
- New: a sent log per session, so after respawning a session `unsent log` shows every message sent in it, in order, with times. What happens to a sent draft is a setting: `log` by default for agents, `delete` for shells, because the shell's history already keeps run commands. A send is told from a clear by the key seen on input, and any doubt counts as a clear, so text is never dropped on a guess.
- Limits we know about: editor ground truth keeps placeholders on Copilot and Qwen, "lost on crash" was measured only for Codex and droid, the measured settle delays are start-up warm-ups, many agents lack sync marks (section 2.11), and once blank lines are fuzzed the identical floor is 0.55 to 0.56 and the byte-exact floor 0.84 to 0.88. A logged message can miss keys typed just before Enter, zsh can miss the last keystrokes before a crash, fish saves only when the window closes, and bash has no measured way in yet.
- The plan also brings a profile done definition that switches restore on per agent, an atomic orphan claim, a verify rule that tolerates the known line-break error, folder identity, crash safety and an off switch, `unsent status`, accents and wide characters in the fuzz, and a weekly rig run.
- The maintainer's fourteen decisions are in section 8. A long restored Claude draft shows as Claude Code's placeholder, which holds the exact text (measured). One question remains open: the draft of a Claude session that never sent anything, which cannot be resumed.
- Order: evidence saved first, foundations in v0.2, the sent log in v0.2.1, setup in v0.3, zsh in v0.4, then per-session restore with `--json` and `UNSENT_NOTICE` in v0.5, `unsent hook claude` in v0.5.1, keys in v0.6, and one agent per release, each milestone with an exit gate.
- **Next:** step 0 and milestone v0.2.
