package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/kubebao/openbao-operator-cli/internal/bundle"
	"github.com/kubebao/openbao-operator-cli/internal/doctor"
	"github.com/kubebao/openbao-operator-client/adapter"
	shared "github.com/kubebao/openbao-operator-client/evidence"
)

type restoreRuntimeConfig struct {
	kubeconfig, context, group, namespace, cluster, targetUID string
	request, requestUID, phase, coverage, severity            string
	missing                                                   string
	exitCode, candidates                                      int
}

// This test observes a caller-provisioned restore. It creates no Kubernetes resources.
func TestOperatorRestoreDoctorAndBundleReplay(t *testing.T) {
	config := readRestoreRuntimeConfig(t)
	flags := []string{
		"--kubeconfig", config.kubeconfig, "--context", config.context, "--api-group", config.group,
		"--namespace", config.namespace, "-o", "json",
	}
	args := append([]string{"doctor", config.cluster, "--fail-on", "none"}, flags...)
	live := executeRestoreDoctor(t, args, config.exitCode)
	assertRestoreRuntimeCapture(t, live, config)

	path := filepath.Join(t.TempDir(), "operator-restore.tar.gz")
	args = append([]string{"support-bundle", config.cluster, "--output-file", path}, flags...)
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), args, &stdout, &stderr)
	var result bundleResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || code != 0 || result.CollectionResult != "completed" ||
		result.DiagnosticComplete != (config.exitCode == 0) {
		t.Fatalf("live restore bundle did not retain declared collection/coverage: exit=%d stderr=%s", code, &stderr)
	}
	stored, err := bundle.Read(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	args = []string{"doctor", "--bundle", path, "--fail-on", "none", "-o", "json"}
	replay := executeRestoreDoctor(t, args, config.exitCode)
	assertRestoreRuntimeCapture(t, replay, config)
	expected := diagnosticResultWithPolicy(stored.Snapshot(), "none", stored.Manifest.Policy)
	if replay.Source != "bundle" || !reflect.DeepEqual(replay.Findings, expected.Findings) ||
		!reflect.DeepEqual(replay.Coverage, expected.Coverage) || !reflect.DeepEqual(replay.Policy, expected.Policy) ||
		!replay.AnalysisTime.Equal(stored.Manifest.CompletedAt) {
		t.Fatal("live restore bundle replay changed findings, coverage, policy, or reference time")
	}
	repeated := executeRestoreDoctor(t, args, config.exitCode)
	if !reflect.DeepEqual(replay, repeated) {
		t.Fatal("repeated offline operator-restore diagnosis changed the report")
	}
}

func readRestoreRuntimeConfig(t *testing.T) restoreRuntimeConfig {
	t.Helper()
	if os.Getenv("KUBEBAO_TEST_RESTORE_KUBECONFIG") == "" {
		t.Skip("set KUBEBAO_TEST_RESTORE_KUBECONFIG and the declared restore expectations to run read-only operator qualification")
	}
	config := restoreRuntimeConfig{
		kubeconfig: restoreRuntimeEnv(t, "KUBECONFIG"), context: restoreRuntimeEnv(t, "CONTEXT"),
		group: restoreRuntimeEnv(t, "API_GROUP"), namespace: restoreRuntimeEnv(t, "NAMESPACE"),
		cluster: restoreRuntimeEnv(t, "CLUSTER"), targetUID: restoreRuntimeEnv(t, "TARGET_UID"),
		request: restoreRuntimeEnv(t, "REQUEST"), requestUID: restoreRuntimeEnv(t, "REQUEST_UID"),
		phase: restoreRuntimeEnv(t, "PHASE"), coverage: restoreRuntimeEnv(t, "COVERAGE"),
		severity: restoreRuntimeEnv(t, "FINDING_SEVERITY"),
	}
	var err error
	config.exitCode, err = strconv.Atoi(restoreRuntimeEnv(t, "EXIT_CODE"))
	if err != nil || config.exitCode != 0 && config.exitCode != 3 {
		t.Fatal("KUBEBAO_TEST_RESTORE_EXIT_CODE must be 0 or 3 for --fail-on none")
	}
	config.candidates, err = strconv.Atoi(restoreRuntimeEnv(t, "CANDIDATE_COUNT"))
	if err != nil || config.candidates < 1 || config.candidates > 1000 {
		t.Fatal("KUBEBAO_TEST_RESTORE_CANDIDATE_COUNT must be between 1 and 1000")
	}
	if !slices.Contains([]string{"kubebao.org", "openbao.org"}, config.group) ||
		!slices.Contains([]string{"Pending", "Validating", "Running", "Completed", "Failed", "Unknown"}, config.phase) ||
		!slices.Contains([]string{"evaluated", "insufficient_evidence"}, config.coverage) ||
		!slices.Contains([]string{"none", "info", "error"}, config.severity) {
		t.Fatal("restore API group, phase, coverage, or finding-severity expectation is invalid")
	}
	if config.coverage == "insufficient_evidence" {
		config.missing = restoreRuntimeEnv(t, "MISSING_REASON")
	}
	return config
}

func restoreRuntimeEnv(t *testing.T, suffix string) string {
	t.Helper()
	key := "KUBEBAO_TEST_RESTORE_" + suffix
	value := os.Getenv(key)
	if value == "" {
		t.Fatalf("%s must declare the caller-provisioned restore fixture or expected result", key)
	}
	return value
}

func assertRestoreRuntimeCapture(t *testing.T, report diagnosticReport, config restoreRuntimeConfig) {
	t.Helper()
	target := report.Evidence.Target
	if target.APIGroup != config.group || target.Context != config.context || target.Namespace != config.namespace ||
		target.Name != config.cluster || target.UID != config.targetUID || report.Evidence.ResultCode != "completed" {
		t.Fatal("live restore capture does not match the declared target or complete collection")
	}
	if report.Evidence.NormalizedEvidence == nil || len(report.Evidence.NormalizedEvidence.Clusters) != 1 {
		t.Fatal("live restore capture has no unique normalized cluster")
	}
	input := report.Evidence.NormalizedEvidence.Clusters[0]
	if input.Observation == nil || input.Verification == nil || input.Verification.State != shared.VerificationVerified ||
		input.Restores == nil || input.Restores.Collection.Outcome != shared.CollectionComplete ||
		input.Restores.Collection.Items != config.candidates || len(input.Restores.Observations) != config.candidates {
		t.Fatal("live restore capture lost final target verification or admitted restore candidates")
	}
	confirmed := false
	for _, request := range input.Restores.Observations {
		if request.Reference.Identity.Group != config.group || request.Reference.Identity.Namespace != config.namespace {
			t.Fatal("live restore capture crossed the declared API group or namespace")
		}
		if request.Reference.Identity.Name != config.request || request.Reference.Identity.UID != config.requestUID {
			continue
		}
		association, err := adapter.AssociateRestore(*input.Observation, request)
		if err != nil || association.State != shared.AssociationConfirmed || request.Phase.State != shared.ValueReported ||
			request.Phase.Value == nil || *request.Phase.Value != config.phase {
			t.Fatal("declared restore lacks UID confirmation or its expected reported phase")
		}
		confirmed = true
	}
	if !confirmed || report.Policy.ReportHistoricalRestores ||
		!slices.Contains(report.Policy.RestoreUIDBindingContracts, input.AdapterContract) {
		t.Fatal("live restore request or the CLI's declared historical-restore policy was lost")
	}
	assertRestoreRuntimeAnalysis(t, report, config)
}

func assertRestoreRuntimeAnalysis(t *testing.T, report diagnosticReport, config restoreRuntimeConfig) {
	t.Helper()
	coverageFound := false
	for _, coverage := range report.Coverage {
		if coverage.RuleID == "KBAO-RESTORE-001" {
			coverageFound = true
			if coverage.State != config.coverage || config.missing != "" && !slices.Contains(coverage.MissingEvidence, config.missing) {
				t.Fatalf("restore coverage is %s; declared expectation is %s", coverage.State, config.coverage)
			}
		}
	}
	count := 0
	for _, finding := range report.Findings {
		if finding.RuleID != "KBAO-RESTORE-001" || !restoreFindingHasUID(finding, config.requestUID) {
			continue
		}
		count++
		if string(finding.Severity) != config.severity {
			t.Fatalf("restore finding severity is %s; declared expectation is %s", finding.Severity, config.severity)
		}
	}
	want := 1
	if config.severity == "none" {
		want = 0
	}
	if !coverageFound || count != want {
		t.Fatal("restore coverage or UID-specific findings do not match the declared expectations")
	}
}

func restoreFindingHasUID(finding doctor.Finding, uid string) bool {
	for _, ref := range finding.Evidence {
		if ref.Kind == "OpenBaoRestore" && ref.UID == uid {
			return true
		}
	}
	return false
}
