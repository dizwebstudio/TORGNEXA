package bitrix24

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
	ErrConfigurationMissing = errors.New("bitrix24: configuration missing")
	ErrInvalidConfiguration = errors.New("bitrix24: invalid configuration")
)

var hostPattern = regexp.MustCompile(`^(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$`)

type Configuration struct{ PortalHost string }
type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (c Configuration) Validate() error {
	if len(c.PortalHost) > 253 || c.PortalHost != strings.ToLower(strings.TrimSpace(c.PortalHost)) || !hostPattern.MatchString(c.PortalHost) || strings.HasSuffix(c.PortalHost, ".local") {
		return ErrInvalidConfiguration
	}
	return nil
}
func (c Configuration) fingerprint(surface string) string {
	d := sha256.Sum256([]byte(surface + "\x00" + c.PortalHost))
	return hex.EncodeToString(d[:])
}
