package builtinruntime

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseDolyamiCredentialRequiresLoginPasswordAndMatchingMTLSBundle(t *testing.T) {
	_, bundle := generateTestKeyPair(t, "dolyami-merchant")
	parts := strings.SplitN(string(bundle), "-----BEGIN PRIVATE KEY-----", 2)
	if len(parts) != 2 {
		t.Fatal("test certificate bundle did not contain a private key")
	}
	certificatePEM := parts[0]
	privateKeyPEM := "-----BEGIN PRIVATE KEY-----" + parts[1]
	secret, err := json.Marshal(dolyamiCredential{Login: "demo-login", Password: "demo-password", CertificatePEM: certificatePEM, PrivateKeyPEM: privateKeyPEM})
	if err != nil {
		t.Fatal(err)
	}
	credential, certificate, err := parseDolyamiCredential(secret)
	if err != nil || credential.Login != "demo-login" || len(certificate.Certificate) == 0 {
		t.Fatalf("valid credential rejected: credential=%+v cert=%v err=%v", credential, certificate, err)
	}
	for _, invalid := range [][]byte{
		[]byte(`{"login":"demo-login","password":"demo-password"}`),
		[]byte(`{"login":"demo-login","password":"demo-password","certificate_pem":"not pem","private_key_pem":"not pem"}`),
	} {
		if _, _, err := parseDolyamiCredential(invalid); err == nil {
			t.Fatalf("invalid credential accepted: %s", invalid)
		}
	}
}
