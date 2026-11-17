package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/diagnose"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
	"github.com/kubebao/openbao-operator-cli/internal/bundle"
)

func TestRestoreTestDiagnosisLiveAndOffline(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name       string
		outcome    string
		condition  string
		reason     string
		generation int
		active     bool
		unfinished bool
		code       int
		finding    bool
	}{
		{name: "failed", outcome: "Failed", condition: "False", reason: "Failed", generation: 1, code: 2, finding: true},
		{
			name: "blocked during active test", outcome: "Passed", condition: "False", reason: "SnapshotUnavailable", generation: 1,
			active: true, code: 2, finding: true,
		},
		{name: "passed", outcome: "Passed", condition: "True", reason: "Passed", generation: 1},
		{name: "active suppresses last failure", outcome: "Failed", condition: "True", reason: "Passed", generation: 1, active: true},
		{name: "missing outcome", condition: "True", reason: "Passed", generation: 1, code: 3},
		{name: "invalid outcome", outcome: "Unexpected", condition: "True", reason: "Passed", generation: 1, code: 3},
		{name: "stale condition", outcome: "Passed", condition: "True", reason: "Passed", code: 3},
		{name: "unknown condition", outcome: "Passed", condition: "Unknown", reason: "Waiting", generation: 1, code: 3},
		{
			name: "active with unknown condition", outcome: "Failed", condition: "Unknown", reason: "Running", generation: 1,
			active: true, code: 3,
		},
		{
			name: "missing finish time", outcome: "Failed", condition: "False", reason: "Failed", generation: 1,
			unfinished: true, code: 3, finding: true,
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			last := map[string]any{"message": "PRIVATE_RESTORE_TEST_CANARY"}
			if scenario.outcome != "" {
				last["outcome"] = scenario.outcome
			}
			if !scenario.unfinished {
				last["finishedAt"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
			}
			evidence := map[string]any{
				"last": last,
				"conditions": []any{map[string]any{
					"type": "Passed", "status": scenario.condition, "reason": scenario.reason,
					"observedGeneration": scenario.generation, "message": "PRIVATE_RESTORE_TEST_CANARY",
				}},
			}
			if scenario.active {
				evidence["active"] = map[string]any{"name": "disposable-test"}
			}
			server := restoreTestDiagnosisServer(t, evidence)
			var stdout, stderr bytes.Buffer
			code := Execute(context.Background(), []string{
				"doctor", "prod", "--kubeconfig", statusConfig(t, server.URL), "--api-group", "openbao.org", "-o", "json",
			}, &stdout, &stderr)
			var live diagnosticReport
			if err := json.Unmarshal(stdout.Bytes(), &live); err != nil || code != scenario.code {
				t.Fatalf("live result: code=%d stdout=%s stderr=%s error=%v", code, &stdout, &stderr, err)
			}
			if (len(live.Findings) == 1) != scenario.finding || len(live.Findings) > 1 {
				t.Fatalf("restore-test findings = %+v", live.Findings)
			}
			if scenario.finding && (live.Findings[0].RuleID != string(diagnose.RuleRestoreTest) ||
				string(live.Findings[0].Severity) != "warning" || len(live.Findings[0].SuggestedChecks) == 0) {
				t.Fatalf("restore-test warning lost its identity or next step: %+v", live.Findings[0])
			}
			assertRestoreTestReplay(t, server, live, scenario.code)
		})
	}
}

func restoreTestDiagnosisServer(t *testing.T, evidence map[string]any) *httptest.Server {
	t.Helper()
	backend := doctorServer(t, true, false)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/openbaoclusters/prod") {
			backend.Config.Handler.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("restore-test diagnosis attempted a mutation: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		cluster := map[string]any{
			"apiVersion": "openbao.org/v1alpha1", "kind": "OpenBaoCluster",
			"metadata": map[string]any{
				"name": "prod", "namespace": "context-namespace", "uid": "cluster", "generation": 1, "resourceVersion": "10",
			},
			"spec": map[string]any{"replicas": 1, "backup": map[string]any{"schedule": "@daily"}},
			"status": map[string]any{
				"observedGeneration": 1, "workload": map[string]any{}, "adminOps": map[string]any{},
				"conditions": []any{
					map[string]any{"type": "Available", "status": "True", "observedGeneration": 1},
					map[string]any{"type": "OpenBaoSealed", "status": "False", "observedGeneration": 1},
				},
				"backup": map[string]any{
					"lastBackupTime":      time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
					"consecutiveFailures": 0, "restoreTest": evidence,
				},
			},
		}
		if err := json.NewEncoder(w).Encode(cluster); err != nil {
			t.Errorf("write restore-test fixture: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func assertRestoreTestReplay(t *testing.T, server *httptest.Server, live diagnosticReport, code int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "restore-test.tar.gz")
	stored := bundle.New(live.Evidence.Snapshot, live.Policy, buildinfo.Current(), time.Second)
	if err := bundle.Write(context.Background(), path, stored, false); err != nil {
		t.Fatal(err)
	}
	server.Close()
	var stdout, stderr bytes.Buffer
	got := Execute(context.Background(), []string{"doctor", "--bundle", path, "-o", "json"}, &stdout, &stderr)
	var replay diagnosticReport
	if err := json.Unmarshal(stdout.Bytes(), &replay); err != nil || got != code || replay.Source != "bundle" {
		t.Fatalf("offline result: code=%d stdout=%s stderr=%s error=%v", got, &stdout, &stderr, err)
	}
	if !reflect.DeepEqual(live.Findings, replay.Findings) || !reflect.DeepEqual(live.Coverage, replay.Coverage) ||
		!reflect.DeepEqual(live.Policy, replay.Policy) {
		t.Fatal("offline replay changed restore-test findings, coverage, or policy")
	}
	if strings.Contains(stdout.String()+stderr.String(), "PRIVATE_RESTORE_TEST_CANARY") {
		t.Fatal("restore-test provider message reached output")
	}
	stdout.Reset()
	stderr.Reset()
	if got := Execute(context.Background(), []string{"doctor", "--bundle", path, "--verbose"}, &stdout, &stderr); got != code ||
		!strings.Contains(stdout.String(), "Restore testing") {
		t.Fatalf("restore-test text report: code=%d stdout=%s stderr=%s", got, &stdout, &stderr)
	}
}
