package domain

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func mustCurrency(t *testing.T, code string) Currency {
	t.Helper()
	value, err := NewCurrency(code)
	if err != nil {
		t.Fatalf("NewCurrency(%q): %v", code, err)
	}
	return value
}

func mustDecimal(t *testing.T, value string) Decimal {
	t.Helper()
	parsed, err := ParseDecimal(value)
	if err != nil {
		t.Fatalf("ParseDecimal(%q): %v", value, err)
	}
	return parsed
}

func mustCountry(t *testing.T, code string) CountryCode {
	t.Helper()
	value, err := NewCountryCode(code)
	if err != nil {
		t.Fatalf("NewCountryCode(%q): %v", code, err)
	}
	return value
}

func mustZone(t *testing.T, name string) TimeZone {
	t.Helper()
	value, err := NewTimeZone(name)
	if err != nil {
		t.Fatalf("NewTimeZone(%q): %v", name, err)
	}
	return value
}

func TestCurrencyIsCanonicalAndRegistryNeutral(t *testing.T) {
	for _, code := range []string{"RUB", "USD", "EUR", "XTS"} {
		if _, err := NewCurrency(code); err != nil {
			t.Fatalf("valid code %s: %v", code, err)
		}
	}
	for _, code := range []string{"rub", "US", "USDT", "R1B", " RUB"} {
		if _, err := NewCurrency(code); err == nil {
			t.Fatalf("NewCurrency(%q) unexpectedly succeeded", code)
		}
	}
}

func TestSharedValidationPrimitives(t *testing.T) {
	validUUID := "018f0f8a-7abc-7def-8abc-0123456789ab"
	validULID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if !ValidUUIDv7(validUUID) || !ValidSortableID(validUUID) {
		t.Fatalf("expected UUIDv7 to be valid")
	}
	if !ValidULID(validULID) || !ValidSortableID(validULID) {
		t.Fatalf("expected ULID to be valid")
	}
	for _, value := range []string{"018f0f8a-7abc-6def-8abc-0123456789ab", "01arz3ndektsv4rrffq69g5fav", "RUB1", "bad token"} {
		if ValidSortableID(value) || ValidCurrencyCode(value) && value != "RUB1" || ValidToken(value) && value == "bad token" {
			t.Fatalf("unexpectedly accepted invalid shared value %q", value)
		}
	}
	if !ValidCurrencyCode("RUB") || !ValidToken("order:demo-1") {
		t.Fatalf("expected shared currency/token validation to accept canonical values")
	}
	if !ValidText("Описание товара", 1, 64, false) || !ValidText("строка 1\nстрока 2", 1, 64, true) {
		t.Fatalf("expected shared text validation to accept valid values")
	}
	if ValidText(" leading", 1, 64, false) || ValidText("line\nfeed", 1, 64, false) {
		t.Fatalf("expected shared text validation to reject invalid values")
	}
}

func TestMoneyUsesMinorUnitsAndRejectsCrossCurrencyArithmetic(t *testing.T) {
	rub := mustCurrency(t, "RUB")
	usd := mustCurrency(t, "USD")
	left, _ := NewMoney(1250, rub)
	right, _ := NewMoney(275, rub)
	sum, err := left.Add(right)
	if err != nil {
		t.Fatal(err)
	}
	if sum.MinorUnits() != 1525 || sum.Currency() != rub {
		t.Fatalf("sum = %+v", sum)
	}

	other, _ := NewMoney(1, usd)
	if _, err := left.Add(other); err == nil {
		t.Fatal("cross-currency add unexpectedly succeeded")
	}

	max, _ := NewMoney(math.MaxInt64, rub)
	one, _ := NewMoney(1, rub)
	if _, err := max.Add(one); err == nil {
		t.Fatal("overflow unexpectedly succeeded")
	}
}

func TestMoneyJSONNeverUsesMajorUnitFloat(t *testing.T) {
	money, _ := NewMoney(12345, mustCurrency(t, "RUB"))
	encoded, err := json.Marshal(money)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"minor_units":12345,"currency":"RUB"}`; got != want {
		t.Fatalf("json = %s, want %s", got, want)
	}
	if strings.Contains(string(encoded), "123.45") {
		t.Fatal("money leaked major-unit float representation")
	}

	var decoded Money
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Equal(money) {
		t.Fatalf("decoded = %+v, want %+v", decoded, money)
	}
	if err := json.Unmarshal([]byte(`{"minor_units":12345,"currency":"rub"}`), &decoded); err == nil {
		t.Fatal("lowercase currency unexpectedly accepted")
	}
	if err := json.Unmarshal([]byte(`{"minor_units":12345,"currency":"RUB","amount":123.45}`), &decoded); err == nil {
		t.Fatal("unknown float field unexpectedly accepted")
	}
}

func TestPrimitiveJSONRejectsMissingAndUnknownRequiredFields(t *testing.T) {
	var money Money
	for _, raw := range []string{
		`{"currency":"RUB"}`,
		`{"minor_units":100}`,
		`{"minor_units":100,"currency":"RUB","extra":true}`,
	} {
		if err := json.Unmarshal([]byte(raw), &money); err == nil {
			t.Fatalf("Money accepted %s", raw)
		}
	}

	var quantity Quantity
	for _, raw := range []string{
		`{"unit":"KG"}`,
		`{"value":"1.25"}`,
		`{"value":"1.25","unit":"KG","extra":true}`,
	} {
		if err := json.Unmarshal([]byte(raw), &quantity); err == nil {
			t.Fatalf("Quantity accepted %s", raw)
		}
	}

	var tax TaxTreatment
	for _, raw := range []string{
		`{"jurisdiction":"RU","category":"standard","rate_fraction":"0.2"}`,
		`{"jurisdiction":"RU","category":"standard","rate_fraction":"0.2","price_includes_tax":true,"extra":1}`,
	} {
		if err := json.Unmarshal([]byte(raw), &tax); err == nil {
			t.Fatalf("TaxTreatment accepted %s", raw)
		}
	}
}

func TestDecimalCanonicalFixedPoint(t *testing.T) {
	cases := map[string]string{
		"0": "0", "12": "12", "12.3400": "12.34", "0.000001": "0.000001", "-42.5": "-42.5",
		"9223372036.854775807": "9223372036.854775807",
	}
	for input, want := range cases {
		got, err := ParseDecimal(input)
		if err != nil {
			t.Fatalf("ParseDecimal(%q): %v", input, err)
		}
		if got.String() != want {
			t.Fatalf("ParseDecimal(%q).String() = %q, want %q", input, got.String(), want)
		}
		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != `"`+want+`"` {
			t.Fatalf("decimal JSON = %s", encoded)
		}
	}
	for _, input := range []string{"", "+1", "01", "1.", ".1", "-0", "1e3", "1.0000000001", "9223372036854775808"} {
		if _, err := ParseDecimal(input); err == nil {
			t.Fatalf("ParseDecimal(%q) unexpectedly succeeded", input)
		}
	}
	var decimal Decimal
	if err := json.Unmarshal([]byte(`1.25`), &decimal); err == nil {
		t.Fatal("JSON number unexpectedly accepted for Decimal")
	}
}

func TestDecimalArithmeticAlignsExactly(t *testing.T) {
	left := mustDecimal(t, "1.2")
	right := mustDecimal(t, "0.03")
	sum, err := left.Add(right)
	if err != nil {
		t.Fatal(err)
	}
	if sum.String() != "1.23" {
		t.Fatalf("sum = %s", sum.String())
	}
	diff, err := sum.Sub(mustDecimal(t, "0.23"))
	if err != nil {
		t.Fatal(err)
	}
	if diff.String() != "1" {
		t.Fatalf("difference = %s", diff.String())
	}
	cmp, err := mustDecimal(t, "1.200").Cmp(mustDecimal(t, "1.2"))
	if err != nil || cmp != 0 {
		t.Fatalf("Cmp = %d, %v", cmp, err)
	}
}

func TestQuantityRequiresExplicitCanonicalUnit(t *testing.T) {
	unit, err := NewUnitCode("KG")
	if err != nil {
		t.Fatal(err)
	}
	quantity, err := NewQuantity(mustDecimal(t, "1.25"), unit)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(quantity)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"value":"1.25","unit":"KG"}`; got != want {
		t.Fatalf("quantity json = %s, want %s", got, want)
	}
	if _, err := NewUnitCode("kg"); err == nil {
		t.Fatal("lowercase unit unexpectedly accepted")
	}
}

func TestLocaleCanonicalSubset(t *testing.T) {
	for _, locale := range []string{"ru", "ru-RU", "en-US", "zh-Hans-CN", "es-419"} {
		value, err := NewLocale(locale)
		if err != nil || value.String() != locale {
			t.Fatalf("NewLocale(%q) = %q, %v", locale, value, err)
		}
	}
	for _, locale := range []string{"RU-ru", "en-us", "zh-hans-CN", "en_US", "e", "en-US-x-private"} {
		if _, err := NewLocale(locale); err == nil {
			t.Fatalf("NewLocale(%q) unexpectedly succeeded", locale)
		}
	}
}

func TestAddressIsNonLocalizedData(t *testing.T) {
	address := Address{Country: mustCountry(t, "NL"), Region: "Noord-Holland", City: "Amsterdam", PostalCode: "1012", Lines: []string{"Dam 1"}}
	if err := address.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := address
	invalid.Lines = []string{"  Dam 1"}
	if err := invalid.Validate(); err == nil {
		t.Fatal("non-canonical address unexpectedly accepted")
	}
}

func TestUTCInstantRejectsNonUTCOffsets(t *testing.T) {
	utc, err := ParseUTCInstant("2026-08-09T09:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if utc.String() != "2026-08-09T09:00:00Z" {
		t.Fatalf("utc = %s", utc.String())
	}
	if _, err := ParseUTCInstant("2026-08-09T12:00:00+03:00"); err == nil {
		t.Fatal("non-UTC persistence offset unexpectedly accepted")
	}
	zeroOffset, err := ParseUTCInstant("2026-08-09T09:00:00+00:00")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(zeroOffset)
	if string(encoded) != `"2026-08-09T09:00:00Z"` {
		t.Fatalf("normalized JSON = %s", encoded)
	}
}

func TestResolveLocalTimeRejectsDSTGap(t *testing.T) {
	zone := mustZone(t, "Europe/Amsterdam")
	local := LocalDateTime{Year: 2026, Month: 3, Day: 29, Hour: 2, Minute: 30}
	_, err := ResolveLocalTime(local, zone, RejectAmbiguous)
	if !errors.Is(err, ErrNonexistentLocalTime) {
		t.Fatalf("gap error = %v", err)
	}
}

func TestResolveLocalTimeRequiresPolicyForDSTFold(t *testing.T) {
	zone := mustZone(t, "Europe/Amsterdam")
	local := LocalDateTime{Year: 2026, Month: 10, Day: 25, Hour: 2, Minute: 30}
	if _, err := ResolveLocalTime(local, zone, RejectAmbiguous); !errors.Is(err, ErrAmbiguousLocalTime) {
		t.Fatalf("fold error = %v", err)
	}
	earlier, err := ResolveLocalTime(local, zone, PreferEarlier)
	if err != nil {
		t.Fatal(err)
	}
	later, err := ResolveLocalTime(local, zone, PreferLater)
	if err != nil {
		t.Fatal(err)
	}
	if !earlier.Time().Before(later.Time()) || later.Time().Sub(earlier.Time()) != time.Hour {
		t.Fatalf("fold candidates = %s, %s", earlier, later)
	}
	if earlier.String() != "2026-10-25T00:30:00Z" || later.String() != "2026-10-25T01:30:00Z" {
		t.Fatalf("unexpected fold resolution: %s / %s", earlier, later)
	}
}

func TestResolveLocalTimeOrdinaryAndUTC(t *testing.T) {
	local := LocalDateTime{Year: 2026, Month: 8, Day: 9, Hour: 12, Minute: 7}
	instant, err := ResolveLocalTime(local, mustZone(t, "Europe/Amsterdam"), RejectAmbiguous)
	if err != nil {
		t.Fatal(err)
	}
	if instant.String() != "2026-08-09T10:07:00Z" {
		t.Fatalf("instant = %s", instant.String())
	}
	utc, err := ResolveLocalTime(local, mustZone(t, "UTC"), RejectAmbiguous)
	if err != nil {
		t.Fatal(err)
	}
	if utc.String() != "2026-08-09T12:07:00Z" {
		t.Fatalf("UTC instant = %s", utc.String())
	}
}

func TestTaxTreatmentIsExplicitAndBounded(t *testing.T) {
	rate := mustDecimal(t, "0.2")
	standard := TaxTreatment{Jurisdiction: "RU", Category: TaxStandard, RateFraction: &rate, PriceIncludes: true}
	if err := standard.Validate(); err != nil {
		t.Fatal(err)
	}
	zero := mustDecimal(t, "0")
	if err := (TaxTreatment{Jurisdiction: "RU", Category: TaxZero, RateFraction: &zero}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (TaxTreatment{Jurisdiction: "RU", Category: TaxExempt}).Validate(); err != nil {
		t.Fatal(err)
	}
	tooHigh := mustDecimal(t, "1.01")
	if err := (TaxTreatment{Jurisdiction: "RU", Category: TaxStandard, RateFraction: &tooHigh}).Validate(); err == nil {
		t.Fatal("tax rate > 1 unexpectedly accepted")
	}
	if err := (TaxTreatment{Jurisdiction: "RU", Category: TaxExempt, RateFraction: &zero}).Validate(); err == nil {
		t.Fatal("exempt rate unexpectedly accepted")
	}
}

func TestTaxRequestKeepsCountrySpecificRulesBehindPort(t *testing.T) {
	rub := mustCurrency(t, "RUB")
	amount, _ := NewMoney(10000, rub)
	instant, _ := ParseUTCInstant("2026-08-09T09:00:00Z")
	ru := mustCountry(t, "RU")
	request := TaxRequest{
		SellerCountry: ru, BuyerCountry: ru,
		ShipTo:     &Address{Country: ru, Region: "Moscow", City: "Moscow", Lines: []string{"Example 1"}},
		OccurredAt: instant, Amount: amount,
	}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	request.ShipTo.Country = mustCountry(t, "NL")
	if err := request.Validate(); err == nil {
		t.Fatal("buyer/ship-to mismatch unexpectedly accepted")
	}
}

// Compile-time port checks keep formatting/tax behavior outside shared values.
type stubPorts struct{}

func (stubPorts) ValidateCurrency(context.Context, Currency) error                 { return nil }
func (stubPorts) MinorUnitScale(context.Context, Currency) (uint8, error)          { return 2, nil }
func (stubPorts) FormatAddress(context.Context, Address, Locale) (string, error)   { return "", nil }
func (stubPorts) FormatMoney(context.Context, Money, Locale) (string, error)       { return "", nil }
func (stubPorts) FormatQuantity(context.Context, Quantity, Locale) (string, error) { return "", nil }
func (stubPorts) FormatInstant(context.Context, UTCInstant, TimeZone, Locale) (string, error) {
	return "", nil
}
func (stubPorts) ResolveTax(context.Context, TaxRequest) (TaxTreatment, error) {
	return TaxTreatment{}, nil
}

var _ AddressFormatter = stubPorts{}
var _ CurrencyMetadataProvider = stubPorts{}
var _ LocalizationPort = stubPorts{}
var _ TaxProvider = stubPorts{}
