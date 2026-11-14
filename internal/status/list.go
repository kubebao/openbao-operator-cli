package status

import (
	"fmt"
	"io"
	"strings"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	"github.com/kubebao/openbao-operator-cli/internal/render"
)

// ListRow is one listed cluster. Rejected marks normalized evidence that failed validation.
type ListRow struct {
	Namespace   string
	Name        string
	Observation *evidence.Observation
	Rejected    bool
}

// ListView is the input of the cluster list text view.
type ListView struct {
	Target        evidence.Target
	AllNamespaces bool
	ResultCode    string
	Rows          []ListRow
	Collection    []evidence.Step
	Width         int
}

// WriteList writes a human-readable table of clusters. Styling reaches the destination only through a render.Output.
func WriteList(writer io.Writer, view ListView) error {
	var b strings.Builder
	scope := "namespace " + view.Target.Namespace
	if view.AllNamespaces {
		scope = "all namespaces"
	}
	context := []string{scope}
	if view.Target.Context != "" {
		context = append(context, "context "+view.Target.Context)
	}
	b.WriteString(render.Bold("OpenBaoClusters") + "  " + render.Faint(strings.Join(context, " · ")) + "\n")
	switch view.ResultCode {
	case "completed", "incomplete":
		writeRows(&b, view)
	case "not_found":
		b.WriteString("\n" + render.Status(render.Bad, "The OpenBaoCluster API is not served by this Kubernetes cluster") + "\n")
		b.WriteString("  Neither kubebao.org nor openbao.org serves OpenBaoCluster. Check the context and the operator installation.\n")
	case "ambiguous_api_group":
		b.WriteString("\n" + render.Status(render.Bad, "Both kubebao.org and openbao.org serve OpenBaoCluster") + "\n")
		b.WriteString("  Choose one with --api-group.\n")
	default:
		b.WriteString("\n" + render.Status(render.Bad, "Clusters could not be listed") + "\n")
		b.WriteString("  The list " + render.Humanize(view.ResultCode) + ".\n")
	}
	if table := render.CollectionTable(view.Collection, false, "  "); table != "" {
		b.WriteString("\n" + render.Heading("Collection problems") + "\n" + table)
	}
	if view.ResultCode == "incomplete" {
		b.WriteString(render.Status(render.Unknown, "The list is incomplete; clusters that could not be read are not shown.") + "\n")
	}
	if _, err := io.WriteString(writer, b.String()); err != nil {
		return fmt.Errorf("write cluster list: %w", err)
	}
	return nil
}

func writeRows(b *strings.Builder, view ListView) {
	if len(view.Rows) == 0 {
		b.WriteString("\n  No OpenBaoClusters found.\n")
		if !view.AllNamespaces {
			b.WriteString(render.NextCommand("Search every namespace:", "kubebao list -A", "  "))
		}
		return
	}
	headers := []string{"NAME", "VERSION", "VOTERS", "AVAILABLE", "RECONCILIATION", "MAINTENANCE", "ATTENTION"}
	if view.AllNamespaces {
		headers = append([]string{"NAMESPACE"}, headers...)
	}
	rows := make([][]string, 0, len(view.Rows))
	for _, r := range view.Rows {
		row := listRow(r)
		if view.AllNamespaces {
			row = append([]string{r.Namespace}, row...)
		}
		rows = append(rows, row)
	}
	b.WriteString("\n" + render.Table(headers, rows, "  "))
	first := view.Rows[0]
	example := followUp("kubebao status", evidence.Target{Name: first.Name, Namespace: first.Namespace, Context: view.Target.Context})
	b.WriteString("\n" + render.NextCommand("Show one cluster in detail:", example, "  "))
}

func listRow(r ListRow) []string {
	o := r.Observation
	if o == nil {
		return []string{r.Name, "", "", "", "", "", render.Paint(render.Unknown, "? not observed")}
	}
	available := render.Paint(render.Unknown, "unknown")
	if o.Available != nil {
		available = conditionStatus(o.Available.Status)
	}
	maintenanceValue := render.Faint("unknown")
	if o.MaintenanceEnabled != nil {
		maintenanceValue = "disabled"
		if *o.MaintenanceEnabled {
			maintenanceValue = render.Paint(render.Warn, "enabled")
		}
	}
	return []string{r.Name, shortVersion(o), shortReplicas(o.Voters), available, reconciliation(o.Paused), maintenanceValue, attention(r)}
}

func shortVersion(o *evidence.Observation) string {
	switch {
	case o.ObservedVersion == "" && o.DesiredVersion == "":
		return render.Faint("unknown")
	case o.ObservedVersion == "":
		return o.DesiredVersion + render.Faint(" (desired)")
	case o.DesiredVersion == "" || o.DesiredVersion == o.ObservedVersion:
		return o.ObservedVersion
	default:
		return o.ObservedVersion + " → " + render.Paint(render.Info, o.DesiredVersion)
	}
}

func shortReplicas(r evidence.Replicas) string {
	if r.Ready == nil || r.Desired == nil {
		return render.Faint("unknown")
	}
	tone := render.Good
	if *r.Ready != *r.Desired {
		tone = render.Warn
	}
	return render.Paint(tone, fmt.Sprintf("%d/%d", *r.Ready, *r.Desired))
}

func attention(r ListRow) string {
	if r.Rejected {
		return render.Paint(render.Bad, "✗ cannot interpret")
	}
	issues, gaps := 0, 0
	for _, item := range r.Observation.Attention {
		if strings.HasSuffix(item.Code, "_incomplete") {
			gaps++
		} else {
			issues++
		}
	}
	switch {
	case issues == 1:
		return render.Paint(render.Warn, "! 1 issue")
	case issues > 1:
		return render.Paint(render.Warn, fmt.Sprintf("! %d issues", issues))
	case gaps > 0:
		return render.Paint(render.Unknown, "? evidence incomplete")
	default:
		return render.Paint(render.Good, "✓ none reported")
	}
}
