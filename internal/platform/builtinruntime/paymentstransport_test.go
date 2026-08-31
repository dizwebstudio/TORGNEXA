package builtinruntime

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func TestDecimalMinorUnitsRoundTrip(t *testing.T) {
	cases := []struct {
		decimal string
		minor   int64
	}{
		{"0.01", 1}, {"1.00", 100}, {"150.00", 15000}, {"150", 15000}, {"150.5", 15050}, {"-10.50", -1050},
	}
	for _, tc := range cases {
		got, err := decimalToMinorUnits(tc.decimal)
		if err != nil {
			t.Fatalf("decimalToMinorUnits(%q): %v", tc.decimal, err)
		}
		if got != tc.minor {
			t.Fatalf("decimalToMinorUnits(%q) = %d, want %d", tc.decimal, got, tc.minor)
		}
	}
	if got := minorUnitsToDecimal(15000); got != "150.00" {
		t.Fatalf("minorUnitsToDecimal(15000) = %q, want 150.00", got)
	}
	if got := minorUnitsToDecimal(1); got != "0.01" {
		t.Fatalf("minorUnitsToDecimal(1) = %q, want 0.01", got)
	}
	for _, bad := range []string{"", "abc", "1.999", "1.2.3", "-"} {
		if _, err := decimalToMinorUnits(bad); err == nil {
			t.Fatalf("decimalToMinorUnits(%q) accepted invalid input", bad)
		}
	}
}

func TestSplitBasicCredential(t *testing.T) {
	user, pass, err := splitBasicCredential([]byte("shop_123\nsecret_abc"))
	if err != nil || string(user) != "shop_123" || string(pass) != "secret_abc" {
		t.Fatalf("splitBasicCredential = %q %q %v", user, pass, err)
	}
	for _, bad := range [][]byte{nil, []byte("no-newline"), []byte("\nempty-user"), []byte("empty-pass\n")} {
		if _, _, err := splitBasicCredential(bad); err == nil {
			t.Fatalf("splitBasicCredential(%q) accepted invalid input", bad)
		}
	}
}

// testTLSTransport builds an httpTransport wired to dial directly into a
// local httptest.NewTLSServer, bypassing the SSRF-safe public-host DNS
// resolution do() otherwise requires (see the identical technique in
// TestCertHTTPTransportPresentsClientCertificate). The TLS config under
// test — including, for sbpHTTP, the client certificate — is unchanged;
// only where the socket connects is swapped for the test.
func testTLSTransport(t *testing.T, server *httptest.Server) *httpTransport {
	t.Helper()
	transport := newHTTPTransport()
	addr := server.Listener.Addr().String()
	httpTransportImpl := transport.client.Transport.(*http.Transport)
	httpTransportImpl.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	httpTransportImpl.TLSClientConfig.InsecureSkipVerify = true
	return transport
}

func TestYooKassaCreateStatusRefundRoundTrip(t *testing.T) {
	var lastIdempotenceKey string
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/payments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Has("created_at.gte") {
				_ = json.NewEncoder(w).Encode(map[string]any{"type": "list", "items": []any{map[string]any{
					"id": "2019-payment", "status": "succeeded",
					"amount":        map[string]string{"value": "150.00", "currency": "RUB"},
					"income_amount": map[string]string{"value": "147.00", "currency": "RUB"},
					"created_at":    "2026-08-30T09:00:00Z",
				}}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "2019-payment", "status": "succeeded",
				"amount":        map[string]string{"value": "150.00", "currency": "RUB"},
				"income_amount": map[string]string{"value": "147.00", "currency": "RUB"},
				"created_at":    time.Now().UTC().Format(time.RFC3339),
			})
			return
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != "shop_1" || pass != "secret_1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		lastIdempotenceKey = r.Header.Get("Idempotence-Key")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "2019-payment", "status": "pending",
			"amount":       map[string]string{"value": "150.00", "currency": "RUB"},
			"confirmation": map[string]string{"type": "redirect", "confirmation_url": "https://yookassa.ru/checkout/pay/xyz"},
			"created_at":   time.Now().UTC().Format(time.RFC3339),
		})
	})
	mux.HandleFunc("/v3/payments/2019-payment", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "2019-payment", "status": "succeeded",
			"amount":        map[string]string{"value": "150.00", "currency": "RUB"},
			"income_amount": map[string]string{"value": "147.00", "currency": "RUB"},
			"created_at":    time.Now().UTC().Format(time.RFC3339),
		})
	})
	mux.HandleFunc("/v3/refunds", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "list", "items": []any{map[string]any{
				"id": "refund-1", "payment_id": "2019-payment", "status": "succeeded",
				"amount": map[string]string{"value": "50.00", "currency": "RUB"}, "created_at": "2026-08-30T09:15:00Z",
			}}})
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			PaymentID string `json:"payment_id"`
		}
		_ = json.Unmarshal(body, &req)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "refund-1", "payment_id": req.PaymentID, "status": "succeeded", "amount": map[string]string{"value": "50.00", "currency": "RUB"}, "created_at": time.Now().UTC().Format(time.RFC3339)})
	})
	server := httptest.NewTLSServer(mux)
	defer server.Close()

	client := yookassaHTTP{h: testTLSTransport(t, server)}
	secret := []byte("shop_1\nsecret_1")

	created, err := client.Create(context.Background(), secret, sdk.PaymentCreateRequest{
		ExternalID: "order-1", IdempotencyKey: "idem-1", Purpose: "Order #1",
		Amount: sdk.PaymentAmount{MinorUnits: 15000, Currency: "RUB"}, ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.RemoteID != "2019-payment" || created.PaymentURL != "https://yookassa.ru/checkout/pay/xyz" {
		t.Fatalf("unexpected create result: %+v", created)
	}
	if lastIdempotenceKey != "idem-1" {
		t.Fatalf("Idempotence-Key not forwarded: got %q", lastIdempotenceKey)
	}

	status, err := client.Status(context.Background(), secret, sdk.PaymentStatusRequest{RemoteID: "2019-payment"})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Status != "succeeded" || status.Amount.MinorUnits != 15000 || status.CommissionMinorUnits != 300 {
		t.Fatalf("unexpected status result: %+v", status)
	}

	refund, err := client.Refund(context.Background(), secret, sdk.PaymentRefundRequest{
		RemotePaymentID: "2019-payment", ExternalID: "refund-order-1", IdempotencyKey: "idem-2", Amount: sdk.PaymentAmount{MinorUnits: 5000, Currency: "RUB"},
	})
	if err != nil {
		t.Fatalf("Refund: %v", err)
	}
	if refund.RemoteRefundID != "refund-1" || refund.Status != "succeeded" {
		t.Fatalf("unexpected refund result: %+v", refund)
	}

	reconciled, err := client.Reconcile(context.Background(), secret, sdk.PaymentReconcileRequest{From: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(reconciled.Items) != 2 || reconciled.Items[0].Kind != "sale" || reconciled.Items[1].Kind != "refund" {
		t.Fatalf("unexpected reconciliation result: %+v", reconciled.Items)
	}

	notification, _ := json.Marshal(map[string]any{"type": "notification", "event": "payment.succeeded", "object": map[string]any{"id": "2019-payment", "status": "succeeded", "amount": map[string]string{"value": "150.00", "currency": "RUB"}, "created_at": time.Now().UTC().Format(time.RFC3339)}})
	deliveryID, eventType, remoteID, err := client.VerifyWebhook(context.Background(), secret, notification, []byte("unused"))
	if err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}
	if remoteID != "2019-payment" || eventType != "payment_succeeded" || len(deliveryID) != 32 {
		t.Fatalf("unexpected webhook verification: delivery=%q event=%q remote=%q", deliveryID, eventType, remoteID)
	}

	// A byte-identical redelivery must produce the same deliveryID so the
	// repository's ON CONFLICT DO NOTHING dedups it.
	deliveryID2, _, _, err := client.VerifyWebhook(context.Background(), secret, notification, []byte("unused"))
	if err != nil || deliveryID2 != deliveryID {
		t.Fatalf("redelivery did not produce a stable deliveryID: %q vs %q (err=%v)", deliveryID, deliveryID2, err)
	}
}

func TestYooKassaVerifyWebhookIgnoresUnverifiedBodyStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/payments/2019-payment", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// The authoritative record disagrees with what the (attacker-controlled)
		// notification body will claim below.
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "2019-payment", "status": "pending", "amount": map[string]string{"value": "150.00", "currency": "RUB"}, "created_at": time.Now().UTC().Format(time.RFC3339)})
	})
	server := httptest.NewTLSServer(mux)
	defer server.Close()
	client := yookassaHTTP{h: testTLSTransport(t, server)}

	spoofed, _ := json.Marshal(map[string]any{"type": "notification", "event": "payment.succeeded", "object": map[string]any{"id": "2019-payment", "status": "succeeded", "amount": map[string]string{"value": "150.00", "currency": "RUB"}, "created_at": time.Now().UTC().Format(time.RFC3339)}})
	_, eventType, _, err := client.VerifyWebhook(context.Background(), []byte("shop_1\nsecret_1"), spoofed, []byte("x"))
	if err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}
	if eventType != "payment_pending" {
		t.Fatalf("VerifyWebhook trusted the unverified body status: got %q, want payment_pending (the authoritative status)", eventType)
	}
}

func TestRobokassaRefundUsesOpKeyAndPassword3(t *testing.T) {
	var refundPayloads []map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/Merchant/WebService/Service.asmx/OpStateExt", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Query().Get("MerchantLogin") != "shop_1" {
			t.Fatalf("unexpected OpStateExt request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<OperationStateResponse><Result><Code>0</Code></Result><State><Code>100</Code></State><Info><OutSum>150.00</OutSum><OpKey>0005F891-8CCD-434B-8455-816AFFFDBF37-0VOisWikFF</OpKey></Info></OperationStateResponse>`)
	})
	mux.HandleFunc("/RefundService/Refund/Create", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected refund request: %s %s", r.Method, r.URL.String())
		}
		body, _ := io.ReadAll(r.Body)
		var token string
		if err := json.Unmarshal(body, &token); err != nil {
			t.Fatalf("refund body is not a JSON string: %v", err)
		}
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			t.Fatalf("refund token has %d parts", len(parts))
		}
		payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			t.Fatalf("decode refund payload: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			t.Fatalf("decode refund payload JSON: %v", err)
		}
		refundPayloads = append(refundPayloads, payload)
		headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
		if err != nil {
			t.Fatalf("decode refund header: %v", err)
		}
		if string(headerBytes) != `{"alg":"HS256","typ":"JWT"}` {
			t.Fatalf("unexpected refund JWT header: %s", headerBytes)
		}
		mac := hmac.New(sha256.New, []byte("password_3"))
		_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
		if got := base64.RawURLEncoding.EncodeToString(mac.Sum(nil)); got != parts[2] {
			t.Fatalf("refund JWT signature mismatch")
		}
		w.Header().Set("Content-Type", "application/json")
		if len(refundPayloads) == 1 {
			_, _ = io.WriteString(w, `{"success":true,"requestId":"refund-request-full"}`)
			return
		}
		_, _ = io.WriteString(w, `{"success":true,"requestId":"refund-request-partial"}`)
	})
	server := httptest.NewTLSServer(mux)
	defer server.Close()

	client := robokassaHTTP{h: testTLSTransport(t, server)}
	secret := []byte("shop_1\npassword_1\npassword_2\npassword_3")
	full, err := client.Refund(context.Background(), secret, sdk.PaymentRefundRequest{
		RemotePaymentID: "1932809606", ExternalID: "refund-full", IdempotencyKey: "refund-full",
		Amount: sdk.PaymentAmount{MinorUnits: 15000, Currency: "RUB"},
	})
	if err != nil {
		t.Fatalf("full Refund: %v", err)
	}
	if full.RemoteRefundID != "refund-request-full" || full.Status != "accepted" {
		t.Fatalf("unexpected full refund result: %+v", full)
	}
	partial, err := client.Refund(context.Background(), secret, sdk.PaymentRefundRequest{
		RemotePaymentID: "1932809606", ExternalID: "refund-partial", IdempotencyKey: "refund-partial",
		Amount: sdk.PaymentAmount{MinorUnits: 5000, Currency: "RUB"},
	})
	if err != nil {
		t.Fatalf("partial Refund: %v", err)
	}
	if partial.RemoteRefundID != "refund-request-partial" || partial.Status != "accepted" {
		t.Fatalf("unexpected partial refund result: %+v", partial)
	}
	if len(refundPayloads) != 2 || refundPayloads[0]["OpKey"] != "0005F891-8CCD-434B-8455-816AFFFDBF37-0VOisWikFF" || refundPayloads[0]["RefundSum"] != nil {
		t.Fatalf("unexpected full refund payload: %+v", refundPayloads[0])
	}
	if refundPayloads[1]["OpKey"] != "0005F891-8CCD-434B-8455-816AFFFDBF37-0VOisWikFF" || refundPayloads[1]["RefundSum"] != 50.0 {
		t.Fatalf("unexpected partial refund payload: %+v", refundPayloads[1])
	}
}

func TestRobokassaRefundRequiresPassword3(t *testing.T) {
	client := robokassaHTTP{h: newHTTPTransport()}
	_, err := client.Refund(context.Background(), []byte("shop_1\npassword_1\npassword_2"), sdk.PaymentRefundRequest{
		RemotePaymentID: "1932809606", ExternalID: "refund", IdempotencyKey: "refund", Amount: sdk.PaymentAmount{MinorUnits: 100, Currency: "RUB"},
	})
	if err == nil || !strings.Contains(err.Error(), "password3") {
		t.Fatalf("Refund without Password3 error = %v", err)
	}
}

// SBP has no real, network-reachable gateway to test against in this
// environment (see the payments plan's "Known limitation" and ADR-0071's
// Operational impact clause) — do() correctly refuses to resolve a
// non-public test hostname, and that SSRF-safe DNS resolution is exactly the
// thing that must never be weakened for a test. mTLS itself is proven
// end-to-end in TestCertHTTPTransportPresentsClientCertificate; what's
// tested here is the SBP wire *contract* — the pure request/response
// marshaling sbpHTTP.Create/Status build on top of that proven transport.
func TestSBPRequestAndResponseContract(t *testing.T) {
	body, err := sbpCreateRequestBody(sdk.PaymentCreateRequest{
		ExternalID: "order-1", Purpose: "Order #1", Amount: sdk.PaymentAmount{MinorUnits: 15000, Currency: "RUB"},
	})
	if err != nil {
		t.Fatalf("sbpCreateRequestBody: %v", err)
	}
	var decoded sbpOrderRequest
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if decoded.Amount != 15000 || decoded.Currency != "RUB" || decoded.OrderID != "order-1" {
		t.Fatalf("unexpected request body: %+v", decoded)
	}

	order, err := parseSBPOrderResponse([]byte(`{"qrcId":"qrc-1","payload":"https://qr.example/pay/qrc-1"}`))
	if err != nil || order.QRCID != "qrc-1" || order.PayloadURL != "https://qr.example/pay/qrc-1" {
		t.Fatalf("parseSBPOrderResponse = %+v, %v", order, err)
	}
	if _, err := parseSBPOrderResponse([]byte(`{}`)); err == nil {
		t.Fatal("expected missing qrcId to be rejected")
	}

	status, err := parseSBPStatusResponse([]byte(`{"status":"paid","amount":15000,"currency":"RUB"}`))
	if err != nil || status.Status != "paid" || status.Amount != 15000 {
		t.Fatalf("parseSBPStatusResponse = %+v, %v", status, err)
	}
	if _, err := parseSBPStatusResponse([]byte(`{}`)); err == nil {
		t.Fatal("expected missing status to be rejected")
	}
}
