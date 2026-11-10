package collect

import (
	"errors"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

func reference(object *unstructured.Unstructured) evidence.Reference {
	return evidence.Reference{
		APIVersion: evidence.Clean(object.GetAPIVersion()), Kind: evidence.Clean(object.GetKind()),
		Namespace: evidence.Clean(object.GetNamespace()), Name: evidence.Clean(object.GetName()),
		UID: evidence.Clean(string(object.GetUID())), ResourceVersion: evidence.Clean(object.GetResourceVersion()),
		CollectedAt: time.Now().UTC(),
	}
}

func maintenanceAnnotation(object *unstructured.Unstructured) *string {
	value, ok := object.GetAnnotations()["openbao.org/maintenance"]
	if !ok {
		return nil
	}
	value = evidence.Clean(value)
	return &value
}

func projectStatefulSet(object *unstructured.Unstructured, association evidence.Association) (evidence.StatefulSet, error) {
	var wire struct {
		Metadata struct {
			Generation *int64 `json:"generation"`
		} `json:"metadata"`
		Spec struct {
			Replicas *int64 `json:"replicas"`
		} `json:"spec"`
		Status struct {
			ObservedGeneration *int64 `json:"observedGeneration"`
			ReadyReplicas      *int64 `json:"readyReplicas"`
		} `json:"status"`
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &wire); err != nil {
		return evidence.StatefulSet{}, errors.New("invalid StatefulSet projection")
	}
	return evidence.StatefulSet{
		Reference: reference(object), Association: association,
		Generation: wire.Metadata.Generation, ObservedGeneration: wire.Status.ObservedGeneration,
		Desired: wire.Spec.Replicas, Ready: wire.Status.ReadyReplicas,
		Deleting: object.GetDeletionTimestamp() != nil, MaintenanceAnnotation: maintenanceAnnotation(object),
	}, nil
}

type containerProjection struct {
	Name         string `json:"name"`
	Ready        *bool  `json:"ready"`
	RestartCount *int64 `json:"restartCount"`
	State        struct {
		Waiting *struct {
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"waiting"`
		Terminated *struct {
			Reason   string `json:"reason"`
			ExitCode *int64 `json:"exitCode"`
		} `json:"terminated"`
	} `json:"state"`
}

func projectPod(object *unstructured.Unstructured, association evidence.Association) (evidence.Pod, error) {
	var wire struct {
		Status struct {
			Phase      string `json:"phase"`
			Conditions []struct {
				Type    string `json:"type"`
				Status  string `json:"status"`
				Reason  string `json:"reason"`
				Message string `json:"message"`
			} `json:"conditions"`
			Containers     []containerProjection `json:"containerStatuses"`
			InitContainers []containerProjection `json:"initContainerStatuses"`
		} `json:"status"`
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &wire); err != nil {
		return evidence.Pod{}, errors.New("invalid Pod projection")
	}
	result := evidence.Pod{
		Reference:             reference(object),
		Association:           association,
		Phase:                 evidence.Clean(wire.Status.Phase),
		Deleting:              object.GetDeletionTimestamp() != nil,
		MaintenanceAnnotation: maintenanceAnnotation(object),
		Containers:            []evidence.ContainerState{},
	}
	seen := false
	for _, condition := range wire.Status.Conditions {
		if condition.Type != "Ready" {
			continue
		}
		if seen {
			return evidence.Pod{}, errors.New("duplicate Pod Ready condition")
		}
		seen = true
		if condition.Status == "True" || condition.Status == "False" {
			value := condition.Status == "True"
			result.Ready = &value
		} else if condition.Status != "Unknown" {
			return evidence.Pod{}, errors.New("invalid Pod Ready condition")
		}
		result.ReadyReason, result.ReadyMessage = evidence.Clean(condition.Reason), evidence.Clean(condition.Message)
	}
	result.Containers = append(result.Containers, projectContainers(wire.Status.InitContainers, "init")...)
	result.Containers = append(result.Containers, projectContainers(wire.Status.Containers, "app")...)
	return result, nil
}

func projectContainers(containers []containerProjection, kind string) []evidence.ContainerState {
	result := make([]evidence.ContainerState, 0, len(containers))
	for _, container := range containers {
		state := evidence.ContainerState{
			Name:         evidence.Clean(container.Name),
			Kind:         kind,
			Ready:        container.Ready,
			RestartCount: container.RestartCount,
		}
		if waiting := container.State.Waiting; waiting != nil {
			state.WaitingReason, state.WaitingMessage = evidence.Clean(waiting.Reason), evidence.Clean(waiting.Message)
		}
		if terminated := container.State.Terminated; terminated != nil {
			state.TerminatedReason, state.ExitCode = evidence.Clean(terminated.Reason), terminated.ExitCode
		}
		result = append(result, state)
	}
	return result
}

func projectRestore(object *unstructured.Unstructured, association evidence.Association) (evidence.Restore, error) {
	var wire struct {
		Metadata struct {
			Generation *int64 `json:"generation"`
		} `json:"metadata"`
		Spec struct {
			Cluster string `json:"cluster"`
		} `json:"spec"`
		Status struct {
			Phase                    string `json:"phase"`
			Message                  string `json:"message"`
			CompletionTime           string `json:"completionTime"`
			AdministratorDisposition string `json:"administratorDisposition"`
			Target                   *struct {
				UID string `json:"uid"`
			} `json:"target"`
			Execution *struct {
				TargetUID   string `json:"targetUID"`
				OperationID string `json:"operationID"`
				Stage       string `json:"stage"`
			} `json:"execution"`
			Restart *struct {
				CompletedAt string `json:"completedAt"`
			} `json:"restart"`
			Conditions []evidence.Condition `json:"conditions"`
		} `json:"status"`
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &wire); err != nil {
		return evidence.Restore{}, errors.New("invalid restore projection")
	}
	result := evidence.Restore{
		Reference:   reference(object),
		Association: association,
		Generation:  wire.Metadata.Generation,
		Phase: evidence.Clean(
			wire.Status.Phase,
		),
		Message:    evidence.Clean(wire.Status.Message),
		TargetName: evidence.Clean(wire.Spec.Cluster),
		CompletionTime: evidence.Clean(
			wire.Status.CompletionTime,
		),
		AdministratorDisposition: evidence.Clean(wire.Status.AdministratorDisposition),
		Conditions:               wire.Status.Conditions,
	}
	if wire.Status.Target != nil {
		result.TargetUID = evidence.Clean(wire.Status.Target.UID)
	}
	if wire.Status.Execution != nil {
		result.ExecutionTargetUID = evidence.Clean(wire.Status.Execution.TargetUID)
		result.OperationID, result.ExecutionStage = evidence.Clean(
			wire.Status.Execution.OperationID,
		), evidence.Clean(
			wire.Status.Execution.Stage,
		)
	}
	if wire.Status.Restart != nil {
		result.RestartCompletedAt = evidence.Clean(wire.Status.Restart.CompletedAt)
	}
	if result.Conditions == nil {
		result.Conditions = []evidence.Condition{}
	}
	seen := map[string]bool{}
	for i := range result.Conditions {
		condition := &result.Conditions[i]
		if condition.Type == "" || seen[condition.Type] ||
			(condition.Status != "True" && condition.Status != "False" && condition.Status != "Unknown") {
			return evidence.Restore{}, errors.New("invalid restore conditions")
		}
		seen[condition.Type] = true
		evidence.CleanStrings(&condition.Type, &condition.Reason, &condition.Message, &condition.LastTransitionTime)
		condition.Field = "status.conditions[type=" + condition.Type + "]"
		condition.GenerationCurrent = evidence.GenerationCurrent(result.Generation, condition.ObservedGeneration)
	}
	return result, nil
}
