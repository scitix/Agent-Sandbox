// Copyright 2026 ScitiX
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package managedagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// HandsProvisioner derives an agent's sandbox supply on a worker cluster.
//
// It is an interface rather than a concrete client because the object it
// creates does not live in the control plane's API server: SandboxEnv is a
// worker-cluster CRD, so the control plane reaches it over the worker's REST
// API. Keeping that behind an interface is also what lets the reconciler be
// tested without a worker.
type HandsProvisioner interface {
	// EnsureEnv makes the env and its member pools exist, and reports whether
	// the env is serving. It must be safe to call on every reconcile.
	EnsureEnv(ctx context.Context, clusterID string, spec DerivedEnv) (ready bool, detail string, err error)
}

// DerivedEnv is the env one agent needs, already named and sized.
type DerivedEnv struct {
	Name        string
	TemplateRef string
	Image       string
	Members     []DerivedMember
	Labels      map[string]string
	Annotations map[string]string
}

// DerivedMember is one member pool of a derived env.
//
// Resources and InstanceType are alternatives, not a pair: a cluster with an
// instance-type catalog sizes members by catalog entry, one without it needs
// the size spelled out, and sending both is rejected.
type DerivedMember struct {
	InstanceType string
	Resources    *corev1.ResourceRequirements
	Replicas     int32
	MinReplicas  int32
	MaxReplicas  int32
}

// HandsEnvName is the env derived for an agent.
//
// The worker caps env names at 24 characters because it appends the instance
// type to form each pool name. Deriving a name that the worker will reject
// would surface as a create failure on every reconcile, so the suffix is
// budgeted for here rather than discovered at the remote.
func HandsEnvName(agent string) string {
	const suffix = "-hands"
	const maxEnvName = 24
	if len(agent)+len(suffix) > maxEnvName {
		// Truncation can land on a separator, which would leave a doubled dash
		// in the middle of the derived name.
		agent = strings.TrimRight(agent[:maxEnvName-len(suffix)], "-")
	}
	return agent + suffix
}

// DeriveEnv turns the agent's auto spec into the env to create.
func DeriveEnv(ma *agentsv1alpha1.ManagedAgent) (DerivedEnv, error) {
	auto := ma.Spec.Hands.Auto
	if auto == nil {
		return DerivedEnv{}, fmt.Errorf("hands.auto is not configured")
	}
	if auto.TemplateRef == "" {
		return DerivedEnv{}, fmt.Errorf("hands.auto.templateRef is required")
	}
	if len(auto.InstanceTypes) == 0 {
		return DerivedEnv{}, fmt.Errorf("hands.auto.instanceTypes must not be empty")
	}

	out := DerivedEnv{
		Name:        HandsEnvName(ma.Name),
		TemplateRef: auto.TemplateRef,
		Image:       auto.Image,
		Labels: map[string]string{
			LabelManagedBy: "agentbox-managedagent",
			LabelAgent:     ma.Name,
		},
		Annotations: map[string]string{
			AnnotationOwnerAgent: ma.Namespace + "/" + ma.Name,
		},
	}
	for _, it := range auto.InstanceTypes {
		replicas := it.Replicas
		if replicas < 0 {
			return DerivedEnv{}, fmt.Errorf("instanceType %q has negative replicas", it.Name)
		}
		m := DerivedMember{InstanceType: it.Name, Resources: it.Resources, Replicas: replicas}
		if it.MinReplicas != nil {
			m.MinReplicas = *it.MinReplicas
		}
		if it.MaxReplicas != nil {
			m.MaxReplicas = *it.MaxReplicas
		}
		out.Members = append(out.Members, m)
	}
	return out, nil
}

// Labels and annotations stamped on a derived env, so an operator looking at a
// worker cluster can tell where the object came from and what happens to it.
const (
	LabelManagedBy = "agentbox.navix.sh/managed-by"
	LabelAgent     = "agentbox.navix.sh/managed-agent"

	// AnnotationOwnerAgent records the agent an env was derived for. Cross-
	// cluster ownerReferences do not exist, so this is the only link back, and
	// it is also how an orphan is found: an env carrying LabelAgent whose named
	// agent no longer exists has outlived its owner.
	//
	// A derived env is never deleted with its agent, and nothing marks it at
	// deletion time. Marking would need a finalizer, which would block deleting
	// an agent whenever its worker cluster is unreachable — trading a tidy
	// annotation for an object that cannot be removed during an outage. The env
	// also holds warm pods that someone may still be using. Reclaiming one is
	// therefore a human decision, made against this label.
	AnnotationOwnerAgent = "agentbox.navix.sh/owner-managed-agent"
)

// ClusterEndpoint is one worker cluster's native API, as the control plane
// addresses it.
type ClusterEndpoint struct {
	BaseURL string
	// HostHeader carries the virtual host when the base URL is an IP. The
	// worker's ingress routes on Host, so omitting it lands on the default
	// backend and every call 404s while the address looks correct.
	HostHeader string
}

// ClusterResolver hands out the endpoint for a cluster id.
type ClusterResolver func(clusterID string) (ClusterEndpoint, bool)

// RESTHandsProvisioner provisions envs over a worker cluster's native REST API.
type RESTHandsProvisioner struct {
	Resolve ClusterResolver
	// APIKey authenticates to every worker. The control plane acts as itself
	// here, not on behalf of the agent's owner: the env is platform
	// infrastructure and outlives any one caller's credential.
	APIKey string
	Client *http.Client
}

// NewRESTHandsProvisioner builds a provisioner with a bounded HTTP client.
func NewRESTHandsProvisioner(resolve ClusterResolver, apiKey string) *RESTHandsProvisioner {
	return &RESTHandsProvisioner{
		Resolve: resolve,
		APIKey:  apiKey,
		Client:  &http.Client{Timeout: 15 * time.Second},
	}
}

type envWire struct {
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
}

// EnsureEnv creates the env and any missing member pool, then reports whether
// the remote considers it serving.
func (p *RESTHandsProvisioner) EnsureEnv(
	ctx context.Context,
	clusterID string,
	spec DerivedEnv,
) (bool, string, error) {
	ep, ok := p.Resolve(clusterID)
	if !ok {
		return false, "", fmt.Errorf("cluster %q is not registered on this control plane", clusterID)
	}

	env, err := p.getEnv(ctx, ep, spec.Name)
	if err != nil {
		return false, "", err
	}
	if env == nil {
		if err := p.createEnv(ctx, ep, spec); err != nil {
			return false, "", err
		}
		// The env exists but its pools do not yet; report progress rather than
		// readiness so the next reconcile finishes the job.
		return false, fmt.Sprintf("created SandboxEnv %q on cluster %q", spec.Name, clusterID), nil
	}

	created, err := p.ensureMembers(ctx, ep, spec)
	if err != nil {
		return false, "", err
	}
	if created > 0 {
		return false, fmt.Sprintf("added %d member pool(s) to SandboxEnv %q", created, spec.Name), nil
	}

	state := "still rolling out"
	if env.Ready {
		state = "ready"
	}
	return env.Ready,
		fmt.Sprintf("SandboxEnv %q on cluster %q is %s (%d member pool(s))",
			spec.Name, clusterID, state, len(spec.Members)),
		nil
}

// getEnv finds the env in the cluster's env list.
//
// It lists rather than fetching the env by name: the single-env endpoint also
// renders that env's usage docs, which needs a recoverable per-user API key,
// and the control plane authenticates with an admin key that has none. That
// path answers 422 for an env that exists and is perfectly healthy — a
// readiness probe built on it reports the env broken forever after creating it.
func (p *RESTHandsProvisioner) getEnv(
	ctx context.Context,
	ep ClusterEndpoint,
	name string,
) (*envWire, error) {
	body, code, err := p.do(ctx, ep, http.MethodGet, "/v1/envs", nil)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("list envs: %s", httpDetail(code, body))
	}
	var list struct {
		Items []envWire `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("list envs: malformed response: %w", err)
	}
	for i := range list.Items {
		if list.Items[i].Name == name {
			return &list.Items[i], nil
		}
	}
	return nil, nil
}

func (p *RESTHandsProvisioner) createEnv(ctx context.Context, ep ClusterEndpoint, spec DerivedEnv) error {
	req := map[string]any{
		"name":        spec.Name,
		"templateRef": map[string]any{"name": spec.TemplateRef},
		"labels":      spec.Labels,
		"annotations": spec.Annotations,
	}
	if spec.Image != "" {
		req["overrides"] = map[string]any{"image": spec.Image}
	}
	body, code, err := p.do(ctx, ep, http.MethodPost, "/v1/envs", req)
	if err != nil {
		return err
	}
	// A concurrent reconcile may have won the race; that is the desired state
	// either way.
	if code == http.StatusConflict {
		return nil
	}
	if code != http.StatusOK && code != http.StatusCreated {
		return fmt.Errorf("create env %q: %s", spec.Name, httpDetail(code, body))
	}
	return nil
}

// ensureMembers adds the member pools the env is missing and returns how many
// it created. Existing members are left alone: their replica counts are owned
// by the env's autoscaler once it has run, and rewriting them here would fight
// it on every reconcile.
func (p *RESTHandsProvisioner) ensureMembers(
	ctx context.Context,
	ep ClusterEndpoint,
	spec DerivedEnv,
) (int, error) {
	path := "/v1/envs/" + spec.Name + "/sandboxpools"
	body, code, err := p.do(ctx, ep, http.MethodGet, path, nil)
	if err != nil {
		return 0, err
	}
	if code != http.StatusOK {
		return 0, fmt.Errorf("list members of %q: %s", spec.Name, httpDetail(code, body))
	}
	var list struct {
		Items []struct {
			InstanceType string `json:"instanceType"`
			ScalingGroup string `json:"scalingGroup"`
			Name         string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return 0, fmt.Errorf("list members of %q: malformed response: %w", spec.Name, err)
	}
	// A member identifies itself by whichever field sized it: catalog members
	// come back with instanceType, inline ones with scalingGroup. Matching only
	// one of them makes every existing inline member look absent, so each
	// reconcile would try to add it again.
	have := map[string]bool{}
	for _, it := range list.Items {
		if it.InstanceType != "" {
			have[it.InstanceType] = true
		}
		if it.ScalingGroup != "" {
			have[it.ScalingGroup] = true
		}
	}

	created := 0
	for _, m := range spec.Members {
		if have[m.InstanceType] {
			continue
		}
		req := map[string]any{"replicas": m.Replicas}
		if m.Resources != nil {
			req["scalingGroup"] = m.InstanceType
			req["inlineResources"] = m.Resources
		} else {
			req["instanceType"] = m.InstanceType
		}
		if m.MinReplicas > 0 {
			req["minReplicas"] = m.MinReplicas
		}
		if m.MaxReplicas > 0 {
			req["maxReplicas"] = m.MaxReplicas
		}
		b, c, err := p.do(ctx, ep, http.MethodPost, path, req)
		if err != nil {
			return created, err
		}
		if c == http.StatusConflict {
			continue
		}
		if c != http.StatusOK && c != http.StatusCreated {
			return created, fmt.Errorf("add member %q to %q: %s", m.InstanceType, spec.Name, httpDetail(c, b))
		}
		created++
	}
	return created, nil
}

func (p *RESTHandsProvisioner) do(
	ctx context.Context,
	ep ClusterEndpoint,
	method, path string,
	payload any,
) ([]byte, int, error) {
	var rdr io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		rdr = bytes.NewReader(raw)
	}
	url := strings.TrimSuffix(ep.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, 0, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.APIKey != "" {
		req.Header.Set("AGENTBOX-API-KEY", p.APIKey)
	}
	if ep.HostHeader != "" {
		req.Host = ep.HostHeader
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("%s %s: reading response: %w", method, path, err)
	}
	return body, resp.StatusCode, nil
}

// httpDetail keeps the remote's own error text in the message. A bare status
// code sends the reader to the wrong cluster's logs to find out what the
// control plane was already told.
func httpDetail(code int, body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if len(trimmed) > 300 {
		trimmed = trimmed[:300] + "…"
	}
	if trimmed == "" {
		return fmt.Sprintf("HTTP %d", code)
	}
	return fmt.Sprintf("HTTP %d: %s", code, trimmed)
}
