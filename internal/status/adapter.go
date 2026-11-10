package status

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// clusterV1Alpha1 is an allowlist for the public v1alpha1 resource.
// It deliberately omits arbitrary annotations and configuration payloads.
type clusterV1Alpha1 struct {
	Metadata struct {
		Generation *int64 `json:"generation"`
	} `json:"metadata"`
	Spec struct {
		Version      string `json:"version"`
		Replicas     *int64 `json:"replicas"`
		ReadReplicas *struct {
			Replicas *int64 `json:"replicas"`
		} `json:"readReplicas"`
		Paused      *bool `json:"paused"`
		Maintenance *struct {
			Enabled *bool `json:"enabled"`
		} `json:"maintenance"`
	} `json:"spec"`
	Status struct {
		ObservedGeneration *int64 `json:"observedGeneration"`
		Phase              string `json:"phase"`
		CurrentVersion     string `json:"currentVersion"`
		ReadyReplicas      *int64 `json:"readyReplicas"`
		ReadReplicas       *struct {
			ReadyReplicas *int64 `json:"readyReplicas"`
		} `json:"readReplicas"`
		Conditions    []Condition    `json:"conditions"`
		OperationLock *OperationLock `json:"operationLock"`
		BreakGlass    *BreakGlass    `json:"breakGlass"`
		Backup        *Backup        `json:"backup"`
		Upgrade       *Upgrade       `json:"upgrade"`
		BlueGreen     *BlueGreen     `json:"blueGreen"`
		Restore       *LegacyRestore `json:"restore"`
		Workload      *struct {
			LastError *ControllerError `json:"lastError"`
		} `json:"workload"`
		AdminOps *struct {
			LastError *ControllerError `json:"lastError"`
		} `json:"adminOps"`
	} `json:"status"`
}

func ProjectV1Alpha1(object *unstructured.Unstructured, collectedAt time.Time) (*Observation, error) {
	var source clusterV1Alpha1
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &source); err != nil {
		return nil, errors.New("resource fields do not match the v1alpha1 status adapter")
	}
	o := &Observation{
		CollectedAt: collectedAt, ResourceVersion: object.GetResourceVersion(),
		Adapter: "openbaocluster-v1alpha1/v1", Qualification: "unqualified",
		Generation: source.Metadata.Generation, ObservedGeneration: source.Status.ObservedGeneration,
		GenerationCurrent: generationCurrent(source.Metadata.Generation, source.Status.ObservedGeneration),
		Phase:             source.Status.Phase, DesiredVersion: source.Spec.Version, ObservedVersion: source.Status.CurrentVersion,
		Voters: Replicas{Desired: source.Spec.Replicas, Ready: source.Status.ReadyReplicas},
		Paused: source.Spec.Paused, MaintenancePropagation: "not_collected",
		OperationLock: source.Status.OperationLock, BreakGlass: source.Status.BreakGlass,
		Backup: source.Status.Backup, Upgrade: source.Status.Upgrade, BlueGreen: source.Status.BlueGreen,
		LegacyRestore: source.Status.Restore, Conditions: source.Status.Conditions,
		Attention: []Attention{},
		Limitations: []string{
			"No operator release is qualified; known v1alpha1 fields are projected with interpretation limits.",
			"Collection time is not the time of the operator's observation; no reconciliation heartbeat is available.",
			"Workloads and restore requests are not collected; maintenance propagation and current restore progress are unverified.",
			"OpenBao service-registration conditions report labels, not independent health probes.",
		},
	}
	if source.Spec.ReadReplicas != nil {
		o.ReadReplicas.Desired = source.Spec.ReadReplicas.Replicas
	}
	if source.Status.ReadReplicas != nil {
		o.ReadReplicas.Ready = source.Status.ReadReplicas.ReadyReplicas
	}
	if source.Spec.Maintenance != nil {
		o.MaintenanceEnabled = source.Spec.Maintenance.Enabled
	}
	// API-group migration preserves these documented annotation keys.
	if value, found, err := unstructured.NestedString(object.Object, "metadata", "annotations", "openbao.org/restore-hold"); err != nil {
		return nil, errors.New("restore hold does not match the v1alpha1 status adapter")
	} else if found {
		o.RestoreHold = value
	}
	if source.Status.Workload != nil {
		o.WorkloadError = source.Status.Workload.LastError
	}
	if source.Status.AdminOps != nil {
		o.AdminOpsError = source.Status.AdminOps.LastError
	}
	if o.Conditions == nil {
		o.Conditions = []Condition{}
	}
	seen := map[string]bool{}
	for i := range o.Conditions {
		condition := &o.Conditions[i]
		if condition.Type == "" || seen[condition.Type] {
			return nil, errors.New("condition types are empty or duplicated")
		}
		seen[condition.Type] = true
		if condition.Status != "True" && condition.Status != "False" && condition.Status != "Unknown" {
			return nil, errors.New("condition status does not match the v1alpha1 status adapter")
		}
		condition.GenerationCurrent = generationCurrent(o.Generation, condition.ObservedGeneration)
		condition.Field = "status.conditions[type=" + clean(condition.Type) + "]"
		cleanStrings(&condition.Type, &condition.Status, &condition.Reason, &condition.Message, &condition.LastTransitionTime)
	}
	sort.Slice(o.Conditions, func(i, j int) bool { return o.Conditions[i].Type < o.Conditions[j].Type })
	for i := range o.Conditions {
		if o.Conditions[i].Type == "Available" {
			o.Available = &o.Conditions[i]
		}
	}
	sanitizeObservation(o)
	o.Attention = attention(o)
	return o, nil
}

func generationCurrent(generation, observed *int64) *bool {
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

func clean(value string) string {
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

func cleanStrings(values ...*string) {
	for _, value := range values {
		*value = clean(*value)
	}
}

func sanitizeError(value *ControllerError) {
	if value != nil {
		cleanStrings(&value.Reason, &value.Message, &value.At)
	}
}

func sanitizeObservation(o *Observation) {
	cleanStrings(&o.ResourceVersion, &o.Phase, &o.DesiredVersion, &o.ObservedVersion, &o.RestoreHold)
	if v := o.OperationLock; v != nil {
		cleanStrings(&v.Operation, &v.Holder, &v.Message, &v.AcquiredAt, &v.RenewedAt)
	}
	if v := o.BreakGlass; v != nil {
		cleanStrings(&v.Reason, &v.Message, &v.EnteredAt)
	}
	if v := o.Backup; v != nil {
		cleanStrings(&v.LastBackupTime, &v.LastAttemptTime, &v.NextScheduledBackup,
			&v.LastFailureReason, &v.LastFailureMessage, &v.LastFailureTime)
	}
	if v := o.Upgrade; v != nil {
		cleanStrings(&v.ExecutionID, &v.TargetVersion, &v.FromVersion, &v.StartedAt)
		sanitizeError(v.Failure)
	}
	if v := o.BlueGreen; v != nil {
		cleanStrings(&v.Phase, &v.OperationID, &v.GreenVersion, &v.StartTime, &v.LastJobFailure, &v.RollbackReason)
	}
	if v := o.LegacyRestore; v != nil {
		cleanStrings(&v.Name, &v.UID, &v.RestartCompletedAt)
	}
	sanitizeError(o.WorkloadError)
	sanitizeError(o.AdminOpsError)
}

func attention(o *Observation) []Attention {
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
