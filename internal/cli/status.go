package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
	"github.com/kubebao/openbao-operator-cli/internal/collect"
)

type commandResult struct {
	code    int
	message string
}

func (r commandResult) Error() string { return r.message }
func (r commandResult) ExitCode() int { return r.code }

func newStatusCommand(info buildinfo.Info) *cobra.Command {
	options := collect.Options{RequestTimeout: 30 * time.Second, UserAgent: "kubebao/" + info.Version}
	output := "text"
	verbose, watch := false, false
	interval := 2 * time.Second
	command := &cobra.Command{
		Use:   "status NAME",
		Short: "Report operator observations for one OpenBaoCluster",
		Long: "Read the named OpenBaoCluster and report its public configuration and status.\n" +
			"Watch mode refreshes the same resource; it stops if the resource UID changes.\n" +
			"Workloads, restore requests, and OpenBao endpoints are not queried.",
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if output != "text" && output != "json" {
				return fmt.Errorf("unsupported output format %q: use text or json", output)
			}
			if interval < 100*time.Millisecond {
				return errors.New("watch interval must be at least 100ms")
			}
			options.Name = args[0]
			client, err := collect.NewClient(options)
			if err != nil {
				return err
			}
			for {
				snapshot := client.Collect(command.Context())
				report := makeStatusReport(snapshot)
				if watch && command.Context().Err() != nil {
					return nil
				}
				if err := writeStatus(command.OutOrStdout(), report, output, verbose); err != nil {
					return err
				}
				if report.ExitCode != 0 {
					return commandResult{code: report.ExitCode, message: "status result: " + report.ResultCode}
				}
				if !watch {
					return nil
				}
				timer := time.NewTimer(interval)
				select {
				case <-command.Context().Done():
					timer.Stop()
					return nil
				case <-timer.C:
				}
			}
		},
	}
	flags := command.Flags()
	addKubernetesFlags(command, &options)
	flags.StringVarP(&output, "output", "o", output, "Output format: text or json (newline-delimited in watch mode)")
	flags.BoolVarP(&verbose, "verbose", "v", verbose, "Include evidence fields, structured reasons, and interpretation limits")
	flags.BoolVarP(&watch, "watch", "w", watch, "Refresh the selected resource until interrupted or collection fails")
	flags.DurationVar(&interval, "watch-interval", interval, "Delay between watch collections; minimum 100ms")
	return command
}
