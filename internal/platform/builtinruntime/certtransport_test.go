package builtinruntime

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"
)

// generateTestKeyPair returns a self-signed leaf certificate and its PEM
// bundle, in the same "CERTIFICATE then PRIVATE KEY" shape a real payment
// certificate secret would carry.
func generateTestKeyPair(t *testing.T, commonName string) (tls.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}
	var bundle bytes.Buffer
	bundle.Write(certPEM)
	bundle.Write(keyPEM)
	return pair, bundle.Bytes()
}

func TestParseClientCertificateAcceptsBundleInEitherOrder(t *testing.T) {
	_, bundle := generateTestKeyPair(t, "sbp-test")
	if _, err := parseClientCertificate(bundle); err != nil {
		t.Fatalf("cert-then-key bundle rejected: %v", err)
	}
	certPEM, rest := pem.Decode(bundle)
	if certPEM == nil {
		t.Fatal("expected a decodable certificate block")
	}
	reordered := append(append([]byte(nil), rest...), pem.EncodeToMemory(certPEM)...)
	if _, err := parseClientCertificate(reordered); err != nil {
		t.Fatalf("key-then-cert bundle rejected: %v", err)
	}
}

func TestParseClientCertificateRejectsMalformedInput(t *testing.T) {
	if _, err := parseClientCertificate(nil); err == nil {
		t.Fatal("expected empty secret to be rejected")
	}
	if _, err := parseClientCertificate([]byte("not pem")); err == nil {
		t.Fatal("expected non-PEM input to be rejected")
	}
	_, certOnly := generateTestKeyPair(t, "cert-only")
	certBlock, _ := pem.Decode(certOnly)
	if _, err := parseClientCertificate(pem.EncodeToMemory(certBlock)); err == nil {
		t.Fatal("expected certificate without key to be rejected")
	}
	_, bundleA := generateTestKeyPair(t, "a")
	_, bundleB := generateTestKeyPair(t, "b")
	certBlockA, restA := pem.Decode(bundleA)
	_, restA = pem.Decode(restA) // consume A's key block, keeping only the certificate
	_ = restA
	_, keyOnlyB := pem.Decode(bundleB)
	keyBlockB, _ := pem.Decode(keyOnlyB)
	mismatched := append(pem.EncodeToMemory(certBlockA), pem.EncodeToMemory(keyBlockB)...)
	if _, err := parseClientCertificate(mismatched); err == nil {
		t.Fatal("expected mismatched certificate/key pair to be rejected")
	}
}

// TestCertHTTPTransportPresentsClientCertificate proves the TLS config
// newCertHTTPTransport builds actually authenticates with a client
// certificate end to end, independent of the pinned-dial/SSRF host
// validation in do() (which requires a real public hostname and is covered
// separately by TestHostAndAddressPolicy).
func TestCertHTTPTransportPresentsClientCertificate(t *testing.T) {
	serverCert, _ := generateTestKeyPair(t, "server")
	clientCert, clientBundle := generateTestKeyPair(t, "sbp-merchant-1")
	leaf, err := x509.ParseCertificate(clientCert.Certificate[0])
	if err != nil {
		t.Fatalf("parse client leaf: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)

	var sawClientCert bool
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawClientCert = len(r.TLS.PeerCertificates) == 1 && r.TLS.PeerCertificates[0].Subject.CommonName == "sbp-merchant-1"
		w.WriteHeader(http.StatusOK)
	})}
	server.TLSConfig = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
	}
	go func() { _ = server.ServeTLS(listener, "", "") }()
	defer server.Close()

	parsedClientCert, err := parseClientCertificate(clientBundle)
	if err != nil {
		t.Fatalf("parseClientCertificate: %v", err)
	}
	transport := newCertHTTPTransport(nil, parsedClientCert)
	// Bypass the SSRF-safe DNS resolution path used by do(): this test
	// targets a loopback listener, which validHost() correctly refuses to
	// resolve for production traffic. Swap in a transport whose DialContext
	// dials the listener directly, keeping the TLS certificate config under
	// test unchanged.
	addr := listener.Addr().String()
	transport.client.Transport.(*http.Transport).DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	transport.client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true

	req, err := http.NewRequest(http.MethodGet, "https://sbp-gateway.test/health", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := transport.client.Do(req)
	if err != nil {
		t.Fatalf("mTLS request failed: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	if !sawClientCert {
		t.Fatal("server did not observe the expected client certificate")
	}
}
