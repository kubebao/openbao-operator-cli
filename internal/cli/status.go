package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
	"github.com/kubebao/openbao-operator-cli/internal/collect"
	"github.com/kubebao/openbao-operator-cli/internal/render"
)

type commandResult struct {
	code    int
	message string
}

func (r commandResult) Error() string { return r.message }
func (r commandResult) ExitCode() int { return r.code }

func newStatusCommand(info buildinfo.Info, term *terminal) *cobra.Command {
	options := collect.Options{RequestTimeout: 30 * time.Second, UserAgent: "kubebao/" + info.Version}
	output := "text"
	verbose, watch := false, false
	interval := 2 * time.Second
	command := &cobra.Command{
		Use:   "status NAME",
		Short: "Show what the operator reports for a cluster",
		Long: "Show what the operator reports for one OpenBaoCluster: versions, replicas, availability,\n" +
			"active controls, backups, and upgrades.\n\n" +
			"Status reads only the OpenBaoCluster resource. It does not read Pods or contact OpenBao;\n" +
			"use doctor to also check StatefulSets, Pods, and restores. Watch mode refreshes until\n" +
			"interrupted and stops if the resource is deleted or replaced.",
		Example: "  kubebao status prod -n security\n" +
			"  kubebao status prod -n security --verbose\n" +
			"  kubebao status prod -n security --watch\n" +
			"  kubebao status prod -n security -o json | jq .observation.voters",
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
			loop := statusLoop{client: client, out: term.stdout(command), format: output, verbose: verbose, interval: interval}
			if watch {
				return loop.watch(command.Context())
			}
			return loop.once(command.Context())
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

type statusLoop struct {
	client   *collect.Client
	out      *render.Output
	format   string
	verbose  bool
	interval time.Duration
}

func (l statusLoop) once(ctx context.Context) error {
	return l.write(makeStatusReport(l.client.Collect(ctx)))
}

// watch polls the selected UID. On a terminal, text output redraws in place; elsewhere reports are appended.
func (l statusLoop) watch(ctx context.Context) error {
	redraw := l.format == "text" && l.out.Terminal()
	for {
		report := makeStatusReport(l.client.Collect(ctx))
		if ctx.Err() != nil {
			return nil
		}
		if redraw {
			if err := writeWatchHeader(l.out, l.interval); err != nil {
				return err
			}
		}
		if err := l.write(report); err != nil {
			return err
		}
		timer := time.NewTimer(l.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (l statusLoop) write(report statusReport) error {
	if err := writeStatus(l.out, report, l.format, l.verbose); err != nil {
		return err
	}
	if report.ExitCode != 0 {
		return commandResult{code: report.ExitCode, message: "status result: " + report.ResultCode}
	}
	return nil
}
