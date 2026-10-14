package ledger_test

import (
	"encoding/json"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/ledgertest"
)

// An amount that names its own precision is posted at the currency's
// precision with its value kept: Blnk sums precise amounts on a balance
// whatever precision a transaction names, so "1 NGN at precision 10000"
// must move 1 NGN, not 100.
func TestForeignPrecisionKeepsTheValue(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	a, b := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, a.ID, "1000", "NGN", "in")

	var fromClient ledger.Money // what an API client might send
	if err := json.Unmarshal([]byte(`{"amount":"1","currency":"NGN","precision":10000}`), &fromClient); err != nil {
		t.Fatal(err)
	}
	r, err := svc.Payments().Transfer(ctx, ledger.TransferInput{Fee: ledger.NoFee, From: a.ID, To: b.ID, Amount: fromClient, Reference: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if bal(t, svc, b.ID) != "1" || r.Amount.Precision != 100 || r.Amount.PreciseAmount.Int64() != 100 {
		t.Fatalf("moved %s (receipt %+v), want 1 NGN at precision 100", bal(t, svc, b.ID), r.Amount)
	}
	// The same through a fee and a split part.
	fee := ledger.Precise(big.NewInt(5000), 10000, "NGN") // 0.5 NGN
	if _, err := svc.Payments().Transfer(ctx, ledger.TransferInput{Fee: fee, From: a.ID, To: b.ID, Amount: ledger.Amount("10", "NGN"), Reference: "t2"}); err != nil {
		t.Fatal(err)
	}
	if got := sys(t, svc, ledger.FeeIncomeAccount, "NGN"); got != "0.5" {
		t.Fatalf("fee income %s, want 0.5", got)
	}
	if _, err := svc.Payments().Split(ctx, ledger.SplitInput{From: a.ID, Amount: ledger.Amount("10", "NGN"), Reference: "s1", Parts: []ledger.SplitPart{
		{To: b.ID, Amount: ledger.Precise(big.NewInt(20000), 10000, "NGN")}, // 2 NGN
		{To: ledger.FeeIncomeAccount, Rest: true},
	}}); err != nil {
		t.Fatal(err)
	}
	if bal(t, svc, b.ID) != "13" {
		t.Fatalf("b = %s, want 13", bal(t, svc, b.ID))
	}
	// More decimal places than the currency has is refused, not truncated.
	_, err = svc.Payments().Transfer(ctx, ledger.TransferInput{Fee: ledger.NoFee, From: a.ID, To: b.ID, Amount: ledger.Precise(big.NewInt(12345), 10000, "NGN"), Reference: "t3"})
	if !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("1.2345 NGN must be refused, got %v", err)
	}
}

// Blnk applies a partial commit to every leg of a split in turn, leaving
// some legs committed when a later one is too small, so it is refused.
func TestPartialConfirmOfAWithdrawalWithFeeIsRefused(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	a := open(t, svc, "NGN")
	fund(t, svc, a.ID, "1000", "NGN", "in")
	wd, err := svc.Payments().Withdraw(ctx, ledger.WithdrawInput{AccountID: a.ID, Amount: ledger.Amount("100", "NGN"), Fee: ledger.Amount("1", ""), Reference: "w"})
	if err != nil {
		t.Fatal(err)
	}
	part := ledger.Amount("50", "")
	if _, err := svc.Pending().Confirm(ctx, wd.TransactionID, &part); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("partial confirm of a split: %v", err)
	}
	res, err := svc.Pending().ConfirmMany(ctx, []ledger.ConfirmItem{{TransactionID: wd.TransactionID, Partial: &part}})
	if !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("ConfirmMany partial of a split: %v %v", res, err)
	}
	if _, err := svc.Pending().Confirm(ctx, wd.TransactionID, nil); err != nil {
		t.Fatalf("full confirm: %v", err)
	}
	if bal(t, svc, a.ID) != "899" {
		t.Fatalf("balance %s, want 899", bal(t, svc, a.ID))
	}
}

func TestDisputeCannotExceedTheTransaction(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	cust, merchant := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, cust.ID, "5000", "NGN", "in")
	fund(t, svc, merchant.ID, "5000", "NGN", "in2")
	pay, _ := svc.Payments().Transfer(ctx, ledger.TransferInput{Fee: ledger.NoFee, From: cust.ID, To: merchant.ID, Amount: ledger.Amount("100", "NGN"), Reference: "pay"})
	_, err := svc.Disputes().Open(ctx, ledger.DisputeInput{TransactionID: pay.TransactionID, Amount: ledger.Amount("100.01", ""), Reference: "d"})
	if !errors.Is(err, ledger.ErrAmountExceeded) {
		t.Fatalf("dispute above the payment: %v", err)
	}
}

func TestScheduleRequiresAReferenceAndIsNotTakenForLegs(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	a, b := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, a.ID, "1000", "NGN", "in")
	in := ledger.RecurringInput{
		Transfer: ledger.TransferInput{Fee: ledger.NoFee, From: a.ID, To: b.ID, Amount: ledger.Amount("10", "NGN")},
		First:    core.Now().Add(time.Hour), Every: ledger.Monthly, Count: 2,
	}
	if _, err := svc.Payments().Schedule(ctx, in); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("schedule without a reference: %v", err)
	}
	in.Transfer.Reference = "plan"
	if _, err := svc.Payments().Schedule(ctx, in); err != nil {
		t.Fatal(err)
	}
	// "plan_1" and "plan_2" are occurrences, not the legs of a "plan" split.
	if r, err := svc.Transactions().GetByReference(ctx, "plan"); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("GetByReference(plan) = %+v, %v; want ErrNotFound", r, err)
	}
}

// A retry is a replay only when it describes the same movement: the same
// total paid to one recipient is not the same as a payment plus a fee.
func TestReplayNeedsTheSameRecipients(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	a, b := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, a.ID, "1000", "NGN", "in")
	if _, err := svc.Payments().Transfer(ctx, ledger.TransferInput{Fee: ledger.Amount("1", ""), From: a.ID, To: b.ID, Amount: ledger.Amount("100", "NGN"), Reference: "t"}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Payments().Transfer(ctx, ledger.TransferInput{Fee: ledger.NoFee, From: a.ID, To: b.ID, Amount: ledger.Amount("101", "NGN"), Reference: "t"})
	if !errors.Is(err, ledger.ErrDuplicateReference) {
		t.Fatalf("101 to b is not 100 to b + 1 fee: %v", err)
	}
}

func TestPartialAndFullRefundsDontOverlap(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	cust, merchant := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, cust.ID, "5000", "NGN", "in")
	pay, _ := svc.Payments().Transfer(ctx, ledger.TransferInput{Fee: ledger.NoFee, From: cust.ID, To: merchant.ID, Amount: ledger.Amount("100", "NGN"), Reference: "pay"})

	// A retried partial refund replays instead of counting itself.
	for i := 0; i < 2; i++ {
		r, err := svc.Transactions().RefundPartial(ctx, pay.TransactionID, ledger.PartialRefundInput{Amount: ledger.Amount("100", ""), Reference: "pr1"})
		if err != nil || r.Replayed != (i == 1) {
			t.Fatalf("attempt %d: %+v %v", i, r, err)
		}
	}
	if bal(t, svc, cust.ID) != "5000" {
		t.Fatalf("cust = %s, want 5000", bal(t, svc, cust.ID))
	}
	// Blnk's full reversal would refund it a second time.
	if _, err := svc.Transactions().Refund(ctx, pay.TransactionID, ledger.RefundInput{}); !errors.Is(err, ledger.ErrAlreadyFinalized) {
		t.Fatalf("full refund after partial refunds: %v", err)
	}

	pay2, _ := svc.Payments().Transfer(ctx, ledger.TransferInput{Fee: ledger.NoFee, From: cust.ID, To: merchant.ID, Amount: ledger.Amount("100", "NGN"), Reference: "pay2"})
	if _, err := svc.Transactions().Refund(ctx, pay2.TransactionID, ledger.RefundInput{}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Transactions().RefundPartial(ctx, pay2.TransactionID, ledger.PartialRefundInput{Amount: ledger.Amount("1", ""), Reference: "pr2"})
	if !errors.Is(err, ledger.ErrAlreadyFinalized) {
		t.Fatalf("partial refund after a full refund: %v", err)
	}
	if bal(t, svc, cust.ID) != "5000" {
		t.Fatalf("cust = %s, want 5000", bal(t, svc, cust.ID))
	}
}

func TestIsZeroReadsUnresolvedAmounts(t *testing.T) {
	for m, want := range map[*ledger.Money]bool{
		{}:                                 true,
		ptr(ledger.NoFee):                  true,
		ptr(ledger.Amount("0.00", "USD")):  true,
		ptr(ledger.Amount("0.01", "USD")):  false,
		ptr(ledger.Minor(0, "USD")):        true,
		ptr(ledger.Minor(5, "USD")):        false,
		ptr(ledger.Amount("1,000", "NGN")): false,
	} {
		if got := m.IsZero(); got != want {
			t.Errorf("%+v.IsZero() = %v, want %v", *m, got, want)
		}
	}
}

func ptr(m ledger.Money) *ledger.Money { return &m }
