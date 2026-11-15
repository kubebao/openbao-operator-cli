// Package collect orchestrates caller-scoped shared client reads.
package collect

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubebao/openbao-operator-client/adapter"
	clientcollect "github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/diagnose"
	shared "github.com/kubebao/openbao-operator-client/evidence"
	"github.com/kubebao/openbao-operator-client/operations"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

const (
	maxCandidates = 1000
)

type Options struct {
	Kubeconfig     string
	Context        string
	Namespace      string
	Name           string
	APIGroup       string
	RequestTimeout time.Duration
	UserAgent      string
}

// Client pins the selected API group and UID after its first successful read.
// Status reads discovery and the named cluster only. Doctor declares additional scopes.
type Client struct {
	shared   *clientcollect.Client
	controls *operations.Client
	contract adapter.Contract
	target   evidence.Target
	origin   string
	uid      string
	timeout  time.Duration
}

func NewClient(options Options) (*Client, error) {
	if len(validation.IsDNS1123Subdomain(options.Name)) != 0 {
		return nil, errors.New("cluster name must be a Kubernetes DNS subdomain")
	}
	return newClient(options)
}

// NewListClient selects a context and namespace without a cluster name. Use it only with List.
func NewListClient(options Options) (*Client, error) {
	if options.Name != "" {
		return nil, errors.New("a list client does not select a cluster name")
	}
	return newClient(options)
}

func newClient(options Options) (*Client, error) {
	if options.APIGroup != "" && options.APIGroup != "openbao.org" && options.APIGroup != "kubebao.org" {
		return nil, errors.New("unsupported API group: use kubebao.org or openbao.org")
	}
	if options.RequestTimeout <= 0 {
		return nil, errors.New("request timeout must be greater than zero")
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = options.Kubeconfig
	rules.WarnIfAllMissing = false
	raw, err := rules.Load()
	if err != nil {
		return nil, errors.New("cannot load kubeconfig; check --kubeconfig and KUBECONFIG")
	}
	contextName := options.Context
	if contextName == "" {
		contextName = raw.CurrentContext
	}
	selected, ok := raw.Contexts[contextName]
	if !ok || selected == nil {
		return nil, errors.New("selected Kubernetes context does not exist; check --context and kubeconfig")
	}
	loader := clientcmd.NewNonInteractiveClientConfig(
		*raw,
		contextName,
		&clientcmd.ConfigOverrides{Context: clientcmdapi.Context{Namespace: options.Namespace}},
		rules,
	)
	namespace, _, err := loader.Namespace()
	if err != nil || len(validation.IsDNS1123Label(namespace)) != 0 {
		return nil, errors.New("cannot resolve a valid Kubernetes namespace; check --namespace and kubeconfig")
	}
	config, err := loader.ClientConfig()
	if err != nil {
		return nil, errors.New("cannot configure Kubernetes access; check the selected kubeconfig context and credentials")
	}
	config.Timeout = options.RequestTimeout
	config.UserAgent = options.UserAgent
	config.WarningHandler = rest.NoWarnings{}
	transport, err := rest.TransportFor(config)
	if err != nil {
		return nil, errors.New("cannot initialize Kubernetes transport")
	}
	client, err := clientcollect.NewClient(config.Host, transport)
	if err != nil {
		return nil, errors.New("cannot initialize bounded Kubernetes access")
	}
	controls, err := operations.NewClient(config.Host, transport)
	if err != nil {
		return nil, errors.New("cannot initialize guarded Kubernetes controls")
	}
	// The origin is stable for this kubeconfig cluster and endpoint, without serializing the endpoint or credentials.
	sum := sha256.Sum256([]byte(selected.Cluster + "\x00" + config.Host))
	return &Client{
		shared:   client,
		controls: controls,
		origin:   "kubeconfig:" + hex.EncodeToString(sum[:16]),
		timeout:  options.RequestTimeout,
		target: evidence.Target{
			Context:           evidence.Clean(contextName),
			KubernetesCluster: evidence.Clean(selected.Cluster),
			Namespace:         namespace,
			Name:              options.Name,
			APIGroup:          options.APIGroup,
		},
	}, nil
}

func (c *Client) request(contract adapter.Contract, group, version, resource string, action shared.ReadAction) clientcollect.Request {
	limits := clientcollect.DefaultLimits()
	limits.MaxPages, limits.MaxCandidates, limits.PageSize = 16, maxCandidates, 100
	limits.MaxBytes, limits.MaxRetainedBytes, limits.MaxResponseBytes = 16*1024*1024, 16*1024*1024, 8*1024*1024
	limits.Timeout = c.timeout
	projection := adapter.DefaultProjectionLimits()
	projection.MaxBytes = limits.MaxResponseBytes
	scope := shared.Scope{Origin: c.origin, Group: group, Version: version, Resource: resource, Action: action}
	if action != shared.ReadDiscovery {
		scope.Namespace = c.target.Namespace
	}
	if action == shared.ReadGet {
		scope.Name = c.target.Name
	}
	return clientcollect.Request{Scope: scope, Contract: contract, Limits: limits, Projection: projection}
}

func (c *Client) clusterRequest() clientcollect.Request {
	request := c.request(c.contract, c.target.APIGroup, "v1alpha1", "openbaoclusters", shared.ReadGet)
	request.ExpectedUID = c.uid
	return request
}

func (c *Client) Collect(ctx context.Context) evidence.Snapshot {
	snapshot := evidence.Snapshot{
		SchemaVersion: "v1",
		ResultCode:    "completed",
		StartedAt:     time.Now().UTC(),
		Target:        c.target,
		Collection:    []evidence.Step{},
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	c.collect(ctx, &snapshot)
	snapshot.CompletedAt = time.Now().UTC()
	return snapshot
}

func (c *Client) collect(ctx context.Context, snapshot *evidence.Snapshot) {
	if c.contract == "" && !c.selectAPI(ctx, snapshot) {
		return
	}
	result, err := c.shared.Clusters(ctx, c.clusterRequest())
	if err != nil {
		snapshot.ResultCode = "incomplete"
		return
	}
	step := collectionStep(result.Collection)
	if len(result.Observations) == 1 {
		step.UID = result.Observations[0].Reference.Identity.UID
		step.ResourceVersion = text(result.Observations[0].Reference.ResourceVersion)
	}
	snapshot.Collection = append(snapshot.Collection, step)
	if result.Collection.Outcome != shared.CollectionComplete || len(result.Observations) != 1 {
		snapshot.ResultCode = "incomplete"
		switch result.Collection.Outcome {
		case shared.CollectionDeleted:
			snapshot.ResultCode = "not_found"
			if c.uid != "" {
				snapshot.ResultCode = "resource_deleted"
			}
		case shared.CollectionReplaced:
			snapshot.ResultCode = "resource_replaced"
		case shared.CollectionInvalid:
			snapshot.ResultCode = "invalid_resource"
		}
		return
	}
	cluster := result.Observations[0]
	c.uid = cluster.Reference.Identity.UID
	c.target.UID = c.uid
	snapshot.Target = c.target
	snapshot.Observation = projectCluster(cluster)
	snapshot.NormalizedEvidence = &diagnose.Capture{
		Clusters: []diagnose.ClusterInput{
			{
				Subject:     cluster.Reference.Identity,
				Observation: &cluster,
				Collection:  result.Collection,
			},
		},
	}
}

func discovered(value shared.Discovery) bool {
	return value.State == shared.ObjectReported && value.Kind.Value != nil && *value.Kind.Value == "OpenBaoCluster" &&
		value.Namespaced.Value != nil &&
		*value.Namespaced.Value
}

func collectionStep(read shared.Collection) evidence.Step {
	return evidence.Step{
		Resource: read.Scope.Resource, Namespace: read.Scope.Namespace, Name: read.Scope.Name, APIGroup: read.Scope.Group,
		Action: string(read.Scope.Action), Outcome: outcome(read.Outcome), CollectedAt: read.Interval.CompletedAt,
	}
}

func outcome(value shared.CollectionOutcome) string {
	switch value {
	case shared.CollectionComplete:
		return "collected"
	case shared.CollectionDenied:
		return "denied"
	case shared.CollectionDeleted:
		return "not_found"
	case shared.CollectionUnsupported:
		return "not_applicable"
	case shared.CollectionPageLimit, shared.CollectionSizeLimit:
		return "limit_reached"
	case shared.CollectionPaginationLoop:
		return "repeated_continue_token"
	default:
		return string(value)
	}
}
