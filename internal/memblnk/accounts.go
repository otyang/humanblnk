package memblnk

import (
	"encoding/json"
	"maps"
	"net/http"
	"strconv"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// --- Ledgers ---

func (s *Server) addLedger(l *blnk.Ledger) {
	s.ledgers[l.LedgerID] = l
	s.ledgerOrder = append(s.ledgerOrder, l.LedgerID)
}

func (s *Server) createLedger(_ *http.Request, body []byte) (int, any) {
	var r blnk.CreateLedgerRequest
	if err := json.Unmarshal(body, &r); err != nil {
		return respond(0, nil, invalid(blnk.CodeMalformedRequest, "%v", err))
	}
	if r.Name == "" {
		return respond(0, nil, invalid(blnk.CodeValidation, "name is required"))
	}
	l := &blnk.Ledger{LedgerID: newID("ldg_"), Name: r.Name, CreatedAt: blnk.Time{Time: s.now()}, MetaData: r.MetaData}
	s.addLedger(l)
	return http.StatusCreated, l
}

func (s *Server) getLedger(r *http.Request, _ []byte) (int, any) {
	l, ok := s.ledgers[r.PathValue("id")]
	if !ok {
		return respond(0, nil, notFound(blnk.CodeLedgerNotFound, "ledger", r.PathValue("id")))
	}
	return http.StatusOK, l
}

// listLedgers is newest first, 10 unless limit says otherwise.
func (s *Server) listLedgers(r *http.Request, _ []byte) (int, any) {
	out := make([]*blnk.Ledger, 0, len(s.ledgerOrder))
	for i := len(s.ledgerOrder) - 1; i >= 0; i-- {
		out = append(out, s.ledgers[s.ledgerOrder[i]])
	}
	return http.StatusOK, page(out, r, 10)
}

func (s *Server) filterLedgers(_ *http.Request, body []byte) (int, any) {
	items := make([]any, len(s.ledgerOrder))
	for i, id := range s.ledgerOrder {
		items[i] = s.ledgers[id]
	}
	return s.filter(body, items)
}

func (s *Server) renameLedger(r *http.Request, body []byte) (int, any) {
	l, ok := s.ledgers[r.PathValue("id")]
	if !ok {
		return respond(0, nil, notFound(blnk.CodeLedgerNotFound, "ledger", r.PathValue("id")))
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &in); err != nil || in.Name == "" {
		return respond(0, nil, invalid(blnk.CodeValidation, "name is required"))
	}
	l.Name = in.Name
	return http.StatusOK, l
}

// page is items[offset:offset+limit] from r's query, def if no limit.
func page[T any](items []T, r *http.Request, def int) []T {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = def
	}
	offset = min(max(offset, 0), len(items))
	return items[offset:min(offset+limit, len(items))]
}

// --- Balances ---

func (s *Server) addBalance(b *blnk.Balance) {
	s.balances[b.BalanceID] = b
	s.balanceOrder = append(s.balanceOrder, b.BalanceID)
}

func (s *Server) createBalance(_ *http.Request, body []byte) (int, any) {
	var r blnk.CreateBalanceRequest
	if err := json.Unmarshal(body, &r); err != nil {
		return respond(0, nil, invalid(blnk.CodeMalformedRequest, "%v", err))
	}
	switch {
	case r.Currency == "":
		return respond(0, nil, invalid(blnk.CodeBalanceInvalid, "currency is required"))
	case r.LedgerID == "":
		return respond(0, nil, invalid(blnk.CodeBalanceInvalid, "ledger_id is required"))
	case s.ledgers[r.LedgerID] == nil, r.IdentityID != "" && s.identities[r.IdentityID] == nil:
		// Blnk finds out from a foreign key, and says so for either.
		return respond(0, nil, invalid(blnk.CodeBadRequest, "Invalid ledger ID"))
	case r.Indicator != "" && r.LedgerID != GeneralLedger:
		return respond(0, nil, invalid(blnk.CodeBalanceInvalid, "an indicator balance must be in the general ledger"))
	case r.Indicator != "" && s.indicator(r.Indicator, r.Currency) != nil:
		return respond(0, nil, conflict(blnk.CodeConflict, "balance %s in %s already exists", r.Indicator, r.Currency))
	}
	b := &blnk.Balance{
		BalanceID: newID("bln_"), LedgerID: r.LedgerID, IdentityID: r.IdentityID, Indicator: r.Indicator,
		Currency: r.Currency, CreatedAt: blnk.Time{Time: s.now()}, MetaData: r.MetaData,
		TrackFundLineage: r.TrackFundLineage, AllocationStrategy: r.AllocationStrategy,
	}
	s.addBalance(b)
	return http.StatusCreated, b
}

// indicator is the system balance named indicator in currency, if any.
func (s *Server) indicator(indicator, currency string) *blnk.Balance {
	for _, id := range s.balanceOrder {
		if b := s.balances[id]; b.Indicator == indicator && b.Currency == currency {
			return b
		}
	}
	return nil
}

func (s *Server) getBalance(r *http.Request, _ []byte) (int, any) {
	b, ok := s.balances[r.PathValue("id")]
	if !ok {
		return respond(0, nil, notFound(blnk.CodeBalanceNotFound, "balance", r.PathValue("id")))
	}
	out := *b // queued balances are always 0: nothing is queued
	for _, inc := range r.URL.Query()["include"] {
		switch inc {
		case "identity":
			out.Identity = s.identities[b.IdentityID]
		case "ledger":
			out.Ledger = s.ledgers[b.LedgerID]
		}
	}
	return http.StatusOK, &out
}

func (s *Server) getBalanceByIndicator(r *http.Request, _ []byte) (int, any) {
	b := s.indicator(r.PathValue("indicator"), r.PathValue("currency"))
	if b == nil {
		return respond(0, nil, notFound(blnk.CodeBalanceNotFound, "balance", r.PathValue("indicator")+" in "+r.PathValue("currency")))
	}
	return http.StatusOK, b
}

func (s *Server) filterBalances(_ *http.Request, body []byte) (int, any) {
	items := make([]any, len(s.balanceOrder))
	for i, id := range s.balanceOrder {
		items[i] = s.balances[id]
	}
	return s.filter(body, items)
}

// --- Identities ---

func (s *Server) createIdentity(_ *http.Request, body []byte) (int, any) {
	var id blnk.Identity
	if err := json.Unmarshal(body, &id); err != nil {
		return respond(0, nil, invalid(blnk.CodeMalformedRequest, "%v", err))
	}
	if id.IdentityID == "" {
		id.IdentityID = newID("idt_")
	}
	if s.identities[id.IdentityID] != nil {
		return respond(0, nil, conflict(blnk.CodeConflict, "identity %s already exists", id.IdentityID))
	}
	id.CreatedAt = blnk.Time{Time: s.now()}
	s.identities[id.IdentityID] = &id
	s.identityOrder = append(s.identityOrder, id.IdentityID)
	return http.StatusCreated, &id
}

func (s *Server) getIdentity(r *http.Request, _ []byte) (int, any) {
	id, ok := s.identities[r.PathValue("id")]
	if !ok {
		return respond(0, nil, notFound(blnk.CodeIdentityNotFound, "identity", r.PathValue("id")))
	}
	return http.StatusOK, id
}

func (s *Server) filterIdentities(_ *http.Request, body []byte) (int, any) {
	items := make([]any, len(s.identityOrder))
	for i, id := range s.identityOrder {
		items[i] = s.identities[id]
	}
	return s.filter(body, items)
}

// --- Metadata ---

// updateMetadata merges keys into an entity's metadata and returns the
// merged metadata; for a transaction, only the keys sent.
func (s *Server) updateMetadata(r *http.Request, body []byte) (int, any) {
	var in struct {
		MetaData blnk.MetaData `json:"meta_data"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return respond(0, nil, invalid(blnk.CodeMalformedRequest, "%v", err))
	}
	id := r.PathValue("id")
	merge := func(md *blnk.MetaData) blnk.MetaData {
		if *md == nil {
			*md = blnk.MetaData{}
		}
		maps.Copy(*md, in.MetaData)
		return *md
	}
	type out struct {
		MetaData blnk.MetaData `json:"meta_data"`
	}
	switch {
	case s.txns[id] != nil:
		merge(&s.txns[id].MetaData)
		return http.StatusOK, out{in.MetaData}
	case s.balances[id] != nil:
		return http.StatusOK, out{merge(&s.balances[id].MetaData)}
	case s.identities[id] != nil:
		return http.StatusOK, out{merge(&s.identities[id].MetaData)}
	case s.ledgers[id] != nil:
		return http.StatusOK, out{merge(&s.ledgers[id].MetaData)}
	case !strings.Contains(id, "_"):
		return respond(0, nil, invalid(blnk.CodeMetadataInvalidID, "invalid entity id %s", id))
	}
	return respond(0, nil, notFound(blnk.CodeMetadataEntityNotFound, "entity", id))
}
