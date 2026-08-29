package dolyami

import (
	"context"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// Configuration is non-secret account configuration. Долями supplies the
// actual API host per merchant contract, so the host is kept operator-owned.
type Configuration struct {
	ProbeURL string
}

// ConfigurationSource resolves host-owned configuration for one account.
type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

// Validate accepts only an absolute HTTPS endpoint; the host runtime applies
// the stricter public-address egress policy before dialing it.
func (configuration Configuration) Validate() error {
	if strings.TrimSpace(configuration.ProbeURL) == "" || !strings.HasPrefix(configuration.ProbeURL, "https://") || strings.ContainsAny(configuration.ProbeURL, "\r\n\t ") {
		return ErrInvalidConfiguration
	}
	return nil
}
