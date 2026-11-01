// SPDX-License-Identifier: Apache-2.0

// Command sdo is the official Scrape.do CLI. It also bundles an MCP server,
// runnable with `sdo mcp serve`.
//
// It lives in cmd/sdo so that `go install github.com/scrape-do/sdo-cli/cmd/sdo@latest`
// installs a binary named sdo (Go names the binary after the last path element).
package main

import (
	"os"

	"github.com/scrape-do/sdo-cli/cmd"
)

func main() {
	os.Exit(cmd.Main(os.Args[1:], os.Stdout, os.Stderr))
}
