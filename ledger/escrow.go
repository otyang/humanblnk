package ledger

import (
	"context"
	"fmt"
	"math/big"

	"github.com/otyang/humanblnk/blnk"
)

// Escrow is money taken from a payer and held until it is released to the
// beneficiary or refunded to the payer, in whole or in parts.
//
// Each escrow is its own Blnk balance, so what is still held is simply that
// balance, and Blnk itself refuses to release or refund more than is left.
type Escrow struct {
	ID          string // the escrow account id; pass it to Escrows().Release / Refund
	Payer       string
	Beneficiary string
	Amount      Money  // amount originally put in escrow
	Held        Money  // still in escrow
	Status      string // "held" while anything remains, then "settled"
	Reference   string // reference of the funding transaction
	Account     *Account
}

type EscrowInput struct {
	Payer       string // account the money comes from
	Beneficiary string // account it goes to on release
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

// Open opens an escrow account and moves Amount into it from the payer.
func (s escrowsAPI) Open(ctx context.Context, in EscrowInput) (*Escrow, error) {
	const op = "ledger.Escrows.Open"
	if err := s.validateMove(op, in.Payer, in.Reference); err != nil {
		return nil, err
	}
	if in.Beneficiary == "" || in.Beneficiary == in.Payer {
		return nil, invalid(op, "a beneficiary different from the payer is required")
	}

	// A retry finds the funding transaction and returns the escrow it funded.
	if t, err := s.byReference(ctx, in.Reference); err == nil {
		if kind, _ := t.MetaData["txn_type"].(string); kind != "escrow_fund" || t.Source != in.Payer {
			return nil, &OpError{Op: op, Kind: ErrDuplicateReference,
				Cause: fmt.Errorf("reference %q belongs to another transaction", in.Reference)}
		}
		return s.Escrows().Get(ctx, t.Destination)
	} else if !blnk.IsNotFound(err) {
		return nil, mapErr(op, err)
	}

	payer, err := s.c.GetBalance(ctx, in.Payer)
	if err != nil {
		return nil, mapErr(op, err)
	}
	amount, err := s.resolveIn(op, in.Amount, payer.Currency)
	if err != nil {
		return nil, err
	}
	acct, err := s.c.CreateBalance(ctx, blnk.CreateBalanceRequest{
		LedgerID: payer.LedgerID, Currency: payer.Currency, IdentityID: payer.IdentityID,
		MetaData: blnk.MetaData{
			metaKind: string(EscrowAccount), "status": "active",
			"escrow_payer": in.Payer, "escrow_beneficiary": in.Beneficiary,
			"escrow_reference": in.Reference, "escrow_amount": amount.PreciseAmount.String(),
		},
	})
	if err != nil {
		return nil, mapErr(op, err)
	}
	md := map[string]any{"escrow_id": acct.BalanceID, "escrow_beneficiary": in.Beneficiary}
	for k, v := range in.MetaData {
		md[k] = v
	}
	if _, err := s.move(ctx, op, TransferInput{
		From: in.Payer, To: acct.BalanceID, Amount: amount, Reference: in.Reference,
		Description: firstNonEmpty(in.Description, "Escrow"), MetaData: md,
	}, "escrow_fund"); err != nil {
		return nil, err
	}
	return s.Escrows().Get(ctx, acct.BalanceID)
}

// Get returns an escrow and how much of it is still held.
func (s escrowsAPI) Get(ctx context.Context, escrowID string) (*Escrow, error) {
	const op = "ledger.Escrows.Get"
	b, err := s.escrowBalance(ctx, op, escrowID)
	if err != nil {
		return nil, err
	}
	a := s.toAccount(b)
	e := &Escrow{
		ID: b.BalanceID, Account: a, Held: a.Balance, Status: "held",
		Reference: str(b.MetaData["escrow_reference"]),
		Payer:     str(b.MetaData["escrow_payer"]), Beneficiary: str(b.MetaData["escrow_beneficiary"]),
	}
	if v, ok := new(big.Int).SetString(str(b.MetaData["escrow_amount"]), 10); ok {
		e.Amount = s.money(v, a.Precision, a.Currency)
	}
	if a.Balance.Sign() <= 0 {
		e.Status = "settled"
	}
	return e, nil
}

// Release pays escrowed money to the beneficiary.
func (s escrowsAPI) Release(ctx context.Context, escrowID string, in EscrowSettleInput) (*Receipt, error) {
	return s.settleEscrow(ctx, "ledger.Escrows.Release", escrowID, in, "escrow_beneficiary", "escrow_release")
}

// Refund returns escrowed money to the payer.
func (s escrowsAPI) Refund(ctx context.Context, escrowID string, in EscrowSettleInput) (*Receipt, error) {
	return s.settleEscrow(ctx, "ledger.Escrows.Refund", escrowID, in, "escrow_payer", "escrow_refund")
}

func (s *Service) settleEscrow(ctx context.Context, op, escrowID string, in EscrowSettleInput, toKey, kind string) (*Receipt, error) {
	b, err := s.escrowBalance(ctx, op, escrowID)
	if err != nil {
		return nil, err
	}
	amount := in.Amount
	if !amount.isSet() {
		amount = s.money(&b.Balance.Int, s.Precision(b.Currency), b.Currency)
		if amount.Sign() <= 0 {
			return nil, invalid(op, "escrow %s has nothing left", escrowID)
		}
	}
	if amount, err = s.resolveIn(op, amount, b.Currency); err != nil {
		return nil, err
	}
	md := map[string]any{"escrow_id": escrowID}
	for k, v := range in.MetaData {
		md[k] = v
	}
	return s.move(ctx, op, TransferInput{
		From: escrowID, To: str(b.MetaData[toKey]), Amount: amount, Reference: in.Reference,
		Description: in.Description, MetaData: md,
	}, kind)
}

func (s *Service) escrowBalance(ctx context.Context, op, id string) (*blnk.Balance, error) {
	if id == "" {
		return nil, invalid(op, "escrow id is required")
	}
	b, err := s.c.GetBalance(ctx, id)
	if err != nil {
		return nil, mapErr(op, err)
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
