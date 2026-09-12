package retentionrepo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrivacyMemberExportHidesOIDCSubject(t *testing.T) {
	t.Parallel()
	const subject = "issuer.example.test|synthetic-private-subject"
	for _, test := range []struct {
		name    string
		subject string
		bound   bool
	}{
		{name: "bound", subject: subject, bound: true},
		{name: "unbound", bound: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(privacyMemberExport(privacyMember{ID: "member-1", Email: "synthetic@example.test", OIDCSubject: test.subject}))
			if err != nil {
				t.Fatal(err)
			}
			payload := string(raw)
			if strings.Contains(payload, "oidc_subject") || strings.Contains(payload, subject) {
				t.Fatalf("privacy export exposed internal identity reference: %s", payload)
			}
			var exported map[string]any
			if err := json.Unmarshal(raw, &exported); err != nil {
				t.Fatal(err)
			}
			if got, ok := exported["identity_bound"].(bool); !ok || got != test.bound {
				t.Fatalf("identity_bound = %#v, want %t", exported["identity_bound"], test.bound)
			}
		})
	}
}
