package api

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/core/userprofile"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/securitysettingsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/tenancyrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/userprofilerepo"
	"github.com/torgnexa/torgnexa/internal/platform/securitysettings"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func auditFixtureRequest(ctx context.Context, scope tenancy.Scope, method, path, key, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", key)
	identity := userprofile.Identity{SubjectRef: strings.Repeat("a", 64), Email: "synthetic@example.test", Username: "synthetic"}
	ctx = context.WithValue(ctx, requestScopeKey{}, scope)
	ctx = context.WithValue(ctx, requestIdentityKey{}, Principal{Issuer: "https://id.example.test", Subject: "synthetic-admin", SubjectRef: identity.SubjectRef, Profile: identity})
	return r.WithContext(ctx)
}

// The trigger fails the actual INSERT, after the business write has executed.
func auditFailureSwitch(t *testing.T, ctx context.Context, admin *sql.DB, scope tenancy.Scope) func(bool) {
	t.Helper()
	name := "audit_fail_" + strings.ReplaceAll(scope.WorkspaceID().String(), "-", "")
	if _, err := admin.ExecContext(ctx, `CREATE OR REPLACE FUNCTION audit_fixture_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic audit write failure'; END $$`); err != nil {
		t.Fatal(err)
	}
	query := fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON audit_records FOR EACH ROW WHEN (NEW.workspace_id='%s') EXECUTE FUNCTION audit_fixture_failure()`, name, scope.WorkspaceID().String())
	if _, err := admin.ExecContext(ctx, query); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS `+name+` ON audit_records`) })
	return func(enabled bool) {
		verb := "DISABLE"
		if enabled {
			verb = "ENABLE"
		}
		if _, err := admin.ExecContext(ctx, `ALTER TABLE audit_records `+verb+` TRIGGER `+name); err != nil {
			t.Fatal(err)
		}
	}
}
func auditCount(t *testing.T, ctx context.Context, admin *sql.DB, scope tenancy.Scope) int {
	t.Helper()
	var count int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM audit_records WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
func postgresAuditor(t *testing.T, db *sql.DB) *audit.Service {
	t.Helper()
	repo, err := auditrepo.New(db)
	if err != nil {
		t.Fatal(err)
	}
	service, err := audit.NewService(repo)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestA06PostgresMemberAuditRollbackAndReplay(t *testing.T) {
	for _, status := range []string{"active", "disabled"} {
		t.Run(status, func(t *testing.T) {
			ctx, db, admin, scope := auditPostgres(t)
			repo, _ := tenancyrepo.New(db)
			member, err := repo.InviteMember(ctx, scope, tenancyrepo.Member{ID: auditFixtureID(), Email: "member@example.test", DisplayName: "Synthetic", Role: "viewer", InvitationKey: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			api := memberSettingsAPI{repository: repo, audit: postgresAuditor(t, db)}
			fail := auditFailureSwitch(t, ctx, admin, scope)
			body := fmt.Sprintf(`{"role":"operator","status":%q,"expected_version":1}`, status)
			send := func() int {
				w := httptest.NewRecorder()
				api.update(w, auditFixtureRequest(ctx, scope, "PATCH", MembersSettingsPath+"/"+member.ID, "member-change", body))
				return w.Code
			}
			if code := send(); code != 500 {
				t.Fatalf("injected failure status=%d", code)
			}
			saved, err := repo.GetMember(ctx, scope, member.ID)
			if err != nil || saved.Role != "viewer" || saved.Version != 1 {
				t.Fatalf("mutation escaped rollback: %+v %v", saved, err)
			}
			if auditCount(t, ctx, admin, scope) != 0 {
				t.Fatal("audit survived rollback")
			}
			fail(false)
			for range 2 {
				if code := send(); code != 200 {
					t.Fatalf("retry status=%d", code)
				}
			}
			saved, err = repo.GetMember(ctx, scope, member.ID)
			if err != nil || saved.Role != "operator" || saved.Status != status || saved.Version != 2 {
				t.Fatal("retry state invalid", err)
			}
			if auditCount(t, ctx, admin, scope) != 1 {
				t.Fatal("retry duplicated audit")
			}
		})
	}
}

func TestA06PostgresProfileAuditRollbackAndReplay(t *testing.T) {
	ctx, db, admin, scope := auditPostgres(t)
	repo, _ := userprofilerepo.New(db)
	identity := userprofile.Identity{SubjectRef: strings.Repeat("a", 64), Email: "synthetic@example.test", Username: "synthetic"}
	if _, err := repo.Ensure(ctx, scope, identity); err != nil {
		t.Fatal(err)
	}
	api := profileAPI{profiles: repo, audit: postgresAuditor(t, db)}
	fail := auditFailureSwitch(t, ctx, admin, scope)
	send := func() int {
		w := httptest.NewRecorder()
		api.update(w, auditFixtureRequest(ctx, scope, "PATCH", CurrentUserProfilePath, "profile-change", `{"given_name":"Changed","version":1}`))
		return w.Code
	}
	if code := send(); code < 500 {
		t.Fatalf("injected failure status=%d", code)
	}
	profile, err := repo.Get(ctx, scope, identity.SubjectRef)
	if err != nil || profile.GivenName != "" || profile.Version != 1 {
		t.Fatal("profile escaped rollback", err)
	}
	fail(false)
	for range 2 {
		if code := send(); code != 200 {
			t.Fatalf("retry status=%d", code)
		}
	}
	profile, err = repo.Get(ctx, scope, identity.SubjectRef)
	if err != nil || profile.GivenName != "Changed" || profile.Version != 2 {
		t.Fatal("profile not updated", err)
	}
	if auditCount(t, ctx, admin, scope) != 1 {
		t.Fatal("profile audit missing or duplicated")
	}
}

func TestA06PostgresIdentityProviderAuditRollbackAndReplay(t *testing.T) {
	ctx, db, admin, scope := auditPostgres(t)
	repo, _ := securitysettingsrepo.New(db)
	policy, _ := newIdentityFixturePolicy()
	api := identityProviderSettingsAPI{store: repo, audit: postgresAuditor(t, db), policy: policy, validator: idpValidatorStub{}}
	fail := auditFailureSwitch(t, ctx, admin, scope)
	send := func(action, key, body string) int {
		w := httptest.NewRecorder()
		path := identityProviderSettingsPrefix + "corporate"
		method := "PUT"
		if action != "" {
			path += ":" + action
			method = "POST"
		}
		r := auditFixtureRequest(ctx, scope, method, path, key, body)
		if action == "" {
			api.save(w, r)
		} else {
			api.action(w, r)
		}
		return w.Code
	}
	draft := `{"protocol":"oidc","display_name":"Synthetic","issuer_url":"https://id.example.test","client_id":"client","callback_url":"https://console.example.test/oidc/callback","expected_version":0}`
	if code := send("", "draft", draft); code != 500 {
		t.Fatalf("draft failure=%d", code)
	}
	var rows int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM settings_identity_providers WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&rows); err != nil || rows != 0 {
		t.Fatal("draft escaped rollback", err)
	}
	fail(false)
	if code := send("", "draft", draft); code != 200 {
		t.Fatalf("draft=%d", code)
	}
	if code := send("validate", "validate", `{"expected_version":1}`); code != 200 {
		t.Fatalf("validate=%d", code)
	}
	for _, action := range []string{"activate", "disable"} {
		before, err := repo.Provider(ctx, scope, "corporate")
		if err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf(`{"expected_version":%d}`, before.Version)
		fail(true)
		if code := send(action, action, body); code != 500 {
			t.Fatalf("%s failure=%d", action, code)
		}
		after, err := repo.Provider(ctx, scope, "corporate")
		if err != nil || after.Version != before.Version || after.Enabled != before.Enabled {
			t.Fatal("provider escaped rollback", err)
		}
		fail(false)
		for range 2 {
			if code := send(action, action, body); code != 200 {
				t.Fatalf("%s retry=%d", action, code)
			}
		}
		after, err = repo.Provider(ctx, scope, "corporate")
		if err != nil || after.Version != before.Version+1 || after.Enabled != (action == "activate") {
			t.Fatal("provider retry state", err)
		}
	}
	if auditCount(t, ctx, admin, scope) != 4 {
		t.Fatal("identity provider audit missing or duplicated")
	}
}

func newIdentityFixturePolicy() (*securitysettings.ProviderURLPolicy, error) {
	return securitysettings.NewProviderURLPolicy([]string{"id.example.test"}, []string{"https://console.example.test"}, idpResolver{"id.example.test": {net.ParseIP("8.8.8.8")}})
}

func TestA06PostgresWorkspaceAuditRollback(t *testing.T) {
	ctx, db, admin, scope := auditPostgres(t)
	repo, _ := tenancyrepo.New(db)
	api := workspaceSettingsAPI{repository: repo, audit: postgresAuditor(t, db)}
	fail := auditFailureSwitch(t, ctx, admin, scope)
	send := func() int {
		w := httptest.NewRecorder()
		api.update(w, auditFixtureRequest(ctx, scope, "PUT", WorkspaceSettingsPath, "workspace-change", `{"organization_name":"Changed organization","workspace_name":"Changed workspace","organization_version":1,"workspace_version":1}`))
		return w.Code
	}
	if code := send(); code != 500 {
		t.Fatalf("failure status=%d", code)
	}
	organization, err := repo.Organization(ctx, scope)
	if err != nil || organization.Version != 1 {
		t.Fatal("organization escaped rollback", err)
	}
	workspace, err := repo.Workspace(ctx, scope)
	if err != nil || workspace.Version != 1 {
		t.Fatal("workspace escaped rollback", err)
	}
	fail(false)
	if code := send(); code != 200 {
		t.Fatalf("retry status=%d", code)
	}
	if auditCount(t, ctx, admin, scope) != 1 {
		t.Fatal("workspace audit missing")
	}
}
