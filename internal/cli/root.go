// Package cli implements kubebao commands and process result handling.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
)

// Execute writes command output and returns a process exit code.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return execute(ctx, os.Args[0], args, stdout, stderr)
}

func execute(ctx context.Context, executable string, args []string, stdout, stderr io.Writer) int {
	term := &terminal{environ: os.Environ()}
	command := newCommand(buildinfo.Current(), term)
	if filepath.Base(executable) == "kubectl-kubebao" {
		command.Annotations = map[string]string{cobra.CommandDisplayNameAnnotation: "kubectl kubebao"}
	}
	command.SetArgs(args)
	command.SetOut(stdout)
	command.SetErr(stderr)
	if err := command.ExecuteContext(ctx); err != nil {
		term.writeError(stderr, err)
		var result interface{ ExitCode() int }
		if errors.As(err, &result) {
			return result.ExitCode()
		}
		return 1
	}
	return 0
}

const (
	groupInspect = "inspect"
	groupControl = "control"
)

func newCommand(info buildinfo.Info, term *terminal) *cobra.Command {
	root := &cobra.Command{
		Use:   "kubebao",
		Short: "Inspect and operate OpenBao clusters managed by the OpenBao Operator",
		Long: "kubebao inspects and operates OpenBao clusters managed by the OpenBao Operator.\n\n" +
			"It reads what the operator reports through the Kubernetes API, diagnoses common problems,\n" +
			"packages evidence for support, and submits guarded pause, maintenance, and restart requests.\n" +
			"It never reads Secrets or contacts OpenBao directly.",
		Example: "  kubebao status prod -n security\n" +
			"  kubebao doctor prod -n security\n" +
			"  kubebao maintenance enable prod -n security --wait",
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
	term.addFlags(root)
	root.AddGroup(
		&cobra.Group{ID: groupInspect, Title: "Inspect and diagnose:"},
		&cobra.Group{ID: groupControl, Title: "Control a cluster:"},
	)
	inspect := []*cobra.Command{newStatusCommand(info, term), newDoctorCommand(info, term), newBundleCommand(info, term)}
	control := []*cobra.Command{
		newPauseCommand(info, term, true), newPauseCommand(info, term, false), newMaintenanceCommand(info, term),
		newRestartCommand(info, term),
	}
	for _, command := range inspect {
		command.GroupID = groupInspect
	}
	for _, command := range control {
		command.GroupID = groupControl
	}
	root.AddCommand(inspect...)
	root.AddCommand(control...)
	root.AddCommand(newVersionCommand(info))
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
