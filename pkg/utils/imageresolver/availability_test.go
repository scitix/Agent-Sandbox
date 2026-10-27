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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAvailabilityAlwaysProbesDigestAndClassifies(t *testing.T) {
	for _, tc := range []struct {
		code   int
		reason string
	}{{200, ""}, {404, "Missing"}, {403, "Unauthorized"}, {503, "Unavailable"}} {
		t.Run(http.StatusText(tc.code), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v2/" {
					w.WriteHeader(200)
					return
				}
				requests++
				w.Header().Set("Content-Length", "2")
				w.Header().Set("Docker-Content-Digest", "sha256:"+strings.Repeat("a", 64))
				w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
				w.WriteHeader(tc.code)
			}))
			defer server.Close()
			resolver := NewResolver(nil, time.Hour)
			ref := strings.TrimPrefix(server.URL, "http://") + "/repo@sha256:" + strings.Repeat("a", 64)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err := resolver.CheckAvailability(ctx, ref)
			if tc.reason == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var ae *AvailabilityError
				if !errors.As(err, &ae) || ae.Reason != tc.reason {
					t.Fatalf("got %v want %s", err, tc.reason)
				}
			}
			if requests == 0 {
				t.Fatal("digest reference skipped registry probe")
			}
		})
	}
}
