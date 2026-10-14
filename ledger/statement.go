package ledger

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// BalanceAt returns the account's posted balance at t, e.g. a month-end
// balance. It uses Blnk's historical balance endpoint recomputed from
// transactions (Core 0.10.1+). Pending (INFLIGHT) amounts aren't included.
func (s accountsAPI) BalanceAt(ctx context.Context, accountID string, t time.Time) (Money, error) {
	const op = "ledger.Accounts.BalanceAt"
	if accountID == "" {
		return Money{}, invalid(op, "account id is required")
	}
	h, err := s.c.GetBalanceAt(ctx, accountID, t, true)
	if err != nil {
		return Money{}, mapErr(op, err)
	}
	return s.money(&h.Balance.Balance.Int, s.Precision(h.Balance.Currency), h.Balance.Currency), nil
}

// Statement is an account statement for [From, To): opening balance, every
// posted entry with the running balance after it, and closing balance.
type Statement struct {
	AccountID string
	Currency  string
	From, To  time.Time
	Opening   Money
	Closing   Money
	Credits   Money // total in
	Debits    Money // total out
	Lines     []StatementLine
}

// StatementLine is one posted entry on a Statement.
type StatementLine struct {
	Date        time.Time
	Reference   string
	Description string
	Credit      Money  // money in (zero for debits)
	Debit       Money  // money out (zero for credits)
	Balance     Money  // running balance after this line
	Counterpart string // the other account: an id, or "@Name" for system accounts
	Receipt     Receipt
}

// maxStatementLines bounds one statement; page long periods by month.
const maxStatementLines = 10000

// Statement builds a statement from posted (APPLIED) entries. Opening comes
// from Blnk's historical balance; the running balance is computed here and
// checked against the historical balance at To, so a statement that doesn't
// add up is an error rather than a wrong document.
func (s accountsAPI) Statement(ctx context.Context, accountID string, from, to time.Time) (*Statement, error) {
	const op = "ledger.Accounts.Statement"
	switch {
	case accountID == "":
		return nil, invalid(op, "account id is required")
	case !from.Before(to):
		return nil, invalid(op, "from must be before to")
	}
	b, err := s.c.GetBalance(ctx, accountID)
	if err != nil {
		return nil, mapErr(op, err)
	}
	cur, prec := strings.ToUpper(b.Currency), s.Precision(b.Currency)
	m := func(v *big.Int) Money { return s.money(v, prec, cur) }

	opening, err := s.Accounts().BalanceAt(ctx, accountID, from.Add(-time.Nanosecond))
	if err != nil {
		return nil, err
	}
	st := &Statement{AccountID: accountID, Currency: cur, From: from, To: to, Opening: opening}
	run := new(big.Int).Set(opening.PreciseAmount)
	in, out := new(big.Int), new(big.Int)
	zero := m(new(big.Int))
	names := map[string]string{} // balance id → "@Name" for system accounts

	for offset := 0; ; offset += 100 {
		txs, _, err := s.c.FilterTransactions(ctx, blnk.FilterRequest{
			Filters: []blnk.Filter{
				{Field: "balance_id", Operator: "eq", Value: accountID},
				{Field: "status", Operator: "eq", Value: blnk.StatusApplied},
				// effective_date is what Blnk's historical balances count by,
				// so a scheduled transfer appears on the day it posted.
				{Field: "effective_date", Operator: "gte", Value: from.UTC().Format(time.RFC3339Nano)},
				{Field: "effective_date", Operator: "lt", Value: to.UTC().Format(time.RFC3339Nano)},
			},
			SortBy: "effective_date", SortOrder: "asc", Limit: 100, Offset: offset,
		})
		if err != nil {
			return nil, mapErr(op, err)
		}
		for i := range txs {
			t := &txs[i]
			if isSplitParent(t) {
				continue // its legs are listed individually
			}
			line := StatementLine{
				Date: effective(t), Reference: t.Reference, Description: t.Description,
				Credit: zero, Debit: zero, Receipt: *s.toReceipt(t),
			}
			amt := &t.PreciseAmount.Int
			switch accountID {
			case t.Destination:
				in.Add(in, amt)
				line.Credit, line.Counterpart = m(amt), s.indicatorOf(ctx, t.Source, names)
			case t.Source:
				out.Add(out, amt)
				line.Debit, line.Counterpart = m(amt), s.indicatorOf(ctx, t.Destination, names)
			default:
				continue
			}
			st.Lines = append(st.Lines, line)
			if len(st.Lines) > maxStatementLines {
				return nil, invalid(op, "more than %d entries; request a shorter period", maxStatementLines)
			}
		}
		if len(txs) < 100 {
			break
		}
	}
	// Blnk doesn't store a payment's legs in order; keep each payment's legs
	// together with its fee last, then work out the running balance.
	// A payment's legs (and the records of confirming them) share their
	// moment and description, so that is what groups them.
	group := make(map[string]int, len(st.Lines)) // group → first position
	key := make([]int, len(st.Lines))
	for i, l := range st.Lines {
		k := l.Date.Format(time.RFC3339Nano) + "|" + l.Description
		if _, ok := group[k]; !ok {
			group[k] = i
		}
		key[i] = group[k]
	}
	idx := make([]int, len(st.Lines))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		la, lb := st.Lines[idx[a]], st.Lines[idx[b]]
		if !la.Date.Equal(lb.Date) {
			return la.Date.Before(lb.Date)
		}
		if key[idx[a]] != key[idx[b]] {
			return key[idx[a]] < key[idx[b]]
		}
		return !s.isFeeLine(la) && s.isFeeLine(lb)
	})
	sorted := make([]StatementLine, len(st.Lines))
	for i, j := range idx {
		sorted[i] = st.Lines[j]
	}
	st.Lines = sorted
	for i := range st.Lines {
		run.Add(run, st.Lines[i].Credit.PreciseAmount)
		run.Sub(run, st.Lines[i].Debit.PreciseAmount)
		st.Lines[i].Balance = m(run)
	}
	st.Closing, st.Credits, st.Debits = m(run), m(in), m(out)

	closing, err := s.Accounts().BalanceAt(ctx, accountID, to.Add(-time.Nanosecond))
	if err != nil {
		return nil, err
	}
	if closing.PreciseAmount.Cmp(run) != 0 {
		return nil, &OpError{Op: op, Kind: ErrConflict, Cause: fmt.Errorf(
			"entries add up to %s but Blnk's balance at %s is %s (backdated entries or activity during the read?)",
			st.Closing, to.Format(time.RFC3339), closing)}
	}
	return st, nil
}

// WriteCSV writes the statement as CSV: an opening row, one row per entry, a
// closing row. Amounts are plain decimals with every decimal place.
func (st *Statement) WriteCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	rows := [][]string{
		{"date", "reference", "description", "counterpart", "credit", "debit", "balance", "currency"},
		{st.From.UTC().Format(time.RFC3339), "", "Opening balance", "", "", "", st.Opening.Fixed(), st.Currency},
	}
	blank := func(m Money) string {
		if m.Sign() == 0 {
			return ""
		}
		return m.Fixed()
	}
	for _, l := range st.Lines {
		rows = append(rows, []string{
			l.Date.UTC().Format(time.RFC3339), l.Reference, l.Description, l.Counterpart,
			blank(l.Credit), blank(l.Debit), l.Balance.Fixed(), st.Currency,
		})
	}
	rows = append(rows, []string{st.To.UTC().Format(time.RFC3339), "", "Closing balance", "",
		st.Credits.Fixed(), st.Debits.Fixed(), st.Closing.Fixed(), st.Currency})
	if err := cw.WriteAll(rows); err != nil {
		return err
	}
	return cw.Error()
}

// effective is when a transaction counts in balances: effective_date, else created_at.
func effective(t *blnk.Transaction) time.Time {
	if !t.EffectiveDate.IsZero() {
		return t.EffectiveDate.Time
	}
	return t.CreatedAt.Time
}

// isFeeLine reports whether a statement line is a fee leg.
func (s *Service) isFeeLine(l StatementLine) bool { return l.Counterpart == s.cfg.FeeIncomeAccount }
