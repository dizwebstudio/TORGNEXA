package fivepost

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrConfigurationMissing = errors.New("5Post: configuration missing")
	ErrInvalidConfiguration = errors.New("5Post: invalid configuration")
	fivepostLocationPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
)

// Configuration contains tenant-scoped, non-secret settings required by the
// universal 5Post order endpoint. API keys remain in sdk.Runtime.
type Configuration struct {
	SenderLocation      string `json:"sender_location"`
	ReturnLocation      string `json:"return_location,omitempty"`
	BrandName           string `json:"brand_name,omitempty"`
	UndeliverableOption string `json:"undeliverable_option"`
	BarcodeEnrichment   string `json:"barcode_enrichment"`
}

// ConfigurationSource resolves non-secret account settings.
type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

// Validate ensures the create route cannot guess a provider warehouse,
// return policy or barcode contract.
func (configuration Configuration) Validate() error {
	if !fivepostLocationPattern.MatchString(configuration.SenderLocation) || strings.TrimSpace(configuration.SenderLocation) != configuration.SenderLocation {
		return ErrInvalidConfiguration
	}
	if configuration.ReturnLocation != "" && (!fivepostLocationPattern.MatchString(configuration.ReturnLocation) || strings.TrimSpace(configuration.ReturnLocation) != configuration.ReturnLocation) {
		return ErrInvalidConfiguration
	}
	if !validFivePostText(configuration.BrandName, 128, true) {
		return ErrInvalidConfiguration
	}
	if configuration.UndeliverableOption != "RETURN" && configuration.UndeliverableOption != "UTILIZATION" {
		return ErrInvalidConfiguration
	}
	switch configuration.BarcodeEnrichment {
	case "NONE", "ENABLED", "PARTIAL":
	default:
		return ErrInvalidConfiguration
	}
	return nil
}

func validFivePostText(value string, maxRunes int, optional bool) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return optional
	}
	if utf8.RuneCountInString(value) > maxRunes {
		return false
	}
	for _, symbol := range value {
		if unicode.IsControl(symbol) {
			return false
		}
	}
	return true
}
