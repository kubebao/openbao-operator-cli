package doctor

import (
	"strings"

	"github.com/kubebao/openbao-operator-client/diagnose"

	"github.com/kubebao/openbao-operator-cli/internal/render"
)

// RuleTitle names what a rule checks. Rule IDs stay stable in JSON and appear beside titles in text.
func RuleTitle(rule string) string {
	switch diagnose.RuleID(rule) {
	case diagnose.RuleAvailability:
		return "Cluster availability"
	case diagnose.RuleSealed:
		return "Sealed state"
	case diagnose.RuleReadiness:
		return "Cluster readiness"
	case diagnose.RuleGeneration:
		return "Operator generation acknowledgement"
	case diagnose.RuleCondition:
		return "Operator conditions"
	case diagnose.RuleController:
		return "Controller errors"
	case diagnose.RuleBackupFailed:
		return "Backup failures"
	case diagnose.RuleBackupAbsent:
		return "Successful backup"
	case diagnose.RuleBackupAge:
		return "Backup age"
	case diagnose.RuleUpgrade:
		return "Rolling upgrade"
	case diagnose.RuleWorkload:
		return "StatefulSet readiness"
	case diagnose.RulePod:
		return "Pod readiness"
	case diagnose.RuleRestriction:
		return "Active restrictions"
	case diagnose.RuleRestore:
		return "Restore progress"
	case diagnose.RuleRestoreTest:
		return "Restore testing"
	case diagnose.RuleRecovery:
		return "Recovery"
	case diagnose.RuleReplication:
		return "Replication"
	default:
		return rule
	}
}

// CoverageState explains a rule coverage state.
func CoverageState(state string) string {
	switch state {
	case string(diagnose.Evaluated):
		return "evaluated"
	case string(diagnose.NotApplicable):
		return "not applicable"
	case string(diagnose.Excluded):
		return "not selected"
	case string(diagnose.NotRequested):
		return "not requested"
	case "insufficient_evidence":
		return "not enough evidence"
	default:
		return render.Humanize(state)
	}
}

// MissingReason explains why a rule could not be evaluated.
func MissingReason(reason string) string {
	switch diagnose.CoverageReason(reason) {
	case diagnose.ReasonScopeIncomplete:
		return "a required read was incomplete"
	case diagnose.ReasonMissingObservation:
		return "the resource was not observed"
	case diagnose.ReasonStaleEvidence:
		return "evidence is stale"
	case diagnose.ReasonMissingField:
		return "a required field is not reported"
	case diagnose.ReasonGeneration:
		return "the operator has not acknowledged the current generation"
	case diagnose.ReasonTime:
		return "reported times are inconsistent"
	case diagnose.ReasonNotConfigured:
		return "the feature is not configured"
	case diagnose.ReasonHigherPriority:
		return "a more severe backup signal applies"
	case diagnose.ReasonVerification:
		return "the cluster could not be verified again after collection"
	case diagnose.ReasonAssociation:
		return "ownership of a related resource is unconfirmed"
	case diagnose.ReasonScopeRestricted:
		return "the read scope was restricted"
	case diagnose.ReasonExcluded:
		return "the rule was not selected"
	case diagnose.ReasonNotRequested:
		return "the evidence was not requested"
	default:
		return render.Humanize(reason)
	}
}

// MissingReasons joins reasons in a sentence fragment.
func MissingReasons(reasons []string) string {
	phrases := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		phrases = append(phrases, MissingReason(reason))
	}
	return strings.Join(phrases, "; ")
}
