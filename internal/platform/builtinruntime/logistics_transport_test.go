package builtinruntime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dellin "github.com/torgnexa/torgnexa/connectors/logistics/dellin"
	pek "github.com/torgnexa/torgnexa/connectors/logistics/pek"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func TestCDEKCredentialProbe(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/oauth/token":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Fatalf("unexpected token request: method=%s content-type=%q", r.Method, r.Header.Get("Content-Type"))
			}
			if err := r.ParseForm(); err != nil || r.Form.Get("client_id") != "client-1" || r.Form.Get("client_secret") != "secret-1" || r.Form.Get("grant_type") != "client_credentials" {
				t.Fatalf("unexpected token form: %v", r.Form)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "temporary-token"})
		case "/v2/location/cities":
			if r.Method != http.MethodGet || r.URL.Query().Get("size") != "1" || r.Header.Get("Authorization") != "Bearer temporary-token" {
				t.Fatalf("unexpected cities request: method=%s query=%s authorization=%q", r.Method, r.URL.RawQuery, r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total": 0, "items": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	if err := transport.Ping(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`)); err != nil {
		t.Fatalf("CDEK probe failed: %v", err)
	}
	if err := transport.Ping(context.Background(), []byte(`{"client_id":"client-1"}`)); err == nil {
		t.Fatal("malformed CDEK credentials accepted")
	}
	if err := transport.Ping(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1","unexpected":"value"}`)); err == nil {
		t.Fatal("unknown CDEK credential field accepted")
	}
}

func TestCDEKPickupPointsAreBoundedAndNormalized(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "pickup-token"})
		case "/v2/deliverypoints":
			if r.Method != http.MethodGet || r.URL.Query().Get("country_code") != "RU" || r.URL.Query().Get("city") != "Москва" || r.URL.Query().Get("size") != "2" || r.Header.Get("Authorization") != "Bearer pickup-token" {
				t.Fatalf("unexpected CDEK delivery-point request: method=%s query=%s authorization=%q", r.Method, r.URL.RawQuery, r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"code": 137, "uuid": "office-uuid-1", "name": "ПВЗ на Тверской", "is_closed": false, "location": map[string]any{"country_code": "RU", "city": "Москва", "address": "Тверская, 1"}},
				{"code": "138", "address": "Ленина, 2", "location": map[string]any{"country_code": "RU", "city": "Москва"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	points, err := transport.Pickup(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), sdk.PickupPointQuery{Country: "RU", City: "Москва", Limit: 2})
	if err != nil {
		t.Fatalf("CDEK delivery-point request failed: %v", err)
	}
	if len(points) != 2 || points[0].RemoteID != "office-uuid-1" || points[0].Name != "ПВЗ на Тверской" || points[0].Address != "Тверская, 1" || !points[0].Active {
		t.Fatalf("unexpected first normalized point: %+v", points)
	}
	if points[1].RemoteID != "138" || points[1].Name != "СДЭК ПВЗ 138" || points[1].Address != "Ленина, 2" {
		t.Fatalf("unexpected fallback normalized point: %+v", points[1])
	}
}

func TestCDEKPickupPointsRejectIncompleteResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/oauth/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "pickup-token"})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"code": 137, "location": map[string]any{"country_code": "RU", "city": "Москва"}}})
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	if _, err := transport.Pickup(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), sdk.PickupPointQuery{Country: "RU", City: "Москва", Limit: 1}); err == nil {
		t.Fatal("incomplete CDEK delivery-point response accepted")
	}
}

func TestCDEKRatesUseBoundedTariffListAndExactMoney(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "rate-token"})
		case "/v2/calculator/tarifflist":
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer rate-token" || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected CDEK tariff request: method=%s authorization=%q content-type=%q", r.Method, r.Header.Get("Authorization"), r.Header.Get("Content-Type"))
			}
			var request cdekRateRequest
			if json.NewDecoder(r.Body).Decode(&request) != nil || request.From.CountryCode != "RU" || request.From.PostalCode != "101000" || request.From.City != "Москва" || request.To.City != "Санкт-Петербург" || len(request.Packages) != 1 || request.Packages[0].Length != 11 || request.Packages[0].Width != 10 || request.Packages[0].Height != 10 {
				t.Fatalf("unexpected CDEK tariff body: %+v", request)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tariff_codes": []any{
				map[string]any{"tariff_code": 136, "total_sum": "123.45", "currency": "RUB", "period_min": 1, "period_max": 2},
				map[string]any{"tariff_code": "137", "delivery_sum": 9.5, "currency": "RUB", "period_min": 0, "period_max": 1},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	request := sdk.RateRequest{
		From:    sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Тверская, 1"},
		To:      sdk.Address{Country: "RU", PostalCode: "190000", City: "Санкт-Петербург", Line1: "Невский, 1"},
		Parcels: []sdk.Parcel{{WeightGrams: 1000, LengthMM: 101, WidthMM: 100, HeightMM: 100}},
	}
	quotes, err := transport.Rates(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), request)
	if err != nil {
		t.Fatalf("CDEK rate request failed: %v", err)
	}
	if len(quotes) != 2 || quotes[0].ServiceCode != "cdek_tariff_136" || quotes[0].Cost.MinorUnits != 12345 || quotes[1].Cost.MinorUnits != 950 {
		t.Fatalf("unexpected normalized CDEK quotes: %+v", quotes)
	}
}

func TestCDEKRatesRejectDuplicateTariffs(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/oauth/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "rate-token"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tariff_codes": []any{
			map[string]any{"tariff_code": 136, "total_sum": 1, "currency": "RUB", "period_min": 1, "period_max": 1},
			map[string]any{"tariff_code": 136, "total_sum": 2, "currency": "RUB", "period_min": 1, "period_max": 1},
		}})
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	request := sdk.RateRequest{From: sdk.Address{Country: "RU", City: "Москва", Line1: "Тверская, 1"}, To: sdk.Address{Country: "RU", City: "Москва", Line1: "Тверская, 2"}, Parcels: []sdk.Parcel{{WeightGrams: 1, LengthMM: 1, WidthMM: 1, HeightMM: 1}}}
	if _, err := transport.Rates(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), request); err == nil {
		t.Fatal("duplicate CDEK tariffs accepted")
	}
}

func TestCDEKShipmentCreationUsesOfficialOrderPayload(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "create-token"})
		case "/v2/orders":
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer create-token" || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected CDEK create request: method=%s authorization=%q content-type=%q", r.Method, r.Header.Get("Authorization"), r.Header.Get("Content-Type"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || strings.Contains(string(body), "secret-1") {
				t.Fatalf("credential leaked into CDEK create request: %s", body)
			}
			var request cdekCreateOrder
			if json.Unmarshal(body, &request) != nil || request.Type != 1 || request.Number != "order-17" || request.TariffCode != 136 || request.DeliveryPoint != "pvz-137" || request.FromLocation.City != "Москва" || request.ToLocation.City != "Санкт-Петербург" || request.Recipient.Name != "Иван Петров" || len(request.Recipient.Phones) != 1 || request.Recipient.Phones[0].Number != "+79991234567" || len(request.Packages) != 1 || request.Packages[0].Weight != 1000 || request.Packages[0].Length != 10 {
				t.Fatalf("unexpected CDEK create body: %s", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"entity": map[string]any{"uuid": "72753031-1820-4f99-9240-aab139f05ca5", "cdek_number": "1100285492", "number": "order-17"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	request := sdk.ShipmentCreateRequest{
		ExternalID: "order-17", ServiceCode: "cdek_tariff_136", IdempotencyKey: "create-17",
		From:    sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Тверская, 1"},
		To:      sdk.Address{Country: "RU", PostalCode: "190000", City: "Санкт-Петербург", Line1: "Невский, 1"},
		Parcels: []sdk.Parcel{{WeightGrams: 1000, LengthMM: 100, WidthMM: 100, HeightMM: 100}}, PickupPointRef: "pvz-137",
		Sender: sdk.LogisticsContact{Name: "ООО Торгнекса", Phone: "+74951234567"}, Recipient: sdk.LogisticsContact{Name: "Иван Петров", Phone: "+79991234567", Email: "ivan@example.com"},
	}
	result, err := transport.Create(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), request)
	if err != nil {
		t.Fatalf("CDEK create request failed: %v", err)
	}
	if result.RemoteID != "1100285492" || result.Status != "created" || result.TrackingNumber != "1100285492" || result.Cost.Currency != "RUB" || result.Cost.MinorUnits != 0 || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized CDEK shipment: %+v", result)
	}
}

func TestCDEKShipmentCreationRejectsUnqualifiedServiceCodeAndMalformedResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/oauth/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "create-token"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"entity": map[string]any{"number": "order-17"}})
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	request := sdk.ShipmentCreateRequest{ExternalID: "order-17", ServiceCode: "remote_tariff_136", IdempotencyKey: "create-17", From: sdk.Address{Country: "RU", City: "Москва", Line1: "Тверская, 1"}, To: sdk.Address{Country: "RU", City: "Казань", Line1: "Баумана, 1"}, Parcels: []sdk.Parcel{{WeightGrams: 1, LengthMM: 1, WidthMM: 1, HeightMM: 1}}, Sender: sdk.LogisticsContact{Name: "ООО Торгнекса", Phone: "+74951234567"}, Recipient: sdk.LogisticsContact{Name: "Иван Петров", Phone: "+79991234567"}}
	if _, err := transport.Create(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), request); err == nil {
		t.Fatal("unqualified CDEK service code accepted")
	}
	request.ServiceCode = "cdek_tariff_136"
	if _, err := transport.Create(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), request); err == nil {
		t.Fatal("malformed CDEK create response accepted")
	}
}

func TestCDEKTrackingReadsLatestStatusWithoutExposingCredentials(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/oauth/token":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected CDEK token method: %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tracking-token"})
		case "/v2/orders":
			if r.Method != http.MethodGet || r.URL.Query().Get("cdek_number") != "1100285492" || r.URL.Query().Get("uuid") != "" || r.Header.Get("Authorization") != "Bearer tracking-token" {
				t.Fatalf("unexpected CDEK tracking request: method=%s query=%s authorization=%q", r.Method, r.URL.RawQuery, r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"entity": map[string]any{
				"uuid":        "72753031-1820-4f99-9240-aab139f05ca5",
				"cdek_number": "1100285492",
				"number":      "store-order-17",
				"statuses": []any{
					map[string]any{"code": "DELIVERED", "date_time": "2026-08-30T07:05:06Z"},
					map[string]any{"code": "ACCEPTED", "date_time": "2026-08-30T06:00:00+0000"},
				},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	result, err := transport.Track(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), sdk.ShipmentStatusRequest{RemoteID: "1100285492"})
	if err != nil {
		t.Fatalf("CDEK tracking request failed: %v", err)
	}
	if result.RemoteID != "1100285492" || result.Status != "DELIVERED" || result.TrackingNumber != "1100285492" || result.ObservedAt.Format(time.RFC3339) != "2026-08-30T07:05:06Z" || result.Cost.Currency != "RUB" || result.Cost.MinorUnits != 0 {
		t.Fatalf("unexpected normalized CDEK tracking result: %+v", result)
	}
}

func TestCDEKTrackingUsesUUIDQueryAndRejectsInvalidStatusHistory(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/oauth/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tracking-token"})
			return
		}
		if r.URL.Path != "/v2/orders" || r.URL.Query().Get("uuid") != "72753031-1820-4f99-9240-aab139f05ca5" {
			t.Fatalf("unexpected UUID tracking request: path=%s query=%s", r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"entity": map[string]any{"uuid": "72753031-1820-4f99-9240-aab139f05ca5", "statuses": []any{map[string]any{"code": "", "date_time": "not-a-time"}}}})
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	if _, err := transport.Track(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), sdk.ShipmentStatusRequest{RemoteID: "72753031-1820-4f99-9240-aab139f05ca5"}); err == nil {
		t.Fatal("invalid CDEK status history accepted")
	}
}

func TestCDEKWebhookReverifiesOrderAndIgnoresClaimedStatus(t *testing.T) {
	const eventUUID = "82753031-1820-4f99-9240-aab139f05ca5"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "webhook-token"})
		case "/v2/orders":
			if r.Method != http.MethodGet || r.URL.Query().Get("cdek_number") != "1100285492" || r.Header.Get("Authorization") != "Bearer webhook-token" {
				t.Fatalf("unexpected CDEK webhook verification request: method=%s query=%s authorization=%q", r.Method, r.URL.RawQuery, r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"entity": map[string]any{
				"uuid": "72753031-1820-4f99-9240-aab139f05ca5", "cdek_number": "1100285492",
				"statuses": []any{map[string]any{"code": "DELIVERED", "date_time": "2026-08-30T07:05:06Z"}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	body := []byte(`{"type":"ORDER_STATUS","uuid":"` + eventUUID + `","date_time":"2026-08-30T07:00:00+0000","attributes":{"cdek_number":"1100285492","code":"CANCELLED","status_code":"9"}}`)
	result, err := transport.Webhook(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), body, nil)
	if err != nil {
		t.Fatalf("CDEK webhook verification failed: %v", err)
	}
	if result.DeliveryID != eventUUID || result.RemoteID != "1100285492" || result.Status != "DELIVERED" || result.OccurredAt.Format(time.RFC3339) != "2026-08-30T07:05:06Z" {
		t.Fatalf("unexpected verified CDEK webhook: %+v", result)
	}
}

func TestCDEKWebhookRejectsNonStatusEvent(t *testing.T) {
	transport := cdekHTTP{h: &httpTransport{client: http.DefaultClient}}
	if _, err := transport.Webhook(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), []byte(`{"type":"PRINT_FORM","uuid":"82753031-1820-4f99-9240-aab139f05ca5","attributes":{"cdek_number":"1100285492"}}`), nil); err == nil {
		t.Fatal("non-status CDEK webhook event accepted")
	}
}

func TestCDEKCancelResolvesNumberAndDeletesByUUID(t *testing.T) {
	const uuid = "72753031-1820-4f99-9240-aab139f05ca5"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "cancel-token"})
		case "/v2/orders":
			if r.Method != http.MethodGet || r.URL.Query().Get("cdek_number") != "1100285492" || r.Header.Get("Authorization") != "Bearer cancel-token" {
				t.Fatalf("unexpected CDEK cancellation lookup: method=%s query=%s authorization=%q", r.Method, r.URL.RawQuery, r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"entity": map[string]any{"uuid": uuid, "cdek_number": "1100285492"}})
		case "/v2/orders/" + uuid:
			if r.Method != http.MethodDelete || r.Header.Get("Authorization") != "Bearer cancel-token" {
				t.Fatalf("unexpected CDEK cancellation request: method=%s authorization=%q", r.Method, r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	result, err := transport.Cancel(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), sdk.ShipmentCancelRequest{RemoteID: "1100285492", IdempotencyKey: "cancel-1"})
	if err != nil {
		t.Fatalf("CDEK cancellation failed: %v", err)
	}
	if result.RemoteID != "1100285492" || result.Status != "cancelled" || result.TrackingNumber != "1100285492" || result.Cost.Currency != "RUB" || !result.ObservedAt.After(time.Time{}) {
		t.Fatalf("unexpected normalized CDEK cancellation result: %+v", result)
	}
}

func TestCDEKCancelRejectsMismatchedLookup(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/oauth/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "cancel-token"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"entity": map[string]any{"uuid": "72753031-1820-4f99-9240-aab139f05ca5", "cdek_number": "other-number"}})
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	if _, err := transport.Cancel(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), sdk.ShipmentCancelRequest{RemoteID: "1100285492", IdempotencyKey: "cancel-1"}); err == nil {
		t.Fatal("CDEK cancellation accepted a mismatched lookup")
	}
}

func TestCDEKRefusalUsesOfficialReturnEndpoint(t *testing.T) {
	const orderUUID = "72753031-1820-4f99-9240-aab139f05ca5"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/oauth/token":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected CDEK token method: %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "return-token"})
		case "/v2/orders/" + orderUUID + "/refusal":
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer return-token" || r.Header.Get("Accept") != "application/json" {
				t.Fatalf("unexpected CDEK refusal request: method=%s authorization=%q accept=%q", r.Method, r.Header.Get("Authorization"), r.Header.Get("Accept"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || len(body) != 0 {
				t.Fatalf("CDEK refusal must not invent a request body: %q", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"entity": map[string]any{"uuid": orderUUID, "cdek_number": "1100285492"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	result, err := transport.Return(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), sdk.ReturnCreateRequest{OriginalRemoteID: orderUUID, ExternalID: "return-17", MailType: "refusal", IdempotencyKey: "return-17"})
	if err != nil {
		t.Fatalf("CDEK refusal request failed: %v", err)
	}
	if result.RemoteID != "1100285492" || result.Status != "created" || result.TrackingNumber != "1100285492" || result.Cost.Currency != "RUB" || !result.ObservedAt.After(time.Time{}) {
		t.Fatalf("unexpected normalized CDEK refusal result: %+v", result)
	}
}

func TestCDEKRefusalRejectsMismatchedProviderResponse(t *testing.T) {
	const orderUUID = "72753031-1820-4f99-9240-aab139f05ca5"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/oauth/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "return-token"})
			return
		}
		if r.URL.Path != "/v2/orders/"+orderUUID+"/refusal" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"entity": map[string]any{"uuid": "82753031-1820-4f99-9240-aab139f05ca5", "cdek_number": "1100285492"}})
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	if _, err := transport.Return(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), sdk.ReturnCreateRequest{OriginalRemoteID: orderUUID, ExternalID: "return-17", MailType: "refusal", IdempotencyKey: "return-17"}); err == nil {
		t.Fatal("CDEK refusal accepted a mismatched provider response")
	}
}

func TestCDEKClientReturnUsesOfficialEndpointAndTariff(t *testing.T) {
	const orderUUID = "72753031-1820-4f99-9240-aab139f05ca5"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "return-token"})
		case "/v2/orders/" + orderUUID + "/clientReturn":
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer return-token" || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected CDEK client return request: method=%s authorization=%q content-type=%q", r.Method, r.Header.Get("Authorization"), r.Header.Get("Content-Type"))
			}
			var body struct {
				TariffCode int `json:"tariff_code"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TariffCode != 136 {
				t.Fatalf("unexpected CDEK client return body: %+v err=%v", body, err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"entity": map[string]any{"uuid": orderUUID, "cdek_number": "1100285492"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	result, err := transport.Return(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), sdk.ReturnCreateRequest{OriginalRemoteID: orderUUID, ExternalID: "return-18", MailType: "client_return", TariffCode: 136, IdempotencyKey: "return-18"})
	if err != nil {
		t.Fatalf("CDEK client return request failed: %v", err)
	}
	if result.RemoteID != "1100285492" || result.Status != "created" || result.TrackingNumber != "1100285492" || result.Cost.Currency != "RUB" || !result.ObservedAt.After(time.Time{}) {
		t.Fatalf("unexpected normalized CDEK client return result: %+v", result)
	}
}

func TestCDEKLabelCreatesBarcodePrintRequest(t *testing.T) {
	const orderUUID = "72753031-1820-4f99-9240-aab139f05ca5"
	const printUUID = "82753031-1820-4f99-9240-aab139f05ca5"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "label-token"})
		case "/v2/orders":
			if r.Method != http.MethodGet || r.URL.Query().Get("cdek_number") != "1100285492" || r.Header.Get("Authorization") != "Bearer label-token" {
				t.Fatalf("unexpected CDEK label lookup: method=%s query=%s authorization=%q", r.Method, r.URL.RawQuery, r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"entity": map[string]any{"uuid": orderUUID, "cdek_number": "1100285492"}})
		case "/v2/print/barcodes":
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer label-token" || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected CDEK label request: method=%s authorization=%q content-type=%q", r.Method, r.Header.Get("Authorization"), r.Header.Get("Content-Type"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || strings.Contains(string(body), "secret-1") {
				t.Fatalf("credential leaked into CDEK label request: %s", body)
			}
			var request cdekPrintRequest
			if json.Unmarshal(body, &request) != nil || len(request.Orders) != 1 || request.Orders[0].OrderUUID != orderUUID || request.CopyCount != 1 || request.Format != "A6" {
				t.Fatalf("unexpected CDEK label body: %s", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"entity": map[string]any{"uuid": printUUID, "url": "https://api.cdek.ru/v2/print/barcodes/" + printUUID + ".pdf"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	result, err := transport.Label(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), sdk.LabelRequest{RemoteID: "1100285492", Format: "a6"})
	if err != nil {
		t.Fatalf("CDEK label request failed: %v", err)
	}
	if result.ArtifactRef != "cdek:print:barcode:"+printUUID || result.MediaType != "application/pdf" || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized CDEK label result: %+v", result)
	}
}

func TestCDEKLabelRejectsMismatchedLookupOrMalformedResponse(t *testing.T) {
	t.Run("mismatched lookup", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2/oauth/token" {
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "label-token"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"entity": map[string]any{"uuid": "72753031-1820-4f99-9240-aab139f05ca5", "cdek_number": "other-number"}})
		}))
		defer server.Close()
		transport := cdekHTTP{h: testTLSTransport(t, server)}
		if _, err := transport.Label(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), sdk.LabelRequest{RemoteID: "1100285492", Format: "pdf"}); err == nil {
			t.Fatal("CDEK label accepted a mismatched lookup")
		}
	})
	t.Run("malformed print response", func(t *testing.T) {
		const orderUUID = "72753031-1820-4f99-9240-aab139f05ca5"
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2/oauth/token" {
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "label-token"})
				return
			}
			if r.URL.Path != "/v2/print/barcodes" || r.Method != http.MethodPost {
				t.Fatalf("unexpected CDEK print request: method=%s path=%s", r.Method, r.URL.Path)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"entity": map[string]any{"number": orderUUID}})
		}))
		defer server.Close()
		transport := cdekHTTP{h: testTLSTransport(t, server)}
		if _, err := transport.Label(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`), sdk.LabelRequest{RemoteID: orderUUID, Format: "pdf"}); err == nil {
			t.Fatal("malformed CDEK label response accepted")
		}
	})
}

func TestDellinCredentialProbe(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v4/auth/login.json" || r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected Деловые Линии request: method=%s path=%s content-type=%q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		var request struct {
			AppKey string `json:"appkey"`
			PAT    string `json:"pat"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.AppKey != "app-1" || request.PAT != "pat-1" {
			t.Fatalf("unexpected Деловые Линии body")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"status": 200}, "data": map[string]any{"sessionID": "session-only"}})
	}))
	defer server.Close()
	transport := dellinHTTP{h: testTLSTransport(t, server)}
	if err := transport.Ping(context.Background(), []byte(`{"appkey":"app-1","pat":"pat-1"}`)); err != nil {
		t.Fatalf("Деловые Линии probe failed: %v", err)
	}
	if err := transport.Ping(context.Background(), []byte(`{"appkey":"app-1"}`)); err == nil {
		t.Fatal("malformed Деловые Линии credentials accepted")
	}
}

func TestDellinRatesUseCalculatorAndExactMoney(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4/auth/login.json":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected Деловые Линии login request: method=%s content-type=%q", r.Method, r.Header.Get("Content-Type"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"status": 200}, "data": map[string]any{"sessionID": "session-1"}})
		case "/v2/calculator.json":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected Деловые Линии calculator request: method=%s content-type=%q", r.Method, r.Header.Get("Content-Type"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || strings.Contains(string(body), "pat-1") {
				t.Fatalf("PAT leaked into calculator request: %s", body)
			}
			var request dellinRateRequest
			if json.Unmarshal(body, &request) != nil || request.AppKey != "app-1" || request.SessionID != "session-1" || request.Delivery.DeliveryType.Type != "auto" || request.Delivery.Derival.Variant != "address" || request.Delivery.Arrival.Variant != "address" || request.Delivery.Derival.Address.Search != "RU, 101000, Москва, Тверская, 1" || request.Delivery.Arrival.Address.Search != "RU, 190000, Санкт-Петербург, Невский, 1" || request.Payment.Type != "cash" || request.Cargo.Quantity != 1 || request.Cargo.Length.String() != "0.1" || request.Cargo.Width.String() != "0.1" || request.Cargo.Height.String() != "0.1" || request.Cargo.Weight.String() != "1" || request.Cargo.TotalWeight.String() != "1" || request.Cargo.TotalVolume.String() != "0.001" {
				t.Fatalf("unexpected Деловые Линии calculator body: %s", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"metadata": map[string]any{"status": 200},
				"data":     map[string]any{"price": "1499.00", "priceMinimal": "auto", "deliveryTerm": 2, "orderDates": map[string]string{"arrivalToOspReceiver": "2026-09-02"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := dellinHTTP{h: testTLSTransport(t, server)}
	request := sdk.RateRequest{
		From:    sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Тверская, 1"},
		To:      sdk.Address{Country: "RU", PostalCode: "190000", City: "Санкт-Петербург", Line1: "Невский, 1"},
		Parcels: []sdk.Parcel{{WeightGrams: 1000, LengthMM: 100, WidthMM: 100, HeightMM: 100}},
	}
	rates, err := transport.Rates(context.Background(), []byte(`{"appkey":"app-1","pat":"pat-1"}`), request)
	if err != nil {
		t.Fatalf("Деловые Линии calculator request failed: %v", err)
	}
	if len(rates) != 1 || rates[0].ServiceCode != "dellin_auto" || rates[0].Cost.MinorUnits != 149900 || rates[0].Cost.Currency != "RUB" || !rates[0].MinDeliveryAt.Equal(time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected normalized Деловые Линии rate: %+v", rates)
	}
}

func TestDellinCancellationReturnsAcceptedPendingState(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4/auth/login.json":
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"status": 200}, "data": map[string]any{"sessionID": "cancel-session"}})
		case "/v3/orders/cancel_delivery.json":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected Деловые Линии cancellation request: method=%s content-type=%q", r.Method, r.Header.Get("Content-Type"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || strings.Contains(string(body), "pat-1") {
				t.Fatalf("PAT leaked into Деловые Линии cancellation request: %s", body)
			}
			var request struct {
				AppKey    string `json:"appkey"`
				SessionID string `json:"sessionID"`
				OrderID   int64  `json:"orderID"`
				Requester string `json:"requester"`
			}
			if json.Unmarshal(body, &request) != nil || request.AppKey != "app-1" || request.SessionID != "cancel-session" || request.OrderID != 3954004 || request.Requester != "sender" {
				t.Fatalf("unexpected Деловые Линии cancellation body: %s", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"status": 200}, "data": map[string]any{"status": "success"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := dellinHTTP{h: testTLSTransport(t, server)}
	result, err := transport.Cancel(context.Background(), []byte(`{"appkey":"app-1","pat":"pat-1"}`), sdk.ShipmentCancelRequest{RemoteID: "3954004", IdempotencyKey: "cancel-17"})
	if err != nil || result.RemoteID != "3954004" || result.Status != "cancellation_pending" || result.Cost.Currency != "RUB" || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized Деловые Линии cancellation: result=%+v err=%v", result, err)
	}
}

func TestDellinCancellationRejectsNonSuccessDecisionResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v4/auth/login.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"status": 200}, "data": map[string]any{"sessionID": "cancel-session"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"status": 200}, "data": map[string]any{"status": "rejected"}})
	}))
	defer server.Close()
	transport := dellinHTTP{h: testTLSTransport(t, server)}
	if _, err := transport.Cancel(context.Background(), []byte(`{"appkey":"app-1","pat":"pat-1"}`), sdk.ShipmentCancelRequest{RemoteID: "3954004", IdempotencyKey: "cancel-17"}); err == nil {
		t.Fatal("non-success Деловые Линии cancellation response accepted")
	}
}

func TestDellinRatesRejectMalformedCalculatorPrice(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v4/auth/login.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"status": 200}, "data": map[string]any{"sessionID": "session-1"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"status": 200}, "data": map[string]any{"price": "1.234", "priceMinimal": "auto", "deliveryTerm": 1}})
	}))
	defer server.Close()
	transport := dellinHTTP{h: testTLSTransport(t, server)}
	request := sdk.RateRequest{From: sdk.Address{Country: "RU", City: "Москва", Line1: "Тверская, 1"}, To: sdk.Address{Country: "RU", City: "Казань", Line1: "Баумана, 1"}, Parcels: []sdk.Parcel{{WeightGrams: 1, LengthMM: 1, WidthMM: 1, HeightMM: 1}}}
	if _, err := transport.Rates(context.Background(), []byte(`{"appkey":"app-1","pat":"pat-1"}`), request); err == nil {
		t.Fatal("malformed Деловые Линии calculator price accepted")
	}
}

func TestDellinPickupPointsReadCatalogAndBoundResults(t *testing.T) {
	badURL := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/public/terminals.json":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected Деловые Линии directory request: method=%s content-type=%q", r.Method, r.Header.Get("Content-Type"))
			}
			var request struct {
				AppKey string `json:"appkey"`
			}
			if json.NewDecoder(r.Body).Decode(&request) != nil || request.AppKey != "app-1" {
				t.Fatalf("unexpected Деловые Линии directory body")
			}
			catalogURL := "https://api.dellin.ru/catalog/terminals_v3.json?sk=directory-key&e=123"
			if badURL {
				catalogURL = "https://example.invalid/catalog/terminals_v3.json?sk=directory-key&e=123"
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"hash": "directory-hash", "url": catalogURL})
		case "/catalog/terminals_v3.json":
			if r.Method != http.MethodGet || r.URL.Query().Get("sk") != "directory-key" || r.URL.Query().Get("e") != "123" {
				t.Fatalf("unexpected Деловые Линии catalog request: method=%s query=%s", r.Method, r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"city": []any{
				map[string]any{"name": "Москва", "terminals": map[string]any{"terminal": []any{
					map[string]any{"id": "101", "name": "Терминал Москва", "address": "ул. Примерная, 1", "giveoutCargo": true},
					map[string]any{"id": "102", "name": "ПВЗ Москва", "fullAddress": "г. Москва, ул. Вторая, 2", "isPVZ": true},
					map[string]any{"id": "103", "name": "Офис без выдачи", "address": "ул. Третья, 3", "giveoutCargo": false, "isPVZ": false},
				}}},
				map[string]any{"name": "Казань", "terminals": map[string]any{"terminal": []any{map[string]any{"id": "201", "name": "ПВЗ Казань", "address": "ул. Четвёртая, 4", "isPVZ": true}}}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := dellinHTTP{h: testTLSTransport(t, server)}
	points, err := transport.Pickup(context.Background(), []byte(`{"appkey":"app-1","pat":"pat-1"}`), sdk.PickupPointQuery{Country: "RU", City: "Москва", Limit: 2})
	if err != nil {
		t.Fatalf("Деловые Линии pickup request failed: %v", err)
	}
	if len(points) != 2 || points[0].RemoteID != "101" || points[0].Address != "ул. Примерная, 1" || points[1].RemoteID != "102" || points[1].Address != "г. Москва, ул. Вторая, 2" {
		t.Fatalf("unexpected normalized Деловые Линии points: %+v", points)
	}
	badURL = true
	if _, err := transport.Pickup(context.Background(), []byte(`{"appkey":"app-1","pat":"pat-1"}`), sdk.PickupPointQuery{Country: "RU", City: "Москва", Limit: 2}); err == nil {
		t.Fatal("untrusted Деловые Линии catalog URL accepted")
	}
}

func TestDellinTrackingUsesBoundedStatusHistory(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v3/orders/statuses_history.json" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected Деловые Линии tracking request: method=%s path=%s content-type=%q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		var request struct {
			AppKey string   `json:"appkey"`
			DocIDs []string `json:"docIds"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.AppKey != "app-1" || len(request.DocIDs) != 1 || request.DocIDs[0] != "400267443" {
			t.Fatalf("unexpected Деловые Линии tracking body: %+v", request)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"metadata": map[string]any{"status": 200},
			"data": map[string]any{"statusHistory": map[string]any{"400267443": []any{
				map[string]any{"number": "400267443", "state": "waiting", "stateDate": "2026-08-28T03:00:00+03:00"},
				map[string]any{"number": "400267443", "state": "finished", "stateDate": "2026-08-29T03:00:00+03:00"},
			}}},
		})
	}))
	defer server.Close()
	transport := dellinHTTP{h: testTLSTransport(t, server)}
	result, err := transport.Track(context.Background(), []byte(`{"appkey":"app-1","pat":"pat-1"}`), sdk.ShipmentStatusRequest{RemoteID: "400267443"})
	if err != nil {
		t.Fatalf("Деловые Линии tracking request failed: %v", err)
	}
	wantObservedAt := time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)
	if result.RemoteID != "400267443" || result.TrackingNumber != "400267443" || result.Status != "delivered" || !result.ObservedAt.Equal(wantObservedAt) || result.Cost.Currency != "RUB" || result.Cost.MinorUnits != 0 {
		t.Fatalf("unexpected normalized Деловые Линии tracking result: %+v", result)
	}
}

func TestDellinTrackingRejectsMismatchedDocument(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"metadata": map[string]any{"status": 200},
			"data": map[string]any{"statusHistory": map[string]any{"400267443": []any{
				map[string]any{"number": "400267444", "state": "inway", "stateDate": "2026-08-28T03:00:00+03:00"},
			}}},
		})
	}))
	defer server.Close()
	transport := dellinHTTP{h: testTLSTransport(t, server)}
	if _, err := transport.Track(context.Background(), []byte(`{"appkey":"app-1","pat":"pat-1"}`), sdk.ShipmentStatusRequest{RemoteID: "400267443"}); err == nil {
		t.Fatal("mismatched Деловые Линии document accepted")
	}
}

func TestDellinLabelRequestsOfficialPrintableWaybill(t *testing.T) {
	pdf := []byte("%PDF-1.7\nsynthetic waybill\n%%EOF")
	encoded := base64.StdEncoding.EncodeToString(pdf)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4/auth/login.json":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected Деловые Линии login method: %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"status": 200}, "data": map[string]any{"sessionID": "label-session"}})
		case "/v1/printable.json":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected Деловые Линии printable request: method=%s content-type=%q", r.Method, r.Header.Get("Content-Type"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || strings.Contains(string(body), "pat-1") {
				t.Fatalf("PAT leaked into Деловые Линии printable request: %s", body)
			}
			var request struct {
				AppKey    string `json:"appkey"`
				SessionID string `json:"sessionID"`
				DocUID    string `json:"docUID"`
				Mode      string `json:"mode"`
			}
			if json.Unmarshal(body, &request) != nil || request.AppKey != "app-1" || request.SessionID != "label-session" || request.DocUID != "0xad339ac31247666145816f2aeb4935ab" || request.Mode != "order" {
				t.Fatalf("unexpected Деловые Линии printable body: %s", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"status": 200}, "data": []any{map[string]any{
				"uid": "0xad339ac31247666145816f2aeb4935ab", "base64": encoded, "url": []string{"https://assets.dellin.ru/private.pdf"},
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := dellinHTTP{h: testTLSTransport(t, server)}
	result, err := transport.Label(context.Background(), []byte(`{"appkey":"app-1","pat":"pat-1"}`), sdk.LabelRequest{RemoteID: "0xad339ac31247666145816f2aeb4935ab", Format: "pdf"})
	if err != nil {
		t.Fatalf("Деловые Линии label request failed: %v", err)
	}
	if !strings.HasPrefix(result.ArtifactRef, "dellin:printable:order:0xad339ac31247666145816f2aeb4935ab:") || result.MediaType != "application/pdf" || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized Деловые Линии label: %+v", result)
	}
}

func TestDellinLabelRejectsNonPDFDocument(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("not a pdf"))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v4/auth/login.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"status": 200}, "data": map[string]any{"sessionID": "label-session"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"status": 200}, "data": []any{map[string]any{"uid": "0xad339ac31247666145816f2aeb4935ab", "base64": encoded}}})
	}))
	defer server.Close()
	transport := dellinHTTP{h: testTLSTransport(t, server)}
	if _, err := transport.Label(context.Background(), []byte(`{"appkey":"app-1","pat":"pat-1"}`), sdk.LabelRequest{RemoteID: "0xad339ac31247666145816f2aeb4935ab", Format: "pdf"}); err == nil {
		t.Fatal("non-PDF Деловые Линии label accepted")
	}
}

func TestDellinShipmentCreationUsesOfficialRequestPayload(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4/auth/login.json":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected Деловые Линии login request: method=%s content-type=%q", r.Method, r.Header.Get("Content-Type"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"status": 200}, "data": map[string]any{"sessionID": "create-session"}})
		case "/v2/request.json":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected Деловые Линии create request: method=%s content-type=%q", r.Method, r.Header.Get("Content-Type"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || strings.Contains(string(body), "pat-1") {
				t.Fatalf("PAT leaked into Деловые Линии create request: %s", body)
			}
			var request dellinCreateRequest
			if json.Unmarshal(body, &request) != nil || request.AppKey != "app-1" || request.SessionID != "create-session" || !request.InOrder || request.Delivery.DeliveryType.Type != "auto" || request.Delivery.Derival.ProduceDate != "2026-09-15" || request.Delivery.Derival.Variant != "address" || request.Delivery.Derival.Address.Search != "RU, 101000, Москва, Тверская, 1" || request.Delivery.Derival.Time.WorktimeStart != "09:00" || request.Delivery.Derival.Time.WorktimeEnd != "18:00" || request.Delivery.Arrival.Variant != "address" || request.Delivery.Arrival.Address.Search != "RU, 190000, Санкт-Петербург, Невский, 1" || request.Members.Requester.Role != "sender" || request.Members.Requester.UID != "requester-1" || request.Members.Sender.CounteragentID != 123 || len(request.Members.Sender.ContactPersons) != 1 || request.Members.Sender.ContactPersons[0].Name != "ООО Торгнекса" || request.Members.Sender.PhoneNumbers[0].Number != "74951234567" || request.Members.Receiver.Counteragent == nil || !request.Members.Receiver.Counteragent.IsAnonym || request.Members.Receiver.Counteragent.Phone != "79991234567" || request.Members.Receiver.Counteragent.Name != "Иван Петров" || request.Cargo.Quantity != 1 || request.Cargo.Length.String() != "0.1" || request.Cargo.Width.String() != "0.1" || request.Cargo.Height.String() != "0.1" || request.Cargo.Weight.String() != "1" || request.Cargo.TotalWeight.String() != "1" || request.Cargo.TotalVolume.String() != "0.001" || request.Cargo.FreightUID != "freight-1" || request.Payment.Type != "cash" || request.Payment.PrimaryPayer != "sender" || request.CargoCode != "order-17" {
				t.Fatalf("unexpected Деловые Линии create body: %s", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"status": 200}, "data": map[string]any{"state": "success", "requestID": 3954004, "barcode": "41508460D0905400400000014"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := dellinHTTP{h: testTLSTransport(t, server)}
	result, err := transport.Create(context.Background(), []byte(`{"appkey":"app-1","pat":"pat-1"}`), sdk.ShipmentCreateRequest{
		ExternalID: "order-17", ServiceCode: "dellin_auto", IdempotencyKey: "create-17",
		From:    sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Тверская, 1"},
		To:      sdk.Address{Country: "RU", PostalCode: "190000", City: "Санкт-Петербург", Line1: "Невский, 1"},
		Parcels: []sdk.Parcel{{WeightGrams: 1000, LengthMM: 100, WidthMM: 100, HeightMM: 100}},
		Sender:  sdk.LogisticsContact{Name: "ООО Торгнекса", Phone: "+74951234567"}, Recipient: sdk.LogisticsContact{Name: "Иван Петров", Phone: "+79991234567"},
	}, dellin.Configuration{RequesterUID: "requester-1", SenderCounteragentID: 123, FreightUID: "freight-1", ProduceDate: "2026-09-15", DerivalWorktimeStart: "09:00", DerivalWorktimeEnd: "18:00", PaymentType: "cash"})
	if err != nil {
		t.Fatalf("Деловые Линии create request failed: %v", err)
	}
	if result.RemoteID != "3954004" || result.TrackingNumber != "41508460D0905400400000014" || result.Status != "created" || result.Cost.Currency != "RUB" {
		t.Fatalf("unexpected normalized Деловые Линии shipment: %+v", result)
	}
}

func TestPekPickupPointsReadBranchDirectoryAndFilterOperations(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/branches/all/" || r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json;charset=utf-8" {
			t.Fatalf("unexpected ПЭК branch request: method=%s path=%s content-type=%q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		user, password, ok := r.BasicAuth()
		if !ok || user != "user-1" || password != "password-1" {
			t.Fatalf("unexpected ПЭК basic credentials: user=%q password=%q ok=%v", user, password, ok)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"branches": []any{
			map[string]any{
				"title":  "Москва",
				"cities": []any{map[string]any{"title": "Москва", "divisions": []string{"division-1", "division-2"}}},
				"divisions": []any{
					map[string]any{"id": "division-1", "name": "Москва Центр", "warehouses": []any{
						map[string]any{"id": "warehouse-1", "name": "Москва 01", "addressDivision": "Россия, Москва, Тверская, 1", "kindsOfTransportation": []any{map[string]any{"operations": []string{"Выдача грузов"}}}},
					}},
					map[string]any{"id": "division-2", "name": "Москва только приём", "warehouses": []any{
						map[string]any{"id": "warehouse-2", "name": "Москва 02", "address": "Москва, Вторая, 2", "kindsOfTransportation": []any{map[string]any{"operations": []string{"Приём грузов"}}}},
					}},
				},
			},
		}})
	}))
	defer server.Close()
	transport := pekHTTP{h: testTLSTransport(t, server)}
	points, err := transport.Pickup(context.Background(), []byte(`{"username":"user-1","password":"password-1"}`), sdk.PickupPointQuery{Country: "RU", City: "Москва", Limit: 10})
	if err != nil {
		t.Fatalf("ПЭК pickup request failed: %v", err)
	}
	if len(points) != 1 || points[0].RemoteID != "warehouse-1" || points[0].Name != "Москва 01" || points[0].Address != "Россия, Москва, Тверская, 1" || !points[0].Active {
		t.Fatalf("unexpected normalized ПЭК points: %+v", points)
	}
}

func TestPekRatesUseAuthenticatedWarehouseCalculatorAndExactMoney(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "user-1" || password != "password-1" {
			t.Fatalf("unexpected ПЭК basic credentials: user=%q password=%q ok=%v", user, password, ok)
		}
		switch r.URL.Path {
		case "/api/v1/branches/all/":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json;charset=utf-8" {
				t.Fatalf("unexpected ПЭК branch request: method=%s content-type=%q", r.Method, r.Header.Get("Content-Type"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"branches": []any{
				map[string]any{
					"title": "Москва", "cities": []any{map[string]any{"title": "Москва", "divisions": []string{"moscow-division"}}},
					"divisions": []any{map[string]any{"id": "moscow-division", "name": "Москва", "warehouses": []any{
						map[string]any{"id": "warehouse-moscow", "name": "Москва", "address": "Москва, Тверская, 1", "kindsOfTransportation": []any{map[string]any{"operations": []string{"Выдача грузов"}}}},
					}}},
				},
				map[string]any{
					"title": "Санкт-Петербург", "cities": []any{map[string]any{"title": "Санкт-Петербург", "divisions": []string{"spb-division"}}},
					"divisions": []any{map[string]any{"id": "spb-division", "name": "Санкт-Петербург", "warehouses": []any{
						map[string]any{"id": "warehouse-spb", "name": "Санкт-Петербург", "address": "Санкт-Петербург, Невский, 1", "kindsOfTransportation": []any{map[string]any{"operations": []string{"Выдача грузов"}}}},
					}}},
				},
			}})
		case "/api/v1/calculator/calculateprice/":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json;charset=utf-8" {
				t.Fatalf("unexpected ПЭК calculator request: method=%s content-type=%q", r.Method, r.Header.Get("Content-Type"))
			}
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode ПЭК calculator request: %v", err)
			}
			var types []int
			var sender, receiver string
			var cargos []map[string]json.RawMessage
			if json.Unmarshal(payload["types"], &types) != nil || len(types) != 1 || types[0] != 3 ||
				json.Unmarshal(payload["senderWarehouseId"], &sender) != nil || sender != "warehouse-moscow" ||
				json.Unmarshal(payload["receiverWarehouseId"], &receiver) != nil || receiver != "warehouse-spb" ||
				json.Unmarshal(payload["cargos"], &cargos) != nil || len(cargos) != 1 ||
				string(cargos[0]["length"]) != "0.101" || string(cargos[0]["width"]) != "0.100" || string(cargos[0]["height"]) != "0.100" || string(cargos[0]["weight"]) != "1" {
				t.Fatalf("unexpected ПЭК calculator payload: %v", payload)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"hasError": false, "transfers": []any{
				map[string]any{"type": 3, "hasError": false, "costTotal": "1234.50", "estDeliveryTime": 3},
				map[string]any{"type": 1, "hasError": true, "errorMessage": "unsupported", "costTotal": 0, "estDeliveryTime": 0},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := pekHTTP{h: testTLSTransport(t, server)}
	request := sdk.RateRequest{
		From:    sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Тверская, 1"},
		To:      sdk.Address{Country: "RU", PostalCode: "190000", City: "Санкт-Петербург", Line1: "Невский, 1"},
		Parcels: []sdk.Parcel{{WeightGrams: 1000, LengthMM: 101, WidthMM: 100, HeightMM: 100}},
	}
	quotes, err := transport.Rates(context.Background(), []byte(`{"username":"user-1","password":"password-1"}`), request)
	if err != nil {
		t.Fatalf("ПЭК rate request failed: %v", err)
	}
	if len(quotes) != 1 || quotes[0].ServiceCode != "pek_type_3" || quotes[0].Cost.MinorUnits != 123450 || quotes[0].Cost.Currency != "RUB" {
		t.Fatalf("unexpected normalized ПЭК quotes: %+v", quotes)
	}
}

func TestPekTrackingUsesBasicStatusAndNormalizesRussianStatus(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cargos/basicstatus/" || r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json;charset=utf-8" {
			t.Fatalf("unexpected ПЭК tracking request: method=%s path=%s content-type=%q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		user, password, ok := r.BasicAuth()
		if !ok || user != "user-1" || password != "password-1" {
			t.Fatalf("unexpected ПЭК tracking credentials: user=%q password=%q ok=%v", user, password, ok)
		}
		var payload struct {
			CargoCodes []string `json:"cargoCodes"`
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil || len(payload.CargoCodes) != 1 || payload.CargoCodes[0] != "780339690775" {
			t.Fatalf("unexpected ПЭК tracking payload: %+v", payload)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"cargos": []any{
			map[string]any{
				"info":  map[string]any{"cargoStatus": "Выполняется адресная доставка"},
				"cargo": map[string]any{"code": "780339690775"},
			},
		}})
	}))
	defer server.Close()
	transport := pekHTTP{h: testTLSTransport(t, server)}
	result, err := transport.Track(context.Background(), []byte(`{"username":"user-1","password":"password-1"}`), sdk.ShipmentStatusRequest{RemoteID: "780339690775"})
	if err != nil {
		t.Fatalf("ПЭК tracking request failed: %v", err)
	}
	if result.RemoteID != "780339690775" || result.TrackingNumber != "780339690775" || result.Status != "on_delivery" || result.Cost.Currency != "RUB" || result.Cost.MinorUnits != 0 || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized ПЭК tracking result: %+v", result)
	}
}

func TestPekShipmentCreateUsesBoundedPreregristrationContract(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/preregistration/submit/" {
			t.Fatalf("unexpected ПЭК preregistration request: method=%s path=%s", r.Method, r.URL.Path)
		}
		user, password, ok := r.BasicAuth()
		if !ok || user != "user-1" || password != "password-1" {
			t.Fatalf("unexpected ПЭК preregistration credentials: user=%q password=%q ok=%v", user, password, ok)
		}
		var payload struct {
			Common struct {
				DocflowType string `json:"docflowType"`
				OrderType   int    `json:"orderType"`
			} `json:"common"`
			Sender struct {
				WarehouseID string `json:"warehouseId"`
			} `json:"sender"`
			Cargos []struct {
				Common struct {
					CustomerCorrelation string          `json:"customerCorrelation"`
					Type                int             `json:"type"`
					PositionsCount      int             `json:"positionsCount"`
					Weight              json.RawMessage `json:"weight"`
					Volume              json.RawMessage `json:"volume"`
					Width               json.RawMessage `json:"width"`
				} `json:"common"`
			} `json:"cargos"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode ПЭК preregistration request: %v", err)
		}
		cargo := payload.Cargos
		if payload.Common.DocflowType != "FFS" || payload.Common.OrderType != 0 || payload.Sender.WarehouseID != "abcd1234-0001" || len(cargo) != 1 || cargo[0].Common.CustomerCorrelation != "order:pek:1" || cargo[0].Common.Type != 3 || cargo[0].Common.PositionsCount != 1 || string(cargo[0].Common.Weight) != "1" || string(cargo[0].Common.Volume) != "0.001" || string(cargo[0].Common.Width) != "0.100" {
			t.Fatalf("unexpected ПЭК preregistration payload: %+v", payload)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"documentId": "doc-pek-1", "cargos": []any{map[string]any{"cargoCode": "780339690775"}}})
	}))
	defer server.Close()
	transport := pekHTTP{h: testTLSTransport(t, server)}
	request := sdk.ShipmentCreateRequest{
		ExternalID: "order:pek:1", ServiceCode: "pek_type_3", IdempotencyKey: "idem:pek:1",
		From:    sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Тверская, 1"},
		To:      sdk.Address{Country: "RU", PostalCode: "190000", City: "Санкт-Петербург", Line1: "Невский, 1"},
		Parcels: []sdk.Parcel{{WeightGrams: 1000, LengthMM: 100, WidthMM: 100, HeightMM: 100}},
		Sender:  sdk.LogisticsContact{Name: "Отправитель", Phone: "+79990000000"}, Recipient: sdk.LogisticsContact{Name: "Получатель", Phone: "+79990000001"},
	}
	result, err := transport.Create(context.Background(), []byte(`{"username":"user-1","password":"password-1"}`), request, pek.Configuration{SenderWarehouseID: "abcd1234-0001", SenderLegalForm: 1, SenderTitle: "ООО Пример", SenderINN: "7700000000", SenderKPP: "770001001"})
	if err != nil {
		t.Fatalf("ПЭК shipment creation failed: %v", err)
	}
	if result.RemoteID != "780339690775" || result.Status != "created" || result.TrackingNumber != result.RemoteID || result.Cost.Currency != "RUB" || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized ПЭК shipment: %+v", result)
	}
}

func TestPekShipmentCreateRejectsResponseWithoutDocument(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"cargos": []any{map[string]any{"cargoCode": "780339690775"}}})
	}))
	defer server.Close()
	transport := pekHTTP{h: testTLSTransport(t, server)}
	request := sdk.ShipmentCreateRequest{
		ExternalID: "order:pek:2", ServiceCode: "pek_type_3", IdempotencyKey: "idem:pek:2",
		From: sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Тверская, 1"}, To: sdk.Address{Country: "RU", PostalCode: "190000", City: "Санкт-Петербург", Line1: "Невский, 1"},
		Parcels: []sdk.Parcel{{WeightGrams: 1000, LengthMM: 100, WidthMM: 100, HeightMM: 100}}, Sender: sdk.LogisticsContact{Name: "Отправитель", Phone: "+79990000000"}, Recipient: sdk.LogisticsContact{Name: "Получатель", Phone: "+79990000001"},
	}
	configuration := pek.Configuration{SenderWarehouseID: "abcd1234-0001", SenderLegalForm: 1, SenderTitle: "ООО Пример", SenderINN: "7700000000"}
	if _, err := transport.Create(context.Background(), []byte(`{"username":"user-1","password":"password-1"}`), request, configuration); err == nil {
		t.Fatal("ПЭК preregistration response without document id accepted")
	}
}

func TestPekTrackingRejectsMalformedResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"cargos": []any{map[string]any{"info": map[string]any{"cargoStatus": "В пути"}}}})
	}))
	defer server.Close()
	transport := pekHTTP{h: testTLSTransport(t, server)}
	if _, err := transport.Track(context.Background(), []byte(`{"username":"user-1","password":"password-1"}`), sdk.ShipmentStatusRequest{RemoteID: "780339690775"}); err == nil {
		t.Fatal("malformed ПЭК tracking response accepted")
	}
}

func TestOzonDeliveryCredentialProbe(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/warehouse/list" || r.Method != http.MethodPost || r.Header.Get("Client-Id") != "client-1" || r.Header.Get("Api-Key") != "key-1" {
			t.Fatalf("unexpected Ozon Delivery request: method=%s path=%s client=%q api-key=%q", r.Method, r.URL.Path, r.Header.Get("Client-Id"), r.Header.Get("Api-Key"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"warehouses": []any{}, "cursor": ""})
	}))
	defer server.Close()
	transport := ozonDeliveryHTTP{h: testTLSTransport(t, server)}
	if err := transport.Ping(context.Background(), []byte(`{"client_id":"client-1","api_key":"key-1"}`)); err != nil {
		t.Fatalf("Ozon Delivery probe failed: %v", err)
	}
	if err := transport.Ping(context.Background(), []byte(`{"client_id":"client-1"}`)); err == nil {
		t.Fatal("malformed Ozon Delivery credentials accepted")
	}
}

func TestRussianPostCredentialProbe(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1.0/settings" || r.Method != http.MethodGet || r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России request: method=%s path=%s authorization=%q user-authorization=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-User-Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"user": "synthetic"})
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	if err := transport.Ping(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`)); err != nil {
		t.Fatalf("Почта России probe failed: %v", err)
	}
	if err := transport.Ping(context.Background(), []byte(`{"token":"token-1"}`)); err == nil {
		t.Fatal("malformed Почта России credentials accepted")
	}
	if err := transport.Ping(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz","unexpected":"value"}`)); err == nil {
		t.Fatal("unknown Почта России credential field accepted")
	}
}

func TestRussianPostBatchDirectoryRequest(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/1.0/batch" || r.URL.Query().Get("size") != "2" || r.URL.Query().Get("page") != "3" || r.URL.Query().Get("mailType") != "ONLINE_PARCEL" || r.URL.Query().Get("mailCategory") != "ORDERED" {
			t.Fatalf("unexpected Почта России batch request: method=%s path=%s query=%v", r.Method, r.URL.Path, r.URL.Query())
		}
		if r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России batch credentials: authorization=%q user-authorization=%q", r.Header.Get("Authorization"), r.Header.Get("X-User-Authorization"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"batch-name": "batch-2026-001", "batch-status": "CREATED", "shipment-count": 2},
			{"batch-name": "batch-2026-002", "batch-status": "SENT", "shipment-count": 5},
		})
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	batches, err := transport.Batches(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsBatchQuery{MailType: "ONLINE_PARCEL", MailCategory: "ORDERED", Limit: 2, Page: 3})
	if err != nil {
		t.Fatalf("Почта России batch request failed: %v", err)
	}
	if len(batches) != 2 || batches[0].RemoteID != "batch-2026-001" || batches[0].Status != "CREATED" || batches[0].ShipmentCount != 2 || batches[1].RemoteID != "batch-2026-002" || batches[1].ShipmentCount != 5 {
		t.Fatalf("unexpected normalized Почта России batches: %+v", batches)
	}
	if batches[0].ObservedAt.IsZero() || batches[0].ObservedAt.Location() != time.UTC {
		t.Fatalf("batch observation timestamp is not UTC: %v", batches[0].ObservedAt)
	}
}

func TestRussianPostBatchDirectoryRejectsDuplicateRows(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"batch-name": "batch-duplicate", "batch-status": "CREATED", "shipment-count": 1},
			{"batch-name": "batch-duplicate", "batch-status": "SENT", "shipment-count": 2},
		})
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	if _, err := transport.Batches(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsBatchQuery{Limit: 2}); err == nil {
		t.Fatal("duplicate Почта России batch rows accepted")
	}
}

func TestRussianPostArchivedBatchDirectoryUsesOfficialEndpoint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/1.0/archive" || r.URL.RawQuery != "" {
			t.Fatalf("unexpected Почта России archive batch request: method=%s path=%s query=%q", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России archive credentials: authorization=%q user-authorization=%q", r.Header.Get("Authorization"), r.Header.Get("X-User-Authorization"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"batch-name": "25", "batch-status": "ARCHIVED", "shipment-count": 2}})
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	batches, err := transport.ArchivedBatches(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsArchiveBatchQuery{Limit: 10})
	if err != nil {
		t.Fatalf("Почта России archive batch request failed: %v", err)
	}
	if len(batches) != 1 || batches[0].RemoteID != "25" || batches[0].Status != "ARCHIVED" || batches[0].ShipmentCount != 2 || batches[0].ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized Почта России archived batches: %+v", batches)
	}

	invalidServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"batch-name": "25", "batch-status": "ARCHIVED", "shipment-count": -1}})
	}))
	defer invalidServer.Close()
	if _, err := (pochtarussiaHTTP{h: testTLSTransport(t, invalidServer)}).ArchivedBatches(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsArchiveBatchQuery{Limit: 10}); err == nil {
		t.Fatal("negative archived batch shipment count accepted")
	}
}

func TestRussianPostBatchCreationRequest(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/1.0/user/shipment" || r.URL.Query().Get("sending-date") != "2026-08-31" || r.URL.Query().Get("use-online-balance") != "true" {
			t.Fatalf("unexpected Почта России batch creation request: method=%s path=%s query=%v", r.Method, r.URL.Path, r.URL.Query())
		}
		var orderIDs []int64
		if err := json.NewDecoder(r.Body).Decode(&orderIDs); err != nil || len(orderIDs) != 2 || orderIDs[0] != 57565818 || orderIDs[1] != 57565819 {
			t.Fatalf("unexpected Почта России batch creation body: %v", orderIDs)
		}
		if r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России batch credentials: authorization=%q user-authorization=%q", r.Header.Get("Authorization"), r.Header.Get("X-User-Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result-ids": []any{310115153}, "errors": []any{}})
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	batch, err := transport.CreateBatch(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsBatchCreateRequest{OrderIDs: []string{"57565818", "57565819"}, SendingDate: "2026-08-31", UseOnlineBalance: true, IdempotencyKey: "batch-idem-1"})
	if err != nil {
		t.Fatalf("Почта России batch creation failed: %v", err)
	}
	if batch.RemoteID != "310115153" || batch.Status != "CREATED" || batch.ShipmentCount != 2 || batch.ObservedAt.IsZero() || batch.ObservedAt.Location() != time.UTC {
		t.Fatalf("unexpected normalized Почта России batch: %+v", batch)
	}
}

func TestRussianPostBatchCreationRejectsNonNumericOrderID(t *testing.T) {
	transport := pochtarussiaHTTP{h: &httpTransport{}}
	if _, err := transport.CreateBatch(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsBatchCreateRequest{OrderIDs: []string{"batch-1"}, IdempotencyKey: "batch-idem-2"}); err == nil {
		t.Fatal("non-numeric Почта России batch order id accepted")
	}
}

func TestRussianPostTrackingUsesSOAPHistoryAndLatestOperation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rtm34" || r.Header.Get("Content-Type") != "application/soap+xml; charset=utf-8" || r.Header.Get("Accept") != "application/soap+xml, text/xml;q=0.9" {
			t.Fatalf("unexpected Почта России tracking request: method=%s path=%s content-type=%q accept=%q", r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("Accept"))
		}
		var requestBody []byte
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read tracking request: %v", err)
		}
		requestText := string(requestBody)
		for _, expected := range []string{"getOperationHistory", "<data:Barcode>RA644000001RU</data:Barcode>", "<data:MessageType>0</data:MessageType>", "<data:Language>RUS</data:Language>", "<data:login>tracking-login</data:login>", "<data:password>tracking-password</data:password>"} {
			if !strings.Contains(requestText, expected) {
				t.Fatalf("tracking SOAP request missing %q: %s", expected, requestText)
			}
		}
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"><soap:Body><ns7:getOperationHistoryResponse xmlns:ns7="http://russianpost.org/operationhistory"><ns7:historyRecord><ns3:ItemParameters xmlns:ns3="http://russianpost.org/operationhistory/data"><ns3:Barcode>RA644000001RU</ns3:Barcode></ns3:ItemParameters><ns3:OperationParameters xmlns:ns3="http://russianpost.org/operationhistory/data"><ns3:OperType><ns3:Id>1</ns3:Id><ns3:Name>Принято</ns3:Name></ns3:OperType><ns3:OperDate>2026-08-28T03:00:00.000+03:00</ns3:OperDate></ns3:OperationParameters></ns7:historyRecord><ns7:historyRecord><ns3:ItemParameters xmlns:ns3="http://russianpost.org/operationhistory/data"><ns3:Barcode>RA644000001RU</ns3:Barcode></ns3:ItemParameters><ns3:OperationParameters xmlns:ns3="http://russianpost.org/operationhistory/data"><ns3:OperType><ns3:Id>2</ns3:Id><ns3:Name>Вручение</ns3:Name></ns3:OperType><ns3:OperDate>2026-08-29T03:00:00.000+03:00</ns3:OperDate></ns3:OperationParameters></ns7:historyRecord></ns7:getOperationHistoryResponse></soap:Body></soap:Envelope>`))
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	result, err := transport.Track(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz","tracking_login":"tracking-login","tracking_password":"tracking-password"}`), sdk.ShipmentStatusRequest{RemoteID: "RA644000001RU"})
	if err != nil {
		t.Fatalf("Почта России tracking request failed: %v", err)
	}
	wantObservedAt := time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)
	if result.RemoteID != "RA644000001RU" || result.TrackingNumber != "RA644000001RU" || result.Status != "delivered" || !result.ObservedAt.Equal(wantObservedAt) || result.Cost.Currency != "RUB" || result.Cost.MinorUnits != 0 {
		t.Fatalf("unexpected normalized Почта России tracking result: %+v", result)
	}
}

func TestRussianPostTrackingRejectsMismatchedBarcode(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"><soap:Body><response:getOperationHistoryResponse xmlns:response="http://russianpost.org/operationhistory"><response:historyRecord><data:ItemParameters xmlns:data="http://russianpost.org/operationhistory/data"><data:Barcode>RA644000002RU</data:Barcode></data:ItemParameters><data:OperationParameters xmlns:data="http://russianpost.org/operationhistory/data"><data:OperType><data:Id>1</data:Id></data:OperType><data:OperDate>2026-08-28T03:00:00Z</data:OperDate></data:OperationParameters></response:historyRecord></response:getOperationHistoryResponse></soap:Body></soap:Envelope>`))
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	if _, err := transport.Track(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz","tracking_login":"tracking-login","tracking_password":"tracking-password"}`), sdk.ShipmentStatusRequest{RemoteID: "RA644000001RU"}); err == nil {
		t.Fatal("mismatched Почта России barcode accepted")
	}
}

func TestRussianPostLabelRequestsOfficialBacklogForm(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/1.0/forms/backlog/310115153/forms" ||
			r.URL.Query().Get("print-type") != "PAPER" || r.URL.Query().Get("sending-date") == "" ||
			r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России label request: method=%s path=%s query=%v authorization=%q user-authorization=%q", r.Method, r.URL.Path, r.URL.Query(), r.Header.Get("Authorization"), r.Header.Get("X-User-Authorization"))
		}
		if _, err := time.Parse("2006-01-02", r.URL.Query().Get("sending-date")); err != nil {
			t.Fatalf("invalid sending date: %v", err)
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.7\nsynthetic form\n%%EOF"))
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	result, err := transport.Label(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LabelRequest{RemoteID: "310115153", Format: "pdf"})
	if err != nil {
		t.Fatalf("Почта России label request failed: %v", err)
	}
	if !strings.HasPrefix(result.ArtifactRef, "pochta-russia:form:backlog:310115153:") || result.MediaType != "application/pdf" || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized Почта России label: %+v", result)
	}
}

func TestRussianPostLabelRejectsNonPDFResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"not-ready"}`))
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	if _, err := transport.Label(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LabelRequest{RemoteID: "310115153", Format: "pdf"}); err == nil {
		t.Fatal("non-PDF Почта России label response accepted")
	}
}

func TestRussianPostReturnLabelRequestsEasyReturnPDF(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/1.0/forms/RA644000001RU/easy-return-pdf" || r.URL.Query().Get("print-type") != "PAPER" || r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России return label request: method=%s path=%s query=%s authorization=%q user-authorization=%q", r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("X-User-Authorization"))
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.7\nsynthetic return label\n%%EOF"))
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	result, err := transport.Label(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LabelRequest{RemoteID: "RA644000001RU", Format: "return_pdf"})
	if err != nil {
		t.Fatalf("Почта России return label request failed: %v", err)
	}
	if !strings.HasPrefix(result.ArtifactRef, "pochta-russia:form:return:RA644000001RU:") || result.MediaType != "application/pdf" || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized Почта России return label: %+v", result)
	}
	if _, err := transport.Label(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LabelRequest{RemoteID: "not-an-rpo", Format: "return_pdf"}); err == nil {
		t.Fatal("invalid Почта России return label RPO accepted")
	}
}

func TestRussianPostShipmentCreationUsesBacklogAndNormalizesOrderID(t *testing.T) {
	reject := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/1.0/user/backlog" || r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России shipment request: method=%s path=%s authorization=%q user-authorization=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-User-Authorization"))
		}
		var orders []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&orders); err != nil || len(orders) != 1 {
			t.Fatalf("invalid Почта России backlog request: orders=%v err=%v", orders, err)
		}
		order := orders[0]
		if order["order-num"] != "order-001" || order["mail-type"] != "ONLINE_PARCEL" || order["index-to"] != float64(190000) || order["postoffice-code"] != "101000" || order["given-name"] != "Пётр" || order["surname"] != "Петров" || order["street-to"] != "Невский" || order["house-to"] != "1" || order["tel-address"] != "+79990000001" {
			t.Fatalf("unexpected Почта России backlog mapping: %+v", order)
		}
		if reject {
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"position": 0}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result-ids": []any{57565818}})
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	request := sdk.ShipmentCreateRequest{
		ExternalID: "order-001", ServiceCode: "pochta_parcel_online", IdempotencyKey: "idem-001", PickupPointRef: "101000",
		From:      sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Мясницкая, 1"},
		To:        sdk.Address{Country: "RU", PostalCode: "190000", City: "Санкт-Петербург", Line1: "Невский, 1"},
		Parcels:   []sdk.Parcel{{WeightGrams: 1000, LengthMM: 100, WidthMM: 100, HeightMM: 100}},
		Sender:    sdk.LogisticsContact{Name: "Иван Иванов", Phone: "+79990000000"},
		Recipient: sdk.LogisticsContact{Name: "Пётр Петров", Phone: "+79990000001"},
	}
	result, err := transport.Create(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), request)
	if err != nil {
		t.Fatalf("Почта России shipment creation failed: %v", err)
	}
	if result.RemoteID != "57565818" || result.Status != "created" || result.Cost.Currency != "RUB" || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized Почта России shipment: %+v", result)
	}
	reject = true
	if _, err := transport.Create(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), request); err == nil {
		t.Fatal("Почта России backlog error response accepted")
	}
}

func TestRussianPostShipmentCancellationDeletesExactBacklogOrder(t *testing.T) {
	reject := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/1.0/backlog" || r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России cancellation request: method=%s path=%s authorization=%q user-authorization=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-User-Authorization"))
		}
		var ids []int64
		if err := json.NewDecoder(r.Body).Decode(&ids); err != nil || len(ids) != 1 || ids[0] != 57565818 {
			t.Fatalf("unexpected Почта России cancellation body: ids=%v err=%v", ids, err)
		}
		if reject {
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"position": 0}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result-ids": []any{57565818}})
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	request := sdk.ShipmentCancelRequest{RemoteID: "57565818", IdempotencyKey: "idem-cancel-001"}
	result, err := transport.Cancel(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), request)
	if err != nil {
		t.Fatalf("Почта России shipment cancellation failed: %v", err)
	}
	if result.RemoteID != request.RemoteID || result.Status != "cancelled" || result.Cost.Currency != "RUB" || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized Почта России cancellation: %+v", result)
	}
	reject = true
	if _, err := transport.Cancel(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), request); err == nil {
		t.Fatal("Почта России cancellation error response accepted")
	}
}

func TestRussianPostReturnCreatesShipmentForExactRPO(t *testing.T) {
	reject := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/1.0/returns" || r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России return request: method=%s path=%s authorization=%q user-authorization=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-User-Authorization"))
		}
		var payload struct {
			DirectBarcode string `json:"direct-barcode"`
			MailType      string `json:"mail-type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.DirectBarcode != "RA644000001RU" || payload.MailType != "POSTAL_PARCEL" {
			t.Fatalf("unexpected Почта России return body: %+v err=%v", payload, err)
		}
		if reject {
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"position": 0}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"return-barcode": "RA644000002RU"})
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	request := sdk.ReturnCreateRequest{OriginalRemoteID: "RA644000001RU", ExternalID: "return-001", MailType: "POSTAL_PARCEL", IdempotencyKey: "return-idem-001"}
	result, err := transport.Return(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), request)
	if err != nil {
		t.Fatalf("Почта России return request failed: %v", err)
	}
	if result.RemoteID != "RA644000002RU" || result.Status != "created" || result.TrackingNumber != result.RemoteID || result.Cost.Currency != "RUB" || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized Почта России return: %+v", result)
	}
	reject = true
	if _, err := transport.Return(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), request); err == nil {
		t.Fatal("Почта России return error response accepted")
	}
}

func TestPEKShipmentCancellationAnnulsExactPreRegistration(t *testing.T) {
	success := true
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/order/cancellation/" || r.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("synthetic-user:synthetic-key")) {
			t.Fatalf("unexpected ПЭК cancellation request: method=%s path=%s authorization=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		var codes []string
		if err := json.NewDecoder(r.Body).Decode(&codes); err != nil || len(codes) != 1 || codes[0] != "780339690775" {
			t.Fatalf("unexpected ПЭК cancellation body: codes=%v err=%v", codes, err)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"code": "780339690775", "success": success, "description": "Предварительное оформление аннулировано"}})
	}))
	defer server.Close()
	transport := pekHTTP{h: testTLSTransport(t, server)}
	request := sdk.ShipmentCancelRequest{RemoteID: "780339690775", IdempotencyKey: "cancel-pek-1"}
	result, err := transport.Cancel(context.Background(), []byte(`{"username":"synthetic-user","password":"synthetic-key"}`), request)
	if err != nil {
		t.Fatalf("ПЭК shipment cancellation failed: %v", err)
	}
	if result.RemoteID != request.RemoteID || result.Status != "cancelled" || result.TrackingNumber != request.RemoteID || result.Cost.Currency != "RUB" || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized ПЭК cancellation: %+v", result)
	}
	success = false
	if _, err := transport.Cancel(context.Background(), []byte(`{"username":"synthetic-user","password":"synthetic-key"}`), request); err == nil {
		t.Fatal("ПЭК unsuccessful cancellation response accepted")
	}
}

func TestPEKCargoReturnRequestsOneAcceptedCargo(t *testing.T) {
	success := true
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/cargos/cancelandreturncargo/" || r.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("synthetic-user:synthetic-key")) {
			t.Fatalf("unexpected ПЭК cargo return request: method=%s path=%s authorization=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		var request struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Code != "780339690775" {
			t.Fatalf("unexpected ПЭК cargo return body: %+v err=%v", request, err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": success, "description": "Возврат заказа/ груза отправителю успешно оформлен"})
	}))
	defer server.Close()
	transport := pekHTTP{h: testTLSTransport(t, server)}
	request := sdk.ReturnCreateRequest{OriginalRemoteID: "780339690775", ExternalID: "return-pek-1", MailType: "pek_cargo_return", IdempotencyKey: "return-pek-1"}
	result, err := transport.Return(context.Background(), []byte(`{"username":"synthetic-user","password":"synthetic-key"}`), request)
	if err != nil {
		t.Fatalf("ПЭК cargo return request failed: %v", err)
	}
	if result.RemoteID != request.OriginalRemoteID || result.Status != "created" || result.TrackingNumber != request.OriginalRemoteID || result.Cost.Currency != "RUB" || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized ПЭК cargo return: %+v", result)
	}
	success = false
	if _, err := transport.Return(context.Background(), []byte(`{"username":"synthetic-user","password":"synthetic-key"}`), request); err == nil {
		t.Fatal("ПЭК unsuccessful cargo return response accepted")
	}
}

func TestPEKLabelRequestsOneCargoPDF(t *testing.T) {
	pdf := []byte("%PDF-1.7\nfixture")
	encoded := base64.StdEncoding.EncodeToString(pdf)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/order/print/" {
			t.Fatalf("unexpected ПЭК label request: method=%s path=%s", r.Method, r.URL.Path)
		}
		var request struct {
			CargoIndex string `json:"cargoIndex"`
			Type       string `json:"type"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.CargoIndex != "780339690775" || request.Type != "simple" {
			t.Fatalf("unexpected ПЭК label body: %+v", request)
		}
		_ = json.NewEncoder(w).Encode(encoded)
	}))
	defer server.Close()
	transport := pekHTTP{h: testTLSTransport(t, server)}
	result, err := transport.Label(context.Background(), []byte(`{"username":"synthetic-user","password":"synthetic-key"}`), sdk.LabelRequest{RemoteID: "780339690775", Format: "pdf"})
	if err != nil {
		t.Fatalf("ПЭК label request failed: %v", err)
	}
	if result.MediaType != "application/pdf" || result.ArtifactRef == "" || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized ПЭК label: %+v", result)
	}

	invalidServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode("not-a-pdf")
	}))
	defer invalidServer.Close()
	invalidTransport := pekHTTP{h: testTLSTransport(t, invalidServer)}
	if _, err := invalidTransport.Label(context.Background(), []byte(`{"username":"synthetic-user","password":"synthetic-key"}`), sdk.LabelRequest{RemoteID: "780339690775", Format: "pdf"}); err == nil {
		t.Fatal("ПЭК non-PDF label response accepted")
	}
}

func TestPEKLabelRequestsFullOrderPDF(t *testing.T) {
	pdf := []byte("%PDF-1.7\nfull order form\n%%EOF")
	encoded := base64.StdEncoding.EncodeToString(pdf)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/order/print/" {
			t.Fatalf("unexpected ПЭК order-form request: method=%s path=%s", r.Method, r.URL.Path)
		}
		var request struct {
			CargoIndex string `json:"cargoIndex"`
			Type       string `json:"type"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.CargoIndex != "780339690775" || request.Type != "big" {
			t.Fatalf("unexpected ПЭК order-form body: %+v", request)
		}
		_ = json.NewEncoder(w).Encode(encoded)
	}))
	defer server.Close()
	transport := pekHTTP{h: testTLSTransport(t, server)}
	result, err := transport.Label(context.Background(), []byte(`{"username":"synthetic-user","password":"synthetic-key"}`), sdk.LabelRequest{RemoteID: "780339690775", Format: "request_pdf"})
	if err != nil {
		t.Fatalf("ПЭК order-form request failed: %v", err)
	}
	if !strings.HasPrefix(result.ArtifactRef, "pek:print:big:780339690775:") || result.MediaType != "application/pdf" || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized ПЭК order form: %+v", result)
	}
}

func TestRussianPostPickupPointsReadDirectoryAndOfficeDetails(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России pickup credentials: authorization=%q user-authorization=%q", r.Header.Get("Authorization"), r.Header.Get("X-User-Authorization"))
		}
		switch r.URL.Path {
		case "/postoffice/1.0/by-address":
			if r.Method != http.MethodGet || r.URL.Query().Get("address") != "Москва" || r.URL.Query().Get("top") != "2" {
				t.Fatalf("unexpected Почта России pickup search: method=%s query=%v", r.Method, r.URL.Query())
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"is-matched": false, "postoffices": []any{"101000", 101001}})
		case "/postoffice/1.0/101000":
			if r.URL.Query().Get("filter-by-office-type") != "true" {
				t.Fatalf("office type filter missing: %v", r.URL.Query())
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"postal-code": "101000", "settlement": "Москва", "address-source": "Москва, Чистопрудный бульвар, 1", "type-code": "ГОПС",
			})
		case "/postoffice/1.0/101001":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"postal-code": 101001, "settlement": "Москва", "address-source": "Москва, Мясницкая улица, 26", "is-closed": true,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	points, err := transport.Pickup(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.PickupPointQuery{Country: "RU", City: "Москва", Limit: 2})
	if err != nil {
		t.Fatalf("Почта России pickup request failed: %v", err)
	}
	if len(points) != 2 || points[0].RemoteID != "101000" || points[0].Address != "Москва, Чистопрудный бульвар, 1" || !points[0].Active || points[1].RemoteID != "101001" || points[1].Active {
		t.Fatalf("unexpected normalized Почта России points: %+v", points)
	}
}

func TestRussianPostRatesUsePostalCalculatorAndExactMoney(t *testing.T) {
	badResponse := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/calculate/tariff/delivery" || r.Header.Get("Authorization") != "" ||
			r.URL.Query().Get("format") != "json" || r.URL.Query().Get("errorcode") != "1" || r.URL.Query().Get("object") != "23030" ||
			r.URL.Query().Get("weight") != "1000" || r.URL.Query().Get("from") != "101000" || r.URL.Query().Get("to") != "190000" {
			t.Fatalf("unexpected Почта России calculator request: method=%s path=%s query=%v authorization=%q", r.Method, r.URL.Path, r.URL.Query(), r.Header.Get("Authorization"))
		}
		if badResponse {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 23030, "errors": []any{map[string]any{"type": 1, "code": 42, "msg": "not available"}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       23030,
			"items":    []any{map[string]any{"id": "delivery", "name": "Доставка"}},
			"paynds":   123450,
			"delivery": map[string]any{"min": 2, "max": 4},
		})
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	request := sdk.RateRequest{
		From:    sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Тверская, 1"},
		To:      sdk.Address{Country: "RU", PostalCode: "190000", City: "Санкт-Петербург", Line1: "Невский, 1"},
		Parcels: []sdk.Parcel{{WeightGrams: 1000, LengthMM: 100, WidthMM: 100, HeightMM: 100}},
	}
	quotes, err := transport.Rates(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), request)
	if err != nil {
		t.Fatalf("Почта России rate request failed: %v", err)
	}
	if len(quotes) != 1 || quotes[0].ServiceCode != "pochta_parcel_online" || quotes[0].Cost.MinorUnits != 123450 || quotes[0].Cost.Currency != "RUB" {
		t.Fatalf("unexpected normalized Почта России quotes: %+v", quotes)
	}
	badResponse = true
	if _, err := transport.Rates(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), request); err == nil {
		t.Fatal("Почта России calculator error response accepted")
	}
}

func TestRussianPostPickupPointsCapProviderFanout(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/postoffice/1.0/by-address" || r.URL.Query().Get("top") != "50" {
			t.Fatalf("unexpected Почта России provider limit: path=%s query=%v", r.URL.Path, r.URL.Query())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"postoffices": []any{}})
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	if points, err := transport.Pickup(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.PickupPointQuery{Country: "RU", City: "Москва", Limit: 500}); err != nil || len(points) != 0 {
		t.Fatalf("points=%+v err=%v", points, err)
	}
}

func TestRussianPostBatchSubmissionUsesCheckinEndpoint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/1.0/batch/24/checkin" || r.URL.Query().Get("useOnlineBalance") != "true" || r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России batch hand-off request: method=%s path=%s query=%v", r.Method, r.URL.Path, r.URL.Query())
		}
		if content, err := io.ReadAll(r.Body); err != nil || len(content) != 0 {
			t.Fatalf("batch hand-off must not send a body: %q", content)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"f103-sent": true})
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	result, err := transport.SubmitBatch(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsBatchSubmitRequest{BatchID: "24", UseOnlineBalance: true, IdempotencyKey: "submit-24"})
	if err != nil {
		t.Fatalf("Почта России batch hand-off failed: %v", err)
	}
	if result.RemoteID != "24" || result.Status != "SUBMITTED" || !result.Accepted || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized Почта России batch hand-off: %+v", result)
	}

	invalidServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"f103-sent": false})
	}))
	defer invalidServer.Close()
	if _, err := (pochtarussiaHTTP{h: testTLSTransport(t, invalidServer)}).SubmitBatch(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsBatchSubmitRequest{BatchID: "24", IdempotencyKey: "submit-24"}); err == nil {
		t.Fatal("unsuccessful batch hand-off response accepted")
	}
}

func TestRussianPostBatchArchiveUsesOfficialEndpoint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/1.0/archive" || r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России batch archive request: method=%s path=%s", r.Method, r.URL.Path)
		}
		var payload []int64
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload) != 1 || payload[0] != 25 {
			t.Fatalf("unexpected batch archive payload: %#v err=%v", payload, err)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"batch-name": 25}})
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	result, err := transport.ArchiveBatch(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsBatchArchiveRequest{BatchID: "25", IdempotencyKey: "archive-25"})
	if err != nil {
		t.Fatalf("Почта России batch archive failed: %v", err)
	}
	if result.RemoteID != "25" || result.Status != "ARCHIVED" || !result.Archived || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized Почта России batch archive: %+v", result)
	}

	invalidServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"batch-name": 25, "error-code": "NOT_FOUND"}})
	}))
	defer invalidServer.Close()
	if _, err := (pochtarussiaHTTP{h: testTLSTransport(t, invalidServer)}).ArchiveBatch(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsBatchArchiveRequest{BatchID: "25", IdempotencyKey: "archive-25"}); err == nil {
		t.Fatal("batch archive error response accepted")
	}
}

func TestRussianPostBatchUnarchiveUsesOfficialEndpoint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/1.0/archive/revert" || r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России batch restore request: method=%s path=%s", r.Method, r.URL.Path)
		}
		var payload []int64
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload) != 1 || payload[0] != 25 {
			t.Fatalf("unexpected batch restore payload: %#v err=%v", payload, err)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"batch-name": 25}})
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	result, err := transport.UnarchiveBatch(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsBatchUnarchiveRequest{BatchID: "25", IdempotencyKey: "restore-25"})
	if err != nil {
		t.Fatalf("Почта России batch restore failed: %v", err)
	}
	if result.RemoteID != "25" || result.Status != "RESTORED" || result.Archived || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized Почта России batch restore: %+v", result)
	}

	invalidServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"batch-name": 25, "error-code": "NOT_FOUND"}})
	}))
	defer invalidServer.Close()
	if _, err := (pochtarussiaHTTP{h: testTLSTransport(t, invalidServer)}).UnarchiveBatch(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsBatchUnarchiveRequest{BatchID: "25", IdempotencyKey: "restore-25"}); err == nil {
		t.Fatal("batch restore error response accepted")
	}
}

func TestRussianPostSeparateReturnUsesOfficialEndpoint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/1.0/returns/return-without-direct" || r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России separate return request: method=%s path=%s", r.Method, r.URL.Path)
		}
		var payload []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload) != 1 {
			t.Fatalf("unexpected separate return payload: %v", err)
		}
		if payload[0]["mail-type"] != "ONLINE_PARCEL" || payload[0]["insr-value"] != float64(1299) || payload[0]["recipient-name"] != "Пётр Петров" || payload[0]["sender-name"] != "Иван Иванов" {
			t.Fatalf("unexpected separate return fields: %#v", payload[0])
		}
		from, ok := payload[0]["address-from"].(map[string]any)
		if !ok || from["index"] != "101000" || from["place"] != "Москва" || from["street"] != "Мясницкая" || from["house"] != "1" {
			t.Fatalf("unexpected separate return address: %#v", payload[0]["address-from"])
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"position": 0, "return-barcode": "RA644000003RU"}})
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	result, err := transport.CreateSeparateReturn(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsSeparateReturnRequest{
		From:              sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Мясницкая, 1"},
		To:                &sdk.Address{Country: "RU", PostalCode: "190000", City: "Санкт-Петербург", Line1: "Невский, 1"},
		InsuredValueMinor: 129900, MailType: "online_parcel", OrderNumber: "return-001", PostOfficeCode: "101000",
		RecipientName: "Пётр Петров", SenderName: "Иван Иванов", IdempotencyKey: "separate-return-001",
	})
	if err != nil {
		t.Fatalf("Почта России separate return failed: %v", err)
	}
	if result.RemoteID != "RA644000003RU" || result.TrackingNumber != result.RemoteID || result.Status != "created" {
		t.Fatalf("unexpected normalized separate return: %+v", result)
	}

	invalidServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"position": 0, "errors": []map[string]string{{"code": "INVALID"}}}})
	}))
	defer invalidServer.Close()
	if _, err := (pochtarussiaHTTP{h: testTLSTransport(t, invalidServer)}).CreateSeparateReturn(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsSeparateReturnRequest{
		From: sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Мясницкая, 1"}, MailType: "ONLINE_PARCEL",
		RecipientName: "Пётр Петров", SenderName: "Иван Иванов", IdempotencyKey: "separate-return-002",
	}); err == nil {
		t.Fatal("separate return provider error accepted")
	}
}

func TestRussianPostSeparateReturnDeletionUsesOfficialEndpoint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/1.0/returns/delete-separate-return" || r.URL.Query().Get("barcode") != "RA644000003RU" || r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России separate return deletion request: method=%s path=%s query=%v", r.Method, r.URL.Path, r.URL.Query())
		}
		if content, err := io.ReadAll(r.Body); err != nil || len(content) != 0 {
			t.Fatalf("separate return deletion must not send a body: %q", content)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	result, err := transport.DeleteSeparateReturn(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsSeparateReturnDeleteRequest{ReturnBarcode: "RA644000003RU", IdempotencyKey: "delete-return-1"})
	if err != nil {
		t.Fatalf("Почта России separate return deletion failed: %v", err)
	}
	if result.RemoteID != "RA644000003RU" || result.Status != "DELETED" || !result.Deleted || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized separate return deletion: %+v", result)
	}

	invalidServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "RETURN_SHIPMENT_NOT_FOUND", "description": "not found"})
	}))
	defer invalidServer.Close()
	if _, err := (pochtarussiaHTTP{h: testTLSTransport(t, invalidServer)}).DeleteSeparateReturn(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsSeparateReturnDeleteRequest{ReturnBarcode: "RA644000003RU", IdempotencyKey: "delete-return-2"}); err == nil {
		t.Fatal("separate return deletion provider error accepted")
	}
}

func TestRussianPostSeparateReturnEditUsesOfficialEndpoint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/1.0/returns/RA644000003RU" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России separate return edit request: method=%s path=%s query=%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("unexpected separate return edit payload: %v", err)
		}
		if payload["mail-type"] != "ONLINE_PARCEL" || payload["insr-value"] != float64(1299) || payload["recipient-name"] != "Пётр Петров" || payload["sender-name"] != "Иван Иванов" {
			t.Fatalf("unexpected separate return edit fields: %#v", payload)
		}
		from, ok := payload["address-from"].(map[string]any)
		if !ok || from["index"] != "101000" || from["place"] != "Москва" || from["street"] != "Мясницкая" || from["house"] != "1" {
			t.Fatalf("unexpected separate return edit address: %#v", payload["address-from"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"position": 0, "return-barcode": "RA644000003RU"})
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	result, err := transport.EditSeparateReturn(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsSeparateReturnUpdateRequest{
		ReturnBarcode:     "RA644000003RU",
		From:              sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Мясницкая, 1"},
		To:                &sdk.Address{Country: "RU", PostalCode: "190000", City: "Санкт-Петербург", Line1: "Невский, 1"},
		InsuredValueMinor: 129900, MailType: "online_parcel", OrderNumber: "return-001", PostOfficeCode: "101000",
		RecipientName: "Пётр Петров", SenderName: "Иван Иванов", IdempotencyKey: "separate-return-edit-001",
	})
	if err != nil {
		t.Fatalf("Почта России separate return edit failed: %v", err)
	}
	if result.RemoteID != "RA644000003RU" || result.Status != "UPDATED" || !result.Updated || result.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized separate return edit: %+v", result)
	}

	invalidServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"position": 0, "errors": []map[string]string{{"code": "INVALID"}}, "return-barcode": "RA644000003RU"})
	}))
	defer invalidServer.Close()
	if _, err := (pochtarussiaHTTP{h: testTLSTransport(t, invalidServer)}).EditSeparateReturn(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsSeparateReturnUpdateRequest{
		ReturnBarcode: "RA644000003RU", From: sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Мясницкая, 1"}, MailType: "ONLINE_PARCEL",
		RecipientName: "Пётр Петров", SenderName: "Иван Иванов", IdempotencyKey: "separate-return-edit-002",
	}); err == nil {
		t.Fatal("separate return edit provider error accepted")
	}

	mismatchedServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"position": 0, "return-barcode": "RA644000004RU"})
	}))
	defer mismatchedServer.Close()
	if _, err := (pochtarussiaHTTP{h: testTLSTransport(t, mismatchedServer)}).EditSeparateReturn(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`), sdk.LogisticsSeparateReturnUpdateRequest{
		ReturnBarcode: "RA644000003RU", From: sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Мясницкая, 1"}, MailType: "ONLINE_PARCEL",
		RecipientName: "Пётр Петров", SenderName: "Иван Иванов", IdempotencyKey: "separate-return-edit-003",
	}); err == nil {
		t.Fatal("separate return edit mismatched barcode accepted")
	}
}

func TestOzonPayCredentialProbe(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/product/list" || r.Method != http.MethodPost || r.Header.Get("Client-Id") != "client-1" || r.Header.Get("Api-Key") != "key-1" {
			t.Fatalf("unexpected Ozon Pay request: method=%s path=%s client=%q api-key=%q", r.Method, r.URL.Path, r.Header.Get("Client-Id"), r.Header.Get("Api-Key"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"items": []any{}, "total": 0, "last_id": ""}})
	}))
	defer server.Close()
	transport := ozonPayHTTP{h: testTLSTransport(t, server)}
	if err := transport.Ping(context.Background(), []byte(`{"client_id":"client-1","api_key":"key-1"}`)); err != nil {
		t.Fatalf("Ozon Pay probe failed: %v", err)
	}
	if err := transport.Ping(context.Background(), []byte(`{"client_id":"client-1","api_key":"key-1","unexpected":"value"}`)); err == nil {
		t.Fatal("unknown Ozon Pay credential field accepted")
	}
	if err := transport.Ping(context.Background(), []byte(`{"client_id":"client-1","api_key":"key-1"}{}`)); err == nil {
		t.Fatal("trailing Ozon Pay credential JSON accepted")
	}
}
