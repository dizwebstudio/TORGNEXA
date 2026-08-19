package api

import (
	"errors"
	"net/http"

	"github.com/torgnexa/torgnexa/internal/core/legalparty"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/entitlements"
	"github.com/torgnexa/torgnexa/internal/platform/lineage"
)

var errProductionScopeMissing = errors.New("api: production scope missing")

// productionScopeResolver adapts the scope installed by the mandatory security
// composition to older domain-specific API interfaces. It never reads tenant
// identifiers from headers, query parameters, or request bodies.
type productionScopeResolver struct{}

func (productionScopeResolver) WebhookScope(r *http.Request) (tenancy.Scope, error) {
	if scope, ok := ScopeFromContext(r.Context()); ok {
		return scope, nil
	}
	return tenancy.Scope{}, errProductionScopeMissing
}

func (p productionScopeResolver) LineageScope(r *http.Request) (lineage.Scope, error) {
	scope, err := p.WebhookScope(r)
	if err != nil {
		return lineage.Scope{}, err
	}
	return lineage.NewScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
}

func (p productionScopeResolver) LegalPartyScope(r *http.Request) (legalparty.Scope, error) {
	scope, err := p.WebhookScope(r)
	if err != nil {
		return legalparty.Scope{}, err
	}
	return legalparty.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
}

func (p productionScopeResolver) EntitlementScope(r *http.Request) (entitlements.Scope, error) {
	scope, err := p.WebhookScope(r)
	if err != nil {
		return entitlements.Scope{}, err
	}
	return entitlements.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
}
