package ledger

import (
	"context"

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

// OpenDispute puts the disputed amount on hold in the recipient's account,
// pointed back at the original payer, so it can't be spent while the dispute
// is open. The hold has no expiry. Close it with ResolveDispute.
func (s *Service) OpenDispute(ctx context.Context, in DisputeInput) (*Receipt, error) {
	const op = "ledger.OpenDispute"
	if in.TransactionID == "" {
		return nil, invalid(op, "transaction id is required")
	}
	orig, err := s.c.GetTransaction(ctx, in.TransactionID)
	if err != nil {
		return nil, mapErr(op, err)
	}
	if orig.Status != blnk.StatusApplied {
		return nil, invalid(op, "only posted transactions can be disputed (status %s)", orig.Status)
	}
	if orig.Destination == "" {
		return nil, invalid(op, "transaction %s has several destinations; dispute the leg's transaction id", in.TransactionID)
	}
	amount := s.txMoney(orig)
	if in.Amount.isSet() {
		m := in.Amount
		if m.Precision == 0 {
			m.Precision = amount.Precision
		}
		if amount, err = s.resolveIn(op, m, orig.Currency); err != nil {
			return nil, err
		}
	}
	md := map[string]any{"dispute_of": in.TransactionID, "dispute_reason": in.Reason}
	for k, v := range in.MetaData {
		md[k] = v
	}
	return s.hold(ctx, op, HoldInput{
		AccountID: orig.Destination, Destination: orig.Source, Amount: amount,
		Reference: in.Reference, Description: "Dispute: " + firstNonEmpty(in.Reason, orig.Reference), MetaData: md,
	}, "dispute")
}

// ResolveDispute closes a dispute opened with OpenDispute. With DisputeUpheld,
// partial refunds only part of the held amount; the rest stays held until you
// resolve again or reject.
func (s *Service) ResolveDispute(ctx context.Context, disputeID string, outcome DisputeOutcome, partial *Money) (*Receipt, error) {
	switch outcome {
	case DisputeUpheld:
		return s.Confirm(ctx, disputeID, partial)
	case DisputeRejected:
		if partial != nil && partial.isSet() {
			return nil, invalid("ledger.ResolveDispute", "a rejected dispute releases everything still held")
		}
		return s.Cancel(ctx, disputeID)
	}
	return nil, invalid("ledger.ResolveDispute", "unknown outcome %d", outcome)
}
