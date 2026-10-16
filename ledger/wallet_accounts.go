package ledger

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// AccountQuery filters accounts. Empty fields don't filter.
type AccountQuery struct {
	IdentityID string
	Currency   string
	Kind       AccountKind       // CustomerAccount, PotAccount or EscrowAccount; empty = all three
	Status     string            // "active", "frozen", "closed", ...
	MetaData   map[string]string // exact matches on meta_data keys, e.g. {"owner": "alice"}
	Limit      int               // default 20, max 100
	Offset     int
}

// AccountPage is one page of Wallet().FindAccounts results.
type AccountPage struct {
	Items  []*Account
	Limit  int
	Offset int
	Total  int
}

// metaKey is what Blnk's filter accepts after "meta_data." (one level only).
var metaKey = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// metaField is the filter field name for a meta_data key.
func metaField(key string) string { return "meta_data." + key }

// FindAccounts lists accounts matching q, oldest first. House accounts are never
// listed (read them with Ops().ListHouseAccounts).
func (s walletAPI) FindAccounts(ctx context.Context, q AccountQuery) (*AccountPage, error) {
	const op = "ledger.Wallet.FindAccounts"
	if q.Limit <= 0 {
		q.Limit = 20
	}
	if q.Limit > 100 {
		q.Limit = 100
	}
	var fs []blnk.Filter
	add := func(field, value string) {
		if value != "" {
			fs = append(fs, blnk.Filter{Field: field, Operator: "eq", Value: value})
		}
	}
	switch q.Kind {
	case "", CustomerAccount, PotAccount, EscrowAccount:
	default:
		return nil, invalid(op, "kind must be customer, pot or escrow (house accounts: Ops().ListHouseAccounts)")
	}
	add("identity_id", q.IdentityID)
	add("currency", strings.ToUpper(q.Currency))
	add(metaField("status"), q.Status)
	if q.Kind != "" && q.Kind != CustomerAccount {
		add(metaField(metaKind), string(q.Kind))
	}
	keys := make([]string, 0, len(q.MetaData))
	for k := range q.MetaData {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !metaKey.MatchString(k) {
			return nil, invalid(op, "meta_data key %q: Blnk filters accept letters, digits and _ only, starting with a letter", k)
		}
		add(metaField(k), q.MetaData[k])
	}
	if len(fs) == 0 {
		return nil, invalid(op, "at least one filter is required")
	}
	bs, total, err := s.c.FilterBalances(ctx, blnk.FilterRequest{
		Filters: fs, SortBy: "created_at", SortOrder: "asc", IncludeCount: true,
		Limit: q.Limit, Offset: q.Offset,
	})
	if err != nil {
		return nil, mapErr(op, err)
	}
	p := &AccountPage{Limit: q.Limit, Offset: q.Offset, Total: total}
	for i := range bs {
		if strings.HasPrefix(bs[i].Indicator, "@") {
			p.Total-- // a house account: never listed here
			continue
		}
		a := s.toAccount(&bs[i])
		if q.Kind == CustomerAccount && a.Kind != CustomerAccount {
			p.Total--
			continue
		}
		p.Items = append(p.Items, a)
	}
	return p, nil
}

// findOne returns the first account matching q, or nil.
func (s *Service) findOne(ctx context.Context, q AccountQuery) (*Account, error) {
	q.Limit = 1
	p, err := s.Wallet().FindAccounts(ctx, q)
	if err != nil || len(p.Items) == 0 {
		return nil, err
	}
	return p.Items[0], nil
}

// OpenAccountInput opens a customer account.
type OpenAccountInput struct {
	Currency   string
	IdentityID string   // optional: link to a Blnk identity
	LedgerID   LedgerID // optional: overrides Config.LedgerID; must exist

	// ExternalID is your own id for this account (e.g. your user id). With it,
	// Wallet().OpenAccount is safe to retry: an account already opened with the same
	// ExternalID and currency is returned instead of a second one being created.
	ExternalID string
	// MetaData is stored on the account. The keys this package manages
	// ("status", "account_kind", "external_id") are ignored here: use Freeze,
	// Close and ExternalID instead.
	MetaData map[string]any
}

// Account is a customer account (or pot or escrow) as the wallet sees it,
// amounts in both forms. House accounts are HouseAccount.
type Account struct {
	ID         AccountID   `json:"id"`
	LedgerID   LedgerID    `json:"ledger_id"`
	IdentityID string      `json:"identity_id,omitempty"`
	Kind       AccountKind `json:"kind"` // customer, pot or escrow
	Currency   string      `json:"currency"`
	Precision  int64       `json:"precision"`

	Balances
	Available Money `json:"available"` // balance - pending debits (what can be spent now)

	Status    string         `json:"status"` // from meta_data.status ("active" if unset)
	CreatedAt time.Time      `json:"created_at"`
	MetaData  map[string]any `json:"meta_data,omitempty"`
}

// Balances are the figures every Blnk balance carries, customer or house.
type Balances struct {
	Balance    Money `json:"balance"`     // settled balance
	PendingIn  Money `json:"pending_in"`  // inflight credits
	PendingOut Money `json:"pending_out"` // inflight debits
	Credits    Money `json:"credits"`     // lifetime credits
	Debits     Money `json:"debits"`      // lifetime debits
}

// Direction filters History by which side the account is on.
type Direction int

const (
	All      Direction = iota // money in and out
	Incoming                  // the account is the destination
	Outgoing                  // the account is the source
)

// HistoryQuery filters and pages Wallet().History.
type HistoryQuery struct {
	Direction Direction
	Statuses  []string // "posted"/"pending"/... or Blnk's "APPLIED"/"INFLIGHT"/...; empty = all
	Since     time.Time
	Until     time.Time
	Limit     int // default 20, max 100
	Offset    int
}

// HistoryPage is one page of Wallet().History.
type HistoryPage struct {
	Items  []Receipt
	Limit  int
	Offset int
	Total  int
}

// managedMeta are the metadata keys this package owns on accounts. Callers
// can't set them through OpenAccountInput.MetaData.
var managedMeta = []string{"status", "status_reason", "status_changed_at", metaKind, "external_id"}

// OpenAccount creates a customer account (a Blnk balance) in Config.LedgerID or
// in.LedgerID, marked active. The ledger must exist. With ExternalID it is
// safe to retry.
func (s walletAPI) OpenAccount(ctx context.Context, in OpenAccountInput) (*Account, error) {
	md := blnk.MetaData{}
	for k, v := range in.MetaData {
		md[k] = v
	}
	for _, k := range managedMeta {
		delete(md, k)
	}
	return s.openAccount(ctx, "ledger.Wallet.OpenAccount", in, md)
}

// openAccount opens an account with md as its metadata as given, so pots and
// escrows can set their account_kind.
func (s *Service) openAccount(ctx context.Context, op string, in OpenAccountInput, md blnk.MetaData) (*Account, error) {
	ledgerID := in.LedgerID
	if ledgerID == "" {
		ledgerID = s.cfg.LedgerID
	}
	if ledgerID == "" {
		return nil, invalid(op, "ledger id is required (set Config.LedgerID)")
	}
	if strings.TrimSpace(in.Currency) == "" {
		return nil, invalid(op, "currency is required")
	}
	if err := s.knownCurrency(op, in.Currency); err != nil {
		return nil, err
	}
	if ledgerID == GeneralLedgerID {
		return nil, &OpError{Op: op, Kind: ErrWrongKind, Cause: fmt.Errorf("%s holds house accounts; open customer accounts in your own ledger", ledgerID)}
	}
	if _, err := s.ledgerFor(ctx, op, ledgerID, true); err != nil {
		return nil, err
	}
	if in.ExternalID != "" {
		existing, err := s.findOne(ctx, AccountQuery{Currency: in.Currency, MetaData: map[string]string{"external_id": in.ExternalID}})
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
	}
	if md == nil {
		md = blnk.MetaData{}
	}
	md["status"] = "active"
	if in.ExternalID != "" {
		md["external_id"] = in.ExternalID
	}
	b, err := s.c.CreateBalance(ctx, blnk.CreateBalanceRequest{
		LedgerID:   string(ledgerID),
		Currency:   strings.ToUpper(in.Currency),
		IdentityID: in.IdentityID,
		MetaData:   md,
	})
	if err != nil {
		return nil, mapErr(op, err)
	}
	return s.toAccount(b), nil
}

// GetAccount returns an account with its balance, available balance (balance minus
// pending debits) and pending amounts.
func (s walletAPI) GetAccount(ctx context.Context, id AccountID) (*Account, error) {
	b, err := s.loadAccount(ctx, "ledger.Wallet.GetAccount", id)
	if err != nil {
		return nil, err
	}
	return s.toAccount(b), nil
}

// History lists an account's transactions, newest first.
func (s walletAPI) History(ctx context.Context, id AccountID, q HistoryQuery) (*HistoryPage, error) {
	const op = "ledger.Wallet.History"
	if _, err := s.loadAccount(ctx, op, id); err != nil {
		return nil, err
	}
	return s.history(ctx, op, string(id), q)
}

// history lists the transactions of a balance (customer or house) by its id.
func (s *Service) history(ctx context.Context, op, balanceID string, q HistoryQuery) (*HistoryPage, error) {
	if q.Limit <= 0 {
		q.Limit = 20
	}
	if q.Limit > 100 {
		q.Limit = 100
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	// balance_id is Blnk's virtual "source OR destination" field, so every
	// direction is one exactly-paginated query.
	field := "balance_id"
	switch q.Direction {
	case Incoming:
		field = "destination"
	case Outgoing:
		field = "source"
	}
	fs := append([]blnk.Filter{{Field: field, Operator: "eq", Value: balanceID}}, s.historyFilters(q)...)
	txs, total, err := s.c.FilterTransactions(ctx, blnk.FilterRequest{
		Filters: fs, SortBy: "created_at", SortOrder: "desc",
		IncludeCount: true, Limit: q.Limit, Offset: q.Offset,
	})
	if err != nil {
		return nil, mapErr(op, err)
	}
	return s.page(txs, q, total), nil
}

func (s *Service) historyFilters(q HistoryQuery) []blnk.Filter {
	var fs []blnk.Filter
	if len(q.Statuses) == 1 {
		fs = append(fs, blnk.Filter{Field: "status", Operator: "eq", Value: blnkStatusOf(q.Statuses[0])})
	} else if len(q.Statuses) > 1 {
		vals := make([]any, len(q.Statuses))
		for i, st := range q.Statuses {
			vals[i] = blnkStatusOf(st)
		}
		fs = append(fs, blnk.Filter{Field: "status", Operator: "in", Values: vals})
	}
	if !q.Since.IsZero() {
		fs = append(fs, blnk.Filter{Field: "created_at", Operator: "gte", Value: q.Since.UTC().Format(time.RFC3339)})
	}
	if !q.Until.IsZero() {
		fs = append(fs, blnk.Filter{Field: "created_at", Operator: "lt", Value: q.Until.UTC().Format(time.RFC3339)})
	}
	return fs
}

func (s *Service) page(txs []blnk.Transaction, q HistoryQuery, total int) *HistoryPage {
	p := &HistoryPage{Limit: q.Limit, Offset: q.Offset, Total: total}
	for i := range txs {
		p.Items = append(p.Items, *s.toReceipt(&txs[i]))
	}
	return p
}
