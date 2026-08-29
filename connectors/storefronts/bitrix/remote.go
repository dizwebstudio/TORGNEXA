package bitrix

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type bitrixProduct struct {
	ID          int64       `json:"id"`
	IblockID    int64       `json:"iblockId"`
	Name        string      `json:"name"`
	Active      string      `json:"active"`
	Code        string      `json:"code"`
	XMLID       string      `json:"xmlId"`
	DetailText  string      `json:"detailText"`
	Quantity    json.Number `json:"quantity"`
	TimestampX  string      `json:"timestampX"`
	DateCreated string      `json:"dateCreate"`
}

type bitrixProductList struct {
	Products []bitrixProduct `json:"products"`
}

var bitrixMoneyPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,17})(?:\.[0-9]{1,9})?$`)

type bitrixPrice struct {
	ID             int64       `json:"id"`
	ProductID      int64       `json:"productId"`
	CatalogGroupID int64       `json:"catalogGroupId"`
	Price          json.Number `json:"price"`
	Currency       string      `json:"currency"`
	TimestampX     string      `json:"timestampX"`
}

type bitrixStore struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Active string `json:"active"`
}

type bitrixStoreProduct struct {
	ID        int64       `json:"id"`
	ProductID int64       `json:"productId"`
	StoreID   int64       `json:"storeId"`
	Amount    json.Number `json:"amount"`
}

type bitrixDocument struct {
	ID        int64  `json:"id"`
	DocType   string `json:"docType"`
	DocNumber string `json:"docNumber"`
	Status    string `json:"status"`
}

type bitrixDocumentElement struct {
	ID        int64       `json:"id"`
	DocID     int64       `json:"docId"`
	ElementID int64       `json:"elementId"`
	Amount    json.Number `json:"amount"`
	StoreFrom *int64      `json:"storeFrom"`
	StoreTo   *int64      `json:"storeTo"`
}

// bitrixInteger accepts the numeric and quoted-numeric forms used by the
// Bitrix REST catalog and sale responses. The provider documents both forms
// for basket identifiers, so decoding one shape only would turn a valid
// response into a fail-closed error.
type bitrixInteger int64

func (value *bitrixInteger) UnmarshalJSON(raw []byte) error {
	text := strings.TrimSpace(string(raw))
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		var quoted string
		if json.Unmarshal(raw, &quoted) != nil {
			return ErrInvalidResponse
		}
		text = quoted
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return ErrInvalidResponse
	}
	*value = bitrixInteger(parsed)
	return nil
}

type bitrixSaleOrder struct {
	ID            bitrixInteger `json:"id"`
	AccountNumber string        `json:"accountNumber"`
	XMLID         string        `json:"xmlId"`
	StatusID      string        `json:"statusId"`
	DateInsert    string        `json:"dateInsert"`
	DateUpdate    string        `json:"dateUpdate"`
}

type bitrixBasketItem struct {
	ID        bitrixInteger `json:"id"`
	OrderID   bitrixInteger `json:"orderId"`
	ProductID bitrixInteger `json:"productId"`
	Quantity  json.Number   `json:"quantity"`
}

func decodePriceList(body []byte) ([]bitrixPrice, error) {
	var envelope struct {
		Result struct {
			Prices []bitrixPrice `json:"prices"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &envelope) != nil || len(envelope.Result.Prices) > 50 {
		return nil, ErrInvalidResponse
	}
	return envelope.Result.Prices, nil
}

func decodeStoreList(body []byte) ([]bitrixStore, int, error) {
	var envelope struct {
		Result *struct {
			Stores []bitrixStore `json:"stores"`
		} `json:"result"`
		Total int `json:"total"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Result == nil || envelope.Total < 0 || len(envelope.Result.Stores) > 50 {
		return nil, 0, ErrInvalidResponse
	}
	return envelope.Result.Stores, envelope.Total, nil
}

func decodeStoreProductList(body []byte) ([]bitrixStoreProduct, int, error) {
	var envelope struct {
		Result *struct {
			StoreProducts []bitrixStoreProduct `json:"storeProducts"`
		} `json:"result"`
		Total int `json:"total"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Result == nil || envelope.Total < 0 || len(envelope.Result.StoreProducts) > 50 {
		return nil, 0, ErrInvalidResponse
	}
	return envelope.Result.StoreProducts, envelope.Total, nil
}

func decodeDocumentList(body []byte) ([]bitrixDocument, int, error) {
	var envelope struct {
		Result *struct {
			Documents []bitrixDocument `json:"documents"`
		} `json:"result"`
		Total int `json:"total"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Result == nil || envelope.Total < 0 || len(envelope.Result.Documents) > 50 {
		return nil, 0, ErrInvalidResponse
	}
	return envelope.Result.Documents, envelope.Total, nil
}

func decodeDocumentElementList(body []byte) ([]bitrixDocumentElement, int, error) {
	var envelope struct {
		Result *struct {
			Elements []bitrixDocumentElement `json:"documentElements"`
		} `json:"result"`
		Total int `json:"total"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Result == nil || envelope.Total < 0 || len(envelope.Result.Elements) > 50 {
		return nil, 0, ErrInvalidResponse
	}
	return envelope.Result.Elements, envelope.Total, nil
}

func decodeSaleOrderList(body []byte) ([]bitrixSaleOrder, int, error) {
	var envelope struct {
		Result *struct {
			Orders []bitrixSaleOrder `json:"orders"`
		} `json:"result"`
		Total int `json:"total"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Result == nil || envelope.Total < 0 || len(envelope.Result.Orders) > 50 || len(envelope.Result.Orders) > envelope.Total {
		return nil, 0, ErrInvalidResponse
	}
	return envelope.Result.Orders, envelope.Total, nil
}

func decodeBasketItemList(body []byte) ([]bitrixBasketItem, int, error) {
	var envelope struct {
		Result *struct {
			BasketItems []bitrixBasketItem `json:"basketItems"`
		} `json:"result"`
		Total int `json:"total"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Result == nil || envelope.Total < 0 || len(envelope.Result.BasketItems) > 50 || len(envelope.Result.BasketItems) > envelope.Total {
		return nil, 0, ErrInvalidResponse
	}
	return envelope.Result.BasketItems, envelope.Total, nil
}

func normalizeBitrixMoney(value json.Number) (string, error) {
	text := value.String()
	if !bitrixMoneyPattern.MatchString(text) {
		return "", ErrInvalidResponse
	}
	return text, nil
}

func decodeProductList(body []byte) ([]bitrixProduct, error) {
	var envelope struct {
		Result *bitrixProductList `json:"result"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Result == nil || len(envelope.Result.Products) > 50 {
		return nil, ErrInvalidResponse
	}
	return envelope.Result.Products, nil
}

func parseBitrixTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("bitrix: missing timestamp")
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), nil
	}
	if parsed, err := time.Parse("2006-01-02T15:04:05-07:00", value); err == nil {
		return parsed.UTC(), nil
	}
	return time.Time{}, errors.New("bitrix: invalid timestamp")
}

func productSKU(product bitrixProduct) string {
	if product.XMLID != "" {
		return product.XMLID
	}
	if product.Code != "" {
		return product.Code
	}
	return strconv.FormatInt(product.ID, 10)
}

func productStatus(active string) string {
	if strings.EqualFold(active, "Y") {
		return "publish"
	}
	return "draft"
}

func activeValue(status string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "publish", "enabled", "active", "y":
		return "Y", true
	case "draft", "private", "disabled", "archived", "n":
		return "N", true
	default:
		return "", false
	}
}

func productUpdatedAt(product bitrixProduct) (time.Time, error) {
	if product.TimestampX != "" {
		return parseBitrixTime(product.TimestampX)
	}
	return parseBitrixTime(product.DateCreated)
}

func validRemoteText(value string, max int) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > max {
		return false
	}
	for _, r := range value {
		if r == 0 || r == 0x7f {
			return false
		}
	}
	return true
}
