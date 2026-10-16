package ledger_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/ledgertest"
)

// Money must never move into a frozen account, whatever the path.
func TestFrozenReceiverIsRefusedOnEveryPath(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	w, ops := svc.Wallet(), svc.Ops()
	ada, shop := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, ada.ID, "50000", "NGN", "seed")

	// A hold paying a frozen account is refused up front.
	if err := ops.FreezeAccount(ctx, shop.ID, "review"); err != nil {
		t.Fatal(err)
	}
	_, err := w.Hold(ctx, ledger.HoldInput{AccountID: ada.ID, Destination: shop.ID, Amount: ledger.Amount("1000", "NGN"), Reference: "h1"})
	if !errors.Is(err, ledger.ErrAccountFrozen) {
		t.Fatalf("hold into frozen account: %v", err)
	}
	if err := ops.UnfreezeAccount(ctx, shop.ID); err != nil {
		t.Fatal(err)
	}

	// A pending transfer created before the freeze can't settle into it.
	p, err := w.Transfer(ctx, ledger.TransferInput{From: ada.ID, To: shop.ID, Amount: ledger.Amount("2000", "NGN"),
		Fee: ledger.NoFee, Reference: "p1", Pending: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := ops.FreezeAccount(ctx, shop.ID, "review"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Confirm(ctx, p.TransactionID, nil); !errors.Is(err, ledger.ErrAccountFrozen) {
		t.Fatalf("confirm into frozen account: %v", err)
	}
	// In a batch, only that item fails; the others still go through.
	q, err := w.Transfer(ctx, ledger.TransferInput{From: ada.ID, To: open(t, svc, "NGN").ID, Amount: ledger.Amount("300", "NGN"),
		Fee: ledger.NoFee, Reference: "p2", Pending: true})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.ConfirmMany(ctx, []ledger.ConfirmItem{{TransactionID: p.TransactionID}, {TransactionID: q.TransactionID}})
	if err != nil || len(res) != 2 || res[0].OK || !errors.Is(res[0].Err, ledger.ErrAccountFrozen) || !res[1].OK {
		t.Fatalf("confirm many with one frozen receiver: %+v %v", res, err)
	}
	// Cancelling still returns the money to its owner.
	if _, err := w.Cancel(ctx, p.TransactionID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got := bal(t, svc, shop.ID); got != "0" {
		t.Fatalf("frozen account received %s", got)
	}
}

// Settlement and suspense take house accounts only: a customer account there
// would be posted with overdraft allowed.
func TestBackOfficeRefusesCustomerAccounts(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	ops := svc.Ops()
	ada := open(t, svc, "NGN")
	cases := map[string]error{}
	_, cases["settlement clearing"] = ops.RecordSettlement(ctx, ledger.SettlementInput{ClearingAccount: string(ada.ID),
		Direction: ledger.SettlementPayout, Amount: ledger.Amount("4000", "NGN"), Reference: "s1"})
	_, cases["settlement bank"] = ops.RecordSettlement(ctx, ledger.SettlementInput{Provider: "paystack", BankAccount: string(ada.ID),
		Amount: ledger.Amount("4000", "NGN"), Reference: "s2"})
	_, cases["settlement fee"] = ops.RecordSettlement(ctx, ledger.SettlementInput{Provider: "paystack", FeeAccount: string(ada.ID),
		Amount: ledger.Amount("4000", "NGN"), Fee: ledger.Amount("10", ""), Reference: "s3"})
	_, cases["suspense hold"] = ops.HoldInSuspense(ctx, ledger.SuspenseInput{ClearingAccount: string(ada.ID),
		Amount: ledger.Amount("4000", "NGN"), Reference: "u1"})
	_, cases["suspense return"] = ops.ReturnFromSuspense(ctx, ledger.ReturnFromSuspenseInput{ClearingAccount: string(ada.ID),
		Amount: ledger.Amount("4000", "NGN"), Reference: "u2"})
	for name, err := range cases {
		if !errors.Is(err, ledger.ErrWrongKind) || !errors.Is(err, ledger.ErrInvalidInput) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := bal(t, svc, ada.ID); got != "0" {
		t.Fatalf("customer account touched: %s", got)
	}
}

// A full refund never takes a customer below zero, and works for a payment
// that carried a fee (stored by Blnk only as legs).
func TestFullRefund(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	w := svc.Wallet()
	ada, shop := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, ada.ID, "20000", "NGN", "seed")

	pay, err := w.Transfer(ctx, ledger.TransferInput{From: ada.ID, To: shop.ID, Amount: ledger.Amount("10000", "NGN"),
		Fee: ledger.Amount("50", ""), Reference: "pay"})
	if err != nil {
		t.Fatal(err)
	}
	// The shop spends part of it: a full refund would overdraw it.
	other := open(t, svc, "NGN")
	if _, err := w.Transfer(ctx, ledger.TransferInput{From: shop.ID, To: other.ID, Amount: ledger.Amount("4000", "NGN"),
		Fee: ledger.NoFee, Reference: "spend"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Refund(ctx, pay.TransactionID, ledger.RefundInput{Reason: "cancelled"}); !errors.Is(err, ledger.ErrInsufficientFunds) {
		t.Fatalf("refund that would overdraw: %v", err)
	}
	if got := bal(t, svc, shop.ID); got != "6000" {
		t.Fatalf("shop after refused refund: %s", got)
	}

	// Once the shop has the money again, the whole payment, fee included, comes back.
	fund(t, svc, shop.ID, "4000", "NGN", "topup")
	r, err := w.Refund(ctx, pay.TransactionID, ledger.RefundInput{Reason: "cancelled"})
	if err != nil {
		t.Fatalf("refund with fee: %v", err)
	}
	if len(r.Legs) != 2 || r.Amount.Value != "10050" || r.Legs[0].Amount.Value != "10000" {
		t.Fatalf("refund receipt: %s legs %+v", r.Amount.Value, r.Legs)
	}
	if got := bal(t, svc, ada.ID); got != "20000" {
		t.Fatalf("ada after refund: %s", got)
	}
	if got := bal(t, svc, shop.ID); got != "0" {
		t.Fatalf("shop after refund: %s", got)
	}
	if _, err := w.Refund(ctx, pay.TransactionID, ledger.RefundInput{}); !errors.Is(err, ledger.ErrAlreadyFinalized) {
		t.Fatalf("second refund: %v", err)
	}
}

// A refused escrow leaves no account behind, and a retry reuses the one it opened.
func TestOpenEscrowLeavesNothingBehind(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	w := svc.Wallet()
	ada, shop := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, ada.ID, "1000", "NGN", "seed")

	in := ledger.EscrowInput{Payer: ada.ID, Beneficiary: shop.ID, Amount: ledger.Amount("5000", "NGN"), Reference: "esc"}
	for i := 0; i < 2; i++ {
		if _, err := w.OpenEscrow(ctx, in); !errors.Is(err, ledger.ErrInsufficientFunds) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	escrows, err := w.FindAccounts(ctx, ledger.AccountQuery{Kind: ledger.EscrowAccount})
	if err != nil {
		t.Fatal(err)
	}
	if escrows.Total != 0 {
		t.Fatalf("refused escrows left %d accounts", escrows.Total)
	}
	fund(t, svc, ada.ID, "4000", "NGN", "seed2")
	e, err := w.OpenEscrow(ctx, in)
	if err != nil || e.Held.Value != "5000" {
		t.Fatalf("escrow: %+v %v", e, err)
	}
	again, err := w.OpenEscrow(ctx, in)
	if err != nil || again.ID != e.ID {
		t.Fatalf("retry: %+v %v", again, err)
	}
}

// Limit usage counts only limited movements, dates a scheduled transfer by
// when it posts, and never reports less than zero left.
func TestLimitUsage(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{Limits: []ledger.Limit{{Tier: "*", Currency: "NGN", Daily: "10000"}}})
	core.SetNow(time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	w, ops := svc.Wallet(), svc.Ops()
	ada, shop := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, ada.ID, "100000", "NGN", "seed")
	fund(t, svc, shop.ID, "100000", "NGN", "seed2")

	// An adjustment debit and a merchant refund are back-office flows.
	if _, err := ops.Adjust(ctx, ledger.AdjustInput{AccountID: ada.ID, Type: ledger.AdjustDebit, Amount: ledger.Amount("3000", "NGN"),
		Reason: "correction", RequestedBy: "a", ApprovedBy: "b", Reference: "adj"}); err != nil {
		t.Fatal(err)
	}
	pay, err := w.Transfer(ctx, ledger.TransferInput{From: ada.ID, To: shop.ID, Amount: ledger.Amount("2000", "NGN"), Fee: ledger.NoFee, Reference: "pay"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.RefundPartial(ctx, pay.TransactionID, ledger.PartialRefundInput{Amount: ledger.Amount("500", ""), Reference: "rf"}); err != nil {
		t.Fatal(err)
	}
	u, err := w.LimitUsage(ctx, ada.ID)
	if err != nil || u.DailyUsed.Value != "2000" {
		t.Fatalf("ada usage: %+v %v", u, err)
	}
	if u, err := w.LimitUsage(ctx, shop.ID); err != nil || u.DailyUsed.Value != "0" {
		t.Fatalf("shop usage after paying a refund: %+v %v", u, err)
	}

	// A transfer scheduled for tomorrow counts tomorrow, when it posts.
	if _, err := w.Transfer(ctx, ledger.TransferInput{From: ada.ID, To: shop.ID, Amount: ledger.Amount("4000", "NGN"), Fee: ledger.NoFee,
		Reference: "later", ScheduledFor: time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	core.Advance(24 * time.Hour)
	if u, err := w.LimitUsage(ctx, ada.ID); err != nil || u.DailyUsed.Value != "4000" {
		t.Fatalf("usage on the day the scheduled transfer posted: %+v %v", u, err)
	}

	// Bulk items aren't checked, so usage can pass the cap; what's left stops at zero.
	if _, err := w.Bulk(ctx, ledger.BulkTransferInput{Atomic: true, Items: []ledger.TransferInput{
		{From: ada.ID, To: shop.ID, Amount: ledger.Amount("9000", "NGN"), Fee: ledger.NoFee, Reference: "b1"},
	}}); err != nil {
		t.Fatal(err)
	}
	if u, err := w.LimitUsage(ctx, ada.ID); err != nil || u.DailyLeft == nil || u.DailyLeft.Value != "0" {
		t.Fatalf("left after going over: %+v %v", u, err)
	}
}

func TestAmountsAndMessages(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	if _, err := svc.Parse("", "NGN"); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("empty amount: %v", err)
	}
	if _, err := ledger.ParseDecimal(" ", "NGN", 100); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("empty decimal: %v", err)
	}
	if m, err := svc.Parse("1500.5", "NGN"); err != nil || m.Display != "1500.50" {
		t.Fatalf("parse display: %+v %v", m, err)
	}
	if m := ledger.Precise(bigInt("150050"), 100, "NGN"); m.Display != "1500.50" {
		t.Fatalf("precise display: %q", m.Display)
	}

	ada := open(t, svc, "NGN")
	_, err := svc.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: ada.ID, Amount: ledger.Amount("1.505", "NGN"), Fee: ledger.NoFee, Reference: "d", Provider: "paystack"})
	if err == nil || strings.Count(err.Error(), "invalid input") != 1 {
		t.Fatalf("message: %v", err)
	}
	ledgerID := ledger.LedgerID(ada.LedgerID)
	_, err = svc.Wallet().GetAccount(ctx, ledger.AccountID(ledgerID))
	if err == nil || strings.Contains(err.Error(), " a account") {
		t.Fatalf("article: %v", err)
	}
}

func TestExchangeQuoteDebitDisplay(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	usd, ngn := open(t, svc, "USD"), open(t, svc, "NGN")
	fund(t, svc, usd.ID, "300", "USD", "seed")
	q, err := svc.Wallet().QuoteExchange(ctx, ledger.ExchangeInput{From: usd.ID, To: ngn.ID, Sell: ledger.Amount("100", "USD"),
		Rate: "1500", Fee: ledger.Amount("1", ""), Reference: "fx"})
	if err != nil || q.Debit.Display != "101.00" {
		t.Fatalf("debit: %+v %v", q, err)
	}
}

// Retrying a plan after its first occurrence has run returns the plan.
func TestScheduleRetryAfterFirstRun(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	core.SetNow(time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	ada, pot := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, ada.ID, "100000", "NGN", "seed")
	in := ledger.RecurringInput{
		Transfer: ledger.TransferInput{From: ada.ID, To: pot.ID, Amount: ledger.Amount("1000", "NGN"), Fee: ledger.NoFee, Reference: "plan"},
		First:    time.Date(2026, 9, 2, 7, 0, 0, 0, time.UTC), Every: ledger.Monthly, Count: 3,
	}
	if _, err := svc.Wallet().Schedule(ctx, in); err != nil {
		t.Fatal(err)
	}
	core.Advance(48 * time.Hour) // the first occurrence has posted
	again, err := svc.Wallet().Schedule(ctx, in)
	if err != nil || len(again) != 3 {
		t.Fatalf("retry: %v %v", again, err)
	}
	for i, r := range again {
		if !r.Replayed {
			t.Fatalf("occurrence %d not a replay: %+v", i+1, r)
		}
	}
	if got := bal(t, svc, pot.ID); got != "1000" {
		t.Fatalf("pot: %s", got)
	}
}

// A caller's own txn_type can't make a transfer invisible to limits.
func TestTxnTypeCannotBypassLimits(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{Limits: []ledger.Limit{{Tier: "*", Currency: "NGN", Daily: "10000"}}})
	w := svc.Wallet()
	ada, shop := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, ada.ID, "50000", "NGN", "seed")
	for i, want := range []error{nil, ledger.ErrLimitExceeded} {
		_, err := w.Transfer(ctx, ledger.TransferInput{From: ada.ID, To: shop.ID, Amount: ledger.Amount("9000", "NGN"), Fee: ledger.NoFee,
			Reference: "t" + string(rune('a'+i)), MetaData: map[string]any{"txn_type": "refund"}})
		if !errors.Is(err, want) && !(want == nil && err == nil) {
			t.Fatalf("transfer %d: %v", i, err)
		}
	}
}

// A dispute ring-fences money for a payer who is frozen, and can return it.
func TestDisputeWithFrozenPayer(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	w, ops := svc.Wallet(), svc.Ops()
	ada, shop := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, ada.ID, "5000", "NGN", "seed")
	pay, err := w.Transfer(ctx, ledger.TransferInput{From: ada.ID, To: shop.ID, Amount: ledger.Amount("2000", "NGN"), Fee: ledger.NoFee, Reference: "pay"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ops.FreezeAccount(ctx, ada.ID, "fraud review"); err != nil {
		t.Fatal(err)
	}
	d, err := w.OpenDispute(ctx, ledger.DisputeInput{TransactionID: pay.TransactionID, Reference: "chargeback"})
	if err != nil {
		t.Fatalf("open dispute: %v", err)
	}
	if _, err := w.ResolveDispute(ctx, d.TransactionID, ledger.DisputeUpheld, nil); err != nil {
		t.Fatalf("resolve dispute: %v", err)
	}
	if got := bal(t, svc, ada.ID); got != "5000" {
		t.Fatalf("ada: %s", got)
	}
}

// A refund whose answer was lost (no mark written) is finished by a retry, and
// a single transaction already reversed is ErrAlreadyFinalized, not a funds error.
func TestRefundRetryAfterLostAnswer(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	w := svc.Wallet()
	c := core.Client()
	ada, shop := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, ada.ID, "20000", "NGN", "seed")

	pay, err := w.Transfer(ctx, ledger.TransferInput{From: ada.ID, To: shop.ID, Amount: ledger.Amount("10000", "NGN"),
		Fee: ledger.Amount("50", ""), Reference: "pay"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.GetTransaction(ctx, pay.TransactionID)
	if err != nil || len(got.Legs) != 2 {
		t.Fatalf("legs: %+v %v", got, err)
	}
	var payee string
	for _, l := range got.Legs {
		if l.Account == string(shop.ID) {
			payee = l.TransactionID
		}
	}
	// The payee leg was reversed but the caller never heard back.
	if _, err := c.Refund(ctx, payee, nil); err != nil {
		t.Fatal(err)
	}
	r, err := w.Refund(ctx, pay.TransactionID, ledger.RefundInput{})
	if err != nil || r.Amount.Value != "50" {
		t.Fatalf("retry finishes the fee leg: %+v %v", r, err)
	}
	if got := bal(t, svc, ada.ID); got != "20000" {
		t.Fatalf("ada: %s", got)
	}

	single, err := w.Transfer(ctx, ledger.TransferInput{From: ada.ID, To: shop.ID, Amount: ledger.Amount("3000", "NGN"), Fee: ledger.NoFee, Reference: "single"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Refund(ctx, single.TransactionID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Refund(ctx, single.TransactionID, ledger.RefundInput{}); !errors.Is(err, ledger.ErrAlreadyFinalized) {
		t.Fatalf("single already reversed: %v", err)
	}
}

// A full refund doesn't count against the limit of the account paying it back.
func TestFullRefundNotCountedForPayer(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{Limits: []ledger.Limit{{Tier: "*", Currency: "NGN", Daily: "10000"}}})
	w := svc.Wallet()
	ada, shop := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, ada.ID, "20000", "NGN", "seed")
	pay, err := w.Transfer(ctx, ledger.TransferInput{From: ada.ID, To: shop.ID, Amount: ledger.Amount("8000", "NGN"), Fee: ledger.NoFee, Reference: "pay"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Refund(ctx, pay.TransactionID, ledger.RefundInput{}); err != nil {
		t.Fatal(err)
	}
	if u, err := w.LimitUsage(ctx, shop.ID); err != nil || u.DailyUsed.Value != "0" {
		t.Fatalf("shop usage: %+v %v", u, err)
	}
}

// An escrow account left by a failed funding is reused only for the same escrow.
func TestEscrowReuseMustMatch(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	w := svc.Wallet()
	ada, bob, carol := open(t, svc, "NGN"), open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, ada.ID, "10000", "NGN", "seed")
	// What a funding that failed after the account was opened leaves behind.
	left, err := core.Client().CreateBalance(ctx, blnk.CreateBalanceRequest{LedgerID: string(ada.LedgerID), Currency: "NGN",
		MetaData: blnk.MetaData{"account_kind": "escrow", "status": "active", "escrow_payer": string(ada.ID),
			"escrow_beneficiary": string(bob.ID), "escrow_reference": "esc", "escrow_amount": "500000"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.OpenEscrow(ctx, ledger.EscrowInput{Payer: ada.ID, Beneficiary: carol.ID, Amount: ledger.Amount("5000", "NGN"), Reference: "esc"}); !errors.Is(err, ledger.ErrDuplicateReference) {
		t.Fatalf("different beneficiary: %v", err)
	}
	if _, err := w.OpenEscrow(ctx, ledger.EscrowInput{Payer: ada.ID, Beneficiary: bob.ID, Amount: ledger.Amount("3000", "NGN"), Reference: "esc"}); !errors.Is(err, ledger.ErrDuplicateReference) {
		t.Fatalf("different amount: %v", err)
	}
	e, err := w.OpenEscrow(ctx, ledger.EscrowInput{Payer: ada.ID, Beneficiary: bob.ID, Amount: ledger.Amount("5000", "NGN"), Reference: "esc"})
	if err != nil || string(e.ID) != left.BalanceID {
		t.Fatalf("same escrow reuses the account: %+v %v", e, err)
	}
}

// A plan replays only its own occurrences.
func TestScheduleReplayMustMatch(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	core.SetNow(time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	w := svc.Wallet()
	ada, pot, shop := open(t, svc, "NGN"), open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, ada.ID, "100000", "NGN", "seed")
	if _, err := w.Transfer(ctx, ledger.TransferInput{From: ada.ID, To: shop.ID, Amount: ledger.Amount("500", "NGN"), Fee: ledger.NoFee, Reference: "rent_1"}); err != nil {
		t.Fatal(err)
	}
	plan := func(ref, amount string) ledger.RecurringInput {
		return ledger.RecurringInput{
			Transfer: ledger.TransferInput{From: ada.ID, To: pot.ID, Amount: ledger.Amount(amount, "NGN"), Fee: ledger.NoFee, Reference: ref},
			First:    time.Date(2026, 9, 2, 7, 0, 0, 0, time.UTC), Every: ledger.Monthly, Count: 2,
		}
	}
	if _, err := w.Schedule(ctx, plan("rent", "1000")); !errors.Is(err, ledger.ErrDuplicateReference) {
		t.Fatalf("unrelated rent_1: %v", err)
	}
	if _, err := w.Schedule(ctx, plan("save", "1000")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Schedule(ctx, plan("save", "2000")); !errors.Is(err, ledger.ErrDuplicateReference) {
		t.Fatalf("changed amount: %v", err)
	}
}

func TestWrongKindArticle(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	ada := open(t, svc, "NGN")
	_, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN", LedgerID: ledger.LedgerID(ada.ID)})
	if !errors.Is(err, ledger.ErrWrongKind) || strings.Contains(err.Error(), " a account") || strings.Count(err.Error(), "invalid input") > 0 {
		t.Fatalf("message: %v", err)
	}
}
