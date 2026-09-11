package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/connectorauth"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

func TestConnectorAuditPostgresOAuthLifecycle(t *testing.T) {
	for _, scenario := range []string{"start_audit_failure", "claim_audit_failure", "completion_audit_failure", "exchange_failure", "exchange_failure_audit_failure"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, db, admin, scope := auditPostgres(t)
			provider := webhookEncryptedSecrets(t, db)
			old, err := provider.Create(ctx, scope, secrets.ClassOAuthClient, []byte(`{"client_id":"synthetic-client","client_secret":"synthetic-client-secret"}`))
			if err != nil {
				t.Fatal(err)
			}
			fixture := connectorAuditFixture(t)
			id := webhookAccountFixture(t, ctx, admin, scope, fixture.OAuthConnector, fixture.OAuthFamily, old.Reference.String())
			repo, _ := connectorrepo.New(db)
			before, err := repo.ChangeAccountStatus(ctx, sdk.AccountStatusChange{OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), AccountID: id, Status: sdk.AccountDisabled, ExpectedVersion: 2})
			if err != nil {
				t.Fatal(err)
			}
			policy, err := connectorauth.NewCallbackPolicy([]string{"https://console.example.test"})
			if err != nil {
				t.Fatal(err)
			}
			fail := auditFailureSwitch(t, ctx, admin, scope)
			fail(false)
			exchanges := 0
			api := connectorAccountAPI{repository: repo, oauthStore: repo, audit: postgresAuditor(t, db), secrets: provider, callbacks: policy, now: func() time.Time { return time.Now().UTC() }, exchange: func(_ context.Context, _ sdk.OAuth2Configuration, client connectorauth.OAuthClient, code, callback, verifier string, _ time.Duration) ([]byte, error) {
				exchanges++
				if client.ClientID != "synthetic-client" || code != "synthetic-code" || len(verifier) < 43 {
					t.Fatal("invalid synthetic exchange")
				}
				// Claim + audit must have committed before a remote effect.
				if auditCount(t, ctx, admin, scope) != 2 {
					t.Fatal("exchange preceded durable claim audit")
				}
				if scenario == "completion_audit_failure" || scenario == "exchange_failure_audit_failure" {
					fail(true)
				}
				if strings.HasPrefix(scenario, "exchange_failure") {
					return nil, errors.New("synthetic provider response must not leak")
				}
				return []byte(`{"access_token":"synthetic-access","refresh_token":"synthetic-refresh","token_type":"Bearer"}`), nil
			}}
			startBody := fmt.Sprintf(`{"account_id":%q,"expected_version":%d,"callback_url":"https://console.example.test/oauth/connectors/callback"}`, id, before.Version)
			sendStart := func() *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				api.oauthStart(w, auditFixtureRequest(ctx, scope, "POST", ConnectorOAuthStartPath, "synthetic-start", startBody))
				return w
			}
			if scenario == "start_audit_failure" {
				fail(true)
				w := sendStart()
				if w.Code != 500 {
					t.Fatalf("start failure status=%d", w.Code)
				}
				var sessions, refs int
				if err := admin.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM connector_oauth_sessions WHERE workspace_id=$1),(SELECT count(*) FROM secret_references WHERE workspace_id=$1)`, scope.WorkspaceID().String()).Scan(&sessions, &refs); err != nil || sessions != 0 || refs != 1 || auditCount(t, ctx, admin, scope) != 0 {
					t.Fatal("start left state/secret/audit", err)
				}
				fail(false)
			}
			w := sendStart()
			if w.Code != 200 {
				t.Fatalf("start status=%d", w.Code)
			}
			var first connectorOAuthStartResponse
			if err = json.Unmarshal(w.Body.Bytes(), &first); err != nil {
				t.Fatal(err)
			}
			w = sendStart()
			var replay connectorOAuthStartResponse
			if err = json.Unmarshal(w.Body.Bytes(), &replay); err != nil || w.Code != 200 || replay != first || auditCount(t, ctx, admin, scope) != 1 {
				t.Fatal("OAuth start replay changed result or audit", err)
			}
			var refs int
			if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM secret_references WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&refs); err != nil || refs != 2 {
				t.Fatal("OAuth replay persisted an orphan secret", err)
			}
			parsed, err := url.Parse(first.AuthorizationURL)
			if err != nil {
				t.Fatal(err)
			}
			state := parsed.Query().Get("state")
			callbackBody := fmt.Sprintf(`{"code":"synthetic-code","state":%q,"callback_url":"https://console.example.test/oauth/connectors/callback"}`, state)
			sendCallback := func() *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				api.oauthCallback(w, auditFixtureRequest(ctx, scope, "POST", ConnectorOAuthCallbackPath, "synthetic-callback", callbackBody))
				return w
			}
			if scenario == "claim_audit_failure" {
				fail(true)
				w = sendCallback()
				if w.Code != 500 || exchanges != 0 {
					t.Fatal("failed claim contacted provider", w.Code, exchanges)
				}
				var status, ref string
				if err := admin.QueryRowContext(ctx, `SELECT status,pending_secret_reference FROM connector_oauth_sessions WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&status, &ref); err != nil || status != "pending" {
					t.Fatal("claim escaped rollback", err)
				}
				metadata, err := provider.Describe(ctx, scope, secrets.Reference(ref))
				if err != nil || metadata.Status != secrets.StatusActive || auditCount(t, ctx, admin, scope) != 1 {
					t.Fatal("failed claim revoked state", err)
				}
				fail(false)
			}
			w = sendCallback()
			want := 200
			switch scenario {
			case "completion_audit_failure", "exchange_failure_audit_failure":
				want = 500
			case "exchange_failure":
				want = 502
			}
			if w.Code != want || exchanges != 1 {
				t.Fatal("callback outcome", w.Code, exchanges)
			}
			for _, secret := range []string{"synthetic-client-secret", "synthetic-access", "synthetic-refresh", "synthetic provider response"} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatal("callback leaked secret or provider error")
				}
			}
			var status string
			if err := admin.QueryRowContext(ctx, `SELECT status FROM connector_oauth_sessions WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&status); err != nil || status != "consumed" {
				t.Fatal("one-time claim was reactivated", err)
			}
			fail(false)
			if w := sendCallback(); w.Code != 409 || exchanges != 1 {
				t.Fatal("callback exchanged twice", w.Code, exchanges)
			}
			saved, err := repo.AccountByID(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), id)
			if err != nil {
				t.Fatal(err)
			}
			oldState, err := provider.Describe(ctx, scope, old.Reference)
			if err != nil {
				t.Fatal(err)
			}
			if want == 500 {
				if saved.Version != before.Version || saved.SecretReference != before.SecretReference || oldState.Status != secrets.StatusActive || auditCount(t, ctx, admin, scope) != 2 {
					t.Fatal("failed OAuth completion changed binding or audit")
				}
				if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM secret_references WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&refs); err != nil || refs != 2 {
					t.Fatal("failed completion left token secret", err)
				}
			} else if want == 502 {
				if saved.Health.ReasonCode != "oauth_exchange_failed" || saved.SecretReference != before.SecretReference || oldState.Status != secrets.StatusActive || auditCount(t, ctx, admin, scope) != 3 {
					t.Fatal("failed exchange health lacks atomic audit")
				}
			} else if saved.Version != before.Version+1 || saved.SecretReference == before.SecretReference || saved.Status != sdk.AccountDisabled || oldState.Status != secrets.StatusRevoked || auditCount(t, ctx, admin, scope) != 3 {
				t.Fatal("OAuth completion incomplete")
			}
		})
	}
}
