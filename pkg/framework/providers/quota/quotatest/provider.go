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

// Package quotatest holds the quota Provider doubles the rest of the tree
// shares.
//
// It exists because the billing question (quota.TemplatePolicy) is asked in
// three packages that must all agree — the template projection, the member-Pool
// write path, and the SandboxEnv reconciler — and each of them needs a Provider
// that answers it. Three copies of a four-line type would drift into three
// different notions of "billed"; this is the one.
//
// Not a _test.go file: the callers are tests in OTHER packages, and Go cannot
// import a package's test files.
package quotatest

import (
	"github.com/scitix/agent-sandbox/pkg/framework/providers/quota"
)

// BillingKey is the annotation a Policy double bills on by default: the
// reservation annotation real templates carry, so a test that forgets to pass a
// key still exercises the production shape rather than a private one.
const BillingKey = "scheduling.navix.sh/reservation-replica-quota"

// Policy is a quota.Provider that bills templates whose annotations carry Key —
// the shape every closed-source Provider has, reduced to the one thing tests
// assert on.
//
// The zero value is a Provider that bills nothing, so `quotatest.Policy{}` is
// exactly as inert as the open-source Noop it embeds.
type Policy struct {
	quota.Noop

	// Key is the annotation whose presence marks a template as billed. Empty
	// means this Provider never bills anything — the open-source answer, but
	// with the optional interface still implemented, which is the case worth
	// distinguishing from Noop in a test.
	Key string
}

// RequiresQuota implements quota.TemplatePolicy.
func (p Policy) RequiresQuota(templateAnnotations map[string]string) bool {
	if p.Key == "" {
		return false
	}
	return templateAnnotations[p.Key] != ""
}

// Billing returns a Policy that bills on BillingKey — the common case.
func Billing() Policy { return Policy{Key: BillingKey} }

// Static is a Provider that answers the billing question with one fixed
// answer, for tests that are about what the CALLER does with the answer rather
// than about how it is derived.
type Static struct {
	quota.Noop
	Billed bool
}

// RequiresQuota implements quota.TemplatePolicy.
func (s Static) RequiresQuota(map[string]string) bool { return s.Billed }
