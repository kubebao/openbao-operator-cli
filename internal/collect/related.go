package collect

import (
	"context"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	clientcollect "github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/diagnose"
	shared "github.com/kubebao/openbao-operator-client/evidence"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

func (c *Client) CollectDiagnostic(ctx context.Context) evidence.Snapshot {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	snapshot := c.Collect(ctx)
	if snapshot.NormalizedEvidence == nil {
		return snapshot
	}
	input := &snapshot.NormalizedEvidence.Clusters[0]
	sets := c.request(adapter.KubernetesAppsV1, "apps", "v1", "statefulsets", shared.ReadList)
	pods := c.request(adapter.KubernetesCoreV1, "", "v1", "pods", shared.ReadList)
	restores := c.request(c.contract, c.target.APIGroup, "v1alpha1", "openbaorestores", shared.ReadList)
	sets.Scope.Selector, pods.Scope.Selector = "openbao.org/cluster="+c.target.Name, "openbao.org/cluster="+c.target.Name
	limits := sets.Limits
	limits.MaxPages, limits.MaxCandidates, limits.MaxBytes, limits.MaxRetainedBytes = 64, 3*maxCandidates, 48*1024*1024, 48*1024*1024
	result, err := c.shared.Related(
		ctx,
		*input.Observation,
		clientcollect.RelatedPlan{StatefulSets: &sets, Pods: &pods, Restores: &restores, Verify: c.clusterRequest(), Limits: limits},
	)
	if err != nil {
		snapshot.ResultCode = "incomplete"
		snapshot.CompletedAt = time.Now().UTC()
		return snapshot
	}
	input.Verification = &result.Verification
	if result.StatefulSets != nil {
		input.StatefulSets = &diagnose.ResourceInput[shared.StatefulSet]{
			Collection:   result.StatefulSets.Collection,
			Observations: result.StatefulSets.Observations,
		}
		snapshot.Collection = append(snapshot.Collection, collectionStep(result.StatefulSets.Collection))
	}
	if result.Pods != nil {
		input.Pods = &diagnose.ResourceInput[shared.Pod]{
			Collection:   result.Pods.Collection,
			Observations: result.Pods.Observations,
		}
		snapshot.Collection = append(snapshot.Collection, collectionStep(result.Pods.Collection))
	}
	if result.Restores != nil {
		input.Restores = &diagnose.ResourceInput[shared.Restore]{
			Collection:   result.Restores.Collection,
			Observations: result.Restores.Observations,
		}
		snapshot.Collection = append(snapshot.Collection, collectionStep(result.Restores.Collection))
	}
	step := collectionStep(result.Verification.Read)
	step.Action = "verify_target"
	if result.Verification.After != nil {
		step.UID = result.Verification.After.Identity.UID
		step.ResourceVersion = text(result.Verification.After.ResourceVersion)
		step.Generation = result.Verification.AfterGeneration.Value
	}
	snapshot.Collection = append(snapshot.Collection, step)
	snapshot.Related = projectRelated(result)
	if result.Outcome != shared.CollectionComplete {
		snapshot.ResultCode = "incomplete"
	}
	switch result.Verification.State {
	case shared.VerificationDeleted:
		snapshot.ResultCode = "resource_deleted"
	case shared.VerificationReplaced:
		snapshot.ResultCode = "resource_replaced"
	case shared.VerificationChanged:
		snapshot.ResultCode = "target_changed"
	case shared.VerificationInsufficient:
		snapshot.ResultCode = "target_verification_incomplete"
	}
	snapshot.CompletedAt = time.Now().UTC()
	return snapshot
}
