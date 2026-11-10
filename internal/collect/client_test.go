package collect

import (
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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func kubeconfig(t *testing.T, server string) string {
	t.Helper()
	content := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: fixture
clusters:
- name: fixture-cluster
  cluster:
    server: %s
contexts:
- name: fixture
  context:
    cluster: fixture-cluster
    namespace: security
    user: fixture
users:
- name: fixture
  user: {}
`, server)
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCollectionOutcomesAndGroupSelection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, group, wantResult string
		otherCode, wantCode     int
	}{
		{"absent alternate API", "", "completed", 404, 0},
		{"denied alternate API", "", "incomplete", 403, 3},
		{"failed alternate API", "", "incomplete", 500, 3},
		{"ambiguous target", "", "ambiguous_api_group", 200, 1},
		{"selected group", "openbao.org", "completed", 403, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := collectionServer(t, tc.otherCode, tc.group != "")
			client, err := NewClient(
				Options{Kubeconfig: kubeconfig(t, server.URL), Name: "prod", APIGroup: tc.group, RequestTimeout: time.Second},
			)
			if err != nil {
				t.Fatal(err)
			}
			report := client.Collect(context.Background())
			if report.ExitCode != tc.wantCode || report.ResultCode != tc.wantResult {
				t.Fatalf(
					"result = %s/%d, want %s/%d; steps: %+v",
					report.ResultCode,
					report.ExitCode,
					tc.wantResult,
					tc.wantCode,
					report.Collection,
				)
			}
			if tc.wantCode != 1 && (report.Observation == nil || report.Target.UID != "fixture-cluster-uid") {
				t.Fatal("usable target evidence was discarded")
			}
			encoded, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "PROVIDER_SECRET_CANARY") {
				t.Fatal("raw provider error reached output")
			}
		})
	}
}

func TestRefreshPinsUIDAndAvoidsRediscovery(t *testing.T) {
	t.Parallel()
	var reads, discoveries atomic.Int32
	object := fixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/apis/openbao.org/v1alpha1" {
			discoveries.Add(1)
			_ = json.NewEncoder(w).Encode(metav1.APIResourceList{
				APIResources: []metav1.APIResource{{Name: "openbaoclusters", Kind: "OpenBaoCluster", Namespaced: true}},
			})
			return
		}
		response := object.DeepCopy()
		if reads.Add(1) > 1 {
			response.SetUID("replacement-uid")
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	client, err := NewClient(Options{
		Kubeconfig: kubeconfig(t, server.URL), Name: "prod", APIGroup: "openbao.org", RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report := client.Collect(context.Background()); report.ExitCode != 0 {
		t.Fatalf("initial collection: %+v", report)
	}
	report := client.Collect(context.Background())
	if report.ExitCode != 3 || report.ResultCode != "resource_replaced" || report.Observation != nil ||
		report.Target.UID != "fixture-cluster-uid" || discoveries.Load() != 1 {
		t.Fatalf("resource identity was not pinned: %+v", report)
	}
	if report.Collection[0].UID != "replacement-uid" {
		t.Fatal("replacement identity was not recorded")
	}
}

func TestCollectionTimeoutRemainsIncomplete(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	client, err := NewClient(Options{
		Kubeconfig: kubeconfig(t, server.URL), Name: "prod", APIGroup: "openbao.org", RequestTimeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	report := client.Collect(context.Background())
	if report.ExitCode != 3 || report.ResultCode != "incomplete" || report.Observation != nil {
		t.Fatalf("timed-out collection became a complete observation: %+v", report)
	}
}

func TestRefreshReportsDeletion(t *testing.T) {
	t.Parallel()
	var reads atomic.Int32
	object := fixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/apis/openbao.org/v1alpha1" {
			_ = json.NewEncoder(w).Encode(metav1.APIResourceList{
				APIResources: []metav1.APIResource{{Name: "openbaoclusters", Kind: "OpenBaoCluster", Namespaced: true}},
			})
			return
		}
		if reads.Add(1) > 1 {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(object)
	}))
	defer server.Close()
	client, err := NewClient(Options{
		Kubeconfig: kubeconfig(t, server.URL), Name: "prod", APIGroup: "openbao.org", RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report := client.Collect(context.Background()); report.ExitCode != 0 {
		t.Fatalf("initial collection: %+v", report)
	}
	report := client.Collect(context.Background())
	if report.ExitCode != 3 || report.ResultCode != "resource_deleted" || report.Target.UID != "fixture-cluster-uid" {
		t.Fatalf("deletion was not bound to the selected resource: %+v", report)
	}
}

func collectionServer(t *testing.T, otherCode int, explicit bool) *httptest.Server {
	t.Helper()
	object := fixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		group := "openbao.org"
		if strings.Contains(r.URL.Path, "kubebao.org") {
			group = "kubebao.org"
			if explicit {
				t.Error("explicit API selection queried another group")
			}
			if otherCode != 200 {
				w.WriteHeader(otherCode)
				_, _ = fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","message":"PROVIDER_SECRET_CANARY"}`)
				return
			}
		}
		if r.URL.Path == "/apis/"+group+"/v1alpha1" {
			_ = json.NewEncoder(w).Encode(metav1.APIResourceList{
				GroupVersion: group + "/v1alpha1",
				APIResources: []metav1.APIResource{{Name: "openbaoclusters", Kind: "OpenBaoCluster", Namespaced: true}},
			})
			return
		}
		if r.URL.Path != "/apis/"+group+"/v1alpha1/namespaces/security/openbaoclusters/prod" {
			t.Errorf("unexpected resource read: %s", r.URL.Path)
		}
		response := object.DeepCopy()
		response.SetAPIVersion(group + "/v1alpha1")
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)
	return server
}
