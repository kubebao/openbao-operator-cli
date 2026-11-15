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

	"github.com/kubebao/openbao-operator-client/operations"

	"github.com/kubebao/openbao-operator-cli/internal/control"
)

type restartFixture struct {
	mu               sync.Mutex
	group            string
	runtime          string
	marker           string
	generation       int
	status           int
	unknown          bool
	paused           bool
	maintenance      bool
	blueGreen        bool
	replacement      bool
	workload         string
	cancelWorkload   context.CancelFunc
	patches          atomic.Int32
	workloadRequests atomic.Int32
}

func (f *restartFixture) serve(t *testing.T) *httptest.Server {
	t.Helper()
	if f.group == "" {
		f.group = "kubebao.org"
	}
	f.generation = 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/" + f.group + "/v1alpha1":
			restartJSON(t, w, map[string]any{
				"apiVersion": "v1", "kind": "APIResourceList", "groupVersion": f.group + "/v1alpha1",
				"resources": []any{map[string]any{"name": "openbaoclusters", "kind": "OpenBaoCluster", "namespaced": true}},
			})
		case "/apis/" + f.group + "/v1alpha1/namespaces/context-namespace/openbaoclusters/prod":
			if r.Method == http.MethodPatch {
				f.patch(t, w, r)
				return
			}
			if r.Method != http.MethodGet {
				t.Errorf("unexpected target method: %s", r.Method)
			}
			restartJSON(t, w, f.cluster())
		case "/apis/apps/v1/namespaces/context-namespace/statefulsets", "/api/v1/namespaces/context-namespace/pods":
			if r.Method != http.MethodGet || r.URL.Query().Get("labelSelector") != "openbao.org/cluster=prod" {
				t.Errorf("unexpected restart workload scope: %s %s", r.Method, r.URL)
			}
			f.workloads(t, w, r)
		default:
			t.Errorf("unexpected restart request: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func restartJSON(t *testing.T, w io.Writer, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("write synthetic restart fixture: %v", err)
	}
}

func (f *restartFixture) state() (string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.marker, f.generation
}

func (f *restartFixture) cluster() map[string]any {
	uid := "cluster"
	if f.replacement && f.generation > 1 {
		uid = "replacement"
	}
	spec := map[string]any{
		"replicas": 1, "version": "2.7.2", "paused": f.paused,
		"upgrade":     map[string]any{"strategy": "RollingUpdate"},
		"maintenance": map[string]any{"enabled": f.maintenance},
	}
	if f.marker != "" || f.runtime == "object" {
		spec["runtime"] = map[string]any{"restartAt": f.marker, "unprojected": "PRIVATE_RESTART_CANARY"}
	} else if f.runtime == "null" {
		spec["runtime"] = nil
	}
	status := map[string]any{
		"observedGeneration":      f.generation,
		"readyReplicas":           1,
		"currentVersion":          "2.7.2",
		"acceptedUpgradeStrategy": "RollingUpdate",
		"conditions": []any{
			map[string]any{"type": "Available", "status": "True", "reason": "Ready", "observedGeneration": f.generation},
		},
	}
	if f.blueGreen {
		spec["upgrade"] = map[string]any{"strategy": "BlueGreen"}
		status["acceptedUpgradeStrategy"] = "BlueGreen"
		status["blueGreen"] = map[string]any{"phase": "Idle"}
	}
	return map[string]any{
		"apiVersion": f.group + "/v1alpha1", "kind": "OpenBaoCluster",
		"metadata": map[string]any{
			"name": "prod", "namespace": "context-namespace", "uid": uid,
			"resourceVersion": strconv.Itoa(f.generation * 10), "generation": f.generation,
		},
		"spec": spec, "status": status,
	}
}

func (f *restartFixture) patch(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	f.patches.Add(1)
	if r.Header.Get("Content-Type") != "application/json-patch+json" {
		t.Error("restart did not submit JSON Patch")
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("read restart patch: %v", err)
		return
	}
	var patch []struct {
		Operation string          `json:"op"`
		Path      string          `json:"path"`
		Value     json.RawMessage `json:"value"`
	}
	if json.Unmarshal(raw, &patch) != nil || len(patch) != 4 {
		t.Errorf("invalid restart patch: %s", raw)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	for i, path := range []string{"/metadata/uid", "/metadata/resourceVersion", "/metadata/generation"} {
		if patch[i].Operation != "test" || patch[i].Path != path {
			t.Errorf("missing restart guard: %s", raw)
		}
	}
	if string(patch[0].Value) != `"cluster"` || string(patch[1].Value) != fmt.Sprintf("%q", strconv.Itoa(f.generation*10)) ||
		string(patch[2].Value) != strconv.Itoa(f.generation) {
		t.Errorf("restart was not bound to its initial read: %s", raw)
	}
	marker := ""
	if patch[3].Operation != "add" {
		t.Errorf("unexpected restart operation: %s", raw)
	}
	if f.runtime == "object" || f.marker != "" {
		if patch[3].Path != "/spec/runtime/restartAt" || json.Unmarshal(patch[3].Value, &marker) != nil {
			t.Errorf("restart changed an unrelated field: %s", raw)
		}
	} else {
		var runtime map[string]string
		if patch[3].Path != "/spec/runtime" || json.Unmarshal(patch[3].Value, &runtime) != nil || len(runtime) != 1 {
			t.Errorf("restart did not preserve its parent shape: %s", raw)
		}
		marker = runtime["restartAt"]
	}
	if _, err := time.Parse(time.RFC3339Nano, marker); err != nil || marker == f.marker {
		t.Errorf("restart marker is not a new RFC3339 timestamp: %q", marker)
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		_, _ = io.WriteString(w, `{"message":"PRIVATE_RESTART_CANARY"}`)
		return
	}
	previous, generation := f.marker, f.generation
	f.marker, f.generation = marker, f.generation+1
	if f.unknown {
		connection, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("drop restart response: %v", err)
			return
		}
		_ = connection.Close()
		return
	}
	restartJSON(t, w, f.cluster())
	if r.URL.Query().Get("dryRun") == "All" {
		f.marker, f.generation = previous, generation
	}
}

func (f *restartFixture) workloads(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	f.workloadRequests.Add(1)
	if f.cancelWorkload != nil {
		f.cancelWorkload()
	}
	if f.workload == "denied" || f.workload == "partial" && r.URL.Query().Get("continue") != "" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	metadata := map[string]any{"resourceVersion": "20"}
	if strings.Contains(r.URL.Path, "statefulsets") {
		if f.workload == "partial" {
			metadata["continue"] = "tail"
		}
		restartJSON(t, w, map[string]any{
			"apiVersion": "apps/v1", "kind": "StatefulSetList", "metadata": metadata,
			"items": []any{f.statefulSet()},
		})
		return
	}
	restartJSON(t, w, map[string]any{"apiVersion": "v1", "kind": "PodList", "metadata": metadata, "items": []any{f.pod()}})
}

func (f *restartFixture) statefulSet() map[string]any {
	strategy := "RollingUpdate"
	if f.workload == "on-delete" {
		strategy = "OnDelete"
	}
	return map[string]any{
		"apiVersion": "apps/v1", "kind": "StatefulSet",
		"metadata": map[string]any{
			"name": "prod", "namespace": "context-namespace", "uid": "set", "generation": 1, "resourceVersion": "20",
			"ownerReferences": []any{map[string]any{
				"apiVersion": f.group + "/v1alpha1", "kind": "OpenBaoCluster", "name": "prod", "uid": "cluster", "controller": true,
			}},
		},
		"spec": map[string]any{
			"replicas": 1, "selector": map[string]any{"matchLabels": map[string]any{"openbao.org/workload-pool": "voter"}},
			"template": map[string]any{"metadata": map[string]any{
				"labels":      map[string]any{"openbao.org/workload-pool": "voter"},
				"annotations": map[string]any{"openbao.org/restart-at": f.marker},
			}},
			"updateStrategy": map[string]any{"type": strategy, "rollingUpdate": map[string]any{"partition": 0}},
		},
		"status": map[string]any{
			"observedGeneration": 1, "replicas": 1, "readyReplicas": 1, "currentReplicas": 1, "updatedReplicas": 1,
			"currentRevision": "revision-new", "updateRevision": "revision-new",
		},
	}
}

func (f *restartFixture) pod() map[string]any {
	marker, revision, ready := f.marker, "revision-new", "True"
	if f.workload == "wrong-marker" {
		marker = "previous-marker"
	}
	if f.workload == "wrong-revision" {
		revision = "revision-old"
	}
	if f.workload == "not-ready" {
		ready = "False"
	}
	return map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{
			"name": "prod-0", "namespace": "context-namespace", "uid": "pod", "generation": 1, "resourceVersion": "20",
			"labels":      map[string]any{"openbao.org/workload-pool": "voter", "controller-revision-hash": revision},
			"annotations": map[string]any{"openbao.org/restart-at": marker},
			"ownerReferences": []any{map[string]any{
				"apiVersion": "apps/v1", "kind": "StatefulSet", "name": "prod", "uid": "set", "controller": true,
			}},
		},
		"status": map[string]any{
			"phase": "Running", "conditions": []any{map[string]any{"type": "Ready", "status": ready, "observedGeneration": 1}},
		},
	}
}

func executeRestart(t *testing.T, fixture *restartFixture, args ...string) (int, control.RestartReport) {
	t.Helper()
	server := fixture.serve(t)
	args = append(args, "--kubeconfig", statusConfig(t, server.URL), "--api-group", fixture.group, "--request-timeout", "5s", "-o", "json")
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), args, &stdout, &stderr)
	var report control.RestartReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid restart report: %v; stdout=%s stderr=%s", err, &stdout, &stderr)
	}
	if strings.Contains(stdout.String()+stderr.String(), "PRIVATE_RESTART_CANARY") {
		t.Fatal("restart leaked provider or unapproved field text")
	}
	return code, report
}

func TestRestartSubmissionIsGuardedAndScoped(t *testing.T) {
	t.Parallel()
	for _, group := range []string{"kubebao.org", "openbao.org"} {
		for _, runtime := range []string{"absent", "null", "object"} {
			t.Run(group+"/"+runtime, func(t *testing.T) {
				f := &restartFixture{group: group, runtime: runtime}
				if runtime == "object" {
					f.marker = "earlier"
				}
				code, report := executeRestart(t, f, "restart", "prod")
				if code != 0 || report.Submission == nil || report.Submission.Outcome != operations.OutcomeAccepted ||
					report.Assessment != nil || report.Target.APIGroup != group || report.Target.UID != "cluster" ||
					f.patches.Load() != 1 || f.workloadRequests.Load() != 0 || report.Plan == nil {
					t.Fatalf("restart submission: code=%d report=%+v", code, report)
				}
			})
		}
	}
}

func TestRestartDryRunAndRejectionNeverWaitOrRetry(t *testing.T) {
	t.Parallel()
	f := &restartFixture{}
	code, report := executeRestart(t, f, "restart", "prod", "--dry-run")
	marker, generation := f.state()
	if code != 0 || report.Submission == nil || report.Submission.Outcome != operations.OutcomeDryRunAccepted || marker != "" ||
		generation != 1 || f.workloadRequests.Load() != 0 {
		t.Fatal("restart dry-run persisted or collected rollout evidence")
	}
	for _, status := range []int{http.StatusForbidden, http.StatusConflict, http.StatusUnprocessableEntity} {
		f := &restartFixture{status: status}
		code, report := executeRestart(t, f, "restart", "prod", "--wait")
		if code != 1 || report.Submission == nil || report.Assessment != nil || f.patches.Load() != 1 || f.workloadRequests.Load() != 0 {
			t.Fatalf("restart rejection retried or waited: code=%d report=%+v", code, report)
		}
	}
}

func TestRestartWaitRetainsUnknownSubmission(t *testing.T) {
	t.Parallel()
	for _, unknown := range []bool{false, true} {
		f := &restartFixture{unknown: unknown}
		code, report := executeRestart(t, f, "restart", "prod", "--wait")
		want := operations.OutcomeAccepted
		if unknown {
			want = operations.OutcomeUnknown
		}
		marker, _ := f.state()
		if code != 0 || report.ResultCode != "observed" || report.Submission == nil || report.Submission.Outcome != want ||
			report.Assessment == nil || report.Assessment.State != operations.AssessmentObserved || f.patches.Load() != 1 ||
			marker != report.Plan.RestartAt || f.workloadRequests.Load() != 2 {
			t.Fatalf("restart wait: code=%d report=%+v", code, report)
		}
	}
	f := &restartFixture{unknown: true}
	code, report := executeRestart(t, f, "restart", "prod")
	if code != 3 || report.ResultCode != "unknown" || report.Assessment != nil || f.patches.Load() != 1 {
		t.Fatal("unknown restart without wait was treated as confirmed or retried")
	}
}

func TestRestartWaitCannotConfirmIncompleteRollouts(t *testing.T) {
	t.Parallel()
	for _, workload := range []string{"denied", "partial", "wrong-marker", "wrong-revision", "not-ready", "on-delete"} {
		t.Run(workload, func(t *testing.T) {
			f := &restartFixture{workload: workload}
			started := time.Now()
			code, report := executeRestart(t, f, "restart", "prod", "--wait", "--wait-timeout", "80ms", "--poll-interval", "100ms")
			if code != 3 || report.Assessment == nil || report.Assessment.State == operations.AssessmentObserved ||
				report.Submission.Outcome != operations.OutcomeAccepted || f.patches.Load() != 1 || time.Since(started) > 3*time.Second {
				t.Fatalf("incomplete rollout became observed: code=%d report=%+v", code, report)
			}
		})
	}
}

func TestRestartRejectsKnownBlockedClustersBeforeSubmission(t *testing.T) {
	t.Parallel()
	for _, f := range []*restartFixture{{paused: true}, {blueGreen: true}} {
		code, report := executeRestart(t, f, "restart", "prod", "--wait")
		if code != 3 || report.Submission != nil || f.patches.Load() != 0 || f.workloadRequests.Load() != 0 {
			t.Fatalf("known blocked target received restart: code=%d report=%+v", code, report)
		}
	}
}

func TestRestartPreservesMaintenanceWithoutTreatingItAsPause(t *testing.T) {
	t.Parallel()
	f := &restartFixture{maintenance: true}
	code, report := executeRestart(t, f, "restart", "prod", "--wait")
	if code != 0 || report.Assessment == nil || report.Assessment.State != operations.AssessmentObserved ||
		!f.maintenance || f.patches.Load() != 1 {
		t.Fatalf("maintenance changed or blocked an otherwise supported restart: code=%d report=%+v", code, report)
	}
}

func TestRestartWaitStopsOnTargetReplacement(t *testing.T) {
	t.Parallel()
	f := &restartFixture{replacement: true}
	code, report := executeRestart(t, f, "restart", "prod", "--wait")
	if code != 3 || report.ResultCode != "resource_replaced" || report.Submission.Outcome != operations.OutcomeUnknown ||
		f.patches.Load() != 1 || f.workloadRequests.Load() != 0 {
		t.Fatalf("restart switched to a replacement: code=%d report=%+v", code, report)
	}
}

func TestRestartCancellationCannotSubmitOrRetry(t *testing.T) {
	t.Parallel()
	for _, duringWait := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f := &restartFixture{}
		if duringWait {
			f.cancelWorkload = cancel
		} else {
			cancel()
		}
		server := f.serve(t)
		var stdout, stderr bytes.Buffer
		code := Execute(ctx, []string{
			"restart", "prod", "--wait", "--kubeconfig", statusConfig(t, server.URL),
			"--api-group", f.group, "-o", "json",
		}, &stdout, &stderr)
		wantPatches := int32(0)
		if duringWait {
			wantPatches = 1
		}
		if code != 3 || f.patches.Load() != wantPatches {
			t.Fatalf("restart cancellation submitted or retried: code=%d patches=%d", code, f.patches.Load())
		}
	}
}

func TestRestartFlagsFailBeforeCredentialLoading(t *testing.T) {
	t.Parallel()
	for _, extra := range [][]string{{"--dry-run", "--wait"}, {"--poll-interval", "1ms"}, {"--wait-timeout", "0s"}, {"-o", "yaml"}} {
		args := append([]string{"restart", "prod", "--kubeconfig", "/does/not/exist"}, extra...)
		var stdout, stderr bytes.Buffer
		if code := Execute(context.Background(), args, &stdout, &stderr); code != 1 || stdout.Len() != 0 ||
			strings.Contains(stderr.String(), "cannot load kubeconfig") {
			t.Fatalf("invalid restart flags loaded credentials: code=%d stderr=%s", code, &stderr)
		}
	}
}

func TestRestartTextAndOutputFailurePreserveSubmission(t *testing.T) {
	t.Parallel()
	f := &restartFixture{}
	server := f.serve(t)
	args := []string{"restart", "prod", "--dry-run", "--kubeconfig", statusConfig(t, server.URL), "--api-group", f.group}
	var stdout, stderr bytes.Buffer
	if code := Execute(context.Background(), args, &stdout, &stderr); code != 0 ||
		!strings.Contains(stdout.String(), "Dry run: the API server would accept spec.runtime.restartAt = ") ||
		!strings.Contains(stdout.String(), "Submission  accepted in dry run; not persisted") ||
		strings.Contains(stdout.String(), "Observation") || strings.Contains(stdout.String(), "\x1b[") {
		t.Fatalf("restart text: stdout=%s stderr=%s", &stdout, &stderr)
	}
	if code := Execute(context.Background(), args, failingWriter{}, &stderr); code != 1 || f.patches.Load() != 2 {
		t.Fatal("restart output failure retried or changed process result")
	}
}

func TestRestartTextSeparatesConfiguredMarkerAndWorkloadConvergence(t *testing.T) {
	t.Parallel()
	for _, workload := range []string{"", "not-ready"} {
		f := &restartFixture{workload: workload}
		server := f.serve(t)
		var stdout, stderr bytes.Buffer
		code := Execute(context.Background(), []string{
			"restart", "prod", "--wait", "--wait-timeout", "80ms", "--poll-interval", "100ms",
			"--kubeconfig", statusConfig(t, server.URL), "--api-group", f.group,
		}, &stdout, &stderr)
		wantCode, convergence := 0, "yes"
		if workload != "" {
			wantCode, convergence = 3, "no"
		}
		if code != wantCode || !strings.Contains(stdout.String(), "Submission         accepted by the API server") ||
			!strings.Contains(stdout.String(), "Marker configured  yes") ||
			!strings.Contains(stdout.String(), "Rollout converged  "+convergence) {
			t.Fatalf("restart text merged distinct results: code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
		}
	}
}
