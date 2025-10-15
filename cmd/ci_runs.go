package cmd

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var (
	ciRunsCmd = &cobra.Command{
		Use:   "runs",
		Short: "Inspect CI run history",
		Long:  "Commands for listing and inspecting CI runs recorded on the server.",
	}

	ciRunsListCmd = &cobra.Command{
		Use:   "list",
		Short: "List CI runs for the repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("this command does not accept arguments")
			}

			wd, err := os.Getwd()
			if err != nil {
				return errors.Join(errors.New("get working directory"), err)
			}

			c, err := client.OpenFromFile(cmd.Context(), wd)
			if err != nil {
				return errors.Join(errors.New("open client"), err)
			}
			defer c.Close()
			configureClientOutputs(cmd, c)

			runs, err := c.ListCIRuns()
			if err != nil {
				return errors.Join(errors.New("list CI runs"), err)
			}

			if len(runs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No CI runs found for this repository.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tEvent\tTask\tStatus\tCode\tStarted\tFinished\tConfig\tPattern\tReason")
			for _, run := range runs {
				status := "failure"
				if run.Success {
					status = "success"
				}
				pattern := "-"
				if run.Pattern != nil {
					pattern = *run.Pattern
				}
				fmt.Fprintf(
					w,
					"%d\t%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n",
					run.Id,
					run.EventType,
					run.TaskType,
					status,
					run.StatusCode,
					run.StartedAt,
					run.FinishedAt,
					run.ConfigFilename,
					pattern,
					run.Reason,
				)
			}
			_ = w.Flush()

			return nil
		},
	}

	ciRunsInspectCmd = &cobra.Command{
		Use:   "inspect <run-id>",
		Short: "Inspect the log output of a CI run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid run id %q: %w", args[0], err)
			}
			if runID < 0 || runID > math.MaxInt32 {
				return fmt.Errorf("run id %d is out of range", runID)
			}

			wd, err := os.Getwd()
			if err != nil {
				return errors.Join(errors.New("get working directory"), err)
			}

			c, err := client.OpenFromFile(cmd.Context(), wd)
			if err != nil {
				return errors.Join(errors.New("open client"), err)
			}
			defer c.Close()
			configureClientOutputs(cmd, c)

			resp, err := c.GetCIRun(runID)
			if err != nil {
				return errors.Join(errors.New("fetch CI run"), err)
			}

			run := resp.Run
			status := "failure"
			if run.Success {
				status = "success"
			}
			pattern := "-"
			if run.Pattern != nil {
				pattern = *run.Pattern
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Run ID: %d\n", run.Id)
			fmt.Fprintf(cmd.OutOrStdout(), "Config: %s\n", run.ConfigFilename)
			fmt.Fprintf(cmd.OutOrStdout(), "Event: %s\n", run.EventType)
			fmt.Fprintf(cmd.OutOrStdout(), "Revision: %s\n", run.Rev)
			fmt.Fprintf(cmd.OutOrStdout(), "Pattern: %s\n", pattern)
			fmt.Fprintf(cmd.OutOrStdout(), "Reason: %s\n", run.Reason)
			fmt.Fprintf(cmd.OutOrStdout(), "Task: %s\n", run.TaskType)
			fmt.Fprintf(cmd.OutOrStdout(), "Status: %s\n", status)
			fmt.Fprintf(cmd.OutOrStdout(), "Code: %d\n", run.StatusCode)
			fmt.Fprintf(cmd.OutOrStdout(), "Started: %s\n", run.StartedAt)
			fmt.Fprintf(cmd.OutOrStdout(), "Finished: %s\n", run.FinishedAt)
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), "--- Log ---")
			fmt.Fprintln(cmd.OutOrStdout(), resp.Log)

			return nil
		},
	}
)

func init() {
	ciCmd.AddCommand(ciRunsCmd)
	ciRunsCmd.AddCommand(ciRunsListCmd)
	ciRunsCmd.AddCommand(ciRunsInspectCmd)
}