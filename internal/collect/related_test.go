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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

func relatedObject(t *testing.T, content string) unstructured.Unstructured {
	t.Helper()
	var object unstructured.Unstructured
	if err := object.UnmarshalJSON([]byte(content)); err != nil {
		t.Fatal(err)
	}
	return object
}

func setFixture(t *testing.T, uid, ownerUID string) unstructured.Unstructured {
	t.Helper()
	return relatedObject(t, fmt.Sprintf(`{
	"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"namespace":"security","name":"prod","uid":%q,
	"generation":1,"resourceVersion":"100","ownerReferences":[{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",
	"name":"prod","uid":%q,"controller":true}]},"spec":{"replicas":1,"template":{"spec":{"args":["SPEC_CANARY"]}}},
	"status":{"observedGeneration":1,"readyReplicas":1}}`, uid, ownerUID))
}

func podFixture(t *testing.T, uid, ownerUID string) unstructured.Unstructured {
	t.Helper()
	return relatedObject(t, fmt.Sprintf(`{
	"apiVersion":"v1","kind":"Pod","metadata":{"namespace":"security","name":%q,"uid":%q,"resourceVersion":"101",
	"annotations":{"arbitrary":"SPEC_CANARY"},"ownerReferences":[{"apiVersion":"apps/v1","kind":"StatefulSet",
	"name":"prod","uid":%q,"controller":true}]},"spec":{"containers":[{"name":"bao","args":["SPEC_CANARY"],
	"env":[{"name":"VALUE","value":"SPEC_CANARY"}]}]},"status":{"phase":"Running",
	"conditions":[{"type":"Ready","status":"False","reason":"ContainersNotReady"}],"containerStatuses":[{"name":"bao",
	"ready":false,"restartCount":2,"state":{"waiting":{"reason":"CrashLoopBackOff",
	"message":"password=PRIVATE_VALUE"}}}]}}`, uid, uid, ownerUID))
}

func restoreFixture(t *testing.T, uid, targetUID string) unstructured.Unstructured {
	t.Helper()
	return relatedObject(t, fmt.Sprintf(`{
	"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoRestore","metadata":{"namespace":"security","name":%q,"uid":%q,
	"resourceVersion":"102","generation":1},"spec":{"cluster":"prod","source":{"key":"SPEC_CANARY"},
	"tokenSecretRef":{"name":"SPEC_CANARY"}},"status":{"phase":"Unknown","message":"token=PRIVATE_VALUE",
	"execution":{"targetUID":%q,"operationID":"restore-operation-123","stage":"Unknown"}}}`, uid, uid, targetUID))
}

type relatedScenario struct {
	sets, pods, restores []unstructured.Unstructured
	setCode              int
	continued            bool
	replace              bool
	changeGeneration     bool
}

func diagnosticServer(t *testing.T, scenario relatedScenario) *httptest.Server {
	t.Helper()
	base := fixture(t)
	var clusterReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected mutation: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/openbao.org/v1alpha1":
			_, _ = fmt.Fprint(w, `{"kind":"APIResourceList","apiVersion":"v1","resources":[
			{"name":"openbaoclusters","kind":"OpenBaoCluster","namespaced":true}]}`)
		case "/apis/openbao.org/v1alpha1/namespaces/security/openbaoclusters/prod":
			object := base.DeepCopy()
			if clusterReads.Add(1) > 1 {
				if scenario.replace {
					object.SetUID(types.UID("replacement"))
				}
				if scenario.changeGeneration {
					object.SetGeneration(8)
				}
			}
			_ = json.NewEncoder(w).Encode(object)
		case "/apis/apps/v1/namespaces/security/statefulsets":
			writeRelatedList(t, w, r, "apps/v1", "StatefulSetList", scenario.sets, scenario.setCode, scenario.continued)
		case "/api/v1/namespaces/security/pods":
			writeRelatedList(t, w, r, "v1", "PodList", scenario.pods, 0, false)
		case "/apis/openbao.org/v1alpha1/namespaces/security/openbaorestores":
			writeRelatedList(t, w, r, "openbao.org/v1alpha1", "OpenBaoRestoreList", scenario.restores, 0, false)
		default:
			t.Errorf("unexpected resource read: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func writeRelatedList(t *testing.T, w http.ResponseWriter, r *http.Request, version, kind string,
	items []unstructured.Unstructured, code int, continued bool,
) {
	t.Helper()
	if r.URL.Query().Get("limit") != "100" {
		t.Error("collection is not paginated")
	}
	if kind != "OpenBaoRestoreList" && r.URL.Query().Get("labelSelector") != "openbao.org/cluster=prod" {
		t.Error("missing candidate selector")
	}
	if code != 0 {
		w.WriteHeader(code)
		_, _ = fmt.Fprint(w, `{"kind":"Status","message":"API_BODY_CANARY"}`)
		return
	}
	list := &unstructured.UnstructuredList{Items: items}
	list.SetAPIVersion(version)
	list.SetKind(kind)
	list.SetResourceVersion("1000")
	if list.Items == nil {
		list.Items = []unstructured.Unstructured{}
	}
	if continued {
		list.SetContinue("repeated-token")
		if r.URL.Query().Get("continue") != "" {
			list.Items = []unstructured.Unstructured{}
		}
	}
	_ = json.NewEncoder(w).Encode(list)
}

func collectScenario(t *testing.T, scenario relatedScenario) evidence.Snapshot {
	t.Helper()
	server := diagnosticServer(t, scenario)
	client, err := NewClient(
		Options{Kubeconfig: kubeconfig(t, server.URL), APIGroup: "openbao.org", Name: "prod", RequestTimeout: 5 * time.Second},
	)
	if err != nil {
		t.Fatal(err)
	}
	return client.CollectDiagnostic(context.Background())
}

func TestDiagnosticAssociationAndProjection(t *testing.T) {
	t.Parallel()
	snapshot := collectScenario(t, relatedScenario{
		sets: []unstructured.Unstructured{
			setFixture(t, "own-set", "fixture-cluster-uid"),
			setFixture(t, "foreign-set", "old-cluster-uid"),
		},
		pods: []unstructured.Unstructured{podFixture(t, "own-pod", "own-set"), podFixture(t, "foreign-pod", "foreign-set")},
		restores: []unstructured.Unstructured{
			restoreFixture(t, "own-restore", "fixture-cluster-uid"),
			restoreFixture(t, "foreign-restore", "old-cluster-uid"),
		},
	})
	if snapshot.ExitCode != 0 || len(snapshot.Related.StatefulSets) != 1 || len(snapshot.Related.Pods) != 1 ||
		len(snapshot.Related.Restores) != 1 {
		t.Fatalf("ownership correlation failed: %+v", snapshot)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"SPEC_CANARY", "PRIVATE_VALUE", "foreign-restore", "foreign-pod"} {
		if strings.Contains(string(encoded), value) {
			t.Fatalf("unapproved evidence retained: %s", value)
		}
	}
	if snapshot.Related.Restores[0].Association.Basis != "status.execution.targetUID" {
		t.Fatal("restore UID proof was lost")
	}
}

func TestLabelsAndNamesDoNotProveAssociation(t *testing.T) {
	t.Parallel()
	orphan := podFixture(t, "orphan", "unknown-set")
	request := restoreFixture(t, "unbound", "")
	snapshot := collectScenario(
		t,
		relatedScenario{pods: []unstructured.Unstructured{orphan}, restores: []unstructured.Unstructured{request}},
	)
	if snapshot.ExitCode != 3 || len(snapshot.Related.Pods) != 0 || len(snapshot.Related.Restores) != 0 ||
		len(snapshot.Related.Omissions) != 2 {
		t.Fatalf("unverified ownership became associated evidence: %+v", snapshot)
	}
}

func TestPartialCollectionPreservesIndependentEvidence(t *testing.T) {
	t.Parallel()
	snapshot := collectScenario(t, relatedScenario{
		setCode:  http.StatusForbidden,
		restores: []unstructured.Unstructured{restoreFixture(t, "own-restore", "fixture-cluster-uid")},
	})
	if snapshot.ExitCode != 3 || snapshot.Observation == nil || len(snapshot.Related.Restores) != 1 {
		t.Fatalf("partial evidence discarded: %+v", snapshot)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "API_BODY_CANARY") {
		t.Fatal("API body reached output")
	}
}

func TestDiagnosticTargetChangesRemainIncomplete(t *testing.T) {
	t.Parallel()
	for _, scenario := range []relatedScenario{{replace: true}, {changeGeneration: true}} {
		snapshot := collectScenario(t, scenario)
		if snapshot.ExitCode != 3 || snapshot.Target.UID != "fixture-cluster-uid" {
			t.Fatalf("target change was accepted: %+v", snapshot)
		}
		if scenario.changeGeneration {
			last := snapshot.Collection[len(snapshot.Collection)-1]
			if last.Generation == nil || *last.Generation != 8 {
				t.Fatal("final generation was discarded")
			}
		}
	}
}

func TestConflictingRestoreIdentitiesAreNotAssociated(t *testing.T) {
	t.Parallel()
	request := restoreFixture(t, "conflict", "fixture-cluster-uid")
	if err := unstructured.SetNestedField(request.Object, "another-cluster", "spec", "cluster"); err != nil {
		t.Fatal(err)
	}
	snapshot := collectScenario(t, relatedScenario{restores: []unstructured.Unstructured{request}})
	if snapshot.ExitCode != 3 || len(snapshot.Related.Restores) != 0 || len(snapshot.Related.Omissions) != 1 {
		t.Fatalf("conflicting target evidence was accepted: %+v", snapshot)
	}
}

func TestLegacyCompletedRestoreRemainsUnassociated(t *testing.T) {
	t.Parallel()
	request := restoreFixture(t, "legacy", "")
	if err := unstructured.SetNestedField(request.Object, "Completed", "status", "phase"); err != nil {
		t.Fatal(err)
	}
	snapshot := collectScenario(t, relatedScenario{restores: []unstructured.Unstructured{request}})
	if snapshot.ExitCode != 3 || len(snapshot.Related.Restores) != 0 || len(snapshot.Related.Omissions) != 1 {
		t.Fatalf("legacy name-only association was accepted: %+v", snapshot)
	}
}

func TestDiagnosticPaginationAndLimits(t *testing.T) {
	t.Parallel()
	snapshot := collectScenario(t, relatedScenario{continued: true})
	if snapshot.ExitCode != 3 || snapshot.Collection[2].Outcome != "repeated_continue_token" {
		t.Fatalf("repeated pagination token was accepted: %+v", snapshot.Collection)
	}
	items := make([]unstructured.Unstructured, maxCandidates+1)
	for i := range items {
		items[i] = setFixture(t, fmt.Sprintf("set-%d", i), "fixture-cluster-uid")
	}
	snapshot = collectScenario(t, relatedScenario{sets: items})
	if snapshot.ExitCode != 3 || len(snapshot.Related.StatefulSets) != maxCandidates {
		t.Fatalf("candidate limit failed: %d", len(snapshot.Related.StatefulSets))
	}
}
