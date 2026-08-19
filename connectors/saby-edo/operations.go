package sabyedo

import (
	"context"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"time"
)

type RemoteDocument struct {
	RemoteID, ExternalID, Kind, Status, CounterpartyRef, SignatureRef, MChDRef string
	ObservedAt                                                                 time.Time
}
type Transport interface {
	Ping(context.Context, []byte) error
	Read(context.Context, []byte, string) (RemoteDocument, error)
	Send(context.Context, []byte, sdk.EDOSendRequest) (RemoteDocument, error)
	RequestSigning(context.Context, []byte, sdk.EDOSignWorkflowRequest) (sdk.EDOSignWorkflowResult, error)
}

func manifest() sdk.Manifest {
	return sdk.Manifest{ID: "saby-edo", Name: "Saby EDO", Family: sdk.FamilyEDO, Version: "1.0.0", SDKVersion: 1, Capabilities: []sdk.Capability{"edo.documents.read", "edo.documents.send", "edo.documents.sign_request"}, Auth: []sdk.AuthRequirement{{Kind: sdk.AuthBearer, SecretClass: "edo.credential", Required: true}}, RateLimit: sdk.RateLimitPolicy{MaxConcurrency: 2, MinIntervalMS: 200, RequestTimeoutMS: 30000, Retry: sdk.RetryPolicy{MaxAttempts: 4, BaseBackoffMS: 500, MaxBackoffMS: 30000}}}
}
func (c *Connector) ReadEDODocument(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.EDODocumentRequest) (sdk.EDODocument, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || q.Validate() != nil || sdk.RequireCapability(Manifest(), "edo.documents.read") != nil {
		return sdk.EDODocument{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var d RemoteDocument
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; d, x = c.transport.Read(ctx, s, q.RemoteID); return x })
	if e != nil {
		return sdk.EDODocument{}, e
	}
	return sdk.EDODocument{RemoteID: d.RemoteID, ExternalID: d.ExternalID, Kind: d.Kind, Status: d.Status, CounterpartyRef: d.CounterpartyRef, SignatureRef: d.SignatureRef, MChDRef: d.MChDRef, ObservedAt: d.ObservedAt.UTC()}, nil
}
func (c *Connector) SendEDODocument(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.EDOSendRequest) (sdk.EDOSendResult, error) {
	if sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || q.Validate() != nil || sdk.RequireCapability(Manifest(), "edo.documents.send") != nil {
		return sdk.EDOSendResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var d RemoteDocument
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; d, x = c.transport.Send(ctx, s, q); return x })
	if e != nil {
		return sdk.EDOSendResult{}, e
	}
	if d.RemoteID == "" || d.Status == "" || d.ObservedAt.IsZero() {
		return sdk.EDOSendResult{}, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
	}
	return sdk.EDOSendResult{RemoteID: d.RemoteID, Status: d.Status, AcceptedAt: d.ObservedAt.UTC()}, nil
}
func (c *Connector) RequestEDOSigning(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.EDOSignWorkflowRequest) (sdk.EDOSignWorkflowResult, error) {
	if q.Validate() != nil || sdk.RequireCapability(Manifest(), "edo.documents.sign_request") != nil {
		return sdk.EDOSignWorkflowResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var out sdk.EDOSignWorkflowResult
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; out, x = c.transport.RequestSigning(ctx, s, q); return x })
	if e != nil {
		return sdk.EDOSignWorkflowResult{}, e
	}
	return out, nil
}

var _ sdk.EDODocumentReader = (*Connector)(nil)
var _ sdk.EDODocumentSender = (*Connector)(nil)
var _ sdk.EDOSignWorkflowRequester = (*Connector)(nil)
