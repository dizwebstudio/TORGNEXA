package builtinruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func (cdekHTTP) Create(context.Context, []byte, sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{}, errLogisticsOperationNotAdmitted
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
func (cdekHTTP) Return(context.Context, []byte, sdk.ReturnCreateRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{}, errLogisticsOperationNotAdmitted
}
func (cdekHTTP) Label(context.Context, []byte, sdk.LabelRequest) (sdk.LabelResult, error) {
	return sdk.LabelResult{}, errLogisticsOperationNotAdmitted
}
func (cdekHTTP) Webhook(context.Context, []byte, []byte, []byte) (sdk.LogisticsWebhook, error) {
	return sdk.LogisticsWebhook{}, errLogisticsOperationNotAdmitted
}

var _ cdek.Transport = cdekHTTP{}

// pekHTTP probes and reads the official ПЭК personal-cabinet API with the
// documented Basic login/access-key pair. Branch lookup and the bounded
// calculator preview are read-only; shipment operations stay
// qualification-gated.
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

const pekTrackingResponseLimit = 50

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

func (pekHTTP) Create(context.Context, []byte, sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{}, errLogisticsOperationNotAdmitted
}

var _ pek.Transport = pekHTTP{}

// dellinHTTP validates an app key and personal access token by opening a
// short-lived Деловые Линии API session. The returned session ID is discarded;
// future operational calls remain qualification-gated.
type dellinHTTP struct{ h *httpTransport }

type dellinCredentials struct {
	AppKey string `json:"appkey"`
	PAT    string `json:"pat"`
}

func readDellinCredentials(secret []byte) (dellinCredentials, error) {
	var credentials dellinCredentials
	if decodeStrict(secret, &credentials) != nil || strings.TrimSpace(credentials.AppKey) == "" || strings.TrimSpace(credentials.PAT) == "" {
		return dellinCredentials{}, errors.New("Деловые Линии credentials must be JSON with appkey and pat")
	}
	return credentials, nil
}

func (transport dellinHTTP) Ping(ctx context.Context, secret []byte) error {
	if transport.h == nil {
		return errors.New("Деловые Линии credential probe unavailable")
	}
	credentials, err := readDellinCredentials(secret)
	if err != nil {
		return err
	}
	body, err := json.Marshal(credentials)
	if err != nil {
		return err
	}
	headers := http.Header{"Content-Type": []string{"application/json"}, "Accept": []string{"application/json"}}
	status, response, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.dellin.ru", "/v4/auth/login.json", url.Values{}, body, headers, nil, nil)
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("Деловые Линии credential probe rejected with status %d", status)
	}
	var result struct {
		Metadata struct {
			Status int `json:"status"`
		} `json:"metadata"`
		Data struct {
			SessionID string `json:"sessionID"`
		} `json:"data"`
	}
	if json.Unmarshal(response, &result) != nil || (result.Metadata.Status != 0 && result.Metadata.Status != http.StatusOK) || strings.TrimSpace(result.Data.SessionID) == "" {
		return errors.New("Деловые Линии credential probe returned no session")
	}
	return nil
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
	Token string `json:"token"`
	Key   string `json:"key"`
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

const pochtarussiaPickupLimit = 50

func readPochtaRussiaCredentials(secret []byte) (pochtarussiaCredentials, error) {
	var credentials pochtarussiaCredentials
	if decodeStrict(secret, &credentials) != nil || strings.TrimSpace(credentials.Token) == "" || strings.TrimSpace(credentials.Key) == "" {
		return pochtarussiaCredentials{}, errors.New("Почта России credentials must be JSON with token and key")
	}
	return credentials, nil
}

func pochtarussiaHeaders(credentials pochtarussiaCredentials) http.Header {
	return http.Header{
		"Authorization":        []string{"AccessToken " + credentials.Token},
		"X-User-Authorization": []string{"Basic " + credentials.Key},
		"Accept":               []string{"application/json;charset=UTF-8"},
		"Content-Type":         []string{"application/json"},
	}
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

var _ pochtarussia.Transport = pochtarussiaHTTP{}
