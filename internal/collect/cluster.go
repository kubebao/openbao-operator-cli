package collect

import (
	"errors"
	"sort"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"

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
		Conditions    []evidence.Condition    `json:"conditions"`
		OperationLock *evidence.OperationLock `json:"operationLock"`
		BreakGlass    *evidence.BreakGlass    `json:"breakGlass"`
		Backup        *evidence.Backup        `json:"backup"`
		Upgrade       *evidence.Upgrade       `json:"upgrade"`
		BlueGreen     *evidence.BlueGreen     `json:"blueGreen"`
		Restore       *evidence.LegacyRestore `json:"restore"`
		Workload      *struct {
			LastError *evidence.ControllerError `json:"lastError"`
		} `json:"workload"`
		AdminOps *struct {
			LastError *evidence.ControllerError `json:"lastError"`
		} `json:"adminOps"`
	} `json:"status"`
}

func ProjectV1Alpha1(object *unstructured.Unstructured, collectedAt time.Time) (*evidence.Observation, error) {
	var source clusterV1Alpha1
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &source); err != nil {
		return nil, errors.New("resource fields do not match the v1alpha1 status adapter")
	}
	o := &evidence.Observation{
		CollectedAt: collectedAt, ResourceVersion: object.GetResourceVersion(),
		Adapter: "openbaocluster-v1alpha1/v1", Qualification: "unqualified",
		Generation: source.Metadata.Generation, ObservedGeneration: source.Status.ObservedGeneration,
		GenerationCurrent: evidence.GenerationCurrent(source.Metadata.Generation, source.Status.ObservedGeneration),
		Phase:             source.Status.Phase, DesiredVersion: source.Spec.Version, ObservedVersion: source.Status.CurrentVersion,
		Voters: evidence.Replicas{Desired: source.Spec.Replicas, Ready: source.Status.ReadyReplicas},
		Paused: source.Spec.Paused, MaintenancePropagation: "not_collected",
		OperationLock: source.Status.OperationLock, BreakGlass: source.Status.BreakGlass,
		Backup: source.Status.Backup, Upgrade: source.Status.Upgrade, BlueGreen: source.Status.BlueGreen,
		LegacyRestore: source.Status.Restore, Conditions: source.Status.Conditions,
		Attention: []evidence.Attention{},
		Limitations: []string{
			"No operator release is qualified; known v1alpha1 fields are projected with interpretation limits.",
			"Collection time is not the time of the operator's observation; no reconciliation heartbeat is available.",
			"Cluster fields alone do not verify maintenance propagation or current restore progress.",
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
	if origin, found, err := unstructured.NestedString(object.Object, "metadata", "annotations", "openbao.org/restore-origin"); err != nil {
		return nil, errors.New("restore origin does not match the v1alpha1 status adapter")
	} else if found {
		o.RestoreOrigin = origin
	}
	if source.Status.Workload != nil {
		o.WorkloadError = source.Status.Workload.LastError
	}
	if source.Status.AdminOps != nil {
		o.AdminOpsError = source.Status.AdminOps.LastError
	}
	if o.Conditions == nil {
		o.Conditions = []evidence.Condition{}
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
		condition.GenerationCurrent = evidence.GenerationCurrent(o.Generation, condition.ObservedGeneration)
		condition.Field = "status.conditions[type=" + evidence.Clean(condition.Type) + "]"
		evidence.CleanStrings(&condition.Type, &condition.Status, &condition.Reason, &condition.Message, &condition.LastTransitionTime)
	}
	sort.Slice(o.Conditions, func(i, j int) bool { return o.Conditions[i].Type < o.Conditions[j].Type })
	for i := range o.Conditions {
		if o.Conditions[i].Type == "Available" {
			o.Available = &o.Conditions[i]
		}
	}
	evidence.SanitizeObservation(o)
	o.Attention = evidence.ClusterAttention(o)
	return o, nil
}
