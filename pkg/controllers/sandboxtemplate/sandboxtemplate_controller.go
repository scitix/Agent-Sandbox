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

package sandboxtemplate

import (
	"context"
	"fmt"
	"time"

	"github.com/scitix/agent-sandbox/pkg/sandboxrender"
	"github.com/scitix/agent-sandbox/pkg/templateimages"
	"github.com/scitix/agent-sandbox/pkg/utils/imageresolver"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// SandboxTemplateReconciler reconciles a SandboxTemplate object
type SandboxTemplateReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Registry *sandboxrender.RegistryRewrite
	Checker  imageresolver.AvailabilityChecker
	// Namespace is where template imagePullSecrets are resolved for background probes.
	Namespace string
}

// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxtemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxtemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxtemplates/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the SandboxTemplate object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.1/pkg/reconcile
func (r *SandboxTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	t := &agentsv1alpha1.SandboxTemplate{}
	if err := r.Get(ctx, req.NamespacedName, t); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if r.Checker == nil || !templateimages.Enabled(t, r.Registry) {
		return ctrl.Result{}, nil
	}
	plan := templateimages.Build(t, r.Registry, "")
	previous := t.Status.ImageAvailability
	now := time.Now()
	// Poll the inexpensive fingerprint so a changed registry mapping invalidates
	// the persisted result even though that ConfigMap is not a template event.
	if previous != nil && previous.ClusterID == r.Registry.LocalClusterID && previous.InputHash == plan.Hash && previous.NextCheckAt.After(now) {
		return ctrl.Result{RequeueAfter: min(5*time.Minute, time.Until(previous.NextCheckAt.Time))}, nil
	}
	before := t.DeepCopy()
	result := templateimages.Check(ctx, r.Checker, plan, r.Registry.LocalClusterID, imageresolver.WithPullSecrets(r.Namespace, t.Spec.Template.Spec.ImagePullSecrets))
	t.Status.ImageAvailability = result
	condition := metav1.Condition{Type: "ImagesAvailable", Status: metav1.ConditionTrue, Reason: "ImagesAccessible", Message: "All rendered template images are accessible", ObservedGeneration: t.Generation}
	failures := 0
	for _, image := range result.Images {
		if image.Reason != "Available" {
			failures++
			if condition.Status != metav1.ConditionFalse {
				condition.Status = metav1.ConditionUnknown
				condition.Reason = "ImageCheckUnavailable"
			}
			if image.Reason == "Missing" {
				condition.Status = metav1.ConditionFalse
				condition.Reason = "ImageMissing"
			}
			logf.FromContext(ctx).Info("Template image availability check failed", "cluster", result.ClusterID, "template", t.Name, "field", image.Field, "image", image.Image, "reason", image.Reason)
		}
	}
	if failures > 0 {
		condition.Message = fmt.Sprintf("%d template images failed availability checks; see status.imageAvailability.images", failures)
	}
	meta.SetStatusCondition(&t.Status.Conditions, condition)
	if err := r.Status().Patch(ctx, t, client.MergeFrom(before)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: min(5*time.Minute, time.Until(result.NextCheckAt.Time))}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SandboxTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentsv1alpha1.SandboxTemplate{}).
		Named("sandboxtemplate").
		Complete(r)
}
