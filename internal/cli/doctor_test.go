package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kubebao/openbao-operator-cli/internal/doctor"
)

func doctorServer(t *testing.T, ready bool, denied bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			t.Errorf("unexpected mutation: %s", r.Method)
		}
		switch r.URL.Path {
		case "/apis/openbao.org/v1alpha1":
			_, _ = fmt.Fprint(w, `{"kind":"APIResourceList","resources":[
			{"name":"openbaoclusters","kind":"OpenBaoCluster","namespaced":true}]}`)
		case "/apis/openbao.org/v1alpha1/namespaces/context-namespace/openbaoclusters/prod":
			_, _ = fmt.Fprint(w, `{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",
			"metadata":{"name":"prod","namespace":"context-namespace","uid":"cluster","generation":1,"resourceVersion":"10"},
			"spec":{"replicas":1},"status":{"observedGeneration":1,"conditions":[
			{"type":"Available","status":"True","observedGeneration":1}]}}`)
		case "/apis/apps/v1/namespaces/context-namespace/statefulsets":
			_, _ = fmt.Fprint(w, `{"apiVersion":"apps/v1","kind":"StatefulSetList","items":[
			{"metadata":{"name":"prod","namespace":"context-namespace","uid":"set","generation":1,
			"ownerReferences":[{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",
			"name":"prod","uid":"cluster","controller":true}]},
			"spec":{"replicas":1},"status":{"observedGeneration":1,"readyReplicas":1}}]}`)
		case "/api/v1/namespaces/context-namespace/pods":
			if denied {
				w.WriteHeader(http.StatusForbidden)
				_, _ = fmt.Fprint(w, `{"kind":"Status","message":"API_PRIVATE_CANARY"}`)
				return
			}
			condition := "False"
			if ready {
				condition = "True"
			}
			_, _ = fmt.Fprintf(w, `{"apiVersion":"v1","kind":"PodList","items":[
			{"metadata":{"name":"prod-0","namespace":"context-namespace","uid":"pod",
			"ownerReferences":[{"apiVersion":"apps/v1","kind":"StatefulSet","name":"prod","uid":"set","controller":true}]},
			"spec":{"containers":[{"args":["SPEC_PRIVATE_CANARY"]}]},"status":{"phase":"Running","conditions":[
			{"type":"Ready","status":%q,"reason":"FixtureReadiness"}]}}]}`, condition)
		case "/apis/openbao.org/v1alpha1/namespaces/context-namespace/openbaorestores":
			_, _ = fmt.Fprint(w, `{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoRestoreList","items":[]}`)
		default:
			t.Errorf("unexpected read: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDoctorCommandResults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		ready, denied bool
		threshold     string
		code          int
	}{
		{"captured readiness", true, false, "warning", 0},
		{"readiness finding", false, false, "warning", 2},
		{"higher threshold", false, false, "error", 0},
		{"incomplete evidence", true, true, "none", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := doctorServer(t, tc.ready, tc.denied)
			var stdout, stderr bytes.Buffer
			code := Execute(context.Background(), []string{
				"doctor", "prod", "--kubeconfig", statusConfig(t, server.URL),
				"--api-group", "openbao.org", "--fail-on", tc.threshold, "-o", "json",
			}, &stdout, &stderr)
			var report doctor.Report
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
				t.Fatalf("invalid JSON: %v; stderr %s", err, &stderr)
			}
			if code != tc.code || report.ExitCode != tc.code || report.RuleSetVersion != doctor.RuleSetVersion {
				t.Fatalf("unexpected report: code %d, %+v", code, report)
			}
			if report.Evidence.Target.UID != "cluster" || len(report.Evidence.Related.StatefulSets) != 1 {
				t.Fatal("capture evidence was lost")
			}
			if strings.Contains(stdout.String()+stderr.String(), "PRIVATE_CANARY") {
				t.Fatal("unapproved data reached command output")
			}
		})
	}
}

func TestDoctorVerboseOutputAndWriteFailure(t *testing.T) {
	t.Parallel()
	server := doctorServer(t, false, false)
	args := []string{"doctor", "prod", "--kubeconfig", statusConfig(t, server.URL), "--api-group", "openbao.org", "--verbose"}
	var stdout, stderr bytes.Buffer
	if code := Execute(context.Background(), args, &stdout, &stderr); code != 2 || !strings.Contains(stdout.String(), "KBAO-POD-001") ||
		!strings.Contains(stdout.String(), "UID=pod") {
		t.Fatalf("verbose finding output failed: code %d; stdout %s", code, &stdout)
	}
	stderr.Reset()
	if code := Execute(context.Background(), args, failingWriter{}, &stderr); code != 1 {
		t.Fatalf("output failure returned %d", code)
	}
}

func TestDoctorRejectsThresholdBeforeLoadingCredentials(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := Execute(
		context.Background(),
		[]string{"doctor", "prod", "--kubeconfig", "/does/not/exist", "--fail-on", "major"},
		&stdout,
		&stderr,
	)
	if code != 1 || stdout.Len() != 0 || strings.Contains(stderr.String(), "cannot load kubeconfig") {
		t.Fatalf("invalid threshold reached collection: %d %s", code, &stderr)
	}
}
