// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

func newPluginCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin <route>",
		Short: "Call a documented plugin endpoint (e.g. google/shopping)",
		Args:  cobra.ExactArgs(1),
		Long: "Calls a documented plugin endpoint and writes its output to stdout (JSON indented,\n" +
			"anything else as it is); a metadata line (status, cost) goes to stderr. Each\n" +
			"call spends Scrape.do credits. The route \"amazon\" is the Amazon raw-HTML plugin\n" +
			"(/plugin/amazon/). A parameter that takes several values (hotel_class,\n" +
			"departure_id, ...) takes one comma-separated value.",
		Example: "  sdo plugin google/shopping --param q=laptop --param gl=us\n" +
			"  sdo plugin amazon --param url=https://www.amazon.com/dp/B0C7BKZ883 --param geocode=us",
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, _ := cmd.Flags().GetStringArray("param")
			params, err := parseParams(raw)
			if err != nil {
				return err
			}
			client, err := a.newClient()
			if err != nil {
				return err
			}
			res, err := client.PluginResult(cmd.Context(), args[0], params)
			if err != nil {
				return err
			}
			a.printErr(cmd, "%s", resultMeta(res))
			if !strings.Contains(strings.ToLower(res.ContentType()), "json") {
				return a.writeBody(cmd, maskToken(res.Bytes(), a.secret))
			}
			var out any
			if err := res.JSON(&out); err != nil {
				return err
			}
			return a.printJSON(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().StringArray("param", nil, "plugin parameter as key=value (repeatable; a repeated key sends a list)")
	cmd.Flags().Bool("raw", false, rawFlagUsage)
	return cmd
}

// parseParams maps repeatable --param key=value flags to plugin parameters. A
// key given more than once becomes a list, which the SDK sends as a repeated
// query key (tags=a&tags=b).
func parseParams(raw []string) (map[string]any, error) {
	params := map[string]any{}
	for _, kv := range raw {
		k, v, found := strings.Cut(kv, "=")
		if !found || k == "" {
			return nil, usageErrorf("invalid --param %q (expected key=value)", kv)
		}
		if strings.EqualFold(k, "token") {
			return nil, usageErrorf("--param %s: the token comes from --token, SDO_TOKEN or 'sdo auth login'", k)
		}
		switch prev := params[k].(type) {
		case nil:
			params[k] = v
		case string:
			params[k] = []string{prev, v}
		case []string:
			params[k] = append(prev, v)
		}
	}
	return params, nil
}
