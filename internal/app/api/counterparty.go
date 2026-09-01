package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/legalparty"
)

type counterpartyWriter interface {
	legalPartyMasterReader
	Counterparty(context.Context, legalparty.Scope, legalparty.ID) (legalparty.Counterparty, error)
	CreateCounterparty(context.Context, legalparty.Scope, legalparty.Counterparty, legalparty.Mutation) (legalparty.Counterparty, error)
}

type counterpartyCreateInput struct {
	Code      string                      `json:"code"`
	PartyType legalparty.PartyType        `json:"party_type"`
	PartyID   string                      `json:"party_id"`
	Role      legalparty.CounterpartyRole `json:"role"`
}

func newCounterpartyWriteRoutes(writer counterpartyWriter) []ProtectedRoute {
	return []ProtectedRoute{{Method: http.MethodPost, Path: "/api/v1/counterparties", Permission: "counterparties.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if writer == nil {
			writeProblem(w, http.StatusServiceUnavailable, "Counterparty writer is unavailable")
			return
		}
		createCounterparty(w, r, writer)
	})}}
}

func createCounterparty(w http.ResponseWriter, r *http.Request, writer counterpartyWriter) {
	scope, scopeErr := productionScopeResolver{}.LegalPartyScope(r)
	principal, identified := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var input counterpartyCreateInput
	if scopeErr != nil || !scope.Valid() || !identified || !principal.Valid() || writer == nil || !validIdempotencyKey(key) || decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	input.Code = strings.TrimSpace(input.Code)
	input.PartyID = strings.TrimSpace(input.PartyID)
	partyID, partyErr := legalparty.ParseID(input.PartyID)
	if partyErr != nil || !input.PartyType.Valid() || !input.Role.Valid() || input.Code == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if err := counterpartyMasterExists(r.Context(), scope, input.PartyType, partyID, writer); err != nil {
		if errors.Is(err, legalparty.ErrNotFound) {
			writeProblem(w, http.StatusUnprocessableEntity, "Legal party not found")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	id, parseErr := legalparty.ParseID(stableLegalPartyID("counterparty", scope, key))
	if parseErr != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	now := time.Now().UTC()
	created, err := writer.CreateCounterparty(r.Context(), scope, legalparty.Counterparty{ID: id, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Code: input.Code, PartyType: input.PartyType, PartyID: partyID, Role: input.Role, Status: legalparty.StatusDraft, Version: 1, CreatedAt: now, UpdatedAt: now}, legalPartyMutation(principal, key, now))
	if err != nil {
		if errors.Is(err, legalparty.ErrConflict) {
			if existing, lookupErr := writer.Counterparty(r.Context(), scope, id); lookupErr == nil && sameCounterparty(existing, input, partyID) {
				writeJSON(w, http.StatusCreated, counterpartyView(existing))
				return
			}
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		writeLegalPartyWriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, counterpartyView(created))
}

func counterpartyMasterExists(ctx context.Context, scope legalparty.Scope, partyType legalparty.PartyType, partyID legalparty.ID, reader legalPartyMasterReader) error {
	switch partyType {
	case legalparty.PartyLegalEntity:
		_, err := reader.LegalEntity(ctx, scope, partyID)
		return err
	case legalparty.PartyIndividualEntrepreneur:
		_, err := reader.IndividualEntrepreneur(ctx, scope, partyID)
		return err
	case legalparty.PartyBranch:
		_, err := reader.Branch(ctx, scope, partyID)
		return err
	default:
		return legalparty.ErrInvalid
	}
}

func sameCounterparty(v legalparty.Counterparty, input counterpartyCreateInput, partyID legalparty.ID) bool {
	return v.Code == input.Code && v.PartyType == input.PartyType && v.PartyID == partyID && v.Role == input.Role
}

func counterpartyView(v legalparty.Counterparty) map[string]any {
	return map[string]any{"id": v.ID, "code": v.Code, "party_type": v.PartyType, "party_id": v.PartyID, "role": v.Role, "status": v.Status, "version": v.Version, "created_at": v.CreatedAt, "updated_at": v.UpdatedAt}
}
