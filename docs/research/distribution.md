# unsent: distribution, one-command setup, updates, signing and naming (verified)

Tags on every claim: **[measured]** = run on this Mac (by the lane, by the verifier, or both, as noted); **[source]** = a file in the repo or on this machine, with a line; **[docs]** = upstream docs, cited; **[guess]** = inference. "(lane)" means the lane measured it and the verifier did not re-run it; "(re-checked)" means the verifier reproduced it on 2026-09-26.

Repo state read: `GeiserX/unsent` HEAD `064c7f43eb6eb347208115ab849c7d322de4b256`, tags v0.1.0 and v0.1.1 [measured, re-checked].

## Plain version

- **Name:** keep `unsent`. `unsent-cli` is free everywhere, but an email product called Unsent (unsent.dev) ships SDKs in 8 languages and has no CLI, so `unsent-cli` reads like their CLI. [measured facts, re-checked; the conclusion is a guess]
- **Install:** a Homebrew **formula** (not a cask) in a per-project tap `GeiserX/homebrew-unsent`, plus the existing `go install` and release tarballs, plus `.deb`/`.rpm` from goreleaser `nfpms`. Skip npm, curl-pipe-sh, casks and notarization for now.
- **Setup:** `unsent setup` writes one marked block defining `function claude { ... }` (not `claude() { ... }`), only for agents this build has a reader for. `unsent setup --undo` removes it. Drafts are never deleted.
- **Updates:** the package manager only. No self-updater or update check, because the repo forbids network access.
- **Signing:** not needed for a formula, `go install` or curl installs. Only browser-downloaded tarballs and casks need notarization.

---

## 1. Naming

### Who holds the names (2026-09-26)

| Place | `unsent` | `unsent-cli` | Tag |
|---|---|---|---|
| GitHub repo | `GeiserX/unsent`, created 2026-09-25T17:47Z, 1 star. Org `unsent-dev` holds the email SDKs | free | measured (re-checked: created date, stars, org) |
| Homebrew core formula / cask | free (formulae.brew.sh API 404 for both) | free | measured (re-checked) |
| Homebrew taps (code search `filename:unsent.rb path:Formula`) | 0 | 0 | measured (lane) |
| npm | unscoped `unsent` free (404); scope `@unsent` belongs to the email company (`@unsent/sdk`) | free (404) | measured (re-checked the 404s; `@unsent` scope from lane) |
| crates.io | taken: "Rust SDK for the Unsent API", 1.0.3, repo `souravsspace/unsent-rust` | free | measured (re-checked) |
| PyPI | taken: Python SDK 1.0.3, homepage unsent.dev | free | measured (re-checked) |
| RubyGems | taken (200) | free | measured (re-checked the 200) |
| pkg.go.dev | `github.com/souravsspace/unsent-go/pkg/unsent` | none | measured (lane) |
| Debian / Ubuntu, AUR, nixpkgs | absent | absent | measured (lane) |
| Domains | `unsent.dev` is live, title "Unsent · Email for developers", logos for Node, Python, PHP, Go, Ruby, Java, Rust, Elixir. `unsent.io` parked; `unsent.app`, `unsent.tools` registered | `unsent-cli.dev` no DNS | measured (re-checked unsent.dev title; the rest lane) |

The `unsent-dev` org repos are `unsent-typescript unsent-python unsent-go unsent-ruby unsent-java unsent-php unsent-elixir unsent-rust` [measured, re-checked].

### Recommendation: keep `unsent`

1. **The command is `unsent` either way.** Agent CLIs name the package `X-cli` and the binary `X`: `@google/gemini-cli` installs bin `gemini`; the Homebrew `copilot-cli` cask installs `/opt/homebrew/Caskroom/copilot-cli/0.0.422/copilot` [measured: copilot path re-checked; gemini from lane]. So `-cli` would change only the repo and package names.
2. **The suffix makes the one real collision worse.** `unsent-cli` is exactly the name the email company's missing CLI would get [guess].
3. **The registries where `unsent` is taken (crates.io, PyPI, RubyGems) are not channels for a Go binary.** Every channel we would use (Homebrew core and tap, npm unscoped, GitHub, Debian, AUR, nix) is free [measured].
4. **A rename is cheap only now.** Downloads so far: v0.1.0 has 7 asset downloads (2 + 5), v0.1.1 has 1, and the repo has 1 star [measured, re-checked; the lane's "downloadCount 0" was wrong, and at least one of these is probably the lane's own download]. A later rename also changes the Go module path: new tags would declare `module github.com/GeiserX/unsent-cli`, and `go install github.com/GeiserX/unsent@latest` would fail with a module path mismatch [docs: https://go.dev/ref/mod#go-mod-file-module, not re-checked].
5. **If the collision still worries you, pick a distinct word, not a suffix.** Trademarks were not checked [guess].

---

## 2. Install channels

### What ships today
- `.goreleaser.yaml`: darwin and linux, amd64 and arm64, `CGO_ENABLED=0`, ldflags `-s -w -X main.version=...`, tarballs with LICENSE and README, `checksums.txt`. No `brews`, `homebrew_casks`, `nfpms` or `notarize` block [source: `.goreleaser.yaml`, re-checked].
- `release.yml` runs goreleaser `~> v2` on `ubuntu-latest` on `v*.*.*` tags with only `GITHUB_TOKEN` and `permissions: contents: write` [source: `.github/workflows/release.yml`, re-checked].
- v0.1.1: 4 tarballs of 1.3 to 1.5 MB; the darwin/arm64 binary is 3.3 MB and prints `unsent 0.1.1` [measured (lane)].
- `go install github.com/GeiserX/unsent@latest` works, installs v0.1.1, 63 s on a loaded box, 5.8 MB unstripped binary [measured (lane)]. Needs Go 1.26 or `GOTOOLCHAIN=auto` (`go.mod`: `go 1.26.0`) [source, re-checked; toolchain download behaviour docs: https://go.dev/doc/toolchain].

### Recommended order

| Channel | Verdict | Why |
|---|---|---|
| **Homebrew formula in `GeiserX/homebrew-unsent`** | Do first | Matches the existing per-project taps (`homebrew-catsup`, `-lynxprompt`, `-vpn-bypass`, `-mcp-upgrade`, `-agenttap`) [measured (lane)]. A formula binary is not quarantined, so it needs no Apple signing (section 5). |
| `go install` | Keep | Works [measured (lane)]. |
| GitHub release tarballs | Keep | Already there. Browser downloads hit Gatekeeper (section 5). |
| `.deb` / `.rpm` / `.apk` via goreleaser `nfpms` | Add | Same release run [docs: https://goreleaser.com/customization/nfpm/, not re-checked]. |
| AUR via goreleaser `aurs` | Optional | Needs an AUR SSH key secret [docs: https://goreleaser.com/customization/aur/, not re-checked]. |
| homebrew-core | Later | Homebrew's policy: "at least 30 forks, 30 watchers **or** 75 stars", or "at least 90 forks, 90 watchers or 225 stars for a self-submission by the repository owner", and "a code repository less than 30 days old is normally not eligible" [docs: Homebrew/brew `docs/Package-Acceptance-Policy.md`, Notability section, re-checked]. Any one threshold is enough. The repo was created 2026-09-25, so ~2026-10-25 is the earliest date, and a self-submission needs 225 stars. Core builds from source, so no signing. |
| curl \| sh into `~/.local/bin` | Skip for now | curl downloads get `com.apple.provenance` but no `com.apple.quarantine`, and the binary runs [measured (lane), with the mcp-upgrade darwin_arm64 release]. Works, but duplicates brew and go. |
| npm (unscoped `unsent`) | Skip for now | Name free [measured]. Agent CLIs ship native binaries as per-platform `optionalDependencies` (`@openai/codex`, `@anthropic-ai/claude-code`, `@biomejs/biome`) [measured (lane)], but codex's `bin` is a JS launcher (`bin/codex.js`), which would put a Node process between the terminal and unsent for the whole session. If npm is ever added, copy biome/esbuild, where `bin` is the native binary [guess on the job-control risk]. |

### Goreleaser Homebrew trap
- Goreleaser lists `brews` as deprecated "since v2.10 (soft) since v2.16" (hard) in favour of `homebrew_casks`, and states "Deprecated options are only removed on major versions of GoReleaser" [docs: https://goreleaser.com/deprecations/, re-checked]. `release.yml` pins `~> v2`, so a `brews` block keeps working until a v3 is adopted [source + docs]. The lane only said "deprecated in v2.10"; the hard-deprecation step at v2.16 is added here.
- Casks get quarantined: `/opt/homebrew/Caskroom/copilot-cli/0.0.422/copilot` carries `com.apple.quarantine: 0181;...;Homebrew\x20Cask` [measured, re-checked]. Formula binaries do not: `Cellar/gh/*/bin/gh` has only `com.apple.provenance` and is `Signature=adhoc`, `flags=0x20002(adhoc,linker-signed)` [measured, re-checked]. The lane also reported tmux the same way [measured (lane)].
- Goreleaser's cask docs offer an `xattr -dr com.apple.quarantine` postflight with a warning that it bypasses macOS security [docs: https://goreleaser.com/customization/publish/homebrew_casks/, not re-checked].
- Homebrew 5.0.0: "`--no-quarantine` and `--quarantine` flags have been deprecated", "Casks without codesigning are deprecated", and "We will disable all Homebrew/homebrew-cask casks that fail Gatekeeper checks in September 2026" [docs: https://brew.sh/2025/11/12/homebrew-5.0.0/, re-checked]. The post names only the official cask repo; third-party taps and formulae are not mentioned. Whether a later Homebrew extends this to third-party casks is unknown [guess].
- **So:** ship a formula. Either keep a `brews:` block, as `mcp-upgrade/.goreleaser.yml` does with `skip_upload: true` [source (lane)], or hand-write a small formula. A cask is an option only after notarization exists.
- Pushing to the tap repo needs a token with write access to it; the workflow's `GITHUB_TOKEN` is scoped to its own repo [docs: GitHub Actions token scope, not re-checked]. That mcp-upgrade updates its tap by hand for this reason is a guess [guess].

---

## 3. One-command setup and uninstall

### Facts that constrain the design
- **Only Claude Code has a reader.** `extractorFor` switches on `filepath.Base(command)` and returns `claudeBox` for `claude`, nil otherwise [source: `extract.go:38-45`, re-checked]. Any other agent prints `unsent: no reader for "X" yet, running it without saving drafts` [source: `wrap.go:76`, re-checked]. So "wrap every installed agent" means every installed agent this build has a reader for. Today: `claude` only.
- **Non-tty calls pass straight through.** `wrap` calls `exec.LookPath(args[0])` and, if stdin or stdout is not a terminal, `execAgent` (a `syscall.Exec` with `os.Environ()`) [source: `wrap.go:31`, `wrap.go:36-45`, `wrap.go:207-209`, re-checked]. So `git diff | claude -p` is safe behind a wrapper.
- **Agents installed on this Mac:** `claude` and `codex` (symlinks in `~/.local/bin`), `cursor-agent`, `copilot` (Homebrew cask) [measured (lane); copilot cask path re-checked]. No fish installed.

### Shell traps
1. **`claude() { ... }` fails when `alias claude=...` already exists.** The alias expands inside the definition. zsh 5.9: `defining function based on alias 'claude'` then `parse error near '()'`; bash 3.2 and 5.3: `syntax error near unexpected token '('` [measured, re-checked in `/bin/bash` 3.2, Homebrew bash 5.3, `/bin/zsh` 5.9 with `--norc`/`-f` and `expand_aliases` on]. **Correction:** the lane called this "silently not defined". It is not silent: the error prints at shell start. And it is worse than a missing function: in the re-check, **no line after the bad definition ran in any of the three shells**, so the rest of the rc file is skipped too [measured, re-checked].
2. **`function claude { ... }` works with or without the alias, in either order, and composes with it.** `alias claude='claude --flag'` plus the setup block gives `unsent claude --flag hello` whether the alias comes before or after the block [measured, re-checked in all three shells].
3. **An alias that points at a path bypasses the function.** `alias claude=/path/claude` runs the agent unwrapped [measured (lane); the verifier's attempt passed in bash 5.3 and zsh, but that run read the user's rc files, so treat it as lane-only]. Setup should detect this and warn.
4. **Launchers that start the agent through `env` or `command` bypass shell functions.** the maintainer's own `cc` and `ccr` end in `env "${_cc_clean_env[@]}" ... claude ... "$@"` [source: the Claude Code zsh shell snapshot under `$CLAUDE_CONFIG_DIR/shell-snapshots/`, lines 166 and 173, re-checked]. `env` runs the binary found on PATH, not a shell function [measured (lane); the verifier's re-check actually reached the real binary, see Verification notes]. Setup can only warn; the fix is `env ... unsent claude "$@"`.
5. **PATH shims loop forever.** A `claude` shim ahead of the real one that runs `exec unsent claude` makes `exec.LookPath("claude")` find the shim again, since unsent passes PATH through unchanged and never excludes itself [source: `wrap.go:31`, `wrap.go:209`, re-checked]. The lane stopped it with a counter after 26 re-entries using the real unsent built from HEAD [measured (lane)]. So no shim directory. Never replace the agent binary either: the Claude Code shell snapshot runs `~/.local/bin/claude` as `rg` through `ARGV0=rg "$_cc_bin"` [source: snapshot lines 600-605, re-checked]. That call uses an absolute path, so a `claude` shell **function** does not affect it; only replacing the binary would.
6. **Claude Code copies user functions and aliases into its Bash tool** (the snapshot has `# Functions` and `# Aliases` sections) [source, re-checked]. A `claude` function will be live inside agent shells. Harmless for non-tty calls, which pass straight through [source]; whether the Bash tool is ever a tty was not checked [guess: it is not].
7. **Removing unsent while the block stays is safe.** With `unsent` gone from PATH, the function falls back to `command claude` and the agent runs with its arguments [measured, re-checked in all three shells].
8. **An `eval "$(unsent init zsh)"` line costs one process start per shell,** ~100-150 ms median on this box under load 17-24 (`/bin/echo` 147 ms, `unsent version` 143 ms, `zsh -f -c true` 131 ms) [measured (lane)]. The builtin-only block costs nothing extra [guess, follows from using `command -v`].

### Recommended design
`unsent setup [zsh|bash|fish]` (defaults to `$SHELL`) writes one marked block, idempotent (re-running rewrites it); `unsent setup --undo` deletes it. The agent list is baked in from this build's readers, so after an upgrade that adds a reader the user runs `unsent setup` again. The block, re-checked in bash 3.2, bash 5.3 and zsh 5.9 [measured, re-checked]:

```sh
# >>> unsent >>>  (remove this block to undo; `unsent setup` rewrites it)
for _unsent_a in claude; do
  if command -v "$_unsent_a" >/dev/null 2>&1; then
    eval "function $_unsent_a { if command -v unsent >/dev/null 2>&1; then unsent $_unsent_a \"\$@\"; else command $_unsent_a \"\$@\"; fi; }"
  fi
done
unset _unsent_a
# <<< unsent <<<
```

- **zsh:** `${ZDOTDIR:-$HOME}/.zshrc` [guess, standard].
- **bash:** `~/.bashrc`. macOS terminals start login shells, which read `~/.bash_profile` and not `~/.bashrc`, so setup should warn if `.bash_profile` does not source `.bashrc` [docs: bash manual, INVOCATION, not re-checked].
- **fish:** a drop-in `~/.config/fish/conf.d/unsent.fish` with `function claude --wraps claude; ...; end`; undo deletes the file [docs: fishshell.com conf.d and `function --wraps`; not measured, no fish on this Mac].
- **Warnings setup prints:** aliases pointing at a path (trap 3); user functions that call the agent through `env`, `command` or a full path (trap 4).
- **Bypass:** the shell's own `command claude` [measured (lane)].
- **Uninstall:** `unsent setup --undo`, then `brew uninstall unsent`; either order is safe (trap 7). Drafts in `~/.local/state/unsent` (or `XDG_STATE_HOME`/`UNSENT_HOME`) are user data: print the path, never delete them [source: `README.md:88`, `store.go:63`]. A `--purge` flag is YAGNI.

---

## 4. Auto-update
- **No self-updater and no update check.** The repo rule "Never add network access. Drafts stay on the machine." [source: `CLAUDE.md:38`, re-checked] and the README promise "`unsent` makes no network connections" [source: `README.md:88`, re-checked] rule both out. Updates come from `brew upgrade`, `go install ...@latest` or the distro package manager.
- **Upgrading mid-session:** running sessions keep the old binary; replacing the file on disk does not affect a running process [guess, standard Unix behaviour when the installer replaces rather than overwrites in place].
- **Upgrade risk: draft format has no schema version.** `record` has no format version (its `Version bool` field marks a safety copy, not a format) [source: `store.go:25-40`, re-checked]. Go's `json.Unmarshal` ignores unknown fields, so adding fields is safe [docs: encoding/json]. Two breaking cases, with different outcomes:
  - A **type change** (e.g. `draft` no longer a string) makes `json.Unmarshal` fail, and the file is skipped by `load` and by `orphans` [source: `store.go:294`, `store.go:316`, re-checked]. Old drafts become invisible but stay on disk.
  - **Added by the verifier, and worse:** a **renamed field** (e.g. `draft` becomes `text`) unmarshals without error into an empty `Draft`. `orphans()` then treats a dead session's file as an empty box and calls `s.remove`, which `os.Remove`s the draft file and its lock [source: `store.go:324-326`, `store.go:146-149`, re-checked]. An upgrade that renames a field would **delete** old drafts the first time `orphans()` runs, which breaks the repo's promise that no text the user did not delete is lost. Rule: never rename or retype a record field without a format version and a migration.

## 5. macOS signing and notarization
- **Go signs darwin/arm64 binaries ad hoc at link time, even when cross-built on Linux.** The goreleaser-on-Ubuntu mcp-upgrade release shows `flags=0x20002(adhoc,linker-signed)`; unsent v0.1.1 shows `Signature=adhoc`, `Identifier=a.out`. darwin/amd64 builds are unsigned (`source=no usable signature`). `spctl --assess --type execute` rejects all of them, and `syspolicy_check distribution` reports the ad-hoc signature as not suitable for distribution [measured (lane)]. The same `adhoc,linker-signed` flags appear on the Homebrew `gh` bottle [measured, re-checked].
- **When that matters:**
  - Homebrew formula: no quarantine [measured, re-checked on gh].
  - `go install`: built locally, ad-hoc signed [measured (lane)].
  - curl: no quarantine [measured (lane)].
  - Browser download of a release tarball: quarantined, so Gatekeeper blocks the first run [docs: Apple; not executed, to avoid a dialog].
  - Casks: quarantined [measured, re-checked on copilot-cli].
- **How, if wanted:** goreleaser OSS `notarize.macos` (anchore/quill) runs on Linux and needs a Developer ID `.p12` plus password and an App Store Connect `.p8` key with issuer ID and key ID [docs: https://goreleaser.com/customization/notarize/, not re-checked]. A bare Mach-O cannot have a ticket stapled, so Gatekeeper checks notarization online on first run [docs: Apple, not re-checked]. The maintainer has an Apple Developer account.
- **Recommendation:** leave it off until there is a cask or a real browser-download audience. It fixes only that path.

---

## Detection recipe

Name checks (anonymous HTTP; `unset GITHUB_TOKEN GH_TOKEN` before any `gh` command):
- npm: `curl -s -o /dev/null -w '%{http_code}' https://registry.npmjs.org/<name>` (404 = free)
- crates.io: `curl -A research https://crates.io/api/v1/crates/<name>` (a User-Agent is required)
- PyPI: `https://pypi.org/pypi/<name>/json`; RubyGems: `https://rubygems.org/api/v1/gems/<name>.json`
- Homebrew: `https://formulae.brew.sh/api/formula/<name>.json` and `.../api/cask/<name>.json`
- Taps: `gh api -X GET search/code -f q='filename:<name>.rb path:Formula'`
- GitHub: `gh api -X GET search/repositories -f q='<name> in:name'`
- AUR: `https://aur.archlinux.org/rpc/v5/info?arg[]=<name>`; nixpkgs: `gh api repos/NixOS/nixpkgs/contents/pkgs/by-name/<2 letters>/<name>`

Agent detection for setup (no process start): `command -v "$a"`, only for agents with a reader in `extractorFor` (`extract.go:38`). Today that is `claude`.

Setup warnings: an `alias <agent>` whose value starts with `/` or `~`; functions whose body calls the agent through `env`, `command` or an absolute path (`whence -f` / `type <fn>`, then grep for the agent name).

Quarantine and signing: `xattr -l <bin>` (look for `com.apple.quarantine`), `codesign -dv <bin>`, `spctl --assess --type execute -vv <bin>`.

**Testing trap for shell experiments (new, found during verification):** run shell tests with `env -i PATH=<fake bin>:/usr/bin:/bin zsh -f` or `bash --norc --noprofile`. An interactive `zsh -i` sources `~/.zshrc`, which prepends `~/.local/bin` (line 143) ahead of any fake PATH, so a test that calls `env claude` or `command claude` reaches the **real** Claude Code.

## Risks
- Brand collision with Unsent (unsent.dev), which owns the name on crates.io, PyPI, RubyGems, the npm `@unsent` scope and pkg.go.dev. Trademark status not checked.
- Goreleaser `brews` is hard-deprecated since v2.16; it stays until a goreleaser major, and `release.yml` pins `~> v2`. Casks, the replacement, get quarantined unless notarized.
- Tap auto-publishing needs a cross-repo token secret; `GITHUB_TOKEN` cannot push to the tap.
- A renamed draft field would make `orphans()` delete old drafts; a retyped field would hide them (section 4).
- Shell-function wrapping misses agents started through `env`, `command`, an absolute-path alias, or tmux/IDE launchers, so users may think they are covered when they are not. Setup must say so.
- A `claude() {` form in the setup block, under an existing alias, aborts the rest of the user's rc file.
- A PATH shim or replacing the agent binary causes an exec loop and would break Claude Code's `ARGV0=rg` trick.

## Open questions
- Name: does the maintainer still want a different name, knowing `unsent-cli` reads as the unsent.dev CLI? If yes, rename before any tap or package ships, and use a distinct word.
- Should setup do anything about the maintainer's own launchers (`cc`, `ccr`, `cx`) beyond warning? The fix is editing them to call `env ... unsent claude`.
- Notarization now or later? Only browser tarballs and a future cask need it.
- Fish behaviour is docs-only; fish is not installed here.
- The lane's first open question reported a stray relayed user request unrelated to this lane ("delete that warning"). It was not acted on here either; the orchestrator should check where it belongs.

## Verification notes

What I checked:
- **Source, all re-read at HEAD 064c7f4:** `extract.go:38-45` (only `claude` has a reader), `wrap.go:31` (`LookPath`), `wrap.go:36-45` (non-tty exec), `wrap.go:76` (no-reader message), `wrap.go:207-209` (`syscall.Exec` with the environment unchanged), `store.go:25-40` (no format version), `store.go:294` and `316` (skip on unmarshal error), `.goreleaser.yaml`, `release.yml`, `go.mod`, `CLAUDE.md:38`, `README.md:88`. All held.
- **Registries:** npm `unsent` and `unsent-cli` 404; PyPI and RubyGems `unsent` 200; crates.io `unsent` 1.0.3 Rust SDK for the Unsent API; Homebrew formula and cask 404; `unsent-dev` org has the 8 SDK repos; unsent.dev title "Unsent · Email for developers". All held.
- **Docs:** Homebrew Package-Acceptance-Policy Notability section (held; clarified that the thresholds are "or"); Homebrew 5.0.0 post (held); goreleaser deprecations page (held, added the v2.16 hard deprecation and the "removed only on majors" rule).
- **On-disk quarantine and signature:** copilot-cli cask binary has `com.apple.quarantine` from Homebrew Cask; the `gh` formula binary has only `com.apple.provenance` and an ad-hoc linker signature. Held.
- **Shell traps 1, 2 and 7** re-measured with fake `claude`/`unsent` scripts in `/bin/bash` 3.2, Homebrew bash 5.3 and `/bin/zsh` 5.9 with no rc files. Held, and trap 1 is worse than stated: the parse error aborts the rest of the rc file.
- **Shell snapshot:** the `cc`/`ccr` `env ... claude` lines and the `ARGV0=rg "$_cc_bin"` call are there.

What I changed:
- Download counts: v0.1.0 has 7 and v0.1.1 has 1, not 0.
- Trap 1: an error is printed (not silent), and the rest of the rc file does not run.
- Goreleaser `brews`: soft-deprecated v2.10, hard-deprecated v2.16, removed only on a major; the `~> v2` pin bounds the risk.
- Homebrew-core thresholds: any one of forks, watchers or stars is enough; a self-submission needs 225 stars.
- Draft format risk: added that a renamed field makes `orphans()` delete old drafts (`store.go:324-326` into `store.go:146-149`), which is worse than the lane's "hide".
- Trap 5: the ARGV0 call uses an absolute path, so a `claude` function does not break it; only replacing the binary does.
- Every claim is now tagged, and claims I did not re-run are marked "(lane)" or "not re-checked": timings, the shim loop count, curl provenance, goreleaser nfpm/aur/notarize pages, the Go module path doc, the fish docs.
