// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	sdo "github.com/scrape-do/sdo-go"
	"github.com/spf13/cobra"
)

func newJobsCmd(a *app) *cobra.Command {
	jobs := &cobra.Command{
		Use:   "jobs",
		Short: "Manage async scrape jobs (q.scrape.do)",
	}
	jobs.AddCommand(
		newJobsCreateCmd(a),
		newJobsCreatePluginCmd(a),
		newJobsStatusCmd(a),
		newJobsResultCmd(a),
		newJobsListCmd(a),
		newJobsCancelCmd(a),
	)
	groupCommand(jobs)
	return jobs
}

func newJobsCreateCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "create <url> [url...]",
		Short:   "Submit an async job for one or more target URLs",
		Args:    cobra.MinimumNArgs(1),
		Example: `  sdo jobs create https://a.com https://b.com --option Super=true --option GeoCode=us`,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, _ := cmd.Flags().GetStringArray("option")
			options, err := parseOptions(raw)
			if err != nil {
				return err
			}
			client, err := a.newClient()
			if err != nil {
				return err
			}
			job, err := client.Jobs().Create(cmd.Context(), args, options)
			if err != nil {
				return err
			}
			a.printErr(cmd, "created job %s (%d tasks)", job.JobID, len(job.TaskIDs))
			return a.printJSON(cmd.OutOrStdout(), job)
		},
	}
	cmd.Flags().StringArray("option", nil, "job body option as Key=Value, e.g. Super=true (repeatable)")
	return cmd
}

func newJobsCreatePluginCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-plugin <key>",
		Short: "Submit an async plugin job (one task per params entry)",
		Long: "Submits a plugin job: <key> names the plugin (amazon/pdp, google/search, ...)\n" +
			"and every entry of the params array becomes one task, with the plugin's own\n" +
			"parameter names (up to 1000 entries). Give the array with --params as JSON,\n" +
			"or with --params-file (a path, or - for stdin). --option adds job body\n" +
			"options such as GeoCode or WebhookURL.",
		Args: cobra.ExactArgs(1),
		Example: "  sdo jobs create-plugin amazon/pdp --params '[{\"asin\":\"B0C7BKZ883\",\"geocode\":\"us\"}]'\n" +
			"  sdo jobs create-plugin google/search --params-file queries.json --option WebhookURL=https://example.com/hook",
		RunE: func(cmd *cobra.Command, args []string) error {
			params, err := readPluginParams(cmd)
			if err != nil {
				return err
			}
			raw, _ := cmd.Flags().GetStringArray("option")
			options, err := parseOptions(raw)
			if err != nil {
				return err
			}
			client, err := a.newClient()
			if err != nil {
				return err
			}
			job, err := client.Jobs().CreatePlugin(cmd.Context(), args[0], params, options)
			if err != nil {
				return err
			}
			a.printErr(cmd, "created job %s (%d tasks)", job.JobID, len(job.TaskIDs))
			return a.printJSON(cmd.OutOrStdout(), job)
		},
	}
	cmd.Flags().String("params", "", "the params array as JSON, one object per task")
	cmd.Flags().String("params-file", "", "read the params array from a JSON file (- for stdin)")
	cmd.Flags().StringArray("option", nil, "job body option as Key=Value, e.g. GeoCode=us (repeatable)")
	return cmd
}

// maxParamsFileBytes bounds what --params-file reads (1000 tasks of generous
// params fit easily).
const maxParamsFileBytes = 8 << 20

// readPluginParams takes the params array from --params or --params-file
// (exactly one of them).
func readPluginParams(cmd *cobra.Command) ([]map[string]any, error) {
	f := cmd.Flags()
	inline, file := f.Changed("params"), f.Changed("params-file")
	var raw string
	switch {
	case inline && file:
		return nil, usageErrorf("--params and --params-file are mutually exclusive")
	case inline:
		raw, _ = f.GetString("params")
	case file:
		path, _ := f.GetString("params-file")
		var r io.Reader
		if path == "-" {
			r = cmd.InOrStdin()
		} else {
			fh, err := os.Open(path)
			if err != nil {
				return nil, usageErrorf("--params-file: %v", err)
			}
			defer fh.Close()
			r = fh
		}
		b, err := io.ReadAll(io.LimitReader(r, maxParamsFileBytes+1))
		if err != nil {
			return nil, usageErrorf("--params-file: %v", err)
		}
		if len(b) > maxParamsFileBytes {
			return nil, usageErrorf("--params-file is larger than %d bytes", maxParamsFileBytes)
		}
		raw = string(b)
	default:
		return nil, usageErrorf("give the params array with --params or --params-file")
	}
	var list []any
	if err := decodeJSONInto(raw, &list); err != nil || list == nil {
		return nil, usageErrorf("params must be a JSON array of objects, one per task")
	}
	params := make([]map[string]any, len(list))
	for i, v := range list {
		obj, ok := v.(map[string]any)
		if !ok {
			return nil, usageErrorf("params entry %d is not a JSON object", i)
		}
		params[i] = obj
	}
	return params, nil
}

func newJobsStatusCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "status <jobID>",
		Short: "Show the status of a job and its tasks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.newClient()
			if err != nil {
				return err
			}
			st, err := client.Jobs().Status(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.printJSON(cmd.OutOrStdout(), st)
		},
	}
}

func newJobsResultCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "result <jobID> <taskID>",
		Short: "Fetch the delivered content of a completed task",
		Long: "Writes the task's content to stdout exactly as delivered (base64 content is\n" +
			"decoded, so binary targets come out byte for byte); metadata goes to stderr.\n" +
			"With --json, prints the whole task result as JSON instead.\n\n" +
			"Exits 0 only when the task succeeded. A failed or canceled task exits 1,\n" +
			"and a task that has not finished yet (queued, pending, ...) exits 4, so a\n" +
			"script can poll until the exit code is no longer 4. In either case nothing\n" +
			"is written to stdout, except the JSON with --json.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.newClient()
			if err != nil {
				return err
			}
			tr, err := client.Jobs().Result(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				if err := a.printJSON(cmd.OutOrStdout(), tr); err != nil {
					return err
				}
				return taskOutcome(tr)
			}
			a.printErr(cmd, "status=%d url=%s task=%s state=%s", tr.StatusCode, tr.URL, tr.TaskID, tr.Status)
			if err := taskOutcome(tr); err != nil {
				return err
			}
			body, err := tr.ContentBytes()
			if err != nil {
				return err
			}
			return a.writeBody(cmd, maskToken(body, a.secret))
		},
	}
	cmd.Flags().Bool("json", false, "print the full task result as JSON")
	cmd.Flags().Bool("raw", false, rawFlagUsage)
	return cmd
}

// taskPendingError reports a task that has not finished yet (exit 4).
type taskPendingError struct{ error }

func (e taskPendingError) Unwrap() error { return e.error }

// taskOutcome maps a task's state to the command's result: nil only for a
// successful task, so `sdo jobs result ... > page.html` cannot leave an empty
// file behind a zero exit code.
func taskOutcome(tr *sdo.TaskResult) error {
	state := strings.ToLower(tr.Status)
	switch state {
	case "success":
		return nil
	case "queuing", "queued", "pending", "rotating":
		return taskPendingError{fmt.Errorf("task %s has not finished yet (state=%s); try again later", tr.TaskID, state)}
	case "error", "canceled", "cancelled":
		msg := fmt.Sprintf("task %s did not succeed (state=%s)", tr.TaskID, state)
		if tr.ErrorMessage != "" {
			msg += ": " + tr.ErrorMessage
		}
		return errors.New(msg)
	default:
		return fmt.Errorf("task %s is in an unknown state %q; no content was written", tr.TaskID, tr.Status)
	}
}

func newJobsListCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List jobs a page at a time, with optional filters",
		Long: "Prints one page of jobs as JSON (Jobs, TotalCount, PageSize, PageNumber,\n" +
			"TotalPages); the default is the 10 most recent. Filter by --status and by\n" +
			"start time (--from/--to, RFC 3339 such as 2025-01-01T00:00:00Z), choose\n" +
			"--sort, and walk --page up to TotalPages.",
		Args:    cobra.NoArgs,
		Example: "  sdo jobs list --status success --from 2025-01-01T00:00:00Z --page-size 50",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := listOptions(cmd)
			if err != nil {
				return err
			}
			client, err := a.newClient()
			if err != nil {
				return err
			}
			page, err := client.Jobs().List(cmd.Context(), opts)
			if err != nil {
				return err
			}
			a.printErr(cmd, "page %d of %d (%d jobs in total)", page.PageNumber, page.TotalPages, page.TotalCount)
			return a.printJSON(cmd.OutOrStdout(), page)
		},
	}
	f := cmd.Flags()
	f.Int("page", 0, "page number, from 1 (default 1)")
	f.Int("page-size", 0, "jobs per page, 1-100 (default 10)")
	f.String("status", "", "only jobs in this state: queuing, queued, pending, rotating, success, error, canceled")
	f.String("from", "", "only jobs started at or after this RFC 3339 time")
	f.String("to", "", "only jobs started at or before this RFC 3339 time")
	f.String("sort", "", "start_time_desc (newest first, the default) or start_time_asc")
	return cmd
}

// listOptions maps the list flags to the SDK's options; the SDK validates the
// values, so a bad page size or status is the same error everywhere.
func listOptions(cmd *cobra.Command) (*sdo.ListJobsOptions, error) {
	f := cmd.Flags()
	opts := &sdo.ListJobsOptions{}
	if f.Changed("page") {
		opts.Page, _ = f.GetInt("page")
		if opts.Page < 1 {
			return nil, usageErrorf("--page must be 1 or more")
		}
	}
	if f.Changed("page-size") {
		opts.PageSize, _ = f.GetInt("page-size")
		if opts.PageSize < 1 {
			return nil, usageErrorf("--page-size must be 1 to 100")
		}
	}
	opts.Status, _ = f.GetString("status")
	opts.Sort, _ = f.GetString("sort")
	for _, b := range []struct {
		flag string
		dst  *time.Time
	}{{"from", &opts.StartFrom}, {"to", &opts.StartTo}} {
		v, _ := f.GetString(b.flag)
		if v == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, usageErrorf("--%s must be an RFC 3339 time such as 2025-01-01T00:00:00Z", b.flag)
		}
		*b.dst = t
	}
	return opts, nil
}

func newJobsCancelCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <jobID>",
		Short: "Cancel a job by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.newClient()
			if err != nil {
				return err
			}
			if err := client.Jobs().Cancel(cmd.Context(), args[0]); err != nil {
				return err
			}
			a.printErr(cmd, "canceled job %s", args[0])
			return nil
		},
	}
}

func newAccountCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "account",
		Short: "Show account concurrency and credit snapshot",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.newClient()
			if err != nil {
				return err
			}
			acc, err := client.Jobs().Me(cmd.Context())
			if err != nil {
				return err
			}
			// Present the SDK's corrected field name; the API misspells the wire
			// key as "AvaliableCredits" and the CLI must not leak that typo.
			return a.printJSON(cmd.OutOrStdout(), map[string]any{
				"TotalConcurrency": acc.TotalConcurrency,
				"FreeConcurrency":  acc.FreeConcurrency,
				"ActiveJobs":       acc.ActiveJobs,
				"AvailableCredits": acc.AvailableCredits,
			})
		},
	}
}

// parseOptions maps repeatable --option Key=Value flags to a job-body map. Each
// value is decoded as JSON when possible (so Super=true is a bool, Count=3 a
// number, kept exact) and falls back to the raw string otherwise (GeoCode=us).
func parseOptions(raw []string) (map[string]any, error) {
	opts := map[string]any{}
	for _, kv := range raw {
		k, v, found := strings.Cut(kv, "=")
		if !found || k == "" {
			return nil, usageErrorf("invalid --option %q (expected Key=Value)", kv)
		}
		if parsed, err := decodeJSONArg(v); err == nil {
			opts[k] = parsed
		} else {
			opts[k] = v
		}
	}
	return opts, nil
}
