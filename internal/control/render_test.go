package control

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kubebao/openbao-operator-client/operations"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	"github.com/kubebao/openbao-operator-cli/internal/render"
)

func TestEveryResultCodeHasPlainWording(t *testing.T) {
	t.Parallel()
	target := evidence.Target{Context: "prod-eu", Namespace: "security", Name: "vault", APIGroup: "openbao.org"}
	accepted := &operations.Result{Outcome: operations.OutcomeAccepted}
	for _, tc := range []struct {
		result     string
		submission *operations.Result
		want       string
	}{
		{
			"dry_run_accepted", &operations.Result{Outcome: operations.OutcomeDryRunAccepted},
			"Dry run: the API server would accept spec.paused = true",
		},
		{"accepted", accepted, "Pause requested for vault"},
		{"observed", accepted, "Pause confirmed for vault"},
		{"observation_blocked", accepted, "but not yet applied"},
		{"wait_incomplete", accepted, "Stopped waiting before the operator confirmed the change"},
		{"assessment_incomplete", accepted, "the result could not be assessed"},
		{"unknown", &operations.Result{Outcome: operations.OutcomeUnknown}, "It was not retried."},
		{"denied", &operations.Result{Outcome: operations.OutcomeDenied}, "Pause denied by the API server"},
		{"conflict", &operations.Result{Outcome: operations.OutcomeConflict}, "the cluster changed after it was read"},
		{"invalid", &operations.Result{Outcome: operations.OutcomeInvalid}, "Rejected by the API server as invalid"},
		{"rate_limited", &operations.Result{Outcome: operations.OutcomeRateLimited}, "Rate limited by the API server"},
		{"invalid_control_evidence", nil, "Not submitted: the cluster evidence is not safe to act on"},
		{"not_found", nil, "OpenBaoCluster vault was not found. Nothing was submitted."},
		{"something_new", accepted, "Result: something new"},
	} {
		var buffer bytes.Buffer
		report := Report{Target: target, ResultCode: tc.result, Submission: tc.submission}
		options := Options{Control: operations.ControlPause, Enabled: true}
		if err := WriteText(render.Plain(&buffer), report, options, TextOptions{}); err != nil {
			t.Fatal(err)
		}
		if got := buffer.String(); !strings.Contains(got, tc.want) ||
			strings.Contains(tc.result, "_") && strings.Contains(got, tc.result) && tc.result != "something_new" {
			t.Fatalf("%s: missing %q or leaked the bare code:\n%s", tc.result, tc.want, got)
		}
	}
}

func TestRestartTextSeparatesMarkerAndRollout(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	report := RestartReport{
		Target: evidence.Target{
			Namespace: "security",
			Name:      "vault",
			Selection: &evidence.Selection{Context: "prod", KubeconfigFiles: []string{"/config"}},
		},
		ResultCode: "wait_incomplete",
		Plan:       &operations.RestartPlan{RestartAt: "2026-11-14T12:00:00Z"},
		Submission: &operations.Result{Outcome: operations.OutcomeAccepted},
		Assessment: &operations.Assessment{
			State:   operations.AssessmentPending,
			Reasons: []operations.AssessmentReason{operations.ReasonRolloutRevision, operations.ReasonRolloutReadiness},
		},
	}
	if err := WriteRestartText(render.Plain(&buffer), report, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	got := buffer.String()
	for _, want := range []string{
		"Change             spec.runtime.restartAt = 2026-11-14T12:00:00Z",
		"Observation        pending · the rollout is still replacing Pods; replaced Pods are not Ready yet",
		"Marker configured  unknown",
		"kubebao status vault -n security",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
