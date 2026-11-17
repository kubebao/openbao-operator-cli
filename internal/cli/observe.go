package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
	"github.com/kubebao/openbao-operator-cli/internal/collect"
	"github.com/kubebao/openbao-operator-cli/internal/control"
)

func newObserveCommand(info buildinfo.Info, term *terminal) *cobra.Command {
	access := collect.Options{RequestTimeout: 30 * time.Second, UserAgent: "kubebao/" + info.Version}
	options := control.Options{WaitTimeout: 2 * time.Minute, PollInterval: 2 * time.Second}
	output := "text"
	command := &cobra.Command{
		Use: "observe --request-file FILE", Short: "Verify a saved control request with reads only",
		Long: "Read later evidence for a request saved before submission with --request-file.\n" +
			"The original cluster origin, UID, field value, and restart marker remain pinned.\n" +
			"This command never submits a change or resumes reconciliation. A saved request records\n" +
			"intent; it does not prove API acceptance or attribute the current state to that request.",
		Example: "  kubebao observe --request-file restart.json --kubeconfig ./config --context prod --wait",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if output != "text" && output != "json" {
				return fmt.Errorf("unsupported output format %q: use text or json", output)
			}
			if err := validateWait(options); err != nil {
				return err
			}
			path, err := filepath.Abs(options.RequestFile)
			if err != nil || options.RequestFile == "" {
				return errors.New("--request-file must name a saved request")
			}
			request, err := control.ReadRequest(command.Context(), path)
			if err != nil {
				return err
			}
			id := request.Target().Identity
			if command.Flags().Changed("namespace") && access.Namespace != id.Namespace ||
				command.Flags().Changed("api-group") && access.APIGroup != id.Group {
				return errors.New("namespace and API group must match the saved request")
			}
			access.Name, access.Namespace, access.APIGroup = id.Name, id.Namespace, id.Group
			options.RequestFile, options.RequestTimeout = path, access.RequestTimeout
			client, err := collect.NewClient(access)
			if err != nil {
				return err
			}
			progress := waitProgress(term.stderr(command), &options)
			defer progress.Clear()
			return writeObservation(command, term, client, request, options, output)
		},
	}
	addKubernetesFlags(command, &access)
	flags := command.Flags()
	flags.Lookup("namespace").Usage = "Namespace from the saved request; supplied values must match"
	flags.Lookup("api-group").Usage = "API group from the saved request; supplied values must match"
	flags.StringVar(&options.RequestFile, "request-file", "", "Original request file created before submission")
	flags.StringVarP(&output, "output", "o", output, "Output format: text or json")
	flags.BoolVar(&options.Wait, "wait", false, "Poll with reads until the saved request is observed or blocked")
	flags.DurationVar(&options.WaitTimeout, "wait-timeout", options.WaitTimeout, "Maximum duration of observation")
	flags.DurationVar(&options.PollInterval, "poll-interval", options.PollInterval, "Delay between checks; minimum 100ms")
	completeOutput(command)
	return command
}

func writeObservation(command *cobra.Command, term *terminal, client *collect.Client,
	request control.Request, options control.Options, output string,
) error {
	out := term.stdout(command)
	text := control.TextOptions{Width: out.Width()}
	code, result := 0, ""
	var err error
	if request.Restart != nil {
		report := control.ObserveRestart(command.Context(), client, *request.Restart, options)
		code, result = report.ExitCode, report.ResultCode
		if output == "json" {
			err = writeJSON(command.OutOrStdout(), report)
		} else {
			err = control.WriteRestartText(out, report, text)
		}
	} else {
		options.Control, options.Enabled = request.Control.Control, request.Control.Enabled
		report := control.Observe(command.Context(), client, *request.Control, options)
		code, result = report.ExitCode, report.ResultCode
		if output == "json" {
			err = writeJSON(command.OutOrStdout(), report)
		} else {
			err = control.WriteText(out, report, options, text)
		}
	}
	if err != nil {
		return err
	}
	if code != 0 {
		return commandResult{code: code, message: "observation result: " + result}
	}
	return nil
}
