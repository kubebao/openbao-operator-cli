package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
	"github.com/kubebao/openbao-operator-cli/internal/bundle"
	"github.com/kubebao/openbao-operator-cli/internal/collect"
	"github.com/kubebao/openbao-operator-cli/internal/doctor"
	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	"github.com/kubebao/openbao-operator-cli/internal/render"
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

func newBundleCommand(info buildinfo.Info, term *terminal) *cobra.Command {
	options := collect.Options{RequestTimeout: 30 * time.Second, UserAgent: "kubebao/" + info.Version}
	output, outputFile := "text", ""
	overwrite := false
	var maxBackupAge time.Duration
	command := &cobra.Command{
		Use:   "support-bundle NAME --output-file FILE",
		Short: "Save diagnostic evidence to a local archive",
		Long: "Collect the same read-only evidence as doctor and write it to a private local archive,\n" +
			"together with the findings and a readable summary.\n\n" +
			"Logs and Secrets are not collected. Review resource names and metadata before sharing\n" +
			"the archive. Diagnose it offline with doctor --bundle.",
		Example: "  kubebao support-bundle prod -n security --output-file prod-support.tar.gz\n" +
			"  kubebao doctor --bundle prod-support.tar.gz",
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
			} else if err := writeBundleText(term.stdout(command), result); err != nil {
				return err
			}
			if result.ExitCode != 0 {
				return commandResult{code: result.ExitCode, message: "partial support bundle saved"}
			}
			return nil
		},
	}
	addKubernetesFlags(command, &options)
	addClusterArgument(command, &options)
	addBackupAgeFlag(command, &maxBackupAge)
	command.Flags().StringVar(&outputFile, "output-file", "", "Local archive destination (required)")
	command.Flags().BoolVar(&overwrite, "overwrite", false, "Replace an existing regular output file")
	command.Flags().StringVarP(&output, "output", "o", output, "Result format: text or json")
	completeOutput(command)
	return command
}

func writeBundleText(output *render.Output, result bundleResult) error {
	var b strings.Builder
	if result.ExitCode == 0 {
		b.WriteString(render.Status(render.Good, "Support bundle written") + "\n")
	} else {
		b.WriteString(render.Status(render.Warn, "Partial support bundle written") + "\n")
		b.WriteString("  The cluster " + render.TargetResult(result.CollectionResult) + ". The bundle keeps the evidence that was read.\n")
	}
	diagnosis := "complete"
	if !result.DiagnosticComplete {
		diagnosis = "incomplete"
	}
	b.WriteString("\n" + render.Fields([]render.Field{
		{Label: "File", Value: result.OutputFile},
		{Label: "Collection", Value: render.Humanize(result.CollectionResult)},
		{Label: "Diagnosis", Value: diagnosis},
	}, "  "))
	b.WriteString("\n" + render.Wrap("Logs and Secrets are not included. Review resource names and metadata before sharing.",
		output.Width(), "  ") + "\n")
	b.WriteString("  " + render.Next("Diagnose offline: "+render.Command("kubebao doctor --bundle "+result.OutputFile)) + "\n")
	if _, err := output.WriteString(b.String()); err != nil {
		return fmt.Errorf("write support bundle result: %w", err)
	}
	return nil
}
