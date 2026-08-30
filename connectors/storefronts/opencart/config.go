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

type Configuration struct {
	StoreHost          string
	BasePath           string
	StoreCurrency      string
	OrderStatusMapping map[string]string
}
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
	d := sha256.Sum256([]byte(surface + "\x00" + c.StoreHost + "\x00" + c.BasePath + "\x00" + c.StoreCurrency + "\x00" + c.orderStatusFingerprint()))
	return hex.EncodeToString(d[:])
}

var canonicalOrderStatuses = [...]string{"pending", "confirmed", "processing", "fulfilled", "cancelled"}

func (c Configuration) orderStatusFingerprint() string {
	var builder strings.Builder
	for _, status := range canonicalOrderStatuses {
		builder.WriteString(status)
		builder.WriteByte('=')
		builder.WriteString(c.OrderStatusMapping[status])
		builder.WriteByte('\x00')
	}
	return builder.String()
}

// OrderStatuses returns the tenant-provided canonical-to-OpenCart state map.
// OpenCart order status identifiers are installation-specific, so status
// writes are admitted only when every canonical lifecycle state has an
// explicit unique positive numeric ID.
func (c Configuration) OrderStatuses() (map[string]string, error) {
	if len(c.OrderStatusMapping) != len(canonicalOrderStatuses) {
		return nil, ErrInvalidConfiguration
	}
	seen := make(map[string]struct{}, len(c.OrderStatusMapping))
	result := make(map[string]string, len(c.OrderStatusMapping))
	for _, canonical := range canonicalOrderStatuses {
		remote, ok := c.OrderStatusMapping[canonical]
		if !ok || !validRemoteStatus(remote) {
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

func validRemoteStatus(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 19 {
		return false
	}
	for _, symbol := range value {
		if symbol < '0' || symbol > '9' {
			return false
		}
	}
	return value != "0"
}
