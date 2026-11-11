package collect

import (
	"context"

	"github.com/kubebao/openbao-operator-client/adapter"
	shared "github.com/kubebao/openbao-operator-client/evidence"
	"github.com/kubebao/openbao-operator-client/operations"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

// ControlClient shares the caller's transport policy. Collection never submits a control.
func (c *Client) ControlClient() *operations.Client { return c.controls }

func contractFor(group string) adapter.Contract {
	if group == "kubebao.org" {
		return adapter.KubeBaoV1Alpha1Snapshot
	}
	return adapter.V1Alpha1Snapshot
}

func (c *Client) selectAPI(ctx context.Context, snapshot *evidence.Snapshot) bool {
	groups := []string{"kubebao.org", "openbao.org"}
	if c.target.APIGroup != "" {
		groups = []string{c.target.APIGroup}
	}
	selected := ""
	for _, group := range groups {
		request := c.request(contractFor(group), group, "v1alpha1", "openbaoclusters", shared.ReadDiscovery)
		result, err := c.shared.Discover(ctx, request)
		if err != nil {
			snapshot.ResultCode = "incomplete"
			return false
		}
		step := collectionStep(result.Collection)
		step.Action = "discover"
		snapshot.Collection = append(snapshot.Collection, step)
		if result.Collection.Outcome == shared.CollectionUnsupported {
			continue
		}
		if result.Collection.Outcome != shared.CollectionComplete || len(result.Observations) != 1 || !discovered(result.Observations[0]) {
			snapshot.ResultCode = "incomplete"
			return false
		}
		if selected != "" {
			snapshot.ResultCode = "ambiguous_api_group"
			return false
		}
		selected = group
	}
	if selected == "" {
		snapshot.ResultCode = "not_found"
		return false
	}
	c.target.APIGroup, c.target.APIVersion = selected, selected+"/v1alpha1"
	c.contract = contractFor(selected)
	snapshot.Target = c.target
	return true
}
