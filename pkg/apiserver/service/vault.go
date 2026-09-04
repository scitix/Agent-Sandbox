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

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// The vault stores the credentials a sandbox may use without being able to read
// them. A rule on a create request names one — ${e2b.secrets.openai} — and the
// egress proxy substitutes the real value per outbound request; the value never
// enters the request body, the Pod, or any API response.
//
// Scope is (namespace, user), not namespace alone. Namespaces are normally
// t-{team}-{user} and hold a single user, where the two are the same thing —
// but `default` is shared, and a namespace-only scope would let everyone there
// read and overwrite each other's credentials.
//
// The user half of that scope is an implementation detail, deliberately: the
// API, the SDK and the console all speak bare names. Secret.fill("openai-key")
// resolves to the calling user's own value, so the same agent code runs for
// everyone. Putting the user in the name would bind that code to whoever wrote
// it.

const (
	// vaultSecretPrefix names the per-user Secret that holds one user's entries.
	vaultSecretPrefix = "agbx-vault-"

	// VaultLabelOwner carries the hashed user so entries can be listed with a
	// label selector rather than by parsing object names.
	VaultLabelOwner = "agentbox.navix.sh/vault-owner"
	// VaultLabelManagedBy marks the Secret as vault-owned.
	VaultLabelManagedBy = "agentbox.navix.sh/managed-by"
	// VaultManagedByValue is the value of VaultLabelManagedBy.
	VaultManagedByValue = "vault"

	// VaultAnnotationUser records the unhashed user for operators reading the
	// object; the slug in the name is lossy and the hash is opaque.
	VaultAnnotationUser = "agentbox.navix.sh/vault-user"
	// VaultAnnotationMeta holds per-entry bookkeeping as JSON: version,
	// timestamps and caller metadata. Values live in data, never here.
	VaultAnnotationMeta = "agentbox.navix.sh/vault-meta"

	// vaultIDPrefix is the E2B identifier prefix. A name may not start with it,
	// so "sec_x" and "x" can never denote different entries.
	vaultIDPrefix = "sec_"

	// maxVaultValueBytes bounds a single value. A Kubernetes Secret caps at
	// 1 MiB in total; 64 KiB per entry leaves room for many entries and is far
	// above anything an injectable credential needs.
	maxVaultValueBytes = 64 * 1024

	maxVaultNameLen     = 128
	maxVaultMetaEntries = 32
	maxVaultMetaKeyLen  = 128
	maxVaultMetaValLen  = 1024
	maxVaultMetaBytes   = 8192
)

// vaultNameRe is the E2B name rule, copied exactly rather than tightened:
// a name that works against E2B has to work here, or the compatibility claim is
// false. Note it allows "_" and forbids "." — which is what lets "." be used
// as an unambiguous separator elsewhere.
var vaultNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// vaultSlugRe matches the characters an object name may keep from a username.
var vaultSlugRe = regexp.MustCompile(`[^a-z0-9-]+`)

// VaultItem is one entry's metadata. There is deliberately no value field: a
// read path that cannot express a value cannot leak one, which is a stronger
// guarantee than remembering not to set it.
type VaultItem struct {
	Name      string
	Version   int64
	Metadata  map[string]string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SecretID returns the E2B-shaped identifier for this entry.
func (v VaultItem) SecretID() string { return vaultIDPrefix + v.Name }

// VaultCreateInput is a create request. Value is write-only.
type VaultCreateInput struct {
	Name     string
	Value    string
	Metadata map[string]string
}

// VaultUpdateInput is an update request. E2B's update always carries a value —
// there is no "leave it as it was" state — so neither does this.
type VaultUpdateInput struct {
	Name     string
	Value    string
	Metadata map[string]string
	// MetadataSet distinguishes "no metadata given" from "metadata cleared".
	MetadataSet bool
}

// VaultService is the vault's read/write surface.
type VaultService interface {
	List(ctx context.Context, ns, user string) ([]VaultItem, *domain.AppError)
	Get(ctx context.Context, ns, user, idOrName string) (*VaultItem, *domain.AppError)
	Create(ctx context.Context, ns, user string, in VaultCreateInput) (*VaultItem, *domain.AppError)
	Update(ctx context.Context, ns, user, idOrName string, in VaultUpdateInput) (*VaultItem, *domain.AppError)
	Delete(ctx context.Context, ns, user, idOrName string) *domain.AppError

	// ResolveRefs maps entry names to the Secret key each lives in, for the
	// claim path to embed in a SecretInjection. It reports the first name that
	// does not exist rather than resolving what it can: a rule referencing a
	// missing credential must fail the create, not produce a sandbox that
	// reaches its upstream with a decoy.
	ResolveRefs(ctx context.Context, ns, user string, names []string) (map[string]agentsv1alpha1.SecretKeyRef, *domain.AppError)
}

type k8sVaultService struct {
	client client.Client
	// replicator forwards writes to the Hub when this deployment federates
	// several clusters. Nil keeps the vault local.
	replicator VaultReplicator
}

// NewVaultService returns a VaultService backed by Kubernetes Secrets.
func NewVaultService(c client.Client) VaultService {
	return &k8sVaultService{client: c}
}

// VaultSecretName is the object name holding one user's entries.
//
// The slug keeps the name readable; the hash keeps it unique, because slugging
// is lossy ("a.b" and "a-b" slug alike) and two users must never share a
// Secret. Exported so the sync layer can name the same object on every cluster.
func VaultSecretName(user string) string {
	sum := sha256.Sum256([]byte(user))
	hash := hex.EncodeToString(sum[:])[:6]

	slug := vaultSlugRe.ReplaceAllString(strings.ToLower(user), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	if slug == "" {
		slug = "u"
	}
	return vaultSecretPrefix + slug + "-" + hash
}

// vaultOwnerHash is the label value identifying a user's Secret.
func vaultOwnerHash(user string) string {
	sum := sha256.Sum256([]byte(user))
	return hex.EncodeToString(sum[:])[:6]
}

// vaultEntryMeta is the per-entry bookkeeping stored in the meta annotation.
type vaultEntryMeta struct {
	Version   int64             `json:"v"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// canonicalVaultName normalises and validates a name or a sec_-prefixed ID.
// Both address the same entry, so a caller can round-trip whichever the API
// handed back without a second lookup table.
func canonicalVaultName(idOrName string) (string, *domain.AppError) {
	name := strings.TrimSpace(idOrName)
	name = strings.TrimPrefix(name, vaultIDPrefix)
	name = strings.ToLower(name)

	if name == "" {
		return "", domain.NewBadRequest("secret name is required")
	}
	if len(name) > maxVaultNameLen {
		return "", domain.NewBadRequest(fmt.Sprintf(
			"secret name must be at most %d characters", maxVaultNameLen))
	}
	if strings.ContainsAny(name, "{}") {
		return "", domain.NewBadRequest(
			"secret name cannot contain '{' or '}': it is interpolated into the " +
				"${e2b.secrets.<name>} placeholder the runtime resolves")
	}
	if !vaultNameRe.MatchString(name) {
		return "", domain.NewBadRequest(fmt.Sprintf(
			"secret name %q must contain only letters, digits, '-' and '_'", name))
	}
	return name, nil
}

func validateVaultValue(value string) *domain.AppError {
	if value == "" {
		return domain.NewBadRequest("secret value must not be empty")
	}
	if len(value) > maxVaultValueBytes {
		return domain.NewBadRequest(fmt.Sprintf(
			"secret value must be at most %d bytes", maxVaultValueBytes))
	}
	return nil
}

func validateVaultMetadata(md map[string]string) *domain.AppError {
	if len(md) > maxVaultMetaEntries {
		return domain.NewBadRequest(fmt.Sprintf("metadata must have at most %d entries", maxVaultMetaEntries))
	}
	total := 0
	for k, v := range md {
		if len(k) > maxVaultMetaKeyLen {
			return domain.NewBadRequest(fmt.Sprintf("metadata key %q must be at most %d bytes", k, maxVaultMetaKeyLen))
		}
		if len(v) > maxVaultMetaValLen {
			return domain.NewBadRequest(fmt.Sprintf("metadata value for %q must be at most %d bytes", k, maxVaultMetaValLen))
		}
		total += len(k) + len(v)
	}
	if total > maxVaultMetaBytes {
		return domain.NewBadRequest(fmt.Sprintf("metadata must be at most %d bytes in total", maxVaultMetaBytes))
	}
	return nil
}

// loadVault reads a user's Secret. A missing Secret is not an error: the vault
// is created on first write, so "no Secret" and "no entries" are the same state.
func (s *k8sVaultService) loadVault(ctx context.Context, ns, user string) (*corev1.Secret, map[string]vaultEntryMeta, *domain.AppError) {
	secret := &corev1.Secret{}
	key := client.ObjectKey{Namespace: ns, Name: VaultSecretName(user)}
	if err := s.client.Get(ctx, key, secret); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, map[string]vaultEntryMeta{}, nil
		}
		return nil, nil, domain.NewInternal(fmt.Sprintf("read vault for %s/%s: %v", ns, user, err), err)
	}

	meta := map[string]vaultEntryMeta{}
	if raw := secret.Annotations[VaultAnnotationMeta]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &meta); err != nil {
			// Bookkeeping is recoverable; the values are what matter. Report
			// entries with zero-valued metadata rather than failing the read.
			meta = map[string]vaultEntryMeta{}
		}
	}
	return secret, meta, nil
}

func itemsFrom(secret *corev1.Secret, meta map[string]vaultEntryMeta) []VaultItem {
	if secret == nil {
		return nil
	}
	items := make([]VaultItem, 0, len(secret.Data))
	for name := range secret.Data {
		m := meta[name]
		items = append(items, VaultItem{
			Name:      name,
			Version:   max(m.Version, 1),
			Metadata:  m.Metadata,
			CreatedAt: m.CreatedAt,
			UpdatedAt: m.UpdatedAt,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func (s *k8sVaultService) List(ctx context.Context, ns, user string) ([]VaultItem, *domain.AppError) {
	secret, meta, appErr := s.loadVault(ctx, ns, user)
	if appErr != nil {
		return nil, appErr
	}
	return itemsFrom(secret, meta), nil
}

func (s *k8sVaultService) Get(ctx context.Context, ns, user, idOrName string) (*VaultItem, *domain.AppError) {
	name, appErr := canonicalVaultName(idOrName)
	if appErr != nil {
		return nil, appErr
	}
	secret, meta, appErr := s.loadVault(ctx, ns, user)
	if appErr != nil {
		return nil, appErr
	}
	if secret == nil {
		return nil, vaultNotFound(name)
	}
	if _, ok := secret.Data[name]; !ok {
		return nil, vaultNotFound(name)
	}
	m := meta[name]
	return &VaultItem{
		Name:      name,
		Version:   max(m.Version, 1),
		Metadata:  m.Metadata,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}, nil
}

func vaultNotFound(name string) *domain.AppError {
	return domain.NewNotFound(fmt.Sprintf("secret %q not found", name))
}

func (s *k8sVaultService) Create(ctx context.Context, ns, user string, in VaultCreateInput) (*VaultItem, *domain.AppError) {
	name, appErr := canonicalVaultName(in.Name)
	if appErr != nil {
		return nil, appErr
	}
	if appErr := validateVaultValue(in.Value); appErr != nil {
		return nil, appErr
	}
	if appErr := validateVaultMetadata(in.Metadata); appErr != nil {
		return nil, appErr
	}

	secret, meta, appErr := s.loadVault(ctx, ns, user)
	if appErr != nil {
		return nil, appErr
	}
	if secret != nil {
		if _, exists := secret.Data[name]; exists {
			return nil, domain.NewConflict(fmt.Sprintf("secret %q already exists; update it instead", name))
		}
	}

	now := time.Now().UTC().Truncate(time.Second)

	// With a Hub, the write goes there and comes back on the watch stream, so
	// this cluster learns the result exactly as every other cluster does and
	// the version counter stays single-valued.
	if s.replicator != nil {
		return s.replicateWrite(ctx, ns, user, name, in.Value, in.Metadata, now)
	}

	entry := vaultEntryMeta{Version: 1, CreatedAt: now, UpdatedAt: now, Metadata: in.Metadata}
	meta[name] = entry

	if appErr := s.persist(ctx, ns, user, secret, meta, func(data map[string][]byte) {
		data[name] = []byte(in.Value)
	}); appErr != nil {
		return nil, appErr
	}
	return &VaultItem{Name: name, Version: 1, Metadata: in.Metadata, CreatedAt: now, UpdatedAt: now}, nil
}

// replicateWrite forwards a create or rotate to the Hub and projects its answer.
func (s *k8sVaultService) replicateWrite(
	ctx context.Context,
	ns, user, name, value string,
	metadata map[string]string,
	now time.Time,
) (*VaultItem, *domain.AppError) {
	stored, err := s.replicator.RequestVaultPut(ctx, VaultReplicatedEntry{
		Namespace: ns,
		User:      user,
		Name:      name,
		Value:     value,
		Metadata:  metadata,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return nil, domain.NewServiceUnavailable(fmt.Sprintf(
			"the control plane is not reachable, so the secret was not stored: %v", err))
	}
	// Apply locally too rather than waiting for the broadcast: the caller's very
	// next request may create a sandbox referencing this entry, and losing that
	// race would fail a create for a secret that exists.
	if applyErr := s.ApplyVaultEntry(ctx, *stored); applyErr != nil {
		return nil, domain.NewInternal("secret stored centrally but not locally", applyErr)
	}
	return &VaultItem{
		Name:      stored.Name,
		Version:   stored.Version,
		Metadata:  stored.Metadata,
		CreatedAt: stored.CreatedAt,
		UpdatedAt: stored.UpdatedAt,
	}, nil
}

func (s *k8sVaultService) Update(ctx context.Context, ns, user, idOrName string, in VaultUpdateInput) (*VaultItem, *domain.AppError) {
	name, appErr := canonicalVaultName(idOrName)
	if appErr != nil {
		return nil, appErr
	}
	if appErr := validateVaultValue(in.Value); appErr != nil {
		return nil, appErr
	}
	if appErr := validateVaultMetadata(in.Metadata); appErr != nil {
		return nil, appErr
	}

	secret, meta, appErr := s.loadVault(ctx, ns, user)
	if appErr != nil {
		return nil, appErr
	}
	if secret == nil {
		return nil, vaultNotFound(name)
	}
	if _, exists := secret.Data[name]; !exists {
		return nil, vaultNotFound(name)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if s.replicator != nil {
		md := in.Metadata
		if !in.MetadataSet {
			md = meta[name].Metadata
		}
		return s.replicateWrite(ctx, ns, user, name, in.Value, md, now)
	}

	entry := meta[name]
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.Version = max(entry.Version, 1) + 1
	entry.UpdatedAt = now
	if in.MetadataSet {
		entry.Metadata = in.Metadata
	}
	meta[name] = entry

	if appErr := s.persist(ctx, ns, user, secret, meta, func(data map[string][]byte) {
		data[name] = []byte(in.Value)
	}); appErr != nil {
		return nil, appErr
	}
	return &VaultItem{
		Name: name, Version: entry.Version, Metadata: entry.Metadata,
		CreatedAt: entry.CreatedAt, UpdatedAt: entry.UpdatedAt,
	}, nil
}

func (s *k8sVaultService) Delete(ctx context.Context, ns, user, idOrName string) *domain.AppError {
	name, appErr := canonicalVaultName(idOrName)
	if appErr != nil {
		return appErr
	}
	secret, meta, appErr := s.loadVault(ctx, ns, user)
	if appErr != nil {
		return appErr
	}
	if secret == nil {
		return vaultNotFound(name)
	}
	if _, exists := secret.Data[name]; !exists {
		return vaultNotFound(name)
	}
	if s.replicator != nil {
		if err := s.replicator.RequestVaultDelete(ctx, ns, user, name); err != nil {
			return domain.NewServiceUnavailable(fmt.Sprintf(
				"the control plane is not reachable, so the secret was not deleted: %v", err))
		}
	}
	delete(meta, name)
	return s.persist(ctx, ns, user, secret, meta, func(data map[string][]byte) {
		delete(data, name)
	})
}

// persist writes the vault Secret, creating it on first use. mutate applies the
// change to the data map.
func (s *k8sVaultService) persist(
	ctx context.Context,
	ns, user string,
	existing *corev1.Secret,
	meta map[string]vaultEntryMeta,
	mutate func(data map[string][]byte),
) *domain.AppError {
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return domain.NewInternal("encode vault metadata", err)
	}

	if existing == nil {
		data := map[string][]byte{}
		mutate(data)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      VaultSecretName(user),
				Namespace: ns,
				Labels: map[string]string{
					VaultLabelManagedBy: VaultManagedByValue,
					VaultLabelOwner:     vaultOwnerHash(user),
				},
				Annotations: map[string]string{
					VaultAnnotationUser: user,
					VaultAnnotationMeta: string(metaJSON),
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: data,
		}
		if err := s.client.Create(ctx, secret); err != nil {
			if k8serrors.IsAlreadyExists(err) {
				return domain.NewConflict("vault was created concurrently; retry the request")
			}
			return domain.NewInternal(fmt.Sprintf("create vault for %s/%s: %v", ns, user, err), err)
		}
		return nil
	}

	updated := existing.DeepCopy()
	if updated.Data == nil {
		updated.Data = map[string][]byte{}
	}
	mutate(updated.Data)
	if updated.Labels == nil {
		updated.Labels = map[string]string{}
	}
	updated.Labels[VaultLabelManagedBy] = VaultManagedByValue
	updated.Labels[VaultLabelOwner] = vaultOwnerHash(user)
	if updated.Annotations == nil {
		updated.Annotations = map[string]string{}
	}
	updated.Annotations[VaultAnnotationUser] = user
	updated.Annotations[VaultAnnotationMeta] = string(metaJSON)

	if err := s.client.Update(ctx, updated); err != nil {
		if k8serrors.IsConflict(err) {
			return domain.NewConflict("vault changed concurrently; retry the request")
		}
		return domain.NewInternal(fmt.Sprintf("update vault for %s/%s: %v", ns, user, err), err)
	}
	return nil
}

func (s *k8sVaultService) ResolveRefs(
	ctx context.Context,
	ns, user string,
	names []string,
) (map[string]agentsv1alpha1.SecretKeyRef, *domain.AppError) {
	if len(names) == 0 {
		return nil, nil
	}
	secret, _, appErr := s.loadVault(ctx, ns, user)
	if appErr != nil {
		return nil, appErr
	}

	refs := make(map[string]agentsv1alpha1.SecretKeyRef, len(names))
	for _, raw := range names {
		name, nameErr := canonicalVaultName(raw)
		if nameErr != nil {
			return nil, nameErr
		}
		if secret == nil || len(secret.Data[name]) == 0 {
			return nil, domain.NewBadRequest(unknownSecretMessage(name, secret))
		}
		refs[name] = agentsv1alpha1.SecretKeyRef{Name: VaultSecretName(user), Key: name}
	}
	return refs, nil
}

// unknownSecretMessage names the missing entry and lists what does exist.
// Listing the alternatives is what turns a dead end into a correctable mistake
// for a caller that cannot see the vault — most often an agent that guessed the
// name. Entry names are not secret; the values are.
func unknownSecretMessage(name string, secret *corev1.Secret) string {
	var have []string
	if secret != nil {
		for k := range secret.Data {
			have = append(have, k)
		}
		sort.Strings(have)
	}
	if len(have) == 0 {
		return fmt.Sprintf("unknown secret %q: this user has no secrets yet. "+
			"Create one first, then reference it as ${e2b.secrets.%s}.", name, name)
	}
	const maxListed = 20
	listed := have
	suffix := ""
	if len(listed) > maxListed {
		listed = listed[:maxListed]
		suffix = ", …"
	}
	return fmt.Sprintf("unknown secret %q. Existing secrets: %s%s.",
		name, strings.Join(listed, ", "), suffix)
}

// ---------------------------------------------------------------------------
// Replication
// ---------------------------------------------------------------------------

// The vault is replicated Hub → every Worker, so the k8sVaultService is both a
// local store and a sink for entries other clusters wrote. Writes go through
// the Hub and return on the watch stream; this cluster applies its own writes
// by exactly the same path as everyone else's, which is what keeps the version
// counter single-valued.

// VaultReplicator forwards writes to the Hub. Implemented by the sync service.
type VaultReplicator interface {
	RequestVaultPut(ctx context.Context, e VaultReplicatedEntry) (*VaultReplicatedEntry, error)
	RequestVaultDelete(ctx context.Context, namespace, user, name string) error
}

// SetReplicator wires Hub replication. Without one the vault is local to this
// cluster, which is correct for a single-cluster deployment and wrong for a
// federated one — hence the explicit wiring rather than a silent default.
func (s *k8sVaultService) SetReplicator(r VaultReplicator) {
	s.replicator = r
}

// ApplyVaultEntry writes a replicated entry into local storage.
func (s *k8sVaultService) ApplyVaultEntry(ctx context.Context, e VaultReplicatedEntry) error {
	secret, meta, appErr := s.loadVault(ctx, e.Namespace, e.User)
	if appErr != nil {
		return fmt.Errorf("%s", appErr.Message)
	}
	entry := vaultEntryMeta{
		Version:   e.Version,
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
		Metadata:  e.Metadata,
	}
	// A replicated event that is older than what we hold is a reordered
	// delivery, not a rotation; applying it would move the entry backwards.
	if prev, ok := meta[e.Name]; ok && prev.Version > e.Version {
		return nil
	}
	meta[e.Name] = entry
	if appErr := s.persist(ctx, e.Namespace, e.User, secret, meta, func(data map[string][]byte) {
		data[e.Name] = []byte(e.Value)
	}); appErr != nil {
		return fmt.Errorf("%s", appErr.Message)
	}
	return nil
}

// DeleteVaultEntry removes a replicated entry from local storage.
func (s *k8sVaultService) DeleteVaultEntry(ctx context.Context, namespace, user, name string) error {
	secret, meta, appErr := s.loadVault(ctx, namespace, user)
	if appErr != nil {
		return fmt.Errorf("%s", appErr.Message)
	}
	if secret == nil {
		return nil
	}
	if _, ok := secret.Data[name]; !ok {
		return nil
	}
	delete(meta, name)
	if appErr := s.persist(ctx, namespace, user, secret, meta, func(data map[string][]byte) {
		delete(data, name)
	}); appErr != nil {
		return fmt.Errorf("%s", appErr.Message)
	}
	return nil
}

// ReconcileVault drops local entries the authoritative snapshot does not list.
//
// Only entries in namespaces the snapshot mentions are considered. A snapshot
// says what the Hub knows, and the Hub only knows what some Worker told it; a
// namespace nobody has written from since the Hub started is absent from the
// snapshot without being empty, and deleting on that basis would destroy
// credentials on evidence that does not exist.
func (s *k8sVaultService) ReconcileVault(ctx context.Context, keep []VaultReplicatedEntry) error {
	type scope struct{ ns, user string }
	wanted := map[scope]map[string]struct{}{}
	for _, e := range keep {
		sc := scope{e.Namespace, e.User}
		if wanted[sc] == nil {
			wanted[sc] = map[string]struct{}{}
		}
		wanted[sc][e.Name] = struct{}{}
	}

	for sc, names := range wanted {
		secret, meta, appErr := s.loadVault(ctx, sc.ns, sc.user)
		if appErr != nil || secret == nil {
			continue
		}
		var stale []string
		for name := range secret.Data {
			if _, ok := names[name]; !ok {
				stale = append(stale, name)
			}
		}
		if len(stale) == 0 {
			continue
		}
		for _, name := range stale {
			delete(meta, name)
		}
		if appErr := s.persist(ctx, sc.ns, sc.user, secret, meta, func(data map[string][]byte) {
			for _, name := range stale {
				delete(data, name)
			}
		}); appErr != nil {
			return fmt.Errorf("%s", appErr.Message)
		}
	}
	return nil
}
