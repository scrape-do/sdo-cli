// SPDX-License-Identifier: Apache-2.0

package mcp

import "github.com/scrape-do/sdo-cli/internal/version"

// This file holds what differs between this package and its copy in the other
// repository (sdo-mcp's internal/mcp and sdo-cli's internal/mcp). Every other
// file is kept identical apart from the module path, which sdo-mcp's CI checks.

// serverVersion is reported in the handshake's serverInfo.
const serverVersion = version.CLI

// tooLargeHint replaces the SDK's generic raise-the-limit parenthetical in a
// response-too-large error with where this server's limit is set.
const tooLargeHint = "(the limit is --max-response-bytes, a flag of the sdo command that starts this server)"
