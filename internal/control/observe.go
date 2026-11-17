package control

import (
	"context"

	"github.com/kubebao/openbao-operator-client/operations"

	"github.com/kubebao/openbao-operator-cli/internal/collect"
	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

// Observe assesses the original plan with reads only. It never prepares or submits a new request.
func Observe(ctx context.Context, client *collect.Client, plan operations.Plan, options Options) Report {
	report := Report{
		SchemaVersion: "v1", ReadOnly: true, Plan: &plan, RequestFile: options.RequestFile,
		Target: client.Target(), Collection: []evidence.Step{}, Warnings: []string{},
	}
	if plan.Validate() != nil || client.BindTarget(plan.Target) != nil {
		report.ResultCode, report.ExitCode = "invalid_request_target", 1
		return report
	}
	options.Control, options.Enabled = plan.Control, plan.Enabled
	report.Target = client.Target()
	return wait(ctx, client, options, report)
}

// ObserveRestart reuses the saved marker and UID. It never creates another restart marker.
func ObserveRestart(ctx context.Context, client *collect.Client, plan operations.RestartPlan, options Options) RestartReport {
	report := RestartReport{
		SchemaVersion: "v1", ReadOnly: true, Plan: &plan, RequestFile: options.RequestFile,
		Target: client.Target(), Collection: []evidence.Step{},
	}
	if plan.Validate() != nil || client.BindTarget(plan.Target) != nil {
		report.ResultCode, report.ExitCode = "invalid_request_target", 1
		return report
	}
	report.Target = client.Target()
	return waitRestart(ctx, client, options, report)
}
