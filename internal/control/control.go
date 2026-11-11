// Package control submits guarded requests and waits for separately collected evidence.
package control

import (
	"context"
	"time"

	shared "github.com/kubebao/openbao-operator-client/evidence"
	"github.com/kubebao/openbao-operator-client/operations"

	"github.com/kubebao/openbao-operator-cli/internal/collect"
	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

type Options struct {
	Control        operations.Control
	Enabled        bool
	DryRun         bool
	Wait           bool
	WaitTimeout    time.Duration
	PollInterval   time.Duration
	RequestTimeout time.Duration
}

// Report separates API acceptance from observation. Assessment is absent without --wait.
type Report struct {
	SchemaVersion    string                       `json:"schemaVersion"`
	Target           evidence.Target              `json:"target"`
	ResultCode       string                       `json:"resultCode"`
	ExitCode         int                          `json:"exitCode"`
	Plan             *operations.Plan             `json:"plan,omitempty"`
	Submission       *operations.Result           `json:"submission,omitempty"`
	Assessment       *operations.Assessment       `json:"assessment,omitempty"`
	AssessmentPolicy *operations.AssessmentPolicy `json:"assessmentPolicy,omitempty"`
	Collection       []evidence.Step              `json:"collection"`
	Warnings         []string                     `json:"warnings"`
}

// Run submits at most once. A wait never retries a mutation, including after an unknown outcome.
func Run(ctx context.Context, client *collect.Client, options Options) Report {
	snapshot := client.Collect(ctx)
	report := Report{
		SchemaVersion: "v1", Target: snapshot.Target, ResultCode: snapshot.ResultCode,
		Collection: snapshot.Collection, Warnings: []string{},
	}
	cluster, ok := observed(snapshot)
	if !ok {
		report.ExitCode = 3
		if snapshot.ResultCode == "not_found" || snapshot.ResultCode == "ambiguous_api_group" {
			report.ExitCode = 1
		}
		return report
	}
	plan, err := prepare(cluster, options)
	if err != nil {
		report.ResultCode, report.ExitCode = "invalid_control_evidence", 3
		return report
	}
	report.Plan = &plan
	if options.Control == operations.ControlMaintenance && cluster.Paused.Value != nil && *cluster.Paused.Value {
		report.Warnings = append(report.Warnings, "maintenance_propagation_blocked_paused")
	}
	submit := operations.DefaultSubmitOptions()
	submit.DryRun, submit.Timeout = options.DryRun, options.RequestTimeout
	result, err := client.ControlClient().Submit(ctx, plan, submit)
	if err != nil {
		report.ResultCode, report.ExitCode = "invalid_control_request", 1
		return report
	}
	report.Submission = &result
	report.ResultCode, report.ExitCode = string(result.Outcome), submissionCode(result.Outcome)
	if !options.Wait || result.Outcome != operations.OutcomeAccepted && result.Outcome != operations.OutcomeUnknown {
		return report
	}
	return wait(ctx, client, options, report)
}

func prepare(cluster shared.Cluster, options Options) (operations.Plan, error) {
	if options.Control == operations.ControlMaintenance {
		return operations.PrepareMaintenance(cluster, options.Enabled)
	}
	return operations.PreparePause(cluster, options.Enabled)
}

func observed(snapshot evidence.Snapshot) (shared.Cluster, bool) {
	if snapshot.ResultCode != "completed" || snapshot.NormalizedEvidence == nil || len(snapshot.NormalizedEvidence.Clusters) != 1 ||
		snapshot.NormalizedEvidence.Clusters[0].Observation == nil {
		return shared.Cluster{}, false
	}
	return *snapshot.NormalizedEvidence.Clusters[0].Observation, true
}

func submissionCode(outcome operations.Outcome) int {
	switch outcome {
	case operations.OutcomeAccepted, operations.OutcomeDryRunAccepted:
		return 0
	case operations.OutcomeUnknown, operations.OutcomeNotSubmitted:
		return 3
	default:
		return 1
	}
}

func wait(ctx context.Context, client *collect.Client, options Options, report Report) Report {
	ctx, cancel := context.WithTimeout(ctx, options.WaitTimeout)
	defer cancel()
	for {
		if ctx.Err() != nil {
			report.ResultCode, report.ExitCode = "wait_incomplete", 3
			return report
		}
		snapshot := client.Collect(ctx)
		report.Collection = snapshot.Collection
		cluster, ok := observed(snapshot)
		if !ok {
			report.ResultCode, report.ExitCode = snapshot.ResultCode, 3
			return report
		}
		var assessment operations.Assessment
		var err error
		policy := operations.DefaultAssessmentPolicy(time.Now().UTC())
		if options.Control == operations.ControlMaintenance {
			related, readErr := client.ControlWorkloads(ctx, cluster)
			if readErr != nil {
				report.ResultCode, report.ExitCode = "assessment_incomplete", 3
				return report
			}
			policy = operations.DefaultAssessmentPolicy(time.Now().UTC())
			assessment, err = operations.AssessMaintenance(*report.Plan, cluster, related, policy)
		} else {
			assessment, err = operations.AssessPause(*report.Plan, cluster, policy)
		}
		if err != nil {
			report.ResultCode, report.ExitCode = "assessment_incomplete", 3
			return report
		}
		report.Assessment = &assessment
		report.AssessmentPolicy = &policy
		if assessment.State == operations.AssessmentObserved {
			report.ResultCode, report.ExitCode = "observed", 0
			return report
		}
		if assessment.State == operations.AssessmentBlocked {
			report.ResultCode, report.ExitCode = "observation_blocked", 3
			return report
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
