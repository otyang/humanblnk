package ledger

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// Escrow is money taken from a payer and held until it is released to the
// beneficiary or refunded to the payer, in whole or in parts.
//
// Each escrow is its own Blnk balance, so what is still held is simply that
// balance, and Blnk itself refuses to release or refund more than is left.
type Escrow struct {
	ID          AccountID // the escrow account id; pass it to Wallet().ReleaseEscrow / Refund
	Payer       AccountID
	Beneficiary AccountID
	Amount      Money  // amount originally put in escrow
	Held        Money  // still in escrow
	Status      string // "held" while anything remains, then "settled"
	Reference   string // reference of the funding transaction
	Account     *Account
}

// EscrowInput opens an escrow.
type EscrowInput struct {
	Payer       AccountID // customer account the money comes from
	Beneficiary AccountID // account it goes to on release: a customer account, or a house account as AccountID("@Name")
	Amount      Money
	Reference   string // REQUIRED; retrying with the same reference returns the same escrow
	Description string
	MetaData    map[string]any
}

// EscrowSettleInput releases or refunds escrowed money.
type EscrowSettleInput struct {
	Amount      Money  // zero = everything still held
	Reference   string // REQUIRED
	Description string
	MetaData    map[string]any
}

// OpenEscrow opens an escrow account and moves Amount into it from the payer.
func (s walletAPI) OpenEscrow(ctx context.Context, in EscrowInput) (*Escrow, error) {
	const op = "ledger.Wallet.OpenEscrow"
	if err := s.validateMove(op, in.Payer, in.Reference); err != nil {
		return nil, err
	}
	if err := customerOnly(op, in.Payer); err != nil {
		return nil, err
	}
	if in.Beneficiary == "" || in.Beneficiary == in.Payer {
		return nil, invalid(op, "a beneficiary different from the payer is required")
	}

	payer, err := s.loadAccount(ctx, op, in.Payer)
	if err != nil {
		return nil, err
	}
	amount, err := s.resolveIn(op, in.Amount, payer.Currency)
	if err != nil {
		return nil, err
	}

	// A retry finds the funding transaction and returns the escrow it
	// funded, if it is the same escrow.
	if t, err := s.byReference(ctx, in.Reference); err == nil {
		if kind, _ := t.MetaData["txn_type"].(string); kind != "escrow_fund" || t.Source != string(in.Payer) ||
			t.PreciseAmount.Cmp(amount.PreciseAmount) != 0 || str(t.MetaData["escrow_beneficiary"]) != string(in.Beneficiary) {
			return nil, &OpError{Op: op, Kind: ErrDuplicateReference,
				Cause: fmt.Errorf("reference %q belongs to another transaction or escrow", in.Reference)}
		}
		return s.Wallet().GetEscrow(ctx, AccountID(t.Destination))
	} else if !blnk.IsNotFound(err) {
		return nil, mapErr(op, err)
	}
	// Check what the funding transfer would check before opening the escrow
	// account, so a refused escrow doesn't leave an empty account behind.
	if err := s.checkActiveWith(ctx, op, map[AccountID]*blnk.Balance{in.Payer: payer}, []AccountID{in.Payer}, in.Beneficiary); err != nil {
		return nil, err
	}
	if err := s.checkLimits(ctx, op, in.Payer, amount.PreciseAmount); err != nil {
		return nil, err
	}
	if payer.Available().Cmp(amount.PreciseAmount) < 0 {
		return nil, &OpError{Op: op, Kind: ErrInsufficientFunds, Cause: fmt.Errorf(
			"account %s has %s available, escrow needs %s", in.Payer, s.money(payer.Available(), amount.Precision, amount.Currency), amount.Display)}
	}
	// A retry after a failed funding reuses the account opened for this reference.
	var escrowID string
	if prev, err := s.findOne(ctx, AccountQuery{Kind: EscrowAccount, MetaData: map[string]string{"escrow_reference": in.Reference}}); err != nil {
		return nil, err
	} else if prev != nil {
		// Reuse it only for the same escrow: release and refund pay whoever
		// its metadata names.
		if str(prev.MetaData["escrow_payer"]) != string(in.Payer) ||
			str(prev.MetaData["escrow_beneficiary"]) != string(in.Beneficiary) ||
			str(prev.MetaData["escrow_amount"]) != amount.PreciseAmount.String() ||
			!strings.EqualFold(prev.Currency, amount.Currency) {
			return nil, &OpError{Op: op, Kind: ErrDuplicateReference,
				Cause: fmt.Errorf("reference %q belongs to another escrow", in.Reference)}
		}
		escrowID = string(prev.ID)
	} else {
		acct, err := s.c.CreateBalance(ctx, blnk.CreateBalanceRequest{
			LedgerID: payer.LedgerID, Currency: payer.Currency, IdentityID: payer.IdentityID,
			MetaData: blnk.MetaData{
				metaKind: string(EscrowAccount), "status": "active", metaPrecision: strconv.FormatInt(amount.Precision, 10),
				"escrow_payer": string(in.Payer), "escrow_beneficiary": string(in.Beneficiary),
				"escrow_reference": in.Reference, "escrow_amount": amount.PreciseAmount.String(),
			},
		})
		if err != nil {
			return nil, mapErr(op, err)
		}
		escrowID = acct.BalanceID
	}
	md := map[string]any{"escrow_id": escrowID, "escrow_beneficiary": string(in.Beneficiary)}
	for k, v := range in.MetaData {
		md[k] = v
	}
	if _, err := s.move(ctx, op, TransferInput{
		From: in.Payer, To: AccountID(escrowID), Amount: amount, Reference: in.Reference,
		Description: firstNonEmpty(in.Description, "Escrow"), MetaData: md,
	}, "escrow_fund"); err != nil {
		return nil, err
	}
	return s.Wallet().GetEscrow(ctx, AccountID(escrowID))
}

// GetEscrow returns an escrow and how much of it is still held.
func (s walletAPI) GetEscrow(ctx context.Context, escrowID AccountID) (*Escrow, error) {
	const op = "ledger.Wallet.GetEscrow"
	b, err := s.escrowBalance(ctx, op, escrowID)
	if err != nil {
		return nil, err
	}
	a := s.toAccount(b)
	e := &Escrow{
		ID: AccountID(b.BalanceID), Account: a, Held: a.Balance, Status: "held",
		Reference: str(b.MetaData["escrow_reference"]),
		Payer:     AccountID(str(b.MetaData["escrow_payer"])), Beneficiary: AccountID(str(b.MetaData["escrow_beneficiary"])),
	}
	if v, ok := new(big.Int).SetString(str(b.MetaData["escrow_amount"]), 10); ok {
		e.Amount = s.money(v, a.Precision, a.Currency)
	}
	if a.Balance.Sign() <= 0 {
		e.Status = "settled"
	}
	return e, nil
}

// ReleaseEscrow pays escrowed money to the beneficiary.
func (s walletAPI) ReleaseEscrow(ctx context.Context, escrowID AccountID, in EscrowSettleInput) (*Receipt, error) {
	return s.settleEscrow(ctx, "ledger.Wallet.ReleaseEscrow", escrowID, in, "escrow_beneficiary", "escrow_release")
}

// RefundEscrow returns escrowed money to the payer.
func (s walletAPI) RefundEscrow(ctx context.Context, escrowID AccountID, in EscrowSettleInput) (*Receipt, error) {
	return s.settleEscrow(ctx, "ledger.Wallet.RefundEscrow", escrowID, in, "escrow_payer", "escrow_refund")
}

func (s *Service) settleEscrow(ctx context.Context, op string, escrowID AccountID, in EscrowSettleInput, toKey, kind string) (*Receipt, error) {
	b, err := s.escrowBalance(ctx, op, escrowID)
	if err != nil {
		return nil, err
	}
	amount := in.Amount
	if !amount.isSet() {
		amount = s.money(&b.Balance.Int, s.precisionOf(b), b.Currency)
		if amount.Sign() <= 0 {
			return nil, invalid(op, "escrow %s has nothing left", escrowID)
		}
	}
	if amount, err = s.resolveIn(op, amount, b.Currency); err != nil {
		return nil, err
	}
	md := map[string]any{"escrow_id": string(escrowID)}
	for k, v := range in.MetaData {
		md[k] = v
	}
	return s.move(ctx, op, TransferInput{
		From: escrowID, To: AccountID(str(b.MetaData[toKey])), Amount: amount, Reference: in.Reference,
		Description: in.Description, MetaData: md,
	}, kind)
}

func (s *Service) escrowBalance(ctx context.Context, op string, id AccountID) (*blnk.Balance, error) {
	if id == "" {
		return nil, invalid(op, "escrow id is required")
	}
	b, err := s.loadAccount(ctx, op, id)
	if err != nil {
		return nil, err
	}
	if str(b.MetaData[metaKind]) != string(EscrowAccount) {
		return nil, invalid(op, "account %s is not an escrow", id)
	}
	return b, nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
