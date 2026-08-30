package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const (
	logisticsPickupPointsPath = "/api/v1/logistics/pickup-points"
	logisticsRatesPath        = "/api/v1/logistics/rates"
	logisticsTrackingPath     = "/api/v1/logistics/tracking"
)

var logisticsRemoteIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)

// logisticsCapabilityStore is kept separate from the concrete PostgreSQL
// repository so the operation remains testable without a database.
type logisticsCapabilityStore interface {
	AccountByID(context.Context, string, string, string) (sdk.Account, error)
	AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error)
}

type logisticsRuntime interface {
	SupportsCapability(string, string) bool
	PickupPoints(context.Context, sdk.Account, sdk.Runtime, sdk.PickupPointQuery) ([]sdk.PickupPoint, error)
	LogisticsRates(context.Context, sdk.Account, sdk.Runtime, sdk.RateRequest) ([]sdk.RateQuote, error)
	LogisticsTracking(context.Context, sdk.Account, sdk.Runtime, sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error)
}

type logisticsAPI struct {
	accounts logisticsCapabilityStore
	secrets  secrets.SecretProvider
	runtime  logisticsRuntime
}

type pickupPointView struct {
	RemoteID string `json:"remote_id"`
	Name     string `json:"name"`
	Country  string `json:"country"`
	City     string `json:"city"`
	Address  string `json:"address"`
	Active   bool   `json:"active"`
}

type logisticsAddressInput struct {
	Country    string `json:"country"`
	PostalCode string `json:"postal_code"`
	City       string `json:"city"`
	Line1      string `json:"line1"`
}

type logisticsParcelInput struct {
	WeightGrams int64 `json:"weight_grams"`
	LengthMM    int64 `json:"length_mm"`
	WidthMM     int64 `json:"width_mm"`
	HeightMM    int64 `json:"height_mm"`
}

type logisticsRatesInput struct {
	ConnectorAccountID string                 `json:"connector_account_id"`
	From               logisticsAddressInput  `json:"from"`
	To                 logisticsAddressInput  `json:"to"`
	Parcels            []logisticsParcelInput `json:"parcels"`
}

type logisticsRateView struct {
	OptionID      string    `json:"option_id"`
	Cost          moneyView `json:"cost"`
	MinDeliveryAt time.Time `json:"min_delivery_at"`
	MaxDeliveryAt time.Time `json:"max_delivery_at"`
	ObservedAt    time.Time `json:"observed_at"`
}

type logisticsTrackingView struct {
	RemoteID       string    `json:"remote_id"`
	Status         string    `json:"status"`
	TrackingNumber string    `json:"tracking_number,omitempty"`
	ObservedAt     time.Time `json:"observed_at"`
}

func newLogisticsRoutes(accounts logisticsCapabilityStore, secretProvider secrets.SecretProvider, runtime logisticsRuntime) []ProtectedRoute {
	api := logisticsAPI{accounts: accounts, secrets: secretProvider, runtime: runtime}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: logisticsPickupPointsPath, Permission: "connectors.read", Handler: http.HandlerFunc(api.listPickupPoints)},
		{Method: http.MethodPost, Path: logisticsRatesPath, Permission: "connectors.read", Handler: http.HandlerFunc(api.calculateRates)},
		{Method: http.MethodGet, Path: logisticsTrackingPath, Permission: "connectors.read", Handler: http.HandlerFunc(api.readTracking)},
	}
}

func (api logisticsAPI) listPickupPoints(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.accounts == nil || api.secrets == nil || api.runtime == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	accountID := strings.TrimSpace(r.URL.Query().Get("connector_account_id"))
	country := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("country")))
	city := strings.TrimSpace(r.URL.Query().Get("city"))
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		limit = value
	}
	query := sdk.PickupPointQuery{Country: country, City: city, Limit: limit}
	if accountID == "" || query.Validate(500) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID)
	if err != nil || account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
		writeProblem(w, http.StatusConflict, "Logistics connector account unavailable")
		return
	}
	const capability = sdk.Capability("pickup.points.read")
	if !supportsLogisticsCapability(api.runtime, account, capability) {
		writeProblem(w, http.StatusConflict, "Pickup-point operation is unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, capability) {
		writeProblem(w, http.StatusConflict, "Pickup-point capability is not enabled")
		return
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	points, err := api.runtime.PickupPoints(r.Context(), account, runtime, query)
	if err != nil {
		writeLogisticsError(w, err)
		return
	}
	views := make([]pickupPointView, 0, len(points))
	for _, point := range points {
		views = append(views, pickupPointView{RemoteID: point.RemoteID, Name: point.Name, Country: point.Country, City: point.City, Address: point.Address, Active: point.Active})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (api logisticsAPI) calculateRates(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.accounts == nil || api.secrets == nil || api.runtime == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var input logisticsRatesInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	request := input.toSDK()
	accountID := strings.TrimSpace(input.ConnectorAccountID)
	if accountID == "" || request.Validate() != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID)
	if err != nil || account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
		writeProblem(w, http.StatusConflict, "Logistics connector account unavailable")
		return
	}
	const capability = sdk.Capability("logistics.rates.read")
	if !supportsLogisticsCapability(api.runtime, account, capability) {
		writeProblem(w, http.StatusConflict, "Rate operation is unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, capability) {
		writeProblem(w, http.StatusConflict, "Rate capability is not enabled")
		return
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	quotes, err := api.runtime.LogisticsRates(r.Context(), account, runtime, request)
	if err != nil {
		writeLogisticsError(w, err)
		return
	}
	views := make([]logisticsRateView, 0, len(quotes))
	for _, quote := range quotes {
		if quote.Validate() != nil {
			writeProblem(w, http.StatusBadGateway, "Logistics provider unavailable")
			return
		}
		views = append(views, logisticsRateView{
			OptionID:      logisticsRateOptionID(account.ID, quote.ServiceCode),
			Cost:          moneyView{MinorUnits: quote.Cost.MinorUnits, Currency: quote.Cost.Currency},
			MinDeliveryAt: quote.MinDeliveryAt,
			MaxDeliveryAt: quote.MaxDeliveryAt,
			ObservedAt:    quote.ObservedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (api logisticsAPI) readTracking(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.accounts == nil || api.secrets == nil || api.runtime == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	accountID := strings.TrimSpace(r.URL.Query().Get("connector_account_id"))
	remoteID := strings.TrimSpace(r.URL.Query().Get("remote_id"))
	if accountID == "" || !logisticsRemoteIDPattern.MatchString(remoteID) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID)
	if err != nil || account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
		writeProblem(w, http.StatusConflict, "Logistics connector account unavailable")
		return
	}
	const capability = sdk.Capability("logistics.track.read")
	if !supportsLogisticsCapability(api.runtime, account, capability) {
		writeProblem(w, http.StatusConflict, "Tracking operation is unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, capability) {
		writeProblem(w, http.StatusConflict, "Tracking capability is not enabled")
		return
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	result, err := api.runtime.LogisticsTracking(r.Context(), account, runtime, sdk.ShipmentStatusRequest{RemoteID: remoteID})
	if err != nil {
		writeLogisticsError(w, err)
		return
	}
	if result.RemoteID == "" || len(result.RemoteID) > 192 || result.Status == "" || len(result.Status) > 64 || len(result.TrackingNumber) > 192 || result.ObservedAt.IsZero() {
		writeProblem(w, http.StatusBadGateway, "Logistics provider unavailable")
		return
	}
	writeJSON(w, http.StatusOK, logisticsTrackingView{RemoteID: result.RemoteID, Status: result.Status, TrackingNumber: result.TrackingNumber, ObservedAt: result.ObservedAt})
}

func (input logisticsRatesInput) toSDK() sdk.RateRequest {
	address := func(value logisticsAddressInput) sdk.Address {
		return sdk.Address{Country: strings.ToUpper(strings.TrimSpace(value.Country)), PostalCode: strings.TrimSpace(value.PostalCode), City: strings.TrimSpace(value.City), Line1: strings.TrimSpace(value.Line1)}
	}
	parcels := make([]sdk.Parcel, 0, len(input.Parcels))
	for _, parcel := range input.Parcels {
		parcels = append(parcels, sdk.Parcel{WeightGrams: parcel.WeightGrams, LengthMM: parcel.LengthMM, WidthMM: parcel.WidthMM, HeightMM: parcel.HeightMM})
	}
	return sdk.RateRequest{From: address(input.From), To: address(input.To), Parcels: parcels}
}

func logisticsRateOptionID(accountID, serviceCode string) string {
	digest := sha256.Sum256([]byte(accountID + "\x00" + serviceCode))
	return "option-" + hex.EncodeToString(digest[:12])
}

// supportsLogisticsCapability keeps the route capability-driven without
// making the provider identity part of an authorization branch. The runtime
// registry owns the connector lookup; this API only asks whether the declared
// capability has an executable route.
func supportsLogisticsCapability(runtime logisticsRuntime, account sdk.Account, capability sdk.Capability) bool {
	if runtime == nil {
		return false
	}
	return runtime.SupportsCapability(account.ConnectorID, string(capability))
}

func writeLogisticsError(w http.ResponseWriter, err error) {
	var remote *sdk.RemoteError
	switch {
	case errors.Is(err, builtinruntime.ErrUnavailable), errors.Is(err, builtinruntime.ErrConfigurationNeeded):
		writeProblem(w, http.StatusConflict, "Logistics operation unavailable")
	case errors.Is(err, sdk.ErrInvalidLogisticsRequest), errors.Is(err, sdk.ErrInvalidPickupRequest):
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	case errors.As(err, &remote) && remote.Category == sdk.ErrorInvalidRequest:
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	default:
		writeProblem(w, http.StatusBadGateway, "Logistics provider unavailable")
	}
}
