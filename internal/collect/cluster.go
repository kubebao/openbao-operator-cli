package collect

import (
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	shared "github.com/kubebao/openbao-operator-client/evidence"
)

func text(value shared.Text) string {
	if value.Value == nil {
		return ""
	}
	return *value.Value
}

func timestamp(value shared.Timestamp) string {
	if value.Value == nil {
		return ""
	}
	return value.Value.UTC().Format(time.RFC3339Nano)
}

func acknowledged(state shared.GenerationState) *bool {
	switch state {
	case shared.GenerationCurrent:
		value := true
		return &value
	case shared.GenerationStale, shared.GenerationInconsistent:
		value := false
		return &value
	default:
		return nil
	}
}

func conditions(values shared.Conditions, generation shared.Integer, prefix string) []evidence.Condition {
	result := []evidence.Condition{}
	for _, c := range values.Values {
		result = append(result, evidence.Condition{
			Type:               c.Type,
			Status:             string(c.Status),
			Reason:             text(c.Reason),
			ObservedGeneration: c.ObservedGeneration.Value,
			LastTransitionTime: timestamp(
				c.LastTransitionAt,
			),
			GenerationCurrent: acknowledged(c.Acknowledgement(generation)),
			Field:             prefix + "[type=" + c.Type + "]",
		})
	}
	return result
}

func controllerError(value shared.ControllerError) *evidence.ControllerError {
	if value.State != shared.ObjectReported {
		return nil
	}
	return &evidence.ControllerError{Reason: text(value.Reason), At: timestamp(value.At)}
}

// projectCluster maps approved normalized facts into the CLI's display model.
// Empty message fields preserve JSON layout while excluding all provider text.
func projectCluster(source shared.Cluster) *evidence.Observation {
	o := &evidence.Observation{
		CollectedAt:        *source.Reference.CollectedAt.Value,
		ResourceVersion:    text(source.Reference.ResourceVersion),
		Qualification:      "unqualified",
		Generation:         source.Generation.Value,
		ObservedGeneration: source.ObservedGeneration.Value,
		GenerationCurrent:  acknowledged(source.Acknowledgement()),
		Phase:              text(source.Phase),
		DesiredVersion:     text(source.DesiredVersion),
		ObservedVersion:    text(source.ObservedVersion),
		Voters: evidence.Replicas{
			Desired: source.DesiredReplicas.Value,
			Ready:   source.ReadyReplicas.Value,
		},
		ReadReplicas:           evidence.Replicas{Desired: source.ReadReplicas.Desired.Value, Ready: source.ReadReplicas.Ready.Value},
		Paused:                 source.Paused.Value,
		MaintenanceEnabled:     source.Maintenance.Enabled.Value,
		MaintenancePropagation: "not_collected",
		RestoreOrigin: text(
			source.RestoreOrigin,
		),
		RestoreHold:   text(source.RestoreHold),
		WorkloadError: controllerError(source.WorkloadError),
		AdminOpsError: controllerError(source.AdminOpsError),
		Conditions:    conditions(source.Conditions, source.Generation, "status.conditions"),
		Attention:     []evidence.Attention{},
		Limitations: []string{
			"Projection uses inspected API semantics; discovery alone does not qualify an operator release.",
			"Collection time is not an operator heartbeat; condition transitions are not producer observation times.",
			"Cluster fields do not verify maintenance propagation or current restore progress.",
			"Provider messages are excluded, including verbose and JSON output.",
		},
	}
	for i := range o.Conditions {
		if o.Conditions[i].Type == "Available" {
			o.Available = &o.Conditions[i]
		}
	}
	if v := source.OperationLock; v.State == shared.ObjectReported {
		o.OperationLock = &evidence.OperationLock{
			Operation:  text(v.Operation),
			Holder:     text(v.Holder),
			AcquiredAt: timestamp(v.AcquiredAt),
			RenewedAt:  timestamp(v.RenewedAt),
		}
	}
	if v := source.BreakGlass; v.State == shared.ObjectReported {
		o.BreakGlass = &evidence.BreakGlass{Active: v.Active.Value, Reason: text(v.Reason), EnteredAt: timestamp(v.EnteredAt)}
	}
	if v := source.Backup; v.State == shared.ObjectReported {
		o.Backup = &evidence.Backup{
			LastBackupTime:      timestamp(v.LastSuccessAt),
			LastAttemptTime:     timestamp(v.LastAttemptAt),
			NextScheduledBackup: timestamp(v.NextScheduledAt),
			ConsecutiveFailures: v.ConsecutiveFailures.Value,
			LastFailureReason:   text(v.LastFailureReason),
			LastFailureTime:     timestamp(v.LastFailureAt),
		}
	}
	if v := source.Upgrade; v.State == shared.ObjectReported {
		o.Upgrade = &evidence.Upgrade{
			ExecutionID:      text(v.ExecutionID),
			TargetVersion:    text(v.TargetVersion),
			FromVersion:      text(v.FromVersion),
			StartedAt:        timestamp(v.StartedAt),
			CurrentPartition: v.CurrentPartition.Value,
			CompletedPods:    v.CompletedPods.Values,
			Failure:          controllerError(v.Failure),
		}
	}
	if v := source.BlueGreen; v.State == shared.ObjectReported {
		o.BlueGreen = &evidence.BlueGreen{
			Phase:           text(v.Phase),
			OperationID:     text(v.OperationID),
			GreenVersion:    text(v.GreenVersion),
			StartTime:       timestamp(v.StartedAt),
			JobFailureCount: v.JobFailureCount.Value,
			LastJobFailure:  text(v.LastFailedJob),
		}
	}
	if v := source.LegacyRestore; v.State == shared.ObjectReported {
		o.LegacyRestore = &evidence.LegacyRestore{Name: text(v.Name), UID: text(v.UID), RestartCompletedAt: timestamp(v.RestartCompletedAt)}
	}
	return o
}
