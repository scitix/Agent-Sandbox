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

package imageresolver

import (
	"context"
	"testing"
	"time"
)

func TestParseDigest(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "full imageID with registry and digest",
			input: "registry.example.com/project/idle-base@sha256:5dc5af625160247cb98c27a82eda42a9c64642e1d9ca2be16ae7ef3dc267293b",
			want:  "sha256:5dc5af625160247cb98c27a82eda42a9c64642e1d9ca2be16ae7ef3dc267293b",
		},
		{
			name:  "docker hub imageID",
			input: "docker.io/library/ubuntu@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1",
			want:  "sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1",
		},
		{
			name:  "bare digest reference",
			input: "sha256:5dc5af625160247cb98c27a82eda42a9c64642e1d9ca2be16ae7ef3dc267293b",
			want:  "sha256:5dc5af625160247cb98c27a82eda42a9c64642e1d9ca2be16ae7ef3dc267293b",
		},
		{
			name:    "tag-only reference has no digest",
			input:   "registry.example.com/repo:v1.0",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDigest(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseDigest(%q) = %q, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDigest(%q) error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("parseDigest(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeRef(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "fully qualified with tag",
			input: "registry.example.com/myrepo:v1.0",
			want:  "registry.example.com/myrepo:v1.0",
		},
		{
			name:  "docker hub short name stays familiar",
			input: "ubuntu:22.04",
			want:  "ubuntu:22.04",
		},
		{
			name:  "docker hub user repo stays familiar",
			input: "myuser/myrepo:latest",
			want:  "myuser/myrepo:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeRef(tt.input)
			if got != tt.want {
				t.Errorf("normalizeRef(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDigestFromStatus(t *testing.T) {
	r := NewResolver(nil, time.Hour)

	image := "registry.example.com/project/terminal-bench-195:tag"
	imageID := "registry.example.com/project/terminal-bench-195@sha256:e034397bcf697441b211789e72e832815aadd94106998b833400ff53a3c3e12b"

	digest, err := r.DigestFromStatus(image, imageID)
	if err != nil {
		t.Fatalf("DigestFromStatus() error: %v", err)
	}
	if digest != "sha256:e034397bcf697441b211789e72e832815aadd94106998b833400ff53a3c3e12b" {
		t.Errorf("got digest %q, want sha256:e034397...", digest)
	}

	// Verify that DigestFromStatus does NOT cache the tag→digest mapping.
	// Use a private registry that won't resolve, so if cache was populated
	// Resolve would succeed; if not, it should fail.
	key := normalizeRef(image)
	if _, ok := r.get(key); ok {
		t.Fatal("DigestFromStatus should NOT cache tag→digest mappings")
	}
}

func TestDigestFromStatus_ReturnsDigest(t *testing.T) {
	r := NewResolver(nil, time.Hour)

	image1 := "registry.example.com/project/terminal-bench-195:tag1"
	imageID1 := "registry.example.com/project/terminal-bench-195@sha256:e034397bcf697441b211789e72e832815aadd94106998b833400ff53a3c3e12b"

	digest1, err := r.DigestFromStatus(image1, imageID1)
	if err != nil {
		t.Fatalf("DigestFromStatus(image1) error: %v", err)
	}
	if digest1 != "sha256:e034397bcf697441b211789e72e832815aadd94106998b833400ff53a3c3e12b" {
		t.Errorf("got %q", digest1)
	}
}

func TestResolve_DigestRef(t *testing.T) {
	r := NewResolver(nil, time.Hour)

	// Resolving a digest-based ref should extract the digest directly, no network.
	ref := "registry.example.com/repo@sha256:5dc5af625160247cb98c27a82eda42a9c64642e1d9ca2be16ae7ef3dc267293b"
	got, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("Resolve(digest ref) error: %v", err)
	}
	if got != "sha256:5dc5af625160247cb98c27a82eda42a9c64642e1d9ca2be16ae7ef3dc267293b" {
		t.Errorf("got %q, want sha256:5dc5af...", got)
	}
}

func TestResolve_CacheExpiry(t *testing.T) {
	r := NewResolver(nil, 1*time.Millisecond) // very short TTL

	// Populate cache via Resolve with a digest ref.
	digestRef := "registry.example.com/repo@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1"
	_, err := r.Resolve(context.Background(), digestRef)
	if err != nil {
		t.Fatalf("Resolve(digest ref) error: %v", err)
	}

	// Should be in cache now.
	key := normalizeRef(digestRef)
	if _, ok := r.get(key); !ok {
		t.Fatal("expected cache hit immediately after Resolve")
	}

	// Wait for TTL expiry.
	time.Sleep(5 * time.Millisecond)

	// Cache should be expired.
	if _, ok := r.get(key); ok {
		t.Fatal("expected cache miss after TTL expiry")
	}
}

func TestResolve_EmptyRef(t *testing.T) {
	r := NewResolver(nil, time.Hour)
	_, err := r.Resolve(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty ref")
	}
}

func TestDigestFromStatus_EmptyImageID(t *testing.T) {
	r := NewResolver(nil, time.Hour)
	_, err := r.DigestFromStatus("registry.example.com/repo:tag", "")
	if err == nil {
		t.Fatal("expected error for empty imageID")
	}
}

func TestParseDigest_BareShortHash(t *testing.T) {
	// Bare "sha256:old" is not a valid digest (too short hex).
	_, err := parseDigest("sha256:old")
	if err == nil {
		t.Fatal("expected error for invalid short digest sha256:old")
	}
}

func TestParseDigest_Valid64CharHex(t *testing.T) {
	digest, err := parseDigest("sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest != "sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1" {
		t.Errorf("got %q", digest)
	}
}

func TestResolve_DockerHubShortName(t *testing.T) {
	r := NewResolver(nil, time.Hour)
	// ubuntu:22.04 should parse and either resolve from Docker Hub (if reachable)
	// or fail with a network error. Either way, it should not panic.
	_, _ = r.Resolve(context.Background(), "ubuntu:22.04")

	// Use a definitely-unreachable registry to verify error handling.
	_, err := r.Resolve(context.Background(), "nonexistent-registry.invalid/repo:tag")
	if err == nil {
		t.Fatal("expected error for unreachable registry")
	}
}
