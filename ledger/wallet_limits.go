package ledger

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// Limit caps what a customer account can send. Amounts are decimals in
// Currency; "" means no cap. Configure them in Config.Limits, e.g. by KYC tier:
//
//	Limits: []ledger.Limit{
//		{Tier: "tier1", Currency: "NGN", PerTransaction: "50000", Daily: "200000"},
//		{Tier: "tier2", Currency: "NGN", PerTransaction: "1000000", Daily: "5000000", Monthly: "50000000"},
//		{Currency: "NGN", PerTransaction: "10000", Daily: "30000"}, // accounts with no tier
//	}
//
// An account's tier is its meta_data.tier. Limits apply to money leaving
// customer accounts (Withdraw, Transfer, Schedule, Hold, Split, Exchange and
// Wallet().OpenEscrow); house accounts, pots and escrows are not limited, and
// Bulk batches are not checked. Usage is read before posting, so two
// payments racing each other can both pass the check.
type Limit struct {
	Tier           string // "" matches accounts without a tier; "*" matches any tier
	Currency       string
	PerTransaction string
	Daily          string
	Monthly        string
}

// LimitUsage is how much of its limits an account has used.
type LimitUsage struct {
	Limit       *Limit // nil when no limit applies
	DailyUsed   Money
	MonthlyUsed Money
	DailyLeft   *Money // nil when there is no daily cap
	MonthlyLeft *Money
}

// LimitUsage reports usage against the account's limit (for "you can send X
// more today" in your app).
func (s walletAPI) LimitUsage(ctx context.Context, id AccountID) (*LimitUsage, error) {
	const op = "ledger.Wallet.LimitUsage"
	b, err := s.loadAccount(ctx, op, id)
	if err != nil {
		return nil, err
	}
	return s.usage(ctx, op, b)
}

func (s *Service) usage(ctx context.Context, op string, b *blnk.Balance) (*LimitUsage, error) {
	prec, cur := s.Precision(b.Currency), strings.ToUpper(b.Currency)
	zero := s.money(new(big.Int), prec, cur)
	u := &LimitUsage{Limit: s.limitFor(b), DailyUsed: zero, MonthlyUsed: zero}
	if u.Limit == nil || (u.Limit.Daily == "" && u.Limit.Monthly == "") {
		return u, nil
	}
	loc := s.cfg.Location
	if loc == nil {
		loc = time.UTC
	}
	now := s.cfg.Now().In(loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	since := day
	if u.Limit.Monthly != "" {
		since = month
	}
	txs, err := s.outflows(ctx, b.BalanceID, since)
	if err != nil {
		return nil, mapErr(op, err)
	}
	daily, monthly := spent(txs, day), spent(txs, month)
	u.DailyUsed, u.MonthlyUsed = s.money(daily, prec, cur), s.money(monthly, prec, cur)
	left := func(capStr string, used *big.Int) (*Money, error) {
		if capStr == "" {
			return nil, nil
		}
		c, err := parseDecimal(capStr, prec)
		if err != nil {
			return nil, &OpError{Op: op, Kind: ErrInvalidInput, Cause: fmt.Errorf("limit: %w", err)}
		}
		m := s.money(c.Sub(c, used), prec, cur)
		return &m, nil
	}
	if u.DailyLeft, err = left(u.Limit.Daily, daily); err != nil {
		return nil, err
	}
	if u.MonthlyLeft, err = left(u.Limit.Monthly, monthly); err != nil {
		return nil, err
	}
	return u, nil
}

// checkLimits rejects a debit of amount from accountID that would break its limit.
func (s *Service) checkLimits(ctx context.Context, op string, accountID AccountID, amount *big.Int) error {
	if len(s.cfg.Limits) == 0 || accountID.isIndicator() {
		return nil
	}
	b, err := s.c.GetBalance(ctx, string(accountID))
	if err != nil {
		return mapErr(op, err)
	}
	lim := s.limitFor(b)
	if lim == nil {
		return nil
	}
	prec, cur := s.Precision(b.Currency), strings.ToUpper(b.Currency)
	exceeded := func(what, capStr string, used *big.Int) error {
		c, err := parseDecimal(capStr, prec)
		if err != nil {
			return &OpError{Op: op, Kind: ErrInvalidInput, Cause: fmt.Errorf("limit: %w", err)}
		}
		if new(big.Int).Add(used, amount).Cmp(c) > 0 {
			return &OpError{Op: op, Kind: ErrLimitExceeded, Cause: fmt.Errorf("%s limit %s %s: already used %s, this needs %s",
				what, capStr, cur, formatDecimal(used, prec), formatDecimal(amount, prec))}
		}
		return nil
	}
	if lim.PerTransaction != "" {
		if err := exceeded("per-transaction", lim.PerTransaction, new(big.Int)); err != nil {
			return err
		}
	}
	if lim.Daily == "" && lim.Monthly == "" {
		return nil
	}
	u, err := s.usage(ctx, op, b)
	if err != nil {
		return err
	}
	if lim.Daily != "" {
		if err := exceeded("daily", lim.Daily, u.DailyUsed.PreciseAmount); err != nil {
			return err
		}
	}
	if lim.Monthly != "" {
		if err := exceeded("monthly", lim.Monthly, u.MonthlyUsed.PreciseAmount); err != nil {
			return err
		}
	}
	return nil
}

// limitFor picks the first configured limit for the account's tier and currency.
func (s *Service) limitFor(b *blnk.Balance) *Limit {
	if strings.HasPrefix(b.Indicator, "@") {
		return nil
	}
	if k := str(b.MetaData[metaKind]); k != "" && k != string(CustomerAccount) {
		return nil
	}
	tier := str(b.MetaData["tier"])
	for i := range s.cfg.Limits {
		l := &s.cfg.Limits[i]
		if !strings.EqualFold(l.Currency, b.Currency) {
			continue
		}
		if l.Tier == "*" || strings.EqualFold(l.Tier, tier) {
			return l
		}
	}
	return nil
}

// outflows lists every transaction with the account as source since t.
func (s *Service) outflows(ctx context.Context, accountID string, since time.Time) ([]blnk.Transaction, error) {
	var out []blnk.Transaction
	for offset := 0; ; offset += 100 {
		page, _, err := s.c.FilterTransactions(ctx, blnk.FilterRequest{
			Filters: []blnk.Filter{
				{Field: "source", Operator: "eq", Value: accountID},
				{Field: "created_at", Operator: "gte", Value: since.UTC().Format(time.RFC3339)},
			},
			SortBy: "created_at", SortOrder: "asc", Limit: 100, Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < 100 {
			break
		}
	}
	// Blnk 0.15's filter leaves parent_transaction empty on the rows it
	// returns, which spent needs to count each amount once. A queue copy
	// names its parent in meta_data; a commit or void of a pending
	// transaction (inflight, but APPLIED or VOID) is read back by id.
	for i := range out {
		t := &out[i]
		if t.ParentTransaction != "" {
			continue
		}
		if qp := t.QueuedParent(); qp != "" && qp != t.GetID() {
			t.ParentTransaction = qp
			continue
		}
		if t.Inflight && (t.Status == blnk.StatusApplied || t.Status == blnk.StatusVoid) {
			full, err := s.c.GetTransaction(ctx, t.GetID())
			if err != nil {
				return nil, err
			}
			t.ParentTransaction = full.ParentTransaction
		}
	}
	return out, nil
}

// spent sums money committed to leave the account since t, counting each
// amount once:
//   - an INFLIGHT or QUEUED transaction counts in full;
//   - an APPLIED or INFLIGHT child of a counted INFLIGHT/QUEUED parent (a
//     commit, or the "_q" copy the queue writes) isn't counted again;
//   - a VOID (or REJECTED) child of a counted parent gives its amount back;
//   - a split parent (no single destination) is skipped; its legs count.
func spent(txs []blnk.Transaction, since time.Time) *big.Int {
	byID := make(map[string]*blnk.Transaction, len(txs))
	for i := range txs {
		byID[txs[i].GetID()] = &txs[i]
	}
	sum := new(big.Int)
	for i := range txs {
		t := &txs[i]
		if t.CreatedAt.Before(since) || isSplitParent(t) {
			continue
		}
		parent := byID[t.ParentTransaction]
		// The parent (or, for a split, its legs) already counted this money.
		counted := parent != nil && !parent.CreatedAt.Before(since) &&
			(parent.Status == blnk.StatusInflight || parent.Status == blnk.StatusQueued)
		switch t.Status {
		case blnk.StatusInflight, blnk.StatusQueued, blnk.StatusApplied:
			if !counted {
				sum.Add(sum, &t.PreciseAmount.Int)
			}
		case blnk.StatusVoid, blnk.StatusRejected:
			if counted {
				sum.Sub(sum, &t.PreciseAmount.Int)
			}
		}
	}
	if sum.Sign() < 0 {
		sum.SetInt64(0)
	}
	return sum
}

// isSplitParent reports whether t is the parent record of a multi-destination
// (or multi-source) transaction, whose legs are separate child transactions.
func isSplitParent(t *blnk.Transaction) bool {
	return (t.Destination == "" && len(t.Destinations) > 0) || (t.Source == "" && len(t.Sources) > 0)
}

// limited reports whether a movement of this txn_type counts against limits:
// customer-initiated spending does; refunds, escrow settlement, disputes,
// adjustments and back-office flows don't.
func limited(kind string) bool {
	switch kind {
	case "transfer", "hold", "exchange", "split", "escrow_fund", "withdrawal":
		return true
	}
	return false
}
