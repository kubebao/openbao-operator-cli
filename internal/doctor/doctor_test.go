package doctor

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

func ptr[T any](value T) *T { return &value }

func healthySnapshot() evidence.Snapshot {
	captured := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	ref := evidence.Reference{
		APIVersion:      "apps/v1",
		Kind:            "StatefulSet",
		Namespace:       "security",
		Name:            "prod",
		UID:             "set",
		ResourceVersion: "20",
		CollectedAt:     captured,
	}
	podRef := evidence.Reference{
		APIVersion:      "v1",
		Kind:            "Pod",
		Namespace:       "security",
		Name:            "prod-0",
		UID:             "pod",
		ResourceVersion: "21",
		CollectedAt:     captured,
	}
	return evidence.Snapshot{
		SchemaVersion: "v1",
		ResultCode:    "completed",
		StartedAt:     captured,
		CompletedAt:   captured.Add(time.Second),
		Target: evidence.Target{
			Context:    "fixture",
			Namespace:  "security",
			Name:       "prod",
			UID:        "cluster",
			APIGroup:   "openbao.org",
			APIVersion: "openbao.org/v1alpha1",
		},
		Observation: &evidence.Observation{
			CollectedAt:        captured,
			ResourceVersion:    "10",
			Generation:         ptr(int64(1)),
			ObservedGeneration: ptr(int64(1)),
			GenerationCurrent:  ptr(true),
			Voters:             evidence.Replicas{Desired: ptr(int64(1)), Ready: ptr(int64(1))},
			Available: &evidence.Condition{
				Type:               "Available",
				Status:             "True",
				ObservedGeneration: ptr(int64(1)),
				GenerationCurrent:  ptr(true),
				Field:              "status.conditions[type=Available]",
			},
		},
		Collection: []evidence.Step{
			{Resource: "statefulsets", Action: "list", Outcome: "collected"},
			{Resource: "pods", Action: "list", Outcome: "collected"},
			{Resource: "openbaorestores", Action: "list", Outcome: "collected"},
		},
		Related: &evidence.Related{
			StatefulSets: []evidence.StatefulSet{
				{
					Reference:          ref,
					Generation:         ptr(int64(1)),
					ObservedGeneration: ptr(int64(1)),
					Desired:            ptr(int64(1)),
					Ready:              ptr(int64(1)),
				},
			},
			Pods: []evidence.Pod{
				{Reference: podRef, Phase: "Running", Ready: ptr(true), Association: evidence.Association{OwnerUID: "set"}},
			},
			Restores:  []evidence.Restore{},
			Omissions: []evidence.Omission{},
		},
	}
}

func TestFindingThresholdAndIncompletePrecedence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		threshold  Severity
		incomplete bool
		want       int
	}{
		{"warning threshold", Warning, false, 2},
		{"error threshold", Error, false, 0},
		{"disabled finding failure", None, false, 0},
		{"partial evidence", Warning, true, 3},
		{"partial evidence with disabled threshold", None, true, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			snapshot := healthySnapshot()
			snapshot.Related.Pods[0].Ready = ptr(false)
			snapshot.Related.Pods[0].ReadyReason = "ContainersNotReady"
			if tc.incomplete {
				snapshot.Collection[1].Outcome = "failed"
			}
			report := Analyze(snapshot, tc.threshold)
			if report.ExitCode != tc.want || len(report.Findings) != 1 {
				t.Fatalf("unexpected diagnosis: %+v", report)
			}
			if report.Findings[0].RuleID != "KBAO-POD-001" || report.Findings[0].Evidence[0].UID != "pod" {
				t.Fatal("finding lacks stable rule and resource identity")
			}
			if tc.incomplete && len(report.Findings[0].MissingEvidence) == 0 {
				t.Fatal("finding lost its evidence limitation")
			}
		})
	}
}

func TestStaleEvidenceCannotProduceCompleteDiagnosis(t *testing.T) {
	t.Parallel()
	snapshot := healthySnapshot()
	snapshot.Observation.Available.GenerationCurrent = ptr(false)
	snapshot.Observation.Available.Status = "False"
	report := Analyze(snapshot, Warning)
	if report.ExitCode != 3 || len(report.Findings) != 1 || len(report.Findings[0].MissingEvidence) == 0 {
		t.Fatalf("stale availability was treated as current: %+v", report)
	}
}

func TestIntentionalControlsAreInformational(t *testing.T) {
	t.Parallel()
	snapshot := healthySnapshot()
	snapshot.Observation.Paused, snapshot.Observation.MaintenanceEnabled = ptr(true), ptr(true)
	report := Analyze(snapshot, Warning)
	if report.ExitCode != 0 || len(report.Findings) != 2 {
		t.Fatalf("intentional restrictions became failures: %+v", report)
	}
	for _, finding := range report.Findings {
		if finding.Severity != Info {
			t.Fatal("control severity is not informational")
		}
	}
}

func TestHeldRestoreAndHistoricalOutcome(t *testing.T) {
	t.Parallel()
	snapshot := healthySnapshot()
	snapshot.Related.Restores = []evidence.Restore{{
		Reference: evidence.Reference{UID: "restore", Name: "recovery"},
		Phase:     "Unknown", OperationID: "operation", ExecutionTargetUID: "cluster",
	}}
	if report := Analyze(snapshot, Warning); report.ExitCode != 0 {
		t.Fatalf("historical outcome became a current fault: %+v", report)
	}
	snapshot.Observation.RestoreHold = "operation"
	report := Analyze(snapshot, Warning)
	if report.ExitCode != 2 || report.Findings[0].Severity != Error || report.Findings[0].RuleID != "KBAO-RESTORE-001" {
		t.Fatalf("held uncertain restore was missed: %+v", report)
	}
	snapshot.Observation.RestoreHold = "unmatched-operation"
	if report := Analyze(snapshot, Warning); report.ExitCode != 3 {
		t.Fatalf("uncorrelated hold appeared complete: %+v", report)
	}
}

func TestSerializedEvidenceProducesSameAnalysis(t *testing.T) {
	t.Parallel()
	snapshot := healthySnapshot()
	snapshot.Related.Pods[0].Ready = nil
	before, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	live := Analyze(snapshot, Warning)
	var captured evidence.Snapshot
	if err := json.Unmarshal(before, &captured); err != nil {
		t.Fatal(err)
	}
	stored := Analyze(captured, Warning)
	if !reflect.DeepEqual(live, stored) {
		t.Fatal("live and serialized evidence produced different diagnoses")
	}
	after, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !live.AnalysisTime.Equal(snapshot.CompletedAt) {
		t.Fatal("analysis mutated evidence or replaced capture time")
	}
}

func TestMissingSnapshotAndInvalidThreshold(t *testing.T) {
	t.Parallel()
	if report := Analyze(evidence.Snapshot{}, Warning); report.ExitCode != 3 {
		t.Fatal("empty evidence appeared complete")
	}
	if report := Analyze(healthySnapshot(), Severity("invalid")); report.ExitCode != 1 {
		t.Fatal("invalid threshold accepted")
	}
}

func TestMissingPodPopulationIsReported(t *testing.T) {
	t.Parallel()
	snapshot := healthySnapshot()
	snapshot.Related.StatefulSets[0].Desired, snapshot.Related.StatefulSets[0].Ready = ptr(int64(3)), ptr(int64(3))
	report := Analyze(snapshot, Warning)
	if report.ExitCode != 2 || len(report.Findings) != 1 || report.Findings[0].RuleID != "KBAO-POD-001" {
		t.Fatalf("missing associated Pod population was not reported: %+v", report)
	}
}
