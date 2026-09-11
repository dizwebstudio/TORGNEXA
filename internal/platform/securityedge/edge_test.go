package securityedge

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func cfg() Config {
	return Config{TrustedProxyCIDRs: []string{"10.0.0.0/8"}, AdminCIDRs: []string{"10.10.0.0/16"}, AllowedOrigins: []string{"https://app.example"}, MaxRequestBytes: 11 << 30, MaxUploadBytes: 10 << 30, PreAuthRatePerMinute: 20, RatePerMinute: 2, HSTSSeconds: 31536000}
}
func TestSpoofedForwardedHeadersRejected(t *testing.T) {
	if _, e := ClientIP("203.0.113.5:1234", "198.51.100.1", cfg()); e != ErrSpoofedForwarding {
		t.Fatalf("err=%v", e)
	}
	ip, e := ClientIP("10.0.0.5:1234", "198.51.100.1", cfg())
	if e != nil || ip.String() != "198.51.100.1" {
		t.Fatalf("%v %v", ip, e)
	}
}
func TestHeadersCSRFAndLimits(t *testing.T) {
	c := cfg()
	if c.Validate() != nil {
		t.Fatal("config invalid")
	}
	h := SecurityHeaders(c)
	if h["Strict-Transport-Security"] == "" || h["Content-Security-Policy"] == "" || !strings.Contains(h["Content-Security-Policy"], "img-src 'self' data: https:") {
		t.Fatal("headers missing")
	}
	if CSRFAllowed("POST", "https://evil.example", c) {
		t.Fatal("cross-origin state change allowed")
	}
	l := NewLimiter()
	l.now = func() time.Time { return time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC) }
	limit := Limit{Name: "test", Key: "tenant/ip", Max: c.RatePerMinute, Window: time.Minute}
	if _, err := l.Allow(context.Background(), limit); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Allow(context.Background(), limit); err != nil {
		t.Fatal(err)
	}
	if decision, err := l.Allow(context.Background(), limit); !errors.Is(err, ErrRateLimited) || decision.RetryAfter != time.Minute {
		t.Fatal("rate limit failed")
	}
}

func TestForwardedChainWalksFromTrustedProxyTowardClient(t *testing.T) {
	c := cfg()
	// nginx $proxy_add_x_forwarded_for appends the socket peer to a client-supplied
	// header. The forged left-most address must not win over the actual untrusted
	// client immediately to the right of it.
	ip, err := ClientIP("10.0.0.5:1234", "203.0.113.66, 198.51.100.9", c)
	if err != nil || ip.String() != "198.51.100.9" {
		t.Fatalf("client ip=%v err=%v", ip, err)
	}

	// Multiple trusted proxy hops are skipped from right to left.
	ip, err = ClientIP("10.0.0.5:1234", "203.0.113.77, 10.1.1.8", c)
	if err != nil || ip.String() != "203.0.113.77" {
		t.Fatalf("multi-proxy client ip=%v err=%v", ip, err)
	}
}

func TestForwardedChainRejectsMalformedOrExcessiveHops(t *testing.T) {
	c := cfg()
	if _, err := ClientIP("10.0.0.5:1234", "198.51.100.9, not-an-ip", c); err != ErrInvalid {
		t.Fatalf("malformed hop err=%v", err)
	}
	parts := make([]string, maxForwardedHops+1)
	for i := range parts {
		parts[i] = "198.51.100.9"
	}
	if _, err := ClientIP("10.0.0.5:1234", strings.Join(parts, ","), c); err != ErrInvalid {
		t.Fatalf("excessive hops err=%v", err)
	}
}

func TestLimiterBoundsKeyCardinalityAndEvictsExpiredEntries(t *testing.T) {
	l := NewLocalLimiter(2)
	now := time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	limit := func(key string) Limit { return Limit{Name: "test", Key: key, Max: 10, Window: time.Minute} }
	if _, err := l.Allow(context.Background(), limit("key-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Allow(context.Background(), limit("key-2")); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Allow(context.Background(), limit("key-3")); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("third unique key err=%v", err)
	}
	now = now.Add(time.Minute + time.Second)
	if _, err := l.Allow(context.Background(), limit("key-3")); err != nil {
		t.Fatalf("expired entries were not evicted: %v", err)
	}
	if active := l.activeKeys.Load(); active != 1 {
		t.Fatalf("active keys=%d", active)
	}
	metrics := l.Metrics()
	if metrics.Allowed != 3 || metrics.Limited != 1 || metrics.CapacityLimited != 1 {
		t.Fatalf("metrics=%+v", metrics)
	}
}

func TestLocalLimiterIsAtomicUnderConcurrency(t *testing.T) {
	limiter := NewLocalLimiter(100)
	limit := Limit{Name: "concurrent", Key: "same-principal", Max: 37, Window: time.Minute}
	var allowed atomic.Int64
	var limited atomic.Int64
	var wait sync.WaitGroup
	for range 200 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := limiter.Allow(context.Background(), limit)
			switch {
			case err == nil:
				allowed.Add(1)
			case errors.Is(err, ErrRateLimited):
				limited.Add(1)
			default:
				t.Errorf("Allow() error = %v", err)
			}
		}()
	}
	wait.Wait()
	if allowed.Load() != 37 || limited.Load() != 163 {
		t.Fatalf("allowed=%d limited=%d", allowed.Load(), limited.Load())
	}
}

func TestLimiterHashesIdentityKeys(t *testing.T) {
	const raw = "tenant-fixture\x00principal-fixture"
	digest := limitDigest(Limit{Name: "authenticated", Key: raw, Max: 1, Window: time.Minute})
	if strings.Contains(string(digest[:]), raw) {
		t.Fatal("raw identity was retained in limiter key")
	}
}
