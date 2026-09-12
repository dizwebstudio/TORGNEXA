package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/core/userprofile"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/retentionrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/securitysettingsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/tenancyrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/userprofilerepo"
	"github.com/torgnexa/torgnexa/internal/platform/retention"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
	"github.com/torgnexa/torgnexa/internal/platform/securitysettings"
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

type privacyExportSecrets struct {
	material []byte
}

func (store *privacyExportSecrets) Create(_ context.Context, scope tenancy.Scope, class secrets.Class, material []byte) (secrets.Metadata, error) {
	if !scope.Valid() || class != secrets.ClassPrivacyExport {
		return secrets.Metadata{}, secrets.ErrInvalidMaterial
	}
	store.material = append(store.material[:0], material...)
	reference := secrets.Reference("sec:v1:" + strings.Repeat("0", 32))
	return secrets.Metadata{Reference: reference, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Class: class, Status: secrets.StatusActive, CurrentVersion: 1}, nil
}

func (store *privacyExportSecrets) Use(_ context.Context, _ tenancy.Scope, _ secrets.Reference, consumer func([]byte) error) error {
	return consumer(append([]byte(nil), store.material...))
}

func (*privacyExportSecrets) Describe(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
	return secrets.Metadata{}, errors.New("unused")
}

func (*privacyExportSecrets) Rotate(context.Context, tenancy.Scope, secrets.Reference, []byte) (secrets.Metadata, error) {
	return secrets.Metadata{}, errors.New("unused")
}

func (*privacyExportSecrets) Revoke(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
	return secrets.Metadata{}, errors.New("unused")
}

func assertNoOIDCSubjectLeak(t *testing.T, payload, subject string) {
	t.Helper()
	if strings.Contains(payload, "oidc_subject") || strings.Contains(payload, subject) {
		t.Fatalf("external payload exposed internal identity reference: %s", payload)
	}
}

func TestOIDCSubjectPrivacyPostgres(t *testing.T) {
	ctx, db, admin, scope := auditPostgres(t)
	repository, err := tenancyrepo.New(db)
	if err != nil {
		t.Fatal(err)
	}
	const subject = "issuer.example.test|synthetic-private-subject"
	memberID := auditFixtureID()
	if _, err := admin.ExecContext(ctx, `INSERT INTO workspace_members(id,organization_id,workspace_id,email,display_name,oidc_subject,role_code,status,invitation_key) VALUES($1,$2,$3,'bound@example.test','Bound synthetic',$4,'viewer','active','bound-fixture')`, memberID, scope.OrganizationID().String(), scope.WorkspaceID().String(), subject); err != nil {
		t.Fatal(err)
	}
	unboundID := auditFixtureID()
	if _, err := repository.InviteMember(ctx, scope, tenancyrepo.Member{ID: unboundID, Email: "unbound@example.test", DisplayName: "Unbound synthetic", Role: "viewer", InvitationKey: "unbound-fixture"}); err != nil {
		t.Fatal(err)
	}

	memberAPI := memberSettingsAPI{repository: repository, audit: postgresAuditor(t, db)}
	listRequest := httptest.NewRequest(http.MethodGet, MembersSettingsPath+"?limit=100", nil)
	listRequest = listRequest.WithContext(context.WithValue(listRequest.Context(), requestScopeKey{}, scope))
	listResponse := httptest.NewRecorder()
	memberAPI.list(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d", listResponse.Code)
	}
	assertNoOIDCSubjectLeak(t, listResponse.Body.String(), subject)
	var page struct {
		Items []struct {
			ID            string `json:"id"`
			IdentityBound bool   `json:"identity_bound"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, member := range page.Items {
		found[member.ID] = member.IdentityBound
	}
	if !found[memberID] || found[unboundID] || len(found) != 2 {
		t.Fatalf("identity binding projection = %#v", found)
	}

	updateResponse := httptest.NewRecorder()
	memberAPI.update(updateResponse, auditFixtureRequest(ctx, scope, http.MethodPatch, MembersSettingsPath+"/"+memberID, "privacy-member-change", `{"role":"operator","status":"active","expected_version":1}`))
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updateResponse.Code, updateResponse.Body.String())
	}
	assertNoOIDCSubjectLeak(t, updateResponse.Body.String(), subject)
	if !strings.Contains(updateResponse.Body.String(), `"identity_bound":true`) {
		t.Fatalf("bound status missing from update response: %s", updateResponse.Body.String())
	}

	var auditPayloads, eventPayloads string
	if err := admin.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('actor_id',actor_id,'action',action,'resource_type',resource_type,'resource_id',resource_id,'summary',summary))::text,'[]') FROM audit_records WHERE organization_id=$1 AND workspace_id=$2`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&auditPayloads); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg(payload)::text,'[]') FROM outbox_events WHERE organization_id=$1 AND workspace_id=$2`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&eventPayloads); err != nil {
		t.Fatal(err)
	}
	assertNoOIDCSubjectLeak(t, auditPayloads, subject)
	assertNoOIDCSubjectLeak(t, eventPayloads, subject)

	exports := &privacyExportSecrets{}
	privacyStore, err := retentionrepo.NewPrivacyStore(db, exports)
	if err != nil {
		t.Fatal(err)
	}
	export := func(jobID string) map[string]any {
		t.Helper()
		result, err := privacyStore.Step(ctx, scope, retention.Step{JobID: jobID, Action: retention.ActionExport, Subject: retention.SubjectRef{Kind: "workspace_member", OpaqueID: memberID}, Limit: 1})
		if err != nil || !result.Done || result.Processed != 1 || result.ArtifactRef == "" {
			t.Fatalf("privacy export = %+v, %v", result, err)
		}
		assertNoOIDCSubjectLeak(t, string(exports.material), subject)
		var payload map[string]any
		if err := json.Unmarshal(exports.material, &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}
	if bound, ok := export("privacy-export-bound")["identity_bound"].(bool); !ok || !bound {
		t.Fatal("privacy export did not preserve minimized bound status")
	}
	if _, err := privacyStore.Step(ctx, scope, retention.Step{JobID: "privacy-delete", Action: retention.ActionDelete, Subject: retention.SubjectRef{Kind: "workspace_member", OpaqueID: memberID}, Limit: 1}); err != nil {
		t.Fatal(err)
	}
	var storedSubject sql.NullString
	var status, email string
	if err := admin.QueryRowContext(ctx, `SELECT oidc_subject,status,email FROM workspace_members WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), memberID).Scan(&storedSubject, &status, &email); err != nil {
		t.Fatal(err)
	}
	if storedSubject.Valid || status != "disabled" || !strings.HasSuffix(email, "@invalid.torgnexa") {
		t.Fatalf("privacy deletion retained identity: subject=%#v status=%q email=%q", storedSubject, status, email)
	}
	if bound, ok := export("privacy-export-deleted")["identity_bound"].(bool); !ok || bound {
		t.Fatal("post-deletion export did not report cleared binding")
	}

	for _, lifecycle := range []struct {
		name            string
		action          retention.Action
		anonymizedEmail bool
	}{
		{name: "restrict", action: retention.ActionRestrict},
		{name: "retention_anonymize", action: retention.ActionAnonymize, anonymizedEmail: true},
	} {
		t.Run(lifecycle.name, func(t *testing.T) {
			id := auditFixtureID()
			email := lifecycle.name + "@example.test"
			internalSubject := subject + "-" + lifecycle.name
			if _, err := admin.ExecContext(ctx, `INSERT INTO workspace_members(id,organization_id,workspace_id,email,display_name,oidc_subject,role_code,status,invitation_key) VALUES($1,$2,$3,$4,'Lifecycle synthetic',$5,'viewer','active',$6)`, id, scope.OrganizationID().String(), scope.WorkspaceID().String(), email, internalSubject, lifecycle.name+"-fixture"); err != nil {
				t.Fatal(err)
			}
			if _, err := privacyStore.Step(ctx, scope, retention.Step{JobID: "privacy-" + lifecycle.name, Action: lifecycle.action, Subject: retention.SubjectRef{Kind: "workspace_member", OpaqueID: id}, Limit: 1}); err != nil {
				t.Fatal(err)
			}
			var stored sql.NullString
			var storedEmail, storedStatus string
			if err := admin.QueryRowContext(ctx, `SELECT oidc_subject,email,status FROM workspace_members WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id).Scan(&stored, &storedEmail, &storedStatus); err != nil {
				t.Fatal(err)
			}
			if stored.Valid || storedStatus != "disabled" {
				t.Fatalf("%s retained identity: subject=%#v status=%q", lifecycle.action, stored, storedStatus)
			}
			if got := strings.HasSuffix(storedEmail, "@invalid.torgnexa"); got != lifecycle.anonymizedEmail {
				t.Fatalf("%s anonymized email = %t, want %t", lifecycle.action, got, lifecycle.anonymizedEmail)
			}
		})
	}
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
