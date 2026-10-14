package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/ledgertest"
)

// These tests run the real Service against the in-memory Blnk emulator.

var ctx = context.Background()

func open(t *testing.T, svc *ledger.Service, cur string) *ledger.Account {
	t.Helper()
	a, err := svc.OpenAccount(ctx, ledger.OpenAccountInput{Currency: cur})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func fund(t *testing.T, svc *ledger.Service, id, amount, cur, ref string) {
	t.Helper()
	if _, err := svc.Deposit(ctx, ledger.DepositInput{AccountID: id, Amount: ledger.Amount(amount, cur), Reference: ref, Provider: "paystack"}); err != nil {
		t.Fatal(err)
	}
}

func bal(t *testing.T, svc *ledger.Service, id string) string {
	t.Helper()
	a, err := svc.GetAccount(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return a.Balance.Value
}

func sys(t *testing.T, svc *ledger.Service, ind, cur string) string {
	t.Helper()
	a, err := svc.GetSystemAccount(ctx, ind, cur)
	if err != nil {
		t.Fatal(err)
	}
	return a.Balance.Value
}

func TestEmulatorCoreFlows(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	alice, bob := open(t, svc, "NGN"), open(t, svc, "NGN")

	// Deposit with fee: Dr @PaystackClearing 10,000 / Cr @FeeIncome 150 / Cr alice 9,850.
	if _, err := svc.Deposit(ctx, ledger.DepositInput{AccountID: alice.ID, Amount: ledger.Amount("10000", "NGN"),
		Fee: ledger.Amount("150", ""), Reference: "dep1", Provider: "paystack"}); err != nil {
		t.Fatal(err)
	}
	if got := bal(t, svc, alice.ID); got != "9850" {
		t.Fatalf("alice after deposit = %s", got)
	}
	if got := sys(t, svc, "@PaystackClearing", "NGN"); got != "-10000" {
		t.Fatalf("clearing = %s", got)
	}

	// Replay is idempotent.
	r, err := svc.Deposit(ctx, ledger.DepositInput{AccountID: alice.ID, Amount: ledger.Amount("10000", "NGN"),
		Fee: ledger.Amount("150", ""), Reference: "dep1", Provider: "paystack"})
	if err != nil || !r.Replayed || bal(t, svc, alice.ID) != "9850" {
		t.Fatalf("replay: %+v %v", r, err)
	}

	// Transfer with fee, then overspend.
	if _, err := svc.Transfer(ctx, ledger.TransferInput{From: alice.ID, To: bob.ID, Amount: ledger.Amount("2000", "NGN"),
		Fee: ledger.Amount("10", ""), Reference: "p2p1"}); err != nil {
		t.Fatal(err)
	}
	if bal(t, svc, alice.ID) != "7840" || bal(t, svc, bob.ID) != "2000" {
		t.Fatalf("after transfer alice=%s bob=%s", bal(t, svc, alice.ID), bal(t, svc, bob.ID))
	}
	if _, err := svc.Transfer(ctx, ledger.TransferInput{From: bob.ID, To: alice.ID, Amount: ledger.Amount("2000.01", "NGN"), Reference: "x"}); !errors.Is(err, ledger.ErrInsufficientFunds) {
		t.Fatalf("want insufficient funds, got %v", err)
	}

	// Withdraw is pending until confirmed; Available drops at once.
	wd, err := svc.Withdraw(ctx, ledger.WithdrawInput{AccountID: alice.ID, Amount: ledger.Amount("1000", "NGN"), Reference: "wd1", Provider: "flutterwave"})
	if err != nil || !wd.Pending() {
		t.Fatalf("withdraw: %+v %v", wd, err)
	}
	a, _ := svc.GetAccount(ctx, alice.ID)
	if a.Balance.Value != "7840" || a.Available.Value != "6840" {
		t.Fatalf("pending withdrawal: balance=%s available=%s", a.Balance.Value, a.Available.Value)
	}
	if _, err := svc.Confirm(ctx, wd.TransactionID, nil); err != nil {
		t.Fatal(err)
	}
	if bal(t, svc, alice.ID) != "6840" || sys(t, svc, "@FlutterwaveClearing", "NGN") != "1000" {
		t.Fatalf("after confirm alice=%s", bal(t, svc, alice.ID))
	}

	// Hold, partial confirm, cancel the rest.
	h, err := svc.Hold(ctx, ledger.HoldInput{AccountID: bob.ID, Destination: ledger.MerchantPayableAccount, Amount: ledger.Amount("1000", "NGN"), Reference: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	part := ledger.Amount("600", "")
	if _, err := svc.Confirm(ctx, h.TransactionID, &part); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cancel(ctx, h.TransactionID); err != nil {
		t.Fatal(err)
	}
	b, _ := svc.GetAccount(ctx, bob.ID)
	if b.Balance.Value != "1400" || b.Available.Value != "1400" {
		t.Fatalf("after hold bob balance=%s available=%s", b.Balance.Value, b.Available.Value)
	}
	if _, err := svc.Cancel(ctx, h.TransactionID); !errors.Is(err, ledger.ErrAlreadyFinalized) {
		t.Fatalf("second cancel: %v", err)
	}

	// Withdrawal hold expires after 24h.
	wd2, _ := svc.Withdraw(ctx, ledger.WithdrawInput{AccountID: alice.ID, Amount: ledger.Amount("100", "NGN"), Reference: "wd2"})
	core.Advance(25 * time.Hour)
	got, _ := svc.GetTransaction(ctx, wd2.TransactionID)
	a, _ = svc.GetAccount(ctx, alice.ID)
	if got.Status != "INFLIGHT" || a.Available.Value != "6840" {
		t.Fatalf("expired hold not released: %s available=%s", got.Status, a.Available.Value)
	}

	// History sees the account's movements.
	page, err := svc.History(ctx, alice.ID, ledger.HistoryQuery{Limit: 50})
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("history: %+v %v", page, err)
	}
}

func TestFeeRules(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	for _, c := range []struct {
		rule   ledger.FeeRule
		amount string
		want   string
	}{
		{ledger.FeeRule{Percent: "1.5", Min: "10", Max: "2000"}, "100", "10"},        // floor
		{ledger.FeeRule{Percent: "1.5", Min: "10", Max: "2000"}, "10000", "150"},     // percent
		{ledger.FeeRule{Percent: "1.5", Min: "10", Max: "2000"}, "1000000", "2000"},  // cap
		{ledger.FeeRule{Percent: "1.5"}, "100.33", "1.5"},                            // 1.50495 → half-up 1.50
		{ledger.FeeRule{Percent: "1.5", Rounding: ledger.RoundUp}, "100.33", "1.51"}, // up
		{ledger.FeeRule{Percent: "1", Fixed: "100"}, "5000", "150"},
		{ledger.FeeRule{Tiers: []ledger.FeeTier{
			{UpTo: "5000", Rule: ledger.FeeRule{Fixed: "10"}},
			{UpTo: "50000", Rule: ledger.FeeRule{Fixed: "25"}},
			{Rule: ledger.FeeRule{Percent: "0.1", Max: "500"}},
		}}, "50000", "25"},
		{ledger.FeeRule{Tiers: []ledger.FeeTier{
			{UpTo: "5000", Rule: ledger.FeeRule{Fixed: "10"}},
			{Rule: ledger.FeeRule{Percent: "0.1", Max: "500"}},
		}}, "100000", "100"},
	} {
		got, err := svc.ComputeFee(c.rule, ledger.Amount(c.amount, "NGN"))
		if err != nil || got.Value != c.want {
			t.Errorf("%+v on %s = %s (%v), want %s", c.rule, c.amount, got.Value, err, c.want)
		}
	}
}

func TestSplitMarketplacePayment(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	seller, rider := open(t, svc, "NGN"), open(t, svc, "NGN")
	r, err := svc.Split(ctx, ledger.SplitInput{
		From: ledger.ClearingAccount("paystack"), AllowOverdraft: true,
		Amount: ledger.Amount("10000.01", "NGN"), Reference: "order-1",
		Parts: []ledger.SplitPart{
			{To: rider.ID, Amount: ledger.Amount("500", "")},
			{To: ledger.FeeIncomeAccount, Percent: "10"},
			{To: seller.ID, Rest: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Legs) != 3 || bal(t, svc, rider.ID) != "500" || sys(t, svc, "@FeeIncome", "NGN") != "1000" || bal(t, svc, seller.ID) != "8500.01" {
		t.Fatalf("split: legs=%d rider=%s fee=%s seller=%s", len(r.Legs), bal(t, svc, rider.ID), sys(t, svc, "@FeeIncome", "NGN"), bal(t, svc, seller.ID))
	}
	if _, err := svc.Split(ctx, ledger.SplitInput{From: seller.ID, Amount: ledger.Amount("100", "NGN"), Reference: "bad",
		Parts: []ledger.SplitPart{{To: rider.ID, Amount: ledger.Amount("60", "")}, {To: rider.ID, Amount: ledger.Amount("30", "")}}}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("parts not adding up must be rejected: %v", err)
	}
}

func TestExchange(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	usd, ngn := open(t, svc, "USD"), open(t, svc, "NGN")
	fund(t, svc, usd.ID, "100", "USD", "in")

	x, err := svc.Exchange(ctx, ledger.ExchangeInput{From: usd.ID, To: ngn.ID, Sell: ledger.Amount("10.01", "USD"),
		Rate: "1550.255", Fee: ledger.Amount("0.10", ""), Reference: "fx1"})
	if err != nil {
		t.Fatal(err)
	}
	// 10.01 × 1550.255 = 15518.05255 → rounds down to 15518.05
	if x.Bought.Value != "15518.05" || bal(t, svc, ngn.ID) != "15518.05" || bal(t, svc, usd.ID) != "89.89" {
		t.Fatalf("fx: bought=%s ngn=%s usd=%s", x.Bought.Value, bal(t, svc, ngn.ID), bal(t, svc, usd.ID))
	}
	if sys(t, svc, "@FXPool", "USD") != "10.01" || sys(t, svc, "@FXPool", "NGN") != "-15518.05" || sys(t, svc, "@FeeIncome", "USD") != "0.1" {
		t.Fatal("pool/fee positions wrong")
	}
	again, err := svc.Exchange(ctx, ledger.ExchangeInput{From: usd.ID, To: ngn.ID, Sell: ledger.Amount("10.01", "USD"),
		Rate: "1550.255", Fee: ledger.Amount("0.10", ""), Reference: "fx1"})
	if err != nil || !again.Replayed || bal(t, svc, usd.ID) != "89.89" {
		t.Fatalf("fx replay: %+v %v", again, err)
	}

	// Buy an exact amount: sell rounds up.
	y, err := svc.Exchange(ctx, ledger.ExchangeInput{From: usd.ID, To: ngn.ID, Buy: ledger.Amount("1000", "NGN"), Rate: "1550", Reference: "fx2"})
	if err != nil || y.Sold.Value != "0.65" || y.Bought.Value != "1000" {
		t.Fatalf("buy exact: %+v %v", y, err)
	}

	// Atomic: an overspend leaves nothing behind.
	if _, err := svc.Exchange(ctx, ledger.ExchangeInput{From: usd.ID, To: ngn.ID, Sell: ledger.Amount("1000", "USD"), Rate: "1550", Reference: "fx3"}); !errors.Is(err, ledger.ErrInsufficientFunds) {
		t.Fatalf("want insufficient funds, got %v", err)
	}
	if _, err := svc.GetTransactionByReference(ctx, "fx3_buy"); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("failed exchange left a leg behind: %v", err)
	}
}

func TestEscrow(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	buyer, seller := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, buyer.ID, "5000", "NGN", "in")

	e, err := svc.OpenEscrow(ctx, ledger.EscrowInput{Payer: buyer.ID, Beneficiary: seller.ID, Amount: ledger.Amount("3000", "NGN"), Reference: "esc1"})
	if err != nil || e.Held.Value != "3000" || e.Status != "held" || bal(t, svc, buyer.ID) != "2000" {
		t.Fatalf("open: %+v %v", e, err)
	}
	again, err := svc.OpenEscrow(ctx, ledger.EscrowInput{Payer: buyer.ID, Beneficiary: seller.ID, Amount: ledger.Amount("3000", "NGN"), Reference: "esc1"})
	if err != nil || again.ID != e.ID || bal(t, svc, buyer.ID) != "2000" {
		t.Fatalf("retry must return the same escrow: %+v %v", again, err)
	}
	if _, err := svc.ReleaseEscrow(ctx, e.ID, ledger.EscrowSettleInput{Amount: ledger.Amount("1000", ""), Reference: "rel1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RefundEscrow(ctx, e.ID, ledger.EscrowSettleInput{Reference: "ref1"}); err != nil {
		t.Fatal(err)
	}
	e, _ = svc.GetEscrow(ctx, e.ID)
	if e.Status != "settled" || bal(t, svc, seller.ID) != "1000" || bal(t, svc, buyer.ID) != "4000" || e.Account.Kind != ledger.EscrowAccount {
		t.Fatalf("after settle: %+v seller=%s buyer=%s", e, bal(t, svc, seller.ID), bal(t, svc, buyer.ID))
	}
	if _, err := svc.ReleaseEscrow(ctx, e.ID, ledger.EscrowSettleInput{Amount: ledger.Amount("1", ""), Reference: "rel2"}); !errors.Is(err, ledger.ErrInsufficientFunds) {
		t.Fatalf("over-release must fail: %v", err)
	}
}

func TestDisputeAndPartialRefund(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	cust, merchant := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, cust.ID, "5000", "NGN", "in")
	pay, _ := svc.Transfer(ctx, ledger.TransferInput{From: cust.ID, To: merchant.ID, Amount: ledger.Amount("3000", "NGN"), Reference: "pay1"})

	d, err := svc.OpenDispute(ctx, ledger.DisputeInput{TransactionID: pay.TransactionID, Amount: ledger.Amount("1000", ""), Reference: "dsp1", Reason: "not delivered"})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := svc.GetAccount(ctx, merchant.ID)
	if m.Available.Value != "2000" {
		t.Fatalf("disputed funds must be held: %s", m.Available.Value)
	}
	if _, err := svc.ResolveDispute(ctx, d.TransactionID, ledger.DisputeUpheld, nil); err != nil {
		t.Fatal(err)
	}
	if bal(t, svc, cust.ID) != "3000" || bal(t, svc, merchant.ID) != "2000" {
		t.Fatalf("after dispute cust=%s merchant=%s", bal(t, svc, cust.ID), bal(t, svc, merchant.ID))
	}

	if _, err := svc.RefundPartial(ctx, pay.TransactionID, ledger.PartialRefundInput{Amount: ledger.Amount("1500", ""), Reference: "pr1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RefundPartial(ctx, pay.TransactionID, ledger.PartialRefundInput{Amount: ledger.Amount("1500.01", ""), Reference: "pr2"}); !errors.Is(err, ledger.ErrAmountExceeded) {
		t.Fatalf("over-refund must fail: %v", err)
	}
	if bal(t, svc, cust.ID) != "4500" {
		t.Fatalf("after partial refund cust=%s", bal(t, svc, cust.ID))
	}
}

func TestRecurringCloseSuspenseAdjustSettlement(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{EnforceAccountStatus: true})
	main, savings := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, main.ID, "1000", "NGN", "in")

	plan, err := svc.ScheduleRecurring(ctx, ledger.RecurringInput{
		Transfer: ledger.TransferInput{From: main.ID, To: savings.ID, Amount: ledger.Amount("100", "NGN"), Reference: "save"},
		First:    core.Now().Add(24 * time.Hour), Every: ledger.Monthly, Count: 3,
	})
	if err != nil || len(plan) != 3 || plan[2].EntryStatus != ledger.EntryScheduled {
		t.Fatalf("schedule: %v %v", plan, err)
	}
	core.Advance(40 * 24 * time.Hour) // two occurrences due
	if bal(t, svc, savings.ID) != "200" {
		t.Fatalf("savings = %s", bal(t, svc, savings.ID))
	}

	// Suspense: park, then allocate.
	if _, err := svc.ParkInSuspense(ctx, ledger.SuspenseInput{Amount: ledger.Amount("750", "NGN"), Provider: "paystack", Reference: "unk1", Note: "no narration"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AllocateSuspense(ctx, ledger.AllocateInput{AccountID: savings.ID, Amount: ledger.Amount("750", "NGN"), Reference: "alloc1", SuspenseReference: "unk1"}); err != nil {
		t.Fatal(err)
	}
	if sys(t, svc, "@Suspense", "NGN") != "0" || bal(t, svc, savings.ID) != "950" {
		t.Fatal("suspense allocation wrong")
	}

	// Adjustments need an approver other than the requester.
	if _, err := svc.Adjust(ctx, ledger.AdjustInput{AccountID: main.ID, Type: ledger.AdjustCredit, Amount: ledger.Amount("5", "NGN"),
		Reason: "goodwill", RequestedBy: "ops1", ApprovedBy: "ops1", Reference: "adj1"}); !errors.Is(err, ledger.ErrApprovalRequired) {
		t.Fatalf("self-approval must fail: %v", err)
	}
	if _, err := svc.Adjust(ctx, ledger.AdjustInput{AccountID: main.ID, Type: ledger.AdjustCredit, Amount: ledger.Amount("5", "NGN"),
		Reason: "goodwill", RequestedBy: "ops1", ApprovedBy: "lead1", Reference: "adj1"}); err != nil {
		t.Fatal(err)
	}

	// Settlement clears the provider receivable (1000 deposit + 750 suspense).
	st, err := svc.RecordSettlement(ctx, ledger.SettlementInput{Provider: "paystack", Amount: ledger.Amount("1750", "NGN"), Fee: ledger.Amount("25", ""), Reference: "stl1"})
	if err != nil || len(st.Entries) != 2 {
		t.Fatalf("settlement: %+v %v", st, err)
	}
	if sys(t, svc, "@PaystackClearing", "NGN") != "0" || sys(t, svc, "@BankOperating", "NGN") != "-1725" || sys(t, svc, "@ProviderFees", "NGN") != "-25" {
		t.Fatalf("settlement balances: clearing=%s bank=%s fees=%s", sys(t, svc, "@PaystackClearing", "NGN"), sys(t, svc, "@BankOperating", "NGN"), sys(t, svc, "@ProviderFees", "NGN"))
	}

	// Close: pending activity blocks, then sweep and close.
	h, _ := svc.Hold(ctx, ledger.HoldInput{AccountID: savings.ID, Destination: main.ID, Amount: ledger.Amount("1", "NGN"), Reference: "h"})
	if _, err := svc.CloseAccount(ctx, savings.ID, ledger.CloseInput{SweepTo: main.ID, Reference: "close1"}); !errors.Is(err, ledger.ErrPendingActivity) {
		t.Fatalf("want pending activity, got %v", err)
	}
	res, err := svc.CancelMany(ctx, []string{h.TransactionID, "txn_missing"})
	if err != nil || !res[0].OK || res[1].OK || !errors.Is(res[1].Err, ledger.ErrNotFound) {
		t.Fatalf("cancel many: %+v %v", res, err)
	}
	closed, err := svc.CloseAccount(ctx, savings.ID, ledger.CloseInput{SweepTo: main.ID, Reference: "close1", Reason: "customer request"})
	if err != nil || closed.Status != "closed" || closed.Balance.Value != "0" {
		t.Fatalf("close: %+v %v", closed, err)
	}
	if _, err := svc.Transfer(ctx, ledger.TransferInput{From: main.ID, To: savings.ID, Amount: ledger.Amount("1", "NGN"), Reference: "late"}); !errors.Is(err, ledger.ErrAccountClosed) {
		t.Fatalf("closed account must refuse money: %v", err)
	}
}

func TestCustomersPotsAndFind(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	in := ledger.CustomerInput{FirstName: "Ada", LastName: "Obi", Email: "ada@example.com",
		Currencies: []string{"NGN", "usd"}, ExternalID: "user-42"}
	c, err := svc.OpenCustomer(ctx, in)
	if err != nil || len(c.Accounts) != 2 || c.Accounts["USD"] == nil || c.Identity.FirstName != "Ada" {
		t.Fatalf("open customer: %+v %v", c, err)
	}
	// Retrying (e.g. after a timeout) reuses everything and adds new currencies.
	in.Currencies = []string{"NGN", "USD", "USDT"}
	again, err := svc.OpenCustomer(ctx, in)
	if err != nil || again.ID != c.ID || again.Accounts["NGN"].ID != c.Accounts["NGN"].ID || again.Accounts["USDT"] == nil {
		t.Fatalf("retry: %+v %v", again, err)
	}

	ngn := c.Accounts["NGN"]
	rent, err := svc.OpenPot(ctx, ledger.PotInput{ParentID: ngn.ID, Name: "Rent", Goal: ledger.Amount("500000", "")})
	if err != nil || rent.Kind != ledger.PotAccount || rent.Goal.Value != "500000" || rent.IdentityID != c.ID {
		t.Fatalf("pot: %+v %v", rent, err)
	}
	if dup, _ := svc.OpenPot(ctx, ledger.PotInput{ParentID: ngn.ID, Name: "Rent"}); dup.ID != rent.ID {
		t.Fatal("same pot name must return the same pot")
	}
	_, _ = svc.OpenPot(ctx, ledger.PotInput{ParentID: ngn.ID, Name: "Holiday"})
	pots, err := svc.ListPots(ctx, ngn.ID)
	if err != nil || len(pots) != 2 {
		t.Fatalf("list pots: %v %v", pots, err)
	}
	// GetCustomer shows main accounts only.
	if got, _ := svc.GetCustomer(ctx, c.ID); got.Accounts["NGN"].ID != ngn.ID || len(got.Accounts) != 3 {
		t.Fatalf("get customer: %+v", got.Accounts)
	}
	page, err := svc.FindAccounts(ctx, ledger.AccountQuery{IdentityID: c.ID, Currency: "NGN"})
	if err != nil || page.Total != 3 { // main + two pots
		t.Fatalf("find: %+v %v", page, err)
	}
	if page, _ := svc.FindAccounts(ctx, ledger.AccountQuery{IdentityID: c.ID, Kind: ledger.CustomerAccount}); len(page.Items) != 3 {
		t.Fatalf("find customer kind: %d", len(page.Items))
	}
}

func TestLimits(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{Limits: []ledger.Limit{
		{Tier: "tier1", Currency: "NGN", PerTransaction: "5000", Daily: "8000"},
	}})
	a, _ := svc.OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN", MetaData: map[string]any{"tier": "tier1"}})
	free := open(t, svc, "NGN") // no tier, no limit
	fund(t, svc, a.ID, "50000", "NGN", "in")

	if _, err := svc.Transfer(ctx, ledger.TransferInput{From: a.ID, To: free.ID, Amount: ledger.Amount("5000.01", "NGN"), Reference: "big"}); !errors.Is(err, ledger.ErrLimitExceeded) {
		t.Fatalf("per-transaction: %v", err)
	}
	if _, err := svc.Transfer(ctx, ledger.TransferInput{From: a.ID, To: free.ID, Amount: ledger.Amount("5000", "NGN"), Reference: "t1"}); err != nil {
		t.Fatal(err)
	}
	// A hold counts while held; cancelling it gives the allowance back.
	h, err := svc.Hold(ctx, ledger.HoldInput{AccountID: a.ID, Destination: free.ID, Amount: ledger.Amount("3000", "NGN"), Reference: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Withdraw(ctx, ledger.WithdrawInput{AccountID: a.ID, Amount: ledger.Amount("1", "NGN"), Reference: "w1"}); !errors.Is(err, ledger.ErrLimitExceeded) {
		t.Fatalf("daily: %v", err)
	}
	if _, err := svc.Cancel(ctx, h.TransactionID); err != nil {
		t.Fatal(err)
	}
	u, err := svc.LimitUsage(ctx, a.ID)
	if err != nil || u.DailyUsed.Value != "5000" || u.DailyLeft.Value != "3000" {
		t.Fatalf("usage: %+v %v", u, err)
	}
	// A partially confirmed hold counts once.
	h2, _ := svc.Hold(ctx, ledger.HoldInput{AccountID: a.ID, Destination: free.ID, Amount: ledger.Amount("2000", "NGN"), Reference: "h2"})
	part := ledger.Amount("500", "")
	_, _ = svc.Confirm(ctx, h2.TransactionID, &part)
	if u, _ := svc.LimitUsage(ctx, a.ID); u.DailyUsed.Value != "7000" {
		t.Fatalf("partial confirm double counted: %s", u.DailyUsed.Value)
	}
	// Next day the daily allowance resets.
	core.Advance(24 * time.Hour)
	if _, err := svc.Transfer(ctx, ledger.TransferInput{From: a.ID, To: free.ID, Amount: ledger.Amount("4000", "NGN"), Reference: "t2"}); err != nil {
		t.Fatalf("next day: %v", err)
	}
	// Untiered accounts aren't limited.
	if _, err := svc.Transfer(ctx, ledger.TransferInput{From: free.ID, To: a.ID, Amount: ledger.Amount("6000", "NGN"), Reference: "t3"}); err != nil {
		t.Fatal(err)
	}
}

func TestStatementAndBalanceAt(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	a, b := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, a.ID, "1000", "NGN", "in1") // before the period
	core.Advance(time.Hour)
	from := core.Now()
	core.Advance(time.Minute)
	fund(t, svc, a.ID, "500", "NGN", "in2")
	core.Advance(time.Minute)
	_, _ = svc.Transfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("300", "NGN"), Fee: ledger.Amount("5", ""), Reference: "t1"})
	core.Advance(time.Minute)
	_, _ = svc.Hold(ctx, ledger.HoldInput{AccountID: a.ID, Destination: b.ID, Amount: ledger.Amount("100", "NGN"), Reference: "h1"}) // pending: not on the statement
	core.Advance(time.Minute)
	to := core.Now()
	core.Advance(time.Minute)
	fund(t, svc, a.ID, "1", "NGN", "in3") // after the period

	st, err := svc.Statement(ctx, a.ID, from, to)
	if err != nil {
		t.Fatal(err)
	}
	// The fee is its own leg in Blnk, so it is its own line (as on a bank statement).
	if st.Opening.Value != "1000" || st.Closing.Value != "1195" || len(st.Lines) != 3 ||
		st.Lines[0].Credit.Value != "500" || st.Lines[1].Debit.Value != "300" ||
		st.Lines[2].Debit.Value != "5" || st.Lines[2].Counterpart != "@FeeIncome" || st.Lines[2].Balance.Value != "1195" {
		t.Fatalf("statement: opening=%s closing=%s lines=%+v", st.Opening.Value, st.Closing.Value, st.Lines)
	}
	var csv strings.Builder
	if err := st.WriteCSV(&csv); err != nil || !strings.Contains(csv.String(), "Opening balance,,,,1000.00,NGN") {
		t.Fatalf("csv: %v\n%s", err, csv.String())
	}
	if m, err := svc.BalanceAt(ctx, a.ID, from); err != nil || m.Value != "1000" {
		t.Fatalf("balance at: %v %v", m, err)
	}
}

func TestQuotes(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{CoreDryRun: true})
	a, b := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, a.ID, "1000", "NGN", "in")
	fee, _ := svc.ComputeFee(ledger.FeeRule{Percent: "1"}, ledger.Amount("500", "NGN"))

	q, err := svc.QuoteTransfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("500", "NGN"), Fee: fee, Reference: "q1"})
	if err != nil || !q.Allowed || !q.CheckedByLedger || q.Debit.Value != "505" || q.Fee.Value != "5" || q.AvailableAfter.Value != "495" {
		t.Fatalf("quote: %+v %v", q, err)
	}
	if bal(t, svc, a.ID) != "1000" {
		t.Fatal("a quote must not move money")
	}
	// The reference is still free afterwards.
	if _, err := svc.Transfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("500", "NGN"), Fee: fee, Reference: "q1"}); err != nil {
		t.Fatal(err)
	}
	q, err = svc.QuoteWithdraw(ctx, ledger.WithdrawInput{AccountID: a.ID, Amount: ledger.Amount("496", "NGN"), Reference: "q2"})
	if err != nil || q.Allowed || !errors.Is(q.Reason, ledger.ErrInsufficientFunds) {
		t.Fatalf("over-limit quote: %+v %v", q, err)
	}
	q, err = svc.QuoteDeposit(ctx, ledger.DepositInput{AccountID: a.ID, Amount: ledger.Amount("100", "NGN"), Fee: ledger.Amount("1.5", ""), Reference: "q3"})
	if err != nil || q.Credit.Value != "98.5" || q.AvailableAfter.Value != "593.5" {
		t.Fatalf("deposit quote: %+v %v", q, err)
	}
}

func TestWatchAndWebhooks(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	a := open(t, svc, "NGN")
	fund(t, svc, a.ID, "1000", "NGN", "in")
	w, err := svc.WatchBalance(ctx, ledger.WatchInput{AccountID: a.ID, Operator: "<", Threshold: ledger.Amount("100.50", "")})
	if err != nil || w.Threshold.PreciseAmount.Int64() != 10050 {
		t.Fatalf("watch: %+v %v", w, err)
	}
	if _, err := svc.Withdraw(ctx, ledger.WithdrawInput{AccountID: a.ID, Amount: ledger.Amount("950", "NGN"), Reference: "w1", Immediate: true}); err != nil {
		t.Fatal(err)
	}

	var got []*ledger.Event
	h := svc.WebhookHandler(ledger.WebhookConfig{Secret: "s3cret"}, func(_ context.Context, e *ledger.Event) error {
		got = append(got, e)
		return nil
	})
	for _, e := range core.Events() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, core.WebhookRequest(e, "s3cret"))
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", e.Event, rec.Code, rec.Body)
		}
	}
	var alert, applied bool
	for _, e := range got {
		switch e.Name {
		case ledger.EventBalanceAlert:
			alert = e.Alert.AccountID == a.ID && e.Alert.Threshold.PreciseAmount.Int64() == 10050
		case ledger.EventTransactionApplied:
			applied = applied || (e.Receipt.Reference == "w1" && e.Receipt.Amount.Value == "950")
		}
	}
	if !alert || !applied {
		t.Fatalf("events: alert=%v applied=%v (%d events)", alert, applied, len(got))
	}

	// Wrong secret and replayed (stale) deliveries are refused.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, core.WebhookRequest(core.Events()[0], "wrong"))
	if rec.Code != 401 {
		t.Fatalf("bad signature: %d", rec.Code)
	}
	old := core.WebhookRequest(core.Events()[0], "s3cret")
	core.Advance(time.Hour)
	if _, err := svc.ParseWebhook(old, ledger.WebhookConfig{Secret: "s3cret"}); !errors.Is(err, ledger.ErrSignature) {
		t.Fatalf("stale delivery accepted: %v", err)
	}
	if err := svc.Unwatch(ctx, w.ID); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileTrialBalanceAndInstrument(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	a := open(t, svc, "NGN")
	fund(t, svc, a.ID, "1000", "NGN", "ps_1")
	fund(t, svc, a.ID, "250.50", "NGN", "ps_2")

	r, err := svc.Reconcile(ctx, ledger.ReconcileInput{Source: "paystack", Records: []ledger.ExternalRecord{
		{ID: "1", Amount: ledger.Amount("1000", "NGN"), Reference: "ps_1", Date: core.Now()},
		{ID: "2", Amount: ledger.Amount("250.50", "NGN"), Reference: "ps_2", Date: core.Now()},
		{ID: "3", Amount: ledger.Amount("99", "NGN"), Reference: "ps_404", Date: core.Now()},
	}})
	if err != nil || !r.Done() || r.Matched != 2 || r.Unmatched != 1 {
		t.Fatalf("reconcile: %+v %v", r, err)
	}

	tb, err := svc.TrialBalance(ctx, "NGN")
	if err != nil || !tb.Balanced || tb.CustomerFunds.Value != "1250.5" || len(tb.System) != 1 || tb.System[0].Balance.Value != "-1250.5" {
		t.Fatalf("trial balance: %+v %v", tb, err)
	}

	var ops []string
	l := ledger.Instrument(svc, ledger.ObserverFunc(func(ctx context.Context, op string) (context.Context, func(error)) {
		return ctx, func(err error) { ops = append(ops, op+":"+fmt.Sprint(err == nil)) }
	}))
	_, _ = l.GetAccount(ctx, a.ID)
	_, _ = l.Transfer(ctx, ledger.TransferInput{From: a.ID, To: a.ID, Amount: ledger.Amount("1", "NGN"), Reference: "x"})
	if strings.Join(ops, ",") != "GetAccount:true,Transfer:false" {
		t.Fatalf("observed: %v", ops)
	}
}

func TestLimitsCountPendingWithdrawalWithFeeOnce(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{Limits: []ledger.Limit{{Tier: "*", Currency: "NGN", Daily: "100000"}}})
	a, _ := svc.OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN", MetaData: map[string]any{"tier": "t"}})
	fund(t, svc, a.ID, "10000", "NGN", "in")
	wd, err := svc.Withdraw(ctx, ledger.WithdrawInput{AccountID: a.ID, Amount: ledger.Amount("5000", "NGN"), Fee: ledger.Amount("50", ""), Reference: "w"})
	if err != nil {
		t.Fatal(err)
	}
	if u, _ := svc.LimitUsage(ctx, a.ID); u.DailyUsed.Value != "5050" {
		t.Fatalf("pending: %s", u.DailyUsed.Value)
	}
	if _, err := svc.Confirm(ctx, wd.TransactionID, nil); err != nil {
		t.Fatal(err)
	}
	if u, _ := svc.LimitUsage(ctx, a.ID); u.DailyUsed.Value != "5050" {
		t.Fatalf("confirmed withdrawal counted twice: %s", u.DailyUsed.Value)
	}
}
