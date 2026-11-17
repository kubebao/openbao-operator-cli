package control

import (
	"context"
	"errors"
	"time"

	"github.com/kubebao/openbao-operator-client/operations"

	"github.com/kubebao/openbao-operator-cli/internal/collect"
	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

// RestartReport separates a restart request from later rollout observations.
// An observed rollout does not attribute Pod replacement to this request.
type RestartReport struct {
	ReadOnly         bool                         `json:"readOnly,omitempty"`
	RequestFile      string                       `json:"-"`
	SchemaVersion    string                       `json:"schemaVersion"`
	Target           evidence.Target              `json:"target"`
	ResultCode       string                       `json:"resultCode"`
	ExitCode         int                          `json:"exitCode"`
	Plan             *operations.RestartPlan      `json:"plan,omitempty"`
	Submission       *operations.Result           `json:"submission,omitempty"`
	Assessment       *operations.Assessment       `json:"assessment,omitempty"`
	AssessmentPolicy *operations.AssessmentPolicy `json:"assessmentPolicy,omitempty"`
	Collection       []evidence.Step              `json:"collection"`
}

// RunRestart submits one marker once. Waiting only reads the selected group and UID.
func RunRestart(ctx context.Context, client *collect.Client, options Options) RestartReport {
	snapshot := client.Collect(ctx)
	report := RestartReport{
		SchemaVersion: "v1", Target: snapshot.Target, ResultCode: snapshot.ResultCode, Collection: snapshot.Collection,
	}
	cluster, ok := observed(snapshot)
	if !ok {
		report.ExitCode = 3
		if snapshot.ResultCode == "not_found" || snapshot.ResultCode == "ambiguous_api_group" {
			report.ExitCode = 1
		}
		return report
	}
	marker := time.Now().UTC().Format(time.RFC3339Nano)
	plan, err := operations.PrepareRestart(cluster, client.Contract(), marker)
	if err != nil {
		report.ResultCode, report.ExitCode = "invalid_restart_evidence", 3
		if errors.Is(err, operations.ErrRestartBlocked) {
			report.ResultCode = "restart_blocked"
		}
		return report
	}
	report.Plan = &plan
	if options.RequestFile != "" {
		request := Request{FormatVersion: requestVersion, Restart: &plan}
		if options.DryRun || WriteRequest(ctx, options.RequestFile, request) != nil {
			report.ResultCode, report.ExitCode = "request_not_saved", 1
			return report
		}
		report.RequestFile = options.RequestFile
	}
	submit := operations.DefaultSubmitOptions()
	submit.DryRun, submit.Timeout = options.DryRun, options.RequestTimeout
	result, err := client.ControlClient().SubmitRestart(ctx, plan, submit)
	if err != nil {
		report.ResultCode, report.ExitCode = "invalid_restart_request", 1
		return report
	}
	report.Submission = &result
	report.ResultCode, report.ExitCode = string(result.Outcome), submissionCode(result.Outcome)
	if !options.Wait || result.Outcome != operations.OutcomeAccepted && result.Outcome != operations.OutcomeUnknown {
		return report
	}
	return waitRestart(ctx, client, options, report)
}

func waitRestart(ctx context.Context, client *collect.Client, options Options, report RestartReport) RestartReport {
	ctx, cancel := context.WithTimeout(ctx, options.WaitTimeout)
	defer cancel()
	for {
		if ctx.Err() != nil {
			report.ResultCode, report.ExitCode = "wait_incomplete", 3
			return report
		}
		report.Assessment, report.AssessmentPolicy = nil, nil
		snapshot := client.Collect(ctx)
		report.Target, report.Collection = snapshot.Target, snapshot.Collection
		cluster, ok := observed(snapshot)
		if !ok {
			report.ResultCode, report.ExitCode = snapshot.ResultCode, 3
			return report
		}
		related, err := client.ControlWorkloads(ctx, cluster)
		if err != nil {
			report.ResultCode, report.ExitCode = "assessment_incomplete", 3
			return report
		}
		policy := operations.DefaultAssessmentPolicy(time.Now().UTC())
		assessment, err := operations.AssessRestart(*report.Plan, cluster, related, policy)
		if err != nil {
			report.ResultCode, report.ExitCode = "assessment_incomplete", 3
			return report
		}
		report.Assessment, report.AssessmentPolicy = &assessment, &policy
		if assessment.State == operations.AssessmentObserved {
			report.ResultCode, report.ExitCode = "observed", 0
			return report
		}
		if assessment.State == operations.AssessmentBlocked {
			report.ResultCode, report.ExitCode = "observation_blocked", 3
			return report
		}
		if !options.Wait {
			report.ResultCode, report.ExitCode = "observation_incomplete", 3
			return report
		}
		if options.Observe != nil {
			options.Observe(assessment)
		}
		timer := time.NewTimer(options.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			report.ResultCode, report.ExitCode = "wait_incomplete", 3
			return report
		case <-timer.C:
		}
	}
}
