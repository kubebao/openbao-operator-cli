// Package bundle stores bounded, projected evidence for offline diagnosis.
package bundle

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
	"github.com/kubebao/openbao-operator-cli/internal/doctor"
	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	"github.com/kubebao/openbao-operator-client/diagnose"
	shared "github.com/kubebao/openbao-operator-client/evidence"
)

const FormatVersion = "v1"

// Manifest describes collection independently of diagnostic completeness.
type Manifest struct {
	FormatVersion         string          `json:"formatVersion"`
	EvidenceSchemaVersion string          `json:"evidenceSchemaVersion"`
	CLI                   buildinfo.Info  `json:"cli"`
	StartedAt             time.Time       `json:"startedAt"`
	CompletedAt           time.Time       `json:"completedAt"`
	Target                evidence.Target `json:"target"`
	CollectionResult      string          `json:"collectionResult"`
	Collection            []evidence.Step `json:"collection"`
	Options               Options         `json:"options"`
	Policy                diagnose.Policy `json:"policy"`
}

type Options struct {
	IncludeLogs    bool          `json:"includeLogs"`
	RequestTimeout time.Duration `json:"requestTimeout"`
}

// Bundle contains only projected evidence. Display reports are recomputed on read.
type Bundle struct {
	Manifest Manifest
	Capture  *diagnose.Capture
}

func New(snapshot evidence.Snapshot, policy diagnose.Policy, info buildinfo.Info, timeout time.Duration) Bundle {
	return Bundle{
		Manifest: Manifest{
			FormatVersion: FormatVersion, EvidenceSchemaVersion: shared.SchemaVersion,
			CLI: info, StartedAt: snapshot.StartedAt, CompletedAt: snapshot.CompletedAt,
			Target: snapshot.Target, CollectionResult: snapshot.ResultCode,
			Collection: snapshot.Collection, Options: Options{RequestTimeout: timeout}, Policy: policy,
		},
		Capture: snapshot.NormalizedEvidence,
	}
}

// Snapshot rebuilds the analysis input without trusting archived display reports.
// Legacy live-only display projections are deliberately not deserialized.
func (b Bundle) Snapshot() evidence.Snapshot {
	target := b.Manifest.Target
	target.Context = evidence.Clean(target.Context)
	target.KubernetesCluster = evidence.Clean(target.KubernetesCluster)
	return evidence.Snapshot{
		SchemaVersion: "v1", ResultCode: b.Manifest.CollectionResult,
		StartedAt: b.Manifest.StartedAt, CompletedAt: b.Manifest.CompletedAt,
		Target: target, Collection: b.Manifest.Collection, NormalizedEvidence: b.Capture,
	}
}

func (b Bundle) Validate() error {
	m := b.Manifest
	if m.FormatVersion != FormatVersion || m.EvidenceSchemaVersion != shared.SchemaVersion ||
		m.Policy.RuleSetVersion != diagnose.RuleSetVersion {
		return errors.New("unsupported bundle, evidence, or rule-set version")
	}
	if m.StartedAt.IsZero() || m.CompletedAt.Before(m.StartedAt) || !m.Policy.ReferenceTime.Equal(m.CompletedAt) ||
		m.Options.IncludeLogs || m.Options.RequestTimeout <= 0 || len(m.Collection) > 128 ||
		m.Policy.MaxSubjects > 3001 {
		return errors.New("invalid bundle collection metadata")
	}
	if err := validateMetadata(m); err != nil {
		return err
	}
	if b.Capture == nil {
		if m.CollectionResult == "completed" || m.Target.UID != "" {
			return errors.New("bundle lacks the selected target evidence")
		}
		// Validate the policy even when discovery or the initial read failed.
		_, err := diagnose.Analyze(diagnose.Capture{SchemaVersion: shared.SchemaVersion}, m.Policy)
		if err != nil {
			return errors.New("unsupported or invalid diagnostic policy")
		}
		return nil
	}
	if len(b.Capture.Clusters) != 1 {
		return errors.New("bundle must contain exactly one selected cluster")
	}
	input := b.Capture.Clusters[0]
	id := input.Subject
	if id.Name != m.Target.Name || id.Namespace != m.Target.Namespace || id.UID != m.Target.UID ||
		id.Group != m.Target.APIGroup || input.Observation == nil || input.Observation.Reference.APIVersion != m.Target.APIVersion {
		return errors.New("bundle target identity does not match captured evidence")
	}
	reads := []shared.Collection{input.Collection}
	if input.Verification != nil {
		reads = append(reads, input.Verification.Read)
	}
	if input.StatefulSets != nil {
		reads = append(reads, input.StatefulSets.Collection)
	}
	if input.Pods != nil {
		reads = append(reads, input.Pods.Collection)
	}
	if input.Restores != nil {
		reads = append(reads, input.Restores.Collection)
	}
	for _, read := range reads {
		if read.Outcome == shared.CollectionNotCollected && read.Interval.StartedAt.IsZero() && read.Interval.CompletedAt.IsZero() {
			continue
		}
		if read.Interval.StartedAt.Before(m.StartedAt) || read.Interval.CompletedAt.After(m.CompletedAt) {
			return errors.New("captured read is outside the bundle collection interval")
		}
	}
	if _, err := diagnose.Analyze(*b.Capture, m.Policy); err != nil {
		return errors.New("unsupported or invalid normalized evidence or policy")
	}
	return nil
}

func validateMetadata(m Manifest) error {
	switch m.CollectionResult {
	case "completed", "incomplete", "not_found", "ambiguous_api_group", "invalid_resource", "resource_deleted", "resource_replaced",
		"target_changed", "target_verification_incomplete":
	default:
		return errors.New("invalid bundle collection result")
	}
	values := []string{
		m.CLI.Version, m.CLI.Commit, m.CLI.GoVersion, m.CLI.OS, m.CLI.Arch,
		m.Target.Context, m.Target.KubernetesCluster, m.Target.Namespace, m.Target.Name,
		m.Target.APIGroup, m.Target.APIVersion, m.Target.UID,
	}
	if m.Target.Name == "" || m.Target.Namespace == "" {
		return errors.New("bundle target is missing")
	}
	for _, step := range m.Collection {
		uncollected := step.Outcome == string(shared.CollectionNotCollected) && step.CollectedAt.IsZero()
		if !uncollected && (step.CollectedAt.Before(m.StartedAt) || step.CollectedAt.After(m.CompletedAt)) {
			return errors.New("collection step is outside the bundle interval")
		}
		values = append(values, step.Resource, step.Namespace, step.Name, step.APIGroup, step.Action,
			step.Outcome, step.UID, step.ResourceVersion)
	}
	for _, value := range values {
		if len(value) > 4096 || evidence.Clean(value) != value {
			return errors.New("invalid bundle display metadata")
		}
	}
	return nil
}

func (b Bundle) entries() (map[string][]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	report, err := doctor.Analyze(b.Snapshot(), b.Manifest.Policy)
	if err != nil {
		return nil, errors.New("cannot analyze bundle evidence")
	}
	result := "completed"
	if b.Manifest.CollectionResult != "completed" || !report.Complete {
		result = "incomplete"
	}
	var summary bytes.Buffer
	fmt.Fprintf(&summary, "Captured evidence: %s to %s\nCollection: %s\n",
		b.Manifest.StartedAt.Format(time.RFC3339Nano), b.Manifest.CompletedAt.Format(time.RFC3339Nano), b.Manifest.CollectionResult)
	if err := doctor.WriteText(&summary, report, result, true); err != nil {
		return nil, err
	}
	entries := map[string][]byte{"summary.txt": summary.Bytes()}
	for name, value := range map[string]any{
		"manifest.json": b.Manifest, "evidence/capture.json": b.Capture,
		"findings.json": struct {
			SchemaVersion  string                `json:"schemaVersion"`
			RuleSetVersion string                `json:"ruleSetVersion"`
			Policy         diagnose.Policy       `json:"policy"`
			Complete       bool                  `json:"complete"`
			Findings       []doctor.Finding      `json:"findings"`
			Coverage       []doctor.RuleCoverage `json:"coverage"`
		}{"v1", report.RuleSetVersion, report.Policy, report.Complete, report.Findings, report.Coverage},
	} {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, errors.New("cannot encode bundle evidence")
		}
		entries[name] = append(data, '\n')
	}
	for name, data := range entries {
		if int64(len(data)) > entryLimit(name) {
			return nil, errors.New("bundle entry exceeds its size limit")
		}
	}
	return entries, nil
}

func decodeJSON(data []byte, value any) error {
	if !utf8.Valid(data) {
		return errors.New("bundle JSON is not valid UTF-8")
	}
	// Inspect object keys before typed decoding. encoding/json otherwise accepts
	// duplicate keys, which can make identity and policy interpretation ambiguous.
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := jsonValue(decoder, 0); err != nil {
		return errors.New("invalid bundle JSON")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("invalid trailing bundle JSON")
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return errors.New("unsupported or invalid bundle JSON fields")
	}
	return nil
}

func jsonValue(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return errors.New("JSON nesting limit reached")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	keys := map[string]bool{}
	for decoder.More() {
		if delim == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			name = strings.ToLower(name)
			if !ok || keys[name] {
				return errors.New("duplicate or invalid JSON key")
			}
			keys[name] = true
		}
		if err := jsonValue(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
