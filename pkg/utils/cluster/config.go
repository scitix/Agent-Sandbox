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

import (
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"os"
	"sort"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/yaml"
)

// URLKind selects which plane (and header override) of a GatewayConfig to use.
type URLKind int

const (
	// URLKindNative targets the Native API (agent-sandbox-api).
	URLKindNative URLKind = iota
	// URLKindE2B targets the E2B-compatible API (agent-sandbox-e2b-api).
	URLKindE2B
	// URLKindData targets the data plane (agent-sandbox-data-plane).
	URLKindData
)

// ClusterConfig holds the full configuration snapshot pushed from the Manager
// to every Worker. It is the single wire + on-disk (ConfigMap) format for all
// cluster-wide settings delivered via the sync channel.
//
// Fields beyond Clusters (e.g. HostAliases) let the Manager propagate
// Worker-level operational config that would otherwise need to be baked into
// each Worker cluster's install YAML.
//
// HostAliases reuses corev1.HostAlias verbatim so operators can copy/paste
// Pod spec snippets straight into Manager Helm values.
type ClusterConfig struct {
	Clusters    []ClusterEntry     `json:"clusters"`
	HostAliases []corev1.HostAlias `json:"hostAliases,omitempty"`
}

// ClusterEntry describes a single cluster and how to reach it.
type ClusterEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"` // Dashboard-facing address
	// Selector is a full PromQL label matcher expression that uniquely identifies
	// this cluster's metrics, e.g. `cluster="cluster1"` or
	// `cluster="cluster1",region="region1"`. It is passed through verbatim to Prometheus
	// queries on the Dashboard; ID is purely an internal business identifier and
	// no longer has to match the `cluster` label value.
	Selector   string            `json:"selector,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"` // Dashboard headers
	Visible    string            `json:"visible,omitempty"` // visibility control
	Gateway    *GatewayConfig    `json:"gateway,omitempty"` // cross-cluster gateway config
	Registries []RegistryEntry   `json:"registries,omitempty"`
	// Logs carries this cluster's scoping for the central log service. The
	// dashboard has read it from clusters.yaml since the log integration
	// existed; it is on the Go side too because the worker now serves sandbox
	// logs and needs the same scope to ask about its own pods.
	Logs *LogsConfig `json:"logs,omitempty"`
}

// LogsConfig scopes queries to the central log service for one cluster.
type LogsConfig struct {
	// Filters are forwarded verbatim as equality matchers, e.g.
	// {"region": "region-a", "cluster": "prod-foo"}. Without them a query for
	// a pod name would match same-named pods in other clusters.
	Filters map[string]string `json:"filters,omitempty"`
}

// RegistryEntry describes a private container image registry that belongs to a
// cluster. When a sandbox creation request carries an image whose host matches a
// registry owned by a different cluster, the host is automatically rewritten to
// the corresponding registry (same Type) of the local cluster.
//
// Host must be a plain registry hostname, optionally with a port
// (e.g. "us-docker.pkg.dev" or "registry.example.com:5000").
// Do NOT include a path component here; path rewriting is not supported.
//
// Type is an arbitrary label used to pair registries across clusters.
// Only registries with the same Type are considered equivalent and eligible for
// rewriting. Leaving Type empty is valid — empty-type registries are only
// matched against other empty-type registries. Each cluster must have at most
// one registry per Type value; duplicates within the same cluster are ignored
// (the first entry wins) and a warning is logged.
type RegistryEntry struct {
	Host string `json:"host"`
	Type string `json:"type,omitempty"`
}

// GatewayConfig describes the internal gateway used for cross-cluster forwarding.
//
// Each plane has its own explicit URL. Header injection is layered:
//  1. Headers applies to every forwarded request (common auth tokens, etc.).
//  2. NativeHeaders / E2BHeaders / DataHeaders are per-plane overrides that are
//     merged on top of Headers — keys present in the per-plane map win on conflict.
//
// Example:
//
//	nativeURL: "https://native.cluster-a.internal"
//	e2bURL:    "https://e2b.cluster-a.internal"
//	dataURL:   "https://data.cluster-a.internal"
//	headers:
//	  X-GW-Auth: shared-token
//	dataHeaders:
//	  X-GW-Auth: data-only-token   # overrides shared-token for data-plane calls
type GatewayConfig struct {
	// Explicit per-plane URLs.
	NativeURL string `json:"nativeURL,omitempty"` // Native API (agent-sandbox-api)
	E2BURL    string `json:"e2bURL,omitempty"`    // E2B-compat API (agent-sandbox-e2b-api)
	DataURL   string `json:"dataURL,omitempty"`   // data plane (agent-sandbox-data-plane)

	// Headers injected into every forwarded request.
	Headers map[string]string `json:"headers,omitempty"`

	// Per-plane header overrides. Merged onto Headers; the per-plane value wins
	// when a key exists in both.
	NativeHeaders map[string]string `json:"nativeHeaders,omitempty"`
	E2BHeaders    map[string]string `json:"e2bHeaders,omitempty"`
	DataHeaders   map[string]string `json:"dataHeaders,omitempty"`
}

// NativeAPIBaseURL returns the base URL for the Native API (agent-sandbox-api service).
func (g *GatewayConfig) NativeAPIBaseURL() string { return g.NativeURL }

// E2BAPIBaseURL returns the base URL for the E2B-compatible API (agent-sandbox-e2b-api service).
func (g *GatewayConfig) E2BAPIBaseURL() string { return g.E2BURL }

// DataPlaneBaseURL returns the base URL for the data plane (agent-sandbox-data-plane service).
func (g *GatewayConfig) DataPlaneBaseURL() string { return g.DataURL }

// MergedHeaders returns Headers merged with the per-plane override for the
// given kind. Keys in the override win over keys in Headers. Returns nil when
// both maps are empty so callers can range over it safely.
func (g *GatewayConfig) MergedHeaders(kind URLKind) map[string]string {
	var override map[string]string
	switch kind {
	case URLKindNative:
		override = g.NativeHeaders
	case URLKindE2B:
		override = g.E2BHeaders
	case URLKindData:
		override = g.DataHeaders
	}

	if len(g.Headers) == 0 && len(override) == 0 {
		return nil
	}
	merged := make(map[string]string, len(g.Headers)+len(override))
	maps.Copy(merged, g.Headers)
	maps.Copy(merged, override)
	return merged
}

// registryMeta records the cluster ownership and type of a single registry host.
type registryMeta struct {
	clusterID string
	typ       string
}

// Store is a thread-safe in-memory store of cluster configurations keyed by cluster ID.
type Store struct {
	mu       sync.RWMutex
	clusters map[string]ClusterEntry

	// hostAliases is the latest snapshot of in-process host → IP overrides
	// propagated from the Manager. ExtProc and CrossClusterForwarder consult
	// these before falling back to the system resolver.
	hostAliases []corev1.HostAlias
	// subscribers receive a copy of hostAliases every time ApplyConfig changes them.
	hostAliasSubs []HostAliasSubscriber

	// hostIndex maps a registry host to its owning cluster ID and type.
	// Built eagerly in applyConfigLocked and never modified after that.
	hostIndex map[string]registryMeta
	// typeIndex maps "clusterID:type" to the first registry host of that type
	// for the given cluster. Used to look up the local replacement target.
	typeIndex map[string]string
}

// HostAliasSubscriber is invoked with the full host-alias list every time the
// Manager pushes a new ClusterConfig. Subscribers must treat the slice as
// read-only and copy it if they need to retain it.
type HostAliasSubscriber func([]corev1.HostAlias)

// NewStore creates an empty cluster config store.
func NewStore() *Store {
	return &Store{
		clusters:  make(map[string]ClusterEntry),
		hostIndex: make(map[string]registryMeta),
		typeIndex: make(map[string]string),
	}
}

// SubscribeHostAliases registers fn to be called with the current host-alias
// list immediately (if any are already loaded) and on every subsequent update.
// Intended for wiring DNS resolvers at startup; Unsubscribe is intentionally
// omitted because subscriber lifecycle matches the process.
func (s *Store) SubscribeHostAliases(fn HostAliasSubscriber) {
	s.mu.Lock()
	s.hostAliasSubs = append(s.hostAliasSubs, fn)
	current := append([]corev1.HostAlias(nil), s.hostAliases...)
	s.mu.Unlock()
	if len(current) > 0 {
		fn(current)
	}
}

// HostAliases returns a copy of the current host-alias list.
func (s *Store) HostAliases() []corev1.HostAlias {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]corev1.HostAlias(nil), s.hostAliases...)
}

// HostAliasIP returns the in-cluster IP the given hostname is pinned to by the
// current host-alias set, or ok=false when no alias covers it. Matching is
// exact (the alias list holds literal hostnames, not wildcards) and
// case-insensitive, since DNS names are.
//
// Callers use this to tell a client "you can reach <host> directly at <ip>" —
// the same override the sandbox Pods and the cross-cluster forwarder resolve
// through.
func (s *Store) HostAliasIP(host string) (string, bool) {
	if host == "" {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, alias := range s.hostAliases {
		for _, h := range alias.Hostnames {
			if strings.EqualFold(h, host) && alias.IP != "" {
				return alias.IP, true
			}
		}
	}
	return "", false
}

// Get returns the ClusterEntry for the given cluster ID.
func (s *Store) Get(clusterID string) (ClusterEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.clusters[clusterID]
	return entry, ok
}

// Set replaces all entries in the store.
func (s *Store) Set(entries []ClusterEntry) {
	m := make(map[string]ClusterEntry, len(entries))
	for _, e := range entries {
		if e.ID != "" {
			m[e.ID] = e
		}
	}
	s.mu.Lock()
	s.clusters = m
	s.mu.Unlock()
}

// ConfigDiff describes what changed between two consecutive ClusterConfig snapshots.
// An empty ConfigDiff (IsEmpty() == true) means nothing changed.
type ConfigDiff struct {
	AddedClusters   []ClusterEntry
	RemovedClusters []ClusterEntry
	UpdatedClusters []ClusterEntry // holds the new value after update
	AddedAliases    []corev1.HostAlias
	RemovedAliases  []corev1.HostAlias
}

// IsEmpty returns true when the diff carries no changes.
func (d ConfigDiff) IsEmpty() bool {
	return len(d.AddedClusters) == 0 &&
		len(d.RemovedClusters) == 0 &&
		len(d.UpdatedClusters) == 0 &&
		len(d.AddedAliases) == 0 &&
		len(d.RemovedAliases) == 0
}

// clusterIDs extracts the sorted ID list from a slice of ClusterEntry for compact logging.
func clusterIDs(entries []ClusterEntry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	sort.Strings(ids)
	return ids
}

// aliasIPs extracts the IP list from a slice of HostAlias for compact logging.
func aliasIPs(aliases []corev1.HostAlias) []string {
	ips := make([]string, len(aliases))
	for i, a := range aliases {
		ips[i] = a.IP
	}
	sort.Strings(ips)
	return ips
}

// diffConfig computes the diff between two cluster maps and two host-alias slices.
func diffConfig(
	oldClusters map[string]ClusterEntry, newClusters map[string]ClusterEntry,
	oldAliases, newAliases []corev1.HostAlias,
) ConfigDiff {
	var d ConfigDiff

	// --- clusters ---
	for id, ne := range newClusters {
		if oe, ok := oldClusters[id]; !ok {
			d.AddedClusters = append(d.AddedClusters, ne)
		} else {
			ya, _ := yaml.Marshal(oe)
			yb, _ := yaml.Marshal(ne)
			if string(ya) != string(yb) {
				d.UpdatedClusters = append(d.UpdatedClusters, ne)
			}
		}
	}
	for id, oe := range oldClusters {
		if _, ok := newClusters[id]; !ok {
			d.RemovedClusters = append(d.RemovedClusters, oe)
		}
	}

	// --- host aliases: compare by IP bucket ---
	type bucket struct{ hosts []string }
	toMap := func(aliases []corev1.HostAlias) map[string]bucket {
		m := make(map[string]bucket, len(aliases))
		for _, a := range aliases {
			h := append([]string(nil), a.Hostnames...)
			sort.Strings(h)
			m[a.IP] = bucket{h}
		}
		return m
	}
	oldAM := toMap(oldAliases)
	newAM := toMap(newAliases)

	for ip, nb := range newAM {
		ob, exists := oldAM[ip]
		if !exists {
			d.AddedAliases = append(d.AddedAliases, corev1.HostAlias{IP: ip, Hostnames: nb.hosts})
		} else if !stringSlicesEqual(ob.hosts, nb.hosts) {
			// same IP, different hostname set → treat as replace
			d.RemovedAliases = append(d.RemovedAliases, corev1.HostAlias{IP: ip, Hostnames: ob.hosts})
			d.AddedAliases = append(d.AddedAliases, corev1.HostAlias{IP: ip, Hostnames: nb.hosts})
		}
	}
	for ip, ob := range oldAM {
		if _, ok := newAM[ip]; !ok {
			d.RemovedAliases = append(d.RemovedAliases, corev1.HostAlias{IP: ip, Hostnames: ob.hosts})
		}
	}
	return d
}

// stringSlicesEqual compares two pre-sorted string slices.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ApplyConfig atomically replaces both clusters and host aliases from a full
// snapshot, notifies host-alias subscribers when the alias list changes, and
// returns a ConfigDiff describing what changed. An empty diff means nothing
// changed. Passing a zero-valued ClusterConfig is an explicit reset; callers
// that want to guard against accidental empty pushes must do so before calling
// this method (see ConfigMapWriter).
func (s *Store) ApplyConfig(cfg ClusterConfig) ConfigDiff {
	m := make(map[string]ClusterEntry, len(cfg.Clusters))
	for _, e := range cfg.Clusters {
		if e.ID != "" {
			m[e.ID] = e
		}
	}
	aliases := append([]corev1.HostAlias(nil), cfg.HostAliases...)
	hostIdx, typeIdx := buildRegistryIndexes(m)

	s.mu.Lock()
	diff := diffConfig(s.clusters, m, s.hostAliases, aliases)
	s.clusters = m
	s.hostAliases = aliases
	s.hostIndex = hostIdx
	s.typeIndex = typeIdx
	subs := append([]HostAliasSubscriber(nil), s.hostAliasSubs...)
	s.mu.Unlock()

	if len(diff.AddedAliases) > 0 || len(diff.RemovedAliases) > 0 {
		for _, fn := range subs {
			fn(append([]corev1.HostAlias(nil), aliases...))
		}
	}
	return diff
}

// buildRegistryIndexes constructs the hostIndex and typeIndex from a cluster map.
// Within each cluster, only the first RegistryEntry for a given Type is kept;
// subsequent duplicates are skipped (caller is responsible for logging warnings
// at config-load time if needed — keeping this function pure/log-free makes it
// easier to test).
func buildRegistryIndexes(clusters map[string]ClusterEntry) (hostIdx map[string]registryMeta, typeIdx map[string]string) {
	hostIdx = make(map[string]registryMeta)
	typeIdx = make(map[string]string)
	for clusterID, entry := range clusters {
		// Track which types we have already seen for this cluster so that the
		// first entry wins and duplicates are silently dropped.
		seenTypes := make(map[string]struct{})
		for _, reg := range entry.Registries {
			if reg.Host == "" {
				continue
			}
			hostIdx[reg.Host] = registryMeta{clusterID: clusterID, typ: reg.Type}
			typeKey := clusterID + ":" + reg.Type
			if _, seen := seenTypes[reg.Type]; !seen {
				seenTypes[reg.Type] = struct{}{}
				typeIdx[typeKey] = reg.Host
			}
		}
	}
	return
}

// LookupRegistry returns the owning cluster ID and registry type for the given
// host. Returns ok=false when the host is not registered to any cluster (i.e.
// it is a public registry that should not be rewritten).
func (s *Store) LookupRegistry(host string) (clusterID, typ string, ok bool) {
	s.mu.RLock()
	meta, ok := s.hostIndex[host]
	s.mu.RUnlock()
	if !ok {
		return "", "", false
	}
	return meta.clusterID, meta.typ, true
}

// RegistryForType returns the first registry host configured for the given
// cluster and registry type. Returns ok=false when the cluster has no registry
// of that type (the caller should keep the original image unchanged).
func (s *Store) RegistryForType(clusterID, typ string) (host string, ok bool) {
	s.mu.RLock()
	host, ok = s.typeIndex[clusterID+":"+typ]
	s.mu.RUnlock()
	return
}

// LoadFromFile reads a clusters.yaml file and replaces the store contents.
func (s *Store) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg ClusterConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}
	_ = s.ApplyConfig(cfg) // diff not needed by file-loader callers
	return nil
}

// All returns a snapshot of all cluster entries sorted by ID. The sort is
// load-bearing: callers serialize the result into ConfigMaps and WS sync
// frames, and downstream components hash/diff those bytes. Map iteration order
// would produce a fresh "change" each tick even when nothing actually changed.
func (s *Store) All() []ClusterEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := make([]ClusterEntry, 0, len(s.clusters))
	for _, e := range s.clusters {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries
}

// LoadFromData parses YAML data (e.g. from a ConfigMap data field) and replaces the store contents.
// An empty or nil input clears the store (no clusters configured).
func (s *Store) LoadFromData(data []byte) error {
	if len(data) == 0 {
		s.ApplyConfig(ClusterConfig{})
		return nil
	}
	var cfg ClusterConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}
	s.ApplyConfig(cfg)
	return nil
}

// ClusterConfigKey is the ConfigMap data key that holds the full snapshot
// (clusters + host aliases) pushed by the Manager.
const ClusterConfigKey = "config.yaml"

// LegacyClustersKey is the previous key name ("clusters.yaml") which held
// only the clusters list. Still honored on read for rolling upgrades.
const LegacyClustersKey = "clusters.yaml"

// LoadFromConfigMapObject reads the snapshot from a ConfigMap object and
// replaces the store contents. Prefers the new "config.yaml" key; falls back
// to the legacy "clusters.yaml" key when the new key is absent so that
// Workers running a newer build can still read a ConfigMap written by an
// older Manager during a rolling upgrade.
// If both keys are missing or empty, the store is cleared.
func (s *Store) LoadFromConfigMapObject(cm *corev1.ConfigMap) error {
	if v, ok := cm.Data[ClusterConfigKey]; ok && v != "" {
		return s.LoadFromData([]byte(v))
	}
	return s.LoadFromData([]byte(cm.Data[LegacyClustersKey]))
}

// configMapEventHandler implements cache.ResourceEventHandler to reload the
// Store whenever the target ConfigMap changes.
type configMapEventHandler struct {
	store        *Store
	namespace    string
	name         string
	lastDataHash string // SHA-256 of config snapshot; skip reload when raw bytes haven't changed
	loaded       bool   // false until the first successful ApplyConfig
}

func (h *configMapEventHandler) match(obj any) *corev1.ConfigMap {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok || cm.Namespace != h.namespace || cm.Name != h.name {
		return nil
	}
	return cm
}

func (h *configMapEventHandler) OnAdd(obj any, _ bool) {
	if cm := h.match(obj); cm != nil {
		h.reload(cm)
	}
}

func (h *configMapEventHandler) OnUpdate(_, newObj any) {
	if cm := h.match(newObj); cm != nil {
		h.reload(cm)
	}
}

func (h *configMapEventHandler) OnDelete(obj any) {
	if cm := h.match(obj); cm != nil {
		// ConfigMap deleted → clear the store.
		h.store.ApplyConfig(ClusterConfig{})
		ctrl.Log.WithName("cluster-store").Info("Cluster config ConfigMap deleted, store cleared",
			"namespace", h.namespace, "configMap", h.name)
	}
}

func (h *configMapEventHandler) reload(cm *corev1.ConfigMap) {
	// Skip if the snapshot content hasn't changed since the last reload.
	// Hash the preferred key with fallback to the legacy key (so a rolling
	// upgrade that renames the key doesn't get stuck on an old hash).
	rawData := cm.Data[ClusterConfigKey]
	if rawData == "" {
		rawData = cm.Data[LegacyClustersKey]
	}
	dataHash := fmt.Sprintf("%x", sha256.Sum256([]byte(rawData)))
	if dataHash == h.lastDataHash {
		return
	}

	log := ctrl.Log.WithName("cluster-store")

	// Parse the ConfigMap into a ClusterConfig so we can call ApplyConfig
	// directly and learn whether the effective config actually changed.
	var cfg ClusterConfig
	if v, ok := cm.Data[ClusterConfigKey]; ok && v != "" {
		if err := yaml.Unmarshal([]byte(v), &cfg); err != nil {
			log.Error(err, "Failed to parse cluster config from ConfigMap",
				"namespace", h.namespace, "configMap", h.name)
			return
		}
	} else if v2 := cm.Data[LegacyClustersKey]; v2 != "" {
		if err := yaml.Unmarshal([]byte(v2), &cfg); err != nil {
			log.Error(err, "Failed to parse cluster config from ConfigMap (legacy key)",
				"namespace", h.namespace, "configMap", h.name)
			return
		}
	}

	h.lastDataHash = dataHash
	diff := h.store.ApplyConfig(cfg)

	if !h.loaded {
		// First successful load: always print the full config so operators can
		// confirm what was picked up at startup.
		h.loaded = true
		entries := h.store.All()
		aliases := h.store.HostAliases()
		serialized, _ := yaml.Marshal(ClusterConfig{Clusters: entries, HostAliases: aliases})
		log.Info("Cluster config loaded (initial)",
			"namespace", h.namespace, "configMap", h.name,
			"clusters", len(entries), "hostAliases", len(aliases), "config", string(serialized))
		return
	}

	if diff.IsEmpty() {
		// Raw bytes changed (e.g. YAML whitespace) but effective config is identical;
		// nothing worth logging.
		return
	}

	// Subsequent update: log only what changed.
	kvs := []any{
		"namespace", h.namespace, "configMap", h.name,
	}
	if len(diff.AddedClusters) > 0 {
		kvs = append(kvs, "clustersAdded", clusterIDs(diff.AddedClusters))
	}
	if len(diff.RemovedClusters) > 0 {
		kvs = append(kvs, "clustersRemoved", clusterIDs(diff.RemovedClusters))
	}
	if len(diff.UpdatedClusters) > 0 {
		kvs = append(kvs, "clustersUpdated", clusterIDs(diff.UpdatedClusters))
		for _, e := range diff.UpdatedClusters {
			serialized, _ := yaml.Marshal(e)
			kvs = append(kvs, "cluster:"+e.ID, string(serialized))
		}
	}
	if len(diff.AddedAliases) > 0 {
		kvs = append(kvs, "aliasesAdded", aliasIPs(diff.AddedAliases))
	}
	if len(diff.RemovedAliases) > 0 {
		kvs = append(kvs, "aliasesRemoved", aliasIPs(diff.RemovedAliases))
	}
	log.Info("Cluster config changed", kvs...)
}

// WatchConfigMap registers an informer event handler that reloads the Store
// whenever the target ConfigMap is created, updated, or deleted.
// The cache must be started (i.e. mgr.Start called) for events to fire.
// The initial load happens automatically via the informer's OnAdd callback
// once the cache syncs.
func (s *Store) WatchConfigMap(ctx context.Context, informerCache cache.Cache, namespace, name string) error {
	informer, err := informerCache.GetInformer(ctx, &corev1.ConfigMap{})
	if err != nil {
		return err
	}
	_, err = informer.AddEventHandler(&configMapEventHandler{
		store:     s,
		namespace: namespace,
		name:      name,
	})
	return err
}
