package api

import (
	"errors"
	"net/http"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/notifications"
)

type contextNotificationIdentityResolver struct{}

func (contextNotificationIdentityResolver) NotificationIdentity(r *http.Request) (tenancy.Scope, string, error) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	if !scopeOK || !principalOK || principal.Subject == "" {
		return tenancy.Scope{}, "", errors.New("notification identity unavailable")
	}
	return scope, principal.Subject, nil
}

func newNotificationRoutes(service *notifications.Service) []ProtectedRoute {
	resolver := contextNotificationIdentityResolver{}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: NotificationsPath, Permission: "notifications.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			notificationList(w, r, service, resolver)
		})},
		{Method: http.MethodHead, Path: NotificationsPath, Permission: "notifications.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			notificationList(w, r, service, resolver)
		})},
		{Method: http.MethodPost, Path: NotificationsPath + "/", PathPrefix: true, Permission: "notifications.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			notificationAction(w, r, service, resolver)
		})},
		{Method: http.MethodGet, Path: NotificationsPath + "/", PathPrefix: true, Permission: "notifications.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			notificationAction(w, r, service, resolver)
		})},
		{Method: http.MethodHead, Path: NotificationsPath + "/", PathPrefix: true, Permission: "notifications.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			notificationAction(w, r, service, resolver)
		})},
		{Method: http.MethodGet, Path: NotificationPreferencesPath, PathPrefix: true, Permission: "notifications.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { notificationPreference(w, r, service, resolver) })},
		{Method: http.MethodPut, Path: NotificationPreferencesPath, PathPrefix: true, Permission: "notifications.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { notificationPreference(w, r, service, resolver) })},
	}
}
