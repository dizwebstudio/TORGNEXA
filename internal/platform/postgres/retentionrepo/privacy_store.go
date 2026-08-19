package retentionrepo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/retention"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

type PrivacyStore struct {
	db      *sql.DB
	secrets secrets.SecretProvider
}

func NewPrivacyStore(db *sql.DB, secretStore secrets.SecretProvider) (*PrivacyStore, error) {
	if db == nil || secretStore == nil {
		return nil, retention.ErrInvalid
	}
	return &PrivacyStore{db: db, secrets: secretStore}, nil
}
func (*PrivacyStore) Name() string                { return "postgres-workspace-members" }
func (*PrivacyStore) Class() retention.StoreClass { return retention.StoreAuthoritative }
func (*PrivacyStore) Supports(action retention.Action) bool {
	switch action {
	case retention.ActionExport, retention.ActionCorrect, retention.ActionDelete, retention.ActionAnonymize, retention.ActionRestrict, retention.ActionTenantDelete:
		return true
	}
	return false
}

type privacyMember struct {
	ID, Email, DisplayName, OIDCSubject, Role, Status string
	Version                                           int64
}

func (s *PrivacyStore) Step(ctx context.Context, scope tenancy.Scope, step retention.Step) (retention.StepResult, error) {
	if ctx == nil || s == nil || s.db == nil || s.secrets == nil || !scope.Valid() || step.Limit < 1 {
		return retention.StepResult{}, retention.ErrInvalid
	}
	if step.Action == retention.ActionTenantDelete {
		return retention.StepResult{}, retention.ErrManualReview
	}
	member, found, err := s.member(ctx, scope, step.Subject)
	if err != nil {
		return retention.StepResult{}, err
	}
	if !found {
		return retention.StepResult{Processed: 0, Digest: retention.EvidenceDigest(step.JobID, s.Name(), string(step.Action), "not-found"), Done: true}, nil
	}
	switch step.Action {
	case retention.ActionExport:
		raw, _ := json.Marshal(map[string]any{"id": member.ID, "email": member.Email, "display_name": member.DisplayName, "oidc_subject": member.OIDCSubject, "role": member.Role, "status": member.Status})
		metadata, err := s.secrets.Create(ctx, scope, secrets.ClassPrivacyExport, raw)
		clear(raw)
		if err != nil {
			return retention.StepResult{}, err
		}
		return retention.StepResult{Processed: 1, Digest: retention.EvidenceDigest(step.JobID, s.Name(), string(step.Action), member.ID), ArtifactRef: metadata.Reference.String(), Done: true}, nil
	case retention.ActionCorrect:
		ref, err := secrets.ParseReference(step.CorrectionArtifactRef)
		if err != nil {
			return retention.StepResult{}, retention.ErrInvalid
		}
		var patch struct{ Email, DisplayName string }
		if err = s.secrets.Use(ctx, scope, ref, func(raw []byte) error { return json.Unmarshal(raw, &patch) }); err != nil {
			return retention.StepResult{}, err
		}
		if patch.Email == "" {
			patch.Email = member.Email
		}
		patch.Email = strings.ToLower(strings.TrimSpace(patch.Email))
		patch.DisplayName = strings.TrimSpace(patch.DisplayName)
		if patch.Email == "" || len(patch.Email) > 254 || len(patch.DisplayName) > 160 {
			return retention.StepResult{}, retention.ErrInvalid
		}
		if err = s.update(ctx, scope, member.ID, `email=$4,display_name=$5`, member.Version, patch.Email, patch.DisplayName); err != nil {
			return retention.StepResult{}, err
		}
	case retention.ActionRestrict:
		if err = s.update(ctx, scope, member.ID, `status='disabled',oidc_subject=NULL`, member.Version); err != nil {
			return retention.StepResult{}, err
		}
	case retention.ActionDelete, retention.ActionAnonymize:
		digest := sha256.Sum256([]byte(scope.OrganizationID().String() + "|" + scope.WorkspaceID().String() + "|" + member.ID))
		anon := "deleted+" + hex.EncodeToString(digest[:8]) + "@invalid.torgnexa"
		if err = s.update(ctx, scope, member.ID, `email=$4,display_name='',oidc_subject=NULL,status='disabled'`, member.Version, anon); err != nil {
			return retention.StepResult{}, err
		}
	default:
		return retention.StepResult{}, retention.ErrUnsupported
	}
	return retention.StepResult{Processed: 1, Digest: retention.EvidenceDigest(step.JobID, s.Name(), string(step.Action), member.ID), Done: true}, nil
}

func (s *PrivacyStore) member(ctx context.Context, scope tenancy.Scope, subject retention.SubjectRef) (privacyMember, bool, error) {
	if !subject.Valid() {
		return privacyMember{}, false, retention.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return privacyMember{}, false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, applyScopeStatement, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return privacyMember{}, false, err
	}
	var m privacyMember
	var oidc sql.NullString
	query := `SELECT id,email,display_name,oidc_subject,role_code,status,version FROM workspace_members WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`
	if subject.Kind == "oidc_subject" {
		query = `SELECT id,email,display_name,oidc_subject,role_code,status,version FROM workspace_members WHERE organization_id=$1 AND workspace_id=$2 AND oidc_subject=$3`
	}
	err = tx.QueryRowContext(ctx, query, scope.OrganizationID().String(), scope.WorkspaceID().String(), subject.OpaqueID).Scan(&m.ID, &m.Email, &m.DisplayName, &oidc, &m.Role, &m.Status, &m.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return privacyMember{}, false, nil
	}
	if err != nil {
		return privacyMember{}, false, err
	}
	m.OIDCSubject = oidc.String
	return m, true, nil
}

func (s *PrivacyStore) update(ctx context.Context, scope tenancy.Scope, id, setClause string, version int64, args ...any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, applyScopeStatement, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.privacy_execution','on',true)`); err != nil {
		return err
	}
	base := []any{scope.OrganizationID().String(), scope.WorkspaceID().String(), id}
	base = append(base, args...)
	expectedIndex := len(base) + 1
	base = append(base, version)
	statement := `UPDATE workspace_members SET ` + setClause + `,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$` + fmtInt(expectedIndex)
	res, err := tx.ExecContext(ctx, statement, base...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return retention.ErrConflict
	}
	return tx.Commit()
}

func fmtInt(v int) string { return strconv.Itoa(v) }

var _ retention.Store = (*PrivacyStore)(nil)
