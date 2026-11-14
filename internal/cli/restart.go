package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	shared "github.com/kubebao/openbao-operator-client/evidence"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
	"github.com/kubebao/openbao-operator-cli/internal/collect"
	"github.com/kubebao/openbao-operator-cli/internal/control"
)

func newRestartCommand(info buildinfo.Info) *cobra.Command {
	access := collect.Options{RequestTimeout: 30 * time.Second, UserAgent: "kubebao/" + info.Version}
	options := control.Options{WaitTimeout: 2 * time.Minute, PollInterval: 2 * time.Second}
	output := "text"
	command := &cobra.Command{
		Use: "restart NAME", Short: "Request an operator-managed rolling restart",
		Long: "Request an operator-managed rolling restart through spec.runtime.restartAt.\n" +
			"Submit one PATCH guarded by UID, resource version, and generation.\n" +
			"Use --wait to assess the requested marker, workload revisions, and readiness.\n" +
			"An unknown submission is never retried. Waiting does not establish serving health or quorum.",
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if output != "text" && output != "json" {
				return fmt.Errorf("unsupported output format %q: use text or json", output)
			}
			if options.Wait && options.DryRun {
				return errors.New("--wait cannot be combined with --dry-run")
			}
			if options.WaitTimeout <= 0 || options.PollInterval < 100*time.Millisecond {
				return errors.New("wait timeout must be positive and poll interval must be at least 100ms")
			}
			access.Name, options.RequestTimeout = args[0], access.RequestTimeout
			client, err := collect.NewClient(access)
			if err != nil {
				return err
			}
			report := control.RunRestart(command.Context(), client, options)
			if err := writeRestart(command.OutOrStdout(), report, output); err != nil {
				return err
			}
			if report.ExitCode != 0 {
				return commandResult{code: report.ExitCode, message: "restart result: " + report.ResultCode}
			}
			return nil
		},
	}
	addKubernetesFlags(command, &access)
	command.Flags().StringVarP(&output, "output", "o", output, "Output format: text or json")
	command.Flags().BoolVar(&options.DryRun, "dry-run", false, "Validate through API server dry-run without persisting the restart")
	command.Flags().BoolVar(&options.Wait, "wait", false, "Wait for the requested marker, workload revision, and readiness")
	command.Flags().
		DurationVar(&options.WaitTimeout, "wait-timeout", options.WaitTimeout, "Maximum duration of observation after submission")
	command.Flags().
		DurationVar(&options.PollInterval, "poll-interval", options.PollInterval, "Delay between observation attempts; minimum 100ms")
	return command
}

func writeRestart(writer io.Writer, report control.RestartReport, output string) error {
	if output == "json" {
		return writeJSON(writer, report)
	}
	if _, err := fmt.Fprintf(writer, "Cluster: %s/%s\nAPI group: %s\nRestart result: %s\n", report.Target.Namespace,
		report.Target.Name, report.Target.APIGroup, report.ResultCode); err != nil {
		return fmt.Errorf("write restart result: %w", err)
	}
	if report.Plan != nil {
		if _, err := fmt.Fprintf(writer, "Restart marker: %s\n", report.Plan.RestartAt); err != nil {
			return fmt.Errorf("write restart marker: %w", err)
		}
	}
	if report.Submission != nil {
		if _, err := fmt.Fprintf(writer, "API submission: %s\n", report.Submission.Outcome); err != nil {
			return fmt.Errorf("write restart submission: %w", err)
		}
	}
	if report.Assessment != nil {
		if _, err := fmt.Fprintf(
			writer,
			"Marker configured: %s\nWorkload convergence: %s\nRollout observation: %s\nReasons: %v\n",
			restartBoolean(report.Assessment.Configured),
			restartBoolean(report.Assessment.OperatorObserved),
			report.Assessment.State,
			report.Assessment.Reasons,
		); err != nil {
			return fmt.Errorf("write restart assessment: %w", err)
		}
	} else if report.Submission != nil {
		if _, err := fmt.Fprintln(writer, "Rollout observation: not assessed"); err != nil {
			return fmt.Errorf("write restart observation limit: %w", err)
		}
	}
	return nil
}

func restartBoolean(value shared.Boolean) string {
	if value.State == shared.ValueReported && value.Value != nil {
		return strconv.FormatBool(*value.Value)
	}
	return string(value.State)
}
