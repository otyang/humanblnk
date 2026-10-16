package ledger

import (
	"context"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// SettlementDirection says which way a provider settlement moves money.
type SettlementDirection int

const (
	// SettlementCollection: the provider pays you the money it collected
	// (deposits). Clears the provider's receivable.
	SettlementCollection SettlementDirection = iota
	// SettlementPayout: your bank funds the payouts the provider made for you
	// (withdrawals). Clears the provider's payable.
	SettlementPayout
)

// SettlementInput records a provider settlement (Ops().RecordSettlement).
type SettlementInput struct {
	Provider        string // e.g. "paystack" → @PaystackClearing
	ClearingAccount string // or an exact clearing account
	Direction       SettlementDirection
	Amount          Money  // amount cleared from the clearing account
	Fee             Money  // optional provider fee, booked to the fee expense account
	Reference       string // REQUIRED, e.g. the provider's settlement id
	BankAccount     string // default BankOperatingAccount
	FeeAccount      string // default ProviderFeesAccount
	Description     string
}

// SettlementReceipt holds the settlement's entries.
type SettlementReceipt struct {
	Entries  []*Receipt // main entry, then the fee entry if any
	Replayed bool
}

// RecordSettlement clears a provider clearing account against your bank, so
// the clearing balance always equals what is still outstanding with that
// provider and can be reconciled with its settlement report.
//
//	Collection: Dr Bank (Amount−Fee), Dr ProviderFees (Fee) / Cr Clearing (Amount)
//	Payout:     Dr Clearing (Amount), Dr ProviderFees (Fee) / Cr Bank (Amount+Fee)
//
// The entries post atomically.
func (s opsAPI) RecordSettlement(ctx context.Context, in SettlementInput) (*SettlementReceipt, error) {
	const op = "ledger.Ops.RecordSettlement"
	if in.Provider == "" && in.ClearingAccount == "" {
		return nil, invalid(op, "provider or clearing account is required")
	}
	clearing := s.clearing(in.ClearingAccount, in.Provider, "")
	bank := firstNonEmpty(in.BankAccount, BankOperatingAccount)
	feeAcct := firstNonEmpty(in.FeeAccount, ProviderFeesAccount)
	amount, fee, err := s.prepare(op, AccountID(clearing), in.Amount, in.Fee, in.Reference)
	if err != nil {
		return nil, err
	}
	md := map[string]any{"settlement_reference": in.Reference, "provider": in.Provider}
	desc := firstNonEmpty(in.Description, "Settlement "+strings.TrimPrefix(clearing, "@"))
	zero := s.money(newInt(0), amount.Precision, amount.Currency)

	var main, feeLeg TransferInput
	switch in.Direction {
	case SettlementCollection:
		if fee.cmp(amount) >= 0 {
			return nil, invalid(op, "fee must be less than the settled amount")
		}
		main = TransferInput{From: AccountID(bank), To: AccountID(clearing), Amount: amount.add(neg(fee))}
		feeLeg = TransferInput{From: AccountID(feeAcct), To: AccountID(clearing), Amount: fee}
	case SettlementPayout:
		main = TransferInput{From: AccountID(clearing), To: AccountID(bank), Amount: amount}
		feeLeg = TransferInput{From: AccountID(feeAcct), To: AccountID(bank), Amount: fee}
	default:
		return nil, invalid(op, "unknown settlement direction")
	}
	main.Reference, main.Description, main.MetaData = in.Reference, desc, md
	feeLeg.Reference, feeLeg.Description, feeLeg.MetaData = in.Reference+"_fee", desc+" fee", md

	var reqs []blnk.CreateTransactionRequest
	for _, t := range []TransferInput{main, feeLeg} {
		if !t.Amount.IsPositive() {
			continue
		}
		t.AllowOverdraft = true // house accounts on both sides
		r, err := s.transferReq(t, t.Amount, zero, "settlement")
		if err != nil {
			return nil, invalid(op, "%v", err)
		}
		reqs = append(reqs, r)
	}
	entries, err := s.atomic(ctx, op, reqs)
	if err != nil {
		return nil, err
	}
	return &SettlementReceipt{Entries: entries, Replayed: entries[0].Replayed}, nil
}
