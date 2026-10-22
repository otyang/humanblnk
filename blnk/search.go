package blnk

import (
	"context"
	"encoding/json"
	"net/http"
)

// Search collections.
const (
	CollectionLedgers         = "ledgers"
	CollectionBalances        = "balances"
	CollectionTransactions    = "transactions"
	CollectionIdentities      = "identities"
	CollectionReconciliations = "reconciliations"
)

// Query runs a Typesense search on one collection (POST /search/{collection}).
// Needs Typesense configured on Core.
func (s *SearchService) Query(ctx context.Context, collection string, q SearchParams) (*SearchResult, error) {
	var out SearchResult
	return &out, s.client.do(ctx, http.MethodPost, "/search/"+esc(collection), q, &out)
}

// MultiQuery runs several searches in one request (POST /multi-search); set
// Collection on each. Results come back in the same order.
func (s *SearchService) MultiQuery(ctx context.Context, searches []SearchParams) ([]SearchResult, error) {
	var out struct {
		Results []SearchResult `json:"results"`
	}
	err := s.client.do(ctx, http.MethodPost, "/multi-search", map[string][]SearchParams{"searches": searches}, &out)
	return out.Results, err
}

// Reindex drops and rebuilds every search collection from the database in
// the background (POST /search/reindex). batchSize <= 0 leaves Blnk's default
// of 1000. A reindex already running fails with SRCH_REINDEX_IN_PROGRESS.
func (s *SearchService) Reindex(ctx context.Context, batchSize int) (*ReindexProgress, error) {
	var out struct {
		Progress ReindexProgress `json:"progress"`
	}
	body := map[string]int{}
	if batchSize > 0 {
		body["batch_size"] = batchSize
	}
	return &out.Progress, s.client.do(ctx, http.MethodPost, "/search/reindex", body, &out)
}

// ReindexProgress reports the current or last reindex (GET /search/reindex);
// SRCH_REINDEX_NOT_STARTED if there has been none since Core started.
func (s *SearchService) ReindexProgress(ctx context.Context) (*ReindexProgress, error) {
	var out ReindexProgress
	return &out, s.client.do(ctx, http.MethodGet, "/search/reindex", nil, &out)
}

// SearchParams are Typesense search parameters
// (https://typesense.org/docs/latest/api/search.html).
type SearchParams struct {
	Collection    string `json:"collection,omitempty"` // MultiQuery only
	Q             string `json:"q"`                    // "*" matches everything
	QueryBy       string `json:"query_by,omitempty"`   // comma-separated fields
	FilterBy      string `json:"filter_by,omitempty"`  // e.g. "currency:=USD && balance:>1000"
	SortBy        string `json:"sort_by,omitempty"`    // e.g. "created_at:desc"
	FacetBy       string `json:"facet_by,omitempty"`
	IncludeFields string `json:"include_fields,omitempty"`
	ExcludeFields string `json:"exclude_fields,omitempty"`
	GroupBy       string `json:"group_by,omitempty"`
	GroupLimit    int    `json:"group_limit,omitempty"`
	Page          int    `json:"page,omitempty"`     // from 1
	PerPage       int    `json:"per_page,omitempty"` // Typesense default 10, max 250
}

// SearchResult is one collection's search answer.
type SearchResult struct {
	Found        int               `json:"found"`
	OutOf        int               `json:"out_of"`
	Page         int               `json:"page"`
	SearchTimeMS int               `json:"search_time_ms"`
	Hits         []SearchHit       `json:"hits"`
	FacetCounts  []json.RawMessage `json:"facet_counts,omitempty"`
}

// SearchHit is one matching document. Document is the indexed record
// (amounts as strings, times as unix seconds); decode it into your own type.
type SearchHit struct {
	Document   json.RawMessage   `json:"document"`
	Highlights []json.RawMessage `json:"highlights,omitempty"`
	TextMatch  int64             `json:"text_match"`
}

// ReindexProgress is the state of a reindex.
type ReindexProgress struct {
	Status           string   `json:"status"` // in_progress | completed | failed
	Phase            string   `json:"phase"`
	TotalRecords     int64    `json:"total_records"`
	ProcessedRecords int64    `json:"processed_records"`
	Errors           []string `json:"errors,omitempty"`
	StartedAt        Time     `json:"started_at"`
	CompletedAt      Time     `json:"completed_at"`
	Paused           bool     `json:"paused"`
	PauseReason      string   `json:"pause_reason,omitempty"`
	PausedAt         Time     `json:"paused_at"`
}
