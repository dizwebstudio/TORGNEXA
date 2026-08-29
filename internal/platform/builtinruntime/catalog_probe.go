package builtinruntime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// catalogProbeConnector is the bounded account/health bridge used by
// category-specific providers that do not yet have a domain workflow in the
// application. It is deliberately capability-free: a successful probe only
// proves that the supplied credentials reach the provider, never that a
// product, document or publication route is executable.
type catalogProbeConnector struct {
	h      *httpTransport
	id     string
	config ConfigLoader
}

func (c catalogProbeConnector) Manifest() sdk.Manifest {
	manifest, _ := sdk.CatalogManifest(c.id)
	return manifest
}

func (c catalogProbeConnector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	checkedAt := time.Now().UTC()
	manifest := c.Manifest()
	if c.h == nil || ctx == nil || manifest.Validate() != nil || account.Validate() != nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, manifest) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	var result sdk.Health
	secretErr := runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		if len(secret) == 0 {
			result = sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "credentials_missing", CheckedAt: checkedAt}
			return nil
		}
		probe, err := c.probe(manifest, ctx, account.ID, secret)
		if err != nil {
			result = sdk.Health{Status: sdk.HealthDegraded, ReasonCode: probeReason(err), CheckedAt: checkedAt}
			return nil
		}
		result = probe
		return nil
	})
	if secretErr != nil {
		return sdk.Health{}, secretErr
	}
	return result, nil
}

type probeError string

func (e probeError) Error() string { return string(e) }

const (
	errProbeNotConfigured = probeError("probe not configured")
	errProbeInvalid       = probeError("probe invalid")
	errProbeUnavailable   = probeError("probe unavailable")
	errProbeRejected      = probeError("probe rejected")
)

func probeReason(err error) string {
	switch {
	case errors.Is(err, errProbeNotConfigured):
		return "provider_probe_not_configured"
	case errors.Is(err, errProbeInvalid):
		return "provider_probe_invalid"
	case errors.Is(err, errProbeRejected):
		return "provider_auth_rejected"
	default:
		return "provider_unavailable"
	}
}

func (c catalogProbeConnector) probe(manifest sdk.Manifest, ctx context.Context, accountID string, secret []byte) (sdk.Health, error) {
	probe := manifest.ConnectionTest
	var rawConfig json.RawMessage
	if probe == nil {
		if c.config == nil {
			return sdk.Health{}, errProbeNotConfigured
		}
		var err error
		rawConfig, err = c.config(ctx, accountID)
		if err != nil {
			return sdk.Health{}, errProbeNotConfigured
		}
		var value struct {
			ProbeURL string `json:"probe_url"`
		}
		if decodeStrict(rawConfig, &value) != nil || value.ProbeURL == "" {
			return sdk.Health{}, errProbeNotConfigured
		}
		probe = &sdk.ConnectionTest{URL: value.ProbeURL, Method: http.MethodGet, Headers: map[string]string{"Authorization": "Bearer ${secret}"}}
	}
	if probe.Validate() != nil {
		return sdk.Health{}, errProbeInvalid
	}
	u, err := url.Parse(probe.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || !validHost(strings.ToLower(u.Hostname())) || (u.Port() != "" && u.Port() != "443") {
		return sdk.Health{}, errProbeInvalid
	}
	values := probeValues(secret)
	headers := make(http.Header, len(probe.Headers))
	for name, value := range probe.Headers {
		expanded, ok := expandProbeValue(value, values)
		if !ok {
			return sdk.Health{}, errProbeInvalid
		}
		headers.Set(name, expanded)
	}
	body, ok := expandProbeValue(probe.Body, values)
	if !ok {
		return sdk.Health{}, errProbeInvalid
	}
	probePath := u.EscapedPath()
	if probePath == "" {
		probePath = "/"
	}
	status, _, _, _, _, callErr := c.h.do(ctx, probe.Method, strings.ToLower(u.Hostname()), probePath, u.Query(), []byte(body), headers, nil, nil)
	if callErr != nil {
		return sdk.Health{}, errProbeUnavailable
	}
	if !probeStatusOK(status, probe.SuccessStatuses) {
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return sdk.Health{}, errProbeRejected
		}
		return sdk.Health{}, errProbeUnavailable
	}
	return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: time.Now().UTC()}, nil
}

func probeStatusOK(status int, accepted []int) bool {
	if len(accepted) == 0 {
		return status >= 200 && status < 300
	}
	for _, candidate := range accepted {
		if candidate == status {
			return true
		}
	}
	return false
}

func probeValues(secret []byte) map[string]string {
	values := map[string]string{
		"secret":        string(secret),
		"access_token":  string(secret),
		"authorization": string(secret),
		"token":         string(secret),
		"basic":         base64.StdEncoding.EncodeToString(secret),
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(secret, &object) != nil {
		lines := bytes.Split(secret, []byte{'\n'})
		if len(lines) > 0 {
			values["line1"] = string(bytes.TrimSpace(lines[0]))
			values["authorization"] = values["line1"]
		}
		if len(lines) > 1 {
			values["line2"] = string(bytes.TrimSpace(lines[1]))
			values["session_id"] = values["line2"]
		}
		return values
	}
	for key, raw := range object {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			values[key] = text
			continue
		}
		var number json.Number
		if json.Unmarshal(raw, &number) == nil {
			values[key] = number.String()
		}
	}
	return values
}

func expandProbeValue(value string, values map[string]string) (string, bool) {
	for key, replacement := range values {
		value = strings.ReplaceAll(value, "${"+key+"}", replacement)
	}
	return value, !strings.Contains(value, "${")
}

var _ sdk.Connector = catalogProbeConnector{}
