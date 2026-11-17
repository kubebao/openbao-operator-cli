package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/operations"

	"github.com/kubebao/openbao-operator-cli/internal/control"
)

type observationResult struct {
	ReadOnly   bool                   `json:"readOnly"`
	ResultCode string                 `json:"resultCode"`
	Plan       json.RawMessage        `json:"plan"`
	Submission *operations.Result     `json:"submission"`
	Assessment *operations.Assessment `json:"assessment"`
}

func observationCall(t *testing.T, config string, args ...string) (int, observationResult) {
	t.Helper()
	args = append(args, "--kubeconfig", config, "--request-timeout", "2s", "-o", "json")
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), args, &stdout, &stderr)
	var result observationResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("invalid command result: %v; stdout=%s stderr=%s", err, &stdout, &stderr)
	}
	return code, result
}

func privateRequestContents(t *testing.T, file, config, endpoint string) []byte {
	t.Helper()
	//nolint:gosec // The caller creates this request inside t.TempDir.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(config)) || bytes.Contains(data, []byte(endpoint)) {
		t.Fatal("request file contains local connection configuration")
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("request is not private: %v %v", info, err)
	}
	return data
}

func TestObserveControlsReadsOnlyAndRetainsOriginalPlan(t *testing.T) {
	t.Parallel()
	for _, group := range []string{"openbao.org", "kubebao.org"} {
		for _, command := range [][]string{
			{"pause", "prod"},
			{"resume", "prod"},
			{"maintenance", "enable", "prod"},
			{"maintenance", "disable", "prod"},
		} {
			t.Run(group+"/"+strings.Join(command, "-"), func(t *testing.T) {
				f := &controlFixture{group: group, unknown: true}
				server := f.serve(t)
				config, file := statusConfig(t, server.URL), filepath.Join(t.TempDir(), "request.json")
				args := append(append([]string{}, command...), "--api-group", group, "--request-file", file)
				code, original := observationCall(t, config, args...)
				if code != 3 || original.ResultCode != "unknown" {
					t.Fatalf("expected unknown submission: %d %+v", code, original)
				}
				before := privateRequestContents(t, file, config, server.URL)
				code, result := observationCall(t, config, "observe", "--request-file", file)
				if code != 0 || result.ResultCode != "observed" || !result.ReadOnly || result.Submission != nil ||
					result.Assessment == nil || !bytes.Equal(result.Plan, original.Plan) || f.patches.Load() != 1 {
					t.Fatalf("read-only observation: %d %+v patches=%d", code, result, f.patches.Load())
				}
				//nolint:gosec // Read the same test-owned file to verify observation does not rewrite it.
				after, err := os.ReadFile(file)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("observation changed request: %v", err)
				}
			})
		}
	}
}

func TestObserveControlIncompleteAndBlockedEvidenceCannotSubmit(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"replacement", "paused", "denied", "timeout", "pending"} {
		t.Run(scenario, func(t *testing.T) {
			f := &controlFixture{}
			server := f.serve(t)
			config, file := statusConfig(t, server.URL), filepath.Join(t.TempDir(), "request.json")
			_, _ = observationCall(t, config, "maintenance", "enable", "prod", "--api-group", f.group, "--request-file", file)
			f.mu.Lock()
			switch scenario {
			case "replacement":
				f.replacement = true
			case "paused":
				f.paused = true
			case "denied":
				f.deniedWorkloads = true
			default:
				f.stalled = true
			}
			f.mu.Unlock()
			args := []string{"observe", "--request-file", file}
			want := "observation_incomplete"
			switch scenario {
			case "replacement":
				want = "resource_replaced"
			case "paused":
				want = "observation_blocked"
			case "timeout":
				args = append(args, "--wait", "--wait-timeout", "50ms", "--poll-interval", "100ms")
				want = "wait_incomplete"
			}
			code, result := observationCall(t, config, args...)
			if code != 3 || result.ResultCode != want || !result.ReadOnly || result.Submission != nil || f.patches.Load() != 1 {
				t.Fatalf("unsafe or incorrect observation: %d %+v patches=%d", code, result, f.patches.Load())
			}
			paused, _, _ := f.state()
			if scenario == "paused" && !paused {
				t.Fatal("observation resumed reconciliation")
			}
		})
	}
}

func TestObserveRestartUsesSavedMarkerWithoutAnotherPatch(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"observed", "later-marker", "replacement", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			f := &restartFixture{unknown: true}
			server := f.serve(t)
			config, file := statusConfig(t, server.URL), filepath.Join(t.TempDir(), "restart.json")
			_, original := observationCall(t, config, "restart", "prod", "--api-group", f.group, "--request-file", file)
			saved, err := control.ReadRequest(context.Background(), file)
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			if scenario == "later-marker" {
				f.marker = "2099-01-01T00:00:00Z"
			}
			if scenario == "replacement" {
				f.replacement = true
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancelled" {
				f.cancelWorkload = cancel
			}
			f.mu.Unlock()
			var stdout, stderr bytes.Buffer
			code := Execute(ctx, []string{"observe", "--request-file", file, "--kubeconfig", config, "-o", "json"}, &stdout, &stderr)
			var result control.RestartReport
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatalf("decode: %v stdout=%s stderr=%s", err, &stdout, &stderr)
			}
			wantCode := 3
			if scenario == "observed" {
				wantCode = 0
			}
			if code != wantCode || result.Submission != nil || !result.ReadOnly || result.Plan.RestartAt != saved.Restart.RestartAt ||
				f.patches.Load() != 1 || original.Submission.Outcome != operations.OutcomeUnknown {
				t.Fatalf("restart continuation changed request: %d %+v patches=%d", code, result, f.patches.Load())
			}
			if scenario == "replacement" && f.workloadRequests.Load() != 0 {
				t.Fatal("read workloads for replacement UID")
			}
		})
	}
}

func TestObserveRejectsWrongOriginBeforeNetworkReads(t *testing.T) {
	t.Parallel()
	f := &controlFixture{}
	server := f.serve(t)
	config, file := statusConfig(t, server.URL), filepath.Join(t.TempDir(), "request.json")
	_, _ = observationCall(t, config, "pause", "prod", "--api-group", f.group, "--request-file", file)
	var requests atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); http.NotFound(w, nil) }))
	defer other.Close()
	code, report := observationCall(t, statusConfig(t, other.URL), "observe", "--request-file", file)
	if code != 1 || report.ResultCode != "invalid_request_target" || requests.Load() != 0 || f.patches.Load() != 1 {
		t.Fatalf("wrong origin read: %d %+v requests=%d", code, report, requests.Load())
	}
}

func TestSavedRequestValidationAndFileSafetyPrecedeCredentials(t *testing.T) {
	t.Parallel()
	f := &controlFixture{}
	server := f.serve(t)
	file := filepath.Join(t.TempDir(), "request.json")
	_, _ = observationCall(t, statusConfig(t, server.URL), "pause", "prod", "--api-group", f.group, "--request-file", file)
	//nolint:gosec // The fixture writes this request in t.TempDir.
	valid, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{
		[]byte(`{}`), []byte(`{"formatVersion":"kubebao-control-request/v1"}`), []byte(`null`),
		append(valid, valid...), []byte(strings.Repeat("x", 32769)),
		bytes.Replace(valid, []byte(`"formatVersion":`), []byte(`"formatVersion":"other","formatVersion":`), 1),
		bytes.Replace(valid, []byte(`"control":`), []byte(`"unknown":`), 1),
		bytes.Replace(valid, []byte(`"control":`), []byte(`"restart":{},"control":`), 1),
		bytes.Replace(valid, []byte(`"enabled":true`), []byte(`"enabled":null`), 1),
		bytes.Replace(valid, []byte(`"enabled":true`), []byte(`"enabled":false,"enabled":true`), 1),
		bytes.Replace(valid, []byte(`"enabled":true`), []byte(`"enabled":true,"password":"PRIVATE_REQUEST_CANARY"`), 1),
		[]byte(`{"formatVersion":"kubebao-control-request/v1","control":null}`),
	} {
		bad := filepath.Join(t.TempDir(), "bad.json")
		//nolint:gosec // Only malformed JSON bytes vary; the destination is always inside t.TempDir.
		if err := os.WriteFile(bad, data, 0o600); err != nil {
			t.Fatal(err)
		}
		assertRejectedRequest(t, bad)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	assertRejectedRequest(t, link)
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	assertRejectedRequest(t, fifo)
	assertRejectedRequest(t, t.TempDir())
	if f.patches.Load() != 1 {
		t.Fatal("invalid request submitted a mutation")
	}
}

func assertRejectedRequest(t *testing.T, path string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"observe", "--request-file", path, "--kubeconfig", "/missing/config"}, &stdout, &stderr)
	if code != 1 || stdout.Len() != 0 || strings.Contains(stderr.String(), "kubeconfig") ||
		strings.Contains(stderr.String(), "PRIVATE_REQUEST_CANARY") {
		t.Fatalf("invalid request reached credentials or leaked input: %d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
}

func TestRequestOutputAndDryRunCannotSubmit(t *testing.T) {
	t.Parallel()
	f := &controlFixture{}
	server := f.serve(t)
	file := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(file, []byte("preserve existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"pause", "prod", "--request-file", file},
		{"pause", "prod", "--request-file", ""},
		{"restart", "prod", "--request-file", filepath.Join(file, "child")},
		{"pause", "prod", "--request-file", file + "new", "--dry-run"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Execute(context.Background(), append(args, "--kubeconfig", statusConfig(t, server.URL)), &stdout, &stderr); code != 1 {
			t.Fatalf("unsafe output accepted: %d %s %s", code, &stdout, &stderr)
		}
	}
	if f.patches.Load() != 0 {
		t.Fatal("invalid request output submitted a mutation")
	}
	//nolint:gosec // Check the original contents of this test-owned destination.
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "preserve existing" {
		t.Fatal("existing request was overwritten")
	}
}

func TestRequestIsSavedBeforePatchAndSaveFailurePreventsPatch(t *testing.T) {
	t.Parallel()
	for _, collide := range []bool{false, true} {
		f := &controlFixture{}
		file := filepath.Join(t.TempDir(), "request.json")
		var checked atomic.Bool
		f.onPatch = func() {
			request, err := control.ReadRequest(context.Background(), file)
			if err != nil || request.Control == nil || request.Control.Target.Identity.UID != "cluster" {
				t.Errorf("original plan missing before PATCH: %+v %v", request, err)
			}
			checked.Store(true)
		}
		if collide {
			f.onGet = func() {
				if err := os.WriteFile(file, []byte("keep concurrent file"), 0o600); err != nil {
					t.Error(err)
				}
			}
		}
		server := f.serve(t)
		code, result := observationCall(t, statusConfig(t, server.URL), "pause", "prod", "--api-group", f.group, "--request-file", file)
		if collide {
			if code != 1 || result.ResultCode != "request_not_saved" || f.patches.Load() != 0 {
				t.Fatalf("save failure submitted request: %d %+v", code, result)
			}
			continue
		}
		if code != 0 || !checked.Load() || f.patches.Load() != 1 {
			t.Fatalf("save order not verified: %d %+v", code, result)
		}
		request, err := control.ReadRequest(context.Background(), file)
		if err != nil {
			t.Fatal(err)
		}
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		newPath := file + ".cancelled"
		if err := control.WriteRequest(cancelled, newPath, request); err == nil {
			t.Fatal("cancelled save succeeded")
		}
		if _, err := os.Lstat(newPath); !os.IsNotExist(err) {
			t.Fatalf("cancelled save published output: %v", err)
		}
		if err := control.WriteRequest(context.Background(), file, request); err == nil {
			t.Fatal("request was overwritten")
		}
	}
}

func TestObserveScopeFlagsFailBeforeCredentialLoading(t *testing.T) {
	t.Parallel()
	f := &controlFixture{}
	server := f.serve(t)
	file := filepath.Join(t.TempDir(), "request.json")
	_, _ = observationCall(t, statusConfig(t, server.URL), "pause", "prod", "--api-group", f.group, "--request-file", file)
	for _, extra := range [][]string{{"-n", "other"}, {"--api-group", "openbao.org"}, {"--dry-run"}} {
		var stdout, stderr bytes.Buffer
		args := append([]string{"observe", "--request-file", file, "--kubeconfig", "/missing/config"}, extra...)
		if code := Execute(context.Background(), args, &stdout, &stderr); code != 1 ||
			strings.Contains(stderr.String(), "cannot load kubeconfig") || stdout.Len() != 0 {
			t.Fatalf("invalid scope reached credentials: %d stdout=%s stderr=%s", code, &stdout, &stderr)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	cancel()
	if _, err := control.ReadRequest(ctx, file); err == nil {
		t.Fatal("cancelled request read succeeded")
	}
}

func TestReadOnlyObservationTextAndOutputFailure(t *testing.T) {
	t.Parallel()
	f := &controlFixture{stalled: true}
	server := f.serve(t)
	config, file := statusConfig(t, server.URL), filepath.Join(t.TempDir(), "request.json")
	_, _ = observationCall(t, config, "pause", "prod", "--api-group", f.group, "--request-file", file)
	args := []string{"observe", "--request-file", file, "--kubeconfig", config}
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), args, &stdout, &stderr)
	if code != 3 || !strings.Contains(stdout.String(), "Read-only observation.") || strings.Contains(stdout.String(), "was submitted") ||
		!strings.Contains(stdout.String(), "--request-file "+file) || !strings.Contains(stdout.String(), "--kubeconfig "+config) ||
		!strings.Contains(stdout.String(), "--api-group kubebao.org") {
		t.Fatalf("misleading continuation text: %d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	if code := Execute(context.Background(), args, failingWriter{}, &stderr); code != 1 || f.patches.Load() != 1 {
		t.Fatal("output failure retried the mutation or lost output error")
	}
}

func TestObserveWaitDiscardsEarlierAssessmentAfterReplacement(t *testing.T) {
	t.Parallel()
	f := &controlFixture{stalled: true}
	var reads atomic.Int32
	f.onGet = func() {
		if reads.Add(1) >= 3 {
			f.replacement = true
		}
	}
	server := f.serve(t)
	config, file := statusConfig(t, server.URL), filepath.Join(t.TempDir(), "request.json")
	_, _ = observationCall(t, config, "pause", "prod", "--api-group", f.group, "--request-file", file)
	code, result := observationCall(t, config, "observe", "--request-file", file, "--wait", "--poll-interval", "100ms")
	if code != 3 || result.ResultCode != "resource_replaced" || result.Assessment != nil || f.patches.Load() != 1 {
		t.Fatalf("earlier assessment survived replacement: %d %+v", code, result)
	}
}
