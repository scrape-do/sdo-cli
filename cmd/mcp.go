// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"github.com/scrape-do/sdo-cli/internal/mcp"
	"github.com/spf13/cobra"
)

func newMCPCmd(a *app) *cobra.Command {
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Run the MCP server over stdio (newline-framed JSON-RPC)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.newClient()
			if err != nil {
				return err
			}
			return mcp.Serve(cmd.Context(), client, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	m := &cobra.Command{
		Use:   "mcp",
		Short: "Model Context Protocol server",
	}
	groupCommand(m)
	m.AddCommand(serve)
	return m
}
