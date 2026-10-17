package ledger

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// Every method loads the object it works on through one of these, so it can
// only ever act on the kind of object it was written for.

// wrongKind is the error for an id of another kind of object.
func wrongKind(op, id, got, want string) error {
	hint := ""
	switch got {
	case "house account":
		hint = "; read house accounts with Ops().GetHouseAccount"
	case "ledger":
		hint = "; read ledgers with Ops().GetLedger"
	}
	return &OpError{Op: op, Kind: ErrWrongKind, Cause: fmt.Errorf("%s is %s, not %s%s", id, article(got), article(want), hint)}
}

// article puts "a" or "an" before a noun.
func article(noun string) string {
	if noun != "" && strings.ContainsRune("aeiou", rune(noun[0])) {
		return "an " + noun
	}
	return "a " + noun
}

// houseOnly refuses anything but a house ("@") account where the money must
// come from or go to the business's own books: provider clearing, bank and
// fee accounts are posted with overdraft allowed, so a customer account there
// could be taken below zero.
func houseOnly(op, field, id string) error {
	if strings.HasPrefix(id, "@") {
		return nil
	}
	return &OpError{Op: op, Kind: ErrWrongKind, Cause: fmt.Errorf("%s %q must be a house account (\"@Name\")", field, id)}
}

// customerOnly refuses an "@" indicator, or an id of another kind, where only
// a customer account is accepted. It doesn't call Blnk.
func customerOnly(op string, id AccountID) error {
	if id == "" {
		return invalid(op, "account id is required")
	}
	if kind := foreignKind(string(id), "account"); kind != "" {
		return wrongKind(op, string(id), kind, "customer account")
	}
	return nil
}

// loadAccount fetches a customer account (or pot or escrow), refusing ledger
// ids, other ids and house accounts.
func (s *Service) loadAccount(ctx context.Context, op string, id AccountID) (*blnk.Balance, error) {
	if err := customerOnly(op, id); err != nil {
		return nil, err
	}
	b, err := s.c.GetBalance(ctx, string(id))
	if err != nil {
		return nil, mapErr(op, err)
	}
	if strings.HasPrefix(b.Indicator, "@") {
		return nil, wrongKind(op, string(id), "house account ("+b.Indicator+")", "customer account")
	}
	return b, nil
}

// loadHouse fetches a house account by indicator and currency.
func (s *Service) loadHouse(ctx context.Context, op string, ref HouseRef) (*blnk.Balance, error) {
	if !strings.HasPrefix(ref.Indicator, "@") || len(ref.Indicator) < 2 {
		if kind := foreignKind(ref.Indicator, "house account"); kind != "" {
			return nil, wrongKind(op, ref.Indicator, kind, "house account")
		}
		return nil, invalid(op, "indicator must start with @, e.g. @FeeIncome")
	}
	if strings.TrimSpace(ref.Currency) == "" {
		return nil, invalid(op, "currency is required")
	}
	b, err := s.c.GetBalanceByIndicator(ctx, ref.Indicator, strings.ToUpper(ref.Currency))
	if err != nil {
		return nil, mapErr(op, err)
	}
	s.indicators.Store(ref.Indicator+"|"+strings.ToUpper(ref.Currency), b.BalanceID)
	return b, nil
}

// ledgerEntry caches a ledger. Ledgers are never deleted, so a ledger once seen
// always exists; only its status is re-read after Config.LedgerStatusTTL.
type ledgerEntry struct {
	info *LedgerInfo
	at   time.Time
}

// ledgerFor returns a ledger, refusing other kinds of id. A ledger that
// doesn't exist is ErrLedgerNotFound. forOpen also refuses a ledger marked as
// a duplicate, naming the one to use. The result is cached: existence for the
// life of the Service, status for Config.LedgerStatusTTL.
func (s *Service) ledgerFor(ctx context.Context, op string, id LedgerID, forOpen bool) (*LedgerInfo, error) {
	if id == "" {
		return nil, invalid(op, "ledger id is required")
	}
	if kind := foreignKind(string(id), "ledger"); kind != "" {
		return nil, wrongKind(op, string(id), kind, "ledger")
	}
	var info *LedgerInfo
	if e, ok := s.ledgers.Load(id); ok {
		entry := e.(*ledgerEntry)
		if s.cfg.LedgerStatusTTL > 0 && s.cfg.Now().Sub(entry.at) < s.cfg.LedgerStatusTTL {
			info = entry.info
		}
	}
	if info == nil {
		gen := s.ledgerGen(id)
		l, err := s.c.GetLedger(ctx, string(id))
		if blnk.IsNotFound(err) {
			return nil, &OpError{Op: op, Kind: ErrLedgerNotFound, Cause: fmt.Errorf("ledger %s does not exist", id)}
		}
		if err != nil {
			return nil, mapErr(op, err)
		}
		info = toLedgerInfo(l)
		s.ledgerMu.Lock()
		if s.ledgerGens[id] == gen { // no status change since the read began
			s.ledgers.Store(id, &ledgerEntry{info: info, at: s.cfg.Now()})
		}
		s.ledgerMu.Unlock()
	}
	if forOpen && info.Status == "duplicate" {
		return nil, invalid(op, "ledger %s is a duplicate of %s; use that one", id, info.DuplicateOf)
	}
	return info, nil
}

// forgetLedger drops a cached ledger after this Service changed its status,
// and stops a read already in flight from caching the old status.
func (s *Service) forgetLedger(id LedgerID) {
	s.ledgerMu.Lock()
	defer s.ledgerMu.Unlock()
	if s.ledgerGens == nil {
		s.ledgerGens = map[LedgerID]uint64{}
	}
	s.ledgerGens[id]++
	s.ledgers.Delete(id)
}

// ledgerGen is how many times id's cached status has been dropped.
func (s *Service) ledgerGen(id LedgerID) uint64 {
	s.ledgerMu.Lock()
	defer s.ledgerMu.Unlock()
	return s.ledgerGens[id]
}

// fourEyes enforces the maker-checker rule on manual money movements: one
// person asks, another approves, and both names are recorded. The library
// can't tell who is really logged in, so take both from your own
// authentication and approval flow, never from free text.
func fourEyes(op, what, requestedBy, approvedBy string) error {
	req, appr := strings.TrimSpace(requestedBy), strings.TrimSpace(approvedBy)
	switch {
	case req == "":
		return &OpError{Op: op, Kind: ErrApprovalRequired, Cause: fmt.Errorf("RequestedBy is required: who asked for this %s", what)}
	case appr == "":
		return &OpError{Op: op, Kind: ErrApprovalRequired, Cause: fmt.Errorf("ApprovedBy is required: who approved this %s", what)}
	case strings.EqualFold(req, appr):
		return &OpError{Op: op, Kind: ErrApprovalRequired, Cause: fmt.Errorf("%s can't approve their own %s", req, what)}
	}
	return nil
}
