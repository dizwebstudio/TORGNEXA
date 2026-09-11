package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/aiadvisory"
	"github.com/torgnexa/torgnexa/internal/platform/mcpaccounts"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/agentgovernancerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/aiadvisoryrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/mcpaccountsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/reconciliationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/syncrepo"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

func createReconciliationAuditPolicy(t *testing.T, ctx context.Context, dbScope tenancy.Scope, accountID string, policies *syncrepo.Repository) string {
	t.Helper()
	policyID := auditFixtureID()
	_, err := policies.CreatePolicy(ctx, dbScope, syncengine.PolicyCreate{ID: policyID, ConnectorAccountID: accountID, EntityType: "orders", Direction: syncengine.DirectionInbound, SourceOfTruth: syncengine.SourceRemote, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return policyID
}

func createReconciliationAuditAccount(t *testing.T, ctx context.Context, db *sql.DB, admin *sql.DB, scope tenancy.Scope, provider, family string) string {
	t.Helper()
	secretProvider := webhookEncryptedSecrets(t, db)
	metadata, err := secretProvider.Create(ctx, scope, "connector_token", []byte("synthetic-connector-credential"))
	if err != nil {
		t.Fatal(err)
	}
	return webhookAccountFixture(t, ctx, admin, scope, provider, family, metadata.Reference.String())
}

type mcpAuditErrorSpy struct {
	*mcpaccountsrepo.Repository
	err error
}

func (spy *mcpAuditErrorSpy) CreateGoverned(ctx context.Context, scope tenancy.Scope, id string, cmd mcpaccounts.CreateAccount, tokenHash []byte, expiresAt time.Time, key string, digest []byte, actor, evidenceID string) (mcpaccounts.Account, bool, error) {
	account, replayed, err := spy.Repository.CreateGoverned(ctx, scope, id, cmd, tokenHash, expiresAt, key, digest, actor, evidenceID)
	spy.err = err
	return account, replayed, err
}

type aiAuditErrorSpy struct {
	*aiadvisoryrepo.Repository
	err error
}

func (spy *aiAuditErrorSpy) CreateGoverned(ctx context.Context, scope tenancy.Scope, id string, cmd aiadvisory.CreateAccount, secretReference, key string, digest []byte, actor, evidenceID string) (aiadvisory.Account, bool, error) {
	account, replayed, err := spy.Repository.CreateGoverned(ctx, scope, id, cmd, secretReference, key, digest, actor, evidenceID)
	spy.err = err
	return account, replayed, err
}

func TestConnectorAuditPostgresManualReconciliationRollbackAndReplay(t *testing.T) {
	ctx, db, admin, scope := auditPostgres(t)
	fixture := connectorAuditFixture(t)
	accountID := createReconciliationAuditAccount(t, ctx, db, admin, scope, fixture.ProbeConnector, fixture.ProbeFamily)
	policies, _ := syncrepo.New(db)
	runs, _ := reconciliationrepo.New(db)
	policyID := createReconciliationAuditPolicy(t, ctx, scope, accountID, policies)
	auditor := postgresAuditor(t, db)
	fail := auditFailureSwitch(t, ctx, admin, scope)

	send := func() int {
		request := auditFixtureRequest(ctx, scope, http.MethodPost, "/api/v1/reconciliation/jobs", "manual-reconciliation", `{"policy_id":"`+policyID+`"}`)
		response := httptest.NewRecorder()
		createReconciliationJob(response, request, policies, runs, auditor, nil)
		return response.Code
	}
	if code := send(); code != http.StatusInternalServerError {
		t.Fatalf("audit failure status=%d", code)
	}
	assertReconciliationEvidenceCounts(t, ctx, admin, scope, 0, 0)
	fail(false)
	if code := send(); code != http.StatusAccepted {
		t.Fatalf("retry status=%d", code)
	}
	if code := send(); code != http.StatusAccepted {
		t.Fatalf("replay status=%d", code)
	}
	assertReconciliationEvidenceCounts(t, ctx, admin, scope, 1, 1)
}

func TestConnectorAuditPostgresAccountSyncRollbackAndReplay(t *testing.T) {
	ctx, db, admin, scope := auditPostgres(t)
	fixture := connectorAuditFixture(t)
	accountID := createReconciliationAuditAccount(t, ctx, db, admin, scope, fixture.ProbeConnector, fixture.ProbeFamily)
	policies, _ := syncrepo.New(db)
	runs, _ := reconciliationrepo.New(db)
	createReconciliationAuditPolicy(t, ctx, scope, accountID, policies)
	service := connectorManualSync{policies: policies, runs: runs, audit: postgresAuditor(t, db)}
	fail := auditFailureSwitch(t, ctx, admin, scope)
	_, err := service.Start(ctx, scope, accountID, "synthetic-admin", "account-sync", time.Now().UTC())
	if !errors.Is(err, errSettingsAudit) {
		t.Fatalf("audit failure error=%v", err)
	}
	assertReconciliationEvidenceCounts(t, ctx, admin, scope, 0, 0)
	fail(false)
	for attempt := 0; attempt < 2; attempt++ {
		count, err := service.Start(ctx, scope, accountID, "synthetic-admin", "account-sync", time.Now().UTC())
		if err != nil || count != 1 {
			t.Fatalf("attempt=%d count=%d error=%v", attempt, count, err)
		}
	}
	assertReconciliationEvidenceCounts(t, ctx, admin, scope, 1, 1)
}

func TestConnectorAuditPostgresMCPMutationRollback(t *testing.T) {
	ctx, db, admin, scope := auditPostgres(t)
	repository, _ := mcpaccountsrepo.New(db)
	spy := &mcpAuditErrorSpy{Repository: repository}
	api := mcpAccountsAPI{repository: spy, audit: postgresAuditor(t, db)}
	fail := auditFailureSwitch(t, ctx, admin, scope)
	body := `{"label":"Automation","agent_id":"agent-1","model_id":"model-1","integration_id":"integration-1","permissions":["commerce.products.read"]}`
	send := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		api.create(response, auditFixtureRequest(ctx, scope, http.MethodPost, MCPAccountsPath, "mcp-create", body))
		return response
	}
	if response := send(); response.Code != http.StatusInternalServerError {
		t.Fatalf("audit failure status=%d body=%s repository_error=%v", response.Code, response.Body.String(), spy.err)
	}
	assertTableCount(t, ctx, admin, scope, "mcp_client_accounts", 0)
	assertTableCount(t, ctx, admin, scope, "security_evidence", 0)
	fail(false)
	first, replay := send(), send()
	if first.Code != http.StatusCreated || replay.Code != http.StatusCreated || !strings.Contains(replay.Body.String(), `"replayed":true`) || strings.Contains(replay.Body.String(), `"token":"`) {
		t.Fatalf("first=%d replay=%d body=%s", first.Code, replay.Code, replay.Body.String())
	}
	assertTableCount(t, ctx, admin, scope, "mcp_client_accounts", 1)
	assertTableCount(t, ctx, admin, scope, "security_evidence", 1)
	if auditCount(t, ctx, admin, scope) != 1 {
		t.Fatal("MCP replay duplicated authoritative audit")
	}
}

func TestConnectorAuditPostgresAIProviderMutationRollback(t *testing.T) {
	ctx, db, admin, scope := auditPostgres(t)
	repository, _ := aiadvisoryrepo.New(db)
	spy := &aiAuditErrorSpy{Repository: repository}
	api := aiAdvisoryAPI{repository: spy, secrets: webhookEncryptedSecrets(t, db), audit: postgresAuditor(t, db)}
	fail := auditFailureSwitch(t, ctx, admin, scope)
	body := `{"provider":"openai-compatible","label":"Synthetic","model":"synthetic-model","credential":"synthetic-provider-credential"}`
	send := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		api.create(response, auditFixtureRequest(ctx, scope, http.MethodPost, AIProviderAccountsPath, "ai-provider-create", body))
		return response
	}
	if response := send(); response.Code != http.StatusInternalServerError {
		t.Fatalf("audit failure status=%d body=%s repository_error=%v", response.Code, response.Body.String(), spy.err)
	}
	for _, table := range []string{"ai_provider_accounts", "secret_references", "security_evidence"} {
		assertTableCount(t, ctx, admin, scope, table, 0)
	}
	fail(false)
	if response := send(); response.Code != http.StatusCreated {
		t.Fatalf("retry status=%d body=%s", response.Code, response.Body.String())
	}
	if response := send(); response.Code != http.StatusCreated {
		t.Fatalf("replay status=%d body=%s", response.Code, response.Body.String())
	}
	for _, table := range []string{"ai_provider_accounts", "secret_references", "security_evidence"} {
		assertTableCount(t, ctx, admin, scope, table, 1)
	}
	if auditCount(t, ctx, admin, scope) != 1 {
		t.Fatal("AI provider replay duplicated authoritative audit")
	}
}

func TestConnectorAuditPostgresMCPGovernanceRollbackAndReplay(t *testing.T) {
	ctx, db, admin, scope := auditPostgres(t)
	accounts, _ := mcpaccountsrepo.New(db)
	governance, _ := agentgovernancerepo.New(db)
	accountID := auditFixtureID()
	tokenHash := sha256.Sum256([]byte(auditFixtureID()))
	_, err := accounts.Create(ctx, scope, accountID, mcpaccounts.CreateAccount{Label: "Synthetic", AgentID: "agent-1", ModelID: "model-1", IntegrationID: "integration-1", Permissions: []string{"commerce.products.read"}}, tokenHash[:], time.Now().UTC().Add(24*time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	auditor := postgresAuditor(t, db)
	routes := newMCPAgentPolicyRoutes(accounts, governance, governance, auditor)
	var installRoute, killRoute ProtectedRoute
	for _, route := range routes {
		if route.Method == http.MethodPost && route.Path == mcpAccountsPrefix {
			installRoute = route
		}
		if route.Method == http.MethodPost && route.Path == MCPAgentKillSwitchPath {
			killRoute = route
		}
	}
	fail := auditFailureSwitch(t, ctx, admin, scope)
	send := func(route ProtectedRoute, path, key, body string) int {
		response := httptest.NewRecorder()
		route.Handler.ServeHTTP(response, auditFixtureRequest(ctx, scope, http.MethodPost, path, key, body))
		return response.Code
	}
	policyPath := MCPAccountsPath + "/" + accountID + ":install-policy"
	if code := send(installRoute, policyPath, "mcp-policy", `{}`); code != http.StatusInternalServerError {
		t.Fatalf("policy audit failure status=%d", code)
	}
	assertGovernanceCounts(t, ctx, admin, scope, 0, 0, 0)
	fail(false)
	for attempt := 0; attempt < 2; attempt++ {
		if code := send(installRoute, policyPath, "mcp-policy", `{}`); code != http.StatusOK {
			t.Fatalf("policy attempt=%d status=%d", attempt, code)
		}
	}
	assertGovernanceCounts(t, ctx, admin, scope, 1, 0, 1)
	if code := send(installRoute, policyPath, "mcp-policy", `{"price_change_max_calls":1}`); code != http.StatusConflict {
		t.Fatalf("policy idempotency conflict status=%d", code)
	}
	assertGovernanceCounts(t, ctx, admin, scope, 1, 0, 1)
	fail(true)
	if code := send(killRoute, MCPAgentKillSwitchPath, "mcp-kill", `{"disabled":true,"reason":"synthetic incident"}`); code != http.StatusInternalServerError {
		t.Fatalf("kill-switch audit failure status=%d", code)
	}
	assertGovernanceCounts(t, ctx, admin, scope, 1, 0, 1)
	fail(false)
	for attempt := 0; attempt < 2; attempt++ {
		if code := send(killRoute, MCPAgentKillSwitchPath, "mcp-kill", `{"disabled":true,"reason":"synthetic incident"}`); code != http.StatusOK {
			t.Fatalf("kill-switch attempt=%d status=%d", attempt, code)
		}
	}
	assertGovernanceCounts(t, ctx, admin, scope, 1, 1, 2)
	if code := send(killRoute, MCPAgentKillSwitchPath, "mcp-kill", `{"disabled":false,"reason":"different request"}`); code != http.StatusConflict {
		t.Fatalf("kill-switch idempotency conflict status=%d", code)
	}
	assertGovernanceCounts(t, ctx, admin, scope, 1, 1, 2)
}

func assertReconciliationEvidenceCounts(t *testing.T, ctx context.Context, admin *sql.DB, scope tenancy.Scope, runs, records int) {
	t.Helper()
	var runCount, auditRecords int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM reconciliation_runs WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM audit_records WHERE workspace_id=$1 AND action IN ('reconciliation.run.queued','connector.account.sync_queued')`, scope.WorkspaceID().String()).Scan(&auditRecords); err != nil {
		t.Fatal(err)
	}
	if runCount != runs || auditRecords != records {
		t.Fatalf("runs=%d audit=%d want=%d/%d", runCount, auditRecords, runs, records)
	}
}

func assertTableCount(t *testing.T, ctx context.Context, admin *sql.DB, scope tenancy.Scope, table string, want int) {
	t.Helper()
	var query string
	switch table {
	case "ai_provider_accounts":
		query = `SELECT count(*) FROM ai_provider_accounts WHERE workspace_id=$1`
	case "mcp_client_accounts":
		query = `SELECT count(*) FROM mcp_client_accounts WHERE workspace_id=$1`
	case "secret_references":
		query = `SELECT count(*) FROM secret_references WHERE workspace_id=$1`
	case "security_evidence":
		query = `SELECT count(*) FROM security_evidence WHERE workspace_id=$1`
	default:
		t.Fatalf("unsupported table %q", table)
	}
	var count int
	if err := admin.QueryRowContext(ctx, query, scope.WorkspaceID().String()).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("%s count=%d want=%d", table, count, want)
	}
}

func assertGovernanceCounts(t *testing.T, ctx context.Context, admin *sql.DB, scope tenancy.Scope, policies, switches, audits int) {
	t.Helper()
	var policyCount, switchCount, auditRecords int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM ai_agent_policies WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&policyCount); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM ai_agent_kill_switches WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&switchCount); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM audit_records WHERE workspace_id=$1 AND action LIKE 'settings.mcp_agent_%'`, scope.WorkspaceID().String()).Scan(&auditRecords); err != nil {
		t.Fatal(err)
	}
	if policyCount != policies || switchCount != switches || auditRecords != audits {
		t.Fatalf("policies=%d switches=%d audits=%d want=%d/%d/%d", policyCount, switchCount, auditRecords, policies, switches, audits)
	}
}
