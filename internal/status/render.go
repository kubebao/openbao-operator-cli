package status

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

func Write(writer io.Writer, report Report, output string, verbose bool) error {
	if output == "json" {
		if err := json.NewEncoder(writer).Encode(report); err != nil {
			return fmt.Errorf("write status JSON: %w", err)
		}
		return nil
	}
	var buffer strings.Builder
	fmt.Fprintf(&buffer, "Status: %s / %s/%s\n", report.Target.Context, report.Target.Namespace, report.Target.Name)
	if o := report.Observation; o != nil {
		if len(o.Attention) > 0 {
			buffer.WriteString("\nAttention (reported evidence):\n")
			for _, item := range o.Attention {
				fmt.Fprintf(&buffer, "  - %s\n", item.Summary)
				if verbose {
					fmt.Fprintf(&buffer, "    %s\n", item.Field)
				}
			}
		}
		writeObservation(&buffer, report, verbose)
	} else {
		fmt.Fprintf(&buffer, "\nResult: %s; no current resource observation is available.\n", report.ResultCode)
	}
	for _, step := range report.Collection {
		if verbose || (step.Outcome != "collected" && step.Outcome != "not_applicable" && step.Outcome != "not_found") {
			fmt.Fprintf(&buffer, "Collection: %s %s: %s\n", step.APIGroup, step.Action, step.Outcome)
		}
	}
	if report.ResultCode == "ambiguous_api_group" {
		buffer.WriteString("Select the target with --api-group openbao.org or --api-group kubebao.org.\n")
	}
	if report.ExitCode == 3 {
		buffer.WriteString("Collection is incomplete; unavailable evidence remains unknown.\n")
	}
	if _, err := io.WriteString(writer, buffer.String()); err != nil {
		return fmt.Errorf("write status: %w", err)
	}
	return nil
}

func writeObservation(buffer *strings.Builder, report Report, verbose bool) {
	o := report.Observation
	buffer.WriteString("\n")
	table := tabwriter.NewWriter(buffer, 0, 4, 2, ' ', 0)
	row := func(name, value string) { _, _ = fmt.Fprintf(table, "%s\t%s\n", name+":", value) }
	row("Kubernetes cluster", report.Target.KubernetesCluster)
	row("API", report.Target.APIVersion+" / OpenBaoCluster")
	row("UID", report.Target.UID)
	row("Collected at", o.CollectedAt.Format(time.RFC3339))
	row("Phase (reported)", known(o.Phase))
	row("OpenBao version", known(o.ObservedVersion)+" observed / "+known(o.DesiredVersion)+" desired")
	row("Voters (reported)", number(o.Voters.Ready)+" ready / "+number(o.Voters.Desired)+" desired")
	row("Read replicas (reported)", number(o.ReadReplicas.Ready)+" ready / "+number(o.ReadReplicas.Desired)+" desired")
	availability := "unknown (not reported)"
	if o.Available != nil {
		availability = conditionSummary(*o.Available)
	}
	row("Available (reported)", availability)
	row("Generation", number(o.ObservedGeneration)+" observed / "+number(o.Generation)+" current; matches: "+boolean(o.GenerationCurrent))
	row("Pause requested", boolean(o.Paused))
	row("Maintenance requested", boolean(o.MaintenanceEnabled)+"; propagation not collected")
	row("Restore hold", known(o.RestoreHold))
	if lock := o.OperationLock; lock != nil {
		row("Operation lock", known(lock.Operation)+"; holder "+known(lock.Holder)+"; renewed "+known(lock.RenewedAt))
	} else {
		row("Operation lock", "not reported")
	}
	if glass := o.BreakGlass; glass != nil {
		row("Break glass", boolean(glass.Active)+"; "+known(glass.Reason))
	}
	if backup := o.Backup; backup != nil {
		row("Last successful backup", known(backup.LastBackupTime))
		row("Backup failures", number(backup.ConsecutiveFailures)+" consecutive; last reason "+known(backup.LastFailureReason))
		row("Last backup attempt", known(backup.LastAttemptTime))
		row("Next scheduled backup", known(backup.NextScheduledBackup))
	} else {
		row("Backup", "not reported")
	}
	if upgrade := o.Upgrade; upgrade != nil {
		row(
			"Rolling upgrade",
			known(upgrade.FromVersion)+" -> "+known(upgrade.TargetVersion)+"; partition "+number(upgrade.CurrentPartition),
		)
		row("Upgrade started", known(upgrade.StartedAt))
	}
	if blueGreen := o.BlueGreen; blueGreen != nil {
		row("Blue/green upgrade", known(blueGreen.Phase)+"; target "+known(blueGreen.GreenVersion))
	}
	if restore := o.LegacyRestore; restore != nil {
		row("Legacy restore restart", known(restore.Name)+"; completed "+known(restore.RestartCompletedAt))
	}
	row("Compatibility", o.Qualification+"; known v1alpha1 fields only")
	_ = table.Flush()
	buffer.WriteString("\nCollection time is not an operator heartbeat. Service-registration labels are not independent health probes.\n")
	if verbose {
		buffer.WriteString("\nConditions:\n")
		for _, condition := range o.Conditions {
			fmt.Fprintf(buffer, "  %s: %s\n    %s\n    %s; last transition %s\n",
				condition.Type, conditionSummary(condition), known(condition.Message), condition.Field, known(condition.LastTransitionTime))
		}
		writeMessages(buffer, o)
		buffer.WriteString("\nInterpretation limits:\n")
		for _, limitation := range o.Limitations {
			fmt.Fprintf(buffer, "  - %s\n", limitation)
		}
	}
}

func writeMessages(buffer *strings.Builder, o *Observation) {
	for _, entry := range []struct {
		name  string
		value *ControllerError
	}{
		{"Workload error", o.WorkloadError}, {"Admin operation error", o.AdminOpsError},
	} {
		if entry.value != nil {
			fmt.Fprintf(buffer, "%s: %s: %s\n", entry.name, known(entry.value.Reason), known(entry.value.Message))
		}
	}
	if o.Backup != nil && o.Backup.LastFailureMessage != "" {
		fmt.Fprintf(buffer, "Last backup failure: %s\n", o.Backup.LastFailureMessage)
	}
	if o.Upgrade != nil && o.Upgrade.Failure != nil {
		fmt.Fprintf(buffer, "Upgrade failure: %s: %s\n", o.Upgrade.Failure.Reason, o.Upgrade.Failure.Message)
	}
	if o.BreakGlass != nil && o.BreakGlass.Message != "" {
		fmt.Fprintf(buffer, "Break glass: %s\n", o.BreakGlass.Message)
	}
}

func conditionSummary(condition Condition) string {
	return condition.Status + " (" + condition.Reason + "); generation " + number(condition.ObservedGeneration) +
		"; matches current generation: " + boolean(condition.GenerationCurrent)
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

func boolean(value *bool) string {
	if value == nil {
		return "unknown"
	}
	return strconv.FormatBool(*value)
}
