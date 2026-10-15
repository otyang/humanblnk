package ledger

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// ----- Account closure -------------------------------------------------------

// CloseInput closes an account (Accounts().Close).
type CloseInput struct {
	// SweepTo receives any remaining balance: another customer account, or a
	// system account such as AccountID("@PayoutClearing"). Required when the
	// balance isn't zero.
	SweepTo   AccountID
	Reference string // REQUIRED when there is a balance to sweep
	Reason    string
}

// Close moves any remaining balance to SweepTo and marks the account
// closed. It refuses while anything is pending (INFLIGHT) or the balance is
// negative. Closed accounts are rejected by every movement when
// Config.EnforceAccountStatus is on. Closing a frozen account is allowed.
func (s accountsAPI) Close(ctx context.Context, accountID AccountID, in CloseInput) (*Account, error) {
	const op = "ledger.Accounts.Close"
	b, err := s.loadAccount(ctx, op, accountID)
	if err != nil {
		return nil, err
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
		if in.SweepTo == accountID {
			return nil, invalid(op, "cannot sweep an account into itself")
		}
		// A frozen account can still be swept on closure; a frozen ledger can't:
		// nothing leaves it.
		if s.cfg.EnforceAccountStatus {
			lg, err := s.ledgerFor(ctx, op, LedgerID(b.LedgerID), false)
			if err != nil {
				return nil, err
			}
			if lg.Status == "frozen" {
				return nil, &OpError{Op: op, Kind: ErrLedgerFrozen, Cause: fmt.Errorf("ledger %s of account %s is frozen", b.LedgerID, accountID)}
			}
		}
		amount := s.money(&b.Balance.Int, s.Precision(b.Currency), b.Currency)
		// Built directly so a frozen account can still be swept on closure.
		req, err := s.transferReq(TransferInput{
			From: accountID, To: in.SweepTo, Amount: amount, Reference: in.Reference,
			Description: "Account closure", MetaData: map[string]any{"close_reason": in.Reason},
		}, amount, s.money(newInt(0), amount.Precision, amount.Currency), "account_closure")
		if err != nil {
			return nil, invalid(op, "%v", err)
		}
		if _, err := s.post(ctx, op, req); err != nil {
			return nil, err
		}
	}
	if _, err := s.c.UpdateMetadata(ctx, string(accountID), blnk.MetaData{
		"status": "closed", "status_reason": in.Reason,
		"status_changed_at": s.cfg.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return nil, mapErr(op, err)
	}
	return s.Accounts().Get(ctx, accountID)
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
func (s operationsAPI) ParkInSuspense(ctx context.Context, in SuspenseInput) (*Receipt, error) {
	md := map[string]any{"suspense_note": in.Note}
	for k, v := range in.MetaData {
		md[k] = v
	}
	return s.move(ctx, "ledger.Operations.ParkInSuspense", TransferInput{
		From: AccountID(s.clearing(in.ClearingAccount, in.Provider, s.cfg.DepositClearingAccount)), To: SuspenseAccount,
		Amount: in.Amount, Reference: in.Reference, AllowOverdraft: true,
		Description: firstNonEmpty(in.Note, "Unidentified funds"), MetaData: md,
	}, "suspense_in")
}

// AllocateInput moves money out of suspense to the customer it belongs to.
// To send it back to the provider instead, use ReturnSuspense.
type AllocateInput struct {
	AccountID AccountID // the customer account
	Amount    Money
	Reference string // REQUIRED
	// SuspenseReference links the allocation to the ParkInSuspense reference.
	SuspenseReference string
	Note              string
}

// AllocateSuspense moves parked money to its owner: @Suspense → customer
// account. It can't allocate more than suspense holds.
func (s operationsAPI) AllocateSuspense(ctx context.Context, in AllocateInput) (*Receipt, error) {
	const op = "ledger.Operations.AllocateSuspense"
	if err := customerOnly(op, in.AccountID); err != nil {
		return nil, err
	}
	return s.move(ctx, op, TransferInput{
		From: SuspenseAccount, To: in.AccountID, Amount: in.Amount, Reference: in.Reference,
		Description: firstNonEmpty(in.Note, "Suspense allocation"),
		MetaData:    map[string]any{"suspense_reference": in.SuspenseReference},
	}, "suspense_out")
}

// ReturnSuspenseInput sends parked money back to the provider it came through.
type ReturnSuspenseInput struct {
	Provider        string // e.g. "paystack" → @PaystackClearing
	ClearingAccount string // or an exact clearing account; default Config.DepositClearingAccount
	Amount          Money
	Reference       string // REQUIRED
	// SuspenseReference links the return to the ParkInSuspense reference.
	SuspenseReference string
	Note              string
}

// ReturnSuspense sends parked money back where it came from: @Suspense → the
// provider's clearing account, e.g. when the provider reverses the payment.
// It can't return more than suspense holds.
func (s operationsAPI) ReturnSuspense(ctx context.Context, in ReturnSuspenseInput) (*Receipt, error) {
	const op = "ledger.Operations.ReturnSuspense"
	clearing := s.clearing(in.ClearingAccount, in.Provider, s.cfg.DepositClearingAccount)
	if !strings.HasPrefix(clearing, "@") {
		return nil, invalid(op, "clearing account must be a system account (\"@Name\")")
	}
	return s.move(ctx, op, TransferInput{
		From: SuspenseAccount, To: AccountID(clearing), Amount: in.Amount, Reference: in.Reference,
		Description: firstNonEmpty(in.Note, "Suspense return"),
		MetaData:    map[string]any{"suspense_reference": in.SuspenseReference},
	}, "suspense_return")
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
	AccountID   AccountID // the customer account corrected
	Type        AdjustmentType
	Amount      Money
	Reason      string // REQUIRED
	RequestedBy string
	ApprovedBy  string // REQUIRED; must differ from RequestedBy when both are set
	Reference   string // REQUIRED
	// Counterparty is the other side of the entry, a system account; default @Adjustments.
	Counterparty string
	// AllowOverdraft lets a debit take the account below zero.
	AllowOverdraft bool
}

// Adjust posts a correction between the account and @Adjustments, recording
// reason, requester and approver in metadata so every correction is auditable.
func (s operationsAPI) Adjust(ctx context.Context, in AdjustInput) (*Receipt, error) {
	const op = "ledger.Operations.Adjust"
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
		return nil, invalid(op, "counterparty must be a system account (\"@Name\")")
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

// SettlementInput records a provider settlement (Operations().RecordSettlement).
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
func (s operationsAPI) RecordSettlement(ctx context.Context, in SettlementInput) (*SettlementReceipt, error) {
	const op = "ledger.Operations.RecordSettlement"
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
