package shopware

import "encoding/json"

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
type shopwareCalculatedPrice struct {
	TotalPrice json.Number `json:"totalPrice"`
}
type shopwareRefund struct {
	ID        string                  `json:"id"`
	Reason    string                  `json:"reason"`
	Amount    shopwareCalculatedPrice `json:"amount"`
	CreatedAt string                  `json:"createdAt"`
}
