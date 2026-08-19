package connectors

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

var ErrInvalidRemoteError = errors.New("connectors: invalid normalized remote error")

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

type ErrorCategory string

const (
	ErrorInvalidRequest ErrorCategory = "invalid_request"
	ErrorUnauthorized   ErrorCategory = "unauthorized"
	ErrorForbidden      ErrorCategory = "forbidden"
	ErrorNotFound       ErrorCategory = "not_found"
	ErrorConflict       ErrorCategory = "conflict"
	ErrorRateLimited    ErrorCategory = "rate_limited"
	ErrorTransient      ErrorCategory = "transient"
	ErrorUnavailable    ErrorCategory = "unavailable"
	ErrorTimeout        ErrorCategory = "timeout"
	ErrorUnsupported    ErrorCategory = "unsupported"
	ErrorInternal       ErrorCategory = "internal"
)

func (category ErrorCategory) Valid() bool {
	switch category {
	case ErrorInvalidRequest, ErrorUnauthorized, ErrorForbidden, ErrorNotFound, ErrorConflict,
		ErrorRateLimited, ErrorTransient, ErrorUnavailable, ErrorTimeout, ErrorUnsupported, ErrorInternal:
		return true
	default:
		return false
	}
}

func (category ErrorCategory) Retryable() bool {
	return category == ErrorRateLimited || category == ErrorTransient || category == ErrorUnavailable || category == ErrorTimeout
}

// RemoteError contains normalized, bounded metadata only. Raw provider body,
// headers, URLs, tokens and error messages are deliberately absent.
type RemoteError struct {
	Category        ErrorCategory `json:"category"`
	Code            string        `json:"code"`
	RemoteRequestID string        `json:"remote_request_id,omitempty"`
	RetryAfterMS    int64         `json:"retry_after_ms,omitempty"`
}

func NewRemoteError(category ErrorCategory, code, remoteRequestID string, retryAfter time.Duration) (*RemoteError, error) {
	if retryAfter < 0 || retryAfter > 24*time.Hour {
		return nil, ErrInvalidRemoteError
	}
	value := &RemoteError{Category: category, Code: code, RemoteRequestID: remoteRequestID, RetryAfterMS: retryAfter.Milliseconds()}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func (remote *RemoteError) Validate() error {
	if remote == nil || !remote.Category.Valid() || !safeCodePattern.MatchString(remote.Code) || remote.RetryAfterMS < 0 || remote.RetryAfterMS > int64((24*time.Hour)/time.Millisecond) {
		return ErrInvalidRemoteError
	}
	if remote.RemoteRequestID != "" && !requestIDPattern.MatchString(remote.RemoteRequestID) {
		return ErrInvalidRemoteError
	}
	if remote.RetryAfterMS > 0 && remote.Category != ErrorRateLimited && remote.Category != ErrorUnavailable && remote.Category != ErrorTransient {
		return ErrInvalidRemoteError
	}
	return nil
}

func (remote *RemoteError) Error() string {
	if remote == nil {
		return "connectors: remote error"
	}
	return fmt.Sprintf("connectors: remote %s (%s)", remote.Category, remote.Code)
}

func (remote *RemoteError) Retryable() bool { return remote != nil && remote.Category.Retryable() }

// RetryDelay returns deterministic host scheduling guidance. Dynamic
// Retry-After is preferred, otherwise bounded exponential backoff is used.
func RetryDelay(policy RetryPolicy, attempt int, remote *RemoteError) (time.Duration, bool) {
	if policy.Validate() != nil || attempt < 1 || attempt >= policy.MaxAttempts || remote == nil || remote.Validate() != nil || !remote.Retryable() {
		return 0, false
	}
	max := time.Duration(policy.MaxBackoffMS) * time.Millisecond
	if remote.RetryAfterMS > 0 {
		retryAfter := time.Duration(remote.RetryAfterMS) * time.Millisecond
		if retryAfter > max {
			return max, true
		}
		return retryAfter, true
	}
	delay := time.Duration(policy.BaseBackoffMS) * time.Millisecond
	for current := 1; current < attempt; current++ {
		if delay >= max/2 {
			delay = max
			break
		}
		delay *= 2
	}
	if delay > max {
		delay = max
	}
	return delay, true
}
