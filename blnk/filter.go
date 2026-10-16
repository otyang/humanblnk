package blnk

import (
	"encoding/json"
	"fmt"
)

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

// Filter is one condition of a filter request.
type Filter struct {
	Field    string `json:"field"`
	Operator string `json:"operator"` // eq ne gt gte lt lte in between like ilike isnull isnotnull
	Value    any    `json:"value,omitempty"`
	Values   []any  `json:"values,omitempty"`
}

// FilterRequest is the body of POST /transactions/filter and /balances/filter.
type FilterRequest struct {
	Filters         []Filter `json:"filters"`
	LogicalOperator string   `json:"logical_operator,omitempty"` // and | or
	SortBy          string   `json:"sort_by,omitempty"`
	SortOrder       string   `json:"sort_order,omitempty"` // asc | desc
	IncludeCount    bool     `json:"include_count,omitempty"`
	Limit           int      `json:"limit,omitempty"` // max 100
	Offset          int      `json:"offset,omitempty"`
}
