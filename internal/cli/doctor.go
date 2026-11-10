package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
	"github.com/kubebao/openbao-operator-cli/internal/collect"
	"github.com/kubebao/openbao-operator-cli/internal/doctor"
)

func newDoctorCommand(info buildinfo.Info) *cobra.Command {
	options := collect.Options{RequestTimeout: 30 * time.Second, UserAgent: "kubebao/" + info.Version}
	output, failOn := "text", "warning"
	verbose := false
	command := &cobra.Command{
		Use:   "doctor NAME",
		Short: "Diagnose workload readiness and recovery restrictions from Kubernetes evidence",
		Long: "Collect approved cluster, StatefulSet, Pod, and restore observations.\n" +
			"Rules interpret captured evidence without contacting OpenBao or changing cluster state.\n" +
			"Missing or unreadable evidence remains unknown.",
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if output != "text" && output != "json" {
				return fmt.Errorf("unsupported output format %q: use text or json", output)
			}
			threshold, err := doctor.ParseSeverity(failOn)
			if err != nil {
				return err
			}
			options.Name = args[0]
			client, err := collect.NewClient(options)
			if err != nil {
				return err
			}
			report := doctor.Analyze(client.CollectDiagnostic(command.Context()), threshold)
			if err := doctor.Write(command.OutOrStdout(), report, output, verbose); err != nil {
				return err
			}
			if report.ExitCode != 0 {
				return commandResult{code: report.ExitCode, message: "diagnosis result: " + report.ResultCode}
			}
			return nil
		},
	}
	addKubernetesFlags(command, &options)
	command.Flags().StringVarP(&output, "output", "o", output, "Output format: text or json")
	command.Flags().
		BoolVarP(&verbose, "verbose", "v", verbose, "Include evidence references, rule coverage, and successful collection steps")
	command.Flags().StringVar(&failOn, "fail-on", failOn, "Finding threshold for exit code 2: info, warning, error, or none")
	return command
}
