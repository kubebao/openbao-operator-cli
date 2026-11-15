package doctor

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	"github.com/kubebao/openbao-operator-cli/internal/render"
	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/diagnose"
	shared "github.com/kubebao/openbao-operator-client/evidence"
)

func TestValidationFieldIsReportedWithoutRejectedEvidence(t *testing.T) {
	snapshot := captureFixture(t)
	canary := "PRIVATE_CANARY"
	snapshot.NormalizedEvidence.Clusters[0].Pods.Observations[0].Metadata.Reference.ResourceVersion = shared.Text{
		State: shared.ValueInvalid, Value: &canary,
	}
	report, err := Analyze(snapshot, Policy(snapshot.CompletedAt))
	want := "clusters[0].pods.observations[0].metadata.reference.resourceVersion"
	if err == nil || report.ValidationPath != want || strings.Contains(err.Error(), "CANARY") || report.Evidence.NormalizedEvidence != nil {
		t.Fatal("invalid capture lost its safe path or retained rejected evidence")
	}
	data, marshalErr := json.Marshal(report)
	var text bytes.Buffer
	if marshalErr != nil || WriteText(render.Plain(&text), report, "incomplete", TextOptions{}) != nil ||
		!strings.Contains(text.String(), want) || strings.Contains(string(data), "CANARY") {
		t.Fatal("validation output omitted the field path or exposed rejected values")
	}
}

func TestAllSharedCoverageCausesReachCLIReports(t *testing.T) {
	snapshot := captureFixture(t)
	input := &snapshot.NormalizedEvidence.Clusters[0]
	input.Pods.Collection.Outcome = shared.CollectionPartial
	for index := range input.Pods.Observations[0].Conditions.Values {
		input.Pods.Observations[0].Conditions.Values[index].ObservedGeneration = shared.Integer{State: shared.ValueUnknown}
	}
	report, err := Analyze(snapshot, Policy(snapshot.CompletedAt))
	if err != nil {
		t.Fatal(err)
	}
	for _, coverage := range report.Coverage {
		if coverage.RuleID == string(diagnose.RulePod) {
			if !slices.Contains(coverage.MissingEvidence, string(diagnose.ReasonGeneration)) ||
				!slices.Contains(coverage.MissingEvidence, string(diagnose.ReasonScopeIncomplete)) {
				t.Fatal("shared coverage causes were lost in the CLI mapping")
			}
			return
		}
	}
	t.Fatal("Pod rule coverage was omitted")
}

func captureFixture(t *testing.T) evidence.Snapshot {
	t.Helper()
	at := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	options := adapter.ProjectionOptions{
		Origin:      "fixture",
		CollectedAt: at,
		Contract:    adapter.OpenBaoV1Alpha1,
		Limits:      adapter.DefaultProjectionLimits(),
	}
	cluster, err := adapter.ProjectCluster(
		[]byte(
			`{
  "apiVersion": "openbao.org/v1alpha1",
  "kind": "OpenBaoCluster",
  "metadata": {
    "namespace": "security",
    "name": "prod",
    "uid": "cluster",
    "generation": 1
  },
  "spec": {
    "replicas": 1
  },
  "status": {
    "observedGeneration": 1,
    "workload": {},
    "adminOps": {},
    "conditions": [
      {
        "type": "OpenBaoSealed",
        "status": "False",
        "observedGeneration": 1
      },
      {
        "type": "Available",
        "status": "True",
        "observedGeneration": 1
      }
    ]
  }
}`,
		),
		options,
	)
	if err != nil {
		t.Fatal(err)
	}
	read := func(group, version, resource string, action shared.ReadAction, items int) shared.Collection {
		scope := shared.Scope{Origin: "fixture", Group: group, Version: version, Resource: resource, Namespace: "security", Action: action}
		if action == shared.ReadGet {
			scope.Name = "prod"
		}
		return shared.Collection{
			Scope:    scope,
			Interval: shared.Interval{StartedAt: at, CompletedAt: at},
			Outcome:  shared.CollectionComplete,
			Items:    items,
		}
	}
	root := read("openbao.org", "v1alpha1", "openbaoclusters", shared.ReadGet, 1)
	verification, err := shared.NewTargetVerification(cluster, root, []shared.Cluster{cluster})
	if err != nil {
		t.Fatal(err)
	}
	options.Contract = adapter.KubernetesAppsV1
	set, err := adapter.ProjectStatefulSet(
		[]byte(
			`{
  "apiVersion": "apps/v1",
  "kind": "StatefulSet",
  "metadata": {
    "namespace": "security",
    "name": "prod",
    "uid": "set",
    "generation": 1,
    "ownerReferences": [
      {
        "apiVersion": "openbao.org/v1alpha1",
        "kind": "OpenBaoCluster",
        "name": "prod",
        "uid": "cluster",
        "controller": true
      }
    ]
  },
  "spec": {
    "replicas": 1
  },
  "status": {
    "observedGeneration": 1,
    "readyReplicas": 1
  }
}`,
		),
		options,
	)
	if err != nil {
		t.Fatal(err)
	}
	options.Contract = adapter.KubernetesCoreV1
	pod, err := adapter.ProjectPod(
		[]byte(
			`{
  "apiVersion": "v1",
  "kind": "Pod",
  "metadata": {
    "namespace": "security",
    "name": "prod-0",
    "uid": "pod",
    "generation": 1,
    "ownerReferences": [
      {
        "apiVersion": "apps/v1",
        "kind": "StatefulSet",
        "name": "prod",
        "uid": "set",
        "controller": true
      }
    ]
  },
  "status": {
    "phase": "Running",
    "conditions": [
      {
        "type": "Ready",
        "status": "True",
        "observedGeneration": 1
      }
    ]
  }
}`,
		),
		options,
	)
	if err != nil {
		t.Fatal(err)
	}
	sets := read("apps", "v1", "statefulsets", shared.ReadList, 1)
	sets.Scope.Selector = "openbao.org/cluster=prod"
	pods := read("", "v1", "pods", shared.ReadList, 1)
	pods.Scope.Selector = sets.Scope.Selector
	input := diagnose.ClusterInput{
		Subject:      cluster.Reference.Identity,
		Observation:  &cluster,
		Collection:   root,
		Verification: &verification,
		StatefulSets: &diagnose.ResourceInput[shared.StatefulSet]{
			Collection:   sets,
			Observations: []shared.StatefulSet{set},
		},
		Pods: &diagnose.ResourceInput[shared.Pod]{
			Collection:   pods,
			Observations: []shared.Pod{pod},
		},
		Restores: &diagnose.ResourceInput[shared.Restore]{
			Collection:   read("openbao.org", "v1alpha1", "openbaorestores", shared.ReadList, 0),
			Observations: []shared.Restore{},
		},
	}
	return evidence.Snapshot{
		SchemaVersion:      "v1",
		ResultCode:         "completed",
		StartedAt:          at,
		CompletedAt:        at.Add(time.Second),
		Target:             evidence.Target{Context: "fixture", Namespace: "security", Name: "prod", UID: "cluster"},
		NormalizedEvidence: &diagnose.Capture{Clusters: []diagnose.ClusterInput{input}},
	}
}

func TestSharedAnalysisReplayAndIndependentFindingIdentity(t *testing.T) {
	snapshot := captureFixture(t)
	input := &snapshot.NormalizedEvidence.Clusters[0]
	input.Pods.Observations[0].Conditions.Values[0].Status = shared.ConditionFalse
	second := input.Pods.Observations[0]
	second.Metadata.Reference.Identity.Name = "prod-1"
	second.Metadata.Reference.Identity.UID = "pod-2"
	input.Pods.Observations = append(input.Pods.Observations, second)
	input.Pods.Collection.Items = 2
	policy := Policy(snapshot.CompletedAt)
	report, err := Analyze(snapshot, policy)
	if err != nil || !report.Complete || len(report.Findings) != 2 {
		t.Fatalf("analysis: %+v, %v", report, err)
	}
	if report.Findings[0].ID == report.Findings[1].ID {
		t.Fatal("per-Pod identity collapsed")
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded evidence.Snapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	replay, err := Analyze(decoded, policy)
	if err != nil || !reflect.DeepEqual(report, replay) {
		t.Fatal("serialized evidence changed pure analysis")
	}
	if strings.Contains(string(data), `"exitCode"`) {
		t.Fatal("process result entered evidence")
	}
}

func TestIncompleteCoverageRetainsIndependentFindings(t *testing.T) {
	snapshot := captureFixture(t)
	input := &snapshot.NormalizedEvidence.Clusters[0]
	input.Observation.Conditions.Values[0].Status = shared.ConditionFalse
	input.Pods.Collection.Outcome = shared.CollectionDenied
	input.Pods.Collection.Items = 0
	input.Pods.Observations = []shared.Pod{}
	report, err := Analyze(snapshot, Policy(snapshot.CompletedAt))
	if err != nil || report.Complete || len(report.Findings) != 1 || report.Findings[0].RuleID != string(diagnose.RuleAvailability) {
		t.Fatalf("independent finding lost: %+v, %v", report, err)
	}
}

func TestOnlyNormalizedEvidenceDrivesAnalysis(t *testing.T) {
	snapshot := captureFixture(t)
	snapshot.Observation = &evidence.Observation{Phase: "Failed"}
	report, err := Analyze(snapshot, Policy(snapshot.CompletedAt))
	if err != nil || !report.Complete || len(report.Findings) != 0 {
		t.Fatalf("legacy display drove diagnosis: %+v %v", report, err)
	}
	snapshot.NormalizedEvidence.Clusters[0].Observation.Reference.APIVersion = "unsupported/v1"
	if _, err := Analyze(snapshot, Policy(snapshot.CompletedAt)); err == nil {
		t.Fatal("invalid normalized evidence was accepted")
	}
}

func TestBackupAgeRequiresDeclaredThreshold(t *testing.T) {
	snapshot := captureFixture(t)
	backup := &snapshot.NormalizedEvidence.Clusters[0].Observation.Backup
	zero := int64(0)
	last := snapshot.StartedAt.Add(-4 * time.Hour)
	backup.Configuration, backup.State = shared.ObjectReported, shared.ObjectReported
	backup.ConsecutiveFailures = shared.Integer{State: shared.ValueReported, Value: &zero}
	backup.LastSuccessAt = shared.Timestamp{State: shared.ValueReported, Value: &last}
	for _, tc := range []struct {
		limit time.Duration
		want  bool
	}{{0, false}, {3 * time.Hour, true}, {5 * time.Hour, false}} {
		policy := WithBackupAge(Policy(snapshot.CompletedAt), tc.limit)
		report, err := Analyze(snapshot, policy)
		if err != nil || !report.Complete {
			t.Fatalf("backup policy analysis incomplete: %v", err)
		}
		found := false
		for _, finding := range report.Findings {
			found = found || finding.RuleID == string(diagnose.RuleBackupAge)
		}
		if found != tc.want || slices.Contains(policy.Rules, diagnose.RuleBackupAge) != (tc.limit > 0) {
			t.Fatal("backup age rule did not follow the caller-declared threshold")
		}
	}
}

func TestExpandedPolicyReportsRecordedOperatorFailures(t *testing.T) {
	snapshot := captureFixture(t)
	cluster := snapshot.NormalizedEvidence.Clusters[0].Observation
	reason := "FixtureFailure"
	failure := shared.ControllerError{
		State: shared.ObjectReported, Reason: shared.Text{State: shared.ValueReported, Value: &reason},
		At: shared.Timestamp{State: shared.ValueUnknown},
	}
	cluster.WorkloadError = failure
	cluster.Upgrade.State, cluster.Upgrade.Failure = shared.ObjectReported, failure
	cluster.Backup.State, cluster.Backup.Configuration = shared.ObjectReported, shared.ObjectReported
	failures := int64(2)
	cluster.Backup.ConsecutiveFailures = shared.Integer{State: shared.ValueReported, Value: &failures}
	condition := cluster.Conditions.Values[0]
	condition.Type, condition.Status = "TLSReady", shared.ConditionFalse
	cluster.Conditions.Values = append(cluster.Conditions.Values, condition)
	report, err := Analyze(snapshot, Policy(snapshot.CompletedAt))
	if err != nil || !report.Complete {
		t.Fatalf("expanded policy could not evaluate recorded failures: %v", err)
	}
	for _, rule := range []diagnose.RuleID{
		diagnose.RuleCondition, diagnose.RuleController, diagnose.RuleBackupFailed, diagnose.RuleUpgrade,
	} {
		if !slices.ContainsFunc(report.Findings, func(f Finding) bool { return f.RuleID == string(rule) }) {
			t.Fatalf("recorded failure missing from doctor: %s", rule)
		}
	}
}
