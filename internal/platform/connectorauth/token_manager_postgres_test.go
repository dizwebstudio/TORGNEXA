package connectorauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/secretrepo"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
	"sync/atomic"
	"testing"
	"time"
)

func TestA08PostgresRefreshBoundedPool(t *testing.T) {
	for _, poolSize := range []int{1, 4} {
		t.Run(fmt.Sprint(poolSize), func(t *testing.T) {
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
				return json.Marshal(TokenBundle{AccessToken: "synthetic-new-access", RefreshToken: "synthetic-new-refresh", TokenType: "Bearer", ExpiresAt: at.Add(time.Hour).Format(time.RFC3339), ClientID: current.ClientID, ClientSecret: current.ClientSecret})
			}
			accounts := make([]sdk.Account, poolSize)
			for i := range accounts {
				raw, _ := json.Marshal(TokenBundle{AccessToken: "synthetic-old-access", RefreshToken: "synthetic-old-refresh", TokenType: "Bearer", ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339), ClientID: "synthetic-client", ClientSecret: "synthetic-secret"})
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
			for range poolSize * 4 {
				if err := <-results; err != nil {
					t.Fatal(err)
				}
			}
			if calls.Load() != int64(poolSize) {
				t.Fatalf("refreshes=%d want=%d", calls.Load(), poolSize)
			}
			for _, account := range accounts {
				metadata, err := provider.Describe(ctx, scope, secrets.Reference(account.SecretReference))
				if err != nil || metadata.CurrentVersion != 2 {
					t.Fatalf("rotation version=%d error=%v", metadata.CurrentVersion, err)
				}
			}
			if db.Stats().InUse != 0 {
				t.Fatal("connection leaked")
			}
		})
	}
}

func TestA08PostgresRefreshRollbackAndCancellation(t *testing.T) {
	ctx, db, _, scope := auditPostgres(t)
	repo, _ := secretrepo.New(db)
	keys, _ := secrets.NewStaticKeyring("synthetic", map[string][]byte{"synthetic": make([]byte, 32)})
	provider, _ := secrets.NewLocalEncryptedProvider(repo, keys)
	now := time.Now().UTC()
	raw, _ := json.Marshal(TokenBundle{AccessToken: "synthetic-old-access", RefreshToken: "synthetic-old-refresh", TokenType: "Bearer", ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339), ClientID: "client", ClientSecret: "synthetic"})
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
