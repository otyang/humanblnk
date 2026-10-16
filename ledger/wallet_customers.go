package ledger

import (
	"context"
	"fmt"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

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

	// ExternalID is your own user id. It makes Wallet().OpenCustomer safe to retry:
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

// OpenCustomer creates the identity and opens its accounts.
func (s walletAPI) OpenCustomer(ctx context.Context, in CustomerInput) (*Customer, error) {
	const op = "ledger.Wallet.OpenCustomer"
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
		if _, err := s.Wallet().OpenAccount(ctx, OpenAccountInput{
			Currency: cur, IdentityID: identityID, ExternalID: in.ExternalID, MetaData: in.MetaData,
		}); err != nil {
			return nil, fmt.Errorf("%s account: %w", strings.ToUpper(cur), err)
		}
	}
	return s.Wallet().GetCustomer(ctx, identityID)
}

// GetCustomer returns an identity and its customer accounts, by currency.
// Pots and escrows are left out; list them with Wallet().ListPots / Wallet().FindAccounts.
func (s walletAPI) GetCustomer(ctx context.Context, identityID string) (*Customer, error) {
	const op = "ledger.Wallet.GetCustomer"
	if identityID == "" {
		return nil, invalid(op, "identity id is required")
	}
	id, err := s.c.GetIdentity(ctx, identityID)
	if err != nil {
		return nil, mapErr(op, err)
	}
	c := &Customer{ID: identityID, Identity: id, Accounts: map[string]*Account{}}
	for offset := 0; ; offset += 100 {
		p, err := s.Wallet().FindAccounts(ctx, AccountQuery{IdentityID: identityID, Limit: 100, Offset: offset})
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

// PotInput opens a savings pot under a customer account.
type PotInput struct {
	ParentID AccountID // the customer's main account
	Name     string    // e.g. "Rent", unique per parent
	Goal     Money     // optional target
}

// Pot is a savings sub-account. Move money in and out with Transfer.
type Pot struct {
	*Account
	Name   string
	Parent AccountID
	Goal   Money // zero when no goal was set
}

// OpenPot opens a pot in the parent's currency, linked to the parent and its
// identity. Opening the same name twice returns the existing pot.
func (s walletAPI) OpenPot(ctx context.Context, in PotInput) (*Pot, error) {
	const op = "ledger.Wallet.OpenPot"
	if in.ParentID == "" || strings.TrimSpace(in.Name) == "" {
		return nil, invalid(op, "parent account and name are required")
	}
	parent, err := s.loadAccount(ctx, op, in.ParentID)
	if err != nil {
		return nil, err
	}
	md := blnk.MetaData{metaKind: string(PotAccount), "parent_account": string(in.ParentID), "pot_name": in.Name}
	if in.Goal.isSet() {
		goal, err := s.resolveIn(op, in.Goal, parent.Currency)
		if err != nil {
			return nil, err
		}
		md["pot_goal"] = goal.Value
	}
	a, err := s.openAccount(ctx, op, OpenAccountInput{
		Currency: parent.Currency, IdentityID: parent.IdentityID, LedgerID: LedgerID(parent.LedgerID),
		ExternalID: "pot:" + string(in.ParentID) + ":" + in.Name,
	}, md)
	if err != nil {
		return nil, err
	}
	return s.toPot(a), nil
}

// ListPots lists the pots under a parent account.
func (s walletAPI) ListPots(ctx context.Context, parentID AccountID) ([]*Pot, error) {
	if _, err := s.loadAccount(ctx, "ledger.Wallet.ListPots", parentID); err != nil {
		return nil, err
	}
	p, err := s.Wallet().FindAccounts(ctx, AccountQuery{Kind: PotAccount, MetaData: map[string]string{"parent_account": string(parentID)}, Limit: 100})
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
	p := &Pot{Account: a, Name: str(a.MetaData["pot_name"]), Parent: AccountID(str(a.MetaData["parent_account"]))}
	if g := str(a.MetaData["pot_goal"]); g != "" {
		p.Goal, _ = ParseDecimal(g, a.Currency, a.Precision)
	}
	return p
}
