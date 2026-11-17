package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/control"
	"github.com/kubebao/openbao-operator-client/operations"
)

type controlFixture struct {
	mu                                             sync.Mutex
	group                                          string
	paused, maintenance                            bool
	generation                                     int
	status                                         int
	unknown, stalled, replacement, deniedWorkloads bool
	patches                                        atomic.Int32
	onGet                                          func()
	onPatch                                        func()
}

func (f *controlFixture) state() (bool, bool, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.paused, f.maintenance, f.generation
}

func (f *controlFixture) serve(t *testing.T) *httptest.Server {
	t.Helper()
	if f.group == "" {
		f.group = "kubebao.org"
	}
	f.generation = 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/apis/" + f.group + "/v1alpha1":
			_, _ = fmt.Fprintf(
				w,
				`{"apiVersion":"v1",
"kind":"APIResourceList",
"groupVersion":%q,
"resources":[{"name":"openbaoclusters",
"kind":"OpenBaoCluster",
"namespaced":true}]}`,

				f.group+"/v1alpha1",
			)
		case "/apis/" + f.group + "/v1alpha1/namespaces/context-namespace/openbaoclusters/prod":
			if r.Method == http.MethodPatch {
				f.patch(t, w, r)
				return
			}
			if r.Method != http.MethodGet {
				t.Errorf("unexpected method: %s", r.Method)
			}
			if f.onGet != nil {
				f.onGet()
			}
			f.write(w, f.paused, f.maintenance, f.generation)
		case "/apis/apps/v1/namespaces/context-namespace/statefulsets", "/api/v1/namespaces/context-namespace/pods":
			if r.Method != http.MethodGet || r.URL.Query().Get("labelSelector") != "openbao.org/cluster=prod" {
				t.Errorf("unexpected workload scope: %s %s", r.Method, r.URL)
			}
			f.workloads(w, r)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func (f *controlFixture) patch(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	f.patches.Add(1)
	if f.onPatch != nil {
		f.onPatch()
	}
	if r.Header.Get("Content-Type") != "application/json-patch+json" {
		t.Error("control is not JSON Patch")
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Error(err)
		return
	}
	var patch []struct {
		Operation string          `json:"op"`
		Path      string          `json:"path"`
		Value     json.RawMessage `json:"value"`
	}
	if json.Unmarshal(raw, &patch) != nil || len(patch) != 4 {
		t.Errorf("invalid patch: %s", raw)
		return
	}
	for i, path := range []string{"/metadata/uid", "/metadata/resourceVersion", "/metadata/generation"} {
		if patch[i].Operation != "test" || patch[i].Path != path {
			t.Errorf("missing guard: %s", raw)
		}
	}
	if string(patch[0].Value) != `"cluster"` || string(patch[1].Value) != `"10"` || string(patch[2].Value) != "1" {
		t.Errorf("patch not bound to initial read: %s", raw)
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		_, _ = fmt.Fprint(w, `{"message":"PRIVATE_CONTROL_CANARY"}`)
		return
	}
	paused, maintenance := f.paused, f.maintenance
	switch patch[3].Path {
	case "/spec/paused":
		if json.Unmarshal(patch[3].Value, &paused) != nil {
			t.Error("invalid pause value")
		}
	case "/spec/maintenance/enabled":
		if json.Unmarshal(patch[3].Value, &maintenance) != nil {
			t.Error("invalid maintenance value")
		}
	default:
		t.Errorf("changed unrelated field: %s", raw)
	}
	if r.URL.Query().Get("dryRun") != "All" {
		f.paused, f.maintenance, f.generation = paused, maintenance, 2
	}
	if f.unknown {
		connection, _, hijackErr := http.NewResponseController(w).Hijack()
		if hijackErr != nil {
			t.Error(hijackErr)
			return
		}
		_ = connection.Close()
		return
	}
	f.write(w, paused, maintenance, 2)
}

func (f *controlFixture) write(w io.Writer, paused, maintenance bool, generation int) {
	uid, status, reason := "cluster", "True", "Ready"
	if paused {
		status, reason = "Unknown", "Paused"
	}
	observed := generation
	if f.stalled && generation > 1 {
		observed = 1
	}
	if f.replacement && generation > 1 {
		uid = "replacement"
	}
	_, _ = fmt.Fprintf(
		w,
		`{"apiVersion":%q,
"kind":"OpenBaoCluster",
"metadata":{"name":"prod",
"namespace":"context-namespace",
"uid":%q,
"resourceVersion":%q,
"generation":%d},
"spec":{"replicas":1,
"paused":%t,
"maintenance":{"enabled":%t}},
"status":{"observedGeneration":%d,
"readyReplicas":1,
"conditions":[{"type":"Available",
"status":%q,
"reason":%q,
"observedGeneration":%d}]}}`,

		f.group+"/v1alpha1",
		uid,
		strconv.Itoa(generation*10),
		generation,
		paused,
		maintenance,
		observed,
		status,
		reason,
		observed,
	)
}

func (f *controlFixture) workloads(w http.ResponseWriter, r *http.Request) {
	if f.deniedWorkloads {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	annotation := "{}"
	if f.maintenance {
		annotation = `{"openbao.org/maintenance":"true"}`
	}
	if strings.Contains(r.URL.Path, "statefulsets") {
		_, _ = fmt.Fprintf(
			w,
			`{"apiVersion":"apps/v1",
"kind":"StatefulSetList",
"metadata":{"resourceVersion":"20"},
"items":[{"apiVersion":"apps/v1",
"kind":"StatefulSet",
"metadata":{"namespace":"context-namespace",
"name":"prod",
"uid":"set",
"generation":1,
"resourceVersion":"20",
"annotations":%s,
"ownerReferences":[{"apiVersion":%q,
"kind":"OpenBaoCluster",
"name":"prod",
"uid":"cluster",
"controller":true}]},
"spec":{"replicas":1,
"selector":{"matchLabels":{"openbao.org/workload-pool":"voter"}},
"template":{"metadata":{"labels":{"openbao.org/workload-pool":"voter"}}}},
"status":{"observedGeneration":1}}]}`,

			annotation,
			f.group+"/v1alpha1",
		)
		return
	}
	_, _ = fmt.Fprintf(
		w,
		`{"apiVersion":"v1",
"kind":"PodList",
"metadata":{"resourceVersion":"20"},
"items":[{"apiVersion":"v1",
"kind":"Pod",
"metadata":{"namespace":"context-namespace",
"name":"prod-0",
"uid":"pod",
"generation":1,
"resourceVersion":"20",
"labels":{"openbao.org/workload-pool":"voter"},
"annotations":%s,
"ownerReferences":[{"apiVersion":"apps/v1",
"kind":"StatefulSet",
"name":"prod",
"uid":"set",
"controller":true}]}}]}`,

		annotation,
	)
}

func executeControl(t *testing.T, fixture *controlFixture, args ...string) (int, control.Report) {
	t.Helper()
	server := fixture.serve(t)
	args = append(args, "--kubeconfig", statusConfig(t, server.URL), "--api-group", fixture.group, "--request-timeout", "5s", "-o", "json")
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), args, &stdout, &stderr)
	var report control.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid control report: %v; stdout=%s stderr=%s", err, &stdout, &stderr)
	}
	if strings.Contains(stdout.String()+stderr.String(), "PRIVATE_CONTROL_CANARY") {
		t.Fatal("provider message leaked")
	}
	return code, report
}

func TestControlsSubmitOnlyTheSelectedFieldAndGroup(t *testing.T) {
	t.Parallel()
	for _, group := range []string{"kubebao.org", "openbao.org"} {
		commands := [][]string{
			{"pause", "prod"},
			{"resume", "prod"},
			{"maintenance", "enable", "prod"},
			{"maintenance", "disable", "prod"},
		}
		for _, args := range commands {
			fixture := &controlFixture{group: group, paused: args[0] == "resume"}
			code, report := executeControl(t, fixture, args...)
			if code != 0 || report.Submission == nil || report.Submission.Outcome != operations.OutcomeAccepted ||
				report.Assessment != nil ||
				fixture.patches.Load() != 1 {
				t.Fatalf("control: code=%d report=%+v", code, report)
			}
			if report.Target.APIGroup != group || report.Target.UID != "cluster" {
				t.Fatal("target identity lost")
			}
		}
	}
}

func TestControlsDryRunAndRejectionDoNotWaitOrRetry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status int
		want   operations.Outcome
	}{
		{403, operations.OutcomeDenied}, {409, operations.OutcomeConflict}, {422, operations.OutcomeInvalid},
	} {
		fixture := &controlFixture{status: tc.status}
		code, report := executeControl(t, fixture, "pause", "prod", "--wait")
		if code != 1 || report.Submission == nil || report.Submission.Outcome != tc.want || report.Assessment != nil ||
			fixture.patches.Load() != 1 {
			t.Fatalf("rejection: code=%d report=%+v", code, report)
		}
	}
	fixture := &controlFixture{}
	code, report := executeControl(t, fixture, "pause", "prod", "--dry-run")
	paused, _, generation := fixture.state()
	if code != 0 || report.Submission.Outcome != operations.OutcomeDryRunAccepted || paused || generation != 1 {
		t.Fatal("dry-run persisted or lost hypothetical classification")
	}
}

func TestControlWaitSeparatesUnknownAcceptanceAndObservation(t *testing.T) {
	t.Parallel()
	fixture := &controlFixture{unknown: true}
	code, report := executeControl(t, fixture, "pause", "prod", "--wait")
	if code != 0 || report.ResultCode != "observed" || report.Submission.Outcome != operations.OutcomeUnknown ||
		report.Assessment == nil || report.Assessment.State != operations.AssessmentObserved || fixture.patches.Load() != 1 {
		t.Fatalf("unknown submission: code=%d report=%+v", code, report)
	}
}

func TestMaintenanceWaitRequiresWorkloadsAndNeverResumes(t *testing.T) {
	t.Parallel()
	for _, paused := range []bool{false, true} {
		fixture := &controlFixture{paused: paused}
		code, report := executeControl(t, fixture, "maintenance", "enable", "prod", "--wait")
		want := 0
		if paused {
			want = 3
		}
		actualPaused, maintenance, _ := fixture.state()
		if code != want || report.Assessment == nil || actualPaused != paused || !maintenance || fixture.patches.Load() != 1 {
			t.Fatalf("maintenance result: code=%d report=%+v", code, report)
		}
		if paused && (report.Assessment.State != operations.AssessmentBlocked || len(report.Warnings) != 1) {
			t.Fatal("paused propagation was not reported")
		}
	}
}

func TestUnknownSubmissionWithoutWaitRemainsUnknown(t *testing.T) {
	t.Parallel()
	fixture := &controlFixture{unknown: true}
	code, report := executeControl(t, fixture, "pause", "prod")
	if code != 3 || report.ResultCode != "unknown" || report.Assessment != nil || fixture.patches.Load() != 1 {
		t.Fatalf("unknown submission was retried or treated as failure: code=%d report=%+v", code, report)
	}
}

func TestControlCancellationBeforeCollectionCannotSubmit(t *testing.T) {
	t.Parallel()
	fixture := &controlFixture{}
	server := fixture.serve(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	code := Execute(ctx, []string{
		"pause", "prod", "--kubeconfig", statusConfig(t, server.URL),
		"--api-group", fixture.group, "--wait", "-o", "json",
	}, &stdout, &stderr)
	var report control.Report
	if json.Unmarshal(stdout.Bytes(), &report) != nil || code != 3 || report.Submission != nil || fixture.patches.Load() != 0 {
		t.Fatal("cancellation allowed a mutation")
	}
}

func TestMaintenanceDeniedWorkloadsCannotEstablishObservation(t *testing.T) {
	t.Parallel()
	fixture := &controlFixture{deniedWorkloads: true}
	code, report := executeControl(t, fixture, "maintenance", "enable", "prod", "--wait", "--wait-timeout", "80ms")
	if code != 3 || report.Assessment == nil || report.Assessment.State != operations.AssessmentInsufficient ||
		report.Submission.Outcome != operations.OutcomeAccepted || fixture.patches.Load() != 1 {
		t.Fatalf("denied workloads became verified propagation: code=%d report=%+v", code, report)
	}
}

func TestControlWaitStopsOnReplacementOrTimeout(t *testing.T) {
	t.Parallel()
	for _, replacement := range []bool{false, true} {
		fixture := &controlFixture{replacement: replacement, stalled: !replacement}
		started := time.Now()
		code, report := executeControl(t, fixture, "pause", "prod", "--wait", "--wait-timeout", "80ms", "--poll-interval", "100ms")
		want := "wait_incomplete"
		if replacement {
			want = "resource_replaced"
		}
		if code != 3 || report.ResultCode != want || fixture.patches.Load() != 1 || time.Since(started) > 3*time.Second {
			t.Fatalf("bounded wait: code=%d report=%+v", code, report)
		}
	}
}

func TestControlFlagsFailBeforeCredentialLoading(t *testing.T) {
	t.Parallel()
	for _, extra := range [][]string{{"--dry-run", "--wait"}, {"--poll-interval", "1ms"}, {"--wait-timeout", "0s"}} {
		args := append([]string{"pause", "prod", "--kubeconfig", "/does/not/exist"}, extra...)
		var stdout, stderr bytes.Buffer
		if code := Execute(
			context.Background(),
			args,
			&stdout,
			&stderr,
		); code != 1 || stdout.Len() != 0 ||
			strings.Contains(stderr.String(), "cannot load kubeconfig") {
			t.Fatalf("invalid flags loaded credentials: code=%d stderr=%s", code, &stderr)
		}
	}
}

func TestControlTextAndOutputFailureKeepSubmissionSeparate(t *testing.T) {
	t.Parallel()
	fixture := &controlFixture{}
	server := fixture.serve(t)
	args := []string{"pause", "prod", "--dry-run", "--kubeconfig", statusConfig(t, server.URL), "--api-group", fixture.group}
	var stdout, stderr bytes.Buffer
	if code := Execute(context.Background(), args, &stdout, &stderr); code != 0 ||
		!strings.Contains(stdout.String(), "Dry run: the API server would accept spec.paused = true") ||
		!strings.Contains(stdout.String(), "Submission  accepted in dry run; not persisted") {
		t.Fatalf("dry-run text: stdout=%s stderr=%s", &stdout, &stderr)
	}
	if code := Execute(context.Background(), args, failingWriter{}, &stderr); code != 1 || fixture.patches.Load() != 2 {
		t.Fatal("output failure changed process result or retried the request")
	}
}
