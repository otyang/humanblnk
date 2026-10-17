package ledger

import (
	"context"
	"fmt"
	"math/big"

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
// is open. The hold has no expiry. Close it with Wallet().ResolveDispute.
//
// A zero Amount disputes what is left of the transaction: its amount minus
// refunds and other disputes still open or upheld. Asking for more than that
// is ErrAmountExceeded; nothing left is ErrAlreadyFinalized.
func (s walletAPI) OpenDispute(ctx context.Context, in DisputeInput) (r *Receipt, err error) {
	const op = "ledger.Wallet.OpenDispute"
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
	// Only what hasn't already gone back to the payer, by refund or by
	// another dispute, can be disputed.
	c, err := s.claimsOn(ctx, orig, in.Reference)
	if err != nil {
		return nil, mapErr(op, err)
	}
	full := s.txMoney(orig)
	left := s.money(new(big.Int).Sub(full.PreciseAmount, c.total(full.PreciseAmount)), full.Precision, full.Currency)
	if !left.IsPositive() {
		return nil, &OpError{Op: op, Kind: ErrAlreadyFinalized,
			Cause: fmt.Errorf("transaction %s has already been refunded or disputed in full", orig.GetID())}
	}
	amount := left
	if in.Amount.isSet() {
		if amount, err = s.resolveInAt(op, in.Amount, orig.Currency, full.Precision); err != nil {
			return nil, err
		}
		if amount.cmp(left) > 0 {
			return nil, &OpError{Op: op, Kind: ErrAmountExceeded,
				Cause: fmt.Errorf("%s is more than the %s of transaction %s not yet refunded or disputed", amount, left, orig.GetID())}
		}
	}
	md := map[string]any{"dispute_of": orig.GetID(), "dispute_reason": in.Reason}
	for k, v := range in.MetaData {
		md[k] = v
	}
	// A frozen recipient's balance is held by FreezeAccount: release it so
	// the dispute can take its share, then hold what is left again.
	recipient := AccountID(orig.Destination)
	b, err := s.c.GetBalance(ctx, string(recipient))
	if err != nil {
		return nil, mapErr(op, err)
	}
	if str(b.MetaData[metaFreezeHold]) != "" {
		if err := s.releaseFrozen(ctx, op, recipient); err != nil {
			return nil, err
		}
		defer func() {
			if ferr := s.holdFrozen(ctx, op, recipient, str(b.MetaData["status_reason"])); ferr != nil && err == nil {
				err = ferr
			}
		}()
	}
	return s.hold(ctx, op, HoldInput{
		AccountID: AccountID(orig.Destination), Destination: AccountID(orig.Source), Amount: amount,
		Reference: in.Reference, Description: "Dispute: " + firstNonEmpty(in.Reason, orig.Reference), MetaData: md,
	}, "dispute")
}

// ResolveDispute closes a dispute opened with Wallet().OpenDispute. With DisputeUpheld,
// partial refunds only part of the held amount; the rest stays held until you
// resolve again or reject.
func (s walletAPI) ResolveDispute(ctx context.Context, disputeID string, outcome DisputeOutcome, partial *Money) (*Receipt, error) {
	switch outcome {
	case DisputeUpheld:
		return s.Wallet().Confirm(ctx, disputeID, partial)
	case DisputeRejected:
		if partial != nil && partial.isSet() {
			return nil, invalid("ledger.Wallet.ResolveDispute", "a rejected dispute releases everything still held")
		}
		return s.Wallet().Cancel(ctx, disputeID)
	}
	return nil, invalid("ledger.Wallet.ResolveDispute", "unknown outcome %d", outcome)
}
