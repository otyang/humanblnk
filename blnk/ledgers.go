package blnk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// CreateLedger creates a ledger (POST /ledgers).
func (c *Client) CreateLedger(ctx context.Context, r CreateLedgerRequest) (*Ledger, error) {
	var out Ledger
	return &out, c.do(ctx, http.MethodPost, "/ledgers", r, &out)
}

// GetLedger fetches a ledger by id.
func (c *Client) GetLedger(ctx context.Context, id string) (*Ledger, error) {
	var out Ledger
	return &out, c.do(ctx, http.MethodGet, "/ledgers/"+esc(id), nil, &out)
}

// ListLedgers lists ledgers, oldest first (GET /ledgers?limit=&offset=).
func (c *Client) ListLedgers(ctx context.Context, limit, offset int) ([]Ledger, error) {
	q := url.Values{"limit": {fmt.Sprint(limit)}, "offset": {fmt.Sprint(offset)}}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/ledgers?"+q.Encode(), nil, &raw); err != nil {
		return nil, err
	}
	out, _, err := decodeList[Ledger](raw)
	return out, err
}

// FilterLedgers runs POST /ledgers/filter.
func (c *Client) FilterLedgers(ctx context.Context, r FilterRequest) ([]Ledger, int, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, "/ledgers/filter", r, &raw); err != nil {
		return nil, 0, err
	}
	return decodeList[Ledger](raw)
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
