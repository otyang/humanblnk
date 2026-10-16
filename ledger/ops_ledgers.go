package ledger

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// CreateLedgerInput creates a ledger (Ops().CreateLedger).
type CreateLedgerInput struct {
	// Key is your unique, stable name for the ledger, e.g. "wallets-ng". It is
	// what makes Create idempotent: a second Create with the same Key returns
	// the ledger the first one made. REQUIRED.
	Key string
	// Name is the ledger's display name. Default Key.
	Name string
	// MetaData is stored on the ledger. The keys this package manages
	// ("ledger_key", "status", "status_reason", "status_changed_at",
	// "duplicate_of") are ignored here.
	MetaData map[string]any
}

// LedgerInfo is a Blnk ledger: a group of accounts.
type LedgerInfo struct {
	ID          LedgerID       `json:"id"`
	Key         string         `json:"key,omitempty"` // CreateLedgerInput.Key; empty for ledgers made elsewhere
	Name        string         `json:"name"`
	Status      string         `json:"status"`                 // active, frozen, or duplicate
	DuplicateOf LedgerID       `json:"duplicate_of,omitempty"` // set when Status is duplicate: the ledger to use
	CreatedAt   time.Time      `json:"created_at"`
	MetaData    map[string]any `json:"meta_data,omitempty"`

	// Replayed: a ledger with this Key already existed; this is it.
	Replayed bool `json:"replayed,omitempty"`
}

// LedgerQuery pages Ops().ListLedgers.
type LedgerQuery struct {
	Limit  int // default 20, max 100
	Offset int
}

// LedgerPage is one page of Ops().ListLedgers.
type LedgerPage struct {
	Items  []*LedgerInfo
	Limit  int
	Offset int
}

// ledgerMeta are the ledger metadata keys this package owns.
var ledgerMeta = []string{"ledger_key", "status", "status_reason", "status_changed_at", "duplicate_of"}

// CreateLedger creates a ledger, or returns the one already created with the same
// Key. Blnk has no idempotency for ledgers and doesn't require unique names,
// so this package provides it:
//
//  1. look for a ledger with meta_data.ledger_key == Key: found, return it;
//  2. otherwise create one carrying the key;
//  3. look again. Two Creates racing in different processes can both reach
//     step 2; then the oldest ledger is the one every caller gets, and the
//     others are marked duplicate (duplicate_of) and refused by Wallet().OpenAccount.
//
// Calls in one process with the same Key run one at a time.
func (s opsAPI) CreateLedger(ctx context.Context, in CreateLedgerInput) (*LedgerInfo, error) {
	const op = "ledger.Ops.CreateLedger"
	key := strings.TrimSpace(in.Key)
	if key == "" {
		return nil, invalid(op, "key is required: it is what keeps the same ledger from being created twice")
	}
	name := firstNonEmpty(strings.TrimSpace(in.Name), key)

	mu, _ := s.ledgerKeys.LoadOrStore(key, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	found, err := s.ledgersByKey(ctx, op, key)
	if err != nil {
		return nil, err
	}
	if len(found) > 0 {
		canonical, err := s.settleTwins(ctx, op, found)
		if err != nil {
			return nil, err
		}
		return replayLedger(op, canonical, name)
	}

	md := blnk.MetaData{}
	for k, v := range in.MetaData {
		md[k] = v
	}
	for _, k := range ledgerMeta {
		delete(md, k)
	}
	md["ledger_key"], md["status"] = key, "active"
	l, err := s.c.CreateLedger(ctx, blnk.CreateLedgerRequest{Name: name, MetaData: md})
	if err != nil {
		return nil, mapErr(op, err)
	}
	created := toLedgerInfo(l)

	found, err = s.ledgersByKey(ctx, op, key)
	if err != nil || len(found) == 0 {
		return created, nil // ours is in Blnk; the check for a racing twin can't run
	}
	canonical, err := s.settleTwins(ctx, op, found)
	if err != nil {
		return nil, err
	}
	if canonical.ID == created.ID {
		return created, nil
	}
	return replayLedger(op, canonical, name)
}

// settleTwins keeps the oldest of the ledgers sharing a key and marks the
// others as its duplicates, so Wallet().OpenAccount refuses them. found is oldest first.
func (s *Service) settleTwins(ctx context.Context, op string, found []*LedgerInfo) (*LedgerInfo, error) {
	canonical := found[0]
	for _, dup := range found[1:] {
		if dup.Status == "duplicate" {
			continue
		}
		if _, err := s.c.UpdateMetadata(ctx, string(dup.ID), blnk.MetaData{
			"status": "duplicate", "duplicate_of": string(canonical.ID),
			"status_changed_at": s.cfg.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			return nil, mapErr(op, err)
		}
		s.forgetLedger(dup.ID)
	}
	return canonical, nil
}

// replayLedger returns the existing ledger for a Create with the same key,
// unless the name differs: then the key is taken by another ledger.
func replayLedger(op string, l *LedgerInfo, name string) (*LedgerInfo, error) {
	if l.Name != name {
		return nil, &OpError{Op: op, Kind: ErrDuplicateReference,
			Cause: fmt.Errorf("ledger key %q is already used by %s, named %q", l.Key, l.ID, l.Name)}
	}
	l.Replayed = true
	return l, nil
}

// ledgersByKey returns the ledgers carrying key, oldest first.
func (s *Service) ledgersByKey(ctx context.Context, op, key string) ([]*LedgerInfo, error) {
	ls, _, err := s.c.FilterLedgers(ctx, blnk.FilterRequest{
		Filters: []blnk.Filter{{Field: metaField("ledger_key"), Operator: "eq", Value: key}},
		SortBy:  "created_at", SortOrder: "asc", Limit: 100,
	})
	if err != nil {
		return nil, mapErr(op, err)
	}
	out := make([]*LedgerInfo, 0, len(ls))
	for i := range ls {
		if str(ls[i].MetaData["ledger_key"]) == key {
			out = append(out, toLedgerInfo(&ls[i]))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// GetLedger returns a ledger. A ledger that doesn't exist is ErrLedgerNotFound.
func (s opsAPI) GetLedger(ctx context.Context, id LedgerID) (*LedgerInfo, error) {
	s.forgetLedger(id) // always read it fresh
	return s.ledgerFor(ctx, "ledger.Ops.GetLedger", id, false)
}

// ListLedgers lists ledgers, oldest first.
func (s opsAPI) ListLedgers(ctx context.Context, q LedgerQuery) (*LedgerPage, error) {
	const op = "ledger.Ops.ListLedgers"
	if q.Limit <= 0 {
		q.Limit = 20
	}
	if q.Limit > 100 {
		q.Limit = 100
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	ls, err := s.c.ListLedgers(ctx, q.Limit, q.Offset)
	if err != nil {
		return nil, mapErr(op, err)
	}
	p := &LedgerPage{Limit: q.Limit, Offset: q.Offset, Items: make([]*LedgerInfo, len(ls))}
	for i := range ls {
		p.Items[i] = toLedgerInfo(&ls[i])
	}
	return p, nil
}

// Check verifies the setup against Blnk at start-up: the client is configured
// correctly (URL, Blnk Cloud instance ID), Core is up and reaches its database
// (GET /health), and Config.LedgerID, when set, exists and isn't a duplicate.
// This module needs Blnk Core 0.15.4 or later.
func (s *Service) Check(ctx context.Context) error {
	const op = "ledger.Check"
	if _, err := s.c.Health(ctx); err != nil {
		return &OpError{Op: op, Kind: ErrUnavailable, Cause: err}
	}
	if s.cfg.LedgerID == "" {
		return nil
	}
	s.forgetLedger(s.cfg.LedgerID)
	if _, err := s.ledgerFor(ctx, op, s.cfg.LedgerID, true); err != nil {
		return fmt.Errorf("Config.LedgerID: %w", err)
	}
	return nil
}

func toLedgerInfo(l *blnk.Ledger) *LedgerInfo {
	info := &LedgerInfo{
		ID: LedgerID(l.LedgerID), Name: l.Name, Status: "active",
		Key: str(l.MetaData["ledger_key"]), DuplicateOf: LedgerID(str(l.MetaData["duplicate_of"])),
		CreatedAt: l.CreatedAt.Time, MetaData: l.MetaData,
	}
	if st := str(l.MetaData["status"]); st != "" {
		info.Status = st
	}
	return info
}
