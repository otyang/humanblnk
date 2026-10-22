package memblnk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// filter answers a POST …/filter request over items, each a record as Blnk
// would send it. Fields are its JSON names; "meta_data.key" reaches into
// metadata. Without sort_by, matches are newest first.
func (s *Server) filter(body []byte, items []any) (int, any) {
	var r blnk.FilterRequest
	if err := json.Unmarshal(body, &r); err != nil {
		return respond(0, nil, invalid(blnk.CodeMalformedRequest, "%v", err))
	}
	if r.Limit < 0 || r.Limit > 100 {
		return respond(0, nil, invalid(blnk.CodeValidation, "limit %d out of range 1-100", r.Limit))
	}
	type row struct {
		item any
		doc  map[string]any
	}
	var rows []row
	for _, it := range items {
		doc, err := document(it)
		if err != nil {
			return respond(0, nil, fail(http.StatusInternalServerError, blnk.CodeInternal, "%v", err))
		}
		ok, ferr := matches(doc, r.Filters, r.LogicalOperator)
		if ferr != nil {
			return respond(0, nil, ferr)
		}
		if ok {
			rows = append(rows, row{it, doc})
		}
	}
	sortBy, desc := r.SortBy, r.SortOrder != "asc"
	if sortBy == "" {
		sortBy, desc = "created_at", true
	}
	sort.SliceStable(rows, func(i, j int) bool {
		c := compare(field(rows[i].doc, sortBy), field(rows[j].doc, sortBy))
		if desc {
			return c > 0
		}
		return c < 0
	})
	total := len(rows)
	limit := r.Limit
	if limit == 0 {
		limit = 20
	}
	offset := min(max(r.Offset, 0), len(rows))
	rows = rows[offset:min(offset+limit, len(rows))]
	out := make([]any, len(rows))
	for i, rw := range rows {
		out[i] = rw.item
	}
	if r.IncludeCount {
		return http.StatusOK, map[string]any{"data": out, "total_count": total}
	}
	return http.StatusOK, out
}

// document is v as the JSON object Blnk would send, numbers kept exact.
func document(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var doc map[string]any
	return doc, d.Decode(&doc)
}

// field is doc's value at a dotted path, or nil.
func field(doc map[string]any, path string) any {
	var cur any = doc
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

func matches(doc map[string]any, fs []blnk.Filter, logical string) (bool, *failure) {
	if len(fs) == 0 {
		return true, nil
	}
	or := strings.EqualFold(logical, "or")
	for _, f := range fs {
		ok, err := holds(field(doc, f.Field), f)
		if err != nil {
			return false, err
		}
		if or && ok {
			return true, nil
		}
		if !or && !ok {
			return false, nil
		}
	}
	return !or, nil
}

// holds reports whether got satisfies f.
func holds(got any, f blnk.Filter) (bool, *failure) {
	isNull := got == nil || got == ""
	switch f.Operator {
	case "isnull":
		return isNull, nil
	case "isnotnull":
		return !isNull, nil
	case "eq":
		return compare(got, f.Value) == 0, nil
	case "ne":
		return compare(got, f.Value) != 0, nil
	case "gt":
		return !isNull && compare(got, f.Value) > 0, nil
	case "gte":
		return !isNull && compare(got, f.Value) >= 0, nil
	case "lt":
		return !isNull && compare(got, f.Value) < 0, nil
	case "lte":
		return !isNull && compare(got, f.Value) <= 0, nil
	case "in":
		for _, v := range values(f) {
			if compare(got, v) == 0 {
				return true, nil
			}
		}
		return false, nil
	case "between":
		vs := values(f)
		if len(vs) != 2 {
			return false, invalid(blnk.CodeValidation, "between on %s needs two values", f.Field)
		}
		return !isNull && compare(got, vs[0]) >= 0 && compare(got, vs[1]) <= 0, nil
	case "like", "ilike":
		pattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(text(f.Value)), "%", ".*") + "$"
		if f.Operator == "ilike" {
			pattern = "(?i)" + pattern
		}
		return regexp.MustCompile(pattern).MatchString(text(got)), nil
	}
	return false, invalid(blnk.CodeValidation, "unknown filter operator %q", f.Operator)
}

// values are f's operands: Values, or Value split on commas or "|".
func values(f blnk.Filter) []any {
	if len(f.Values) > 0 {
		return f.Values
	}
	var out []any
	for _, v := range strings.FieldsFunc(text(f.Value), func(r rune) bool { return r == ',' || r == '|' }) {
		out = append(out, strings.TrimSpace(v))
	}
	return out
}

// compare orders a and b as Blnk's columns would: as numbers when either is
// a JSON number, as times when both are timestamps, else as text, so that
// customer "007" is never customer "7".
func compare(a, b any) int {
	sa, sb := text(a), text(b)
	if isNumber(a) || isNumber(b) {
		if na, ok := decimal(sa); ok {
			if nb, ok := decimal(sb); ok {
				return na.Cmp(nb)
			}
		}
	}
	if ta, err := time.Parse(time.RFC3339Nano, sa); err == nil {
		if tb, err := time.Parse(time.RFC3339Nano, sb); err == nil {
			return ta.Compare(tb)
		}
	}
	return strings.Compare(sa, sb)
}

func isNumber(v any) bool {
	switch v.(type) {
	case json.Number, float64, int, int64:
		return true
	}
	return false
}

// decimal reads a plain decimal number, nothing fancier ("0x10", "1/2").
var plainNumber = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?([eE][-+]?[0-9]+)?$`)

func decimal(s string) (*big.Rat, bool) {
	if !plainNumber.MatchString(s) {
		return nil, false
	}
	return new(big.Rat).SetString(s)
}

func text(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	}
	return fmt.Sprint(v)
}
