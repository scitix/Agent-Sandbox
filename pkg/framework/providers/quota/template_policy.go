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

package quota

// PoolSizing says how the member Pools of a SandboxTemplate must be sized.
//
// It has three values rather than two because "billed" and "not billed" are not
// the whole story: a deployment can also have no rule at all, and collapsing
// that into "not billed" would silently take a feature away from it. The
// InstanceType catalog is an open-source feature — a deployment with a catalog
// and no quota backend sizes Pools by instance type perfectly well — so
// "nothing here is billed" must keep the caller's choice, not forbid one of
// the choices.
//
// The three states are therefore:
//
//   - Billed: this template's Pools spend quota. A Pool must name the quota it
//     spends (the quota.scitix.ai/url label) AND the instance type its units
//     are counted in.
//   - FreeForm: the quota Provider governs this deployment, and it says this
//     template is not billed. A Pool is sized by inlineResources alone; an
//     instance type or a quota label is refused, because each asserts
//     something the server would not honour — an instance bought, a quota
//     charged.
//   - Either: no billing rule applies to this template (the Provider is
//     absent, or implements no policy). Both shapes are accepted, which is
//     what every deployment did before this rule existed.
type PoolSizing string

const (
	// PoolSizingBilled — quota AND instance type are required.
	PoolSizingBilled PoolSizing = "billed"

	// PoolSizingFreeForm — inlineResources only; instance type and quota label
	// are refused.
	PoolSizingFreeForm PoolSizing = "free-form"

	// PoolSizingEither — no billing rule; the caller picks the shape.
	PoolSizingEither PoolSizing = "either"
)

// TemplatePolicy is an OPTIONAL interface a quota Provider may implement to
// say, per template, which of the two governed sizing shapes applies.
//
// The rule is the Provider's to state because only the Provider knows what a
// Template is billed against: the open-source build has no quota backend at
// all, and a closed-source one keys the answer off its own annotations. The
// answer reaches clients as PoolSizing on the Template and on the Env (stamped
// there by the reconciler so it does not cost a template lookup per read),
// which is what the console, the CLI and the Pool write path all switch on
// instead of each re-deriving the rule.
//
// Implementations must be total and cheap: this runs on read paths (Template
// projection), on every SandboxEnv reconcile, and on every member-Pool write.
// Annotations are the whole input — a Provider that needs more of the Template
// is asking the wrong interface for it.
type TemplatePolicy interface {
	// RequiresQuota reports whether a SandboxPool rendered from a Template
	// carrying these annotations must declare a quota label and an instance
	// type. It is the billed/free-form split; "either" is the absence of the
	// interface, never a return value.
	RequiresQuota(templateAnnotations map[string]string) bool
}

// Policy returns a Provider's TemplatePolicy, if it has one. The boolean is
// the difference between PoolSizingFreeForm and PoolSizingEither, which is why
// callers should use SizingFor rather than type-asserting themselves.
func Policy(provider Provider) (TemplatePolicy, bool) {
	if provider == nil {
		return nil, false
	}
	policy, ok := provider.(TemplatePolicy)
	return policy, ok
}

// SizingFor answers, for a Template carrying these annotations, which sizing
// shape its Pools take. This is the one place the three states are decided.
//
// A nil Provider, and a Provider that does not implement TemplatePolicy, both
// yield PoolSizingEither: the absence of a policy is not "nothing spends quota
// here", it is "this deployment has no opinion", and the open-source meaning of
// no opinion is that the caller still chooses.
func SizingFor(provider Provider, templateAnnotations map[string]string) PoolSizing {
	policy, ok := Policy(provider)
	if !ok {
		return PoolSizingEither
	}
	if policy.RequiresQuota(templateAnnotations) {
		return PoolSizingBilled
	}
	return PoolSizingFreeForm
}
