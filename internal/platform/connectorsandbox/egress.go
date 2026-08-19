package connectorsandbox

import (
	"context"
	"net"
	"net/netip"
	"sort"

	"github.com/torgnexa/torgnexa/internal/platform/pluginsecurity"
)

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type DialTarget struct {
	IP         netip.Addr `json:"-"`
	Port       uint16     `json:"port"`
	ServerName string     `json:"server_name"`
}

type EgressGuard struct{ Resolver Resolver }

func (guard EgressGuard) Plan(ctx context.Context, plan pluginsecurity.AdmissionPlan, destination pluginsecurity.NetworkDestination) ([]DialTarget, error) {
	if !networkGranted(plan, destination) || destination.Validate() != nil || guard.Resolver == nil {
		return nil, ErrEgressDenied
	}
	values, err := guard.Resolver.LookupNetIP(ctx, "ip", destination.Host)
	if err != nil || len(values) == 0 {
		return nil, ErrEgressDenied
	}
	seen := map[netip.Addr]struct{}{}
	result := make([]DialTarget, 0, len(values))
	for _, value := range values {
		value = value.Unmap()
		if !publicInternetAddress(value) {
			return nil, ErrEgressDenied
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, DialTarget{IP: value, Port: destination.Port, ServerName: destination.Host})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].IP.Less(result[j].IP) })
	if len(result) == 0 {
		return nil, ErrEgressDenied
	}
	return result, nil
}

func publicInternetAddress(value netip.Addr) bool {
	if !value.IsValid() || !value.IsGlobalUnicast() || value.IsLoopback() || value.IsPrivate() || value.IsLinkLocalUnicast() || value.IsLinkLocalMulticast() || value.IsMulticast() || value.IsUnspecified() {
		return false
	}
	// Explicitly reject special-use ranges that IsGlobalUnicast may still report.
	blocked := []netip.Prefix{
		netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
	for _, prefix := range blocked {
		if prefix.Contains(value) {
			return false
		}
	}
	return true
}

// SystemResolver is the production host resolver. Providers never receive it.
type SystemResolver struct{ value *net.Resolver }

func NewSystemResolver() SystemResolver { return SystemResolver{value: net.DefaultResolver} }
func (r SystemResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return r.value.LookupNetIP(ctx, network, host)
}
