package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/webhooks"
)

const (
	WebhookSubscriptionsPath = "/api/v1/webhook-subscriptions"
	WebhookDeliveriesPrefix  = "/api/v1/webhook-deliveries/"
)

type WebhookScopeResolver interface {
	WebhookScope(*http.Request) (tenancy.Scope, error)
}

// webhookService captures the provider-neutral management operations without exposing secret references.
type webhookService interface {
	CreateSubscription(ctx context.Context, scope tenancy.Scope, id, endpoint string, eventTypes []eventbus.EventType, signingMaterial []byte) (webhooks.Subscription, error)
	ListSubscriptions(context.Context, tenancy.Scope) ([]webhooks.Subscription, error)
	DisableSubscription(context.Context, tenancy.Scope, string) error
	RotateSigningSecret(context.Context, tenancy.Scope, string, []byte, time.Duration) (webhooks.Subscription, error)
	Replay(context.Context, tenancy.Scope, string) (webhooks.Delivery, error)
	DeliveryHistory(context.Context, tenancy.Scope, string, int) ([]webhooks.HistoryEntry, error)
}

func newHandlerWithWebhooks(logger *slog.Logger, manager webhookService, resolver WebhookScopeResolver) http.Handler {
	return recoverPanics(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == WebhookSubscriptionsPath:
			webhookSubscriptions(w, r, manager, resolver)
		case strings.HasPrefix(r.URL.Path, WebhookSubscriptionsPath+"/"):
			webhookSubscriptionAction(w, r, manager, resolver)
		case strings.HasPrefix(r.URL.Path, WebhookDeliveriesPrefix):
			webhookDeliveryAction(w, r, manager, resolver)
		default:
			route(w, r)
		}
	}))
}

func newWebhookRoutes(manager webhookService) []ProtectedRoute {
	resolver := productionScopeResolver{}
	subscriptions := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { webhookSubscriptions(w, r, manager, resolver) })
	subscriptionAction := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { webhookSubscriptionAction(w, r, manager, resolver) })
	deliveryAction := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { webhookDeliveryAction(w, r, manager, resolver) })
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: WebhookSubscriptionsPath, Permission: "webhooks.read", Handler: subscriptions},
		{Method: http.MethodHead, Path: WebhookSubscriptionsPath, Permission: "webhooks.read", Handler: subscriptions},
		{Method: http.MethodPost, Path: WebhookSubscriptionsPath, Permission: "webhooks.write", Handler: subscriptions},
		{Method: http.MethodDelete, Path: WebhookSubscriptionsPath + "/", PathPrefix: true, Permission: "webhooks.write", Handler: subscriptionAction},
		{Method: http.MethodPost, Path: WebhookSubscriptionsPath + "/", PathPrefix: true, Permission: "webhooks.write", Handler: subscriptionAction},
		{Method: http.MethodPost, Path: WebhookDeliveriesPrefix, PathPrefix: true, Permission: "webhooks.write", Handler: deliveryAction},
		{Method: http.MethodGet, Path: WebhookDeliveriesPrefix, PathPrefix: true, Permission: "webhooks.read", Handler: deliveryAction},
		{Method: http.MethodHead, Path: WebhookDeliveriesPrefix, PathPrefix: true, Permission: "webhooks.read", Handler: deliveryAction},
	}
}

type createWebhookRequest struct {
	ID            string   `json:"id"`
	Endpoint      string   `json:"endpoint"`
	EventTypes    []string `json:"event_types"`
	SigningSecret string   `json:"signing_secret"`
}
type rotateWebhookRequest struct {
	SigningSecret  string `json:"signing_secret"`
	OverlapSeconds int64  `json:"overlap_seconds"`
}
type subscriptionsResponse struct {
	Items []webhooks.Subscription `json:"items"`
}
type replayResponse struct {
	DeliveryID string                  `json:"delivery_id"`
	ReplayOf   string                  `json:"replay_of"`
	Status     webhooks.DeliveryStatus `json:"status"`
}
type historyResponse struct {
	Items []webhooks.HistoryEntry `json:"items"`
}

func webhookSubscriptions(w http.ResponseWriter, r *http.Request, manager webhookService, resolver WebhookScopeResolver) {
	if manager == nil || resolver == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	scope, ok := webhookAPIScope(w, r, resolver)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		items, err := manager.ListSubscriptions(r.Context(), scope)
		if err != nil {
			webhookAPIError(w, err)
			return
		}
		writeWebhookJSON(w, r, http.StatusOK, subscriptionsResponse{Items: items})
	case http.MethodPost:
		var in createWebhookRequest
		if !decodeWebhookJSON(w, r, &in, 16<<10) {
			return
		}
		types := make([]eventbus.EventType, len(in.EventTypes))
		for i, raw := range in.EventTypes {
			typ, err := eventbus.ParseEventType(raw)
			if err != nil {
				writeProblem(w, http.StatusBadRequest, "Bad Request")
				return
			}
			types[i] = typ
		}
		material := []byte(in.SigningSecret)
		defer wipeBytes(material)
		sub, err := manager.CreateSubscription(r.Context(), scope, in.ID, in.Endpoint, types, material)
		if err != nil {
			webhookAPIError(w, err)
			return
		}
		writeWebhookJSON(w, r, http.StatusCreated, sub)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
	}
}

func webhookSubscriptionAction(w http.ResponseWriter, r *http.Request, manager webhookService, resolver WebhookScopeResolver) {
	tail := strings.TrimPrefix(r.URL.Path, WebhookSubscriptionsPath+"/")
	parts := strings.Split(tail, "/")
	if len(parts) == 1 && parts[0] != "" {
		if r.Method != http.MethodDelete {
			w.Header().Set("Allow", "DELETE")
			writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}
		scope, ok := webhookAPIScope(w, r, resolver)
		if !ok {
			return
		}
		if err := manager.DisableSubscription(r.Context(), scope, parts[0]); err != nil {
			webhookAPIError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(parts) != 2 || parts[0] == "" || parts[1] != "rotate-secret" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	scope, ok := webhookAPIScope(w, r, resolver)
	if !ok {
		return
	}
	var in rotateWebhookRequest
	if !decodeWebhookJSON(w, r, &in, 8<<10) {
		return
	}
	material := []byte(in.SigningSecret)
	defer wipeBytes(material)
	sub, err := manager.RotateSigningSecret(r.Context(), scope, parts[0], material, time.Duration(in.OverlapSeconds)*time.Second)
	if err != nil {
		webhookAPIError(w, err)
		return
	}
	writeWebhookJSON(w, r, http.StatusOK, sub)
}

func webhookDeliveryAction(w http.ResponseWriter, r *http.Request, manager webhookService, resolver WebhookScopeResolver) {
	tail := strings.TrimPrefix(r.URL.Path, WebhookDeliveriesPrefix)
	parts := strings.Split(tail, "/")
	if len(parts) != 2 || parts[0] == "" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	scope, ok := webhookAPIScope(w, r, resolver)
	if !ok {
		return
	}
	switch parts[1] {
	case "replay":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}
		delivery, err := manager.Replay(r.Context(), scope, parts[0])
		if err != nil {
			webhookAPIError(w, err)
			return
		}
		writeWebhookJSON(w, r, http.StatusAccepted, replayResponse{DeliveryID: delivery.ID, ReplayOf: delivery.ReplayOf, Status: delivery.Status})
	case "history":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v < 1 || v > 100 {
				writeProblem(w, http.StatusBadRequest, "Bad Request")
				return
			}
			limit = v
		}
		items, err := manager.DeliveryHistory(r.Context(), scope, parts[0], limit)
		if err != nil {
			webhookAPIError(w, err)
			return
		}
		writeWebhookJSON(w, r, http.StatusOK, historyResponse{Items: items})
	default:
		writeProblem(w, http.StatusNotFound, "Not Found")
	}
}

func webhookAPIScope(w http.ResponseWriter, r *http.Request, resolver WebhookScopeResolver) (tenancy.Scope, bool) {
	scope, err := resolver.WebhookScope(r)
	if err != nil || !scope.Valid() {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return tenancy.Scope{}, false
	}
	return scope, true
}
func decodeWebhookJSON(w http.ResponseWriter, r *http.Request, out any, maxBytes int64) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return false
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return false
	}
	return true
}
func webhookAPIError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, webhooks.ErrInvalid), errors.Is(err, webhooks.ErrUnsafeURL):
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	case errors.Is(err, webhooks.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found")
	case errors.Is(err, webhooks.ErrConflict):
		writeProblem(w, http.StatusConflict, "Conflict")
	default:
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
	}
}
func writeWebhookJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(value)
	}
}
func wipeBytes(v []byte) {
	for i := range v {
		v[i] = 0
	}
}
