package bitrix24

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type flexID string

func (v *flexID) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		*v = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if json.Unmarshal(b, &s) != nil {
			return ErrInvalidResponse
		}
		if s != "" {
			if _, e := strconv.ParseInt(s, 10, 64); e != nil {
				return ErrInvalidResponse
			}
		}
		*v = flexID(s)
		return nil
	}
	var n json.Number
	if json.Unmarshal(b, &n) != nil {
		return ErrInvalidResponse
	}
	if _, e := strconv.ParseInt(string(n), 10, 64); e != nil {
		return ErrInvalidResponse
	}
	*v = flexID(n.String())
	return nil
}

type decimalText string

func (v *decimalText) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		*v = ""
		return nil
	}
	s := string(b)
	if len(b) > 0 && b[0] == '"' {
		if json.Unmarshal(b, &s) != nil {
			return ErrInvalidResponse
		}
	}
	s = strings.TrimSpace(s)
	if s == "" {
		*v = ""
		return nil
	}
	if !decimalPattern.MatchString(s) {
		return ErrInvalidResponse
	}
	*v = decimalText(s)
	return nil
}

var decimalPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]{0,17})(?:\.[0-9]{1,9})?$`)

func parseRemoteTime(s string) (time.Time, error) {
	t, e := time.Parse(time.RFC3339, s)
	if e != nil {
		return time.Time{}, ErrInvalidResponse
	}
	return t.UTC(), nil
}

type remoteItem struct {
	ID           flexID      `json:"id"`
	Title        string      `json:"title"`
	Name         string      `json:"name"`
	SecondName   string      `json:"secondName"`
	LastName     string      `json:"lastName"`
	StageID      string      `json:"stageId"`
	CategoryID   flexID      `json:"categoryId"`
	CompanyID    flexID      `json:"companyId"`
	ContactID    flexID      `json:"contactId"`
	ContactIDs   []flexID    `json:"contactIds"`
	Opportunity  decimalText `json:"opportunity"`
	CurrencyID   string      `json:"currencyId"`
	OriginatorID string      `json:"originatorId"`
	OriginID     string      `json:"originId"`
	CreatedTime  string      `json:"createdTime"`
	UpdatedTime  string      `json:"updatedTime"`
}

type remoteProductRow struct {
	ID          flexID      `json:"id"`
	OwnerID     flexID      `json:"ownerId"`
	OwnerType   string      `json:"ownerType"`
	ProductID   flexID      `json:"productId"`
	ProductName string      `json:"productName"`
	Price       decimalText `json:"price"`
	Quantity    decimalText `json:"quantity"`
	TaxRate     decimalText `json:"taxRate"`
	TaxIncluded string      `json:"taxIncluded"`
}
