package connectors

import (
	"context"
	"errors"
	"regexp"
	"time"
)

var ErrInvalidEDORequest = errors.New("connectors: invalid edo request")
var edoRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)

type EDODocumentRequest struct{ RemoteID string }
type EDODocument struct {
	RemoteID, ExternalID, Kind, Status, CounterpartyRef, SignatureRef, MChDRef string
	ObservedAt                                                                 time.Time
}

func (r EDODocumentRequest) Validate() error {
	if !edoRefPattern.MatchString(r.RemoteID) {
		return ErrInvalidEDORequest
	}
	return nil
}
func (d EDODocument) Validate() error {
	if !edoRefPattern.MatchString(d.RemoteID) || !safeCodePattern.MatchString(d.Kind) || !safeCodePattern.MatchString(d.Status) || d.ObservedAt.IsZero() || d.ObservedAt.Location() != time.UTC {
		return ErrInvalidEDORequest
	}
	return nil
}

type EDOSendRequest struct{ ExternalID, Kind, CounterpartyRef, ArtifactRef, SignatureRef, MChDRef, ApprovalRef, IdempotencyKey string }
type EDOSendResult struct {
	RemoteID, Status string
	AcceptedAt       time.Time
}

func (r EDOSendRequest) Validate() error {
	if !edoRefPattern.MatchString(r.ExternalID) || !safeCodePattern.MatchString(r.Kind) || !edoRefPattern.MatchString(r.CounterpartyRef) || !edoRefPattern.MatchString(r.ArtifactRef) || !edoRefPattern.MatchString(r.SignatureRef) || !edoRefPattern.MatchString(r.ApprovalRef) || !edoRefPattern.MatchString(r.IdempotencyKey) {
		return ErrInvalidEDORequest
	}
	if r.MChDRef != "" && !edoRefPattern.MatchString(r.MChDRef) {
		return ErrInvalidEDORequest
	}
	return nil
}
func (r EDOSendResult) Validate() error {
	if !edoRefPattern.MatchString(r.RemoteID) || !safeCodePattern.MatchString(r.Status) || r.AcceptedAt.IsZero() || r.AcceptedAt.Location() != time.UTC {
		return ErrInvalidEDORequest
	}
	return nil
}

type EDOSignWorkflowRequest struct{ ExternalID, ArtifactRef, CertificateRef, MChDRef, ApprovalRef string }
type EDOSignWorkflowResult struct {
	WorkflowRef, Status string
	CreatedAt           time.Time
}

func (r EDOSignWorkflowRequest) Validate() error {
	if !edoRefPattern.MatchString(r.ExternalID) || !edoRefPattern.MatchString(r.ArtifactRef) || !edoRefPattern.MatchString(r.CertificateRef) || !edoRefPattern.MatchString(r.ApprovalRef) {
		return ErrInvalidEDORequest
	}
	return nil
}

type EDODocumentReader interface {
	ReadEDODocument(context.Context, Account, Runtime, EDODocumentRequest) (EDODocument, error)
}
type EDODocumentSender interface {
	SendEDODocument(context.Context, Account, Runtime, EDOSendRequest) (EDOSendResult, error)
}
type EDOSignWorkflowRequester interface {
	RequestEDOSigning(context.Context, Account, Runtime, EDOSignWorkflowRequest) (EDOSignWorkflowResult, error)
}
