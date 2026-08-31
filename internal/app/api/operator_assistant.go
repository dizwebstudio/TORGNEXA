package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	coreinventory "github.com/torgnexa/torgnexa/internal/core/inventory"
	"github.com/torgnexa/torgnexa/internal/core/operatorassistant"
	corereturns "github.com/torgnexa/torgnexa/internal/core/returns"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inventoryrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/operatorassistantrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/publicationqualityrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/reconciliationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/reportrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/returnsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/syncrepo"
)

const (
	AssistantSessionsPath = "/api/v1/assistant/sessions"
	AssistantRunsPath     = "/api/v1/assistant/runs/"
	AssistantFeedbackPath = "/api/v1/assistant/feedback"
	AssistantApprovePath  = "/api/v1/assistant/action-previews/"
)

type operatorAssistantRepository interface {
	CreateSession(context.Context, tenancy.Scope, operatorassistant.Session) (operatorassistant.Session, error)
	ListSessions(context.Context, tenancy.Scope, string, int) ([]operatorassistant.Session, error)
	GetSession(context.Context, tenancy.Scope, string, string) (operatorassistant.Session, error)
	CreateRun(context.Context, tenancy.Scope, operatorassistant.Run) (operatorassistant.Run, error)
	GetRun(context.Context, tenancy.Scope, string, string) (operatorassistant.Run, error)
	SaveAnswer(context.Context, tenancy.Scope, string, string, int64, operatorassistant.RunState, operatorassistant.Answer, time.Time) (operatorassistant.Run, error)
	CancelRun(context.Context, tenancy.Scope, string, string, int64, time.Time) (operatorassistant.Run, error)
	RecordFeedback(context.Context, tenancy.Scope, string, operatorassistant.Feedback, time.Time) error
}

type operatorAssistantPreviewRepository interface {
	CreateActionPreview(context.Context, tenancy.Scope, string, string, operatorassistant.ActionPreview, time.Time) error
	GetActionPreview(context.Context, tenancy.Scope, string, string) (operatorassistant.ActionPreview, error)
	MarkActionPreview(context.Context, tenancy.Scope, string, string, string, time.Time) error
}

type operatorAssistantSource interface {
	Retrieve(context.Context, tenancy.Scope, operatorassistant.Intent, operatorassistant.ContextHint) (operatorassistant.ContextPack, error)
}

type operatorAssistantAudit interface {
	Capture(context.Context, tenancy.Scope, audit.Entry) (audit.Record, error)
}

type operatorAssistantApproval interface {
	CreateRequest(context.Context, tenancy.Scope, string, string, approval.RequestCommand) (approval.Request, error)
}

type integrationAssistantSource struct{ reader integrationCenterReader }

func (s integrationAssistantSource) Retrieve(ctx context.Context, scope tenancy.Scope, intent operatorassistant.Intent, hint operatorassistant.ContextHint) (operatorassistant.ContextPack, error) {
	if hint.Validate() != nil {
		return operatorassistant.ContextPack{}, operatorassistant.ErrInvalid
	}
	if intent != operatorassistant.IntentIntegration || s.reader == nil {
		pack, err := operatorassistant.BuildContext(intent, nil, nil, []string{"source_not_connected"}, true, time.Now().UTC())
		if err != nil {
			return operatorassistant.ContextPack{}, err
		}
		pack.Freshness = operatorassistant.Missing
		return pack, nil
	}
	result, err := s.reader.Read(ctx, scope, integrationCenterReadRequest{Limit: 20, AccountID: hint.ResourceID})
	if err != nil {
		pack, packErr := operatorassistant.BuildContext(intent, nil, nil, []string{"integration_center_unavailable"}, true, time.Now().UTC())
		if packErr != nil {
			return operatorassistant.ContextPack{}, err
		}
		pack.Freshness = operatorassistant.Unavailable
		return pack, nil
	}
	now := time.Now().UTC()
	facts := make([]operatorassistant.Fact, 0, len(result.Rows))
	for _, row := range result.Rows {
		observed := result.GeneratedAt
		if observed.IsZero() {
			observed = now
		}
		observed = observed.UTC()
		if observed.After(now) {
			observed = now
		}
		sum := sha256.Sum256([]byte(row.SnapshotDigest + row.AccountID))
		label := operatorassistant.SafeMarkdown(row.DisplayName)
		if label == "" {
			label = "Интеграция " + safeFactID(row.AccountID)
		}
		facts = append(facts, operatorassistant.Fact{
			Code:  "integration." + safeFactID(row.AccountID),
			Label: label,
			Value: string(row.Overall),
			Source: operatorassistant.EvidenceRef{
				SourceKind: "integration_center", SourceRef: row.AccountID,
				SourceVersion: strconv.FormatInt(row.Version, 10), ObservedAt: observed,
				CheckedAt: now, Watermark: row.SnapshotDigest,
				Freshness: operatorassistant.Fresh, ContextTrust: operatorassistant.TrustedSystem,
				EvidenceDigest: hex.EncodeToString(sum[:]), Visibility: "full",
				DeepLink:   "/integrations/status/" + row.AccountID,
				AgeSeconds: maxInt64(0, int64(now.Sub(observed).Seconds())), TTLSeconds: 3600,
			},
			OutputKind: operatorassistant.SourceFacts,
		})
	}
	return operatorassistant.BuildContext(intent, facts, result.SourceWatermarks, nil, result.Partial, now)
}

// operatorAssistantSources is the provider-neutral retrieval boundary for the
// first assistant slice. Each adapter reads an existing tenant-scoped
// projection; it never calls a connector or a secret provider. Missing
// adapters deliberately return an explicit unavailable context instead of
// turning an empty result into a healthy claim.
type operatorAssistantSources struct {
	integration    integrationCenterReader
	quality        *publicationqualityrepo.Repository
	inventory      *inventoryrepo.Repository
	returns        *returnsrepo.Repository
	sync           *syncrepo.Repository
	reconciliation *reconciliationrepo.Repository
	reports        reportReader
}

func (s operatorAssistantSources) Retrieve(ctx context.Context, scope tenancy.Scope, intent operatorassistant.Intent, hint operatorassistant.ContextHint) (operatorassistant.ContextPack, error) {
	if hint.Validate() != nil || !scope.Valid() {
		return operatorassistant.ContextPack{}, operatorassistant.ErrInvalid
	}
	if intent == operatorassistant.IntentIntegration {
		return (integrationAssistantSource{reader: s.integration}).Retrieve(ctx, scope, intent, hint)
	}
	now := time.Now().UTC()
	if s.integration == nil && s.quality == nil && s.inventory == nil && s.returns == nil && s.sync == nil && s.reconciliation == nil && s.reports == nil {
		return assistantUnavailablePack(intent, "source_not_connected", now)
	}
	var facts []operatorassistant.Fact
	var watermarks []string
	var omissions []string
	partial := false
	var err error
	switch intent {
	case operatorassistant.IntentSync:
		if s.sync == nil && s.reconciliation == nil {
			return assistantUnavailablePack(intent, "sync_source_not_connected", now)
		}
		if s.sync != nil {
			jobs, e := s.sync.ListSyncJobs(ctx, scope, 20)
			if e != nil {
				partial, omissions = true, append(omissions, "sync_jobs_unavailable")
			} else {
				for _, job := range jobs {
					at := job.UpdatedAt.UTC()
					if at.IsZero() {
						at = now
					}
					fact, e := assistantFact("sync.job."+safeFactID(job.ID), "Синхронизация "+job.ID, string(job.Status), "sync_job:"+job.ID, strconv.FormatInt(int64(job.AttemptCount), 10), at, "/sync")
					if e != nil {
						partial, omissions = true, append(omissions, "sync_fact_redacted")
						continue
					}
					facts = append(facts, fact)
				}
				watermarks = append(watermarks, "sync_jobs:"+strconv.Itoa(len(jobs)))
			}
		}
		if s.reconciliation != nil {
			drifts, e := s.reconciliation.ListRecentDrifts(ctx, scope, 20)
			if e != nil {
				partial, omissions = true, append(omissions, "reconciliation_unavailable")
			} else {
				for _, drift := range drifts {
					at := drift.DetectedAt.UTC()
					if at.IsZero() {
						at = now
					}
					fact, e := assistantFact("reconciliation.drift."+safeFactID(drift.ID), "Расхождение "+drift.ID, string(drift.Status), "reconciliation_drift:"+drift.ID, strconv.FormatInt(drift.Version, 10), at, "/reconciliation")
					if e != nil {
						partial, omissions = true, append(omissions, "reconciliation_fact_redacted")
						continue
					}
					facts = append(facts, fact)
				}
				watermarks = append(watermarks, "reconciliation_drifts:"+strconv.Itoa(len(drifts)))
			}
		}
	case operatorassistant.IntentProductQuality:
		if s.quality == nil {
			return assistantUnavailablePack(intent, "quality_source_not_connected", now)
		}
		items, e := s.quality.ListRuns(ctx, scope, hint.ResourceID, 20)
		if e != nil {
			err = e
			break
		}
		for _, item := range items {
			at := item.EvaluatedAt.UTC()
			if at.IsZero() {
				at = now
			}
			fact, e := assistantFact("quality.run."+safeFactID(item.ID), "Качество товара "+item.ProductID, string(item.Decision)+" ("+strconv.FormatInt(item.ScoreBPS/100, 10)+"%)", "quality_run:"+item.ID, strconv.FormatInt(item.Version, 10), at, "/products/quality")
			if e != nil {
				partial, omissions = true, append(omissions, "quality_fact_redacted")
				continue
			}
			facts = append(facts, fact)
		}
		watermarks = append(watermarks, "quality_runs:"+strconv.Itoa(len(items)))
	case operatorassistant.IntentInventory:
		if s.inventory == nil {
			return assistantUnavailablePack(intent, "inventory_source_not_connected", now)
		}
		invScope, e := coreinventory.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
		if e != nil {
			return operatorassistant.ContextPack{}, e
		}
		items, e := s.inventory.ListPositionViews(ctx, invScope, 20)
		if e != nil {
			err = e
			break
		}
		for _, item := range items {
			at, parseErr := time.Parse(time.RFC3339Nano, item.UpdatedAt)
			if parseErr != nil {
				at = now
			} else {
				at = at.UTC()
			}
			fact, e := assistantFact("inventory.position."+safeFactID(item.ID), item.SKU+" / "+item.WarehouseName, "доступно "+item.Available+" "+item.Unit+", резерв "+item.Reserved, "inventory_position:"+item.ID, strconv.FormatInt(item.Version, 10), at, "/inventory")
			if e != nil {
				partial, omissions = true, append(omissions, "inventory_fact_redacted")
				continue
			}
			facts = append(facts, fact)
		}
		watermarks = append(watermarks, "inventory_positions:"+strconv.Itoa(len(items)))
	case operatorassistant.IntentOrderReturn:
		if s.returns == nil {
			return assistantUnavailablePack(intent, "returns_source_not_connected", now)
		}
		returnScope, e := corereturns.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
		if e != nil {
			return operatorassistant.ContextPack{}, e
		}
		items, e := s.returns.ListReturns(ctx, returnScope, 20)
		if e != nil {
			err = e
			break
		}
		for _, item := range items {
			at := item.UpdatedAt.UTC()
			if at.IsZero() {
				at = now
			}
			fact, e := assistantFact("returns.item."+safeFactID(item.ID.String()), "Возврат по заказу "+item.OrderID, string(item.Status)+", причина "+item.ReasonCode, "return:"+item.ID.String(), strconv.FormatInt(item.Version, 10), at, "/returns")
			if e != nil {
				partial, omissions = true, append(omissions, "return_fact_redacted")
				continue
			}
			facts = append(facts, fact)
		}
		watermarks = append(watermarks, "returns:"+strconv.Itoa(len(items)))
	case operatorassistant.IntentUnitEconomics, operatorassistant.IntentReportSummary:
		if s.reports == nil {
			return assistantUnavailablePack(intent, "report_source_not_connected", now)
		}
		reportID := "sales_daily"
		if intent == operatorassistant.IntentUnitEconomics {
			reportID = "unit_economics_by_channel"
		}
		data, e := s.reports.Report(ctx, scope, reportID, reportrepo.Filter{Limit: 20})
		if e != nil {
			err = e
			break
		}
		for rowIndex, row := range data.Rows {
			value := strings.Join(row, " · ")
			at := data.GeneratedAt.UTC()
			if at.IsZero() {
				at = now
			}
			fact, e := assistantFact("report."+safeFactID(reportID)+"."+strconv.Itoa(rowIndex), data.ID, value, "report:"+reportID+":"+strconv.Itoa(rowIndex), data.Source, at, "/reports/"+reportID)
			if e != nil {
				partial, omissions = true, append(omissions, "report_fact_redacted")
				continue
			}
			facts = append(facts, fact)
		}
		watermarks = append(watermarks, "report:"+reportID+":"+strconv.Itoa(len(data.Rows)))
	case operatorassistant.IntentWorkflowDraft:
		// A draft is grounded in the same operational evidence as the
		// integration state center. It is still only a preview; execution is
		// owned by Workflow/Approval and never by this adapter.
		if s.integration == nil {
			return assistantUnavailablePack(intent, "workflow_source_not_connected", now)
		}
		integrationPack, e := (integrationAssistantSource{reader: s.integration}).Retrieve(ctx, scope, operatorassistant.IntentIntegration, hint)
		if e != nil {
			return assistantUnavailablePack(intent, "workflow_source_unavailable", now)
		}
		facts, watermarks, partial = integrationPack.Facts, integrationPack.Watermarks, integrationPack.Partial
		if partial {
			omissions = append(omissions, "integration_context_partial")
		}
	default:
		return assistantUnavailablePack(intent, "source_not_connected", now)
	}
	if err != nil {
		return assistantUnavailablePack(intent, "source_unavailable", now)
	}
	if len(facts) == 0 {
		omissions = append(omissions, "no_authoritative_facts")
	}
	pack, e := operatorassistant.BuildContext(intent, facts, watermarks, omissions, partial, now)
	if e != nil {
		return operatorassistant.ContextPack{}, e
	}
	if partial {
		pack.Freshness = operatorassistant.Stale
	}
	return pack, nil
}

func assistantUnavailablePack(intent operatorassistant.Intent, omission string, now time.Time) (operatorassistant.ContextPack, error) {
	pack, err := operatorassistant.BuildContext(intent, nil, nil, []string{omission}, true, now)
	if err != nil {
		return operatorassistant.ContextPack{}, err
	}
	pack.Freshness = operatorassistant.Unavailable
	return pack, nil
}

func assistantFact(code, label, value, sourceRef, sourceVersion string, observed time.Time, deepLink string) (operatorassistant.Fact, error) {
	now := time.Now().UTC()
	if observed.IsZero() {
		observed = now
	}
	observed = observed.UTC()
	if observed.After(now) {
		observed = now
	}
	if value == "" {
		value = "нет значения"
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{sourceRef, sourceVersion, label, value}, "\x00")))
	ref := operatorassistant.EvidenceRef{SourceKind: "postgresql_projection", SourceRef: sourceRef, SourceVersion: sourceVersion, ObservedAt: observed, CheckedAt: now, Watermark: sourceVersion, Freshness: operatorassistant.Fresh, ContextTrust: operatorassistant.TrustedSystem, EvidenceDigest: hex.EncodeToString(sum[:]), Visibility: "tenant", DeepLink: deepLink, AgeSeconds: maxInt64(0, int64(now.Sub(observed).Seconds())), TTLSeconds: 3600}
	fact := operatorassistant.Fact{Code: code, Label: label, Value: value, Source: ref, OutputKind: operatorassistant.SourceFacts}
	if fact.Validate(now) != nil {
		return operatorassistant.Fact{}, operatorassistant.ErrInvalid
	}
	return fact, nil
}

func safeFactID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	result := strings.Trim(b.String(), "-._")
	if result == "" {
		return "unknown"
	}
	return result
}

type operatorAssistantAPI struct {
	repository operatorAssistantRepository
	source     operatorAssistantSource
	approval   operatorAssistantApproval
	audit      operatorAssistantAudit
	now        func() time.Time
}

func newOperatorAssistantRoutes(repository operatorAssistantRepository, source operatorAssistantSource, auditors ...operatorAssistantAudit) []ProtectedRoute {
	return newOperatorAssistantRoutesWithApproval(repository, source, nil, auditors...)
}

func newOperatorAssistantRoutesWithApproval(repository operatorAssistantRepository, source operatorAssistantSource, approvalRepository operatorAssistantApproval, auditors ...operatorAssistantAudit) []ProtectedRoute {
	var auditService operatorAssistantAudit
	if len(auditors) > 0 {
		auditService = auditors[0]
	}
	api := &operatorAssistantAPI{repository: repository, source: source, approval: approvalRepository, audit: auditService, now: func() time.Time { return time.Now().UTC() }}
	return []ProtectedRoute{
		{Method: http.MethodPost, Path: AssistantSessionsPath, Permission: "assistant.ask", Handler: http.HandlerFunc(api.createSession)},
		{Method: http.MethodGet, Path: AssistantSessionsPath, Permission: "assistant.read", Handler: http.HandlerFunc(api.listSessions)},
		{Method: http.MethodGet, Path: AssistantSessionsPath + "/", PathPrefix: true, Permission: "assistant.read", Handler: http.HandlerFunc(api.sessionDetail)},
		{Method: http.MethodPost, Path: AssistantSessionsPath + "/", PathPrefix: true, Permission: "assistant.ask", Handler: http.HandlerFunc(api.message)},
		{Method: http.MethodGet, Path: AssistantRunsPath, PathPrefix: true, Permission: "assistant.read", Handler: http.HandlerFunc(api.run)},
		{Method: http.MethodPost, Path: AssistantRunsPath, PathPrefix: true, Permission: "assistant.ask", Handler: http.HandlerFunc(api.cancel)},
		{Method: http.MethodPost, Path: AssistantFeedbackPath, Permission: "assistant.feedback", Handler: http.HandlerFunc(api.feedback)},
		{Method: http.MethodPost, Path: AssistantApprovePath, PathPrefix: true, Permission: "assistant.preview", Handler: http.HandlerFunc(api.approve)},
	}
}

type assistantSessionRequest struct {
	Title  string `json:"title,omitempty"`
	Locale string `json:"locale,omitempty"`
}

type assistantMessageRequest struct {
	Question    string                        `json:"question"`
	ContextHint operatorassistant.ContextHint `json:"context_hint,omitempty"`
}

type assistantCancelRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}

type assistantSessionView struct {
	ID        string    `json:"id"`
	ActorID   string    `json:"actor_id"`
	Title     string    `json:"title,omitempty"`
	Locale    string    `json:"locale"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type assistantEvidenceView struct {
	SourceKind     string    `json:"source_kind"`
	SourceRef      string    `json:"source_ref"`
	SourceVersion  string    `json:"source_version,omitempty"`
	ObservedAt     time.Time `json:"observed_at"`
	CheckedAt      time.Time `json:"checked_at"`
	Watermark      string    `json:"watermark,omitempty"`
	Freshness      string    `json:"freshness"`
	ContextTrust   string    `json:"context_trust"`
	EvidenceDigest string    `json:"evidence_digest"`
	Visibility     string    `json:"visibility"`
	DeepLink       string    `json:"deep_link,omitempty"`
	AgeSeconds     int64     `json:"age_seconds"`
	TTLSeconds     int64     `json:"ttl_seconds"`
}

type assistantFactView struct {
	Code        string                `json:"code"`
	Label       string                `json:"label"`
	Value       string                `json:"value"`
	Source      assistantEvidenceView `json:"source"`
	OutputKind  string                `json:"output_kind"`
	AIGenerated bool                  `json:"ai_generated"`
}

type assistantRecommendationView struct {
	Code           string `json:"code"`
	Title          string `json:"title"`
	Reason         string `json:"reason"`
	ExpectedEffect string `json:"expected_effect"`
	NextLink       string `json:"next_link,omitempty"`
	OutputKind     string `json:"output_kind"`
	AIGenerated    bool   `json:"ai_generated"`
}

type assistantActionView struct {
	ID                 string    `json:"id"`
	Action             string    `json:"action"`
	ResourceType       string    `json:"resource_type"`
	ResourceID         string    `json:"resource_id"`
	ExpectedVersion    int64     `json:"expected_version"`
	Risk               string    `json:"risk"`
	RequiredPermission string    `json:"required_permission"`
	Capability         string    `json:"capability,omitempty"`
	RuntimeStage       string    `json:"runtime_stage,omitempty"`
	ApprovalRequired   bool      `json:"approval_required"`
	Impact             string    `json:"impact"`
	IdempotencyKey     string    `json:"idempotency_key"`
	PreviewDigest      string    `json:"preview_digest"`
	ExpiresAt          time.Time `json:"expires_at"`
	Status             string    `json:"status"`
	EvidenceDigest     string    `json:"evidence_digest,omitempty"`
}

type assistantAnswerView struct {
	Summary         string                        `json:"summary"`
	Text            string                        `json:"text"`
	GroundingState  string                        `json:"grounding_state"`
	Coverage        int                           `json:"coverage_percent"`
	Facts           []assistantFactView           `json:"facts"`
	Evidence        []assistantEvidenceView       `json:"evidence"`
	Limitations     []string                      `json:"limitations"`
	Recommendations []assistantRecommendationView `json:"recommendations"`
	ActionPreviews  []assistantActionView         `json:"action_previews"`
	Provider        string                        `json:"provider,omitempty"`
	Model           string                        `json:"model,omitempty"`
	AIGenerated     bool                          `json:"ai_generated"`
	OutputKind      string                        `json:"output_kind"`
	AnswerDigest    string                        `json:"answer_digest"`
}

type assistantRunView struct {
	ID            string               `json:"id"`
	SessionID     string               `json:"session_id"`
	State         string               `json:"state"`
	Intent        string               `json:"intent"`
	ContextDigest string               `json:"context_digest"`
	Answer        *assistantAnswerView `json:"answer,omitempty"`
	ErrorCode     string               `json:"error_code,omitempty"`
	Version       int64                `json:"version"`
	CreatedAt     time.Time            `json:"created_at"`
	UpdatedAt     time.Time            `json:"updated_at"`
}

func sessionView(value operatorassistant.Session) assistantSessionView {
	return assistantSessionView{ID: value.ID, ActorID: value.ActorID, Title: value.Title, Locale: value.Locale, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func evidenceView(value operatorassistant.EvidenceRef) assistantEvidenceView {
	return assistantEvidenceView{SourceKind: value.SourceKind, SourceRef: value.SourceRef, SourceVersion: value.SourceVersion, ObservedAt: value.ObservedAt, CheckedAt: value.CheckedAt, Watermark: value.Watermark, Freshness: string(value.Freshness), ContextTrust: string(value.ContextTrust), EvidenceDigest: value.EvidenceDigest, Visibility: value.Visibility, DeepLink: value.DeepLink, AgeSeconds: value.AgeSeconds, TTLSeconds: value.TTLSeconds}
}

func answerView(value *operatorassistant.Answer) *assistantAnswerView {
	if value == nil {
		return nil
	}
	out := &assistantAnswerView{Summary: value.Summary, Text: operatorassistant.SafeMarkdown(value.Text), GroundingState: string(value.GroundingState), Coverage: value.Coverage, Limitations: append([]string(nil), value.Limitations...), Provider: value.TransportRef, Model: value.Model, AIGenerated: value.AIGenerated, OutputKind: string(value.OutputKind), AnswerDigest: value.AnswerDigest}
	out.Evidence = make([]assistantEvidenceView, 0, len(value.Evidence))
	for _, item := range value.Evidence {
		out.Evidence = append(out.Evidence, evidenceView(item))
	}
	out.Facts = make([]assistantFactView, 0, len(value.Facts))
	for _, item := range value.Facts {
		out.Facts = append(out.Facts, assistantFactView{Code: item.Code, Label: item.Label, Value: item.Value, Source: evidenceView(item.Source), OutputKind: string(item.OutputKind), AIGenerated: item.AIGenerated})
	}
	out.Recommendations = make([]assistantRecommendationView, 0, len(value.Recommendations))
	for _, item := range value.Recommendations {
		out.Recommendations = append(out.Recommendations, assistantRecommendationView{Code: item.Code, Title: item.Title, Reason: item.Reason, ExpectedEffect: item.ExpectedEffect, NextLink: item.NextLink, OutputKind: string(item.OutputKind), AIGenerated: item.AIGenerated})
	}
	out.ActionPreviews = make([]assistantActionView, 0, len(value.ActionPreviews))
	for _, item := range value.ActionPreviews {
		out.ActionPreviews = append(out.ActionPreviews, assistantActionView{ID: item.ID, Action: item.Action, ResourceType: item.ResourceType, ResourceID: item.ResourceID, ExpectedVersion: item.ExpectedVersion, Risk: string(item.Risk), RequiredPermission: item.RequiredPermission, Capability: item.Capability, RuntimeStage: item.RuntimeStage, ApprovalRequired: item.ApprovalRequired, Impact: item.Impact, IdempotencyKey: item.IdempotencyKey, PreviewDigest: item.PreviewDigest, ExpiresAt: item.ExpiresAt, Status: item.Status, EvidenceDigest: item.EvidenceDigest})
	}
	return out
}

func runView(value operatorassistant.Run) assistantRunView {
	return assistantRunView{ID: value.ID, SessionID: value.SessionID, State: string(value.State), Intent: string(value.Intent), ContextDigest: value.ContextDigest, Answer: answerView(value.Answer), ErrorCode: value.ErrorCode, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func (api *operatorAssistantAPI) createSession(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	if !ok || !principalOK || api == nil || api.repository == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input assistantSessionRequest
	if decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	session, err := operatorassistant.NewSession(scope, principal.SubjectRef, "as."+newApprovalID(), input.Title, input.Locale, api.now())
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	created, err := api.repository.CreateSession(r.Context(), scope, session)
	if err != nil {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, sessionView(created))
}

func (api *operatorAssistantAPI) listSessions(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	if !ok || !principalOK || api == nil || api.repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		limit = value
	}
	items, err := api.repository.ListSessions(r.Context(), scope, principal.SubjectRef, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	out := make([]assistantSessionView, 0, len(items))
	for _, item := range items {
		out = append(out, sessionView(item))
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (api *operatorAssistantAPI) sessionDetail(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	id := strings.TrimPrefix(r.URL.Path, AssistantSessionsPath+"/")
	if !ok || !principalOK || api == nil || api.repository == nil || id == "" || strings.Contains(id, "/") {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	session, err := api.repository.GetSession(r.Context(), scope, principal.SubjectRef, id)
	if errors.Is(err, operatorassistantrepo.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, sessionView(session))
}

func (api *operatorAssistantAPI) message(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	if !ok || !principalOK || api == nil || api.repository == nil || api.source == nil || !validIdempotencyKey(r.Header.Get("Idempotency-Key")) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	sessionID := strings.TrimPrefix(r.URL.Path, AssistantSessionsPath+"/")
	sessionID = strings.TrimSuffix(sessionID, "/messages")
	if sessionID == "" || strings.Contains(sessionID, "/") {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input assistantMessageRequest
	if decodeStrictJSON(r, &input) != nil || strings.TrimSpace(input.Question) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	session, err := api.repository.GetSession(r.Context(), scope, principal.SubjectRef, sessionID)
	if errors.Is(err, operatorassistantrepo.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	intent, classifyErr := operatorassistant.ClassifyIntent(input.Question)
	if classifyErr != nil {
		intent = operatorassistant.IntentUnsupported
	}
	pack, sourceErr := api.source.Retrieve(r.Context(), scope, intent, input.ContextHint)
	if sourceErr != nil {
		pack, _ = operatorassistant.BuildContext(intent, nil, nil, []string{"source_unavailable"}, true, api.now())
		pack.Freshness = operatorassistant.Unavailable
	}
	runID := "ar." + digestID(r.Header.Get("Idempotency-Key")+"\x00"+session.ID)
	run, err := operatorassistant.NewRun(session, principal.SubjectRef, runID, intent, pack.ContextDigest, api.now())
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	created, err := api.repository.CreateRun(r.Context(), scope, run)
	if err != nil {
		if existing, getErr := api.repository.GetRun(r.Context(), scope, principal.SubjectRef, runID); getErr == nil {
			writeJSON(w, http.StatusOK, runView(existing))
			return
		}
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if classifyErr != nil {
		created, err = api.repository.SaveAnswer(r.Context(), scope, principal.SubjectRef, created.ID, created.Version, operatorassistant.RunBlocked, refusalAnswer(pack, api.now()), api.now())
	} else {
		answer, answerErr := operatorassistant.ComposeGroundedAnswer(input.Question, pack, api.now())
		if answerErr != nil {
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		for _, preview := range assistantPreviews(intent, pack, api.now()) {
			answer.ActionPreviews = append(answer.ActionPreviews, preview)
		}
		if len(answer.ActionPreviews) > 0 {
			digest := answer.AnswerDigest
			for _, preview := range answer.ActionPreviews {
				digest += "\x00" + preview.PreviewDigest
			}
			sum := sha256.Sum256([]byte(digest))
			answer.AnswerDigest = hex.EncodeToString(sum[:])
		}
		state := operatorassistant.RunCompleted
		if answer.GroundingState != operatorassistant.Grounded {
			state = operatorassistant.RunPartial
		}
		created, err = api.repository.SaveAnswer(r.Context(), scope, principal.SubjectRef, created.ID, created.Version, state, answer, api.now())
		if err == nil {
			previewRepository, hasPreviewRepository := api.repository.(operatorAssistantPreviewRepository)
			if !hasPreviewRepository {
				previewRepository = nil
			}
			for _, preview := range answer.ActionPreviews {
				if previewRepository == nil {
					break
				}
				if previewErr := previewRepository.CreateActionPreview(r.Context(), scope, created.ID, principal.SubjectRef, preview, api.now()); previewErr != nil {
					err = previewErr
					break
				}
			}
		}
	}
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Assistant run could not be completed")
		return
	}
	if api.audit != nil && created.Answer != nil {
		_, _ = api.audit.Capture(r.Context(), scope, audit.Entry{ActorID: principal.Subject, Source: "api.operator_assistant", Action: "assistant.run.completed", ResourceType: "assistant_run", ResourceID: created.ID, CorrelationID: r.Header.Get("Idempotency-Key"), Risk: audit.RiskRead, Summary: audit.Summary{"intent": string(created.Intent), "state": string(created.State), "grounding_state": string(created.Answer.GroundingState), "context_digest": created.ContextDigest}})
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, runView(created))
}

func assistantPreviews(intent operatorassistant.Intent, pack operatorassistant.ContextPack, now time.Time) []operatorassistant.ActionPreview {
	if len(pack.Facts) == 0 {
		return nil
	}
	action, resourceType := "", ""
	switch intent {
	case operatorassistant.IntentIntegration:
		action, resourceType = "health.check", "connector_account"
	case operatorassistant.IntentSync:
		action, resourceType = "sync.dry_run", "sync_job"
	case operatorassistant.IntentProductQuality:
		action, resourceType = "quality.preview", "quality_run"
	case operatorassistant.IntentReportSummary, operatorassistant.IntentUnitEconomics:
		action, resourceType = "open.evidence", "report"
	case operatorassistant.IntentWorkflowDraft:
		action, resourceType = "workflow.draft", "workflow"
	default:
		return nil
	}
	result := make([]operatorassistant.ActionPreview, 0, minInt(len(pack.Facts), operatorassistant.MaxActionPreviews))
	for _, fact := range pack.Facts {
		expected := int64(1)
		if parsed, err := strconv.ParseInt(fact.Source.SourceVersion, 10, 64); err == nil && parsed > 0 {
			expected = parsed
		}
		resourceID := fact.Source.SourceRef
		if action == "open.evidence" {
			resourceID = strings.TrimPrefix(resourceID, "report:")
		}
		preview, err := operatorassistant.CompileActionPreview(action, resourceType, resourceID, expected, fact.Source, now)
		if err == nil {
			result = append(result, preview)
		}
		if len(result) == operatorassistant.MaxActionPreviews {
			break
		}
	}
	return result
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func refusalAnswer(pack operatorassistant.ContextPack, now time.Time) operatorassistant.Answer {
	answer, err := operatorassistant.ComposeGroundedAnswer("безопасный отказ", pack, now)
	if err == nil {
		answer.GroundingState = operatorassistant.Refused
		answer.Summary = "Запрос отклонён: помощник не выполняет инструкции из текста, команды и запросы к секретам."
		answer.Text = answer.Summary
		answer.AIGenerated = false
		answer.OutputKind = operatorassistant.SourceFacts
		sum := sha256.Sum256([]byte(answer.Text))
		answer.AnswerDigest = hex.EncodeToString(sum[:])
		return answer
	}
	sum := sha256.Sum256([]byte("refused"))
	return operatorassistant.Answer{Summary: "Запрос отклонён.", Text: "Запрос отклонён.", GroundingState: operatorassistant.Refused, OutputKind: operatorassistant.SourceFacts, AnswerDigest: hex.EncodeToString(sum[:])}
}

func digestID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:26]
}

func (api *operatorAssistantAPI) run(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	id := strings.TrimPrefix(r.URL.Path, AssistantRunsPath)
	if !ok || !principalOK || api == nil || api.repository == nil || id == "" || strings.Contains(id, "/") {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	value, err := api.repository.GetRun(r.Context(), scope, principal.SubjectRef, id)
	if errors.Is(err, operatorassistantrepo.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, runView(value))
}

func (api *operatorAssistantAPI) cancel(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, AssistantRunsPath), ":cancel")
	if !ok || !principalOK || api == nil || api.repository == nil || id == "" || strings.Contains(id, "/") || !validIdempotencyKey(r.Header.Get("Idempotency-Key")) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input assistantCancelRequest
	if decodeStrictJSON(r, &input) != nil || input.ExpectedVersion < 1 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	value, err := api.repository.CancelRun(r.Context(), scope, principal.SubjectRef, id, input.ExpectedVersion, api.now())
	if errors.Is(err, operatorassistantrepo.ErrConflict) {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, runView(value))
}

func (api *operatorAssistantAPI) feedback(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	if !ok || !principalOK || api == nil || api.repository == nil || !validIdempotencyKey(r.Header.Get("Idempotency-Key")) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input operatorassistant.Feedback
	if decodeStrictJSON(r, &input) != nil || input.Validate() != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if err := api.repository.RecordFeedback(r.Context(), scope, principal.SubjectRef, input, api.now()); err != nil {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "recorded"})
}

func (api *operatorAssistantAPI) approve(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, AssistantApprovePath), ":approve")
	previewRepository, hasPreviewRepository := func() (operatorAssistantPreviewRepository, bool) {
		if api == nil || api.repository == nil {
			return nil, false
		}
		repository, ok := api.repository.(operatorAssistantPreviewRepository)
		return repository, ok
	}()
	if !ok || !principalOK || api == nil || api.approval == nil || !hasPreviewRepository || id == "" || strings.Contains(id, "/") || !validIdempotencyKey(r.Header.Get("Idempotency-Key")) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	preview, err := previewRepository.GetActionPreview(r.Context(), scope, principal.SubjectRef, id)
	if errors.Is(err, operatorassistantrepo.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if !preview.ApprovalRequired || preview.Risk != operatorassistant.RiskSensitiveWrite {
		writeProblem(w, http.StatusConflict, "Approval is not required for this preview")
		return
	}
	now := api.now()
	if preview.Status != "pending" || !preview.ExpiresAt.After(now) {
		writeProblem(w, http.StatusConflict, "Preview is stale or already handled")
		return
	}
	request, err := api.approval.CreateRequest(r.Context(), scope, preview.Action, preview.ResourceType, approval.RequestCommand{
		RequestID: "apr:" + digestID(preview.ID), ResourceID: preview.ResourceID, Risk: approval.RiskWriteSensitive,
		Mutation: approval.Mutation{AuditID: newApprovalID(), EventID: newApprovalID(), ActorID: principal.SubjectRef, Source: "api.operator_assistant", CorrelationID: r.Header.Get("Idempotency-Key"), OccurredAt: now},
	})
	if err != nil {
		writeProblem(w, http.StatusConflict, "Approval policy denied this preview")
		return
	}
	if err := previewRepository.MarkActionPreview(r.Context(), scope, principal.SubjectRef, id, "approved", now); err != nil {
		if errors.Is(err, operatorassistantrepo.ErrConflict) {
			writeProblem(w, http.StatusConflict, "Preview is stale or already handled")
		} else {
			writeProblem(w, http.StatusServiceUnavailable, "Preview status could not be persisted")
		}
		return
	}
	if api.audit != nil {
		_, _ = api.audit.Capture(r.Context(), scope, audit.Entry{ActorID: principal.Subject, Source: "api.operator_assistant", Action: "assistant.action_preview.approval_requested", ResourceType: "assistant_action_preview", ResourceID: preview.ID, CorrelationID: request.ID, Risk: audit.RiskWriteSensitive, Summary: audit.Summary{"approval_request_id": request.ID, "preview_digest": preview.PreviewDigest, "evidence_digest": preview.EvidenceDigest}})
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "approval_requested", "preview_id": preview.ID, "approval_request_id": request.ID, "state": string(request.State), "expires_at": request.ExpiresAt})
}
