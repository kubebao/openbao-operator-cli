package cli

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestTextOutputIsPlainUnlessColorIsForced(t *testing.T) {
	t.Parallel()
	server := doctorServer(t, false, false)
	args := []string{"doctor", "prod", "--kubeconfig", statusConfig(t, server.URL), "--api-group", "openbao.org"}
	for _, tc := range []struct {
		color  string
		styled bool
	}{{"", false}, {"never", false}, {"always", true}} {
		var stdout, stderr bytes.Buffer
		call := args
		if tc.color != "" {
			call = append(append([]string{}, args...), "--color", tc.color)
		}
		if code := Execute(context.Background(), call, &stdout, &stderr); code != 2 {
			t.Fatalf("color %q: exit %d; stderr %s", tc.color, code, &stderr)
		}
		if got := strings.Contains(stdout.String(), "\x1b["); got != tc.styled {
			t.Fatalf("color %q: styled=%v; stdout %q", tc.color, got, &stdout)
		}
		if !strings.Contains(stdout.String(), "kubectl -n context-namespace describe pod prod-0 --context fixture --kubeconfig") {
			t.Fatalf("color %q: finding lost its suggested command: %s", tc.color, &stdout)
		}
	}
}

func TestJSONOutputIgnoresColor(t *testing.T) {
	t.Parallel()
	server := doctorServer(t, false, false)
	var stdout, stderr bytes.Buffer
	args := []string{
		"doctor",
		"prod",
		"--kubeconfig",
		statusConfig(t, server.URL),
		"--api-group",
		"openbao.org",
		"-o",
		"json",
		"--color",
		"always",
	}
	if code := Execute(context.Background(), args, &stdout, &stderr); code != 2 || strings.Contains(stdout.String(), "\x1b[") {
		t.Fatalf("JSON contained escape sequences: exit %d; %q", code, &stdout)
	}
}

func TestReportedResultsAreNotPrintedAsErrors(t *testing.T) {
	t.Parallel()
	server := doctorServer(t, false, false)
	var stdout, stderr bytes.Buffer
	args := []string{"doctor", "prod", "--kubeconfig", statusConfig(t, server.URL), "--api-group", "openbao.org"}
	if code := Execute(context.Background(), args, &stdout, &stderr); code != 2 ||
		strings.Contains(stderr.String(), "Error:") || !strings.Contains(stderr.String(), "kubebao: diagnosis result: findings") {
		t.Fatalf("findings were reported as an error: exit %d; stderr %q", code, &stderr)
	}
	stdout.Reset()
	stderr.Reset()
	missing := statusServer(t, "context-namespace", http.NotFound)
	if code := Execute(context.Background(), []string{"status", "prod", "--kubeconfig", statusConfig(t, missing.URL)},
		&stdout, &stderr); code != 1 ||
		!strings.Contains(
			stderr.String(),
			"Error:",
		) || !strings.Contains(stdout.String(), "kubebao list -n context-namespace --api-group openbao.org "+
		"--context fixture --kubeconfig") {
		t.Fatalf("missing target: exit %d; stdout %q; stderr %q", code, &stdout, &stderr)
	}
}

func TestBlockedMaintenanceSuggestsAnExplicitResume(t *testing.T) {
	t.Parallel()
	fixture := &controlFixture{paused: true}
	server := fixture.serve(t)
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{
		"maintenance", "enable", "prod", "--wait",
		"--kubeconfig", statusConfig(t, server.URL), "--api-group", fixture.group,
	}, &stdout, &stderr)
	paused, _, _ := fixture.state()
	text := stdout.String()
	if code != 3 || !paused || !strings.Contains(text, "Maintenance mode enabled for prod, but not yet applied") ||
		!strings.Contains(text, "kubebao resume prod -n context-namespace --api-group kubebao.org --context fixture --kubeconfig") ||
		!strings.Contains(text, "never resumes implicitly") || !strings.Contains(text, "Observation  blocked · reconciliation is paused") {
		t.Fatalf("blocked maintenance text: exit %d; stdout %s", code, text)
	}
}

// operatorAdvisoryCluster mirrors a healthy cluster: no read replicas, the permanent etcd advisory, an unverified
// user-access condition, and a successful latest backup with the omitted zero failure count.
const operatorAdvisoryCluster = `{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",
	"metadata":{"name":"prod","namespace":"context-namespace","uid":"uid-1","resourceVersion":"42","generation":3},
	"spec":{"version":"2.6.4","replicas":3,"backup":{"schedule":"@daily"}},
	"status":{"observedGeneration":3,"readyReplicas":3,
	"backup":{"lastBackupTime":"2026-01-01T00:00:09Z","lastAttemptTime":"2026-01-01T00:00:00Z"},
	"conditions":[
		{"type":"Available","status":"True","reason":"AllReplicasReady","observedGeneration":3},
		{"type":"EtcdEncryptionWarning","status":"True","reason":"EtcdEncryptionUnknown","observedGeneration":3},
		{"type":"OpenBaoSealed","status":"False","reason":"Unsealed","observedGeneration":3},
		{"type":"ReadReplicasReady","status":"False","reason":"NoReadReplicasConfigured","observedGeneration":3},
		{"type":"UserAccessBootstrap","status":"Unknown","reason":"UserAccessUnverified","observedGeneration":3}]}}`

func TestStatusSeparatesAdvisoriesFromIssuesAndNamesConditionGaps(t *testing.T) {
	t.Parallel()
	server := statusServer(t, "context-namespace", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(operatorAdvisoryCluster))
	})
	var stdout, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"status", "prod", "--kubeconfig", statusConfig(t, server.URL)}, &stdout,
		&stderr); code != 0 {
		t.Fatalf("exit %d; stderr %s", code, &stderr)
	}
	text := stdout.String()
	for _, want := range []string{
		"? No issues reported, but some checks lack evidence",
		"• The operator cannot verify that the Kubernetes API server encrypts Secrets at rest in etcd.",
		"? Operator conditions: UserAccessBootstrap is Unknown  KBAO-CONDITION-001",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"Needs attention", "ReadReplicasReady", "Backup failures"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("unexpected %q in:\n%s", unwanted, text)
		}
	}
	stdout.Reset()
	if code := Execute(context.Background(), []string{"status", "prod", "-o", "json", "--kubeconfig", statusConfig(t, server.URL)},
		&stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), `"severity":"info"`) ||
		!strings.Contains(stdout.String(), `"consecutiveFailures":{"state":"implied","value":0}`) {
		t.Fatalf("JSON lacks attention severity or the implied failure count: %s", &stdout)
	}
}
