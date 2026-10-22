// Package watches sets thresholds on accounts: Blnk balance monitors, which
// fire the webhook event "balance.monitor" while they hold. Use it through
// ledger.Ledger.Watches.
package watches

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/otyang/humanblnk/clients/blnk"
	"github.com/otyang/humanblnk/ledger/internal/core"
	"github.com/otyang/humanblnk/ledger/types"
	"github.com/otyang/humanblnk/money"
)

// Service creates, reads, changes and deletes watches.
type Service struct{ k *core.Core }

// New returns a Service on k.
func New(k *core.Core) *Service { return &Service{k} }

// Create starts watching an account. The threshold must be in the account's
// currency, and at most 2^53 in its smallest unit (about 90 trillion naira,
// or 0.009 ETH): Blnk reads a threshold as a float, and only up to there is
// every whole number exact. Anything larger is ErrInvalid, and Create
// checks that Blnk stored exactly the threshold sent.
func (s *Service) Create(ctx context.Context, w types.Watch) (types.Watch, error) {
	m, err := s.monitor(ctx, w)
	if err != nil {
		return types.Watch{}, err
	}
	created, err := s.k.C.Monitors.Create(ctx, m)
	if err != nil {
		return types.Watch{}, core.MapErr(err)
	}
	if got := created.Condition.PreciseValue; got == nil || got.String() != string(m.Condition.Value) {
		return types.Watch{}, fmt.Errorf("ledger: watch %s stored threshold %v, not the %s sent (Delete it)", created.MonitorID, got, m.Condition.Value)
	}
	if w.Description != "" {
		// Blnk ignores a description on create; Update sets it. The
		// condition is sent unchanged, so its stored threshold stands.
		m.Description = w.Description
		if err := s.k.C.Monitors.Update(ctx, created.MonitorID, m); err != nil {
			return types.Watch{}, fmt.Errorf("ledger: watch %s created, but setting its description failed: %w", created.MonitorID, core.MapErr(err))
		}
		created.Description = w.Description
	}
	return s.watch(ctx, created)
}

// Get fetches a watch by id.
func (s *Service) Get(ctx context.Context, id types.WatchID) (types.Watch, error) {
	m, err := s.k.C.Monitors.Get(ctx, string(id))
	if err != nil {
		return types.Watch{}, core.MapErr(err)
	}
	return s.watch(ctx, m)
}

// List lists the watches on account, or every watch when account is empty.
func (s *Service) List(ctx context.Context, account types.AccountRef) ([]types.Watch, error) {
	var (
		ms  []blnk.BalanceMonitor
		err error
	)
	if account == "" {
		ms, err = s.k.C.Monitors.List(ctx)
	} else {
		ms, err = s.k.C.Monitors.ListByBalance(ctx, string(account))
	}
	if err != nil {
		return nil, core.MapErr(err)
	}
	out := make([]types.Watch, len(ms))
	for i := range ms {
		if out[i], err = s.watch(ctx, &ms[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Change replaces watch id with w and returns the new watch, which has a new
// ID. Blnk can't change a monitor's threshold in place, so the new watch is
// created before the old one is deleted: the account is never unwatched,
// and both may fire once in between.
func (s *Service) Change(ctx context.Context, id types.WatchID, w types.Watch) (types.Watch, error) {
	created, err := s.Create(ctx, w)
	if err != nil {
		return types.Watch{}, err
	}
	if err := s.Delete(ctx, id); err != nil {
		return created, fmt.Errorf("ledger: watch %s created, but deleting %s failed (Delete it again): %w", created.ID, id, err)
	}
	return created, nil
}

// Delete stops a watch.
func (s *Service) Delete(ctx context.Context, id types.WatchID) error {
	return core.MapErr(s.k.C.Monitors.Delete(ctx, string(id)))
}

// maxExactThreshold is the largest threshold, in the smallest unit, that
// survives Blnk's int64(float64(value) × precision) unchanged.
var maxExactThreshold = new(big.Int).Lsh(big.NewInt(1), 53)

// monitor turns w into Blnk's monitor, checking its currency.
func (s *Service) monitor(ctx context.Context, w types.Watch) (blnk.BalanceMonitor, error) {
	switch {
	case w.Account == "" || w.Account.IsSystem():
		return blnk.BalanceMonitor{}, core.Invalid("a watch needs a customer account")
	case w.Field == "" || w.Op == "":
		return blnk.BalanceMonitor{}, core.Invalid("a watch needs a field and an operator")
	case !w.Threshold.Valid():
		return blnk.BalanceMonitor{}, money.ErrUnknownCurrency
	}
	if err := s.k.SameCurrency(ctx, w.Account, w.Threshold); err != nil {
		return blnk.BalanceMonitor{}, err
	}
	minor, _, err := core.ToBlnk(w.Threshold)
	if err != nil {
		return blnk.BalanceMonitor{}, err
	}
	if new(big.Int).Abs(&minor.Int).Cmp(maxExactThreshold) > 0 {
		return blnk.BalanceMonitor{}, core.Invalid("threshold %s %s is too large for Blnk to store exactly", w.Threshold, w.Threshold.Code())
	}
	return blnk.BalanceMonitor{
		BalanceID: string(w.Account),
		Condition: blnk.MonitorCondition{
			Field:     string(w.Field),
			Operator:  string(w.Op),
			Value:     json.Number(minor.String()),
			Precision: 1, // Value is already in the smallest unit
		},
	}, nil
}

// watch converts a Blnk monitor; its threshold is stored in the smallest
// unit of the account's currency.
func (s *Service) watch(ctx context.Context, m *blnk.BalanceMonitor) (types.Watch, error) {
	c, err := s.k.CurrencyOf(ctx, m.BalanceID)
	if err != nil {
		return types.Watch{}, err
	}
	var threshold money.Money
	if m.Condition.PreciseValue != nil {
		if threshold, err = money.FromMinor(&m.Condition.PreciseValue.Int, c); err != nil {
			return types.Watch{}, err
		}
	}
	return types.Watch{
		ID: types.WatchID(m.MonitorID), Account: types.AccountRef(m.BalanceID),
		Field: types.WatchField(m.Condition.Field), Op: types.WatchOp(m.Condition.Operator),
		Threshold: threshold, Description: m.Description, CreatedAt: m.CreatedAt.Time,
	}, nil
}
