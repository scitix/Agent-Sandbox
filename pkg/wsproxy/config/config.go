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

// Package config holds the wsproxy runtime configuration.
// All env-var defaults and flag registrations are centralised here so the
// rest of the codebase is free of os.Getenv calls.
package config

import (
	"flag"
	"os"
	"strconv"
)

const (
	defaultListenAddr              = ":9003"
	defaultInternalAddr            = ":9004"
	defaultManagedAgentGatewayAddr = ":9005"
	defaultClustersFilePath        = "/etc/agentbox/clusters.yaml"
	defaultAPIKeyNamespace         = "agentbox-system"
	defaultImagesCatalogConfigMap  = "agentbox-images-catalog"
	defaultNotificationConfigMap   = "agentbox-notifications"
)

// Config holds all wsproxy runtime settings.
// Each field maps 1:1 to a CLI flag whose default value is the matching
// environment variable (or a hard-coded default when the env var is absent).
type Config struct {
	// ListenAddr is the address the terminal WebSocket proxy listens on.
	// Flag: --listen-addr  Env: WSPROXY_LISTEN_ADDR  Default: :9003
	ListenAddr string

	// InternalAddr is the address the internal management API listens on.
	// Only used when SyncToken is non-empty.
	// Flag: --internal-addr  Env: WSPROXY_INTERNAL_ADDR  Default: :9004
	InternalAddr string

	// ClustersFilePath is the path to clusters.yaml consumed by cluster.Store.
	// Flag: --clusters-config  Env: CLUSTERS_CONFIG_PATH
	ClustersFilePath string

	// Secret is the single shared secret used for:
	//   - dialling Worker /v1/ws/sync endpoints (AGENTBOX-SYNC-TOKEN header);
	//   - gating the legacy /internal/* routes (static token check);
	//   - verifying Bearer JWTs issued by the BFF (HS256).
	// When empty the sync manager is disabled and JWT auth is off.
	// Flag: --secret  Env: AGENTBOX_SECRET
	Secret string

	// AdminKey is the shared admin API key used by the internal API auth
	// middleware to recognise admin callers via AGENTBOX-API-KEY header.
	// When empty the internal API runs in dev mode (anonymous admin).
	// Flag: --admin-key  Env: AGENTBOX_ADMIN_KEY
	AdminKey string

	// MaxKeysPerUser is the per-(namespace, user) API key count limit.
	// 0 means unlimited.
	// Flag: --max-keys-per-user  Env: AGENTBOX_MAX_KEYS_PER_USER
	MaxKeysPerUser int

	// APIKeyNamespace is the Kubernetes namespace where API key Secrets are stored.
	// Flag: --apikey-namespace  Env: AGENTBOX_APIKEY_NAMESPACE  Default: agentbox-system
	APIKeyNamespace string

	// ImagesCatalogConfigMap is the name of the ConfigMap that holds the images
	// catalog. It is stored in APIKeyNamespace.
	// Flag: --images-catalog-configmap  Env: AGENTBOX_IMAGES_CATALOG_CONFIGMAP
	// Default: agentbox-images-catalog
	ImagesCatalogConfigMap string

	// ManagedAgentEnabled starts the ManagedAgent controller alongside the
	// proxy. It lives here rather than in the worker binary because a
	// ManagedAgent is a control-plane object: the worker chart installs on
	// every cluster, so reconciling it there would run one controller per
	// cluster for a single set of resources.
	// Flag: --managed-agent  Env: AGENTBOX_MANAGED_AGENT_ENABLED  Default: false
	ManagedAgentEnabled bool

	// ManagedAgentGatewayAddr is the listener that serves published agents to
	// callers outside the cluster. It is separate from the internal API because
	// that one trusts a manager token: an ingress may only be pointed at a port
	// where every request carries its own credential. Empty disables publishing.
	ManagedAgentGatewayAddr string

	// ManagedAgentPublicBaseURL is the shared route published agents answer on,
	// e.g. "https://console.example.com/agentbox/api/managed-agents". It is
	// configuration rather than something the controller can derive: only the
	// chart knows the hostname and base path the ingress was created with.
	ManagedAgentPublicBaseURL string

	// ManagedAgentProxyService is this process as in-cluster callers address it,
	// "<service>.<namespace>:<port>". Reported as status.endpoint so nothing
	// hands out the Brain's own unauthenticated address.
	ManagedAgentProxyService string

	// ManagedAgentNamespace restricts the controller's cache and watches to one
	// namespace. Empty watches all namespaces, which needs cluster-wide RBAC.
	// Flag: --managed-agent-namespace  Env: AGENTBOX_MANAGED_AGENT_NAMESPACE
	ManagedAgentNamespace string

	// ManagedAgentBrainImage is the Brain image an agent gets when it names none.
	//
	// This is what lets an agent be created from a prompt alone: requiring the
	// caller to name an image means knowing which one ships a gateway compatible
	// with this control plane, and that is the deployment's business rather than
	// the tenant's.
	//
	// Empty keeps the image required, which is correct for a deployment that has
	// not published one — inventing a reference would surface much later as an
	// ImagePullBackOff, a long way from the cause.
	// Flag: --managed-agent-brain-image  Env: AGENTBOX_MANAGED_AGENT_BRAIN_IMAGE
	ManagedAgentBrainImage string

	// ManagedAgentBrainImageTag is the tag paired with the repository above.
	// Kept separate so the chart can carry `image.repository` and `image.tag` as
	// two values, the way every other image in it is expressed.
	// Flag: --managed-agent-brain-image-tag
	// Env: AGENTBOX_MANAGED_AGENT_BRAIN_IMAGE_TAG
	ManagedAgentBrainImageTag string

	// ManagedAgentBrainPullSecrets names imagePullSecrets for the default Brain
	// image, comma-separated. A private registry needs them, and an agent created
	// from a prompt alone has no place to declare them.
	// Flag: --managed-agent-brain-pull-secrets
	// Env: AGENTBOX_MANAGED_AGENT_BRAIN_PULL_SECRETS
	ManagedAgentBrainPullSecrets string

	// ── Default sandbox supply ────────────────────────────────────────────────
	//
	// Together these describe one E2B-compatible sandbox service an agent is given
	// when it declares no hands of its own. It is the other half of creating an
	// agent from a prompt alone: the three sandbox branches all ask the caller
	// which cluster, which environment and which image, and the image in
	// particular fails silently — a sandbox started from a pool's default image
	// comes up and then refuses every command.
	//
	// The credential is named, not carried: the pod that reads it is the Brain, in
	// the agent's namespace, so what this process needs is a Secret reference to
	// render rather than the key itself. Nothing here ever holds the value.
	//
	// HandsAPIURL empty disables the default entirely.
	// Flag: --managed-agent-hands-api-url
	// Env: AGENTBOX_MANAGED_AGENT_HANDS_API_URL
	ManagedAgentHandsAPIURL string

	// ManagedAgentHandsDomain is the data-plane gateway, host plus any ingress
	// path. Omitting the path is the usual cause of "the sandbox exists but no
	// port answers".
	// Flag: --managed-agent-hands-domain
	// Env: AGENTBOX_MANAGED_AGENT_HANDS_DOMAIN
	ManagedAgentHandsDomain string

	// ManagedAgentHandsHTTPS selects https for the data plane. Default true.
	// Flag: --managed-agent-hands-https
	// Env: AGENTBOX_MANAGED_AGENT_HANDS_HTTPS
	ManagedAgentHandsHTTPS bool

	// ManagedAgentHandsEnvName is the environment to launch sandboxes from,
	// written verbatim: a bare name ("navix") or one scoped to a cluster
	// ("cluster::navix").
	// Flag: --managed-agent-hands-env-name
	// Env: AGENTBOX_MANAGED_AGENT_HANDS_ENV_NAME
	ManagedAgentHandsEnvName string

	// ManagedAgentHandsImage overrides the sandbox main-container image.
	// Effectively required: a member pool's default image does not run the sandbox
	// command endpoint, so leaving it empty yields sandboxes that start and then
	// answer every command with a 502, with nothing wrong on the control plane.
	// Flag: --managed-agent-hands-image
	// Env: AGENTBOX_MANAGED_AGENT_HANDS_IMAGE
	ManagedAgentHandsImage string

	// ManagedAgentHandsScalingGroup pins the default's sandboxes to one member
	// pool. Empty lets the environment route them.
	// Flag: --managed-agent-hands-scaling-group
	// Env: AGENTBOX_MANAGED_AGENT_HANDS_SCALING_GROUP
	ManagedAgentHandsScalingGroup string

	// ManagedAgentHandsSecretName and ...Key name the Secret holding the sandbox
	// API key for the default supply. It must exist in the namespace the agents
	// run in, since it is the Brain's kubelet that resolves it.
	//
	// This credential is the platform's own, deliberately not the caller's: the
	// default environment belongs to the deployment, and an agent using it must
	// not need — or receive — a sandbox key of its own.
	// Flag: --managed-agent-hands-secret-name
	// Env: AGENTBOX_MANAGED_AGENT_HANDS_SECRET_NAME
	ManagedAgentHandsSecretName string
	// Flag: --managed-agent-hands-secret-key
	// Env: AGENTBOX_MANAGED_AGENT_HANDS_SECRET_KEY
	ManagedAgentHandsSecretKey string

	// ── Default model provider ────────────────────────────────────────────────
	//
	// Address and model list only. The key stays per agent: this publishes where
	// the deployment's models live so a caller does not have to know, and the
	// console prefills a create form from it.
	// Flag: --managed-agent-model-base-url
	// Env: AGENTBOX_MANAGED_AGENT_MODEL_BASE_URL
	ManagedAgentModelBaseURL string

	// ManagedAgentModels is the model dropdown a new agent starts with, as
	// comma-separated entries of "id", "id|Display Name" or
	// "id|Display Name|nonreasoning". The list is the only source of that
	// dropdown — no endpoint is queried for it — so a deployment that publishes
	// none leaves the caller to type them.
	// Flag: --managed-agent-models  Env: AGENTBOX_MANAGED_AGENT_MODELS
	ManagedAgentModels string

	// ManagedAgentModelDefault is the model a new agent starts on, and
	// ManagedAgentModelSmall backs a harness's own side tasks (titles and the
	// like).
	// Flag: --managed-agent-model-default
	// Env: AGENTBOX_MANAGED_AGENT_MODEL_DEFAULT
	ManagedAgentModelDefault string
	// Flag: --managed-agent-model-small
	// Env: AGENTBOX_MANAGED_AGENT_MODEL_SMALL
	ManagedAgentModelSmall string

	// ManagedAgentModelSecretName and ...Key name the Secret holding the model
	// credential every agent gets when it declares no harness of its own.
	//
	// Both set is what turns the published endpoint into a usable default: without
	// a credential the Brain comes up healthy and reports every harness
	// unavailable, which is legible but is still an agent that cannot answer.
	//
	// Configuring it is a deliberate trade a deployment makes, not an oversight to
	// fix. One shared key is one quota, one revocation and one audit trail for
	// every agent; a deployment that needs those separate leaves this unset and
	// each agent brings its own key. Only the REFERENCE is held here — the Brain
	// resolves it in the agents' namespace and this process never reads the value.
	// Flag: --managed-agent-model-secret-name
	// Env: AGENTBOX_MANAGED_AGENT_MODEL_SECRET_NAME
	ManagedAgentModelSecretName string
	// Flag: --managed-agent-model-secret-key
	// Env: AGENTBOX_MANAGED_AGENT_MODEL_SECRET_KEY
	ManagedAgentModelSecretKey string

	// NotificationConfigMap is the name of the ConfigMap that holds the
	// notification service's config + runtime state. Stored in APIKeyNamespace.
	// Flag: --notification-configmap  Env: AGENTBOX_NOTIFICATION_CONFIGMAP
	// Default: agentbox-notifications
	NotificationConfigMap string

	// FeishuWebhookURL is the Feishu (Lark) bot webhook the notification
	// service posts daily reports and idle alerts to. Empty disables sending.
	// Flag: --feishu-webhook-url  Env: FEISHU_WEBHOOK_URL
	FeishuWebhookURL string

	// PrometheusURL is the base query URL of the Prometheus-compatible metrics
	// store the notification service reads sandbox-create counters from.
	// Empty disables the daily report and idle alert (no data source).
	// Flag: --prometheus-url  Env: PROMETHEUS_URL
	PrometheusURL string

	// PrometheusToken is the bearer token sent with every PrometheusURL query.
	// Flag: --prometheus-token  Env: PROMETHEUS_TOKEN
	PrometheusToken string
}

// FromFlags registers all wsproxy flags on fs and returns a *Config whose
// fields point to the registered flag values. Call flag.Parse() (or
// fs.Parse()) after this to populate the fields.
//
// Each flag's default is the corresponding environment variable, falling back
// to the built-in default when the env var is absent.
func FromFlags(fs *flag.FlagSet) *Config {
	cfg := &Config{}

	fs.StringVar(&cfg.ListenAddr, "listen-addr",
		envOr("WSPROXY_LISTEN_ADDR", defaultListenAddr),
		"Address the terminal WebSocket proxy listens on.")

	fs.StringVar(&cfg.InternalAddr, "internal-addr",
		envOr("WSPROXY_INTERNAL_ADDR", defaultInternalAddr),
		"Address the internal management API listens on (requires --sync-token).")

	fs.StringVar(&cfg.ClustersFilePath, "clusters-config",
		envOr("CLUSTERS_CONFIG_PATH", defaultClustersFilePath),
		"Path to the clusters.yaml file.")

	fs.StringVar(&cfg.Secret, "secret",
		os.Getenv("AGENTBOX_SECRET"),
		"Shared secret for /v1/ws/sync dialling, /internal/* token gate, and BFF JWT (HS256) verification. "+
			"Empty = sync manager and JWT auth disabled.")

	fs.StringVar(&cfg.AdminKey, "admin-key",
		os.Getenv("AGENTBOX_ADMIN_KEY"),
		"Admin API key for internal API auth. Empty = dev mode (anonymous admin).")

	fs.IntVar(&cfg.MaxKeysPerUser, "max-keys-per-user",
		envInt("AGENTBOX_MAX_KEYS_PER_USER", 0),
		"Per-user API key count limit (0 = unlimited).")

	fs.StringVar(&cfg.APIKeyNamespace, "apikey-namespace",
		envOr("AGENTBOX_APIKEY_NAMESPACE", defaultAPIKeyNamespace),
		"Kubernetes namespace for API key Secrets.")

	fs.BoolVar(&cfg.ManagedAgentEnabled, "managed-agent",
		envOr("AGENTBOX_MANAGED_AGENT_ENABLED", "") != "",
		"Run the ManagedAgent controller in this process (control plane only).")

	fs.StringVar(&cfg.ManagedAgentGatewayAddr, "managed-agent-gateway-addr",
		envOr("WSPROXY_MANAGED_AGENT_GATEWAY_ADDR", defaultManagedAgentGatewayAddr),
		"Address serving published ManagedAgents to external callers. Empty disables it.")

	fs.StringVar(&cfg.ManagedAgentProxyService, "managed-agent-proxy-service",
		envOr("WSPROXY_MANAGED_AGENT_PROXY_SERVICE", ""),
		"This process as in-cluster callers address it, \"<service>.<namespace>:<port>\".")

	fs.StringVar(&cfg.ManagedAgentPublicBaseURL, "managed-agent-public-base-url",
		envOr("WSPROXY_MANAGED_AGENT_PUBLIC_BASE_URL", ""),
		"Public base URL published agents answer on; reported as status.publicURL.")

	fs.StringVar(&cfg.ManagedAgentNamespace, "managed-agent-namespace",
		os.Getenv("AGENTBOX_MANAGED_AGENT_NAMESPACE"),
		"Namespace the ManagedAgent controller watches. Empty watches all namespaces.")

	fs.StringVar(&cfg.ManagedAgentBrainImage, "managed-agent-brain-image",
		os.Getenv("AGENTBOX_MANAGED_AGENT_BRAIN_IMAGE"),
		"Default Brain image repository for agents that name none. "+
			"Empty keeps spec.image.repository required.")

	fs.StringVar(&cfg.ManagedAgentBrainImageTag, "managed-agent-brain-image-tag",
		os.Getenv("AGENTBOX_MANAGED_AGENT_BRAIN_IMAGE_TAG"),
		"Tag for --managed-agent-brain-image.")

	fs.StringVar(&cfg.ManagedAgentBrainPullSecrets, "managed-agent-brain-pull-secrets",
		os.Getenv("AGENTBOX_MANAGED_AGENT_BRAIN_PULL_SECRETS"),
		"Comma-separated imagePullSecrets for the default Brain image.")

	fs.StringVar(&cfg.ManagedAgentHandsAPIURL, "managed-agent-hands-api-url",
		os.Getenv("AGENTBOX_MANAGED_AGENT_HANDS_API_URL"),
		"E2B-compatible control endpoint of the default sandbox supply. "+
			"Empty means agents must declare their own hands.")

	fs.StringVar(&cfg.ManagedAgentHandsDomain, "managed-agent-hands-domain",
		os.Getenv("AGENTBOX_MANAGED_AGENT_HANDS_DOMAIN"),
		"Data-plane gateway of the default sandbox supply, host plus any ingress path.")

	fs.BoolVar(&cfg.ManagedAgentHandsHTTPS, "managed-agent-hands-https",
		envBool("AGENTBOX_MANAGED_AGENT_HANDS_HTTPS", true),
		"Use https for the default sandbox supply's data plane.")

	fs.StringVar(&cfg.ManagedAgentHandsEnvName, "managed-agent-hands-env-name",
		os.Getenv("AGENTBOX_MANAGED_AGENT_HANDS_ENV_NAME"),
		"Environment the default sandbox supply launches from.")

	fs.StringVar(&cfg.ManagedAgentHandsImage, "managed-agent-hands-image",
		os.Getenv("AGENTBOX_MANAGED_AGENT_HANDS_IMAGE"),
		"Sandbox image for the default supply. Empty yields sandboxes that start and "+
			"then refuse every command.")

	fs.StringVar(&cfg.ManagedAgentHandsScalingGroup, "managed-agent-hands-scaling-group",
		os.Getenv("AGENTBOX_MANAGED_AGENT_HANDS_SCALING_GROUP"),
		"Member pool the default supply's sandboxes are pinned to.")

	fs.StringVar(&cfg.ManagedAgentHandsSecretName, "managed-agent-hands-secret-name",
		os.Getenv("AGENTBOX_MANAGED_AGENT_HANDS_SECRET_NAME"),
		"Secret holding the default supply's sandbox API key; must exist in the agents' namespace.")

	fs.StringVar(&cfg.ManagedAgentHandsSecretKey, "managed-agent-hands-secret-key",
		os.Getenv("AGENTBOX_MANAGED_AGENT_HANDS_SECRET_KEY"),
		"Key within --managed-agent-hands-secret-name.")

	fs.StringVar(&cfg.ManagedAgentModelBaseURL, "managed-agent-model-base-url",
		os.Getenv("AGENTBOX_MANAGED_AGENT_MODEL_BASE_URL"),
		"Model endpoint a new agent starts with. The key is not published here.")

	fs.StringVar(&cfg.ManagedAgentModels, "managed-agent-models",
		os.Getenv("AGENTBOX_MANAGED_AGENT_MODELS"),
		"Comma-separated model dropdown for a new agent: \"id\", \"id|Name\" or \"id|Name|nonreasoning\".")

	fs.StringVar(&cfg.ManagedAgentModelDefault, "managed-agent-model-default",
		os.Getenv("AGENTBOX_MANAGED_AGENT_MODEL_DEFAULT"),
		"Model a new agent starts on.")

	fs.StringVar(&cfg.ManagedAgentModelSmall, "managed-agent-model-small",
		os.Getenv("AGENTBOX_MANAGED_AGENT_MODEL_SMALL"),
		"Model backing a harness's own side tasks.")

	fs.StringVar(&cfg.ManagedAgentModelSecretName, "managed-agent-model-secret-name",
		os.Getenv("AGENTBOX_MANAGED_AGENT_MODEL_SECRET_NAME"),
		"Secret holding the default model credential. Empty leaves agents to bring their own.")

	fs.StringVar(&cfg.ManagedAgentModelSecretKey, "managed-agent-model-secret-key",
		os.Getenv("AGENTBOX_MANAGED_AGENT_MODEL_SECRET_KEY"),
		"Key within --managed-agent-model-secret-name.")

	fs.StringVar(&cfg.ImagesCatalogConfigMap, "images-catalog-configmap",
		envOr("AGENTBOX_IMAGES_CATALOG_CONFIGMAP", defaultImagesCatalogConfigMap),
		"Name of the ConfigMap holding the images catalog (stored in --apikey-namespace).")

	fs.StringVar(&cfg.NotificationConfigMap, "notification-configmap",
		envOr("AGENTBOX_NOTIFICATION_CONFIGMAP", defaultNotificationConfigMap),
		"Name of the ConfigMap holding notification config + runtime state (stored in --apikey-namespace).")

	fs.StringVar(&cfg.FeishuWebhookURL, "feishu-webhook-url",
		os.Getenv("FEISHU_WEBHOOK_URL"),
		"Feishu bot webhook URL for daily reports and idle alerts. Empty disables sending.")

	fs.StringVar(&cfg.PrometheusURL, "prometheus-url",
		os.Getenv("PROMETHEUS_URL"),
		"Base query URL of the Prometheus-compatible metrics store. Empty disables the notification service's data source.")

	fs.StringVar(&cfg.PrometheusToken, "prometheus-token",
		os.Getenv("PROMETHEUS_TOKEN"),
		"Bearer token sent with every Prometheus query.")

	return cfg
}

// Validate checks that the Config is consistent. It does not require
// Secret to be set — an empty Secret simply disables the sync manager
// and internal API.
func (c *Config) Validate() error {
	return nil
}

// SyncEnabled reports whether the sync manager (and internal API) should be
// started. It is true when Secret is non-empty.
func (c *Config) SyncEnabled() bool {
	return c.Secret != ""
}

// ── helpers ───────────────────────────────────────────────────────────────────

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envBool reads a boolean whose default is not necessarily false, so an unset
// variable and an explicit "false" have to stay distinguishable.
func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
