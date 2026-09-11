package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/connectorauth"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

var errOAuthStartReplay = errors.New("connector oauth: existing start")

func (api *connectorAccountAPI) createOAuthStart(request *http.Request, scope tenancy.Scope, account sdk.Account, config sdk.OAuth2Configuration, clientID, callbackURL string, pending connectorauth.PendingMaterial, challenge string) (connectorOAuthStartResponse, error) {
	principal, _ := PrincipalFromContext(request.Context())
	payload, err := json.Marshal(pending)
	if err != nil {
		return connectorOAuthStartResponse{}, err
	}
	defer clear(payload)
	var stored connectorauth.Session
	result, err := auditedSettingsMutation(request.Context(), scope, api.audit, func(ctx context.Context) (connectorOAuthStartResponse, error) {
		var result connectorOAuthStartResponse
		err := api.withSecrets(ctx, scope, func(ctx context.Context) error {
			metadata, err := api.secrets.Create(ctx, scope, secrets.ClassOAuthState, payload)
			if err != nil {
				return err
			}
			digest, _ := connectorauth.StateDigest(pending.State)
			now := api.now().UTC()
			proposed := connectorauth.Session{ID: newApprovalID(), AccountID: account.ID, AccountVersion: account.Version, ActorID: principal.Subject, StateDigest: digest, PendingSecretRef: metadata.Reference.String(), CallbackURL: callbackURL, CorrelationID: strings.TrimSpace(request.Header.Get("Idempotency-Key")), Status: "pending", CreatedAt: now, ExpiresAt: now.Add(connectorauth.OAuthSessionTTL)}
			var replayed bool
			stored, replayed, err = api.oauthStore.CreateOrReplay(ctx, scope, proposed)
			if err != nil {
				return err
			}
			if replayed {
				// Roll back the unused candidate secret instead of persisting an
				// orphan or appending another audit for the same OAuth start.
				return errOAuthStartReplay
			}
			url, err := connectorauth.AuthorizationURL(config, clientID, stored.CallbackURL, pending.State, challenge)
			result = connectorOAuthStartResponse{AuthorizationURL: url, ExpiresAt: stored.ExpiresAt.UTC().Format(time.RFC3339)}
			return err
		})
		return result, err
	}, func(ctx context.Context, _ connectorOAuthStartResponse) error {
		return api.capture(request.WithContext(ctx), scope, "connector.account.oauth_started", account, audit.RiskWriteSensitive)
	})
	if !errors.Is(err, errOAuthStartReplay) {
		return result, err
	}
	if !api.now().Before(stored.ExpiresAt) {
		return connectorOAuthStartResponse{}, connectorauth.ErrSessionConflict
	}
	pending, err = api.readPending(request.Context(), scope, stored.PendingSecretRef)
	if err != nil {
		return connectorOAuthStartResponse{}, connectorauth.ErrSessionConflict
	}
	digest := sha256.Sum256([]byte(pending.CodeVerifier))
	challenge = base64.RawURLEncoding.EncodeToString(digest[:])
	url, err := connectorauth.AuthorizationURL(config, clientID, stored.CallbackURL, pending.State, challenge)
	return connectorOAuthStartResponse{AuthorizationURL: url, ExpiresAt: stored.ExpiresAt.UTC().Format(time.RFC3339)}, err
}

type oauthCallbackClaim struct {
	session connectorauth.Session
	pending connectorauth.PendingMaterial
}

func (api *connectorAccountAPI) claimOAuthCallback(request *http.Request, scope tenancy.Scope, digest, state, actor, callbackURL string) (oauthCallbackClaim, error) {
	// Commit the one-time claim and its audit before any non-transactional
	// remote exchange. A later failure must never make the code retryable.
	return auditedSettingsMutation(request.Context(), scope, api.audit, func(ctx context.Context) (oauthCallbackClaim, error) {
		var claim oauthCallbackClaim
		err := api.withSecrets(ctx, scope, func(ctx context.Context) error {
			var err error
			claim.session, err = api.oauthStore.Consume(ctx, scope, digest, actor, callbackURL, api.now().UTC())
			if err != nil {
				return err
			}
			claim.pending, err = api.readPending(ctx, scope, claim.session.PendingSecretRef)
			if err != nil || subtle.ConstantTimeCompare([]byte(claim.pending.State), []byte(state)) != 1 {
				return connectorauth.ErrSessionConflict
			}
			reference, err := secrets.ParseReference(claim.session.PendingSecretRef)
			if err != nil {
				return err
			}
			_, err = api.secrets.Revoke(ctx, scope, reference)
			return err
		})
		return claim, err
	}, func(ctx context.Context, claim oauthCallbackClaim) error {
		_, err := api.audit.Capture(ctx, scope, audit.Entry{ActorID: boundedActorRef(actor), Source: "api", Action: "connector.account.oauth_callback_claimed", ResourceType: "connector_account", ResourceID: claim.session.AccountID, CorrelationID: request.Header.Get("Idempotency-Key"), Risk: audit.RiskWriteSensitive, Summary: audit.Summary{"account_version": claim.session.AccountVersion}})
		return err
	})
}
