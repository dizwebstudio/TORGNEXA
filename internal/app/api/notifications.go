package api

import (
	"bytes"
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
	"github.com/torgnexa/torgnexa/internal/platform/notifications"
)

const (
	NotificationsPath           = "/api/v1/notifications"
	NotificationPreferencesPath = "/api/v1/notification-preferences/"
)

type NotificationIdentityResolver interface {
	NotificationIdentity(*http.Request) (tenancy.Scope, string, error)
}
type NotificationService interface {
	List(context.Context, tenancy.Scope, string, int) ([]notifications.Notification, error)
	MarkRead(context.Context, tenancy.Scope, string, string) (notifications.Notification, error)
	PutPreference(context.Context, tenancy.Scope, notifications.Preference) (notifications.Preference, error)
	GetPreference(context.Context, tenancy.Scope, string, notifications.Channel) (notifications.Preference, error)
	Deliveries(context.Context, tenancy.Scope, string, string) ([]notifications.Delivery, error)
}

func newHandlerWithNotifications(logger *slog.Logger, service NotificationService, resolver NotificationIdentityResolver) http.Handler {
	return recoverPanics(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == NotificationsPath:
			notificationList(w, r, service, resolver)
		case strings.HasPrefix(r.URL.Path, NotificationsPath+"/"):
			notificationAction(w, r, service, resolver)
		case strings.HasPrefix(r.URL.Path, NotificationPreferencesPath):
			notificationPreference(w, r, service, resolver)
		default:
			route(w, r)
		}
	}))
}

func notificationIdentity(w http.ResponseWriter, r *http.Request, resolver NotificationIdentityResolver) (tenancy.Scope, string, bool) {
	if resolver == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return tenancy.Scope{}, "", false
	}
	scope, recipient, err := resolver.NotificationIdentity(r)
	if err != nil || !scope.Valid() || recipient == "" {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return tenancy.Scope{}, "", false
	}
	return scope, recipient, true
}
func notificationList(w http.ResponseWriter, r *http.Request, s NotificationService, resolver NotificationIdentityResolver) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	if s == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	scope, recipient, ok := notificationIdentity(w, r, resolver)
	if !ok {
		return
	}
	if r.URL.Query().Has("recipient_id") || r.URL.Query().Has("organization_id") || r.URL.Query().Has("workspace_id") {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > notifications.MaxPageSize {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		limit = v
	}
	items, err := s.List(r.Context(), scope, recipient, limit)
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	writeNotificationJSON(w, r, notifications.Page{Items: items})
}
func notificationAction(w http.ResponseWriter, r *http.Request, s NotificationService, resolver NotificationIdentityResolver) {
	if s == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	scope, recipient, ok := notificationIdentity(w, r, resolver)
	if !ok {
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, NotificationsPath+"/")
	parts := strings.Split(tail, "/")
	if len(parts) != 2 || parts[0] == "" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	switch parts[1] {
	case "read":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}
		n, err := s.MarkRead(r.Context(), scope, recipient, parts[0])
		if err != nil {
			writeNotificationError(w, err)
			return
		}
		writeNotificationJSON(w, r, n)
	case "deliveries":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}
		ds, err := s.Deliveries(r.Context(), scope, recipient, parts[0])
		if err != nil {
			writeNotificationError(w, err)
			return
		}
		writeNotificationJSON(w, r, struct {
			Items []notifications.Delivery `json:"items"`
		}{ds})
	default:
		writeProblem(w, http.StatusNotFound, "Not Found")
	}
}
func notificationPreference(w http.ResponseWriter, r *http.Request, s NotificationService, resolver NotificationIdentityResolver) {
	if s == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	scope, recipient, ok := notificationIdentity(w, r, resolver)
	if !ok {
		return
	}
	raw := strings.TrimPrefix(r.URL.Path, NotificationPreferencesPath)
	if strings.Contains(raw, "/") || raw == "" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	ch := notifications.Channel(raw)
	if !ch.Valid() {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		out, err := s.GetPreference(r.Context(), scope, recipient, ch)
		if err != nil {
			writeNotificationError(w, err)
			return
		}
		writeNotificationJSON(w, r, out)
		return
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, HEAD, PUT")
		writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	var input struct {
		Enabled         *bool                  `json:"enabled"`
		MinSeverity     notifications.Severity `json:"min_severity"`
		Categories      []string               `json:"categories"`
		QuietEnabled    *bool                  `json:"quiet_enabled"`
		QuietStart      string                 `json:"quiet_start"`
		QuietEnd        string                 `json:"quiet_end"`
		Timezone        string                 `json:"timezone"`
		ExpectedVersion *int64                 `json:"expected_version"`
	}
	if err := decodeNotificationJSON(r, &input); err != nil || input.Enabled == nil || !input.MinSeverity.Valid() {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	defaults := notifications.DefaultPreference(recipient, ch, time.Now().UTC())
	if input.ExpectedVersion == nil {
		current, getErr := s.GetPreference(r.Context(), scope, recipient, ch)
		if getErr != nil {
			writeNotificationError(w, getErr)
			return
		}
		input.ExpectedVersion = &current.Version
	}
	if *input.ExpectedVersion < 1 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	quiet := defaults.QuietEnabled
	if input.QuietEnabled != nil {
		quiet = *input.QuietEnabled
	}
	if len(input.Categories) == 0 {
		input.Categories = defaults.Categories
	}
	if input.QuietStart == "" {
		input.QuietStart = defaults.QuietStart
	}
	if input.QuietEnd == "" {
		input.QuietEnd = defaults.QuietEnd
	}
	if input.Timezone == "" {
		input.Timezone = defaults.Timezone
	}
	p := notifications.Preference{RecipientID: recipient, Channel: ch, Enabled: *input.Enabled, MinSeverity: input.MinSeverity, Categories: input.Categories, QuietEnabled: quiet, QuietStart: input.QuietStart, QuietEnd: input.QuietEnd, Timezone: input.Timezone, Version: *input.ExpectedVersion, UpdatedAt: time.Now().UTC()}
	out, err := s.PutPreference(r.Context(), scope, p)
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	writeNotificationJSON(w, r, out)
}
func decodeNotificationJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	const maxBody = 32 << 10
	limited := io.LimitReader(r.Body, maxBody+1)
	raw, err := io.ReadAll(limited)
	if err != nil || len(raw) == 0 || len(raw) > maxBody {
		return errors.New("invalid")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing")
	}
	return nil
}
func writeNotificationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, notifications.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	case errors.Is(err, notifications.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found")
	case errors.Is(err, notifications.ErrConflict):
		writeProblem(w, http.StatusConflict, "Conflict")
	default:
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
	}
}
func writeNotificationJSON(w http.ResponseWriter, r *http.Request, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_ = jsonEncode(w, v)
	}
}
