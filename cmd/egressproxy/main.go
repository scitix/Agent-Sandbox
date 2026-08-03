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

// Command egressproxy is the sandbox egress filter. It ships in the idle image
// and runs in three roles inside a sandbox Pod:
//
//	install-redirect  (init container, CAP_NET_ADMIN) — program iptables REDIRECT
//	serve             (native sidecar)                — the filtering proxy
//	set-policy        (invoked via kubectl exec)      — control-plane policy push
//	set-secrets       (invoked via kubectl exec)      — credential injection push
//	reset             (invoked via kubectl exec)      — fail closed and wipe secrets
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/scitix/agent-sandbox/pkg/egressproxy"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: egressproxy <serve|install-redirect|set-policy|set-secrets|reset> [flags]")
		os.Exit(2)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe(log)
	case "install-redirect":
		err = egressproxy.InstallRedirect(egressproxy.RedirectConfig{
			ProxyUID:  egressproxy.DefaultProxyUID,
			HTTPPort:  egressproxy.DefaultHTTPPort,
			TLSPort:   egressproxy.DefaultTLSPort,
			OtherPort: egressproxy.DefaultOtherPort,
		})
	case "set-policy":
		err = runSetPolicy()
	case "set-secrets":
		err = runSetSecrets()
	case "reset":
		err = runReset()
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		log.Error("egressproxy failed", "cmd", os.Args[1], "err", err)
		os.Exit(1)
	}
}

func runServe(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	p := egressproxy.NewProxy(egressproxy.ServeConfig{
		PolicyPath:  egressproxy.DefaultPolicyPath,
		SecretsPath: egressproxy.DefaultSecretsPath,
		HTTPPort:    egressproxy.DefaultHTTPPort,
		TLSPort:     egressproxy.DefaultTLSPort,
		OtherPort:   egressproxy.DefaultOtherPort,
		Logger:      log,
	})
	if err := p.Serve(ctx); err != nil && err != context.Canceled {
		return err
	}
	return nil
}

// runSetPolicy reads a Policy JSON from stdin and writes it atomically. The
// control plane pipes the effective policy here via `kubectl exec`.
func runSetPolicy() error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	var p egressproxy.Policy
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("parse policy: %w", err)
	}
	return egressproxy.WritePolicy(egressproxy.DefaultPolicyPath, p)
}

// runSetSecrets reads a Secrets JSON from stdin and writes it atomically with
// owner-only permissions. This is the only path credential material takes into
// the sandbox Pod, and it terminates in a tmpfs volume mounted by the sidecar
// alone — the sandbox containers never see the file.
func runSetSecrets() error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	var s egressproxy.Secrets
	if err := json.Unmarshal(data, &s); err != nil {
		// Deliberately does not wrap the parse error with the payload.
		return fmt.Errorf("parse secrets: invalid JSON")
	}
	return egressproxy.WriteSecrets(egressproxy.DefaultSecretsPath, s)
}

// runReset fails the policy closed and wipes the injection config. Both halves
// matter on release: a recycled pod must carry neither the previous sandbox's
// egress allowances nor its credentials, CA key, or placeholder map.
func runReset() error {
	if err := egressproxy.WritePolicy(egressproxy.DefaultPolicyPath, egressproxy.FailClosed()); err != nil {
		return err
	}
	return egressproxy.RemoveSecrets(egressproxy.DefaultSecretsPath)
}
