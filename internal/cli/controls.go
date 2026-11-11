package cli

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
	"github.com/kubebao/openbao-operator-cli/internal/collect"
	"github.com/kubebao/openbao-operator-cli/internal/control"
	"github.com/kubebao/openbao-operator-client/operations"
)

func newPauseCommand(info buildinfo.Info, enabled bool) *cobra.Command {
	name, description := "pause", "Pause operator reconciliation for one OpenBaoCluster"
	if !enabled {
		name, description = "resume", "Resume operator reconciliation for one OpenBaoCluster"
	}
	return newControlCommand(info, name, description, operations.ControlPause, enabled)
}

func newMaintenanceCommand(info buildinfo.Info) *cobra.Command {
	command := &cobra.Command{
		Use: "maintenance", Short: "Set maintenance mode without changing reconciliation pause",
		Args: cobra.NoArgs,
	}
	command.AddCommand(
		newControlCommand(info, "enable", "Enable maintenance mode for one OpenBaoCluster", operations.ControlMaintenance, true),
		newControlCommand(info, "disable", "Disable maintenance mode for one OpenBaoCluster", operations.ControlMaintenance, false),
	)
	return command
}

func newControlCommand(info buildinfo.Info, name, description string, kind operations.Control, enabled bool) *cobra.Command {
	access := collect.Options{RequestTimeout: 30 * time.Second, UserAgent: "kubebao/" + info.Version}
	options := control.Options{Control: kind, Enabled: enabled, WaitTimeout: 2 * time.Minute, PollInterval: 2 * time.Second}
	output := "text"
	command := &cobra.Command{
		Use: name + " NAME", Short: description,
		Long: description + ".\n" +
			"Submit one field-scoped PATCH guarded by UID, resource version, and generation.\n" +
			"API acceptance does not establish operator observation. Use --wait to collect and assess later evidence.\n" +
			"An unknown submission outcome is never retried. Maintenance never resumes a paused cluster.",
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
			report := control.Run(command.Context(), client, options)
			if err := writeControl(command.OutOrStdout(), report, output); err != nil {
				return err
			}
			if report.ExitCode != 0 {
				return commandResult{code: report.ExitCode, message: "control result: " + report.ResultCode}
			}
			return nil
		},
	}
	addKubernetesFlags(command, &access)
	command.Flags().StringVarP(&output, "output", "o", output, "Output format: text or json")
	command.Flags().BoolVar(&options.DryRun, "dry-run", false, "Validate through API server dry-run without persisting the control")
	command.Flags().BoolVar(&options.Wait, "wait", false, "Wait for later evidence of operator observation or maintenance propagation")
	command.Flags().
		DurationVar(&options.WaitTimeout, "wait-timeout", options.WaitTimeout, "Maximum duration of observation after submission")
	command.Flags().
		DurationVar(&options.PollInterval, "poll-interval", options.PollInterval, "Delay between observation attempts; minimum 100ms")
	return command
}

func writeControl(writer io.Writer, report control.Report, output string) error {
	if output == "json" {
		return writeJSON(writer, report)
	}
	if _, err := fmt.Fprintf(writer, "Cluster: %s/%s\nAPI group: %s\nControl result: %s\n", report.Target.Namespace,
		report.Target.Name, report.Target.APIGroup, report.ResultCode); err != nil {
		return fmt.Errorf("write control result: %w", err)
	}
	if report.Submission != nil {
		if _, err := fmt.Fprintf(writer, "API submission: %s\n", report.Submission.Outcome); err != nil {
			return fmt.Errorf("write submission result: %w", err)
		}
	}
	if report.Assessment != nil {
		if _, err := fmt.Fprintf(writer, "Observation: %s\nReasons: %v\n", report.Assessment.State, report.Assessment.Reasons); err != nil {
			return fmt.Errorf("write control assessment: %w", err)
		}
	} else if report.Submission != nil {
		if _, err := fmt.Fprintln(writer, "Operator observation: not assessed"); err != nil {
			return fmt.Errorf("write observation limit: %w", err)
		}
	}
	if len(report.Warnings) > 0 {
		if _, err := fmt.Fprintf(writer, "Warnings: %v\n", report.Warnings); err != nil {
			return fmt.Errorf("write control warnings: %w", err)
		}
	}
	return nil
}
