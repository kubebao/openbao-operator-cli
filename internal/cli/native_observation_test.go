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

	"github.com/kubebao/openbao-operator-cli/internal/bundle"
)

// Native API fixtures retain the operator's legacy workload metadata keys.
func nativeDoctorServer(t *testing.T) *httptest.Server {
	t.Helper()
	backend := doctorServer(t, true, false)
	t.Cleanup(backend.Close)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/apis/openbao.org/") {
			http.NotFound(w, r)
			return
		}
		request := r.Clone(r.Context())
		request.URL.Path = strings.Replace(request.URL.Path, "/apis/kubebao.org/", "/apis/openbao.org/", 1)
		recorded := httptest.NewRecorder()
		backend.Config.Handler.ServeHTTP(recorded, request)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(recorded.Code)
		_, _ = w.Write(bytes.ReplaceAll(recorded.Body.Bytes(), []byte("openbao.org/v1alpha1"), []byte("kubebao.org/v1alpha1")))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestNativeObservationBundleAndOfflineDiagnosis(t *testing.T) {
	t.Parallel()
	server := nativeDoctorServer(t)
	kubeconfig := statusConfig(t, server.URL)
	path := filepath.Join(t.TempDir(), "native.tar.gz")
	for _, command := range [][]string{
		{"status", "prod", "--kubeconfig", kubeconfig, "-o", "json"},
		{"doctor", "prod", "--kubeconfig", kubeconfig, "-o", "json"},
		{"support-bundle", "prod", "--kubeconfig", kubeconfig, "--output-file", path, "-o", "json"},
	} {
		t.Run(command[0], func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Execute(context.Background(), command, &stdout, &stderr); code != 0 {
				t.Fatalf("native %s: code=%d output=%s stderr=%s", command[0], code, &stdout, &stderr)
			}
			if !json.Valid(stdout.Bytes()) || strings.Contains(stdout.String(), "PRIVATE_CANARY") {
				t.Fatal("invalid or unapproved native command output")
			}
			for _, removed := range []string{`"ruleSetVersion"`, `"adapterContract"`, `"approvedContracts"`, `"adapter"`} {
				if bytes.Contains(stdout.Bytes(), []byte(removed)) {
					t.Fatal("native command output contains removed projection or rule metadata")
				}
			}
		})
	}
	stored, err := bundle.Read(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Manifest.FormatVersion != "v2" || stored.Capture == nil || len(stored.Capture.Clusters) != 1 {
		t.Fatal("bundle lost the current format or normalized capture")
	}
	capture, err := json.Marshal(stored.Capture)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := json.Marshal(stored.Manifest.Policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{
		"schemaVersion", "ruleSetVersion", "adapterContract", "approvedContracts", "restoreUIDBindingContracts",
	} {
		if bytes.Contains(capture, []byte(removed)) || bytes.Contains(policy, []byte(removed)) {
			t.Fatal("bundle retains removed evidence or rule version metadata")
		}
	}
	if stored.Snapshot().Target.APIGroup != "kubebao.org" {
		t.Fatal("bundle lost native target identity")
	}
	server.Close()
	var stdout, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"doctor", "--bundle", path, "-o", "json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("native offline diagnosis: code=%d output=%s stderr=%s", code, &stdout, &stderr)
	}
	var replay diagnosticReport
	if err := json.Unmarshal(stdout.Bytes(), &replay); err != nil {
		t.Fatal(err)
	}
	expected := diagnosticResultWithPolicy(stored.Snapshot(), "warning", stored.Manifest.Policy)
	if replay.Source != "bundle" || !reflect.DeepEqual(replay.Findings, expected.Findings) ||
		!reflect.DeepEqual(replay.Coverage, expected.Coverage) || !replay.AnalysisTime.Equal(stored.Manifest.CompletedAt) {
		t.Fatal("native capture does not replay with identical findings, coverage, and reference time")
	}
}
