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

package domain

// AuthInfo holds the authenticated caller's identity extracted from the API key or JWT.
// It has no dependency on gin or net/http.
type AuthInfo struct {
	Namespace string
	Role      string
	User      string
	Team      string
	QuotaURL  string
	// AuthMethod indicates how the caller was authenticated: "apikey" or "jwt".
	AuthMethod string
	// Email is the user's email address (populated from JWT claims).
	Email string
	// Name is the user's display name (populated from JWT claims).
	Name string
}
