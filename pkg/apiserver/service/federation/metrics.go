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

package federation

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var fedLabels = []string{"cluster", "namespace", "env", "member", "group"}

var (
	// federationIdle mirrors the idle capacity each cluster advertises for an
	// Env, as seen from this Worker's registry (includes this Worker's own
	// cluster). Rewritten in full on every publish so a cluster that ages out
	// disappears from the metric.
	federationIdle = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentbox_federation_env_idle",
		Help: "Idle sandbox capacity advertised per cluster for a SandboxEnv, as seen in this Worker's federation registry.",
	}, fedLabels)

	federationRunning = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentbox_federation_env_running",
		Help: "Running sandbox count advertised per cluster for a SandboxEnv in this Worker's federation registry.",
	}, fedLabels)

	federationDesired = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentbox_federation_env_desired",
		Help: "Desired replica count advertised per cluster for a SandboxEnv in this Worker's federation registry.",
	}, fedLabels)
)

func init() {
	ctrlmetrics.Registry.MustRegister(federationIdle, federationRunning, federationDesired)
}

// PublishMetrics rewrites the federation gauges from a full registry snapshot.
// Resetting first ensures records that aged out since the last publish stop
// being exported instead of lingering at a stale value.
func PublishMetrics(snapshot []Capacity) {
	federationIdle.Reset()
	federationRunning.Reset()
	federationDesired.Reset()
	for _, c := range snapshot {
		lv := []string{c.ClusterID, c.Namespace, c.EnvName, c.MemberPool, c.ScalingGroup}
		federationIdle.WithLabelValues(lv...).Set(float64(c.Idle))
		federationRunning.WithLabelValues(lv...).Set(float64(c.Running))
		federationDesired.WithLabelValues(lv...).Set(float64(c.Desired))
	}
}
