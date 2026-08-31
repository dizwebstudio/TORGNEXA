package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/logistics"
	"github.com/torgnexa/torgnexa/internal/core/social"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/socialdispatchrepo"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

func TestRelatedSocialIDPreservesUUIDv7AndSeparatesRecords(t *testing.T) {
	publication := "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0204"
	content, err := relatedSocialID(publication, 1)
	if err != nil {
		t.Fatal(err)
	}
	variant, err := relatedSocialID(publication, 2)
	if err != nil {
		t.Fatal(err)
	}
	if content == variant || content == publication || variant == publication {
		t.Fatalf("derived IDs are not distinct: %s %s %s", publication, content, variant)
	}
	if _, err := social.ParseContentID(content); err != nil {
		t.Fatal(err)
	}
	if _, err := social.ParseVariantID(variant); err != nil {
		t.Fatal(err)
	}
}

func TestSocialRoutesExposeOnlyCanonicalChannelAndPublicationSurface(t *testing.T) {
	routes := newSocialRoutes(nil, nil, nil)
	if len(routes) != 7 {
		t.Fatalf("got %d social routes, want 7", len(routes))
	}
	for _, route := range routes {
		if route.Permission != "connectors.read" && route.Permission != "connectors.accounts.write" && route.Permission != "social.post.edit" && route.Permission != "social.post.delete" {
			t.Fatalf("unexpected permission %q", route.Permission)
		}
	}
}

func TestValidSocialTextAppliesProductionLimit(t *testing.T) {
	if !validSocialText("Привет, мир", 4000) {
		t.Fatal("valid Unicode text was rejected")
	}
	for _, value := range []string{"", " с пробелом", string(make([]rune, 4001))} {
		if validSocialText(value, 4000) {
			t.Fatalf("invalid social text was accepted: length=%d", len([]rune(value)))
		}
	}
	if validSocialText("text", 0) || validSocialText("text", -1) {
		t.Fatal("non-executable social text limit was accepted")
	}
}

type socialEditRepositoryStub struct {
	social.Repository
	publication social.Publication
	variant     social.ContentVariant
	channel     social.ChannelAccount
}

func (stub socialEditRepositoryStub) Publication(context.Context, social.Scope, social.PublicationID) (social.Publication, error) {
	return stub.publication, nil
}
func (stub socialEditRepositoryStub) Variant(context.Context, social.Scope, social.VariantID) (social.ContentVariant, error) {
	return stub.variant, nil
}
func (stub socialEditRepositoryStub) ChannelAccount(context.Context, social.Scope, social.ChannelAccountID) (social.ChannelAccount, error) {
	return stub.channel, nil
}
func (stub socialEditRepositoryStub) ListChannelAccounts(context.Context, social.Scope, int) ([]social.ChannelAccount, error) {
	return nil, errors.New("not used")
}
func (stub socialEditRepositoryStub) ListPublications(context.Context, social.Scope, int) ([]social.Publication, error) {
	return nil, errors.New("not used")
}

type socialEditAccountStub struct {
	account  sdk.Account
	settings []sdk.AccountCapabilitySetting
}

func (stub socialEditAccountStub) AccountByID(context.Context, string, string, string) (sdk.Account, error) {
	return stub.account, nil
}
func (stub socialEditAccountStub) AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error) {
	return stub.settings, nil
}

type socialEditConfigStub struct{}

func (socialEditConfigStub) Config(context.Context, tenancy.Scope, string) (json.RawMessage, int64, error) {
	return json.RawMessage(`{"chat_id":-70801090403050}`), 1, nil
}
func (socialEditConfigStub) Put(context.Context, tenancy.Scope, string, json.RawMessage, int64) (int64, error) {
	return 1, nil
}

type socialEditRuntimeStub struct {
	connectorRuntimeAdmission
	editor    sdk.SocialEditor
	deleter   sdk.SocialDeleter
	supported bool
}

func (stub socialEditRuntimeStub) SupportsCapability(string, string) bool { return stub.supported }
func (stub socialEditRuntimeStub) SocialEditor(sdk.Account, builtinruntime.ConfigLoader) (sdk.SocialEditor, error) {
	return stub.editor, nil
}
func (stub socialEditRuntimeStub) SocialDeleter(sdk.Account, builtinruntime.ConfigLoader) (sdk.SocialDeleter, error) {
	return stub.deleter, nil
}

type socialEditEditorStub struct {
	result  sdk.SocialPublishResult
	request sdk.SocialEditRequest
}

func (stub *socialEditEditorStub) EditSocial(_ context.Context, _ sdk.Account, runtime sdk.Runtime, request sdk.SocialEditRequest, _ sdk.MediaAccessor) (sdk.SocialPublishResult, error) {
	if runtime == nil {
		return sdk.SocialPublishResult{}, errors.New("runtime missing")
	}
	stub.request = request
	return stub.result, nil
}

type socialDeleteDeleterStub struct {
	result sdk.SocialDeleteResult
	calls  int
}

func (stub *socialDeleteDeleterStub) DeleteSocial(_ context.Context, _ sdk.Account, runtime sdk.Runtime, remoteID string) (sdk.SocialDeleteResult, error) {
	if runtime == nil {
		return sdk.SocialDeleteResult{}, errors.New("runtime missing")
	}
	stub.calls++
	if stub.result.RemotePublicationID == "" {
		stub.result.RemotePublicationID = remoteID
	}
	return stub.result, nil
}

type socialEditReceiptStub struct{ receipt socialdispatchrepo.Receipt }

func (stub socialEditReceiptStub) Receipt(context.Context, tenancy.Scope, string) (socialdispatchrepo.Receipt, error) {
	return stub.receipt, nil
}

type socialEditOperationStub struct {
	receipt  logistics.OperationReceipt
	fresh    bool
	complete json.RawMessage
}

func (stub *socialEditOperationStub) BeginOperation(context.Context, tenancy.Scope, string, string, [32]byte) (logistics.OperationReceipt, bool, error) {
	return stub.receipt, stub.fresh, nil
}
func (stub *socialEditOperationStub) CompleteOperation(_ context.Context, _ tenancy.Scope, _, _ string, result json.RawMessage) error {
	stub.complete = append([]byte(nil), result...)
	return nil
}

func TestEditSocialPublicationRequiresApprovalAndPersistsConfirmedResult(t *testing.T) {
	now := time.Date(2026, 8, 31, 11, 0, 0, 0, time.UTC)
	scope := validTestScope(t)
	org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
	publicationID := social.PublicationID("018f0e8b-8a58-7def-8000-000000000104")
	channelID := social.ChannelAccountID("018f0e8b-8a58-7def-8000-000000000102")
	variantID := social.VariantID("018f0e8b-8a58-7def-8000-000000000103")
	account := sdk.Account{ID: "telegram-main", OrganizationID: org, WorkspaceID: workspace, ConnectorID: "telegram", Family: sdk.FamilySocial, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthHealthy, CheckedAt: now}, CreatedAt: now, UpdatedAt: now}
	publication := social.Publication{ID: publicationID, OrganizationID: org, WorkspaceID: workspace, VariantID: variantID, ChannelAccountID: channelID, Schedule: social.ImmediateSchedule(), Status: social.PublicationPublished, Attempt: 1, Version: 3, CreatedAt: now.Add(-time.Hour), UpdatedAt: now, PublishedAt: func() *time.Time { value := now.Add(-time.Minute); return &value }()}
	variant := social.ContentVariant{ID: variantID, OrganizationID: org, WorkspaceID: workspace, ContentID: social.ContentID("018f0e8b-8a58-7def-8000-000000000101"), Format: social.FormatText, Body: "Старый текст", Version: 1, CreatedAt: now.Add(-time.Hour)}
	channel := social.ChannelAccount{ID: channelID, OrganizationID: org, WorkspaceID: workspace, ConnectorAccountID: account.ID, DisplayName: "Основной Telegram", Capabilities: []social.Capability{social.CapabilityPostText}, Status: social.ChannelActive, Version: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now}
	editor := &socialEditEditorStub{result: sdk.SocialPublishResult{RemotePublicationID: "tg:-70801090403050:42", Status: sdk.SocialRemotePublished, ObservedAt: now}}
	operations := &socialEditOperationStub{fresh: true, receipt: logistics.OperationReceipt{State: "pending", Result: json.RawMessage(`{}`)}}
	routes := newSocialRoutes(socialEditRepositoryStub{publication: publication, variant: variant, channel: channel}, socialEditAccountStub{account: account, settings: []sdk.AccountCapabilitySetting{
		{Capability: "social.post.edit", Direction: sdk.CapabilityWrite, Risk: sdk.CapabilityRiskWriteSensitive, ApprovalRequired: true, Enabled: true},
		{Capability: "social.post.text", Direction: sdk.CapabilityWrite, Risk: sdk.CapabilityRiskWriteSensitive, ApprovalRequired: true, Enabled: true},
	}}, socialEditRuntimeStub{supported: true, editor: editor}, socialRouteDependency{secrets: struct{ secrets.SecretProvider }{}, configs: socialEditConfigStub{}, receipts: socialEditReceiptStub{receipt: socialdispatchrepo.Receipt{PublicationID: publicationID.String(), ConnectorAccountID: account.ID, RemotePublicationID: "tg:-70801090403050:42", ObservedAt: now}}, approvals: logisticsApprovalStub{request: approval.Request{ID: "approval-edit-1", Action: "social.publication.edit", ResourceType: "social_publication", ResourceID: publicationID.String(), Risk: approval.RiskWriteSensitive, State: approval.StateApproved}}, operations: operations})
	var route ProtectedRoute
	for _, candidate := range routes {
		if candidate.Method == http.MethodPatch {
			route = candidate
		}
	}
	body := strings.NewReader(`{"kind":"text","text":"Новый текст"}`)
	request := httptest.NewRequest(http.MethodPatch, socialPublicationsPath+"/"+publicationID.String(), body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "018f0e8b-8a58-7def-8000-000000000105")
	request.Header.Set("Approval-Request-ID", "approval-edit-1")
	ctx := context.WithValue(request.Context(), requestScopeKey{}, scope)
	ctx = context.WithValue(ctx, requestIdentityKey{}, Principal{Issuer: "https://id.example.test", Subject: "operator-1"})
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request.WithContext(ctx))
	if response.Code != http.StatusCreated || len(operations.complete) == 0 || editor.request.RemotePublicationID != "tg:-70801090403050:42" || editor.request.Text != "Новый текст" {
		t.Fatalf("status=%d body=%s request=%+v complete=%s", response.Code, response.Body.String(), editor.request, operations.complete)
	}
}

func TestDeleteSocialPublicationRequiresApprovalAndDoesNotRepeatPendingOperation(t *testing.T) {
	now := time.Date(2026, 8, 31, 11, 0, 0, 0, time.UTC)
	scope := validTestScope(t)
	org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
	publicationID := social.PublicationID("018f0e8b-8a58-7def-8000-000000000204")
	channelID := social.ChannelAccountID("018f0e8b-8a58-7def-8000-000000000202")
	variantID := social.VariantID("018f0e8b-8a58-7def-8000-000000000203")
	account := sdk.Account{ID: "telegram-main", OrganizationID: org, WorkspaceID: workspace, ConnectorID: "telegram", Family: sdk.FamilySocial, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthHealthy, CheckedAt: now}, CreatedAt: now, UpdatedAt: now}
	publication := social.Publication{ID: publicationID, OrganizationID: org, WorkspaceID: workspace, VariantID: variantID, ChannelAccountID: channelID, Schedule: social.ImmediateSchedule(), Status: social.PublicationPublished, Attempt: 1, Version: 3, CreatedAt: now.Add(-time.Hour), UpdatedAt: now, PublishedAt: func() *time.Time { value := now.Add(-time.Minute); return &value }()}
	variant := social.ContentVariant{ID: variantID, OrganizationID: org, WorkspaceID: workspace, ContentID: social.ContentID("018f0e8b-8a58-7def-8000-000000000201"), Format: social.FormatText, Body: "Сообщение", Version: 1, CreatedAt: now.Add(-time.Hour)}
	channel := social.ChannelAccount{ID: channelID, OrganizationID: org, WorkspaceID: workspace, ConnectorAccountID: account.ID, DisplayName: "Основной Telegram", Capabilities: []social.Capability{social.CapabilityPostText}, Status: social.ChannelActive, Version: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now}
	deleter := &socialDeleteDeleterStub{result: sdk.SocialDeleteResult{RemotePublicationID: "tg:-70801090403050:42", Deleted: true, ObservedAt: now}}
	operations := &socialEditOperationStub{fresh: true, receipt: logistics.OperationReceipt{State: "pending", Result: json.RawMessage(`{}`)}}
	dependencies := socialRouteDependency{secrets: struct{ secrets.SecretProvider }{}, configs: socialEditConfigStub{}, receipts: socialEditReceiptStub{receipt: socialdispatchrepo.Receipt{PublicationID: publicationID.String(), ConnectorAccountID: account.ID, RemotePublicationID: "tg:-70801090403050:42", ObservedAt: now}}, approvals: logisticsApprovalStub{request: approval.Request{ID: "approval-delete-1", Action: "social.publication.delete", ResourceType: "social_publication", ResourceID: publicationID.String(), Risk: approval.RiskWriteSensitive, State: approval.StateApproved}}, operations: operations}
	routes := newSocialRoutes(socialEditRepositoryStub{publication: publication, variant: variant, channel: channel}, socialEditAccountStub{account: account, settings: []sdk.AccountCapabilitySetting{{Capability: "social.post.delete", Direction: sdk.CapabilityWrite, Risk: sdk.CapabilityRiskWriteSensitive, ApprovalRequired: true, Enabled: true}}}, socialEditRuntimeStub{supported: true, deleter: deleter}, dependencies)
	var route ProtectedRoute
	for _, candidate := range routes {
		if candidate.Method == http.MethodDelete {
			route = candidate
		}
	}
	request := httptest.NewRequest(http.MethodDelete, socialPublicationsPath+"/"+publicationID.String(), nil)
	request.Header.Set("Idempotency-Key", "018f0e8b-8a58-7def-8000-000000000205")
	request.Header.Set("Approval-Request-ID", "approval-delete-1")
	ctx := context.WithValue(request.Context(), requestScopeKey{}, scope)
	ctx = context.WithValue(ctx, requestIdentityKey{}, Principal{Issuer: "https://id.example.test", Subject: "operator-1"})
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request.WithContext(ctx))
	if response.Code != http.StatusCreated || len(operations.complete) == 0 || deleter.calls != 1 {
		t.Fatalf("status=%d body=%s calls=%d complete=%s", response.Code, response.Body.String(), deleter.calls, operations.complete)
	}

	pending := &socialEditOperationStub{fresh: false, receipt: logistics.OperationReceipt{State: "pending", Result: json.RawMessage(`{}`)}}
	pendingDependencies := dependencies
	pendingDependencies.operations = pending
	pendingRoutes := newSocialRoutes(socialEditRepositoryStub{publication: publication, variant: variant, channel: channel}, socialEditAccountStub{account: account, settings: []sdk.AccountCapabilitySetting{{Capability: "social.post.delete", Direction: sdk.CapabilityWrite, Risk: sdk.CapabilityRiskWriteSensitive, ApprovalRequired: true, Enabled: true}}}, socialEditRuntimeStub{supported: true, deleter: deleter}, pendingDependencies)
	for _, candidate := range pendingRoutes {
		if candidate.Method == http.MethodDelete {
			request = httptest.NewRequest(http.MethodDelete, socialPublicationsPath+"/"+publicationID.String(), nil)
			request.Header.Set("Idempotency-Key", "018f0e8b-8a58-7def-8000-000000000206")
			request.Header.Set("Approval-Request-ID", "approval-delete-1")
			ctx = context.WithValue(request.Context(), requestScopeKey{}, scope)
			ctx = context.WithValue(ctx, requestIdentityKey{}, Principal{Issuer: "https://id.example.test", Subject: "operator-1"})
			response = httptest.NewRecorder()
			candidate.Handler.ServeHTTP(response, request.WithContext(ctx))
			if response.Code != http.StatusAccepted || deleter.calls != 1 {
				t.Fatalf("pending status=%d body=%s calls=%d", response.Code, response.Body.String(), deleter.calls)
			}
		}
	}
}
