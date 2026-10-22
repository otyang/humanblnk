// Package identities creates and reads owners for customer accounts: Blnk
// identities that hold no personal data. Names, phone numbers and KYC stay in
// HumanBlnk's own customer records, which keep the identity's id. Use it through
// humanblnk.Ledger.Identities.
package identities

import (
	"context"
	"fmt"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/internal/core"
	"github.com/otyang/humanblnk/internal/types"
)

// customerKey is the metadata key FindByCustomer looks up.
const customerKey = "customer_id"

// Service creates and reads identities.
type Service struct{ k *core.Core }

// New returns a Service on k.
func New(k *core.Core) *Service { return &Service{k} }

// Create creates an identity and returns its id ("idt_…"), the Owner of the
// customer's accounts. meta is for references, never personal data; put
// HumanBlnk's customer id under "customer_id" so FindByCustomer can find it.
//
// It is not retried after a timeout (a repeat would create a second one):
// try FindByCustomer before creating again. A stray identity owns nothing,
// but a customer's accounts should all have one owner.
func (s *Service) Create(ctx context.Context, meta types.Meta) (string, error) {
	id, err := s.k.C.Identities.Create(ctx, blnk.Identity{MetaData: blnk.MetaData(meta)})
	if err != nil {
		return "", core.MapErr(err)
	}
	return id.IdentityID, nil
}

// Get fetches an identity by id.
func (s *Service) Get(ctx context.Context, id string) (types.Identity, error) {
	i, err := s.k.C.Identities.Get(ctx, id)
	if err != nil {
		return types.Identity{}, core.MapErr(err)
	}
	return identity(i), nil
}

// UpdateMeta merges meta into the identity's metadata: keys not in meta
// keep their values. Like Create, it is for references, never personal data.
func (s *Service) UpdateMeta(ctx context.Context, id string, meta types.Meta) (types.Meta, error) {
	if len(meta) == 0 {
		return nil, core.Invalid("no metadata to update")
	}
	md, err := s.k.C.Metadata.Update(ctx, id, blnk.MetaData(meta))
	if err != nil {
		return nil, core.MapErr(err)
	}
	return types.Meta(md), nil
}

// FindByCustomer returns the identity created for HumanBlnk's customer
// customerID (its meta "customer_id"), or ErrNotFound. If a retried Create
// made more than one, it returns the oldest.
func (s *Service) FindByCustomer(ctx context.Context, customerID string) (types.Identity, error) {
	if customerID == "" {
		return types.Identity{}, core.Invalid("no customer id")
	}
	ids, _, err := s.k.C.Identities.Filter(ctx, blnk.FilterRequest{
		Filters: []blnk.Filter{{Field: "meta_data." + customerKey, Operator: "eq", Value: customerID}},
		SortBy:  "created_at", SortOrder: "asc", Limit: 1,
	})
	if err != nil {
		return types.Identity{}, core.MapErr(err)
	}
	if len(ids) == 0 {
		return types.Identity{}, fmt.Errorf("%w: no identity for customer %s", types.ErrNotFound, customerID)
	}
	return identity(&ids[0]), nil
}

func identity(i *blnk.Identity) types.Identity {
	return types.Identity{ID: i.IdentityID, Meta: types.Meta(i.MetaData), CreatedAt: i.CreatedAt.Time}
}
