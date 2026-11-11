// Package cli implements kubebao commands and process result handling.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
)

// Execute writes command output and returns a process exit code.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	command := newCommand(buildinfo.Current())
	command.SetArgs(args)
	command.SetOut(stdout)
	command.SetErr(stderr)
	if err := command.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		var result interface{ ExitCode() int }
		if errors.As(err, &result) {
			return result.ExitCode()
		}
		return 1
	}
	return 0
}

func newCommand(info buildinfo.Info) *cobra.Command {
	root := &cobra.Command{
		Use:   "kubebao",
		Short: "Companion CLI for the OpenBao Operator",
		Long: "kubebao is a companion CLI for the OpenBao Operator.\n\n" +
			"Read cluster status and diagnose workload or recovery restrictions through Kubernetes.\n" +
			"Create local support bundles and diagnose captured evidence offline. Operator controls are planned.",
		Version:       info.String(),
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(newVersionCommand(info), newStatusCommand(info), newDoctorCommand(info), newBundleCommand(info))
	return root
}

func newVersionCommand(info buildinfo.Info) *cobra.Command {
	output := "text"
	command := &cobra.Command{
		Use:   "version",
		Short: "Print CLI build information without cluster access",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			var err error
			switch output {
			case "text":
				_, err = fmt.Fprintln(command.OutOrStdout(), info.String())
			case "json":
				err = json.NewEncoder(command.OutOrStdout()).Encode(struct {
					SchemaVersion string `json:"schemaVersion"`
					buildinfo.Info
				}{SchemaVersion: "v1", Info: info})
			default:
				return fmt.Errorf("unsupported output format %q: use text or json", output)
			}
			if err != nil {
				return fmt.Errorf("write version: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVarP(&output, "output", "o", output, "Output format: text or json")
	return command
}
