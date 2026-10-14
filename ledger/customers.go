package ledger

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// ----- Finding accounts --------------------------------------------------------

// AccountQuery filters accounts. Empty fields don't filter.
type AccountQuery struct {
	IdentityID string
	Currency   string
	Kind       AccountKind       // CustomerAccount, PotAccount, EscrowAccount (system accounts: use Accounts().GetSystem)
	Status     string            // "active", "frozen", "closed", ...
	MetaData   map[string]string // exact matches on meta_data keys, e.g. {"owner": "alice"}
	Limit      int               // default 20, max 100
	Offset     int
}

// AccountPage is one page of Accounts().Find results.
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

// Find lists accounts matching q, oldest first.
func (s accountsAPI) Find(ctx context.Context, q AccountQuery) (*AccountPage, error) {
	const op = "ledger.Accounts.Find"
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
	p, err := s.Accounts().Find(ctx, q)
	if err != nil || len(p.Items) == 0 {
		return nil, err
	}
	return p.Items[0], nil
}

// ----- Customers -------------------------------------------------------------------

// CustomerInput onboards a customer: a Blnk identity plus one account per currency.
type CustomerInput struct {
	Type             string // "individual" (default) or "organization"
	FirstName        string
	LastName         string
	OtherNames       string
	OrganizationName string
	Email            string
	Phone            string
	Country          string
	Currencies       []string // e.g. ["NGN", "USD", "USDT"]; at least one

	// ExternalID is your own user id. It makes Customers().Open safe to retry:
	// accounts already opened for it are reused, missing ones are opened.
	ExternalID string
	MetaData   map[string]any
}

// Customer is an identity with its customer accounts.
type Customer struct {
	ID       string // Blnk identity id
	Identity *blnk.Identity
	Accounts map[string]*Account // by currency
}

// Open creates the identity and opens its accounts.
func (s customersAPI) Open(ctx context.Context, in CustomerInput) (*Customer, error) {
	const op = "ledger.Customers.Open"
	if len(in.Currencies) == 0 {
		return nil, invalid(op, "at least one currency is required")
	}
	typ := firstNonEmpty(in.Type, "individual")
	if typ != "individual" && typ != "organization" {
		return nil, invalid(op, "type must be individual or organization")
	}
	if typ == "individual" && in.FirstName == "" && in.LastName == "" {
		return nil, invalid(op, "a name is required")
	}
	if typ == "organization" && in.OrganizationName == "" {
		return nil, invalid(op, "organization name is required")
	}

	var identityID string
	if in.ExternalID != "" {
		existing, err := s.findOne(ctx, AccountQuery{MetaData: map[string]string{"external_id": in.ExternalID}})
		if err != nil {
			return nil, err
		}
		if existing != nil {
			identityID = existing.IdentityID
		}
	}
	if identityID == "" {
		md := blnk.MetaData{}
		for k, v := range in.MetaData {
			md[k] = v
		}
		if in.ExternalID != "" {
			md["external_id"] = in.ExternalID
		}
		id, err := s.c.CreateIdentity(ctx, blnk.Identity{
			IdentityType: typ, FirstName: in.FirstName, LastName: in.LastName, OtherNames: in.OtherNames,
			OrganizationName: in.OrganizationName, EmailAddress: in.Email, PhoneNumber: in.Phone,
			Country: in.Country, MetaData: md,
		})
		if err != nil {
			return nil, mapErr(op, err)
		}
		identityID = id.IdentityID
	}
	for _, cur := range in.Currencies {
		if _, err := s.Accounts().Open(ctx, OpenAccountInput{
			Currency: cur, IdentityID: identityID, ExternalID: in.ExternalID, MetaData: in.MetaData,
		}); err != nil {
			return nil, fmt.Errorf("%s account: %w", strings.ToUpper(cur), err)
		}
	}
	return s.Customers().Get(ctx, identityID)
}

// Get returns an identity and its customer accounts, by currency.
// Pots and escrows are left out; list them with Customers().ListPots / Accounts().Find.
func (s customersAPI) Get(ctx context.Context, identityID string) (*Customer, error) {
	const op = "ledger.Customers.Get"
	if identityID == "" {
		return nil, invalid(op, "identity id is required")
	}
	id, err := s.c.GetIdentity(ctx, identityID)
	if err != nil {
		return nil, mapErr(op, err)
	}
	c := &Customer{ID: identityID, Identity: id, Accounts: map[string]*Account{}}
	for offset := 0; ; offset += 100 {
		p, err := s.Accounts().Find(ctx, AccountQuery{IdentityID: identityID, Limit: 100, Offset: offset})
		if err != nil {
			return nil, err
		}
		for _, a := range p.Items {
			if _, seen := c.Accounts[a.Currency]; !seen && a.Kind == CustomerAccount {
				c.Accounts[a.Currency] = a
			}
		}
		if offset+100 >= p.Total {
			return c, nil
		}
	}
}

// ----- Pots ----------------------------------------------------------------------

// PotInput opens a savings pot under a customer account.
type PotInput struct {
	ParentID string // the customer's main account
	Name     string // e.g. "Rent", unique per parent
	Goal     Money  // optional target
}

// Pot is a savings sub-account. Move money in and out with Transfer.
type Pot struct {
	*Account
	Name   string
	Parent string
	Goal   Money // zero when no goal was set
}

// OpenPot opens a pot in the parent's currency, linked to the parent and its
// identity. Opening the same name twice returns the existing pot.
func (s customersAPI) OpenPot(ctx context.Context, in PotInput) (*Pot, error) {
	const op = "ledger.Customers.OpenPot"
	if in.ParentID == "" || strings.TrimSpace(in.Name) == "" {
		return nil, invalid(op, "parent account and name are required")
	}
	parent, err := s.c.GetBalance(ctx, in.ParentID)
	if err != nil {
		return nil, mapErr(op, err)
	}
	md := map[string]any{metaKind: string(PotAccount), "parent_account": in.ParentID, "pot_name": in.Name}
	if in.Goal.isSet() {
		goal, err := s.resolveIn(op, in.Goal, parent.Currency)
		if err != nil {
			return nil, err
		}
		md["pot_goal"] = goal.Value
	}
	a, err := s.Accounts().Open(ctx, OpenAccountInput{
		Currency: parent.Currency, IdentityID: parent.IdentityID, LedgerID: parent.LedgerID,
		ExternalID: "pot:" + in.ParentID + ":" + in.Name, MetaData: md,
	})
	if err != nil {
		return nil, err
	}
	return s.toPot(a), nil
}

// ListPots lists the pots under a parent account.
func (s customersAPI) ListPots(ctx context.Context, parentID string) ([]*Pot, error) {
	if parentID == "" {
		return nil, invalid("ledger.Customers.ListPots", "parent account is required")
	}
	p, err := s.Accounts().Find(ctx, AccountQuery{Kind: PotAccount, MetaData: map[string]string{"parent_account": parentID}, Limit: 100})
	if err != nil {
		return nil, err
	}
	out := make([]*Pot, len(p.Items))
	for i, a := range p.Items {
		out[i] = s.toPot(a)
	}
	return out, nil
}

func (s *Service) toPot(a *Account) *Pot {
	p := &Pot{Account: a, Name: str(a.MetaData["pot_name"]), Parent: str(a.MetaData["parent_account"])}
	if g := str(a.MetaData["pot_goal"]); g != "" {
		p.Goal, _ = ParseDecimal(g, a.Currency, a.Precision)
	}
	return p
}
