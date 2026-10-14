package ledger

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// ----- Account closure -------------------------------------------------------

type CloseInput struct {
	// SweepTo receives any remaining balance (e.g. the customer's bank payout
	// account, or another of their accounts). Required when the balance isn't zero.
	SweepTo   string
	Reference string // REQUIRED when there is a balance to sweep
	Reason    string
}

// CloseAccount moves any remaining balance to SweepTo and marks the account
// closed. It refuses while anything is pending (INFLIGHT) or the balance is
// negative. Closed accounts are rejected by every movement when
// Config.EnforceAccountStatus is on. Closing a frozen account is allowed.
func (s *Service) CloseAccount(ctx context.Context, accountID string, in CloseInput) (*Account, error) {
	const op = "ledger.CloseAccount"
	if accountID == "" || strings.HasPrefix(accountID, "@") {
		return nil, invalid(op, "a customer account id is required")
	}
	b, err := s.c.GetBalance(ctx, accountID)
	if err != nil {
		return nil, mapErr(op, err)
	}
	if str(b.MetaData["status"]) == "closed" {
		return s.toAccount(b), nil
	}
	if b.InflightDebitBalance.Sign() != 0 || b.InflightCreditBalance.Sign() != 0 {
		return nil, &OpError{Op: op, Kind: ErrPendingActivity,
			Cause: fmt.Errorf("confirm or cancel pending transactions on %s first", accountID)}
	}
	switch b.Balance.Sign() {
	case -1:
		return nil, invalid(op, "account %s is overdrawn; settle it before closing", accountID)
	case 1:
		if in.SweepTo == "" || strings.TrimSpace(in.Reference) == "" {
			return nil, invalid(op, "account has a balance: SweepTo and Reference are required")
		}
		amount := Precise(&b.Balance.Int, s.Precision(b.Currency), b.Currency)
		// Built directly so a frozen account can still be swept on closure.
		req, err := s.transferReq(TransferInput{
			From: accountID, To: in.SweepTo, Amount: amount, Reference: in.Reference,
			Description: "Account closure", MetaData: map[string]any{"close_reason": in.Reason},
		}, amount, Precise(newInt(0), amount.Precision, amount.Currency), "account_closure")
		if err != nil {
			return nil, invalid(op, "%v", err)
		}
		if _, err := s.post(ctx, op, req); err != nil {
			return nil, err
		}
	}
	if _, err := s.c.UpdateMetadata(ctx, accountID, blnk.MetaData{
		"status": "closed", "status_reason": in.Reason,
		"status_changed_at": s.cfg.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return nil, mapErr(op, err)
	}
	return s.GetAccount(ctx, accountID)
}

// ----- Suspense ----------------------------------------------------------------

// SuspenseInput parks money that arrived but can't be matched to a customer yet.
type SuspenseInput struct {
	Amount          Money
	Provider        string // provider it arrived through, e.g. "paystack"
	ClearingAccount string // or an exact clearing account; default Config.DepositClearingAccount
	Reference       string // REQUIRED, e.g. the provider's payment id
	Note            string // what is known about it (sender name, narration, ...)
	MetaData        map[string]any
}

// ParkInSuspense records unidentified incoming money: clearing account → @Suspense.
func (s *Service) ParkInSuspense(ctx context.Context, in SuspenseInput) (*Receipt, error) {
	md := map[string]any{"suspense_note": in.Note}
	for k, v := range in.MetaData {
		md[k] = v
	}
	return s.move(ctx, "ledger.ParkInSuspense", TransferInput{
		From: s.clearing(in.ClearingAccount, in.Provider, s.cfg.DepositClearingAccount), To: SuspenseAccount,
		Amount: in.Amount, Reference: in.Reference, AllowOverdraft: true,
		Description: firstNonEmpty(in.Note, "Unidentified funds"), MetaData: md,
	}, "suspense_in")
}

// AllocateInput moves money out of suspense once its owner is known.
type AllocateInput struct {
	AccountID string // the customer account, or a clearing account to send it back
	Amount    Money
	Reference string // REQUIRED
	// SuspenseReference links the allocation to the ParkInSuspense reference.
	SuspenseReference string
	Note              string
}

// AllocateSuspense moves parked money to its owner: @Suspense → account. It
// can't allocate more than suspense holds.
func (s *Service) AllocateSuspense(ctx context.Context, in AllocateInput) (*Receipt, error) {
	return s.move(ctx, "ledger.AllocateSuspense", TransferInput{
		From: SuspenseAccount, To: in.AccountID, Amount: in.Amount, Reference: in.Reference,
		Description: firstNonEmpty(in.Note, "Suspense allocation"),
		MetaData:    map[string]any{"suspense_reference": in.SuspenseReference},
	}, "suspense_out")
}

// ----- Adjustments -------------------------------------------------------------

// AdjustmentType is the direction of a manual correction.
type AdjustmentType int

const (
	AdjustCredit AdjustmentType = iota + 1 // add money to the account
	AdjustDebit                            // take money from the account
)

// AdjustInput is a manual correction, with who asked for it and who approved it.
type AdjustInput struct {
	AccountID   string
	Type        AdjustmentType
	Amount      Money
	Reason      string // REQUIRED
	RequestedBy string
	ApprovedBy  string // REQUIRED; must differ from RequestedBy when both are set
	Reference   string // REQUIRED
	// Counterparty is the other side of the entry; default @Adjustments.
	Counterparty string
	// AllowOverdraft lets a debit take the account below zero.
	AllowOverdraft bool
}

// Adjust posts a correction between the account and @Adjustments, recording
// reason, requester and approver in metadata so every correction is auditable.
func (s *Service) Adjust(ctx context.Context, in AdjustInput) (*Receipt, error) {
	const op = "ledger.Adjust"
	if strings.TrimSpace(in.Reason) == "" {
		return nil, invalid(op, "reason is required")
	}
	if strings.TrimSpace(in.ApprovedBy) == "" {
		return nil, &OpError{Op: op, Kind: ErrApprovalRequired, Cause: fmt.Errorf("ApprovedBy is required")}
	}
	if in.RequestedBy != "" && strings.EqualFold(in.RequestedBy, in.ApprovedBy) {
		return nil, &OpError{Op: op, Kind: ErrApprovalRequired, Cause: fmt.Errorf("requester cannot approve their own adjustment")}
	}
	other := firstNonEmpty(in.Counterparty, AdjustmentsAccount)
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
	if in.AccountID == "" {
		return nil, invalid(op, "account id is required")
	}
	return s.move(ctx, op, t, "adjustment")
}

// ----- Provider settlement ---------------------------------------------------------

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
func (s *Service) RecordSettlement(ctx context.Context, in SettlementInput) (*SettlementReceipt, error) {
	const op = "ledger.RecordSettlement"
	if in.Provider == "" && in.ClearingAccount == "" {
		return nil, invalid(op, "provider or clearing account is required")
	}
	clearing := s.clearing(in.ClearingAccount, in.Provider, "")
	bank := firstNonEmpty(in.BankAccount, BankOperatingAccount)
	feeAcct := firstNonEmpty(in.FeeAccount, ProviderFeesAccount)
	amount, fee, err := s.prepare(op, clearing, in.Amount, in.Fee, in.Reference)
	if err != nil {
		return nil, err
	}
	md := map[string]any{"settlement_reference": in.Reference, "provider": in.Provider}
	desc := firstNonEmpty(in.Description, "Settlement "+strings.TrimPrefix(clearing, "@"))
	zero := Precise(newInt(0), amount.Precision, amount.Currency)

	var main, feeLeg TransferInput
	switch in.Direction {
	case SettlementCollection:
		if fee.cmp(amount) >= 0 {
			return nil, invalid(op, "fee must be less than the settled amount")
		}
		main = TransferInput{From: bank, To: clearing, Amount: amount.add(neg(fee))}
		feeLeg = TransferInput{From: feeAcct, To: clearing, Amount: fee}
	case SettlementPayout:
		main = TransferInput{From: clearing, To: bank, Amount: amount}
		feeLeg = TransferInput{From: feeAcct, To: bank, Amount: fee}
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
		t.AllowOverdraft = true // system accounts on both sides
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
