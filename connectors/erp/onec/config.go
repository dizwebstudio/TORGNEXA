package onec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrConfigurationMissing = errors.New("onec: configuration missing")
	ErrInvalidConfiguration = errors.New("onec: invalid configuration")
)

var (
	hostPattern     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)
	odataName       = regexp.MustCompile(`^[\pL_][\pL\pN_]{0,127}$`)
	resourcePattern = regexp.MustCompile(`^(?:Catalog|AccumulationRegister)_[\pL\pN_]{1,120}$`)
)

type CatalogMapping struct {
	Resource      string
	IDField       string
	CodeField     string
	SKUField      string
	TitleField    string
	BrandField    string
	RevisionField string
	ArchivedField string
}

type InventoryMapping struct {
	Resource      string
	Function      string
	ProductField  string
	LocationField string
	QuantityField string
}

type Configuration struct {
	Host      string
	BasePath  string
	Catalog   CatalogMapping
	Inventory InventoryMapping
}

// ConfigurationSource is host-injected non-secret account configuration. It is
// local to this provider so Connector SDK v1 Runtime remains frozen.
type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (configuration Configuration) Validate() error {
	if !validHost(configuration.Host) || !validBasePath(configuration.BasePath) || configuration.Catalog.validate() != nil || configuration.Inventory.validate() != nil {
		return ErrInvalidConfiguration
	}
	return nil
}

func (mapping CatalogMapping) validate() error {
	if !resourcePattern.MatchString(mapping.Resource) || !validName(mapping.IDField) || !validName(mapping.CodeField) ||
		!validOptionalName(mapping.SKUField) || !validName(mapping.TitleField) || !validOptionalName(mapping.BrandField) ||
		!validName(mapping.RevisionField) || !validName(mapping.ArchivedField) {
		return ErrInvalidConfiguration
	}
	return nil
}

func (mapping InventoryMapping) validate() error {
	if !resourcePattern.MatchString(mapping.Resource) || mapping.Function != "Balance" || !validName(mapping.ProductField) ||
		!validName(mapping.LocationField) || !validName(mapping.QuantityField) {
		return ErrInvalidConfiguration
	}
	return nil
}

func validHost(value string) bool {
	if value != strings.ToLower(value) || !hostPattern.MatchString(value) || !strings.Contains(value, ".") || strings.Contains(value, "..") || strings.HasPrefix(value, "-") || strings.HasSuffix(value, "-") || value == "localhost" {
		return false
	}
	allNumeric := true
	for _, r := range value {
		if (r < '0' || r > '9') && r != '.' {
			allNumeric = false
			break
		}
	}
	return !allNumeric
}

func validBasePath(value string) bool {
	if len(value) < len("/x/odata/standard.odata") || len(value) > 512 || !utf8.ValidString(value) || !strings.HasPrefix(value, "/") || !strings.HasSuffix(value, "/odata/standard.odata") {
		return false
	}
	if strings.Contains(value, "//") || strings.Contains(value, "..") || strings.ContainsAny(value, "?#\\\r\n\x00") {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validName(value string) bool         { return odataName.MatchString(value) }
func validOptionalName(value string) bool { return value == "" || validName(value) }

func configFingerprint(configuration Configuration, surface string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		surface, configuration.Host, configuration.BasePath,
		configuration.Catalog.Resource, configuration.Catalog.IDField, configuration.Catalog.CodeField,
		configuration.Catalog.SKUField, configuration.Catalog.TitleField, configuration.Catalog.BrandField,
		configuration.Catalog.RevisionField, configuration.Catalog.ArchivedField,
		configuration.Inventory.Resource, configuration.Inventory.Function, configuration.Inventory.ProductField,
		configuration.Inventory.LocationField, configuration.Inventory.QuantityField,
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}
