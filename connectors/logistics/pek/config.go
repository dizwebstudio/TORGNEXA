package pek

import (
	"context"
	"errors"
	"regexp"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var pekWarehouseIDPattern = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

var (
	ErrConfigurationMissing = errors.New("pek: configuration missing")
	ErrInvalidConfiguration = errors.New("pek: invalid configuration")
)

// Configuration contains non-secret sender data required by the ПЭК
// preregistration API. Credentials remain in sdk.Runtime and are never part
// of this structure.
type Configuration struct {
	SenderWarehouseID string `json:"sender_warehouse_id"`
	SenderLegalForm   int    `json:"sender_legal_form"`
	SenderTitle       string `json:"sender_title"`
	SenderINN         string `json:"sender_inn"`
	SenderKPP         string `json:"sender_kpp,omitempty"`
}

// ConfigurationSource resolves tenant-scoped, non-secret ПЭК settings.
type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

// Validate ensures that the configuration is sufficient for the bounded
// B2C/self-delivery preregistration path.
func (configuration Configuration) Validate() error {
	if !pekWarehouseIDPattern.MatchString(strings.TrimSpace(configuration.SenderWarehouseID)) || strings.TrimSpace(configuration.SenderWarehouseID) != configuration.SenderWarehouseID || configuration.SenderTitle == "" || strings.TrimSpace(configuration.SenderTitle) != configuration.SenderTitle || len(configuration.SenderTitle) > 512 {
		return ErrInvalidConfiguration
	}
	if configuration.SenderLegalForm != 1 && configuration.SenderLegalForm != 2 && configuration.SenderLegalForm != 3 {
		return ErrInvalidConfiguration
	}
	if configuration.SenderLegalForm != 3 && !digits(configuration.SenderINN, 5, 20) {
		return ErrInvalidConfiguration
	}
	if configuration.SenderKPP != "" && !digits(configuration.SenderKPP, 4, 12) {
		return ErrInvalidConfiguration
	}
	return nil
}

func digits(value string, min, max int) bool {
	if len(value) < min || len(value) > max {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
