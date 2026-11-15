package collect

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	"github.com/kubebao/openbao-operator-client/adapter"
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

func TestSharedProjectionMapsUnknownAndReportedFields(t *testing.T) {
	captured := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	data, err := fixture(t).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := adapter.ProjectCluster(
		data,
		adapter.ProjectionOptions{
			Origin:      "fixture",
			CollectedAt: captured,
			Contract:    adapter.OpenBaoV1Alpha1,
			Limits:      adapter.DefaultProjectionLimits(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	o := projectCluster(normalized)
	if o.GenerationCurrent == nil || !*o.GenerationCurrent || o.Available.GenerationCurrent == nil || *o.Available.GenerationCurrent {
		t.Fatal("resource and condition generations conflated")
	}
	if !o.CollectedAt.Equal(captured) || o.Available.LastTransitionTime != "2026-11-09T10:00:00Z" {
		t.Fatal("collection and transition times conflated")
	}
	if o.ReadReplicas.Ready == nil || *o.ReadReplicas.Ready != 0 || o.Paused == nil || *o.Paused {
		t.Fatal("reported zero or false was lost")
	}
	encoded, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"EXCLUDED_FIELD_CANARY", "FIXTURE_CREDENTIAL", "Two voters are ready", "[REDACTED]"} {
		if bytes.Contains(encoded, []byte(value)) {
			t.Fatalf("provider or unapproved text reached output: %s", value)
		}
	}
	if o.WorkloadError == nil || o.WorkloadError.Message != "" || o.WorkloadError.Reason != "WorkloadBlocked" {
		t.Fatal("structured error reason lost or message retained")
	}
}

func TestLocalKubeconfigDisplayMetadataIsBounded(t *testing.T) {
	for _, value := range []string{"Bearer PRIVATE_VALUE", "password=PRIVATE_VALUE", "line\nPRIVATE_VALUE"} {
		got := evidence.Clean(value)
		if strings.Contains(got, "\n") || strings.Contains(got, "password=PRIVATE_VALUE") || strings.Contains(got, "Bearer PRIVATE_VALUE") {
			t.Fatal("unbounded local metadata")
		}
	}
}
