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
	var check bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the sdo CLI and SDK versions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "sdo CLI %s\nsdo-go SDK %s\n", CLIVersion, sdo.Version); err != nil {
				return err
			}
			if !check {
				return nil
			}
			latest, err := latestRelease(cmd.Context())
			if err == nil {
				err = reportRelease(cmd.OutOrStdout(), CLIVersion, latest)
			}
			if err != nil {
				return fmt.Errorf("could not check for a newer release: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false,
		"also ask GitHub (api.github.com, anonymously) whether a newer sdo release exists")
	return cmd
}
