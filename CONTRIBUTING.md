# Contributing to sdo-cli

Thanks for your interest in improving `sdo` — the official Scrape.do command-line tool. `sdo-cli` builds the `sdo` binary (a [Cobra](https://github.com/spf13/cobra) application) and bundles the Model Context Protocol server exposed via `sdo mcp serve`.

This document covers local setup, the test-driven workflow we expect, and the commit and sign-off conventions required for a change to be merged.

## Governance

`sdo` uses **Solo+AI governance**. The founder is the sole approver and final reviewer of every change. There is no committee and no voting — discussion is welcome on issues and pull requests, but whether to merge rests with one person.

We require a **DCO sign-off**, not a CLA. You retain copyright over your contribution; the sign-off is your assertion that you have the right to submit it under the project license. See [Sign-off (DCO)](#sign-off-dco) below.

## License

`sdo-cli` is licensed under **Apache-2.0**. By contributing, you agree that your contributions are licensed under the same terms. Do not add code under an incompatible license, and do not copy code from sources whose license you cannot verify.

## No telemetry, no UI

Two hard constraints shape what we accept:

- **No telemetry.** The `sdo` binary must not phone home, collect usage analytics, or emit any data to Scrape.do or third parties beyond the API calls a user explicitly invokes. Pull requests that add tracking, "anonymous" metrics, crash reporters, or update-check beacons will be rejected. The one allowed exception is `sdo version --check`: a single anonymous request to the GitHub releases API, made only when the user runs that command. Any automatic check (at startup, in the background, periodically, or enabled by a setting or environment variable) is forbidden, and so is sending a credential or anything identifying beyond the `User-Agent` with that request.
- **No UI.** `sdo` is CLI-only and ships alongside the language SDKs and MCP. Do not add a web dashboard, a TUI app, a browser launcher, or any graphical surface. Output is text (and structured formats like JSON) on stdout/stderr.

## Authentication model

Auth is **token-only**. The CLI resolves the token in one place (`cmd/token.go`): `--token` > `SDO_TOKEN` > the per-user config file written by `sdo auth login` (mode `0600`, never followed through a symlink). There is no OAuth flow and no username/password. When you add or change a command that talks to the API, build the client through the shared `newClient` path — never introduce another credential source — and print through the redacting helpers (`printErr`, `printJSON`, `maskToken`).

```bash
sdo auth login        # or: export SDO_TOKEN="your-token-here"
sdo scrape https://example.com
```

Never commit a real token. Tests use a stub token against an `httptest` server; no test and no CI job contacts the live API.

## Prerequisites

- **Go 1.26 or newer** (`go version` to check; `go.mod` declares `go 1.26.9`; CI also runs the current release, and releases are built with it).
- `git` with your `user.name` and `user.email` configured (required for sign-off).
- Nothing else: `go.mod` pins the [`sdo-go`](https://github.com/scrape-do/sdo-go) SDK to a released version, which `go` fetches from the module proxy. A local SDK checkout is only needed to work against unreleased SDK changes (below).

## Development setup

`go.mod` requires a released `sdo-go` version and carries **no `replace` directive**, so `go build ./...` works from a plain clone and `go install github.com/scrape-do/sdo-cli/cmd/sdo@version` works for users (the `main` package lives in `cmd/sdo` so the installed binary is named `sdo`).

To develop against a local SDK checkout (an SDK change the CLI needs, before it is released), use a `go.work` file in the **parent** directory. It is never committed (this repository also ignores `go.work` and `go.work.sum`, in case one is created inside it):

```text
sdo-workspace/
├── go.work      # local only
├── sdo-go/      # github.com/scrape-do/sdo-go
└── sdo-cli/     # this repository — builds the `sdo` binary
```

```bash
git clone https://github.com/scrape-do/sdo-go.git
git clone https://github.com/scrape-do/sdo-cli.git
go work init ./sdo-cli ./sdo-go
cd sdo-cli
```

Every `go` command inside `sdo-cli/` now builds against `../sdo-go`. `GOWORK=off go build ./...` builds against the version pinned in `go.mod` / `go.sum` instead, which is what CI and the release build do: they never use a workspace, so a change that needs new SDK behaviour ships only after that SDK version is released and pinned here (see Releasing).

### Run the checks CI runs

```bash
gofmt -l .                     # must print nothing
go vet ./...
go test -race ./...
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1   # floor: 99%
go run ./tools/apisurface > api-surface.txt && git diff --exit-code api-surface.txt
```

`api-surface.txt` is the CLI's compatibility surface — every command, argument and flag, and every MCP tool parameter, one per line. Regenerate and commit it whenever you change any of them; CI fails on drift.

### Build the binary

```bash
go build -o sdo ./cmd/sdo
./sdo --help
./sdo mcp serve --help
```

The `sdo` artifact is git-ignored; do not commit it.

## Releasing

A release of the CLI that needs a new SDK goes in this order:

1. Release `sdo-go` first and push its tag (`vX.Y.Z`).
2. In this repository, pin that version (with `GOWORK=off` if you use a workspace):

   ```bash
   go get github.com/scrape-do/sdo-go@vX.Y.Z
   go mod tidy
   ```

3. Bump `CLI` in `internal/version/version.go`, move the `Unreleased` CHANGELOG entries under the new version, and merge to `main`.
4. Tag that commit on `main` with `v<CLI version>`. The release workflow refuses a tag that does not match `internal/version`, builds against the pinned SDK version, tests, builds the archives and runs the binary from every archive on a runner of its platform, all in read-only jobs, and publishes (cosign-signed checksums, SBOMs) only after approval in the `release` environment and only when the tagged commit is on `main`.

GoReleaser generates `THIRD_PARTY_LICENSES.txt` before the build (`go run ./tools/thirdparty`: the license and notice files of the Go runtime and of every module linked into the binary for any release target, read from the module cache) and ships it in every archive with `LICENSE`, `NOTICE`, `README.md` and `CHANGELOG.md`. A new dependency without a license file stops the release.

Development is trunk-based (pull requests into `main`), and the tag picks the
channel: `vX.Y.Z-rc.N` becomes a GitHub **pre-release** after approval in the
`prerelease` environment; `vX.Y.Z` a normal release after approval in `release`.

The `release` and `prerelease` environments have required reviewers and a deployment rule limited to `v*` tags. Keep them: an environment that does not exist is created on first use with no protection.

### Homebrew and Scoop

The build job also runs `go run ./tools/pkgmanifests -version X.Y.Z -checksums dist/checksums.txt -out dist/pkgmanifests`, which writes the Homebrew formula (`Formula/sdo.rb`) and the Scoop manifest (`bucket/sdo.json`) for the version from `checksums.txt` alone, and uploads them as the `pkgmanifests` artifact; the smoke job lints the formula (`brew style`, `ruby -c`) on macOS and parses the manifest on Windows. Its golden files in `tools/pkgmanifests/testdata/` are the real v0.2.0 files; after an intended change to the output, regenerate them with `go test ./tools/pkgmanifests -update` and review the diff.

For a final release (never a release candidate), two more jobs run after the GitHub Release exists:

- **package-managers**, on the self-hosted runners: checks with plain bash that every URL in the two files is this release's and every hash is the one in the signed `checksums.txt`, then commits `Formula/sdo.rb` to `scrape-do/homebrew-tap` and `bucket/sdo.json` to `scrape-do/scoop-bucket` on `main` with GitHub's GraphQL `createCommitOnBranch` (GitHub signs the commit; the org's ruleset refuses unsigned ones), messages `sdo X.Y.Z`. A re-run that finds identical files skips with a notice.
- **verify-install**, on GitHub-hosted runners, without a token: `brew install scrape-do/tap/sdo` on macOS and `scoop install sdo` from the bucket on Windows, each checking that `sdo --version` prints the version.

One-time setup, by an org owner:

1. Create a GitHub App owned by the `scrape-do` organisation with no webhook and one repository permission, **Contents: Read and write** (Metadata: Read-only comes with it), and install it on `homebrew-tap` and `scoop-bucket` only.
2. Add two organisation secrets, available to `sdo-cli` and `sdo-mcp`: `TAP_APP_ID` (the App's ID) and `TAP_APP_PRIVATE_KEY` (a private key generated for it, the whole PEM).

The job mints an installation token for those two repositories, limited to `contents: write`, which is revoked when the job ends.

## Test-driven development

We are **TDD-first**. Write the failing test before the implementation, then make it pass, then refactor. Concretely, for any behavioral change:

1. Add or update a test that captures the desired behavior and fails for the right reason.
2. Implement the minimum code needed to make it pass.
3. Refactor with the test as your safety net.

A pull request that adds or changes behavior without an accompanying test will not be merged. Bug fixes must include a regression test that fails on the old code and passes on the new code.

### The conformance corpus

The request and response behaviour the CLI relies on — parameter serialization, validation, decoding, errors, redaction — lives in `sdo-go`, which is tested against a cross-language **conformance corpus** shared by every Scrape.do SDK. The CLI does not re-implement any of it:

- A change that needs different wire behaviour belongs in `sdo-go` (and its specification) first; the CLI then picks it up with a version bump.
- Keep CLI behavior consistent with the Python (`sdo-sdk`), JS (`@scrape-do/sdk`), and Go (`sdo-go`) implementations. Divergence across languages is a bug.

## Coding standards

- Run `gofmt` (or `go fmt ./...`) before committing — unformatted code fails CI.
- Run `go vet ./...` and resolve all reported issues.
- Keep commands small: a Cobra command should parse flags, call into a service/SDK layer, and format output. Business logic belongs in testable functions, not in `RunE` closures.
- Write to `cmd.OutOrStdout()` / `cmd.ErrOrStderr()`, not directly to `os.Stdout` / `os.Stderr`, so output is capturable in tests.
- Return errors; do not call `os.Exit` outside `main`. `cmd.Main` prints each error once, redacted, and maps it to the exit code (`usageErrorf` for command-line mistakes).
- Keep per-invocation state on the `app` struct, not in package-level variables.
- No new third-party dependency without justification in the pull request description. Prefer the standard library and what `sdo-go` already provides.

## Conventional Commits

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/). The format is:

```text
<type>(<optional scope>): <description>
```

Common types:

| Type       | When to use                                              |
|------------|----------------------------------------------------------|
| `feat`     | A new command, flag, or user-visible capability          |
| `fix`      | A bug fix                                                 |
| `docs`     | Documentation only                                       |
| `test`     | Adding or correcting tests                               |
| `refactor` | Code change that neither fixes a bug nor adds a feature  |
| `chore`    | Build, tooling, dependencies                             |

Examples:

```text
feat(scrape): add --render flag to request JS rendering
fix(mcp): report an empty token with a clear error
test(scrape): cover non-2xx response handling
```

Breaking changes use a `!` after the type/scope and a `BREAKING CHANGE:` footer:

```text
feat(scrape)!: rename --output-file to --out

BREAKING CHANGE: --output-file is removed; use --out.
```

## Sign-off (DCO)

Every commit must be signed off under the [Developer Certificate of Origin](https://developercertificate.org/). Add the sign-off automatically with the `-s` flag:

```bash
git commit -s -m "fix(mcp): report an empty token with a clear error"
```

This appends a trailer to your commit message:

```text
Signed-off-by: Your Name <you@example.com>
```

The name and email must match your `git config user.name` and `user.email`. Commits without a valid sign-off cannot be merged. If you forgot to sign off, amend with:

```bash
git commit --amend -s --no-edit
```

For a branch with several commits, rebase and sign off each one:

```bash
git rebase --signoff main
```

## Pull request checklist

Before opening a pull request, confirm:

- [ ] `go test -race ./...` passes locally and coverage stays at or above 99%.
- [ ] `go build -o sdo ./cmd/sdo` succeeds.
- [ ] `gofmt -l .` prints nothing and `go vet ./...` is clean.
- [ ] New behavior has tests written test-first.
- [ ] `go.mod` has no `replace` directive and no `go.work` is committed.
- [ ] `api-surface.txt` is regenerated if a command, flag, argument or MCP tool changed.
- [ ] No telemetry, no UI, no second auth path was introduced.
- [ ] Commits use Conventional Commits and are DCO signed off (`-s`).
- [ ] If the public surface changed, the spec and conformance corpus were updated.

## Reporting bugs and proposing features

Open an issue. For bugs, include the `sdo` version (`sdo --version`), your OS, the exact command, and the observed vs. expected behavior — with `SDO_TOKEN` and any other secrets redacted. For features, describe the use case before the proposed flags; given Solo+AI governance, a clear problem statement is the fastest path to a decision.

## Security

Do not file security issues in the public tracker. Report vulnerabilities privately to the maintainer as described in `SECURITY.md`. Never include a live `SDO_TOKEN` or any credential in an issue, pull request, test fixture, or commit.
