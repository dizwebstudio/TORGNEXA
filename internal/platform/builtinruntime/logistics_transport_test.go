package builtinruntime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
