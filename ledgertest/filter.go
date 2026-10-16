package ledgertest

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

func (s *Server) filterBalances(body []byte) (any, int, error) {
	var in blnk.FilterRequest
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, 0, fail(400, blnk.CodeMalformedRequest, "%v", err)
	}
	var rows []any
	ids := make([]string, 0, len(s.balances))
	for id := range s.balances {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		rows = append(rows, s.balances[id].view())
	}
	return applyFilter(in, rows)
}

func (s *Server) filterTxns(body []byte) (any, int, error) {
	var in blnk.FilterRequest
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, 0, fail(400, blnk.CodeMalformedRequest, "%v", err)
	}
	rows := make([]any, 0, len(s.order))
	for _, id := range s.order {
		rows = append(rows, s.txns[id].stored())
	}
	out, status, err := applyFilter(in, rows)
	if err != nil {
		return nil, 0, err
	}
	// Blnk 0.15 filters on parent_transaction but returns it empty on every
	// row; only a GET by id or reference shows it.
	blank := func(data []any) {
		for i, r := range data {
			t := r.(blnk.Transaction)
			t.ParentTransaction = ""
			data[i] = t
		}
	}
	switch v := out.(type) {
	case []any:
		blank(v)
	case map[string]any:
		blank(v["data"].([]any))
	}
	return out, status, nil
}

func applyFilter(in blnk.FilterRequest, rows []any) (any, int, error) {
	if in.Limit <= 0 || in.Limit > 100 {
		in.Limit = 20 // Blnk resets out-of-range limits to 20 rather than capping
	}
	type rec struct {
		raw any
		m   map[string]any
		pos int
	}
	var matched []rec
	for i, row := range rows {
		raw, _ := json.Marshal(row)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		ok := true
		if strings.EqualFold(in.LogicalOperator, "or") && len(in.Filters) > 0 {
			ok = false
			for _, f := range in.Filters {
				if match(m, f) {
					ok = true
					break
				}
			}
		} else {
			for _, f := range in.Filters {
				if !match(m, f) {
					ok = false
					break
				}
			}
		}
		if ok {
			matched = append(matched, rec{row, m, i})
		}
	}
	if in.SortBy != "" {
		desc := strings.EqualFold(in.SortOrder, "desc")
		sort.SliceStable(matched, func(i, j int) bool {
			c := compare(field(matched[i].m, in.SortBy), field(matched[j].m, in.SortBy))
			if c == 0 {
				c = matched[i].pos - matched[j].pos
			}
			if desc {
				return c > 0
			}
			return c < 0
		})
	}
	total := len(matched)
	if in.Offset > len(matched) {
		matched = nil
	} else {
		matched = matched[in.Offset:]
	}
	if len(matched) > in.Limit {
		matched = matched[:in.Limit]
	}
	data := make([]any, len(matched))
	for i, r := range matched {
		data[i] = r.raw
	}
	if in.IncludeCount {
		return map[string]any{"data": data, "total_count": total}, 200, nil
	}
	return data, 200, nil
}

// field reads "a", "meta_data.key" or "meta_data->>key" from a decoded record.
func field(m map[string]any, name string) any {
	name = strings.Replace(name, "->>", ".", 1)
	var cur any = m
	for _, p := range strings.Split(name, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func match(m map[string]any, f blnk.Filter) bool {
	if f.Field == "balance_id" {
		if _, isTxn := m["transaction_id"]; isTxn {
			// Virtual field on transactions: source OR destination.
			src, dst := f, f
			src.Field, dst.Field = "source", "destination"
			if strings.ToLower(f.Operator) == "ne" {
				return match(m, src) && match(m, dst)
			}
			return match(m, src) || match(m, dst)
		}
	}
	v := field(m, f.Field)
	switch strings.ToLower(f.Operator) {
	case "eq":
		return compare(v, f.Value) == 0
	case "ne":
		return compare(v, f.Value) != 0
	case "gt":
		return compare(v, f.Value) > 0
	case "gte":
		return compare(v, f.Value) >= 0
	case "lt":
		return compare(v, f.Value) < 0
	case "lte":
		return compare(v, f.Value) <= 0
	case "in":
		for _, x := range f.Values {
			if compare(v, x) == 0 {
				return true
			}
		}
		return false
	case "between":
		return len(f.Values) == 2 && compare(v, f.Values[0]) >= 0 && compare(v, f.Values[1]) <= 0
	case "like", "ilike":
		pat := strings.ToLower(fmt.Sprint(f.Value))
		sv := strings.ToLower(fmt.Sprint(v))
		switch {
		case strings.HasPrefix(pat, "%") && strings.HasSuffix(pat, "%"):
			return strings.Contains(sv, strings.Trim(pat, "%"))
		case strings.HasPrefix(pat, "%"):
			return strings.HasSuffix(sv, strings.TrimPrefix(pat, "%"))
		case strings.HasSuffix(pat, "%"):
			return strings.HasPrefix(sv, strings.TrimSuffix(pat, "%"))
		}
		return sv == pat
	case "isnull":
		return v == nil || v == ""
	case "isnotnull":
		return v != nil && v != ""
	}
	return false
}

// compare orders numbers numerically, RFC3339 times chronologically and
// everything else as strings.
func compare(a, b any) int {
	as, bs := fmt.Sprint(a), fmt.Sprint(b)
	if a == nil {
		as = ""
	}
	if b == nil {
		bs = ""
	}
	if ar, ok := new(big.Rat).SetString(as); ok {
		if br, ok := new(big.Rat).SetString(bs); ok {
			return ar.Cmp(br)
		}
	}
	if at, err := time.Parse(time.RFC3339Nano, as); err == nil {
		if bt, err := time.Parse(time.RFC3339Nano, bs); err == nil {
			return at.Compare(bt)
		}
	}
	return strings.Compare(as, bs)
}
