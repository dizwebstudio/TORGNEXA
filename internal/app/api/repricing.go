package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/repricing"
)

const RepricingPreviewPath = "/api/v1/pricing/repricing/preview"

type repricingPreviewRequest struct {
	RunID      string                     `json:"run_id"`
	Candidates []repricing.CandidateInput `json:"candidates"`
}

func newRepricingRoutes() []ProtectedRoute {
	return []ProtectedRoute{{Method: http.MethodPost, Path: RepricingPreviewPath, Permission: "products.read", Handler: http.HandlerFunc(repricingPreview)}}
}

func repricingPreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := ScopeFromContext(r.Context()); !ok {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var input repricingPreviewRequest
	if decodeStrictJSON(r, &input) != nil || len(input.Candidates) == 0 || len(input.Candidates) > 1000 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		runID = newApprovalID()
	}
	preview, err := repricing.BuildPreview(runID, input.Candidates, time.Now().UTC())
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid repricing preview")
		return
	}
	writeJSON(w, http.StatusOK, preview)
}
