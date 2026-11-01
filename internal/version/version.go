// SPDX-License-Identifier: Apache-2.0

// Package version holds the sdo CLI's own version: the single value behind
// `sdo --version`, `sdo version`, the User-Agent header and the MCP server's
// serverInfo.
package version

// CLI is the sdo CLI version. The release workflow refuses a tag that does not
// match it, so the binary never reports one version and ships as another.
const CLI = "0.1.0"

// UserAgent identifies the CLI, and the MCP server it bundles, to the gateway.
const UserAgent = "sdo-cli/" + CLI
