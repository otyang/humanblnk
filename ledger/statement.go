package ledger

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math/big"
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
				{Field: "created_at", Operator: "gte", Value: from.UTC().Format(time.RFC3339Nano)},
				{Field: "created_at", Operator: "lt", Value: to.UTC().Format(time.RFC3339Nano)},
			},
			SortBy: "created_at", SortOrder: "asc", Limit: 100, Offset: offset,
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
				Date: t.CreatedAt.Time, Reference: t.Reference, Description: t.Description,
				Credit: zero, Debit: zero, Receipt: *s.toReceipt(t),
			}
			amt := &t.PreciseAmount.Int
			switch accountID {
			case t.Destination:
				run.Add(run, amt)
				in.Add(in, amt)
				line.Credit, line.Counterpart = m(amt), s.indicatorOf(ctx, t.Source, names)
			case t.Source:
				run.Sub(run, amt)
				out.Add(out, amt)
				line.Debit, line.Counterpart = m(amt), s.indicatorOf(ctx, t.Destination, names)
			default:
				continue
			}
			line.Balance = m(run)
			st.Lines = append(st.Lines, line)
			if len(st.Lines) > maxStatementLines {
				return nil, invalid(op, "more than %d entries; request a shorter period", maxStatementLines)
			}
		}
		if len(txs) < 100 {
			break
		}
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
