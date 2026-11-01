# Security Policy

`sdo-cli` ships the `sdo` binary, the official command-line interface for Scrape.do. It also bundles a Model Context Protocol (MCP) server, started with `sdo mcp serve`, that exposes Scrape.do operations as tools to MCP-capable hosts. This document describes how to report vulnerabilities and the security properties you can rely on.

## Supported Versions

Security fixes land on the latest released minor version. We recommend always running the most recent release of the `sdo` binary.

| Version | Supported |
| ------- | --------- |
| Latest minor release | Yes |
| Older releases | No — please upgrade |

## Reporting a Vulnerability

**Do not open a public issue for security problems.**

Report privately through GitHub's [Security Advisories](https://github.com/scrape-do/sdo-cli/security/advisories/new) on this repository. If you cannot use that channel, email **security@scrape.do** with:

- A description of the issue and its impact.
- The exact `sdo` version (`sdo version`), OS, and architecture.
- Minimal steps or a proof of concept to reproduce.
- Whether the issue is in the CLI itself, the bundled MCP server (`sdo mcp serve`), or token handling.

### What to Expect

- **Acknowledgement** within 3 business days.
- **Triage and severity assessment** within 7 business days.
- A coordinated fix and disclosure timeline communicated directly to you.
- Credit in the advisory and release notes, unless you ask to remain anonymous.

Governance is **Solo + AI**: the founder is the sole approver of every change, including security fixes. Contributions are accepted under **DCO sign-off** (`Signed-off-by` trailer), not a CLA. There is no committee and no external maintainer who can independently merge a fix; this keeps the trust boundary small and auditable.

Please give us a reasonable window to ship a fix before any public disclosure.

## Token Handling

`sdo` authenticates to the Scrape.do API with a single API token. There is no username/password, OAuth, or session cookie — token-only.

- **Precedence:** `--token` > the `SDO_TOKEN` environment variable > the per-user config file written by `sdo auth login`. `sdo auth status` names the source in use and never prints the token.
- **Avoid `--token` on shared machines.** Flag values are visible in the process list (`ps`, `/proc`) and are commonly captured in shell history. Prefer `sdo auth login` or `SDO_TOKEN` from a secrets manager.
- **The config file** lives at `<user config dir>/sdo/config.json` (`$XDG_CONFIG_HOME` or `~/.config` on Linux, `~/Library/Application Support` on macOS, `%AppData%` on Windows), or at `SDO_CONFIG_FILE`. `sdo auth login` reads the token without echoing it when stdin is a terminal, creates the directory with mode `0700` (and narrows the default `<user config dir>/sdo` to `0700` if you own it and it is wider), and writes the file atomically: to a new mode-`0600` file in the same directory that is then renamed over the target, so the token never sits in a file others can read, even if one existed with wider permissions. It refuses a config path that is a symlink or not a regular file, on both write and read, and refuses to read a file readable by group or others; on Unix the file is opened with `O_NOFOLLOW` and its type and mode are checked on the opened file, so a link swapped in after the check is refused too.
- **HTTPS only.** The token is sent only to the configured Scrape.do API base (default `https://api.scrape.do` and `https://q.scrape.do`; override with `SDO_BASE_URL` / `SDO_ASYNC_BASE_URL`). A plain-`http` base is refused unless its host is `localhost`, `127.0.0.1` or `::1`, or you pass `--insecure-http` — which sends the token in cleartext. Redirects are never followed, so a `3xx` cannot carry the token elsewhere.
- **Redaction.** The CLI's own messages — errors and the metadata lines on stderr — have the token (literally and percent-encoded) and credential-shaped values (`token=…`, `api_key=…`, `"access_token": "…"`, `X-Token: …`) replaced with `***`. Data you asked for — page content (`sdo scrape`, `sdo jobs result`), raw plugin bodies, and the JSON output of `sdo plugin`, `sdo jobs …` and `sdo account` — is written as delivered, except that the token itself (8 characters or longer, literally and percent-encoded, in JSON keys and values alike) is masked: a credential-shaped string that belongs to the data, such as a `?token=` link on a scraped page, is left alone.
- **Token hygiene.** Surrounding whitespace is trimmed from every token source; a token with whitespace, control or non-ASCII characters inside is refused before any request, with an error naming its source.
- The token is never logged. The only file it is written to is the config file above, and only by `sdo auth login`. The only other files `sdo` writes are the ones you name with `sdo scrape --output-file`, written with mode `0600` through a temporary file renamed over the target (an existing file is replaced, not reused with its old mode); a symlink or non-regular file at that path is refused.
- **Terminal output.** Control characters (C0 except newline and tab, DEL, C1, and invalid UTF-8 bytes) in API- or target-supplied text printed to stderr are escaped. A body with NUL or ESC bytes is not written to a terminal unless you pass `--raw`.

Treat your token like a password. Rotate it immediately if you believe it has been exposed; rotation is done from your Scrape.do dashboard and invalidates the old token.

## Bundled MCP Server

`sdo mcp serve` runs a local MCP server so MCP hosts (editors, agents, assistants) can call Scrape.do through `sdo`. This introduces a classic **confused-deputy** risk: the MCP host, not the human, decides when to invoke tools, so the server must never act on its own initiative or on ambient instructions hidden in fetched content.

Design properties that bound this risk:

- **Explicit tool calls only.** The server performs an action *only* in response to a structured MCP tool invocation from the connected host. It does not poll or schedule background work.
- **Untrusted content is fenced.** Scraped pages, plugin output and error text written by the gateway or the target are returned between `--- BEGIN UNTRUSTED CONTENT <nonce> ---` and `--- END UNTRUSTED CONTENT <nonce> ---` lines. The nonce is random per response and is the primary control: a page cannot know it, so it cannot print the real closing line and continue in the server's voice. As a second layer, marker look-alikes inside the content are neutralized — any "untrusted content" phrase and any line shaped like a marker line (three or more dashes, then BEGIN or END) — after folding Unicode tricks away: compatibility forms (NFKC), zero-width and other format characters, combining marks, no-break and other Unicode spaces and dashes, and Cyrillic, Greek and other letters that imitate Latin ones. Server metadata (status, cost when known, URL) is a separate content item, and binary responses (images, PDFs) are described, not returned. This makes the trust boundary visible to the model; it does not make a model immune to instructions in page text.
- **Bounded output.** Each tool result carries at most 128 KiB of outside text; truncation is stated outside the fenced block.
- **Token stays server-side.** The token is read by the `sdo` process (same precedence as the CLI) and held only there. It is **not** a tool argument, and every tool result — success or error — passes through the same redaction as CLI output before it reaches the host, so a gateway or plugin that echoes the token cannot hand it to the model.
- **Validated input.** Tool arguments are checked against each tool's closed input schema before anything is sent: an unknown argument or a mistyped value is a JSON-RPC `-32602` error, never silently dropped or coerced. Plugin routes chosen by the model are checked against the documented route shape (no `../`, `?` or `#`), and plugin parameters cannot carry a `token`.
- **Bounded, cancellable work.** At most 4 tool calls run at once; a `notifications/cancelled` from the host abandons the call (and its gateway request) and suppresses its reply. Each call spends Scrape.do credits, and the tool descriptions say so.
- **Local transport.** The server communicates over local stdio with the host process that launched it. It does not open a network listener.
- **No privilege escalation.** MCP tools expose the same Scrape.do operations the CLI already offers under the same token.

Only connect the bundled MCP server to MCP hosts you trust, and scope the token to the access level the workload actually needs.

## Privacy and Telemetry

**`sdo` collects no telemetry and has no update check.** There is no analytics, no usage reporting, no crash phone-home, and no background network activity. The only outbound traffic is the API requests you explicitly make to Scrape.do, identified by the `User-Agent` header `sdo-cli/<version>` and nothing else (no OS, no runtime version). This holds for both the CLI and the bundled MCP server.

## Supply-Chain Integrity

- `sdo` is built on the `sdo-go` SDK, which is tested against a cross-language conformance corpus shared by every Scrape.do SDK. Development is **TDD-first**: changes ship with tests, and CI enforces formatting, `go vet`, the race detector, a coverage floor, secret scanning, `govulncheck`, OSV scanning and a drift check on the command/MCP surface.
- Dependencies and the SHA-pinned GitHub Actions are kept current by Dependabot, with a 7-day cooldown before a new release is proposed.
- Releases are built from tagged commits on `main` by GoReleaser, published only after approval in a protected `release` environment. For version X.Y.Z (the tag `vX.Y.Z`) a release holds:
  - `sdo_X.Y.Z_<os>_<arch>.tar.gz` for `linux` and `darwin`, and `sdo_X.Y.Z_windows_<arch>.zip`, each for `amd64` and `arm64` (e.g. `sdo_X.Y.Z_darwin_arm64.tar.gz`). Each archive holds the `sdo` binary (`sdo.exe` on Windows), `LICENSE`, `NOTICE`, `README.md`, `CHANGELOG.md` and `THIRD_PARTY_LICENSES.txt`, the license and notice texts of the Go runtime and every module linked into the binary.
  - `<archive>.sbom.json`, an SPDX SBOM per archive (e.g. `sdo_X.Y.Z_linux_amd64.tar.gz.sbom.json`).
  - `checksums.txt`, the SHA-256 of every file above, and `checksums.txt.cosign.bundle`, a cosign keyless signature for it, signed by the release workflow's GitHub OIDC identity and recorded in the Rekor transparency log.

  To verify a download (cosign v3):

  ```bash
  cosign verify-blob checksums.txt \
    --bundle checksums.txt.cosign.bundle \
    --certificate-identity "https://github.com/scrape-do/sdo-cli/.github/workflows/release.yml@refs/tags/vX.Y.Z" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com
  sha256sum --ignore-missing -c checksums.txt
  ```

- Sibling repositories under the same governance: `sdo-sdk-python` (PyPI `sdo-sdk`), `sdo-sdk-js` (npm `@scrape-do/sdk`), and `sdo-go` (`github.com/scrape-do/sdo-go`).

## Scope

In scope:

- The `sdo` binary and its subcommands.
- The bundled MCP server (`sdo mcp serve`), including token isolation and the confused-deputy properties above.
- Token handling and redaction.

Out of scope:

- The Scrape.do API service itself — report those issues to **security@scrape.do**.
- Vulnerabilities in third-party MCP hosts that connect to the bundled server.
- Misconfiguration on the operator's machine, such as exporting `SDO_TOKEN` into an untrusted environment or passing `--insecure-http` to a non-local gateway.

## License

This project is licensed under **Apache-2.0**. Security fixes are released under the same license.
