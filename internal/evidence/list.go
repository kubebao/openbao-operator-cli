package evidence

import "time"

// ClusterList describes one bounded list collection of OpenBaoClusters.
// Target names the context, API group, and namespace; Namespace is empty when all namespaces were listed.
type ClusterList struct {
	SchemaVersion string     `json:"schemaVersion"`
	ResultCode    string     `json:"resultCode"`
	StartedAt     time.Time  `json:"startedAt"`
	CompletedAt   time.Time  `json:"completedAt"`
	Target        Target     `json:"target"`
	AllNamespaces bool       `json:"allNamespaces"`
	Clusters      []Snapshot `json:"clusters"`
	Collection    []Step     `json:"collection"`
}
