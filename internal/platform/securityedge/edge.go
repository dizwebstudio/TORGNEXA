// Package securityedge implements vendor-neutral reverse-proxy trust and browser/API edge policy.
package securityedge

import (
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
)

var ErrInvalid = errors.New("securityedge: invalid value")
var ErrSpoofedForwarding = errors.New("securityedge: untrusted forwarded headers")
var ErrRateLimited = errors.New("securityedge: rate limited")

const maxForwardedHops = 32
const defaultMaxLimiterKeys = 100_000

type Config struct {
	TrustedProxyCIDRs               []string
	AdminCIDRs                      []string
	AllowedOrigins                  []string
	MaxRequestBytes, MaxUploadBytes int64
	RatePerMinute                   int
	HSTSSeconds                     int64
}

func (c Config) Validate() error {
	if c.MaxRequestBytes <= 0 || c.MaxUploadBytes <= 0 || c.MaxRequestBytes < c.MaxUploadBytes || c.RatePerMinute < 1 || c.HSTSSeconds < 31536000 {
		return ErrInvalid
	}
	for _, x := range append(append([]string{}, c.TrustedProxyCIDRs...), c.AdminCIDRs...) {
		if _, e := netip.ParsePrefix(x); e != nil {
			return ErrInvalid
		}
	}
	return nil
}
func trusted(ip netip.Addr, cidrs []string) bool {
	for _, raw := range cidrs {
		p, _ := netip.ParsePrefix(raw)
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
func ClientIP(remoteAddr, forwarded string, c Config) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return netip.Addr{}, ErrInvalid
	}
	ip = ip.Unmap()
	forwarded = strings.TrimSpace(forwarded)
	if forwarded == "" {
		return ip, nil
	}
	if !trusted(ip, c.TrustedProxyCIDRs) {
		return netip.Addr{}, ErrSpoofedForwarding
	}

	// X-Forwarded-For is an append-only hop chain when deployed with the
	// documented nginx $proxy_add_x_forwarded_for configuration. A client can
	// therefore inject values on the *left*. Walk from the trusted socket peer
	// toward the client and stop at the first untrusted hop; never trust the
	// left-most value merely because the immediate peer is trusted.
	parts := strings.Split(forwarded, ",")
	if len(parts) == 0 || len(parts) > maxForwardedHops {
		return netip.Addr{}, ErrInvalid
	}
	hops := make([]netip.Addr, len(parts))
	for index, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return netip.Addr{}, ErrInvalid
		}
		hop, parseErr := netip.ParseAddr(raw)
		if parseErr != nil {
			return netip.Addr{}, ErrInvalid
		}
		hops[index] = hop.Unmap()
	}

	current := ip
	for index := len(hops) - 1; index >= 0 && trusted(current, c.TrustedProxyCIDRs); index-- {
		current = hops[index]
	}
	return current, nil
}
func AdminAllowed(ip netip.Addr, c Config) bool { return trusted(ip, c.AdminCIDRs) }
func SecurityHeaders(c Config) map[string]string {
	return map[string]string{"Strict-Transport-Security": "max-age=" + itoa(c.HSTSSeconds) + "; includeSubDomains", "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer", "Permissions-Policy": "camera=(), microphone=(), geolocation=()", "Content-Security-Policy": "default-src 'self'; frame-ancestors 'none'; object-src 'none'", "Cross-Origin-Opener-Policy": "same-origin"}
}
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 20)
	for n > 0 {
		b = append(b, byte('0'+n%10))
		n /= 10
	}
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}
func OriginAllowed(origin string, c Config) bool {
	for _, x := range c.AllowedOrigins {
		if origin == x {
			return true
		}
	}
	return false
}
func CSRFAllowed(method, origin string, c Config) bool {
	switch method {
	case "GET", "HEAD", "OPTIONS":
		return true
	default:
		return OriginAllowed(origin, c)
	}
}

type Limiter struct {
	mu        sync.Mutex
	window    map[string]time.Time
	count     map[string]int
	lastSweep time.Time
	maxKeys   int
	Now       func() time.Time
}

func NewLimiter() *Limiter {
	return &Limiter{window: map[string]time.Time{}, count: map[string]int{}, maxKeys: defaultMaxLimiterKeys, Now: time.Now}
}
func (l *Limiter) Allow(key string, c Config) error {
	if l == nil || key == "" || c.RatePerMinute < 1 || l.Now == nil {
		return ErrInvalid
	}
	now := l.Now().UTC()
	l.mu.Lock()
	defer l.mu.Unlock()

	// Bound attacker-controlled key cardinality. Expired entries are swept at
	// most once per minute, and a full live window fails closed for unseen keys
	// instead of growing the process heap without limit.
	if l.lastSweep.IsZero() || now.Sub(l.lastSweep) >= time.Minute {
		for existing, start := range l.window {
			if now.Sub(start) >= time.Minute {
				delete(l.window, existing)
				delete(l.count, existing)
			}
		}
		l.lastSweep = now
	}
	maxKeys := l.maxKeys
	if maxKeys <= 0 {
		maxKeys = defaultMaxLimiterKeys
	}
	if _, exists := l.window[key]; !exists && len(l.window) >= maxKeys {
		return ErrRateLimited
	}

	start := l.window[key]
	if start.IsZero() || now.Sub(start) >= time.Minute {
		l.window[key] = now
		l.count[key] = 0
	}
	if l.count[key] >= c.RatePerMinute {
		return ErrRateLimited
	}
	l.count[key]++
	return nil
}

type SecuritySignal struct {
	Code, ClientFingerprint, PathClass string
	OccurredAt                         time.Time
}
type SignalSink interface{ EmitSecurityEdgeSignal(SecuritySignal) error }
