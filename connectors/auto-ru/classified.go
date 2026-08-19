package autoru

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func encodePage(page int) string {
	if page <= 1 {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(page)))
}

func decodePage(cursor string) (int, error) {
	if cursor == "" {
		return 1, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, sdk.ErrInvalidClassified
	}
	page, err := strconv.Atoi(string(b))
	if err != nil || page < 2 || page > 1000000 {
		return 0, sdk.ErrInvalidClassified
	}
	return page, nil
}

type offerEnvelope struct {
	Offers []offer `json:"offers"`
	Result struct {
		Offers []offer `json:"offers"`
	} `json:"result"`
	Pagination struct {
		Page             int `json:"page"`
		PageSize         int `json:"page_size"`
		TotalOffersCount int `json:"total_offers_count"`
		TotalPageCount   int `json:"total_page_count"`
	} `json:"pagination"`
}

type offer struct {
	ID                    json.RawMessage `json:"id"`
	Status                string          `json:"status"`
	State                 string          `json:"state"`
	FeedprocessorUniqueID string          `json:"feedprocessor_unique_id"`
	PriceInfo             struct {
		Price    json.Number `json:"price"`
		Currency string      `json:"currency"`
	} `json:"price_info"`
	CarInfo struct {
		MarkInfo struct {
			Name string `json:"name"`
		} `json:"mark_info"`
		ModelInfo struct {
			Name string `json:"name"`
		} `json:"model_info"`
	} `json:"car_info"`
	Documents struct {
		Year int `json:"year"`
	} `json:"documents"`
	AdditionalInfo struct {
		UpdateDate json.RawMessage `json:"update_date"`
	} `json:"additional_info"`
}

func (c *Connector) ReadClassifiedListings(ctx context.Context, a sdk.Account, rt sdk.Runtime, p sdk.PageRequest) (out sdk.ClassifiedListingPage, err error) {
	if sdk.RequireCapability(Manifest(), "classified.listings.read") != nil || p.Validate(100) != nil {
		return out, sdk.ErrInvalidClassified
	}
	page, err := decodePage(p.Cursor)
	if err != nil {
		return out, err
	}
	err = c.with(ctx, a, rt, func(cfg Configuration, creds credentials) error {
		q := "page=" + strconv.Itoa(page) + "&page_size=" + strconv.Itoa(p.Limit)
		r, err := c.transport.Do(ctx, request("GET", "/user/offers/cars", q, nil, cfg, creds))
		if err != nil {
			return remote(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
		}
		if err = normalize(r, false); err != nil {
			return err
		}
		var root offerEnvelope
		dec := json.NewDecoder(bytes.NewReader(r.Body))
		dec.UseNumber()
		if dec.Decode(&root) != nil {
			return ErrInvalidResponse
		}
		offers := root.Offers
		if len(offers) == 0 && len(root.Result.Offers) > 0 {
			offers = root.Result.Offers
		}
		for _, v := range offers {
			id, ok := scalarID(v.ID)
			if !ok {
				return ErrInvalidResponse
			}
			status := strings.TrimSpace(v.Status)
			if status == "" {
				status = strings.TrimSpace(v.State)
			}
			year := v.Documents.Year
			title := strings.TrimSpace(strings.Join(nonempty(v.CarInfo.MarkInfo.Name, v.CarInfo.ModelInfo.Name, func() string {
				if year > 0 {
					return strconv.Itoa(year)
				}
				return ""
			}()), " "))
			updated, ok := providerTime(v.AdditionalInfo.UpdateDate)
			if !ok || status == "" || title == "" {
				return ErrInvalidResponse
			}
			item := sdk.ClassifiedListing{
				RemoteID:   id,
				ExternalID: strings.TrimSpace(v.FeedprocessorUniqueID),
				Title:      title,
				Status:     strings.ToUpper(status),
				UpdatedAt:  updated,
			}
			if v.PriceInfo.Price.String() != "" {
				item.Price = v.PriceInfo.Price.String()
				item.Currency = normalizeCurrency(v.PriceInfo.Currency)
			}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			out.Items = append(out.Items, item)
		}
		if root.Pagination.TotalPageCount > page || (root.Pagination.TotalOffersCount > page*p.Limit) || (root.Pagination.TotalPageCount == 0 && len(offers) == p.Limit) {
			out.NextCursor = encodePage(page + 1)
		}
		return out.Validate(p.Limit)
	})
	return out, err
}

func (c *Connector) PublishClassified(ctx context.Context, a sdk.Account, rt sdk.Runtime, in sdk.ClassifiedPublicationRequest) (out sdk.ClassifiedPublicationReceipt, err error) {
	if sdk.RequireCapability(Manifest(), "classified.publications.write") != nil || in.Validate() != nil || in.Kind != sdk.ClassifiedPublicationVehicle {
		return out, sdk.ErrInvalidClassified
	}
	section := strings.ToUpper(string(in.Section))
	body, _ := json.Marshal(struct {
		Source string `json:"source"`
	}{Source: in.SourceURL})
	err = c.with(ctx, a, rt, func(cfg Configuration, creds credentials) error {
		r, err := c.transport.Do(ctx, request("POST", "/feeds/tasks/cars/"+section, "", body, cfg, creds))
		if err != nil {
			return remote(sdk.ErrorConflict, "write_outcome_unknown", "", 0)
		}
		if err = normalize(r, true); err != nil {
			return err
		}
		id, ok := taskID(r.Body)
		if !ok {
			return ErrInvalidResponse
		}
		out = sdk.ClassifiedPublicationReceipt{RemoteTaskID: id, State: sdk.ClassifiedPublicationSubmitted}
		return out.Validate()
	})
	return out, err
}

func (c *Connector) ReadClassifiedPublicationStatus(ctx context.Context, a sdk.Account, rt sdk.Runtime, remoteTaskID string) (out sdk.ClassifiedPublicationStatus, err error) {
	if sdk.RequireCapability(Manifest(), "classified.publications.status.read") != nil || !safeID(remoteTaskID) {
		return out, sdk.ErrInvalidClassified
	}
	err = c.with(ctx, a, rt, func(cfg Configuration, creds credentials) error {
		r, err := c.transport.Do(ctx, request("GET", "/feeds/history/"+remoteTaskID, "page=1&page_size=1", nil, cfg, creds))
		if err != nil {
			return remote(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
		}
		if err = normalize(r, false); err != nil {
			return err
		}
		var root feedHistoryEnvelope
		dec := json.NewDecoder(bytes.NewReader(r.Body))
		dec.UseNumber()
		if dec.Decode(&root) != nil {
			return ErrInvalidResponse
		}
		v := root.task()
		id := v.ID.String()
		if id == "" {
			id = v.TaskID.String()
		}
		if id == "" {
			id = remoteTaskID
		}
		if id != remoteTaskID {
			return ErrInvalidResponse
		}
		state, ok := publicationState(v.Status)
		if !ok {
			return ErrInvalidResponse
		}
		out = sdk.ClassifiedPublicationStatus{
			RemoteTaskID: id,
			State:        state,
			Total:        v.Offers,
			Inserted:     v.Inserted,
			Updated:      v.Updated,
			Deleted:      v.Deleted,
			Skipped:      v.Skipped,
			Errors:       v.Errors,
			Notices:      v.Notices,
			CheckedAt:    c.now().UTC(),
		}
		return out.Validate()
	})
	return out, err
}

type feedTask struct {
	ID       json.Number `json:"id"`
	TaskID   json.Number `json:"task_id"`
	Status   string      `json:"status"`
	Offers   int64       `json:"count_offers"`
	Inserted int64       `json:"count_offers_inserted"`
	Updated  int64       `json:"count_offers_updated"`
	Deleted  int64       `json:"count_offers_deleted"`
	Skipped  int64       `json:"count_offers_skipped"`
	Errors   int64       `json:"count_errors"`
	Notices  int64       `json:"count_notices"`
}

type feedHistoryEnvelope struct {
	Task feedTask `json:"task"`
}

func (e feedHistoryEnvelope) task() feedTask { return e.Task }

func taskID(body []byte) (string, bool) {
	var root struct {
		ID     json.Number `json:"id"`
		TaskID json.Number `json:"task_id"`
		Task   struct {
			ID     json.Number `json:"id"`
			TaskID json.Number `json:"task_id"`
		} `json:"task"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if dec.Decode(&root) != nil {
		return "", false
	}
	for _, v := range []json.Number{root.ID, root.TaskID, root.Task.ID, root.Task.TaskID} {
		if safeID(v.String()) {
			return v.String(), true
		}
	}
	return "", false
}

func scalarID(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && safeID(s) {
		return s, true
	}
	var n json.Number
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&n) == nil && safeID(n.String()) {
		return n.String(), true
	}
	return "", false
}

func providerTime(raw json.RawMessage) (time.Time, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if at, err := time.Parse(time.RFC3339, s); err == nil {
			return at.UTC(), true
		}
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return unixAuto(v)
		}
	}
	var n json.Number
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&n) == nil {
		if v, err := strconv.ParseInt(n.String(), 10, 64); err == nil {
			return unixAuto(v)
		}
	}
	return time.Time{}, false
}

func unixAuto(v int64) (time.Time, bool) {
	if v <= 0 {
		return time.Time{}, false
	}
	if v > 100000000000 {
		v /= 1000
	}
	if v < 946684800 || v > 4102444800 {
		return time.Time{}, false
	}
	return time.Unix(v, 0).UTC(), true
}

func publicationState(v string) (sdk.ClassifiedPublicationState, bool) {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "NEW":
		return sdk.ClassifiedPublicationSubmitted, true
	case "PROCESSING":
		return sdk.ClassifiedPublicationProcessing, true
	case "SUCCESS":
		return sdk.ClassifiedPublicationSucceeded, true
	case "FAILURE", "FAILED":
		return sdk.ClassifiedPublicationFailed, true
	default:
		return "", false
	}
}

func normalizeCurrency(v string) string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "RUR", "RUB":
		return "RUB"
	case "EUR":
		return "EUR"
	case "USD":
		return "USD"
	default:
		return strings.ToUpper(strings.TrimSpace(v))
	}
}

func nonempty(v ...string) []string {
	out := make([]string, 0, len(v))
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func safeID(v string) bool {
	if v == "" || v != strings.TrimSpace(v) || len(v) > 300 {
		return false
	}
	for _, r := range v {
		if r <= 0x20 || r == 0x7f || r == '/' || r == '?' || r == '#' {
			return false
		}
	}
	return true
}
