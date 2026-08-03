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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"
)

// leafTTL bounds a minted leaf certificate. Short because it is regenerated on
// demand and cached only in memory; the CA above it is itself per-sandbox and
// expires with the sandbox.
const leafTTL = 24 * time.Hour

// leafCacheSize caps the per-sandbox leaf cache. Sandboxes talk to a handful of
// intercepted hosts, so this never evicts in practice; it exists so a sandbox
// cannot grow the map without bound by varying SNI.
const leafCacheSize = 64

// certAuthority mints per-host leaf certificates from the sandbox's CA. The CA
// private key lives only here, in the sidecar's memory — it is never written to
// a volume the sandbox can mount.
type certAuthority struct {
	caCert *x509.Certificate
	caKey  *ecdsa.PrivateKey

	mu    sync.Mutex
	cache map[string]*tls.Certificate
	order []string // insertion order, for cheap FIFO eviction
}

// newCertAuthority parses the PEM pair pushed by the control plane.
func newCertAuthority(certPEM, keyPEM string) (*certAuthority, error) {
	if certPEM == "" || keyPEM == "" {
		return nil, errors.New("empty CA material")
	}
	cBlock, _ := pem.Decode([]byte(certPEM))
	if cBlock == nil {
		return nil, errors.New("CA cert is not valid PEM")
	}
	cert, err := x509.ParseCertificate(cBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA cert: %w", err)
	}
	kBlock, _ := pem.Decode([]byte(keyPEM))
	if kBlock == nil {
		return nil, errors.New("CA key is not valid PEM")
	}
	key, err := x509.ParseECPrivateKey(kBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA key: %w", err)
	}
	return &certAuthority{
		caCert: cert,
		caKey:  key,
		cache:  make(map[string]*tls.Certificate, leafCacheSize),
	}, nil
}

// leafFor returns a certificate valid for host, minting and caching one on
// first use.
func (a *certAuthority) leafFor(host string) (*tls.Certificate, error) {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "" {
		return nil, errors.New("empty SNI")
	}

	a.mu.Lock()
	if c, ok := a.cache[h]; ok {
		a.mu.Unlock()
		return c, nil
	}
	a.mu.Unlock()

	leaf, err := a.mint(h)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	// Another goroutine may have won the race; keep whichever is already there
	// so both connections present the same certificate.
	if c, ok := a.cache[h]; ok {
		return c, nil
	}
	if len(a.order) >= leafCacheSize {
		oldest := a.order[0]
		a.order = a.order[1:]
		delete(a.cache, oldest)
	}
	a.cache[h] = leaf
	a.order = append(a.order, h)
	return leaf, nil
}

func (a *certAuthority) mint(host string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate leaf key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		// Backdate a minute: sandbox clocks are synced by envd at init but a
		// small skew must not produce a not-yet-valid certificate.
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(leafTTL),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}
	// Cap the leaf at the CA's own expiry so a long-lived leaf cannot outlive
	// the sandbox-scoped CA that vouches for it.
	if tmpl.NotAfter.After(a.caCert.NotAfter) {
		tmpl.NotAfter = a.caCert.NotAfter
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.caCert, &key.PublicKey, a.caKey)
	if err != nil {
		return nil, fmt.Errorf("sign leaf for %s: %w", host, err)
	}
	return &tls.Certificate{
		Certificate: [][]byte{der, a.caCert.Raw},
		PrivateKey:  key,
	}, nil
}

// GenerateCA mints a self-signed CA for one sandbox and returns the PEM pair.
// The control plane calls this at claim time: it holds both delivery channels
// (exec to the sidecar for the key, envd /init for the certificate), so
// generating here avoids a round-trip to read the certificate back out of the
// sidecar.
func GenerateCA(commonName string, ttl time.Duration) (certPEM, keyPEM string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate CA key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("generate CA serial: %w", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(ttl),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		// Leaf-only: this CA may sign end-entity certificates and nothing else.
		MaxPathLen:     0,
		MaxPathLenZero: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("self-sign CA: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("marshal CA key: %w", err)
	}
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	return certPEM, keyPEM, nil
}
