package wildberries

import (
	"context"
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const advertisingHost = "advert-api.wildberries.ru"

type promotionCountResponse struct {
	Adverts []struct {
		Type       int `json:"type"`
		Status     int `json:"status"`
		Count      int `json:"count"`
		AdvertList []struct { AdvertID int64 `json:"advertId"`; ChangeTime string `json:"changeTime"` } `json:"advert_list"`
	} `json:"adverts"`
}

// ReadAdvertisingCampaigns reads the current campaign identity list. WB's
// count endpoint groups IDs by status, so detailed names are intentionally
// left as a stable bounded fallback until the detail endpoint is qualified.
func (connector *Connector) ReadAdvertisingCampaigns(ctx context.Context, account sdk.Account, runtime sdk.Runtime, page sdk.PageRequest) (sdk.AdvertisingCampaignPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "ads.read") != nil || page.Validate(100) != nil || page.Cursor != "" { return sdk.AdvertisingCampaignPage{}, sdk.ErrInvalidReadRequest }
	var output sdk.AdvertisingCampaignPage
	err := runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "GET", Host: advertisingHost, Path: "/adv/v1/promotion/count", Token: secret})
		if callErr != nil { return normalizedTransportError() }; if remote := normalizeHTTP(response); remote != nil { return remote }
		var parsed promotionCountResponse; if len(response.Body) == 0 || json.Unmarshal(response.Body, &parsed) != nil { return ErrInvalidResponse }
		for _, group := range parsed.Adverts { for _, item := range group.AdvertList { if item.AdvertID <= 0 { return ErrInvalidResponse }; observed := connector.now().UTC(); if item.ChangeTime != "" { if parsedTime, parseErr := time.Parse(time.RFC3339Nano, item.ChangeTime); parseErr == nil { observed = parsedTime.UTC() } }; output.Items = append(output.Items, sdk.RemoteCampaign{RemoteID: strconv.FormatInt(item.AdvertID,10), Name: "WB campaign " + strconv.FormatInt(item.AdvertID,10), Status: wbCampaignStatus(group.Status), Currency: "RUB", ObservedAt: observed}) } }
		if len(output.Items) > page.Limit { output.Items = output.Items[:page.Limit] }
		return nil
	})
	return output, err
}

func wbCampaignStatus(status int) string { switch status { case 9: return "active"; case 11: return "paused"; case 7: return "draft"; case -1: return "archived"; default: return "unknown" } }

type wbAdvertisingStat struct {
	AdvertID int64 `json:"advertId"`
	Days []struct {
		Date string `json:"date"`
		Views json.Number `json:"views"`
		Clicks json.Number `json:"clicks"`
		Orders json.Number `json:"orders"`
		Sum json.Number `json:"sum"`
		SumPrice json.Number `json:"sum_price"`
		Apps []struct { Views json.Number `json:"views"`; Clicks json.Number `json:"clicks"`; Orders json.Number `json:"orders"`; Sum json.Number `json:"sum"`; SumPrice json.Number `json:"sum_price"`; NM []struct { NmID int64 `json:"nmId"`; Views json.Number `json:"views"`; Clicks json.Number `json:"clicks"`; Orders json.Number `json:"orders"`; Sum json.Number `json:"sum"`; SumPrice json.Number `json:"sum_price"` } `json:"nm"` } `json:"apps"`
	} `json:"days"`
}

func (connector *Connector) readAdvertisingStats(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.AdvertisingQuery) ([]sdk.RemoteAdSpendFact, []sdk.RemoteAdPerformanceFact, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "ads.read") != nil || query.Validate(50, 100) != nil { return nil,nil,sdk.ErrInvalidAdvertisingRequest }
	ids := strings.Join(query.CampaignIDs, ",")
	parameters := []QueryParam{{Name:"ids",Value:ids},{Name:"beginDate",Value:query.From.Format("2006-01-02")},{Name:"endDate",Value:query.To.Add(-time.Nanosecond).Format("2006-01-02")}}
	var spends []sdk.RemoteAdSpendFact; var performance []sdk.RemoteAdPerformanceFact
	err := runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method:"GET",Host:advertisingHost,Path:"/adv/v3/fullstats",Query:parameters,Token:secret}); if callErr != nil{return normalizedTransportError()}; if remote:=normalizeHTTP(response);remote!=nil{return remote}
		var parsed []wbAdvertisingStat; if len(response.Body)==0||json.Unmarshal(response.Body,&parsed)!=nil{return ErrInvalidResponse}
		for _, campaign := range parsed { for _, day := range campaign.Days { start, err := parseWBDate(day.Date); if err != nil{return ErrInvalidResponse}; end:=start.Add(24*time.Hour); views,clicks,orders,sum,sumPrice:=decimalInt(day.Views),decimalInt(day.Clicks),decimalInt(day.Orders),minorFromDecimal(day.Sum.String()),minorFromDecimal(day.SumPrice.String()); for _, app := range day.Apps { views+=decimalInt(app.Views);clicks+=decimalInt(app.Clicks);orders+=decimalInt(app.Orders);sum+=minorFromDecimal(app.Sum.String());sumPrice+=minorFromDecimal(app.SumPrice.String()); for _, product:=range app.NM { sku:=strconv.FormatInt(product.NmID,10); productViews,productClicks,productOrders:=decimalInt(product.Views),decimalInt(product.Clicks),decimalInt(product.Orders); productSpend,productRevenue:=minorFromDecimal(product.Sum.String()),minorFromDecimal(product.SumPrice.String()); remoteID:=strconv.FormatInt(campaign.AdvertID,10)+":"+start.Format("2006-01-02")+":"+sku; spends=append(spends,sdk.RemoteAdSpendFact{RemoteFactID:remoteID,CampaignID:strconv.FormatInt(campaign.AdvertID,10),SKU:sku,PeriodStart:start,PeriodEnd:end,AmountMinor:productSpend,Currency:"RUB",ObservedAt:connector.now().UTC(),EffectiveAt:start,Quality:"confirmed"});performance=append(performance,sdk.RemoteAdPerformanceFact{RemoteFactID:remoteID,CampaignID:strconv.FormatInt(campaign.AdvertID,10),SKU:sku,Impressions:productViews,Clicks:productClicks,Orders:productOrders,RevenueMinor:productRevenue,Currency:"RUB",PeriodStart:start,PeriodEnd:end,ObservedAt:connector.now().UTC(),EffectiveAt:start,Quality:"confirmed"}) } }; remoteID:=strconv.FormatInt(campaign.AdvertID,10)+":"+start.Format("2006-01-02"); spends=append(spends,sdk.RemoteAdSpendFact{RemoteFactID:remoteID,CampaignID:strconv.FormatInt(campaign.AdvertID,10),PeriodStart:start,PeriodEnd:end,AmountMinor:sum,Currency:"RUB",ObservedAt:connector.now().UTC(),EffectiveAt:start,Quality:"confirmed"});performance=append(performance,sdk.RemoteAdPerformanceFact{RemoteFactID:remoteID,CampaignID:strconv.FormatInt(campaign.AdvertID,10),PeriodStart:start,PeriodEnd:end,Impressions:views,Clicks:clicks,Orders:orders,RevenueMinor:sumPrice,Currency:"RUB",ObservedAt:connector.now().UTC(),EffectiveAt:start,Quality:"confirmed"}) } }
		return nil
	}); return spends,performance,err
}

// ReadAdvertisingSpend reads campaign and product spend facts.
func (connector *Connector) ReadAdvertisingSpend(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.AdvertisingQuery) (sdk.AdvertisingSpendPage, error) { spends,_,err:=connector.readAdvertisingStats(ctx,account,runtime,query); return sdk.AdvertisingSpendPage{Items:spends},err }

// ReadAdvertisingPerformance reads delivery and attributed conversion facts.
func (connector *Connector) ReadAdvertisingPerformance(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.AdvertisingQuery) (sdk.AdvertisingPerformancePage, error) { _,performance,err:=connector.readAdvertisingStats(ctx,account,runtime,query); return sdk.AdvertisingPerformancePage{Items:performance},err }

func parseWBDate(value string) (time.Time,error) { value=strings.TrimSpace(value); if len(value)>=10 { value=value[:10] }; parsed,err:=time.Parse("2006-01-02",value); return parsed.UTC(),err }
func decimalInt(value json.Number) int64 { n,err:=strconv.ParseInt(string(value),10,64);if err!=nil{return 0};return n }
func minorFromDecimal(value string) int64 { value=strings.Trim(strings.TrimSpace(value),"\"");if value==""{return 0}; negative:=false;if value[0]=='-'{negative=true;value=value[1:]}; parts:=strings.SplitN(value,".",2); whole:=new(big.Int);if _,ok:=whole.SetString(parts[0],10);!ok{return 0};frac:="";if len(parts)==2{frac=parts[1]};if len(frac)>2{frac=frac[:2]};for len(frac)<2{frac+="0"}; cents:=new(big.Int);if frac!=""{if _,ok:=cents.SetString(frac,10);!ok{return 0}};whole.Mul(whole,big.NewInt(100));whole.Add(whole,cents);if negative{whole.Neg(whole)};if !whole.IsInt64(){return 0};return whole.Int64()}

var _ sdk.AdvertisingReader = (*Connector)(nil)
