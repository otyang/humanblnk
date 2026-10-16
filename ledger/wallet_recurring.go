package ledger

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// Interval is the gap between occurrences of a recurring transfer.
type Interval struct {
	Months int
	Days   int
}

var (
	Daily   = Interval{Days: 1}
	Weekly  = Interval{Days: 7}
	Monthly = Interval{Months: 1}
)

// RecurringInput schedules the same transfer Count times.
type RecurringInput struct {
	Transfer TransferInput // Reference is the base; occurrence i uses Reference+"_"+i
	First    time.Time     // when the first occurrence runs
	Every    Interval
	Count    int // 1–120
}

// Schedule creates every occurrence up front as a Blnk scheduled
// transaction (scheduled_for), e.g. a monthly savings plan or subscription.
// Blnk has no native recurrence, so a plan is a set of scheduled transfers
// sharing meta_data.recurring_reference. Calling it again with the same input
// is safe: existing occurrences come back as replays.
//
// Monthly dates follow time.AddDate, so a plan starting on the 31st lands on
// the 1st of shorter months; start on the 28th or earlier to avoid that.
func (s walletAPI) Schedule(ctx context.Context, in RecurringInput) ([]*Receipt, error) {
	const op = "ledger.Wallet.Schedule"
	switch {
	case strings.TrimSpace(in.Transfer.Reference) == "":
		return nil, invalid(op, "reference (idempotency key) is required")
	case in.Count < 1 || in.Count > 120:
		return nil, invalid(op, "count must be between 1 and 120")
	case in.Every.Months < 0 || in.Every.Days < 0 || in.Every == (Interval{}):
		return nil, invalid(op, "interval must be positive")
	case in.First.IsZero():
		return nil, invalid(op, "first occurrence is required")
	case in.Transfer.Pending:
		return nil, invalid(op, "a scheduled transfer cannot also be pending")
	}
	// A retry finds the plan's first occurrence and returns what exists, even
	// once the first run time has passed.
	base := in.Transfer.Reference
	retry := false
	if _, err := s.byReference(ctx, base+"_1"); err == nil {
		retry = true
	} else if !blnk.IsNotFound(err) {
		return nil, mapErr(op, err)
	}
	if !retry && !in.First.After(s.cfg.Now()) {
		return nil, invalid(op, "first occurrence must be in the future")
	}
	// The fee is decided once, now, and charged on every occurrence.
	tmpl, err := s.withTransferFee(ctx, op, in.Transfer, nil)
	if err != nil {
		return nil, err
	}
	in.Transfer = tmpl
	out := make([]*Receipt, 0, in.Count)
	for i := 0; i < in.Count; i++ {
		t := in.Transfer
		t.Reference = fmt.Sprintf("%s_%d", base, i+1)
		t.ScheduledFor = in.First.AddDate(0, in.Every.Months*i, in.Every.Days*i)
		t.MetaData = map[string]any{"recurring_reference": base, "occurrence": i + 1, "occurrences": in.Count}
		for k, v := range in.Transfer.MetaData {
			t.MetaData[k] = v
		}
		if retry {
			if prev, err := s.byReference(ctx, t.Reference); err == nil {
				if !s.sameOccurrence(ctx, op, prev, t, base) {
					return out, &OpError{Op: op, Kind: ErrDuplicateReference, Cause: fmt.Errorf(
						"reference %q is already used by a different transaction than occurrence %d of this plan", t.Reference, i+1)}
				}
				r := s.toReceipt(prev)
				r.Replayed = true
				out = append(out, r)
				continue
			} else if !blnk.IsNotFound(err) {
				return out, mapErr(op, err)
			}
			if !t.ScheduledFor.After(s.cfg.Now()) {
				return out, invalid(op, "occurrence %d of %d was never created and its time has passed", i+1, in.Count)
			}
		}
		r, err := s.move(ctx, op, t, "transfer")
		if err != nil {
			return out, fmt.Errorf("occurrence %d of %d: %w", i+1, in.Count, err)
		}
		out = append(out, r)
	}
	return out, nil
}

// sameOccurrence reports whether prev is occurrence t of plan base: it belongs
// to the plan and moves the same money from the same account.
func (s *Service) sameOccurrence(ctx context.Context, op string, prev *blnk.Transaction, t TransferInput, base string) bool {
	if str(prev.MetaData["recurring_reference"]) != base {
		return false
	}
	amount, err := s.resolve(op, t.Amount, prev.Currency)
	if err != nil {
		return false
	}
	want := new(big.Int).Set(amount.PreciseAmount)
	if t.Fee.isSet() {
		fee, err := s.resolve(op, t.Fee, prev.Currency)
		if err != nil {
			return false
		}
		want.Add(want, fee.PreciseAmount)
	}
	return prev.PreciseAmount.Cmp(want) == 0 && s.sameAccount(ctx, prev.Source, string(t.From), prev.Currency)
}
