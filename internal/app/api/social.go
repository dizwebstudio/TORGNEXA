package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/logistics"
	"github.com/torgnexa/torgnexa/internal/core/social"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/socialdispatchrepo"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const (
	socialChannelsPath     = "/api/v1/social/channels"
	socialPublicationsPath = "/api/v1/social/publications"
)

type socialAPIRepository interface {
	social.Repository
	ListChannelAccounts(context.Context, social.Scope, int) ([]social.ChannelAccount, error)
	ListPublications(context.Context, social.Scope, int) ([]social.Publication, error)
}

type socialConnectorAccounts interface {
	AccountByID(context.Context, string, string, string) (sdk.Account, error)
	AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error)
}

type socialTextRuntime interface {
	SocialTextLimit(string) int
}

type socialEditorRuntime interface {
	SocialEditor(sdk.Account, builtinruntime.ConfigLoader) (sdk.SocialEditor, error)
}

type socialDeleterRuntime interface {
	SocialDeleter(sdk.Account, builtinruntime.ConfigLoader) (sdk.SocialDeleter, error)
}

type socialRemoteReceiptStore interface {
	Receipt(context.Context, tenancy.Scope, string) (socialdispatchrepo.Receipt, error)
}

type socialOperationStore interface {
	BeginOperation(context.Context, tenancy.Scope, string, string, [32]byte) (logistics.OperationReceipt, bool, error)
	CompleteOperation(context.Context, tenancy.Scope, string, string, json.RawMessage) error
}

type socialApprovalStore interface {
	Request(context.Context, tenancy.Scope, string) (approval.Request, error)
}

type socialRouteDependency struct {
	secrets    secrets.SecretProvider
	configs    connectorRuntimeConfigRepository
	receipts   socialRemoteReceiptStore
	approvals  socialApprovalStore
	operations socialOperationStore
}

type socialAPI struct {
	repository socialAPIRepository
	accounts   socialConnectorAccounts
	runtime    connectorRuntimeAdmission
	secrets    secrets.SecretProvider
	configs    connectorRuntimeConfigRepository
	receipts   socialRemoteReceiptStore
	approvals  socialApprovalStore
	operations socialOperationStore
}

type socialChannelView struct {
	ID                 string   `json:"id"`
	ConnectorAccountID string   `json:"connector_account_id"`
	DisplayName        string   `json:"display_name"`
	Capabilities       []string `json:"capabilities"`
	Status             string   `json:"status"`
	Version            int64    `json:"version"`
}

type socialPublicationView struct {
	ID               string          `json:"id"`
	ChannelAccountID string          `json:"channel_account_id"`
	ChannelName      string          `json:"channel_name"`
	Text             string          `json:"text"`
	Buttons          []social.Button `json:"buttons,omitempty"`
	Status           string          `json:"status"`
	ReasonCode       string          `json:"reason_code,omitempty"`
	Attempt          int             `json:"attempt"`
	PublishAt        *time.Time      `json:"publish_at,omitempty"`
	PublishedAt      *time.Time      `json:"published_at,omitempty"`
	Version          int64           `json:"version"`
	CreatedAt        time.Time       `json:"created_at"`
}

type socialPublicationEditView struct {
	RemotePublicationID string                 `json:"remote_publication_id"`
	Status              sdk.SocialRemoteStatus `json:"status"`
	Updated             bool                   `json:"updated"`
	ObservedAt          time.Time              `json:"observed_at"`
}

type socialPublicationDeleteView struct {
	RemotePublicationID string    `json:"remote_publication_id"`
	Deleted             bool      `json:"deleted"`
	ObservedAt          time.Time `json:"observed_at"`
}

func newSocialRoutes(repository socialAPIRepository, accounts socialConnectorAccounts, runtime connectorRuntimeAdmission, dependencies ...socialRouteDependency) []ProtectedRoute {
	api := socialAPI{repository: repository, accounts: accounts, runtime: runtime}
	if len(dependencies) > 0 {
		api.secrets = dependencies[0].secrets
		api.configs = dependencies[0].configs
		api.receipts = dependencies[0].receipts
		api.approvals = dependencies[0].approvals
		api.operations = dependencies[0].operations
	}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: socialChannelsPath, Permission: "connectors.read", Handler: http.HandlerFunc(api.listChannels)},
		{Method: http.MethodPost, Path: socialChannelsPath, Permission: "connectors.accounts.write", Handler: http.HandlerFunc(api.createChannel)},
		{Method: http.MethodPut, Path: socialChannelsPath + "/", PathPrefix: true, Permission: "connectors.accounts.write", Handler: http.HandlerFunc(api.updateChannel)},
		{Method: http.MethodGet, Path: socialPublicationsPath, Permission: "connectors.read", Handler: http.HandlerFunc(api.listPublications)},
		{Method: http.MethodPost, Path: socialPublicationsPath, Permission: "connectors.accounts.write", Handler: http.HandlerFunc(api.createPublication)},
		{Method: http.MethodPatch, Path: socialPublicationsPath + "/", PathPrefix: true, Permission: "social.post.edit", Handler: http.HandlerFunc(api.editPublication)},
		{Method: http.MethodDelete, Path: socialPublicationsPath + "/", PathPrefix: true, Permission: "social.post.delete", Handler: http.HandlerFunc(api.deletePublication)},
	}
}

func (api socialAPI) listChannels(w http.ResponseWriter, r *http.Request) {
	scope, socialScope, ok := socialRequestScope(w, r)
	_ = scope
	if !ok || api.repository == nil {
		return
	}
	limit, valid := socialLimit(r)
	if !valid {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	items, err := api.repository.ListChannelAccounts(r.Context(), socialScope, limit)
	if err != nil {
		writeSocialError(w, err)
		return
	}
	views := make([]socialChannelView, 0, len(items))
	for _, item := range items {
		views = append(views, socialChannelResponse(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (api socialAPI) createChannel(w http.ResponseWriter, r *http.Request) {
	tenantScope, socialScope, ok := socialRequestScope(w, r)
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var input struct {
		ID                 string `json:"id"`
		ConnectorAccountID string `json:"connector_account_id"`
		DisplayName        string `json:"display_name"`
		Active             bool   `json:"active"`
	}
	if !ok || !principalOK || api.repository == nil || api.accounts == nil || api.runtime == nil || decodeStrictJSON(r, &input) != nil || key != input.ID {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	id, err := social.ParseChannelAccountID(input.ID)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), tenantScope.OrganizationID().String(), tenantScope.WorkspaceID().String(), strings.TrimSpace(input.ConnectorAccountID))
	if err != nil || account.Family != sdk.FamilySocial {
		writeProblem(w, http.StatusConflict, "Social connector account unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), tenantScope, account.ID)
	if err != nil || !socialTextEnabled(settings) {
		writeProblem(w, http.StatusConflict, "Social text capability is not enabled")
		return
	}
	capabilities := []social.Capability{social.CapabilityPostText}
	if socialButtonEnabled(settings) {
		capabilities = []social.Capability{social.CapabilityPostButtons, social.CapabilityPostText}
	}
	command := social.CreateChannelAccount{ID: id, ConnectorAccountID: account.ID, DisplayName: strings.TrimSpace(input.DisplayName), Capabilities: capabilities}
	channel, err := api.repository.CreateChannelAccount(r.Context(), socialScope, command, socialMutation(principal.Subject, key))
	if errors.Is(err, social.ErrConflict) {
		channel, err = api.repository.ChannelAccount(r.Context(), socialScope, id)
		matchesIdentity := reflect.DeepEqual(
			struct {
				AccountID   string
				DisplayName string
			}{channel.ConnectorAccountID, channel.DisplayName},
			struct {
				AccountID   string
				DisplayName string
			}{command.ConnectorAccountID, command.DisplayName},
		)
		matchesCapability := reflect.DeepEqual(channel.Capabilities, command.Capabilities)
		matchesReplay := matchesIdentity && matchesCapability
		if err == nil && !matchesReplay {
			err = social.ErrConflict
		}
	}
	if err == nil && input.Active && channel.Status != social.ChannelActive {
		channel, err = api.repository.UpdateChannelAccount(r.Context(), socialScope, social.UpdateChannelAccount{ID: channel.ID, ExpectedVersion: channel.Version, DisplayName: channel.DisplayName, Capabilities: channel.Capabilities, Status: social.ChannelActive}, socialMutation(principal.Subject, key))
	}
	if err != nil {
		writeSocialError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, socialChannelResponse(channel))
}

func (api socialAPI) updateChannel(w http.ResponseWriter, r *http.Request) {
	_, socialScope, ok := socialRequestScope(w, r)
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	rawID := strings.TrimPrefix(r.URL.Path, socialChannelsPath+"/")
	var input struct {
		DisplayName     string `json:"display_name"`
		Status          string `json:"status"`
		ExpectedVersion int64  `json:"expected_version"`
	}
	if !ok || !principalOK || api.repository == nil || strings.Contains(rawID, "/") || !validIdempotencyKey(key) || decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	id, err := social.ParseChannelAccountID(rawID)
	status := social.ChannelStatus(input.Status)
	if err != nil || !status.Valid() {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	current, err := api.repository.ChannelAccount(r.Context(), socialScope, id)
	if err != nil {
		writeSocialError(w, err)
		return
	}
	channel, err := api.repository.UpdateChannelAccount(r.Context(), socialScope, social.UpdateChannelAccount{ID: id, ExpectedVersion: input.ExpectedVersion, DisplayName: strings.TrimSpace(input.DisplayName), Capabilities: current.Capabilities, Status: status}, socialMutation(principal.Subject, key))
	if err != nil {
		writeSocialError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, socialChannelResponse(channel))
}

func (api socialAPI) listPublications(w http.ResponseWriter, r *http.Request) {
	_, socialScope, ok := socialRequestScope(w, r)
	if !ok || api.repository == nil {
		return
	}
	limit, valid := socialLimit(r)
	if !valid {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	items, err := api.repository.ListPublications(r.Context(), socialScope, limit)
	if err != nil {
		writeSocialError(w, err)
		return
	}
	views := make([]socialPublicationView, 0, len(items))
	for _, item := range items {
		view, viewErr := api.publicationResponse(r.Context(), socialScope, item)
		if viewErr != nil {
			writeSocialError(w, viewErr)
			return
		}
		views = append(views, view)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (api socialAPI) createPublication(w http.ResponseWriter, r *http.Request) {
	tenantScope, socialScope, ok := socialRequestScope(w, r)
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var input struct {
		ID               string          `json:"id"`
		ChannelAccountID string          `json:"channel_account_id"`
		Text             string          `json:"text"`
		Buttons          []social.Button `json:"buttons"`
		PublishAt        *time.Time      `json:"publish_at"`
	}
	if !ok || !principalOK || api.repository == nil || api.accounts == nil || api.runtime == nil || decodeStrictJSON(r, &input) != nil || key != input.ID {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	publicationID, err := social.ParsePublicationID(input.ID)
	channelID, channelErr := social.ParseChannelAccountID(input.ChannelAccountID)
	contentID, contentErr := relatedSocialID(input.ID, 1)
	variantID, variantErr := relatedSocialID(input.ID, 2)
	if err != nil || channelErr != nil || contentErr != nil || variantErr != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	channel, err := api.repository.ChannelAccount(r.Context(), socialScope, channelID)
	if err != nil || channel.Status != social.ChannelActive {
		writeProblem(w, http.StatusConflict, "Social channel unavailable")
		return
	}
	if social.ValidateButtons(input.Buttons) != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid publication buttons")
		return
	}
	if len(input.Buttons) > 0 && !channel.Supports(social.CapabilityPostButtons) {
		writeProblem(w, http.StatusConflict, "Social button capability is not enabled")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), tenantScope.OrganizationID().String(), tenantScope.WorkspaceID().String(), channel.ConnectorAccountID)
	limiter, limitOK := api.runtime.(socialTextRuntime)
	if err != nil || account.Family != sdk.FamilySocial || !limitOK {
		writeProblem(w, http.StatusConflict, "Social channel unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), tenantScope, account.ID)
	if err != nil || !socialTextEnabled(settings) || (len(input.Buttons) > 0 && !socialButtonEnabled(settings)) {
		writeProblem(w, http.StatusConflict, "Social capability is not enabled")
		return
	}
	textLimit := limiter.SocialTextLimit(account.ConnectorID)
	if !validSocialText(input.Text, textLimit) {
		writeProblem(w, http.StatusBadRequest, "Text exceeds the channel runtime limit")
		return
	}
	schedule := social.ImmediateSchedule()
	if input.PublishAt != nil {
		at := input.PublishAt.UTC()
		if !at.After(time.Now().UTC()) {
			writeProblem(w, http.StatusBadRequest, "publish_at must be in the future")
			return
		}
		schedule, err = social.AtSchedule(at)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
	}
	content, err := api.repository.CreateContent(r.Context(), socialScope, social.CreateContent{ID: social.ContentID(contentID), Body: input.Text}, socialMutation(principal.Subject, key))
	if errors.Is(err, social.ErrConflict) {
		content, err = api.repository.Content(r.Context(), socialScope, social.ContentID(contentID))
		if err == nil && content.Body != input.Text {
			err = social.ErrConflict
		}
	}
	if err == nil && content.Status == social.ContentDraft {
		content, err = api.repository.ChangeContentStatus(r.Context(), socialScope, social.ChangeContentStatus{ID: content.ID, ExpectedVersion: content.Version, Status: social.ContentReady}, socialMutation(principal.Subject, key))
	}
	if err != nil {
		writeSocialError(w, err)
		return
	}
	variantCommand := social.CreateVariant{ID: social.VariantID(variantID), ContentID: content.ID, Format: social.FormatText, Body: input.Text, Buttons: input.Buttons}
	variant, err := api.repository.CreateVariant(r.Context(), socialScope, variantCommand, socialMutation(principal.Subject, key))
	if errors.Is(err, social.ErrConflict) {
		variant, err = api.repository.Variant(r.Context(), socialScope, variantCommand.ID)
		if err == nil && (variant.ContentID != content.ID || variant.Format != social.FormatText || variant.Body != input.Text || !socialButtonsEqual(variant.Buttons, input.Buttons)) {
			err = social.ErrConflict
		}
	}
	if err != nil {
		writeSocialError(w, err)
		return
	}
	command := social.CreatePublication{ID: publicationID, VariantID: variant.ID, ChannelAccountID: channelID, Schedule: schedule}
	publication, err := api.repository.CreatePublication(r.Context(), socialScope, command, socialMutation(principal.Subject, key))
	if errors.Is(err, social.ErrConflict) {
		publication, err = api.repository.Publication(r.Context(), socialScope, publicationID)
		if err == nil && (publication.VariantID != variant.ID || publication.ChannelAccountID != channelID || !socialSchedulesEqual(publication.Schedule, schedule)) {
			err = social.ErrConflict
		}
	}
	if err != nil {
		writeSocialError(w, err)
		return
	}
	view, err := api.publicationResponse(r.Context(), socialScope, publication)
	if err != nil {
		writeSocialError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, view)
}

func (api socialAPI) editPublication(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	approvalID := strings.TrimSpace(r.Header.Get("Approval-Request-ID"))
	if !scopeOK || !principalOK || principal.Subject == "" {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if api.repository == nil || api.accounts == nil || api.runtime == nil || api.secrets == nil || api.configs == nil || api.receipts == nil || api.approvals == nil || api.operations == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	publicationID, err := social.ParsePublicationID(strings.Trim(strings.TrimPrefix(r.URL.Path, socialPublicationsPath+"/"), "/"))
	if err != nil || strings.Contains(strings.Trim(strings.TrimPrefix(r.URL.Path, socialPublicationsPath+"/"), "/"), "/") {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if !validIdempotencyKey(key) || approvalID == "" {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and Approval-Request-ID Required")
		return
	}
	var input struct {
		Kind    sdk.SocialPostKind   `json:"kind"`
		Text    string               `json:"text"`
		Media   []sdk.SocialMediaRef `json:"media"`
		Buttons []sdk.SocialButton   `json:"buttons"`
	}
	if decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	publication, err := api.repository.Publication(r.Context(), socialScopeFromTenant(scope), publicationID)
	if err != nil {
		writeSocialError(w, err)
		return
	}
	if publication.Status != social.PublicationPublished {
		writeProblem(w, http.StatusConflict, "Only a published publication can be edited")
		return
	}
	variant, err := api.repository.Variant(r.Context(), socialScopeFromTenant(scope), publication.VariantID)
	if err != nil {
		writeSocialError(w, err)
		return
	}
	channel, err := api.repository.ChannelAccount(r.Context(), socialScopeFromTenant(scope), publication.ChannelAccountID)
	if err != nil {
		writeSocialError(w, err)
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), channel.ConnectorAccountID)
	if err != nil || account.Family != sdk.FamilySocial || account.Status != sdk.AccountActive || account.Health.Status != sdk.HealthHealthy {
		writeProblem(w, http.StatusConflict, "Social connector account unavailable")
		return
	}
	const editCapability = sdk.Capability("social.post.edit")
	if !api.runtime.SupportsCapability(account.ConnectorID, string(editCapability)) {
		writeProblem(w, http.StatusConflict, "Social publication editing is unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, editCapability) {
		writeProblem(w, http.StatusConflict, "Social publication editing capability is not enabled")
		return
	}
	request := sdk.SocialEditRequest{RemotePublicationID: "", Kind: input.Kind, Text: input.Text, Media: input.Media, Buttons: input.Buttons}
	remoteReceipt, err := api.receipts.Receipt(r.Context(), scope, publication.ID.String())
	if err != nil || remoteReceipt.ConnectorAccountID != account.ID || remoteReceipt.RemotePublicationID == "" {
		writeProblem(w, http.StatusConflict, "Published remote receipt is unavailable")
		return
	}
	request.RemotePublicationID = remoteReceipt.RemotePublicationID
	if request.Validate() != nil || input.Kind == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if required, requiredErr := sdk.RequiredSocialPublishCapability(request.Kind); requiredErr != nil || !sdk.CapabilityEnabled(settings, required) {
		writeProblem(w, http.StatusConflict, "Social publication capability is not enabled")
		return
	}
	if len(request.Buttons) > 0 && !sdk.CapabilityEnabled(settings, sdk.Capability("social.post.buttons")) {
		writeProblem(w, http.StatusConflict, "Social button capability is not enabled")
		return
	}
	requested, err := api.approvals.Request(r.Context(), scope, approvalID)
	if err != nil || requested.Action != "social.publication.edit" || requested.ResourceType != "social_publication" || requested.ResourceID != publication.ID.String() || requested.Risk != approval.RiskWriteSensitive || requested.State != approval.StateApproved {
		writeProblem(w, http.StatusConflict, "Approved matching request required")
		return
	}
	digestPayload, err := json.Marshal(struct {
		PublicationID string                `json:"publication_id"`
		AccountID     string                `json:"account_id"`
		Variant       social.VariantFormat  `json:"variant"`
		Request       sdk.SocialEditRequest `json:"request"`
	}{PublicationID: publication.ID.String(), AccountID: account.ID, Variant: variant.Format, Request: request})
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	digest := sha256.Sum256(digestPayload)
	receipt, fresh, err := api.operations.BeginOperation(r.Context(), scope, "social.publication.edit", key, digest)
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
		var view socialPublicationEditView
		if receipt.State != "completed" || json.Unmarshal(receipt.Result, &view) != nil || !socialPublicationEditViewValid(view) || view.RemotePublicationID != request.RemotePublicationID {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		writeJSON(w, http.StatusOK, view)
		return
	}
	editorRuntime, runtimeOK := api.runtime.(socialEditorRuntime)
	if !runtimeOK {
		writeProblem(w, http.StatusConflict, "Social publication editing is unavailable")
		return
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	editor, err := editorRuntime.SocialEditor(account, func(ctx context.Context, accountID string) (json.RawMessage, error) {
		raw, _, loadErr := api.configs.Config(ctx, scope, accountID)
		return raw, loadErr
	})
	if err != nil || editor == nil {
		writeProblem(w, http.StatusConflict, "Social publication editing is unavailable")
		return
	}
	updated, err := editor.EditSocial(r.Context(), account, runtime, request, nil)
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "Social provider unavailable")
		return
	}
	view := socialPublicationEditView{RemotePublicationID: updated.RemotePublicationID, Status: updated.Status, Updated: updated.Status == sdk.SocialRemotePublished, ObservedAt: updated.ObservedAt}
	if updated.Validate() != nil || !view.Updated || view.RemotePublicationID != request.RemotePublicationID || !socialPublicationEditViewValid(view) {
		writeProblem(w, http.StatusBadGateway, "Social provider returned an invalid result")
		return
	}
	encoded, err := json.Marshal(view)
	if err != nil || api.operations.CompleteOperation(r.Context(), scope, "social.publication.edit", key, encoded) != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func socialScopeFromTenant(scope tenancy.Scope) social.Scope {
	value, _ := social.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	return value
}

func socialPublicationEditViewValid(view socialPublicationEditView) bool {
	return view.RemotePublicationID != "" && view.Status == sdk.SocialRemotePublished && view.Updated && !view.ObservedAt.IsZero() && view.ObservedAt.Location() == time.UTC
}

func (api socialAPI) deletePublication(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	approvalID := strings.TrimSpace(r.Header.Get("Approval-Request-ID"))
	if !scopeOK || !principalOK || principal.Subject == "" {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if api.repository == nil || api.accounts == nil || api.runtime == nil || api.secrets == nil || api.configs == nil || api.receipts == nil || api.approvals == nil || api.operations == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	pathID := strings.Trim(strings.TrimPrefix(r.URL.Path, socialPublicationsPath+"/"), "/")
	publicationID, err := social.ParsePublicationID(pathID)
	if err != nil || strings.Contains(pathID, "/") {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if !validIdempotencyKey(key) || approvalID == "" {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and Approval-Request-ID Required")
		return
	}
	publication, err := api.repository.Publication(r.Context(), socialScopeFromTenant(scope), publicationID)
	if err != nil {
		writeSocialError(w, err)
		return
	}
	if publication.Status != social.PublicationPublished {
		writeProblem(w, http.StatusConflict, "Only a published publication can be deleted")
		return
	}
	channel, err := api.repository.ChannelAccount(r.Context(), socialScopeFromTenant(scope), publication.ChannelAccountID)
	if err != nil {
		writeSocialError(w, err)
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), channel.ConnectorAccountID)
	if err != nil || account.Family != sdk.FamilySocial || account.Status != sdk.AccountActive || account.Health.Status != sdk.HealthHealthy {
		writeProblem(w, http.StatusConflict, "Social connector account unavailable")
		return
	}
	const deleteCapability = sdk.Capability("social.post.delete")
	if !api.runtime.SupportsCapability(account.ConnectorID, string(deleteCapability)) {
		writeProblem(w, http.StatusConflict, "Social publication deletion is unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, deleteCapability) {
		writeProblem(w, http.StatusConflict, "Social publication deletion capability is not enabled")
		return
	}
	remoteReceipt, err := api.receipts.Receipt(r.Context(), scope, publication.ID.String())
	if err != nil || remoteReceipt.ConnectorAccountID != account.ID || remoteReceipt.RemotePublicationID == "" {
		writeProblem(w, http.StatusConflict, "Published remote receipt is unavailable")
		return
	}
	requested, err := api.approvals.Request(r.Context(), scope, approvalID)
	if err != nil || requested.Action != "social.publication.delete" || requested.ResourceType != "social_publication" || requested.ResourceID != publication.ID.String() || requested.Risk != approval.RiskWriteSensitive || requested.State != approval.StateApproved {
		writeProblem(w, http.StatusConflict, "Approved matching request required")
		return
	}
	digestPayload, err := json.Marshal(struct {
		PublicationID       string `json:"publication_id"`
		AccountID           string `json:"account_id"`
		RemotePublicationID string `json:"remote_publication_id"`
	}{PublicationID: publication.ID.String(), AccountID: account.ID, RemotePublicationID: remoteReceipt.RemotePublicationID})
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	digest := sha256.Sum256(digestPayload)
	receipt, fresh, err := api.operations.BeginOperation(r.Context(), scope, "social.publication.delete", key, digest)
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
		var view socialPublicationDeleteView
		if receipt.State != "completed" || json.Unmarshal(receipt.Result, &view) != nil || !socialPublicationDeleteViewValid(view) || view.RemotePublicationID != remoteReceipt.RemotePublicationID {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		writeJSON(w, http.StatusOK, view)
		return
	}
	deleterRuntime, runtimeOK := api.runtime.(socialDeleterRuntime)
	if !runtimeOK {
		writeProblem(w, http.StatusConflict, "Social publication deletion is unavailable")
		return
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	deleter, err := deleterRuntime.SocialDeleter(account, func(ctx context.Context, accountID string) (json.RawMessage, error) {
		raw, _, loadErr := api.configs.Config(ctx, scope, accountID)
		return raw, loadErr
	})
	if err != nil || deleter == nil {
		writeProblem(w, http.StatusConflict, "Social publication deletion is unavailable")
		return
	}
	deleted, err := deleter.DeleteSocial(r.Context(), account, runtime, remoteReceipt.RemotePublicationID)
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "Social provider unavailable")
		return
	}
	view := socialPublicationDeleteView{RemotePublicationID: deleted.RemotePublicationID, Deleted: deleted.Deleted, ObservedAt: deleted.ObservedAt}
	if deleted.Validate() != nil || !view.Deleted || view.RemotePublicationID != remoteReceipt.RemotePublicationID || !socialPublicationDeleteViewValid(view) {
		writeProblem(w, http.StatusBadGateway, "Social provider returned an invalid result")
		return
	}
	encoded, err := json.Marshal(view)
	if err != nil || api.operations.CompleteOperation(r.Context(), scope, "social.publication.delete", key, encoded) != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func socialPublicationDeleteViewValid(view socialPublicationDeleteView) bool {
	return view.RemotePublicationID != "" && view.Deleted && !view.ObservedAt.IsZero() && view.ObservedAt.Location() == time.UTC
}

func validSocialText(value string, maxRunes int) bool {
	return maxRunes > 0 && value != "" && strings.TrimSpace(value) == value && utf8.RuneCountInString(value) <= maxRunes
}

func (api socialAPI) publicationResponse(ctx context.Context, scope social.Scope, publication social.Publication) (socialPublicationView, error) {
	variant, err := api.repository.Variant(ctx, scope, publication.VariantID)
	if err != nil {
		return socialPublicationView{}, err
	}
	channel, err := api.repository.ChannelAccount(ctx, scope, publication.ChannelAccountID)
	if err != nil {
		return socialPublicationView{}, err
	}
	return socialPublicationView{ID: publication.ID.String(), ChannelAccountID: channel.ID.String(), ChannelName: channel.DisplayName, Text: variant.Body, Buttons: variant.Buttons, Status: string(publication.Status), ReasonCode: publication.ReasonCode, Attempt: publication.Attempt, PublishAt: publication.Schedule.PublishAt, PublishedAt: publication.PublishedAt, Version: publication.Version, CreatedAt: publication.CreatedAt}, nil
}

func socialRequestScope(w http.ResponseWriter, r *http.Request) (tenancy.Scope, social.Scope, bool) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return tenancy.Scope{}, social.Scope{}, false
	}
	value, err := social.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	if err != nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return tenancy.Scope{}, social.Scope{}, false
	}
	return scope, value, true
}

func socialMutation(actor, correlation string) social.Mutation {
	now := time.Now().UTC()
	return social.Mutation{EventID: newApprovalID(), AuditID: newApprovalID(), ActorID: boundedActorRef(actor), Source: "api.social", CorrelationID: correlation, OccurredAt: now}
}

func socialChannelResponse(channel social.ChannelAccount) socialChannelView {
	capabilities := make([]string, 0, len(channel.Capabilities))
	for _, capability := range channel.Capabilities {
		capabilities = append(capabilities, string(capability))
	}
	return socialChannelView{ID: channel.ID.String(), ConnectorAccountID: channel.ConnectorAccountID, DisplayName: channel.DisplayName, Capabilities: capabilities, Status: string(channel.Status), Version: channel.Version}
}

func socialTextEnabled(settings []sdk.AccountCapabilitySetting) bool {
	for _, setting := range settings {
		if setting.Capability == sdk.Capability("social.post.text") && setting.Enabled {
			return true
		}
	}
	return false
}

func socialButtonEnabled(settings []sdk.AccountCapabilitySetting) bool {
	for _, setting := range settings {
		if setting.Capability == sdk.Capability("social.post.buttons") && setting.Enabled {
			return true
		}
	}
	return false
}

func socialButtonsEqual(left, right []social.Button) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func socialSchedulesEqual(left, right social.Schedule) bool {
	if left.Mode != right.Mode || (left.PublishAt == nil) != (right.PublishAt == nil) {
		return false
	}
	return left.PublishAt == nil || left.PublishAt.Equal(*right.PublishAt)
}

func socialLimit(r *http.Request) (int, bool) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return 0, false
		}
		limit = value
	}
	return limit, limit >= 1 && limit <= 100
}

func relatedSocialID(value string, discriminator byte) (string, error) {
	raw, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	if err != nil || len(raw) != 16 || discriminator == 0 {
		return "", social.ErrInvalidRecord
	}
	raw[15] ^= discriminator
	encoded := hex.EncodeToString(raw)
	result := encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
	if _, err := social.ParseContentID(result); err != nil {
		return "", err
	}
	return result, nil
}

func writeSocialError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, social.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found")
	case errors.Is(err, social.ErrConflict), errors.Is(err, social.ErrInvalidState), errors.Is(err, social.ErrCapabilityMissing), errors.Is(err, social.ErrChannelUnavailable):
		writeProblem(w, http.StatusConflict, "Social state conflict")
	case errors.Is(err, social.ErrInvalidRecord), errors.Is(err, social.ErrMediaUnavailable):
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	default:
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
	}
}
