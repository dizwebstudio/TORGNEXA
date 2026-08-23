// Package trustcontrol contains provider-neutral validation and deterministic
// calculations for the tenant trust control plane. It never performs network
// calls or handles raw credentials.
package trustcontrol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalid = errors.New("trustcontrol: invalid")
	ErrDenied  = errors.New("trustcontrol: denied")
)

// Receipt is the durable identity of an idempotent operation.
type Receipt struct {
	Operation, Key, State, ResourceType, ResourceID string
	RequestSHA256                                   []byte
	Result                                          map[string]any
	CreatedAt                                       time.Time
	CompletedAt                                     *time.Time
}

// Evidence is minimized append-only security evidence.
type Evidence struct {
	ID, Type, ActorRef, ResourceType, ResourceID, CorrelationID, Decision string
	RequestSHA256                                                         []byte
	Summary                                                               map[string]any
	OccurredAt                                                            time.Time
}

// Policy is one immutable AI egress policy revision.
type Policy struct {
	Version             int64     `json:"version"`
	Enabled             bool      `json:"enabled"`
	AllowedDataClasses  []string  `json:"allowed_data_classes"`
	AllowedDestinations []string  `json:"allowed_providers"`
	AllowedModels       []string  `json:"allowed_models"`
	MaxPromptBytes      int       `json:"max_prompt_bytes"`
	MonthlyRequestLimit int       `json:"monthly_request_limit"`
	CreatedAt           time.Time `json:"created_at"`
}

// ValidatePolicy enforces bounded, exact allowlists.
func ValidatePolicy(policy Policy) error {
	if policy.Version < 1 || policy.MaxPromptBytes < 1 || policy.MaxPromptBytes > 32000 || policy.MonthlyRequestLimit < 1 || policy.MonthlyRequestLimit > 1_000_000 {
		return ErrInvalid
	}
	for values, maximum := range map[*[]string]int{&policy.AllowedDataClasses: 8, &policy.AllowedDestinations: 16, &policy.AllowedModels: 32} {
		if len(*values) < 1 || len(*values) > maximum {
			return ErrInvalid
		}
		seen := map[string]struct{}{}
		for _, value := range *values {
			if !validToken(value) {
				return ErrInvalid
			}
			if _, exists := seen[value]; exists {
				return ErrInvalid
			}
			seen[value] = struct{}{}
		}
		sort.Strings(*values)
	}
	return nil
}

// EgressRequest is the metadata needed to decide whether text may leave the
// tenant. Prompt bodies are intentionally absent from persisted decisions.
type EgressRequest struct {
	Destination, Model string
	DataClasses        []string
	PromptBytes        int
	MonthlyUsed        int
}

// AuthorizeEgress applies an exact allowlist and budget policy.
func AuthorizeEgress(policy Policy, request EgressRequest) error {
	if ValidatePolicy(policy) != nil || !policy.Enabled || request.PromptBytes < 1 || request.PromptBytes > policy.MaxPromptBytes || request.MonthlyUsed >= policy.MonthlyRequestLimit || !contains(policy.AllowedDestinations, request.Destination) || !contains(policy.AllowedModels, request.Model) || len(request.DataClasses) == 0 {
		return ErrDenied
	}
	for _, class := range request.DataClasses {
		if !validToken(class) || !contains(policy.AllowedDataClasses, class) {
			return ErrDenied
		}
	}
	return nil
}

var (
	emailPattern  = regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`)
	phonePattern  = regexp.MustCompile(`(?:\+?\d[\d ()-]{8,}\d)`)
	bearerPattern = regexp.MustCompile(`(?i)\b(?:bearer|api[_ -]?key|token|password)\s*[:=]\s*[^\s,;]{4,}`)
)

// RedactPrompt returns a bounded preview; callers must not persist it.
func RedactPrompt(value string, maxBytes int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || maxBytes < 1 || len(value) > maxBytes {
		return "", ErrInvalid
	}
	value = emailPattern.ReplaceAllString(value, "[REDACTED_EMAIL]")
	value = phonePattern.ReplaceAllString(value, "[REDACTED_PHONE]")
	value = bearerPattern.ReplaceAllString(value, "[REDACTED_SECRET]")
	return value, nil
}

// DigestJSON returns the deterministic SHA-256 digest of a JSON-compatible
// value. encoding/json sorts map keys, making the snapshot stable.
func DigestJSON(value any) ([]byte, []byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, nil, ErrInvalid
	}
	digest := sha256.Sum256(encoded)
	return encoded, digest[:], nil
}

// ScenarioInput is an immutable fixed-decimal profitability snapshot.
type ScenarioInput struct {
	Name                      string `json:"name"`
	SaleCurrency              string `json:"sale_currency"`
	CostCurrency              string `json:"cost_currency"`
	QuantityMilli             int64  `json:"quantity_milli"`
	SaleUnitPriceMinor        int64  `json:"sale_unit_price_minor"`
	CostUnitMinor             int64  `json:"cost_unit_minor"`
	LogisticsTotalCostMinor   int64  `json:"logistics_total_cost_minor"`
	AdvertisingTotalCostMinor int64  `json:"advertising_total_cost_minor"`
	MarketplaceFeeBasisPoints int64  `json:"marketplace_fee_basis_points"`
	CostToSaleFXRateMicros    int64  `json:"cost_to_sale_fx_rate_micros"`
}

// ScenarioResult is expressed entirely in sale-currency minor units.
type ScenarioResult struct {
	AlgorithmVersion        string `json:"algorithm_version"`
	Currency                string `json:"currency"`
	RevenueMinor            int64  `json:"revenue_minor"`
	MarketplaceFeeMinor     int64  `json:"marketplace_fee_minor"`
	ConvertedCostsMinor     int64  `json:"converted_costs_minor"`
	ContributionProfitMinor int64  `json:"contribution_profit_minor"`
	MarginBasisPoints       int64  `json:"margin_basis_points"`
}

const ProfitabilityAlgorithmVersion = "profitability-v1"

var (
	familyPattern     = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,62}$`)
	capabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{2,95}$`)
)

// ValidateReplayTarget validates provider-neutral connector identifiers.
func ValidateReplayTarget(family, capability string) error {
	if !familyPattern.MatchString(family) || !capabilityPattern.MatchString(capability) {
		return ErrInvalid
	}
	return nil
}

// CalculateScenario performs overflow-safe integer/fixed-decimal arithmetic.
func CalculateScenario(input ScenarioInput) (ScenarioResult, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.SaleCurrency = strings.ToUpper(strings.TrimSpace(input.SaleCurrency))
	input.CostCurrency = strings.ToUpper(strings.TrimSpace(input.CostCurrency))
	if input.Name == "" || len(input.Name) > 120 || !currency(input.SaleCurrency) || !currency(input.CostCurrency) || input.QuantityMilli < 1 || input.SaleUnitPriceMinor < 0 || input.CostUnitMinor < 0 || input.LogisticsTotalCostMinor < 0 || input.AdvertisingTotalCostMinor < 0 || input.MarketplaceFeeBasisPoints < 0 || input.MarketplaceFeeBasisPoints > 10000 || input.CostToSaleFXRateMicros < 1 {
		return ScenarioResult{}, ErrInvalid
	}
	mulDiv := func(a, b, divisor int64) (*big.Int, error) {
		value := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
		value.Quo(value, big.NewInt(divisor))
		if !value.IsInt64() {
			return nil, ErrInvalid
		}
		return value, nil
	}
	revenue, err := mulDiv(input.SaleUnitPriceMinor, input.QuantityMilli, 1000)
	if err != nil {
		return ScenarioResult{}, err
	}
	fee, err := mulDiv(revenue.Int64(), input.MarketplaceFeeBasisPoints, 10000)
	if err != nil {
		return ScenarioResult{}, err
	}
	unitCosts, err := mulDiv(input.CostUnitMinor, input.QuantityMilli, 1000)
	if err != nil {
		return ScenarioResult{}, err
	}
	costs := new(big.Int).Add(unitCosts, big.NewInt(input.LogisticsTotalCostMinor))
	costs.Add(costs, big.NewInt(input.AdvertisingTotalCostMinor))
	converted := new(big.Int).Mul(costs, big.NewInt(input.CostToSaleFXRateMicros))
	converted.Quo(converted, big.NewInt(1_000_000))
	profit := new(big.Int).Sub(revenue, fee)
	profit.Sub(profit, converted)
	if !converted.IsInt64() || !profit.IsInt64() {
		return ScenarioResult{}, ErrInvalid
	}
	margin := int64(0)
	if revenue.Sign() > 0 {
		marginValue := new(big.Int).Mul(profit, big.NewInt(10000))
		marginValue.Quo(marginValue, revenue)
		if !marginValue.IsInt64() {
			return ScenarioResult{}, ErrInvalid
		}
		margin = marginValue.Int64()
	}
	return ScenarioResult{AlgorithmVersion: ProfitabilityAlgorithmVersion, Currency: input.SaleCurrency, RevenueMinor: revenue.Int64(), MarketplaceFeeMinor: fee.Int64(), ConvertedCostsMinor: converted.Int64(), ContributionProfitMinor: profit.Int64(), MarginBasisPoints: margin}, nil
}

// ValidateSyntheticFixture rejects credential-shaped fields and requires an
// explicit synthetic marker. It returns a safe deterministic summary.
func ValidateSyntheticFixture(raw json.RawMessage) (map[string]any, []byte, error) {
	if len(raw) < 2 || len(raw) > 65536 {
		return nil, nil, ErrInvalid
	}
	var fixture any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(&fixture) != nil {
		return nil, nil, ErrInvalid
	}
	root, ok := fixture.(map[string]any)
	if !ok || root["_synthetic"] != true || unsafeFixtureValue(fixture, 0) {
		return nil, nil, ErrDenied
	}
	encoded, digest, err := DigestJSON(fixture)
	if err != nil || len(encoded) > 65536 {
		return nil, nil, ErrInvalid
	}
	return map[string]any{"valid": true, "fixture_sha256": hex.EncodeToString(digest), "bytes": len(encoded), "remote_calls": 0, "writes": 0}, encoded, nil
}

func unsafeFixtureValue(value any, depth int) bool {
	if depth > 16 {
		return true
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			lower := strings.ToLower(key)
			for _, forbidden := range []string{"authorization", "credential", "password", "secret", "token", "cookie", "private_key"} {
				if strings.Contains(lower, forbidden) {
					return true
				}
			}
			if unsafeFixtureValue(nested, depth+1) {
				return true
			}
		}
	case []any:
		if len(typed) > 1000 {
			return true
		}
		for _, nested := range typed {
			if unsafeFixtureValue(nested, depth+1) {
				return true
			}
		}
	}
	return false
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func validToken(value string) bool {
	if value == "" || len(value) > 120 || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._:/-", char)) {
			return false
		}
	}
	return true
}

func currency(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, char := range value {
		if char < 'A' || char > 'Z' {
			return false
		}
	}
	return true
}
