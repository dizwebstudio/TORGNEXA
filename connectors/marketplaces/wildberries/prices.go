package wildberries

import (
	"context"
	"encoding/json"
	"math/big"
	"strconv"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const pricesHost = "discounts-prices-api.wildberries.ru"

type priceUploadRequest struct {
	Data []priceUploadItem `json:"data"`
}

type priceUploadItem struct {
	NmID     int64 `json:"nmID"`
	SizeID   int64 `json:"sizeID"`
	Price    int64 `json:"price"`
	Discount int64 `json:"discount"`
}

// WritePrice submits one WB size price. WB's prices API addresses a size by
// the parent nmID plus chrtID, so the host must provide ProductRemoteID from
// the product mapping; silently treating a variant ID as nmID would update the
// wrong remote object.
func (connector *Connector) WritePrice(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PriceWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	nmID, err := strconv.ParseInt(request.ProductRemoteID, 10, 64)
	if err != nil || nmID <= 0 {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	sizeID, err := strconv.ParseInt(request.VariantRemoteID, 10, 64)
	if err != nil || sizeID <= 0 {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	price, ok := wholeMoney(request.Value)
	if !ok {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	discount, ok := wbDiscount(request.Value, request.CompareAt)
	if !ok {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	body, err := json.Marshal(priceUploadRequest{Data: []priceUploadItem{{NmID: nmID, SizeID: sizeID, Price: price, Discount: discount}}})
	if err != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	var receipt sdk.CommerceWriteReceipt
	err = runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: pricesHost, Path: "/api/v2/upload/task", Body: body, Token: secret, IdempotencyKey: request.IdempotencyKey})
		if callErr != nil {
			return writeOutcomeUnknown()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}

func wholeMoney(value string) (int64, bool) {
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && strings.Trim(parts[1], "0") != "") {
		return 0, false
	}
	parsed, err := strconv.ParseInt(parts[0], 10, 64)
	return parsed, err == nil && parsed >= 0
}

func wbDiscount(price, compareAt string) (int64, bool) {
	if compareAt == "" {
		return 0, true
	}
	current, ok := wholeMoney(price)
	if !ok {
		return 0, false
	}
	base, ok := wholeMoney(compareAt)
	if !ok || base < current || base == 0 {
		return 0, false
	}
	numerator := new(big.Int).Mul(big.NewInt(base-current), big.NewInt(100))
	numerator.Quo(numerator, big.NewInt(base))
	if !numerator.IsInt64() || numerator.Int64() < 0 || numerator.Int64() > 100 {
		return 0, false
	}
	return numerator.Int64(), true
}

var _ sdk.PriceWriter = (*Connector)(nil)
