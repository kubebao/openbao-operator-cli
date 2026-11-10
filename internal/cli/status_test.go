package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

func statusConfig(t *testing.T, server string) string {
	t.Helper()
	config := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: fixture
clusters:
- name: fixture-cluster
  cluster: {server: %q}
contexts:
- name: fixture
  context: {cluster: fixture-cluster, user: fixture, namespace: context-namespace}
- name: alternate
  context: {cluster: fixture-cluster, user: fixture, namespace: other-namespace}
users:
- name: fixture
  user: {}
`, server)
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func statusServer(t *testing.T, namespace string, resource http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		switch r.URL.Path {
		case "/apis/openbao.org/v1alpha1":
			_, _ = fmt.Fprint(
				w,
				`{"apiVersion":"v1","kind":"APIResourceList","resources":[
				{"name":"openbaoclusters","kind":"OpenBaoCluster","namespaced":true}]}`,
			)
		case "/apis/kubebao.org/v1alpha1":
			http.NotFound(w, r)
		case "/apis/openbao.org/v1alpha1/namespaces/" + namespace + "/openbaoclusters/prod":
			resource(w, r)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func writeStatusFixture(w http.ResponseWriter, namespace, uid string) {
	_, _ = fmt.Fprintf(
		w,
		`{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",
		"metadata":{"name":"prod","namespace":%q,"uid":%q,"resourceVersion":"42","generation":3},
		"spec":{"version":"2.6.4","replicas":3},"status":{"observedGeneration":3,
		"conditions":[{"type":"Available","status":"False","reason":"NoReadyReplicas","observedGeneration":3}]}}`,
		namespace,
		uid,
	)
}

func TestStatusUsesSelectedContextAndNamespace(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, namespace, context string
		extra                    []string
	}{
		{"context namespace", "context-namespace", "fixture", nil},
		{"alternate context", "other-namespace", "alternate", []string{"--context", "alternate"}},
		{"namespace override", "override", "alternate", []string{"--context", "alternate", "-n", "override"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := statusServer(
				t,
				tc.namespace,
				func(w http.ResponseWriter, _ *http.Request) { writeStatusFixture(w, tc.namespace, "original") },
			)
			args := append([]string{"status", "prod", "--kubeconfig", statusConfig(t, server.URL), "--output", "json"}, tc.extra...)
			var stdout, stderr bytes.Buffer
			code := Execute(context.Background(), args, &stdout, &stderr)
			var report evidence.Snapshot
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
				t.Fatalf("invalid output: %v; stderr: %s", err, &stderr)
			}
			if code != 0 || report.ExitCode != 0 || report.ResultCode != "completed" || stderr.Len() != 0 {
				t.Fatalf("code %d; report %+v; stderr %s", code, report, &stderr)
			}
			if report.Target.Context != tc.context || report.Target.Namespace != tc.namespace ||
				report.Observation.Available.Status != "False" {
				t.Fatalf("target or unhealthy status was not preserved: %+v", report)
			}
		})
	}
}

func TestStatusWatchStopsAtResourceReplacement(t *testing.T) {
	t.Parallel()
	var reads atomic.Int32
	server := statusServer(t, "context-namespace", func(w http.ResponseWriter, _ *http.Request) {
		uid := "original"
		if reads.Add(1) > 1 {
			uid = "replacement"
		}
		writeStatusFixture(w, "context-namespace", uid)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	code := Execute(ctx, []string{
		"status", "prod", "--kubeconfig", statusConfig(t, server.URL), "--api-group", "openbao.org",
		"--watch", "--watch-interval", "100ms", "--output", "json",
	}, &stdout, &stderr)
	decoder := json.NewDecoder(&stdout)
	var initial, replaced evidence.Snapshot
	if err := decoder.Decode(&initial); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&replaced); err != nil {
		t.Fatal(err)
	}
	if code != 3 || initial.Target.UID != "original" || replaced.Target.UID != "original" || replaced.ResultCode != "resource_replaced" {
		t.Fatalf("watch result %d; initial %+v; replaced %+v", code, initial.Target, replaced)
	}
	if reads.Load() != 2 {
		t.Fatalf("watch continued after replacement: %d reads", reads.Load())
	}
}

func TestStatusIncompleteJSONDoesNotContainProviderErrors(t *testing.T) {
	t.Parallel()
	server := statusServer(t, "context-namespace", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"Status","reason":"Forbidden","message":"PRIVATE_PROVIDER_RESPONSE"}`)
	})
	var stdout, stderr bytes.Buffer
	code := Execute(
		context.Background(),
		[]string{"status", "prod", "--kubeconfig", statusConfig(t, server.URL), "-o", "json"},
		&stdout,
		&stderr,
	)
	var report evidence.Snapshot
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if code != 3 || report.ResultCode != "incomplete" || report.Observation != nil {
		t.Fatalf("unexpected result: %+v", report)
	}
	if strings.Contains(stdout.String()+stderr.String(), "PRIVATE_PROVIDER_RESPONSE") {
		t.Fatal("provider response leaked")
	}
}

func TestStatusUsesKubeconfigEnvironment(t *testing.T) {
	server := statusServer(t, "context-namespace", func(w http.ResponseWriter, _ *http.Request) {
		writeStatusFixture(w, "context-namespace", "original")
	})
	t.Setenv("KUBECONFIG", statusConfig(t, server.URL))
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"status", "prod", "--output", "json"}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("KUBECONFIG was not used: code %d; stderr %s", code, &stderr)
	}
	var report evidence.Snapshot
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Target.Context != "fixture" || report.Target.Namespace != "context-namespace" {
		t.Fatalf("wrong target: %+v", report.Target)
	}
}

func TestStatusRejectsInputBeforeLoadingCredentials(t *testing.T) {
	t.Parallel()
	cases := [][]string{
		{"--output", "yaml"}, {"--api-group", "arbitrary.example"}, {"--request-timeout", "0s"}, {"--watch-interval", "0s"},
	}
	for _, extra := range cases {
		var stdout, stderr bytes.Buffer
		args := append([]string{"status", "prod", "--kubeconfig", "/does/not/exist"}, extra...)
		if code := Execute(context.Background(), args, &stdout, &stderr); code != 1 || stdout.Len() != 0 {
			t.Fatalf("unexpected result: %d; stdout %s; stderr %s", code, &stdout, &stderr)
		}
		if strings.Contains(stderr.String(), "cannot load kubeconfig") {
			t.Fatalf("credentials loaded before input validation: %s", &stderr)
		}
	}
}
