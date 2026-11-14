package status

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	"github.com/kubebao/openbao-operator-cli/internal/render"
)

func ptr[T any](value T) *T { return &value }

func snapshot() evidence.Snapshot {
	at := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	return evidence.Snapshot{
		ResultCode: "completed", CompletedAt: at,
		Target: evidence.Target{Context: "prod-eu", Namespace: "security", Name: "vault", APIVersion: "openbao.org/v1alpha1"},
		Observation: &evidence.Observation{
			CollectedAt: at, Qualification: "unqualified", Generation: ptr(int64(4)), ObservedGeneration: ptr(int64(4)),
			GenerationCurrent: ptr(true), DesiredVersion: "2.6.4", ObservedVersion: "2.6.4",
			Voters:    evidence.Replicas{Desired: ptr(int64(3)), Ready: ptr(int64(3))},
			Available: &evidence.Condition{Type: "Available", Status: "True", Reason: "AllReplicasReady", GenerationCurrent: ptr(true)},
			Paused:    ptr(false), MaintenanceEnabled: ptr(false),
			Backup: &evidence.Backup{LastBackupTime: "2026-11-14T09:30:00Z", ConsecutiveFailures: ptr(int64(0))},
		},
	}
}

func text(t *testing.T, report evidence.Snapshot, options TextOptions) string {
	t.Helper()
	var buffer bytes.Buffer
	if err := WriteText(render.Plain(&buffer), report, options); err != nil {
		t.Fatal(err)
	}
	return buffer.String()
}

func TestQuietClusterShowsOnlyReportedFacts(t *testing.T) {
	t.Parallel()
	got := text(t, snapshot(), TextOptions{})
	for _, want := range []string{
		"OpenBaoCluster vault  namespace security · context prod-eu",
		"✓ No issues reported by the operator",
		"Voters          3 of 3 ready",
		"Last backup     2026-11-14T09:30:00Z (2h 30m ago)",
		"Reconciliation  active",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	for _, hidden := range []string{"Read replicas", "Restore hold", "Operation lock", "Break glass", "Interpretation limits", "\x1b["} {
		if strings.Contains(got, hidden) {
			t.Fatalf("default view shows %q:\n%s", hidden, got)
		}
	}
	report := snapshot()
	report.Observation.Limitations = []string{"Collection time is not an operator heartbeat."}
	verbose := squeeze(text(t, report, TextOptions{Verbose: true}))
	for _, want := range []string{"Read replicas", "Restore hold not reported", "Operation lock not reported", "Interpretation limits"} {
		if !strings.Contains(verbose, want) {
			t.Fatalf("verbose view omits %q:\n%s", want, verbose)
		}
	}
}

func TestAttentionAndRestrictionsAreProminent(t *testing.T) {
	t.Parallel()
	report := snapshot()
	o := report.Observation
	o.Attention = []evidence.Attention{
		{Code: "KBAO-RESTRICTION-001", Summary: "Reconciliation is paused."},
		{Code: "KBAO-CLUSTER-002_incomplete", Summary: "ignored in text"},
	}
	o.Paused, o.RestoreHold = ptr(true), "restore-1"
	o.ObservedVersion, o.Voters.Ready = "2.6.3", ptr(int64(2))
	o.Backup.ConsecutiveFailures, o.Backup.LastFailureReason = ptr(int64(3)), "UploadFailed"
	got := squeeze(text(t, report, TextOptions{}))
	for _, want := range []string{
		"! Needs attention",
		"! Reconciliation is paused.",
		"? Sealed state: not enough evidence to evaluate KBAO-CLUSTER-002",
		"kubebao doctor vault -n security --context prod-eu",
		"2.6.3 running · 2.6.4 desired",
		"2 of 3 ready",
		"Reconciliation paused",
		"Restore hold restore-1",
		"3 consecutive failures · UploadFailed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRejectedEvidenceReplacesTheVerdict(t *testing.T) {
	t.Parallel()
	got := text(t, snapshot(), TextOptions{ValidationPath: "clusters[0].observation.generation"})
	if !strings.Contains(got, "✗ Reported fields cannot be interpreted") || !strings.Contains(got, "clusters[0].observation.generation") ||
		strings.Contains(got, "No issues reported") {
		t.Fatalf("rejected evidence:\n%s", got)
	}
	missing := evidence.Snapshot{ResultCode: "resource_replaced", Target: evidence.Target{Namespace: "security", Name: "vault"}}
	if got := text(t, missing, TextOptions{}); !strings.Contains(got, "The cluster was replaced by a resource with a different UID.") {
		t.Fatalf("unavailable status:\n%s", got)
	}
}

// squeeze collapses column padding so assertions do not depend on label widths.
func squeeze(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return strings.Join(lines, "\n")
}
