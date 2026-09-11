package securityedge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestValkeyLimiterReplicasShareBudget(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("TORGNEXA_TEST_VALKEY_ADDR"))
	if address == "" {
		t.Skip("TORGNEXA_TEST_VALKEY_ADDR is not set")
	}
	namespace := fmt.Sprintf("torgnexa:test:rate:%d", time.Now().UnixNano())
	config := ValkeyConfig{Address: address, Namespace: namespace, ConnectTimeout: time.Second, RequestTimeout: time.Second, MaxConnections: 8, MaxKeys: 100}
	ctx := context.Background()
	first, err := NewValkeyLimiter(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := NewValkeyLimiter(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })

	limit := Limit{Name: "replicas", Key: "tenant/principal", Max: 2, Window: time.Minute}
	if _, err := first.Allow(ctx, limit); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Allow(ctx, limit); err != nil {
		t.Fatal(err)
	}
	decision, err := first.Allow(ctx, limit)
	if !errors.Is(err, ErrRateLimited) || decision.RetryAfter <= 0 || decision.RetryAfter > time.Minute {
		t.Fatalf("third request decision=%+v error=%v", decision, err)
	}

	t.Run("concurrent", func(t *testing.T) {
		concurrent := Limit{Name: "concurrent", Key: "same-principal", Max: 17, Window: time.Minute}
		var allowed atomic.Int64
		var limited atomic.Int64
		var wait sync.WaitGroup
		for index := range 128 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				limiter := first
				if index%2 == 1 {
					limiter = second
				}
				_, allowErr := limiter.Allow(ctx, concurrent)
				switch {
				case allowErr == nil:
					allowed.Add(1)
				case errors.Is(allowErr, ErrRateLimited):
					limited.Add(1)
				default:
					t.Errorf("Allow() error = %v", allowErr)
				}
			}()
		}
		wait.Wait()
		if allowed.Load() != 17 || limited.Load() != 111 {
			t.Fatalf("allowed=%d limited=%d", allowed.Load(), limited.Load())
		}
	})
}

func TestValkeyLimiterBoundsAttackerControlledCardinality(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("TORGNEXA_TEST_VALKEY_ADDR"))
	if address == "" {
		t.Skip("TORGNEXA_TEST_VALKEY_ADDR is not set")
	}
	config := ValkeyConfig{
		Address: address, Namespace: fmt.Sprintf("torgnexa:test:capacity:%d", time.Now().UnixNano()),
		ConnectTimeout: time.Second, RequestTimeout: time.Second, MaxConnections: 4, MaxKeys: 2,
	}
	limiter, err := NewValkeyLimiter(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = limiter.Close() })
	limit := func(key string) Limit { return Limit{Name: "pre_auth", Key: key, Max: 2, Window: time.Minute} }
	for _, key := range []string{"198.51.100.1", "198.51.100.2"} {
		if _, err := limiter.Allow(context.Background(), limit(key)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := limiter.Allow(context.Background(), limit("198.51.100.3")); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("cardinality exhaustion error=%v", err)
	}
	if _, err := limiter.Allow(context.Background(), limit("198.51.100.1")); err != nil {
		t.Fatalf("existing bounded key stopped working: %v", err)
	}
	metrics := limiter.Metrics()
	if metrics.CapacityLimited != 1 || metrics.Allowed != 3 {
		t.Fatalf("metrics=%+v", metrics)
	}
}

func TestValkeyKeysDoNotContainRawIdentity(t *testing.T) {
	const rawIdentity = "organization-fixture\x00workspace-fixture\x00issuer-fixture\x00subject-fixture"
	limit := Limit{Name: "authenticated", Key: rawIdentity, Max: 1, Window: time.Minute}
	counter, active, member := valkeyKeys("torgnexa:test", limit.Name, limitDigest(limit))
	if strings.Contains(counter, rawIdentity) || strings.Contains(active, rawIdentity) || strings.Contains(member, rawIdentity) {
		t.Fatalf("Valkey key leaked raw tenant/principal identity")
	}
}
