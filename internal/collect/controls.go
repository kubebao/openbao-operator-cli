package collect

import (
	"context"

	"github.com/kubebao/openbao-operator-client/adapter"
	clientcollect "github.com/kubebao/openbao-operator-client/collect"
	shared "github.com/kubebao/openbao-operator-client/evidence"
)

// ControlWorkloads reads bounded namespace workload scopes and verifies the bound target.
// Restore requests are outside maintenance propagation assessment and are not read.
// A paused or unknown reconciliation state cannot establish propagation and adds no workload reads.
func (c *Client) ControlWorkloads(ctx context.Context, cluster shared.Cluster) (clientcollect.RelatedResult, error) {
	if cluster.Paused.Value == nil || *cluster.Paused.Value {
		return clientcollect.RelatedResult{}, nil
	}
	sets := c.request(adapter.KubernetesAppsV1, "apps", "v1", "statefulsets", shared.ReadList)
	pods := c.request(adapter.KubernetesCoreV1, "", "v1", "pods", shared.ReadList)
	sets.Scope.Selector, pods.Scope.Selector = "openbao.org/cluster="+c.target.Name, "openbao.org/cluster="+c.target.Name
	limits := sets.Limits
	limits.MaxPages, limits.MaxCandidates = 48, 2*maxCandidates
	limits.MaxBytes, limits.MaxRetainedBytes = 32*1024*1024, 32*1024*1024
	return c.shared.Related(ctx, cluster, clientcollect.RelatedPlan{
		StatefulSets: &sets, Pods: &pods, Verify: c.clusterRequest(), Limits: limits,
	})
}
