# Development

```sh
go test -race ./...
```

The tests include a fake agent that draws its box the way Claude Code does. Typing, pasting, Ctrl+C, Ctrl+Z and a closed window all run end to end without Claude Code installed. Codex's and pi's recorded sessions in `testdata/codex/` and `testdata/pi/` replay through their readers, so their tests don't need Codex or pi installed either.

`unsent capture claude` (or `unsent capture codex`, `unsent capture pi`), run from a checkout of this repo, records a test fixture: every key and all of the agent's output go to `testdata/<agent>/<version>/capture-<time>.rec`, the window sizes to a `.json` beside it, and each Ctrl+G saves the box as an editor copy instead of opening your editor. Setting `UNSENT_DEBUG_DIR=<folder>` logs the same keys and output there for any session, next to each save's view. Both are off unless you ask, and their files are `0600`, but they hold everything you type: check them before you share or commit them.

Releases are described in [RELEASING.md](RELEASING.md).
