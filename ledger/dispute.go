package ledger

import (
	"context"
	"fmt"

	"github.com/otyang/humanblnk/blnk"
)

// DisputeInput opens a dispute (chargeback) on a posted transaction.
type DisputeInput struct {
	TransactionID string // the disputed transaction (for a split, the leg's transaction id)
	Amount        Money  // zero = the full amount
	Reference     string // REQUIRED
	Reason        string
	MetaData      map[string]any
}

// DisputeOutcome is how a dispute ends.
type DisputeOutcome int

const (
	// DisputeUpheld: the payer wins and the held money goes back to them.
	DisputeUpheld DisputeOutcome = iota + 1
	// DisputeRejected: the recipient wins and the hold is released to them.
	DisputeRejected
)

// Open puts the disputed amount on hold in the recipient's account,
// pointed back at the original payer, so it can't be spent while the dispute
// is open. The hold has no expiry. Close it with Disputes().Resolve.
func (s disputesAPI) Open(ctx context.Context, in DisputeInput) (*Receipt, error) {
	const op = "ledger.Disputes.Open"
	if in.TransactionID == "" {
		return nil, invalid(op, "transaction id is required")
	}
	orig, err := s.byID(ctx, in.TransactionID)
	if err != nil {
		return nil, mapErr(op, err)
	}
	if orig.Status != blnk.StatusApplied {
		return nil, invalid(op, "only posted transactions can be disputed (status %s)", orig.Status)
	}
	if orig, err = s.payeeLeg(ctx, op, orig); err != nil {
		return nil, err
	}
	amount := s.txMoney(orig)
	if in.Amount.isSet() {
		full := amount
		if amount, err = s.resolveInAt(op, in.Amount, orig.Currency, full.Precision); err != nil {
			return nil, err
		}
		if amount.cmp(full) > 0 {
			return nil, &OpError{Op: op, Kind: ErrAmountExceeded,
				Cause: fmt.Errorf("%s is more than the disputed transaction's %s", amount, full)}
		}
	}
	md := map[string]any{"dispute_of": orig.GetID(), "dispute_reason": in.Reason}
	for k, v := range in.MetaData {
		md[k] = v
	}
	return s.hold(ctx, op, HoldInput{
		AccountID: orig.Destination, Destination: orig.Source, Amount: amount,
		Reference: in.Reference, Description: "Dispute: " + firstNonEmpty(in.Reason, orig.Reference), MetaData: md,
	}, "dispute")
}

// Resolve closes a dispute opened with Disputes().Open. With DisputeUpheld,
// partial refunds only part of the held amount; the rest stays held until you
// resolve again or reject.
func (s disputesAPI) Resolve(ctx context.Context, disputeID string, outcome DisputeOutcome, partial *Money) (*Receipt, error) {
	switch outcome {
	case DisputeUpheld:
		return s.Pending().Confirm(ctx, disputeID, partial)
	case DisputeRejected:
		if partial != nil && partial.isSet() {
			return nil, invalid("ledger.Disputes.Resolve", "a rejected dispute releases everything still held")
		}
		return s.Pending().Cancel(ctx, disputeID)
	}
	return nil, invalid("ledger.Disputes.Resolve", "unknown outcome %d", outcome)
}
