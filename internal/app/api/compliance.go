package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/compliance"
)

const ComplianceEvaluatePath = "/api/v1/compliance/evaluate"
const ComplianceDocumentsPath = "/api/v1/compliance/documents"

type ComplianceScopeResolver interface {
	ComplianceScope(*http.Request) (compliance.Scope, error)
}
type ComplianceEvaluator interface {
	Evaluate(context.Context, compliance.Scope, compliance.EvaluationContext) (compliance.Evaluation, error)
}

type contextComplianceScopeResolver struct{}

func (contextComplianceScopeResolver) ComplianceScope(r *http.Request) (compliance.Scope, error) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok {
		return compliance.Scope{}, ErrUnauthorized
	}
	return compliance.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
}

type complianceRepository interface {
	ComplianceEvaluator
	ListDocuments(context.Context, compliance.Scope, int) ([]compliance.ComplianceDocument, error)
}

func newComplianceRoutes(repository complianceRepository) []ProtectedRoute {
	resolver := contextComplianceScopeResolver{}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: ComplianceDocumentsPath, Permission: "compliance.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, err := resolver.ComplianceScope(r)
			if err != nil {
				writeProblem(w, http.StatusUnauthorized, "Unauthorized")
				return
			}
			docs, err := repository.ListDocuments(r.Context(), scope, 100)
			if err != nil {
				writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
				return
			}
			type view struct {
				ID                 string                    `json:"id"`
				DocumentType       compliance.DocumentType   `json:"document_type"`
				Number             string                    `json:"number"`
				Jurisdiction       string                    `json:"jurisdiction"`
				Issuer             string                    `json:"issuer"`
				RegistrySource     string                    `json:"registry_source"`
				RegistryReference  string                    `json:"registry_reference"`
				Status             compliance.DocumentStatus `json:"status"`
				IssuedAt           time.Time                 `json:"issued_at"`
				ExpiresAt          *time.Time                `json:"expires_at,omitempty"`
				VerificationSource string                    `json:"verification_source"`
				VerifiedAt         *time.Time                `json:"verified_at,omitempty"`
				Version            int64                     `json:"version"`
			}
			items := make([]view, 0, len(docs))
			for _, d := range docs {
				v := view{ID: d.ID.String(), DocumentType: d.Type, Number: d.Number, Jurisdiction: d.Jurisdiction, Issuer: d.Issuer, RegistrySource: d.RegistrySource, RegistryReference: d.RegistryReference, Status: d.Status, IssuedAt: d.IssuedAt, VerificationSource: d.VerificationSource, Version: d.Version}
				if !d.ExpiresAt.IsZero() {
					x := d.ExpiresAt
					v.ExpiresAt = &x
				}
				if !d.VerifiedAt.IsZero() {
					x := d.VerifiedAt
					v.VerifiedAt = &x
				}
				items = append(items, v)
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": items})
		})},
		{Method: http.MethodPost, Path: ComplianceEvaluatePath, Permission: "compliance.evaluate", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { complianceEvaluate(w, r, repository, resolver) })},
	}
}

// newHandlerWithComplianceEvaluation exposes a read/evaluate surface only. Mutating
// publication still goes through the host-side connector sandbox guard.
func newHandlerWithComplianceEvaluation(logger *slog.Logger, evaluator ComplianceEvaluator, resolver ComplianceScopeResolver) http.Handler {
	return recoverPanics(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ComplianceEvaluatePath {
			complianceEvaluate(w, r, evaluator, resolver)
			return
		}
		route(w, r)
	}))
}
func complianceEvaluate(w http.ResponseWriter, r *http.Request, evaluator ComplianceEvaluator, resolver ComplianceScopeResolver) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	if evaluator == nil || resolver == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	scope, err := resolver.ComplianceScope(r)
	if err != nil || !scope.Valid() {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	var req compliance.EvaluationContext
	if err = dec.Decode(&req); err != nil || req.Validate() != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	result, err := evaluator.Evaluate(r.Context(), scope, req)
	if err != nil {
		if errors.Is(err, compliance.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_ = jsonEncode(w, result)
}
