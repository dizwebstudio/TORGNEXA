package dellin

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrConfigurationMissing = errors.New("Деловые Линии: configuration missing")
	ErrInvalidConfiguration = errors.New("Деловые Линии: invalid configuration")
)

var dellinReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var dellinTerminalIDPattern = regexp.MustCompile(`^[0-9]{1,12}$`)
var dellinTimePattern = regexp.MustCompile(`^(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)

// Configuration contains tenant-scoped, non-secret values required to place
// an authenticated LTL request. Credentials remain in SecretProvider.
type Configuration struct {
	RequesterUID         string
	SenderCounteragentID int64
	FreightUID           string
	SenderTerminalID     string
	ProduceDate          string
	DerivalWorktimeStart string
	DerivalWorktimeEnd   string
	PaymentType          string
}

// ConfigurationSource resolves non-secret runtime configuration for one
// tenant account.
type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

// IsValidTerminalReference reports whether a provider-owned terminal ID is a
// canonical unsigned decimal reference suitable for the Dellin request API.
func IsValidTerminalReference(value string) bool {
	return value == strings.TrimSpace(value) && dellinTerminalIDPattern.MatchString(value)
}

// Validate checks that the configuration is explicit enough for the official
// request contract and does not rely on guessed provider identifiers.
func (configuration Configuration) Validate() error {
	if !dellinReferencePattern.MatchString(strings.TrimSpace(configuration.RequesterUID)) || configuration.RequesterUID != strings.TrimSpace(configuration.RequesterUID) {
		return ErrInvalidConfiguration
	}
	if configuration.SenderCounteragentID < 1 {
		return ErrInvalidConfiguration
	}
	if !dellinReferencePattern.MatchString(strings.TrimSpace(configuration.FreightUID)) || configuration.FreightUID != strings.TrimSpace(configuration.FreightUID) {
		return ErrInvalidConfiguration
	}
	if configuration.SenderTerminalID != "" && !IsValidTerminalReference(configuration.SenderTerminalID) {
		return ErrInvalidConfiguration
	}
	if _, err := time.Parse("2006-01-02", configuration.ProduceDate); err != nil {
		return ErrInvalidConfiguration
	}
	if !dellinTimePattern.MatchString(configuration.DerivalWorktimeStart) || !dellinTimePattern.MatchString(configuration.DerivalWorktimeEnd) {
		return ErrInvalidConfiguration
	}
	start, _ := time.Parse("15:04", configuration.DerivalWorktimeStart)
	end, _ := time.Parse("15:04", configuration.DerivalWorktimeEnd)
	if !end.After(start) {
		return ErrInvalidConfiguration
	}
	if configuration.PaymentType != "cash" && configuration.PaymentType != "noncash" {
		return ErrInvalidConfiguration
	}
	return nil
}
