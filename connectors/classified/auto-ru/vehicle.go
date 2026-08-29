package autoru

import (
	"bytes"
	"encoding/xml"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

var ErrInvalidVehicle = errors.New("auto-ru: invalid vehicle feed item")

const (
	maxVehicleFeedItems = 10000
	maxVehicleFeedBytes = 16 << 20
)

var uniqueIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,50}$`)

type VehicleFeedItem struct {
	UniqueID          string
	VIN               string
	MarkID            string
	FolderID          string
	ModificationID    string
	ComplectationName string
	NoComplectation   bool
	BodyType          string
	Wheel             string
	Color             string
	Availability      string
	Custom            string
	State             string
	OwnersNumber      string
	Run               int64
	Year              int
	RegistryYear      int
	DoorsCount        int
	Currency          string
	Price             int64
	Description       string
	Images            []string
	Action            string
}

func (v VehicleFeedItem) Validate(section string) error {
	section = strings.ToUpper(strings.TrimSpace(section))
	if section != "NEW" && section != "USED" {
		return ErrInvalidVehicle
	}
	if !validFeedText(v.MarkID, 100) || !validFeedText(v.FolderID, 100) || !validFeedText(v.ModificationID, 100) || !validFeedText(v.BodyType, 100) || !validFeedText(v.Wheel, 100) || !validFeedText(v.Color, 100) || !validFeedText(v.Availability, 100) || !validFeedText(v.Custom, 100) {
		return ErrInvalidVehicle
	}
	if v.VIN == "" && !uniqueIDPattern.MatchString(v.UniqueID) {
		return ErrInvalidVehicle
	}
	if v.VIN != "" && !validVIN(v.VIN) {
		return ErrInvalidVehicle
	}
	if v.UniqueID != "" && !uniqueIDPattern.MatchString(v.UniqueID) {
		return ErrInvalidVehicle
	}
	if v.Year < 1900 || v.Year > 2100 || v.DoorsCount < 1 || v.DoorsCount > 8 || v.Price <= 1500 || !validFeedCurrency(v.Currency) || v.Run < 0 {
		return ErrInvalidVehicle
	}
	if !validEnum(v.Wheel, "Левый", "Правый") || !validEnum(v.Availability, "В наличии", "На заказ") || !validEnum(v.Custom, "Растаможен", "Не растаможен") {
		return ErrInvalidVehicle
	}
	if section == "NEW" {
		if (!v.NoComplectation && !validFeedText(v.ComplectationName, 200)) || (v.NoComplectation && v.ComplectationName != "") || v.State != "" || v.Run != 0 || v.OwnersNumber != "Не было владельцев" {
			return ErrInvalidVehicle
		}
		if v.RegistryYear != 0 && (v.RegistryYear < v.Year || v.RegistryYear > 2100) {
			return ErrInvalidVehicle
		}
	} else {
		if !validEnum(v.State, "Отличное", "Хорошее", "Среднее", "Плохое") || !validEnum(v.OwnersNumber, "Один владелец", "Два владельца", "Три владельца", "Четыре и более") || v.Run <= 0 || v.RegistryYear < v.Year || v.RegistryYear > 2100 {
			return ErrInvalidVehicle
		}
	}
	if v.Description != "" && (!utf8.ValidString(v.Description) || v.Description != strings.TrimSpace(v.Description) || utf8.RuneCountInString(v.Description) > 30000) {
		return ErrInvalidVehicle
	}
	if len(v.Images) > 40 {
		return ErrInvalidVehicle
	}
	for _, raw := range v.Images {
		if !validImageURL(raw) {
			return ErrInvalidVehicle
		}
	}
	if v.Action != "" && v.Action != "hide" && v.Action != "show" {
		return ErrInvalidVehicle
	}
	return nil
}

func BuildVehicleFeed(section string, items []VehicleFeedItem) ([]byte, error) {
	section = strings.ToUpper(strings.TrimSpace(section))
	if len(items) < 1 || len(items) > maxVehicleFeedItems {
		return nil, ErrInvalidVehicle
	}
	root := xmlFeed{Cars: make([]xmlVehicle, 0, len(items))}
	for _, item := range items {
		if err := item.Validate(section); err != nil {
			return nil, err
		}
		root.Cars = append(root.Cars, toXMLVehicle(section, item))
	}
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(root); err != nil {
		return nil, ErrInvalidVehicle
	}
	buf.WriteByte('\n')
	if buf.Len() > maxVehicleFeedBytes {
		return nil, ErrInvalidVehicle
	}
	return buf.Bytes(), nil
}

type xmlFeed struct {
	XMLName xml.Name     `xml:"data"`
	Cars    []xmlVehicle `xml:"cars>car"`
}

type xmlVehicle struct {
	MarkID            string     `xml:"mark_id"`
	FolderID          string     `xml:"folder_id"`
	ModificationID    string     `xml:"modification_id"`
	ComplectationName string     `xml:"complectation_name,omitempty"`
	BodyType          string     `xml:"body_type"`
	Wheel             string     `xml:"wheel"`
	Color             string     `xml:"color"`
	Availability      string     `xml:"availability"`
	Custom            string     `xml:"custom"`
	State             string     `xml:"state,omitempty"`
	OwnersNumber      string     `xml:"owners_number,omitempty"`
	Run               *int64     `xml:"run,omitempty"`
	Year              int        `xml:"year"`
	RegistryYear      *int       `xml:"registry_year,omitempty"`
	DoorsCount        int        `xml:"doors_count"`
	Currency          string     `xml:"currency"`
	VIN               string     `xml:"vin,omitempty"`
	Price             int64      `xml:"price"`
	Description       string     `xml:"description,omitempty"`
	Images            *xmlImages `xml:"images,omitempty"`
	UniqueID          string     `xml:"unique_id,omitempty"`
	Action            string     `xml:"action,omitempty"`
}

type xmlImages struct {
	Image []string `xml:"image"`
}

func toXMLVehicle(section string, v VehicleFeedItem) xmlVehicle {
	x := xmlVehicle{
		MarkID:            v.MarkID,
		FolderID:          v.FolderID,
		ModificationID:    v.ModificationID,
		ComplectationName: v.ComplectationName,
		BodyType:          v.BodyType,
		Wheel:             v.Wheel,
		Color:             v.Color,
		Availability:      v.Availability,
		Custom:            v.Custom,
		State:             v.State,
		OwnersNumber:      v.OwnersNumber,
		Year:              v.Year,
		DoorsCount:        v.DoorsCount,
		Currency:          strings.ToUpper(v.Currency),
		VIN:               strings.ToUpper(v.VIN),
		Price:             v.Price,
		Description:       v.Description,
		UniqueID:          v.UniqueID,
		Action:            v.Action,
	}
	if len(v.Images) > 0 {
		x.Images = &xmlImages{Image: append([]string(nil), v.Images...)}
	}
	if section == "USED" {
		run := v.Run
		x.Run = &run
	}
	if v.RegistryYear != 0 {
		registryYear := v.RegistryYear
		x.RegistryYear = &registryYear
	}
	return x
}

func validVIN(v string) bool {
	if len(v) != 17 || v != strings.ToUpper(v) {
		return false
	}
	for _, r := range v {
		if !((r >= 'A' && r <= 'H') || (r >= 'J' && r <= 'N') || r == 'P' || (r >= 'R' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func validFeedText(v string, max int) bool {
	if v == "" || v != strings.TrimSpace(v) || !utf8.ValidString(v) || utf8.RuneCountInString(v) > max {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validFeedCurrency(v string) bool {
	switch strings.ToUpper(v) {
	case "RUR", "EUR", "USD":
		return v == strings.ToUpper(v)
	default:
		return false
	}
}

func validImageURL(raw string) bool {
	const prefix = "https://"
	if raw == "" || raw != strings.TrimSpace(raw) || len(raw) > 4096 || !strings.HasPrefix(raw, prefix) || strings.Contains(raw, "\\") || strings.Contains(raw, "#") {
		return false
	}
	rest := raw[len(prefix):]
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 || slash == len(rest)-1 {
		return false
	}
	authority := rest[:slash]
	if strings.ContainsAny(authority, "@%[]") || authority != strings.TrimSpace(authority) {
		return false
	}
	host := authority
	if colon := strings.LastIndexByte(authority, ':'); colon >= 0 {
		if authority[colon+1:] != "443" || strings.Contains(authority[:colon], ":") {
			return false
		}
		host = authority[:colon]
	}
	if host == "" || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return false
	}
	for _, r := range strings.ToLower(host) {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-') {
			return false
		}
	}
	return true
}

func validEnum(v string, allowed ...string) bool {
	for _, candidate := range allowed {
		if v == candidate {
			return true
		}
	}
	return false
}
