package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const logisticsPickupPointsPath = "/api/v1/logistics/pickup-points"

// logisticsCapabilityStore is kept separate from the concrete PostgreSQL
// repository so the operation remains testable without a database.
type logisticsCapabilityStore interface {
	AccountByID(context.Context, string, string, string) (sdk.Account, error)
	AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error)
}

type logisticsRuntime interface {
	SupportsCapability(string, string) bool
	PickupPoints(context.Context, sdk.Account, sdk.Runtime, sdk.PickupPointQuery) ([]sdk.PickupPoint, error)
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

func newLogisticsRoutes(accounts logisticsCapabilityStore, secretProvider secrets.SecretProvider, runtime logisticsRuntime) []ProtectedRoute {
	api := logisticsAPI{accounts: accounts, secrets: secretProvider, runtime: runtime}
	return []ProtectedRoute{{Method: http.MethodGet, Path: logisticsPickupPointsPath, Permission: "connectors.read", Handler: http.HandlerFunc(api.listPickupPoints)}}
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
