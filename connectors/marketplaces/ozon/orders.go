package ozon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type postingCursor struct {
	Offset int64 `json:"offset"`
}

type postingListRequest struct {
	Dir    string `json:"dir"`
	Filter struct {
		Since string `json:"since"`
		To    string `json:"to"`
	} `json:"filter"`
	Limit  int64 `json:"limit"`
	Offset int64 `json:"offset"`
	With   struct {
		Barcodes bool `json:"barcodes"`
	} `json:"with"`
}

type postingListResponse struct {
	Result struct {
		Postings []struct {
			PostingNumber string `json:"posting_number"`
			OrderNumber   string `json:"order_number"`
			Status        string `json:"status"`
			CreatedAt     string `json:"created_at"`
			InProcessAt   string `json:"in_process_at"`
			Products      []struct {
				ProductID int64  `json:"product_id"`
				OfferID   string `json:"offer_id"`
				Quantity  int64  `json:"quantity"`
			} `json:"products"`
		} `json:"postings"`
		HasNext bool `json:"has_next"`
	} `json:"result"`
}

// ReadOrders imports Ozon FBS postings through the bounded list endpoint.
// Provider statuses remain remote identifiers here and are mapped by the
// host runtime before entering the canonical order lifecycle.
func (connector *Connector) ReadOrders(ctx context.Context, account sdk.Account, runtime sdk.Runtime, page sdk.PageRequest) (sdk.OrderPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.read") != nil || page.Validate(100) != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	cursor, err := decodePostingCursor(page.Cursor)
	if err != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	now := connector.now().UTC()
	request := postingListRequest{Dir: "ASC", Limit: int64(page.Limit), Offset: cursor.Offset}
	request.Filter.Since = now.Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano)
	request.Filter.To = now.Format(time.RFC3339Nano)
	request.With.Barcodes = true
	body, _ := json.Marshal(request)
	var output sdk.OrderPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(clientID, apiKey []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/v3/posting/fbs/list", Body: body, ClientID: clientID, APIKey: apiKey})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		var parsed postingListResponse
		if len(response.Body) == 0 || len(response.Body) > maxBodyBytes || json.Unmarshal(response.Body, &parsed) != nil || len(parsed.Result.Postings) > page.Limit {
			return ErrInvalidResponse
		}
		items := make([]sdk.RemoteOrder, 0, len(parsed.Result.Postings))
		seen := make(map[string]struct{}, len(parsed.Result.Postings))
		for _, remote := range parsed.Result.Postings {
			if !validRemoteText(remote.PostingNumber, 200) || !validRemoteText(remote.Status, 64) || (remote.OrderNumber != "" && !validRemoteText(remote.OrderNumber, 300)) || len(remote.Products) == 0 || len(remote.Products) > 1000 {
				return ErrInvalidResponse
			}
			if _, duplicate := seen[remote.PostingNumber]; duplicate {
				return ErrInvalidResponse
			}
			seen[remote.PostingNumber] = struct{}{}
			createdAt, parseErr := time.Parse(time.RFC3339Nano, remote.CreatedAt)
			if parseErr != nil {
				return ErrInvalidResponse
			}
			updatedAt := createdAt
			if remote.InProcessAt != "" {
				updatedAt, parseErr = time.Parse(time.RFC3339Nano, remote.InProcessAt)
				if parseErr != nil || updatedAt.Before(createdAt) {
					return ErrInvalidResponse
				}
			}
			orderItems := make([]sdk.RemoteOrderItem, 0, len(remote.Products))
			seenItems := make(map[string]struct{}, len(remote.Products))
			for index, product := range remote.Products {
				if product.Quantity < 1 || product.ProductID < 1 || (product.OfferID != "" && !validRemoteText(product.OfferID, 200)) {
					return ErrInvalidResponse
				}
				variantID := product.OfferID
				if variantID == "" {
					variantID = strconv.FormatInt(product.ProductID, 10)
				}
				itemID := variantID + ":" + strconv.Itoa(index)
				if _, duplicate := seenItems[itemID]; duplicate {
					return ErrInvalidResponse
				}
				seenItems[itemID] = struct{}{}
				item := sdk.RemoteOrderItem{RemoteID: itemID, VariantRemoteID: variantID, Quantity: product.Quantity}
				if item.Validate() != nil {
					return ErrInvalidResponse
				}
				orderItems = append(orderItems, item)
			}
			order := sdk.RemoteOrder{RemoteID: remote.PostingNumber, ExternalID: remote.OrderNumber, StatusRemoteID: remote.Status, CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC(), Items: orderItems}
			if order.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, order)
		}
		output = sdk.OrderPage{Items: items}
		if parsed.Result.HasNext {
			next := cursor.Offset + int64(len(parsed.Result.Postings))
			if next <= cursor.Offset {
				return ErrInvalidResponse
			}
			output.NextCursor, err = encodePostingCursor(postingCursor{Offset: next})
			if err != nil {
				return ErrInvalidResponse
			}
		}
		return output.Validate(page.Limit)
	})
	return output, err
}

func encodePostingCursor(cursor postingCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodePostingCursor(value string) (postingCursor, error) {
	if value == "" {
		return postingCursor{}, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(data) > 128 {
		return postingCursor{}, sdk.ErrInvalidReadRequest
	}
	var cursor postingCursor
	if json.Unmarshal(data, &cursor) != nil || cursor.Offset < 1 {
		return postingCursor{}, sdk.ErrInvalidReadRequest
	}
	return cursor, nil
}
