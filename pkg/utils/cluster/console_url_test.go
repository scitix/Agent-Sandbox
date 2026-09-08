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

package cluster

import "testing"

func TestConsoleBaseURLArrivesFromTheHub(t *testing.T) {
	// A Worker is never configured with the console's address; it learns it
	// from the snapshot the hub publishes. Without this the approval refusal
	// has nowhere to send anyone.
	s := &Store{}
	if got := s.ConsoleBaseURL(); got != "" {
		t.Fatalf("expected empty before any config, got %q", got)
	}
	s.ApplyConfig(ClusterConfig{
		Clusters:       []ClusterEntry{{ID: "c1"}},
		ConsoleBaseURL: "https://console.example.com/agentbox",
	})
	if got := s.ConsoleBaseURL(); got != "https://console.example.com/agentbox" {
		t.Fatalf("got %q", got)
	}
}

func TestASnapshotWithoutAConsoleURLDoesNotEraseOne(t *testing.T) {
	// The hub republishes the whole snapshot on every change, and a hub that
	// has not been given a console address publishes an empty one. Letting that
	// overwrite would make the link appear and disappear with unrelated config
	// edits.
	s := &Store{}
	s.ApplyConfig(ClusterConfig{
		Clusters:       []ClusterEntry{{ID: "c1"}},
		ConsoleBaseURL: "https://console.example.com",
	})
	s.ApplyConfig(ClusterConfig{Clusters: []ClusterEntry{{ID: "c1"}, {ID: "c2"}}})
	if got := s.ConsoleBaseURL(); got != "https://console.example.com" {
		t.Fatalf("the console address should have survived, got %q", got)
	}
}

func TestNilStoreAnswersEmpty(t *testing.T) {
	// The API server passes this method as a function value before any config
	// has arrived, and a single-cluster deployment may have no store at all.
	var s *Store
	if got := s.ConsoleBaseURL(); got != "" {
		t.Fatalf("got %q", got)
	}
}
