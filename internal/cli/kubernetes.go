package cli

import (
	"github.com/spf13/cobra"

	"github.com/kubebao/openbao-operator-cli/internal/collect"
)

func addKubernetesFlags(command *cobra.Command, options *collect.Options) {
	flags := command.Flags()
	flags.StringVar(&options.Kubeconfig, "kubeconfig", "", "Path to kubeconfig; defaults to standard kubeconfig loading")
	flags.StringVar(&options.Context, "context", "", "Kubernetes context; defaults to the current context")
	flags.StringVarP(&options.Namespace, "namespace", "n", "", "Namespace; defaults to the selected context namespace")
	flags.StringVar(&options.APIGroup, "api-group", "", "Select kubebao.org or openbao.org; required if both APIs are served")
	flags.DurationVar(&options.RequestTimeout, "request-timeout", options.RequestTimeout, "Timeout for Kubernetes evidence collection")
	_ = command.RegisterFlagCompletionFunc("context", completeContexts(options))
	_ = command.RegisterFlagCompletionFunc("api-group", cobra.FixedCompletions([]cobra.Completion{"kubebao.org", "openbao.org"},
		cobra.ShellCompDirectiveNoFileComp))
}

// addClusterArgument completes the NAME argument with clusters in the selected namespace.
func addClusterArgument(command *cobra.Command, options *collect.Options) {
	command.ValidArgsFunction = completeClusterNames(options)
}
