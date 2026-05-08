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

package indexer

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

func GetFakeClientBuilderWithIndexers() (*fake.ClientBuilder, error) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("add core scheme: %w", err)
	}
	if err := agentsv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("add api scheme: %w", err)
	}
	cb := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&agentsv1alpha1.SandboxPool{}).
		WithIndex(&corev1.Pod{}, IndexFieldSandboxID, IndexerFuncSandboxID).
		WithIndex(&corev1.Pod{}, IndexFieldSandboxPool, IndexerFuncSandboxPool).
		WithIndex(&corev1.Pod{}, IndexFieldSandboxPhase, IndexerFuncSandboxPhase).
		WithIndex(&corev1.Pod{}, IndexFieldSandboxPoolPhase, IndexerFuncSandboxPoolPhase).
		WithIndex(&corev1.Pod{}, IndexFieldTeam, IndexerFuncPodTeam).
		WithIndex(&corev1.Pod{}, IndexFieldUser, IndexerFuncPodUser).
		WithIndex(&agentsv1alpha1.SandboxPool{}, IndexFieldTeam, IndexerFuncPoolTeam).
		WithIndex(&agentsv1alpha1.SandboxPool{}, IndexFieldUser, IndexerFuncPoolUser)

	return cb, nil
}
