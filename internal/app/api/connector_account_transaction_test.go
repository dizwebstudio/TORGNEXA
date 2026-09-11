package api

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/platform/audit"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

func TestConnectorMutationRequiresAtomicDependencies(t *testing.T) {
	scope := validTestScope(t)
	// Hide the transaction capability while retaining the normal port methods.
	api := connectorAccountAPI{audit: struct{ auditCapturer }{&oauthAuditStub{}}}
	request := auditFixtureRequest(t.Context(), scope, "POST", ConnectorCredentialsPath, "synthetic", "")
	called := false
	_, err := api.mutateAccount(request, scope, "connector.account.disabled", audit.RiskWriteSensitive, func(context.Context) (sdk.Account, error) {
		called = true
		return sdk.Account{}, nil
	})
	if !errors.Is(err, errSettingsAudit) || called {
		t.Fatal("nontransactional auditor allowed mutation", err)
	}
	api.secrets = struct{ secrets.SecretProvider }{&oauthSecretsStub{}}
	err = api.withSecrets(t.Context(), scope, func(context.Context) error { called = true; return nil })
	if !errors.Is(err, secrets.ErrTransactionUnavailable) || called {
		t.Fatal("nontransactional provider allowed mutation", err)
	}
}

func TestConnectorAtomicFailureContract(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/openapi/torgnexa-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"createConnectorAccount", "disableConnectorAccount", "enrollConnectorCredentials", "startConnectorOAuth", "completeConnectorOAuth", "replaceConnectorAccountCapabilities", "checkConnectorAccount", "enableConnectorAccount", "previewConnectorBootstrap", "startConnectorBootstrap", "putConnectorSyncSchedule"} {
		_, section, found := strings.Cut(string(raw), "operationId: "+operation+"\n")
		section, _, _ = strings.Cut(section, "\n  /")
		if !found || !strings.Contains(section, "'500':") || !strings.Contains(section, "audit") {
			t.Errorf("%s lacks authoritative audit failure contract", operation)
		}
	}
	_, callback, _ := strings.Cut(string(raw), "operationId: completeConnectorOAuth\n")
	callback, _, _ = strings.Cut(callback, "\n  /")
	if !strings.Contains(callback, "does not reopen the state") || !strings.Contains(callback, "new idempotency key") {
		t.Fatal("callback contract permits replaying a consumed remote code")
	}
}
