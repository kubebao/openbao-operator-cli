package doctor

import (
	"fmt"
	"strings"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

func clusterAvailability(snapshot evidence.Snapshot) evaluation {
	result := startRule("KBAO-CLUSTER-001")
	o := snapshot.Observation
	if o.Available == nil {
		result.missing("The operator's Available condition is absent")
		return result
	}
	condition := o.Available
	if o.GenerationCurrent == nil || !*o.GenerationCurrent || condition.GenerationCurrent == nil || !*condition.GenerationCurrent {
		result.missing("Current-generation operator availability has not been confirmed")
	}
	ref := clusterReference(snapshot, condition.Field)
	switch condition.Status {
	case "Unknown":
		result.missing("The operator reports availability as Unknown")
		result.add(
			Info,
			"Operator availability is unknown",
			condition.Message,
			"Inspect the condition reason and reconciliation restrictions",
			ref,
		)
	case "False":
		summary := "The operator reports Available=False"
		if condition.GenerationCurrent == nil || !*condition.GenerationCurrent {
			summary += " for unconfirmed generation evidence"
		}
		result.add(
			Warning,
			summary,
			condition.Reason+": "+condition.Message,
			"Inspect workload readiness and the operator condition details",
			ref,
		)
	}
	return result
}

func workloadReadiness(snapshot evidence.Snapshot) evaluation {
	result := startRule("KBAO-WORKLOAD-001")
	if snapshot.Related == nil {
		result.missing("The StatefulSet collection or ownership association is incomplete")
		return result
	}
	complete := scopeOutcome(snapshot, "statefulsets") == "collected"
	if !complete {
		result.missing("The StatefulSet collection or ownership association is incomplete")
	}
	if len(snapshot.Related.StatefulSets) == 0 && snapshot.Observation.Voters.Desired == nil {
		result.missing("No associated StatefulSet or declared voter count is available")
	}
	if complete && len(snapshot.Related.StatefulSets) == 0 && snapshot.Observation.Voters.Desired != nil &&
		*snapshot.Observation.Voters.Desired > 0 {
		result.add(Warning, "No UID-associated StatefulSet was found in the workload scope",
			"The declared voter count is greater than zero", "Inspect operator reconciliation, workload labels, and owner references",
			clusterReference(snapshot, "spec.replicas"))
	}
	for _, set := range snapshot.Related.StatefulSets {
		if set.Deleting {
			continue
		}
		current := evidence.GenerationCurrent(set.Generation, set.ObservedGeneration)
		if current == nil || !*current || set.Ready == nil || set.Desired == nil {
			result.missing("StatefulSet " + set.Name + " lacks current-generation replica evidence")
			continue
		}
		if *set.Ready < *set.Desired {
			result.add(
				Warning,
				"StatefulSet "+set.Name+" has fewer Ready replicas than desired",
				fmt.Sprintf("Kubernetes reports %d Ready replicas and %d desired replicas", *set.Ready, *set.Desired),
				"Inspect the associated Pods and StatefulSet rollout",
				field(set.Reference, "status.readyReplicas"),
				field(set.Reference, "spec.replicas"),
			)
		}
	}
	return result
}

func podReadiness(snapshot evidence.Snapshot) evaluation {
	result := startRule("KBAO-POD-001")
	if snapshot.Related == nil {
		result.missing("The Pod collection or workload ownership association is incomplete")
		return result
	}
	complete := scopeOutcome(snapshot, "pods") == "collected"
	if !complete {
		result.missing("The Pod collection or workload ownership association is incomplete")
	}
	if complete {
		evaluatePodPopulation(snapshot, &result)
	}
	for _, pod := range snapshot.Related.Pods {
		if pod.Deleting {
			continue
		}
		if pod.Ready == nil {
			result.missing("Pod " + pod.Name + " has no usable Ready condition")
		}
		if pod.Phase == "Failed" {
			result.add(Error, "Pod "+pod.Name+" reports Failed phase", pod.ReadyMessage,
				"Inspect Pod conditions and container termination reasons", field(pod.Reference, "status.phase"))
		} else if pod.Ready != nil && !*pod.Ready {
			description := strings.TrimSpace(pod.ReadyReason + ": " + pod.ReadyMessage)
			refs := []evidence.Reference{field(pod.Reference, "status.conditions[type=Ready]")}
			var descriptionSb112 strings.Builder
			for _, container := range pod.Containers {
				if container.WaitingReason == "" {
					continue
				}
				descriptionSb112.WriteString("; " + container.Name + ": " + container.WaitingReason)
				path := "status.containerStatuses"
				if container.Kind == "init" {
					path = "status.initContainerStatuses"
				}
				refs = append(refs, field(pod.Reference, path+"[name="+container.Name+"].state.waiting"))
			}
			description += descriptionSb112.String()
			result.add(Warning, "Pod "+pod.Name+" is not Ready", description,
				"Inspect the reported Ready condition and container state; use approved log access if needed", refs...)
		}
	}
	return result
}

func restrictions(snapshot evidence.Snapshot) evaluation {
	result := startRule("KBAO-RESTRICTION-001")
	o := snapshot.Observation
	if o.Paused != nil && *o.Paused {
		result.add(Info, "Reconciliation is requested paused", "Existing Jobs and Kubernetes controllers can continue",
			"Confirm that the pause is intentional before requesting resume", clusterReference(snapshot, "spec.paused"))
	}
	if o.MaintenanceEnabled != nil && *o.MaintenanceEnabled {
		result.add(Info, "Maintenance mode is requested", "This does not establish workload annotation propagation or grant permissions",
			"Verify the approved maintenance procedure and workload annotations", clusterReference(snapshot, "spec.maintenance.enabled"))
	}
	if o.RestoreHold != "" {
		result.add(
			Info,
			"A restore hold restricts ordinary management",
			"The hold is independent of pause and maintenance",
			"Inspect the correlated restore and its recovery procedure",
			clusterReference(snapshot, "metadata.annotations[openbao.org/restore-hold]"),
		)
	}
	if o.BreakGlass != nil && o.BreakGlass.Active != nil && *o.BreakGlass.Active {
		result.add(Warning, "Break-glass restrictions are active", o.BreakGlass.Reason+": "+o.BreakGlass.Message,
			"Follow the operator's operation-specific recovery procedure", clusterReference(snapshot, "status.breakGlass"))
	}
	return result
}

func restoreRecovery(snapshot evidence.Snapshot) evaluation {
	result := startRule("KBAO-RESTORE-001")
	outcome := scopeOutcome(snapshot, "openbaorestores")
	o := snapshot.Observation
	if outcome == "not_applicable" && o.RestoreHold == "" && o.RestoreOrigin == "" {
		result.coverage.State = notApplicable
		return result
	}
	if snapshot.Related == nil {
		result.missing("Restore request collection or target-UID correlation is incomplete")
		return result
	}
	if outcome != "collected" {
		result.missing("Restore request collection or target-UID correlation is incomplete")
	}
	matchedHold := o.RestoreHold == ""
	for _, restore := range snapshot.Related.Restores {
		held := o.RestoreHold != "" && (o.RestoreHold == restore.OperationID || o.RestoreHold == restore.UID)
		matchedHold = matchedHold || held
		if restore.Phase == "" && (held || restore.UID == o.RestoreOrigin) {
			result.missing("Restore " + restore.Name + " has no reported phase")
		}
		if held && (restore.Phase == "Unknown" || restore.Phase == "Failed" || restore.ExecutionStage == "Unknown") {
			result.add(Error, "The held restore "+restore.Name+" requires recovery inspection",
				restore.Phase+": "+restore.Message, "Follow the restore recovery procedure; do not clear the hold as a diagnostic action",
				field(restore.Reference, "status.phase"), clusterReference(snapshot, "metadata.annotations[openbao.org/restore-hold]"))
		} else if restore.Phase == "Pending" || restore.Phase == "Validating" || restore.Phase == "Running" {
			result.add(Info, "Restore "+restore.Name+" reports "+restore.Phase,
				restore.Message, "Inspect the restore execution stage and conditions", field(restore.Reference, "status.phase"))
		}
	}
	if !matchedHold {
		result.missing("The restore hold could not be correlated to a captured request UID or operation ID")
	}
	return result
}

func evaluatePodPopulation(snapshot evidence.Snapshot, result *evaluation) {
	counts := map[string]int64{}
	for _, pod := range snapshot.Related.Pods {
		if !pod.Deleting {
			counts[pod.Association.OwnerUID]++
		}
	}
	list := evidence.Reference{APIVersion: "v1", Kind: "PodList", Namespace: snapshot.Target.Namespace}
	for _, step := range snapshot.Collection {
		if step.Resource == "pods" && step.Action == "list" {
			list.ResourceVersion, list.CollectedAt = step.ResourceVersion, step.CollectedAt
		}
	}
	for _, set := range snapshot.Related.StatefulSets {
		if set.Deleting || set.Desired == nil || counts[set.UID] >= *set.Desired {
			continue
		}
		result.add(Warning, "Fewer associated Pods were observed for StatefulSet "+set.Name,
			fmt.Sprintf("Captured %d nonterminating, UID-associated Pods for %d desired replicas", counts[set.UID], *set.Desired),
			"Inspect StatefulSet rollout, Pod creation, and workload ownership metadata", field(set.Reference, "spec.replicas"),
			field(list, "items (controller UID="+set.UID+")"))
	}
}
