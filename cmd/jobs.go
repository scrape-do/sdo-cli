// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"fmt"
	"strings"

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
	return &cobra.Command{
		Use:   "list",
		Short: "List every job visible to the caller",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.newClient()
			if err != nil {
				return err
			}
			jobs, err := client.Jobs().List(cmd.Context())
			if err != nil {
				return err
			}
			return a.printJSON(cmd.OutOrStdout(), jobs)
		},
	}
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
