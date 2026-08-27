package builtinruntime

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"time"
)

// newCertHTTPTransport builds a certificate-authenticated HTTP client for
// exactly one call's lifetime. Every other connector in this package shares
// one pooled, pinned-dial *http.Client (newHTTPTransport) because none of
// them carry per-account TLS material. A client certificate is different:
// pooling a connection whose TLS handshake is bound to one tenant's private
// key risks that connection being reused for another tenant's request. So
// this client disables keep-alives and is built fresh inside the caller's
// UseSecret callback and discarded when that callback returns — it is never
// stored on a Registry or reused across calls. The dial-target pinning and
// SSRF-safe host validation are identical to newHTTPTransport; only the TLS
// configuration and pooling policy differ.
func newCertHTTPTransport(resolver *net.Resolver, cert tls.Certificate) *httpTransport {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(dialCtx context.Context, network, _ string) (net.Conn, error) {
			target, ok := dialTargetFromContext(dialCtx)
			if !ok {
				return nil, errors.New("provider http: missing dial target")
			}
			return dialer.DialContext(dialCtx, network, net.JoinHostPort(target, "443"))
		},
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}},
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &httpTransport{resolver: resolver, client: client}
}

// parseClientCertificate reads exactly one client certificate and its
// matching private key out of a single opaque secret blob. Certificate
// secrets are stored as one PEM bundle (a CERTIFICATE block followed by a
// PRIVATE KEY block, in either order) because the platform's secret store
// has one slot per account credential, not a certificate/key pair of slots.
func parseClientCertificate(secret []byte) (tls.Certificate, error) {
	if len(secret) == 0 || len(secret) > 1<<20 {
		return tls.Certificate{}, errors.New("payment certificate: invalid secret")
	}
	var certPEM, keyPEM []byte
	rest := secret
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		switch {
		case block.Type == "CERTIFICATE":
			certPEM = append(certPEM, pem.EncodeToMemory(block)...)
		case block.Type == "PRIVATE KEY" || block.Type == "RSA PRIVATE KEY" || block.Type == "EC PRIVATE KEY":
			if keyPEM != nil {
				return tls.Certificate{}, errors.New("payment certificate: multiple private keys")
			}
			keyPEM = pem.EncodeToMemory(block)
		}
	}
	if len(certPEM) == 0 || len(keyPEM) == 0 {
		return tls.Certificate{}, errors.New("payment certificate: certificate or key missing")
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, errors.New("payment certificate: key pair mismatch")
	}
	return cert, nil
}
