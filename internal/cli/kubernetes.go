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
	flags.StringVar(&options.APIGroup, "api-group", "", "Select the approved openbao.org API group")
	flags.DurationVar(&options.RequestTimeout, "request-timeout", options.RequestTimeout, "Timeout for Kubernetes evidence collection")
}
