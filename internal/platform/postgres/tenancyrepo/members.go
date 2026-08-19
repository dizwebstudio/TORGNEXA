package tenancyrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var ErrMemberConflict = errors.New("workspace member conflict")
var ErrLastAdministrator = errors.New("last active workspace administrator")

type Member struct {
	ID, Email, DisplayName, OIDCSubject, Role, Status, InvitationKey string
	Version                                                          int64
	InvitedAt, UpdatedAt                                             time.Time
}

func (r *Repository) ListMembers(ctx context.Context, scope tenancy.Scope, after string, limit int) ([]Member, error) {
	if limit < 1 || limit > 201 || len(after) > 128 {
		return nil, ErrMemberConflict
	}
	var out []Member
	err := r.withScope(ctx, scope, func(q queryer) error {
		rows, err := q.QueryContext(ctx, `SELECT id,email,display_name,COALESCE(oidc_subject,''),role_code,status,invitation_key,version,invited_at,updated_at FROM workspace_members WHERE organization_id=$1 AND workspace_id=$2 AND ($3='' OR id>$3) ORDER BY id LIMIT $4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), after, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m Member
			if err := rows.Scan(&m.ID, &m.Email, &m.DisplayName, &m.OIDCSubject, &m.Role, &m.Status, &m.InvitationKey, &m.Version, &m.InvitedAt, &m.UpdatedAt); err != nil {
				return err
			}
			m.InvitedAt, m.UpdatedAt = m.InvitedAt.UTC(), m.UpdatedAt.UTC()
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

func (r *Repository) InviteMember(ctx context.Context, scope tenancy.Scope, member Member) (Member, error) {
	member.Email = strings.ToLower(strings.TrimSpace(member.Email))
	member.DisplayName = strings.TrimSpace(member.DisplayName)
	if member.ID == "" || member.InvitationKey == "" || !validMemberRole(member.Role) || !strings.Contains(member.Email, "@") {
		return Member{}, ErrMemberConflict
	}
	requestedEmail, requestedRole := member.Email, member.Role
	err := r.withWriteScope(ctx, scope, func(q queryer) error {
		row := q.QueryRowContext(ctx, `INSERT INTO workspace_members(id,organization_id,workspace_id,email,display_name,role_code,status,invitation_key) VALUES($1,$2,$3,$4,$5,$6,'invited',$7) ON CONFLICT(organization_id,workspace_id,invitation_key) DO UPDATE SET invitation_key=workspace_members.invitation_key RETURNING id,email,display_name,COALESCE(oidc_subject,''),role_code,status,invitation_key,version,invited_at,updated_at`, member.ID, scope.OrganizationID().String(), scope.WorkspaceID().String(), member.Email, member.DisplayName, member.Role, member.InvitationKey)
		return scanMember(row, &member)
	})
	if err != nil {
		return Member{}, fmt.Errorf("invite member: %w", err)
	}
	if member.Email != requestedEmail || member.Role != requestedRole {
		return Member{}, ErrMemberConflict
	}
	return member, nil
}

func (r *Repository) UpdateMember(ctx context.Context, scope tenancy.Scope, id, role, status, mutationKey string, expected int64) (Member, error) {
	if id == "" || mutationKey == "" || expected < 1 || !validMemberRole(role) || (status != "invited" && status != "active" && status != "disabled") {
		return Member{}, ErrMemberConflict
	}
	var member Member
	err := r.withWriteScope(ctx, scope, func(q queryer) error {
		var oldRole, oldStatus string
		if err := q.QueryRowContext(ctx, `SELECT role_code,status FROM workspace_members WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id).Scan(&oldRole, &oldStatus); err != nil {
			return err
		}
		if oldRole == "admin" && oldStatus == "active" && (role != "admin" || status != "active") {
			var count int
			if err := q.QueryRowContext(ctx, `SELECT count(*) FROM workspace_members WHERE organization_id=$1 AND workspace_id=$2 AND role_code='admin' AND status='active'`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&count); err != nil {
				return err
			}
			if count <= 1 {
				return ErrLastAdministrator
			}
		}
		return scanMember(q.QueryRowContext(ctx, `WITH existing AS (SELECT id,email,display_name,COALESCE(oidc_subject,'') oidc_subject,role_code,status,invitation_key,version,invited_at,updated_at FROM workspace_members WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND last_mutation_key=$6), updated AS (UPDATE workspace_members SET role_code=$4,status=$5,last_mutation_key=$6,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$7 AND NOT EXISTS(SELECT 1 FROM existing) RETURNING id,email,display_name,COALESCE(oidc_subject,'') oidc_subject,role_code,status,invitation_key,version,invited_at,updated_at) SELECT * FROM existing UNION ALL SELECT * FROM updated`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id, role, status, mutationKey, expected), &member)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Member{}, ErrMemberConflict
	}
	return member, err
}

func scanMember(row rowScanner, m *Member) error {
	return row.Scan(&m.ID, &m.Email, &m.DisplayName, &m.OIDCSubject, &m.Role, &m.Status, &m.InvitationKey, &m.Version, &m.InvitedAt, &m.UpdatedAt)
}
func validMemberRole(v string) bool {
	return v == "admin" || v == "manager" || v == "operator" || v == "viewer"
}
