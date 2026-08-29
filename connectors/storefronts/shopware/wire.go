package shopware

import "encoding/json"

// unmarshalResource flattens the JSON:API representation emitted by current
// Shopware Admin API versions while retaining compatibility with the compact
// entity representation used by older installations and our SDK fixtures.
// JSON:API puts entity fields below data.attributes and keeps the resource id
// beside that object; the connector's domain code intentionally works with a
// provider-neutral, flat shape.
func unmarshalResource(data []byte, target any, id *string) error {
	var envelope struct {
		ID         string          `json:"id"`
		Attributes json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	if len(envelope.Attributes) == 0 || string(envelope.Attributes) == "null" {
		if err := json.Unmarshal(data, target); err != nil {
			return err
		}
		if id != nil && *id == "" {
			*id = envelope.ID
		}
		return nil
	}
	if err := json.Unmarshal(envelope.Attributes, target); err != nil {
		return err
	}
	if id != nil && *id == "" {
		*id = envelope.ID
	}
	return nil
}

// shopwarePrice.Gross/Net use json.Number rather than float64 to avoid float
// rounding on re-encode; Shopware serializes them as plain JSON numbers.
type shopwarePrice struct {
	CurrencyID string      `json:"currencyId"`
	Gross      json.Number `json:"gross"`
	Net        json.Number `json:"net"`
}
type shopwareProduct struct {
	ID            string          `json:"id"`
	ParentID      *string         `json:"parentId"`
	ChildCount    int             `json:"childCount"`
	ProductNumber string          `json:"productNumber"`
	Name          string          `json:"name"`
	Active        bool            `json:"active"`
	Stock         int64           `json:"stock"`
	Price         []shopwarePrice `json:"price"`
	UpdatedAt     string          `json:"updatedAt"`
}

func (product *shopwareProduct) UnmarshalJSON(data []byte) error {
	type plain shopwareProduct
	var value plain
	var id string
	if err := unmarshalResource(data, &value, &id); err != nil {
		return err
	}
	*product = shopwareProduct(value)
	if product.ID == "" {
		product.ID = id
	}
	return nil
}

type shopwareCurrency struct {
	ID      string `json:"id"`
	IsoCode string `json:"isoCode"`
}

func (currency *shopwareCurrency) UnmarshalJSON(data []byte) error {
	type plain shopwareCurrency
	var value plain
	var id string
	if err := unmarshalResource(data, &value, &id); err != nil {
		return err
	}
	*currency = shopwareCurrency(value)
	if currency.ID == "" {
		currency.ID = id
	}
	return nil
}

// shopwareSearchPage is the common response envelope of POST /api/search/*.
// Shopware 6.7 returns the total in meta.total; older releases and test
// fixtures used a top-level total, so both forms are accepted.
type shopwareSearchPage[T any] struct {
	Data  []T `json:"data"`
	Total int `json:"total"`
	Meta  struct {
		Total int `json:"total"`
	} `json:"meta"`
}

func (page *shopwareSearchPage[T]) UnmarshalJSON(data []byte) error {
	type plain shopwareSearchPage[T]
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*page = shopwareSearchPage[T](value)
	if page.Total == 0 {
		page.Total = page.Meta.Total
	}
	return nil
}

type shopwareStateMachineState struct {
	TechnicalName string `json:"technicalName"`
}
type shopwareLineItem struct {
	ID           string      `json:"id"`
	ReferencedID string      `json:"referencedId"`
	Quantity     json.Number `json:"quantity"`
}
type shopwareOrder struct {
	ID                string                     `json:"id"`
	OrderNumber       string                     `json:"orderNumber"`
	StateMachineState *shopwareStateMachineState `json:"stateMachineState"`
	CreatedAt         string                     `json:"createdAt"`
	UpdatedAt         string                     `json:"updatedAt"`
	LineItems         []shopwareLineItem         `json:"lineItems"`
}

func (order *shopwareOrder) UnmarshalJSON(data []byte) error {
	type plain shopwareOrder
	var value plain
	var id string
	if err := unmarshalResource(data, &value, &id); err != nil {
		return err
	}
	*order = shopwareOrder(value)
	if order.ID == "" {
		order.ID = id
	}
	return nil
}

type shopwareCalculatedPrice struct {
	TotalPrice json.Number `json:"totalPrice"`
}
type shopwareRefund struct {
	ID        string                  `json:"id"`
	Reason    string                  `json:"reason"`
	Amount    shopwareCalculatedPrice `json:"amount"`
	CreatedAt string                  `json:"createdAt"`
}

func (refund *shopwareRefund) UnmarshalJSON(data []byte) error {
	type plain shopwareRefund
	var value plain
	var id string
	if err := unmarshalResource(data, &value, &id); err != nil {
		return err
	}
	*refund = shopwareRefund(value)
	if refund.ID == "" {
		refund.ID = id
	}
	return nil
}
