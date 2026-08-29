package chestnyznak

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"time"
)

type ProductResponse struct {
	GTIN, RemoteID, Status, Name string
	ObservedAt                   time.Time
}
type CodeResponse struct {
	Code, Status, GTIN string
	ObservedAt         time.Time
}
type Transport interface {
	Ping(context.Context, []byte) error
	ProductByGTIN(context.Context, []byte, string) (ProductResponse, error)
	CodeStatuses(context.Context, []byte, []string) ([]CodeResponse, error)
}

func manifest() sdk.Manifest {
	return sdk.Manifest{ID: "chestny-znak", Name: "Chestny ZNAK", Family: sdk.FamilyGovernment, Version: "1.0.0", SDKVersion: sdk.SDKMajor, Capabilities: []sdk.Capability{"government.reconciliation.run", "government.references.read", "marking.status.read"}, Auth: []sdk.AuthRequirement{{Kind: sdk.AuthCertificate, SecretClass: "government.certificate", Required: true}}, RateLimit: sdk.RateLimitPolicy{MaxConcurrency: 2, MinIntervalMS: 200, RequestTimeoutMS: 30000, Retry: sdk.RetryPolicy{MaxAttempts: 4, BaseBackoffMS: 500, MaxBackoffMS: 30000}}}
}
func fingerprint(code string) string {
	h := sha256.Sum256([]byte(code))
	return hex.EncodeToString(h[:])
}
func (c *Connector) ReadGovernmentReference(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.GovernmentReferenceRequest) (sdk.GovernmentReference, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "government.references.read") != nil || q.Validate() != nil || q.Kind != "product_by_gtin" {
		return sdk.GovernmentReference{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var out ProductResponse
	err := useSecret(ctx, r, a, func(s []byte) error { var e error; out, e = c.transport.ProductByGTIN(ctx, s, q.Key); return e })
	if err != nil {
		return sdk.GovernmentReference{}, err
	}
	if out.GTIN != q.Key || out.RemoteID == "" || out.Status == "" || out.ObservedAt.IsZero() {
		return sdk.GovernmentReference{}, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
	}
	return sdk.GovernmentReference{Kind: q.Kind, Key: q.Key, RemoteID: out.RemoteID, Status: out.Status, Attributes: map[string]string{"name": out.Name}, ObservedAt: out.ObservedAt.UTC()}, nil
}
func (c *Connector) ReadMarkingStatus(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.MarkingStatusRequest) (sdk.MarkingStatusObservation, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "marking.status.read") != nil || q.Validate(1000) != nil {
		return sdk.MarkingStatusObservation{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var rows []CodeResponse
	err := useSecret(ctx, r, a, func(s []byte) error { var e error; rows, e = c.transport.CodeStatuses(ctx, s, q.Codes); return e })
	if err != nil {
		return sdk.MarkingStatusObservation{}, err
	}
	if len(rows) == 0 {
		return sdk.MarkingStatusObservation{}, remote(sdk.ErrorNotFound, "marking_not_found", 0)
	}
	out := sdk.MarkingStatusObservation{Items: make([]sdk.MarkingCodeStatus, 0, len(rows))}
	for _, x := range rows {
		if x.Code == "" || x.Status == "" || x.ObservedAt.IsZero() {
			return sdk.MarkingStatusObservation{}, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
		}
		out.Items = append(out.Items, sdk.MarkingCodeStatus{CodeFingerprint: fingerprint(x.Code), Status: x.Status, ProductGTIN: x.GTIN, ObservedAt: x.ObservedAt.UTC()})
	}
	if out.Validate() != nil {
		return sdk.MarkingStatusObservation{}, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
	}
	return out, nil
}
func (c *Connector) ReconcileGovernment(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.GovernmentReconciliationRequest) (sdk.GovernmentReconciliationResult, error) {
	if sdk.RequireCapability(Manifest(), "government.reconciliation.run") != nil || q.Validate(1000) != nil {
		return sdk.GovernmentReconciliationResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	obs, e := c.ReadMarkingStatus(ctx, a, r, sdk.MarkingStatusRequest{Codes: q.RemoteIDs})
	if e != nil {
		return sdk.GovernmentReconciliationResult{}, e
	}
	out := sdk.GovernmentReconciliationResult{Items: make([]sdk.GovernmentReconciliationItem, 0, len(obs.Items))}
	for _, x := range obs.Items {
		out.Items = append(out.Items, sdk.GovernmentReconciliationItem{RemoteID: x.CodeFingerprint, RemoteStatus: x.Status, ObservedAt: x.ObservedAt})
	}
	return out, nil
}

var _ sdk.MarkingStatusReader = (*Connector)(nil)
var _ sdk.GovernmentReferenceReader = (*Connector)(nil)
var _ sdk.GovernmentReconciler = (*Connector)(nil)
var _ = errors.New
