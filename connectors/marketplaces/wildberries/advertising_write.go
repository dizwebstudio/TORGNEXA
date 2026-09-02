package wildberries

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type wbBudgetDeposit struct {
	Sum    int64 `json:"sum"`
	Type   int   `json:"type"`
	Return bool  `json:"return"`
}

type wbCampaignBid struct {
	AdvertID int64          `json:"advert_id"`
	NMBids   []wbProductBid `json:"nm_bids"`
}

type wbProductBid struct {
	NMID       int64  `json:"nm_id"`
	BidKopecks int64  `json:"bid_kopecks"`
	Placement  string `json:"placement"`
}

type wbBidsRequest struct {
	Bids []wbCampaignBid `json:"bids"`
}

// ApplyAdvertisingOperation implements the bounded WB campaign controls
// whose API semantics are explicit: launch, pause and stop transitions, budget
// deposits, and product-card bids. Archive is intentionally not mapped to
// stop because those are different provider-side state changes. Product linking and campaign creation remain outside
// this adapter because their provider contracts require additional campaign
// fields that are not present in the normalized SDK operation.
func (connector *Connector) ApplyAdvertisingOperation(ctx context.Context, account sdk.Account, runtime sdk.Runtime, operation sdk.AdvertisingOperation) (sdk.AdvertisingOperationResult, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "ads.manage") != nil || operation.Validate() != nil {
		return sdk.AdvertisingOperationResult{}, sdk.ErrInvalidAdvertisingRequest
	}
	campaignID, err := strconv.ParseInt(operation.CampaignID, 10, 64)
	if err != nil || campaignID < 1 {
		return sdk.AdvertisingOperationResult{}, sdk.ErrInvalidAdvertisingRequest
	}
	method, path, query, body, err := wbAdvertisingRequest(operation, campaignID)
	if err != nil {
		return sdk.AdvertisingOperationResult{}, err
	}
	if operation.DryRun {
		return sdk.AdvertisingOperationResult{State: sdk.AdvertisingAccepted, ReadAfterWrite: false}, nil
	}
	err = runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: method, Host: advertisingHost, Path: path, Query: query, Body: body, Token: secret, IdempotencyKey: operation.IdempotencyKey})
		if callErr != nil {
			return writeOutcomeUnknown()
		}
		return normalizeHTTP(response)
	})
	if err != nil {
		return sdk.AdvertisingOperationResult{}, err
	}
	return sdk.AdvertisingOperationResult{State: sdk.AdvertisingAccepted, ReadAfterWrite: false}, nil
}

func wbAdvertisingRequest(operation sdk.AdvertisingOperation, campaignID int64) (string, string, []QueryParam, []byte, error) {
	id := strconv.FormatInt(campaignID, 10)
	switch operation.Name {
	case sdk.AdvertisingLaunch:
		return "GET", "/adv/v0/start", []QueryParam{{Name: "id", Value: id}}, nil, nil
	case sdk.AdvertisingPause:
		return "GET", "/adv/v0/pause", []QueryParam{{Name: "id", Value: id}}, nil, nil
	case sdk.AdvertisingStop:
		return "GET", "/adv/v0/stop", []QueryParam{{Name: "id", Value: id}}, nil, nil
	case sdk.AdvertisingSetBudget:
		if operation.AmountMinor < 1 || operation.Currency != "RUB" {
			return "", "", nil, nil, sdk.ErrInvalidAdvertisingRequest
		}
		body, err := json.Marshal(wbBudgetDeposit{Sum: operation.AmountMinor, Type: 1, Return: true})
		return "POST", "/adv/v1/budget/deposit", []QueryParam{{Name: "id", Value: id}}, body, err
	case sdk.AdvertisingSetBid:
		if operation.AmountMinor < 1 || operation.Currency != "RUB" || len(operation.ProductIDs) == 0 || len(operation.ProductIDs) > 50 {
			return "", "", nil, nil, sdk.ErrInvalidAdvertisingRequest
		}
		products := make([]wbProductBid, 0, len(operation.ProductIDs))
		for _, productID := range operation.ProductIDs {
			nmID, err := strconv.ParseInt(productID, 10, 64)
			if err != nil || nmID < 1 {
				return "", "", nil, nil, sdk.ErrInvalidAdvertisingRequest
			}
			products = append(products, wbProductBid{NMID: nmID, BidKopecks: operation.AmountMinor, Placement: "combined"})
		}
		body, err := json.Marshal(wbBidsRequest{Bids: []wbCampaignBid{{AdvertID: campaignID, NMBids: products}}})
		return "PATCH", "/api/advert/v1/bids", nil, body, err
	default:
		return "", "", nil, nil, sdk.ErrInvalidAdvertisingRequest
	}
}

var _ sdk.AdvertisingManager = (*Connector)(nil)
