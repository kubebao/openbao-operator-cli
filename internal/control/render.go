package control

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	shared "github.com/kubebao/openbao-operator-client/evidence"
	"github.com/kubebao/openbao-operator-client/operations"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	"github.com/kubebao/openbao-operator-cli/internal/render"
)

// TextOptions select the text layout.
type TextOptions struct {
	Width int
}

// action describes the requested change in operator terms.
type action struct {
	noun     string // "Pause", "Maintenance mode", "Rolling restart"
	done     string // "paused", "enabled", "requested"
	field    string
	value    string
	observed string // what an observed assessment establishes
}

func controlAction(options Options) action {
	if options.Control == operations.ControlMaintenance {
		a := action{
			noun:  "Maintenance mode",
			done:  "disabled",
			field: "spec.maintenance.enabled",
			value: strconv.FormatBool(options.Enabled),
		}
		a.observed = "The operator removed the maintenance annotation from every managed StatefulSet and Pod."
		if options.Enabled {
			a.done = "enabled"
			a.observed = "The operator applied the maintenance annotation to every managed StatefulSet and Pod."
		}
		return a
	}
	if options.Enabled {
		return action{
			noun: "Pause", done: "requested", field: "spec.paused", value: "true",
			observed: "The operator acknowledged the pause (Available=Unknown, reason Paused). Jobs that already started may continue.",
		}
	}
	return action{
		noun: "Resume", done: "requested", field: "spec.paused", value: "false",
		observed: "The operator acknowledged that reconciliation resumed. The cluster can still be unhealthy; check its status.",
	}
}

// WriteText writes a human-readable result for pause, resume, and maintenance controls.
func WriteText(writer io.Writer, report Report, options Options, text TextOptions) error {
	a := controlAction(options)
	v := view{target: report.Target, text: text, action: a, result: report.ResultCode, submission: report.Submission}
	v.assessment = report.Assessment
	for _, warning := range report.Warnings {
		if warning == "maintenance_propagation_blocked_paused" {
			v.pausedWarning = true
		}
	}
	return v.write(writer, nil)
}

// WriteRestartText writes a human-readable result for a rolling restart request.
func WriteRestartText(writer io.Writer, report RestartReport, text TextOptions) error {
	a := action{
		noun: "Rolling restart", done: "requested", field: "spec.runtime.restartAt",
		observed: "Every managed StatefulSet and Pod runs with the new restart marker and reports Ready. " +
			"This does not prove serving health or Raft quorum.",
	}
	if report.Plan != nil {
		a.value = report.Plan.RestartAt
	}
	v := view{
		target: report.Target, text: text, action: a, result: report.ResultCode, submission: report.Submission,
		assessment: report.Assessment,
	}
	extra := []render.Field{}
	if report.Assessment != nil {
		extra = append(extra,
			render.Field{Label: "Marker configured", Value: boolean(report.Assessment.Configured)},
			render.Field{Label: "Rollout converged", Value: boolean(report.Assessment.OperatorObserved)})
	}
	return v.write(writer, extra)
}

type view struct {
	target        evidence.Target
	text          TextOptions
	action        action
	result        string
	submission    *operations.Result
	assessment    *operations.Assessment
	pausedWarning bool
}

func (v view) write(writer io.Writer, extra []render.Field) error {
	if v.text.Width <= 0 {
		v.text.Width = 100
	}
	tone, headline, notes, next := v.outcome()
	var b strings.Builder
	b.WriteString(render.Status(tone, headline) + "\n")
	for _, note := range notes {
		b.WriteString(render.Wrap(note, v.text.Width, "  ") + "\n")
	}
	for _, s := range next {
		if s.command == "" {
			b.WriteString("  " + render.Next(s.text) + "\n")
		} else {
			b.WriteString(render.NextCommand(s.text, s.command, "  "))
		}
	}
	fields := []render.Field{{Label: "Cluster", Value: "OpenBaoCluster " + v.target.Namespace + "/" + v.target.Name +
		render.Faint(" · "+orUnknown(v.target.APIGroup))}}
	if v.action.value != "" {
		fields = append(fields, render.Field{Label: "Change", Value: v.action.field + " = " + v.action.value})
	}
	if v.submission != nil {
		fields = append(fields, render.Field{Label: "Submission", Value: submission(v.submission.Outcome)})
	}
	switch {
	case v.assessment != nil:
		value := assessmentState(v.assessment.State)
		if reasons := Reasons(v.assessment.Reasons); reasons != "" {
			value += render.Faint(" · " + reasons)
		}
		fields = append(fields, render.Field{Label: "Observation", Value: value})
	case v.submission != nil && v.submission.Outcome != operations.OutcomeDryRunAccepted:
		fields = append(fields, render.Field{Label: "Observation", Value: render.Faint("not checked (use --wait)")})
	}
	fields = append(fields, extra...)
	b.WriteString("\n" + render.Fields(fields, "  "))
	if _, err := io.WriteString(writer, b.String()); err != nil {
		return fmt.Errorf("write control result: %w", err)
	}
	return nil
}

// step is a suggested next action, optionally with a command to copy.
type step struct {
	text    string
	command string
}

// outcome maps result codes to a verdict, explanation, and next steps. Codes remain in JSON.
func (v view) outcome() (render.Tone, string, []string, []step) {
	name, a := v.target.Name, v.action
	status := step{"Check the current state:", v.command("kubebao status")}
	resume := step{"Resume reconciliation when ready; kubebao never resumes implicitly:", v.command("kubebao resume")}
	switch v.result {
	case "dry_run_accepted":
		return render.Good, "Dry run: the API server would accept " + a.field + " = " + a.value,
			[]string{"Nothing was changed."}, nil
	case "accepted":
		notes := []string{"The API server accepted the change. The operator's response was not checked."}
		next := []step{{"Wait for the operator next time with --wait, or check the current state:", v.command("kubebao status")}}
		if v.pausedWarning {
			notes = append(
				notes,
				render.Paint(render.Warn, "Reconciliation is paused, so the operator cannot apply maintenance until it resumes."),
			)
			next = []step{resume}
		}
		return render.Good, a.noun + " " + a.done + " for " + name, notes, next
	case "observed":
		notes := []string{a.observed}
		if v.submission != nil && v.submission.Outcome == operations.OutcomeUnknown {
			notes = append(notes, "The original submission outcome was unknown; later evidence shows the requested state.")
		}
		return render.Good, a.noun + " confirmed for " + name, notes, nil
	case "observation_blocked":
		notes := []string{"The API server accepted the change, but the operator cannot apply it yet."}
		next := []step{{"Find the blocking restriction:", v.command("kubebao status")}}
		if v.pausedWarning || v.hasReason(operations.ReasonPaused) {
			notes = append(notes, "Reconciliation is paused.")
			next = []step{resume}
		}
		return render.Warn, a.noun + " " + a.done + " for " + name + ", but not yet applied", notes, next
	case "wait_incomplete":
		return render.Unknown, "Stopped waiting before the operator confirmed the change",
			[]string{"The change was submitted. The wait ended at the timeout or was interrupted; nothing was undone."},
			[]step{{"Check progress:", v.command("kubebao status")}}
	case "assessment_incomplete":
		return render.Unknown, "Submitted, but the result could not be assessed",
			[]string{"The change was submitted. Later evidence was incomplete, so the operator's response is unknown."},
			[]step{status}
	case "unknown":
		return render.Unknown, "Submission outcome unknown",
			[]string{"The request may or may not have been applied. It was not retried."},
			[]step{{"Check the current state before trying again:", v.command("kubebao status")}}
	case "denied":
		return render.Bad, a.noun + " denied by the API server",
			[]string{"Your credentials lack permission. Controls need get and patch on openbaoclusters."}, nil
	case "conflict":
		return render.Bad, "Not applied: the cluster changed after it was read",
			[]string{"The request was guarded by UID, resource version, and generation, and was not retried."},
			[]step{{text: "Run the command again to act on the current state."}}
	case "invalid":
		return render.Bad, "Rejected by the API server as invalid",
			[]string{"Validation or a patch precondition failed (HTTP 422). The request was not retried."},
			[]step{status}
	case "rate_limited":
		return render.Bad, "Rate limited by the API server", []string{"The request was not retried."},
			[]step{{text: "Try again later."}}
	case "not_submitted":
		return render.Unknown, "Request not submitted", []string{"The request was cancelled before it was sent."}, nil
	case "restart_blocked":
		return render.Bad, "Restart not submitted: a restriction blocks it",
			[]string{"Pause, a restore hold, an operation lock, break glass, or a blue/green upgrade prevents a rolling restart."},
			[]step{{"See which restriction is active:", v.command("kubebao status")}}
	case "invalid_control_evidence", "invalid_restart_evidence":
		return render.Bad, "Not submitted: the cluster evidence is not safe to act on",
			[]string{"The cluster is being deleted, has unacknowledged changes, or reports fields kubebao cannot interpret."},
			[]step{status}
	case "invalid_control_request", "invalid_restart_request":
		return render.Bad, "Not submitted: the request could not be built", nil, nil
	default:
		if v.submission == nil {
			notes := []string{"OpenBaoCluster " + name + " " + render.TargetResult(v.result) + ". Nothing was submitted."}
			var next []step
			if v.result == "not_found" {
				next = []step{{"Check the namespace and context, or list clusters:", "kubectl get openbaoclusters -A"}}
			}
			return render.Bad, "Could not read the cluster", notes, next
		}
		return render.Unknown, "Result: " + render.Humanize(v.result), nil, []step{status}
	}
}

func (v view) hasReason(reason operations.AssessmentReason) bool {
	if v.assessment == nil {
		return false
	}
	for _, r := range v.assessment.Reasons {
		if r == reason {
			return true
		}
	}
	return false
}

// command builds a follow-up command for the same cluster, context, and namespace.
func (v view) command(base string) string {
	parts := []string{base, v.target.Name}
	if v.target.Namespace != "" {
		parts = append(parts, "-n", v.target.Namespace)
	}
	if v.target.Context != "" {
		parts = append(parts, "--context", v.target.Context)
	}
	return strings.Join(parts, " ")
}

func submission(outcome operations.Outcome) string {
	switch outcome {
	case operations.OutcomeAccepted:
		return render.Paint(render.Good, "accepted by the API server")
	case operations.OutcomeDryRunAccepted:
		return render.Paint(render.Good, "accepted in dry run; not persisted")
	case operations.OutcomeUnknown:
		return render.Paint(render.Unknown, "outcome unknown; not retried")
	case operations.OutcomeNotSubmitted:
		return render.Paint(render.Unknown, "not submitted")
	default:
		return render.Paint(render.Bad, render.Humanize(string(outcome)))
	}
}

func assessmentState(state operations.AssessmentState) string {
	switch state {
	case operations.AssessmentObserved:
		return render.Paint(render.Good, "confirmed by later evidence")
	case operations.AssessmentPending:
		return render.Paint(render.Warn, "pending")
	case operations.AssessmentBlocked:
		return render.Paint(render.Bad, "blocked")
	default:
		return render.Paint(render.Unknown, "not enough evidence")
	}
}

// Reasons explains assessment reasons in a sentence fragment.
func Reasons(reasons []operations.AssessmentReason) string {
	phrases := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		phrases = append(phrases, reasonPhrase(reason))
	}
	return strings.Join(phrases, "; ")
}

func reasonPhrase(reason operations.AssessmentReason) string {
	switch reason {
	case operations.ReasonTargetChanged:
		return "the cluster changed"
	case operations.ReasonTargetDeleting:
		return "the cluster is being deleted"
	case operations.ReasonStaleEvidence:
		return "evidence is stale"
	case operations.ReasonInconsistentTime:
		return "reported times are inconsistent"
	case operations.ReasonMissingField:
		return "a required field is not reported"
	case operations.ReasonInvalidField:
		return "a field cannot be interpreted"
	case operations.ReasonGeneration:
		return "the operator has not observed the change yet"
	case operations.ReasonNotConfigured:
		return "the requested value is not configured"
	case operations.ReasonPauseCondition:
		return "the pause condition is not confirmed"
	case operations.ReasonPaused:
		return "reconciliation is paused"
	case operations.ReasonReadNotRequested, operations.ReasonReadIncomplete:
		return "workload reads are incomplete"
	case operations.ReasonScopeRestricted:
		return "the read scope was restricted"
	case operations.ReasonTargetUnverified:
		return "the cluster could not be verified again"
	case operations.ReasonAssociation:
		return "workload ownership is unconfirmed"
	case operations.ReasonAnnotationPending:
		return "maintenance annotations are still propagating"
	case operations.ReasonWorkloadMissing:
		return "an expected workload is missing"
	case operations.ReasonWorkloadDeleting:
		return "a workload is being deleted"
	case operations.ReasonTopologyUnconfirmed, operations.ReasonTopologyAmbiguous:
		return "the workload topology is unconfirmed"
	case operations.ReasonTopologyConflict:
		return "the workload topology conflicts"
	case operations.ReasonRestartBlocked:
		return "a restriction blocks the restart"
	case operations.ReasonRestartStrategy:
		return "the update strategy does not support observing a restart"
	case operations.ReasonRestartMarker:
		return "the restart marker has not reached every workload"
	case operations.ReasonRolloutRevision:
		return "the rollout is still replacing Pods"
	case operations.ReasonRolloutReadiness:
		return "replaced Pods are not Ready yet"
	default:
		return render.Humanize(string(reason))
	}
}

func boolean(value shared.Boolean) string {
	if value.State == shared.ValueReported && value.Value != nil {
		if *value.Value {
			return render.Paint(render.Good, "yes")
		}
		return render.Paint(render.Warn, "no")
	}
	if value.State == "" || value.State == shared.ValueReported {
		return render.Paint(render.Unknown, "unknown")
	}
	return render.Paint(render.Unknown, render.Humanize(string(value.State)))
}

func orUnknown(value string) string {
	if value == "" {
		return "API group unknown"
	}
	return value
}
