package magento

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
	ErrConfigurationMissing = errors.New("magento: configuration missing")
	ErrInvalidConfiguration = errors.New("magento: invalid configuration")
)

var hostPattern = regexp.MustCompile(`^(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$`)
var baseSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,64}$`)

// Configuration is admin-supplied non-secret runtime config: Magento is
// self-hosted (or self-managed on Adobe Commerce Cloud), so there is no
// fixed host, the same shape WooCommerce/Medusa/Shopware/OpenCart/PrestaShop
// already use.
type Configuration struct {
	StoreHost     string
	BasePath      string
	StoreCurrency string
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (configuration Configuration) Validate() error {
	if len(configuration.StoreHost) > 253 || configuration.StoreHost != strings.ToLower(strings.TrimSpace(configuration.StoreHost)) || !hostPattern.MatchString(configuration.StoreHost) || strings.HasSuffix(configuration.StoreHost, ".local") {
		return ErrInvalidConfiguration
	}
	if configuration.BasePath != "" {
		if !strings.HasPrefix(configuration.BasePath, "/") || strings.HasSuffix(configuration.BasePath, "/") || strings.ContainsAny(configuration.BasePath, "?#\\%") || len(configuration.BasePath) > 256 {
			return ErrInvalidConfiguration
		}
		for _, segment := range strings.Split(strings.TrimPrefix(configuration.BasePath, "/"), "/") {
			if segment == "." || segment == ".." || !baseSegmentPattern.MatchString(segment) {
				return ErrInvalidConfiguration
			}
		}
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

func (configuration Configuration) apiPath(suffix string) string {
	return configuration.BasePath + "/rest/V1" + suffix
}

func (configuration Configuration) fingerprint(surface string) string {
	digest := sha256.Sum256([]byte(surface + "\x00" + configuration.StoreHost + "\x00" + configuration.BasePath + "\x00" + configuration.StoreCurrency))
	return hex.EncodeToString(digest[:])
}
