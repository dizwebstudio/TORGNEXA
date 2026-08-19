package prestashop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrConfigurationMissing = errors.New("prestashop: configuration missing")
	ErrInvalidConfiguration = errors.New("prestashop: invalid configuration")
)

var hostPattern = regexp.MustCompile(`^(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$`)
var baseSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,64}$`)

type Configuration struct {
	StoreHost     string
	BasePath      string
	StoreCurrency string
	LanguageID    int64
	ShopID        int64
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
		for _, segment := range strings.Split(strings.TrimPrefix(c.BasePath, "/"), "/") {
			if segment == "." || segment == ".." || !baseSegmentPattern.MatchString(segment) {
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
	if c.LanguageID < 1 || c.ShopID < 0 {
		return ErrInvalidConfiguration
	}
	return nil
}

func (c Configuration) apiPath(suffix string) string { return c.BasePath + "/api" + suffix }
func (c Configuration) commonQuery() []QueryParam {
	q := []QueryParam{{Name: "output_format", Value: "JSON"}, {Name: "language", Value: strconv.FormatInt(c.LanguageID, 10)}}
	if c.ShopID > 0 {
		q = append(q, QueryParam{Name: "id_shop", Value: strconv.FormatInt(c.ShopID, 10)})
	}
	return q
}
func (c Configuration) fingerprint(surface string) string {
	d := sha256.Sum256([]byte(surface + "\x00" + c.StoreHost + "\x00" + c.BasePath + "\x00" + c.StoreCurrency + "\x00" + strconv.FormatInt(c.LanguageID, 10) + "\x00" + strconv.FormatInt(c.ShopID, 10)))
	return hex.EncodeToString(d[:])
}
