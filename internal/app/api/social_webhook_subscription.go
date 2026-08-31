package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/logistics"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const socialWebhookSubscriptionPath = "/api/v1/social/webhooks/subscription"

type socialWebhookSubscriptionInput struct {
	AccountID string `json:"account_id"`
	Endpoint  string `json:"endpoint"`
}

type socialWebhookSubscriptionView struct {
	AccountID string    `json:"account_id"`
	Endpoint  string    `json:"endpoint"`
	Active    bool      `json:"active"`
	UpdatedAt time.Time `json:"updated_at"`
}

type socialWebhookSubscriptionAPI struct {
	accounts   socialConnectorAccounts
	runtime    socialWebhookControllerRuntime
	secrets    secrets.SecretProvider
	configs    connectorRuntimeConfigRepository
	operations socialOperationStore
	audit      auditCapturer
	now        func() time.Time
}

func newSocialWebhookSubscriptionRoutes(accounts socialConnectorAccounts, runtime socialWebhookControllerRuntime, secretSource secrets.SecretProvider, configs connectorRuntimeConfigRepository, operations socialOperationStore, auditService auditCapturer) []ProtectedRoute {
	api := socialWebhookSubscriptionAPI{accounts: accounts, runtime: runtime, secrets: secretSource, configs: configs, operations: operations, audit: auditService, now: func() time.Time { return time.Now().UTC() }}
	return []ProtectedRoute{
		{Method: http.MethodPut, Path: socialWebhookSubscriptionPath, Permission: "connectors.accounts.write", Handler: http.HandlerFunc(api.subscribe)},
		{Method: http.MethodDelete, Path: socialWebhookSubscriptionPath, Permission: "connectors.accounts.write", Handler: http.HandlerFunc(api.unsubscribe)},
	}
}

func (api socialWebhookSubscriptionAPI) subscribe(w http.ResponseWriter, r *http.Request) {
	api.mutate(w, r, true)
}

func (api socialWebhookSubscriptionAPI) unsubscribe(w http.ResponseWriter, r *http.Request) {
	api.mutate(w, r, false)
}

func (api socialWebhookSubscriptionAPI) mutate(w http.ResponseWriter, r *http.Request, active bool) {
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var input socialWebhookSubscriptionInput
	if !ok || !principalOK || api.accounts == nil || api.runtime == nil || api.secrets == nil || api.configs == nil || api.operations == nil || api.audit == nil || !validIdempotencyKey(key) || decodeStrictJSON(r, &input) != nil || strings.TrimSpace(input.AccountID) == "" || strings.TrimSpace(input.Endpoint) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	input.AccountID = strings.TrimSpace(input.AccountID)
	input.Endpoint = strings.TrimSpace(input.Endpoint)
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), input.AccountID)
	if err != nil || account.Family != sdk.FamilySocial || account.Status != sdk.AccountActive {
		writeProblem(w, http.StatusConflict, "Social account unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, sdk.Capability("social.webhooks")) {
		writeProblem(w, http.StatusConflict, "Social webhook capability is not enabled")
		return
	}
	action := "social.webhook.unsubscribe"
	if active {
		action = "social.webhook.subscribe"
	}
	digestPayload, err := json.Marshal(struct {
		Action    string `json:"action"`
		AccountID string `json:"account_id"`
		Endpoint  string `json:"endpoint"`
	}{Action: action, AccountID: account.ID, Endpoint: input.Endpoint})
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	digest := sha256.Sum256(digestPayload)
	receipt, fresh, err := api.operations.BeginOperation(r.Context(), scope, action, key, digest)
	if err != nil {
		if errors.Is(err, logistics.ErrConflict) {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if !fresh {
		if receipt.State == "pending" {
			writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "pending": true, "fresh": false})
			return
		}
		var view socialWebhookSubscriptionView
		if receipt.State != "completed" || json.Unmarshal(receipt.Result, &view) != nil || !socialWebhookSubscriptionViewValid(view) || view.AccountID != account.ID || view.Endpoint != input.Endpoint || view.Active != active {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		writeJSON(w, http.StatusOK, view)
		return
	}
	controller, err := api.runtime.SocialWebhookController(account, func(ctx context.Context, accountID string) (json.RawMessage, error) {
		raw, _, loadErr := api.configs.Config(ctx, scope, accountID)
		return raw, loadErr
	})
	if err != nil || controller == nil {
		writeProblem(w, http.StatusConflict, "Social webhook subscription is unavailable")
		return
	}
	hostRuntime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if active {
		err = controller.SubscribeSocialWebhook(r.Context(), account, hostRuntime, input.Endpoint)
	} else {
		err = controller.UnsubscribeSocialWebhook(r.Context(), account, hostRuntime, input.Endpoint)
	}
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "Social provider unavailable")
		return
	}
	view := socialWebhookSubscriptionView{AccountID: account.ID, Endpoint: input.Endpoint, Active: active, UpdatedAt: api.now().UTC()}
	encoded, err := json.Marshal(view)
	if err != nil || api.operations.CompleteOperation(r.Context(), scope, action, key, encoded) != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	auditAction := "social.webhook.subscription_disabled"
	if active {
		auditAction = "social.webhook.subscription_enabled"
	}
	if _, err = api.audit.Capture(r.Context(), scope, audit.Entry{ActorID: principal.Subject, Source: "api", Action: auditAction, ResourceType: "connector_account", ResourceID: account.ID, CorrelationID: key, Risk: audit.RiskWriteSensitive, Summary: audit.Summary{"active": active}}); err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func socialWebhookSubscriptionViewValid(view socialWebhookSubscriptionView) bool {
	return strings.TrimSpace(view.AccountID) != "" && strings.TrimSpace(view.Endpoint) != "" && !view.UpdatedAt.IsZero() && view.UpdatedAt.Location() == time.UTC
}
