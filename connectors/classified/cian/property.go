package cian

import (
	"bytes"
	"encoding/xml"
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrInvalidProperty = errors.New("cian: invalid property feed item")

const (
	maxPropertyFeedItems = 10000
	maxPropertyFeedBytes = 32 << 20
)

type PropertyPhone struct {
	CountryCode string
	Number      string
}

type PropertyPhoto struct {
	URL       string
	IsDefault bool
}

type PropertyFeedItem struct {
	Category        string
	ExternalID      string
	Description     string
	Address         string
	Latitude        float64
	Longitude       float64
	RoomsCount      int
	TotalArea       float64
	LivingArea      float64
	KitchenArea     float64
	FloorNumber     int
	FloorsCount     int
	Price           int64
	Currency        string
	MortgageAllowed bool
	SaleType        string
	LeaseTermType   string
	Phones          []PropertyPhone
	Photos          []PropertyPhoto
}

func (p PropertyFeedItem) Validate() error {
	if !validEnum(p.Category, "flatSale", "flatRent") || !validText(p.ExternalID, 300) || !validDescription(p.Description) || !validText(p.Address, 1000) {
		return ErrInvalidProperty
	}
	if p.Category == "flatSale" {
		if p.SaleType == "" {
			p.SaleType = "free"
		}
		if !validEnum(p.SaleType, "free", "alternative") || p.LeaseTermType != "" {
			return ErrInvalidProperty
		}
	}
	if p.Category == "flatRent" {
		if p.LeaseTermType == "" {
			p.LeaseTermType = "longTerm"
		}
		if !validEnum(p.LeaseTermType, "longTerm", "fewMonths") || p.SaleType != "" || p.MortgageAllowed {
			return ErrInvalidProperty
		}
	}
	if p.RoomsCount < 1 || (p.RoomsCount > 7 && p.RoomsCount != 9) || p.TotalArea <= 0 || p.TotalArea > 100000 || p.LivingArea < 0 || p.KitchenArea < 0 || p.LivingArea+p.KitchenArea >= p.TotalArea || p.FloorNumber < -2 || p.FloorsCount < 1 || p.FloorNumber > p.FloorsCount || p.Price <= 0 {
		return ErrInvalidProperty
	}
	if p.Latitude != 0 || p.Longitude != 0 {
		if p.Latitude < -90 || p.Latitude > 90 || p.Longitude < -180 || p.Longitude > 180 {
			return ErrInvalidProperty
		}
	}
	if !validEnum(strings.ToLower(p.Currency), "rur", "usd", "eur") || p.Currency != strings.ToLower(p.Currency) {
		return ErrInvalidProperty
	}
	if len(p.Phones) > 2 || len(p.Photos) > 50 {
		return ErrInvalidProperty
	}
	for _, ph := range p.Phones {
		if !validPhone(ph) {
			return ErrInvalidProperty
		}
	}
	defaults := 0
	for _, photo := range p.Photos {
		if !validHTTPSURL(photo.URL, 4096) {
			return ErrInvalidProperty
		}
		if photo.IsDefault {
			defaults++
		}
	}
	if defaults > 1 {
		return ErrInvalidProperty
	}
	return nil
}

func BuildPropertyFeed(items []PropertyFeedItem) ([]byte, error) {
	if len(items) < 1 || len(items) > maxPropertyFeedItems {
		return nil, ErrInvalidProperty
	}
	root := xmlFeed{Version: 2, Objects: make([]xmlProperty, 0, len(items))}
	seen := map[string]struct{}{}
	for _, item := range items {
		if item.Validate() != nil {
			return nil, ErrInvalidProperty
		}
		if _, ok := seen[item.ExternalID]; ok {
			return nil, ErrInvalidProperty
		}
		seen[item.ExternalID] = struct{}{}
		root.Objects = append(root.Objects, toXMLProperty(item))
	}
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if enc.Encode(root) != nil {
		return nil, ErrInvalidProperty
	}
	buf.WriteByte('\n')
	if buf.Len() > maxPropertyFeedBytes {
		return nil, ErrInvalidProperty
	}
	return buf.Bytes(), nil
}

type xmlFeed struct {
	XMLName xml.Name      `xml:"Feed"`
	Version int           `xml:"Feed_Version"`
	Objects []xmlProperty `xml:"Object"`
}

type xmlProperty struct {
	Category       string          `xml:"Category"`
	ExternalID     string          `xml:"ExternalId"`
	Description    string          `xml:"Description"`
	Address        string          `xml:"Address"`
	Coordinates    *xmlCoordinates `xml:"Coordinates,omitempty"`
	FlatRoomsCount int             `xml:"FlatRoomsCount"`
	TotalArea      float64         `xml:"TotalArea"`
	LivingArea     float64         `xml:"LivingArea,omitempty"`
	KitchenArea    float64         `xml:"KitchenArea,omitempty"`
	FloorNumber    int             `xml:"FloorNumber"`
	Building       xmlBuilding     `xml:"Building"`
	Phones         *xmlPhones      `xml:"Phones,omitempty"`
	Photos         *xmlPhotos      `xml:"Photos,omitempty"`
	BargainTerms   xmlBargainTerms `xml:"BargainTerms"`
}

type xmlCoordinates struct {
	Lat float64 `xml:"Lat"`
	Lng float64 `xml:"Lng"`
}
type xmlBuilding struct {
	FloorsCount int `xml:"FloorsCount"`
}
type xmlPhones struct {
	Values []xmlPhone `xml:"PhoneSchema"`
}
type xmlPhone struct {
	CountryCode string `xml:"CountryCode"`
	Number      string `xml:"Number"`
}
type xmlPhotos struct {
	Values []xmlPhoto `xml:"PhotoSchema"`
}
type xmlPhoto struct {
	FullURL   string `xml:"FullUrl"`
	IsDefault bool   `xml:"IsDefault"`
}
type xmlBargainTerms struct {
	Price           int64  `xml:"Price"`
	Currency        string `xml:"Currency"`
	MortgageAllowed bool   `xml:"MortgageAllowed,omitempty"`
	SaleType        string `xml:"SaleType,omitempty"`
	LeaseTermType   string `xml:"LeaseTermType,omitempty"`
}

func toXMLProperty(p PropertyFeedItem) xmlProperty {
	saleType, leaseTermType := p.SaleType, p.LeaseTermType
	if p.Category == "flatSale" && saleType == "" {
		saleType = "free"
	}
	if p.Category == "flatRent" && leaseTermType == "" {
		leaseTermType = "longTerm"
	}
	x := xmlProperty{Category: p.Category, ExternalID: p.ExternalID, Description: p.Description, Address: p.Address, FlatRoomsCount: p.RoomsCount, TotalArea: p.TotalArea, LivingArea: p.LivingArea, KitchenArea: p.KitchenArea, FloorNumber: p.FloorNumber, Building: xmlBuilding{FloorsCount: p.FloorsCount}, BargainTerms: xmlBargainTerms{Price: p.Price, Currency: p.Currency, MortgageAllowed: p.MortgageAllowed, SaleType: saleType, LeaseTermType: leaseTermType}}
	if p.Latitude != 0 || p.Longitude != 0 {
		x.Coordinates = &xmlCoordinates{Lat: p.Latitude, Lng: p.Longitude}
	}
	if len(p.Phones) > 0 {
		x.Phones = &xmlPhones{Values: make([]xmlPhone, 0, len(p.Phones))}
		for _, v := range p.Phones {
			x.Phones.Values = append(x.Phones.Values, xmlPhone{CountryCode: v.CountryCode, Number: v.Number})
		}
	}
	if len(p.Photos) > 0 {
		x.Photos = &xmlPhotos{Values: make([]xmlPhoto, 0, len(p.Photos))}
		for _, v := range p.Photos {
			x.Photos.Values = append(x.Photos.Values, xmlPhoto{FullURL: v.URL, IsDefault: v.IsDefault})
		}
	}
	return x
}

func validDescription(v string) bool {
	if !validText(v, 3000) || utf8.RuneCountInString(v) < 15 || strings.Contains(v, "&") {
		return false
	}
	return true
}
func validText(v string, max int) bool {
	if v == "" || v != strings.TrimSpace(v) || !utf8.ValidString(v) || utf8.RuneCountInString(v) > max {
		return false
	}
	for _, r := range v {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
		if r == 0x7f {
			return false
		}
	}
	return true
}
func validPhone(p PropertyPhone) bool {
	if p.CountryCode != "+7" || len(p.Number) != 10 {
		return false
	}
	for _, r := range p.Number {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func validEnum(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}
