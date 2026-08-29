package bitrix

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrConfigurationMissing = errors.New("bitrix: configuration missing")
	ErrInvalidConfiguration = errors.New("bitrix: invalid configuration")
)

var bitrixHostPattern = regexp.MustCompile(`^(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$`)
var bitrixBaseSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,64}$`)

// Configuration is non-secret site configuration. The catalog iblock ID is
// required because 1C-Bitrix REST catalog.product.list requires iblockId in
// every request; webhook credentials remain in SecretProvider.
type Configuration struct {
	StoreHost          string
	BasePath           string
	CatalogIblockID    int64
	StoreCurrency      string
	PriceTypeID        int64
	OrderStatusMapping map[string]string
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (configuration Configuration) Validate() error {
	if len(configuration.StoreHost) > 253 || configuration.StoreHost != strings.ToLower(strings.TrimSpace(configuration.StoreHost)) || !bitrixHostPattern.MatchString(configuration.StoreHost) || strings.HasSuffix(configuration.StoreHost, ".local") {
		return ErrInvalidConfiguration
	}
	if configuration.BasePath != "" {
		if !strings.HasPrefix(configuration.BasePath, "/") || strings.HasSuffix(configuration.BasePath, "/") || strings.ContainsAny(configuration.BasePath, "?#\\%") || len(configuration.BasePath) > 256 {
			return ErrInvalidConfiguration
		}
		for _, segment := range strings.Split(strings.TrimPrefix(configuration.BasePath, "/"), "/") {
			if segment == "." || segment == ".." || !bitrixBaseSegmentPattern.MatchString(segment) {
				return ErrInvalidConfiguration
			}
		}
	}
	if configuration.CatalogIblockID < 1 || configuration.CatalogIblockID > 2_147_483_647 {
		return ErrInvalidConfiguration
	}
	if len(configuration.StoreCurrency) != 3 || configuration.StoreCurrency != strings.ToUpper(configuration.StoreCurrency) {
		return ErrInvalidConfiguration
	}
	for _, r := range configuration.StoreCurrency {
		if r < 'A' || r > 'Z' {
			return ErrInvalidConfiguration
		}
	}
	return nil
}

func (configuration Configuration) apiBasePath() string { return configuration.BasePath }

func (configuration Configuration) fingerprint(surface string) string {
	digest := sha256.Sum256([]byte(surface + "\x00" + configuration.StoreHost + "\x00" + configuration.BasePath + "\x00" + intString(configuration.CatalogIblockID) + "\x00" + configuration.StoreCurrency + "\x00" + intString(configuration.PriceTypeID) + "\x00" + configuration.orderStatusFingerprint()))
	return hex.EncodeToString(digest[:])
}

var canonicalOrderStatuses = [...]string{"pending", "confirmed", "processing", "fulfilled", "cancelled"}

func (configuration Configuration) orderStatusFingerprint() string {
	var builder strings.Builder
	for _, status := range canonicalOrderStatuses {
		builder.WriteString(status)
		builder.WriteByte('=')
		builder.WriteString(configuration.OrderStatusMapping[status])
		builder.WriteByte('\x00')
	}
	return builder.String()
}

// OrderStatuses returns the configured canonical-to-Bitrix status mapping.
// An order route must configure every canonical lifecycle state explicitly;
// Bitrix installations may use different status identifiers.
func (configuration Configuration) OrderStatuses() (map[string]string, error) {
	if len(configuration.OrderStatusMapping) == 0 {
		return nil, ErrInvalidConfiguration
	}
	if len(configuration.OrderStatusMapping) != len(canonicalOrderStatuses) {
		return nil, ErrInvalidConfiguration
	}
	seen := make(map[string]struct{}, len(configuration.OrderStatusMapping))
	result := make(map[string]string, len(configuration.OrderStatusMapping))
	for _, canonical := range canonicalOrderStatuses {
		remote, ok := configuration.OrderStatusMapping[canonical]
		if !ok || !validRemoteText(remote, 64) {
			return nil, ErrInvalidConfiguration
		}
		if _, duplicate := seen[remote]; duplicate {
			return nil, ErrInvalidConfiguration
		}
		seen[remote] = struct{}{}
		result[canonical] = remote
	}
	return result, nil
}

func reverseOrderStatuses(statuses map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(statuses))
	for canonical, remote := range statuses {
		if _, duplicate := result[remote]; duplicate {
			return nil, ErrInvalidConfiguration
		}
		result[remote] = canonical
	}
	return result, nil
}

func intString(value int64) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	digits := make([]byte, 0, 20)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}
