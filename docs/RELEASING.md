# Releasing unsent

## Cut a release

1. On `main`, with CI green, pick the next version. The binary gets its version from the tag, so there is no version file to edit.
2. Tag and push:

   ```sh
   git tag -a v0.3.0 -m "unsent v0.3.0: <one line on what it brings>"
   git push origin v0.3.0
   ```

A tag with a suffix, such as `v0.3.0-rc1`, is a prerelease.

## What the release workflow does

[`release.yml`](../.github/workflows/release.yml) runs on every `v*.*.*` tag. It runs goreleaser v2 with [`.goreleaser.yaml`](../.goreleaser.yaml), which:

- builds `unsent` for macOS and Linux, amd64 and arm64, with `CGO_ENABLED=0` and the tag stamped into `unsent version`;
- packs each build into `unsent_<version>_<os>_<arch>.tar.gz` with `LICENSE` and `README.md`, and writes `checksums.txt`;
- creates the GitHub release with those files and a changelog from GitHub;
- writes the Homebrew formula and pushes it to the tap (next section).

## The Homebrew tap

The formula is `Formula/unsent.rb` in [GeiserX/homebrew-unsent](https://github.com/GeiserX/homebrew-unsent). goreleaser generates it from the `brews` block. It downloads the release tarball for the machine and installs the `unsent` binary. Its test checks that `unsent version` prints the formula's version. Users install with:

```sh
brew trust --formula GeiserX/unsent/unsent   # newer Homebrew asks once per third-party formula
brew install GeiserX/unsent/unsent
```

Pushing to the tap needs the `TAP_GITHUB_TOKEN` repository secret, a fine-grained token with Contents read and write on `GeiserX/homebrew-unsent` only. The workflow's own `GITHUB_TOKEN` cannot write to another repo.

With the secret set, the release commits the new formula to the tap. Prerelease tags skip the tap, so brew users never get an RC.

With the secret missing or empty, goreleaser skips the tap step and the release still succeeds. It writes the formula only to `dist/homebrew/Formula/unsent.rb` on the runner, which is thrown away, so the tap keeps its old version until a release runs with the secret set.

`brews` is deprecated in goreleaser v2, so `goreleaser check` warns and exits 2 even though the config is valid. goreleaser removes deprecated options only on a major, and the workflow pins `~> v2`. The replacement is a cask, but macOS quarantines cask binaries, so a cask needs notarization first.

`goreleaser check` also passes a broken template, so CI's `release-snapshot` job runs `goreleaser release --snapshot --clean` on pushes to `main` and on pull requests, without the tap token, and fails if `dist/homebrew/Formula/unsent.rb` is missing. Snapshot mode publishes nothing, so it cannot test the token gate itself. To run the same check locally:

```sh
goreleaser release --snapshot --clean && test -s dist/homebrew/Formula/unsent.rb
```

## After the release

- Check the release run and the release page for 4 tarballs and `checksums.txt`.
- If the tap was pushed, run `brew install GeiserX/unsent/unsent && unsent version` on a clean Mac.
