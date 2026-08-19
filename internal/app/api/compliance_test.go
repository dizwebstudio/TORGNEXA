package api

import (
	"context"
	"github.com/torgnexa/torgnexa/internal/core/compliance"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type compScope struct{}

func (compScope) ComplianceScope(*http.Request) (compliance.Scope, error) {
	return compliance.ParseScope("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002")
}

type compEval struct{}

func (compEval) Evaluate(context.Context, compliance.Scope, compliance.EvaluationContext) (compliance.Evaluation, error) {
	return compliance.Evaluation{Outcome: compliance.OutcomeBlock, EvaluatedAt: time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC), FingerprintSHA256: strings.Repeat("a", 64)}, nil
}
func TestComplianceEvaluateAPI(t *testing.T) {
	body := `{"operation":"publication","jurisdiction":"RU","product_id":"018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003","at":"2026-08-10T01:00:00Z"}`
	r := httptest.NewRequest(http.MethodPost, ComplianceEvaluatePath, strings.NewReader(body))
	w := httptest.NewRecorder()
	newHandlerWithComplianceEvaluation(slog.Default(), compEval{}, compScope{}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"block"`) {
		t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
}
