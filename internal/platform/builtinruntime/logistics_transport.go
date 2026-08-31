package builtinruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"math/big"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	cdek "github.com/torgnexa/torgnexa/connectors/logistics/cdek"
	dellin "github.com/torgnexa/torgnexa/connectors/logistics/dellin"
	fivepost "github.com/torgnexa/torgnexa/connectors/logistics/fivepost"
	pek "github.com/torgnexa/torgnexa/connectors/logistics/pek"
	pochtarussia "github.com/torgnexa/torgnexa/connectors/logistics/pochta-russia"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var errLogisticsOperationNotAdmitted = errors.New("logistics operation requires carrier qualification")
var safeCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
var pekDecimalPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]*)(?:\.[0-9]{1,3})?$`)
var pekRuntimeCargoCodePattern = regexp.MustCompile(`^[0-9]{1,18}$`)
var pekRuntimeWarehouseIDPattern = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)
var cdekPhonePattern = regexp.MustCompile(`^[0-9+() -]{7,32}$`)
var pochtarussiaOrderIDPattern = regexp.MustCompile(`^[0-9]{1,18}$`)
var logisticsBatchRemoteIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
var pochtarussiaHousePattern = regexp.MustCompile(`^[0-9]{1,6}[[:alnum:]А-Яа-я/-]{0,8}$`)
var dellinDocumentUIDPattern = regexp.MustCompile(`^0x[0-9A-Fa-f]{8,64}$`)

// fivepostHTTP is the reviewed host-side credential probe. The partner API
// publishes the token endpoint, while shipment payload mappings remain behind
// the SDK until a current partner contract is qualified. Keeping those calls
// fail-closed prevents a guessed write shape from creating a real shipment.
type fivepostHTTP struct{ h *httpTransport }

func (transport fivepostHTTP) Ping(ctx context.Context, secret []byte) error {
	if len(secret) == 0 || transport.h == nil {
		return errors.New("5post credential probe unavailable")
	}
	body, err := json.Marshal(struct {
		APIKey string `json:"apiKey"`
	}{APIKey: string(secret)})
	if err != nil {
		return err
	}
	headers := http.Header{"Content-Type": []string{"application/json"}}
	status, _, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.5post.ru", "/api/v1/auth/token", url.Values{}, body, headers, nil, nil)
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("5post credential probe rejected with status %d", status)
	}
	return nil
}

func (fivepostHTTP) Create(context.Context, []byte, sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{}, errLogisticsOperationNotAdmitted
}
func (fivepostHTTP) Track(context.Context, []byte, sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{}, errLogisticsOperationNotAdmitted
}
func (fivepostHTTP) Cancel(context.Context, []byte, sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{}, errLogisticsOperationNotAdmitted
}
func (fivepostHTTP) Label(context.Context, []byte, sdk.LabelRequest) (sdk.LabelResult, error) {
	return sdk.LabelResult{}, errLogisticsOperationNotAdmitted
}
func (fivepostHTTP) Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	return nil, errLogisticsOperationNotAdmitted
}

var _ fivepost.Transport = fivepostHTTP{}

// cdekHTTP performs the two-step CDEK OAuth client-credentials probe. The
// token is used only for the bounded city-directory request and is never
// persisted or returned to the application layer.
type cdekHTTP struct{ h *httpTransport }

func (transport cdekHTTP) Ping(ctx context.Context, secret []byte) error {
	if transport.h == nil {
		return errors.New("СДЭК credential probe unavailable")
	}
	token, err := transport.accessToken(ctx, secret)
	if err != nil {
		return err
	}
	cityHeaders := http.Header{"Authorization": []string{"Bearer " + token}, "Accept": []string{"application/json"}}
	status, _, _, _, _, err := transport.h.do(ctx, http.MethodGet, "api.cdek.ru", "/v2/location/cities", url.Values{"size": []string{"1"}}, nil, cityHeaders, nil, nil)
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("СДЭК city probe rejected with status %d", status)
	}
	return nil
}

func (transport cdekHTTP) accessToken(ctx context.Context, secret []byte) (string, error) {
	if transport.h == nil {
		return "", errors.New("СДЭК credential probe unavailable")
	}
	var credentials struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if decodeStrict(secret, &credentials) != nil || strings.TrimSpace(credentials.ClientID) == "" || credentials.ClientSecret == "" {
		return "", errors.New("СДЭК credentials must be JSON with client_id and client_secret")
	}
	form := url.Values{
		"grant_type":    []string{"client_credentials"},
		"client_id":     []string{credentials.ClientID},
		"client_secret": []string{credentials.ClientSecret},
	}
	headers := http.Header{"Content-Type": []string{"application/x-www-form-urlencoded"}, "Accept": []string{"application/json"}}
	status, body, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.cdek.ru", "/v2/oauth/token", url.Values{}, []byte(form.Encode()), headers, nil, nil)
	if err != nil {
		return "", err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return "", fmt.Errorf("СДЭК token probe rejected with status %d", status)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(body, &token) != nil || strings.TrimSpace(token.AccessToken) == "" {
		return "", errors.New("СДЭК token probe returned no access token")
	}
	return token.AccessToken, nil
}

type cdekDeliveryPoint struct {
	Code        json.RawMessage `json:"code"`
	UUID        string          `json:"uuid"`
	Name        string          `json:"name"`
	Address     string          `json:"address"`
	AddressNote string          `json:"address_comment"`
	Type        string          `json:"type"`
	IsClosed    bool            `json:"is_closed"`
	IsHandout   bool            `json:"is_handout"`
	IsReception bool            `json:"is_reception"`
	Location    struct {
		CountryCode string `json:"country_code"`
		City        string `json:"city"`
		Address     string `json:"address"`
	} `json:"location"`
}

func cdekScalarText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return strings.TrimSpace(number.String())
	}
	return ""
}

func (transport cdekHTTP) Pickup(ctx context.Context, secret []byte, query sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	token, err := transport.accessToken(ctx, secret)
	if err != nil {
		return nil, err
	}
	status, body, _, _, _, err := transport.h.do(ctx, http.MethodGet, "api.cdek.ru", "/v2/deliverypoints", url.Values{
		"country_code": []string{strings.ToUpper(query.Country)},
		"city":         []string{query.City},
		"size":         []string{strconv.Itoa(query.Limit)},
	}, nil, http.Header{"Authorization": []string{"Bearer " + token}, "Accept": []string{"application/json"}}, nil, nil)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("СДЭК delivery-point request rejected with status %d", status)
	}
	var remotePoints []cdekDeliveryPoint
	if err := json.Unmarshal(body, &remotePoints); err != nil || len(remotePoints) > query.Limit {
		return nil, errors.New("СДЭК delivery-point response rejected")
	}
	now := time.Now().UTC()
	points := make([]sdk.PickupPoint, 0, len(remotePoints))
	for _, remotePoint := range remotePoints {
		remoteID := strings.TrimSpace(remotePoint.UUID)
		if remoteID == "" {
			remoteID = cdekScalarText(remotePoint.Code)
		}
		if remoteID == "" {
			return nil, errors.New("СДЭК delivery-point response has no identifier")
		}
		country := strings.TrimSpace(remotePoint.Location.CountryCode)
		if country == "" {
			country = strings.ToUpper(query.Country)
		}
		city := strings.TrimSpace(remotePoint.Location.City)
		if city == "" {
			city = query.City
		}
		address := strings.TrimSpace(remotePoint.Location.Address)
		if address == "" {
			address = strings.TrimSpace(remotePoint.Address)
		}
		if address == "" {
			address = strings.TrimSpace(remotePoint.AddressNote)
		}
		if len(country) != 2 || city == "" || address == "" {
			return nil, errors.New("СДЭК delivery-point response has incomplete location")
		}
		name := strings.TrimSpace(remotePoint.Name)
		if name == "" {
			name = "СДЭК ПВЗ " + remoteID
		}
		points = append(points, sdk.PickupPoint{RemoteID: remoteID, Name: name, Country: country, City: city, Address: address, Active: !remotePoint.IsClosed, UpdatedAt: now})
	}
	return points, nil
}

type cdekRateLocation struct {
	CountryCode string `json:"country_code"`
	PostalCode  string `json:"postal_code,omitempty"`
	City        string `json:"city"`
	Address     string `json:"address"`
}

type cdekRatePackage struct {
	Weight int64 `json:"weight"`
	Length int64 `json:"length"`
	Width  int64 `json:"width"`
	Height int64 `json:"height"`
}

type cdekRateRequest struct {
	From     cdekRateLocation  `json:"from_location"`
	To       cdekRateLocation  `json:"to_location"`
	Packages []cdekRatePackage `json:"packages"`
}

type cdekCreateContact struct {
	Name   string            `json:"name"`
	Phones []cdekCreatePhone `json:"phones"`
	Email  string            `json:"email,omitempty"`
}

type cdekCreatePhone struct {
	Number string `json:"number"`
}

type cdekCreatePackage struct {
	Number string `json:"number"`
	Weight int64  `json:"weight"`
	Length int64  `json:"length"`
	Width  int64  `json:"width"`
	Height int64  `json:"height"`
}

type cdekCreateOrder struct {
	Type          int                 `json:"type"`
	Number        string              `json:"number"`
	TariffCode    int                 `json:"tariff_code"`
	ShipmentPoint string              `json:"shipment_point,omitempty"`
	DeliveryPoint string              `json:"delivery_point,omitempty"`
	FromLocation  cdekRateLocation    `json:"from_location"`
	ToLocation    cdekRateLocation    `json:"to_location"`
	Sender        cdekCreateContact   `json:"sender"`
	Recipient     cdekCreateContact   `json:"recipient"`
	Packages      []cdekCreatePackage `json:"packages"`
}

type cdekCreateResponse struct {
	Entity struct {
		UUID       string          `json:"uuid"`
		CDEKNumber json.RawMessage `json:"cdek_number"`
		Number     json.RawMessage `json:"number"`
	} `json:"entity"`
}

type cdekPrintOrder struct {
	OrderUUID string `json:"order_uuid"`
}

type cdekPrintRequest struct {
	Orders    []cdekPrintOrder `json:"orders"`
	CopyCount int              `json:"copy_count"`
	Format    string           `json:"format"`
}

type cdekPrintResponse struct {
	Entity struct {
		UUID string `json:"uuid"`
		URL  string `json:"url"`
	} `json:"entity"`
}

type cdekTariffQuote struct {
	TariffCode  json.RawMessage `json:"tariff_code"`
	TotalSum    json.RawMessage `json:"total_sum"`
	DeliverySum json.RawMessage `json:"delivery_sum"`
	Currency    string          `json:"currency"`
	PeriodMin   int             `json:"period_min"`
	PeriodMax   int             `json:"period_max"`
}

type cdekTariffListResponse struct {
	TariffCodes []cdekTariffQuote `json:"tariff_codes"`
}

type cdekOrderStatus struct {
	Code           string `json:"code"`
	StatusCode     string `json:"status_code"`
	DateTime       string `json:"date_time"`
	StatusDateTime string `json:"status_date_time"`
}

type cdekOrderResponse struct {
	UUID       string            `json:"uuid"`
	CDEKNumber json.RawMessage   `json:"cdek_number"`
	Number     json.RawMessage   `json:"number"`
	Statuses   []cdekOrderStatus `json:"statuses"`
}

type cdekOrderEnvelope struct {
	Entity cdekOrderResponse `json:"entity"`
}

type cdekWebhookEvent struct {
	Type     string `json:"type"`
	UUID     string `json:"uuid"`
	DateTime string `json:"date_time"`
	Attrs    struct {
		CDEKNumber     json.RawMessage `json:"cdek_number"`
		Number         json.RawMessage `json:"number"`
		StatusCode     json.RawMessage `json:"status_code"`
		Code           string          `json:"code"`
		StatusDateTime string          `json:"status_date_time"`
	} `json:"attributes"`
}

const cdekTariffResponseLimit = 100

func cdekAddress(value sdk.Address) cdekRateLocation {
	return cdekRateLocation{
		CountryCode: strings.ToUpper(strings.TrimSpace(value.Country)),
		PostalCode:  strings.TrimSpace(value.PostalCode),
		City:        strings.TrimSpace(value.City),
		Address:     strings.TrimSpace(value.Line1),
	}
}

func cdekDimensionMillimeters(value int64) int64 {
	result := value / 10
	if value%10 != 0 {
		result++
	}
	return result
}

func cdekRateAmountText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", errors.New("СДЭК tariff response has no amount")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return "", errors.New("СДЭК tariff amount is invalid")
	}
	switch typed := value.(type) {
	case json.Number:
		return typed.String(), nil
	case string:
		return strings.TrimSpace(typed), nil
	default:
		return "", errors.New("СДЭК tariff amount is invalid")
	}
}

func cdekMinorUnits(raw json.RawMessage) (int64, error) {
	text, err := cdekRateAmountText(raw)
	if err != nil || text == "" || strings.HasPrefix(text, "-") || strings.ContainsAny(text, "eE+") {
		return 0, errors.New("СДЭК tariff amount must be a non-negative decimal")
	}
	parts := strings.Split(text, ".")
	if len(parts) > 2 {
		return 0, errors.New("СДЭК tariff amount has invalid precision")
	}
	whole := parts[0]
	if whole == "" {
		whole = "0"
	}
	for _, symbol := range whole {
		if symbol < '0' || symbol > '9' {
			return 0, errors.New("СДЭК tariff amount is invalid")
		}
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if len(fraction) > 2 {
			return 0, errors.New("СДЭК tariff amount has more than two fractional digits")
		}
		for _, symbol := range fraction {
			if symbol < '0' || symbol > '9' {
				return 0, errors.New("СДЭК tariff amount is invalid")
			}
		}
	}
	for len(fraction) < 2 {
		fraction += "0"
	}
	fractionUnits, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, errors.New("СДЭК tariff amount is invalid")
	}
	wholeUnits, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || wholeUnits > (int64(1<<63-1)-fractionUnits)/100 {
		return 0, errors.New("СДЭК tariff amount is out of range")
	}
	minor := wholeUnits*100 + fractionUnits
	if minor < 0 {
		return 0, errors.New("СДЭК tariff amount is out of range")
	}
	return minor, nil
}

func (transport cdekHTTP) Rates(ctx context.Context, secret []byte, request sdk.RateRequest) ([]sdk.RateQuote, error) {
	if transport.h == nil || request.Validate() != nil {
		return nil, errors.New("СДЭК tariff request is unavailable")
	}
	token, err := transport.accessToken(ctx, secret)
	if err != nil {
		return nil, err
	}
	packages := make([]cdekRatePackage, 0, len(request.Parcels))
	for _, parcel := range request.Parcels {
		packages = append(packages, cdekRatePackage{Weight: parcel.WeightGrams, Length: cdekDimensionMillimeters(parcel.LengthMM), Width: cdekDimensionMillimeters(parcel.WidthMM), Height: cdekDimensionMillimeters(parcel.HeightMM)})
	}
	body, err := json.Marshal(cdekRateRequest{From: cdekAddress(request.From), To: cdekAddress(request.To), Packages: packages})
	if err != nil {
		return nil, err
	}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.cdek.ru", "/v2/calculator/tarifflist", url.Values{}, body, http.Header{"Authorization": []string{"Bearer " + token}, "Accept": []string{"application/json"}}, nil, nil)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("СДЭК tariff request rejected with status %d", status)
	}
	var response cdekTariffListResponse
	if json.Unmarshal(responseBody, &response) != nil || len(response.TariffCodes) > cdekTariffResponseLimit {
		return nil, errors.New("СДЭК tariff response rejected")
	}
	now := time.Now().UTC()
	quotes := make([]sdk.RateQuote, 0, len(response.TariffCodes))
	seen := make(map[string]struct{}, len(response.TariffCodes))
	for _, tariff := range response.TariffCodes {
		remoteCode := cdekScalarText(tariff.TariffCode)
		if remoteCode == "" {
			return nil, errors.New("СДЭК tariff response has no identifier")
		}
		serviceCode := "cdek_tariff_" + remoteCode
		if !safeCodePattern.MatchString(serviceCode) {
			return nil, errors.New("СДЭК tariff response has an invalid identifier")
		}
		if _, duplicate := seen[serviceCode]; duplicate {
			return nil, errors.New("СДЭК tariff response has duplicate identifiers")
		}
		seen[serviceCode] = struct{}{}
		amount := tariff.TotalSum
		if len(bytes.TrimSpace(amount)) == 0 || bytes.Equal(bytes.TrimSpace(amount), []byte("null")) {
			amount = tariff.DeliverySum
		}
		minorUnits, err := cdekMinorUnits(amount)
		if err != nil {
			return nil, err
		}
		currency := strings.ToUpper(strings.TrimSpace(tariff.Currency))
		if len(currency) != 3 {
			return nil, errors.New("СДЭК tariff response has invalid currency")
		}
		for _, symbol := range currency {
			if symbol < 'A' || symbol > 'Z' {
				return nil, errors.New("СДЭК tariff response has invalid currency")
			}
		}
		if tariff.PeriodMin < 0 || tariff.PeriodMax < tariff.PeriodMin || tariff.PeriodMax > 3660 {
			return nil, errors.New("СДЭК tariff response has invalid delivery period")
		}
		quotes = append(quotes, sdk.RateQuote{ServiceCode: serviceCode, Cost: sdk.LogisticsMoney{MinorUnits: minorUnits, Currency: currency}, MinDeliveryAt: now.Add(time.Duration(tariff.PeriodMin) * 24 * time.Hour), MaxDeliveryAt: now.Add(time.Duration(tariff.PeriodMax) * 24 * time.Hour), ObservedAt: now})
	}
	return quotes, nil
}

var cdekUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
var cdekRemotePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)

func cdekStatusTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05-0700", "2006-01-02T15:04:05.000-0700"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, errors.New("СДЭК status timestamp is invalid")
}

func (transport cdekHTTP) Track(ctx context.Context, secret []byte, request sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	remoteID := strings.TrimSpace(request.RemoteID)
	if transport.h == nil || remoteID == "" || !cdekRemotePattern.MatchString(remoteID) {
		return sdk.ShipmentResult{}, errors.New("СДЭК tracking request is unavailable")
	}
	token, err := transport.accessToken(ctx, secret)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	queryKey := "cdek_number"
	if cdekUUIDPattern.MatchString(remoteID) {
		queryKey = "uuid"
	}
	status, body, _, _, _, err := transport.h.do(ctx, http.MethodGet, "api.cdek.ru", "/v2/orders", url.Values{queryKey: []string{remoteID}}, nil, http.Header{"Authorization": []string{"Bearer " + token}, "Accept": []string{"application/json"}}, nil, nil)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.ShipmentResult{}, fmt.Errorf("СДЭК tracking request rejected with status %d", status)
	}
	var response cdekOrderEnvelope
	if json.Unmarshal(body, &response) != nil || len(response.Entity.Statuses) == 0 || len(response.Entity.Statuses) > cdekTariffResponseLimit {
		return sdk.ShipmentResult{}, errors.New("СДЭК tracking response rejected")
	}
	var statusCode string
	var observedAt time.Time
	for _, candidate := range response.Entity.Statuses {
		candidateCode := strings.TrimSpace(candidate.Code)
		if candidateCode == "" {
			candidateCode = strings.TrimSpace(candidate.StatusCode)
		}
		if !safeCodePattern.MatchString(candidateCode) {
			return sdk.ShipmentResult{}, errors.New("СДЭК tracking response has no valid status")
		}
		observedAtText := candidate.DateTime
		if strings.TrimSpace(observedAtText) == "" {
			observedAtText = candidate.StatusDateTime
		}
		candidateTime, parseErr := cdekStatusTime(observedAtText)
		if parseErr != nil {
			return sdk.ShipmentResult{}, parseErr
		}
		if observedAt.IsZero() || candidateTime.After(observedAt) {
			statusCode = candidateCode
			observedAt = candidateTime
		}
	}
	canonicalRemoteID := cdekScalarText(response.Entity.CDEKNumber)
	if canonicalRemoteID == "" {
		canonicalRemoteID = strings.TrimSpace(response.Entity.UUID)
	}
	if canonicalRemoteID == "" {
		canonicalRemoteID = remoteID
	}
	trackingNumber := cdekScalarText(response.Entity.CDEKNumber)
	if trackingNumber == "" {
		trackingNumber = cdekScalarText(response.Entity.Number)
	}
	if trackingNumber == "" {
		trackingNumber = canonicalRemoteID
	}
	if !cdekRemotePattern.MatchString(canonicalRemoteID) || !cdekRemotePattern.MatchString(trackingNumber) {
		return sdk.ShipmentResult{}, errors.New("СДЭК tracking response has an invalid identifier")
	}
	return sdk.ShipmentResult{
		RemoteID:       canonicalRemoteID,
		Status:         statusCode,
		Cost:           sdk.LogisticsMoney{Currency: "RUB"},
		TrackingNumber: trackingNumber,
		ObservedAt:     observedAt,
	}, nil
}

func cdekTariffCode(serviceCode string) (int, error) {
	const prefix = "cdek_tariff_"
	if !strings.HasPrefix(serviceCode, prefix) {
		return 0, errors.New("СДЭК shipment service code is not a tariff code")
	}
	value := strings.TrimPrefix(serviceCode, prefix)
	if value == "" || !safeCodePattern.MatchString(value) {
		return 0, errors.New("СДЭК shipment tariff code is invalid")
	}
	tariff, err := strconv.Atoi(value)
	if err != nil || tariff < 1 || tariff > 10000 {
		return 0, errors.New("СДЭК shipment tariff code is out of range")
	}
	return tariff, nil
}

func validCdekContact(contact sdk.LogisticsContact) bool {
	name := strings.TrimSpace(contact.Name)
	phone := strings.TrimSpace(contact.Phone)
	email := strings.TrimSpace(contact.Email)
	if name == "" || name != contact.Name || len(name) > 255 || phone == "" || phone != contact.Phone || !cdekPhonePattern.MatchString(phone) {
		return false
	}
	if email != "" && (email != contact.Email || len(email) > 254 || strings.ContainsAny(email, "\r\n\t ")) {
		return false
	}
	return true
}

func cdekLabelFormat(value string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "PDF", "A4":
		return "A4", nil
	case "A5":
		return "A5", nil
	case "A6":
		return "A6", nil
	default:
		return "", errors.New("СДЭК label format is not supported")
	}
}

func (transport cdekHTTP) Create(ctx context.Context, secret []byte, request sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	if transport.h == nil || request.Validate() != nil || len(request.Parcels) > 50 || request.PickupPointRef != "" && !cdekRemotePattern.MatchString(request.PickupPointRef) || !validCdekContact(request.Sender) || !validCdekContact(request.Recipient) {
		return sdk.ShipmentResult{}, errors.New("СДЭК shipment creation request is unavailable")
	}
	tariffCode, err := cdekTariffCode(request.ServiceCode)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	token, err := transport.accessToken(ctx, secret)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	packages := make([]cdekCreatePackage, 0, len(request.Parcels))
	for index, parcel := range request.Parcels {
		if parcel.Validate() != nil {
			return sdk.ShipmentResult{}, errors.New("СДЭК shipment parcel is invalid")
		}
		packages = append(packages, cdekCreatePackage{
			Number: strconv.Itoa(index + 1),
			Weight: parcel.WeightGrams,
			Length: cdekDimensionMillimeters(parcel.LengthMM),
			Width:  cdekDimensionMillimeters(parcel.WidthMM),
			Height: cdekDimensionMillimeters(parcel.HeightMM),
		})
	}
	body, err := json.Marshal(cdekCreateOrder{
		Type:          1,
		Number:        request.ExternalID,
		TariffCode:    tariffCode,
		DeliveryPoint: request.PickupPointRef,
		FromLocation:  cdekAddress(request.From),
		ToLocation:    cdekAddress(request.To),
		Sender:        cdekCreateContact{Name: request.Sender.Name, Phones: []cdekCreatePhone{{Number: request.Sender.Phone}}, Email: request.Sender.Email},
		Recipient:     cdekCreateContact{Name: request.Recipient.Name, Phones: []cdekCreatePhone{{Number: request.Recipient.Phone}}, Email: request.Recipient.Email},
		Packages:      packages,
	})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.cdek.ru", "/v2/orders", url.Values{}, body, http.Header{"Authorization": []string{"Bearer " + token}, "Content-Type": []string{"application/json"}, "Accept": []string{"application/json"}}, nil, nil)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.ShipmentResult{}, fmt.Errorf("СДЭК shipment creation rejected with status %d", status)
	}
	var response cdekCreateResponse
	if json.Unmarshal(responseBody, &response) != nil {
		return sdk.ShipmentResult{}, errors.New("СДЭК shipment creation response rejected")
	}
	remoteID := cdekScalarText(response.Entity.CDEKNumber)
	if remoteID == "" {
		remoteID = strings.TrimSpace(response.Entity.UUID)
	}
	if remoteID == "" || !cdekRemotePattern.MatchString(remoteID) {
		return sdk.ShipmentResult{}, errors.New("СДЭК shipment creation response has no valid identifier")
	}
	trackingNumber := cdekScalarText(response.Entity.CDEKNumber)
	if trackingNumber == "" {
		trackingNumber = remoteID
	}
	return sdk.ShipmentResult{RemoteID: remoteID, Status: "created", Cost: sdk.LogisticsMoney{Currency: "RUB"}, TrackingNumber: trackingNumber, ObservedAt: time.Now().UTC()}, nil
}
func (transport cdekHTTP) Cancel(ctx context.Context, secret []byte, request sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error) {
	remoteID := strings.TrimSpace(request.RemoteID)
	if transport.h == nil || remoteID == "" || !cdekRemotePattern.MatchString(remoteID) || request.IdempotencyKey == "" || !safeCodePattern.MatchString(request.IdempotencyKey) {
		return sdk.ShipmentResult{}, errors.New("СДЭК cancellation request is unavailable")
	}
	token, err := transport.accessToken(ctx, secret)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	headers := http.Header{"Authorization": []string{"Bearer " + token}, "Accept": []string{"application/json"}}
	uuid := remoteID
	if !cdekUUIDPattern.MatchString(uuid) {
		status, body, _, _, _, requestErr := transport.h.do(ctx, http.MethodGet, "api.cdek.ru", "/v2/orders", url.Values{"cdek_number": []string{remoteID}}, nil, headers, nil, nil)
		if requestErr != nil {
			return sdk.ShipmentResult{}, requestErr
		}
		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			return sdk.ShipmentResult{}, fmt.Errorf("СДЭК cancellation lookup rejected with status %d", status)
		}
		var response cdekOrderEnvelope
		if json.Unmarshal(body, &response) != nil || !cdekUUIDPattern.MatchString(strings.TrimSpace(response.Entity.UUID)) {
			return sdk.ShipmentResult{}, errors.New("СДЭК cancellation lookup returned no UUID")
		}
		if number := cdekScalarText(response.Entity.CDEKNumber); number != "" && number != remoteID {
			return sdk.ShipmentResult{}, errors.New("СДЭК cancellation lookup identifier mismatch")
		}
		uuid = strings.TrimSpace(response.Entity.UUID)
	}
	status, body, _, _, _, err := transport.h.do(ctx, http.MethodDelete, "api.cdek.ru", "/v2/orders/"+uuid, url.Values{}, nil, headers, nil, nil)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.ShipmentResult{}, fmt.Errorf("СДЭК cancellation rejected with status %d", status)
	}
	if len(bytes.TrimSpace(body)) > 0 {
		var response cdekOrderEnvelope
		if json.Unmarshal(body, &response) != nil {
			return sdk.ShipmentResult{}, errors.New("СДЭК cancellation response rejected")
		}
		if response.Entity.UUID != "" && strings.TrimSpace(response.Entity.UUID) != uuid {
			return sdk.ShipmentResult{}, errors.New("СДЭК cancellation response identifier mismatch")
		}
	}
	return sdk.ShipmentResult{RemoteID: remoteID, Status: "cancelled", Cost: sdk.LogisticsMoney{Currency: "RUB"}, TrackingNumber: remoteID, ObservedAt: time.Now().UTC()}, nil
}
func (transport cdekHTTP) Return(ctx context.Context, secret []byte, request sdk.ReturnCreateRequest) (sdk.ShipmentResult, error) {
	remoteID := strings.TrimSpace(request.OriginalRemoteID)
	if transport.h == nil || request.Validate() != nil || (request.MailType != "refusal" && (request.MailType != "client_return" || request.TariffCode < 1)) || remoteID == "" || !cdekRemotePattern.MatchString(remoteID) {
		return sdk.ShipmentResult{}, errors.New("СДЭК return request is unavailable")
	}
	token, err := transport.accessToken(ctx, secret)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	headers := http.Header{"Authorization": []string{"Bearer " + token}, "Accept": []string{"application/json"}}
	uuid := remoteID
	if !cdekUUIDPattern.MatchString(uuid) {
		status, body, _, _, _, requestErr := transport.h.do(ctx, http.MethodGet, "api.cdek.ru", "/v2/orders", url.Values{"cdek_number": []string{remoteID}}, nil, headers, nil, nil)
		if requestErr != nil {
			return sdk.ShipmentResult{}, requestErr
		}
		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			return sdk.ShipmentResult{}, fmt.Errorf("СДЭК refusal lookup rejected with status %d", status)
		}
		var response cdekOrderEnvelope
		if json.Unmarshal(body, &response) != nil || !cdekUUIDPattern.MatchString(strings.TrimSpace(response.Entity.UUID)) {
			return sdk.ShipmentResult{}, errors.New("СДЭК refusal lookup returned no UUID")
		}
		if number := cdekScalarText(response.Entity.CDEKNumber); number != "" && number != remoteID {
			return sdk.ShipmentResult{}, errors.New("СДЭК refusal lookup identifier mismatch")
		}
		uuid = strings.TrimSpace(response.Entity.UUID)
	}
	path := "/v2/orders/" + uuid + "/refusal"
	var requestBody []byte
	if request.MailType == "client_return" {
		requestBody, err = json.Marshal(struct {
			TariffCode int `json:"tariff_code"`
		}{TariffCode: request.TariffCode})
		if err != nil {
			return sdk.ShipmentResult{}, errors.New("СДЭК client return request could not be encoded")
		}
		path = "/v2/orders/" + uuid + "/clientReturn"
	}
	status, body, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.cdek.ru", path, url.Values{}, requestBody, headers, nil, nil)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.ShipmentResult{}, fmt.Errorf("СДЭК return request rejected with status %d", status)
	}
	var response cdekCreateResponse
	if json.Unmarshal(body, &response) != nil || strings.TrimSpace(response.Entity.UUID) != uuid {
		return sdk.ShipmentResult{}, errors.New("СДЭК return response rejected")
	}
	canonicalRemoteID := cdekScalarText(response.Entity.CDEKNumber)
	if canonicalRemoteID == "" {
		canonicalRemoteID = remoteID
	}
	if !cdekRemotePattern.MatchString(canonicalRemoteID) {
		return sdk.ShipmentResult{}, errors.New("СДЭК return response has no valid identifier")
	}
	return sdk.ShipmentResult{RemoteID: canonicalRemoteID, Status: "created", Cost: sdk.LogisticsMoney{Currency: "RUB"}, TrackingNumber: canonicalRemoteID, ObservedAt: time.Now().UTC()}, nil
}
func (transport cdekHTTP) Label(ctx context.Context, secret []byte, request sdk.LabelRequest) (sdk.LabelResult, error) {
	remoteID := strings.TrimSpace(request.RemoteID)
	if transport.h == nil || request.Validate() != nil || remoteID == "" {
		return sdk.LabelResult{}, errors.New("СДЭК label request is unavailable")
	}
	format, err := cdekLabelFormat(request.Format)
	if err != nil {
		return sdk.LabelResult{}, err
	}
	token, err := transport.accessToken(ctx, secret)
	if err != nil {
		return sdk.LabelResult{}, err
	}
	headers := http.Header{"Authorization": []string{"Bearer " + token}, "Accept": []string{"application/json"}}
	uuid := remoteID
	if !cdekUUIDPattern.MatchString(uuid) {
		status, body, _, _, _, requestErr := transport.h.do(ctx, http.MethodGet, "api.cdek.ru", "/v2/orders", url.Values{"cdek_number": []string{remoteID}}, nil, headers, nil, nil)
		if requestErr != nil {
			return sdk.LabelResult{}, requestErr
		}
		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			return sdk.LabelResult{}, fmt.Errorf("СДЭК label lookup rejected with status %d", status)
		}
		var response cdekOrderEnvelope
		if json.Unmarshal(body, &response) != nil || !cdekUUIDPattern.MatchString(strings.TrimSpace(response.Entity.UUID)) {
			return sdk.LabelResult{}, errors.New("СДЭК label lookup returned no UUID")
		}
		if number := cdekScalarText(response.Entity.CDEKNumber); number != "" && number != remoteID {
			return sdk.LabelResult{}, errors.New("СДЭК label lookup identifier mismatch")
		}
		uuid = strings.TrimSpace(response.Entity.UUID)
	}
	body, err := json.Marshal(cdekPrintRequest{Orders: []cdekPrintOrder{{OrderUUID: uuid}}, CopyCount: 1, Format: format})
	if err != nil {
		return sdk.LabelResult{}, err
	}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.cdek.ru", "/v2/print/barcodes", url.Values{}, body, http.Header{"Authorization": []string{"Bearer " + token}, "Content-Type": []string{"application/json"}, "Accept": []string{"application/json"}}, nil, nil)
	if err != nil {
		return sdk.LabelResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.LabelResult{}, fmt.Errorf("СДЭК label request rejected with status %d", status)
	}
	var response cdekPrintResponse
	if json.Unmarshal(responseBody, &response) != nil || !cdekUUIDPattern.MatchString(strings.TrimSpace(response.Entity.UUID)) {
		return sdk.LabelResult{}, errors.New("СДЭК label response rejected")
	}
	return sdk.LabelResult{ArtifactRef: "cdek:print:barcode:" + strings.TrimSpace(response.Entity.UUID), MediaType: "application/pdf", ObservedAt: time.Now().UTC()}, nil
}
func (transport cdekHTTP) Webhook(ctx context.Context, secret, body, _ []byte) (sdk.LogisticsWebhook, error) {
	if transport.h == nil || len(body) < 2 || len(body) > 2<<20 || !json.Valid(body) {
		return sdk.LogisticsWebhook{}, errors.New("СДЭК webhook body is unavailable")
	}
	var event cdekWebhookEvent
	if json.Unmarshal(body, &event) != nil || strings.TrimSpace(event.Type) != "ORDER_STATUS" || !cdekUUIDPattern.MatchString(strings.TrimSpace(event.UUID)) {
		return sdk.LogisticsWebhook{}, errors.New("СДЭК webhook event is not an order status")
	}
	remoteID := cdekScalarText(event.Attrs.CDEKNumber)
	if remoteID == "" {
		remoteID = cdekScalarText(event.Attrs.Number)
	}
	if !cdekRemotePattern.MatchString(remoteID) {
		return sdk.LogisticsWebhook{}, errors.New("СДЭК webhook has no valid order identifier")
	}
	// CDEK's callback payload is an event hint, not an authentication proof.
	// Re-fetching the order with the account OAuth token is the authoritative
	// verification step; status and timestamp below never come from the body.
	tracked, err := transport.Track(ctx, secret, sdk.ShipmentStatusRequest{RemoteID: remoteID})
	if err != nil {
		return sdk.LogisticsWebhook{}, err
	}
	if tracked.RemoteID != remoteID && tracked.TrackingNumber != remoteID {
		return sdk.LogisticsWebhook{}, errors.New("СДЭК webhook order verification mismatch")
	}
	return sdk.LogisticsWebhook{DeliveryID: strings.TrimSpace(event.UUID), RemoteID: remoteID, Status: tracked.Status, OccurredAt: tracked.ObservedAt.UTC()}, nil
}

var _ cdek.Transport = cdekHTTP{}

// pekHTTP probes and reads the official ПЭК personal-cabinet API with the
// documented Basic login/access-key pair. Branch lookup and the bounded
// calculator preview are read-only. Runtime writes are limited to one
// self-delivery preregistration or one cancellation of a preregistration.
type pekHTTP struct{ h *httpTransport }

type pekCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func readPekCredentials(secret []byte) (pekCredentials, error) {
	var credentials pekCredentials
	if decodeStrict(secret, &credentials) != nil || strings.TrimSpace(credentials.Username) == "" || credentials.Password == "" {
		return pekCredentials{}, errors.New("ПЭК credentials must be JSON with username and password")
	}
	return credentials, nil
}

func (transport pekHTTP) Ping(ctx context.Context, secret []byte) error {
	if transport.h == nil {
		return errors.New("ПЭК credential probe unavailable")
	}
	credentials, err := readPekCredentials(secret)
	if err != nil {
		return err
	}
	headers := http.Header{"Content-Type": []string{"application/json;charset=utf-8"}, "Accept": []string{"application/json"}}
	status, _, _, _, _, err := transport.h.do(ctx, http.MethodPost, "kabinet.pecom.ru", "/api/v1/branches/all/", url.Values{}, []byte(`{}`), headers, []byte(credentials.Username), []byte(credentials.Password))
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("ПЭК credential probe rejected with status %d", status)
	}
	return nil
}

type pekDirectoryOperation struct {
	Operations []string `json:"operations"`
}

type pekDirectoryWarehouse struct {
	ID                    string                  `json:"id"`
	Name                  string                  `json:"name"`
	DivisionName          string                  `json:"divisionName"`
	Address               string                  `json:"address"`
	AddressDivision       string                  `json:"addressDivision"`
	DepartmentClosingDate string                  `json:"departmentClosingDate"`
	KindsOfTransportation []pekDirectoryOperation `json:"kindsOfTransportation"`
}

type pekDirectoryDivision struct {
	ID                    string                  `json:"id"`
	Name                  string                  `json:"name"`
	DepartmentTypeID      int                     `json:"departmentTypeId"`
	DepartmentType        string                  `json:"departmentType"`
	DepartmentClosingDate string                  `json:"departmentClosingDate"`
	KindsOfTransportation []pekDirectoryOperation `json:"kindsOfTransportation"`
	Warehouses            []pekDirectoryWarehouse `json:"warehouses"`
}

type pekDirectoryCity struct {
	Title     string   `json:"title"`
	Divisions []string `json:"divisions"`
}

type pekDirectoryBranch struct {
	Title     string                 `json:"title"`
	Country   string                 `json:"country"`
	Cities    []pekDirectoryCity     `json:"cities"`
	Divisions []pekDirectoryDivision `json:"divisions"`
}

type pekDirectoryResponse struct {
	Branches []pekDirectoryBranch `json:"branches"`
}

func pekHasPickupOperation(operations []pekDirectoryOperation) bool {
	for _, kind := range operations {
		for _, operation := range kind.Operations {
			if strings.Contains(strings.ToLower(strings.TrimSpace(operation)), "выда") {
				return true
			}
		}
	}
	return false
}

func (transport pekHTTP) Pickup(ctx context.Context, secret []byte, query sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	if transport.h == nil || query.Validate(500) != nil || query.Country != "RU" {
		return nil, errors.New("ПЭК pickup-point request is unavailable")
	}
	credentials, err := readPekCredentials(secret)
	if err != nil {
		return nil, err
	}
	headers := http.Header{"Content-Type": []string{"application/json;charset=utf-8"}, "Accept": []string{"application/json"}}
	status, body, _, _, _, err := transport.h.do(ctx, http.MethodPost, "kabinet.pecom.ru", "/api/v1/branches/all/", url.Values{}, []byte(`{}`), headers, []byte(credentials.Username), []byte(credentials.Password))
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("ПЭК branch directory request rejected with status %d", status)
	}
	var directory pekDirectoryResponse
	if json.Unmarshal(body, &directory) != nil || len(directory.Branches) > 10000 {
		return nil, errors.New("ПЭК branch directory response rejected")
	}

	wantedCity := strings.TrimSpace(query.City)
	points := make([]sdk.PickupPoint, 0, query.Limit)
	now := time.Now().UTC()
	for _, branch := range directory.Branches {
		if len(branch.Cities) > 10000 || len(branch.Divisions) > 10000 {
			return nil, errors.New("ПЭК branch directory response exceeds bounds")
		}
		divisionIDs := make(map[string]struct{})
		matchedCity := ""
		cityRecordMatched := false
		for _, city := range branch.Cities {
			if strings.EqualFold(strings.TrimSpace(city.Title), wantedCity) {
				cityRecordMatched = true
				matchedCity = strings.TrimSpace(city.Title)
				for _, divisionID := range city.Divisions {
					if divisionID = strings.TrimSpace(divisionID); divisionID != "" {
						divisionIDs[divisionID] = struct{}{}
					}
				}
			}
		}
		if matchedCity == "" && strings.EqualFold(strings.TrimSpace(branch.Title), wantedCity) {
			matchedCity = strings.TrimSpace(branch.Title)
		}
		if matchedCity == "" {
			continue
		}
		for _, division := range branch.Divisions {
			if len(points) >= query.Limit {
				return points, nil
			}
			if len(division.Warehouses) > 10000 {
				return nil, errors.New("ПЭК branch directory response exceeds bounds")
			}
			if cityRecordMatched {
				if _, ok := divisionIDs[strings.TrimSpace(division.ID)]; !ok {
					continue
				}
			}
			divisionHasPickup := pekHasPickupOperation(division.KindsOfTransportation)
			if !divisionHasPickup && len(division.KindsOfTransportation) > 0 {
				continue
			}
			for _, warehouse := range division.Warehouses {
				if len(points) >= query.Limit {
					return points, nil
				}
				if !divisionHasPickup && !pekHasPickupOperation(warehouse.KindsOfTransportation) {
					continue
				}
				remoteID := strings.TrimSpace(warehouse.ID)
				address := strings.TrimSpace(warehouse.AddressDivision)
				if address == "" {
					address = strings.TrimSpace(warehouse.Address)
				}
				if remoteID == "" || address == "" {
					return nil, errors.New("ПЭК branch directory has incomplete pickup point")
				}
				name := strings.TrimSpace(warehouse.Name)
				if name == "" {
					name = strings.TrimSpace(warehouse.DivisionName)
				}
				if name == "" {
					name = strings.TrimSpace(division.Name)
				}
				if name == "" {
					name = "ПЭК · отделение " + remoteID
				}
				active := strings.TrimSpace(division.DepartmentClosingDate) == "" && strings.TrimSpace(warehouse.DepartmentClosingDate) == ""
				points = append(points, sdk.PickupPoint{RemoteID: remoteID, Name: name, Country: query.Country, City: matchedCity, Address: address, Active: active, UpdatedAt: now})
			}
		}
	}
	return points, nil
}

type pekRateDecimal string

func (value pekRateDecimal) MarshalJSON() ([]byte, error) {
	if !pekDecimalPattern.MatchString(string(value)) {
		return nil, errors.New("ПЭК calculator decimal is invalid")
	}
	return []byte(value), nil
}

type pekRateCargo struct {
	Length pekRateDecimal `json:"length"`
	Width  pekRateDecimal `json:"width"`
	Height pekRateDecimal `json:"height"`
	Weight pekRateDecimal `json:"weight"`
}

type pekRateRequest struct {
	CurrencyCode        string         `json:"currencyCode"`
	Types               []int          `json:"types"`
	SenderWarehouseID   string         `json:"senderWarehouseId"`
	ReceiverWarehouseID string         `json:"receiverWarehouseId"`
	IsOpenCarSender     bool           `json:"isOpenCarSender"`
	IsOpenCarReceiver   bool           `json:"isOpenCarReceiver"`
	IsHyperMarket       bool           `json:"isHyperMarket"`
	IsInsurance         bool           `json:"isInsurance"`
	IsPickUp            bool           `json:"isPickUp"`
	IsDelivery          bool           `json:"isDelivery"`
	Cargos              []pekRateCargo `json:"cargos"`
}

type pekRateTransfer struct {
	Type            int             `json:"type"`
	HasError        bool            `json:"hasError"`
	ErrorMessage    string          `json:"errorMessage"`
	CostTotal       json.RawMessage `json:"costTotal"`
	EstDeliveryTime int             `json:"estDeliveryTime"`
}

type pekRateResponse struct {
	HasError     bool              `json:"hasError"`
	ErrorMessage string            `json:"errorMessage"`
	Transfers    []pekRateTransfer `json:"transfers"`
}

type pekBasicStatusCargo struct {
	Info struct {
		CargoStatus string `json:"cargoStatus"`
	} `json:"info"`
	Cargo struct {
		Code string `json:"code"`
	} `json:"cargo"`
}

type pekBasicStatusResponse struct {
	Cargos []pekBasicStatusCargo `json:"cargos"`
}

type pekCancellationResult struct {
	Code        string `json:"code"`
	Success     bool   `json:"success"`
	Description string `json:"description"`
}

type pekPrintResponse string

const pekTrackingResponseLimit = 50

type pekPreregistrationRequest struct {
	Common pekPreregistrationCommon  `json:"common"`
	Sender pekPreregistrationParty   `json:"sender"`
	Cargos []pekPreregistrationCargo `json:"cargos"`
}

type pekPreregistrationCommon struct {
	DocflowType string `json:"docflowType"`
	OrderType   int    `json:"orderType"`
}

type pekPreregistrationParty struct {
	WarehouseID             string           `json:"warehouseId,omitempty"`
	AddressStock            string           `json:"addressStock,omitempty"`
	CountryRegistrationCode string           `json:"countryOfRegistrationCode"`
	LegalForm               int              `json:"legalForm"`
	INN                     string           `json:"inn,omitempty"`
	KPP                     string           `json:"kpp,omitempty"`
	Title                   string           `json:"title"`
	Person                  string           `json:"person"`
	PersonPhones            []pekPersonPhone `json:"personPhones"`
	Email                   string           `json:"email,omitempty"`
}

type pekPersonPhone struct {
	Phone string `json:"phone"`
}

type pekPreregistrationCargo struct {
	Common   pekPreregistrationCargoCommon `json:"common"`
	Receiver pekPreregistrationParty       `json:"receiver"`
}

type pekPreregistrationCargoCommon struct {
	CustomerCorrelation string         `json:"customerCorrelation"`
	Type                int            `json:"type"`
	PositionsCount      int            `json:"positionsCount"`
	Weight              pekRateDecimal `json:"weight"`
	Volume              pekRateDecimal `json:"volume"`
	Width               pekRateDecimal `json:"width"`
	Length              pekRateDecimal `json:"length"`
	Height              pekRateDecimal `json:"height"`
	Description         string         `json:"description"`
}

type pekPreregistrationResponse struct {
	DocumentID json.RawMessage `json:"documentId"`
	Cargos     []struct {
		CargoCode string `json:"cargoCode"`
	} `json:"cargos"`
}

func pekTrackingStatus(value string) string {
	value = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
	switch {
	case strings.HasPrefix(value, "выдан"), strings.Contains(value, "доставлен получателю"):
		return "delivered"
	case strings.Contains(value, "возвращен отправителю"):
		return "returned_to_sender"
	case strings.Contains(value, "отправлен на возврат"):
		return "returned"
	case strings.Contains(value, "аннулирован"):
		return "cancelled"
	case strings.Contains(value, "утилизирован"):
		return "scrap"
	case strings.Contains(value, "изъят на таможне"):
		return "needs_attention"
	case strings.Contains(value, "адресная доставка"):
		return "on_delivery"
	case strings.Contains(value, "в пути"), strings.Contains(value, "принят к перевозке"), strings.Contains(value, "принят на пвз"), strings.Contains(value, "прибыл"), strings.Contains(value, "разгружается"):
		return "in_transit"
	case strings.Contains(value, "заявка на забор"), strings.Contains(value, "ожидается передача"), strings.Contains(value, "оформлен"):
		return "pending"
	default:
		return "unknown"
	}
}

func pekScaledDecimal(value, scale int64) pekRateDecimal {
	whole := strconv.FormatInt(value/scale, 10)
	remainder := value % scale
	if remainder == 0 {
		return pekRateDecimal(whole)
	}
	fraction := strconv.FormatInt(remainder, 10)
	width := len(strconv.FormatInt(scale-1, 10))
	if len(fraction) < width {
		fraction = strings.Repeat("0", width-len(fraction)) + fraction
	}
	return pekRateDecimal(whole + "." + fraction)
}

func pekRateWarehouse(ctx context.Context, transport pekHTTP, secret []byte, city string) (string, error) {
	points, err := transport.Pickup(ctx, secret, sdk.PickupPointQuery{Country: "RU", City: city, Limit: 50})
	if err != nil {
		return "", err
	}
	for _, point := range points {
		if point.Active && point.RemoteID != "" {
			return point.RemoteID, nil
		}
	}
	return "", errors.New("ПЭК calculator city has no active warehouse")
}

func (transport pekHTTP) Rates(ctx context.Context, secret []byte, request sdk.RateRequest) ([]sdk.RateQuote, error) {
	if transport.h == nil || request.Validate() != nil || !strings.EqualFold(request.From.Country, "RU") || !strings.EqualFold(request.To.Country, "RU") {
		return nil, errors.New("ПЭК tariff request is unavailable")
	}
	credentials, err := readPekCredentials(secret)
	if err != nil {
		return nil, err
	}
	senderWarehouseID, err := pekRateWarehouse(ctx, transport, secret, request.From.City)
	if err != nil {
		return nil, err
	}
	receiverWarehouseID, err := pekRateWarehouse(ctx, transport, secret, request.To.City)
	if err != nil {
		return nil, err
	}
	cargos := make([]pekRateCargo, 0, len(request.Parcels))
	for _, parcel := range request.Parcels {
		cargos = append(cargos, pekRateCargo{
			Length: pekScaledDecimal(parcel.LengthMM, 1000),
			Width:  pekScaledDecimal(parcel.WidthMM, 1000),
			Height: pekScaledDecimal(parcel.HeightMM, 1000),
			Weight: pekScaledDecimal(parcel.WeightGrams, 1000),
		})
	}
	body, err := json.Marshal(pekRateRequest{
		CurrencyCode:        "643",
		Types:               []int{3},
		SenderWarehouseID:   senderWarehouseID,
		ReceiverWarehouseID: receiverWarehouseID,
		Cargos:              cargos,
	})
	if err != nil {
		return nil, err
	}
	headers := http.Header{"Content-Type": []string{"application/json;charset=utf-8"}, "Accept": []string{"application/json"}}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodPost, "kabinet.pecom.ru", "/api/v1/calculator/calculateprice/", url.Values{}, body, headers, []byte(credentials.Username), []byte(credentials.Password))
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("ПЭК calculator request rejected with status %d", status)
	}
	var response pekRateResponse
	if json.Unmarshal(responseBody, &response) != nil || len(response.Transfers) > 100 {
		return nil, errors.New("ПЭК calculator response rejected")
	}
	now := time.Now().UTC()
	quotes := make([]sdk.RateQuote, 0, len(response.Transfers))
	seen := make(map[string]struct{}, len(response.Transfers))
	for _, transfer := range response.Transfers {
		if transfer.HasError {
			continue
		}
		if transfer.Type < 1 || transfer.Type > 10000 || transfer.EstDeliveryTime < 0 || transfer.EstDeliveryTime > 3660 {
			return nil, errors.New("ПЭК calculator response has invalid transfer")
		}
		serviceCode := "pek_type_" + strconv.Itoa(transfer.Type)
		if !safeCodePattern.MatchString(serviceCode) {
			return nil, errors.New("ПЭК calculator response has invalid transfer identifier")
		}
		if _, duplicate := seen[serviceCode]; duplicate {
			return nil, errors.New("ПЭК calculator response has duplicate transfers")
		}
		seen[serviceCode] = struct{}{}
		minorUnits, err := cdekMinorUnits(transfer.CostTotal)
		if err != nil {
			return nil, errors.New("ПЭК calculator response has invalid cost")
		}
		at := now.Add(time.Duration(transfer.EstDeliveryTime) * 24 * time.Hour)
		quotes = append(quotes, sdk.RateQuote{ServiceCode: serviceCode, Cost: sdk.LogisticsMoney{MinorUnits: minorUnits, Currency: "RUB"}, MinDeliveryAt: at, MaxDeliveryAt: at, ObservedAt: now})
	}
	if len(quotes) == 0 {
		return nil, errors.New("ПЭК calculator returned no applicable transfer")
	}
	return quotes, nil
}

func (transport pekHTTP) Track(ctx context.Context, secret []byte, request sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	remoteID := strings.TrimSpace(request.RemoteID)
	if transport.h == nil || remoteID == "" || !cdekRemotePattern.MatchString(remoteID) {
		return sdk.ShipmentResult{}, errors.New("ПЭК tracking request is unavailable")
	}
	credentials, err := readPekCredentials(secret)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	body, err := json.Marshal(struct {
		CargoCodes []string `json:"cargoCodes"`
	}{CargoCodes: []string{remoteID}})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	headers := http.Header{"Content-Type": []string{"application/json;charset=utf-8"}, "Accept": []string{"application/json"}}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodPost, "kabinet.pecom.ru", "/api/v1/cargos/basicstatus/", url.Values{}, body, headers, []byte(credentials.Username), []byte(credentials.Password))
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.ShipmentResult{}, fmt.Errorf("ПЭК tracking request rejected with status %d", status)
	}
	var response pekBasicStatusResponse
	if json.Unmarshal(responseBody, &response) != nil || len(response.Cargos) == 0 || len(response.Cargos) > pekTrackingResponseLimit {
		return sdk.ShipmentResult{}, errors.New("ПЭК tracking response rejected")
	}
	var cargo pekBasicStatusCargo
	for _, candidate := range response.Cargos {
		if strings.TrimSpace(candidate.Cargo.Code) == remoteID {
			cargo = candidate
			break
		}
	}
	if strings.TrimSpace(cargo.Cargo.Code) == "" {
		return sdk.ShipmentResult{}, errors.New("ПЭК tracking response has no matching cargo")
	}
	statusCode := pekTrackingStatus(cargo.Info.CargoStatus)
	canonicalRemoteID := strings.TrimSpace(cargo.Cargo.Code)
	if !cdekRemotePattern.MatchString(canonicalRemoteID) {
		return sdk.ShipmentResult{}, errors.New("ПЭК tracking response has invalid cargo identifier")
	}
	return sdk.ShipmentResult{
		RemoteID:       canonicalRemoteID,
		Status:         statusCode,
		Cost:           sdk.LogisticsMoney{Currency: "RUB"},
		TrackingNumber: canonicalRemoteID,
		ObservedAt:     time.Now().UTC(),
	}, nil
}

func (transport pekHTTP) Create(ctx context.Context, secret []byte, request sdk.ShipmentCreateRequest, configuration pek.Configuration) (sdk.ShipmentResult, error) {
	if transport.h == nil || request.Validate() != nil || configuration.Validate() != nil || len(request.Parcels) > 50 || !strings.EqualFold(request.From.Country, "RU") || !strings.EqualFold(request.To.Country, "RU") || request.ServiceCode != "pek_type_3" || !validPekContact(request.Sender) || !validPekContact(request.Recipient) {
		return sdk.ShipmentResult{}, errors.New("ПЭК preregistration request is unavailable")
	}
	if request.PickupPointRef != "" && !pekRuntimeWarehouseIDPattern.MatchString(request.PickupPointRef) {
		return sdk.ShipmentResult{}, errors.New("ПЭК receiver warehouse is invalid")
	}
	credentials, err := readPekCredentials(secret)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	mass, volume, width, length, height, err := pekShipmentMeasures(request.Parcels)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	receiver := pekPreregistrationParty{
		CountryRegistrationCode: "643",
		LegalForm:               3,
		Title:                   request.Recipient.Name,
		Person:                  request.Recipient.Name,
		PersonPhones:            []pekPersonPhone{{Phone: request.Recipient.Phone}},
		Email:                   request.Recipient.Email,
	}
	if request.PickupPointRef != "" {
		receiver.WarehouseID = request.PickupPointRef
	} else {
		receiver.AddressStock = strings.Join([]string{request.To.Country, request.To.PostalCode, request.To.City, request.To.Line1}, ", ")
	}
	sender := pekPreregistrationParty{
		WarehouseID:             configuration.SenderWarehouseID,
		CountryRegistrationCode: "643",
		LegalForm:               configuration.SenderLegalForm,
		INN:                     configuration.SenderINN,
		KPP:                     configuration.SenderKPP,
		Title:                   configuration.SenderTitle,
		Person:                  request.Sender.Name,
		PersonPhones:            []pekPersonPhone{{Phone: request.Sender.Phone}},
		Email:                   request.Sender.Email,
	}
	body, err := json.Marshal(pekPreregistrationRequest{
		Common: pekPreregistrationCommon{DocflowType: "FFS", OrderType: 0},
		Sender: sender,
		Cargos: []pekPreregistrationCargo{{
			Common:   pekPreregistrationCargoCommon{CustomerCorrelation: request.ExternalID, Type: 3, PositionsCount: len(request.Parcels), Weight: pekRateDecimal(pekScaledDecimal(mass, 1000)), Volume: pekScaledDecimal(volume, 1000), Width: width, Length: length, Height: height, Description: "Заказ " + request.ExternalID},
			Receiver: receiver,
		}},
	})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodPost, "kabinet.pecom.ru", "/api/v1/preregistration/submit/", url.Values{}, body, http.Header{"Content-Type": []string{"application/json;charset=utf-8"}, "Accept": []string{"application/json"}}, []byte(credentials.Username), []byte(credentials.Password))
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.ShipmentResult{}, fmt.Errorf("ПЭК preregistration request rejected with status %d", status)
	}
	var response pekPreregistrationResponse
	if json.Unmarshal(responseBody, &response) != nil || !pekDocumentID(response.DocumentID) || len(response.Cargos) != 1 || !pekRuntimeCargoCodePattern.MatchString(strings.TrimSpace(response.Cargos[0].CargoCode)) {
		return sdk.ShipmentResult{}, errors.New("ПЭК preregistration response rejected")
	}
	remoteID := strings.TrimSpace(response.Cargos[0].CargoCode)
	return sdk.ShipmentResult{RemoteID: remoteID, Status: "created", Cost: sdk.LogisticsMoney{Currency: "RUB"}, TrackingNumber: remoteID, ObservedAt: time.Now().UTC()}, nil
}

func pekDocumentID(raw json.RawMessage) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var number json.Number
	if decoder.Decode(&number) == nil {
		if value, err := strconv.ParseInt(number.String(), 10, 64); err == nil && value > 0 {
			return true
		}
	}
	var text string
	return json.Unmarshal(raw, &text) == nil && safeCodePattern.MatchString(strings.TrimSpace(text))
}

func validPekContact(contact sdk.LogisticsContact) bool {
	name := strings.TrimSpace(contact.Name)
	phone := strings.TrimSpace(contact.Phone)
	return name != "" && name == contact.Name && len(name) <= 255 && phone != "" && phone == contact.Phone && cdekPhonePattern.MatchString(phone) && len(contact.Email) <= 254 && !strings.ContainsAny(contact.Email, "\r\n\t ")
}

func pekShipmentMeasures(parcels []sdk.Parcel) (mass, volumeUnits int64, width, length, height pekRateDecimal, err error) {
	if len(parcels) == 0 {
		return 0, 0, "", "", "", errors.New("ПЭК preregistration requires a parcel")
	}
	const dimensionLimit int64 = 1_000_000
	for _, parcel := range parcels {
		if parcel.Validate() != nil || parcel.LengthMM > dimensionLimit || parcel.WidthMM > dimensionLimit || parcel.HeightMM > dimensionLimit || parcel.WeightGrams > (int64(1<<63-1)-mass) {
			return 0, 0, "", "", "", errors.New("ПЭК preregistration parcel is invalid")
		}
		mass += parcel.WeightGrams
		product := parcel.LengthMM * parcel.WidthMM
		if product > (int64(1<<63-1))/parcel.HeightMM {
			return 0, 0, "", "", "", errors.New("ПЭК preregistration volume is out of range")
		}
		product *= parcel.HeightMM
		units := (product + 999999) / 1000000
		if units > (int64(1<<63-1) - volumeUnits) {
			return 0, 0, "", "", "", errors.New("ПЭК preregistration volume is out of range")
		}
		volumeUnits += units
		if parcel.WidthMM > pekDecimalMillimetres(width) {
			width = pekScaledDecimal(parcel.WidthMM, 1000)
		}
		if parcel.LengthMM > pekDecimalMillimetres(length) {
			length = pekScaledDecimal(parcel.LengthMM, 1000)
		}
		if parcel.HeightMM > pekDecimalMillimetres(height) {
			height = pekScaledDecimal(parcel.HeightMM, 1000)
		}
	}
	return mass, volumeUnits, width, length, height, nil
}

func pekDecimalMillimetres(value pekRateDecimal) int64 {
	if value == "" {
		return 0
	}
	parts := strings.SplitN(string(value), ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole < 0 || whole > (int64(1)<<62) {
		return 0
	}
	if len(parts) == 1 {
		return whole * 1000
	}
	fraction := parts[1]
	if len(fraction) > 3 {
		return 0
	}
	for len(fraction) < 3 {
		fraction += "0"
	}
	minor, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil || whole > ((int64(1<<63-1)-minor)/1000) {
		return 0
	}
	return whole*1000 + minor
}

func (transport pekHTTP) Cancel(ctx context.Context, secret []byte, request sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error) {
	remoteID := strings.TrimSpace(request.RemoteID)
	if transport.h == nil || !pekRuntimeCargoCodePattern.MatchString(remoteID) || !safeCodePattern.MatchString(request.IdempotencyKey) {
		return sdk.ShipmentResult{}, errors.New("ПЭК cancellation request is unavailable")
	}
	credentials, err := readPekCredentials(secret)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	body, err := json.Marshal([]string{remoteID})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	headers := http.Header{"Content-Type": []string{"application/json;charset=utf-8"}, "Accept": []string{"application/json"}}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodPost, "kabinet.pecom.ru", "/api/v1/order/cancellation/", url.Values{}, body, headers, []byte(credentials.Username), []byte(credentials.Password))
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.ShipmentResult{}, fmt.Errorf("ПЭК cancellation request rejected with status %d", status)
	}
	var response []pekCancellationResult
	if json.Unmarshal(responseBody, &response) != nil || len(response) != 1 {
		return sdk.ShipmentResult{}, errors.New("ПЭК cancellation response rejected")
	}
	result := response[0]
	if strings.TrimSpace(result.Code) != remoteID || !result.Success {
		return sdk.ShipmentResult{}, errors.New("ПЭК cancellation response did not confirm the requested cargo")
	}
	return sdk.ShipmentResult{RemoteID: remoteID, Status: "cancelled", Cost: sdk.LogisticsMoney{Currency: "RUB"}, TrackingNumber: remoteID, ObservedAt: time.Now().UTC()}, nil
}

// Label requests the official single-cargo PDF label. The ПЭК endpoint
// returns base64 JSON; only an opaque content-addressed reference leaves the
// host transport, never the PDF body or provider credentials.
func (transport pekHTTP) Label(ctx context.Context, secret []byte, request sdk.LabelRequest) (sdk.LabelResult, error) {
	remoteID := strings.TrimSpace(request.RemoteID)
	format := strings.ToLower(strings.TrimSpace(request.Format))
	if transport.h == nil || !pekRuntimeCargoCodePattern.MatchString(remoteID) || (format != "pdf" && format != "request_pdf") {
		return sdk.LabelResult{}, errors.New("ПЭК label request is unavailable")
	}
	credentials, err := readPekCredentials(secret)
	if err != nil {
		return sdk.LabelResult{}, err
	}
	body, err := json.Marshal(struct {
		CargoIndex string `json:"cargoIndex"`
		Type       string `json:"type"`
	}{CargoIndex: remoteID, Type: map[string]string{"pdf": "simple", "request_pdf": "big"}[format]})
	if err != nil {
		return sdk.LabelResult{}, err
	}
	headers := http.Header{"Content-Type": []string{"application/json;charset=utf-8"}, "Accept": []string{"application/json"}}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodPost, "kabinet.pecom.ru", "/api/v1/order/print/", url.Values{}, body, headers, []byte(credentials.Username), []byte(credentials.Password))
	if err != nil {
		return sdk.LabelResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.LabelResult{}, fmt.Errorf("ПЭК label request rejected with status %d", status)
	}
	var encoded pekPrintResponse
	if json.Unmarshal(responseBody, &encoded) != nil || strings.TrimSpace(string(encoded)) == "" {
		return sdk.LabelResult{}, errors.New("ПЭК label response rejected")
	}
	pdf, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		pdf, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	}
	if err != nil || !bytes.HasPrefix(bytes.TrimSpace(pdf), []byte("%PDF-")) {
		return sdk.LabelResult{}, errors.New("ПЭК label response is not a PDF")
	}
	digest := sha256.Sum256(pdf)
	printType := map[string]string{"pdf": "simple", "request_pdf": "big"}[format]
	return sdk.LabelResult{ArtifactRef: "pek:print:" + printType + ":" + remoteID + ":" + hex.EncodeToString(digest[:]), MediaType: "application/pdf", ObservedAt: time.Now().UTC()}, nil
}

var _ pek.Transport = pekHTTP{}

// dellinHTTP validates an app key and personal access token by opening a
// short-lived Деловые Линии API session. The session ID is used only for the
// duration of the provider call and is never persisted or returned.
type dellinHTTP struct{ h *httpTransport }

type dellinCredentials struct {
	AppKey string `json:"appkey"`
	PAT    string `json:"pat"`
}

type dellinLoginResponse struct {
	Metadata struct {
		Status int `json:"status"`
	} `json:"metadata"`
	Data struct {
		SessionID string `json:"sessionID"`
	} `json:"data"`
}

func readDellinCredentials(secret []byte) (dellinCredentials, error) {
	var credentials dellinCredentials
	if decodeStrict(secret, &credentials) != nil || strings.TrimSpace(credentials.AppKey) == "" || strings.TrimSpace(credentials.PAT) == "" {
		return dellinCredentials{}, errors.New("Деловые Линии credentials must be JSON with appkey and pat")
	}
	return credentials, nil
}

func (transport dellinHTTP) login(ctx context.Context, credentials dellinCredentials) (string, error) {
	body, err := json.Marshal(credentials)
	if err != nil {
		return "", err
	}
	status, response, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.dellin.ru", "/v4/auth/login.json", url.Values{}, body, http.Header{"Content-Type": []string{"application/json"}, "Accept": []string{"application/json"}}, nil, nil)
	if err != nil {
		return "", err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return "", fmt.Errorf("Деловые Линии credential probe rejected with status %d", status)
	}
	var result dellinLoginResponse
	if json.Unmarshal(response, &result) != nil || (result.Metadata.Status != 0 && result.Metadata.Status != http.StatusOK) || strings.TrimSpace(result.Data.SessionID) == "" {
		return "", errors.New("Деловые Линии credential probe returned no session")
	}
	return strings.TrimSpace(result.Data.SessionID), nil
}

func (transport dellinHTTP) Ping(ctx context.Context, secret []byte) error {
	if transport.h == nil {
		return errors.New("Деловые Линии credential probe unavailable")
	}
	credentials, err := readDellinCredentials(secret)
	if err != nil {
		return err
	}
	_, err = transport.login(ctx, credentials)
	return err
}

type dellinCreateAddress struct {
	Search string `json:"search"`
}

type dellinCreateTime struct {
	WorktimeStart string `json:"worktimeStart"`
	WorktimeEnd   string `json:"worktimeEnd"`
}

type dellinCreateContactPerson struct {
	Name string `json:"name"`
}

type dellinCreatePhone struct {
	Number string `json:"number"`
}

type dellinCreateCounteragent struct {
	Form     string `json:"form"`
	IsAnonym bool   `json:"isAnonym"`
	Phone    string `json:"phone,omitempty"`
	Name     string `json:"name"`
}

type dellinCreateMember struct {
	CounteragentID int64                       `json:"counteragentID,omitempty"`
	Counteragent   *dellinCreateCounteragent   `json:"counteragent,omitempty"`
	ContactPersons []dellinCreateContactPerson `json:"contactPersons,omitempty"`
	PhoneNumbers   []dellinCreatePhone         `json:"phoneNumbers,omitempty"`
	Email          string                      `json:"email,omitempty"`
}

type dellinCreateRequest struct {
	AppKey    string `json:"appkey"`
	SessionID string `json:"sessionID"`
	InOrder   bool   `json:"inOrder"`
	Delivery  struct {
		DeliveryType struct {
			Type string `json:"type"`
		} `json:"deliveryType"`
		Derival struct {
			ProduceDate string              `json:"produceDate"`
			Variant     string              `json:"variant"`
			Payer       string              `json:"payer"`
			Address     dellinCreateAddress `json:"address"`
			Time        dellinCreateTime    `json:"time"`
		} `json:"derival"`
		Arrival struct {
			Variant string              `json:"variant"`
			Payer   string              `json:"payer"`
			Address dellinCreateAddress `json:"address"`
		} `json:"arrival"`
		Comment string `json:"comment,omitempty"`
	} `json:"delivery"`
	Members struct {
		Requester struct {
			Role  string `json:"role"`
			UID   string `json:"uid"`
			Email string `json:"email,omitempty"`
		} `json:"requester"`
		Sender   dellinCreateMember `json:"sender"`
		Receiver dellinCreateMember `json:"receiver"`
	} `json:"members"`
	Cargo struct {
		Quantity    int         `json:"quantity"`
		Length      json.Number `json:"length"`
		Width       json.Number `json:"width"`
		Height      json.Number `json:"height"`
		Weight      json.Number `json:"weight"`
		TotalVolume json.Number `json:"totalVolume"`
		TotalWeight json.Number `json:"totalWeight"`
		FreightUID  string      `json:"freightUID"`
	} `json:"cargo"`
	Payment struct {
		Type              string `json:"type"`
		PrimaryPayer      string `json:"primaryPayer"`
		PaymentCitySearch struct {
			Search string `json:"search"`
		} `json:"paymentCitySearch"`
	} `json:"payment"`
	CargoCode string `json:"cargoCode,omitempty"`
}

type dellinCreateResponse struct {
	Metadata struct {
		Status int `json:"status"`
	} `json:"metadata"`
	Data struct {
		State     string      `json:"state"`
		RequestID json.Number `json:"requestID"`
		Barcode   string      `json:"barcode"`
	} `json:"data"`
}

type dellinCancelResponse struct {
	Metadata struct {
		Status int `json:"status"`
	} `json:"metadata"`
	Data struct {
		Status string `json:"status"`
	} `json:"data"`
}

func dellinDeliveryType(serviceCode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(serviceCode)) {
	case "dellin_auto":
		return "auto", nil
	case "dellin_express":
		return "express", nil
	case "dellin_avia":
		return "avia", nil
	case "dellin_small":
		return "small", nil
	case "dellin_letter":
		return "letter", nil
	default:
		return "", errors.New("Деловые Линии shipment service is not supported")
	}
}

func dellinPhoneNumber(value string, anonymous bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value != strings.TrimSpace(value) {
		return "", errors.New("Деловые Линии phone is invalid")
	}
	var digits strings.Builder
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == '+' || r == '(' || r == ')' || r == '-' || r == ' ':
		default:
			return "", errors.New("Деловые Линии phone is invalid")
		}
	}
	number := digits.String()
	if anonymous {
		if len(number) != 11 || number[0] != '7' {
			return "", errors.New("Деловые Линии anonymous recipient phone is invalid")
		}
	} else if len(number) < 7 || len(number) > 15 {
		return "", errors.New("Деловые Линии phone is invalid")
	}
	return number, nil
}

func dellinContactName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value != strings.TrimSpace(value) || len(value) > 255 || strings.ContainsAny(value, "\r\n\t") {
		return "", errors.New("Деловые Линии contact name is invalid")
	}
	return value, nil
}

func dellinMaxParcelWeight(parcels []sdk.Parcel) (json.Number, error) {
	if len(parcels) == 0 || len(parcels) > 50 {
		return "", errors.New("Деловые Линии cargo quantity is out of bounds")
	}
	maxWeight := int64(0)
	for _, parcel := range parcels {
		if parcel.Validate() != nil {
			return "", errors.New("Деловые Линии cargo contains an invalid parcel")
		}
		if parcel.WeightGrams > maxWeight {
			maxWeight = parcel.WeightGrams
		}
	}
	return dellinFixedDecimal(big.NewInt(maxWeight), big.NewInt(1000), 3)
}

func dellinCreateResult(responseBody []byte) (sdk.ShipmentResult, error) {
	var response dellinCreateResponse
	if json.Unmarshal(responseBody, &response) != nil || (response.Metadata.Status != 0 && response.Metadata.Status != http.StatusOK) || strings.ToLower(strings.TrimSpace(response.Data.State)) != "success" {
		return sdk.ShipmentResult{}, errors.New("Деловые Линии shipment creation response rejected")
	}
	requestID, err := strconv.ParseInt(strings.TrimSpace(response.Data.RequestID.String()), 10, 64)
	if err != nil || requestID < 1 || !safeCodePattern.MatchString(strconv.FormatInt(requestID, 10)) {
		return sdk.ShipmentResult{}, errors.New("Деловые Линии shipment creation response has no valid request ID")
	}
	remoteID := strconv.FormatInt(requestID, 10)
	trackingNumber := strings.TrimSpace(response.Data.Barcode)
	if trackingNumber == "" {
		trackingNumber = remoteID
	}
	if !safeCodePattern.MatchString(trackingNumber) {
		return sdk.ShipmentResult{}, errors.New("Деловые Линии shipment creation response has invalid barcode")
	}
	return sdk.ShipmentResult{RemoteID: remoteID, Status: "created", TrackingNumber: trackingNumber, Cost: sdk.LogisticsMoney{Currency: "RUB"}, ObservedAt: time.Now().UTC()}, nil
}

func (transport dellinHTTP) Create(ctx context.Context, secret []byte, request sdk.ShipmentCreateRequest, configuration dellin.Configuration) (sdk.ShipmentResult, error) {
	if transport.h == nil || request.Validate() != nil || len(request.Parcels) > 50 || request.PickupPointRef != "" || configuration.Validate() != nil {
		return sdk.ShipmentResult{}, errors.New("Деловые Линии shipment creation request is unavailable")
	}
	deliveryType, err := dellinDeliveryType(request.ServiceCode)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	fromSearch, err := dellinAddressSearch(request.From)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	toSearch, err := dellinAddressSearch(request.To)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	length, width, height, totalWeight, totalVolume, err := dellinParcelTotals(request.Parcels)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	maxWeight, err := dellinMaxParcelWeight(request.Parcels)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	senderName, err := dellinContactName(request.Sender.Name)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	receiverName, err := dellinContactName(request.Recipient.Name)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	senderPhone, err := dellinPhoneNumber(request.Sender.Phone, false)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	receiverPhone, err := dellinPhoneNumber(request.Recipient.Phone, true)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	credentials, err := readDellinCredentials(secret)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	sessionID, err := transport.login(ctx, credentials)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	body := dellinCreateRequest{AppKey: credentials.AppKey, SessionID: sessionID, InOrder: true, CargoCode: request.ExternalID}
	body.Delivery.DeliveryType.Type = deliveryType
	body.Delivery.Derival.ProduceDate = configuration.ProduceDate
	body.Delivery.Derival.Variant = "address"
	body.Delivery.Derival.Payer = "sender"
	body.Delivery.Derival.Address = dellinCreateAddress{Search: fromSearch}
	body.Delivery.Derival.Time = dellinCreateTime{WorktimeStart: configuration.DerivalWorktimeStart, WorktimeEnd: configuration.DerivalWorktimeEnd}
	body.Delivery.Arrival.Variant = "address"
	body.Delivery.Arrival.Payer = "sender"
	body.Delivery.Arrival.Address = dellinCreateAddress{Search: toSearch}
	body.Delivery.Comment = "TORGNEXA " + request.ExternalID
	body.Members.Requester.Role = "sender"
	body.Members.Requester.UID = configuration.RequesterUID
	body.Members.Requester.Email = request.Sender.Email
	body.Members.Sender = dellinCreateMember{
		CounteragentID: configuration.SenderCounteragentID,
		ContactPersons: []dellinCreateContactPerson{{Name: senderName}},
		PhoneNumbers:   []dellinCreatePhone{{Number: senderPhone}},
		Email:          request.Sender.Email,
	}
	body.Members.Receiver = dellinCreateMember{Counteragent: &dellinCreateCounteragent{Form: "0xAB91FEEA04F6D4AD48DF42161B6C2E7A", IsAnonym: true, Phone: receiverPhone, Name: receiverName}}
	body.Cargo.Quantity = len(request.Parcels)
	body.Cargo.Length = length
	body.Cargo.Width = width
	body.Cargo.Height = height
	body.Cargo.Weight = maxWeight
	body.Cargo.TotalWeight = totalWeight
	body.Cargo.TotalVolume = totalVolume
	body.Cargo.FreightUID = configuration.FreightUID
	body.Payment.Type = configuration.PaymentType
	body.Payment.PrimaryPayer = "sender"
	body.Payment.PaymentCitySearch.Search = request.From.City
	requestBody, err := json.Marshal(body)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.dellin.ru", "/v2/request.json", url.Values{}, requestBody, http.Header{"Content-Type": []string{"application/json"}, "Accept": []string{"application/json"}}, nil, nil)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.ShipmentResult{}, fmt.Errorf("Деловые Линии shipment creation rejected with status %d", status)
	}
	return dellinCreateResult(responseBody)
}

type dellinDirectoryCity struct {
	Name      string `json:"name"`
	Terminals struct {
		Terminal []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Address      string `json:"address"`
			FullAddress  string `json:"fullAddress"`
			IsPVZ        bool   `json:"isPVZ"`
			GiveoutCargo bool   `json:"giveoutCargo"`
		} `json:"terminal"`
	} `json:"terminals"`
}

type dellinDirectoryResponse struct {
	Hash string `json:"hash"`
	URL  string `json:"url"`
}

type dellinTrackingEvent struct {
	Number             json.RawMessage `json:"number"`
	State              string          `json:"state"`
	StateDate          string          `json:"stateDate"`
	DetailedStatusDate string          `json:"detailedStatusDate"`
}

type dellinTrackingResponse struct {
	Metadata struct {
		Status int `json:"status"`
	} `json:"metadata"`
	Data struct {
		StatusHistory map[string][]dellinTrackingEvent `json:"statusHistory"`
	} `json:"data"`
}

type dellinPrintableDocument struct {
	UID    string   `json:"uid"`
	Base64 string   `json:"base64"`
	URLs   []string `json:"url"`
}

type dellinPrintableResponse struct {
	Metadata struct {
		Status int `json:"status"`
	} `json:"metadata"`
	Data []dellinPrintableDocument `json:"data"`
}

type dellinRateAddress struct {
	Variant string `json:"variant"`
	Address struct {
		Search string `json:"search"`
	} `json:"address"`
}

type dellinRateRequest struct {
	AppKey    string `json:"appkey"`
	SessionID string `json:"sessionID"`
	Delivery  struct {
		DeliveryType struct {
			Type string `json:"type"`
		} `json:"deliveryType"`
		Derival dellinRateAddress `json:"derival"`
		Arrival dellinRateAddress `json:"arrival"`
	} `json:"delivery"`
	Payment struct {
		PaymentCitySearch struct {
			Search string `json:"search"`
		} `json:"paymentCitySearch"`
		Type string `json:"type"`
	} `json:"payment"`
	Cargo struct {
		Quantity    int         `json:"quantity"`
		Length      json.Number `json:"length"`
		Width       json.Number `json:"width"`
		Height      json.Number `json:"height"`
		Weight      json.Number `json:"weight"`
		TotalVolume json.Number `json:"totalVolume"`
		TotalWeight json.Number `json:"totalWeight"`
		HazardClass int         `json:"hazardClass"`
	} `json:"cargo"`
}

type dellinRateResponse struct {
	Metadata struct {
		Status int `json:"status"`
	} `json:"metadata"`
	Data struct {
		Price        json.RawMessage `json:"price"`
		PriceMinimal string          `json:"priceMinimal"`
		DeliveryTerm int             `json:"deliveryTerm"`
		OrderDates   struct {
			Pickup                    string `json:"pickup"`
			ArrivalToOspReceiver      string `json:"arrivalToOspReceiver"`
			GiveoutFromOspReceiver    string `json:"giveoutFromOspReceiver"`
			GiveoutFromOspReceiverMax string `json:"giveoutFromOspReceiverMax"`
		} `json:"orderDates"`
	} `json:"data"`
}

func dellinAddressSearch(address sdk.Address) (string, error) {
	parts := make([]string, 0, 4)
	for _, part := range []string{address.Country, address.PostalCode, address.City, address.Line1} {
		if value := strings.TrimSpace(part); value != "" {
			parts = append(parts, value)
		}
	}
	result := strings.Join(parts, ", ")
	if len(result) < 2 || len(result) > 1024 {
		return "", errors.New("Деловые Линии address search is out of bounds")
	}
	return result, nil
}

func dellinFixedDecimal(numerator, denominator *big.Int, scale int) (json.Number, error) {
	if numerator == nil || denominator == nil || denominator.Sign() <= 0 || numerator.Sign() < 0 || scale < 0 || scale > 18 {
		return "", errors.New("Деловые Линии decimal value is invalid")
	}
	value := new(big.Rat).SetFrac(new(big.Int).Set(numerator), new(big.Int).Set(denominator))
	text := strings.TrimRight(strings.TrimRight(value.FloatString(scale), "0"), ".")
	if text == "" {
		text = "0"
	}
	return json.Number(text), nil
}

func dellinParcelTotals(parcels []sdk.Parcel) (length, width, height, weight, volume json.Number, err error) {
	if len(parcels) == 0 || len(parcels) > 50 {
		return "", "", "", "", "", errors.New("Деловые Линии cargo quantity is out of bounds")
	}
	maxLength, maxWidth, maxHeight := int64(0), int64(0), int64(0)
	totalWeight := new(big.Int)
	totalVolume := new(big.Int)
	for _, parcel := range parcels {
		if parcel.Validate() != nil {
			return "", "", "", "", "", errors.New("Деловые Линии cargo contains an invalid parcel")
		}
		if parcel.LengthMM > maxLength {
			maxLength = parcel.LengthMM
		}
		if parcel.WidthMM > maxWidth {
			maxWidth = parcel.WidthMM
		}
		if parcel.HeightMM > maxHeight {
			maxHeight = parcel.HeightMM
		}
		totalWeight.Add(totalWeight, big.NewInt(parcel.WeightGrams))
		parcelVolume := new(big.Int).Mul(big.NewInt(parcel.LengthMM), big.NewInt(parcel.WidthMM))
		parcelVolume.Mul(parcelVolume, big.NewInt(parcel.HeightMM))
		totalVolume.Add(totalVolume, parcelVolume)
	}
	length, err = dellinFixedDecimal(big.NewInt(maxLength), big.NewInt(1000), 3)
	if err != nil {
		return "", "", "", "", "", err
	}
	width, err = dellinFixedDecimal(big.NewInt(maxWidth), big.NewInt(1000), 3)
	if err != nil {
		return "", "", "", "", "", err
	}
	height, err = dellinFixedDecimal(big.NewInt(maxHeight), big.NewInt(1000), 3)
	if err != nil {
		return "", "", "", "", "", err
	}
	weight, err = dellinFixedDecimal(totalWeight, big.NewInt(1000), 3)
	if err != nil {
		return "", "", "", "", "", err
	}
	volume, err = dellinFixedDecimal(totalVolume, big.NewInt(1000000000), 9)
	if err != nil {
		return "", "", "", "", "", err
	}
	return length, width, height, weight, volume, nil
}

func dellinRateServiceCode(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = "auto"
	}
	switch value {
	case "auto", "small", "avia", "express", "letter":
		return "dellin_" + value, nil
	default:
		return "", errors.New("Деловые Линии tariff response has unsupported priceMinimal")
	}
}

func dellinRateTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, errors.New("Деловые Линии tariff response has invalid delivery date")
}

func dellinRateDates(response dellinRateResponse, now time.Time) (time.Time, time.Time, error) {
	minDate := time.Time{}
	for _, value := range []string{response.Data.OrderDates.ArrivalToOspReceiver, response.Data.OrderDates.GiveoutFromOspReceiver, response.Data.OrderDates.Pickup} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parsed, err := dellinRateTime(value)
		if err == nil {
			minDate = parsed
			break
		}
	}
	if minDate.IsZero() {
		minDate = now
	}
	maxDate := time.Time{}
	for _, value := range []string{response.Data.OrderDates.GiveoutFromOspReceiverMax, response.Data.OrderDates.GiveoutFromOspReceiver, response.Data.OrderDates.ArrivalToOspReceiver} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parsed, err := dellinRateTime(value)
		if err == nil {
			maxDate = parsed
			break
		}
	}
	if response.Data.DeliveryTerm < 0 || response.Data.DeliveryTerm > 3660 {
		return time.Time{}, time.Time{}, errors.New("Деловые Линии tariff response has invalid delivery term")
	}
	if maxDate.IsZero() {
		maxDate = minDate.Add(time.Duration(response.Data.DeliveryTerm) * 24 * time.Hour)
	}
	if maxDate.Before(minDate) {
		maxDate = minDate.Add(time.Duration(response.Data.DeliveryTerm) * 24 * time.Hour)
	}
	return minDate, maxDate, nil
}

func dellinTrackingStatus(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "finished":
		return "delivered"
	case "cancelled", "canceled", "annulled", "cancel":
		return "cancelled"
	case "declined":
		return "exception"
	case "draft", "processing", "waiting":
		return "pending"
	default:
		return "in_transit"
	}
}

func dellinTrackingTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("Деловые Линии tracking response has no status date")
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, errors.New("Деловые Линии tracking response has invalid status date")
	}
	return parsed.UTC(), nil
}

func (transport dellinHTTP) Track(ctx context.Context, secret []byte, request sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	remoteID := strings.TrimSpace(request.RemoteID)
	if transport.h == nil || remoteID == "" || !safeCodePattern.MatchString(remoteID) {
		return sdk.ShipmentResult{}, errors.New("Деловые Линии tracking request is unavailable")
	}
	credentials, err := readDellinCredentials(secret)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	body, err := json.Marshal(struct {
		AppKey string   `json:"appkey"`
		DocIDs []string `json:"docIds"`
	}{AppKey: credentials.AppKey, DocIDs: []string{remoteID}})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.dellin.ru", "/v3/orders/statuses_history.json", url.Values{}, body, http.Header{"Content-Type": []string{"application/json"}, "Accept": []string{"application/json"}}, nil, nil)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.ShipmentResult{}, fmt.Errorf("Деловые Линии tracking request rejected with status %d", status)
	}
	var response dellinTrackingResponse
	if json.Unmarshal(responseBody, &response) != nil || (response.Metadata.Status != 0 && response.Metadata.Status != http.StatusOK) {
		return sdk.ShipmentResult{}, errors.New("Деловые Линии tracking response rejected")
	}
	history, ok := response.Data.StatusHistory[remoteID]
	if !ok || len(history) == 0 || len(history) > 100 {
		return sdk.ShipmentResult{}, errors.New("Деловые Линии tracking response has no matching history")
	}
	var latest time.Time
	statusCode := "pending"
	for _, event := range history {
		if number := cdekScalarText(event.Number); number != "" && number != remoteID {
			return sdk.ShipmentResult{}, errors.New("Деловые Линии tracking response has mismatched document")
		}
		state := strings.TrimSpace(event.State)
		if state == "" || !safeCodePattern.MatchString(state) {
			return sdk.ShipmentResult{}, errors.New("Деловые Линии tracking response has invalid state")
		}
		observedAt, parseErr := dellinTrackingTime(event.StateDate)
		if parseErr != nil {
			return sdk.ShipmentResult{}, parseErr
		}
		if latest.IsZero() || observedAt.After(latest) {
			latest = observedAt
			statusCode = dellinTrackingStatus(state)
		}
	}
	return sdk.ShipmentResult{RemoteID: remoteID, Status: statusCode, Cost: sdk.LogisticsMoney{Currency: "RUB"}, TrackingNumber: remoteID, ObservedAt: latest.UTC()}, nil
}

// Label requests the official Деловые Линии waybill form. The API returns the
// PDF as base64; only a content-addressed opaque reference leaves host
// transport, so the document body and provider URL are never exposed.
func (transport dellinHTTP) Label(ctx context.Context, secret []byte, request sdk.LabelRequest) (sdk.LabelResult, error) {
	remoteID := strings.TrimSpace(request.RemoteID)
	if transport.h == nil || request.Validate() != nil || !strings.EqualFold(request.Format, "pdf") || !dellinDocumentUIDPattern.MatchString(remoteID) {
		return sdk.LabelResult{}, errors.New("Деловые Линии label request is unavailable")
	}
	credentials, err := readDellinCredentials(secret)
	if err != nil {
		return sdk.LabelResult{}, err
	}
	sessionID, err := transport.login(ctx, credentials)
	if err != nil {
		return sdk.LabelResult{}, err
	}
	body, err := json.Marshal(struct {
		AppKey    string `json:"appkey"`
		SessionID string `json:"sessionID"`
		DocUID    string `json:"docUID"`
		Mode      string `json:"mode"`
	}{AppKey: credentials.AppKey, SessionID: sessionID, DocUID: remoteID, Mode: "order"})
	if err != nil {
		return sdk.LabelResult{}, err
	}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.dellin.ru", "/v1/printable.json", url.Values{}, body, http.Header{"Content-Type": []string{"application/json"}, "Accept": []string{"application/json"}}, nil, nil)
	if err != nil {
		return sdk.LabelResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.LabelResult{}, fmt.Errorf("Деловые Линии label request rejected with status %d", status)
	}
	var response dellinPrintableResponse
	if json.Unmarshal(responseBody, &response) != nil || (response.Metadata.Status != 0 && response.Metadata.Status != http.StatusOK) || len(response.Data) != 1 {
		return sdk.LabelResult{}, errors.New("Деловые Линии label response rejected")
	}
	document := response.Data[0]
	if strings.TrimSpace(document.UID) != remoteID || strings.TrimSpace(document.Base64) == "" || len(document.URLs) > 1 {
		return sdk.LabelResult{}, errors.New("Деловые Линии label response has mismatched document")
	}
	pdf, err := base64.StdEncoding.DecodeString(strings.TrimSpace(document.Base64))
	if err != nil {
		pdf, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(document.Base64))
	}
	if err != nil || !bytes.HasPrefix(bytes.TrimSpace(pdf), []byte("%PDF-")) {
		return sdk.LabelResult{}, errors.New("Деловые Линии label response is not a PDF")
	}
	digest := sha256.Sum256(pdf)
	return sdk.LabelResult{
		ArtifactRef: "dellin:printable:order:" + remoteID + ":" + hex.EncodeToString(digest[:]),
		MediaType:   "application/pdf",
		ObservedAt:  time.Now().UTC(),
	}, nil
}

// Cancel submits the official address-delivery cancellation request. Dellin
// returns success when the request is accepted for later decision; it does
// not mean the shipment is already cancelled.
func (transport dellinHTTP) Cancel(ctx context.Context, secret []byte, request sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error) {
	remoteID := strings.TrimSpace(request.RemoteID)
	if transport.h == nil || !safeCodePattern.MatchString(remoteID) || strings.TrimSpace(request.IdempotencyKey) == "" {
		return sdk.ShipmentResult{}, errors.New("Деловые Линии cancellation request is unavailable")
	}
	orderID, err := strconv.ParseInt(remoteID, 10, 64)
	if err != nil || orderID < 1 {
		return sdk.ShipmentResult{}, errors.New("Деловые Линии cancellation requires a numeric order ID")
	}
	credentials, err := readDellinCredentials(secret)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	sessionID, err := transport.login(ctx, credentials)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	body, err := json.Marshal(struct {
		AppKey    string `json:"appkey"`
		SessionID string `json:"sessionID"`
		OrderID   int64  `json:"orderID"`
		Requester string `json:"requester"`
	}{AppKey: credentials.AppKey, SessionID: sessionID, OrderID: orderID, Requester: "sender"})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.dellin.ru", "/v3/orders/cancel_delivery.json", url.Values{}, body, http.Header{"Content-Type": []string{"application/json"}, "Accept": []string{"application/json"}}, nil, nil)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.ShipmentResult{}, fmt.Errorf("Деловые Линии cancellation request rejected with status %d", status)
	}
	var response dellinCancelResponse
	if json.Unmarshal(responseBody, &response) != nil || (response.Metadata.Status != 0 && response.Metadata.Status != http.StatusOK) || strings.ToLower(strings.TrimSpace(response.Data.Status)) != "success" {
		return sdk.ShipmentResult{}, errors.New("Деловые Линии cancellation response rejected")
	}
	return sdk.ShipmentResult{RemoteID: remoteID, Status: "cancellation_pending", Cost: sdk.LogisticsMoney{Currency: "RUB"}, TrackingNumber: remoteID, ObservedAt: time.Now().UTC()}, nil
}

func (transport dellinHTTP) Rates(ctx context.Context, secret []byte, request sdk.RateRequest) ([]sdk.RateQuote, error) {
	if transport.h == nil || request.Validate() != nil {
		return nil, errors.New("Деловые Линии tariff request is unavailable")
	}
	fromSearch, err := dellinAddressSearch(request.From)
	if err != nil {
		return nil, err
	}
	toSearch, err := dellinAddressSearch(request.To)
	if err != nil {
		return nil, err
	}
	length, width, height, weight, volume, err := dellinParcelTotals(request.Parcels)
	if err != nil {
		return nil, err
	}
	credentials, err := readDellinCredentials(secret)
	if err != nil {
		return nil, err
	}
	sessionID, err := transport.login(ctx, credentials)
	if err != nil {
		return nil, err
	}
	requestBody := dellinRateRequest{AppKey: credentials.AppKey, SessionID: sessionID}
	requestBody.Delivery.DeliveryType.Type = "auto"
	requestBody.Delivery.Derival.Variant = "address"
	requestBody.Delivery.Derival.Address.Search = fromSearch
	requestBody.Delivery.Arrival.Variant = "address"
	requestBody.Delivery.Arrival.Address.Search = toSearch
	requestBody.Payment.PaymentCitySearch.Search = strings.TrimSpace(request.From.City)
	requestBody.Payment.Type = "cash"
	requestBody.Cargo.Quantity = len(request.Parcels)
	requestBody.Cargo.Length = length
	requestBody.Cargo.Width = width
	requestBody.Cargo.Height = height
	requestBody.Cargo.Weight = weight
	requestBody.Cargo.TotalVolume = volume
	requestBody.Cargo.TotalWeight = weight
	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.dellin.ru", "/v2/calculator.json", url.Values{}, body, http.Header{"Content-Type": []string{"application/json"}, "Accept": []string{"application/json"}}, nil, nil)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Деловые Линии tariff request rejected with status %d", status)
	}
	var response dellinRateResponse
	if json.Unmarshal(responseBody, &response) != nil || (response.Metadata.Status != 0 && response.Metadata.Status != http.StatusOK) {
		return nil, errors.New("Деловые Линии tariff response rejected")
	}
	minorUnits, err := cdekMinorUnits(response.Data.Price)
	if err != nil {
		return nil, err
	}
	serviceCode, err := dellinRateServiceCode(response.Data.PriceMinimal)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	minDate, maxDate, err := dellinRateDates(response, now)
	if err != nil {
		return nil, err
	}
	return []sdk.RateQuote{{ServiceCode: serviceCode, Cost: sdk.LogisticsMoney{MinorUnits: minorUnits, Currency: "RUB"}, MinDeliveryAt: minDate, MaxDeliveryAt: maxDate, ObservedAt: now}}, nil
}

func (transport dellinHTTP) Pickup(ctx context.Context, secret []byte, query sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	if transport.h == nil || query.Validate(500) != nil || query.Country != "RU" {
		return nil, errors.New("Деловые Линии pickup-point request is unavailable")
	}
	credentials, err := readDellinCredentials(secret)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(struct {
		AppKey string `json:"appkey"`
	}{AppKey: credentials.AppKey})
	if err != nil {
		return nil, err
	}
	status, response, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.dellin.ru", "/v3/public/terminals.json", url.Values{}, body, http.Header{"Content-Type": []string{"application/json"}, "Accept": []string{"application/json"}}, nil, nil)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Деловые Линии terminal directory request rejected with status %d", status)
	}
	var reference dellinDirectoryResponse
	if json.Unmarshal(response, &reference) != nil || strings.TrimSpace(reference.Hash) == "" || strings.TrimSpace(reference.URL) == "" {
		return nil, errors.New("Деловые Линии terminal directory returned no catalog reference")
	}
	catalogURL, err := url.Parse(reference.URL)
	if err != nil || catalogURL.User != nil || catalogURL.Port() != "" || !strings.EqualFold(catalogURL.Hostname(), "api.dellin.ru") || (catalogURL.Scheme != "http" && catalogURL.Scheme != "https") || catalogURL.Path != "/catalog/terminals_v3.json" || len(catalogURL.Query()) == 0 {
		return nil, errors.New("Деловые Линии terminal catalog URL rejected")
	}
	status, response, _, _, _, err = transport.h.do(ctx, http.MethodGet, "api.dellin.ru", catalogURL.Path, catalogURL.Query(), nil, http.Header{"Accept": []string{"application/json"}}, nil, nil)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Деловые Линии terminal catalog request rejected with status %d", status)
	}
	var directory struct {
		Cities []dellinDirectoryCity `json:"city"`
	}
	if json.Unmarshal(response, &directory) != nil || len(directory.Cities) > 10000 {
		return nil, errors.New("Деловые Линии terminal catalog response rejected")
	}
	wantedCity := strings.TrimSpace(query.City)
	for _, city := range directory.Cities {
		if !strings.EqualFold(strings.TrimSpace(city.Name), wantedCity) {
			continue
		}
		points := make([]sdk.PickupPoint, 0, min(query.Limit, len(city.Terminals.Terminal)))
		now := time.Now().UTC()
		for _, terminal := range city.Terminals.Terminal {
			if len(points) >= query.Limit || (!terminal.GiveoutCargo && !terminal.IsPVZ) {
				continue
			}
			remoteID := strings.TrimSpace(terminal.ID)
			address := strings.TrimSpace(terminal.Address)
			if address == "" {
				address = strings.TrimSpace(terminal.FullAddress)
			}
			if remoteID == "" || address == "" {
				return nil, errors.New("Деловые Линии terminal catalog has incomplete pickup point")
			}
			name := strings.TrimSpace(terminal.Name)
			if name == "" {
				name = "Деловые Линии · терминал " + remoteID
			}
			points = append(points, sdk.PickupPoint{RemoteID: remoteID, Name: name, Country: query.Country, City: city.Name, Address: address, Active: true, UpdatedAt: now})
		}
		return points, nil
	}
	return []sdk.PickupPoint{}, nil
}

var _ dellin.Transport = dellinHTTP{}

// pochtarussiaHTTP verifies both credentials required by the official
// Otpravka REST API. The application token is sent as AccessToken and the
// generated user key as Basic X-User-Authorization; neither value leaves the
// callback-scoped secret path or is persisted by the transport.
type pochtarussiaHTTP struct{ h *httpTransport }

type pochtarussiaCredentials struct {
	Token            string `json:"token"`
	Key              string `json:"key"`
	TrackingLogin    string `json:"tracking_login"`
	TrackingPassword string `json:"tracking_password"`
}

type pochtarussiaOfficeSearch struct {
	Postoffices []json.RawMessage `json:"postoffices"`
}

type pochtarussiaOffice struct {
	AddressSource     string          `json:"address-source"`
	IsClosed          bool            `json:"is-closed"`
	IsTemporaryClosed bool            `json:"is-temporary-closed"`
	PostalCode        json.RawMessage `json:"postal-code"`
	Settlement        string          `json:"settlement"`
	TypeCode          string          `json:"type-code"`
}

type pochtarussiaBacklogDimension struct {
	Height int64 `json:"height"`
	Length int64 `json:"length"`
	Width  int64 `json:"width"`
}

type pochtarussiaBacklogOrder struct {
	AddressTypeTo string                       `json:"address-type-to"`
	GivenName     string                       `json:"given-name"`
	MiddleName    string                       `json:"middle-name,omitempty"`
	Surname       string                       `json:"surname"`
	IndexTo       int64                        `json:"index-to"`
	MailCategory  string                       `json:"mail-category"`
	MailDirect    int64                        `json:"mail-direct"`
	MailType      string                       `json:"mail-type"`
	Mass          int64                        `json:"mass"`
	OrderNum      string                       `json:"order-num"`
	PlaceTo       string                       `json:"place-to"`
	Postoffice    string                       `json:"postoffice-code,omitempty"`
	RegionTo      string                       `json:"region-to"`
	StreetTo      string                       `json:"street-to"`
	HouseTo       string                       `json:"house-to"`
	TelAddress    string                       `json:"tel-address"`
	TransportType string                       `json:"transport-type"`
	Dimension     pochtarussiaBacklogDimension `json:"dimension"`
}

type pochtarussiaBacklogResponse struct {
	ResultIDs []json.RawMessage `json:"result-ids"`
	Errors    []json.RawMessage `json:"errors"`
}

type pochtarussiaBatch struct {
	Name          string          `json:"batch-name"`
	Status        string          `json:"batch-status"`
	ShipmentCount json.RawMessage `json:"shipment-count"`
}

const pochtarussiaPickupLimit = 50

func readPochtaRussiaCredentials(secret []byte) (pochtarussiaCredentials, error) {
	var credentials pochtarussiaCredentials
	if decodeStrict(secret, &credentials) != nil || strings.TrimSpace(credentials.Token) == "" || strings.TrimSpace(credentials.Key) == "" {
		return pochtarussiaCredentials{}, errors.New("Почта России credentials must be JSON with token and key")
	}
	return credentials, nil
}

func pochtarussiaShipmentType(serviceCode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(serviceCode)) {
	case "pochta_parcel_online":
		return "ONLINE_PARCEL", nil
	case "pochta_parcel":
		return "POSTAL_PARCEL", nil
	default:
		return "", errors.New("Почта России shipment service is not supported")
	}
}

func pochtarussiaNameParts(value string) (string, string, string, error) {
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) != 2 && len(parts) != 3 {
		return "", "", "", errors.New("Почта России recipient name must contain two or three words")
	}
	if len(parts) == 2 {
		return parts[0], "", parts[1], nil
	}
	return parts[0], parts[1], parts[2], nil
}

func pochtarussiaStreetHouse(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value != strings.Join(strings.Fields(value), " ") {
		return "", "", errors.New("Почта России address is not normalized")
	}
	separator := strings.LastIndex(value, ",")
	if separator >= 0 {
		street, house := strings.TrimSpace(value[:separator]), strings.TrimSpace(value[separator+1:])
		if street == "" || !pochtarussiaHousePattern.MatchString(house) {
			return "", "", errors.New("Почта России address must contain a street and house")
		}
		return street, house, nil
	}
	for index, char := range value {
		if char >= '0' && char <= '9' {
			street, house := strings.TrimSpace(value[:index]), strings.TrimSpace(value[index:])
			if street == "" || !pochtarussiaHousePattern.MatchString(house) {
				return "", "", errors.New("Почта России address must contain a street and house")
			}
			return street, house, nil
		}
	}
	return "", "", errors.New("Почта России address has no house number")
}

func pochtarussiaDimension(value int64) (int64, error) {
	if value <= 0 || value%10 != 0 || value/10 > 1000 {
		return 0, errors.New("Почта России parcel dimensions must be whole centimetres")
	}
	return value / 10, nil
}

func pochtarussiaBacklogID(raw json.RawMessage) (string, error) {
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&number) == nil {
		value, err := strconv.ParseInt(number.String(), 10, 64)
		if err == nil && value > 0 {
			return strconv.FormatInt(value, 10), nil
		}
	}
	var text string
	if json.Unmarshal(raw, &text) == nil && pochtarussiaOrderIDPattern.MatchString(strings.TrimSpace(text)) {
		return strings.TrimSpace(text), nil
	}
	return "", errors.New("Почта России backlog response has invalid order id")
}

func pochtarussiaHeaders(credentials pochtarussiaCredentials) http.Header {
	return http.Header{
		"Authorization":        []string{"AccessToken " + credentials.Token},
		"X-User-Authorization": []string{"Basic " + credentials.Key},
		"Accept":               []string{"application/json;charset=UTF-8"},
		"Content-Type":         []string{"application/json"},
	}
}

func (transport pochtarussiaHTTP) Create(ctx context.Context, secret []byte, request sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	if transport.h == nil || request.Validate() != nil || len(request.Parcels) > 50 || !strings.EqualFold(request.From.Country, "RU") || !strings.EqualFold(request.To.Country, "RU") {
		return sdk.ShipmentResult{}, errors.New("Почта России shipment creation request rejected")
	}
	credentials, err := readPochtaRussiaCredentials(secret)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	mailType, err := pochtarussiaShipmentType(request.ServiceCode)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if _, err := pochtarussiaRequestPostalCode(request.From.PostalCode); err != nil {
		return sdk.ShipmentResult{}, err
	}
	toPostal, err := pochtarussiaRequestPostalCode(request.To.PostalCode)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	givenName, middleName, surname, err := pochtarussiaNameParts(request.Recipient.Name)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	street, house, err := pochtarussiaStreetHouse(request.To.Line1)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	indexTo, err := strconv.ParseInt(toPostal, 10, 64)
	if err != nil {
		return sdk.ShipmentResult{}, errors.New("Почта России destination index is invalid")
	}
	var mass int64
	var dimension pochtarussiaBacklogDimension
	for index, parcel := range request.Parcels {
		if parcel.WeightGrams > (int64(1<<63-1) - mass) {
			return sdk.ShipmentResult{}, errors.New("Почта России shipment weight is out of range")
		}
		mass += parcel.WeightGrams
		length, lengthErr := pochtarussiaDimension(parcel.LengthMM)
		width, widthErr := pochtarussiaDimension(parcel.WidthMM)
		height, heightErr := pochtarussiaDimension(parcel.HeightMM)
		if lengthErr != nil || widthErr != nil || heightErr != nil {
			return sdk.ShipmentResult{}, errors.New("Почта России shipment dimensions are unsupported")
		}
		if index == 0 {
			dimension = pochtarussiaBacklogDimension{Height: height, Length: length, Width: width}
			continue
		}
		if dimension.Height != height || dimension.Length != length || dimension.Width != width {
			return sdk.ShipmentResult{}, errors.New("Почта России requires equal dimensions for one backlog order")
		}
	}
	order := pochtarussiaBacklogOrder{
		AddressTypeTo: "DEFAULT", GivenName: givenName, MiddleName: middleName, Surname: surname,
		IndexTo: indexTo, MailCategory: "ORDINARY", MailDirect: 643, MailType: mailType,
		Mass: mass, OrderNum: request.ExternalID, PlaceTo: request.To.City, RegionTo: request.To.City,
		StreetTo: street, HouseTo: house, TelAddress: request.Recipient.Phone, TransportType: "SURFACE", Dimension: dimension,
	}
	if request.PickupPointRef != "" {
		if !pochtarussiaOrderIDPattern.MatchString(request.PickupPointRef) {
			return sdk.ShipmentResult{}, errors.New("Почта России pickup point is invalid")
		}
		order.Postoffice = request.PickupPointRef
	}
	payload, err := json.Marshal([]pochtarussiaBacklogOrder{order})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodPut, "otpravka-api.pochta.ru", "/1.0/user/backlog", url.Values{}, payload, pochtarussiaHeaders(credentials), nil, nil)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.ShipmentResult{}, fmt.Errorf("Почта России shipment creation rejected with status %d", status)
	}
	var response pochtarussiaBacklogResponse
	if json.Unmarshal(responseBody, &response) != nil || len(response.Errors) != 0 || len(response.ResultIDs) != 1 {
		return sdk.ShipmentResult{}, errors.New("Почта России shipment creation response rejected")
	}
	remoteID, err := pochtarussiaBacklogID(response.ResultIDs[0])
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	return sdk.ShipmentResult{RemoteID: remoteID, Status: "created", Cost: sdk.LogisticsMoney{Currency: "RUB"}, ObservedAt: time.Now().UTC()}, nil
}

func (transport pochtarussiaHTTP) Cancel(ctx context.Context, secret []byte, request sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error) {
	if transport.h == nil || !pochtarussiaOrderIDPattern.MatchString(strings.TrimSpace(request.RemoteID)) || strings.TrimSpace(request.IdempotencyKey) == "" {
		return sdk.ShipmentResult{}, errors.New("Почта России cancellation request rejected")
	}
	credentials, err := readPochtaRussiaCredentials(secret)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	orderID, err := strconv.ParseInt(strings.TrimSpace(request.RemoteID), 10, 64)
	if err != nil || orderID <= 0 {
		return sdk.ShipmentResult{}, errors.New("Почта России cancellation order id is invalid")
	}
	payload, err := json.Marshal([]int64{orderID})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodDelete, "otpravka-api.pochta.ru", "/1.0/backlog", url.Values{}, payload, pochtarussiaHeaders(credentials), nil, nil)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.ShipmentResult{}, fmt.Errorf("Почта России cancellation rejected with status %d", status)
	}
	var response pochtarussiaBacklogResponse
	if json.Unmarshal(responseBody, &response) != nil || len(response.Errors) != 0 || len(response.ResultIDs) != 1 {
		return sdk.ShipmentResult{}, errors.New("Почта России cancellation response rejected")
	}
	deletedID, err := pochtarussiaBacklogID(response.ResultIDs[0])
	if err != nil || deletedID != strings.TrimSpace(request.RemoteID) {
		return sdk.ShipmentResult{}, errors.New("Почта России cancellation response identifier mismatch")
	}
	return sdk.ShipmentResult{RemoteID: deletedID, Status: "cancelled", Cost: sdk.LogisticsMoney{Currency: "RUB"}, ObservedAt: time.Now().UTC()}, nil
}

// Return creates one return shipment for an existing RPO through the official
// Otpravka endpoint. Separate return shipments require a different payload
// and are intentionally not part of this neutral operation.
func (transport pochtarussiaHTTP) Return(ctx context.Context, secret []byte, request sdk.ReturnCreateRequest) (sdk.ShipmentResult, error) {
	originalRemoteID := strings.TrimSpace(request.OriginalRemoteID)
	mailType := strings.ToUpper(strings.TrimSpace(request.MailType))
	if transport.h == nil || request.Validate() != nil || !pochtarussiaTrackingBarcode(originalRemoteID) || !pochtarussiaReturnMailType(mailType) {
		return sdk.ShipmentResult{}, errors.New("Почта России return request rejected")
	}
	credentials, err := readPochtaRussiaCredentials(secret)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	payload, err := json.Marshal(struct {
		DirectBarcode string `json:"direct-barcode"`
		MailType      string `json:"mail-type"`
	}{DirectBarcode: originalRemoteID, MailType: mailType})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodPut, "otpravka-api.pochta.ru", "/1.0/returns", url.Values{}, payload, pochtarussiaHeaders(credentials), nil, nil)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.ShipmentResult{}, fmt.Errorf("Почта России return creation rejected with status %d", status)
	}
	var response struct {
		ReturnBarcode json.RawMessage   `json:"return-barcode"`
		Errors        []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(responseBody, &response) != nil || len(response.Errors) != 0 || len(response.ReturnBarcode) == 0 {
		return sdk.ShipmentResult{}, errors.New("Почта России return response rejected")
	}
	remoteID, err := pochtarussiaReturnID(response.ReturnBarcode)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	return sdk.ShipmentResult{RemoteID: remoteID, Status: "created", Cost: sdk.LogisticsMoney{Currency: "RUB"}, TrackingNumber: remoteID, ObservedAt: time.Now().UTC()}, nil
}

func pochtarussiaReturnMailType(value string) bool {
	switch value {
	case "UNDEFINED", "POSTAL_PARCEL", "ONLINE_PARCEL", "EMS", "EMS_OPTIMAL", "FIRST_CLASS":
		return true
	default:
		return false
	}
}

func pochtarussiaReturnID(raw json.RawMessage) (string, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil && pochtarussiaTrackingBarcode(strings.TrimSpace(text)) {
		return strings.TrimSpace(text), nil
	}
	return "", errors.New("Почта России return response has invalid RPO")
}

func (transport pochtarussiaHTTP) Ping(ctx context.Context, secret []byte) error {
	if transport.h == nil {
		return errors.New("Почта России credential probe unavailable")
	}
	credentials, err := readPochtaRussiaCredentials(secret)
	if err != nil {
		return err
	}
	status, response, _, _, _, err := transport.h.do(ctx, http.MethodGet, "otpravka-api.pochta.ru", "/1.0/settings", url.Values{}, nil, pochtarussiaHeaders(credentials), nil, nil)
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("Почта России credential probe rejected with status %d", status)
	}
	if len(response) == 0 || !json.Valid(response) {
		return errors.New("Почта России credential probe returned invalid JSON")
	}
	return nil
}

type pochtarussiaTrackingRecord struct {
	Item struct {
		Barcode string `xml:"Barcode"`
	} `xml:"ItemParameters"`
	Operation struct {
		Type struct {
			ID   string `xml:"Id"`
			Name string `xml:"Name"`
		} `xml:"OperType"`
		Attribute struct {
			ID   string `xml:"Id"`
			Name string `xml:"Name"`
		} `xml:"OperAttr"`
		Date string `xml:"OperDate"`
	} `xml:"OperationParameters"`
}

type pochtarussiaTrackingResponse struct {
	History []pochtarussiaTrackingRecord `xml:"historyRecord"`
}

type pochtarussiaTrackingEnvelope struct {
	Body *struct {
		Response *pochtarussiaTrackingResponse `xml:"getOperationHistoryResponse"`
	} `xml:"Body"`
}

func readPochtaRussiaTrackingCredentials(secret []byte) (pochtarussiaCredentials, error) {
	credentials, err := readPochtaRussiaCredentials(secret)
	if err != nil {
		return pochtarussiaCredentials{}, err
	}
	if strings.TrimSpace(credentials.TrackingLogin) == "" || credentials.TrackingPassword == "" {
		return pochtarussiaCredentials{}, errors.New("Почта России tracking credentials require tracking_login and tracking_password")
	}
	return credentials, nil
}

func pochtarussiaTrackingBarcode(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) == 14 {
		for _, digit := range []byte(value) {
			if digit < '0' || digit > '9' {
				return false
			}
		}
		return true
	}
	if len(value) != 13 {
		return false
	}
	for index, char := range []byte(value) {
		if index < 2 || index > 10 {
			if char < 'A' || char > 'Z' {
				return false
			}
			continue
		}
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func pochtarussiaTrackingSOAPRequest(barcode, login, password string) []byte {
	var body bytes.Buffer
	body.WriteString(xml.Header)
	body.WriteString(`<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope" xmlns:oper="http://russianpost.org/operationhistory" xmlns:data="http://russianpost.org/operationhistory/data" xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soap:Header/><soap:Body><oper:getOperationHistory><data:OperationHistoryRequest><data:Barcode>`)
	_ = xml.EscapeText(&body, []byte(barcode))
	body.WriteString(`</data:Barcode><data:MessageType>0</data:MessageType><data:Language>RUS</data:Language></data:OperationHistoryRequest><data:AuthorizationHeader soapenv:mustUnderstand="1"><data:login>`)
	_ = xml.EscapeText(&body, []byte(login))
	body.WriteString(`</data:login><data:password>`)
	_ = xml.EscapeText(&body, []byte(password))
	body.WriteString(`</data:password></data:AuthorizationHeader></oper:getOperationHistory></soap:Body></soap:Envelope>`)
	return body.Bytes()
}

func pochtarussiaTrackingStatus(record pochtarussiaTrackingRecord) string {
	text := strings.ToLower(strings.TrimSpace(record.Operation.Type.Name + " " + record.Operation.Attribute.Name))
	switch {
	case strings.Contains(text, "вруч"):
		return "delivered"
	case strings.Contains(text, "возврат") || strings.Contains(text, "возвращ"):
		return "returned"
	case strings.Contains(text, "неуда") || strings.Contains(text, "невруч"):
		return "exception"
	case strings.Contains(text, "принят") || strings.Contains(text, "прием") || strings.Contains(text, "приём"):
		return "created"
	default:
		return "in_transit"
	}
}

func (transport pochtarussiaHTTP) Track(ctx context.Context, secret []byte, request sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	if transport.h == nil || !pochtarussiaTrackingBarcode(request.RemoteID) {
		return sdk.ShipmentResult{}, errors.New("Почта России tracking request rejected")
	}
	credentials, err := readPochtaRussiaTrackingCredentials(secret)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodPost, "tracking.russianpost.ru", "/rtm34", url.Values{}, pochtarussiaTrackingSOAPRequest(request.RemoteID, credentials.TrackingLogin, credentials.TrackingPassword), http.Header{
		"Accept":       []string{"application/soap+xml, text/xml;q=0.9"},
		"Content-Type": []string{"application/soap+xml; charset=utf-8"},
	}, nil, nil)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.ShipmentResult{}, fmt.Errorf("Почта России tracking request rejected with status %d", status)
	}
	var envelope pochtarussiaTrackingEnvelope
	if xml.Unmarshal(responseBody, &envelope) != nil || envelope.Body == nil || envelope.Body.Response == nil || len(envelope.Body.Response.History) > 100 {
		return sdk.ShipmentResult{}, errors.New("Почта России tracking response rejected")
	}
	latest := time.Time{}
	statusCode := "pending"
	for _, record := range envelope.Body.Response.History {
		if barcode := strings.TrimSpace(record.Item.Barcode); barcode != "" && barcode != request.RemoteID {
			return sdk.ShipmentResult{}, errors.New("Почта России tracking response has mismatched barcode")
		}
		if strings.TrimSpace(record.Operation.Type.ID) == "" && strings.TrimSpace(record.Operation.Type.Name) == "" {
			return sdk.ShipmentResult{}, errors.New("Почта России tracking response has no operation")
		}
		observedAt, parseErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(record.Operation.Date))
		if parseErr != nil {
			return sdk.ShipmentResult{}, errors.New("Почта России tracking response has invalid operation date")
		}
		if latest.IsZero() || observedAt.After(latest) {
			latest = observedAt
			statusCode = pochtarussiaTrackingStatus(record)
		}
	}
	if latest.IsZero() {
		latest = time.Now().UTC()
	}
	return sdk.ShipmentResult{RemoteID: request.RemoteID, Status: statusCode, Cost: sdk.LogisticsMoney{Currency: "RUB"}, TrackingNumber: request.RemoteID, ObservedAt: latest.UTC()}, nil
}

// Label requests an official Russian Post PDF form. The current neutral label
// contract carries an artifact reference, not binary content, so the returned
// PDF is represented by a content-addressed opaque reference after validating
// its media type and signature. Credentials and the PDF body never leave host
// transport. Format "pdf" requests the pre-batch order form; format
// "return_pdf" requests the one-page easy-return label for an RPO barcode.
func (transport pochtarussiaHTTP) Label(ctx context.Context, secret []byte, request sdk.LabelRequest) (sdk.LabelResult, error) {
	remoteID := strings.TrimSpace(request.RemoteID)
	format := strings.ToLower(strings.TrimSpace(request.Format))
	if transport.h == nil || request.Validate() != nil {
		return sdk.LabelResult{}, errors.New("Почта России label request rejected")
	}
	credentials, err := readPochtaRussiaCredentials(secret)
	if err != nil {
		return sdk.LabelResult{}, err
	}
	path := ""
	query := url.Values{}
	prefix := ""
	switch format {
	case "pdf":
		if !pochtarussiaOrderIDPattern.MatchString(remoteID) {
			return sdk.LabelResult{}, errors.New("Почта России label order id is invalid")
		}
		path = "/1.0/forms/backlog/" + remoteID + "/forms"
		query.Set("sending-date", time.Now().UTC().Format("2006-01-02"))
		query.Set("print-type", "PAPER")
		prefix = "pochta-russia:form:backlog:" + remoteID + ":"
	case "return_pdf":
		if !pochtarussiaTrackingBarcode(remoteID) {
			return sdk.LabelResult{}, errors.New("Почта России return label RPO is invalid")
		}
		path = "/1.0/forms/" + url.PathEscape(remoteID) + "/easy-return-pdf"
		query.Set("print-type", "PAPER")
		prefix = "pochta-russia:form:return:" + remoteID + ":"
	default:
		return sdk.LabelResult{}, errors.New("Почта России label format is unsupported")
	}
	status, responseBody, _, _, responseHeaders, err := transport.h.do(ctx, http.MethodGet, "otpravka-api.pochta.ru", path, query, nil, pochtarussiaHeaders(credentials), nil, nil)
	if err != nil {
		return sdk.LabelResult{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return sdk.LabelResult{}, fmt.Errorf("Почта России label request rejected with status %d", status)
	}
	contentType, _, parseErr := mime.ParseMediaType(responseHeaders.Get("Content-Type"))
	if parseErr != nil || !strings.EqualFold(contentType, "application/pdf") || !bytes.HasPrefix(bytes.TrimSpace(responseBody), []byte("%PDF-")) {
		return sdk.LabelResult{}, errors.New("Почта России label response is not a PDF")
	}
	digest := sha256.Sum256(responseBody)
	return sdk.LabelResult{
		ArtifactRef: prefix + hex.EncodeToString(digest[:]),
		MediaType:   "application/pdf",
		ObservedAt:  time.Now().UTC(),
	}, nil
}

func pochtarussiaPostalCode(raw json.RawMessage) (string, error) {
	var value string
	if len(raw) > 0 && raw[0] == '"' {
		if json.Unmarshal(raw, &value) != nil {
			return "", errors.New("Почта России postoffice code is invalid")
		}
	} else {
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		var number json.Number
		if decoder.Decode(&number) != nil {
			return "", errors.New("Почта России postoffice code is invalid")
		}
		value = number.String()
	}
	value = strings.TrimSpace(value)
	if len(value) != 6 {
		return "", errors.New("Почта России postoffice code has invalid length")
	}
	for _, digit := range []byte(value) {
		if digit < '0' || digit > '9' {
			return "", errors.New("Почта России postoffice code is invalid")
		}
	}
	return value, nil
}

func pochtarussiaRequestPostalCode(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) != 6 {
		return "", errors.New("Почта России rate request has invalid postal code")
	}
	for _, digit := range []byte(value) {
		if digit < '0' || digit > '9' {
			return "", errors.New("Почта России rate request has invalid postal code")
		}
	}
	return value, nil
}

func pochtarussiaNonNegativeInt(raw json.RawMessage) (int64, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var number json.Number
	if decoder.Decode(&number) != nil {
		return 0, errors.New("Почта России rate response has invalid integer")
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil || value < 0 {
		return 0, errors.New("Почта России rate response has invalid integer")
	}
	return value, nil
}

type pochtarussiaRateDelivery struct {
	Min json.RawMessage `json:"min"`
	Max json.RawMessage `json:"max"`
}

type pochtarussiaRateResponse struct {
	ID       int64                    `json:"id"`
	Errors   []json.RawMessage        `json:"errors"`
	Items    []json.RawMessage        `json:"items"`
	PayNDS   json.RawMessage          `json:"paynds"`
	Delivery pochtarussiaRateDelivery `json:"delivery"`
}

func (transport pochtarussiaHTTP) Rates(ctx context.Context, secret []byte, request sdk.RateRequest) ([]sdk.RateQuote, error) {
	if transport.h == nil || request.Validate() != nil || !strings.EqualFold(request.From.Country, "RU") || !strings.EqualFold(request.To.Country, "RU") {
		return nil, errors.New("Почта России rate request rejected")
	}
	if _, err := readPochtaRussiaCredentials(secret); err != nil {
		return nil, err
	}
	from, err := pochtarussiaRequestPostalCode(request.From.PostalCode)
	if err != nil {
		return nil, err
	}
	to, err := pochtarussiaRequestPostalCode(request.To.PostalCode)
	if err != nil {
		return nil, err
	}
	var weight int64
	for _, parcel := range request.Parcels {
		if parcel.WeightGrams > (int64(1<<63-1) - weight) {
			return nil, errors.New("Почта России rate request weight is out of range")
		}
		weight += parcel.WeightGrams
	}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodGet, "tariff.pochta.ru", "/v2/calculate/tariff/delivery", url.Values{
		"format":    []string{"json"},
		"errorcode": []string{"1"},
		"object":    []string{"23030"},
		"weight":    []string{strconv.FormatInt(weight, 10)},
		"from":      []string{from},
		"to":        []string{to},
	}, nil, http.Header{"Accept": []string{"application/json"}}, nil, nil)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Почта России rate request rejected with status %d", status)
	}
	var response pochtarussiaRateResponse
	if json.Unmarshal(responseBody, &response) != nil || response.ID != 23030 || len(response.Errors) != 0 || len(response.Items) == 0 {
		return nil, errors.New("Почта России rate response rejected")
	}
	cost, err := pochtarussiaNonNegativeInt(response.PayNDS)
	if err != nil {
		return nil, err
	}
	minDays, err := pochtarussiaNonNegativeInt(response.Delivery.Min)
	if err != nil {
		return nil, err
	}
	maxDays, err := pochtarussiaNonNegativeInt(response.Delivery.Max)
	if err != nil || maxDays < minDays || maxDays > 3660 {
		return nil, errors.New("Почта России rate response has invalid delivery period")
	}
	now := time.Now().UTC()
	return []sdk.RateQuote{{
		ServiceCode:   "pochta_parcel_online",
		Cost:          sdk.LogisticsMoney{MinorUnits: cost, Currency: "RUB"},
		MinDeliveryAt: now.Add(time.Duration(minDays) * 24 * time.Hour),
		MaxDeliveryAt: now.Add(time.Duration(maxDays) * 24 * time.Hour),
		ObservedAt:    now,
	}}, nil
}

func (transport pochtarussiaHTTP) Pickup(ctx context.Context, secret []byte, query sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	if transport.h == nil || query.Validate(500) != nil || query.Country != "RU" {
		return nil, errors.New("Почта России pickup request rejected")
	}
	credentials, err := readPochtaRussiaCredentials(secret)
	if err != nil {
		return nil, err
	}
	headers := pochtarussiaHeaders(credentials)
	providerLimit := query.Limit
	if providerLimit > pochtarussiaPickupLimit {
		providerLimit = pochtarussiaPickupLimit
	}
	status, response, _, _, _, err := transport.h.do(ctx, http.MethodGet, "otpravka-api.pochta.ru", "/postoffice/1.0/by-address", url.Values{
		"address": []string{strings.TrimSpace(query.City)},
		"top":     []string{strconv.Itoa(providerLimit)},
	}, nil, headers, nil, nil)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Почта России pickup search rejected with status %d", status)
	}
	var search pochtarussiaOfficeSearch
	if json.Unmarshal(response, &search) != nil || len(search.Postoffices) > providerLimit || len(search.Postoffices) > pochtarussiaPickupLimit {
		return nil, errors.New("Почта России pickup search response rejected")
	}
	points := make([]sdk.PickupPoint, 0, len(search.Postoffices))
	seen := make(map[string]struct{}, len(search.Postoffices))
	for _, rawCode := range search.Postoffices {
		code, codeErr := pochtarussiaPostalCode(rawCode)
		if codeErr != nil {
			return nil, codeErr
		}
		if _, duplicate := seen[code]; duplicate {
			return nil, errors.New("Почта России pickup search returned duplicate postoffice")
		}
		seen[code] = struct{}{}
		detailStatus, detailResponse, _, _, _, detailErr := transport.h.do(ctx, http.MethodGet, "otpravka-api.pochta.ru", "/postoffice/1.0/"+code, url.Values{
			"filter-by-office-type": []string{"true"},
		}, nil, headers, nil, nil)
		if detailErr != nil {
			return nil, detailErr
		}
		if detailStatus < http.StatusOK || detailStatus >= http.StatusMultipleChoices {
			return nil, fmt.Errorf("Почта России postoffice %s rejected with status %d", code, detailStatus)
		}
		var office pochtarussiaOffice
		if json.Unmarshal(detailResponse, &office) != nil {
			return nil, errors.New("Почта России postoffice response rejected")
		}
		if officeCode, officeErr := pochtarussiaPostalCode(office.PostalCode); officeErr != nil || officeCode != code {
			return nil, errors.New("Почта России postoffice response has mismatched code")
		}
		address := strings.TrimSpace(office.AddressSource)
		if address == "" {
			return nil, errors.New("Почта России postoffice response has no address")
		}
		city := strings.TrimSpace(office.Settlement)
		if city == "" {
			city = strings.TrimSpace(query.City)
		}
		name := "Почта России · ОПС " + code
		if typeCode := strings.TrimSpace(office.TypeCode); typeCode != "" {
			name += " · " + typeCode
		}
		points = append(points, sdk.PickupPoint{
			RemoteID: code, Name: name, Country: query.Country, City: city,
			Address: address, Active: !office.IsClosed && !office.IsTemporaryClosed,
			UpdatedAt: time.Now().UTC(),
		})
	}
	return points, nil
}

// Batches reads one bounded page of the official Russian Post batch directory.
// Only the batch identity, provider status and count cross the host transport;
// individual order rows remain behind the provider boundary.
func (transport pochtarussiaHTTP) Batches(ctx context.Context, secret []byte, query sdk.LogisticsBatchQuery) ([]sdk.LogisticsBatch, error) {
	if transport.h == nil || query.Validate(100) != nil {
		return nil, errors.New("Почта России batch request rejected")
	}
	credentials, err := readPochtaRussiaCredentials(secret)
	if err != nil {
		return nil, err
	}
	params := url.Values{
		"size": []string{strconv.Itoa(query.Limit)},
		"page": []string{strconv.Itoa(query.Page)},
	}
	if query.MailType != "" {
		params.Set("mailType", query.MailType)
	}
	if query.MailCategory != "" {
		params.Set("mailCategory", query.MailCategory)
	}
	status, responseBody, _, _, _, err := transport.h.do(ctx, http.MethodGet, "otpravka-api.pochta.ru", "/1.0/batch", params, nil, pochtarussiaHeaders(credentials), nil, nil)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Почта России batch request rejected with status %d", status)
	}
	var response []pochtarussiaBatch
	if json.Unmarshal(responseBody, &response) != nil || len(response) > query.Limit {
		return nil, errors.New("Почта России batch response rejected")
	}
	now := time.Now().UTC()
	result := make([]sdk.LogisticsBatch, 0, len(response))
	seen := make(map[string]struct{}, len(response))
	for _, item := range response {
		name := strings.TrimSpace(item.Name)
		batchStatus := strings.TrimSpace(item.Status)
		if !logisticsBatchRemoteIDPattern.MatchString(name) || !safeCodePattern.MatchString(batchStatus) {
			return nil, errors.New("Почта России batch response has invalid identity")
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, errors.New("Почта России batch response has duplicate identity")
		}
		seen[name] = struct{}{}
		count, countErr := pochtarussiaNonNegativeInt(item.ShipmentCount)
		if countErr != nil || count > 1000000 {
			return nil, errors.New("Почта России batch response has invalid shipment count")
		}
		result = append(result, sdk.LogisticsBatch{RemoteID: name, Status: batchStatus, ShipmentCount: int(count), ObservedAt: now})
	}
	return result, nil
}

var _ pochtarussia.Transport = pochtarussiaHTTP{}
