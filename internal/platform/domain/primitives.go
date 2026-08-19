package domain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"
)

const (
	// MaxDecimalScale bounds persisted fixed-point values. Nine fractional digits
	// cover inventory/measurement use cases without introducing binary floats.
	MaxDecimalScale = 9
)

var (
	currencyPattern        = regexp.MustCompile(`^[A-Z]{3}$`)
	countryPattern         = regexp.MustCompile(`^[A-Z]{2}$`)
	unitPattern            = regexp.MustCompile(`^[A-Z][A-Z0-9._-]{0,15}$`)
	codePattern            = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,62}$`)
	taxJurisdictionPattern = regexp.MustCompile(`^[A-Z]{2}(?:-[A-Z0-9]{1,8})*$`)
)

func decodeStrictJSON(data []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("JSON contains trailing value")
		}
		return err
	}
	return nil
}

// Currency is a provider-neutral, ISO-4217-shaped three-letter currency code.
// The primitive validates syntax rather than shipping a stale currency registry.
type Currency string

func NewCurrency(code string) (Currency, error) {
	if !currencyPattern.MatchString(code) {
		return "", fmt.Errorf("currency must be exactly three uppercase ASCII letters")
	}
	return Currency(code), nil
}

func (c Currency) String() string { return string(c) }

func (c Currency) Validate() error {
	_, err := NewCurrency(string(c))
	return err
}

func (c Currency) MarshalText() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return []byte(c), nil
}

func (c *Currency) UnmarshalText(data []byte) error {
	value, err := NewCurrency(string(data))
	if err != nil {
		return err
	}
	*c = value
	return nil
}

// Money is an exact amount represented only in integer minor units. It never
// accepts or exposes binary floating-point arithmetic.
type Money struct {
	minorUnits int64
	currency   Currency
}

func NewMoney(minorUnits int64, currency Currency) (Money, error) {
	if err := currency.Validate(); err != nil {
		return Money{}, fmt.Errorf("money currency: %w", err)
	}
	return Money{minorUnits: minorUnits, currency: currency}, nil
}

func (m Money) MinorUnits() int64      { return m.minorUnits }
func (m Money) Currency() Currency     { return m.currency }
func (m Money) IsZero() bool           { return m.minorUnits == 0 }
func (m Money) Validate() error        { _, err := NewMoney(m.minorUnits, m.currency); return err }
func (m Money) Equal(other Money) bool { return m == other }

func (m Money) Add(other Money) (Money, error) {
	if err := m.Validate(); err != nil {
		return Money{}, err
	}
	if err := other.Validate(); err != nil {
		return Money{}, err
	}
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("currency mismatch: %s != %s", m.currency, other.currency)
	}
	if (other.minorUnits > 0 && m.minorUnits > math.MaxInt64-other.minorUnits) ||
		(other.minorUnits < 0 && m.minorUnits < math.MinInt64-other.minorUnits) {
		return Money{}, errors.New("money addition overflow")
	}
	return NewMoney(m.minorUnits+other.minorUnits, m.currency)
}

func (m Money) Sub(other Money) (Money, error) {
	if other.minorUnits == math.MinInt64 {
		return Money{}, errors.New("money subtraction overflow")
	}
	negated, err := NewMoney(-other.minorUnits, other.currency)
	if err != nil {
		return Money{}, err
	}
	return m.Add(negated)
}

func (m Money) MarshalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		MinorUnits int64    `json:"minor_units"`
		Currency   Currency `json:"currency"`
	}{m.minorUnits, m.currency})
}

func (m *Money) UnmarshalJSON(data []byte) error {
	var wire struct {
		MinorUnits *int64    `json:"minor_units"`
		Currency   *Currency `json:"currency"`
	}
	if err := decodeStrictJSON(data, &wire); err != nil {
		return err
	}
	if wire.MinorUnits == nil || wire.Currency == nil {
		return errors.New("money requires minor_units and currency")
	}
	value, err := NewMoney(*wire.MinorUnits, *wire.Currency)
	if err != nil {
		return err
	}
	*m = value
	return nil
}

// Decimal is a signed fixed-point decimal backed by an int64 coefficient and
// an explicit scale. JSON encoding is always a string to prevent float drift.
type Decimal struct {
	coefficient int64
	scale       uint8
}

func NewDecimal(coefficient int64, scale uint8) (Decimal, error) {
	if scale > MaxDecimalScale {
		return Decimal{}, fmt.Errorf("decimal scale %d exceeds maximum %d", scale, MaxDecimalScale)
	}
	return normalizeDecimal(Decimal{coefficient: coefficient, scale: scale}), nil
}

func ParseDecimal(input string) (Decimal, error) {
	if input == "" || strings.TrimSpace(input) != input || strings.HasPrefix(input, "+") {
		return Decimal{}, errors.New("decimal must use canonical signed decimal syntax")
	}
	negative := false
	digits := input
	if strings.HasPrefix(digits, "-") {
		negative = true
		digits = digits[1:]
	}
	if digits == "" {
		return Decimal{}, errors.New("decimal has no digits")
	}
	parts := strings.Split(digits, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && parts[1] == "") {
		return Decimal{}, errors.New("invalid decimal syntax")
	}
	for _, part := range parts {
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return Decimal{}, errors.New("decimal contains non-digit characters")
			}
		}
	}
	if len(parts[0]) > 1 && parts[0][0] == '0' {
		return Decimal{}, errors.New("decimal integer part has leading zeros")
	}
	scale := 0
	allDigits := parts[0]
	if len(parts) == 2 {
		scale = len(parts[1])
		if scale > MaxDecimalScale {
			return Decimal{}, fmt.Errorf("decimal scale %d exceeds maximum %d", scale, MaxDecimalScale)
		}
		allDigits += parts[1]
	}
	if negative && strings.Trim(allDigits, "0") == "" {
		return Decimal{}, errors.New("negative zero is not canonical")
	}
	value := new(big.Int)
	if _, ok := value.SetString(allDigits, 10); !ok {
		return Decimal{}, errors.New("invalid decimal coefficient")
	}
	if negative {
		value.Neg(value)
	}
	if !value.IsInt64() {
		return Decimal{}, errors.New("decimal coefficient exceeds int64")
	}
	return NewDecimal(value.Int64(), uint8(scale))
}

func normalizeDecimal(value Decimal) Decimal {
	for value.scale > 0 && value.coefficient%10 == 0 {
		value.coefficient /= 10
		value.scale--
	}
	return value
}

func (d Decimal) Coefficient() int64 { return d.coefficient }
func (d Decimal) Scale() uint8       { return d.scale }
func (d Decimal) IsZero() bool       { return d.coefficient == 0 }

func (d Decimal) Validate() error {
	if d.scale > MaxDecimalScale {
		return fmt.Errorf("decimal scale %d exceeds maximum %d", d.scale, MaxDecimalScale)
	}
	if normalizeDecimal(d) != d {
		return errors.New("decimal is not normalized")
	}
	return nil
}

func (d Decimal) String() string {
	if d.coefficient == 0 {
		return "0"
	}
	negative := d.coefficient < 0
	var magnitude uint64
	if negative {
		magnitude = uint64(-(d.coefficient + 1)) + 1
	} else {
		magnitude = uint64(d.coefficient)
	}
	digits := strconv.FormatUint(magnitude, 10)
	if d.scale > 0 {
		for len(digits) <= int(d.scale) {
			digits = "0" + digits
		}
		cut := len(digits) - int(d.scale)
		digits = digits[:cut] + "." + digits[cut:]
	}
	if negative {
		return "-" + digits
	}
	return digits
}

func (d Decimal) MarshalJSON() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(d.String())
}

func (d *Decimal) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return errors.New("decimal JSON value must be a string")
	}
	value, err := ParseDecimal(raw)
	if err != nil {
		return err
	}
	*d = value
	return nil
}

func alignDecimals(left, right Decimal) (int64, int64, uint8, error) {
	if err := left.Validate(); err != nil {
		return 0, 0, 0, err
	}
	if err := right.Validate(); err != nil {
		return 0, 0, 0, err
	}
	target := left.scale
	if right.scale > target {
		target = right.scale
	}
	scaleValue := func(value Decimal) (int64, error) {
		coefficient := big.NewInt(value.coefficient)
		factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(target-value.scale)), nil)
		coefficient.Mul(coefficient, factor)
		if !coefficient.IsInt64() {
			return 0, errors.New("decimal alignment overflow")
		}
		return coefficient.Int64(), nil
	}
	l, err := scaleValue(left)
	if err != nil {
		return 0, 0, 0, err
	}
	r, err := scaleValue(right)
	if err != nil {
		return 0, 0, 0, err
	}
	return l, r, target, nil
}

func (d Decimal) Add(other Decimal) (Decimal, error) {
	left, right, scale, err := alignDecimals(d, other)
	if err != nil {
		return Decimal{}, err
	}
	sum := new(big.Int).Add(big.NewInt(left), big.NewInt(right))
	if !sum.IsInt64() {
		return Decimal{}, errors.New("decimal addition overflow")
	}
	return NewDecimal(sum.Int64(), scale)
}

func (d Decimal) Sub(other Decimal) (Decimal, error) {
	left, right, scale, err := alignDecimals(d, other)
	if err != nil {
		return Decimal{}, err
	}
	difference := new(big.Int).Sub(big.NewInt(left), big.NewInt(right))
	if !difference.IsInt64() {
		return Decimal{}, errors.New("decimal subtraction overflow")
	}
	return NewDecimal(difference.Int64(), scale)
}

func (d Decimal) Cmp(other Decimal) (int, error) {
	left, right, _, err := alignDecimals(d, other)
	if err != nil {
		return 0, err
	}
	switch {
	case left < right:
		return -1, nil
	case left > right:
		return 1, nil
	default:
		return 0, nil
	}
}

// UnitCode is a provider-neutral measurement unit code. Provider-specific unit
// mappings belong in adapters rather than this shared primitive.
type UnitCode string

func NewUnitCode(code string) (UnitCode, error) {
	if !unitPattern.MatchString(code) {
		return "", errors.New("unit code must be 1-16 uppercase ASCII code characters")
	}
	return UnitCode(code), nil
}

func (u UnitCode) Validate() error { _, err := NewUnitCode(string(u)); return err }
func (u UnitCode) String() string  { return string(u) }

func (u UnitCode) MarshalText() ([]byte, error) {
	if err := u.Validate(); err != nil {
		return nil, err
	}
	return []byte(u), nil
}
func (u *UnitCode) UnmarshalText(data []byte) error {
	value, err := NewUnitCode(string(data))
	if err != nil {
		return err
	}
	*u = value
	return nil
}

// Quantity is an exact decimal amount coupled to an explicit unit.
type Quantity struct {
	Value Decimal  `json:"value"`
	Unit  UnitCode `json:"unit"`
}

func NewQuantity(value Decimal, unit UnitCode) (Quantity, error) {
	if err := value.Validate(); err != nil {
		return Quantity{}, fmt.Errorf("quantity value: %w", err)
	}
	if err := unit.Validate(); err != nil {
		return Quantity{}, fmt.Errorf("quantity unit: %w", err)
	}
	return Quantity{Value: value, Unit: unit}, nil
}

func (q Quantity) Validate() error { _, err := NewQuantity(q.Value, q.Unit); return err }

func (q *Quantity) UnmarshalJSON(data []byte) error {
	var wire struct {
		Value *Decimal  `json:"value"`
		Unit  *UnitCode `json:"unit"`
	}
	if err := decodeStrictJSON(data, &wire); err != nil {
		return err
	}
	if wire.Value == nil || wire.Unit == nil {
		return errors.New("quantity requires value and unit")
	}
	value, err := NewQuantity(*wire.Value, *wire.Unit)
	if err != nil {
		return err
	}
	*q = value
	return nil
}

// CountryCode is an ISO-3166-alpha2-shaped country identifier. Syntax is
// validated centrally; authoritative registries remain an adapter concern.
type CountryCode string

func NewCountryCode(code string) (CountryCode, error) {
	if !countryPattern.MatchString(code) {
		return "", errors.New("country code must be exactly two uppercase ASCII letters")
	}
	return CountryCode(code), nil
}
func (c CountryCode) Validate() error { _, err := NewCountryCode(string(c)); return err }
func (c CountryCode) String() string  { return string(c) }
func (c CountryCode) MarshalText() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return []byte(c), nil
}
func (c *CountryCode) UnmarshalText(data []byte) error {
	value, err := NewCountryCode(string(data))
	if err != nil {
		return err
	}
	*c = value
	return nil
}

// Locale is the canonical language[-Script][-REGION] subset of BCP 47 used at
// presentation boundaries. Domain values never contain localized strings.
type Locale string

func NewLocale(input string) (Locale, error) {
	parts := strings.Split(input, "-")
	if len(parts) < 1 || len(parts) > 3 {
		return "", errors.New("locale must be language[-Script][-REGION]")
	}
	language := parts[0]
	if len(language) < 2 || len(language) > 3 || !asciiLetters(language) {
		return "", errors.New("locale language must contain 2-3 ASCII letters")
	}
	canonical := []string{strings.ToLower(language)}
	index := 1
	if index < len(parts) && len(parts[index]) == 4 && asciiLetters(parts[index]) {
		script := strings.ToLower(parts[index])
		canonical = append(canonical, strings.ToUpper(script[:1])+script[1:])
		index++
	}
	if index < len(parts) {
		region := parts[index]
		if len(region) == 2 && asciiLetters(region) {
			canonical = append(canonical, strings.ToUpper(region))
		} else if len(region) == 3 && asciiDigits(region) {
			canonical = append(canonical, region)
		} else {
			return "", errors.New("locale region must contain two ASCII letters or three digits")
		}
		index++
	}
	if index != len(parts) {
		return "", errors.New("unsupported locale extension")
	}
	value := strings.Join(canonical, "-")
	if value != input {
		return "", fmt.Errorf("locale must be canonical: %s", value)
	}
	return Locale(value), nil
}

func (l Locale) Validate() error { _, err := NewLocale(string(l)); return err }
func (l Locale) String() string  { return string(l) }
func (l Locale) MarshalText() ([]byte, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return []byte(l), nil
}
func (l *Locale) UnmarshalText(data []byte) error {
	value, err := NewLocale(string(data))
	if err != nil {
		return err
	}
	*l = value
	return nil
}

func asciiLetters(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if !((value[i] >= 'A' && value[i] <= 'Z') || (value[i] >= 'a' && value[i] <= 'z')) {
			return false
		}
	}
	return true
}
func asciiDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

// Address stores non-localized address components. Presentation formatting is
// deliberately delegated to AddressFormatter.
type Address struct {
	Country    CountryCode `json:"country"`
	Region     string      `json:"region,omitempty"`
	City       string      `json:"city,omitempty"`
	PostalCode string      `json:"postal_code,omitempty"`
	Lines      []string    `json:"lines"`
}

func (a Address) Validate() error {
	if err := a.Country.Validate(); err != nil {
		return fmt.Errorf("address country: %w", err)
	}
	if len(a.Lines) == 0 || len(a.Lines) > 4 {
		return errors.New("address must contain 1-4 lines")
	}
	for _, field := range append([]string{a.Region, a.City, a.PostalCode}, a.Lines...) {
		if !utf8.ValidString(field) || strings.TrimSpace(field) != field || utf8.RuneCountInString(field) > 160 {
			return errors.New("address component is non-canonical, invalid UTF-8, or too long")
		}
	}
	for _, line := range a.Lines {
		if line == "" {
			return errors.New("address lines cannot be empty")
		}
	}
	return nil
}

func (a *Address) UnmarshalJSON(data []byte) error {
	type wireAddress Address
	var wire wireAddress
	if err := decodeStrictJSON(data, &wire); err != nil {
		return err
	}
	value := Address(wire)
	if err := value.Validate(); err != nil {
		return err
	}
	*a = value
	return nil
}

// AddressFormatter formats an Address for a locale without changing domain data.
type AddressFormatter interface {
	FormatAddress(context.Context, Address, Locale) (string, error)
}

// CurrencyMetadataProvider supplies authoritative currency metadata without
// baking a potentially stale ISO registry into the domain primitive.
type CurrencyMetadataProvider interface {
	ValidateCurrency(context.Context, Currency) error
	MinorUnitScale(context.Context, Currency) (uint8, error)
}

// LocalizationPort owns locale-sensitive presentation. Domain primitives remain
// language-neutral and exact.
type LocalizationPort interface {
	FormatMoney(context.Context, Money, Locale) (string, error)
	FormatQuantity(context.Context, Quantity, Locale) (string, error)
	FormatInstant(context.Context, UTCInstant, TimeZone, Locale) (string, error)
}

// UTCInstant is a persisted instant normalized to UTC.
type UTCInstant struct{ value time.Time }

func NewUTCInstant(value time.Time) (UTCInstant, error) {
	if value.IsZero() {
		return UTCInstant{}, errors.New("UTC instant cannot be zero")
	}
	_, offset := value.Zone()
	if offset != 0 {
		return UTCInstant{}, errors.New("persisted instant must have UTC offset 00:00")
	}
	return UTCInstant{value: value.UTC()}, nil
}

func ParseUTCInstant(input string) (UTCInstant, error) {
	value, err := time.Parse(time.RFC3339Nano, input)
	if err != nil {
		return UTCInstant{}, fmt.Errorf("parse UTC instant: %w", err)
	}
	instant, err := NewUTCInstant(value)
	if err != nil {
		return UTCInstant{}, err
	}
	return instant, nil
}
func (u UTCInstant) Time() time.Time { return u.value }
func (u UTCInstant) String() string  { return u.value.UTC().Format(time.RFC3339Nano) }
func (u UTCInstant) Validate() error { _, err := NewUTCInstant(u.value); return err }
func (u UTCInstant) MarshalJSON() ([]byte, error) {
	if err := u.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(u.String())
}
func (u *UTCInstant) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return errors.New("UTC instant JSON value must be a string")
	}
	value, err := ParseUTCInstant(raw)
	if err != nil {
		return err
	}
	*u = value
	return nil
}

// TimeZone identifies a named IANA zone. Fixed offsets and local-machine zones
// are rejected so DST rules remain explicit and reproducible at the edge.
type TimeZone string

func NewTimeZone(name string) (TimeZone, error) {
	if name == "UTC" {
		return TimeZone(name), nil
	}
	if name == "" || name == "Local" || !strings.Contains(name, "/") || strings.ContainsAny(name, " \\") {
		return "", errors.New("timezone must be UTC or a named IANA area/location")
	}
	if _, err := time.LoadLocation(name); err != nil {
		return "", fmt.Errorf("load timezone %q: %w", name, err)
	}
	return TimeZone(name), nil
}
func (z TimeZone) String() string { return string(z) }
func (z TimeZone) Location() (*time.Location, error) {
	if _, err := NewTimeZone(string(z)); err != nil {
		return nil, err
	}
	return time.LoadLocation(string(z))
}
func (z TimeZone) Validate() error { _, err := z.Location(); return err }
func (z TimeZone) MarshalText() ([]byte, error) {
	if err := z.Validate(); err != nil {
		return nil, err
	}
	return []byte(z), nil
}
func (z *TimeZone) UnmarshalText(data []byte) error {
	value, err := NewTimeZone(string(data))
	if err != nil {
		return err
	}
	*z = value
	return nil
}

// LocalDateTime is a timezone-free wall-clock value used only at scheduling /
// presentation edges. It must be resolved explicitly before persistence.
type LocalDateTime struct {
	Year       int `json:"year"`
	Month      int `json:"month"`
	Day        int `json:"day"`
	Hour       int `json:"hour"`
	Minute     int `json:"minute"`
	Second     int `json:"second"`
	Nanosecond int `json:"nanosecond"`
}

func (l LocalDateTime) Validate() error {
	if l.Year < 1 || l.Year > 9999 || l.Month < 1 || l.Month > 12 || l.Day < 1 || l.Day > 31 || l.Hour < 0 || l.Hour > 23 || l.Minute < 0 || l.Minute > 59 || l.Second < 0 || l.Second > 59 || l.Nanosecond < 0 || l.Nanosecond > 999999999 {
		return errors.New("local date-time component out of range")
	}
	candidate := time.Date(l.Year, time.Month(l.Month), l.Day, l.Hour, l.Minute, l.Second, l.Nanosecond, time.UTC)
	if candidate.Year() != l.Year || int(candidate.Month()) != l.Month || candidate.Day() != l.Day || candidate.Hour() != l.Hour || candidate.Minute() != l.Minute || candidate.Second() != l.Second || candidate.Nanosecond() != l.Nanosecond {
		return errors.New("invalid calendar date-time")
	}
	return nil
}

type AmbiguityPolicy string

const (
	RejectAmbiguous AmbiguityPolicy = "reject"
	PreferEarlier   AmbiguityPolicy = "earlier"
	PreferLater     AmbiguityPolicy = "later"
)

var (
	ErrNonexistentLocalTime = errors.New("local time does not exist in timezone")
	ErrAmbiguousLocalTime   = errors.New("local time is ambiguous in timezone")
)

// ResolveLocalTime resolves a wall time by testing the zone offsets around the
// target date. DST gaps return ErrNonexistentLocalTime and folds require an
// explicit earlier/later policy; there is no silent time.Date normalization.
func ResolveLocalTime(local LocalDateTime, zone TimeZone, policy AmbiguityPolicy) (UTCInstant, error) {
	if err := local.Validate(); err != nil {
		return UTCInstant{}, err
	}
	location, err := zone.Location()
	if err != nil {
		return UTCInstant{}, err
	}
	if policy != RejectAmbiguous && policy != PreferEarlier && policy != PreferLater {
		return UTCInstant{}, errors.New("invalid ambiguity policy")
	}
	naive := time.Date(local.Year, time.Month(local.Month), local.Day, local.Hour, local.Minute, local.Second, local.Nanosecond, time.UTC)
	offsets := map[int]struct{}{}
	for delta := -72 * time.Hour; delta <= 72*time.Hour; delta += 3 * time.Hour {
		_, offset := naive.Add(delta).In(location).Zone()
		offsets[offset] = struct{}{}
	}
	candidates := make([]time.Time, 0, 2)
	for offset := range offsets {
		candidate := naive.Add(-time.Duration(offset) * time.Second).UTC()
		wall := candidate.In(location)
		if wall.Year() == local.Year && int(wall.Month()) == local.Month && wall.Day() == local.Day && wall.Hour() == local.Hour && wall.Minute() == local.Minute && wall.Second() == local.Second && wall.Nanosecond() == local.Nanosecond {
			candidates = append(candidates, candidate)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Before(candidates[j]) })
	switch len(candidates) {
	case 0:
		return UTCInstant{}, ErrNonexistentLocalTime
	case 1:
		return NewUTCInstant(candidates[0])
	default:
		switch policy {
		case PreferEarlier:
			return NewUTCInstant(candidates[0])
		case PreferLater:
			return NewUTCInstant(candidates[len(candidates)-1])
		default:
			return UTCInstant{}, ErrAmbiguousLocalTime
		}
	}
}

// TaxCategory is explicit metadata; tax treatment must never be inferred from
// arbitrary product/provider strings.
type TaxCategory string

const (
	TaxStandard      TaxCategory = "standard"
	TaxReduced       TaxCategory = "reduced"
	TaxZero          TaxCategory = "zero"
	TaxExempt        TaxCategory = "exempt"
	TaxOutOfScope    TaxCategory = "out_of_scope"
	TaxReverseCharge TaxCategory = "reverse_charge"
)

func (c TaxCategory) Validate() error {
	switch c {
	case TaxStandard, TaxReduced, TaxZero, TaxExempt, TaxOutOfScope, TaxReverseCharge:
		return nil
	default:
		return errors.New("unsupported tax category")
	}
}

// TaxTreatment carries explicit jurisdiction/category/rate facts. Rate is a
// fraction (for example 0.2 = 20%) and is omitted for exempt/out-of-scope cases.
type TaxTreatment struct {
	Jurisdiction  string      `json:"jurisdiction"`
	Category      TaxCategory `json:"category"`
	RateFraction  *Decimal    `json:"rate_fraction,omitempty"`
	PriceIncludes bool        `json:"price_includes_tax"`
	ReasonCode    string      `json:"reason_code,omitempty"`
}

func (t TaxTreatment) Validate() error {
	if !taxJurisdictionPattern.MatchString(t.Jurisdiction) {
		return errors.New("tax jurisdiction must be an uppercase country/subdivision code")
	}
	if err := t.Category.Validate(); err != nil {
		return err
	}
	if t.ReasonCode != "" && !codePattern.MatchString(t.ReasonCode) {
		return errors.New("tax reason code must be canonical")
	}
	requiresRate := t.Category == TaxStandard || t.Category == TaxReduced || t.Category == TaxZero
	if requiresRate && t.RateFraction == nil {
		return errors.New("tax category requires explicit rate_fraction")
	}
	if !requiresRate && t.RateFraction != nil {
		return errors.New("tax category must not carry rate_fraction")
	}
	if t.RateFraction != nil {
		if err := t.RateFraction.Validate(); err != nil {
			return fmt.Errorf("tax rate: %w", err)
		}
		zero, _ := ParseDecimal("0")
		one, _ := ParseDecimal("1")
		low, err := t.RateFraction.Cmp(zero)
		if err != nil {
			return err
		}
		high, err := t.RateFraction.Cmp(one)
		if err != nil {
			return err
		}
		if low < 0 || high > 0 {
			return errors.New("tax rate_fraction must be between 0 and 1")
		}
		if t.Category == TaxZero && !t.RateFraction.IsZero() {
			return errors.New("zero tax category requires rate_fraction 0")
		}
	}
	return nil
}

func (t *TaxTreatment) UnmarshalJSON(data []byte) error {
	var wire struct {
		Jurisdiction  string      `json:"jurisdiction"`
		Category      TaxCategory `json:"category"`
		RateFraction  *Decimal    `json:"rate_fraction,omitempty"`
		PriceIncludes *bool       `json:"price_includes_tax"`
		ReasonCode    string      `json:"reason_code,omitempty"`
	}
	if err := decodeStrictJSON(data, &wire); err != nil {
		return err
	}
	if wire.PriceIncludes == nil {
		return errors.New("tax treatment requires price_includes_tax")
	}
	value := TaxTreatment{
		Jurisdiction: wire.Jurisdiction, Category: wire.Category, RateFraction: wire.RateFraction,
		PriceIncludes: *wire.PriceIncludes, ReasonCode: wire.ReasonCode,
	}
	if err := value.Validate(); err != nil {
		return err
	}
	*t = value
	return nil
}

// TaxRequest contains explicit facts needed by a tax adapter. It intentionally
// does not encode a country-specific VAT engine in the shared domain.
type TaxRequest struct {
	SellerCountry CountryCode
	BuyerCountry  CountryCode
	ShipTo        *Address
	OccurredAt    UTCInstant
	Amount        Money
}

func (r TaxRequest) Validate() error {
	if err := r.SellerCountry.Validate(); err != nil {
		return fmt.Errorf("seller country: %w", err)
	}
	if err := r.BuyerCountry.Validate(); err != nil {
		return fmt.Errorf("buyer country: %w", err)
	}
	if r.ShipTo != nil {
		if err := r.ShipTo.Validate(); err != nil {
			return fmt.Errorf("ship-to: %w", err)
		}
		if r.ShipTo.Country != r.BuyerCountry {
			return errors.New("ship-to country must match buyer country")
		}
	}
	if err := r.OccurredAt.Validate(); err != nil {
		return fmt.Errorf("occurred_at: %w", err)
	}
	if err := r.Amount.Validate(); err != nil {
		return fmt.Errorf("amount: %w", err)
	}
	return nil
}

// TaxProvider resolves provider/country-specific rules outside the generic core.
type TaxProvider interface {
	ResolveTax(context.Context, TaxRequest) (TaxTreatment, error)
}
