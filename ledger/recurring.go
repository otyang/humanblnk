package ledger

import (
	"context"
	"fmt"
	"strings"
	"time"
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
func (s paymentsAPI) Schedule(ctx context.Context, in RecurringInput) ([]*Receipt, error) {
	const op = "ledger.Payments.Schedule"
	switch {
	case strings.TrimSpace(in.Transfer.Reference) == "":
		return nil, invalid(op, "reference (idempotency key) is required")
	case in.Count < 1 || in.Count > 120:
		return nil, invalid(op, "count must be between 1 and 120")
	case in.Every.Months < 0 || in.Every.Days < 0 || in.Every == (Interval{}):
		return nil, invalid(op, "interval must be positive")
	case in.First.IsZero() || !in.First.After(s.cfg.Now()):
		return nil, invalid(op, "first occurrence must be in the future")
	case in.Transfer.Pending:
		return nil, invalid(op, "a scheduled transfer cannot also be pending")
	}
	// The fee is decided once, now, and charged on every occurrence.
	tmpl, err := s.withTransferFee(ctx, op, in.Transfer, nil)
	if err != nil {
		return nil, err
	}
	in.Transfer = tmpl
	base := in.Transfer.Reference
	out := make([]*Receipt, 0, in.Count)
	for i := 0; i < in.Count; i++ {
		t := in.Transfer
		t.Reference = fmt.Sprintf("%s_%d", base, i+1)
		t.ScheduledFor = in.First.AddDate(0, in.Every.Months*i, in.Every.Days*i)
		t.MetaData = map[string]any{"recurring_reference": base, "occurrence": i + 1, "occurrences": in.Count}
		for k, v := range in.Transfer.MetaData {
			t.MetaData[k] = v
		}
		r, err := s.move(ctx, op, t, "transfer")
		if err != nil {
			return out, fmt.Errorf("occurrence %d of %d: %w", i+1, in.Count, err)
		}
		out = append(out, r)
	}
	return out, nil
}
