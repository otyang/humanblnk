package ledger

import (
	"context"
	"strings"
)

// SuspenseInput holds money that arrived but can't be matched to a customer yet.
type SuspenseInput struct {
	Amount          Money
	Provider        string // provider it arrived through, e.g. "paystack"
	ClearingAccount string // or an exact clearing account; default Config.DepositClearingAccount
	Reference       string // REQUIRED, e.g. the provider's payment id
	Note            string // what is known about it (sender name, narration, ...)
	MetaData        map[string]any
}

// HoldInSuspense records unidentified incoming money: clearing account → @Suspense.
func (s opsAPI) HoldInSuspense(ctx context.Context, in SuspenseInput) (*Receipt, error) {
	md := map[string]any{"suspense_note": in.Note}
	for k, v := range in.MetaData {
		md[k] = v
	}
	return s.move(ctx, "ledger.Ops.HoldInSuspense", TransferInput{
		From: AccountID(s.clearing(in.ClearingAccount, in.Provider, s.cfg.DepositClearingAccount)), To: SuspenseAccount,
		Amount: in.Amount, Reference: in.Reference, AllowOverdraft: true,
		Description: firstNonEmpty(in.Note, "Unidentified funds"), MetaData: md,
	}, "suspense_in")
}

// ClaimFromSuspenseInput moves money out of suspense to the customer it belongs to.
// To send it back to the provider instead, use ReturnFromSuspense.
type ClaimFromSuspenseInput struct {
	AccountID AccountID // the customer account
	Amount    Money
	Reference string // REQUIRED
	// SuspenseReference links the claim to the HoldInSuspense reference.
	SuspenseReference string
	Note              string
}

// ClaimFromSuspense moves held money to its owner: @Suspense → customer
// account. It can't claim more than suspense holds.
func (s opsAPI) ClaimFromSuspense(ctx context.Context, in ClaimFromSuspenseInput) (*Receipt, error) {
	const op = "ledger.Ops.ClaimFromSuspense"
	if err := customerOnly(op, in.AccountID); err != nil {
		return nil, err
	}
	return s.move(ctx, op, TransferInput{
		From: SuspenseAccount, To: in.AccountID, Amount: in.Amount, Reference: in.Reference,
		Description: firstNonEmpty(in.Note, "Suspense claim"),
		MetaData:    map[string]any{"suspense_reference": in.SuspenseReference},
	}, "suspense_out")
}

// ReturnFromSuspenseInput sends held money back to the provider it came through.
type ReturnFromSuspenseInput struct {
	Provider        string // e.g. "paystack" → @PaystackClearing
	ClearingAccount string // or an exact clearing account; default Config.DepositClearingAccount
	Amount          Money
	Reference       string // REQUIRED
	// SuspenseReference links the return to the HoldInSuspense reference.
	SuspenseReference string
	Note              string
}

// ReturnFromSuspense sends held money back where it came from: @Suspense → the
// provider's clearing account, e.g. when the provider reverses the payment.
// It can't return more than suspense holds.
func (s opsAPI) ReturnFromSuspense(ctx context.Context, in ReturnFromSuspenseInput) (*Receipt, error) {
	const op = "ledger.Ops.ReturnFromSuspense"
	clearing := s.clearing(in.ClearingAccount, in.Provider, s.cfg.DepositClearingAccount)
	if !strings.HasPrefix(clearing, "@") {
		return nil, invalid(op, "clearing account must be a house account (\"@Name\")")
	}
	return s.move(ctx, op, TransferInput{
		From: SuspenseAccount, To: AccountID(clearing), Amount: in.Amount, Reference: in.Reference,
		Description: firstNonEmpty(in.Note, "Suspense return"),
		MetaData:    map[string]any{"suspense_reference": in.SuspenseReference},
	}, "suspense_return")
}
