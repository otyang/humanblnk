package blnk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

// Register adds a transaction hook (POST /hooks). Blnk calls it with a
// HookPayload before (PRE_TRANSACTION) or after (POST_TRANSACTION) each
// transaction, only while Active is true. Every /hooks endpoint needs the
// master key (AUTH_MASTER_KEY_REQUIRED otherwise).
func (s *HookService) Register(ctx context.Context, h Hook) (*Hook, error) {
	var out Hook
	return &out, s.client.doOnce(ctx, http.MethodPost, "/hooks", h, &out)
}

// Get fetches a hook by id.
func (s *HookService) Get(ctx context.Context, id string) (*Hook, error) {
	var out Hook
	return &out, s.client.do(ctx, http.MethodGet, "/hooks/"+esc(id), nil, &out)
}

// List lists hooks of hookType (HookPre or HookPost), or every hook when empty.
func (s *HookService) List(ctx context.Context, hookType string) ([]Hook, error) {
	path := "/hooks"
	if hookType != "" {
		path += "?" + url.Values{"type": {hookType}}.Encode()
	}
	return getList[Hook](ctx, s.client, path)
}

// Update replaces a hook's name, URL, type, active flag, timeout and retry
// count (PUT /hooks/{id}): send every field, not just the changed ones.
// Unlike Register it applies no defaults or checks, so a zero Timeout is
// stored as 0 seconds.
func (s *HookService) Update(ctx context.Context, id string, h Hook) (*Hook, error) {
	var out Hook
	return &out, s.client.do(ctx, http.MethodPut, "/hooks/"+esc(id), h, &out)
}

// Delete deletes a hook.
func (s *HookService) Delete(ctx context.Context, id string) error {
	return s.client.do(ctx, http.MethodDelete, "/hooks/"+esc(id), nil, nil)
}

// Hook types.
const (
	HookPre  = "PRE_TRANSACTION"
	HookPost = "POST_TRANSACTION"
)

// Hook is a transaction hook.
type Hook struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Type        string `json:"type"`        // HookPre | HookPost
	Active      bool   `json:"active"`      // inactive hooks are never called
	Timeout     int    `json:"timeout"`     // seconds; Register turns <= 0 into 30
	RetryCount  int    `json:"retry_count"` // retries on failure
	CreatedAt   Time   `json:"created_at,omitzero"`
	LastRun     Time   `json:"last_run,omitzero"`
	LastSuccess bool   `json:"last_success,omitempty"`
}

// HookPayload is the body Blnk POSTs to a hook's URL.
type HookPayload struct {
	TransactionID string          `json:"transaction_id"`
	HookType      string          `json:"hook_type"`
	Timestamp     time.Time       `json:"timestamp"`
	Data          json.RawMessage `json:"data,omitempty"` // the transaction
}

// HookResponse is what a hook endpoint answers.
type HookResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}
