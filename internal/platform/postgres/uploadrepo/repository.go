// Package uploadrepo implements tenant-scoped PostgreSQL persistence for the
// complete upload quarantine, security evidence and release pipeline.
package uploadrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true), set_config('app.workspace_id',$2,true)`

const uploadSelect = `SELECT id,organization_id,workspace_id,original_filename,COALESCE(declared_media_type,''),declared_size_bytes,state,COALESCE(quarantine_object_key,''),COALESCE(released_object_key,''),COALESCE(content_size_bytes,0),COALESCE(content_sha256,''),COALESCE(security_evidence_id,''),version,received_at,quarantined_at,released_at,updated_at FROM uploads WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`

type Repository struct {
	db       *sql.DB
	maxBytes int64
}

var _ uploads.Repository = (*Repository)(nil)
var _ uploads.SecurityPipelineRepository = (*Repository)(nil)

func New(db *sql.DB, maxBytes int64) (*Repository, error) {
	policy := uploads.DefaultPolicy()
	policy.MaxFileBytes = maxBytes
	if db == nil || policy.Validate() != nil {
		return nil, uploads.ErrInvalid
	}
	return &Repository{db: db, maxBytes: maxBytes}, nil
}

func (r *Repository) CreateReceived(ctx context.Context, scope tenancy.Scope, record uploads.Record) error {
	if err := r.validate(ctx, scope); err != nil {
		return err
	}
	if record.State != uploads.StateReceived || record.Validate(scope, r.maxBytes) != nil {
		return uploads.ErrInvalid
	}
	err := r.write(ctx, scope, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO uploads(id,organization_id,workspace_id,original_filename,declared_media_type,declared_size_bytes,state,version,received_at,updated_at) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,'received',1,$7,$8) ON CONFLICT DO NOTHING`, record.ID.String(), scope.OrganizationID().String(), scope.WorkspaceID().String(), record.Metadata.OriginalFilename, record.Metadata.DeclaredMediaType, record.Metadata.DeclaredSizeBytes, record.ReceivedAt, record.UpdatedAt)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return uploads.ErrConflict
		}
		return nil
	})
	return normalize(err)
}

func (r *Repository) MarkQuarantined(ctx context.Context, scope tenancy.Scope, id uploads.ID, object uploads.StoredObject, mutation uploads.Mutation) (uploads.Record, error) {
	if err := r.validate(ctx, scope); err != nil {
		return uploads.Record{}, err
	}
	if !id.Valid() || mutation.Validate() != nil {
		return uploads.Record{}, uploads.ErrInvalid
	}
	if object.ValidateFor(scope, id, r.maxBytes) != nil {
		return uploads.Record{}, uploads.ErrInvalid
	}
	var out uploads.Record
	err := r.write(ctx, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `UPDATE uploads SET state='quarantined',quarantine_object_key=$4,content_size_bytes=$5,content_sha256=$6,quarantined_at=$7,updated_at=$7,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND state='received' RETURNING `+uploadSelectColumns(), scope.OrganizationID().String(), scope.WorkspaceID().String(), id.String(), object.Key, object.SizeBytes, object.SHA256, mutation.OccurredAt)
		var err error
		out, err = scan(row)
		if errors.Is(err, sql.ErrNoRows) {
			return classifyMiss(ctx, tx, scope, id)
		}
		if err != nil {
			return err
		}
		if out.Validate(scope, r.maxBytes) != nil || out.State != uploads.StateQuarantined {
			return uploads.ErrInvalid
		}
		event, err := quarantinedEvent(scope, out, mutation)
		if err != nil {
			return err
		}
		enqueuer, err := outboxrepo.NewTransactionEnqueuer(tx)
		if err != nil {
			return err
		}
		if err := enqueuer.Enqueue(ctx, event); err != nil {
			return fmt.Errorf("upload repository: enqueue quarantine event: %w", err)
		}
		return nil
	})
	if err != nil {
		return uploads.Record{}, normalize(err)
	}
	return out, nil
}

func (r *Repository) MarkValidated(ctx context.Context, scope tenancy.Scope, id uploads.ID, at time.Time) (uploads.Record, error) {
	return r.markSimpleState(ctx, scope, id, uploads.StateQuarantined, uploads.StateValidated, at)
}

func (r *Repository) MarkScanning(ctx context.Context, scope tenancy.Scope, id uploads.ID, at time.Time) (uploads.Record, error) {
	return r.markSimpleState(ctx, scope, id, uploads.StateValidated, uploads.StateScanning, at)
}

func (r *Repository) markSimpleState(ctx context.Context, scope tenancy.Scope, id uploads.ID, from, to uploads.State, at time.Time) (uploads.Record, error) {
	if err := r.validate(ctx, scope); err != nil {
		return uploads.Record{}, err
	}
	if !id.Valid() || at.IsZero() || at.Location() != time.UTC {
		return uploads.Record{}, uploads.ErrInvalid
	}
	var out uploads.Record
	err := r.write(ctx, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `UPDATE uploads SET state=$4,updated_at=$5,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND state=$6 RETURNING `+uploadSelectColumns(), scope.OrganizationID().String(), scope.WorkspaceID().String(), id.String(), string(to), at, string(from))
		var err error
		out, err = scan(row)
		if errors.Is(err, sql.ErrNoRows) {
			return classifyMiss(ctx, tx, scope, id)
		}
		if err != nil {
			return err
		}
		if out.Validate(scope, r.maxBytes) != nil || out.State != to {
			return uploads.ErrInvalid
		}
		return nil
	})
	if err != nil {
		return uploads.Record{}, normalize(err)
	}
	return out, nil
}

func (r *Repository) RecordDecision(ctx context.Context, scope tenancy.Scope, id uploads.ID, evidence uploads.SecurityEvidence, mutation uploads.Mutation) (uploads.Record, uploads.SecurityEvidence, error) {
	if err := r.validate(ctx, scope); err != nil {
		return uploads.Record{}, uploads.SecurityEvidence{}, err
	}
	if !id.Valid() || mutation.Validate() != nil {
		return uploads.Record{}, uploads.SecurityEvidence{}, uploads.ErrInvalid
	}
	var out uploads.Record
	var persisted uploads.SecurityEvidence
	err := r.write(ctx, scope, func(tx *sql.Tx) error {
		current, err := scan(tx.QueryRowContext(ctx, uploadSelect+` FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id.String()))
		if errors.Is(err, sql.ErrNoRows) {
			return uploads.ErrNotFound
		}
		if err != nil {
			return err
		}
		if current.Validate(scope, r.maxBytes) != nil {
			return uploads.ErrInvalid
		}
		allowed := false
		switch evidence.Decision {
		case uploads.DecisionError:
			allowed = current.State == uploads.StateScanning
		case uploads.DecisionClean:
			allowed = current.State == uploads.StateScanning
		case uploads.DecisionRejected:
			allowed = current.State == uploads.StateScanning || current.State == uploads.StateQuarantined
		}
		if !allowed || evidence.UploadID != id || evidence.OrganizationID != scope.OrganizationID() || evidence.WorkspaceID != scope.WorkspaceID() || evidence.ContentSHA256 != current.ContentSHA256 || evidence.ContentSizeBytes != current.ContentSizeBytes {
			return uploads.ErrConflict
		}
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt),0)+1 FROM upload_security_evidence WHERE organization_id=$1 AND workspace_id=$2 AND upload_id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id.String()).Scan(&evidence.Attempt); err != nil {
			return err
		}
		if evidence.Validate(scope, r.maxBytes) != nil {
			return uploads.ErrInvalid
		}
		checks, err := json.Marshal(evidence.Checks)
		if err != nil {
			return uploads.ErrInvalid
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO upload_security_evidence(id,organization_id,workspace_id,upload_id,attempt,policy_version,content_sha256,content_size_bytes,detected_media_type,extension,decision,reason_code,checks,scanner_name,scanner_engine_version,scanner_signature_version,scanner_status,threat_code,rescan_of,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14,$15,$16,$17,NULLIF($18,''),NULLIF($19,''),$20)`, evidence.ID, scope.OrganizationID().String(), scope.WorkspaceID().String(), id.String(), evidence.Attempt, evidence.PolicyVersion, evidence.ContentSHA256, evidence.ContentSizeBytes, evidence.DetectedMediaType, evidence.Extension, string(evidence.Decision), evidence.ReasonCode, string(checks), evidence.Scanner.ScannerName, evidence.Scanner.EngineVersion, evidence.Scanner.SignatureVersion, string(evidence.Scanner.Status), evidence.Scanner.ThreatCode, evidence.RescanOf, evidence.CreatedAt)
		if err != nil {
			return err
		}
		persisted = evidence
		out = current
		if evidence.Decision != uploads.DecisionError {
			next := uploads.StateRejected
			if evidence.Decision == uploads.DecisionClean {
				next = uploads.StateClean
			}
			out, err = scan(tx.QueryRowContext(ctx, `UPDATE uploads SET state=$4,security_evidence_id=$5,updated_at=$6,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND state=$7 RETURNING `+uploadSelectColumns(), scope.OrganizationID().String(), scope.WorkspaceID().String(), id.String(), string(next), evidence.ID, mutation.OccurredAt, string(current.State)))
			if err != nil {
				return err
			}
			if out.Validate(scope, r.maxBytes) != nil || out.State != next || out.SecurityEvidenceID != evidence.ID {
				return uploads.ErrInvalid
			}
		}
		event, err := securityDecisionEvent(scope, id, persisted, mutation)
		if err != nil {
			return err
		}
		enqueuer, err := outboxrepo.NewTransactionEnqueuer(tx)
		if err != nil {
			return err
		}
		if err := enqueuer.Enqueue(ctx, event); err != nil {
			return fmt.Errorf("upload repository: enqueue security decision event: %w", err)
		}
		return nil
	})
	if err != nil {
		return uploads.Record{}, uploads.SecurityEvidence{}, normalize(err)
	}
	return out, persisted, nil
}

func (r *Repository) MarkReleased(ctx context.Context, scope tenancy.Scope, id uploads.ID, evidenceID string, object uploads.StoredObject, mutation uploads.Mutation) (uploads.Record, error) {
	if err := r.validate(ctx, scope); err != nil {
		return uploads.Record{}, err
	}
	if !id.Valid() || mutation.Validate() != nil || object.ValidateReleasedFor(scope, id, r.maxBytes) != nil || evidenceID == "" {
		return uploads.Record{}, uploads.ErrInvalid
	}
	var out uploads.Record
	err := r.write(ctx, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `UPDATE uploads SET state='released',released_object_key=$4,released_at=$5,updated_at=$5,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND state='clean' AND security_evidence_id=$6 AND content_size_bytes=$7 AND content_sha256=$8 RETURNING `+uploadSelectColumns(), scope.OrganizationID().String(), scope.WorkspaceID().String(), id.String(), object.Key, mutation.OccurredAt, evidenceID, object.SizeBytes, object.SHA256)
		var err error
		out, err = scan(row)
		if errors.Is(err, sql.ErrNoRows) {
			return classifyMiss(ctx, tx, scope, id)
		}
		if err != nil {
			return err
		}
		if out.Validate(scope, r.maxBytes) != nil || out.State != uploads.StateReleased {
			return uploads.ErrInvalid
		}
		event, err := releasedEvent(scope, out, mutation)
		if err != nil {
			return err
		}
		enqueuer, err := outboxrepo.NewTransactionEnqueuer(tx)
		if err != nil {
			return err
		}
		if err := enqueuer.Enqueue(ctx, event); err != nil {
			return fmt.Errorf("upload repository: enqueue release event: %w", err)
		}
		return nil
	})
	if err != nil {
		return uploads.Record{}, normalize(err)
	}
	return out, nil
}

func (r *Repository) RequestRescan(ctx context.Context, scope tenancy.Scope, id uploads.ID, reason string, mutation uploads.Mutation) (uploads.Record, error) {
	if err := r.validate(ctx, scope); err != nil {
		return uploads.Record{}, err
	}
	if !id.Valid() || mutation.Validate() != nil || !uploads.ValidRescanReasonCode(reason) {
		return uploads.Record{}, uploads.ErrInvalid
	}
	var out uploads.Record
	err := r.write(ctx, scope, func(tx *sql.Tx) error {
		current, err := scan(tx.QueryRowContext(ctx, uploadSelect+` FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id.String()))
		if errors.Is(err, sql.ErrNoRows) {
			return uploads.ErrNotFound
		}
		if err != nil {
			return err
		}
		if current.State != uploads.StateClean && current.State != uploads.StateRejected && current.State != uploads.StateReleased {
			return uploads.ErrConflict
		}
		prior := current.SecurityEvidenceID
		out, err = scan(tx.QueryRowContext(ctx, `UPDATE uploads SET state='quarantined',released_object_key=NULL,security_evidence_id=NULL,released_at=NULL,updated_at=$4,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND state=$5 RETURNING `+uploadSelectColumns(), scope.OrganizationID().String(), scope.WorkspaceID().String(), id.String(), mutation.OccurredAt, string(current.State)))
		if err != nil {
			return err
		}
		if out.Validate(scope, r.maxBytes) != nil || out.State != uploads.StateQuarantined {
			return uploads.ErrInvalid
		}
		event, err := rescanEvent(scope, id, prior, reason, mutation)
		if err != nil {
			return err
		}
		enqueuer, err := outboxrepo.NewTransactionEnqueuer(tx)
		if err != nil {
			return err
		}
		if err := enqueuer.Enqueue(ctx, event); err != nil {
			return fmt.Errorf("upload repository: enqueue rescan event: %w", err)
		}
		return nil
	})
	if err != nil {
		return uploads.Record{}, normalize(err)
	}
	return out, nil
}

func (r *Repository) ListSecurityEvidence(ctx context.Context, scope tenancy.Scope, id uploads.ID, limit int) ([]uploads.SecurityEvidence, error) {
	if err := r.validate(ctx, scope); err != nil {
		return nil, err
	}
	if !id.Valid() || limit < 1 || limit > 100 {
		return nil, uploads.ErrInvalid
	}
	var out []uploads.SecurityEvidence
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,upload_id,attempt,policy_version,content_sha256,content_size_bytes,detected_media_type,extension,decision,reason_code,checks,scanner_name,scanner_engine_version,scanner_signature_version,scanner_status,COALESCE(threat_code,''),COALESCE(rescan_of,''),created_at FROM upload_security_evidence WHERE organization_id=$1 AND workspace_id=$2 AND upload_id=$3 ORDER BY attempt DESC LIMIT $4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id.String(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e uploads.SecurityEvidence
			var uploadID, decision, scannerStatus string
			var checksRaw []byte
			e.OrganizationID = scope.OrganizationID()
			e.WorkspaceID = scope.WorkspaceID()
			if err := rows.Scan(&e.ID, &uploadID, &e.Attempt, &e.PolicyVersion, &e.ContentSHA256, &e.ContentSizeBytes, &e.DetectedMediaType, &e.Extension, &decision, &e.ReasonCode, &checksRaw, &e.Scanner.ScannerName, &e.Scanner.EngineVersion, &e.Scanner.SignatureVersion, &scannerStatus, &e.Scanner.ThreatCode, &e.RescanOf, &e.CreatedAt); err != nil {
				return err
			}
			e.UploadID = uploads.ID(uploadID)
			e.Decision = uploads.EvidenceDecision(decision)
			e.Scanner.Status = uploads.ScannerStatus(scannerStatus)
			e.CreatedAt = e.CreatedAt.UTC()
			if err := json.Unmarshal(checksRaw, &e.Checks); err != nil || e.Validate(scope, r.maxBytes) != nil {
				return uploads.ErrInvalid
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, normalize(err)
	}
	return out, nil
}

func securityDecisionEvent(scope tenancy.Scope, id uploads.ID, evidence uploads.SecurityEvidence, mutation uploads.Mutation) (eventbus.Event, error) {
	return uploadSecurityEvent(scope, id, mutation, "security.upload.decision.v1", struct {
		EventID           string `json:"event_id"`
		ObjectID          string `json:"object_id"`
		EvidenceID        string `json:"evidence_id"`
		Decision          string `json:"decision"`
		ReasonCode        string `json:"reason_code"`
		DetectedMediaType string `json:"detected_media_type"`
		ScannerStatus     string `json:"scanner_status"`
	}{mutation.EventID, id.String(), evidence.ID, string(evidence.Decision), evidence.ReasonCode, evidence.DetectedMediaType, string(evidence.Scanner.Status)})
}
func releasedEvent(scope tenancy.Scope, record uploads.Record, mutation uploads.Mutation) (eventbus.Event, error) {
	return uploadSecurityEvent(scope, record.ID, mutation, "security.upload.released.v1", struct {
		EventID    string `json:"event_id"`
		ObjectID   string `json:"object_id"`
		EvidenceID string `json:"evidence_id"`
		SHA256     string `json:"sha256"`
		SizeBytes  int64  `json:"size_bytes"`
	}{mutation.EventID, record.ID.String(), record.SecurityEvidenceID, record.ContentSHA256, record.ContentSizeBytes})
}
func rescanEvent(scope tenancy.Scope, id uploads.ID, prior, reason string, mutation uploads.Mutation) (eventbus.Event, error) {
	return uploadSecurityEvent(scope, id, mutation, "security.upload.rescan_requested.v1", struct {
		EventID         string `json:"event_id"`
		ObjectID        string `json:"object_id"`
		ReasonCode      string `json:"reason_code"`
		PriorEvidenceID string `json:"prior_evidence_id"`
	}{mutation.EventID, id.String(), reason, prior})
}
func uploadSecurityEvent(scope tenancy.Scope, id uploads.ID, mutation uploads.Mutation, eventType string, payload any) (eventbus.Event, error) {
	typ, err := eventbus.ParseEventType(eventType)
	if err != nil {
		return eventbus.Event{}, err
	}
	at, err := domain.NewUTCInstant(mutation.OccurredAt)
	if err != nil {
		return eventbus.Event{}, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return eventbus.Event{}, err
	}
	e := eventbus.Event{ID: mutation.EventID, Type: typ, OccurredAt: at, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), EntityType: "upload", EntityID: id.String(), Source: mutation.Source, CorrelationID: mutation.CorrelationID, CausationID: mutation.CausationID, ActorID: mutation.ActorID, TraceID: mutation.TraceID, Data: data}
	if err := e.Validate(); err != nil {
		return eventbus.Event{}, fmt.Errorf("upload repository: event: %w", err)
	}
	return e, nil
}

func (r *Repository) Get(ctx context.Context, scope tenancy.Scope, id uploads.ID) (uploads.Record, error) {
	if err := r.validate(ctx, scope); err != nil {
		return uploads.Record{}, err
	}
	if !id.Valid() {
		return uploads.Record{}, uploads.ErrInvalid
	}
	var out uploads.Record
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		var err error
		out, err = scan(tx.QueryRowContext(ctx, uploadSelect, scope.OrganizationID().String(), scope.WorkspaceID().String(), id.String()))
		return err
	})
	if err != nil {
		return uploads.Record{}, normalize(err)
	}
	if out.Validate(scope, r.maxBytes) != nil {
		return uploads.Record{}, uploads.ErrInvalid
	}
	return out, nil
}

func quarantinedEvent(scope tenancy.Scope, record uploads.Record, mutation uploads.Mutation) (eventbus.Event, error) {
	typ, err := eventbus.ParseEventType("security.upload.quarantined.v1")
	if err != nil {
		return eventbus.Event{}, err
	}
	at, err := domain.NewUTCInstant(mutation.OccurredAt)
	if err != nil {
		return eventbus.Event{}, err
	}
	payload, err := json.Marshal(struct {
		EventID  string  `json:"event_id"`
		ObjectID string  `json:"object_id"`
		Reason   string  `json:"reason"`
		Scanner  *string `json:"scanner"`
	}{EventID: mutation.EventID, ObjectID: record.ID.String(), Reason: "untrusted_upload_received", Scanner: nil})
	if err != nil {
		return eventbus.Event{}, err
	}
	event := eventbus.Event{ID: mutation.EventID, Type: typ, OccurredAt: at, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), EntityType: "upload", EntityID: record.ID.String(), Source: mutation.Source, CorrelationID: mutation.CorrelationID, CausationID: mutation.CausationID, ActorID: mutation.ActorID, TraceID: mutation.TraceID, Data: payload}
	if err := event.Validate(); err != nil {
		return eventbus.Event{}, fmt.Errorf("upload repository: event: %w", err)
	}
	return event, nil
}

func uploadSelectColumns() string {
	return `id,organization_id,workspace_id,original_filename,COALESCE(declared_media_type,''),declared_size_bytes,state,COALESCE(quarantine_object_key,''),COALESCE(released_object_key,''),COALESCE(content_size_bytes,0),COALESCE(content_sha256,''),COALESCE(security_evidence_id,''),version,received_at,quarantined_at,released_at,updated_at`
}

type rowScanner interface{ Scan(...any) error }

func scan(row rowScanner) (uploads.Record, error) {
	var out uploads.Record
	var id, org, ws, state string
	var quarantined, released sql.NullTime
	if err := row.Scan(&id, &org, &ws, &out.Metadata.OriginalFilename, &out.Metadata.DeclaredMediaType, &out.Metadata.DeclaredSizeBytes, &state, &out.QuarantineObjectKey, &out.ReleasedObjectKey, &out.ContentSizeBytes, &out.ContentSHA256, &out.SecurityEvidenceID, &out.Version, &out.ReceivedAt, &quarantined, &released, &out.UpdatedAt); err != nil {
		return uploads.Record{}, err
	}
	out.ID = uploads.ID(id)
	parsedOrg, err := tenancy.ParseOrganizationID(org)
	if err != nil {
		return uploads.Record{}, uploads.ErrInvalid
	}
	out.OrganizationID = parsedOrg
	parsedWS, err := tenancy.ParseWorkspaceID(ws)
	if err != nil {
		return uploads.Record{}, uploads.ErrInvalid
	}
	out.WorkspaceID = parsedWS
	out.State = uploads.State(state)
	if quarantined.Valid {
		t := quarantined.Time.UTC()
		out.QuarantinedAt = &t
	}
	if released.Valid {
		t := released.Time.UTC()
		out.ReleasedAt = &t
	}
	out.ReceivedAt = out.ReceivedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	return out, nil
}

func classifyMiss(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, id uploads.ID) error {
	var state string
	err := tx.QueryRowContext(ctx, `SELECT state FROM uploads WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id.String()).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return uploads.ErrNotFound
	}
	if err != nil {
		return err
	}
	return uploads.ErrConflict
}
func (r *Repository) validate(ctx context.Context, scope tenancy.Scope) error {
	if ctx == nil || r == nil || r.db == nil || !scope.Valid() {
		return uploads.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
func (r *Repository) read(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	return r.tx(ctx, scope, &sql.TxOptions{ReadOnly: true}, fn)
}
func (r *Repository) write(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	return r.tx(ctx, scope, nil, fn)
}
func (r *Repository) tx(ctx context.Context, scope tenancy.Scope, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func normalize(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return uploads.ErrNotFound
	}
	if errors.Is(err, uploads.ErrInvalid) || errors.Is(err, uploads.ErrConflict) || errors.Is(err, uploads.ErrNotFound) {
		return err
	}
	return fmt.Errorf("upload repository: %w", err)
}
