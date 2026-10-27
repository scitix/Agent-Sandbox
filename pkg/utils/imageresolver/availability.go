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

	"github.com/docker/cli/cli/config/configfile"
	configtypes "github.com/docker/cli/cli/config/types"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// AvailabilityChecker always contacts the registry, including for digest refs.
// It must not use Resolve's digest fast path or its successful-resolution cache.
type AvailabilityChecker interface {
	CheckAvailability(context.Context, string, ...ResolveOption) error
}

// AvailabilityError deliberately excludes registry response bodies, URLs with
// credentials and authentication challenges from user-visible errors.
type AvailabilityError struct{ Reason string }

func (e *AvailabilityError) Error() string { return e.Reason }

// WithKeychain supplies request-scoped credentials without persisting them.
func WithKeychain(k authn.Keychain) ResolveOption {
	return func(o *resolveOptions) { o.keychain = k }
}

func (r *Resolver) CheckAvailability(ctx context.Context, image string, opts ...ResolveOption) error {
	var o resolveOptions
	for _, opt := range opts {
		opt(&o)
	}
	keychain, err := r.buildKeychain(ctx, o.namespace, o.pullSecrets)
	if err != nil {
		return &AvailabilityError{Reason: "Unauthorized"}
	}
	if o.keychain != nil {
		keychain = authn.NewMultiKeychain(o.keychain, keychain)
	}
	ref, err := name.ParseReference(image)
	if err != nil {
		return &AvailabilityError{Reason: "InvalidImage"}
	}
	_, err = remote.Head(ref, remote.WithAuthFromKeychain(keychain), remote.WithContext(ctx))
	// Some registries do not implement HEAD. GET fetches only the manifest.
	var te *transport.Error
	if err != nil && (!errors.As(err, &te) || te.StatusCode == http.StatusMethodNotAllowed) && ctx.Err() == nil {
		_, err = remote.Get(ref, remote.WithAuthFromKeychain(keychain), remote.WithContext(ctx))
	}
	if err == nil {
		return nil
	}
	reason := "Unavailable"
	if errors.As(err, &te) {
		switch te.StatusCode {
		case http.StatusNotFound:
			reason = "Missing"
		case http.StatusUnauthorized, http.StatusForbidden:
			reason = "Unauthorized"
		}
	}
	return &AvailabilityError{Reason: reason}
}

// WithCredentials supplies explicit request credentials, retaining default
// registry authentication as fallback for other images in the same template.
func WithCredentials(credentials map[string]authn.AuthConfig) ResolveOption {
	cf := configfile.New("")
	for host, c := range credentials {
		cf.AuthConfigs[host] = configtypes.AuthConfig{Username: c.Username, Password: c.Password, Auth: c.Auth, IdentityToken: c.IdentityToken, RegistryToken: c.RegistryToken}
	}
	return WithKeychain(&configFileKeychain{cf: cf})
}
