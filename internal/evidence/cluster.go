// Package evidence defines CLI display models and a normalized shared capture.
// Command envelopes own process results; shared analysis consumes only the capture.
package evidence

import (
	"time"

	"github.com/kubebao/openbao-operator-client/diagnose"
)

// Snapshot describes a bounded collection attempt, not an atomic cluster snapshot.
type Snapshot struct {
	NormalizedEvidence *diagnose.Capture `json:"normalizedEvidence,omitempty"`
	SchemaVersion      string            `json:"schemaVersion"`
	ResultCode         string            `json:"resultCode"`
	StartedAt          time.Time         `json:"startedAt"`
	CompletedAt        time.Time         `json:"completedAt"`
	Target             Target            `json:"target"`
	Observation        *Observation      `json:"observation"`
	Collection         []Step            `json:"collection"`
	Related            *Related          `json:"related,omitempty"`
}

type Target struct {
	Context           string `json:"context"`
	KubernetesCluster string `json:"kubernetesCluster"`
	Namespace         string `json:"namespace"`
	Name              string `json:"name"`
	APIGroup          string `json:"apiGroup,omitempty"`
	APIVersion        string `json:"apiVersion,omitempty"`
	UID               string `json:"uid,omitempty"`
}

type Step struct {
	Generation      *int64    `json:"generation,omitempty"`
	Resource        string    `json:"resource,omitempty"`
	Namespace       string    `json:"namespace,omitempty"`
	Name            string    `json:"name,omitempty"`
	APIGroup        string    `json:"apiGroup"`
	Action          string    `json:"action"`
	Outcome         string    `json:"outcome"`
	CollectedAt     time.Time `json:"collectedAt"`
	UID             string    `json:"uid,omitempty"`
	ResourceVersion string    `json:"resourceVersion,omitempty"`
}

// Observation contains only fields approved for status output.
// Pointers preserve the difference between missing values and reported zero/false.
type Observation struct {
	CollectedAt            time.Time        `json:"collectedAt"`
	ResourceVersion        string           `json:"resourceVersion"`
	Qualification          string           `json:"qualification"`
	Generation             *int64           `json:"generation"`
	ObservedGeneration     *int64           `json:"observedGeneration"`
	GenerationCurrent      *bool            `json:"generationCurrent"`
	Phase                  string           `json:"phase"`
	DesiredVersion         string           `json:"desiredVersion"`
	ObservedVersion        string           `json:"observedVersion"`
	Voters                 Replicas         `json:"voters"`
	ReadReplicas           Replicas         `json:"readReplicas"`
	Available              *Condition       `json:"available"`
	Conditions             []Condition      `json:"conditions"`
	Paused                 *bool            `json:"paused"`
	MaintenanceEnabled     *bool            `json:"maintenanceEnabled"`
	MaintenancePropagation string           `json:"maintenancePropagation"`
	RestoreOrigin          string           `json:"restoreOrigin,omitempty"`
	RestoreHold            string           `json:"restoreHold"`
	OperationLock          *OperationLock   `json:"operationLock"`
	BreakGlass             *BreakGlass      `json:"breakGlass"`
	Backup                 *Backup          `json:"backup"`
	Upgrade                *Upgrade         `json:"upgrade"`
	BlueGreen              *BlueGreen       `json:"blueGreen"`
	LegacyRestore          *LegacyRestore   `json:"legacyRestore"`
	WorkloadError          *ControllerError `json:"workloadError"`
	AdminOpsError          *ControllerError `json:"adminOpsError"`
	Attention              []Attention      `json:"attention"`
	Limitations            []string         `json:"limitations"`
}

type Replicas struct {
	Desired *int64 `json:"desired"`
	Ready   *int64 `json:"ready"`
}

type Condition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	Message            string `json:"message"`
	ObservedGeneration *int64 `json:"observedGeneration"`
	LastTransitionTime string `json:"lastTransitionTime"`
	GenerationCurrent  *bool  `json:"generationCurrent"`
	Field              string `json:"field"`
}

type ControllerError struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
	At      string `json:"at"`
}

type OperationLock struct {
	Operation  string `json:"operation"`
	Holder     string `json:"holder"`
	Message    string `json:"message"`
	AcquiredAt string `json:"acquiredAt"`
	RenewedAt  string `json:"renewedAt"`
}

type BreakGlass struct {
	Active    *bool  `json:"active"`
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	EnteredAt string `json:"enteredAt"`
}

type Backup struct {
	LastBackupTime      string `json:"lastBackupTime"`
	LastAttemptTime     string `json:"lastAttemptTime"`
	NextScheduledBackup string `json:"nextScheduledBackup"`
	ConsecutiveFailures *int64 `json:"consecutiveFailures"`
	LastFailureReason   string `json:"lastFailureReason"`
	LastFailureMessage  string `json:"lastFailureMessage"`
	LastFailureTime     string `json:"lastFailureTime"`
}

type Upgrade struct {
	ExecutionID      string           `json:"executionID"`
	TargetVersion    string           `json:"targetVersion"`
	FromVersion      string           `json:"fromVersion"`
	StartedAt        string           `json:"startedAt"`
	CurrentPartition *int64           `json:"currentPartition"`
	CompletedPods    []int64          `json:"completedPods"`
	Failure          *ControllerError `json:"failure"`
}

type BlueGreen struct {
	Phase           string `json:"phase"`
	OperationID     string `json:"operationID"`
	GreenVersion    string `json:"greenVersion"`
	StartTime       string `json:"startTime"`
	JobFailureCount *int64 `json:"jobFailureCount"`
	LastJobFailure  string `json:"lastJobFailure"`
	RollbackReason  string `json:"rollbackReason"`
}

type LegacyRestore struct {
	Name               string `json:"name"`
	UID                string `json:"uid"`
	RestartCompletedAt string `json:"restartCompletedAt"`
}

// Attention maps selected shared rule findings and incomplete coverage for status.
type Attention struct {
	Code    string `json:"code"`
	Summary string `json:"summary"`
	Field   string `json:"field"`
}
