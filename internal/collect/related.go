package collect

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

const maxCandidates = 1000

// CollectDiagnostic adds bounded, projected evidence without broadening basic status.
func (c *Client) CollectDiagnostic(ctx context.Context) evidence.Snapshot {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	snapshot := c.Collect(ctx)
	if snapshot.Observation == nil {
		return snapshot
	}
	snapshot.Related = &evidence.Related{
		StatefulSets: []evidence.StatefulSet{}, Pods: []evidence.Pod{},
		Restores: []evidence.Restore{}, Omissions: []evidence.Omission{},
	}
	sets := map[string]evidence.StatefulSet{}
	foreignSets := map[string]bool{}
	setStep := c.listRelated(ctx, &snapshot, schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"},
		"StatefulSet", "openbao.org/cluster="+c.target.Name, func(object *unstructured.Unstructured) {
			addStatefulSet(&snapshot, object, sets, foreignSets)
		})
	snapshot.Collection = append(snapshot.Collection, setStep)
	podStep := c.listRelated(ctx, &snapshot, schema.GroupVersionResource{Version: "v1", Resource: "pods"},
		"Pod", "openbao.org/cluster="+c.target.Name, func(object *unstructured.Unstructured) {
			addPod(&snapshot, object, sets, foreignSets)
		})
	snapshot.Collection = append(snapshot.Collection, podStep)
	// Restores have no declared target-UID selector. Inspect a bounded namespace list,
	// retaining only UID-correlated requests, never their source or authentication spec.
	restoreStep := c.listRelated(ctx, &snapshot,
		schema.GroupVersionResource{Group: c.group, Version: "v1alpha1", Resource: "openbaorestores"},
		"OpenBaoRestore", "", func(object *unstructured.Unstructured) {
			addRestore(&snapshot, object)
		})
	snapshot.Collection = append(snapshot.Collection, restoreStep)
	for _, step := range []evidence.Step{setStep, podStep, restoreStep} {
		if step.Outcome != "collected" && step.Outcome != "not_applicable" {
			snapshot.ResultCode, snapshot.ExitCode = "incomplete", 3
		}
	}
	c.verifyDiagnosticTarget(ctx, &snapshot)
	snapshot.CompletedAt = time.Now().UTC()
	return snapshot
}

func (c *Client) listRelated(
	ctx context.Context, snapshot *evidence.Snapshot, gvr schema.GroupVersionResource,
	kind, selector string, visit func(*unstructured.Unstructured),
) evidence.Step {
	step := evidence.Step{APIGroup: gvr.Group, Resource: gvr.Resource, Namespace: c.target.Namespace, Action: "list", Outcome: "collected"}
	options := metav1.ListOptions{LabelSelector: selector, Limit: 100}
	seenTokens, seenUIDs := map[string]bool{}, map[string]bool{}
	resourceVersion := ""
	initialOmissions, count := len(snapshot.Related.Omissions), 0
	for {
		page, err := c.resources.Resource(gvr).Namespace(c.target.Namespace).List(ctx, options)
		step.CollectedAt = time.Now().UTC()
		if err != nil {
			step.Outcome = outcome(err)
			if step.Outcome == "not_found" {
				step.Outcome = "not_applicable"
			}
			break
		}
		if resourceVersion != "" && page.GetResourceVersion() != resourceVersion {
			step.Outcome = "inconsistent_pages"
			break
		}
		resourceVersion = page.GetResourceVersion()
		step.ResourceVersion = evidence.Clean(resourceVersion)
		for i := range page.Items {
			if count >= maxCandidates {
				step.Outcome = "limit_reached"
				break
			}
			count++
			object := &page.Items[i]
			if reason := prepareRelated(object, gvr, kind, c.target.Namespace, seenUIDs); reason != "" {
				step.Outcome = reason
				continue
			}
			visit(object)
		}
		next := page.GetContinue()
		if next == "" || step.Outcome != "collected" {
			break
		}
		if count >= maxCandidates {
			step.Outcome = "limit_reached"
			break
		}
		if seenTokens[next] {
			step.Outcome = "repeated_continue_token"
			break
		}
		seenTokens[next], options.Continue = true, next
	}
	if step.Outcome == "collected" && len(snapshot.Related.Omissions) > initialOmissions {
		step.Outcome = "association_incomplete"
	}
	return step
}

func (c *Client) verifyDiagnosticTarget(ctx context.Context, snapshot *evidence.Snapshot) {
	final := c.Collect(ctx)
	for _, step := range final.Collection {
		step.Action, step.Resource = "verify_target", "openbaoclusters"
		if final.Observation != nil {
			step.Generation = final.Observation.Generation
		}
		snapshot.Collection = append(snapshot.Collection, step)
	}
	if final.ExitCode != 0 {
		snapshot.ResultCode, snapshot.ExitCode = final.ResultCode, 3
		return
	}
	before, after := snapshot.Observation.Generation, final.Observation.Generation
	if before == nil || after == nil {
		snapshot.ResultCode, snapshot.ExitCode = "target_verification_incomplete", 3
	} else if *before != *after {
		snapshot.ResultCode, snapshot.ExitCode = "target_changed", 3
	}
}

func omit(snapshot *evidence.Snapshot, object *unstructured.Unstructured, reason string) {
	snapshot.Related.Omissions = append(snapshot.Related.Omissions, evidence.Omission{Reference: reference(object), Reason: reason})
}

func controllerOwner(object *unstructured.Unstructured) *metav1.OwnerReference {
	var result *metav1.OwnerReference
	for _, owner := range object.GetOwnerReferences() {
		if owner.Controller == nil || !*owner.Controller {
			continue
		}
		if result != nil {
			return nil
		}
		copy := owner
		result = &copy
	}
	return result
}

func clusterOwner(object *unstructured.Unstructured, target evidence.Target) (evidence.Association, string) {
	owner := controllerOwner(object)
	annotation := object.GetAnnotations()["openbao.org/owner-uid"]
	if owner == nil {
		return evidence.Association{}, "unknown"
	}
	if string(owner.UID) != target.UID {
		if annotation == target.UID {
			return evidence.Association{}, "conflict"
		}
		return evidence.Association{}, "foreign"
	}
	if owner.APIVersion != target.APIVersion || owner.Kind != "OpenBaoCluster" || owner.Name != target.Name ||
		(annotation != "" && annotation != target.UID) {
		return evidence.Association{}, "conflict"
	}
	return evidence.Association{Basis: "metadata.ownerReferences", OwnerUID: target.UID}, "associated"
}

func restoreOwner(object *unstructured.Unstructured, snapshot evidence.Snapshot) (evidence.Association, string) {
	name, _, nameErr := unstructured.NestedString(object.Object, "spec", "cluster")
	if nameErr != nil {
		return evidence.Association{}, "unknown"
	}
	target, _, err := unstructured.NestedString(object.Object, "status", "target", "uid")
	execution, _, executionErr := unstructured.NestedString(object.Object, "status", "execution", "targetUID")
	if err != nil || executionErr != nil {
		return evidence.Association{}, "unknown"
	}
	uid := snapshot.Target.UID
	if name != snapshot.Target.Name {
		if target == uid || execution == uid || snapshot.Observation.RestoreOrigin == string(object.GetUID()) {
			return evidence.Association{}, "conflict"
		}
		return evidence.Association{}, "foreign"
	}
	if (target != "" && target != uid) || (execution != "" && execution != uid) {
		if target == uid || execution == uid || snapshot.Observation.RestoreOrigin == string(object.GetUID()) {
			return evidence.Association{}, "conflict"
		}
		return evidence.Association{}, "foreign"
	}
	basis := ""
	switch {
	case execution == uid:
		basis = "status.execution.targetUID"
	case target == uid:
		basis = "status.target.uid"
	case snapshot.Observation.RestoreOrigin == string(object.GetUID()):
		basis = "metadata.annotations[openbao.org/restore-origin]"
	case snapshot.Observation.RestoreHold == string(object.GetUID()):
		basis = "metadata.annotations[openbao.org/restore-hold]"
	}
	if basis == "" {
		return evidence.Association{}, "unknown"
	}
	return evidence.Association{Basis: basis, OwnerUID: uid}, "associated"
}

func addStatefulSet(
	snapshot *evidence.Snapshot,
	object *unstructured.Unstructured,
	sets map[string]evidence.StatefulSet,
	foreignSets map[string]bool,
) {
	association, state := clusterOwner(object, snapshot.Target)
	if state == "foreign" {
		foreignSets[string(object.GetUID())] = true
		return
	}
	if state != "associated" {
		omit(snapshot, object, "unverified_cluster_owner")
		return
	}
	projected, err := projectStatefulSet(object, association)
	if err != nil {
		omit(snapshot, object, "invalid_projection")
		return
	}
	snapshot.Related.StatefulSets = append(snapshot.Related.StatefulSets, projected)
	sets[string(object.GetUID())] = projected
}

func addPod(
	snapshot *evidence.Snapshot,
	object *unstructured.Unstructured,
	sets map[string]evidence.StatefulSet,
	foreignSets map[string]bool,
) {
	owner := controllerOwner(object)
	if owner != nil && owner.Kind != "StatefulSet" {
		return
	}
	if owner != nil && foreignSets[string(owner.UID)] {
		return
	}
	if owner == nil || owner.APIVersion != "apps/v1" {
		omit(snapshot, object, "unverified_workload_owner")
		return
	}
	set, ok := sets[string(owner.UID)]
	if !ok || owner.Name != set.Name {
		omit(snapshot, object, "unverified_workload_owner")
		return
	}
	projected, err := projectPod(object, evidence.Association{Basis: "metadata.ownerReferences", OwnerUID: set.UID})
	if err != nil {
		omit(snapshot, object, "invalid_projection")
		return
	}
	snapshot.Related.Pods = append(snapshot.Related.Pods, projected)
}

func addRestore(snapshot *evidence.Snapshot, object *unstructured.Unstructured) {
	association, state := restoreOwner(object, *snapshot)
	if state == "foreign" {
		return
	}
	if state != "associated" {
		omit(snapshot, object, "unverified_restore_target")
		return
	}
	projected, err := projectRestore(object, association)
	if err != nil {
		omit(snapshot, object, "invalid_projection")
		return
	}
	snapshot.Related.Restores = append(snapshot.Related.Restores, projected)
}

func prepareRelated(
	object *unstructured.Unstructured,
	gvr schema.GroupVersionResource,
	kind, namespace string,
	seen map[string]bool,
) string {
	if object.GetAPIVersion() == "" {
		object.SetAPIVersion(gvr.GroupVersion().String())
	}
	if object.GetKind() == "" {
		object.SetKind(kind)
	}
	if object.GetAPIVersion() != gvr.GroupVersion().String() || object.GetKind() != kind || object.GetNamespace() != namespace ||
		object.GetUID() == "" ||
		object.GetName() == "" {
		return "invalid_resource"
	}
	uid := string(object.GetUID())
	if seen[uid] {
		return "duplicate_resource"
	}
	seen[uid] = true
	return ""
}
