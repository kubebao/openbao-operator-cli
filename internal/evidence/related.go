package evidence

import "time"

type Reference struct {
	APIVersion      string    `json:"apiVersion"`
	Kind            string    `json:"kind"`
	Namespace       string    `json:"namespace"`
	Name            string    `json:"name"`
	UID             string    `json:"uid"`
	ResourceVersion string    `json:"resourceVersion"`
	CollectedAt     time.Time `json:"collectedAt"`
	Field           string    `json:"field,omitempty"`
}

type Association struct {
	Basis    string `json:"basis"`
	OwnerUID string `json:"ownerUID"`
}

type Related struct {
	StatefulSets []StatefulSet `json:"statefulSets"`
	Pods         []Pod         `json:"pods"`
	Restores     []Restore     `json:"restores"`
	Omissions    []Omission    `json:"omissions"`
}

type Omission struct {
	Reference Reference `json:"reference"`
	Reason    string    `json:"reason"`
}

type StatefulSet struct {
	Reference
	Association           Association `json:"association"`
	Generation            *int64      `json:"generation"`
	ObservedGeneration    *int64      `json:"observedGeneration"`
	Desired               *int64      `json:"desired"`
	Ready                 *int64      `json:"ready"`
	Deleting              bool        `json:"deleting"`
	MaintenanceAnnotation *string     `json:"maintenanceAnnotation"`
}

type Pod struct {
	Reference
	Association           Association      `json:"association"`
	Phase                 string           `json:"phase"`
	Ready                 *bool            `json:"ready"`
	ReadyReason           string           `json:"readyReason"`
	ReadyMessage          string           `json:"readyMessage"`
	Deleting              bool             `json:"deleting"`
	MaintenanceAnnotation *string          `json:"maintenanceAnnotation"`
	Containers            []ContainerState `json:"containers"`
}

type ContainerState struct {
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	Ready            *bool  `json:"ready"`
	RestartCount     *int64 `json:"restartCount"`
	WaitingReason    string `json:"waitingReason"`
	WaitingMessage   string `json:"waitingMessage"`
	TerminatedReason string `json:"terminatedReason"`
	ExitCode         *int64 `json:"exitCode"`
}

type Restore struct {
	Reference
	Association              Association `json:"association"`
	Generation               *int64      `json:"generation"`
	Phase                    string      `json:"phase"`
	Message                  string      `json:"message"`
	TargetName               string      `json:"targetName"`
	TargetUID                string      `json:"targetUID"`
	ExecutionTargetUID       string      `json:"executionTargetUID"`
	OperationID              string      `json:"operationID"`
	ExecutionStage           string      `json:"executionStage"`
	AdministratorDisposition string      `json:"administratorDisposition"`
	CompletionTime           string      `json:"completionTime"`
	RestartCompletedAt       string      `json:"restartCompletedAt"`
	Conditions               []Condition `json:"conditions"`
}
