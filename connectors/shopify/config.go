package shopify

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
	ErrConfigurationMissing = errors.New("shopify: configuration missing")
	ErrInvalidConfiguration = errors.New("shopify: invalid configuration")
)

// shopDomainPattern matches Shopify's own documented shop-domain shape:
// https://shopify.dev/docs/apps/build/authentication-authorization/access-tokens/authorization-code-grant
var shopDomainPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.myshopify\.com$`)

// StoreCurrency is admin-supplied non-secret runtime config (matching
// woocommerce/opencart/prestashop's identical pattern) rather than resolved
// live from GET /shop.json on every price read: Shopify's REST API has no
// per-product currency field, only a shop-wide one, and it changes rarely.
type Configuration struct {
	ShopDomain    string
	StoreCurrency string
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (configuration Configuration) Validate() error {
	if len(configuration.ShopDomain) > 253 || configuration.ShopDomain != strings.ToLower(strings.TrimSpace(configuration.ShopDomain)) || !shopDomainPattern.MatchString(configuration.ShopDomain) {
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

func (configuration Configuration) fingerprint(surface string) string {
	digest := sha256.Sum256([]byte(surface + "\x00" + configuration.ShopDomain))
	return hex.EncodeToString(digest[:])
}

func (configuration Configuration) apiPath(suffix string) string {
	return "/admin/api/" + apiVersion + suffix
}
