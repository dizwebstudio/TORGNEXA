package magento

import (
	"encoding/base64"
	"encoding/json"
)

type pageCursor struct {
	Page        int    `json:"page"`
	Fingerprint string `json:"fingerprint"`
}

func decodePageCursor(value, fingerprint string) (int, error) {
	if value == "" {
		return 1, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) > 512 {
		return 0, ErrInvalidResponse
	}
	var cursor pageCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.Page < 1 || cursor.Page > 1_000_000 || cursor.Fingerprint != fingerprint {
		return 0, ErrInvalidResponse
	}
	return cursor.Page, nil
}
func encodePageCursor(page int, fingerprint string) (string, error) {
	if page < 1 || page > 1_000_000 || len(fingerprint) != 64 {
		return "", ErrInvalidResponse
	}
	raw, err := json.Marshal(pageCursor{Page: page, Fingerprint: fingerprint})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// nextCursor follows Magento's own searchCriteria page/pageSize/total_count
// pagination contract. Magento is documented to return the last valid page
// (not an empty set) when currentPage*pageSize exceeds total_count, so the
// bound is checked before ever requesting a further page, rather than
// inferred from an empty response.
func nextCursor(page, pageSize, totalCount int, fingerprint string) (string, error) {
	if page*pageSize >= totalCount {
		return "", nil
	}
	return encodePageCursor(page+1, fingerprint)
}

type searchFilter struct {
	Field     string
	Value     string
	Condition string
}

// searchCriteriaQuery builds Magento's nested bracket-notation searchCriteria
// query parameters (https://developer.adobe.com/commerce/webapi/rest/use-rest/performing-searches/):
// one filter group per filter, each satisfied with AND semantics (every
// filter must match), which is sufficient for this connector's single-field
// exact-match lookups.
func searchCriteriaQuery(page, pageSize int, filters []searchFilter) []QueryParam {
	query := []QueryParam{
		{Name: "searchCriteria[currentPage]", Value: intString(page)},
		{Name: "searchCriteria[pageSize]", Value: intString(pageSize)},
		{Name: "searchCriteria[sortOrders][0][field]", Value: "updated_at"},
		{Name: "searchCriteria[sortOrders][0][direction]", Value: "ASC"},
	}
	for index, filter := range filters {
		group := "searchCriteria[filter_groups][" + intString(index) + "][filters][0]"
		condition := filter.Condition
		if condition == "" {
			condition = "eq"
		}
		query = append(query,
			QueryParam{Name: group + "[field]", Value: filter.Field},
			QueryParam{Name: group + "[value]", Value: filter.Value},
			QueryParam{Name: group + "[condition_type]", Value: condition},
		)
	}
	return query
}
