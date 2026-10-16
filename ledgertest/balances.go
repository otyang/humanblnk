package ledgertest

import (
	"math/big"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

type balance struct {
	blnk.Balance
	history []snapshot // after every change, oldest first
}

type snapshot struct {
	at                                time.Time
	balance, credit, debit            big.Int
	inflight, inflightCr, inflightDbt big.Int
}

func (b *balance) view() blnk.Balance {
	v := b.Balance
	if b.MetaData != nil {
		v.MetaData = blnk.MetaData{}
		for k, x := range b.MetaData {
			v.MetaData[k] = x
		}
	}
	return v
}

func (s *Server) createBalance(in blnk.CreateBalanceRequest) (*balance, error) {
	if in.Currency == "" {
		return nil, fail(400, blnk.CodeValidation, "currency is required")
	}
	if _, ok := s.ledgers[in.LedgerID]; !ok {
		return nil, fail(404, blnk.CodeLedgerNotFound, "ledger %s not found", in.LedgerID)
	}
	if in.IdentityID != "" && s.identities[in.IdentityID] == nil {
		return nil, fail(404, blnk.CodeNotFound, "identity %s not found", in.IdentityID)
	}
	b := &balance{Balance: blnk.Balance{
		BalanceID: s.id("bln"), LedgerID: in.LedgerID, IdentityID: in.IdentityID,
		Currency: strings.ToUpper(in.Currency), MetaData: in.MetaData, CreatedAt: blnk.Time{Time: s.now},
	}}
	s.balances[b.BalanceID] = b
	b.snap(s.now)
	return b, nil
}

// resolve finds a balance by id, or by "@indicator", creating the indicator
// balance in the general ledger on first use as Blnk does.
func (s *Server) resolve(id, currency string) (*balance, error) {
	if strings.HasPrefix(id, "@") {
		key := id + "|" + strings.ToUpper(currency)
		if bid, ok := s.indicators[key]; ok {
			return s.balances[bid], nil
		}
		b := &balance{Balance: blnk.Balance{
			BalanceID: s.id("bln"), LedgerID: "general_ledger_id", Indicator: id,
			Currency: strings.ToUpper(currency), CreatedAt: blnk.Time{Time: s.now},
		}}
		s.balances[b.BalanceID] = b
		s.indicators[key] = b.BalanceID
		b.snap(s.now)
		return b, nil
	}
	b, ok := s.balances[id]
	if !ok {
		return nil, fail(404, blnk.CodeBalanceNotFound, "balance %s not found", id)
	}
	if !strings.EqualFold(b.Currency, currency) {
		return nil, fail(400, blnk.CodeTxnValidation, "balance %s is %s, transaction is %s", id, b.Currency, currency)
	}
	return b, nil
}

func (b *balance) available() *big.Int {
	return new(big.Int).Sub(&b.Balance.Balance.Int, &b.InflightDebitBalance.Int)
}

func (b *balance) snap(at time.Time) {
	var sn snapshot
	sn.at = at
	sn.balance.Set(&b.Balance.Balance.Int)
	sn.credit.Set(&b.CreditBalance.Int)
	sn.debit.Set(&b.DebitBalance.Int)
	sn.inflight.Set(&b.InflightBalance.Int)
	sn.inflightCr.Set(&b.InflightCreditBalance.Int)
	sn.inflightDbt.Set(&b.InflightDebitBalance.Int)
	b.history = append(b.history, sn)
}

// post applies a settled movement: debit the source, credit the destination.
func post(src, dst *balance, amt *big.Int) {
	src.Balance.Balance.Sub(&src.Balance.Balance.Int, amt)
	src.DebitBalance.Add(&src.DebitBalance.Int, amt)
	dst.Balance.Balance.Add(&dst.Balance.Balance.Int, amt)
	dst.CreditBalance.Add(&dst.CreditBalance.Int, amt)
}

// reserve records (sign +1) or releases (sign -1) an inflight movement.
func reserve(src, dst *balance, amt *big.Int, sign int) {
	a := new(big.Int).Set(amt)
	if sign < 0 {
		a.Neg(a)
	}
	src.InflightDebitBalance.Add(&src.InflightDebitBalance.Int, a)
	src.InflightBalance.Sub(&src.InflightBalance.Int, a)
	dst.InflightCreditBalance.Add(&dst.InflightCreditBalance.Int, a)
	dst.InflightBalance.Add(&dst.InflightBalance.Int, a)
}

// balanceAt serves GET /balances/{id}/at?timestamp=RFC3339: the balance as it
// stood at that moment (posted amounts only, as Blnk returns them).
func (s *Server) balanceAt(id string, q map[string][]string) (any, int, error) {
	b, ok := s.balances[id]
	if !ok {
		return nil, 0, fail(404, blnk.CodeBalanceNotFound, "balance %s not found", id)
	}
	at := s.now
	if ts := first(q["timestamp"]); ts != "" {
		var err error
		if at, err = time.Parse(time.RFC3339, ts); err != nil {
			return nil, 0, fail(400, blnk.CodeValidation, "timestamp %q must be RFC3339", ts)
		}
	}
	var out blnk.HistoricalBalance
	out.Balance.BalanceID, out.Balance.Currency = b.BalanceID, b.Currency
	out.Timestamp = blnk.Time{Time: at}
	out.FromSource = first(q["from_source"]) == "true" || first(q["from_source"]) == "1"
	for _, sn := range b.history {
		if sn.at.After(at) {
			break
		}
		out.Balance.Balance.Set(&sn.balance)
		out.Balance.CreditBalance.Set(&sn.credit)
		out.Balance.DebitBalance.Set(&sn.debit)
	}
	return out, 200, nil
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}
