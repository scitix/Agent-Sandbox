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

package notify

import "testing"

func scalar(v float64) Scalar {
	return &v
}

func TestFormatNumber(t *testing.T) {
	cases := []struct {
		name string
		in   Scalar
		want string
	}{
		{"nil", nil, "N/A"},
		{"integer", scalar(42), "42"},
		{"fractional", scalar(3.14159), "3.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatNumber(c.in); got != c.want {
				t.Errorf("formatNumber(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestFormatPercent(t *testing.T) {
	cases := []struct {
		name string
		in   Scalar
		want string
	}{
		{"nil", nil, "N/A"},
		{"zero", scalar(0), "0.0%"},
		{"half", scalar(0.5), "50.0%"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatPercent(c.in); got != c.want {
				t.Errorf("formatPercent(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestFormatSeconds(t *testing.T) {
	cases := []struct {
		name string
		in   Scalar
		want string
	}{
		{"nil", nil, "N/A"},
		{"milliseconds", scalar(0.25), "250ms"},
		{"seconds", scalar(12.3), "12.3s"},
		{"minutes", scalar(125), "2.1min"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatSeconds(c.in); got != c.want {
				t.Errorf("formatSeconds(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func reportWithCombined(metrics, derived map[string]Scalar) *Report {
	return &Report{
		Scopes: []ScopeReport{
			{Scope: "combined", Metrics: metrics, Derived: derived},
		},
	}
}

func TestDetermineHeaderColor(t *testing.T) {
	cases := []struct {
		name    string
		metrics map[string]Scalar
		derived map[string]Scalar
		want    string
	}{
		{
			name:    "healthy",
			metrics: map[string]Scalar{"failedReplicasCurrent": scalar(0)},
			derived: map[string]Scalar{"createErrorRate": scalar(0), "envoy5xxRate": scalar(0), "createNoIdleRate": scalar(0), "claimTimeoutRate": scalar(0)},
			want:    "green",
		},
		{
			name:    "high create error rate is red",
			metrics: map[string]Scalar{"failedReplicasCurrent": scalar(0)},
			derived: map[string]Scalar{"createErrorRate": scalar(0.2)},
			want:    "red",
		},
		{
			name:    "high envoy 5xx rate is red",
			metrics: map[string]Scalar{"failedReplicasCurrent": scalar(0)},
			derived: map[string]Scalar{"envoy5xxRate": scalar(0.05)},
			want:    "red",
		},
		{
			name:    "many failed replicas is red",
			metrics: map[string]Scalar{"failedReplicasCurrent": scalar(11)},
			derived: map[string]Scalar{},
			want:    "red",
		},
		{
			name:    "elevated no-idle rate is orange",
			metrics: map[string]Scalar{"failedReplicasCurrent": scalar(0)},
			derived: map[string]Scalar{"createNoIdleRate": scalar(0.1)},
			want:    "orange",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			report := reportWithCombined(c.metrics, c.derived)
			if got := determineHeaderColor(report); got != c.want {
				t.Errorf("determineHeaderColor() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestDetermineHeaderColorNoCombinedScope(t *testing.T) {
	report := &Report{Scopes: []ScopeReport{{Scope: "cluster-a"}}}
	if got := determineHeaderColor(report); got != "blue" {
		t.Errorf("determineHeaderColor() = %q, want %q", got, "blue")
	}
}
