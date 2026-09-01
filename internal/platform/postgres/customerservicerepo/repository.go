// Package customerservicerepo persists the tenant-scoped unified customer
// service inbox. Raw provider payloads and unredacted customer data are not
// accepted by this adapter.
package customerservicerepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	core "github.com/torgnexa/torgnexa/internal/core/customerservice"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

var (
	ErrInvalid  = errors.New("customer service repository: invalid request")
	ErrConflict = errors.New("customer service repository: conflict")
	ErrNotFound = errors.New("customer service repository: not found")
)

// Repository is a PostgreSQL adapter for the unified inbox.
type Repository struct{ db *sql.DB }

// New constructs a customer-service repository.
func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &Repository{db: db}, nil
}

// IngestResult describes whether the inbound message was newly persisted.
type IngestResult struct {
	Conversation core.Conversation `json:"conversation"`
	Message      core.Message      `json:"message"`
	Customer     *core.CustomerRef `json:"customer,omitempty"`
	Duplicate    bool              `json:"duplicate"`
}

// Ingest normalizes an already validated inbound record into conversation and
// immutable message history. Polling and webhook replays converge on one row.
func (r *Repository) Ingest(ctx context.Context, scope tenancy.Scope, record core.InboundRecord) (IngestResult, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || record.Validate() != nil {
		return IngestResult{}, ErrInvalid
	}
	var result IngestResult
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		customerID := record.Conversation.CustomerRefID
		if record.Customer != nil {
			customerID = record.Customer.ID
			_, err := tx.ExecContext(ctx, `INSERT INTO customer_service_customer_refs(organization_id,workspace_id,customer_ref_id,source_system,remote_customer_ref,display_name_mask,contact_mask,identity_state,confidence_bps,source,created_at,updated_at,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT(organization_id,workspace_id,source_system,remote_customer_ref) DO NOTHING`, org, workspace, record.Customer.ID, record.Customer.SourceSystem, record.Customer.RemoteCustomerRef, record.Customer.DisplayNameMask, record.Customer.ContactMask, record.Customer.IdentityState, record.Customer.ConfidenceBPS, record.Customer.Source, record.Customer.CreatedAt, record.Customer.UpdatedAt, record.Customer.Version)
			if err != nil {
				return fmt.Errorf("insert customer reference: %w", err)
			}
			if err := tx.QueryRowContext(ctx, `SELECT customer_ref_id FROM customer_service_customer_refs WHERE organization_id=$1 AND workspace_id=$2 AND source_system=$3 AND remote_customer_ref=$4`, org, workspace, record.Customer.SourceSystem, record.Customer.RemoteCustomerRef).Scan(&customerID); err != nil {
				return fmt.Errorf("resolve customer reference: %w", err)
			}
		}
		conversation := record.Conversation
		conversation.CustomerRefID = customerID
		if customerID != "" {
			conversation.IdentityState = record.Message.IdentityState
		}
		var existingID string
		err := tx.QueryRowContext(ctx, `INSERT INTO customer_service_conversations(organization_id,workspace_id,conversation_id,source_system,account_id,remote_thread_id,conversation_type,state,priority,customer_ref_id,identity_state,subject,order_id,order_item_id,product_id,offer_id,return_id,claim_id,assignee_id,team_id,sla_state,first_response_due_at,resolution_due_at,last_message_at,source_quality,moderation_state,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''),$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,NULLIF($22::timestamptz,'0001-01-01 00:00:00+00'),NULLIF($23::timestamptz,'0001-01-01 00:00:00+00'),$24,$25,$26,$27,$28,$29) ON CONFLICT(organization_id,workspace_id,source_system,account_id,remote_thread_id) DO UPDATE SET last_message_at=GREATEST(customer_service_conversations.last_message_at,EXCLUDED.last_message_at),updated_at=GREATEST(customer_service_conversations.updated_at,EXCLUDED.updated_at),version=customer_service_conversations.version+1,customer_ref_id=COALESCE(customer_service_conversations.customer_ref_id,EXCLUDED.customer_ref_id) RETURNING conversation_id`, org, workspace, conversation.ID, conversation.SourceSystem, conversation.AccountID, conversation.RemoteThreadID, conversation.Type, conversation.State, conversation.Priority, customerID, conversation.IdentityState, conversation.Subject, conversation.OrderID, conversation.OrderItemID, conversation.ProductID, conversation.OfferID, conversation.ReturnID, conversation.ClaimID, conversation.AssigneeID, conversation.TeamID, defaultSLAState(conversation.SLAState), nullableTime(conversation.FirstResponseDueAt), nullableTime(conversation.ResolutionDueAt), conversation.LastMessageAt, conversation.SourceQuality, conversation.ModerationState, conversation.Version, conversation.CreatedAt, conversation.UpdatedAt).Scan(&existingID)
		if err != nil {
			return fmt.Errorf("upsert conversation: %w", err)
		}
		result.Conversation = conversation
		result.Conversation.ID = existingID
		if err := scanConversationByID(ctx, tx, org, workspace, existingID, &result.Conversation); err != nil {
			return err
		}
		message := record.Message
		message.ConversationID = existingID
		inserted := true
		var insertedID string
		err = tx.QueryRowContext(ctx, `INSERT INTO customer_service_messages(organization_id,workspace_id,message_id,conversation_id,remote_message_id,inbound_fingerprint,direction,visibility,delivery_state,safe_text,content_digest,language,moderation_state,identity_state,order_id,product_id,occurred_at,received_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) ON CONFLICT DO NOTHING RETURNING message_id`, org, workspace, message.ID, existingID, message.RemoteMessageID, record.Fingerprint, message.Direction, message.Visibility, message.DeliveryState, message.SafeText, message.ContentDigest, message.Language, message.ModerationState, message.IdentityState, message.OrderID, message.ProductID, message.OccurredAt, message.ReceivedAt, message.CreatedAt).Scan(&insertedID)
		if errors.Is(err, sql.ErrNoRows) {
			inserted = false
		} else if err != nil {
			return fmt.Errorf("insert inbound message: %w", err)
		}
		if !inserted {
			if err := tx.QueryRowContext(ctx, `SELECT message_id,conversation_id,remote_message_id,direction,visibility,delivery_state,safe_text,content_digest,language,moderation_state,identity_state,order_id,product_id,occurred_at,received_at,created_at FROM customer_service_messages WHERE organization_id=$1 AND workspace_id=$2 AND inbound_fingerprint=$3`, org, workspace, record.Fingerprint).Scan(messageScanner(&message)...); err != nil {
				if errors.Is(err, sql.ErrNoRows) && message.RemoteMessageID != "" {
					err = tx.QueryRowContext(ctx, `SELECT message_id,conversation_id,remote_message_id,direction,visibility,delivery_state,safe_text,content_digest,language,moderation_state,identity_state,order_id,product_id,occurred_at,received_at,created_at FROM customer_service_messages WHERE organization_id=$1 AND workspace_id=$2 AND conversation_id=$3 AND remote_message_id=$4`, org, workspace, existingID, message.RemoteMessageID).Scan(messageScanner(&message)...)
				}
				if err != nil {
					return fmt.Errorf("read duplicate inbound message: %w", err)
				}
			}
			if message.ContentDigest != record.Message.ContentDigest {
				findingID := "message-conflict-" + record.Fingerprint[:32]
				_, findingErr := tx.ExecContext(ctx, `INSERT INTO customer_service_findings(organization_id,workspace_id,finding_id,conversation_id,kind,severity,status,explanation,expected_digest,observed_digest,detected_at) VALUES($1,$2,$3,$4,'message_content_conflict','block','open','Remote message identity was observed with a different sanitized content digest',$5,$6,$7) ON CONFLICT(organization_id,workspace_id,finding_id) DO NOTHING`, org, workspace, findingID, existingID, message.ContentDigest, record.Message.ContentDigest, record.Message.CreatedAt)
				if findingErr != nil {
					return fmt.Errorf("record inbound message conflict: %w", findingErr)
				}
			}
		}
		result.Message = message
		result.Duplicate = !inserted
		if result.Customer == nil && customerID != "" {
			var customer core.CustomerRef
			if err := tx.QueryRowContext(ctx, `SELECT customer_ref_id,source_system,remote_customer_ref,display_name_mask,contact_mask,identity_state,confidence_bps,source,created_at,updated_at,version FROM customer_service_customer_refs WHERE organization_id=$1 AND workspace_id=$2 AND customer_ref_id=$3`, org, workspace, customerID).Scan(&customer.ID, &customer.SourceSystem, &customer.RemoteCustomerRef, &customer.DisplayNameMask, &customer.ContactMask, &customer.IdentityState, &customer.ConfidenceBPS, &customer.Source, &customer.CreatedAt, &customer.UpdatedAt, &customer.Version); err == nil {
				result.Customer = &customer
			}
		}
		return nil
	})
	return result, err
}

// ListInbox returns a bounded operator queue.
func (r *Repository) ListInbox(ctx context.Context, scope tenancy.Scope, filter core.Filter) (core.InboxPage, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || filter.Limit < 1 || filter.Limit > 200 || core.ValidateCursorRef(filter.AfterID) != nil || core.ValidateCursorRef(filter.AssigneeID) != nil || core.ValidateCursorRef(filter.TeamID) != nil || core.ValidateCursorRef(filter.CustomerRefID) != nil {
		return core.InboxPage{}, ErrInvalid
	}
	var page core.InboxPage
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		search := strings.TrimSpace(filter.Search)
		if len(search) > 160 {
			return ErrInvalid
		}
		search = "%" + escapeLike(search) + "%"
		rows, err := tx.QueryContext(ctx, `SELECT conversation_id,source_system,account_id,remote_thread_id,conversation_type,state,priority,COALESCE(customer_ref_id,''),identity_state,subject,order_id,order_item_id,product_id,offer_id,return_id,claim_id,assignee_id,team_id,sla_state,first_response_due_at,resolution_due_at,last_message_at,source_quality,moderation_state,version,created_at,updated_at FROM customer_service_conversations WHERE organization_id=$1 AND workspace_id=$2 AND ($3='' OR state=$3) AND ($4='' OR conversation_type=$4) AND ($5='' OR priority=$5) AND ($6='' OR assignee_id=$6) AND ($7='' OR team_id=$7) AND ($8='' OR customer_ref_id=$8) AND ($9='' OR sla_state=$9) AND ($10=false OR state NOT IN ('resolved','closed','spam')) AND ($11='' OR conversation_id>$11) AND ($12='%%' OR subject ILIKE $12 ESCAPE '\\' OR order_id ILIKE $12 ESCAPE '\\' OR conversation_id ILIKE $12 ESCAPE '\\') ORDER BY conversation_id LIMIT $13`, org, workspace, filter.State, filter.Type, filter.Priority, filter.AssigneeID, filter.TeamID, filter.CustomerRefID, filter.SLAState, filter.Unresolved, filter.AfterID, search, filter.Limit+1)
		if err != nil {
			return fmt.Errorf("list inbox: %w", err)
		}
		defer rows.Close()
		page.Items = make([]core.Conversation, 0, filter.Limit)
		for rows.Next() {
			var item core.Conversation
			if err := scanConversation(rows, &item); err != nil {
				return err
			}
			page.Items = append(page.Items, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(page.Items) > filter.Limit {
			page.HasMore = true
			page.Items = page.Items[:filter.Limit]
		}
		return nil
	})
	return page, err
}

// GetThread returns messages and durable reply intents for one conversation.
func (r *Repository) GetThread(ctx context.Context, scope tenancy.Scope, conversationID string) (core.Thread, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || core.ValidateCursorRef(conversationID) != nil {
		return core.Thread{}, ErrInvalid
	}
	var thread core.Thread
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		if err := scanConversationByID(ctx, tx, org, workspace, conversationID, &thread.Conversation); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if thread.Conversation.CustomerRefID != "" {
			var customer core.CustomerRef
			if err := tx.QueryRowContext(ctx, `SELECT customer_ref_id,source_system,remote_customer_ref,display_name_mask,contact_mask,identity_state,confidence_bps,source,created_at,updated_at,version FROM customer_service_customer_refs WHERE organization_id=$1 AND workspace_id=$2 AND customer_ref_id=$3`, org, workspace, thread.Conversation.CustomerRefID).Scan(&customer.ID, &customer.SourceSystem, &customer.RemoteCustomerRef, &customer.DisplayNameMask, &customer.ContactMask, &customer.IdentityState, &customer.ConfidenceBPS, &customer.Source, &customer.CreatedAt, &customer.UpdatedAt, &customer.Version); err == nil {
				thread.Customer = &customer
			}
		}
		rows, err := tx.QueryContext(ctx, `SELECT message_id,conversation_id,remote_message_id,direction,visibility,delivery_state,safe_text,content_digest,language,moderation_state,identity_state,order_id,product_id,occurred_at,received_at,created_at FROM customer_service_messages WHERE organization_id=$1 AND workspace_id=$2 AND conversation_id=$3 ORDER BY occurred_at,message_id LIMIT 500`, org, workspace, conversationID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item core.Message
			if err := rows.Scan(messageScanner(&item)...); err != nil {
				rows.Close()
				return err
			}
			thread.Messages = append(thread.Messages, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		replies, err := tx.QueryContext(ctx, `SELECT reply_id,conversation_id,visibility,origin,safe_text,content_digest,template_id,approval_ref,idempotency_key,delivery_state,remote_receipt,error_code,created_at,updated_at,version FROM customer_service_replies WHERE organization_id=$1 AND workspace_id=$2 AND conversation_id=$3 ORDER BY created_at,reply_id LIMIT 500`, org, workspace, conversationID)
		if err != nil {
			return err
		}
		defer replies.Close()
		for replies.Next() {
			var item core.Reply
			if err := replies.Scan(&item.ID, &item.ConversationID, &item.Visibility, &item.Origin, &item.SafeText, &item.ContentDigest, &item.TemplateID, &item.ApprovalRef, &item.IdempotencyKey, &item.DeliveryState, &item.RemoteReceipt, &item.ErrorCode, &item.CreatedAt, &item.UpdatedAt, &item.Version); err != nil {
				return err
			}
			thread.Replies = append(thread.Replies, item)
		}
		return replies.Err()
	})
	return thread, err
}

// Timeline builds a bounded privacy-safe Customer 360 timeline.
func (r *Repository) Timeline(ctx context.Context, scope tenancy.Scope, customerRefID string) ([]core.TimelineEvent, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || core.ValidateCursorRef(customerRefID) != nil || customerRefID == "" {
		return nil, ErrInvalid
	}
	var events []core.TimelineEvent
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		rows, err := tx.QueryContext(ctx, `SELECT conversation_id,conversation_type,subject,last_message_at,order_id,product_id,return_id,claim_id FROM customer_service_conversations WHERE organization_id=$1 AND workspace_id=$2 AND customer_ref_id=$3 ORDER BY last_message_at DESC,conversation_id LIMIT 200`, org, workspace, customerRefID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, kind, subject, orderID, productID, returnID, claimID string
			var at time.Time
			if err := rows.Scan(&id, &kind, &subject, &at, &orderID, &productID, &returnID, &claimID); err != nil {
				return err
			}
			events = append(events, core.TimelineEvent{ID: "conversation:" + id, Kind: string(kind), ReferenceID: id, ConversationID: id, Summary: summaryOrDefault(subject, "Обращение"), OccurredAt: at.UTC()})
			for refKind, refID := range map[string]string{"order": orderID, "product": productID, "return": returnID, "claim": claimID} {
				if refID != "" {
					events = append(events, core.TimelineEvent{ID: refKind + ":" + refID + ":" + id, Kind: refKind, ReferenceID: refID, ConversationID: id, Summary: refKind + " " + refID, OccurredAt: at.UTC()})
				}
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return core.BuildTimeline(events)
}

// QueueReply appends a human/template reply intent. Public delivery is queued
// for a qualified connector worker; internal notes remain draft-only.
func (r *Repository) QueueReply(ctx context.Context, scope tenancy.Scope, reply core.Reply) (core.Reply, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || reply.Validate() != nil {
		return core.Reply{}, ErrInvalid
	}
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		if err := tx.QueryRowContext(ctx, `INSERT INTO customer_service_replies(organization_id,workspace_id,reply_id,conversation_id,visibility,origin,safe_text,content_digest,template_id,approval_ref,idempotency_key,delivery_state,remote_receipt,error_code,created_at,updated_at,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'','',$13,$14,1) ON CONFLICT(organization_id,workspace_id,idempotency_key) DO NOTHING RETURNING reply_id`, org, workspace, reply.ID, reply.ConversationID, reply.Visibility, reply.Origin, reply.SafeText, reply.ContentDigest, reply.TemplateID, reply.ApprovalRef, reply.IdempotencyKey, reply.DeliveryState, reply.CreatedAt, reply.UpdatedAt).Scan(&reply.ID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("queue reply: %w", err)
		}
		var existing core.Reply
		if err := tx.QueryRowContext(ctx, `SELECT reply_id,conversation_id,visibility,origin,safe_text,content_digest,template_id,approval_ref,idempotency_key,delivery_state,remote_receipt,error_code,created_at,updated_at,version FROM customer_service_replies WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, org, workspace, reply.IdempotencyKey).Scan(&existing.ID, &existing.ConversationID, &existing.Visibility, &existing.Origin, &existing.SafeText, &existing.ContentDigest, &existing.TemplateID, &existing.ApprovalRef, &existing.IdempotencyKey, &existing.DeliveryState, &existing.RemoteReceipt, &existing.ErrorCode, &existing.CreatedAt, &existing.UpdatedAt, &existing.Version); err != nil {
			return err
		}
		if existing.ContentDigest != reply.ContentDigest || existing.ConversationID != reply.ConversationID || existing.Visibility != reply.Visibility {
			return ErrConflict
		}
		reply = existing
		return nil
	})
	return reply, err
}

// Assign records an ownership transition with optimistic concurrency.
func (r *Repository) Assign(ctx context.Context, scope tenancy.Scope, assignment core.Assignment) (core.Conversation, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || assignment.Validate() != nil {
		return core.Conversation{}, ErrInvalid
	}
	var conversation core.Conversation
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		if err := scanConversationByIDForUpdate(ctx, tx, org, workspace, assignment.ConversationID, &conversation); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if conversation.Version != assignment.ExpectedVersion {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO customer_service_assignments(organization_id,workspace_id,assignment_id,conversation_id,assignee_id,team_id,reason,expected_version,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, org, workspace, assignment.ID, assignment.ConversationID, assignment.AssigneeID, assignment.TeamID, assignment.Reason, assignment.ExpectedVersion, assignment.CreatedAt); err != nil {
			return fmt.Errorf("append assignment: %w", err)
		}
		if err := tx.QueryRowContext(ctx, `UPDATE customer_service_conversations SET assignee_id=$4,team_id=$5,version=version+1,updated_at=$6 WHERE organization_id=$1 AND workspace_id=$2 AND conversation_id=$3 RETURNING conversation_id`, org, workspace, assignment.ConversationID, assignment.AssigneeID, assignment.TeamID, assignment.CreatedAt).Scan(&conversation.ID); err != nil {
			return err
		}
		return scanConversationByID(ctx, tx, org, workspace, assignment.ConversationID, &conversation)
	})
	return conversation, err
}

// Transition changes only the operator lifecycle and requires the expected
// version so a concurrent triage cannot silently overwrite another action.
func (r *Repository) Transition(ctx context.Context, scope tenancy.Scope, conversationID string, state core.ConversationState, expectedVersion int64, at time.Time) (core.Conversation, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || core.ValidateCursorRef(conversationID) != nil || !state.Valid() || expectedVersion < 1 || at.IsZero() || !at.Equal(at.UTC()) {
		return core.Conversation{}, ErrInvalid
	}
	var conversation core.Conversation
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		var id string
		if err := tx.QueryRowContext(ctx, `UPDATE customer_service_conversations SET state=$4,version=version+1,updated_at=$5 WHERE organization_id=$1 AND workspace_id=$2 AND conversation_id=$3 AND version=$6 RETURNING conversation_id`, org, workspace, conversationID, state, at, expectedVersion).Scan(&id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrConflict
			}
			return err
		}
		return scanConversationByID(ctx, tx, org, workspace, id, &conversation)
	})
	return conversation, err
}

// AddFinding appends an explainable reconciliation finding.
func (r *Repository) AddFinding(ctx context.Context, scope tenancy.Scope, finding core.Finding) error {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || finding.Validate() != nil {
		return ErrInvalid
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		_, err := tx.ExecContext(ctx, `INSERT INTO customer_service_findings(organization_id,workspace_id,finding_id,conversation_id,kind,severity,status,explanation,expected_digest,observed_digest,detected_at,resolved_at) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),$11,NULLIF($12::timestamptz,'0001-01-01 00:00:00+00')) ON CONFLICT(organization_id,workspace_id,finding_id) DO NOTHING`, org, workspace, finding.ID, finding.ConversationID, finding.Kind, finding.Severity, finding.Status, finding.Explanation, finding.ExpectedDigest, finding.ObservedDigest, finding.DetectedAt, nullableTime(finding.ResolvedAt))
		return err
	})
}

// ListFindings returns a bounded reconciliation queue.
func (r *Repository) ListFindings(ctx context.Context, scope tenancy.Scope, afterID string, limit int) ([]core.Finding, bool, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || core.ValidateCursorRef(afterID) != nil || limit < 1 || limit > 200 {
		return nil, false, ErrInvalid
	}
	var items []core.Finding
	var more bool
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		rows, err := tx.QueryContext(ctx, `SELECT finding_id,conversation_id,kind,severity,status,explanation,expected_digest,observed_digest,detected_at,resolved_at FROM customer_service_findings WHERE organization_id=$1 AND workspace_id=$2 AND ($3='' OR finding_id>$3) ORDER BY finding_id LIMIT $4`, org, workspace, afterID, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item core.Finding
			var conversationID string
			var expected, observed sql.NullString
			var resolved sql.NullTime
			if err := rows.Scan(&item.ID, &conversationID, &item.Kind, &item.Severity, &item.Status, &item.Explanation, &expected, &observed, &item.DetectedAt, &resolved); err != nil {
				return err
			}
			item.ConversationID = conversationID
			item.ExpectedDigest, item.ObservedDigest = expected.String, observed.String
			if resolved.Valid {
				item.ResolvedAt = resolved.Time.UTC()
			}
			item.DetectedAt = item.DetectedAt.UTC()
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(items) > limit {
			more = true
			items = items[:limit]
		}
		return nil
	})
	return items, more, err
}

// Summary returns queue counters used by the inbox dashboard.
func (r *Repository) Summary(ctx context.Context, scope tenancy.Scope) (core.Summary, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() {
		return core.Summary{}, ErrInvalid
	}
	var summary core.Summary
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		return tx.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE state='unread'),count(*) FILTER(WHERE state='open'),count(*) FILTER(WHERE state IN ('pending_customer','pending_internal')),count(*) FILTER(WHERE sla_state='breached'),count(*) FILTER(WHERE conversation_type='review'),count(*) FILTER(WHERE conversation_type='question'),count(*) FILTER(WHERE conversation_type='claim'),(SELECT count(*) FROM customer_service_replies WHERE organization_id=$1 AND workspace_id=$2 AND delivery_state='unknown') FROM customer_service_conversations WHERE organization_id=$1 AND workspace_id=$2`, org, workspace).Scan(&summary.Total, &summary.Unread, &summary.Open, &summary.Pending, &summary.Breached, &summary.Reviews, &summary.Questions, &summary.Claims, &summary.UnknownReplies)
	})
	if summary.Total == 0 {
		summary.Quality = core.QualityUnknown
	} else if summary.Breached > 0 || summary.UnknownReplies > 0 {
		summary.Quality = core.QualityPartial
	} else {
		summary.Quality = core.QualityConfirmed
	}
	return summary, err
}

func (r *Repository) tx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

type scanner interface{ Scan(...any) error }

func scanConversation(row scanner, item *core.Conversation) error {
	var customerID, subject, orderID, orderItemID, productID, offerID, returnID, claimID, assigneeID, teamID string
	var firstDue, resolutionDue sql.NullTime
	if err := row.Scan(&item.ID, &item.SourceSystem, &item.AccountID, &item.RemoteThreadID, &item.Type, &item.State, &item.Priority, &customerID, &item.IdentityState, &subject, &orderID, &orderItemID, &productID, &offerID, &returnID, &claimID, &assigneeID, &teamID, &item.SLAState, &firstDue, &resolutionDue, &item.LastMessageAt, &item.SourceQuality, &item.ModerationState, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return err
	}
	item.CustomerRefID, item.Subject, item.OrderID, item.OrderItemID, item.ProductID, item.OfferID = customerID, subject, orderID, orderItemID, productID, offerID
	item.ReturnID, item.ClaimID, item.AssigneeID, item.TeamID = returnID, claimID, assigneeID, teamID
	if firstDue.Valid {
		item.FirstResponseDueAt = firstDue.Time.UTC()
	}
	if resolutionDue.Valid {
		item.ResolutionDueAt = resolutionDue.Time.UTC()
	}
	item.LastMessageAt, item.CreatedAt, item.UpdatedAt = item.LastMessageAt.UTC(), item.CreatedAt.UTC(), item.UpdatedAt.UTC()
	return nil
}

func scanConversationByID(ctx context.Context, tx *sql.Tx, org, workspace, id string, item *core.Conversation) error {
	return scanConversation(tx.QueryRowContext(ctx, `SELECT conversation_id,source_system,account_id,remote_thread_id,conversation_type,state,priority,COALESCE(customer_ref_id,''),identity_state,subject,order_id,order_item_id,product_id,offer_id,return_id,claim_id,assignee_id,team_id,sla_state,first_response_due_at,resolution_due_at,last_message_at,source_quality,moderation_state,version,created_at,updated_at FROM customer_service_conversations WHERE organization_id=$1 AND workspace_id=$2 AND conversation_id=$3`, org, workspace, id), item)
}

func scanConversationByIDForUpdate(ctx context.Context, tx *sql.Tx, org, workspace, id string, item *core.Conversation) error {
	return scanConversation(tx.QueryRowContext(ctx, `SELECT conversation_id,source_system,account_id,remote_thread_id,conversation_type,state,priority,COALESCE(customer_ref_id,''),identity_state,subject,order_id,order_item_id,product_id,offer_id,return_id,claim_id,assignee_id,team_id,sla_state,first_response_due_at,resolution_due_at,last_message_at,source_quality,moderation_state,version,created_at,updated_at FROM customer_service_conversations WHERE organization_id=$1 AND workspace_id=$2 AND conversation_id=$3 FOR UPDATE`, org, workspace, id), item)
}

func messageScanner(item *core.Message) []any {
	return []any{&item.ID, &item.ConversationID, &item.RemoteMessageID, &item.Direction, &item.Visibility, &item.DeliveryState, &item.SafeText, &item.ContentDigest, &item.Language, &item.ModerationState, &item.IdentityState, &item.OrderID, &item.ProductID, &item.OccurredAt, &item.ReceivedAt, &item.CreatedAt}
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func defaultSLAState(value string) string {
	if value == "" {
		return "new"
	}
	return value
}

func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

func summaryOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
