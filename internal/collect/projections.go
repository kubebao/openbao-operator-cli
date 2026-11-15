package collect

import (
	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	clientcollect "github.com/kubebao/openbao-operator-client/collect"
	shared "github.com/kubebao/openbao-operator-client/evidence"
)

func reference(ref shared.Reference) evidence.Reference {
	return evidence.Reference{
		APIVersion:      ref.APIVersion,
		Kind:            ref.Identity.Kind,
		Namespace:       ref.Identity.Namespace,
		Name:            ref.Identity.Name,
		UID:             ref.Identity.UID,
		ResourceVersion: text(ref.ResourceVersion),
		CollectedAt:     *ref.CollectedAt.Value,
		Field:           ref.Field,
	}
}

func projectRelated(source clientcollect.RelatedResult) *evidence.Related {
	result := &evidence.Related{
		StatefulSets: []evidence.StatefulSet{},
		Pods:         []evidence.Pod{},
		Restores:     []evidence.Restore{},
		Omissions:    []evidence.Omission{},
	}
	associations := map[shared.Identity]shared.Association{}
	for _, a := range source.Associations {
		associations[a.Subject] = a
		if a.State != shared.AssociationConfirmed && a.State != shared.AssociationForeign {
			result.Omissions = append(
				result.Omissions,
				evidence.Omission{
					Reference: evidence.Reference{
						Kind:      a.Subject.Kind,
						Namespace: a.Subject.Namespace,
						Name:      a.Subject.Name,
						UID:       a.Subject.UID,
					},
					Reason: string(a.State),
				},
			)
		}
	}
	if source.StatefulSets != nil {
		for _, v := range source.StatefulSets.Observations {
			a := associations[v.Metadata.Reference.Identity]
			if a.State != shared.AssociationConfirmed {
				continue
			}
			result.StatefulSets = append(result.StatefulSets, projectSet(v, a))
		}
	}
	if source.Pods != nil {
		for _, v := range source.Pods.Observations {
			a := associations[v.Metadata.Reference.Identity]
			if a.State != shared.AssociationConfirmed {
				continue
			}
			value := projectPod(v, a)
			result.Pods = append(result.Pods, value)
		}
	}
	if source.Restores != nil {
		for _, v := range source.Restores.Observations {
			a := associations[v.Reference.Identity]
			if a.State != shared.AssociationConfirmed {
				continue
			}
			result.Restores = append(result.Restores, projectRestore(v, a))
		}
	}
	return result
}

func projectSet(v shared.StatefulSet, a shared.Association) evidence.StatefulSet {
	return evidence.StatefulSet{
		Reference: reference(
			v.Metadata.Reference,
		),
		Association:           evidence.Association{Basis: string(a.Basis), OwnerUID: a.Target.UID},
		Generation:            v.Generation.Value,
		ObservedGeneration:    v.ObservedGeneration.Value,
		Desired:               v.DesiredReplicas.Value,
		Ready:                 v.ReadyReplicas.Value,
		Deleting:              v.Metadata.DeletionTimestamp.State == shared.ValueReported,
		MaintenanceAnnotation: v.Metadata.Maintenance.Value,
	}
}

func projectPod(v shared.Pod, a shared.Association) evidence.Pod {
	value := evidence.Pod{
		Reference: reference(
			v.Metadata.Reference,
		),
		Association:           evidence.Association{Basis: string(a.Basis), OwnerUID: a.Target.UID},
		Phase:                 text(v.Phase),
		Deleting:              v.Metadata.DeletionTimestamp.State == shared.ValueReported,
		MaintenanceAnnotation: v.Metadata.Maintenance.Value,
		Containers:            []evidence.ContainerState{},
	}
	for _, c := range v.Conditions.Values {
		if c.Type == "Ready" {
			if c.Status != shared.ConditionUnknown {
				ready := c.Status == shared.ConditionTrue
				value.Ready = &ready
			}
			value.ReadyReason = text(c.Reason)
		}
	}
	for _, list := range []shared.Containers{v.InitContainers, v.Containers} {
		for _, c := range list.Values {
			value.Containers = append(
				value.Containers,
				evidence.ContainerState{
					Name:             c.Name,
					Kind:             string(c.Kind),
					Ready:            c.Ready.Value,
					RestartCount:     c.RestartCount.Value,
					WaitingReason:    text(c.WaitingReason),
					TerminatedReason: text(c.TerminatedReason),
					ExitCode:         c.ExitCode.Value,
				},
			)
		}
	}
	return value
}

func projectRestore(v shared.Restore, a shared.Association) evidence.Restore {
	return evidence.Restore{
		Reference: reference(
			v.Reference,
		),
		Association: evidence.Association{Basis: string(a.Basis), OwnerUID: a.Target.UID},
		Generation:  v.Generation.Value,
		Phase:       text(v.Phase),
		TargetName:  text(v.TargetName),
		TargetUID:   text(v.Target.UID),
		ExecutionTargetUID: text(
			v.Execution.TargetUID,
		),
		OperationID:              text(v.Execution.OperationID),
		ExecutionStage:           text(v.Execution.Stage),
		AdministratorDisposition: text(v.AdministratorDisposition),
		CompletionTime:           timestamp(v.CompletedAt),
		RestartCompletedAt:       timestamp(v.RestartCompletedAt),
		Conditions:               conditions(v.Conditions, v.Generation, "status.conditions"),
	}
}
