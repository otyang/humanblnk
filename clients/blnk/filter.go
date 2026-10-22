package blnk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// getList GETs path and decodes a list in either response shape.
func getList[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	out, _, err := decodeList[T](raw)
	return out, err
}

// maxFilterLimit is the largest page the filter endpoints serve.
const maxFilterLimit = 100

// filterList POSTs r to path and returns the matches and the total count,
// which is only the page length unless r.IncludeCount is set.
func filterList[T any](ctx context.Context, c *Client, path string, r FilterRequest) ([]T, int, error) {
	if r.Limit < 0 || r.Limit > maxFilterLimit {
		// Blnk would quietly answer with 20 rows instead.
		return nil, 0, fmt.Errorf("blnk: filter limit %d out of range 1-%d (0 for the default of 20)", r.Limit, maxFilterLimit)
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, path, r, &raw); err != nil {
		return nil, 0, err
	}
	return decodeList[T](raw)
}

// decodeList reads a bare array or {"data": [...], "total_count": n}.
func decodeList[T any](raw json.RawMessage) ([]T, int, error) {
	var arr []T
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, len(arr), nil
	}
	var wrapped struct {
		Data       []T `json:"data"`
		TotalCount int `json:"total_count"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, 0, fmt.Errorf("blnk: decode filter response: %w", err)
	}
	if wrapped.TotalCount == 0 {
		wrapped.TotalCount = len(wrapped.Data)
	}
	return wrapped.Data, wrapped.TotalCount, nil
}

// ListOptions pages a GET list endpoint. Zero Limit leaves Blnk's default
// (10 for ledgers and balances, 20 for transactions, identities and accounts).
// Filters are query-string conditions written field_operator=value, e.g.
// {"currency_in": {"USD,EUR"}, "created_at_between": {"2026-01-01|2026-12-31"}};
// identities and accounts honour Limit and Offset only when Filters are set.
type ListOptions struct {
	Limit   int
	Offset  int
	Filters url.Values
}

// path is base with the options as its query string.
func (o ListOptions) path(base string) string {
	q := url.Values{}
	for k, v := range o.Filters {
		q[k] = v
	}
	if o.Limit > 0 {
		q.Set("limit", strconv.Itoa(o.Limit))
	}
	if o.Offset > 0 {
		q.Set("offset", strconv.Itoa(o.Offset))
	}
	if len(q) == 0 {
		return base
	}
	return base + "?" + q.Encode()
}

// Filter is one condition of a filter request.
type Filter struct {
	Field    string `json:"field"`
	Operator string `json:"operator"` // eq ne gt gte lt lte in between like ilike isnull isnotnull
	Value    any    `json:"value,omitempty"`
	Values   []any  `json:"values,omitempty"`
}

// FilterRequest is the body of the POST .../filter endpoints (ledgers,
// balances, transactions, identities, accounts). The Filter methods return
// the matches and a count: the total of all matches with IncludeCount, else
// just the number on this page.
type FilterRequest struct {
	Filters         []Filter `json:"filters"`
	LogicalOperator string   `json:"logical_operator,omitempty"` // and | or
	SortBy          string   `json:"sort_by,omitempty"`
	SortOrder       string   `json:"sort_order,omitempty"`    // asc | desc
	IncludeCount    bool     `json:"include_count,omitempty"` // count every match, not just this page
	Limit           int      `json:"limit,omitempty"`         // 1-100, or 0 for Blnk's default of 20; anything else is an error
	Offset          int      `json:"offset,omitempty"`
}
