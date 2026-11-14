package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
	"github.com/kubebao/openbao-operator-cli/internal/bundle"
	"github.com/kubebao/openbao-operator-cli/internal/collect"
)

func newDoctorCommand(info buildinfo.Info, term *terminal) *cobra.Command {
	options := collect.Options{RequestTimeout: 30 * time.Second, UserAgent: "kubebao/" + info.Version}
	output, failOn := "text", "warning"
	verbose := false
	bundlePath := ""
	var maxBackupAge time.Duration
	command := &cobra.Command{
		Use:   "doctor [NAME] [--bundle FILE]",
		Short: "Diagnose a cluster from live or bundled evidence",
		Long: "Check one OpenBaoCluster and the StatefulSets, Pods, and restores it owns, then report\n" +
			"findings with a suggested next step and the resources that were examined.\n\n" +
			"Doctor is read-only. Rules run on collected Kubernetes evidence, never on live OpenBao probes,\n" +
			"and evidence that could not be read is reported as unknown. Use --bundle to diagnose a\n" +
			"support bundle offline.",
		Example: "  kubebao doctor prod -n security\n" +
			"  kubebao doctor prod -n security --verbose\n" +
			"  kubebao doctor prod -n security --fail-on error --max-backup-age 48h\n" +
			"  kubebao doctor --bundle prod-support.tar.gz",
		Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if output != "text" && output != "json" {
				return fmt.Errorf("unsupported output format %q: use text or json", output)
			}
			if err := parseFailureThreshold(failOn); err != nil {
				return err
			}
			if err := validateBackupAge(command, maxBackupAge); err != nil {
				return err
			}
			var report diagnosticReport
			if command.Flags().Changed("bundle") {
				var err error
				report, err = offlineDiagnosis(command, args, bundlePath, failOn)
				if err != nil {
					return err
				}
			} else {
				if len(args) != 1 {
					return errors.New("specify a cluster name or --bundle FILE")
				}
				options.Name = args[0]
				client, err := collect.NewClient(options)
				if err != nil {
					return err
				}
				snapshot := client.CollectDiagnostic(command.Context())
				report = diagnosticResultWithPolicy(snapshot, failOn, diagnosticPolicy(snapshot, maxBackupAge))
			}
			if err := writeDiagnostic(term.stdout(command), report, output, verbose); err != nil {
				return err
			}
			if report.ExitCode != 0 {
				return commandResult{code: report.ExitCode, message: "diagnosis result: " + report.ResultCode}
			}
			return nil
		},
	}
	addKubernetesFlags(command, &options)
	command.ValidArgsFunction = func(c *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if c.Flags().Changed("bundle") {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completeClusterNames(&options)(c, args, toComplete)
	}
	command.Flags().StringVarP(&output, "output", "o", output, "Output format: text or json")
	completeOutput(command)
	command.Flags().
		BoolVarP(&verbose, "verbose", "v", verbose, "Include evidence references, rule coverage, and successful collection steps")
	command.Flags().StringVar(&failOn, "fail-on", failOn, "Finding threshold for exit code 2: info, warning, error, or none")
	command.Flags().StringVar(&bundlePath, "bundle", "", "Diagnose a local support bundle without cluster access")
	addBackupAgeFlag(command, &maxBackupAge)
	_ = command.RegisterFlagCompletionFunc("fail-on", cobra.FixedCompletions([]cobra.Completion{"info", "warning", "error", "none"},
		cobra.ShellCompDirectiveNoFileComp))
	return command
}

func offlineDiagnosis(command *cobra.Command, args []string, path, failOn string) (diagnosticReport, error) {
	if path == "" || len(args) != 0 {
		return diagnosticReport{}, errors.New("--bundle requires a file and cannot be combined with a cluster name")
	}
	for _, name := range []string{"kubeconfig", "context", "namespace", "api-group", "request-timeout", "max-backup-age"} {
		if command.Flags().Changed(name) {
			return diagnosticReport{}, fmt.Errorf("--%s cannot be combined with --bundle; offline analysis uses the recorded policy", name)
		}
	}
	stored, err := bundle.Read(command.Context(), path)
	if err != nil {
		return diagnosticReport{}, err
	}
	report := diagnosticResultWithPolicy(stored.Snapshot(), failOn, stored.Manifest.Policy)
	report.Source = "bundle"
	return report, nil
}

func addBackupAgeFlag(command *cobra.Command, limit *time.Duration) {
	command.Flags().DurationVar(limit, "max-backup-age", 0, "Declared maximum backup age; omit to skip age evaluation (for example, 48h)")
}

func validateBackupAge(command *cobra.Command, limit time.Duration) error {
	if command.Flags().Changed("max-backup-age") && limit <= 0 {
		return errors.New("--max-backup-age must be a positive duration")
	}
	return nil
}
