package saleor

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
	ErrConfigurationMissing = errors.New("saleor: configuration missing")
	ErrInvalidConfiguration = errors.New("saleor: invalid configuration")
)

var hostPattern = regexp.MustCompile(`^(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$`)
var baseSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,64}$`)

// slugPattern matches Django's default SlugField charset (lowercase
// letters, digits, hyphens), which is what Saleor uses for both Channel.slug
// and Warehouse.slug.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Configuration is admin-supplied non-secret runtime config: Saleor is
// self-hosted (or self-managed on Saleor Cloud), so there is no fixed host,
// the same shape WooCommerce/Medusa/Shopware/Magento already use. Channel
// and Warehouse are Saleor's own slugs, resolved to the entity ids the
// GraphQL API actually requires (Query.channel(slug:)/warehouses(filter:
// {slugs:})) and cached in-memory per call, the same pattern Shopware
// already uses to resolve its currency UUID from an ISO code.
type Configuration struct {
	StoreHost string
	BasePath  string
	Channel   string
	Warehouse string
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
	if len(configuration.Channel) > 100 || !slugPattern.MatchString(configuration.Channel) {
		return ErrInvalidConfiguration
	}
	if len(configuration.Warehouse) > 100 || !slugPattern.MatchString(configuration.Warehouse) {
		return ErrInvalidConfiguration
	}
	return nil
}

func (configuration Configuration) graphqlPath() string {
	return configuration.BasePath + "/graphql/"
}

func (configuration Configuration) fingerprint(surface string) string {
	digest := sha256.Sum256([]byte(surface + "\x00" + configuration.StoreHost + "\x00" + configuration.BasePath + "\x00" + configuration.Channel + "\x00" + configuration.Warehouse))
	return hex.EncodeToString(digest[:])
}
