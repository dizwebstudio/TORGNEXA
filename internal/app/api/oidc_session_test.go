package api

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/securitysettings"
)

type a09ObservationErrorStore struct {
	settingsSecurityStoreStub
	err error
}

func (s *a09ObservationErrorStore) Observe(context.Context, tenancy.Scope, securitysettings.Observation) error {
	return s.err
}

func TestA09OIDCSessionFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"active", nil, nil},
		{"revoked", securitysettings.ErrSessionRevoked, ErrUnauthenticated},
		{"wrapped_revocation", errors.Join(errors.New("store context"), securitysettings.ErrSessionRevoked), ErrUnauthenticated},
		{"invalid_binding", securitysettings.ErrInvalid, ErrUnauthenticated},
		{"database_unavailable", errors.New("synthetic database detail"), ErrAuthenticationUnavailable},
		{"deadline", context.DeadlineExceeded, ErrAuthenticationUnavailable},
		{"cancellation", context.Canceled, ErrAuthenticationUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newA02OIDCFixture(t, validTestScope(t), &a09ObservationErrorStore{err: tc.err}, a02EmailCase{email: a02InvitedEmail, verification: "true"})
			principal, err := fixture.authenticator.Authenticate(t.Context(), fixture.request())
			if !errors.Is(err, tc.want) || principal.Valid() != (tc.want == nil) {
				t.Fatalf("session-check classification: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "database detail") {
				t.Fatal("authentication error leaked session store details")
			}
		})
	}
}

func TestA09SessionOutageContract(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/openapi/torgnexa-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	info, _, _ := strings.Cut(string(raw), "\nservers:")
	info = strings.Join(strings.Fields(info), " ")
	for _, required := range []string{"session-store persistence failure returns 503", "Retry-After: 5", "Cache-Control: no-store", "without WWW-Authenticate", "Invalid or revoked sessions still return 401", "no business mutation runs"} {
		if !strings.Contains(info, required) {
			t.Errorf("shared authentication contract missing %q", required)
		}
	}
}
