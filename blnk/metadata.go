package blnk

import (
	"context"
	"net/http"
)

// Update merges keys into the metadata of a ledger, balance,
// transaction or identity (POST /{entity_id}/metadata) and returns the merged
// metadata; for a transaction (or a bulk_ batch id) it returns only the keys
// sent.
func (s *MetadataService) Update(ctx context.Context, entityID string, md MetaData) (MetaData, error) {
	var out struct {
		MetaData MetaData `json:"meta_data"`
	}
	err := s.client.do(ctx, http.MethodPost, "/"+esc(entityID)+"/metadata", map[string]any{"meta_data": md}, &out)
	return out.MetaData, err
}

// MetaData is free-form metadata attached to any Blnk entity.
type MetaData map[string]any
