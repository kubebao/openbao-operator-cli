package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
	"github.com/kubebao/openbao-operator-cli/internal/collect"
	"github.com/kubebao/openbao-operator-cli/internal/control"
)

func newRestartCommand(info buildinfo.Info, term *terminal) *cobra.Command {
	access := collect.Options{RequestTimeout: 30 * time.Second, UserAgent: "kubebao/" + info.Version}
	options := control.Options{WaitTimeout: 2 * time.Minute, PollInterval: 2 * time.Second}
	output := "text"
	command := &cobra.Command{
		Use: "restart NAME", Short: "Request a rolling restart managed by the operator",
		Long: "Request a rolling restart of one OpenBaoCluster by writing a new timestamp to\n" +
			"spec.runtime.restartAt. The operator replaces Pods one at a time; kubebao never deletes Pods.\n\n" +
			"The request is not submitted while the cluster is paused, holds a restore or operation lock,\n" +
			"is in break glass, or runs a blue/green upgrade. With --wait, kubebao waits until every\n" +
			"StatefulSet and Pod carries the new marker and reports Ready. That does not prove serving\n" +
			"health or Raft quorum." + guardedRequestHelp,
		Example: "  kubebao restart prod -n security --dry-run\n" +
			"  kubebao restart prod -n security --wait --wait-timeout 10m",
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if output != "text" && output != "json" {
				return fmt.Errorf("unsupported output format %q: use text or json", output)
			}
			if err := validateControlOptions(command, &options); err != nil {
				return err
			}
			access.Name, options.RequestTimeout = args[0], access.RequestTimeout
			client, err := collect.NewClient(access)
			if err != nil {
				return err
			}
			progress := waitProgress(term.stderr(command), &options)
			report := control.RunRestart(command.Context(), client, options)
			progress.Clear()
			if output == "json" {
				err = writeJSON(command.OutOrStdout(), report)
			} else {
				out := term.stdout(command)
				err = control.WriteRestartText(out, report, control.TextOptions{Width: out.Width()})
			}
			if err != nil {
				return err
			}
			if report.ExitCode != 0 {
				return commandResult{code: report.ExitCode, message: "restart result: " + report.ResultCode}
			}
			return nil
		},
	}
	addKubernetesFlags(command, &access)
	addClusterArgument(command, &access)
	addControlFlags(command, &options, &output)
	return command
}
