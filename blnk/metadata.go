package blnk

import (
	"context"
	"net/http"
)

// UpdateMetadata merges keys into the metadata of a ledger, balance,
// transaction or identity (POST /{entity_id}/metadata).
func (c *Client) UpdateMetadata(ctx context.Context, entityID string, md MetaData) (MetaData, error) {
	var out struct {
		MetaData MetaData `json:"meta_data"`
	}
	err := c.do(ctx, http.MethodPost, "/"+esc(entityID)+"/metadata", map[string]any{"meta_data": md}, &out)
	return out.MetaData, err
}

// MetaData is free-form metadata attached to any Blnk entity.
type MetaData map[string]any
