package cli

import (
	"context"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kubebao/openbao-operator-cli/internal/collect"
)

// completionTimeout bounds the cluster list read behind shell completion so a slow API never blocks the shell.
const completionTimeout = 5 * time.Second

// completeClusterNames completes the NAME argument with clusters in the namespace that the
// --kubeconfig, --context, --namespace, and --api-group flags select. Failures complete nothing.
func completeClusterNames(options *collect.Options) cobra.CompletionFunc {
	return func(command *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		access := *options
		access.Name = ""
		access.RequestTimeout = min(access.RequestTimeout, completionTimeout)
		client, err := collect.NewListClient(access)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ctx := command.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		report := makeListReport(client.List(ctx, false))
		completions := []cobra.Completion{}
		for _, c := range report.Clusters {
			if !strings.HasPrefix(c.Name, toComplete) {
				continue
			}
			description := ""
			if o := c.Observation; o != nil {
				parts := []string{}
				if version := firstNonEmpty(o.ObservedVersion, o.DesiredVersion); version != "" {
					parts = append(parts, "OpenBao "+version)
				}
				if o.Paused != nil && *o.Paused {
					parts = append(parts, "paused")
				}
				if o.MaintenanceEnabled != nil && *o.MaintenanceEnabled {
					parts = append(parts, "maintenance")
				}
				description = strings.Join(parts, ", ")
			}
			completions = append(completions, cobra.CompletionWithDesc(c.Name, description))
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeContexts completes --context from the selected kubeconfig without contacting a cluster.
func completeContexts(options *collect.Options) cobra.CompletionFunc {
	return func(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
		names, err := collect.Contexts(options.Kubeconfig)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

func completeOutput(command *cobra.Command) {
	_ = command.RegisterFlagCompletionFunc("output", cobra.FixedCompletions([]cobra.Completion{"text", "json"},
		cobra.ShellCompDirectiveNoFileComp))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
