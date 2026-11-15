package collect

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubebao/openbao-operator-client/diagnose"
	shared "github.com/kubebao/openbao-operator-client/evidence"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

// List reads OpenBaoClusters in the selected namespace, or in every namespace, after API selection.
// Each listed cluster becomes a snapshot that shares the list collection, so status rules apply unchanged.
// A partial list keeps the clusters that were read and reports an incomplete result.
func (c *Client) List(ctx context.Context, allNamespaces bool) evidence.ClusterList {
	list := evidence.ClusterList{
		SchemaVersion: "v1", ResultCode: "completed", StartedAt: time.Now().UTC(), AllNamespaces: allNamespaces,
		Clusters: []evidence.Snapshot{}, Collection: []evidence.Step{},
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	selection := evidence.Snapshot{ResultCode: "completed", Target: c.target, Collection: []evidence.Step{}}
	ok := c.contract != "" || c.selectAPI(ctx, &selection)
	list.Collection = selection.Collection
	list.Target = c.target
	if allNamespaces {
		list.Target.Namespace = ""
	}
	if !ok {
		list.ResultCode = selection.ResultCode
		list.CompletedAt = time.Now().UTC()
		return list
	}
	request := c.request(c.contract, c.target.APIGroup, "v1alpha1", "openbaoclusters", shared.ReadList)
	if allNamespaces {
		request.Scope.Namespace, request.AllowAllNamespaces = "", true
	}
	result, err := c.shared.Clusters(ctx, request)
	if err != nil {
		list.ResultCode, list.CompletedAt = "incomplete", time.Now().UTC()
		return list
	}
	list.Collection = append(list.Collection, collectionStep(result.Collection))
	if result.Collection.Outcome != shared.CollectionComplete {
		list.ResultCode = "incomplete"
	}
	list.CompletedAt = time.Now().UTC()
	for _, cluster := range result.Observations {
		target := c.target
		target.Namespace, target.Name, target.UID = cluster.Reference.Identity.Namespace, cluster.Reference.Identity.Name,
			cluster.Reference.Identity.UID
		list.Clusters = append(list.Clusters, evidence.Snapshot{
			SchemaVersion: "v1", ResultCode: "completed", StartedAt: list.StartedAt, CompletedAt: list.CompletedAt,
			Target: target, Observation: projectCluster(cluster), Collection: []evidence.Step{},
			NormalizedEvidence: &diagnose.Capture{Clusters: []diagnose.ClusterInput{{
				Subject: cluster.Reference.Identity, Observation: &cluster, Collection: result.Collection,
			}}},
		})
	}
	slices.SortFunc(list.Clusters, func(a, b evidence.Snapshot) int {
		return cmp.Or(cmp.Compare(a.Target.Namespace, b.Target.Namespace), cmp.Compare(a.Target.Name, b.Target.Name))
	})
	return list
}

// Contexts returns the context names in the selected kubeconfig without contacting a cluster.
func Contexts(kubeconfig string) ([]string, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = kubeconfig
	rules.WarnIfAllMissing = false
	raw, err := rules.Load()
	if err != nil {
		return nil, errors.New("cannot load kubeconfig")
	}
	names := make([]string, 0, len(raw.Contexts))
	for name := range raw.Contexts {
		names = append(names, evidence.Clean(name))
	}
	slices.Sort(names)
	return names, nil
}
