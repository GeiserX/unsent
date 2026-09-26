# unsent: spec for the next version

Status: draft for the maintainer's approval. Nothing here is built yet.

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
- **Profile**: everything unsent knows about one agent. That is the reader, the wrap rule, the paste placeholder format, the delete keys, the Ctrl+Z policy and what restore-in-box needs.
- **Stitcher**: the code that rebuilds a draft taller than the box from the part on screen plus what it saw before ([`stitch.go`](../stitch.go)).
- **Orphan**: a draft left on disk by a session that ended with text still in the box.
- **Folder**: the resolved real path of the working directory (`filepath.EvalSymlinks`), so `/tmp` and `/private/tmp` on macOS are one folder.
- **Restore-in-box**: unsent pastes a recovered draft into the empty box of a new agent session. It never presses Enter.
- **Placeholder**: the short token some agents show instead of a long paste, for example `[Pasted text #1 +39 lines]`.
- **Alternate screen**: a second terminal screen that full-screen programs switch to (`ESC[?1049h`). Anything printed before the switch is hidden until the program exits.
- **Synchronized output**: the marks `ESC[?2026h` and `ESC[?2026l` around a redraw. Between them the screen is half drawn, so unsent never reads it there.
- **Kitty keyboard protocol**: a terminal mode an agent can ask for (`ESC[>1u`, `ESC[>5u`, `ESC[>7u`). If the outer terminal honours it, Ctrl and Alt keys reach unsent as `CSI` sequences such as `ESC[119;5u` for Ctrl+W instead of the single byte `0x17`. We call these **CSI-u** forms.
- **modifyOtherKeys**: an older xterm mode with the same effect (`ESC[>4;2m`), giving forms like `ESC[27;5;119~`.
- **Ground truth**: the text the agent holds, taken from the agent itself. Most agents hand the box to `$EDITOR` on a key such as Ctrl+G. With `EDITOR` set to a script that copies its file argument, the copy is what the agent holds. For most agents that is the draft with pastes expanded. For Copilot and Qwen the copy keeps the paste placeholders unexpanded [measured, copilot §10; qwen Keys], so it is not the text unsent saves. Section 5.2 lists per agent how much of this was measured.

## 1. What unsent is for

People lose prompts. A terminal window closes, the machine reboots, the agent crashes, or one Ctrl+C too many clears the box. None of the agents we checked writes the draft to a local file while you type [measured (no capture) or source, generic-reader §3.D and each profile's persistence section]. Loss on kill was tested directly only for Codex [measured, generic-reader §3.D] and droid (a double Ctrl+C left nothing on disk) [measured, droid]. For the others it follows from finding no draft file or no write path in source. Two partial exceptions: Amp syncs the draft of an existing thread to its server, but a new thread's draft has nowhere to go, and how the sync behaves after a crash is unmeasured [source; measured (lane) for the new-thread case, amp §Existing recovery]. For opencode, "nothing is written on exit" is a guess, because the source was not traced exhaustively [guess, opencode §9].

unsent runs the agent inside a pseudo-terminal, passes every byte through untouched, reads the box off its shadow screen every 0.4 s, and writes the draft to disk with `fsync`. The next time you start that agent in that folder, the draft comes back into the box, ready to edit or send.

**The promise.** No text the user did not delete is ever lost. Text on screen is read exactly. Text scrolled out of sight is inferred, and an inference can be wrong, so before any save that would lose characters unsent keeps the previous version in history.

There is one known kind of error. A line break can come back as a space. Characters are never dropped by this; line breaks can be. In the stitcher's fuzz test, 61% of saves are byte-exact and every miss is a lost line break [measured, codebase-audit §2]. 94% of the misses have a known cause: a line ends exactly at the edge of the box, and the screen cannot show whether the user pressed Enter there or the text wrapped. The other 6% look like a break lost earlier and kept after the layout changed, which was not traced [guess, codebase-audit §2]. The 61% is a rate for the fuzz model, not for real typing [guess, codebase-audit §2 caveat]; the rig in section 5 measures the real rate.

Some agents also draw text differently from what they hold. Copilot 0.0.422 turns a tab into 4 spaces and deletes box-drawing characters on screen [source + measured, copilot §5; 1.0.88 not checked]. For those, the screen alone cannot be exact, and the spec says so per agent.

## 2. Features

Priorities: **Must** ships in this version. **Should** ships in this version if the musts are done. **Could** is written down so we do not forget it; it does not block anything.

### 2.1 Per-agent support

- **Must. One profile per agent.** Adding an agent means one profile file plus test fixtures from real captures. No agent-specific code anywhere else.
- **Must. Profile done definition.** A profile is done, and its agent may be called supported, only when it has all of these:
  1. the reader, with fixtures replayed from real captures of a named version;
  2. delete keys, the newline key, and the Ctrl+Z policy (section 2.8);
  3. the paste rule: placeholder format, threshold, pairing (section 4 step 9);
  4. restore caps: settle delay, number of empty reads, newline encoding inside a bracketed paste, what cannot go back faithfully, and whether the editor copy holds placeholders (section 2.3);
  5. the quiet gap, if the agent sends no synchronized-output marks (section 2.11);
  6. negative fixtures: empty box, trust and login dialogs, menus, history browsing;
  7. a mutation test that goes red (section 5.1);
  8. one rig run (section 5.2) and a row in the [README](../README.md) with version and date (section 5.3).

  Restore-in-box is switched on for an agent only when items 4 and 6 are done.
- **Must. Tier 1 agents:** Claude Code, Codex, Gemini CLI, GitHub Copilot CLI, opencode, pi and agy. **cursor-agent** joins this list once open question 7 is answered: nothing about its box has been seen on screen yet [source, cursor-agent §0].
- **Should. Tier 2 agents:** Amp, droid, Qwen Code, Crush, Kiro, goose, aider, Kimi Code, Mistral Vibe, Cline and Kilo. Kilo's box looks like opencode's plus a model line inside the panel [measured, others: Kilo]; that it can share opencode's reader is a guess [guess, others: Kilo].
- **Could. Tier 3:** Letta Code, OpenHands, Grok Build, Auggie, CodeBuddy, Continue `cn`. The input boxes of Grok, Auggie and CodeBuddy sit behind a login wall and were never seen [measured, others]; Continue `cn` was not tried [others ranking].
- **Must. Which agents count.** An agent gets a profile when it has more than about 1,000 weekly downloads or the maintainer uses it [design]. Dropped for low downloads: superagent `grok-cli` (907 a week), iFlow CLI (583) and Qodo Command (198) [measured (lane), others]. Never surveyed: Atlassian Rovo Dev CLI, Google Jules CLI, gptme, Open Interpreter, plandex, `llm chat`. They are unmeasured, not excluded.
- **Must. Fail safe.** When a reader cannot find the box, unsent keeps the last good draft and saves nothing new. It never saves a guess over a good draft.
- **Must. Say when saving stopped.** If a reader never matched during a session in which the user typed, unsent prints one line on exit, naming the running version and the last verified one: `unsent: could not read codex 0.152.0's box this session (last verified 0.151.0), nothing was saved`. Today a reader that stops matching after an agent update fails silently. Agents release fast: Amp about 8 builds a day and Qwen 12 releases in 23 days [measured, amp and qwen stability sections]; agy about one release every 2 days [source, GitHub releases API, agy]. Silence is the worst failure here.
- **Must. Say when an agent is not protected.** Today unsent prints `unsent: no reader for "X" yet, running it without saving drafts` before starting an agent it has no reader for [source, [`wrap.go:76`](../wrap.go#L76)]. On the alternate screen that line is hidden at once, so unsent prints it again on exit.
- **Must. Agent identity.** Each saved draft records which agent it came from. The agent is found by the command's base name, with `UNSENT_AGENT=<name>` or `unsent --as <name> <command>` for `npx`, `node cli.js` and renamed binaries [codebase-audit §5 step 4]. Where a base name is shared by two different programs (`opencode`, `grok`, `kimi`), the reader must also confirm the agent from the screen before saving [docs, opencode §1; others "Name clashes"].
- **Must. Accents, emoji and wide characters.** the maintainer writes in Spanish, so accented text is the normal case. The fuzz and the fixtures include accented Latin text in both precomposed and decomposed forms, emoji, and a double-width character at the wrap edge. A double-width character that would cross the edge moves to the next row and leaves that row one cell short, so "a full row means a soft wrap" fails there [source, aider §2; crush §4]. CJK and IME input are unmeasured [generic-reader open questions].
- **Should. `unsent capture <agent>`** writes a fixture bundle into `testdata/<agent>/<version>/`: raw output bytes, raw input bytes, and the editor copy when the profile has an editor key. This turns "add an agent" into a PR instead of a research lane. `UNSENT_DEBUG_DIR` already logs views; it gains raw byte logs for this.

### 2.2 Generic reader

- **Should. A best-effort reader for agents with no profile.** It learns the box layout from the first screen where typed text appears: text column, first-row marker, continuation prefix, typed-text colour. It then reads that layout at the cursor on every save. A prototype, replayed through unsent's emulator, reached the exact 32-line draft on six agents (Codex, Copilot 0.0.422, Gemini, opencode, Crush, aider) [measured, generic-reader §3.A].
- That result is weaker than it sounds:
  - Crush's "exact" draft was missing its first 4 characters, because a start-up dialog ate them, and the test allowed it [measured, generic-reader §3.A].
  - Codex was covered only by a frame test, so the negative controls covered five agents, not six [measured, generic-reader §3.A].
  - It calibrated from the known typed text, not from keystrokes, and its tests printed results instead of asserting.
  - A deliberately broken marker still passed on aider and opencode, through the scrolled-box fallback [measured, generic-reader verification notes].

  The production version must calibrate from the key stream, assert, and pass a mutation test on every agent, including a mutation of the fallback branch.
- **Must, if the generic reader ships. Secret guard.** Never calibrate on a field that looks like a login or API-key prompt. Gemini's API-key dialog is an input box showing the key [measured (no capture), generic-reader §3.A]. Refuse when the screen shows `API key`, `password`, `token` or `passphrase`, or the typed text has no spaces and is over 20 characters [guess, design]. The second rule also refuses long paths and URLs, so it ships with a false-positive fixture set.
- Drafts from the generic reader are labelled "best effort" in `unsent list`. Whether it is on by default is open question 4.

### 2.3 Restore-in-box

We chose this: restore pastes the recovered draft into the empty box and never sends it.

- **Must. Automatic restore.** When an agent starts a chat in a folder that has an orphan from the same agent, unsent pastes the newest orphan into the box once the box is ready. The user sees their draft and decides what to do with it. Open question 1 asks automatic versus on request; until it is answered, the code ships automatic, the recommended option.
- **Must. Chat starts only.** Restore fires on a plain interactive start. It does not fire when the command carries a prompt argument (`claude "fix x"` sends on start), on subcommands that are not a chat box (`codex login`, `claude mcp`, `gemini extensions`), or on resume (`claude --resume`, `codex resume`) unless open question 11 says otherwise [design]. Each profile lists its non-chat subcommands.
- **Must. Safety conditions, all of them, before unsent writes a single byte** [codebase-audit §4.1 unless noted]:
  1. After the profile's settle delay, the reader has returned "empty box" on 3 saves in a row (1.2 s at the 0.4 s tick) [design; the count is per profile]. Settle delays are measured for three agents only, and none of them is a paste delay:
     - Claude Code: Esc+Enter made a new line about 1.2 s after the box appeared on a clean config [measured (no capture), claude §4]. The repo's [CLAUDE.md](../CLAUDE.md) gives about 9 s on the maintainer's config; that plugins and hooks cause the difference is unverified [claude open question 5].
     - Qwen: the placeholder plus about 1 s [measured (lane), qwen startup].
     - Gemini: the box itself took 14 to 24 s to appear [measured (lane), gemini §8]. That is start-up time, not a delay after the box appears.

     Whether a paste in the first seconds is dropped the way an early Esc+Enter is, is unmeasured for every agent [codebase-audit §4.1]. Each profile measures its paste settle delay before restore is switched on.
  2. The user has typed nothing in this session. Focus reports, mouse reports and resizes do not count as typing.
  3. The agent has turned on bracketed paste (`ESC[?2004h`) and not turned it off. unsent tracks this in the output the same way it already tracks `?2026`. Without bracketed paste a newline in the draft is Enter, and Enter sends.
  4. The draft is cleaned first: every `ESC` byte is removed, so an embedded `ESC[201~` followed by a newline can never end the paste early and submit.
  5. The newline encoding inside the paste (`\n` or `\r`) is the one the profile measured. It is unmeasured today, Claude Code included [codebase-audit §4.1].
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
- **Must. Claim the orphan first.** the maintainer runs parallel sessions in one folder, and two new sessions could both inject the same orphan. Before injecting, a session claims the orphan atomically (rename it to a claimed name in the same folder) [design]. If verification fails, the claim is released.
- **Must. Record the paste as a paste.** unsent feeds the injected text to its own paste tracker. Most agents turn a long paste into a placeholder, and without the tracker entry the new session would save the placeholder instead of the text [codebase-audit §4.1].
- **Must. Verify before archiving.** The orphan moves to history only after a save of the new session reads back the same draft. "The same" means equal, or different only by a line break read back as a space where the read-back row was full (the known error in section 1) [design]. If the read-back fails, the orphan stays. After 3 failed restores of one orphan, unsent stops injecting it and leaves it for `unsent restore` [design].
- **Must. Cancel on typing.** Any key the user presses before the injection cancels it.
- **Say what the user will see.** In Claude Code a paste of 4 or more lines, or over 800 characters, shows as `[Pasted text #N +M lines]` [measured, claude §3]. Most restored Claude drafts will therefore appear as a placeholder, not as editable text. Open question 1 includes whether that is acceptable.

### 2.4 Recovery when restore-in-box does not apply

- **Must. Notice after exit.** On the alternate screen, the notice unsent prints before starting the agent is hidden: Claude Code, opencode, Amp, Crush, Copilot 1.0.88, Qwen and Cline all start with `ESC[?1049h` [measured, each profile's screen mode]. Kiro clears the screen at start with `ESC[2J` [measured, kiro §Screen modes]. cursor-agent can wipe scrollback mid-session with `ESC[3J` [source, cursor-agent §1]. So when an orphan was not restored, unsent prints the notice again after the agent exits.
- **Must. `unsent restore`** keeps working as today: clipboard, or print when there is no clipboard, `clip.exe` on WSL [source, generic-reader §5; [`main.go:231-249`](../main.go#L231-L249)].
- **Must. Filter by folder and agent.** The notice, `restore` and restore-in-box pick drafts by folder **and** agent. Today they pick by working directory only, so a Codex draft would be offered in a Claude session [source, codebase-audit §3]. Folder means the resolved real path (Terms). A draft saved in a subfolder is not restored into the parent; the notice counts it. Cross-agent offers are open question 2.
- **Should. Orphans elsewhere.** The notice counts orphans in other folders: `2 more drafts in other folders: unsent list --all`. After a reboot the user may never reopen that agent in that folder, and then nothing else tells them a draft is waiting.
- **Should. Several orphans.** Restore the newest. The notice names how many more wait in `unsent list`.
- **Should. Recovery inside a running session.** A draft cleared by mistake (one Ctrl+C too many, Esc Esc, an Up-arrow recall that replaces the draft) goes to history, but restore-in-box needs a new session. Today the way back is a second terminal, `unsent restore`, and a paste from the clipboard. That works, and it is the fallback. A direct path is open question 10: `unsent restore --into` from another pane, reaching the live session over a unix socket in the state folder (local, not network), or a reserved key chord, which takes a key away from the agent. The safety conditions of 2.3 apply, with "typed nothing" replaced by "the box reads empty now".

### 2.5 History and versions

- **Must. Keep what agents throw away.** A draft cleared with Ctrl+C or Esc Esc goes to unsent's history. Agents mostly do not keep these. Claude Code's own history stores a cleared paste draft as the placeholder only, with `pastedContents: {}` [measured, claude §6]. Codex saves a Ctrl+C-cleared draft only when a thread exists, and stores pastes as placeholders [source, codex §6]. opencode keeps a cleared draft only when it has 20 or more trimmed characters or holds a paste [source, opencode §9].
- **Must. Format version on every record, and a migration rule.** A renamed field in the draft file would unmarshal as an empty draft, and `orphans()` would then delete the file [source, distribution §4]. That is a direct break of the promise. Rule: add a `format` field now; never rename or retype a field without a version bump and a migration test.
- **Must. Existing caps stay:** 500 history entries and 300 safety versions overall [source, codebase-audit §2], 30 safety versions per session [source, [`store.go:20`](../store.go#L20)].
- **Must. Orphans never expire on their own.** Deleting one automatically would break the promise [design]. The user removes them with `unsent forget`.
- **Must. `unsent forget <n>`** deletes one draft or version. A user who typed a secret into a box needs a way to remove it, and orphans have no expiry. This is the user deleting, so the promise allows it.
- **Should. `unsent list`** shows agent, folder, time, size and first line; `--agent`, `--here` and `--all` filter it.
- **Images stay placeholders.** `[Image #N]` comes back as the placeholder, because image bytes never cross the pseudo-terminal. The README says so today; nobody should plan to fix it.
- **Could. `unsent search <words>`** across history.
- **Could. Paste attachments.** Crush keeps big pastes as in-memory attachments that die on quit [source, crush §8]. unsent already has the paste bytes from the input side and could store them next to the draft.

### 2.6 Setup and uninstall

- **Must. `unsent setup [zsh|bash|fish]`** writes one marked block to the shell's rc file. The block lists every agent name this build has a profile for. At each shell start it defines a `function <agent> { … }` wrapper for each listed name found on `PATH`, so an agent installed later is covered from the next shell. Each wrapper falls back to the real agent when `unsent` is missing [measured in bash 3.2, bash 5.3 and zsh 5.9, distribution §3]. After an unsent upgrade that adds profiles, the user runs setup again, which rewrites the block.
- An `eval "$(unsent init zsh)"` line would keep the list current without re-running setup, but costs one process start per shell, about 100 to 150 ms on this Mac under load [measured (lane), distribution §3 trap 8]. The static block costs nothing measurable, so it stays the default.
- The block must use `function name { }`, never `name() { }`. With an existing `alias claude=...` the second form fails to parse and **the rest of the rc file does not run** in all three shells [measured, distribution §3 trap 1].
- **Must. No PATH shims and never replace the agent binary.** A shim makes unsent find itself again and loop [measured (lane), distribution trap 5].
- **Must. Warnings.** Setup warns about aliases that point at a path and about functions that start the agent through `env`, `command` or a full path, because those skip the wrapper. the maintainer's own `cc` and `ccr` launchers call `env ... claude` and would run unprotected [source, distribution trap 4]. See open question 8. Anything that runs the agent without the user's interactive rc file also skips it: scripts, `exec claude`, and probably `tmux new-window claude` and zellij layouts [guess].
- **Must. `unsent status`.** Lists which agent commands in this shell go through unsent, which profile and last verified version each gets, and which aliases or launchers bypass it. Setup warns once; status answers "am I protected?" at any time.
- **Must. Off switch.** `UNSENT_OFF=1` makes `unsent <agent>` exec the agent directly. When a reader misbehaves after an agent update, the user needs a way out that does not mean editing rc files. `command claude` also skips the wrapper; the README says both.
- **Must. `unsent setup --undo`** removes the block. Uninstall is `setup --undo` then `brew uninstall unsent`, in either order [measured, distribution trap 7]. Drafts are user data: uninstall prints where they are and never deletes them.
- **Should. fish** through `~/.config/fish/conf.d/unsent.fish` [docs only; fish is not installed on this Mac].
- Pipes and non-terminal calls already pass straight to the agent, so the wrappers are safe for `git diff | claude -p` [source, distribution §3]. Claude Code copies user functions into its Bash tool; that the Bash tool never runs with a terminal, and so always passes straight through, is unchecked [guess, distribution trap 6].

### 2.7 Privacy

- **Must. No network, ever.** No update check, no telemetry. This rules out a self-updater [source, distribution §4]. A unix socket in the state folder (2.4) is local and allowed.
- **Must. Files stay private:** folder mode `0700`, files `0600`, as today.
- **Must. The debug log is opt-in.** `UNSENT_DEBUG_DIR` writes every view with its text, and with 2.1's capture change, raw input and output bytes. It stays off unless set, and writes `0600` files.
- **Must. Secret guard** for the generic reader (2.2).
- **Must. Nothing unsent ships contains a real key.** Test fixtures come from runs with dummy keys only.

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
- **Should not.** Setting env vars such as `CODEX_TUI_DISABLE_KEYBOARD_ENHANCEMENT=1` to keep legacy bytes. The likely cost is Shift+Enter and Ctrl+M newlines for the user [guess, codex §5]. unsent must not change how the agent behaves.

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

- **Should. zsh integration.** `eval "$(unsent init zsh)"` installs `line-pre-redraw`, `line-init` and `TRAPHUP` hooks that append `$BUFFER` to a per-shell log [measured, other-textboxes §2]. It is exact, costs about 0.03 ms per save, and covers closed windows, crashes and Ctrl+C. Two limits: zsh skips a redraw when more input is already pending, so a shell that dies in that gap loses the last keystrokes [measured (lane capture), other-textboxes §2]; and a power cut can lose the last record or two, because `O_SYNC` is not a full flush on macOS [docs, other-textboxes §2]. The log is opened with `cloexec` so child processes do not inherit it. With atuin, Up and Ctrl+R replace the buffer wholesale; the existing "archive on replacement" rule covers that [source, other-textboxes §2]. It is independent of every other step, so it can ship right after setup (open question 5).
- **Could.** fish on hang-up only. bash has no hook while readline reads [measured]. REPLs and editors are out: vim and pico already keep their own recovery files [measured, other-textboxes §1].

### 2.13 Distribution

- **Must.** A Homebrew formula in `GeiserX/homebrew-unsent`, plus the existing `go install` and release tarballs. Formula binaries are not quarantined, so no Apple signing is needed [measured, distribution §2].
- **Should.** `.deb` and `.rpm` from goreleaser. homebrew-core once the repo is 30 days old, from about 2026-10-25, and has enough stars [docs, distribution §2].
- **Not now.** npm, curl-pipe-sh, a Homebrew cask and notarization. Casks are quarantined, so they would need notarization first [measured, distribution §2].
- Pushing to the tap needs a token that can write to that repo; the workflow's own token cannot [docs].

### 2.14 Command surface

One list, so the plan has one source.

| Command | What it does | Status |
|---|---|---|
| `unsent <agent> [args]` | Run the agent under unsent | Today; gains `--as <name>` |
| `unsent list` | Drafts: agent, folder, time, size, first line; `--agent`, `--here`, `--all` | Today; filters are Should |
| `unsent show <n>` | Print one draft | Today |
| `unsent restore [n]` | Copy a draft to the clipboard, or print it | Today; `--into` per open question 10 |
| `unsent forget <n>` | Delete one draft or version | Must |
| `unsent setup [shell]`, `--undo` | Write or remove the rc block | Must |
| `unsent status` | What is protected in this shell, and what bypasses it | Must |
| `unsent capture <agent>` | Record a fixture bundle | Should |
| `unsent init zsh` | zsh command-line hooks | Should, if approved |
| `unsent version` | Version | Today |

## 3. Support matrix

**Evidence** means how the box itself is known. **measured** = the box was seen on a real screen or replayed from a byte capture. **source** = read in code or a bundle only. **docs** = vendor docs only. Versions are the ones profiled.

| Agent, version | Evidence | What makes the box readable | Main risk |
|---|---|---|---|
| Claude Code 2.1.282 | measured; reader ships today | Two full-width `─` rules, first row `❯` + no-break space, 2-column indent, wrap `cols-4`, cap `rows/2-5` [claude §1, §5] | Kitty push contested at the main prompt; scroll model contested (one row at a time for Up and Down per the claude profile, "jumps" per CLAUDE.md) [claude §5]; `[Image #N]` and `[...Truncated text #N +M lines...]` placeholders unknown to unsent; a box over 10,000 characters collapses its middle even for typed text [source, claude §3] |
| Codex 0.151.0 | measured + source | Bold, not dim, `›` at column 0; dim hint when empty; wrap `cols-3`; cap `rows-4`; scrolls one row at a time [codex §1, §3] | Kitty push measured in tmux; `>7u` with release events on other terminals is source and docs only [codex §5]; an extra empty row after a line that fills the width; history messages also start with `›`, told apart only by dim [codex §3] |
| Gemini CLI 0.61.0 | measured + source | Half-block `▄`/`▀` frame, ` > ` marker at column 1, text from column 3, wrap `cols-6`, fixed cap of 10 rows [gemini §2, §3] | Empty hint is a colour, not dim; five frame variants; no sync marks on the main screen; weekly releases with 66 prompt commits this year [gemini §2, §9] |
| Copilot CLI 0.0.422 and 1.0.88 | measured, both | `─` rules with `❯ ` + ASCII space; 1.0.88 can draw a "rail" frame instead [copilot §1] | Two versions draw different boxes; 0.0.422 shows tabs as 4 spaces and deletes box-drawing characters on screen (1.0.88 unchecked); editor copy keeps placeholders; no sync marks in tmux on either; Esc+Enter submits; it writes to the real `~/.copilot` even with `--config-dir` [copilot §3, §5, §8, §10, §14] |
| cursor-agent 2026.07.20 | **source only**; login gate measured | Background-coloured bar, glyph `→` dim when empty, fake inverse cursor, fixed cap of 6 rows, wrap `cols-7` [cursor-agent §3, §5] | Nothing about the box seen live; needs background and reverse-video per cell; no sync marks; `ESC[2J ESC[3J` clears wipe scrollback mid-session [cursor-agent §1] |
| opencode 1.18.32 | measured + source | Left `┃` bar on a filled panel, meta row `Build · …` below, `╹▀▀▀` edge; alternate screen; every frame synchronized [opencode §3] | Needs per-cell foreground and background; transparent themes remove the fill; plugins can replace the prompt; paste tokens have no index; the name `opencode` also belongs to an archived Go project [opencode §1, §6, §11] |
| pi 0.87.1 | measured + source | Two `─` rules, no glyph, fake reverse-video cursor, exact ` ↑ N more ` counts on the rules, cap `max(5, floor(rows*0.3))` [pi §What the box looks like] | Alt+Enter submits; paste ids renumber when one is deleted; Ctrl+V and right-click pastes never cross the pseudo-terminal; kitty flags 7 pushed; editor round trip resets the paste map [pi §Pastes, §Keys, §External editor] |
| agy 1.2.11 | measured in tmux, mostly with the alternate screen forced on; closed source | Two `─` rules, `> ` ASCII glyph, 2-column indent, exact `↑ N more lines` counts, cap `floor(rows/2)` [agy §The input box] | Closed source, a release about every 2 days, and auto-update ran even with `AGY_CLI_DISABLE_AUTO_UPDATE=1`; hint is a colour; `[Pasted text #N M chars]` not matched by unsent; default screen mode outside tmux unknown [agy §Risks] |
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
1. **CI split.** Step A runs `go test -race -skip Fuzz ./...`; step B runs `UNSENT_FUZZ_SEEDS=50 go test -coverprofile ./...` and uploads coverage. About 33 s and 88 s locally, down from 275 s plus 58 s [measured, codebase-audit §2].
2. **Honest fuzz.** Fix the simulator hang on blank lines, add blank-line edits, and add a byte-exact counter with its own floor. With blank-line edits the byte-exact rate is about 60% (60.18% across modes), so the floor starts at 0.60, not at the 61% measured without blank lines [measured, codebase-audit §2]. Prove it can fail with a mutation that drops `\n` in `unwrap`. Blank lines also push the repeated-words rate to 96.27%, below its 0.97 floor [measured, codebase-audit §2]. Decision: re-tune that floor to the measured value with blank lines in this step, so it can only go up. Step 13 may raise it later; its gain inside the fuzz may be near zero [guess, codebase-audit §5 step 7].
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
       keys     keyset            // before, after, any amount, whole draft, restore, newline
       ctrlZ    suspendPolicy     // first press, second press, pass through
       restore  restoreCaps       // settle delay, empty reads, paste newline, lossy transforms
       editor   editorCaps        // key, and whether the copy keeps placeholders
       noSync   bool              // read after a quiet gap instead of after ?2026l
       quietGap time.Duration
   }
   ```

   `view` stays the reader's output and gains two optional fields: exact hidden-row counts above and below, for agents that print them (pi, agy, droid, Kimi), and a shell-mode prefix.
6. **Agent and folder identity.** `Agent` in `record`, falling back to the base name of `Command[0]` for old files; `UNSENT_AGENT` and `--as`; folder as the resolved real path; notice, `restore` and restore-in-box filter by folder and agent.
7. **Safety of unsent itself.** Panic recovery in the save loop with fallback to passthrough, `UNSENT_OFF=1`, the "saving stopped" and "not protected" lines on exit (2.1, 2.6, 2.11).
8. **Raw input and output logging** under `UNSENT_DEBUG_DIR`, then `unsent capture` on top of it (2.1).
9. **Key decoder** (section 2.8) and per-profile keysets and Ctrl+Z policy. Only after the Warp and Ghostty measurement.
10. **Notice after exit.** A few lines in `wrap()`.
11. **Richer snapshot.** Per cell: foreground, background, reverse video, bold, as well as today's dim flag. Also the alternate-screen flag and the bracketed-paste state. The emulator cells already hold all of this; [`screen.go`](../screen.go) drops it [source, cursor-agent §4]. Without this step, readers for Gemini, opencode, Qwen, Crush, agy, Kiro, Copilot 0.0.422, cursor-agent, Amp, pi and droid cannot tell a hint from a draft or find the cursor [each profile's detection recipe]. Built with the first reader that needs it.
12. **Restore-in-box** with every condition in section 2.3. Tests: a draft containing `ESC[201~\r` never submits; an unverified restore leaves the orphan; a keypress before the box settles cancels the restore; a focus report does not cancel it; two sessions starting together inject once; a captured trust dialog never gets a restore; `claude "prompt"` and `claude --resume` never get a restore.
13. **Line-break memory.** When `unwrap` has to guess at a full row and the known text already has a `\n` between the same two words, keep the `\n`. The gain is a guess; the byte-exact counter from step 2 measures it.
14. **Paste rules per profile.** A placeholder regex plus how to pair it with a captured paste: by number (Claude, agy), by number that renumbers on delete (pi, Kimi), by order and line count because there is no number (opencode, Kiro), by the first 50 characters (droid), or not at all because pastes stay inline (Amp, goose, aider). Each rule uses the agent's own counting: UTF-16 length for Gemini, Kiro and droid, line breaks rather than lines for Claude, a trailing newline counted as a line for Gemini, Qwen and Kiro.
15. **Unwrap and scroll models.** Word wrap with the space swallowed (Claude, Codex, Amp); word wrap with the space at the row end when it fits (Gemini, Crush); droid keeps the space at the row end if it fits, else starts the next row with it, so rows are joined verbatim from untrimmed cells [measured + guess, droid wrapping]; character wrap (aider, goose). Scroll by jump, by one row, or no cap where only the tail is visible (goose, Kiro, Letta). Claude's scroll model is settled first by a per-step capture (section 7, v0.2). The fuzz simulator takes these from the profile so it runs once per profile.
16. **Tier 1 profiles**, one PR each, each meeting the profile done definition (2.1).
17. **Generic reader**, if approved.

## 5. How "works with agent X" is proven

A claim that unsent works with an agent names a version and a date, and rests on three things.

### 5.1 CI replays real captures

- Every profile has fixtures from real runs of that agent version: raw output bytes, and the keystrokes that produced them. CI replays the bytes through unsent's own pipeline (emulator, reader, stitcher, paste tracker) and asserts the exact draft at each step. This is new: the Amp, Kiro, Qwen and generic-reader verification runs replayed captures through `charmbracelet/x/vt` and dumped or read frames [measured, amp, kiro, qwen and generic-reader verification notes], but only the generic prototype ran a reader and the stitcher, and none asserted drafts from unsent's real pipeline.
- The fake agent in [`wrap_test.go`](../wrap_test.go) becomes a replayer of recorded frames, chosen by `UNSENT_FAKE_AGENT=<profile>`, instead of a hand-drawn imitation of Claude Code.
- Negative fixtures for every profile: the empty box, trust and login dialogs, menus, history browsing. None may yield a draft.
- Fixtures include accented text, emoji and a wide character at the wrap edge (2.1).
- **A check that cannot fail is not a check.** Every profile's tests must go red when its reader is mutated: marker, indent, wrap width, and the fallback branches. The generic prototype's marker mutation left aider and opencode green [measured, generic-reader §3.A]; that must not happen again.

### 5.2 The real-app test rig

We call this the rig, not "the real-app harness", because the harness means the agent runtime.

It is opt-in and never runs in CI. Per run:

- A private tmux server, `tmux -L unsent-rig-<agent> -f /dev/null`, fixed size, launched under `env -i` with a scratch `HOME`, scratch XDG directories and the agent's own config directory pointed into the scratch space.
- A login-free path per agent, all found during research: dummy API keys for Claude Code, Codex, Gemini, aider, Crush and Kiro; `OPENAI_BASE_URL=http://127.0.0.1:9/v1` for Qwen; a fake `GEMINI_API_KEY` and an unreachable base URL for agy; an unusable provider for opencode; `PI_OFFLINE=1` for pi; `FACTORY_AIRGAP_ENABLED=true` for droid; a local stub server via `AMP_URL` for Amp; `GOOSE_DISABLE_KEYRING=1` and a fake key for goose. cursor-agent has none and needs a real login (open question 7).
- **Ground truth from the agent's own external-editor key** with `EDITOR` and `VISUAL` set to a copy-and-exit script. Keys: Ctrl+G for Claude Code, Codex, Gemini, Copilot, pi, agy, Amp and Kimi; Ctrl+X e for opencode; Ctrl+X Ctrl+E for aider; Ctrl+O for Crush; Ctrl+X for Qwen; Ctrl+P for droid. What was measured about the copy:
  - Exact draft, pastes expanded, no trailing newline: Codex, agy, droid, aider, opencode, Amp [measured, each profile's external editor section].
  - Claude Code: no trailing newline measured; placeholder expansion was reported but not captured [measured (no capture), claude §3].
  - Gemini: a 1,054-byte copy with pastes expanded; the trailing newline was not recorded [measured, gemini §6].
  - Copilot and Qwen: exact buffer, but paste placeholders stay unexpanded [measured, copilot §10; qwen Keys]. The rig compares their copies against unsent's save with placeholders collapsed back, or on scenarios without collapsed pastes.
  - pi: expansion seen on screen; the byte-exact file is source only [pi §External editor]. Crush: a byte match backed only by research notes, and its round trip trims whitespace [measured (lane), source, crush §9]. Kimi: never exercised [source, others: Kimi].
  - goose and Kiro have no non-submitting editor key, so their ground truth is the typed input plus the screen [source, goose §10, kiro §Draft persistence].

  Traps: Codex needs `-c sandbox_mode="danger-full-access"` or it writes to the real `~/.codex/editor` [measured, codex §5]; pi's round trip resets its paste map and clears scrollback [measured, pi §External editor].
- Scenarios per agent: type; multi-line draft; accented text; paste below and above the placeholder threshold; draft taller than the box with scrolling and edits out of sight; every delete key; Ctrl+C clear; kill the agent with `kill -9`; close the window with `kill-session`; restore-in-box into a fresh session; a trust dialog on start. Each ends with the saved draft compared to ground truth byte for byte. Lost line breaks are reported as a count, never hidden, and this count is the real-world rate section 1 asks for.
- Newline keys per agent, because the wrong one sends the prompt: Esc+Enter submits in Copilot, pi and droid; Ctrl+J and Ctrl+O submit in aider; Ctrl+Enter submits in droid; Ctrl+J toggles the changelog in droid when one is showing [measured or source in each profile]. The rig reads the newline key from the profile.
- **Kitty and modifyOtherKeys runs.** Agents pushed kitty flags in the captures, but tmux does not answer the kitty query, so no capture shows key bytes arriving in CSI-u form [measured, amp §Keys; codex §5]. The rig gets a second driver in Go that plays the outer terminal: it owns the pseudo-terminal in front of unsent, answers `ESC[?u` with flags, and sends keys in legacy, CSI-u or modifyOtherKeys form. Whether that matches a real Ghostty is unmeasured, so the Warp and Ghostty key logs from 2.8 back it up.
- The rig records typing latency and CPU for the performance targets in 2.11.
- **Cadence.** Support claims go stale within days: Amp ships about 8 builds a day. The rig runs weekly on a Mac mini of the fleet, never the MacBook, against the latest release of every supported agent, and commits the result to the README table [design].

**Hard rules for the rig**, each learned from a side effect during the research:

- Never use the real `HOME`. Copilot wrote to the real `~/.copilot` [measured (lane), copilot §15; later writes by another process were found and not attributed]; Codex created `~/.codex/editor` [measured, codex §9]; Amp wrote `~/.cache/amp/logs` [measured, amp §Risks].
- Never start a shell with `-i` or rc files. During verification, a `zsh -i` put `~/.local/bin` first on `PATH` and launched the real Claude Code twice [measured, distribution verification notes]. Use `env -i` with `zsh -f` or `bash --norc`, and a positive control that no real agent is on the rig's `PATH`.
- Unset `GITHUB_TOKEN`, `GH_TOKEN`, AWS and every provider key. Kilo picked up an inherited `GITHUB_TOKEN` and AWS variables and chose real providers [measured, others: side effects]; Copilot also calls `gh auth token` unless started with `--no-auto-login` [source, copilot §14].
- Unset `CI`, or droid and Qwen stop sending sync marks [source, droid; qwen].
- Never submit. Quit with the agent's own exit key on an empty box, then kill the whole process tree: Gemini survived its pane being killed [measured (no capture), gemini §8].
- Turn auto-update off where it can be turned off, and record the version actually run. agy updated itself to 1.2.11 despite the switch [measured, agy §Version comparison].

### 5.3 What the README says

The support table lists, per agent, the version and date of the last rig run and the evidence level. An agent without a rig run is not called supported. When the installed version is newer than the last verified one and the reader fails, the "saving stopped" line (2.1) names both versions.

## 6. Naming: keep `unsent`

Recommendation: keep `unsent`. Evidence, all from [`research/distribution.md`](research/distribution.md) §1:

- **The command is `unsent` either way.** Agent CLIs name the package `X-cli` and the binary `X`: `@google/gemini-cli` installs `gemini`, the `copilot-cli` cask installs `copilot` [measured]. A suffix would change only the repo and package names.
- **The suffix makes the one real collision worse.** An email product called Unsent ([unsent.dev](https://unsent.dev)) ships SDKs in 8 languages and has no CLI [measured]. `unsent-cli` is the exact name their missing CLI would take [guess].
- **Every channel we would use is free for `unsent`:** Homebrew core and taps, unscoped npm, GitHub, Debian, AUR, nixpkgs [measured]. It is taken on crates.io, PyPI and RubyGems, which a Go binary does not use [measured].
- **Renaming is cheap only now.** 8 release downloads and 1 star so far [measured]. A later rename changes the Go module path, and `go install github.com/GeiserX/unsent@latest` would break for existing users [docs].
- If the collision still worries you, pick a different word, not a suffix. Trademarks were not checked.

## 7. Milestones in shipping order

Each milestone is a release on its own, leaves main working, and closes only on its exit gate.

0. **Now, no release: save the evidence** (step 0). Exit gate: every capture folder the specs cite exists on `/Volumes/Data` on a box, with a file count that matches `/tmp`.
1. **v0.2: foundations, Claude only.** Steps 1 to 8 and 10: CI split, honest fuzz with re-tuned floors, fixtures in the repo, record format version, profile extraction, agent and folder identity, reader panic guard and `UNSENT_OFF`, the "saving stopped" and "not protected" lines, raw byte logging, notice after exit. Claude fixes that need no new machinery: Ctrl+D, `ESC d` and word-sized `ESC 0x7f` as delete keys; the `[Image #N]`, `[Audio #N]` and `[...Truncated text …]` placeholders. A per-step capture of Claude's scroll model, and CLAUDE.md and README corrected to "4+ lines or over 800 characters" for the paste threshold [measured, claude §3]. Visible to users: the notice after exit, drafts filtered by agent, the new exit lines.
   Exit gate: fixtures replay green; a mutated reader goes red; a mutation dropping `\n` in `unwrap` turns the byte-exact floor red; a real Claude Code session on the alternate screen shows the notice after exit; CI under 5 minutes on the run page.
2. **v0.3: setup and the tap.** `unsent setup`, `--undo` and `unsent status`; the Homebrew tap. These are measured, need no reader work, and are what put unsent in front of every agent.
   Exit gate: setup and undo pass in bash 3.2, bash 5.3 and zsh 5.9, including an rc file with `alias claude=...` before the block; `brew install` from the tap runs on a clean Mac mini.
3. **v0.4: keys.** The Warp and Ghostty key logs (2.8) first. Then step 9: the key decoder, per-profile keysets, the Ctrl+Z policy. This fixes a real hang today if Ctrl+Z arrives in CSI-u form.
   Exit gate: the recorded Warp and Ghostty key logs replay through the decoder with every delete key and Ctrl+Z recognised; in a real Ghostty session Ctrl+Z suspends and `fg` returns to the agent.
4. **Rig track, no release of its own.** The tmux part lands before v0.5, which needs it to test restore. The Go outer-terminal driver that answers the kitty query lands before v0.7, because Codex pushes the kitty protocol.
5. **v0.5: restore-in-box for Claude Code** with every safety condition (step 12). Needs the Claude settle delay and paste newline encoding measured on the real config first.
   Exit gate: all step 12 tests green; the rig's restore scenario passes on Claude Code; the trust dialog negative fixture yields no restore.
6. **v0.6: zsh command lines**, if approved (open question 5). Independent of every other step.
   Exit gate: a zsh killed with SIGHUP and one killed with `kill -9` both leave the typed line in `unsent list`.
7. **v0.7: Codex; v0.7.1 pi; v0.7.2 agy.** Box shapes close to Claude's. Brings step 11 (richer snapshot: pi and agy need reverse video and colour), step 14 (paste rules) and step 15 (unwrap and scroll models), which the first non-Claude agent needs. Codex needs the decoder from v0.4 because it pushes the kitty protocol.
   Exit gate per release: the profile done definition (2.1), restore included.
8. **v0.8: Gemini; v0.8.1 Copilot; v0.8.2 opencode, with Kilo.** The colour-dependent readers and the quiet-gap read for agents without sync marks. Copilot per open question 6.
   Exit gate per release: the profile done definition.
9. **v0.9: cursor-agent**, after one logged-in capture (open question 7). Exit gate: the profile done definition.
10. **v0.10.x: tier 2**, one agent per release in this order: Amp, droid, Qwen, Crush, Kiro, goose, aider, Kimi, Vibe, Cline. Exit gate per release: the profile done definition.
11. **v0.11: generic reader**, if approved, with the secret guard. Exit gate: mutation of the marker and of the fallback branch goes red on every prototype agent; the secret-guard false-positive set passes.
12. **Later:** step 13 (line-break memory) once the byte-exact counter shows room for it, `.deb`/`.rpm`, homebrew-core, fish setup, `unsent search`, paste attachments, OSC 52, tier 3 agents.

## 8. Open questions for the maintainer

Each answer changes what gets built.

1. **When does restore-in-box fire?** Automatically when the next session of the same agent starts a chat in the same folder (recommended, and the default the code ships if there is no answer), or only when you ask, with a key or `unsent restore --into`? Automatic is the point of the feature; the cost is a draft appearing when you came to do something else. Ctrl+C then moves it to history, so nothing is lost. Also: in Claude Code, a restored draft of 4 or more lines or over 800 characters shows as `[Pasted text #N +M lines]`, not as editable text. Is that acceptable, or should such drafts go to the clipboard instead?
2. **Cross-agent drafts.** Should a Claude draft be offered in a Codex session in the same folder? Recommended: no, same agent only; `unsent list` and `unsent restore <n>` reach any draft.
3. **May unsent change how an agent looks to make it easier to read?** Examples: `COPILOT_PROMPT_FRAME=0` removes Copilot's rail frame [source, copilot §1]; `TWINKI_HARDWARE_CURSOR=1` gives Kiro one cursor model [source, kiro §Height]; `AGENT_CLI_DISABLE_HALF_BLOCK_PROMPT_BAR` gives cursor-agent one layout [source, cursor-agent §11]. Recommended: no; the readers handle every variant. That means more reader work.
4. **Generic reader for agents with no profile:** on by default with the secret guard, or opt-in with `unsent --generic`? Or skip it this version?
5. **zsh command lines:** in this version (v0.6, right after setup) or later?
6. **Copilot:** support only 1.x, the current release, or also 0.0.422, the Homebrew cask installed on this Mac? The two draw different boxes [measured, copilot §13].
7. **cursor-agent:** its box has never been seen. Building its reader needs a logged-in session in a scratch config. `cursor-agent status` says logged in while the TUI asks for a login [measured, cursor-agent §0]. Will you log it in for one capture run, or should it wait?
8. **Your `cc` and `ccr` launchers** start Claude through `env`, which skips the shell wrapper [source, distribution §3]. Should `unsent setup` only warn, or should we change those launchers to call `unsent claude`?
9. **Name:** confirm `unsent` (section 6). A rename after the tap ships costs more.
10. **Recovery inside a running session:** is today's path (second terminal, `unsent restore`, paste) enough? If not, `unsent restore --into` from another pane over a local socket (recommended, takes no key from the agent), or a reserved key chord?
11. **Resume:** should restore fire on `claude --resume` and `codex resume`? Recommended: no; the notice still names the draft.
12. **Scope sentence for the README:** "AI agents' boxes and your zsh command line" [other-textboxes §3], or wider? The README is rewritten with the second supported agent (v0.7).

## Summary

- The spec turns unsent from a Claude Code tool into one that saves the box of every major agent CLI. Each agent gets a profile, and restore-in-box pastes a lost draft back into the empty box without sending it.
- Limits we know about: editor ground truth keeps placeholders on Copilot and Qwen, "lost on crash" was measured only for Codex and droid, the measured settle delays are start-up warm-ups, many agents lack sync marks (section 2.11), and the byte-exact floor is 0.60 once blank lines are fuzzed.
- The plan also brings a profile done definition that switches restore on per agent, an atomic orphan claim, a verify rule that tolerates the known line-break error, folder identity, crash safety and an off switch, `unsent status`, accents and wide characters in the fuzz, and a weekly rig run.
- The plan places every step in a release: evidence saved first, setup before keys, one agent per release, and each milestone with an exit gate.
- **Next:** your answers to the twelve questions in section 8, then step 0 and milestone v0.2.
