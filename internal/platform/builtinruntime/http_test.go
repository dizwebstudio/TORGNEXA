package builtinruntime

import (
	"context"
	"net/netip"
	"os"
	"testing"
	"time"

	maxmessenger "github.com/torgnexa/torgnexa/connectors/social/max-messenger"
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

func TestMAXRuntimeRequestAdmissionIsExact(t *testing.T) {
	allowed := []maxmessenger.Request{
		{Method: "GET", Path: "/me"},
		{Method: "GET", Path: "/chats/-70801090403050"},
		{Method: "GET", Path: "/chats/-70801090403050/members/me"},
		{Method: "POST", Path: "/messages", Params: []maxmessenger.Param{{Name: "chat_id", Value: "-70801090403050"}}, Body: []byte(`{"text":"test"}`)},
	}
	for _, request := range allowed {
		if !validMAXRuntimeRequest(request) {
			t.Fatalf("admitted MAX request rejected: %+v", request)
		}
	}
	rejected := []maxmessenger.Request{
		{Method: "GET", Path: "chats/1"},
		{Method: "GET", Path: "/messages/1"},
		{Method: "POST", Path: "/messages", Params: []maxmessenger.Param{{Name: "chat_id", Value: "0"}}, Body: []byte(`{"text":"test"}`)},
		{Method: "POST", Path: "/messages", Params: []maxmessenger.Param{{Name: "chat_id", Value: "1"}, {Name: "user_id", Value: "2"}}, Body: []byte(`{"text":"test"}`)},
		{Method: "POST", Path: "/uploads", Body: []byte(`{}`)},
	}
	for _, request := range rejected {
		if validMAXRuntimeRequest(request) {
			t.Fatalf("unadmitted MAX request accepted: %+v", request)
		}
	}
}

func TestLiveCBRDailyTransport(t *testing.T) {
	if os.Getenv("TORGNEXA_LIVE_CBR") != "1" {
		t.Skip("live CBR qualification disabled")
	}
	body, err := newCBRDailyHTTP(newHTTPTransport()).Daily(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < 1024 {
		t.Fatalf("CBR daily document unexpectedly small: %d", len(body))
	}
}
