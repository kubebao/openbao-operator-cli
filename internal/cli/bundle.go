package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
	"github.com/kubebao/openbao-operator-cli/internal/bundle"
	"github.com/kubebao/openbao-operator-cli/internal/collect"
	"github.com/kubebao/openbao-operator-cli/internal/doctor"
	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

type bundleResult struct {
	SchemaVersion      string `json:"schemaVersion"`
	FormatVersion      string `json:"formatVersion"`
	OutputFile         string `json:"outputFile"`
	ResultCode         string `json:"resultCode"`
	CollectionResult   string `json:"collectionResult"`
	DiagnosticComplete bool   `json:"diagnosticComplete"`
	ExitCode           int    `json:"exitCode"`
}

func newBundleCommand(info buildinfo.Info) *cobra.Command {
	options := collect.Options{RequestTimeout: 30 * time.Second, UserAgent: "kubebao/" + info.Version}
	output, outputFile := "text", ""
	overwrite := false
	var maxBackupAge time.Duration
	command := &cobra.Command{
		Use:   "support-bundle NAME --output-file FILE",
		Short: "Write a local support bundle from bounded, projected Kubernetes evidence",
		Long: "Collect the same read-only evidence as doctor and write a private local archive.\n" +
			"Logs are not collected. Review resource names and approved metadata before sharing.",
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if output != "text" && output != "json" {
				return fmt.Errorf("unsupported output format %q: use text or json", output)
			}
			if err := validateBackupAge(command, maxBackupAge); err != nil {
				return err
			}
			if err := bundle.CheckOutput(outputFile, overwrite); err != nil {
				return err
			}
			path, err := filepath.Abs(outputFile)
			if err != nil {
				return errors.New("cannot resolve output file")
			}
			options.Name = args[0]
			client, err := collect.NewClient(options)
			if err != nil {
				return err
			}
			snapshot := client.CollectDiagnostic(command.Context())
			policy := diagnosticPolicy(snapshot, maxBackupAge)
			stored := bundle.New(snapshot, policy, info, options.RequestTimeout)
			analysis, err := doctor.Analyze(stored.Snapshot(), policy)
			if err != nil {
				return errors.New("cannot analyze support bundle evidence")
			}
			// Cancellation ends collection. A separate bounded finalization window
			// preserves already captured evidence after an interrupt.
			finish, cancel := context.WithTimeout(context.WithoutCancel(command.Context()), 10*time.Second)
			defer cancel()
			if err := bundle.Write(finish, path, stored, overwrite); err != nil {
				return err
			}
			result := bundleResult{
				SchemaVersion: "v1", FormatVersion: bundle.FormatVersion, OutputFile: evidence.Clean(path),
				ResultCode: "completed", CollectionResult: snapshot.ResultCode, DiagnosticComplete: analysis.Complete,
			}
			if snapshot.ResultCode != "completed" {
				result.ResultCode, result.ExitCode = "partial", 3
			}
			if output == "json" {
				if err := writeJSON(command.OutOrStdout(), result); err != nil {
					return err
				}
			} else if _, err := fmt.Fprintf(command.OutOrStdout(), "Support bundle: %s\nCollection: %s\n",
				result.OutputFile, result.CollectionResult); err != nil {
				return fmt.Errorf("write support bundle result: %w", err)
			}
			if result.ExitCode != 0 {
				return commandResult{code: result.ExitCode, message: "partial support bundle saved"}
			}
			return nil
		},
	}
	addKubernetesFlags(command, &options)
	addBackupAgeFlag(command, &maxBackupAge)
	command.Flags().StringVar(&outputFile, "output-file", "", "Local archive destination (required)")
	command.Flags().BoolVar(&overwrite, "overwrite", false, "Replace an existing regular output file")
	command.Flags().StringVarP(&output, "output", "o", output, "Result format: text or json")
	return command
}
