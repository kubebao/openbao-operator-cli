package cli

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/bundle"
	"github.com/kubebao/openbao-operator-cli/internal/doctor"
)

type restoreCommandScenario struct {
	name, phase, targetUID, hold, threshold string
	coverage, missing                       string
	severity                                doctor.Severity
	denied, partial, retention              bool
	doctorCode, bundleCode, retained        int
}

func TestNonemptyRestoreDoctorAndBundleReplay(t *testing.T) {
	t.Parallel()
	cases := []restoreCommandScenario{
		{
			name: "progress", phase: "Running", targetUID: "cluster", threshold: "info",
			coverage: "evaluated", severity: doctor.Info, doctorCode: 2, retained: 1,
		},
		{
			name: "held failure", phase: "Failed", targetUID: "cluster", hold: "restore-operation", threshold: "warning",
			coverage: "evaluated", severity: doctor.Error, doctorCode: 2, retained: 1,
		},
		{
			name: "ordinary held unknown", phase: "Unknown", targetUID: "cluster", hold: "restore-operation", threshold: "warning",
			coverage: "evaluated", severity: doctor.Error, doctorCode: 2, retained: 1,
		},
		{
			name: "disposable cleanup retention", phase: "Unknown", targetUID: "cluster", hold: "restore-operation", threshold: "warning",
			retention: true, coverage: "evaluated", severity: doctor.Info, retained: 1,
		},
		{
			name: "unselected history", phase: "Unknown", targetUID: "cluster", threshold: "warning",
			coverage: "evaluated", retained: 1,
		},
		{
			name: "completed record", phase: "Completed", targetUID: "cluster", threshold: "warning",
			coverage: "evaluated", retained: 1,
		},
		{
			name: "name without UID proof", phase: "Running", threshold: "none",
			coverage: "insufficient_evidence", missing: "association_unconfirmed", doctorCode: 3, retained: 1,
		},
		{
			name: "different target UID", phase: "Running", targetUID: "previous-cluster", threshold: "info",
			coverage: "evaluated", retained: 1,
		},
		{
			name: "denied restore read", threshold: "none", denied: true,
			coverage: "insufficient_evidence", missing: "scope_incomplete", doctorCode: 3, bundleCode: 3,
		},
		{
			name: "partial restore read retains finding", phase: "Failed", targetUID: "cluster", hold: "restore-operation",
			threshold: "none", partial: true, coverage: "insufficient_evidence", missing: "scope_incomplete",
			severity: doctor.Error, doctorCode: 3, bundleCode: 3, retained: 1,
		},
	}
	for _, group := range []string{"kubebao.org", "openbao.org"} {
		for _, tc := range cases {
			t.Run(group+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				testRestoreCommandReplay(t, group, tc)
			})
		}
	}
}

func testRestoreCommandReplay(t *testing.T, group string, scenario restoreCommandScenario) {
	t.Helper()
	server := restoreDoctorServer(t, group, scenario)
	kubeconfig := statusConfig(t, server.URL)
	live := executeRestoreDoctor(t, []string{
		"doctor", "prod", "--kubeconfig", kubeconfig, "--api-group", group, "--fail-on", scenario.threshold, "-o", "json",
	}, scenario.doctorCode)
	assertRestoreCommandReport(t, live, group, scenario)

	path := filepath.Join(t.TempDir(), "nonempty-restores.tar.gz")
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{
		"support-bundle", "prod", "--kubeconfig", kubeconfig, "--api-group", group,
		"--output-file", path, "-o", "json",
	}, &stdout, &stderr)
	var result bundleResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || code != scenario.bundleCode || result.ExitCode != code {
		t.Fatalf("restore bundle: exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	if result.DiagnosticComplete != (scenario.coverage == "evaluated") {
		t.Fatal("restore collection and diagnostic completeness were combined")
	}
	stored, err := bundle.Read(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Manifest.Policy.ReportHistoricalRestores || stored.Manifest.Target.APIGroup != group {
		t.Fatal("bundle lost CLI historical-restore policy or selected API group")
	}
	assertRestoreArchiveExcludesCanaries(t, path)
	server.Close()

	args := []string{"doctor", "--bundle", path, "--fail-on", scenario.threshold, "-o", "json"}
	replay := executeRestoreDoctor(t, args, scenario.doctorCode)
	assertRestoreCommandReport(t, replay, group, scenario)
	expected := diagnosticResultWithPolicy(stored.Snapshot(), scenario.threshold, stored.Manifest.Policy)
	if replay.Source != "bundle" || !reflect.DeepEqual(replay.Findings, expected.Findings) ||
		!reflect.DeepEqual(replay.Coverage, expected.Coverage) || !reflect.DeepEqual(replay.Policy, expected.Policy) ||
		!replay.AnalysisTime.Equal(stored.Manifest.CompletedAt) {
		t.Fatal("nonempty restore replay changed findings, coverage, policy, or reference time")
	}
	repeated := executeRestoreDoctor(t, args, scenario.doctorCode)
	if !reflect.DeepEqual(replay, repeated) {
		t.Fatal("repeated offline restore diagnosis changed the report")
	}
	if !slices.Equal(restoreFindingIDs(live), restoreFindingIDs(replay)) {
		t.Fatal("restore finding identity changed between live and bundled collection")
	}
}

func executeRestoreDoctor(t *testing.T, args []string, wantCode int) diagnosticReport {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), args, &stdout, &stderr)
	var report diagnosticReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || code != wantCode || report.ExitCode != wantCode {
		t.Fatalf("restore diagnosis: exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	if strings.Contains(stdout.String()+stderr.String(), "PRIVATE_CANARY") {
		t.Fatal("unapproved restore fields or provider errors entered command output")
	}
	return report
}

func assertRestoreCommandReport(t *testing.T, report diagnosticReport, group string, scenario restoreCommandScenario) {
	t.Helper()
	if report.SchemaVersion != "v1" || report.Evidence.Target.APIGroup != group || report.Evidence.NormalizedEvidence == nil {
		t.Fatal("restore diagnosis lost the CLI schema or selected API identity")
	}
	input := report.Evidence.NormalizedEvidence.Clusters[0]
	if input.Restores == nil || len(input.Restores.Observations) != scenario.retained {
		t.Fatal("nonempty or partial restore candidates were lost from normalized evidence")
	}
	coverageFound, findingCount := false, 0
	for _, coverage := range report.Coverage {
		if coverage.RuleID != "KBAO-RESTORE-001" {
			continue
		}
		coverageFound = true
		if coverage.State != scenario.coverage || scenario.missing != "" && !slices.Contains(coverage.MissingEvidence, scenario.missing) {
			t.Fatalf("restore coverage changed: %+v", coverage)
		}
	}
	for _, finding := range report.Findings {
		if finding.RuleID != "KBAO-RESTORE-001" {
			continue
		}
		findingCount++
		assertRestoreCommandFinding(t, finding, group, scenario.severity)
	}
	wantFindings := 0
	if scenario.severity != "" {
		wantFindings = 1
	}
	if !coverageFound || findingCount != wantFindings {
		t.Fatalf("restore coverage or finding count changed: coverage=%t findings=%d", coverageFound, findingCount)
	}
}

func assertRestoreCommandFinding(t *testing.T, finding doctor.Finding, group string, severity doctor.Severity) {
	t.Helper()
	if finding.Severity != severity || finding.Summary != "OpenBaoRestore context-namespace/restore-prod" ||
		!strings.HasPrefix(finding.ID, "KBAO-RESTORE-001:") {
		t.Fatalf("restore finding projection changed: %+v", finding)
	}
	for _, ref := range finding.Evidence {
		if ref.UID == "restore-request" && ref.APIVersion == group+"/v1alpha1" {
			return
		}
	}
	t.Fatal("restore finding lost its request UID or API group")
}

func restoreFindingIDs(report diagnosticReport) []string {
	ids := []string{}
	for _, finding := range report.Findings {
		if finding.RuleID == "KBAO-RESTORE-001" {
			ids = append(ids, finding.ID)
		}
	}
	return ids
}

func restoreDoctorServer(t *testing.T, group string, scenario restoreCommandScenario) *httptest.Server {
	t.Helper()
	var backend *httptest.Server
	if group == "kubebao.org" {
		backend = nativeDoctorServer(t)
	} else {
		backend = doctorServer(t, true, false)
	}
	prefix := "/apis/" + group + "/v1alpha1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("restore observation sent a mutation: %s", r.Method)
		}
		switch r.URL.Path {
		case prefix + "/namespaces/context-namespace/openbaorestores":
			if r.URL.Query().Get("labelSelector") != "" || r.URL.Query().Get("fieldSelector") != "" {
				t.Error("restore observation narrowed the declared namespace LIST")
			}
			if scenario.denied || scenario.partial && r.URL.Query().Get("continue") != "" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"kind":"Status","message":"PROVIDER_PRIVATE_CANARY"}`))
				return
			}
			writeRestoreList(t, w, group, scenario)
		case prefix, prefix + "/namespaces/context-namespace/openbaoclusters/prod",
			"/apis/apps/v1/namespaces/context-namespace/statefulsets", "/api/v1/namespaces/context-namespace/pods":
			recorded := httptest.NewRecorder()
			backend.Config.Handler.ServeHTTP(recorded, r)
			data := recorded.Body.Bytes()
			if scenario.hold != "" && strings.HasSuffix(r.URL.Path, "/openbaoclusters/prod") {
				data = bytes.Replace(data, []byte(`"metadata":{`),
					[]byte(`"metadata":{"annotations":{"openbao.org/restore-hold":"`+scenario.hold+`"},`), 1)
			}
			w.WriteHeader(recorded.Code)
			_, _ = w.Write(data)
		default:
			t.Errorf("restore observation broadened its selected read scope: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func writeRestoreList(t *testing.T, w http.ResponseWriter, group string, scenario restoreCommandScenario) {
	t.Helper()
	stage := map[string]string{
		"Running": "Created", "Failed": "TerminalObserved", "Completed": "FollowThroughComplete", "Unknown": "Unknown",
	}[scenario.phase]
	execution := map[string]any{"operationID": "restore-operation", "stage": stage, "message": "EXECUTION_PRIVATE_CANARY"}
	target := map[string]any{"cleanup": "Complete"}
	if scenario.phase == "Running" {
		target["cleanup"] = "Pending"
	}
	if scenario.targetUID != "" {
		execution["targetUID"], target["uid"] = scenario.targetUID, scenario.targetUID
	}
	spec := map[string]any{"cluster": "prod", "source": map[string]any{"key": "SNAPSHOT_PRIVATE_CANARY"}}
	if scenario.retention {
		spec["clusterTemplate"], spec["targetLifecycle"], spec["cleanupAfterSeconds"] = map[string]any{}, "Disposable", 600
		target["cleanup"], target["appliedAt"] = "Pending", time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
		execution["terminalResult"] = "Succeeded"
	}
	metadata := map[string]any{"resourceVersion": "100"}
	if scenario.partial {
		metadata["continue"] = "later-page"
	}
	response := map[string]any{
		"apiVersion": group + "/v1alpha1", "kind": "OpenBaoRestoreList", "metadata": metadata,
		"items": []any{map[string]any{
			"apiVersion": group + "/v1alpha1", "kind": "OpenBaoRestore",
			"metadata": map[string]any{
				"name": "restore-prod", "namespace": "context-namespace", "uid": "restore-request", "generation": 1,
				"resourceVersion": "90", "annotations": map[string]any{"unapproved": "ANNOTATION_PRIVATE_CANARY"},
			},
			"spec": spec,
			"status": map[string]any{
				"phase": scenario.phase, "execution": execution, "target": target, "message": "STATUS_PRIVATE_CANARY",
			},
		}},
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		t.Error(err)
	}
}

func assertRestoreArchiveExcludesCanaries(t *testing.T, path string) {
	t.Helper()
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	const maxTestArchive = 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(reader, maxTestArchive))
	if err != nil || len(data) == maxTestArchive || bytes.Contains(data, []byte("PRIVATE_CANARY")) {
		t.Fatal("unapproved restore data or provider errors entered an archive entry")
	}
}
