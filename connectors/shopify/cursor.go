package shopify

import (
	"encoding/base64"
	"encoding/json"
)

type pageCursor struct {
	PageInfo    string `json:"page_info"`
	Fingerprint string `json:"fingerprint"`
}

// decodePageCursor returns "" (Shopify's own signal for "first page") for an
// empty input cursor, and the wrapped page_info token otherwise. Shopify
// requires a page_info request to carry no other filter parameters (see
// listQuery), so the first-page filters are applied only when this returns
// "".
func decodePageCursor(value, fingerprint string) (string, error) {
	if value == "" {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) > 2048 {
		return "", ErrInvalidResponse
	}
	var cursor pageCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.PageInfo == "" || cursor.Fingerprint != fingerprint {
		return "", ErrInvalidResponse
	}
	return cursor.PageInfo, nil
}

func encodePageCursor(pageInfo, fingerprint string) (string, error) {
	if pageInfo == "" || len(pageInfo) > 1024 || len(fingerprint) != 64 {
		return "", ErrInvalidResponse
	}
	raw, err := json.Marshal(pageCursor{PageInfo: pageInfo, Fingerprint: fingerprint})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// nextCursor wraps Shopify's own NextPageInfo (already "" when the response
// carried no Link rel="next" entry) as this connector's opaque cursor.
func nextCursor(nextPageInfo, fingerprint string) (string, error) {
	if nextPageInfo == "" {
		return "", nil
	}
	return encodePageCursor(nextPageInfo, fingerprint)
}

// listQuery builds the query for a paginated list endpoint. Per Shopify's
// pagination contract, a request carrying page_info must carry no other
// parameter except limit (and fields, unused here); firstPageFilters are
// therefore only ever sent on the first page, whose filter shape becomes
// part of the opaque page_info Shopify returns for every later page.
func listQuery(pageInfo string, limit int, firstPageFilters ...QueryParam) []QueryParam {
	if pageInfo != "" {
		return []QueryParam{{Name: "page_info", Value: pageInfo}, {Name: "limit", Value: intString(int64(limit))}}
	}
	query := append([]QueryParam{{Name: "limit", Value: intString(int64(limit))}}, firstPageFilters...)
	return query
}
