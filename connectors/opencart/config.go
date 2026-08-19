package opencart

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
	ErrConfigurationMissing = errors.New("opencart: configuration missing")
	ErrInvalidConfiguration = errors.New("opencart: invalid configuration")
)
var hostPattern = regexp.MustCompile(`^(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$`)
var baseSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,64}$`)

type Configuration struct{ StoreHost, BasePath, StoreCurrency string }
type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (c Configuration) Validate() error {
	if len(c.StoreHost) > 253 || c.StoreHost != strings.ToLower(strings.TrimSpace(c.StoreHost)) || !hostPattern.MatchString(c.StoreHost) || strings.HasSuffix(c.StoreHost, ".local") {
		return ErrInvalidConfiguration
	}
	if c.BasePath != "" {
		if !strings.HasPrefix(c.BasePath, "/") || strings.HasSuffix(c.BasePath, "/") || strings.ContainsAny(c.BasePath, "?#\\%") || len(c.BasePath) > 256 {
			return ErrInvalidConfiguration
		}
		for _, seg := range strings.Split(strings.TrimPrefix(c.BasePath, "/"), "/") {
			if seg == "." || seg == ".." || !baseSegmentPattern.MatchString(seg) {
				return ErrInvalidConfiguration
			}
		}
	}
	if len(c.StoreCurrency) != 3 || c.StoreCurrency != strings.ToUpper(c.StoreCurrency) {
		return ErrInvalidConfiguration
	}
	for _, r := range c.StoreCurrency {
		if r < 'A' || r > 'Z' {
			return ErrInvalidConfiguration
		}
	}
	return nil
}
func (c Configuration) apiPath() string { return c.BasePath + "/index.php" }
func (c Configuration) fingerprint(surface string) string {
	d := sha256.Sum256([]byte(surface + "\x00" + c.StoreHost + "\x00" + c.BasePath + "\x00" + c.StoreCurrency))
	return hex.EncodeToString(d[:])
}
