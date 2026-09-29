# Getting started

With Homebrew on macOS or Linux:

```sh
brew tap GeiserX/unsent
brew trust --formula GeiserX/unsent/unsent   # newer Homebrew asks once per third-party formula
brew install unsent
```

With Go:

```sh
go install github.com/GeiserX/unsent@latest
```

Or grab a binary for macOS or Linux from the [releases page](https://github.com/GeiserX/unsent/releases).

## First run

Start your agent through `unsent`, or, in zsh or bash, run setup once so `claude`, `codex` and `pi` go through it from the next shell:

```sh
unsent claude         # one run, drafts saved as you type
unsent setup          # from the next shell, `claude` runs through unsent
unsent status         # am I protected? one line per fact
```

`unsent status` asks a new shell what each agent name runs and what bypasses it. From then on, if a window closes with a message still in the box, the next start of that agent in the same folder prints a line such as:

```text
unsent: recovered a draft from 18:42 today, 23 lines. Run `unsent restore` to copy it.
```

[Using unsent](usage.md) covers setup in detail, restoring drafts and the JSON output.
