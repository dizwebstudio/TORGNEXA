package wildberries

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type productCursor struct {
	UpdatedAt string `json:"updatedAt,omitempty"`
	NmID      int64  `json:"nmID,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type cardsRequest struct {
	Settings struct {
		Cursor productCursor `json:"cursor"`
		Filter struct {
			WithPhoto int `json:"withPhoto"`
		} `json:"filter"`
	} `json:"settings"`
}

type cardsResponse struct {
	Cards []struct {
		NmID       int64  `json:"nmID"`
		VendorCode string `json:"vendorCode"`
		Title      string `json:"title"`
		Brand      string `json:"brand"`
		UpdatedAt  string `json:"updatedAt"`
		Sizes      []struct {
			ChrtID int64    `json:"chrtID"`
			SKUs   []string `json:"skus"`
		} `json:"sizes"`
	} `json:"cards"`
	Cursor struct {
		UpdatedAt string `json:"updatedAt"`
		NmID      int64  `json:"nmID"`
		Total     int    `json:"total"`
	} `json:"cursor"`
}

func (connector *Connector) ReadProducts(ctx context.Context, account sdk.Account, runtime sdk.Runtime, page sdk.PageRequest) (sdk.ProductPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || page.Validate(100) != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	cursor, err := decodeCursor(page.Cursor)
	if err != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	var request cardsRequest
	request.Settings.Cursor = cursor
	request.Settings.Cursor.Limit = page.Limit
	request.Settings.Filter.WithPhoto = -1
	body, _ := json.Marshal(request)
	var output sdk.ProductPage
	err = runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: contentHost, Path: "/content/v2/get/cards/list", Body: body, Token: secret})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		var parsed cardsResponse
		if json.Unmarshal(response.Body, &parsed) != nil || len(parsed.Cards) > page.Limit || parsed.Cursor.Total < 0 || parsed.Cursor.Total != len(parsed.Cards) {
			return ErrInvalidResponse
		}
		items := make([]sdk.RemoteProduct, 0, len(parsed.Cards))
		for _, card := range parsed.Cards {
			updated, parseErr := time.Parse(time.RFC3339Nano, card.UpdatedAt)
			if parseErr != nil {
				return ErrInvalidResponse
			}
			item := sdk.RemoteProduct{RemoteID: strconv.FormatInt(card.NmID, 10), SellerSKU: card.VendorCode, Title: card.Title, Brand: card.Brand, UpdatedAt: updated.UTC()}
			for _, size := range card.Sizes {
				item.Variants = append(item.Variants, sdk.RemoteVariant{RemoteID: strconv.FormatInt(size.ChrtID, 10), SKUs: append([]string(nil), size.SKUs...)})
			}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		output.Items = items
		if parsed.Cursor.Total >= page.Limit && parsed.Cursor.UpdatedAt != "" && parsed.Cursor.NmID > 0 {
			output.NextCursor, err = encodeCursor(productCursor{UpdatedAt: parsed.Cursor.UpdatedAt, NmID: parsed.Cursor.NmID})
			if err != nil {
				return ErrInvalidResponse
			}
		}
		return output.Validate(page.Limit)
	})
	return output, err
}

func encodeCursor(cursor productCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeCursor(value string) (productCursor, error) {
	if value == "" {
		return productCursor{}, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(data) > 512 {
		return productCursor{}, sdk.ErrInvalidReadRequest
	}
	var cursor productCursor
	if json.Unmarshal(data, &cursor) != nil || cursor.NmID < 0 {
		return productCursor{}, sdk.ErrInvalidReadRequest
	}
	if cursor.UpdatedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, cursor.UpdatedAt); err != nil {
			return productCursor{}, sdk.ErrInvalidReadRequest
		}
	}
	if (cursor.UpdatedAt == "") != (cursor.NmID == 0) {
		return productCursor{}, sdk.ErrInvalidReadRequest
	}
	return cursor, nil
}
