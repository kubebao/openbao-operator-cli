package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/collect"
	shared "github.com/kubebao/openbao-operator-client/evidence"
)

func TestCompletedBackupPodDoesNotMakeDoctorIncomplete(t *testing.T) {
	t.Parallel()
	backend := doctorServer(t, true, false)
	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	forward := httputil.NewSingleHostReverseProxy(target)
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/pods") {
			forward.ServeHTTP(writer, request)
			return
		}
		_, _ = fmt.Fprint(writer, `{"apiVersion":"v1","kind":"PodList","metadata":{"resourceVersion":"100"},"items":[
{"metadata":{"name":"prod-0","namespace":"context-namespace","uid":"pod","generation":1,
"ownerReferences":[{"apiVersion":"apps/v1","kind":"StatefulSet","name":"prod","uid":"set","controller":true}]},
"status":{"phase":"Running","conditions":[{"type":"Ready","observedGeneration":1,"status":"True"}]}},
{"metadata":{"name":"backup-0","namespace":"context-namespace","uid":"backup-pod",
"ownerReferences":[{"apiVersion":"batch/v1","kind":"Job","name":"backup","uid":"backup-job","controller":true}]},
"spec":{"credentials":"JOB_PRIVATE_CANARY"},"status":{"phase":"Succeeded"}}]}`)
	}))
	defer proxy.Close()
	var stdout, stderr bytes.Buffer
	code := Execute(
		context.Background(),
		[]string{"doctor", "prod", "--kubeconfig", statusConfig(t, proxy.URL), "-o", "json"},
		&stdout,
		&stderr,
	)
	var report diagnosticReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if code != 0 || report.Evidence.ResultCode != "completed" || len(report.Findings) != 0 || strings.Contains(stdout.String(), "CANARY") {
		t.Fatalf("completed backup Pod changed diagnosis: exit=%d result=%s", code, report.ResultCode)
	}
	if report.Evidence.NormalizedEvidence.Clusters[0].Pods.Collection.Items != 2 {
		t.Fatal("association filtering changed admitted candidate counts")
	}
	for _, coverage := range report.Coverage {
		if coverage.RuleID == "KBAO-POD-001" && coverage.State != "evaluated" {
			t.Fatal("a completed Job Pod limited StatefulSet Pod coverage")
		}
	}
}

func TestUnconfirmedAssociationDoesNotChangeCompletedReads(t *testing.T) {
	t.Parallel()
	backend := doctorServer(t, true, false)
	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	forward := httputil.NewSingleHostReverseProxy(target)
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/openbaorestores") {
			forward.ServeHTTP(writer, request)
			return
		}
		_, _ = fmt.Fprint(
			writer,
			`{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoRestoreList","metadata":{"resourceVersion":"100"},"items":[
{"metadata":{"name":"unbound","namespace":"context-namespace","uid":"unbound"},"spec":{"cluster":"prod"},"status":{"phase":"Completed"}}]}`,
		)
	}))
	defer proxy.Close()
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{
		"doctor", "prod", "--kubeconfig", statusConfig(t, proxy.URL), "--fail-on", "none", "-o", "json",
	}, &stdout, &stderr)
	var report diagnosticReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if code != 3 || report.Evidence.ResultCode != "completed" || report.Evidence.ExitCode != 0 || report.ResultCode != "incomplete" {
		t.Fatalf(
			"read completion and association coverage were combined: exit=%d result=%s read=%s",
			code,
			report.ResultCode,
			report.Evidence.ResultCode,
		)
	}
}

func TestStatusValidationPathExcludesRejectedCaptureFromOutput(t *testing.T) {
	t.Parallel()
	server := doctorServer(t, true, false)
	client, err := collect.NewClient(collect.Options{Name: "prod", Kubeconfig: statusConfig(t, server.URL), RequestTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := client.Collect(context.Background())
	if snapshot.NormalizedEvidence == nil {
		t.Fatal("fixture collection failed")
	}
	canary := "PRIVATE_CANARY"
	snapshot.NormalizedEvidence.Clusters[0].Observation.Reference.ResourceVersion = shared.Text{State: shared.ValueInvalid, Value: &canary}
	report := makeStatusReport(snapshot)
	if report.ExitCode != 3 || report.ValidationPath != "clusters[0].observation.reference.resourceVersion" ||
		report.NormalizedEvidence != nil {
		t.Fatal("status omitted the validation path or retained a rejected capture")
	}
	for _, format := range []string{"text", "json"} {
		var output bytes.Buffer
		if err := writeStatus(&output, report, format, false); err != nil ||
			!strings.Contains(output.String(), report.ValidationPath) || strings.Contains(output.String(), "CANARY") {
			t.Fatal("status validation output exposed data or omitted its field path")
		}
	}
}
