package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	shared "github.com/kubebao/openbao-operator-client/evidence"
	"github.com/kubebao/openbao-operator-client/operations"

	"github.com/kubebao/openbao-operator-cli/internal/collect"
	"github.com/kubebao/openbao-operator-cli/internal/control"
)

type restartRuntimeConfig struct {
	kubeconfig, context, namespace, cluster, targetUID string
}

// This opt-in test mutates only the declared disposable Kind fixture. The CLI
// performs the restart; the harness separately proves actual Pod UID replacement.
func TestOperatorRestartAndRollout(t *testing.T) {
	config := readRestartRuntimeConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	client, err := collect.NewClient(collect.Options{
		Kubeconfig: config.kubeconfig, Context: config.context, Namespace: config.namespace,
		APIGroup: "kubebao.org", Name: config.cluster, RequestTimeout: 15 * time.Second,
	})
	if err != nil {
		t.Fatal("configure the declared disposable restart fixture")
	}
	before, beforePods := restartRuntimeSnapshot(t, ctx, client, config)
	flags := []string{
		"--kubeconfig", config.kubeconfig, "--context", config.context, "--namespace", config.namespace,
		"--api-group", "kubebao.org", "--request-timeout", "15s", "--output", "json",
	}
	dry := executeRuntimeRestart(t, ctx, append([]string{"restart", config.cluster, "--dry-run"}, flags...))
	if dry.Submission == nil || dry.Submission.Outcome != operations.OutcomeDryRunAccepted || dry.Assessment != nil ||
		dry.Plan == nil || dry.Plan.Target.Identity.UID != config.targetUID || dry.Submission.Requests != 1 {
		t.Fatal("server dry-run did not retain hypothetical acceptance for the declared UID")
	}
	afterDry, unchangedPods := restartRuntimeSnapshot(t, ctx, client, config)
	if !sameRuntimeText(before.Runtime.RestartAt, afterDry.Runtime.RestartAt) || !sameRuntimePods(beforePods, unchangedPods) {
		t.Fatal("server dry-run changed the persisted restart marker or Pod identities")
	}
	report := executeRuntimeRestart(t, ctx, append([]string{
		"restart", config.cluster, "--wait", "--wait-timeout", "5m", "--poll-interval", "1s",
	}, flags...))
	if report.Submission == nil || report.Submission.Outcome != operations.OutcomeAccepted || report.Submission.Requests != 1 ||
		report.Assessment == nil || report.Assessment.State != operations.AssessmentObserved || report.Plan == nil ||
		report.Plan.Target.Identity.UID != config.targetUID || report.Plan.RestartAt == dry.Plan.RestartAt {
		t.Fatal("the declared fixture did not reach observed rollout after one guarded restart")
	}
	after, afterPods := restartRuntimeSnapshot(t, ctx, client, config)
	if after.Runtime.RestartAt.Value == nil || *after.Runtime.RestartAt.Value != report.Plan.RestartAt ||
		len(afterPods) != len(beforePods) {
		t.Fatal("final target marker or workload population differs from the observed restart")
	}
	for name, uid := range beforePods {
		if afterPods[name] == "" || afterPods[name] == uid {
			t.Fatal("the requested restart did not replace every original UID-bound Pod")
		}
	}
	t.Logf("group=kubebao.org targetUID=%s pods=%d all-original-UIDs-replaced=true submission=%s assessment=%s",
		config.targetUID, len(beforePods), report.Submission.Outcome, report.Assessment.State)
}

func readRestartRuntimeConfig(t *testing.T) restartRuntimeConfig {
	t.Helper()
	if os.Getenv("KUBEBAO_TEST_RESTART_ALLOW_MUTATION") != "true" {
		t.Skip("set KUBEBAO_TEST_RESTART_ALLOW_MUTATION=true and the declared disposable Kind fixture to qualify restart")
	}
	value := func(suffix string) string {
		name := "KUBEBAO_TEST_RESTART_" + suffix
		result := os.Getenv(name)
		if result == "" {
			t.Fatalf("%s must identify the disposable restart fixture", name)
		}
		return result
	}
	config := restartRuntimeConfig{
		kubeconfig: value("KUBECONFIG"), context: value("CONTEXT"), namespace: value("NAMESPACE"),
		cluster: value("CLUSTER"), targetUID: value("TARGET_UID"),
	}
	if !strings.HasPrefix(config.context, "kind-") {
		t.Fatal("restart qualification requires an explicit disposable Kind context")
	}
	if (shared.Identity{
		Origin: "restart-qualification", Group: "kubebao.org", Kind: "OpenBaoCluster", Namespace: config.namespace,
		Name: config.cluster, UID: config.targetUID,
	}).Validate() != nil {
		t.Fatal("restart qualification requires valid declared target identity")
	}
	return config
}

func executeRuntimeRestart(t *testing.T, ctx context.Context, args []string) control.RestartReport {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Execute(ctx, args, &stdout, &stderr)
	var report control.RestartReport
	if json.Unmarshal(stdout.Bytes(), &report) != nil || code != 0 || report.ExitCode != 0 {
		t.Fatalf("declared restart qualification failed: exit=%d result=%s", code, report.ResultCode)
	}
	return report
}

func restartRuntimeSnapshot(t *testing.T, ctx context.Context, client *collect.Client,
	config restartRuntimeConfig,
) (shared.Cluster, map[string]string) {
	t.Helper()
	snapshot := client.Collect(ctx)
	if snapshot.ResultCode != "completed" || snapshot.NormalizedEvidence == nil || len(snapshot.NormalizedEvidence.Clusters) != 1 ||
		snapshot.NormalizedEvidence.Clusters[0].Observation == nil || snapshot.Target.UID != config.targetUID {
		t.Fatal("the declared restart target UID was not collected")
	}
	cluster := *snapshot.NormalizedEvidence.Clusters[0].Observation
	related, err := client.ControlWorkloads(ctx, cluster)
	if err != nil || related.Outcome != shared.CollectionComplete || related.Verification.State != shared.VerificationVerified ||
		related.Pods == nil || related.StatefulSets == nil {
		t.Fatal("the disposable restart fixture lacks complete verified workload reads")
	}
	parents := map[shared.Identity]bool{}
	for _, association := range related.Associations {
		if association.State == shared.AssociationConfirmed && association.Subject.Kind == "StatefulSet" &&
			association.Target == cluster.Reference.Identity {
			parents[association.Subject] = true
		}
	}
	pods := map[string]string{}
	for _, association := range related.Associations {
		if association.State == shared.AssociationConfirmed && association.Subject.Kind == "Pod" &&
			parents[association.Target] {
			pods[association.Subject.Name] = association.Subject.UID
		}
	}
	if len(pods) == 0 {
		t.Fatal("the disposable restart fixture has no UID-confirmed Pods")
	}
	return cluster, pods
}

func sameRuntimeText(left, right shared.Text) bool {
	return left.State == right.State && (left.Value == nil && right.Value == nil ||
		left.Value != nil && right.Value != nil && *left.Value == *right.Value)
}

func sameRuntimePods(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for name, uid := range left {
		if right[name] != uid {
			return false
		}
	}
	return true
}
