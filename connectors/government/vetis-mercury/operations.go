package vetismercury

import (
	"context"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type Transport interface {
	Ping(context.Context, []byte) error
	Read(context.Context, []byte, sdk.GovernmentDocumentRequest) (sdk.GovernmentDocument, error)
	Write(context.Context, []byte, sdk.GovernmentWriteRequest) (sdk.GovernmentWriteResult, error)
	Inventory(context.Context, []byte, sdk.GovernmentInventoryRequest) (sdk.GovernmentInventoryObservation, error)
	Reconcile(context.Context, []byte, sdk.GovernmentReconciliationRequest) (sdk.GovernmentReconciliationResult, error)
}

func manifest() sdk.Manifest {
	return sdk.Manifest{ID: "vetis-mercury", Name: "VetIS Mercury", Family: sdk.FamilyGovernment, Version: "1.0.0", SDKVersion: 1, Capabilities: []sdk.Capability{"government.inventory.read", "government.reconciliation.run", "vetis.documents.read", "vetis.documents.write"}, Auth: []sdk.AuthRequirement{{Kind: sdk.AuthAPIKey, SecretClass: "government.credential", Required: true}}, RateLimit: sdk.RateLimitPolicy{MaxConcurrency: 2, MinIntervalMS: 200, RequestTimeoutMS: 30000, Retry: sdk.RetryPolicy{MaxAttempts: 4, BaseBackoffMS: 500, MaxBackoffMS: 30000}}}
}
func (c *Connector) ReadGovernmentDocument(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.GovernmentDocumentRequest) (sdk.GovernmentDocument, error) {
	if q.Validate() != nil || sdk.RequireCapability(Manifest(), "vetis.documents.read") != nil {
		return sdk.GovernmentDocument{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var out sdk.GovernmentDocument
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; out, x = c.transport.Read(ctx, s, q); return x })
	if e != nil {
		return sdk.GovernmentDocument{}, e
	}
	if out.Validate() != nil {
		return sdk.GovernmentDocument{}, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
	}
	return out, nil
}
func (c *Connector) WriteGovernmentDocument(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.GovernmentWriteRequest) (sdk.GovernmentWriteResult, error) {
	if q.Validate() != nil || sdk.RequireCapability(Manifest(), "vetis.documents.write") != nil {
		return sdk.GovernmentWriteResult{}, remote(sdk.ErrorInvalidRequest, "approval_or_request_rejected", 0)
	}
	var out sdk.GovernmentWriteResult
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; out, x = c.transport.Write(ctx, s, q); return x })
	if e != nil {
		return sdk.GovernmentWriteResult{}, e
	}
	if out.Validate() != nil {
		return sdk.GovernmentWriteResult{}, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
	}
	return out, nil
}
func (c *Connector) ReadGovernmentInventory(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.GovernmentInventoryRequest) (sdk.GovernmentInventoryObservation, error) {
	if q.Validate() != nil || sdk.RequireCapability(Manifest(), "government.inventory.read") != nil {
		return sdk.GovernmentInventoryObservation{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var out sdk.GovernmentInventoryObservation
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; out, x = c.transport.Inventory(ctx, s, q); return x })
	if e != nil {
		return sdk.GovernmentInventoryObservation{}, e
	}
	return out, nil
}
func (c *Connector) ReconcileGovernment(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.GovernmentReconciliationRequest) (sdk.GovernmentReconciliationResult, error) {
	if q.Validate(500) != nil || sdk.RequireCapability(Manifest(), "government.reconciliation.run") != nil {
		return sdk.GovernmentReconciliationResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var out sdk.GovernmentReconciliationResult
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; out, x = c.transport.Reconcile(ctx, s, q); return x })
	return out, e
}

var _ sdk.GovernmentDocumentReader = (*Connector)(nil)
var _ sdk.GovernmentDocumentWriter = (*Connector)(nil)
var _ sdk.GovernmentInventoryReader = (*Connector)(nil)
var _ sdk.GovernmentReconciler = (*Connector)(nil)
