package sbp

import (
	"context"
	"errors"
	"regexp"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrConfigurationMissing = errors.New("sbp: configuration missing")
	ErrInvalidConfiguration = errors.New("sbp: invalid configuration")
)

var hostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)
var memberIDPattern = regexp.MustCompile(`^[0-9]{6,12}$`)

// Configuration is host-injected, non-secret account configuration. SBP has
// no single universal API host: every acquiring bank fronts the same NSPK
// C2B protocol shape on its own gateway (ADR-0071 "typed host-injected
// transport"), so the merchant's acquiring bank chooses GatewayHost and
// MemberID when they configure this connector account. The client
// certificate proving that bank relationship stays in the account secret,
// never here.
type Configuration struct {
	GatewayHost string
	MemberID    string
}

// ConfigurationSource is local to this provider so Connector SDK v1 Runtime
// remains frozen, matching the onec/woocommerce host-injection pattern.
type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (configuration Configuration) Validate() error {
	if !validGatewayHost(configuration.GatewayHost) || !memberIDPattern.MatchString(configuration.MemberID) {
		return ErrInvalidConfiguration
	}
	return nil
}

func validGatewayHost(value string) bool {
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
