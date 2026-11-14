// Package status renders operator-reported cluster status for people.
package status

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/doctor"
	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	"github.com/kubebao/openbao-operator-cli/internal/render"
)

// TextOptions select optional sections of the text report.
type TextOptions struct {
	Verbose    bool
	Incomplete bool
	// ValidationPath names a field that could not be interpreted.
	ValidationPath string
	Width          int
}

// WriteText writes a human-readable status report. Styling reaches the destination only through a render.Output.
func WriteText(writer io.Writer, report evidence.Snapshot, options TextOptions) error {
	if options.Width <= 0 {
		options.Width = 100
	}
	var b strings.Builder
	b.WriteString(heading(report.Target) + "\n")
	if o := report.Observation; o != nil {
		if options.ValidationPath != "" {
			writeRejected(&b, options.ValidationPath)
		} else {
			writeVerdict(&b, report, options)
		}
		writeObservation(&b, report, options)
	} else {
		writeUnavailable(&b, report, options)
	}
	writeCollection(&b, report.Collection, options)
	if report.Observation != nil {
		writeFooter(&b, report, options.Verbose)
	}
	if _, err := io.WriteString(writer, b.String()); err != nil {
		return fmt.Errorf("write status: %w", err)
	}
	return nil
}

func heading(target evidence.Target) string {
	context := []string{"namespace " + target.Namespace}
	if target.Context != "" {
		context = append(context, "context "+target.Context)
	}
	return "OpenBaoCluster " + render.Bold(target.Name) + "  " + render.Faint(strings.Join(context, " · "))
}

func writeUnavailable(b *strings.Builder, report evidence.Snapshot, options TextOptions) {
	b.WriteString("\n" + render.Status(render.Bad, "Status unavailable") + "\n")
	b.WriteString("  The cluster " + render.TargetResult(report.ResultCode) + ".\n")
	if options.ValidationPath != "" {
		b.WriteString("  Field: " + options.ValidationPath + "\n")
	}
	if report.ResultCode == "not_found" {
		b.WriteString(render.NextCommand("Check the namespace and context, or list clusters:", "kubectl get openbaoclusters -A", "  "))
	}
}

// writeRejected replaces the verdict when normalized evidence failed validation: no rule ran.
func writeRejected(b *strings.Builder, path string) {
	b.WriteString("\n" + render.Status(render.Bad, "Reported fields cannot be interpreted") + "\n")
	b.WriteString("  Field: " + path + "\n")
	b.WriteString(render.Faint("  Rule checks were skipped. The values below are display projections only.") + "\n")
}

// writeVerdict summarizes selected rule results. It never claims health the operator did not report.
func writeVerdict(b *strings.Builder, report evidence.Snapshot, options TextOptions) {
	target, o := report.Target, report.Observation
	issues, notes, gaps := splitAttention(o.Attention)
	tone, label := render.Good, "No issues reported by the operator"
	switch {
	case len(issues) > 0:
		tone, label = render.Warn, "Needs attention"
	case len(gaps) > 0 || options.Incomplete:
		tone, label = render.Unknown, "No issues reported, but some checks lack evidence"
	}
	b.WriteString("\n" + render.Status(tone, label) + "\n")
	for _, item := range append(issues, notes...) {
		b.WriteString(render.Item(attentionTone(item), item.Summary, options.Width, "  ") + "\n")
		if options.Verbose && item.Field != "" {
			b.WriteString(render.Faint("    field: "+item.Field) + "\n")
		}
	}
	for _, item := range gaps {
		rule := strings.TrimSuffix(item.Code, "_incomplete")
		why := "not enough evidence to evaluate"
		if conditionGaps := doctor.ConditionGaps(report); rule == "KBAO-CONDITION-001" && len(conditionGaps) > 0 {
			why = strings.Join(conditionGaps, "; ")
		}
		b.WriteString(render.Item(render.Unknown, doctor.RuleTitle(rule)+": "+why+"  "+render.Faint(rule), options.Width, "  ") + "\n")
	}
	if len(issues) > 0 {
		b.WriteString(render.NextCommand("Check StatefulSets, Pods, and restores:", followUp("kubebao doctor", target), "  "))
	}
}

func writeObservation(b *strings.Builder, report evidence.Snapshot, options TextOptions) {
	o, verbose := report.Observation, options.Verbose
	reference := report.CompletedAt
	cluster := []render.Field{
		{Label: "Version", Value: version(o)},
		{Label: "Voters", Value: replicas(o.Voters)},
	}
	if verbose || o.ReadReplicas.Desired != nil {
		cluster = append(cluster, render.Field{Label: "Read replicas", Value: replicas(o.ReadReplicas)})
	}
	cluster = append(cluster, render.Field{Label: "Available", Value: availability(o.Available)})
	if verbose || o.Phase != "" {
		cluster = append(cluster, render.Field{Label: "Phase", Value: known(o.Phase)})
	}
	cluster = append(cluster, render.Field{Label: "Generation", Value: generation(o)})
	cluster = append(cluster, controllerErrors(o, verbose)...)
	controls := controlFields(o, reference, verbose)
	ops := operations(o, reference, verbose)
	width := render.LabelWidth(cluster, controls, ops)
	section(b, "Cluster", cluster, width)
	section(b, "Controls", controls, width)
	section(b, "Operations", ops, width)

	if verbose {
		writeConditions(b, o.Conditions)
	}
	if verbose && len(o.Limitations) > 0 {
		b.WriteString("\n" + render.Heading("Interpretation limits") + "\n")
		for _, limitation := range o.Limitations {
			b.WriteString(render.Item(render.Neutral, limitation, options.Width, "  ") + "\n")
		}
	}
}

func controlFields(o *evidence.Observation, reference time.Time, verbose bool) []render.Field {
	controls := []render.Field{
		{Label: "Reconciliation", Value: reconciliation(o.Paused)},
		{Label: "Maintenance", Value: maintenance(o.MaintenanceEnabled)},
	}
	if verbose || o.RestoreHold != "" {
		controls = append(controls, render.Field{Label: "Restore hold", Value: hold(o.RestoreHold)})
	}
	if lock := o.OperationLock; lock != nil {
		controls = append(controls, render.Field{Label: "Operation lock", Value: render.Paint(render.Warn, known(lock.Operation)) +
			" held by " + known(lock.Holder) + render.Faint(" · renewed "+render.Timestamp(lock.RenewedAt, reference))})
	} else if verbose {
		controls = append(controls, render.Field{Label: "Operation lock", Value: "not reported"})
	}
	if glass := o.BreakGlass; glass != nil {
		controls = append(controls, render.Field{Label: "Break glass", Value: breakGlass(glass)})
	} else if verbose {
		controls = append(controls, render.Field{Label: "Break glass", Value: "not reported"})
	}
	return controls
}

func operations(o *evidence.Observation, reference time.Time, verbose bool) []render.Field {
	fields := []render.Field{}
	if backup := o.Backup; backup != nil {
		fields = append(fields, render.Field{Label: "Last backup", Value: render.Timestamp(backup.LastBackupTime, reference)})
		if backup.ConsecutiveFailures != nil && *backup.ConsecutiveFailures > 0 {
			failure := render.Paint(render.Bad, number(backup.ConsecutiveFailures)+" consecutive failures")
			if backup.LastFailureReason != "" {
				failure += " · " + backup.LastFailureReason
			}
			fields = append(fields, render.Field{Label: "Backup failures", Value: failure})
		} else if verbose {
			fields = append(fields, render.Field{Label: "Backup failures", Value: number(backup.ConsecutiveFailures)})
		}
		if verbose || backup.NextScheduledBackup != "" {
			fields = append(fields, render.Field{Label: "Next backup", Value: known(backup.NextScheduledBackup)})
		}
		if verbose {
			fields = append(fields, render.Field{Label: "Last attempt", Value: render.Timestamp(backup.LastAttemptTime, reference)})
		}
	} else {
		fields = append(fields, render.Field{Label: "Backups", Value: "not reported"})
	}
	if upgrade := o.Upgrade; upgrade != nil {
		value := known(upgrade.FromVersion) + " → " + known(upgrade.TargetVersion) + " · partition " + number(upgrade.CurrentPartition)
		if upgrade.StartedAt != "" {
			value += render.Faint(" · started " + render.Timestamp(upgrade.StartedAt, reference))
		}
		if upgrade.Failure != nil {
			value += "\n" + render.Paint(render.Bad, "failed: "+known(upgrade.Failure.Reason))
		}
		fields = append(fields, render.Field{Label: "Rolling upgrade", Value: value})
	}
	if blueGreen := o.BlueGreen; blueGreen != nil {
		fields = append(
			fields,
			render.Field{Label: "Blue/green upgrade", Value: known(blueGreen.Phase) + " · target " + known(blueGreen.GreenVersion)},
		)
	}
	if restore := o.LegacyRestore; restore != nil {
		fields = append(fields, render.Field{Label: "Restore restart", Value: known(restore.Name) + " · completed " +
			render.Timestamp(restore.RestartCompletedAt, reference)})
	}
	return fields
}

func controllerErrors(o *evidence.Observation, verbose bool) []render.Field {
	fields := []render.Field{}
	for _, item := range []struct {
		label string
		value *evidence.ControllerError
	}{{"Workload errors", o.WorkloadError}, {"Admin op errors", o.AdminOpsError}} {
		switch {
		case item.value != nil:
			value := render.Paint(render.Bad, known(item.value.Reason))
			if item.value.At != "" {
				value += render.Faint(" · " + item.value.At)
			}
			fields = append(fields, render.Field{Label: item.label, Value: value})
		case verbose:
			fields = append(fields, render.Field{Label: item.label, Value: "none reported"})
		}
	}
	return fields
}

func writeConditions(b *strings.Builder, conditions []evidence.Condition) {
	rows := [][]string{}
	for _, c := range conditions {
		current := "unknown"
		if c.GenerationCurrent != nil {
			current = map[bool]string{true: "current", false: "stale"}[*c.GenerationCurrent]
		}
		rows = append(rows, []string{
			c.Type, conditionStatus(c.Status), known(c.Reason),
			number(c.ObservedGeneration) + " (" + current + ")", known(c.LastTransitionTime),
		})
	}
	if len(rows) > 0 {
		b.WriteString("\n" + render.Heading("Conditions") + "\n")
		b.WriteString(render.Table([]string{"TYPE", "STATUS", "REASON", "GENERATION", "LAST TRANSITION"}, rows, "  "))
	}
}

func writeCollection(b *strings.Builder, steps []evidence.Step, options TextOptions) {
	table := render.CollectionTable(steps, options.Verbose, "  ")
	if table != "" {
		title := "Collection problems"
		if options.Verbose {
			title = "Collection"
		}
		b.WriteString("\n" + render.Heading(title) + "\n" + table)
	}
	if options.Incomplete {
		b.WriteString(render.Status(render.Unknown, "Collection is incomplete; values that could not be read are shown as unknown.") + "\n")
	}
}

func writeFooter(b *strings.Builder, report evidence.Snapshot, verbose bool) {
	o := report.Observation
	b.WriteString("\n" + render.Faint(fmt.Sprintf("Collected %s via %s · %s", o.CollectedAt.Format(time.RFC3339),
		report.Target.APIVersion, compatibility(o.Qualification))) + "\n")
	if verbose {
		identity := []string{}
		if report.Target.KubernetesCluster != "" {
			identity = append(identity, "Kubernetes cluster "+report.Target.KubernetesCluster)
		}
		if report.Target.UID != "" {
			identity = append(identity, "UID "+report.Target.UID)
		}
		if len(identity) > 0 {
			b.WriteString(render.Faint(strings.Join(identity, " · ")) + "\n")
		}
	}
	b.WriteString(
		render.Faint("Values are what the operator reports; kubebao does not probe OpenBao. Use -v for interpretation limits.") + "\n",
	)
}

func section(b *strings.Builder, title string, fields []render.Field, labelWidth int) {
	if len(fields) == 0 {
		return
	}
	b.WriteString("\n" + render.Heading(title) + "\n" + render.AlignedFields(fields, "  ", labelWidth))
}

func version(o *evidence.Observation) string {
	switch {
	case o.ObservedVersion == "" && o.DesiredVersion == "":
		return render.Paint(render.Unknown, "not reported")
	case o.ObservedVersion == "":
		return o.DesiredVersion + render.Faint(" desired · running version not reported")
	case o.DesiredVersion == "" || o.ObservedVersion == o.DesiredVersion:
		return o.ObservedVersion
	default:
		return o.ObservedVersion + " running · " + render.Paint(render.Info, o.DesiredVersion+" desired")
	}
}

func replicas(r evidence.Replicas) string {
	switch {
	case r.Ready == nil && r.Desired == nil:
		return render.Paint(render.Unknown, "not reported")
	case r.Ready == nil:
		return render.Paint(render.Unknown, "unknown") + " of " + number(r.Desired) + " ready"
	case r.Desired == nil:
		return number(r.Ready) + " ready" + render.Faint(" · desired count not reported")
	case *r.Ready == *r.Desired:
		return render.Paint(render.Good, number(r.Ready)+" of "+number(r.Desired)+" ready")
	default:
		return render.Paint(render.Warn, number(r.Ready)+" of "+number(r.Desired)+" ready")
	}
}

func availability(c *evidence.Condition) string {
	if c == nil {
		return render.Paint(render.Unknown, "not reported")
	}
	value := conditionStatus(c.Status)
	if c.Reason != "" {
		value += " · " + c.Reason
	}
	switch {
	case c.GenerationCurrent == nil:
		value += render.Faint(" · generation acknowledgement unknown")
	case !*c.GenerationCurrent:
		value += render.Paint(render.Warn, " · stale (generation "+number(c.ObservedGeneration)+")")
	}
	return value
}

func conditionStatus(status string) string {
	switch status {
	case "True":
		return render.Paint(render.Good, "True")
	case "False":
		return render.Paint(render.Bad, "False")
	case "":
		return render.Paint(render.Unknown, "not reported")
	default:
		return render.Paint(render.Unknown, status)
	}
}

func generation(o *evidence.Observation) string {
	switch {
	case o.GenerationCurrent == nil:
		return number(o.Generation) + render.Faint(" · operator acknowledgement unknown")
	case *o.GenerationCurrent:
		return number(o.Generation) + render.Faint(" · observed by the operator")
	default:
		return render.Paint(render.Warn, number(o.Generation)+" · operator has observed "+number(o.ObservedGeneration))
	}
}

func reconciliation(paused *bool) string {
	switch {
	case paused == nil:
		return render.Paint(render.Unknown, "unknown")
	case *paused:
		return render.Paint(render.Warn, "paused")
	default:
		return "active"
	}
}

func maintenance(enabled *bool) string {
	switch {
	case enabled == nil:
		return render.Faint("not reported")
	case *enabled:
		return render.Paint(render.Warn, "enabled") + render.Faint(" · requested; propagation to workloads not checked")
	default:
		return "disabled"
	}
}

func hold(value string) string {
	if value == "" {
		return "not reported"
	}
	return render.Paint(render.Warn, value)
}

func breakGlass(glass *evidence.BreakGlass) string {
	switch {
	case glass.Active == nil:
		return render.Paint(render.Unknown, "state unknown")
	case *glass.Active:
		value := render.Paint(render.Bad, "active")
		if glass.Reason != "" {
			value += " · " + glass.Reason
		}
		return value
	default:
		return "inactive"
	}
}

func compatibility(qualification string) string {
	if qualification == "" || qualification == "unqualified" {
		return "operator release not qualified; known v1alpha1 fields only"
	}
	return qualification
}

func known(value string) string {
	if value == "" {
		return "not reported"
	}
	return value
}

func number(value *int64) string {
	if value == nil {
		return "unknown"
	}
	return strconv.FormatInt(*value, 10)
}

// followUp builds a command for the same cluster, namespace, and context.
func followUp(base string, target evidence.Target) string {
	parts := []string{base, target.Name}
	if target.Namespace != "" {
		parts = append(parts, "-n", target.Namespace)
	}
	if target.Context != "" {
		parts = append(parts, "--context", target.Context)
	}
	return strings.Join(parts, " ")
}

// splitAttention separates findings that need action from informational notes and from incomplete evaluations.
func splitAttention(items []evidence.Attention) ([]evidence.Attention, []evidence.Attention, []evidence.Attention) {
	issues, notes, gaps := []evidence.Attention{}, []evidence.Attention{}, []evidence.Attention{}
	for _, item := range items {
		switch {
		case strings.HasSuffix(item.Code, "_incomplete"):
			gaps = append(gaps, item)
		case item.Severity == "info":
			notes = append(notes, item)
		default:
			issues = append(issues, item)
		}
	}
	return issues, notes, gaps
}

func attentionTone(item evidence.Attention) render.Tone {
	switch item.Severity {
	case "info":
		return render.Info
	case "error":
		return render.Bad
	default:
		return render.Warn
	}
}
