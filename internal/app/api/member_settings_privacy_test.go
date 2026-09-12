package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/platform/postgres/tenancyrepo"
)

func TestWorkspaceMemberResponseHidesOIDCSubject(t *testing.T) {
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
			raw, err := json.Marshal(memberToView(tenancyrepo.Member{ID: "member-1", Email: "synthetic@example.test", OIDCSubject: test.subject}))
			if err != nil {
				t.Fatal(err)
			}
			payload := string(raw)
			if strings.Contains(payload, "oidc_subject") || strings.Contains(payload, subject) {
				t.Fatalf("member response exposed internal identity reference: %s", payload)
			}
			var view map[string]any
			if err := json.Unmarshal(raw, &view); err != nil {
				t.Fatal(err)
			}
			if got, ok := view["identity_bound"].(bool); !ok || got != test.bound {
				t.Fatalf("identity_bound = %#v, want %t", view["identity_bound"], test.bound)
			}
		})
	}
}
