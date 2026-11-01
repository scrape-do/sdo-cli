// SPDX-License-Identifier: Apache-2.0

package cmd

import "github.com/spf13/cobra"

func newUsageCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "usage",
		Short: "Show the account's usage statistics (requests and concurrency left)",
		Long: "Prints the account's usage statistics from the /info endpoint as JSON:\n" +
			"IsActive, ConcurrentRequest, MaxMonthlyRequest, RemainingConcurrentRequest and\n" +
			"RemainingMonthlyRequest. It costs no credits. The endpoint allows 10 requests\n" +
			"per minute; above that it answers 429, which is not retried.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.newClient()
			if err != nil {
				return err
			}
			u, err := client.Usage(cmd.Context())
			if err != nil {
				return err
			}
			// The API's own field names, as 'sdo account' prints them.
			return a.printJSON(cmd.OutOrStdout(), map[string]any{
				"IsActive":                   u.IsActive,
				"ConcurrentRequest":          u.ConcurrentRequest,
				"MaxMonthlyRequest":          u.MaxMonthlyRequest,
				"RemainingConcurrentRequest": u.RemainingConcurrentRequest,
				"RemainingMonthlyRequest":    u.RemainingMonthlyRequest,
			})
		},
	}
}
