package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	txboundary "github.com/torgnexa/torgnexa/internal/platform/postgres/database"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/syncrepo"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

// Provider choices are fixture data; the production transaction code stays
// independent of provider names and uses the reviewed manifests.
type connectorAuditProviders struct {
	ProbeConnector string `json:"probe_connector"`
	ProbeFamily    string `json:"probe_family"`
	OAuthConnector string `json:"oauth_connector"`
	OAuthFamily    string `json:"oauth_family"`
}

func connectorAuditFixture(t *testing.T) connectorAuditProviders {
	t.Helper()
	var fixture connectorAuditProviders
	raw, err := os.ReadFile("testdata/connector-audit.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestConnectorAuditPostgresCredentialsRollback(t *testing.T) {
	for _, stage := range []string{"audit", "revoke", "commit", "cancel"} {
		t.Run(stage, func(t *testing.T) { testConnectorCredentialRollback(t, stage) })
	}
}

func testConnectorCredentialRollback(t *testing.T, stage string) {
	t.Helper()
	ctx, db, admin, scope := auditPostgres(t)
	fixture := connectorAuditFixture(t)
	provider := webhookEncryptedSecrets(t, db)
	old, err := provider.Create(ctx, scope, secrets.ClassConnectorToken, []byte("synthetic-old-material"))
	if err != nil {
		t.Fatal(err)
	}
	id := webhookAccountFixture(t, ctx, admin, scope, fixture.ProbeConnector, fixture.ProbeFamily, old.Reference.String())
	repo, _ := connectorrepo.New(db)
	before, err := repo.AccountByID(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), id)
	if err != nil {
		t.Fatal(err)
	}
	api := connectorAccountAPI{repository: repo, secrets: provider, audit: postgresAuditor(t, db)}
	requestCtx := ctx
	var restore func()
	switch stage {
	case "audit":
		fail := auditFailureSwitch(t, ctx, admin, scope)
		restore = func() { fail(false) }
	case "revoke":
		restore = webhookFailureSwitch(t, ctx, admin, scope, "secret_references", "BEFORE UPDATE")
	case "commit":
		restore = webhookFailureSwitch(t, ctx, admin, scope, "audit_records", "deferred")
	case "cancel":
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithCancel(ctx)
		t.Cleanup(cancel)
		api.secrets = cancelAfterRevokeProvider{TransactionalProvider: provider, cancel: cancel}
		restore = func() { requestCtx, api.secrets = ctx, provider }
	}
	body := fmt.Sprintf(`{"account_id":%q,"expected_version":%d,"material_base64":%q}`, id, before.Version, base64.StdEncoding.EncodeToString([]byte("synthetic-new-material")))
	send := func() int {
		w := httptest.NewRecorder()
		api.credentials(w, auditFixtureRequest(requestCtx, scope, "POST", ConnectorCredentialsPath, "synthetic-rotation", body))
		return w.Code
	}
	if code := send(); code != 500 {
		t.Fatalf("injected audit failure status=%d", code)
	}
	saved, err := repo.AccountByID(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), id)
	if err != nil || saved.Version != before.Version || saved.SecretReference != before.SecretReference || saved.Status != before.Status {
		t.Fatal("credential binding escaped audit rollback")
	}
	metadata, err := provider.Describe(ctx, scope, old.Reference)
	if err != nil || metadata.Status != secrets.StatusActive {
		t.Fatal("old credential was revoked despite rollback")
	}
	var count int
	if err = admin.QueryRowContext(ctx, `SELECT count(*) FROM secret_references WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&count); err != nil || count != 1 || auditCount(t, ctx, admin, scope) != 0 {
		t.Fatal("failed rotation left new secret or audit evidence", err)
	}
	restore()
	if code := send(); code != 200 {
		t.Fatalf("retry status=%d", code)
	}
	saved, err = repo.AccountByID(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), id)
	if err != nil || saved.Version != before.Version+1 || saved.SecretReference == before.SecretReference || saved.Status != sdk.AccountDisabled {
		t.Fatal("retry did not bind the new credential")
	}
	metadata, err = provider.Describe(ctx, scope, old.Reference)
	if err != nil || metadata.Status != secrets.StatusRevoked || auditCount(t, ctx, admin, scope) != 1 {
		t.Fatal("successful rotation lacks revocation or unique audit", err)
	}
	if code := send(); code != 409 || auditCount(t, ctx, admin, scope) != 1 {
		t.Fatal("stale retry duplicated credential mutation or audit")
	}
}

type cancelAfterRevokeProvider struct {
	secrets.TransactionalProvider
	cancel context.CancelFunc
}

func (provider cancelAfterRevokeProvider) Revoke(ctx context.Context, scope tenancy.Scope, reference secrets.Reference) (secrets.Metadata, error) {
	metadata, err := provider.TransactionalProvider.Revoke(ctx, scope, reference)
	provider.cancel()
	return metadata, err
}

// Only the health response is mocked; account, version, history and audit use
// the production PostgreSQL repositories and authoritative transaction owner.
type connectorAuditRegistry struct{ *builtinruntime.Registry }

func (connectorAuditRegistry) Health(context.Context, sdk.Account, sdk.Runtime, func(context.Context, string) (json.RawMessage, error)) (sdk.Health, error) {
	return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: time.Now().UTC()}, nil
}

func TestConnectorAuditPostgresAccountMutations(t *testing.T) {
	fixture := connectorAuditFixture(t)
	for _, scenario := range []string{"create", "disable", "enable", "capabilities", "health"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, db, admin, scope := auditPostgres(t)
			repo, _ := connectorrepo.New(db)
			provider := webhookEncryptedSecrets(t, db)
			metadata, err := provider.Create(ctx, scope, secrets.ClassConnectorToken, []byte("synthetic-connector-token"))
			if err != nil {
				t.Fatal(err)
			}
			connector, family := fixture.OAuthConnector, fixture.OAuthFamily
			if scenario == "health" {
				connector, family = fixture.ProbeConnector, fixture.ProbeFamily
			}
			id := webhookAccountFixture(t, ctx, admin, scope, connector, family, metadata.Reference.String())
			if scenario == "enable" {
				_, err = admin.ExecContext(ctx, `UPDATE connector_accounts SET status='disabled',health_status='healthy',health_checked_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE id=$1`, id)
				if err != nil {
					t.Fatal(err)
				}
			}
			before, err := repo.AccountByID(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), id)
			if err != nil {
				t.Fatal(err)
			}
			api := connectorAccountAPI{repository: repo, audit: postgresAuditor(t, db), secrets: provider, registry: connectorAuditRegistry{builtinruntime.New()}, now: func() time.Time { return time.Now().UTC() }}
			method, path, handler := "POST", ConnectorAccountsDisablePath, api.disable
			body := fmt.Sprintf(`{"account_id":%q,"expected_version":%d}`, id, before.Version)
			key, success, replay := "synthetic-account-update", 200, 409
			switch scenario {
			case "create":
				id = auditFixtureID()
				key = id
				body = fmt.Sprintf(`{"account_id":%q,"connector_id":%q}`, id, fixture.OAuthConnector)
				path, handler, success, replay = ConnectorAccountsPath, api.create, 201, 200
			case "enable":
				path, handler = ConnectorEnablePath, api.enable
			case "health":
				path, handler = ConnectorHealthPath, api.check
			case "capabilities":
				method, path, handler = "PUT", ConnectorCapabilitiesPath, api.capabilities
				body = fmt.Sprintf(`{"account_id":%q,"expected_version":%d,"enabled":[]}`, id, before.Version)
			}
			fail := auditFailureSwitch(t, ctx, admin, scope)
			send := func() int {
				w := httptest.NewRecorder()
				handler(w, auditFixtureRequest(ctx, scope, method, path, key, body))
				if strings.Contains(w.Body.String(), "synthetic-connector-token") || strings.Contains(w.Body.String(), "synthetic audit write failure") {
					t.Fatal("secret or DB error leaked")
				}
				return w.Code
			}
			if code := send(); code != 500 {
				t.Fatalf("failure status=%d", code)
			}
			saved, err := repo.AccountByID(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), id)
			if scenario == "create" {
				if !errors.Is(err, sdk.ErrAccountNotFound) {
					t.Fatal("create escaped rollback", err)
				}
			} else if err != nil || saved.Version != before.Version || saved.Status != before.Status || saved.Health.Status != before.Health.Status {
				t.Fatal("account escaped rollback", err)
			}
			for _, table := range []string{"connector_health_history", "connector_account_capability_history"} {
				var count int
				if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&count); err != nil || count != 0 {
					t.Fatal("history escaped rollback", err)
				}
			}
			if auditCount(t, ctx, admin, scope) != 0 {
				t.Fatal("audit survived rollback")
			}
			fail(false)
			if code := send(); code != success {
				t.Fatalf("retry status=%d", code)
			}
			if code := send(); code != replay {
				t.Fatalf("replay status=%d", code)
			}
			if auditCount(t, ctx, admin, scope) != 1 {
				t.Fatal("audit missing or duplicated")
			}
			var actor, summary string
			if err := admin.QueryRowContext(ctx, `SELECT actor_id,summary::text FROM audit_records WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&actor, &summary); err != nil || actor != boundedActorRef("synthetic-admin") || strings.Contains(summary, "sec:v1:") {
				t.Fatal("audit is not minimized", err)
			}
		})
	}
}

func TestConnectorAuditPostgresConcurrentCredentialReplacement(t *testing.T) {
	fixture := connectorAuditFixture(t)
	ctx, db, admin, scope := auditPostgres(t)
	db.SetMaxOpenConns(4)
	provider := webhookEncryptedSecrets(t, db)
	old, err := provider.Create(ctx, scope, secrets.ClassConnectorToken, []byte("synthetic-old-material"))
	if err != nil {
		t.Fatal(err)
	}
	id := webhookAccountFixture(t, ctx, admin, scope, fixture.ProbeConnector, fixture.ProbeFamily, old.Reference.String())
	repo, _ := connectorrepo.New(db)
	api := connectorAccountAPI{repository: repo, secrets: provider, audit: postgresAuditor(t, db)}
	body := fmt.Sprintf(`{"account_id":%q,"expected_version":2,"material_base64":%q}`, id, base64.StdEncoding.EncodeToString([]byte("synthetic-new-material")))
	start, codes := make(chan struct{}), make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			<-start
			w := httptest.NewRecorder()
			api.credentials(w, auditFixtureRequest(ctx, scope, "POST", ConnectorCredentialsPath, "same-key", body))
			codes <- w.Code
		})
	}
	close(start)
	wg.Wait()
	close(codes)
	seen := map[int]int{}
	for code := range codes {
		seen[code]++
	}
	if seen[200] != 1 || seen[409] != 1 {
		t.Fatal("concurrent outcomes", seen)
	}
	var refs, active int
	if err := admin.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE status='active') FROM secret_references WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&refs, &active); err != nil || refs != 2 || active != 1 || auditCount(t, ctx, admin, scope) != 1 {
		t.Fatal("loser left secret or audit", err)
	}
}

func TestConnectorAuditPostgresBootstrapMutations(t *testing.T) {
	fixture := connectorAuditFixture(t)
	for _, scenario := range []string{"preview", "initial_job", "schedule"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, db, admin, scope := auditPostgres(t)
			provider := webhookEncryptedSecrets(t, db)
			material, err := provider.Create(ctx, scope, secrets.ClassConnectorToken, []byte("synthetic-bootstrap-token"))
			if err != nil {
				t.Fatal(err)
			}
			id := webhookAccountFixture(t, ctx, admin, scope, fixture.OAuthConnector, fixture.OAuthFamily, material.Reference.String())
			_, err = admin.ExecContext(ctx, `UPDATE connector_accounts SET health_status='healthy',health_checked_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE id=$1`, id)
			if err != nil {
				t.Fatal(err)
			}
			accounts, _ := connectorrepo.New(db)
			store, _ := syncrepo.New(db)
			_, err = store.CreatePolicy(ctx, scope, syncengine.PolicyCreate{ID: auditFixtureID(), ConnectorAccountID: id, EntityType: "orders", Direction: syncengine.DirectionInbound, SourceOfTruth: syncengine.SourceRemote, Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			api := connectorBootstrapAPI{accounts: accounts, store: store, audit: postgresAuditor(t, db), now: func() time.Time { return now }}
			key, method, path, handler, success, replay := auditFixtureID(), "POST", ConnectorBootstrapPreviewPath, api.preview, 201, 201
			body := fmt.Sprintf(`{"account_id":%q,"expected_version":3}`, id)
			table := "connector_bootstrap_previews"
			if scenario == "initial_job" {
				_, err = store.CreateBootstrapPreview(ctx, scope, syncengine.BootstrapPreview{ID: key, AccountID: id, AccountVersion: 3, PolicyCount: 1, ReadCount: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
				if err != nil {
					t.Fatal(err)
				}
				body = fmt.Sprintf(`{"preview_id":%q}`, key)
				key = "synthetic-job"
				path, handler, success, replay = ConnectorBootstrapStatePath, api.start, 202, 202
				table = "connector_sync_jobs"
			} else if scenario == "schedule" {
				key, method, path, handler, success, replay = "synthetic-schedule", "PUT", ConnectorSchedulePath, api.putSchedule, 200, 409
				table = "connector_sync_schedules"
				body = fmt.Sprintf(`{"account_id":%q,"account_version":3,"mode":"incremental","interval_minutes":60,"enabled":false,"expected_version":0}`, id)
			}
			fail := auditFailureSwitch(t, ctx, admin, scope)
			send := func() int {
				w := httptest.NewRecorder()
				handler(w, auditFixtureRequest(ctx, scope, method, path, key, body))
				return w.Code
			}
			if code := send(); code != 500 {
				t.Fatalf("failure status=%d", code)
			}
			var count int
			if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&count); err != nil || count != 0 {
				t.Fatal("bootstrap mutation escaped rollback", err)
			}
			if scenario == "initial_job" {
				previews, err := store.ListBootstrapPreviews(ctx, scope, 10)
				if err != nil || len(previews) != 1 || previews[0].ConsumedAt != nil {
					t.Fatal("failed dispatch consumed preview", err)
				}
			}
			fail(false)
			if code := send(); code != success {
				t.Fatalf("retry status=%d", code)
			}
			if code := send(); code != replay {
				t.Fatalf("replay status=%d", code)
			}
			if auditCount(t, ctx, admin, scope) != 1 {
				t.Fatal("bootstrap audit missing or duplicated")
			}
		})
	}
}

func TestConnectorAuditPostgresRejectsDifferentTenantAndPool(t *testing.T) {
	ctx, db, _, scope := auditPostgres(t)
	_, otherDB, _, otherScope := auditPostgres(t)
	repo, _ := connectorrepo.New(db)
	foreignRepo, _ := connectorrepo.New(otherDB)
	syncRepo, _ := syncrepo.New(db)
	err := postgresAuditor(t, db).WithinTransaction(ctx, scope, func(txCtx context.Context) error {
		_, err := repo.AccountByID(txCtx, otherScope.OrganizationID().String(), otherScope.WorkspaceID().String(), "synthetic")
		if !errors.Is(err, txboundary.ErrTransactionBoundary) {
			t.Fatal("cross-tenant account accepted", err)
		}
		_, err = foreignRepo.AccountByID(txCtx, scope.OrganizationID().String(), scope.WorkspaceID().String(), "synthetic")
		if !errors.Is(err, txboundary.ErrTransactionBoundary) {
			t.Fatal("cross-pool account accepted", err)
		}
		_, err = syncRepo.ListAccountSchedules(txCtx, otherScope, 1)
		if !errors.Is(err, txboundary.ErrTransactionBoundary) {
			t.Fatal("cross-tenant schedule accepted", err)
		}
		called := false
		err = webhookEncryptedSecrets(t, otherDB).WithinTransaction(txCtx, scope, func(context.Context) error { called = true; return nil })
		if !errors.Is(err, txboundary.ErrTransactionBoundary) || called {
			t.Fatal("foreign secret transaction ran", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
