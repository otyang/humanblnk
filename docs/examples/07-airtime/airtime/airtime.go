// Package airtime sells airtime, paid from the customer's wallet. It is the
// shape every business service takes: it imports ledger, names the small
// part of it it uses as an interface, and keeps its own rules.
package airtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/money"
)

// Holds is the part of the ledger airtime uses; books.Holds satisfies it,
// and a test can pass a fake.
// snippet:interfaces
type Holds interface {
	Place(ctx context.Context, t ledger.Transaction, expires time.Time) (ledger.HoldID, error)
	Capture(ctx context.Context, h ledger.HoldID, amount money.Money) (ledger.Entry, error)
	Cancel(ctx context.Context, h ledger.HoldID) error
}

// Provider sends airtime (VTpass, Reloadly, …). id is ours and the same on
// every retry, so the provider never sends twice.
type Provider interface {
	Buy(ctx context.Context, id, phone string, amount money.Money) error
}

// end-snippet

var (
	// ErrDeclined is a provider's certain "no": nothing was sent.
	ErrDeclined = errors.New("airtime: provider declined")
	// ErrPending is no clear answer yet: the money stays held until Settle.
	ErrPending = errors.New("airtime: provider hasn't answered yet")
)

// Service sells airtime.
type Service struct {
	holds    Holds
	provider Provider
	name     string // the provider, for its clearing account
}

// New returns a Service selling through provider.
func New(h Holds, p Provider, providerName string) *Service {
	return &Service{h, p, providerName}
}

// Purchase is one sale.
type Purchase struct {
	ID     string            // ours, unique per purchase, the same on every retry
	Wallet ledger.AccountRef // the customer's wallet
	Phone  string
	Amount money.Money
}

// Buy takes the money and sends the airtime. It is safe to call again with
// the same Purchase.
// snippet:buy
func (s *Service) Buy(ctx context.Context, p Purchase) (ledger.HoldID, error) {
	hold, err := s.holds.Place(ctx, ledger.Transaction{
		Ref:         ledger.Reference("airtime", p.ID),
		From:        ledger.Single(p.Wallet, p.Amount),
		To:          ledger.Single(ledger.ClearingAccount(s.name), p.Amount),
		Description: "Airtime " + p.Phone,
		Meta:        ledger.Meta{"phone": p.Phone, "provider": s.name},
	}, time.Time{}) // no expiry: only the provider's answer settles it
	if err != nil {
		return "", err // ledger.ErrInsufficientFunds: tell the customer
	}
	err = s.provider.Buy(ctx, p.ID, p.Phone, p.Amount)
	switch {
	case err == nil:
		return hold, s.Settle(ctx, hold, true)
	case errors.Is(err, ErrDeclined):
		return hold, errors.Join(err, s.Settle(ctx, hold, false))
	default:
		return hold, fmt.Errorf("%w: %v", ErrPending, err)
	}
}

// end-snippet

// Settle finishes a purchase once the provider has answered: from Buy, its
// webhook, or a status check.
// snippet:settle
func (s *Service) Settle(ctx context.Context, hold ledger.HoldID, delivered bool) error {
	if !delivered {
		return s.holds.Cancel(ctx, hold)
	}
	_, err := s.holds.Capture(ctx, hold, money.Money{})
	return err
}

// end-snippet
