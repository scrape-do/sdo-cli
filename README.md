<!-- SPDX-License-Identifier: Apache-2.0 -->
# sdo — the Scrape.do CLI

A single static binary to scrape the web, call plugins, run async jobs, and
serve MCP. Built on the [`sdo-go`](https://github.com/scrape-do/sdo-go) SDK.
No telemetry, no automatic update check, no UI.

## Install

Download a prebuilt archive from the GitHub Releases page. For version X.Y.Z
they are named

- `sdo_X.Y.Z_linux_amd64.tar.gz`, `sdo_X.Y.Z_linux_arm64.tar.gz`
- `sdo_X.Y.Z_darwin_amd64.tar.gz`, `sdo_X.Y.Z_darwin_arm64.tar.gz`
- `sdo_X.Y.Z_windows_amd64.zip`, `sdo_X.Y.Z_windows_arm64.zip`

and each holds the `sdo` binary (`sdo.exe` on Windows) with `LICENSE`,
`NOTICE`, `README.md`, `CHANGELOG.md` and `THIRD_PARTY_LICENSES.txt` (the
license texts of everything linked into the binary). Each release also carries
an SPDX SBOM per archive (`<archive>.sbom.json`), `checksums.txt` and a cosign
signature for it; see [SECURITY.md](SECURITY.md) for how to verify a download.

With a Go toolchain (1.26+), once `sdo-cli` and `sdo-go` are published:

```bash
go install github.com/scrape-do/sdo-cli/cmd/sdo@latest   # installs a binary named sdo
```

### Checking for updates

`sdo version --check` prints the versions as `sdo version` does, then asks
GitHub whether a newer release exists:

```bash
sdo version --check
```

It sends one anonymous request to
`https://api.github.com/repos/scrape-do/sdo-cli/releases/latest`, identified
only by the `User-Agent` header `sdo-cli/<version>`; your Scrape.do token is
never read or sent. It is the only command that contacts anything other than
Scrape.do, and it never runs on its own: there is no background, startup or
periodic check. When a newer release exists it prints the upgrade commands and
the release page, and still exits `0`. When the check itself fails (no network,
a timeout after 5 seconds, GitHub's limit for anonymous requests) the versions
are still printed and it exits `1` with the reason on stderr.

### Build from source

Clone `sdo-go` next to this repository and build through a Go workspace (the
full setup, and why, is in [CONTRIBUTING.md](CONTRIBUTING.md#development-setup)):

```bash
git clone https://github.com/scrape-do/sdo-go.git
git clone https://github.com/scrape-do/sdo-cli.git
go work init ./sdo-cli ./sdo-go
go work edit -replace "github.com/scrape-do/sdo-go@v0.1.0=./sdo-go"
cd sdo-cli && go build -o sdo ./cmd/sdo
```

## Usage

```bash
sdo auth login                # store your token (input hidden); or set SDO_TOKEN, or pass --token

# Scrape a URL (body -> stdout, metadata -> stderr):
sdo scrape https://example.com --render --geo-code us

# Wait for an element on a rendered page:
sdo scrape https://example.com --render --wait-selector '#results'

# Write the body to a file, markdown output:
sdo scrape https://example.com --output markdown --output-file page.md

# Send a POST with a JSON body and a custom header to the target:
sdo scrape https://httpbin.org/post --data-json '{"q":"laptop"}' -H 'X-Trace: 1' --custom-headers

# A gateway parameter without a flag of its own:
sdo scrape https://example.com --param someNewParam=value

# Call a documented plugin (Google plugins take gl for the country; output -> stdout,
# status and cost -> stderr):
sdo plugin google/shopping --param q=laptop --param gl=us

# The Amazon raw-HTML plugin (/plugin/amazon/):
sdo plugin amazon --param url=https://www.amazon.com/dp/B0C7BKZ883 --param geocode=us

# Usage statistics (requests and concurrency left; no credits, 10 calls a minute):
sdo usage

# Versions (the CLI's and the SDK's):
sdo version

# The same, then ask GitHub whether a newer release exists (see Checking for updates):
sdo version --check
```

Every call spends Scrape.do credits; `--render` and `--super` cost more.
`sdo scrape --help` lists a flag for each documented scrape parameter.
`--param key=value` (repeatable) sends any other gateway parameter as is; it
cannot set the token or the URL, or a parameter that has a flag.

The documented parameter dependencies are checked before anything is sent (exit
2), each one the gateway rejects or documents as invalid: `--show-frames`,
`--show-websocket-requests` and the screenshot flags need `--render` and
`--return-json`; one screenshot flag at a time; `--particular-screenshot` cannot
be combined with `--play-with-browser`; `--regional-geo-code` needs `--super` and
excludes `--geo-code`; `--zip-code` needs `--super` and `--geo-code`;
`--session-id` is an integer 0–1000000; `--retry-timeout` excludes `--render`;
only one of `--custom-headers`, `--extra-headers`, `--forward-headers`; `--width`
and `--height` need `--render`. With
`--disable-redirection`, the metadata line ends with the target's redirect
target (`location=...`).

A plugin parameter that takes several values (`hotel_class`, `departure_id`,
...) takes one comma-separated value (`--param hotel_class=4,5`); the plugins
read only the first of a repeated `--param`.

`--method` (`GET` `POST` `PUT` `PATCH` `DELETE` `HEAD` `OPTIONS`), `--data`
(a body sent verbatim), `--data-json` (a JSON body) and `-H/--header 'Key: Value'`
shape the request the gateway sends to the target. A body without `--method`
is sent as `POST`; `--data` and `--data-json` are mutually exclusive. A header
name must be an RFC 9110 token and its value printable ASCII; `Host`,
`Content-Length`, `Transfer-Encoding` and `Connection` cannot be set.

### Timeouts and limits

Two different clocks:

- `--timeout` is the **gateway's** timeout for the scrape, in milliseconds
  (5000–120000). `--custom-wait` (0–35000 ms, with `--render`) and
  `--retry-timeout` (5000–55000 ms) are gateway-side too.
- `--client-timeout` is how long **this client** waits for each HTTP attempt,
  in seconds (default 70: the gateway's own 60-second default timeout plus 10,
  or `SDO_TIMEOUT`). When `--timeout` is given (or a plugin's `timeout`
  `--param`), the client waits at least that long plus 10 seconds, so it does
  not give up on a request the gateway is still running. A request that was sent is never
  retried after it times out.

`--max-response-bytes` is the largest response body the client accepts, after
decompression (default 33554432, i.e. 32 MiB). All three global flags also
apply to `sdo mcp serve`.

## Token

Precedence: `--token` > `SDO_TOKEN` > the per-user config file.

```bash
sdo auth login                         # prompts; the token is not echoed
printenv MY_TOKEN | sdo auth login     # or from a pipe
sdo auth status                        # says which source is in use; never prints the token
```

The config file keeps the token out of shell history and out of an exported
environment variable that every child process inherits. It lives under your
platform config dir (`sdo/config.json`; override with `SDO_CONFIG_FILE`) and is
written atomically with mode `0600`. `sdo` refuses a config file that is
readable by other users, or that is a symlink, rather than using it silently.

Surrounding whitespace is ignored in every source (a secret mounted from a file
usually ends in a newline); a token that still contains whitespace or control
characters is refused with an error naming where it came from.

## Environment

| Variable | Meaning |
| --- | --- |
| `SDO_TOKEN` | API token, when `--token` is not given |
| `SDO_CONFIG_FILE` | path of the token file (default `<config dir>/sdo/config.json`) |
| `SDO_BASE_URL` | sync API base (default `https://api.scrape.do`) |
| `SDO_ASYNC_BASE_URL` | async job API base (default `https://q.scrape.do`) |
| `SDO_TIMEOUT` | client per-attempt timeout in seconds, a positive number (default 70); `--client-timeout` wins |

The base URLs must be `https`. Plain `http` is accepted for `localhost`,
`127.0.0.1` and `::1`, or anywhere with `--insecure-http`, which sends the token
in cleartext.

## Async jobs

Asynchronous scraping is the REST job API on `q.scrape.do`. Submit a job, poll
its status, then fetch each task's result. Options are passed with the API's own
job-body keys (`Super`, `GeoCode`, `Render`, `Output`, …). Structured payloads
print to stdout as JSON; a one-line summary prints to stderr.

```bash
# Submit a job for one or more URLs (Super=true becomes a bool; GeoCode=us stays a string):
sdo jobs create https://a.com https://b.com --option Super=true --option GeoCode=us

# A plugin job: one task per params entry (at most 1000), with the plugin's own
# parameter names; give the array inline, from a file, or from stdin (-):
sdo jobs create-plugin amazon/pdp --params '[{"asin":"B0C7BKZ883","geocode":"us"}]'
sdo jobs create-plugin google/search --params-file queries.json --option WebhookURL=https://example.com/hook

# Inspect a job and its per-task state:
sdo jobs status <jobID>

# Fetch one task's content, byte for byte (body -> stdout, metadata -> stderr),
# or the whole task result as JSON:
sdo jobs result <jobID> <taskID> > page.html
sdo jobs result <jobID> <taskID> --json

# Poll until the task has finished (exit code 4 means "not yet"):
until sdo jobs result <jobID> <taskID> > page.html; [ $? -ne 4 ]; do sleep 5; done

# List jobs a page at a time (default: the 10 most recent), filtered by state
# and start time; stdout has Jobs, TotalCount and TotalPages:
sdo jobs list
sdo jobs list --status success --from 2025-01-01T00:00:00Z --sort start_time_asc --page 2 --page-size 50

# Cancel a job:
sdo jobs cancel <jobID>

# Account concurrency + remaining credits (job API):
sdo account
```

`sdo jobs result` exits `0` only when the task succeeded. A failed or canceled
task exits `1` with the task's error message; a task that has not finished yet
(queued, pending, rotating) exits `4`. In both cases nothing is written to
stdout (with `--json`, the task result is printed either way).

## MCP server

The CLI bundles a Model Context Protocol server over stdio:

```bash
sdo mcp serve
```

It exposes `scrape` and `scrape_plugin`, and the async job tools
`async_job_create`, `async_plugin_job_create`, `async_job_status`,
`async_task_result`, `async_jobs_list`, `async_job_cancel` and
`async_account` (the same set as the standalone
[sdo-mcp](https://github.com/scrape-do/sdo-mcp) server). The scrape tools and the
two create tools spend Scrape.do credits; every tool description says whether
a call does. Run `sdo auth login` once, then
point your MCP client (Claude Desktop, Cursor, Claude Code, …) at the binary;
the server reads the token the same way the CLI does, so it need not appear in
the client's configuration file:

```json
{
  "mcpServers": {
    "scrape-do": { "command": "sdo", "args": ["mcp", "serve"] }
  }
}
```

Tool arguments are checked against each tool's input schema: an unknown
argument or a value of the wrong type (`"render": "true"`) is a JSON-RPC
`-32602` error naming it, never silently dropped. Tool calls run concurrently
(up to 4 at a time), so a slow scrape does not hold up other requests, and a
`notifications/cancelled` from the client abandons the call and suppresses its
reply.

Page content, plugin output and error text are returned between
`BEGIN/END UNTRUSTED CONTENT <nonce>` markers, and every tool result is
redacted; see [SECURITY.md](SECURITY.md#bundled-mcp-server).

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | success |
| 1 | the request or local I/O failed (for `sdo version --check`: the check could not be completed; a newer release is not a failure) |
| 2 | usage error: unknown command or flag, wrong arguments, or input rejected before sending (a bad URL, an out-of-range timeout) |
| 3 | authentication: no token found, or the gateway refused it |
| 4 | `sdo jobs result`: the task has not finished yet; try again later |

Errors are printed to stderr as `sdo: <reason>`.

`Scrape.do-Request-Cost` is the authoritative charge. A page the target
answered with 404 or 410, or with any status of 400 or above the gateway
billed (a positive cost: the target's own 400, 401, 403, 422, ...), is a
result, not a failure: the body is printed, the metadata line shows its status
and cost (`status=401 cost=1`), and the exit code is 0. An error from
Scrape.do itself carries no cost (or 0) and keeps its exit code: a parameter it
rejected exits 1, a refused token or exhausted credits exits 3. When the
gateway reported a cost for an error, the message ends with it (`cost=0`).
`sdo plugin` follows the same rules and prints the same metadata line
(`status=200 cost=10`).

## What the CLI writes to disk

- The token file, and only on `sdo auth login` (mode `0600`, see [Token](#token)).
- The files you name: `sdo scrape --output-file` writes the file with mode
  `0600` (a page can carry session cookies or personal data), through a
  temporary file in the same directory renamed over the target, so an existing
  file is replaced by a `0600` one. A path that is a symlink or not a regular
  file is refused.

Nothing else: no cache, no logs, no history. Everything else goes to stdout
and stderr.

## Terminal safety

Control characters in text the API or a target supplies (URLs, task states,
error messages) are printed on stderr as escapes such as `\x1b`, so they
cannot drive your terminal. When stdout is a terminal and a body (`sdo scrape`,
`sdo jobs result`, a raw `sdo plugin` body) contains NUL or ESC bytes, nothing
is written and the command fails, as curl does: redirect or pipe the output,
use `--output-file`, or pass `--raw` to print it anyway.

## CI/CD workflows

| Workflow | Runs on | What it does |
|----------|---------|--------------|
| `ci.yml` | every push to `main` and every pull request | `go vet` and the race-enabled tests of the CLI and the MCP server on Go 1.26 and 1.27 (Linux), plus macOS and Windows on 1.27; a `quality` job for `gofmt` and the 99% statement-coverage floor; a check that `go install` produces a binary named `sdo`; the command, flag and MCP-tool snapshot (`api-surface.txt`) must be up to date; CodeQL code scanning. Everything runs on GitHub-hosted runners; CodeQL runs on the org's self-hosted runners for same-repository events (its upload is a GitHub API call the org's IP allow list only accepts from there); a pull request from a fork is analysed once merged. |
| `security.yml` | push to `main`, pull requests, and weekly | secret scanning over the full git history (gitleaks), OSV-Scanner, `govulncheck`, and an SBOM uploaded as a build artifact. OpenSSF Scorecard runs on pushes to `main` and weekly, with results in code scanning. |
| `release.yml` | a version tag only | Four jobs. **build** (read-only) checks the tag against the CLI version, tests, and builds the archives, checksums, SBOMs and third-party license notices with GoReleaser. **smoke** unpacks every archive on a GitHub-hosted runner of its platform (Linux and Windows on amd64 and arm64, macOS on arm64 and under Rosetta) and runs the binary. **publish** runs only after approval in the `prerelease` or `release` environment and only for a tag on `main`: it verifies the checksums and signs `checksums.txt` with cosign (keyless); a last job on the org's self-hosted runners (the only ones the org's IP allow list lets call the GitHub API) creates the GitHub release — a **pre-release** for `vX.Y.Z-rc.N`, a normal release for `vX.Y.Z`. |
| Dependabot | weekly | update pull requests for Go modules and the GitHub Actions, taken only after a 7-day cooldown; each one runs the checks above. |

`go.mod` pins the `sdo-go` SDK to a released version and `go.sum` verifies it: CI and the release build fetch exactly that version from the Go module proxy.

Every action is pinned to a full commit SHA and every job declares least-privilege `permissions:`. Publishing never happens outside the `scrape-do` organisation, never from a commit that is not on `main`, and never without a maintainer's approval.

## Responsible use

You are responsible for complying with the terms of service, `robots.txt`, and
applicable laws of any site you scrape. Third-party names are trademarks of their
respective owners; Scrape.do is not affiliated with them.

## License

[Apache-2.0](LICENSE).
