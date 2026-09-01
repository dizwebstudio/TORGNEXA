package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/legalparty"
)

type legalPartyWriteStub struct {
	entity       legalparty.LegalEntity
	counterparty legalparty.Counterparty
}

func (s *legalPartyWriteStub) LegalEntity(context.Context, legalparty.Scope, legalparty.ID) (legalparty.LegalEntity, error) {
	if s.entity.ID == "" {
		return legalparty.LegalEntity{}, legalparty.ErrNotFound
	}
	return s.entity, nil
}
func (s *legalPartyWriteStub) IndividualEntrepreneur(context.Context, legalparty.Scope, legalparty.ID) (legalparty.IndividualEntrepreneur, error) {
	return legalparty.IndividualEntrepreneur{}, legalparty.ErrNotFound
}
func (s *legalPartyWriteStub) Branch(context.Context, legalparty.Scope, legalparty.ID) (legalparty.Branch, error) {
	return legalparty.Branch{}, legalparty.ErrNotFound
}
func (s *legalPartyWriteStub) CreateLegalEntity(_ context.Context, _ legalparty.Scope, value legalparty.LegalEntity, _ legalparty.Mutation) (legalparty.LegalEntity, error) {
	if s.entity.ID != "" {
		return legalparty.LegalEntity{}, legalparty.ErrConflict
	}
	s.entity = value
	return value, nil
}
func (s *legalPartyWriteStub) CreateIndividualEntrepreneur(context.Context, legalparty.Scope, legalparty.IndividualEntrepreneur, legalparty.Mutation) (legalparty.IndividualEntrepreneur, error) {
	return legalparty.IndividualEntrepreneur{}, errors.New("not used")
}
func (s *legalPartyWriteStub) CreateBranch(context.Context, legalparty.Scope, legalparty.Branch, legalparty.Mutation) (legalparty.Branch, error) {
	return legalparty.Branch{}, errors.New("not used")
}
func (s *legalPartyWriteStub) Counterparty(context.Context, legalparty.Scope, legalparty.ID) (legalparty.Counterparty, error) {
	if s.counterparty.ID == "" {
		return legalparty.Counterparty{}, legalparty.ErrNotFound
	}
	return s.counterparty, nil
}
func (s *legalPartyWriteStub) CreateCounterparty(_ context.Context, _ legalparty.Scope, value legalparty.Counterparty, _ legalparty.Mutation) (legalparty.Counterparty, error) {
	if s.counterparty.ID != "" {
		return legalparty.Counterparty{}, legalparty.ErrConflict
	}
	s.counterparty = value
	return value, nil
}

func TestCreateLegalPartyIsIdempotentForTheSameRequest(t *testing.T) {
	store := &legalPartyWriteStub{}
	body := `{"party_type":"legal_entity","code":"acme-us","legal_name":"Acme LLC","country_code":"US"}`
	for attempt := 0; attempt < 2; attempt++ {
		request := httptest.NewRequest(http.MethodPost, LegalPartyPath, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "party-acme-us")
		response := httptest.NewRecorder()
		createLegalParty(response, productionRequestContext(t, request), store)
		if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"party_type":"legal_entity"`) {
			t.Fatalf("attempt %d: status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
}

func TestCreateCounterpartyRequiresCanonicalMasterAndIsRetrySafe(t *testing.T) {
	store := &legalPartyWriteStub{}
	request := httptest.NewRequest(http.MethodPost, LegalPartyPath, strings.NewReader(`{"party_type":"legal_entity","code":"acme-us","legal_name":"Acme LLC","country_code":"US"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "party-acme-us")
	response := httptest.NewRecorder()
	createLegalParty(response, productionRequestContext(t, request), store)
	if response.Code != http.StatusCreated {
		t.Fatalf("master status=%d body=%s", response.Code, response.Body.String())
	}

	body := `{"code":"acme-supplier","party_type":"legal_entity","party_id":"` + store.entity.ID.String() + `","role":"supplier"}`
	for attempt := 0; attempt < 2; attempt++ {
		request = httptest.NewRequest(http.MethodPost, "/api/v1/counterparties", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "role-acme-supplier")
		response = httptest.NewRecorder()
		createCounterparty(response, productionRequestContext(t, request), store)
		if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"role":"supplier"`) {
			t.Fatalf("attempt %d: status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
}

func TestCreateCounterpartyRejectsMissingMaster(t *testing.T) {
	store := &legalPartyWriteStub{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/counterparties", strings.NewReader(`{"code":"unknown","party_type":"legal_entity","party_id":"018f0000-0000-7000-8000-000000000001","role":"customer"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "role-unknown")
	response := httptest.NewRecorder()
	createCounterparty(response, productionRequestContext(t, request), store)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
