package blnk

import (
	"context"
	"net/http"
)

// Create creates a ledger (POST /ledgers).
func (s *LedgerService) Create(ctx context.Context, r CreateLedgerRequest) (*Ledger, error) {
	var out Ledger
	return &out, s.client.doOnce(ctx, http.MethodPost, "/ledgers", r, &out)
}

// Get fetches a ledger by id.
func (s *LedgerService) Get(ctx context.Context, id string) (*Ledger, error) {
	var out Ledger
	return &out, s.client.do(ctx, http.MethodGet, "/ledgers/"+esc(id), nil, &out)
}

// List lists ledgers, newest first (GET /ledgers).
func (s *LedgerService) List(ctx context.Context, o ListOptions) ([]Ledger, error) {
	return getList[Ledger](ctx, s.client, o.path("/ledgers"))
}

// Filter runs POST /ledgers/filter.
func (s *LedgerService) Filter(ctx context.Context, r FilterRequest) ([]Ledger, int, error) {
	return filterList[Ledger](ctx, s.client, "/ledgers/filter", r)
}

// Update renames a ledger (PUT /ledgers/{id}); the name is the only field it changes.
func (s *LedgerService) Update(ctx context.Context, id, name string) (*Ledger, error) {
	var out Ledger
	return &out, s.client.do(ctx, http.MethodPut, "/ledgers/"+esc(id), map[string]string{"name": name}, &out)
}

// CreateLedgerRequest is the body of POST /ledgers.
type CreateLedgerRequest struct {
	Name     string   `json:"name"`
	MetaData MetaData `json:"meta_data,omitempty"`
}

// Ledger is a Blnk ledger, a group of balances.
type Ledger struct {
	LedgerID  string   `json:"ledger_id"`
	Name      string   `json:"name"`
	CreatedAt Time     `json:"created_at"`
	MetaData  MetaData `json:"meta_data,omitempty"`
}
