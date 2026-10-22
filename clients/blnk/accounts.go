package blnk

import (
	"context"
	"net/http"
	"net/url"
)

// Create creates an account (POST /accounts): a bank account number linked to
// a balance. Give either BalanceID, or LedgerID + IdentityID + Currency for
// Blnk to create the balance; never both.
func (s *AccountService) Create(ctx context.Context, r CreateAccountRequest) (*Account, error) {
	var out Account
	return &out, s.client.doOnce(ctx, http.MethodPost, "/accounts", r, &out)
}

// Get fetches an account by id; include embeds its "balance", "identity"
// and/or "ledger".
func (s *AccountService) Get(ctx context.Context, id string, include ...string) (*Account, error) {
	path := "/accounts/" + esc(id)
	if len(include) > 0 {
		path += "?" + url.Values{"include": include}.Encode()
	}
	var out Account
	return &out, s.client.do(ctx, http.MethodGet, path, nil, &out)
}

// List lists accounts (GET /accounts). Without o.Filters Blnk returns every
// account and ignores Limit and Offset.
func (s *AccountService) List(ctx context.Context, o ListOptions) ([]Account, error) {
	return getList[Account](ctx, s.client, o.path("/accounts"))
}

// Filter runs POST /accounts/filter.
func (s *AccountService) Filter(ctx context.Context, r FilterRequest) ([]Account, int, error) {
	return filterList[Account](ctx, s.client, "/accounts/filter", r)
}

// CreateAccountRequest is the body of POST /accounts.
type CreateAccountRequest struct {
	BankName   string   `json:"bank_name,omitempty"`
	Number     string   `json:"number,omitempty"`
	Currency   string   `json:"currency,omitempty"`
	IdentityID string   `json:"identity_id,omitempty"`
	LedgerID   string   `json:"ledger_id,omitempty"`
	BalanceID  string   `json:"balance_id,omitempty"`
	MetaData   MetaData `json:"meta_data,omitempty"`
}

// Account is a Blnk account.
type Account struct {
	AccountID  string    `json:"account_id"`
	Name       string    `json:"name"`
	Number     string    `json:"number"`
	BankName   string    `json:"bank_name"`
	Currency   string    `json:"currency"`
	BalanceID  string    `json:"balance_id"`
	IdentityID string    `json:"identity_id"`
	LedgerID   string    `json:"ledger_id"`
	Balance    *Balance  `json:"balance,omitempty"`
	Identity   *Identity `json:"identity,omitempty"`
	Ledger     *Ledger   `json:"ledger,omitempty"`
	CreatedAt  Time      `json:"created_at"`
	MetaData   MetaData  `json:"meta_data,omitempty"`
}
