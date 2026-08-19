package builtinruntime

import (
	"net/netip"
	"testing"
)

func TestHostAndAddressPolicy(t *testing.T) {
	for _, host := range []string{"api-seller.ozon.ru", "marketplace-api.wildberries.ru", "api.moysklad.ru"} {
		if !validHost(host) {
			t.Fatalf("valid host rejected: %s", host)
		}
	}
	for _, host := range []string{"localhost", "127.0.0.1", "Example.COM", "metadata.local", "http://example.com"} {
		if validHost(host) {
			t.Fatalf("unsafe host accepted: %s", host)
		}
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "192.0.2.1", "2001:db8::1"} {
		addr, err := netip.ParseAddr(raw)
		if err != nil {
			t.Fatal(err)
		}
		if publicIP(addr) {
			t.Fatalf("unsafe address accepted: %s", raw)
		}
	}
}
