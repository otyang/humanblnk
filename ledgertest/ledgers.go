package ledgertest

import (
	"fmt"
	"sort"

	"github.com/otyang/humanblnk/blnk"
)

// CreateLedger adds a ledger and returns its id.
func (s *Server) CreateLedger(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.id("ldg")
	s.ledgers[id] = &blnk.Ledger{LedgerID: id, Name: name, CreatedAt: blnk.Time{Time: s.now}}
	return id
}

// sortedLedgers returns every ledger, oldest first.
func (s *Server) sortedLedgers() []*blnk.Ledger {
	out := make([]*blnk.Ledger, 0, len(s.ledgers))
	for _, l := range s.ledgers {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt.Time) {
			return out[i].CreatedAt.Before(out[j].CreatedAt.Time)
		}
		return out[i].LedgerID < out[j].LedgerID
	})
	return out
}

// listLedgers serves GET /ledgers?limit=&offset= (Blnk's default limit is 20).
func (s *Server) listLedgers(q map[string][]string) []blnk.Ledger {
	num := func(k string, def int) int {
		if v := q[k]; len(v) > 0 {
			var n int
			if _, err := fmt.Sscan(v[0], &n); err == nil && n >= 0 {
				return n
			}
		}
		return def
	}
	limit, offset := num("limit", 20), num("offset", 0)
	all := s.sortedLedgers()
	out := []blnk.Ledger{}
	for i := offset; i < len(all) && len(out) < limit; i++ {
		out = append(out, *all[i])
	}
	return out
}
