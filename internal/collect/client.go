package collect

import (
	"context"
	"errors"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

var apiGroups = []string{"openbao.org", "kubebao.org"}

type Options struct {
	Kubeconfig     string
	Context        string
	Namespace      string
	Name           string
	APIGroup       string
	RequestTimeout time.Duration
	UserAgent      string
}

// Client reads only the named OpenBaoCluster and API discovery endpoints.
// After the first selection, it pins both the API group and resource UID.
type Client struct {
	resources dynamic.Interface
	discovery rest.Interface
	target    evidence.Target
	group     string
	uid       string
	timeout   time.Duration
}

func NewClient(options Options) (*Client, error) {
	if len(validation.IsDNS1123Subdomain(options.Name)) != 0 {
		return nil, errors.New("cluster name must be a Kubernetes DNS subdomain")
	}
	if options.APIGroup != "" && options.APIGroup != apiGroups[0] && options.APIGroup != apiGroups[1] {
		return nil, errors.New("unsupported API group: use openbao.org or kubebao.org")
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
	loader := clientcmd.NewNonInteractiveClientConfig(*raw, contextName, &clientcmd.ConfigOverrides{
		Context: clientcmdapi.Context{Namespace: options.Namespace},
	}, rules)
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
	resources, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, errors.New("cannot initialize Kubernetes resource access")
	}
	discover, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, errors.New("cannot initialize Kubernetes discovery")
	}
	return &Client{
		resources: resources, discovery: discover.RESTClient(),
		target: evidence.Target{
			Context: evidence.Clean(contextName), KubernetesCluster: evidence.Clean(selected.Cluster),
			Namespace: namespace, Name: options.Name,
		},
		group: options.APIGroup, timeout: options.RequestTimeout,
	}, nil
}

func (c *Client) Collect(ctx context.Context) evidence.Snapshot {
	report := evidence.Snapshot{
		SchemaVersion: "v1", ResultCode: "completed", StartedAt: time.Now().UTC(),
		Target: c.target, Collection: []evidence.Step{},
	}
	if c.group != "" {
		report.Target.APIGroup, report.Target.APIVersion = c.group, c.group+"/v1alpha1"
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	groups := apiGroups
	if c.group != "" {
		groups = []string{c.group}
	}
	type candidate struct {
		group  string
		object *unstructured.Unstructured
	}
	var candidates []candidate
	for _, group := range groups {
		if c.uid == "" {
			step := c.discover(ctx, group)
			report.Collection = append(report.Collection, step)
			if step.Outcome != "collected" {
				continue
			}
		}
		resource := c.resources.Resource(schema.GroupVersionResource{Group: group, Version: "v1alpha1", Resource: "openbaoclusters"})
		object, err := resource.Namespace(c.target.Namespace).Get(ctx, c.target.Name, metav1.GetOptions{})
		step := evidence.Step{APIGroup: group, Action: "get", Outcome: outcome(err), CollectedAt: time.Now().UTC()}
		if err == nil {
			step.UID, step.ResourceVersion = evidence.Clean(string(object.GetUID())), evidence.Clean(object.GetResourceVersion())
		}
		report.Collection = append(report.Collection, step)
		if err == nil {
			candidates = append(candidates, candidate{group: group, object: object})
		}
	}
	for _, step := range report.Collection {
		if step.Outcome != "collected" && step.Outcome != "not_found" && step.Outcome != "not_applicable" {
			report.ResultCode, report.ExitCode = "incomplete", 3
		}
	}
	switch len(candidates) {
	case 0:
		if report.ExitCode == 0 {
			report.ResultCode, report.ExitCode = "not_found", 1
			if c.uid != "" {
				report.ResultCode, report.ExitCode = "resource_deleted", 3
			}
		}
	case 1:
		chosen := candidates[0]
		object := chosen.object
		if string(object.GetUID()) == "" || object.GetName() != c.target.Name || object.GetNamespace() != c.target.Namespace ||
			object.GetAPIVersion() != chosen.group+"/v1alpha1" || object.GetKind() != "OpenBaoCluster" {
			report.ResultCode, report.ExitCode = "invalid_resource", 3
			break
		}
		if c.uid != "" && c.uid != string(object.GetUID()) {
			report.ResultCode, report.ExitCode = "resource_replaced", 3
			break
		}
		observation, err := ProjectV1Alpha1(object, time.Now().UTC())
		if err != nil {
			report.ResultCode, report.ExitCode = "invalid_resource", 3
			break
		}
		c.group, c.uid = chosen.group, string(object.GetUID())
		c.target.APIGroup, c.target.APIVersion, c.target.UID = c.group, c.group+"/v1alpha1", evidence.Clean(c.uid)
		report.Target, report.Observation = c.target, observation
	default:
		report.ResultCode, report.ExitCode = "ambiguous_api_group", 1
	}
	report.CompletedAt = time.Now().UTC()
	return report
}

func (c *Client) discover(ctx context.Context, group string) evidence.Step {
	var resources metav1.APIResourceList
	err := c.discovery.Get().AbsPath("/apis", group, "v1alpha1").Do(ctx).Into(&resources)
	step := evidence.Step{APIGroup: group, Action: "discover", Outcome: outcome(err), CollectedAt: time.Now().UTC()}
	if apierrors.IsNotFound(err) {
		step.Outcome = "not_applicable"
	}
	if err == nil {
		step.Outcome = "unsupported"
		for _, resource := range resources.APIResources {
			if resource.Name == "openbaoclusters" && resource.Kind == "OpenBaoCluster" && resource.Namespaced {
				step.Outcome = "collected"
				break
			}
		}
	}
	return step
}

func outcome(err error) string {
	switch {
	case err == nil:
		return "collected"
	case apierrors.IsNotFound(err):
		return "not_found"
	case apierrors.IsForbidden(err):
		return "forbidden"
	case apierrors.IsUnauthorized(err):
		return "unauthorized"
	default:
		return "failed"
	}
}
