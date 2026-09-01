package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	core "github.com/torgnexa/torgnexa/internal/core/customerservice"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/customerservicerepo"
)

const (
	CustomerServiceSummaryPath     = "/api/v1/customer-service/summary"
	CustomerServiceInboxPath       = "/api/v1/customer-service/inbox"
	CustomerServiceReviewsPath     = "/api/v1/customer-service/reviews"
	CustomerServiceQuestionsPath   = "/api/v1/customer-service/questions"
	CustomerServiceFindingsPath    = "/api/v1/customer-service/findings"
	CustomerServiceThreadsPath     = "/api/v1/customer-service/threads/"
	CustomerServiceCustomersPath   = "/api/v1/customer-service/customers/"
	CustomerServiceInboundPath     = "/api/v1/customer-service/inbound"
	CustomerServiceRepliesPath     = "/api/v1/customer-service/replies"
	CustomerServiceAssignmentsPath = "/api/v1/customer-service/assignments"
	CustomerServiceTransitionsPath = "/api/v1/customer-service/transitions"
)

type customerServiceAPI struct {
	repository *customerservicerepo.Repository
	audit      auditCapturer
}

func newCustomerServiceRoutes(repository *customerservicerepo.Repository, auditor auditCapturer) []ProtectedRoute {
	api := customerServiceAPI{repository: repository, audit: auditor}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: CustomerServiceSummaryPath, Permission: "customer_service.read", Handler: http.HandlerFunc(api.summary)},
		{Method: http.MethodGet, Path: CustomerServiceInboxPath, Permission: "customer_service.read", Handler: http.HandlerFunc(api.inbox)},
		{Method: http.MethodGet, Path: CustomerServiceReviewsPath, Permission: "customer_service.read", Handler: http.HandlerFunc(api.reviews)},
		{Method: http.MethodGet, Path: CustomerServiceQuestionsPath, Permission: "customer_service.read", Handler: http.HandlerFunc(api.questions)},
		{Method: http.MethodGet, Path: CustomerServiceFindingsPath, Permission: "customer_service.read", Handler: http.HandlerFunc(api.findings)},
		{Method: http.MethodGet, Path: CustomerServiceThreadsPath, PathPrefix: true, Permission: "customer_service.read", Handler: http.HandlerFunc(api.thread)},
		{Method: http.MethodGet, Path: CustomerServiceCustomersPath, PathPrefix: true, Permission: "customer_service.read", Handler: http.HandlerFunc(api.customerTimeline)},
		{Method: http.MethodPost, Path: CustomerServiceInboundPath, Permission: "customer_service.write", Handler: http.HandlerFunc(api.inbound)},
		{Method: http.MethodPost, Path: CustomerServiceRepliesPath, Permission: "customer_service.reply", Handler: http.HandlerFunc(api.reply)},
		{Method: http.MethodPost, Path: CustomerServiceAssignmentsPath, Permission: "customer_service.assign", Handler: http.HandlerFunc(api.assign)},
		{Method: http.MethodPost, Path: CustomerServiceTransitionsPath, Permission: "customer_service.write", Handler: http.HandlerFunc(api.transition)},
	}
}

func (api customerServiceAPI) summary(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	summary, err := api.repository.Summary(r.Context(), scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Customer service summary unavailable")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (api customerServiceAPI) inbox(w http.ResponseWriter, r *http.Request) {
	api.list(w, r, core.Filter{})
}

func (api customerServiceAPI) reviews(w http.ResponseWriter, r *http.Request) {
	api.list(w, r, core.Filter{Type: core.TypeReview})
}

func (api customerServiceAPI) questions(w http.ResponseWriter, r *http.Request) {
	api.list(w, r, core.Filter{Type: core.TypeQuestion})
}

func (api customerServiceAPI) list(w http.ResponseWriter, r *http.Request, defaults core.Filter) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	filter, err := customerServiceFilter(r, defaults)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid customer service filters")
		return
	}
	page, err := api.repository.ListInbox(r.Context(), scope, filter)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Customer service inbox unavailable")
		return
	}
	if page.HasMore && len(page.Items) > 0 {
		page.NextCursor = encodeAccountCursor(page.Items[len(page.Items)-1].ID)
	}
	writeJSON(w, http.StatusOK, page)
}

func (api customerServiceAPI) findings(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	limit, ok := boundedLimit(r, 50, 200)
	if !ok {
		writeProblem(w, http.StatusBadRequest, "Invalid limit")
		return
	}
	after, err := decodeAccountCursor(strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid cursor")
		return
	}
	items, more, err := api.repository.ListFindings(r.Context(), scope, after, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Customer service findings unavailable")
		return
	}
	next := ""
	if more && len(items) > 0 {
		next = encodeAccountCursor(items[len(items)-1].ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}

func (api customerServiceAPI) thread(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, CustomerServiceThreadsPath), "/")
	if !ok || api.repository == nil || id == "" || strings.Contains(id, "/") {
		writeProblem(w, http.StatusBadRequest, "Invalid conversation id")
		return
	}
	thread, err := api.repository.GetThread(r.Context(), scope, id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, customerservicerepo.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeProblem(w, status, "Customer service thread unavailable")
		return
	}
	writeJSON(w, http.StatusOK, thread)
}

func (api customerServiceAPI) customerTimeline(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, CustomerServiceCustomersPath), "/")
	if !ok || api.repository == nil || id == "" || strings.Contains(id, "/") {
		writeProblem(w, http.StatusBadRequest, "Invalid customer reference")
		return
	}
	items, err := api.repository.Timeline(r.Context(), scope, id)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Customer timeline unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api customerServiceAPI) inbound(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !scopeOK || !principalOK || api.repository == nil || api.audit == nil || !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and authenticated context are required")
		return
	}
	var input struct {
		Conversation core.Conversation `json:"conversation"`
		Message      core.Message      `json:"message"`
		Customer     *core.CustomerRef `json:"customer,omitempty"`
	}
	if decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid inbound customer message")
		return
	}
	now := time.Now().UTC()
	if input.Conversation.ID == "" {
		input.Conversation.ID = stableID("conversation-", 24, scope, key)
	}
	if input.Message.ID == "" {
		input.Message.ID = stableID("message-", 24, scope, key)
	}
	record, err := core.NewInbound(input.Conversation, input.Message, input.Customer, now)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid or unsafe inbound customer message")
		return
	}
	if _, err := api.audit.Capture(r.Context(), scope, audit.Entry{ActorID: boundedActorRef(principal.Subject), Source: "api.customer_service", Action: "customer_service.inbound", ResourceType: "conversation", ResourceID: record.Conversation.ID, CorrelationID: key, Risk: audit.RiskWriteSafe, Summary: audit.Summary{"type": record.Conversation.Type, "source_system": record.Conversation.SourceSystem, "remote_thread_id": record.Conversation.RemoteThreadID, "remote_message_id": record.Message.RemoteMessageID, "content_digest": record.Message.ContentDigest}}); err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Customer service audit unavailable")
		return
	}
	result, err := api.repository.Ingest(r.Context(), scope, record)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Customer message ingest failed")
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (api customerServiceAPI) reply(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !scopeOK || !principalOK || api.repository == nil || api.audit == nil || !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and authenticated context are required")
		return
	}
	var input struct {
		ID             string          `json:"id"`
		ConversationID string          `json:"conversation_id"`
		Visibility     core.Visibility `json:"visibility"`
		Origin         string          `json:"origin"`
		SafeText       string          `json:"safe_text"`
		TemplateID     string          `json:"template_id,omitempty"`
		ApprovalRef    string          `json:"approval_ref,omitempty"`
	}
	if decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid reply")
		return
	}
	now := time.Now().UTC()
	if input.ID == "" {
		input.ID = stableID("reply-", 24, scope, key)
	}
	state := core.DeliveryQueued
	if input.Visibility == core.VisibilityInternal || strings.TrimSpace(input.ApprovalRef) == "" {
		state = core.DeliveryDraft
	}
	reply := core.Reply{ID: input.ID, ConversationID: input.ConversationID, Visibility: input.Visibility, Origin: input.Origin, SafeText: core.SanitizeText(input.SafeText), TemplateID: input.TemplateID, ApprovalRef: input.ApprovalRef, IdempotencyKey: key, DeliveryState: state, CreatedAt: now, UpdatedAt: now, Version: 1}
	reply.ContentDigest = core.Digest(reply.SafeText)
	if err := reply.Validate(); err != nil {
		if errors.Is(err, core.ErrAIDraftOnly) {
			writeProblem(w, http.StatusUnprocessableEntity, "AI output is draft-only")
			return
		}
		writeProblem(w, http.StatusBadRequest, "Invalid or unsafe reply")
		return
	}
	if _, err := api.audit.Capture(r.Context(), scope, audit.Entry{ActorID: boundedActorRef(principal.Subject), Source: "api.customer_service", Action: "customer_service.reply.queue", ResourceType: "customer_service_reply", ResourceID: reply.ID, CorrelationID: key, Risk: audit.RiskWriteSensitive, Summary: audit.Summary{"conversation_id": reply.ConversationID, "visibility": reply.Visibility, "delivery_state": reply.DeliveryState, "content_digest": reply.ContentDigest}}); err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Customer service audit unavailable")
		return
	}
	created, err := api.repository.QueueReply(r.Context(), scope, reply)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, customerservicerepo.ErrConflict) {
			status = http.StatusConflict
		}
		writeProblem(w, status, "Reply was not queued")
		return
	}
	writeJSON(w, http.StatusAccepted, created)
}

func (api customerServiceAPI) assign(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !scopeOK || !principalOK || api.repository == nil || api.audit == nil || !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and authenticated context are required")
		return
	}
	var assignment core.Assignment
	if decodeStrictJSON(r, &assignment) != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid assignment")
		return
	}
	if assignment.ID == "" {
		assignment.ID = stableID("assignment-", 24, scope, key)
	}
	if assignment.CreatedAt.IsZero() {
		assignment.CreatedAt = time.Now().UTC()
	}
	if err := assignment.Validate(); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid assignment")
		return
	}
	if _, err := api.audit.Capture(r.Context(), scope, audit.Entry{ActorID: boundedActorRef(principal.Subject), Source: "api.customer_service", Action: "customer_service.assignment", ResourceType: "conversation", ResourceID: assignment.ConversationID, CorrelationID: key, Risk: audit.RiskWriteSafe, Summary: audit.Summary{"assignee_id": assignment.AssigneeID, "team_id": assignment.TeamID, "expected_version": assignment.ExpectedVersion}}); err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Customer service audit unavailable")
		return
	}
	conversation, err := api.repository.Assign(r.Context(), scope, assignment)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, customerservicerepo.ErrConflict) {
			status = http.StatusConflict
		}
		writeProblem(w, status, "Assignment was not applied")
		return
	}
	writeJSON(w, http.StatusOK, conversation)
}

func (api customerServiceAPI) transition(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !scopeOK || !principalOK || api.repository == nil || api.audit == nil || !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and authenticated context are required")
		return
	}
	var input struct {
		ConversationID  string                 `json:"conversation_id"`
		State           core.ConversationState `json:"state"`
		ExpectedVersion int64                  `json:"expected_version"`
	}
	if decodeStrictJSON(r, &input) != nil || input.ConversationID == "" || !input.State.Valid() || input.ExpectedVersion < 1 {
		writeProblem(w, http.StatusBadRequest, "Invalid conversation transition")
		return
	}
	now := time.Now().UTC()
	if _, err := api.audit.Capture(r.Context(), scope, audit.Entry{ActorID: boundedActorRef(principal.Subject), Source: "api.customer_service", Action: "customer_service.transition", ResourceType: "conversation", ResourceID: input.ConversationID, CorrelationID: key, Risk: audit.RiskWriteSafe, Summary: audit.Summary{"state": input.State, "expected_version": input.ExpectedVersion}}); err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Customer service audit unavailable")
		return
	}
	conversation, err := api.repository.Transition(r.Context(), scope, input.ConversationID, input.State, input.ExpectedVersion, now)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, customerservicerepo.ErrConflict) {
			status = http.StatusConflict
		}
		writeProblem(w, status, "Conversation transition was not applied")
		return
	}
	writeJSON(w, http.StatusOK, conversation)
}

func customerServiceFilter(r *http.Request, defaults core.Filter) (core.Filter, error) {
	q := r.URL.Query()
	filter := defaults
	filter.State = core.ConversationState(strings.TrimSpace(q.Get("state")))
	filter.Priority = core.Priority(strings.TrimSpace(q.Get("priority")))
	filter.AssigneeID = strings.TrimSpace(q.Get("assignee_id"))
	filter.TeamID = strings.TrimSpace(q.Get("team_id"))
	filter.CustomerRefID = strings.TrimSpace(q.Get("customer_ref_id"))
	filter.SLAState = strings.TrimSpace(q.Get("sla_state"))
	filter.Search = strings.TrimSpace(q.Get("search"))
	filter.AfterID = strings.TrimSpace(q.Get("cursor"))
	if value := strings.TrimSpace(q.Get("type")); value != "" {
		filter.Type = core.ConversationType(value)
	}
	if value := strings.TrimSpace(q.Get("unresolved")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return core.Filter{}, err
		}
		filter.Unresolved = parsed
	}
	limit, ok := boundedLimit(r, 50, 200)
	if !ok {
		return core.Filter{}, errors.New("invalid limit")
	}
	filter.Limit = limit
	if filter.State != "" && !filter.State.Valid() || filter.Type != "" && !filter.Type.Valid() || filter.Priority != "" && !filter.Priority.Valid() || len(filter.Search) > 160 || len(filter.SLAState) > 32 {
		return core.Filter{}, errors.New("invalid filter")
	}
	if filter.SLAState != "" && filter.SLAState != "new" && filter.SLAState != "in_progress" && filter.SLAState != "waiting" && filter.SLAState != "escalated" && filter.SLAState != "breached" && filter.SLAState != "met" {
		return core.Filter{}, errors.New("invalid SLA state")
	}
	if core.ValidateCursorRef(filter.AfterID) != nil || core.ValidateCursorRef(filter.AssigneeID) != nil || core.ValidateCursorRef(filter.TeamID) != nil || core.ValidateCursorRef(filter.CustomerRefID) != nil {
		return core.Filter{}, errors.New("invalid reference")
	}
	return filter, nil
}
