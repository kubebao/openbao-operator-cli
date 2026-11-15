package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAPISelectionRequiresUnambiguousDiscovery(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		native, legacy int
		want           string
	}{
		{"native", 200, 404, "completed"},
		{"ambiguous", 200, 200, "ambiguous_api_group"},
		{"denied native", 403, 200, "incomplete"},
		{"denied legacy", 200, 403, "incomplete"},
		{"missing", 404, 404, "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				group, code := "kubebao.org", tc.native
				if strings.Contains(r.URL.Path, "openbao.org") {
					group, code = "openbao.org", tc.legacy
				}
				if strings.HasSuffix(r.URL.Path, "v1alpha1") {
					if code != 200 {
						w.WriteHeader(code)
						return
					}
					_, _ = fmt.Fprintf(
						w,
						`{"apiVersion":"v1",
"kind":"APIResourceList",
"groupVersion":%q,
"resources":[{"name":"openbaoclusters",
"kind":"OpenBaoCluster",
"namespaced":true}]}`,

						group+"/v1alpha1",
					)
					return
				}
				reads.Add(1)
				response := fixture(t).DeepCopy()
				response.SetAPIVersion(group + "/v1alpha1")
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()
			client, err := NewClient(Options{Kubeconfig: kubeconfig(t, server.URL), Name: "prod", RequestTimeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			snapshot := client.Collect(context.Background())
			if snapshot.ResultCode != tc.want {
				t.Fatalf("capture: %+v", snapshot)
			}
			if tc.want != "completed" && reads.Load() != 0 {
				t.Fatal("read target before unambiguous selection")
			}
			if tc.want == "completed" && snapshot.Target.APIGroup != "kubebao.org" {
				t.Fatal("native group not selected")
			}
		})
	}
}

func TestSelectedNativeAPINeverFallsBackAfterDeletion(t *testing.T) {
	t.Parallel()
	var discoveries, reads atomic.Int32
	object := fixture(t)
	object.SetAPIVersion("kubebao.org/v1alpha1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/kubebao.org/v1alpha1":
			discoveries.Add(1)
			_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"APIResourceList","groupVersion":"kubebao.org/v1alpha1",
			"resources":[{"name":"openbaoclusters","kind":"OpenBaoCluster","namespaced":true}]}`)
		case "/apis/openbao.org/v1alpha1":
			discoveries.Add(1)
			http.NotFound(w, r)
		case "/apis/kubebao.org/v1alpha1/namespaces/security/openbaoclusters/prod":
			if reads.Add(1) > 1 {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(object)
		default:
			t.Errorf("selected native target escaped its declared scope: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewClient(Options{Kubeconfig: kubeconfig(t, server.URL), Name: "prod", RequestTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := client.Collect(context.Background()); snapshot.ResultCode != "completed" {
		t.Fatal("initial native read failed")
	}
	snapshot := client.Collect(context.Background())
	if snapshot.ResultCode != "resource_deleted" || snapshot.Target.APIGroup != "kubebao.org" ||
		snapshot.Target.UID != "fixture-cluster-uid" || discoveries.Load() != 2 || reads.Load() != 2 {
		t.Fatalf("selected target was not pinned: %+v", snapshot)
	}
}
