package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
	"github.com/kubebao/openbao-operator-cli/internal/bundle"
)

func TestSupportBundleCommandAndOfflineDiagnosis(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                       string
		ready, denied              bool
		collectionCode, doctorCode int
	}{
		{"complete", true, false, 0, 0},
		{"complete with finding", false, false, 0, 2},
		{"partial", false, true, 3, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := doctorServer(t, tc.ready, tc.denied)
			path := filepath.Join(t.TempDir(), "prod.tar.gz")
			var stdout, stderr bytes.Buffer
			code := Execute(context.Background(), []string{
				"support-bundle", "prod", "--kubeconfig", statusConfig(t, server.URL), "--output-file", path, "-o", "json",
				"--max-backup-age", "48h",
			}, &stdout, &stderr)
			var result bundleResult
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || code != tc.collectionCode || result.ExitCode != code ||
				result.OutputFile != path {
				t.Fatalf("collection result: code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
			}
			stored, err := bundle.Read(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Manifest.Policy.MaxBackupAge != 48*time.Hour || stored.Manifest.Options.IncludeLogs {
				t.Fatal("requested collection policy was not recorded")
			}
			data, err := json.Marshal(stored)
			if err != nil || bytes.Contains(data, []byte("PRIVATE_CANARY")) {
				t.Fatal("unapproved container arguments or provider errors entered the bundle")
			}
			server.Close()
			stdout.Reset()
			stderr.Reset()
			code = Execute(context.Background(), []string{"doctor", "--bundle", path, "-o", "json"}, &stdout, &stderr)
			var replay diagnosticReport
			if json.Unmarshal(stdout.Bytes(), &replay) != nil || code != tc.doctorCode || replay.Source != "bundle" {
				t.Fatalf("offline result: code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
			}
			expected := diagnosticResultWithPolicy(stored.Snapshot(), "warning", stored.Manifest.Policy)
			if !reflect.DeepEqual(replay.Findings, expected.Findings) || !reflect.DeepEqual(replay.Coverage, expected.Coverage) ||
				!replay.AnalysisTime.Equal(stored.Manifest.CompletedAt) {
				t.Fatal("offline analysis changed findings, coverage, or reference time")
			}
			if strings.Contains(stdout.String()+stderr.String(), "PRIVATE_CANARY") {
				t.Fatal("excluded provider data reached output")
			}
			stdout.Reset()
			stderr.Reset()
			_ = Execute(context.Background(), []string{"doctor", "--bundle", path}, &stdout, &stderr)
			if !strings.Contains(stdout.String(), "Offline diagnosis of prod") {
				t.Fatal("text output does not identify captured evidence")
			}
		})
	}
}

func TestLiveReportCanBeBundledAndReplayedExactly(t *testing.T) {
	t.Parallel()
	server := doctorServer(t, false, false)
	var stdout, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{
		"doctor", "prod", "--kubeconfig", statusConfig(t, server.URL), "--max-backup-age", "72h", "-o", "json",
	}, &stdout, &stderr); code != 2 {
		t.Fatalf("live diagnosis: %d %s", code, &stderr)
	}
	var live diagnosticReport
	if err := json.Unmarshal(stdout.Bytes(), &live); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "live.tar.gz")
	stored := bundle.New(live.Evidence.Snapshot, live.Policy, buildinfo.Current(), time.Second)
	if err := bundle.Write(context.Background(), path, stored, false); err != nil {
		t.Fatal(err)
	}
	server.Close()
	stdout.Reset()
	stderr.Reset()
	code := Execute(context.Background(), []string{"doctor", "--bundle", path, "-o", "json"}, &stdout, &stderr)
	var replay diagnosticReport
	if err := json.Unmarshal(stdout.Bytes(), &replay); err != nil {
		t.Fatal(err)
	}
	if code != 2 || !reflect.DeepEqual(live.Findings, replay.Findings) || !reflect.DeepEqual(live.Policy, replay.Policy) ||
		!reflect.DeepEqual(live.Coverage, replay.Coverage) {
		t.Fatal("identical captured evidence and policy produced different live/offline analysis")
	}
}

func TestBundleInputValidationPrecedesCredentials(t *testing.T) {
	t.Parallel()
	cases := [][]string{
		{"doctor", "--bundle", "missing.tar.gz", "--kubeconfig", "/does/not/exist"},
		{"doctor", "prod", "--bundle", "missing.tar.gz"},
		{"doctor", "--bundle", ""},
		{"doctor", "--bundle", "missing.tar.gz", "--max-backup-age", "1h"},
		{"doctor", "prod", "--max-backup-age", "0", "--kubeconfig", "/does/not/exist"},
		{"doctor", "prod", "--max-backup-age", "-1h", "--kubeconfig", "/does/not/exist"},
		{"support-bundle", "prod", "--kubeconfig", "/does/not/exist"},
		{"support-bundle", "prod", "--max-backup-age", "0", "--kubeconfig", "/does/not/exist"},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		code := Execute(context.Background(), args, &stdout, &stderr)
		if code != 1 || stdout.Len() != 0 || strings.Contains(stderr.String(), "cannot load kubeconfig") {
			t.Fatalf("input reached credentials: %v code=%d stderr=%s", args, code, &stderr)
		}
	}
	path := filepath.Join(t.TempDir(), "existing.tar.gz")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{
		"support-bundle", "prod", "--output-file", path, "--kubeconfig", "/does/not/exist",
	}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "--overwrite") {
		t.Fatal("existing destination did not stop before credential loading")
	}
}

func TestInterruptedCollectionPublishesPartialBundle(t *testing.T) {
	t.Parallel()
	backend := doctorServer(t, true, false)
	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	forward := httputil.NewSingleHostReverseProxy(target)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/pods") {
			cancel()
			return
		}
		forward.ServeHTTP(w, r)
	}))
	defer proxy.Close()
	path := filepath.Join(t.TempDir(), "interrupted.tar.gz")
	var stdout, stderr bytes.Buffer
	code := Execute(ctx, []string{
		"support-bundle", "prod", "--kubeconfig", statusConfig(t, proxy.URL), "--output-file", path,
	}, &stdout, &stderr)
	if code != 3 || !strings.Contains(stdout.String(), path) {
		t.Fatalf("interrupted evidence was not published: %d %s %s", code, &stdout, &stderr)
	}
	stored, err := bundle.Read(context.Background(), path)
	if err != nil || stored.Capture == nil || stored.Manifest.CollectionResult == "completed" {
		t.Fatalf("partial capture or collection outcomes lost: %v", err)
	}
}

func TestBundleIdentityAndPolicyValidation(t *testing.T) {
	t.Parallel()
	server := doctorServer(t, true, false)
	var stdout, stderr bytes.Buffer
	_ = Execute(context.Background(), []string{
		"doctor", "prod", "--kubeconfig", statusConfig(t, server.URL), "-o", "json",
	}, &stdout, &stderr)
	var live diagnosticReport
	if err := json.Unmarshal(stdout.Bytes(), &live); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*bundle.Bundle){
		func(b *bundle.Bundle) { b.Manifest.Target.UID = "replacement" },
		func(b *bundle.Bundle) { b.Manifest.Policy.MaxEvidenceAge = 0 },
		func(b *bundle.Bundle) { b.Manifest.CompletedAt = b.Manifest.StartedAt.Add(-time.Second) },
		func(b *bundle.Bundle) { b.Manifest.StartedAt = b.Manifest.CompletedAt },
		func(b *bundle.Bundle) { b.Manifest.CollectionResult = "PRIVATE_CANARY" },
		func(b *bundle.Bundle) { b.Manifest.Policy.ReferenceTime = b.Manifest.CompletedAt.Add(time.Second) },
	} {
		stored := bundle.New(live.Evidence.Snapshot, live.Policy, buildinfo.Current(), time.Second)
		change(&stored)
		if err := stored.Validate(); err == nil {
			t.Fatal("changed target, interval, or invalid policy was accepted")
		}
	}
}

func TestBundleAndOfflineOutputFailures(t *testing.T) {
	t.Parallel()
	server := doctorServer(t, false, false)
	path := filepath.Join(t.TempDir(), "bundle.tar.gz")
	var stderr bytes.Buffer
	code := Execute(context.Background(), []string{
		"support-bundle", "prod", "--kubeconfig", statusConfig(t, server.URL), "--output-file", path,
	}, failingWriter{}, &stderr)
	if code != 1 {
		t.Fatal("bundle result output failure did not fail the command")
	}
	if _, err := bundle.Read(context.Background(), path); err != nil {
		t.Fatal("output failure damaged the published archive")
	}
	for _, format := range []string{"text", "json"} {
		stderr.Reset()
		code := Execute(context.Background(), []string{"doctor", "--bundle", path, "-o", format}, failingWriter{}, &stderr)
		if code != 1 {
			t.Fatalf("offline %s output failure returned %d", format, code)
		}
	}
}

func TestCompleteCollectionWithIncompleteRulesStillCreatesSuccessfulBundle(t *testing.T) {
	t.Parallel()
	backend := doctorServer(t, true, false)
	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	forward := httputil.NewSingleHostReverseProxy(target)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/openbaoclusters/prod") {
			_, _ = w.Write([]byte(`{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",
"metadata":{"name":"prod","namespace":"context-namespace","uid":"cluster","generation":1},
"spec":{"replicas":1},"status":{"observedGeneration":1,"conditions":[
{"type":"Available","status":"True","observedGeneration":1}]}}`))
			return
		}
		forward.ServeHTTP(w, r)
	}))
	defer proxy.Close()
	path := filepath.Join(t.TempDir(), "incomplete-rules.tar.gz")
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{
		"support-bundle", "prod", "--kubeconfig", statusConfig(t, proxy.URL), "--output-file", path, "-o", "json",
	}, &stdout, &stderr)
	var result bundleResult
	if json.Unmarshal(stdout.Bytes(), &result) != nil || code != 0 || result.DiagnosticComplete || result.CollectionResult != "completed" {
		t.Fatalf("collection and diagnostic completeness were combined: %d %s %s", code, &stdout, &stderr)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Execute(context.Background(), []string{"doctor", "--bundle", path}, &stdout, &stderr); code != 3 {
		t.Fatalf("offline diagnosis claimed complete rule coverage: %d", code)
	}
}
