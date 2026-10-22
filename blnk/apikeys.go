package blnk

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Create issues an API key (POST /api-keys). The returned Key is the only
// time the secret is shown: Blnk stores a bcrypt hash. Scopes are
// "resource:action", e.g. "transactions:write", "balances:read", "ledgers:*".
// A non-master caller can only grant scopes it holds, to itself.
func (s *APIKeyService) Create(ctx context.Context, r CreateAPIKeyRequest) (*APIKey, error) {
	var out APIKey
	return &out, s.client.doOnce(ctx, http.MethodPost, "/api-keys", r, &out)
}

// List lists an owner's API keys (GET /api-keys). A non-master key may leave
// owner empty and sees only its own owner's keys; the master key must name an
// owner (APIKEY_OWNER_REQUIRED otherwise).
func (s *APIKeyService) List(ctx context.Context, owner string) ([]APIKey, error) {
	path := "/api-keys"
	if owner != "" {
		path += "?" + url.Values{"owner": {owner}}.Encode()
	}
	return getList[APIKey](ctx, s.client, path)
}

// Revoke revokes an API key (DELETE /api-keys/{id}); owner as for List.
func (s *APIKeyService) Revoke(ctx context.Context, id, owner string) error {
	path := "/api-keys/" + esc(id)
	if owner != "" {
		path += "?" + url.Values{"owner": {owner}}.Encode()
	}
	return s.client.do(ctx, http.MethodDelete, path, nil, nil)
}

// CreateAPIKeyRequest is the body of POST /api-keys. Every field is required.
type CreateAPIKeyRequest struct {
	Name      string    `json:"name"`
	Owner     string    `json:"owner"`
	Scopes    []string  `json:"scopes"`
	ExpiresAt time.Time `json:"expires_at"`
}

// APIKey is a Blnk API key.
type APIKey struct {
	APIKeyID   string   `json:"api_key_id"`
	Key        string   `json:"key"` // the secret from Create; List returns its bcrypt hash
	Name       string   `json:"name"`
	OwnerID    string   `json:"owner_id"`
	Scopes     []string `json:"scopes"`
	ExpiresAt  Time     `json:"expires_at"`
	CreatedAt  Time     `json:"created_at"`
	LastUsedAt Time     `json:"last_used_at"`
	IsRevoked  bool     `json:"is_revoked"`
	RevokedAt  Time     `json:"revoked_at"`
}
