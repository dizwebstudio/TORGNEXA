package securityedge

import (
	"strings"
	"testing"
	"time"
)

func cfg() Config {
	return Config{TrustedProxyCIDRs: []string{"10.0.0.0/8"}, AdminCIDRs: []string{"10.10.0.0/16"}, AllowedOrigins: []string{"https://app.example"}, MaxRequestBytes: 11 << 30, MaxUploadBytes: 10 << 30, RatePerMinute: 2, HSTSSeconds: 31536000}
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
	if h["Strict-Transport-Security"] == "" || h["Content-Security-Policy"] == "" {
		t.Fatal("headers missing")
	}
	if CSRFAllowed("POST", "https://evil.example", c) {
		t.Fatal("cross-origin state change allowed")
	}
	l := NewLimiter()
	l.Now = func() time.Time { return time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC) }
	if l.Allow("tenant/ip", c) != nil || l.Allow("tenant/ip", c) != nil || l.Allow("tenant/ip", c) != ErrRateLimited {
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
	c := cfg()
	l := NewLimiter()
	l.maxKeys = 2
	now := time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC)
	l.Now = func() time.Time { return now }
	if err := l.Allow("key-1", c); err != nil {
		t.Fatal(err)
	}
	if err := l.Allow("key-2", c); err != nil {
		t.Fatal(err)
	}
	if err := l.Allow("key-3", c); err != ErrRateLimited {
		t.Fatalf("third unique key err=%v", err)
	}
	now = now.Add(time.Minute + time.Second)
	if err := l.Allow("key-3", c); err != nil {
		t.Fatalf("expired entries were not evicted: %v", err)
	}
	if len(l.window) != 1 || len(l.count) != 1 {
		t.Fatalf("window=%d count=%d", len(l.window), len(l.count))
	}
}
