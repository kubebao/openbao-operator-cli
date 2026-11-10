package status

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func fixture(t *testing.T) *unstructured.Unstructured {
	t.Helper()
	data, err := os.ReadFile("testdata/cluster.json")
	if err != nil {
		t.Fatal(err)
	}
	object := &unstructured.Unstructured{}
	if err := object.UnmarshalJSON(data); err != nil {
		t.Fatal(err)
	}
	return object
}

func TestProjectionPreservesEvidenceAndExcludesSensitiveFields(t *testing.T) {
	t.Parallel()
	captured := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	o, err := ProjectV1Alpha1(fixture(t), captured)
	if err != nil {
		t.Fatal(err)
	}
	if o.GenerationCurrent == nil || !*o.GenerationCurrent || o.Available.GenerationCurrent == nil || *o.Available.GenerationCurrent {
		t.Fatalf("resource and condition generations were conflated: %+v", o)
	}
	if !o.CollectedAt.Equal(captured) || o.Available.LastTransitionTime != "2026-11-09T10:00:00Z" {
		t.Fatal("collection and transition times were conflated")
	}
	if o.ReadReplicas.Ready == nil || *o.ReadReplicas.Ready != 0 || o.Paused == nil || *o.Paused {
		t.Fatal("reported zero and false values were lost")
	}
	if o.MaintenancePropagation != "not_collected" || o.RestoreHold != "restore-operation-123" {
		t.Fatal("control evidence was not preserved")
	}
	for _, item := range o.Attention {
		if strings.Contains(item.Summary, "OpenBaoSealed=False") || strings.Contains(item.Summary, "BackingUp=False") {
			t.Fatalf("normal negative condition became a problem: %+v", item)
		}
	}
	encoded, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, canary := range []string{"EXCLUDED_FIELD_CANARY", "FIXTURE_CREDENTIAL"} {
		if bytes.Contains(encoded, []byte(canary)) {
			t.Fatalf("sensitive fixture value reached JSON: %s", canary)
		}
	}
	if !bytes.Contains(encoded, []byte("[REDACTED]")) {
		t.Fatal("known sensitive pattern was not redacted")
	}
	var rendered bytes.Buffer
	report := Report{Target: Target{Context: "fixture", Namespace: "security", Name: "prod"}, Observation: o}
	if err := Write(&rendered, report, "text", true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.String(), "CANARY") || strings.Contains(rendered.String(), "FIXTURE_CREDENTIAL") {
		t.Fatal("sensitive fixture value reached verbose output")
	}
	if !strings.Contains(rendered.String(), "current-generation applicability is unconfirmed") {
		t.Fatal("stale condition was not called out")
	}
}

func TestMissingEvidenceRemainsUnknown(t *testing.T) {
	t.Parallel()
	object := &unstructured.Unstructured{Object: map[string]any{}}
	o, err := ProjectV1Alpha1(object, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if o.Available != nil || o.GenerationCurrent != nil || o.Paused != nil || o.MaintenanceEnabled != nil || o.Voters.Ready != nil {
		t.Fatalf("missing evidence became an observed value: %+v", o)
	}
	if o.Qualification != "unqualified" || len(o.Limitations) == 0 {
		t.Fatal("interpretation limits are missing")
	}
}

func TestMalformedProjectionDoesNotExposeValues(t *testing.T) {
	t.Parallel()
	object := fixture(t)
	if err := unstructured.SetNestedField(object.Object, "PRIVATE_MALFORMED_VALUE", "spec", "replicas"); err != nil {
		t.Fatal(err)
	}
	_, err := ProjectV1Alpha1(object, time.Now())
	if err == nil || strings.Contains(err.Error(), "PRIVATE_MALFORMED_VALUE") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestKnownCredentialPatterns(t *testing.T) {
	t.Parallel()
	for _, message := range []string{
		"Authorization: Bearer PRIVATE_VALUE", `response {"root_token": "PRIVATE_VALUE"}`,
		`password="PRIVATE_VALUE with spaces"`, "token=PRIVATE_VALUE",
	} {
		if result := clean(message); strings.Contains(result, "PRIVATE_VALUE") {
			t.Fatalf("known credential pattern was retained: %q", result)
		}
	}
}

func TestControlCharactersAreRemoved(t *testing.T) {
	t.Parallel()
	result := clean("message\x1b[2J\nnext\rline")
	if strings.ContainsAny(result, "\x1b\n\r") {
		t.Fatalf("unsafe terminal output: %q", result)
	}
}
