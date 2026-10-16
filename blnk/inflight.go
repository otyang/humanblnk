package blnk

import (
	"context"
	"net/http"
)

// UpdateInflight commits (fully or partially) or voids an INFLIGHT transaction,
// or every transaction in an inflight bulk batch when id is a batch_id. It is
// not retried after a network error or gateway timeout (see WithRetries).
func (c *Client) UpdateInflight(ctx context.Context, id string, r UpdateInflightRequest) (*UpdateInflightResponse, error) {
	var out UpdateInflightResponse
	return &out, c.doOnce(ctx, http.MethodPut, "/transactions/inflight/"+esc(id), r, &out)
}

// BulkCommitInflight commits up to 100 independent inflight transactions.
// Like UpdateInflight, it is not retried when it may have reached Blnk.
func (c *Client) BulkCommitInflight(ctx context.Context, r BulkCommitInflightRequest) (*BulkInflightResponse, error) {
	var out BulkInflightResponse
	return &out, c.doOnce(ctx, http.MethodPost, "/transactions/inflight/bulk/commit", r, &out)
}

// BulkVoidInflight voids up to 100 independent inflight transactions.
func (c *Client) BulkVoidInflight(ctx context.Context, r BulkVoidInflightRequest) (*BulkInflightResponse, error) {
	var out BulkInflightResponse
	return &out, c.do(ctx, http.MethodPost, "/transactions/inflight/bulk/void", r, &out)
}

// Inflight update actions.
const (
	ActionCommit = "commit"
	ActionVoid   = "void"
)

// UpdateInflightRequest is the body of PUT /transactions/inflight/{id}.
// PreciseAmount (commit only) performs a partial commit.
type UpdateInflightRequest struct {
	Status        string  `json:"status"`
	PreciseAmount *BigInt `json:"precise_amount,omitempty"`
	SkipQueue     bool    `json:"skip_queue,omitempty"`
}

// UpdateInflightResponse covers both queued ({"queued":true,...}) and
// synchronous (child APPLIED/VOID transaction) responses.
type UpdateInflightResponse struct {
	Transaction
	Queued bool `json:"queued"`
}

// BulkCommitItem is one transaction to commit; PreciseAmount commits part of it.
type BulkCommitItem struct {
	TransactionID string  `json:"transaction_id"`
	PreciseAmount *BigInt `json:"precise_amount,omitempty"`
}

// BulkCommitInflightRequest is the body of POST /transactions/inflight/bulk/commit.
type BulkCommitInflightRequest struct {
	SkipQueue    bool             `json:"skip_queue,omitempty"`
	Transactions []BulkCommitItem `json:"transactions"`
}

// BulkVoidInflightRequest is the body of POST /transactions/inflight/bulk/void.
type BulkVoidInflightRequest struct {
	SkipQueue      bool     `json:"skip_queue,omitempty"`
	TransactionIDs []string `json:"transaction_ids"`
}

// BulkInflightResult is the outcome for one transaction of a bulk commit or void.
type BulkInflightResult struct {
	TransactionID string `json:"transaction_id"`
	Status        string `json:"status"` // succeeded | failed | queued
	Code          string `json:"code,omitempty"`
	Message       string `json:"message,omitempty"`
}

// BulkInflightResponse is the answer to a bulk commit or void.
type BulkInflightResponse struct {
	Succeeded int                  `json:"succeeded"`
	Failed    int                  `json:"failed"`
	Results   []BulkInflightResult `json:"results"`
}
