package builtinruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	ozondelivery "github.com/torgnexa/torgnexa/connectors/ozon-delivery"
	ozonpay "github.com/torgnexa/torgnexa/connectors/ozon-pay"
)

const ozonSellerAPIHost = "api-seller.ozon.ru"

type ozonPayHTTP struct{ h *httpTransport }

// Ping verifies Seller API access used as the prerequisite for Ozon Pay.
// The probe does not claim that a merchant has completed Ozon Pay activation.
func (transport ozonPayHTTP) Ping(ctx context.Context, secret []byte) error {
	return transport.h.pingOzonSeller(ctx, secret, "/v3/product/list", []byte(`{"filter":{},"limit":1}`), "Ozon Pay")
}

type ozonDeliveryHTTP struct{ h *httpTransport }

// Ping verifies the Seller API warehouse surface used by Ozon Delivery.
// Shipment creation, rates and tracking remain separate qualification steps.
func (transport ozonDeliveryHTTP) Ping(ctx context.Context, secret []byte) error {
	return transport.h.pingOzonSeller(ctx, secret, "/v2/warehouse/list", []byte(`{"limit":1}`), "Ozon Доставка")
}

func (h *httpTransport) pingOzonSeller(ctx context.Context, secret []byte, path string, body []byte, provider string) error {
	if h == nil {
		return errors.New(provider + " credential probe unavailable")
	}
	var credentials struct {
		ClientID string `json:"client_id"`
		APIKey   string `json:"api_key"`
	}
	decoder := json.NewDecoder(bytes.NewReader(secret))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&credentials) != nil || decoder.Decode(&struct{}{}) != io.EOF || strings.TrimSpace(credentials.ClientID) == "" || strings.TrimSpace(credentials.APIKey) == "" {
		return errors.New(provider + " credentials must be JSON with client_id and api_key")
	}
	if path == "" || !strings.HasPrefix(path, "/") {
		return errors.New(provider + " credential probe path unavailable")
	}
	headers := http.Header{
		"Client-Id":    []string{credentials.ClientID},
		"Api-Key":      []string{credentials.APIKey},
		"Content-Type": []string{"application/json"},
		"Accept":       []string{"application/json"},
	}
	status, _, _, _, _, err := h.do(ctx, http.MethodPost, ozonSellerAPIHost, path, url.Values{}, body, headers, nil, nil)
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("%s credential probe rejected with status %d", provider, status)
	}
	return nil
}

var _ ozonpay.Transport = ozonPayHTTP{}
var _ ozondelivery.Transport = ozonDeliveryHTTP{}
