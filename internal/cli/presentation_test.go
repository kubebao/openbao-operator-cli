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
		if !strings.Contains(stdout.String(), "kubectl --context fixture -n context-namespace describe pod prod-0") {
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
		!strings.Contains(stderr.String(), "Error:") || !strings.Contains(stdout.String(), "kubectl get openbaoclusters -A") {
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
		!strings.Contains(text, "kubebao resume prod -n context-namespace --context fixture") ||
		!strings.Contains(text, "never resumes implicitly") || !strings.Contains(text, "Observation  blocked · reconciliation is paused") {
		t.Fatalf("blocked maintenance text: exit %d; stdout %s", code, text)
	}
}
