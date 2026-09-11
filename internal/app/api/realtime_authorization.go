package api

import (
	"context"
	"net/http"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

type requestReauthorizationKey struct{}

// The composition attaches only trusted dependencies and the original binding.
// No request, bearer token, profile or email is copied into this context value.
type requestReauthorization struct {
	deps                   securityDependencies
	issuer, subject        string
	sessionRef, subjectRef string
	expiresAt              time.Time
	scope                  tenancy.Scope
	permission             string
}

func (a requestReauthorization) check(ctx context.Context, request *http.Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	principal, err := a.deps.authenticator.Authenticate(ctx, request.WithContext(ctx))
	if err != nil || !principal.Valid() || principal.Issuer != a.issuer || principal.Subject != a.subject ||
		principal.SessionRef != a.sessionRef || principal.SubjectRef != a.subjectRef ||
		!principal.ExpiresAt.Equal(a.expiresAt) || !time.Now().Before(principal.ExpiresAt) {
		return ErrUnauthenticated
	}
	ctx = context.WithValue(ctx, requestIdentityKey{}, principal)
	scope, err := a.deps.tenant.ResolveTenant(ctx, principal, request.WithContext(ctx))
	if err != nil || !scope.Valid() || scope.OrganizationID() != a.scope.OrganizationID() || scope.WorkspaceID() != a.scope.WorkspaceID() {
		return ErrUnauthorized
	}
	ctx = context.WithValue(ctx, requestScopeKey{}, scope)
	if err := a.deps.authorizer.Authorize(ctx, principal, scope, a.permission); err != nil {
		return ErrUnauthorized
	}
	// A dependency may have returned just as its deadline elapsed. A late
	// successful check never revives an expired/cancelled authorization.
	return ctx.Err()
}

func authorizeRealtimeLifetime(request *http.Request, scope tenancy.Scope, interval, timeout time.Duration) (*http.Request, func(), error) {
	access, ok := request.Context().Value(requestReauthorizationKey{}).(requestReauthorization)
	if !ok || access.deps.authenticator == nil || access.deps.tenant == nil || access.deps.authorizer == nil ||
		access.permission == "" || !scope.Valid() || access.scope.OrganizationID() != scope.OrganizationID() || access.scope.WorkspaceID() != scope.WorkspaceID() {
		return nil, nil, ErrUnauthorized
	}
	if access.expiresAt.IsZero() || !time.Now().Before(access.expiresAt) {
		return nil, nil, ErrUnauthenticated
	}
	ctx, cancel := context.WithDeadline(request.Context(), access.expiresAt)
	streamRequest := request.WithContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkCtx, stopCheck := context.WithTimeout(ctx, timeout)
				err := access.check(checkCtx, streamRequest)
				stopCheck()
				if err != nil {
					return
				}
			}
		}
	}()
	// All concrete security dependencies honor cancellation. Join the monitor
	// before returning the HTTP handler so checks do not outlive this request.
	stop := func() { cancel(); <-done }
	return streamRequest, stop, nil
}
