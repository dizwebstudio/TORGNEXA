package ozon

import (
	"context"
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const performanceHost = "api-performance.ozon.ru"

// Ozon's Performance API uses a separate bearer credential from Seller API
// credentials. The connector receives that credential through the same scoped
// SecretAccessor callback; it is never written to a request model or log.
type ozonCampaignList struct {
	List []struct {
		ID     string      `json:"id"`
		Title  string      `json:"title"`
		Name   string      `json:"name"`
		State  string      `json:"state"`
		Budget json.Number `json:"budget"`
	} `json:"list"`
}

// ReadAdvertisingCampaigns reads Ozon Performance campaigns.
func (connector *Connector) ReadAdvertisingCampaigns(ctx context.Context, account sdk.Account, runtime sdk.Runtime, page sdk.PageRequest) (sdk.AdvertisingCampaignPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "ads.read") != nil || page.Validate(100) != nil {
		return sdk.AdvertisingCampaignPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.AdvertisingCampaignPage
	err := runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		_, bearer, err := parseCredentialBundle(secret)
		if err != nil {
			return err
		}
		response, callErr := connector.transport.Do(ctx, Request{Method: "GET", Host: performanceHost, Path: "/api/client/campaign", Bearer: bearer})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		var parsed ozonCampaignList
		if len(response.Body) == 0 || json.Unmarshal(response.Body, &parsed) != nil {
			return ErrInvalidResponse
		}
		for _, item := range parsed.List {
			if !validRemoteID(item.ID) {
				return ErrInvalidResponse
			}
			name := item.Name
			if name == "" {
				name = item.Title
			}
			if name == "" {
				name = "Ozon campaign " + item.ID
			}
			status := ozonCampaignStatus(item.State)
			budget := minorFromDecimal(item.Budget.String())
			output.Items = append(output.Items, sdk.RemoteCampaign{RemoteID: item.ID, Name: name, Status: status, Currency: "RUB", DailyBudgetMinor: budget, TotalBudgetMinor: budget, ObservedAt: connector.now().UTC()})
		}
		if len(output.Items) > page.Limit {
			output.Items = output.Items[:page.Limit]
		}
		return nil
	})
	return output, err
}

func ozonCampaignStatus(value string) string {
	switch strings.ToLower(value) {
	case "running", "active", "activated":
		return "active"
	case "paused", "stopped":
		return "paused"
	case "archived", "deleted":
		return "archived"
	case "draft":
		return "draft"
	default:
		return "unknown"
	}
}

type ozonAdvertisingRow struct {
	CampaignID    string      `json:"campaignId"`
	CampaignIDAlt string      `json:"campaign_id"`
	Date          string      `json:"date"`
	Expense       json.Number `json:"expense"`
	Spend         json.Number `json:"spend"`
	Views         json.Number `json:"views"`
	Impressions   json.Number `json:"impressions"`
	Clicks        json.Number `json:"clicks"`
	Orders        json.Number `json:"orders"`
	Revenue       json.Number `json:"revenue"`
	OrdersSum     json.Number `json:"ordersSum"`
	RevenueAlt    json.Number `json:"revenue_sum"`
	SKU           string      `json:"sku"`
	SKUAlt        string      `json:"skuId"`
}

func (connector *Connector) readAdvertisingStats(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.AdvertisingQuery) ([]sdk.RemoteAdSpendFact, []sdk.RemoteAdPerformanceFact, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "ads.read") != nil || query.Validate(100, 100) != nil {
		return nil, nil, sdk.ErrInvalidAdvertisingRequest
	}
	params := []QueryParam{{Name: "dateFrom", Value: query.From.Format("2006-01-02")}, {Name: "dateTo", Value: query.To.Add(-time.Nanosecond).Format("2006-01-02")}}
	for _, id := range query.CampaignIDs {
		params = append(params, QueryParam{Name: "campaignIds", Value: id})
	}
	var spends []sdk.RemoteAdSpendFact
	var performance []sdk.RemoteAdPerformanceFact
	err := runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		_, bearer, err := parseCredentialBundle(secret)
		if err != nil {
			return err
		}
		response, callErr := connector.transport.Do(ctx, Request{Method: "GET", Host: performanceHost, Path: "/api/client/statistics/campaign/media/json", Query: params, Bearer: bearer})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		var rows []ozonAdvertisingRow
		if len(response.Body) == 0 || json.Unmarshal(response.Body, &rows) != nil {
			return ErrInvalidResponse
		}
		for index, row := range rows {
			id := row.CampaignID
			if id == "" {
				id = row.CampaignIDAlt
			}
			if !validRemoteID(id) {
				return ErrInvalidResponse
			}
			start, parseErr := time.Parse("2006-01-02", row.Date)
			if parseErr != nil {
				if parsed, secondErr := time.Parse(time.RFC3339Nano, row.Date); secondErr != nil {
					return ErrInvalidResponse
				} else {
					start = parsed.UTC()
				}
			} else {
				start = start.UTC()
			}
			end := start.Add(24 * time.Hour)
			sku := row.SKU
			if sku == "" {
				sku = row.SKUAlt
			}
			remoteID := id + ":" + start.Format("2006-01-02") + ":" + strconv.Itoa(index)
			spend := minorFromDecimal(firstNumber(row.Expense, row.Spend))
			revenue := minorFromDecimal(firstNumber(row.Revenue, row.OrdersSum, row.RevenueAlt))
			impressions := numberInt(firstNumber(row.Impressions, row.Views))
			clicks := numberInt(row.Clicks.String())
			orders := numberInt(row.Orders.String())
			spends = append(spends, sdk.RemoteAdSpendFact{RemoteFactID: remoteID, CampaignID: id, SKU: sku, PeriodStart: start, PeriodEnd: end, AmountMinor: spend, Currency: "RUB", ObservedAt: connector.now().UTC(), EffectiveAt: start, Quality: "confirmed"})
			performance = append(performance, sdk.RemoteAdPerformanceFact{RemoteFactID: remoteID, CampaignID: id, SKU: sku, PeriodStart: start, PeriodEnd: end, Impressions: impressions, Clicks: clicks, Orders: orders, RevenueMinor: revenue, Currency: "RUB", ObservedAt: connector.now().UTC(), EffectiveAt: start, Quality: "confirmed"})
		}
		return nil
	})
	return spends, performance, err
}

func (connector *Connector) ReadAdvertisingSpend(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.AdvertisingQuery) (sdk.AdvertisingSpendPage, error) {
	spend, _, err := connector.readAdvertisingStats(ctx, account, runtime, query)
	return sdk.AdvertisingSpendPage{Items: spend}, err
}
func (connector *Connector) ReadAdvertisingPerformance(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.AdvertisingQuery) (sdk.AdvertisingPerformancePage, error) {
	_, performance, err := connector.readAdvertisingStats(ctx, account, runtime, query)
	return sdk.AdvertisingPerformancePage{Items: performance}, err
}
func firstNumber(values ...json.Number) string {
	for _, value := range values {
		if value != "" {
			return value.String()
		}
	}
	return "0"
}
func numberInt(value string) int64 {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
func validRemoteID(value string) bool {
	return value != "" && len(value) <= 192 && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\x00\r\n")
}
func minorFromDecimal(value string) int64 {
	value = strings.Trim(strings.TrimSpace(value), "\"")
	if value == "" {
		return 0
	}
	negative := false
	if value[0] == '-' {
		negative = true
		value = value[1:]
	}
	parts := strings.SplitN(value, ".", 2)
	whole := new(big.Int)
	if _, ok := whole.SetString(parts[0], 10); !ok {
		return 0
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > 2 {
		fraction = fraction[:2]
	}
	for len(fraction) < 2 {
		fraction += "0"
	}
	cents := new(big.Int)
	if _, ok := cents.SetString(fraction, 10); !ok {
		return 0
	}
	whole.Mul(whole, big.NewInt(100))
	whole.Add(whole, cents)
	if negative {
		whole.Neg(whole)
	}
	if !whole.IsInt64() {
		return 0
	}
	return whole.Int64()
}

var _ sdk.AdvertisingReader = (*Connector)(nil)
