package connectors

import (
	"context"
	"errors"
	"regexp"
	"time"
)

var ErrInvalidSMSRequest = errors.New("connectors: invalid sms request")
var smsPhonePattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

type SMSClass string

const (
	SMSTransactional SMSClass = "transactional"
	SMSMarketing     SMSClass = "marketing"
)

type SMSRequest struct {
	ExternalID     string
	PhoneE164      string // ephemeral PII; host evidence must fingerprint/redact this value.
	Text           string
	Class          SMSClass
	ConsentRef     string
	IdempotencyKey string
}

func (r SMSRequest) Validate() error {
	if !logisticsRefPattern.MatchString(r.ExternalID) || !smsPhonePattern.MatchString(r.PhoneE164) || r.Text == "" || len(r.Text) > 1000 || !logisticsRefPattern.MatchString(r.IdempotencyKey) || (r.Class != SMSTransactional && r.Class != SMSMarketing) {
		return ErrInvalidSMSRequest
	}
	if r.Class == SMSMarketing && r.ConsentRef == "" {
		return ErrInvalidSMSRequest
	}
	return nil
}

type SMSResult struct {
	RemoteID, Status string
	ObservedAt       time.Time
}

func (r SMSResult) Validate() error {
	if !logisticsRefPattern.MatchString(r.RemoteID) || !safeCodePattern.MatchString(r.Status) || r.ObservedAt.IsZero() || r.ObservedAt.Location() != time.UTC {
		return ErrInvalidSMSRequest
	}
	return nil
}

type SMSStatusRequest struct{ RemoteID string }

type SMSSender interface {
	SendSMS(context.Context, Account, Runtime, SMSRequest) (SMSResult, error)
}
type SMSStatusReader interface {
	ReadSMSStatus(context.Context, Account, Runtime, SMSStatusRequest) (SMSResult, error)
}
