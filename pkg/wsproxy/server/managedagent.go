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

package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/router/middleware"
	"github.com/scitix/agent-sandbox/pkg/controllers/managedagent"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

// ManagedAgentAPI serves the console's CRUD over ManagedAgent objects.
//
// ManagedAgent is a control-plane object, so it is served here rather than by
// the per-cluster API: the console reaches it through the same BFF path it uses
// for global API keys and templates, with no cluster in the route.
type ManagedAgentAPI struct {
	Client    client.Client
	Scheme    *runtime.Scheme
	Namespace string
	// Hands creates a SandboxEnv on a worker cluster when a create request asks
	// for one. Nil on a control plane without an admin key — a request that needs
	// it is then refused rather than silently creating an agent with no sandbox
	// supply. Shared with the ManagedAgent controller, which uses it for
	// hands.auto.
	Hands managedagent.HandsProvisioner

	// Gateway forwards the console's own requests to an agent's Brain, so a user
	// can talk to their agent from the platform. Nil leaves that surface off,
	// which is why every route below still works without it.
	//
	// It is the SAME proxy the public listener uses, deliberately: the alternative
	// is two forwarders that agree today and diverge on the next Brain endpoint —
	// and the one that diverges silently is whichever gets less traffic.
	Gateway *ManagedAgentGateway
}

// managedAgentWire is the shape the console consumes: flattened metadata plus
// the spec and status verbatim, matching how templates are exposed.
type managedAgentWire struct {
	Name              string                            `json:"name"`
	Namespace         string                            `json:"namespace"`
	CreationTimestamp string                            `json:"creationTimestamp,omitempty"`
	Spec              agentsv1alpha1.ManagedAgentSpec   `json:"spec"`
	Status            agentsv1alpha1.ManagedAgentStatus `json:"status,omitzero"`
	CRDYaml           string                            `json:"crdYaml,omitempty"`
}

type managedAgentCreateRequest struct {
	Name        string                          `json:"name"`
	Namespace   string                          `json:"namespace,omitempty"`
	Spec        agentsv1alpha1.ManagedAgentSpec `json:"spec"`
	Credentials *managedAgentCredentials        `json:"credentials,omitempty"`
	// SandboxEnv, when present, creates the agent's sandbox supply on a worker
	// cluster before the agent itself is created, and points spec.hands at it.
	SandboxEnv *managedAgentSandboxEnv `json:"sandboxEnv,omitempty"`
}

// managedAgentSandboxEnv creates the agent's SandboxEnv from the console in the
// same request that creates the agent.
//
// Env and Members are the worker's OWN request bodies, forwarded unchanged. That
// is deliberate: an agent's sandbox supply needs the whole env API surface —
// including the credential injection that lets a sandbox use a token it cannot
// read — and mirroring those fields here would be a second schema to keep in
// step with the worker's.
//
// It is create-only. The env, not the agent, is the single writer of that
// configuration afterwards: nothing here is stored on the ManagedAgent, no
// controller reconciles it back, and an update request carrying this field is
// rejected. Later changes are made on the env itself.
type managedAgentSandboxEnv struct {
	// ClusterID names the worker cluster to create the env on. Empty means the
	// control plane's default cluster resolution, same as hands.auto.
	ClusterID string `json:"clusterID,omitempty"`
	// Env is a worker CreateSandboxEnvRequest.
	Env json.RawMessage `json:"env"`
	// Members are worker add-member requests, applied in order. A member is a
	// separate call on the worker, so it is a separate body here too.
	Members []json.RawMessage `json:"members,omitempty"`
}

type managedAgentUpdateRequest struct {
	Spec        agentsv1alpha1.ManagedAgentSpec `json:"spec"`
	Credentials *managedAgentCredentials        `json:"credentials,omitempty"`
	// SandboxEnv is accepted only to be refused with an explanation. Silently
	// ignoring it would let a console think it had just changed an env's
	// credentials here, when the env is the only place that can be changed.
	SandboxEnv *managedAgentSandboxEnv `json:"sandboxEnv,omitempty"`
}

// managedAgentCredentials carries the API keys the console collected.
//
// They are accepted here and written to a Secret rather than stored on the
// object: a key placed in a spec field lives in etcd in the clear and is echoed
// back by every read. Asking the operator for a pre-existing Secret instead
// would be the other way to avoid that, but a person creating their first agent
// in a console does not have one — so the platform makes it.
//
// An omitted field on update means "leave that key alone", which is what lets
// the console show an empty password box without wiping the stored value.
type managedAgentCredentials struct {
	ClaudeCodeAPIKey string `json:"claudeCodeApiKey,omitempty"`
	OpenCodeAPIKey   string `json:"openCodeApiKey,omitempty"`
	ClassifierAPIKey string `json:"classifierApiKey,omitempty"`
	SandboxAPIKey    string `json:"sandboxApiKey,omitempty"`
}

// Keys inside the per-agent credential Secret.
const (
	credKeyClaudeCode = "CLAUDE_CODE_API_KEY"
	credKeyOpenCode   = "OPENCODE_API_KEY"
	credKeyClassifier = "CLASSIFIER_API_KEY"
	credKeySandbox    = "SANDBOX_API_KEY"
)

// credentialSecretName is one Secret per agent, named after it. One object
// rather than one per provider keeps the agent's blast radius and its lifecycle
// the same thing: it is garbage-collected with the agent.
func credentialSecretName(agent string) string {
	return managedagent.BrainName(agent) + "-credentials"
}

// bindCredentialRefs points the spec at the per-agent Secret for every key the
// caller supplied, so the console never has to know the Secret exists.
func bindCredentialRefs(spec *agentsv1alpha1.ManagedAgentSpec, agent string, creds *managedAgentCredentials) {
	if creds == nil {
		return
	}
	name := credentialSecretName(agent)
	ref := func(key string) *agentsv1alpha1.SecretKeySelector {
		return &agentsv1alpha1.SecretKeySelector{Name: name, Key: key}
	}
	if creds.ClaudeCodeAPIKey != "" && spec.Runtime.ClaudeCode != nil {
		spec.Runtime.ClaudeCode.CredentialsRef = *ref(credKeyClaudeCode)
	}
	if creds.OpenCodeAPIKey != "" && spec.Runtime.OpenCode != nil {
		spec.Runtime.OpenCode.CredentialsRef = ref(credKeyOpenCode)
	}
	if creds.ClassifierAPIKey != "" && spec.Classifier != nil {
		spec.Classifier.CredentialsRef = ref(credKeyClassifier)
	}
	if creds.SandboxAPIKey != "" && spec.Hands.External != nil {
		spec.Hands.External.CredentialsRef = ref(credKeySandbox)
	}
}

// writeCredentialSecret merges the supplied keys into the agent's Secret.
//
// It is called after the agent exists so the Secret can be owned by it and
// removed with it. A failure here leaves an agent whose credential references
// point at a key that is not there — which the pod reports as an authentication
// error naming the endpoint, rather than failing silently.
func (a *ManagedAgentAPI) writeCredentialSecret(
	ctx context.Context,
	ma *agentsv1alpha1.ManagedAgent,
	creds *managedAgentCredentials,
) error {
	if creds == nil {
		return nil
	}
	supplied := map[string]string{
		credKeyClaudeCode: creds.ClaudeCodeAPIKey,
		credKeyOpenCode:   creds.OpenCodeAPIKey,
		credKeyClassifier: creds.ClassifierAPIKey,
		credKeySandbox:    creds.SandboxAPIKey,
	}
	if !anyNonEmpty(supplied) {
		return nil
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name:      credentialSecretName(ma.Name),
		Namespace: ma.Namespace,
	}}
	_, err := controllerutil.CreateOrUpdate(ctx, a.Client, secret, func() error {
		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}
		for key, value := range supplied {
			if value != "" {
				secret.Data[key] = []byte(value)
			}
		}
		secret.Type = corev1.SecretTypeOpaque
		return controllerutil.SetControllerReference(ma, secret, a.Scheme)
	})
	return err
}

func anyNonEmpty(m map[string]string) bool {
	for _, v := range m {
		if v != "" {
			return true
		}
	}
	return false
}

// RegisterManagedAgentRoutes mounts the CRUD surface on the given group.
func (a *ManagedAgentAPI) RegisterManagedAgentRoutes(g *gin.RouterGroup) {
	g.GET("/managedagents", a.list)
	g.POST("/managedagents", a.create)
	g.GET("/managedagents/:name", a.get)
	g.PUT("/managedagents/:name", a.update)
	g.DELETE("/managedagents/:name", a.remove)
	if a.Gateway != nil {
		g.Any("/managedagents/:name/proxy", a.proxy)
		g.Any("/managedagents/:name/proxy/*path", a.proxy)
	}
}

// proxy forwards a console request to the agent's Brain.
//
// Two things differ from the public listener, and both follow from who is asking.
//
// It does NOT require spec.ingress.enabled. That flag answers "may callers outside
// the cluster reach this agent", and the console is not one of them — it arrives on
// the internal API, already authenticated, having been reached through the BFF.
// Gating the console on it would mean publishing an agent to the internet just to
// talk to it from the platform that owns it, which is the wrong trade to force.
//
// Tenant scoping is `fetch`'s, so it is the same rule as every other route here:
// an agent belonging to someone else is absent rather than forbidden.
// The end user is PINNED here rather than left to the request.
//
// The Brain otherwise takes the caller's word for which of its end users is
// asking, which is right for an integration acting on behalf of many of its own
// users and wrong for a browser: there the value identifies the person choosing
// it, so anyone could read another user's threads by editing a query string.
// Overriding it means the console can only ever see its own caller's
// conversations, whatever the request says.
//
// Any inbound copy of the header is dropped first. Trusting a header is only safe
// when the hop that authenticated is the one that sets it.
func (a *ManagedAgentAPI) proxy(c *gin.Context) {
	ma, ok := a.fetch(c)
	if !ok {
		return
	}
	c.Request.Header.Del(brainUserHeader)
	if user := callerOf(c).user; user != "" {
		c.Request.Header.Set(brainUserHeader, user)
	}
	a.Gateway.forward(c, ma)
}

func (a *ManagedAgentAPI) list(c *gin.Context) {
	var out agentsv1alpha1.ManagedAgentList
	if err := a.Client.List(c.Request.Context(), &out, client.InNamespace(a.Namespace)); err != nil {
		writeManagedAgentError(c, err)
		return
	}
	who := callerOf(c)
	items := make([]managedAgentWire, 0, len(out.Items))
	for i := range out.Items {
		if !visibleTo(&out.Items[i], who) {
			continue
		}
		items = append(items, toManagedAgentWire(&out.Items[i], false))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *ManagedAgentAPI) get(c *gin.Context) {
	ma, ok := a.fetch(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, toManagedAgentWire(ma, true))
}

func (a *ManagedAgentAPI) create(c *gin.Context) {
	var req managedAgentCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "detail": err.Error()})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	ns := req.Namespace
	if ns == "" {
		ns = a.Namespace
	}
	// Ownership is stamped, never accepted: an agent belongs to the person who
	// created it, and letting the request name someone else would make the
	// visibility rule meaningless.
	who := callerOf(c)
	req.Spec.Owner = &agentsv1alpha1.ManagedAgentOwner{Team: who.team, User: who.user}

	// Create the sandbox supply first, when the caller asked for one. Before the
	// agent, so a failure here leaves nothing behind: an agent whose env creation
	// failed would be published, answer requests, and fail every tool call.
	if req.SandboxEnv != nil {
		if a.Hands == nil {
			c.JSON(http.StatusNotImplemented, gin.H{
				"error": "this control plane cannot create sandbox environments (no admin key configured); " +
					"create the env first and reference it with hands.envRef",
			})
			return
		}
		envName, err := a.Hands.CreateEnv(
			c.Request.Context(), req.SandboxEnv.ClusterID, req.SandboxEnv.Env, req.SandboxEnv.Members)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "create sandbox env", "detail": err.Error()})
			return
		}
		// Point the agent at what was just created, and clear the other branches:
		// hands is one-of, and a leftover auto/external block would win or conflict.
		binding := req.Spec.Hands.Binding
		req.Spec.Hands = agentsv1alpha1.ManagedAgentHands{
			EnvRef: &agentsv1alpha1.HandsEnvRef{
				ClusterID: req.SandboxEnv.ClusterID,
				Name:      envName,
			},
			Binding: binding,
		}
	}

	bindCredentialRefs(&req.Spec, req.Name, req.Credentials)

	ma := &agentsv1alpha1.ManagedAgent{
		ObjectMeta: metav1.ObjectMeta{Name: req.Name, Namespace: ns},
		Spec:       req.Spec,
	}
	if err := a.Client.Create(c.Request.Context(), ma); err != nil {
		writeManagedAgentError(c, err)
		return
	}
	if err := a.writeCredentialSecret(c.Request.Context(), ma, req.Credentials); err != nil {
		writeManagedAgentError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toManagedAgentWire(ma, false))
}

func (a *ManagedAgentAPI) update(c *gin.Context) {
	ma, ok := a.fetch(c)
	if !ok {
		return
	}
	var req managedAgentUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "detail": err.Error()})
		return
	}
	if req.SandboxEnv != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "sandboxEnv can only be set when creating an agent; " +
				"change the SandboxEnv itself to edit its sizing, credentials or egress rules",
		})
		return
	}
	// Ownership is immutable: it is what the visibility rule is built on, so an
	// update carries the stored value forward regardless of what was sent.
	owner := ma.Spec.Owner
	ma.Spec = req.Spec
	ma.Spec.Owner = owner
	bindCredentialRefs(&ma.Spec, ma.Name, req.Credentials)

	if err := a.Client.Update(c.Request.Context(), ma); err != nil {
		writeManagedAgentError(c, err)
		return
	}
	if err := a.writeCredentialSecret(c.Request.Context(), ma, req.Credentials); err != nil {
		writeManagedAgentError(c, err)
		return
	}
	c.JSON(http.StatusOK, toManagedAgentWire(ma, false))
}

func (a *ManagedAgentAPI) remove(c *gin.Context) {
	ma, ok := a.fetch(c)
	if !ok {
		return
	}
	if err := a.Client.Delete(c.Request.Context(), ma); err != nil {
		writeManagedAgentError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// fetch loads one agent and enforces tenant scoping.
//
// An agent belonging to another team is reported as absent rather than
// forbidden: which agents a tenant owns is itself not disclosed.
func (a *ManagedAgentAPI) fetch(c *gin.Context) (*agentsv1alpha1.ManagedAgent, bool) {
	var ma agentsv1alpha1.ManagedAgent
	key := client.ObjectKey{Name: c.Param("name"), Namespace: a.Namespace}
	if err := a.Client.Get(c.Request.Context(), key, &ma); err != nil {
		writeManagedAgentError(c, err)
		return nil, false
	}
	if !visibleTo(&ma, callerOf(c)) {
		c.JSON(http.StatusNotFound, gin.H{"error": "managed agent not found"})
		return nil, false
	}
	return &ma, true
}

// caller describes who is asking, for tenant scoping.
type caller struct {
	team  string
	user  string
	admin bool
}

// visibleTo reports whether this caller may see the agent.
//
// Scoping is to one person, not to a team: an agent carries its creator's
// credentials and can act on their behalf, so a teammate listing agents does
// not see it. An admin sees everything — the manager token operators use
// carries its own synthetic identity, and scoping it like a tenant would hide
// every agent from the caller meant to administer them.
func visibleTo(ma *agentsv1alpha1.ManagedAgent, who caller) bool {
	if who.admin {
		return true
	}
	owner := ma.Spec.Owner
	if owner == nil || (owner.Team == "" && owner.User == "") {
		return true
	}
	return owner.Team == who.team && owner.User == who.user
}

func callerOf(c *gin.Context) caller {
	v, ok := c.Get(middleware.AuthContextKey)
	if !ok {
		return caller{}
	}
	var info domain.AuthInfo
	switch t := v.(type) {
	case domain.AuthInfo:
		info = t
	case *domain.AuthInfo:
		if t == nil {
			return caller{}
		}
		info = *t
	default:
		return caller{}
	}
	return caller{team: info.Team, user: info.User, admin: info.Role == apikey.RoleAdmin}
}

func toManagedAgentWire(ma *agentsv1alpha1.ManagedAgent, withYAML bool) managedAgentWire {
	w := managedAgentWire{
		Name:      ma.Name,
		Namespace: ma.Namespace,
		Spec:      ma.Spec,
		Status:    ma.Status,
	}
	if !ma.CreationTimestamp.IsZero() {
		w.CreationTimestamp = ma.CreationTimestamp.UTC().Format("2006-01-02T15:04:05Z")
	}
	if withYAML {
		// The console renders this in a read-only tab. Server fields are
		// stripped so what is shown is re-appliable.
		clean := &agentsv1alpha1.ManagedAgent{
			TypeMeta: metav1.TypeMeta{
				APIVersion: agentsv1alpha1.GroupVersion.String(),
				Kind:       "ManagedAgent",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        ma.Name,
				Namespace:   ma.Namespace,
				Labels:      ma.Labels,
				Annotations: ma.Annotations,
			},
			Spec: ma.Spec,
		}
		if raw, err := yaml.Marshal(clean); err == nil {
			w.CRDYaml = string(raw)
		}
	}
	return w
}

func writeManagedAgentError(c *gin.Context, err error) {
	switch {
	case apierrors.IsNotFound(err):
		c.JSON(http.StatusNotFound, gin.H{"error": "managed agent not found"})
	case apierrors.IsAlreadyExists(err):
		c.JSON(http.StatusConflict, gin.H{"error": "managed agent already exists"})
	case apierrors.IsInvalid(err), apierrors.IsBadRequest(err):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
