package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kubebao/openbao-operator-client/operations"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
	"github.com/kubebao/openbao-operator-cli/internal/collect"
	"github.com/kubebao/openbao-operator-cli/internal/control"
	"github.com/kubebao/openbao-operator-cli/internal/render"
)

type controlHelp struct {
	short, long, example string
}

func newPauseCommand(info buildinfo.Info, term *terminal, enabled bool) *cobra.Command {
	if enabled {
		return newControlCommand(info, term, "pause", controlHelp{
			short: "Pause operator reconciliation for a cluster",
			long: "Pause operator reconciliation for one OpenBaoCluster by setting spec.paused=true.\n" +
				"The operator stops changing the cluster's resources until it is resumed. Pausing does not\n" +
				"stop OpenBao or Jobs that already started, and it does not enable maintenance mode.",
			example: "  kubebao pause prod -n security --wait",
		}, operations.ControlPause, true)
	}
	return newControlCommand(info, term, "resume", controlHelp{
		short: "Resume operator reconciliation for a cluster",
		long: "Resume operator reconciliation for one OpenBaoCluster by setting spec.paused=false.\n" +
			"Resuming does not change maintenance mode.",
		example: "  kubebao resume prod -n security --wait",
	}, operations.ControlPause, false)
}

func newMaintenanceCommand(info buildinfo.Info, term *terminal) *cobra.Command {
	command := &cobra.Command{
		Use: "maintenance", Short: "Enable or disable maintenance mode",
		Long: "Set spec.maintenance.enabled on one OpenBaoCluster. The operator propagates maintenance\n" +
			"to its StatefulSets and Pods. Maintenance and pause are independent: maintenance never\n" +
			"resumes a paused cluster, and a paused operator cannot propagate maintenance.",
		Args: cobra.NoArgs,
	}
	command.AddCommand(
		newControlCommand(info, term, "enable", controlHelp{
			short:   "Enable maintenance mode for a cluster",
			long:    "Enable maintenance mode for one OpenBaoCluster by setting spec.maintenance.enabled=true.",
			example: "  kubebao maintenance enable prod -n security --wait",
		}, operations.ControlMaintenance, true),
		newControlCommand(info, term, "disable", controlHelp{
			short:   "Disable maintenance mode for a cluster",
			long:    "Disable maintenance mode for one OpenBaoCluster by setting spec.maintenance.enabled=false.",
			example: "  kubebao maintenance disable prod -n security --dry-run",
		}, operations.ControlMaintenance, false),
	)
	return command
}

const guardedRequestHelp = "\n\nThe request changes only that field and is rejected if the cluster changed since it was read.\n" +
	"API acceptance does not mean the operator has acted on it; use --wait to watch for that.\n" +
	"A request with an unknown outcome is never retried."

func newControlCommand(
	info buildinfo.Info, term *terminal, name string, help controlHelp, kind operations.Control, enabled bool,
) *cobra.Command {
	access := collect.Options{RequestTimeout: 30 * time.Second, UserAgent: "kubebao/" + info.Version}
	options := control.Options{Control: kind, Enabled: enabled, WaitTimeout: 2 * time.Minute, PollInterval: 2 * time.Second}
	output := "text"
	command := &cobra.Command{
		Use: name + " NAME", Short: help.short, Long: help.long + guardedRequestHelp, Example: help.example,
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if output != "text" && output != "json" {
				return fmt.Errorf("unsupported output format %q: use text or json", output)
			}
			if err := validateWait(options); err != nil {
				return err
			}
			access.Name, options.RequestTimeout = args[0], access.RequestTimeout
			client, err := collect.NewClient(access)
			if err != nil {
				return err
			}
			progress := waitProgress(term.stderr(command), &options)
			report := control.Run(command.Context(), client, options)
			progress.Clear()
			if output == "json" {
				err = writeJSON(command.OutOrStdout(), report)
			} else {
				out := term.stdout(command)
				err = control.WriteText(out, report, options, control.TextOptions{Width: out.Width()})
			}
			if err != nil {
				return err
			}
			if report.ExitCode != 0 {
				return commandResult{code: report.ExitCode, message: "control result: " + report.ResultCode}
			}
			return nil
		},
	}
	addKubernetesFlags(command, &access)
	addControlFlags(command, &options, &output)
	return command
}

func addControlFlags(command *cobra.Command, options *control.Options, output *string) {
	flags := command.Flags()
	flags.StringVarP(output, "output", "o", *output, "Output format: text or json")
	flags.BoolVar(&options.DryRun, "dry-run", false, "Validate through API server dry-run without persisting the change")
	flags.BoolVar(&options.Wait, "wait", false, "Wait until later evidence shows the operator applied the change")
	flags.DurationVar(&options.WaitTimeout, "wait-timeout", options.WaitTimeout, "Maximum time to wait after submission")
	flags.DurationVar(&options.PollInterval, "poll-interval", options.PollInterval, "Delay between checks while waiting; minimum 100ms")
}

func validateWait(options control.Options) error {
	if options.Wait && options.DryRun {
		return errors.New("--wait cannot be combined with --dry-run")
	}
	if options.WaitTimeout <= 0 || options.PollInterval < 100*time.Millisecond {
		return errors.New("wait timeout must be positive and poll interval must be at least 100ms")
	}
	return nil
}

// waitProgress shows a transient line on an interactive standard error while --wait polls.
func waitProgress(stderr *render.Output, options *control.Options) *render.Progress {
	if !options.Wait || !stderr.Terminal() {
		return nil
	}
	progress, started := render.NewProgress(stderr), time.Now()
	options.Observe = func(assessment operations.Assessment) {
		text := fmt.Sprintf(
			"Waiting for the operator (%s of %s)",
			render.Duration(time.Since(started)),
			render.Duration(options.WaitTimeout),
		)
		if reasons := control.Reasons(assessment.Reasons); reasons != "" {
			text += ": " + reasons
		}
		progress.Update(text)
	}
	return progress
}
