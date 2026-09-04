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

package poststarthooks

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/egressproxy"
)

// sandboxCABundlePath is where envd installs an injected CA. Pointing the
// per-runtime environment variables at the same file gives clients that ignore
// the system trust store the same chain.
const sandboxCABundlePath = "/etc/ssl/certs/ca-certificates.crt"

// defaultCATTL bounds a per-sandbox CA when the Env does not pin one.
const defaultCATTL = 24 * time.Hour

// caEnvVars are the runtime-specific trust-store overrides. Several toolchains
// (Node, Python requests, curl, git, the AWS and Rust SDKs, gRPC) read their own
// variable instead of the system bundle, so interception would fail for them
// without these even though envd installed the CA correctly.
var caEnvVars = []string{
	"SSL_CERT_FILE",
	"REQUESTS_CA_BUNDLE",
	"CURL_CA_BUNDLE",
	"NODE_EXTRA_CA_CERTS",
	"PIP_CERT",
	"GIT_SSL_CAINFO",
	"AWS_CA_BUNDLE",
	"CARGO_HTTP_CAINFO",
	"GRPC_DEFAULT_SSL_ROOTS_FILE_PATH",
}

// injectTemplateRe matches a credential reference in a header value. It is the
// E2B placeholder syntax that Secret.fill() produces, so a rule crosses the API
// boundary unrewritten.
var injectTemplateRe = regexp.MustCompile(`\$\{e2b\.secrets\.([a-zA-Z0-9_-]+)\}`)

// injectionPlan is everything the operator resolved for one claimed sandbox.
// It holds live credential material, so it exists only for the duration of one
// OnSandboxReady call and is never persisted.
type injectionPlan struct {
	// secrets is the payload pushed to the sidecar over exec.
	secrets egressproxy.Secrets
	// caCertPEM is installed into the sandbox's trust store via envd.
	caCertPEM string
	// envVars are the sandbox-visible variables: the trust-store path overrides
	// for the minted CA. None of these is a credential.
	envVars map[string]string
}

// prepareInjection reads the egress-inject annotation, resolves each credential
// from its Secret, mints a per-sandbox CA, and assembles both halves of the
// delivery: what the sandbox learns (the CA certificate) and what the sidecar
// learns (CA key + real values + rules).
//
// Returns (nil, nil) when the pod has no injection configured.
func (r *Runner) prepareInjection(ctx context.Context, pod *corev1.Pod) (*injectionPlan, error) {
	raw := pod.Annotations[agentsv1alpha1.SandboxEgressInjectAnnotationKey]
	if raw == "" {
		return nil, nil
	}
	if r.clientset == nil {
		return nil, fmt.Errorf("no kubernetes client: cannot resolve injected credentials")
	}

	var si agentsv1alpha1.SecretInjection
	if err := json.Unmarshal([]byte(raw), &si); err != nil {
		return nil, fmt.Errorf("decode egress-inject annotation: %w", err)
	}

	// Resolve every credential once. Secret reads are the only point where
	// plaintext enters the operator, and it stays on the stack from here.
	values := make(map[string]string, len(si.Credentials))
	envVars := make(map[string]string, len(caEnvVars))
	for i := range si.Credentials {
		c := &si.Credentials[i]
		secret, err := r.clientset.CoreV1().Secrets(pod.Namespace).Get(ctx, c.ValueFrom.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("read secret %s/%s for credential %q: %w",
				pod.Namespace, c.ValueFrom.Name, c.Name, err)
		}
		v, ok := secret.Data[c.ValueFrom.Key]
		if !ok {
			return nil, fmt.Errorf("secret %s/%s has no key %q (credential %q)",
				pod.Namespace, c.ValueFrom.Name, c.ValueFrom.Key, c.Name)
		}
		if len(v) == 0 {
			return nil, fmt.Errorf("credential %q resolves to an empty value", c.Name)
		}
		values[c.Name] = string(v)
	}

	rules := make([]egressproxy.InjectRule, 0, len(si.Rules))
	for i := range si.Rules {
		src := &si.Rules[i]
		rule := egressproxy.InjectRule{
			Host:         strings.ToLower(src.Host),
			Ports:        toIntSlice(src.Ports),
			PathPrefixes: append([]string(nil), src.PathPrefixes...),
			Methods:      append([]string(nil), src.Methods...),
		}
		for j := range src.Headers {
			h := &src.Headers[j]
			value, err := expandCredentialTemplate(h.Value, values)
			if err != nil {
				return nil, fmt.Errorf("rule %q header %q: %w", src.Host, h.Name, err)
			}
			mode := string(h.Mode)
			if mode == "" {
				mode = egressproxy.ModeOverride
			}
			rule.Headers = append(rule.Headers, egressproxy.InjectHeader{
				Name:  h.Name,
				Value: value,
				Mode:  mode,
			})
		}
		rules = append(rules, rule)
	}

	ttl := defaultCATTL
	if si.CACertTTL != nil && si.CACertTTL.Duration > 0 {
		ttl = si.CACertTTL.Duration
	}
	sandboxID := pod.Annotations[agentsv1alpha1.SandboxIDAnnotationKey]
	certPEM, keyPEM, err := egressproxy.GenerateCA("agentbox-egress-ca:"+sandboxID, ttl)
	if err != nil {
		return nil, fmt.Errorf("generate per-sandbox CA: %w", err)
	}

	for _, k := range caEnvVars {
		envVars[k] = sandboxCABundlePath
	}

	return &injectionPlan{
		secrets: egressproxy.Secrets{
			SandboxID: sandboxID,
			CACertPEM: certPEM,
			CAKeyPEM:  keyPEM,
			Rules:     rules,
		},
		caCertPEM: certPEM,
		envVars:   envVars,
	}, nil
}

// expandCredentialTemplate substitutes ${e2b.secrets.name} with the resolved
// credential. An unknown name is an error rather than an empty expansion: a
// silently blank Authorization header is far harder to diagnose than a refusal.
func expandCredentialTemplate(tmpl string, values map[string]string) (string, error) {
	var missing string
	out := injectTemplateRe.ReplaceAllStringFunc(tmpl, func(m string) string {
		name := injectTemplateRe.FindStringSubmatch(m)[1]
		v, ok := values[name]
		if !ok {
			missing = name
			return m
		}
		return v
	})
	if missing != "" {
		return "", fmt.Errorf("references undeclared credential %q", missing)
	}
	return out, nil
}

func toIntSlice(in []int32) []int {
	if len(in) == 0 {
		return nil
	}
	out := make([]int, 0, len(in))
	for _, v := range in {
		out = append(out, int(v))
	}
	return out
}

// mergeInitHook folds the CA certificate and the sandbox-visible env vars into
// the pod's existing envd /init hook, creating one if the sandbox declared no
// env vars of its own.
//
// Merging rather than issuing a second /init is required, not stylistic: envd
// replaces the whole user env-var set on every call that carries envVars, so a
// second request would wipe whatever the create request had set.
func mergeInitHook(hooks []Action, caCertPEM string, envVars map[string]string) []Action {
	if caCertPEM == "" && len(envVars) == 0 {
		return ensureInitHook(hooks)
	}
	for i := range hooks {
		h := hooks[i].HTTPPost
		if h == nil || h.Path != envdInitPath {
			continue
		}
		if h.Body == nil {
			h.Body = map[string]any{}
		}
		if caCertPEM != "" {
			h.Body["caBundle"] = caCertPEM
		}
		if len(envVars) > 0 {
			merged := map[string]string{}
			// The sandbox's own values come first so an operator-managed decoy
			// or CA path always wins over a colliding user value.
			if existing, ok := h.Body["envVars"].(map[string]string); ok {
				maps.Copy(merged, existing)
			} else if existing, ok := h.Body["envVars"].(map[string]any); ok {
				for k, v := range existing {
					if s, isStr := v.(string); isStr {
						merged[k] = s
					}
				}
			}
			maps.Copy(merged, envVars)
			h.Body["envVars"] = merged
		}
		return hooks
	}

	body := map[string]any{}
	if caCertPEM != "" {
		body["caBundle"] = caCertPEM
	}
	if len(envVars) > 0 {
		body["envVars"] = envVars
	}
	return append(hooks, Action{HTTPPost: &HTTPPostAction{
		Port: envdInitPort,
		Path: envdInitPath,
		Body: body,
	}})
}

// envdInitPort is the in-sandbox envd listener that owns trust-store and
// env-var setup.
const envdInitPort int32 = 49983

// envdInitPath is the endpoint that carries the trust-store certificate and the
// sandbox's environment variables, and that lifts envd's await-init gate.
const envdInitPath = "/init"

// ensureInitHook appends an empty /init when the hook list has none.
//
// envd can be run with a gate that refuses to execute anything until it has
// received an /init (see the await-init patch), which exists because a command
// accepted before setup runs in a sandbox that is not yet configured. That gate
// can only ever be lifted if the call always happens — including for a sandbox
// with no environment variables and no injected certificate, which otherwise
// had nothing to send and sent nothing.
//
// The call is harmless where the gate is off: /init with an empty body sets
// nothing.
func ensureInitHook(hooks []Action) []Action {
	for i := range hooks {
		if h := hooks[i].HTTPPost; h != nil && h.Path == envdInitPath {
			return hooks
		}
	}
	return append(hooks, Action{HTTPPost: &HTTPPostAction{
		Port: envdInitPort,
		Path: envdInitPath,
		Body: map[string]any{},
	}})
}
