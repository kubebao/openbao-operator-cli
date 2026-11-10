package evidence

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

func GenerationCurrent(generation, observed *int64) *bool {
	if generation == nil || observed == nil || *generation < 1 || *observed < 1 {
		return nil
	}
	current := *generation == *observed
	return &current
}

var bearerText = regexp.MustCompile(`(?i)\bbearer\s+[^\s"',;]+`)

var sensitiveText = regexp.MustCompile(
	`(?i)((?:[a-z_-]*token|password|secret|credential|authorization|access[_-]?key)["']?\s*[=:]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)`,
)

func Clean(value string) string {
	value = bearerText.ReplaceAllString(value, "Bearer [REDACTED]")
	value = sensitiveText.ReplaceAllString(value, "${1}[REDACTED]")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	if len(value) > 4096 {
		value = strings.ToValidUTF8(value[:4096], "") + " [truncated]"
	}
	return value
}

func CleanStrings(values ...*string) {
	for _, value := range values {
		*value = Clean(*value)
	}
}

func sanitizeError(value *ControllerError) {
	if value != nil {
		CleanStrings(&value.Reason, &value.Message, &value.At)
	}
}

func SanitizeObservation(o *Observation) {
	CleanStrings(&o.ResourceVersion, &o.Phase, &o.DesiredVersion, &o.ObservedVersion, &o.RestoreHold, &o.RestoreOrigin)
	if v := o.OperationLock; v != nil {
		CleanStrings(&v.Operation, &v.Holder, &v.Message, &v.AcquiredAt, &v.RenewedAt)
	}
	if v := o.BreakGlass; v != nil {
		CleanStrings(&v.Reason, &v.Message, &v.EnteredAt)
	}
	if v := o.Backup; v != nil {
		CleanStrings(&v.LastBackupTime, &v.LastAttemptTime, &v.NextScheduledBackup,
			&v.LastFailureReason, &v.LastFailureMessage, &v.LastFailureTime)
	}
	if v := o.Upgrade; v != nil {
		CleanStrings(&v.ExecutionID, &v.TargetVersion, &v.FromVersion, &v.StartedAt)
		sanitizeError(v.Failure)
	}
	if v := o.BlueGreen; v != nil {
		CleanStrings(&v.Phase, &v.OperationID, &v.GreenVersion, &v.StartTime, &v.LastJobFailure, &v.RollbackReason)
	}
	if v := o.LegacyRestore; v != nil {
		CleanStrings(&v.Name, &v.UID, &v.RestartCompletedAt)
	}
	sanitizeError(o.WorkloadError)
	sanitizeError(o.AdminOpsError)
}

func ClusterAttention(o *Observation) []Attention {
	result := []Attention{}
	add := func(code, summary, field string) {
		result = append(result, Attention{Code: code, Summary: summary, Field: field})
	}
	if o.GenerationCurrent == nil {
		add("generation_unknown", "Operator generation acknowledgement is not reported", "status.observedGeneration")
	} else if !*o.GenerationCurrent {
		add("generation_mismatch", "Operator status does not match the resource generation", "status.observedGeneration")
	}
	if o.Available == nil {
		add("availability_unknown", "Availability is not reported", "status.conditions[type=Available]")
	}
	if o.Paused != nil && *o.Paused {
		add("paused", "Reconciliation is requested paused; existing work can continue", "spec.paused")
	}
	if o.MaintenanceEnabled != nil && *o.MaintenanceEnabled {
		add("maintenance", "Maintenance is requested; workload annotation propagation is not verified", "spec.maintenance.enabled")
	}
	if o.RestoreHold != "" {
		add("restore_hold", "A restore hold restricts ordinary management", "metadata.annotations[openbao.org/restore-hold]")
	}
	if o.BreakGlass != nil && o.BreakGlass.Active != nil && *o.BreakGlass.Active {
		add("break_glass", "Break-glass restrictions are active", "status.breakGlass.active")
	}
	if o.Backup != nil && o.Backup.ConsecutiveFailures != nil && *o.Backup.ConsecutiveFailures > 0 {
		add("backup_failures", "The operator reports consecutive backup failures", "status.backup.consecutiveFailures")
	}
	for _, condition := range o.Conditions {
		if condition.GenerationCurrent == nil || !*condition.GenerationCurrent {
			add("condition_generation_unconfirmed", condition.Type+": current-generation applicability is unconfirmed", condition.Field)
		}
		if conditionNeedsAttention(condition) {
			add("reported_condition", condition.Type+"="+conditionSummary(condition), condition.Field)
		}
	}
	for _, entry := range []struct {
		value *ControllerError
		field string
	}{
		{o.WorkloadError, "status.workload.lastError"}, {o.AdminOpsError, "status.adminOps.lastError"},
	} {
		if entry.value != nil && entry.value.Reason != "" {
			add("controller_error", "Operator reports "+entry.value.Reason, entry.field)
		}
	}
	if o.Upgrade != nil && o.Upgrade.Failure != nil && o.Upgrade.Failure.Reason != "" {
		add("upgrade_failure", "Operator reports "+o.Upgrade.Failure.Reason, "status.upgrade.failure")
	}
	return result
}

func conditionNeedsAttention(c Condition) bool {
	if c.Status == "Unknown" {
		return true
	}
	switch c.Type {
	case "Degraded", "SecurityRisk", "EtcdEncryptionWarning", "NodeSecurityCapabilityMismatch", "OpenBaoSealed":
		return c.Status == "True"
	case "Available", "TLSReady", "ACMEIntegrationReady", "ACMECacheReady", "GatewayIntegrationReady", "IngressIntegrationReady",
		"APIServerNetworkReady", "AuditFileStorageReady", "ProductionReady", "UserAccessBootstrap", "CloudUnsealIdentityReady",
		"BackupConfigurationReady", "StorageConfigured", "ReadReplicaStorageConfigured", "ReadReplicasReady", "ReadServingAvailable",
		"RaftMembershipReady", "ReadReplicasAutopilotHealthy", "OpenBaoInitialized", "OpenBaoLeader", "PolicyReconciliationReady":
		return c.Status == "False"
	default:
		return false
	}
}

func conditionSummary(condition Condition) string {
	return condition.Status + " (" + condition.Reason + "); generation " + evidenceNumber(condition.ObservedGeneration) +
		"; matches current generation: " + evidenceBoolean(condition.GenerationCurrent)
}

func evidenceNumber(v *int64) string {
	if v == nil {
		return "unknown"
	}
	return strconv.FormatInt(*v, 10)
}

func evidenceBoolean(v *bool) string {
	if v == nil {
		return "unknown"
	}
	return strconv.FormatBool(*v)
}
