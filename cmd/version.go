// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"

	"github.com/scrape-do/sdo-cli/internal/version"
	sdo "github.com/scrape-do/sdo-go"
	"github.com/spf13/cobra"
)

// CLIVersion is the CLI's own version (the one `sdo --version` prints and the
// User-Agent carries).
const CLIVersion = version.CLI

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the sdo CLI and SDK versions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "sdo CLI %s\nsdo-go SDK %s\n", CLIVersion, sdo.Version)
			return err
		},
	}
}
