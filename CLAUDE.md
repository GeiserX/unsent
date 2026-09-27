# CLAUDE.md

`unsent` is AutoRecover for AI agent prompts: a Go wrapper that runs an agent CLI in a pseudo-terminal and keeps the text in its input box saved on disk.

## Layout

| File | Job |
| --- | --- |
| `main.go` | CLI: `unsent <agent>`, `--as`, `list`, `show`, `restore`, `log`, `forget --log`, `capture`, `version`; the recovery notice; drafts filtered by agent and folder (the resolved real path) |
| `wrap.go` | Pseudo-terminal passthrough, shadow screen, save loop, signals, Ctrl+Z suspend; `UNSENT_OFF`; a panic anywhere in reading or saving turns the session into plain passthrough (`session.guard`); the lines printed after the agent exits (`exitLines`: no reader, saving stopped, box never read), then the recovery notice again, asked afresh |
| `screen.go` | Snapshot of the shadow screen (text, dim cells, cursor) |
| `extract.go` | The `profile` type (one per agent: name, command names, box reader, paste placeholder, delete, submit and clear keys), `profileFor`, and `agentFor` (`--as`, then a base name a profile answers to, then `UNSENT_AGENT`, then the base name) |
| `agent_claude.go` | Claude Code's profile and its box reader (`claudeBox`) |
| `stitch.go` | Rebuilds drafts taller than the box: word-level edit-distance alignment of each view against the known text; un-wraps rows back into lines |
| `paste.go` | Captures bracketed pastes and expands the profile's placeholders, such as `[Pasted text #N +M lines]`; puts back the middle of a long draft the agent cut out of the box |
| `store.go` | Durable writes, history, per-session lock files |
| `capture.go` | The raw log (`rawLog`: keys and output in `script -r` format, window sizes beside it, files `0600`), written only under `UNSENT_DEBUG_DIR` or by `unsent capture`, which records a fixture bundle into `testdata/<agent>/<version>/` with an editor copy per Ctrl+G |
| `sent.go` | Send detection (submit and clear keys in order, each submit and the first keys after it with the screen they were typed into), `UNSENT_ON_SEND`, the per-session sent logs in `sent/` and their limits |
| `testdata/claude/<version>/` | Real Claude Code captures (raw bytes, keys with output, screens, editor copies) that `replay_test.go` replays through the reader and stitcher |

## Facts about Claude Code's box (measured on 2.1.282)

- Drawn between two full-width `─` rules; the first row starts with `❯` plus a no-break space (U+00A0), shell mode with `!`.
- An empty box shows the marker, then Claude Code's own cursor, a reverse-video space. A configured install also shows a dim (SGR 2) `Try "…"` hint there; a clean config shows none. Nothing but spaces or dim text after the marker is how an empty box is told apart from a draft.
- Rows wrap at window width minus 4 columns; a word longer than that breaks at exactly that width, or one column short when a wide character would cross the edge, and it can start mid-row after other words.
- The box stops growing at `rows/2 - 5` rows and then scrolls; the `❯` marks the first visible row, not the first row of the draft.
- The real terminal cursor sits at the insertion point.
- Up moves the cursor row by row until it reaches the middle row of a scrolled box; each Up after that scrolls the box one row and the cursor stays mid-box, until the first row of the draft is in view. Down does the same the other way. Ctrl+Home, Ctrl+End and Alt+> do not move it; none of the keys tried made the box jump. A row typed mid-box pushes the top row out of sight. So with text out of sight above, the arrow keys never bring the cursor to the top row.
- Ctrl+K deletes to the end of the screen row, not the end of the line: on a wrapped line it joins what follows.
- Ctrl+D deletes the character after the cursor, Alt+D (`ESC d`) the word after it and Alt+Backspace (`ESC 0x7f`) the word before it. Punctuation ends a word: `alpha-19` is two words.
- Every redraw after a key is wrapped in synchronized-output marks (`ESC[?2026h` … `ESC[?2026l`), one pair per keystroke; the first paint at startup is not. The screen is never read inside a pair.
- A paste of 4 or more lines, or over 800 characters, shows as `[Pasted text #N +M lines]`, where M counts line breaks, or `[Pasted text #N]` with no line break. Shorter pastes, such as 3 lines or 800 characters, go in as typed text. Claude Code turns each tab into 4 spaces before it counts, so a 750-character row with 150 tabs collapses. In a window under 12 rows a paste collapses at more than `rows - 10` line breaks: at 11 rows a 3-line paste collapses and a 2-line one does not, at 10 rows a 2-line one does. A pasted path to an image file shows as `[Image #N]`. N counts up across pastes, images and cut middles for as long as Claude Code runs. `[Audio #N]` is in Claude Code's placeholder pattern but was not seen.
- A box over 10,000 characters keeps its first and last 500 and shows the middle as `[...Truncated text #N +M lines...]`. Measured by pasting a 12,000-character text twice: the second paste expands it to the whole text, and Claude Code then cuts it. After Ctrl+G and back, the same middle shows as `[Pasted text #N +M lines]`, same N and M.
- Right after it starts, Claude Code takes several seconds before Esc+Enter makes a new line; keys typed earlier run lines together. Scripted real-app tests wait about 9 s after the box appears.
- Claude Code runs on the terminal's alternate screen (`ESC[?1049h` at startup), so anything printed before it, such as unsent's recovery notice, is hidden until it exits.
- Ground truth for a real-app test: Ctrl+G hands the draft to `$EDITOR`. Point `EDITOR` at a script that copies its file argument and exits, and the copy is exactly what Claude Code holds; compare the saved draft to it byte for byte.
- In tmux, `send-keys Escape Enter` and `send-keys -l '\'` then `Enter` both make a new line, and `paste-buffer -p` sends a bracketed paste. tmux delivers Enter and the next letters separately, so saves see an empty last row between them (the same as a person pausing after Shift+Enter).

- Enter prints the sent message above the box and draws the empty box within about 35 ms (`sends.rec`). Just before that it sets the window title (OSC 0) to a spinner glyph, and to `✳ Claude Code` when done. The emulator ends a string sequence at a 0x9c byte even inside a UTF-8 character, and ✳ is `e2 9c b3`, so the rest of the title landed in the box; `stringSeqs` strips OSC, DCS, APC, PM and SOS before the shadow screen.
- While Claude Code is working (a request retrying), Ctrl+C interrupts the request and leaves the box as it is.

Re-measure these when a Claude Code release changes the prompt, and set the profile's `verified` to the version the fixtures come from. A reader that stops matching fails safe (keeps the last draft) but saves nothing new, and says so on exit with the running and the verified version. The running version is asked at startup, and only from a command the profile answers to: with `--as` the command may be npx or node.

## Rules

- Tests: `go test -race ./...`. Coverage target 90% (`codecov.yml`).
- `wrap_test.go` re-runs the test binary as a fake agent; keep it drawing the box the way the real one does.
- Never add network access. Drafts stay on the machine.
- Adding an agent means one profile file (`agent_<name>.go`, listed in `profiles`) plus tests built from real screen captures.
- The promise: no text the user did not delete is ever lost. Text in view is exact; text out of sight is inferred. So once a draft has been stitched, `keepOld` keeps a safety copy (`store.keepVersion`, capped per session) before any save that loses text (`lostChars`). The delete-key count (`deleteLog`, each key spent once) only guides the stitcher; it never permits a loss. Any change must keep that true.
- The stitcher's fuzz tests (`TestStitchFuzz*`) drive a simulated box through thousands of random edits, with delete keys fed exactly, through the real `deleteLog` on a simulated clock, and as Ctrl+W. With unique words they check the promise at every save; they also hold the exact-draft rate above three floors per mode: the same words, the same bytes with one allowed difference (a line break joined into a space at a full row), and identical with none. Check a stitcher change against real Claude Code too: set `UNSENT_DEBUG_DIR` and every save's view and result is logged to `views.jsonl` there, and the session's raw keys and output to `raw-<id>.rec`, ready to replay. `unsent capture claude` records the same raw log as a fixture bundle, with an editor copy per Ctrl+G.
- A box that empties goes to history unless a send is certain (`session.look`): a submit key was the last key typed before the box read empty (or the first keys after it were typed into an empty box), no clear key came with it, and the box was on screen when the key arrived. Doubt counts as a clear, because under `UNSENT_ON_SEND=delete` a clear taken for a send would lose text. Keep a test that goes red when the submit match is always true (`TestSendCtrlCTestCatchesAnAlwaysTrueSubmit`).
- The screen alone cannot tell text deleted at the edge of the box from text pushed out of sight; the delete keys seen on input (the profile's `keys`, split into keys that delete before and after the cursor) decide. Keep that list in step with the agent's keybindings.

*Generated by [LynxPrompt](https://lynxprompt.com) CLI*
