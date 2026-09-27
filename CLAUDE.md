# CLAUDE.md

`unsent` is AutoRecover for AI agent prompts: a Go wrapper that runs an agent CLI in a pseudo-terminal and keeps the text in its input box saved on disk.

## Layout

| File | Job |
| --- | --- |
| `main.go` | CLI: `unsent <agent>`, `--as`, `list`, `show`, `restore`, `log`, `forget --log`, `version`; the recovery notice; drafts filtered by agent and folder (the resolved real path) |
| `wrap.go` | Pseudo-terminal passthrough, shadow screen, save loop, signals, Ctrl+Z suspend |
| `screen.go` | Snapshot of the shadow screen (text, dim cells, cursor) |
| `extract.go` | The `profile` type (one per agent: name, command names, box reader, paste placeholder, delete, submit and clear keys), `profileFor`, and `agentFor` (`--as`, then a base name a profile answers to, then `UNSENT_AGENT`, then the base name) |
| `agent_claude.go` | Claude Code's profile and its box reader (`claudeBox`) |
| `stitch.go` | Rebuilds drafts taller than the box: word-level edit-distance alignment of each view against the known text; un-wraps rows back into lines |
| `paste.go` | Captures bracketed pastes and expands the profile's placeholders, such as `[Pasted text #N +M lines]` |
| `store.go` | Durable writes, history, per-session lock files |
| `sent.go` | Send detection (submit and clear keys in order, each submit and the first keys after it with the screen they were typed into), `UNSENT_ON_SEND`, the per-session sent logs in `sent/` and their limits |
| `testdata/claude/<version>/` | Real Claude Code captures (raw bytes, keys with output, screens, editor copies) that `replay_test.go` replays through the reader and stitcher |

## Facts about Claude Code's box (measured on 2.1.282)

- Drawn between two full-width `─` rules; the first row starts with `❯` plus a no-break space (U+00A0), shell mode with `!`.
- An empty box shows a dim (SGR 2) hint. Dim text is how an empty box is told apart from a draft.
- Rows wrap at window width minus 4 columns; a word longer than that breaks at exactly that width, or one column short when a wide character would cross the edge, and it can start mid-row after other words.
- The box stops growing at `rows/2 - 5` rows and then scrolls; the `❯` marks the first visible row, not the first row of the draft.
- The real terminal cursor sits at the insertion point.
- Up moves the cursor row by row until it reaches the middle row of a scrolled box; each Up after that scrolls the box one row and the cursor stays mid-box. A row typed mid-box pushes the top row out of sight. So with text out of sight above, the arrow keys never bring the cursor to the top row.
- Ctrl+K deletes to the end of the screen row, not the end of the line: on a wrapped line it joins what follows.
- Some redraws are wrapped in synchronized-output marks (`ESC[?2026h` … `ESC[?2026l`); one measured session had 6 matched pairs, not one per keystroke. The screen is never read inside a pair.
- A paste shows as `[Pasted text #N +M lines]`, or `[Pasted text #N]` with no line break (a 12,000-character one-line paste measured). A 3-line paste shows inline; 6 lines and more become the placeholder.
- Right after it starts, Claude Code takes several seconds before Esc+Enter makes a new line; keys typed earlier run lines together. Scripted real-app tests wait about 9 s after the box appears.
- Claude Code runs on the terminal's alternate screen (`ESC[?1049h` at startup), so anything printed before it, such as unsent's recovery notice, is hidden until it exits.
- Ground truth for a real-app test: Ctrl+G hands the draft to `$EDITOR`. Point `EDITOR` at a script that copies its file argument and exits, and the copy is exactly what Claude Code holds; compare the saved draft to it byte for byte.
- In tmux, `send-keys Escape Enter` and `send-keys -l '\'` then `Enter` both make a new line, and `paste-buffer -p` sends a bracketed paste. tmux delivers Enter and the next letters separately, so saves see an empty last row between them (the same as a person pausing after Shift+Enter).

- Enter prints the sent message above the box and draws the empty box within about 35 ms (`sends.rec`). Just before that it sets the window title (OSC 0) to a spinner glyph, and to `✳ Claude Code` when done. The emulator ends a string sequence at a 0x9c byte even inside a UTF-8 character, and ✳ is `e2 9c b3`, so the rest of the title landed in the box; `stringSeqs` strips OSC, DCS, APC, PM and SOS before the shadow screen.
- While Claude Code is working (a request retrying), Ctrl+C interrupts the request and leaves the box as it is.

Re-measure these when a Claude Code release changes the prompt. A reader that stops matching fails safe (keeps the last draft) but saves nothing new.

## Rules

- Tests: `go test -race ./...`. Coverage target 90% (`codecov.yml`).
- `wrap_test.go` re-runs the test binary as a fake agent; keep it drawing the box the way the real one does.
- Never add network access. Drafts stay on the machine.
- Adding an agent means one profile file (`agent_<name>.go`, listed in `profiles`) plus tests built from real screen captures.
- The promise: no text the user did not delete is ever lost. Text in view is exact; text out of sight is inferred. So once a draft has been stitched, `keepOld` keeps a safety copy (`store.keepVersion`, capped per session) before any save that loses text (`lostChars`). The delete-key count (`deleteLog`, each key spent once) only guides the stitcher; it never permits a loss. Any change must keep that true.
- The stitcher's fuzz tests (`TestStitchFuzz*`) drive a simulated box through thousands of random edits, with delete keys fed exactly, through the real `deleteLog` on a simulated clock, and as Ctrl+W. With unique words they check the promise at every save; they also hold the exact-draft rate above three floors per mode: the same words, the same bytes with one allowed difference (a line break joined into a space at a full row), and identical with none. Check a stitcher change against real Claude Code too: set `UNSENT_DEBUG_DIR` and every save's view and result is logged to `views.jsonl` there, ready to replay.
- A box that empties goes to history unless a send is certain (`session.look`): a submit key was the last key typed before the box read empty (or the first keys after it were typed into an empty box), no clear key came with it, and the box was on screen when the key arrived. Doubt counts as a clear, because under `UNSENT_ON_SEND=delete` a clear taken for a send would lose text. Keep a test that goes red when the submit match is always true (`TestSendCtrlCTestCatchesAnAlwaysTrueSubmit`).
- The screen alone cannot tell text deleted at the edge of the box from text pushed out of sight; the delete keys seen on input (the profile's `keys`, split into keys that delete before and after the cursor) decide. Keep that list in step with the agent's keybindings.

*Generated by [LynxPrompt](https://lynxprompt.com) CLI*
