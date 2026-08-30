package builtinruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
