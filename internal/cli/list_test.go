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
)

const listedClusterTemplate = `{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",
	"metadata":{"name":%q,"namespace":%q,"uid":%q,"resourceVersion":"7","generation":2},
	"spec":{"version":"2.6.4","replicas":3,"paused":%t},
	"status":{"observedGeneration":2,"currentVersion":"2.6.4","readyReplicas":%d,
	"conditions":[{"type":"Available","status":%q,"reason":"Fixture","observedGeneration":2}]}}`

// listServer serves discovery and namespaced or cluster-wide OpenBaoCluster lists. denied rejects every list.
func listServer(t *testing.T, denied bool) *httptest.Server {
	t.Helper()
	prod := fmt.Sprintf(listedClusterTemplate, "prod", "context-namespace", "uid-prod", false, 3, "True")
	stage := fmt.Sprintf(listedClusterTemplate, "stage", "context-namespace", "uid-stage", true, 1, "False")
	other := fmt.Sprintf(listedClusterTemplate, "edge", "other-namespace", "uid-edge", false, 3, "True")
	list := func(items ...string) string {
		return `{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoClusterList","metadata":{"resourceVersion":"9"},"items":[` +
			strings.Join(items, ",") + `]}`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		switch r.URL.Path {
		case "/apis/kubebao.org/v1alpha1":
			http.NotFound(w, r)
		case "/apis/openbao.org/v1alpha1":
			_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"APIResourceList","groupVersion":"openbao.org/v1alpha1","resources":[
				{"name":"openbaoclusters","kind":"OpenBaoCluster","namespaced":true}]}`)
		case "/apis/openbao.org/v1alpha1/namespaces/context-namespace/openbaoclusters":
			if denied {
				w.WriteHeader(http.StatusForbidden)
				_, _ = fmt.Fprint(w, `{"kind":"Status","message":"PRIVATE_LIST_CANARY"}`)
				return
			}
			_, _ = fmt.Fprint(w, list(stage, prod))
		case "/apis/openbao.org/v1alpha1/openbaoclusters":
			_, _ = fmt.Fprint(w, list(stage, other, prod))
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestListShowsClustersInTheSelectedNamespace(t *testing.T) {
	t.Parallel()
	server := listServer(t, false)
	var stdout, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"list", "--kubeconfig", statusConfig(t, server.URL)}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d; stderr %s", code, &stderr)
	}
	text := stdout.String()
	for _, want := range []string{
		"OpenBaoClusters  namespace context-namespace · context fixture",
		"NAME   VERSION  VOTERS  AVAILABLE  RECONCILIATION  MAINTENANCE  ATTENTION",
		"kubebao status prod -n context-namespace --context fixture",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Index(text, "prod ") > strings.Index(text, "stage ") || strings.Contains(text, "edge") ||
		strings.Contains(text, "NAMESPACE") {
		t.Fatalf("list is unsorted or leaked another namespace:\n%s", text)
	}
	if !strings.Contains(squeezeText(text), "stage 2.6.4 1/3 False paused") {
		t.Fatalf("paused cluster row:\n%s", text)
	}
}

func TestListAllNamespacesJSON(t *testing.T) {
	t.Parallel()
	server := listServer(t, false)
	var stdout, stderr bytes.Buffer
	args := []string{"ls", "-A", "-o", "json", "--color", "always", "--kubeconfig", statusConfig(t, server.URL)}
	if code := Execute(context.Background(), args, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d; stderr %s", code, &stderr)
	}
	var report listReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, c := range report.Clusters {
		names = append(names, c.Namespace+"/"+c.Name)
		if c.Observation == nil || c.UID == "" {
			t.Fatalf("cluster without observation or UID: %+v", c)
		}
	}
	if !report.AllNamespaces || report.Target.Namespace != "" || strings.Join(names, ",") !=
		"context-namespace/prod,context-namespace/stage,other-namespace/edge" || strings.Contains(stdout.String(), "\x1b[") {
		t.Fatalf("all-namespace report: %+v", report)
	}
	if len(report.Clusters[1].Observation.Attention) == 0 {
		t.Fatal("an unavailable, paused cluster has no status attention")
	}
}

func TestListDeniedIsIncompleteWithoutProviderText(t *testing.T) {
	t.Parallel()
	server := listServer(t, true)
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"list", "--kubeconfig", statusConfig(t, server.URL)}, &stdout, &stderr)
	if code != 3 || !strings.Contains(stdout.String(), "permission denied") || strings.Contains(stdout.String()+stderr.String(), "CANARY") {
		t.Fatalf("denied list: exit %d; stdout %s; stderr %s", code, &stdout, &stderr)
	}
}

func TestClusterNameCompletion(t *testing.T) {
	t.Parallel()
	server := listServer(t, false)
	config := statusConfig(t, server.URL)
	for _, args := range [][]string{
		{"__complete", "status", "--kubeconfig", config, ""},
		{"__complete", "maintenance", "enable", "--kubeconfig", config, ""},
		{"__complete", "restart", "--kubeconfig", config, "st"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Execute(context.Background(), args, &stdout, &stderr); code != 0 {
			t.Fatalf("%v: exit %d; stderr %s", args, code, &stderr)
		}
		lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
		want := []string{"prod\tOpenBao 2.6.4", "stage\tOpenBao 2.6.4, paused", ":4"}
		if args[len(args)-1] == "st" {
			want = want[1:]
		}
		if strings.Join(lines, "|") != strings.Join(want, "|") {
			t.Fatalf("%v completions = %q, want %q", args, lines, want)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := Execute(
		context.Background(),
		[]string{"__complete", "status", "prod", "--kubeconfig", config, ""},
		&stdout,
		&stderr,
	); code != 0 ||
		strings.TrimSpace(stdout.String()) != ":4" {
		t.Fatalf("second argument completed: %q", &stdout)
	}
}

func TestFlagCompletionAndUnreachableClusters(t *testing.T) {
	t.Parallel()
	config := statusConfig(t, "http://127.0.0.1:1")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"__complete", "status", "--kubeconfig", config, "--context", ""}, "alternate|fixture|:4"},
		{[]string{"__complete", "doctor", "--fail-on", ""}, "info|warning|error|none|:4"},
		{[]string{"__complete", "list", "--color", ""}, "auto|always|never|:4"},
		{[]string{"__complete", "pause", "--kubeconfig", config, ""}, ":4"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Execute(context.Background(), tc.args, &stdout, &stderr); code != 0 {
			t.Fatalf("%v: exit %d", tc.args, code)
		}
		if got := strings.Join(strings.Split(strings.TrimSpace(stdout.String()), "\n"), "|"); got != tc.want {
			t.Fatalf("%v completions = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestEveryReportingCommandCompletesOutput(t *testing.T) {
	t.Parallel()
	for _, path := range [][]string{
		{"list"},
		{"status"},
		{"doctor"},
		{"support-bundle"},
		{"pause"},
		{"resume"},
		{"maintenance", "enable"},
		{"maintenance", "disable"},
		{"restart"},
		{"version"},
	} {
		var stdout, stderr bytes.Buffer
		args := append(append([]string{"__complete"}, path...), "--output", "")
		if code := Execute(context.Background(), args, &stdout, &stderr); code != 0 ||
			strings.Join(strings.Split(strings.TrimSpace(stdout.String()), "\n"), "|") != "text|json|:4" {
			t.Fatalf("%v: --output completions = %q", path, &stdout)
		}
	}
}

func TestCompletionScriptsAndVersionShorthand(t *testing.T) {
	t.Parallel()
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		var stdout, stderr bytes.Buffer
		if code := Execute(context.Background(), []string{"completion", shell}, &stdout, &stderr); code != 0 || stdout.Len() == 0 {
			t.Fatalf("%s completion: exit %d; stderr %s", shell, code, &stderr)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"-v"}, &stdout, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), "unknown shorthand flag: 'v'") || !strings.Contains(stderr.String(), "use --version") {
		t.Fatalf("root -v: exit %d; stderr %s", code, &stderr)
	}
}

func squeezeText(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return strings.Join(lines, "\n")
}
