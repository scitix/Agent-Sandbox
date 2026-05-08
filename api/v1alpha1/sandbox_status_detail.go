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

package v1alpha1

// SandboxStatusDetail holds structured diagnostic information written by the
// reconciler onto the Pod annotation "agentbox.navix.sh/sandbox-status-detail".
type SandboxStatusDetail struct {
	// Reason is a machine-readable cause, e.g. "Pulling", "ImagePullBackOff",
	// "ErrImagePull", "CrashLoopBackOff", "OOMKilled", "PodFailed".
	Reason string `json:"reason"`
	// Message is a human-readable description of the current state.
	Message string `json:"message"`
	// LastUpdatedTime is the RFC3339 timestamp when this record was last written.
	LastUpdatedTime string `json:"lastUpdatedTime"`
}
