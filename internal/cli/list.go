package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
	"github.com/kubebao/openbao-operator-cli/internal/collect"
	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	"github.com/kubebao/openbao-operator-cli/internal/status"
)

// listReport is the command envelope for list. Each cluster carries its projected observation and status attention.
type listReport struct {
	SchemaVersion string          `json:"schemaVersion"`
	ResultCode    string          `json:"resultCode"`
	ExitCode      int             `json:"exitCode"`
	StartedAt     time.Time       `json:"startedAt"`
	CompletedAt   time.Time       `json:"completedAt"`
	Target        evidence.Target `json:"target"`
	AllNamespaces bool            `json:"allNamespaces"`
	Clusters      []listedCluster `json:"clusters"`
	Collection    []evidence.Step `json:"collection"`
}

type listedCluster struct {
	Namespace      string                `json:"namespace"`
	Name           string                `json:"name"`
	UID            string                `json:"uid"`
	ValidationPath string                `json:"validationPath,omitempty"`
	Observation    *evidence.Observation `json:"observation"`
}

func makeListReport(list evidence.ClusterList) listReport {
	report := listReport{
		SchemaVersion: "v1", ResultCode: list.ResultCode, ExitCode: collectionExitCode(list.ResultCode),
		StartedAt: list.StartedAt, CompletedAt: list.CompletedAt, Target: list.Target, AllNamespaces: list.AllNamespaces,
		Clusters: []listedCluster{}, Collection: list.Collection,
	}
	for _, snapshot := range list.Clusters {
		cluster := makeStatusReport(snapshot)
		report.Clusters = append(report.Clusters, listedCluster{
			Namespace: snapshot.Target.Namespace, Name: snapshot.Target.Name, UID: snapshot.Target.UID,
			ValidationPath: cluster.ValidationPath, Observation: cluster.Observation,
		})
	}
	return report
}

func newListCommand(info buildinfo.Info, term *terminal) *cobra.Command {
	options := collect.Options{RequestTimeout: 30 * time.Second, UserAgent: "kubebao/" + info.Version}
	output := "text"
	allNamespaces := false
	command := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List OpenBao clusters and what the operator reports for each",
		Long: "List the OpenBaoClusters in a namespace, or in every namespace with --all-namespaces, with\n" +
			"their version, voter readiness, availability, active controls, and status attention.\n\n" +
			"List reads only OpenBaoCluster resources. It needs list permission on openbaoclusters in the\n" +
			"selected namespace, or across the cluster with --all-namespaces.",
		Example: "  kubebao list -n security\n" +
			"  kubebao list -A\n" +
			"  kubebao list -A -o json | jq -r '.clusters[].name'",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if output != "text" && output != "json" {
				return fmt.Errorf("unsupported output format %q: use text or json", output)
			}
			client, err := collect.NewListClient(options)
			if err != nil {
				return err
			}
			report := makeListReport(client.List(command.Context(), allNamespaces))
			out := term.stdout(command)
			if output == "json" {
				err = writeJSON(out.Unstyled(), report)
			} else {
				err = status.WriteList(out, listView(report, out.Width()))
			}
			if err != nil {
				return err
			}
			if report.ExitCode != 0 {
				return commandResult{code: report.ExitCode, message: "list result: " + report.ResultCode}
			}
			return nil
		},
	}
	addKubernetesFlags(command, &options)
	command.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "List clusters in every namespace")
	command.Flags().StringVarP(&output, "output", "o", output, "Output format: text or json")
	completeOutput(command)
	return command
}

func listView(report listReport, width int) status.ListView {
	view := status.ListView{
		Target: report.Target, AllNamespaces: report.AllNamespaces, ResultCode: report.ResultCode,
		Collection: report.Collection, Width: width,
	}
	for _, c := range report.Clusters {
		view.Rows = append(view.Rows, status.ListRow{
			Namespace: c.Namespace, Name: c.Name, Observation: c.Observation, Rejected: c.ValidationPath != "",
		})
	}
	return view
}
