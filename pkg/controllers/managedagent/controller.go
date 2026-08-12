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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// Reconciler drives a ManagedAgent's Brain workload.
//
// It runs on the control plane only. The worker binary installs on every
// cluster, so a control-plane object reconciled there would get one reconciler
// per cluster all competing for the same resource.
type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// ProxyService is this process reached from inside the cluster, as
	// "<service>.<namespace>:<port>". It is what status.endpoint points at so
	// in-cluster callers go through authentication too.
	ProxyService string

	// PublicBaseURL is the shared route this deployment serves published agents
	// on, e.g. "https://console.example.com/agentbox/api/managed-agents". Empty
	// leaves status.publicURL unset: the agent still works in-cluster, it just
	// has no address to hand out.
	PublicBaseURL string

	// Hands derives an agent's SandboxEnv on a worker cluster. It is nil on a
	// control plane with no worker clusters registered, which is why
	// spec.hands.auto reports "unavailable" instead of failing: the other two
	// hands modes stay usable.
	Hands HandsProvisioner

	// DefaultBrainImage is the image an agent gets when it names none, letting a
	// caller create one from a prompt alone. Unset keeps spec.image required.
	DefaultBrainImage agentsv1alpha1.ManagedAgentImage
}

// +kubebuilder:rbac:groups=agents.navix.sh,resources=managedagents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agents.navix.sh,resources=managedagents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agents.navix.sh,resources=managedagents/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch

// Reconcile brings the Brain in line with the spec and reports what it found.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var ma agentsv1alpha1.ManagedAgent
	if err := r.Get(ctx, req.NamespacedName, &ma); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !ma.DeletionTimestamp.IsZero() {
		// Owned objects go with the ManagedAgent. The Hands do not: a
		// SandboxEnv may be serving sandboxes that outlive the agent, and
		// deleting it would take live user work with it.
		return ctrl.Result{}, nil
	}

	// The generated harness config is written before the checksum reads it.
	// The checksum is what rolls the Deployment, and the file is mounted with a
	// subPath, which kubelet never refreshes in place — so a config written
	// after the hash reaches the pod only on some later, unrelated restart. The
	// symptom is silent: the object says applied, the Secret holds the new
	// bytes, and the running agent keeps using the old ones.
	if err := r.applyOpenCodeConfig(ctx, &ma); err != nil {
		return ctrl.Result{}, err
	}

	checksum, err := r.configChecksum(ctx, &ma)
	if err != nil {
		return ctrl.Result{}, err
	}

	rendered, err := RenderWithDefaults(&ma, checksum, RenderDefaults{
		BrainImage: r.DefaultBrainImage,
	})
	if err != nil {
		// A spec the renderer rejects is a user error, not a transient one:
		// report it and stop rather than hot-looping.
		r.setCondition(&ma, agentsv1alpha1.ManagedAgentConditionBrainReady,
			metav1.ConditionFalse, "InvalidSpec", err.Error())
		ma.Status.Phase = "Failed"
		return ctrl.Result{}, r.writeStatus(ctx, &ma)
	}

	if err := r.applyAll(ctx, &ma, rendered); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.refreshStatus(ctx, &ma, rendered); err != nil {
		return ctrl.Result{}, err
	}
	logger.V(1).Info("reconciled", "phase", ma.Status.Phase)
	return ctrl.Result{}, nil
}

func (r *Reconciler) applyAll(ctx context.Context, ma *agentsv1alpha1.ManagedAgent, rendered *Rendered) error {
	// The claim is created before the Deployment that mounts it: a Deployment
	// referencing a missing claim sits in ContainerCreating with an error that
	// points at the volume rather than at the cause.
	if rendered.PVC != nil {
		if err := r.applyPVC(ctx, ma, rendered.PVC); err != nil {
			return err
		}
	}
	if err := r.applyService(ctx, ma, rendered.Service); err != nil {
		return err
	}
	return r.applyDeployment(ctx, ma, rendered.Deployment)
}

// applyOpenCodeConfig generates the harness's runtime config.
//
// It is generated rather than accepted from the user because the file is what
// pins the harness to a single model provider — see RenderOpenCodeConfig. A
// spec that brings its own config Secret opts out and owns that decision.
func (r *Reconciler) applyOpenCodeConfig(ctx context.Context, ma *agentsv1alpha1.ManagedAgent) error {
	oc := ma.Spec.Runtime.OpenCode
	if oc == nil || (oc.Enabled != nil && !*oc.Enabled) {
		return nil
	}
	if oc.ConfigSecretRef != nil && oc.ConfigSecretRef.Name != "" {
		return nil
	}

	apiKey, err := r.readSecretValue(ctx, ma.Namespace, oc.CredentialsRef)
	if err != nil {
		return err
	}
	raw, err := RenderOpenCodeConfig(ma, apiKey)
	if err != nil {
		return err
	}

	want := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OpenCodeConfigSecretName(ma.Name),
			Namespace: ma.Namespace,
			Labels:    brainLabels(ma.Name),
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, want, func() error {
		want.Type = corev1.SecretTypeOpaque
		want.Data = map[string][]byte{"opencode.json": raw}
		return controllerutil.SetControllerReference(ma, want, r.Scheme)
	})
	return err
}

// readSecretValue resolves one key of a Secret.
//
// A missing Secret or key yields an empty value rather than an error: the
// generated config is still written so the pod starts and reports the
// authentication failure itself, which names the endpoint and is far easier to
// act on than a reconcile loop stuck on a missing key.
func (r *Reconciler) readSecretValue(
	ctx context.Context,
	namespace string,
	ref *agentsv1alpha1.SecretKeySelector,
) (string, error) {
	if ref == nil || ref.Name == "" || ref.Key == "" {
		return "", nil
	}
	var s corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, &s); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return string(s.Data[ref.Key]), nil
}

func (r *Reconciler) applyDeployment(ctx context.Context, ma *agentsv1alpha1.ManagedAgent, want *appsv1.Deployment) error {
	got := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: want.Name, Namespace: want.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, got, func() error {
		got.Labels = want.Labels
		got.Spec = want.Spec
		return controllerutil.SetControllerReference(ma, got, r.Scheme)
	})
	return err
}

func (r *Reconciler) applyService(ctx context.Context, ma *agentsv1alpha1.ManagedAgent, want *corev1.Service) error {
	got := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: want.Name, Namespace: want.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, got, func() error {
		got.Labels = want.Labels
		// ClusterIP is assigned by the apiserver and rejected on update.
		clusterIP := got.Spec.ClusterIP
		got.Spec.Type = want.Spec.Type
		got.Spec.Selector = want.Spec.Selector
		got.Spec.Ports = want.Spec.Ports
		got.Spec.ClusterIP = clusterIP
		return controllerutil.SetControllerReference(ma, got, r.Scheme)
	})
	return err
}

func (r *Reconciler) applyPVC(ctx context.Context, ma *agentsv1alpha1.ManagedAgent, want *corev1.PersistentVolumeClaim) error {
	got := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: want.Name, Namespace: want.Namespace}}
	err := r.Get(ctx, client.ObjectKeyFromObject(got), got)
	if err == nil {
		// A bound claim's spec is immutable apart from a size increase, and
		// re-applying it would fail every reconcile. Leave it alone.
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	if err := controllerutil.SetControllerReference(ma, want, r.Scheme); err != nil {
		return err
	}
	return r.Create(ctx, want)
}

// configChecksum hashes every Secret and ConfigMap the spec references.
//
// Their contents are not part of the pod spec, so without this a credential
// rotation leaves the previous value live in a running pod and nothing in the
// cluster shows that the pod is stale.
func (r *Reconciler) configChecksum(ctx context.Context, ma *agentsv1alpha1.ManagedAgent) (string, error) {
	secrets, configmaps := referencedConfig(ma)

	h := sha256.New()
	for _, name := range sortedKeys(secrets) {
		var s corev1.Secret
		if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: ma.Namespace}, &s); err != nil {
			if apierrors.IsNotFound(err) {
				// A missing Secret is reported through conditions, not by
				// failing the whole reconcile — the Deployment should still be
				// created so the pod's own error says which key is absent.
				hashLine(h, name, "missing")
				continue
			}
			return "", err
		}
		hashLine(h, name, s.ResourceVersion)
	}
	for _, name := range sortedKeys(configmaps) {
		var cm corev1.ConfigMap
		if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: ma.Namespace}, &cm); err != nil {
			if apierrors.IsNotFound(err) {
				hashLine(h, name, "missing")
				continue
			}
			return "", err
		}
		hashLine(h, name, cm.ResourceVersion)
	}
	return hex.EncodeToString(h.Sum(nil))[:32], nil
}

func hashLine(h hash.Hash, name, version string) {
	// hash.Hash never returns an error from Write.
	_, _ = h.Write([]byte(name + ":" + version + "\n"))
}

// referencedConfig collects every Secret and ConfigMap name the spec points at.
func referencedConfig(ma *agentsv1alpha1.ManagedAgent) (secrets, configmaps map[string]bool) {
	secrets = map[string]bool{}
	configmaps = map[string]bool{}

	noteSecret := func(ref *agentsv1alpha1.SecretKeySelector) {
		if ref != nil && ref.Name != "" {
			secrets[ref.Name] = true
		}
	}
	if cc := ma.Spec.Runtime.ClaudeCode; cc != nil {
		noteSecret(&cc.CredentialsRef)
	}
	if oc := ma.Spec.Runtime.OpenCode; oc != nil {
		// Whichever of the two the pod actually mounts. The generated Secret is
		// this controller's own output, but it still has to be hashed: its
		// contents change whenever the agent's models, provider or overlay
		// change, and without it in the checksum the Deployment is not rolled
		// and the subPath mount keeps serving the previous file.
		if oc.ConfigSecretRef != nil {
			noteSecret(oc.ConfigSecretRef)
		} else if openCodeEnabled(ma) {
			secrets[OpenCodeConfigSecretName(ma.Name)] = true
		}
	}
	if c := ma.Spec.Classifier; c != nil {
		noteSecret(c.CredentialsRef)
	}
	if o := ma.Spec.Observability; o != nil && o.Langfuse != nil {
		noteSecret(o.Langfuse.PublicKeyRef)
		noteSecret(o.Langfuse.SecretKeyRef)
	}
	if e := ma.Spec.Hands.E2B; e != nil && e.CredentialsSecret != "" {
		secrets[e.CredentialsSecret] = true
	}
	if p := ma.Spec.Prompt; p != nil && p.From != nil && p.From.Name != "" {
		configmaps[p.From.Name] = true
	}
	for _, s := range ma.Spec.Scenarios {
		if s.Prompt != nil && s.Prompt.From != nil && s.Prompt.From.Name != "" {
			configmaps[s.Prompt.From.Name] = true
		}
	}
	if ma.Spec.Brain != nil {
		for _, v := range ma.Spec.Brain.ExtraVolumes {
			if v.Secret != nil {
				secrets[v.Secret.SecretName] = true
			}
			if v.ConfigMap != nil {
				configmaps[v.ConfigMap.Name] = true
			}
		}
		for _, ef := range ma.Spec.Brain.ExtraEnvFrom {
			if ef.SecretRef != nil {
				secrets[ef.SecretRef.Name] = true
			}
			if ef.ConfigMapRef != nil {
				configmaps[ef.ConfigMapRef.Name] = true
			}
		}
		for _, e := range ma.Spec.Brain.ExtraEnv {
			if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
				secrets[e.ValueFrom.SecretKeyRef.Name] = true
			}
		}
	}
	return secrets, configmaps
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (r *Reconciler) refreshStatus(
	ctx context.Context,
	ma *agentsv1alpha1.ManagedAgent,
	rendered *Rendered,
) error {
	ma.Status.ObservedGeneration = ma.Generation
	ma.Status.Endpoint = Endpoint(ma.Name, ma.Namespace, GatewayPort(ma), r.ProxyService)
	ma.Status.PublicURL = PublicURL(ma, r.PublicBaseURL)
	ma.Status.Scenarios = ScenarioNames(ma)

	var dep appsv1.Deployment
	err := r.Get(ctx, types.NamespacedName{Name: rendered.Deployment.Name, Namespace: ma.Namespace}, &dep)
	switch {
	case apierrors.IsNotFound(err):
		r.setCondition(ma, agentsv1alpha1.ManagedAgentConditionBrainReady,
			metav1.ConditionFalse, "DeploymentMissing", "Brain Deployment has not been created yet")
	case err != nil:
		return err
	case dep.Status.ReadyReplicas > 0:
		r.setCondition(ma, agentsv1alpha1.ManagedAgentConditionBrainReady,
			metav1.ConditionTrue, "DeploymentAvailable", "Brain is serving")
	default:
		r.setCondition(ma, agentsv1alpha1.ManagedAgentConditionBrainReady,
			metav1.ConditionFalse, "DeploymentProgressing",
			fmt.Sprintf("%d/%d replicas ready", dep.Status.ReadyReplicas, dep.Status.Replicas))
	}

	r.refreshHandsStatus(ctx, ma)

	brainReady := meta_IsStatusConditionTrue(ma.Status.Conditions, agentsv1alpha1.ManagedAgentConditionBrainReady)
	handsReady := meta_IsStatusConditionTrue(ma.Status.Conditions, agentsv1alpha1.ManagedAgentConditionHandsReady)
	switch {
	case brainReady && handsReady:
		ma.Status.Phase = "Ready"
	case brainReady:
		ma.Status.Phase = "Degraded"
	default:
		ma.Status.Phase = "Provisioning"
	}
	return r.writeStatus(ctx, ma)
}

// refreshHandsStatus resolves the sandbox supply.
//
// A referenced SandboxEnv normally lives on a worker cluster, which the control
// plane cannot read directly; in that case the reference is recorded and left
// unverified rather than reported as broken.
func (r *Reconciler) refreshHandsStatus(ctx context.Context, ma *agentsv1alpha1.ManagedAgent) {
	if ext := ma.Spec.Hands.External; ext != nil {
		// Nothing to reconcile: the service belongs to someone else. Whether it
		// can actually serve is answered by the remote at call time, and the
		// Brain reports that itself.
		ma.Status.Hands = &agentsv1alpha1.ResolvedHands{EnvName: ext.EnvName, Ready: true}
		r.setCondition(ma, agentsv1alpha1.ManagedAgentConditionHandsReady,
			metav1.ConditionTrue, "External",
			fmt.Sprintf("external sandbox service at %s, environment %q", ext.APIURL, ext.EnvName))
		return
	}

	if auto := ma.Spec.Hands.Auto; auto != nil {
		r.refreshAutoHandsStatus(ctx, ma, auto)
		return
	}

	ref := ma.Spec.Hands.EnvRef
	if ref == nil {
		r.setCondition(ma, agentsv1alpha1.ManagedAgentConditionHandsReady,
			metav1.ConditionFalse, "NoHands",
			"hands must set one of external, envRef, or auto")
		return
	}

	ma.Status.Hands = &agentsv1alpha1.ResolvedHands{
		ClusterID: ref.ClusterID,
		EnvName:   ref.Name,
	}

	ns := ref.Namespace
	if ns == "" {
		ns = ma.Namespace
	}
	var env agentsv1alpha1.SandboxEnv
	err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ns}, &env)
	switch {
	case err == nil:
		ma.Status.Hands.Ready = true
		r.setCondition(ma, agentsv1alpha1.ManagedAgentConditionHandsReady,
			metav1.ConditionTrue, "EnvFound",
			fmt.Sprintf("SandboxEnv %s/%s", ns, ref.Name))
	case apierrors.IsNotFound(err), isKindUnavailable(err):
		// Either the env lives on a worker cluster or this cluster does not
		// install the worker CRDs at all. Both are normal for a control plane;
		// the sandbox endpoint is what actually has to answer, and the Brain
		// reports that itself.
		ma.Status.Hands.Ready = true
		r.setCondition(ma, agentsv1alpha1.ManagedAgentConditionHandsReady,
			metav1.ConditionTrue, "EnvRemote",
			fmt.Sprintf("SandboxEnv %q is not visible from this cluster; the Brain resolves it through the sandbox API", ref.Name))
	default:
		r.setCondition(ma, agentsv1alpha1.ManagedAgentConditionHandsReady,
			metav1.ConditionFalse, "EnvLookupFailed", err.Error())
	}
}

// refreshAutoHandsStatus derives the agent's env on its worker cluster.
//
// The env is created but never deleted from here. It lives on a cluster this
// process only reaches over HTTP, where a read that fails is indistinguishable
// from an object that is gone — so a delete driven by that signal would
// eventually destroy a healthy pool holding warm sandboxes. Reclaiming a
// derived env is left to a human, who has the orphan annotation to find it by.
func (r *Reconciler) refreshAutoHandsStatus(
	ctx context.Context,
	ma *agentsv1alpha1.ManagedAgent,
	auto *agentsv1alpha1.HandsAutoSpec,
) {
	spec, err := DeriveEnv(ma)
	if err != nil {
		r.setCondition(ma, agentsv1alpha1.ManagedAgentConditionHandsReady,
			metav1.ConditionFalse, "InvalidAutoSpec", err.Error())
		return
	}
	ma.Status.Hands = &agentsv1alpha1.ResolvedHands{
		ClusterID: auto.ClusterID,
		EnvName:   spec.Name,
	}

	if r.Hands == nil {
		r.setCondition(ma, agentsv1alpha1.ManagedAgentConditionHandsReady,
			metav1.ConditionFalse, "ProvisioningUnavailable",
			"this control plane has no worker clusters registered, so hands.auto cannot derive an env; use hands.envRef or hands.external")
		return
	}

	ready, detail, err := r.Hands.EnsureEnv(ctx, auto.ClusterID, spec)
	switch {
	case err != nil:
		r.setCondition(ma, agentsv1alpha1.ManagedAgentConditionHandsReady,
			metav1.ConditionFalse, "ProvisioningFailed", err.Error())
	case ready:
		ma.Status.Hands.Ready = true
		r.setCondition(ma, agentsv1alpha1.ManagedAgentConditionHandsReady,
			metav1.ConditionTrue, "EnvDerived", detail)
	default:
		r.setCondition(ma, agentsv1alpha1.ManagedAgentConditionHandsReady,
			metav1.ConditionFalse, "EnvProvisioning", detail)
	}
}

func (r *Reconciler) setCondition(
	ma *agentsv1alpha1.ManagedAgent,
	condType string,
	status metav1.ConditionStatus,
	reason, message string,
) {
	cond := metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: ma.Generation,
		LastTransitionTime: metav1.Now(),
	}
	for i, existing := range ma.Status.Conditions {
		if existing.Type != condType {
			continue
		}
		if existing.Status == status {
			cond.LastTransitionTime = existing.LastTransitionTime
		}
		ma.Status.Conditions[i] = cond
		return
	}
	ma.Status.Conditions = append(ma.Status.Conditions, cond)
}

func (r *Reconciler) writeStatus(ctx context.Context, ma *agentsv1alpha1.ManagedAgent) error {
	return r.Status().Update(ctx, ma)
}

func meta_IsStatusConditionTrue(conds []metav1.Condition, condType string) bool {
	for _, c := range conds {
		if c.Type == condType {
			return c.Status == metav1.ConditionTrue
		}
	}
	return false
}

// isKindUnavailable reports that this cluster does not serve the kind at all.
//
// A control plane that does not install the worker chart has no SandboxEnv CRD,
// so the RESTMapper fails before any request is made. That is an expected
// deployment shape, not a broken reference.
func isKindUnavailable(err error) bool {
	return meta.IsNoMatchError(err) || runtime.IsNotRegisteredError(err) ||
		apierrors.IsMethodNotSupported(err)
}

// SetupWithManager registers the controller.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentsv1alpha1.ManagedAgent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
