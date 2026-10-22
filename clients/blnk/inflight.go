package blnk

import (
	"context"
	"net/http"
)

// Update commits (fully or partially) or voids an INFLIGHT transaction,
// or every transaction in an inflight bulk batch when id is a batch_id. A
// commit is not retried after a network error or gateway timeout (see
// WithRetries): a repeated partial commit would capture twice. A void is
// retried as usual, since it always releases the whole hold and a repeat can
// only fail with TXN_ALREADY_VOIDED.
func (s *InflightService) Update(ctx context.Context, id string, r UpdateInflightRequest) (*UpdateInflightResponse, error) {
	var out UpdateInflightResponse
	path := "/transactions/inflight/" + esc(id)
	if r.Status == ActionVoid {
		return &out, s.client.do(ctx, http.MethodPut, path, r, &out)
	}
	return &out, s.client.doOnce(ctx, http.MethodPut, path, r, &out)
}

// BulkCommit commits up to 100 independent inflight transactions.
// Like Update, it is not retried when it may have reached Blnk.
func (s *InflightService) BulkCommit(ctx context.Context, r BulkCommitInflightRequest) (*BulkInflightResponse, error) {
	var out BulkInflightResponse
	return &out, s.client.doOnce(ctx, http.MethodPost, "/transactions/inflight/bulk/commit", r, &out)
}

// BulkVoid voids up to 100 independent inflight transactions. Unlike commits
// it is retried as usual: a void always releases the whole hold, so a repeat
// can only report the item as ALREADY_QUEUED or ALREADY_VOIDED.
func (s *InflightService) BulkVoid(ctx context.Context, r BulkVoidInflightRequest) (*BulkInflightResponse, error) {
	var out BulkInflightResponse
	return &out, s.client.do(ctx, http.MethodPost, "/transactions/inflight/bulk/void", r, &out)
}

// Preview reports what Update would do, without writing anything. Being
// read-only, it is retried like any read.
func (s *InflightService) Preview(ctx context.Context, id string, r UpdateInflightRequest) (*TransactionPreview, error) {
	body := struct {
		UpdateInflightRequest
		DryRun bool `json:"dry_run"`
	}{r, true}
	var out TransactionPreview
	if err := s.client.do(ctx, http.MethodPut, "/transactions/inflight/"+esc(id), body, &out); err != nil {
		return nil, err
	}
	if err := previewed(out.DryRun, r.Status+" of "+id); err != nil {
		return nil, err
	}
	return &out, nil
}

// PreviewBulkCommit reports what BulkCommit would do; each item is projected
// on its own against current balances.
func (s *InflightService) PreviewBulkCommit(ctx context.Context, r BulkCommitInflightRequest) (*BulkPreview, error) {
	body := struct {
		BulkCommitInflightRequest
		DryRun bool `json:"dry_run"`
	}{r, true}
	var out BulkPreview
	if err := s.client.do(ctx, http.MethodPost, "/transactions/inflight/bulk/commit", body, &out); err != nil {
		return nil, err
	}
	if err := previewed(out.DryRun, "bulk commit"); err != nil {
		return nil, err
	}
	return &out, nil
}

// PreviewBulkVoid reports what BulkVoid would do.
func (s *InflightService) PreviewBulkVoid(ctx context.Context, r BulkVoidInflightRequest) (*BulkPreview, error) {
	body := struct {
		BulkVoidInflightRequest
		DryRun bool `json:"dry_run"`
	}{r, true}
	var out BulkPreview
	if err := s.client.do(ctx, http.MethodPost, "/transactions/inflight/bulk/void", body, &out); err != nil {
		return nil, err
	}
	if err := previewed(out.DryRun, "bulk void"); err != nil {
		return nil, err
	}
	return &out, nil
}

// Inflight update actions.
const (
	ActionCommit = "commit"
	ActionVoid   = "void"
)

// BulkInflightResult.Code values. These short codes are not the TXN_* codes
// of APIError: compare results against these, not CodeAlreadyCommitted etc.
const (
	ItemNotFound         = "NOT_FOUND"
	ItemAlreadyCommitted = "ALREADY_COMMITTED"
	ItemAlreadyVoided    = "ALREADY_VOIDED"
	ItemNotInflight      = "NOT_INFLIGHT"
	ItemInvalidAmount    = "INVALID_AMOUNT"
	ItemLocked           = "LOCKED"
	ItemInternalError    = "INTERNAL_ERROR"
	ItemQueued           = "QUEUED"
	ItemAlreadyQueued    = "ALREADY_QUEUED"
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
