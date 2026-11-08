# gittan

gittan is short for Birgitta.

`gittan` derives an opinionated semantic version from Git history or an explicit bump selection, creates a local tag at `HEAD`, and runs configured pre- and post-bump hooks. Updating `CHANGELOG.md` is opt-in.

## Usage

Run the automatic bump from a Git working tree, or explicitly select a version component:

```sh
go run . --auto
go run . --major
go run . --minor
go run . --patch
```

Automatic mode parses commit history. Manual modes increment the selected component directly and do not inspect commit messages to choose the version; a manual bump can also update the changelog when enabled and runs the configured release hooks. Use `--dry-run` with an explicit bump flag to print only the prospective tag (and a newline), without creating a tag, changing files, or running hooks. The output is suitable for scripts:

```sh
NEXT_VERSION=$(gittan --auto --dry-run)
NEXT_MINOR=$(gittan --minor --dry-run)
go run . --auto --repo /path/to/repository
go run . --auto --prefix v
```

If automatic mode finds no release-worthy commits, it exits successfully without output in dry-run mode; a regular run prints a 👍 no-op message. Regular successful releases also print a 👍 message. Running `gittan` without flags defaults to automatic mode.

The default prefix is empty, so stable tags such as `1.2.3` are considered. Use `--prefix v` to select tags such as `v1.2.3`; `--prefix` must match the complete prefix. `gittan` examines all tags from `git tag --list` and uses the highest matching stable version. Only commits in the Git range after that tag through `HEAD` are considered. If no matching tag exists, all commits through `HEAD` are considered.

With no matching tag, the automatic bump starts from `0.0.0` and applies the highest release level found in history (`fix` yields `0.0.1`; `feat` yields `0.1.0`; a breaking change yields `1.0.0`). With an existing tag, the highest level among all commits since that tag is applied, so a major change takes precedence over minor or patch changes regardless of commit order. A manual bump also starts from `0.0.0` if no matching tag exists (`--patch` gives `0.0.1`, `--minor` gives `0.1.0`, and `--major` gives `1.0.0`). Automatic mode with no release-worthy commits is a successful no-op. Prerelease, build-metadata, malformed, and wrong-prefix tags are ignored.

`gittan` creates local tags only; it does not push, move, or delete tags. It refuses to overwrite an existing target tag. Use `--dry-run` to preview a release.

## Commit conventions

Commit messages use the Conventional Commits format. A scope in parentheses before the colon is optional:

| Commit message | Version change |
| --- | --- |
| `fix: correct parsing` or `fix(parser): handle empty input` | Patch |
| `feat(x): add search` | Minor |
| Any valid type with `!` before the colon, or a `BREAKING CHANGE:` / `BREAKING-CHANGE:` footer | Major |

Types must be followed by a colon, a space, and a non-empty description; an optional scope appears in parentheses. Types are matched case-insensitively. Other types—including `build`, `chore`, `ci`, `docs`, `perf`, `refactor`, `style`, and `test`—are allowed but do not cause a release by themselves and are excluded from generated changelogs. Any of them can still cause a major bump when marked as breaking. A breaking footer must be in a footer block separated from the header/body by a blank line and include a description. Breaking changes take precedence over `feat` and `fix` changes.

## Changelog

`--changelog` accepts three case-insensitive modes. `OFF` does not change `CHANGELOG.md` and is the default; `ON` prepends an entry containing release-worthy commits since the latest matching tag (or all release-worthy commits if no tag matches); `FULL` replaces the file with release-worthy commit subjects from all history, including commits before the latest matching tag. Non-release commits such as `chore`, `ci`, and invalid messages are excluded in both modes. Use `--changelog ON` or `--changelog FULL` without a bump flag to update the changelog without creating a tag or running hooks; these standalone entries are titled `Unreleased` or `History`. Combined with a bump flag, the new tag is the entry heading. Each generated entry is delimited by HTML comments and includes commit-type emojis plus a festive emoji in the heading.

For compatibility, `--update-changelog` maps to `ON`, `--update-changelog=false` maps to `OFF`, and `[general] update-changelog = true` in `gittan.toml` enables `ON` for releases. The `--changelog` mode overrides the TOML setting. `--dry-run` requires an explicit bump flag and cannot be combined with either changelog flag. Automatic no-ops and dry runs do not write a changelog.

## Pre- and post-bump hooks

When a release is needed, pre-bump commands run before the tag is created; post-bump commands run after the tag and any enabled changelog have been written. Commands in each phase run in order:

```toml
[general]
update-changelog = true

[pre-bump]
commands = [
  "printf 'Preparing release %s\\n' %TAG%",
]

[post-bump]
commands = [
  "git push origin %TAG%",
  'git add CHANGELOG.md && git commit -m "chore: update changelog"',
  'git push origin main',
]
```

Commands run through `sh -c` from the repository root. `%TAG%` is replaced with the prospective tag before a pre-bump hook and the actual created tag before a post-bump hook; it is shell-quoted, so use it as an unquoted shell token. Hooks also receive `GITTAN_TAG` and `GITTAN_REPO` in their environment. If `gittan.toml` is absent, `.gittan/pre-bump.sh` and `.gittan/post-bump.sh` are run instead when present; a present TOML file takes precedence over both fallback scripts. Hook commands can be overridden per run with repeatable `--pre-bump 'command'` and `--post-bump 'command'` flags; those flags replace that phase's TOML commands, and `--pre-bump=''` or `--post-bump=''` clears the configured phase. Changelog behavior can be overridden with `--changelog OFF`, `--changelog ON`, or `--changelog FULL`; the older `--update-changelog` and `--update-changelog=false` flags remain supported as ON/OFF aliases. A pre-bump failure prevents tag and changelog creation; a post-bump failure is reported after the tag and optional changelog have been created, and those changes are not rolled back.

## Build and test

Go modules (`go.mod` and `go.sum`) manage the Go dependencies and compiler inputs; the `Makefile` provides familiar build-task shortcuts:

```sh
make build
make test
make vet
make fmt-check
```

The executable is written to `bin/gittan`. Run `make clean` to remove that generated directory. Go and GNU Make must be installed locally.

## Docker

Build the image with:

```sh
docker build -t gittan:local .
```

The multi-stage `Dockerfile` compiles a static Linux executable, then packages it in Alpine with `git` and CA certificates. By default, the container runs `gittan --auto`. Mount the repository and run as its owner so the tag and changelog are writable and Git recognizes the working tree:

```sh
docker run --rm \
  --user "$(id -u):$(id -g)" \
  -v "$PWD:/repo" -w /repo \
  gittan:local
```

To preview instead, pass `--auto --dry-run` after the image name. Hooks that push tags or commits still need registry/remote credentials and Git configuration to be made available to the container; they are not baked into the image.
