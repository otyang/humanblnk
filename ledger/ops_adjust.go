package ledger

import (
	"context"
	"fmt"
	"strings"
)

// AdjustmentType is the direction of a manual correction.
type AdjustmentType int

const (
	AdjustCredit AdjustmentType = iota + 1 // add money to the account
	AdjustDebit                            // take money from the account
)

// AdjustInput is a manual correction, with who asked for it and who approved it.
type AdjustInput struct {
	AccountID   AccountID // the customer account corrected
	Type        AdjustmentType
	Amount      Money
	Reason      string // REQUIRED
	RequestedBy string
	ApprovedBy  string // REQUIRED; must differ from RequestedBy when both are set
	Reference   string // REQUIRED
	// Counterparty is the other side of the entry, a house account; default @Adjustments.
	Counterparty string
	// AllowOverdraft lets a debit take the account below zero.
	AllowOverdraft bool
}

// Adjust posts a correction between the account and @Adjustments, recording
// reason, requester and approver in metadata so every correction is auditable.
func (s opsAPI) Adjust(ctx context.Context, in AdjustInput) (*Receipt, error) {
	const op = "ledger.Ops.Adjust"
	if strings.TrimSpace(in.Reason) == "" {
		return nil, invalid(op, "reason is required")
	}
	if strings.TrimSpace(in.ApprovedBy) == "" {
		return nil, &OpError{Op: op, Kind: ErrApprovalRequired, Cause: fmt.Errorf("ApprovedBy is required")}
	}
	if in.RequestedBy != "" && strings.EqualFold(in.RequestedBy, in.ApprovedBy) {
		return nil, &OpError{Op: op, Kind: ErrApprovalRequired, Cause: fmt.Errorf("requester cannot approve their own adjustment")}
	}
	if err := customerOnly(op, in.AccountID); err != nil {
		return nil, err
	}
	other := AccountID(firstNonEmpty(in.Counterparty, AdjustmentsAccount))
	if !other.isIndicator() {
		return nil, invalid(op, "counterparty must be a house account (\"@Name\")")
	}
	t := TransferInput{
		Amount: in.Amount, Reference: in.Reference, Description: "Adjustment: " + in.Reason,
		MetaData: map[string]any{
			"adjust_reason": in.Reason, "requested_by": in.RequestedBy, "approved_by": in.ApprovedBy,
		},
	}
	switch in.Type {
	case AdjustCredit:
		t.From, t.To, t.AllowOverdraft = other, in.AccountID, true
	case AdjustDebit:
		t.From, t.To, t.AllowOverdraft = in.AccountID, other, in.AllowOverdraft
	default:
		return nil, invalid(op, "type must be AdjustCredit or AdjustDebit")
	}
	return s.move(ctx, op, t, "adjustment")
}
