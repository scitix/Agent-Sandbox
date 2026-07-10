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

package egressproxy

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// RedirectConfig parameterizes the iptables nat rules the init container
// installs in the Pod network namespace.
type RedirectConfig struct {
	ProxyUID  int // sidecar runs as this uid; its own egress is exempted (no loop)
	HTTPPort  int
	TLSPort   int
	OtherPort int
}

const egressChain = "AGENTBOX_EGRESS"

// InstallRedirect programs nat OUTPUT so all sandbox TCP is transparently
// redirected to the local proxy, while the proxy's own traffic, DNS, and
// loopback are exempted. Idempotent: the chain is flushed and rebuilt, and the
// OUTPUT jump is added only if absent.
//
// Runs in the Pod netns (shared by all containers); requires CAP_NET_ADMIN.
func InstallRedirect(cfg RedirectConfig) error {
	rules := [][]string{
		// Exempt the proxy's own upstream connections (prevent redirect loop).
		{"-A", egressChain, "-m", "owner", "--uid-owner", strconv.Itoa(cfg.ProxyUID), "-j", "RETURN"},
		// Exempt loopback (agent <-> envd/app on 127.0.0.1 must not be hijacked).
		{"-A", egressChain, "-o", "lo", "-j", "RETURN"},
		// Exempt DNS so name resolution keeps working even while fail-closed.
		{"-A", egressChain, "-p", "udp", "--dport", "53", "-j", "RETURN"},
		{"-A", egressChain, "-p", "tcp", "--dport", "53", "-j", "RETURN"},
		// Redirect the rest of TCP by original dest port.
		{"-A", egressChain, "-p", "tcp", "--dport", "80", "-j", "REDIRECT", "--to-ports", strconv.Itoa(cfg.HTTPPort)},
		{"-A", egressChain, "-p", "tcp", "--dport", "443", "-j", "REDIRECT", "--to-ports", strconv.Itoa(cfg.TLSPort)},
		{"-A", egressChain, "-p", "tcp", "-j", "REDIRECT", "--to-ports", strconv.Itoa(cfg.OtherPort)},
	}

	steps := []struct {
		desc string
		args []string
		ok   func(error) bool // treat these as success (idempotency)
	}{
		{"create chain", []string{"-t", "nat", "-N", egressChain}, isChainExists},
		{"flush chain", []string{"-t", "nat", "-F", egressChain}, nil},
	}
	for _, s := range steps {
		if err := runIptables(s.args...); err != nil && (s.ok == nil || !s.ok(err)) {
			return fmt.Errorf("%s: %w", s.desc, err)
		}
	}
	for _, r := range rules {
		if err := runIptables(append([]string{"-t", "nat"}, r...)...); err != nil {
			return fmt.Errorf("add rule %v: %w", r, err)
		}
	}
	// Hook OUTPUT -> our chain, once.
	check := []string{"-t", "nat", "-C", "OUTPUT", "-p", "tcp", "-j", egressChain}
	if err := runIptables(check...); err != nil {
		if err := runIptables("-t", "nat", "-A", "OUTPUT", "-p", "tcp", "-j", egressChain); err != nil {
			return fmt.Errorf("hook OUTPUT: %w", err)
		}
	}
	return nil
}

func runIptables(args ...string) error {
	out, err := exec.Command("iptables", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// isChainExists reports whether an iptables -N error is the benign
// "chain already exists" (exit for a re-run), which we treat as success.
func isChainExists(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Chain already exists")
}
