# Migrating

This document records breaking changes between releases of `sdo-cli` — the
`sdo` command-line tool (which also bundles the MCP server via `sdo mcp serve`).
Each entry explains what changed, why, and the exact steps to update your
scripts, pipelines, and shell integrations.

`sdo-cli` follows [Semantic Versioning](https://semver.org/). Until `1.0.0`,
the minor version (`0.X.0`) carries breaking changes; patch releases
(`0.x.Y`) are backward compatible. After `1.0.0`, breaking changes are
confined to major versions.

## What counts as a breaking change

The CLI is consumed by humans at a terminal **and** by scripts that parse its
output. Both surfaces are part of the contract:

- **Flags and arguments.** Removing or renaming a flag, changing a flag from
  taking a value to being a boolean (or vice versa), changing a default value,
  or making a previously optional argument required is a breaking change.
  Adding a new optional flag with a backward-compatible default is **not**.
- **Output format.** Changing the structure of JSON output (`sdo plugin`,
  `sdo jobs …`, `sdo account`, `sdo jobs result --json`: renaming, removing, or
  retyping a field), changing JSON field nullability, or altering
  the default human-readable layout in a way that breaks reasonable parsing is
  a breaking change. Adding a new field to JSON output is **not** breaking;
  scripts must ignore unknown fields.
- **Exit codes.** Changing the exit code for an existing outcome is a breaking
  change. The current mapping is: `0` success, `1` runtime/request error,
  `2` usage/validation error, `3` authentication error, `4` async task not
  finished yet (`sdo jobs result`).
- **Command names and routes.** Renaming or removing a subcommand
  (e.g. `sdo scrape`, `sdo mcp serve`) is a breaking change.
- **Environment and auth.** Authentication is token-only, resolved as
  `--token` > `SDO_TOKEN` > the config file written by `sdo auth login`.
  Changing that order, the environment variables (`SDO_TOKEN`,
  `SDO_CONFIG_FILE`, `SDO_BASE_URL`, `SDO_ASYNC_BASE_URL`, `SDO_TIMEOUT`), or
  adding an alternative credential mechanism, is a breaking change.
- **Install path.** `go install github.com/scrape-do/sdo-cli/cmd/sdo` builds
  the binary; moving that package renames the binary and is a breaking change.

Stability guarantees that will not change without a major version:

- **No telemetry.** The CLI never phones home, never collects usage data, and
  has no opt-in/opt-out telemetry flag to migrate away from.
- **No UI.** `sdo` is CLI-only and ships the MCP server in-process; there is
  no web or desktop interface, and none will be added.
- **Token-only auth.** The API token is the single credential.

## Conformance and parity

The CLI's request/response behavior comes from the `sdo-go` SDK, which is
validated by the cross-language conformance corpus shared with the other
`sdo` SDKs (Python, JS). When a breaking change here stems from an SDK
change, the SDK's `MIGRATING.md` carries a matching entry. All changes are
developed test-first (TDD); the command and MCP surface is pinned by
`api-surface.txt`.

## Governance

`sdo` uses Solo+AI governance: the founder is the sole approver. Contributions
are accepted under the [Developer Certificate of Origin](https://developercertificate.org/)
(DCO) with a `Signed-off-by` trailer — there is no CLA. Licensed under
Apache-2.0.

---

## Releases

No version has been released yet, so there is nothing to migrate from. The
first release's behaviour is described in [CHANGELOG.md](CHANGELOG.md); from
then on, every breaking change gets an entry here.
