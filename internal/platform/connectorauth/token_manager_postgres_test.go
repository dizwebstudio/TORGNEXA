package connectorauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/secretrepo"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestA08PostgresRefreshBoundedPool(t *testing.T) {
	for _, testCase := range []struct {
		poolSize              int
		expectedRefresh       int64
		expectedRefreshMetric uint64
	}{
		{poolSize: 1, expectedRefresh: 1, expectedRefreshMetric: 1},
		{poolSize: 4, expectedRefresh: 4, expectedRefreshMetric: 4},
	} {
		t.Run(fmt.Sprint(testCase.poolSize), func(t *testing.T) {
			poolSize := testCase.poolSize
			ctx, db, _, scope := auditPostgres(t)
			db.SetMaxOpenConns(poolSize)
			repo, err := secretrepo.New(db)
			if err != nil {
				t.Fatal(err)
			}
			keys, err := secrets.NewStaticKeyring("synthetic", map[string][]byte{"synthetic": make([]byte, 32)})
			if err != nil {
				t.Fatal(err)
			}
			provider, err := secrets.NewLocalEncryptedProvider(repo, keys)
			if err != nil {
				t.Fatal(err)
			}
			manager, err := NewTokenManager(provider, repo)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			manager.now = func() time.Time { return now }
			var calls atomic.Int64
			manager.refresh = func(ctx context.Context, _ sdk.OAuth2Configuration, current TokenBundle, _ time.Duration, at time.Time) ([]byte, error) {
				calls.Add(1)
				if current.RefreshToken != "synthetic-old-refresh" {
					return nil, errors.New("rotated token reused")
				}
				return json.Marshal(TokenBundle{AccessToken: "synthetic-new-access", RefreshToken: "synthetic-new-refresh", TokenType: "Bearer", ExpiresAt: at.Add(time.Hour).Format(time.RFC3339), ClientID: current.ClientID, ClientSecret: current.ClientSecret}) // #nosec G117 -- synthetic credentials exercise encrypted-store rotation.
			}
			accounts := make([]sdk.Account, poolSize)
			for i := range accounts {
				raw, _ := json.Marshal(TokenBundle{AccessToken: "synthetic-old-access", RefreshToken: "synthetic-old-refresh", TokenType: "Bearer", ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339), ClientID: "synthetic-client", ClientSecret: "synthetic-secret"}) // #nosec G117 -- synthetic credentials are encrypted immediately.
				metadata, err := provider.Create(ctx, scope, secrets.ClassOAuthRefresh, raw)
				if err != nil {
					t.Fatal(err)
				}
				accounts[i] = oauthAccount(grantID(t, "authorization_code"))
				accounts[i].SecretReference = sdk.SecretReference(metadata.Reference)
			}
			start := make(chan struct{})
			results := make(chan error, poolSize*4)
			for _, account := range accounts {
				for range 4 {
					go func() {
						<-start
						results <- manager.UseAccessToken(ctx, scope, account, func(token []byte) error {
							if string(token) != "synthetic-new-access" {
								return errors.New("unexpected access token")
							}
							return nil
						})
					}()
				}
			}
			close(start)
			if !t.Run("operations", func(t *testing.T) {
				for range poolSize * 4 {
					if err := <-results; err != nil {
						t.Fatal(err)
					}
				}
			}) {
				return
			}
			t.Run("refresh_count", func(t *testing.T) {
				if calls.Load() != testCase.expectedRefresh {
					t.Fatalf("refreshes=%d want=%d", calls.Load(), testCase.expectedRefresh)
				}
			})
			metrics := repo.OAuthRefreshMetrics()
			wantLimit := poolSize
			if poolSize > 1 {
				wantLimit--
			}
			t.Run("metrics_concurrency_limit", func(t *testing.T) {
				if metrics.ConcurrencyLimit != wantLimit {
					t.Fatalf("concurrency limit=%d want=%d", metrics.ConcurrencyLimit, wantLimit)
				}
			})
			t.Run("metrics_peak_in_flight", func(t *testing.T) {
				if metrics.PeakInFlight > int64(wantLimit) {
					t.Fatalf("peak in flight=%d limit=%d", metrics.PeakInFlight, wantLimit)
				}
			})
			t.Run("metrics_refresh_successes", func(t *testing.T) {
				if metrics.RefreshSuccesses != testCase.expectedRefreshMetric {
					t.Fatalf("refresh successes=%d want=%d", metrics.RefreshSuccesses, testCase.expectedRefreshMetric)
				}
			})
			t.Run("metrics_refresh_failures", func(t *testing.T) {
				if metrics.RefreshFailures != 0 {
					t.Fatalf("refresh failures=%d want=0", metrics.RefreshFailures)
				}
			})
			t.Run("metrics_refresh_latency_count", func(t *testing.T) {
				if metrics.RefreshLatency.Count != testCase.expectedRefreshMetric {
					t.Fatalf("refresh latency count=%d want=%d", metrics.RefreshLatency.Count, testCase.expectedRefreshMetric)
				}
			})
			t.Run("rotation_versions", func(t *testing.T) {
				for _, account := range accounts {
					metadata, err := provider.Describe(ctx, scope, secrets.Reference(account.SecretReference))
					if err != nil || metadata.CurrentVersion != 2 {
						t.Fatalf("rotation version=%d error=%v", metadata.CurrentVersion, err)
					}
				}
			})
			t.Run("connections_released", func(t *testing.T) {
				if db.Stats().InUse != 0 {
					t.Fatal("connection leaked")
				}
			})
		})
	}
}

func TestA08PostgresRefreshBoundsConcurrentAccountsAndReportsPoolSaturation(t *testing.T) {
	ctx, db, _, scope := auditPostgres(t)
	db.SetMaxOpenConns(12)
	repo, err := secretrepo.New(db)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := secrets.NewStaticKeyring("synthetic", map[string][]byte{"synthetic": make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := secrets.NewLocalEncryptedProvider(repo, keys)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	manager, err := NewTokenManager(provider, repo)
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return now }
	started := make(chan struct{}, 10)
	release := make(chan struct{})
	var active, peak atomic.Int64
	manager.refresh = func(_ context.Context, _ sdk.OAuth2Configuration, current TokenBundle, _ time.Duration, at time.Time) ([]byte, error) {
		currentActive := active.Add(1)
		defer active.Add(-1)
		for {
			observed := peak.Load()
			if currentActive <= observed || peak.CompareAndSwap(observed, currentActive) {
				break
			}
		}
		started <- struct{}{}
		<-release
		return json.Marshal(TokenBundle{AccessToken: "synthetic-new-access", RefreshToken: "synthetic-new-refresh", TokenType: "Bearer", ExpiresAt: at.Add(time.Hour).Format(time.RFC3339), ClientID: current.ClientID, ClientSecret: current.ClientSecret}) // #nosec G117 -- synthetic credentials exercise encrypted-store rotation.
	}
	accounts := make([]sdk.Account, 10)
	for index := range accounts {
		raw, _ := json.Marshal(TokenBundle{AccessToken: "synthetic-old-access", RefreshToken: "synthetic-old-refresh", TokenType: "Bearer", ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339), ClientID: "client", ClientSecret: "synthetic"}) // #nosec G117 -- synthetic credentials are encrypted immediately.
		metadata, createErr := provider.Create(ctx, scope, secrets.ClassOAuthRefresh, raw)
		if createErr != nil {
			t.Fatal(createErr)
		}
		accounts[index] = oauthAccount(grantID(t, "authorization_code"))
		accounts[index].ID = fmt.Sprintf("oauth-account-%02d", index)
		accounts[index].SecretReference = sdk.SecretReference(metadata.Reference)
	}
	results := make(chan error, len(accounts))
	for _, account := range accounts {
		go func(account sdk.Account) {
			results <- manager.Prepare(ctx, scope, account)
		}(account)
	}
	for range 8 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("eight admitted refreshes did not start")
		}
	}
	select {
	case <-started:
		close(release)
		t.Fatal("refresh concurrency exceeded the process limit")
	case <-time.After(100 * time.Millisecond):
	}
	live := repo.OAuthRefreshMetrics()
	if live.ConcurrencyLimit != 8 || live.InFlight != 8 || live.Waiting < 2 || live.Pool.PeakSaturationPPM < 600_000 {
		close(release)
		t.Fatalf("missing live saturation evidence: %+v", live)
	}
	close(release)
	for range accounts {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	metrics := repo.OAuthRefreshMetrics()
	if peak.Load() != 8 || metrics.PeakInFlight != 8 || metrics.AdmissionWaits < 2 || metrics.AdmissionWait.Count < 2 || metrics.RefreshSuccesses != 10 || metrics.RefreshFailures != 0 || metrics.RefreshLatency.Count != 10 || metrics.InFlight != 0 || metrics.Waiting != 0 {
		t.Fatalf("unexpected bounded refresh metrics: peak=%d metrics=%+v", peak.Load(), metrics)
	}
	t.Logf(`TORGNEXA_REGRESSION_METRIC {"name":"oauth_refresh_pool","operations":%d,"concurrency_limit":%d,"peak_in_flight":%d,"admission_waits":%d,"peak_saturation_ppm":%d,"refresh_p50_ns":%d,"refresh_p95_ns":%d,"refresh_p99_ns":%d}`,
		metrics.RefreshLatency.Count, metrics.ConcurrencyLimit, metrics.PeakInFlight, metrics.AdmissionWaits, metrics.Pool.PeakSaturationPPM,
		durationPercentileUpperBound(metrics.RefreshLatency, 50).Nanoseconds(), durationPercentileUpperBound(metrics.RefreshLatency, 95).Nanoseconds(), durationPercentileUpperBound(metrics.RefreshLatency, 99).Nanoseconds())
}

func durationPercentileUpperBound(metrics secretrepo.DurationMetrics, percentile uint64) time.Duration {
	if metrics.Count == 0 || percentile == 0 || percentile > 100 {
		return 0
	}
	target := (metrics.Count*percentile + 99) / 100
	for _, bucket := range metrics.Buckets {
		if bucket.Count >= target {
			return bucket.LessThanOrEqual
		}
	}
	if len(metrics.Buckets) == 0 {
		return 0
	}
	return metrics.Buckets[len(metrics.Buckets)-1].LessThanOrEqual
}

func TestA08PostgresRefreshRetriesContendedAdvisoryLockWithJitteredBackoff(t *testing.T) {
	ctx, db, _, scope := auditPostgres(t)
	db.SetMaxOpenConns(4)
	repo, err := secretrepo.New(db)
	if err != nil {
		t.Fatal(err)
	}
	held := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- repo.WithRefreshLock(ctx, scope, tokenManagerReference, func(context.Context) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	second := make(chan error, 1)
	go func() {
		second <- repo.WithRefreshLock(ctx, scope, tokenManagerReference, func(context.Context) error { return nil })
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for repo.OAuthRefreshMetrics().LockContentions == 0 {
		select {
		case <-deadline.C:
			close(release)
			t.Fatal("second caller did not observe advisory-lock contention")
		case <-time.After(5 * time.Millisecond):
		}
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	metrics := repo.OAuthRefreshMetrics()
	if metrics.LockAttempts < 3 || metrics.LockContentions == 0 || metrics.LockWait.Count != 2 || metrics.LockWait.Total <= 0 {
		t.Fatalf("unexpected lock wait metrics: %+v", metrics)
	}
}

func TestA08PostgresRefreshRollbackAndCancellation(t *testing.T) {
	ctx, db, _, scope := auditPostgres(t)
	repo, _ := secretrepo.New(db)
	keys, _ := secrets.NewStaticKeyring("synthetic", map[string][]byte{"synthetic": make([]byte, 32)})
	provider, _ := secrets.NewLocalEncryptedProvider(repo, keys)
	now := time.Now().UTC()
	raw, _ := json.Marshal(TokenBundle{AccessToken: "synthetic-old-access", RefreshToken: "synthetic-old-refresh", TokenType: "Bearer", ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339), ClientID: "client", ClientSecret: "synthetic"}) // #nosec G117 -- synthetic credentials are encrypted immediately.
	metadata, err := provider.Create(ctx, scope, secrets.ClassOAuthRefresh, raw)
	if err != nil {
		t.Fatal(err)
	}
	account := oauthAccount(grantID(t, "authorization_code"))
	account.SecretReference = sdk.SecretReference(metadata.Reference)
	manager, _ := NewTokenManager(provider, repo)
	manager.refresh = func(context.Context, sdk.OAuth2Configuration, TokenBundle, time.Duration, time.Time) ([]byte, error) {
		return nil, ErrOAuthRefreshRejected
	}
	if err := manager.UseAccessToken(ctx, scope, account, func([]byte) error { return nil }); !errors.Is(err, ErrOAuthReauthorizationRequired) {
		t.Fatal(err)
	}
	if metrics := repo.OAuthRefreshMetrics(); metrics.RefreshFailures != 1 || metrics.RefreshSuccesses != 0 || metrics.RefreshLatency.Count != 1 {
		t.Fatalf("rejected refresh was not measured: %+v", metrics)
	}
	manager.refresh = func(c context.Context, _ sdk.OAuth2Configuration, _ TokenBundle, _ time.Duration, _ time.Time) ([]byte, error) {
		<-c.Done()
		return nil, c.Err()
	}
	short, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
	defer cancel()
	if err := manager.UseAccessToken(short, scope, account, func([]byte) error { return nil }); err == nil {
		t.Fatal("cancelled refresh succeeded")
	}
	stored, err := provider.Describe(ctx, scope, metadata.Reference)
	if err != nil || stored.CurrentVersion != 1 {
		t.Fatalf("failed refresh changed secret: version=%d error=%v", stored.CurrentVersion, err)
	}
	rollback := errors.New("synthetic rollback")
	err = repo.WithRefreshLock(ctx, scope, metadata.Reference, func(c context.Context) error {
		if _, err := provider.Rotate(c, scope, metadata.Reference, raw); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	stored, err = provider.Describe(ctx, scope, metadata.Reference)
	if err != nil || stored.CurrentVersion != 1 {
		t.Fatal("rotation did not roll back", err)
	}
	if err := repo.WithRefreshLock(ctx, scope, metadata.Reference, func(context.Context) error { return nil }); err != nil {
		t.Fatal("lock leaked", err)
	}
}

func TestConnectorAuditPostgresRefreshIntentPrecedesRemoteEffect(t *testing.T) {
	ctx, db, admin, scope := auditPostgres(t)
	repo, _ := secretrepo.New(db)
	keys, _ := secrets.NewStaticKeyring("synthetic", map[string][]byte{"synthetic": make([]byte, 32)})
	provider, _ := secrets.NewLocalEncryptedProvider(repo, keys)
	now := time.Now().UTC()
	raw, _ := json.Marshal(TokenBundle{AccessToken: "synthetic-old-access", RefreshToken: "synthetic-old-refresh", TokenType: "Bearer", ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339), ClientID: "client", ClientSecret: "synthetic"}) // #nosec G117 -- synthetic credentials are encrypted immediately.
	metadata, err := provider.Create(ctx, scope, secrets.ClassOAuthRefresh, raw)
	if err != nil {
		t.Fatal(err)
	}
	account := oauthAccount(grantID(t, "authorization_code"))
	account.SecretReference = sdk.SecretReference(metadata.Reference)
	manager, _ := NewTokenManager(provider, repo)
	manager.now = func() time.Time { return now }
	var calls atomic.Int64
	manager.refresh = func(_ context.Context, _ sdk.OAuth2Configuration, current TokenBundle, _ time.Duration, at time.Time) ([]byte, error) {
		calls.Add(1)
		return json.Marshal(TokenBundle{AccessToken: "synthetic-new-access", RefreshToken: "synthetic-new-refresh", TokenType: "Bearer", ExpiresAt: at.Add(time.Hour).Format(time.RFC3339), ClientID: current.ClientID, ClientSecret: current.ClientSecret}) // #nosec G117 -- synthetic credentials exercise encrypted-store rotation.
	}
	_, err = admin.ExecContext(ctx, `CREATE FUNCTION fail_oauth_refresh_intent() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''synthetic refresh evidence failure''; END'; CREATE TRIGGER fail_oauth_refresh_intent BEFORE INSERT ON security_evidence FOR EACH ROW WHEN (NEW.evidence_type='connector.oauth_refresh.requested') EXECUTE FUNCTION fail_oauth_refresh_intent()`)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Prepare(ctx, scope, account); !errors.Is(err, ErrOAuthRefreshUnavailable) || calls.Load() != 0 {
		t.Fatalf("refresh escaped missing intent: error=%v calls=%d", err, calls.Load())
	}
	stored, err := provider.Describe(ctx, scope, metadata.Reference)
	if err != nil || stored.CurrentVersion != 1 {
		t.Fatalf("failed intent changed secret: version=%d error=%v", stored.CurrentVersion, err)
	}
	if _, err = admin.ExecContext(ctx, `DROP TRIGGER fail_oauth_refresh_intent ON security_evidence; DROP FUNCTION fail_oauth_refresh_intent()`); err != nil {
		t.Fatal(err)
	}
	if err = manager.Prepare(ctx, scope, account); err != nil || calls.Load() != 1 {
		t.Fatalf("retry failed: error=%v calls=%d", err, calls.Load())
	}
	var count int
	var summary string
	if err = admin.QueryRowContext(ctx, `SELECT count(*),COALESCE(min(summary::text),'') FROM security_evidence WHERE organization_id=$1 AND workspace_id=$2 AND evidence_type='connector.oauth_refresh.requested'`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&count, &summary); err != nil {
		t.Fatal(err)
	}
	expectedRuntime := account.ConnectorID
	if count != 1 || strings.Contains(summary, metadata.Reference.String()) || strings.Contains(summary, "synthetic-old-refresh") || !strings.Contains(summary, expectedRuntime) {
		t.Fatalf("unsafe or missing refresh evidence: count=%d summary=%s", count, summary)
	}
}
