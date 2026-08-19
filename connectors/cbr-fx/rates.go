package cbrfx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"strconv"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type dailyDocument struct {
	XMLName xml.Name    `xml:"ValCurs"`
	Date    string      `xml:"Date,attr"`
	Items   []dailyItem `xml:"Valute"`
}
type dailyItem struct {
	ID       string `xml:"ID,attr"`
	CharCode string `xml:"CharCode"`
	Nominal  string `xml:"Nominal"`
	Value    string `xml:"Value"`
}

func parseDaily(body []byte, _ time.Time) (dailyDocument, error) {
	if len(body) == 0 || len(body) > 4<<20 {
		return dailyDocument{}, errors.New("cbr-fx: invalid document")
	}
	raw := strings.ReplaceAll(string(body), `encoding="windows-1251"`, `encoding="UTF-8"`)
	raw = strings.ToValidUTF8(raw, "?")
	var d dailyDocument
	dec := xml.NewDecoder(strings.NewReader(raw))
	if err := dec.Decode(&d); err != nil {
		return dailyDocument{}, errors.New("cbr-fx: invalid XML")
	}
	if d.Date == "" || len(d.Items) == 0 {
		return dailyDocument{}, errors.New("cbr-fx: empty daily document")
	}
	if _, err := parseCBRDate(d.Date); err != nil {
		return dailyDocument{}, err
	}
	return d, nil
}
func parseCBRDate(s string) (time.Time, error) {
	loc := time.FixedZone("MSK", 3*60*60)
	t, err := time.ParseInLocation("02.01.2006", s, loc)
	if err != nil {
		return time.Time{}, errors.New("cbr-fx: invalid effective date")
	}
	return t.UTC(), nil
}
func unitRate(value, nominal string) (string, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), ",", ".")
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return "", errors.New("cbr-fx: invalid rate")
	}
	digits := parts[0]
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	for _, r := range digits + frac {
		if r < '0' || r > '9' {
			return "", errors.New("cbr-fx: invalid rate")
		}
	}
	n, err := strconv.ParseInt(strings.TrimSpace(nominal), 10, 64)
	if err != nil || n < 1 {
		return "", errors.New("cbr-fx: invalid nominal")
	}
	shift := 0
	for n > 1 && n%10 == 0 {
		n /= 10
		shift++
	}
	if n != 1 {
		return "", errors.New("cbr-fx: non-decimal nominal unsupported")
	}
	scale := len(frac) + shift
	if scale > 9 {
		return "", errors.New("cbr-fx: rate precision exceeds platform limit")
	}
	all := strings.TrimLeft(digits+frac, "0")
	if all == "" {
		return "", errors.New("cbr-fx: zero rate")
	}
	for len(all) <= scale {
		all = "0" + all
	}
	if scale == 0 {
		return strings.TrimLeft(all, "0"), nil
	}
	cut := len(all) - scale
	out := all[:cut] + "." + all[cut:]
	out = strings.TrimRight(out, "0")
	out = strings.TrimRight(out, ".")
	if strings.HasPrefix(out, ".") {
		out = "0" + out
	}
	if out == "" || out == "0" {
		return "", errors.New("cbr-fx: zero rate")
	}
	return out, nil
}
func (c *Connector) ReadFXRate(ctx context.Context, account sdk.Account, _ sdk.Runtime, req sdk.FXRateRequest) (sdk.FXRateObservation, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || req.Validate() != nil {
		return sdk.FXRateObservation{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	if req.QuoteCurrency != "RUB" || req.BaseCurrency == "RUB" || req.RateType != "official" {
		return sdk.FXRateObservation{}, remote(sdk.ErrorUnsupported, "pair_unsupported", 0)
	}
	body, err := c.transport.Daily(ctx, req.AsOf)
	if err != nil {
		return sdk.FXRateObservation{}, remote(sdk.ErrorUnavailable, "remote_unavailable", 0)
	}
	doc, err := parseDaily(body, c.now())
	if err != nil {
		return sdk.FXRateObservation{}, remote(sdk.ErrorInternal, "remote_contract_invalid", 0)
	}
	effective, err := parseCBRDate(doc.Date)
	if err != nil || effective.After(req.AsOf) {
		return sdk.FXRateObservation{}, remote(sdk.ErrorNotFound, "rate_missing", 0)
	}
	var item *dailyItem
	for i := range doc.Items {
		if doc.Items[i].CharCode == req.BaseCurrency {
			item = &doc.Items[i]
			break
		}
	}
	if item == nil {
		return sdk.FXRateObservation{}, remote(sdk.ErrorNotFound, "rate_missing", 0)
	}
	rate, err := unitRate(item.Value, item.Nominal)
	if err != nil {
		return sdk.FXRateObservation{}, remote(sdk.ErrorInternal, "remote_contract_invalid", 0)
	}
	ref := "daily/" + effective.Format("2006-01-02") + "/" + item.ID
	sum := sha256.Sum256([]byte(ref + "|" + req.BaseCurrency + "/" + req.QuoteCurrency + "|" + rate))
	obs := sdk.FXRateObservation{ID: "cbr:" + effective.Format("2006-01-02") + ":" + req.BaseCurrency + ":" + req.QuoteCurrency + ":" + hex.EncodeToString(sum[:6]), BaseCurrency: req.BaseCurrency, QuoteCurrency: req.QuoteCurrency, Rate: rate, Source: "cbr", SourceReference: ref, RateType: "official", ObservedAt: c.now().UTC(), EffectiveAt: effective}
	if obs.Validate() != nil {
		return sdk.FXRateObservation{}, remote(sdk.ErrorInternal, "remote_contract_invalid", 0)
	}
	return obs, nil
}
