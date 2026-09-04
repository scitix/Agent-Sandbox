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

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/egressproxy"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// recordingRepusher stands in for the hook runner's exec channel.
type recordingRepusher struct {
	calls int
	err   error
	// seen is the annotation value the pod carried when the push happened, which
	// is what proves the patch landed before the delivery rather than after.
	seen string
}

func (r *recordingRepusher) RepushEgressPolicy(_ context.Context, pod *corev1.Pod) error {
	r.calls++
	r.seen = pod.Annotations[agentsv1alpha1.SandboxEgressPolicyAnnotationKey]
	return r.err
}

// liveSandboxID is the sandbox every fixture in this file claims.
const liveSandboxID = "sbx-1"

// claimedPod builds a Running sandbox Pod. withProxy controls whether it carries
// the gateway sidecar, which is the one thing that decides if a policy can be
// enforced at all.
func claimedPod(withProxy bool) *corev1.Pod {
	sandboxID := liveSandboxID
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-" + sandboxID,
			Namespace: "team-a",
			Labels: map[string]string{
				agentsv1alpha1.SandboxIDLabelKey:    sandboxID,
				agentsv1alpha1.SandboxPhaseLabelKey: string(agentsv1alpha1.SandboxPhaseRunning),
				agentsv1alpha1.SandboxPoolLabelKey:  "pool-a",
			},
			Annotations: map[string]string{agentsv1alpha1.SandboxIDAnnotationKey: sandboxID},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "s:1"}}},
	}
	if withProxy {
		pod.Spec.InitContainers = []corev1.Container{{Name: agentsv1alpha1.EgressProxyContainerName, Image: "p:1"}}
	}
	return pod
}

func newLiveNetworkService(t *testing.T, pods ...client.Object) (*k8sSandboxService, *recordingRepusher) {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("builder: %v", err)
	}
	rp := &recordingRepusher{}
	return &k8sSandboxService{
		client:         cb.WithObjects(pods...).Build(),
		egressRepusher: rp,
	}, rp
}

func TestUpdateSandboxNetwork_StampsAndDelivers(t *testing.T) {
	svc, rp := newLiveNetworkService(t, claimedPod(true))

	np := &agentsv1alpha1.SandboxNetworkPolicy{
		Egress: &agentsv1alpha1.EgressRules{AllowedDomains: []string{"pypi.org"}},
	}
	if appErr := svc.UpdateSandboxNetwork(context.Background(), "team-a", "sbx-1", np); appErr != nil {
		t.Fatalf("update: %v", appErr)
	}

	if rp.calls != 1 {
		t.Fatalf("expected exactly one push to the sidecar, got %d", rp.calls)
	}
	// The push must see the new policy: recording it and delivering the old one
	// would report success over a sandbox still running the previous ruleset.
	var pol egressproxy.Policy
	if err := json.Unmarshal([]byte(rp.seen), &pol); err != nil {
		t.Fatalf("pushed value is not a policy: %v (%q)", err, rp.seen)
	}
	if len(pol.AllowedDomains) != 1 || pol.AllowedDomains[0] != "pypi.org" || !pol.Enforce {
		t.Fatalf("wrong policy delivered: %+v", pol)
	}

	// And it must be durable, so a re-arm after a restart applies the same thing.
	stored := &corev1.Pod{}
	if err := svc.client.Get(context.Background(),
		client.ObjectKey{Namespace: "team-a", Name: "pod-sbx-1"}, stored); err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Annotations[agentsv1alpha1.SandboxEgressPolicyAnnotationKey] != rp.seen {
		t.Fatal("the stamped annotation and the delivered policy disagree")
	}
}

// An empty body is a full replacement meaning "unrestricted", not "no change" —
// so it has to produce an enforcing allow-all rather than nothing at all.
func TestUpdateSandboxNetwork_EmptyPolicyIsUnrestricted(t *testing.T) {
	svc, rp := newLiveNetworkService(t, claimedPod(true))

	if appErr := svc.UpdateSandboxNetwork(context.Background(), "team-a", "sbx-1", nil); appErr != nil {
		t.Fatalf("update: %v", appErr)
	}
	var pol egressproxy.Policy
	_ = json.Unmarshal([]byte(rp.seen), &pol)
	if !pol.Enforce || pol.DisableEgress {
		t.Fatalf("expected an enforcing allow-all, got %+v", pol)
	}
	if !pol.AllowPrivateNetworks {
		t.Fatal("an unrestricted policy must reach private ranges, as a sandbox with no policy does")
	}
}

// A Pod that predates its Env turning the gateway on has neither the sidecar nor
// the iptables redirect. Accepting a policy for it would report success over
// traffic that leaves unfiltered.
func TestUpdateSandboxNetwork_RefusedWithoutTheSidecar(t *testing.T) {
	svc, rp := newLiveNetworkService(t, claimedPod(false))

	appErr := svc.UpdateSandboxNetwork(context.Background(), "team-a", "sbx-1", nil)
	if appErr == nil {
		t.Fatal("a sandbox without the gateway must be refused")
	}
	if appErr.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected a 400, got %v", appErr.Code)
	}
	if rp.calls != 0 {
		t.Fatal("nothing should have been pushed")
	}
}

func TestUpdateSandboxNetwork_UnknownSandboxIsNotFound(t *testing.T) {
	svc, _ := newLiveNetworkService(t)
	appErr := svc.UpdateSandboxNetwork(context.Background(), "team-a", "nope", nil)
	if appErr == nil || appErr.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected NotFound, got %v", appErr)
	}
}

// A failed delivery must not read as success: the annotation is already stamped,
// but the sandbox is still enforcing the previous ruleset — which matters most
// when the caller was tightening.
func TestUpdateSandboxNetwork_DeliveryFailureIsReported(t *testing.T) {
	svc, rp := newLiveNetworkService(t, claimedPod(true))
	rp.err = errors.New("exec refused")

	appErr := svc.UpdateSandboxNetwork(context.Background(), "team-a", "sbx-1", nil)
	if appErr == nil {
		t.Fatal("a failed push must surface")
	}
	if !strings.Contains(appErr.Message, "still running under the previous one") {
		t.Fatalf("the message must say the old policy is still in force, got %q", appErr.Message)
	}
}
