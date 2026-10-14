package ledger_test

// Source of every code sample in docs.html. Each sample sits between
// "// doc:<name>" and "// enddoc" and runs against the emulator, so the
// documentation only shows code that compiles and works. Change a sample
// here, then regenerate docs.html.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/ledgertest"
)

// Compiled, not run: the production setup shown in the docs.
var _ = docSetup

func docSetup() (*ledger.Service, error) {
	// doc:setup
	client := blnk.New(os.Getenv("BLNK_URL"), // e.g. http://localhost:5001
		blnk.WithAPIKey(os.Getenv("BLNK_API_KEY")))

	svc := ledger.New(client, ledger.Config{
		LedgerID:             os.Getenv("BLNK_LEDGER_ID"), // customer accounts live here
		EnforceAccountStatus: true,                        // frozen/closed accounts can't move money
		Fees:                 pricing,                     // central fee pricing (see Fees)
		Limits: []ledger.Limit{ // KYC tiers (see Limits)
			{Tier: "tier1", Currency: "NGN", PerTransaction: "50000", Daily: "200000"},
			{Tier: "tier2", Currency: "NGN", PerTransaction: "100000", Daily: "500000"},
			{Tier: "tier3", Currency: "NGN", PerTransaction: "5000000", Daily: "25000000"},
		},
		Location: lagos, // daily limits reset at midnight Lagos time
	})
	// enddoc
	return svc, nil
}

var lagos = time.FixedZone("WAT", 3600)

// doc:pricing
// pricing is Oja's fee table. In production it would read rules from your
// database so finance can change prices without a deploy; it runs on every
// chargeable call that doesn't set Fee itself.
func pricing(ctx context.Context, r ledger.FeeRequest) (ledger.Money, error) {
	if r.Account != nil && r.Account.MetaData["tier"] == "tier3" {
		return ledger.NoFee, nil // premium customers pay nothing
	}
	rules := map[ledger.FeeKind]map[string]ledger.FeeRule{
		ledger.FeeDeposit:    {"NGN": {Percent: "1.5", Max: "2000"}},
		ledger.FeeTransfer:   {"NGN": {Fixed: "10"}, "USD": {Fixed: "0.10"}},
		ledger.FeeWithdrawal: {"NGN": {Fixed: "50"}, "BTC": {Fixed: "0.00002"}},
		ledger.FeeExchange:   {"USD": {Percent: "1"}},
	}
	rule, ok := rules[r.Kind][r.Amount.Currency]
	if !ok {
		return ledger.NoFee, nil // no rule: free (or return an error to refuse)
	}
	return ledger.ComputeFee(rule, r.Amount)
}

// enddoc

func TestDocsSamples(t *testing.T) {
	cfg := ledger.Config{
		EnforceAccountStatus: true,
		Fees:                 pricing,
		Limits: []ledger.Limit{
			{Tier: "tier1", Currency: "NGN", PerTransaction: "50000", Daily: "200000"},
			{Tier: "tier2", Currency: "NGN", PerTransaction: "100000", Daily: "500000"},
			{Tier: "tier3", Currency: "NGN", PerTransaction: "5000000", Daily: "25000000"},
		},
	}
	svc, core := ledgertest.New(t, cfg)
	core.SetNow(time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC))
	var l ledger.Ledger = svc
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	say := func(format string, a ...any) { t.Logf(format, a...) }
	finished := false
	defer func() {
		if !finished {
			t.Error("a sample returned early on an error")
		}
	}()

	// doc:onboard
	ada, err := l.Customers().Open(ctx, ledger.CustomerInput{
		FirstName: "Ada", LastName: "Obi",
		Email: "ada@example.com", Phone: "+2348030000000", Country: "NG",
		Currencies: []string{"NGN", "USD", "BTC"},
		ExternalID: "user_8812",                     // your user id: retrying returns the same accounts
		MetaData:   map[string]any{"tier": "tier2"}, // KYC tier: drives limits and pricing
	})
	if err != nil {
		return // e.g. ErrInvalidInput, or ErrUnavailable if Blnk is down
	}
	adaNGN := ada.Accounts["NGN"] // *ledger.Account
	adaUSD := ada.Accounts["USD"]
	adaBTC := ada.Accounts["BTC"]
	// enddoc
	check(err)
	tunde, err := l.Customers().Open(ctx, ledger.CustomerInput{FirstName: "Tunde", LastName: "Bakare",
		Currencies: []string{"NGN"}, ExternalID: "user_9034", MetaData: map[string]any{"tier": "tier1"}})
	check(err)
	tundeNGN := tunde.Accounts["NGN"]

	// doc:topup
	// Paystack's charge.success webhook told you ₦150,000 arrived.
	// Use their reference as yours, so a repeated webhook can't credit twice.
	r, err := l.Payments().Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN.ID,
		Amount:    ledger.Amount("150000", "NGN"),
		Reference: "paystack_T629384756", // Paystack's reference
		Provider:  "paystack",            // debits @PaystackClearing
		// Fee left out: Config.Fees prices it (1.5%, capped at ₦2,000)
	})
	if errors.Is(err, ledger.ErrDuplicateReference) {
		// same reference, different amount or account: investigate, don't retry
	}
	fmt.Println(r.Amount.Display, r.EntryStatus) // 150000.00 posted
	// enddoc
	check(err)
	say("deposit: %s %s legs=%d", r.Amount.Display, r.EntryStatus, len(r.Legs))
	again, err := l.Payments().Deposit(ctx, ledger.DepositInput{AccountID: adaNGN.ID, Amount: ledger.Amount("150000", "NGN"),
		Reference: "paystack_T629384756", Provider: "paystack"})
	check(err)
	if !again.Replayed {
		t.Fatal("replay")
	}
	fund := func(id, amount, cur, ref string) {
		_, err := l.Payments().Deposit(ctx, ledger.DepositInput{AccountID: id, Amount: ledger.Amount(amount, cur), Fee: ledger.NoFee, Reference: ref, Provider: "paystack"})
		check(err)
	}
	fund(adaUSD.ID, "300", "USD", "usd_wire_1")

	// doc:balance
	acct, err := l.Accounts().Get(ctx, adaNGN.ID)
	fmt.Println(acct.Balance.Display)    // 148000.00  posted money (₦150,000 − ₦2,000 fee)
	fmt.Println(acct.Available.Display)  // 148000.00  what she can spend now
	fmt.Println(acct.PendingOut.Display) // 0.00       reserved by holds and pending withdrawals
	// enddoc
	check(err)
	if acct.Balance.Display != "148000.00" {
		t.Fatalf("balance %s", acct.Balance.Display)
	}

	// doc:p2p
	transfer := ledger.TransferInput{
		From: adaNGN.ID, To: tundeNGN.ID,
		Amount:      ledger.Amount("12500", "NGN"),
		Reference:   "p2p_01J8ZK2M4Q", // generate once, when the user taps "Send"
		Description: "Rent share",
	}
	q, err := l.Payments().QuoteTransfer(ctx, transfer) // show this on the confirm screen
	if err != nil {
		return
	}
	if !q.Allowed {
		fmt.Println("can't send:", q.Reason) // ErrInsufficientFunds, ErrLimitExceeded, ErrAccountFrozen…
		return
	}
	fmt.Printf("Send ₦%s, fee ₦%s, total ₦%s\n", q.Amount.Display, q.Fee.Display, q.Debit.Display)
	// Send ₦12500.00, fee ₦10.00, total ₦12510.00

	r, err = l.Payments().Transfer(ctx, transfer) // the user tapped Confirm
	// enddoc
	check(err)
	say("quote %s; transfer %s", q, r.EntryStatus)

	// doc:withdraw
	// Ada withdraws ₦20,000 to her GTBank account through Flutterwave.
	wd, err := l.Payments().Withdraw(ctx, ledger.WithdrawInput{
		AccountID:   adaNGN.ID,
		Amount:      ledger.Amount("20000", "NGN"), // what reaches her bank
		Reference:   "wd_01J8ZM7R2X",
		Provider:    "flutterwave", // credits @FlutterwaveClearing
		Description: "Withdrawal to GTBank ****4410",
	})
	if err != nil {
		return // ErrInsufficientFunds, ErrLimitExceeded, ErrAccountFrozen…
	}
	// wd.EntryStatus == "pending": the ₦20,050 (₦50 fee) is reserved, not yet gone.
	// Now call Flutterwave's transfer API with wd.TransactionID as your reference.

	// Later, in Flutterwave's webhook handler:
	_, err = l.Pending().Confirm(ctx, wd.TransactionID, nil) // transfer.completed: SUCCESSFUL
	// or: l.Pending().Cancel(ctx, wd.TransactionID)        // FAILED: money returns to Ada
	// Neither arrives within 24h? The hold cancels itself (Config.WithdrawalHoldTTL).
	// enddoc
	check(err)

	// doc:card
	// Ada's virtual card is used at a restaurant: authorise ₦18,500.
	auth, err := l.Pending().Hold(ctx, ledger.HoldInput{
		AccountID:   adaNGN.ID,
		Destination: ledger.MerchantPayableAccount, // owed to the card network
		Amount:      ledger.Amount("18500", "NGN"),
		Reference:   "card_auth_449201",
		ExpiresAt:   time.Now().Add(7 * 24 * time.Hour), // released if never captured
		Description: "Mama Nkechi's Kitchen",
	})
	if err != nil {
		return // declined: insufficient funds, limit, frozen card…
	}
	// The merchant captures less than authorised (e.g. a partial order).
	captured := ledger.Amount("17250.50", "") // currency taken from the hold
	if _, err = l.Pending().Confirm(ctx, auth.TransactionID, &captured); err != nil {
		return // posts ₦17,250.50
	}
	_, err = l.Pending().Cancel(ctx, auth.TransactionID) // releases the other ₦1,249.50
	// enddoc
	check(err)
	acct, _ = l.Accounts().Get(ctx, adaNGN.ID)
	say("after card: %s available %s", acct.Balance.Display, acct.Available.Display)

	// doc:fx
	// Ada converts $100 to naira at the rate you quoted (₦1,532.40 per $1).
	x, err := l.Payments().Exchange(ctx, ledger.ExchangeInput{
		From: adaUSD.ID, To: adaNGN.ID,
		Sell:      ledger.Amount("100", "USD"),
		Rate:      "1532.40",
		Reference: "fx_01J8ZQ0T5C",
		// Fee from Config.Fees: 1% of the dollars sold, charged on top
	})
	fmt.Println(x.Sold.Display, x.Fee.Display, "USD →", x.Bought.Display, "NGN")
	// 100.00 1.00 USD → 153240.00 NGN   (both legs post atomically, or neither)
	// enddoc
	check(err)
	say("fx: %s %s → %s", x.Sold.Display, x.Fee.Display, x.Bought.Display)

	// doc:crypto
	// Your custody provider reports 0.0125 BTC arrived on-chain.
	_, err = l.Payments().Deposit(ctx, ledger.DepositInput{
		AccountID: adaBTC.ID,
		Amount:    ledger.Amount("0.0125", "BTC"), // exact: stored as 1,250,000 satoshi
		Reference: "btc_tx_9f2c1e…:0",             // txid:vout, unique per output
		Provider:  "custody",
		Fee:       ledger.NoFee, // incoming crypto is free
	})
	if err != nil {
		return
	}
	_, err = l.Payments().Withdraw(ctx, ledger.WithdrawInput{
		AccountID: adaBTC.ID,
		Amount:    ledger.Amount("0.004567896", "BTC"), // 9 decimals: more than BTC has
		Reference: "btc_out_77",
		Provider:  "onchain",
	})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput)) // true: rejected before reaching Blnk
	out, err := l.Payments().Withdraw(ctx, ledger.WithdrawInput{
		AccountID: adaBTC.ID, Amount: ledger.Amount("0.00456789", "BTC"),
		Reference: "btc_out_77", Provider: "onchain", Immediate: true,
	})
	if err != nil {
		return
	}
	fmt.Println(out.Amount.Value, out.Amount.Display) // 0.00458789 0.00458789 (incl. 0.00002 fee)
	// enddoc
	check(err)
	say("btc out: %s / %s", out.Amount.Value, out.Amount.Display)

	merchant, err := l.Customers().Open(ctx, ledger.CustomerInput{Type: "organization", OrganizationName: "Chidi Stores Ltd",
		Currencies: []string{"NGN"}, ExternalID: "merchant_311"})
	check(err)
	chidi := merchant.Accounts["NGN"]

	// doc:marketplace
	// A ₦25,000 order paid by card through Paystack: the seller gets the rest
	// after Oja's 5% commission and a fixed ₦1,500 delivery fee for the rider.
	order, err := l.Payments().Split(ctx, ledger.SplitInput{
		From:           ledger.ClearingAccount("paystack"), // money arriving from outside
		AllowOverdraft: true,
		Amount:         ledger.Amount("25000", "NGN"),
		Reference:      "order_10442",
		Parts: []ledger.SplitPart{
			{To: ledger.FeeIncomeAccount, Percent: "5", Narration: "Oja commission"},
			{To: tundeNGN.ID, Amount: ledger.Amount("1500", ""), Narration: "Delivery"},
			{To: chidi.ID, Rest: true, Narration: "Order 10442"},
		},
	})
	// Chidi Stores gets exactly ₦22,250. Every share is computed in exact
	// integers; percentage rounding dust goes to the Rest part.
	// enddoc
	check(err)
	say("split legs %d", len(order.Legs))

	// doc:escrow
	// Ada buys a ₦80,000 phone from Chidi Stores with buyer protection.
	esc, err := l.Escrows().Open(ctx, ledger.EscrowInput{
		Payer: adaNGN.ID, Beneficiary: chidi.ID,
		Amount:    ledger.Amount("80000", "NGN"),
		Reference: "escrow_order_10450",
	})
	if err != nil {
		return
	}
	// esc.ID is its own account: the money is out of Ada's balance but not yet Chidi's.

	// Ada confirms delivery → pay the seller everything still held:
	_, err = l.Escrows().Release(ctx, esc.ID, ledger.EscrowSettleInput{Reference: "escrow_10450_release"})
	// Or the order is cancelled → l.Escrows().Refund(ctx, esc.ID, …) returns it to Ada.
	// Partial: set Amount on EscrowSettleInput, e.g. refund the case, release the rest.
	// enddoc
	check(err)

	pay, err := l.Payments().Transfer(ctx, ledger.TransferInput{From: adaNGN.ID, To: chidi.ID, Amount: ledger.Amount("9800", "NGN"), Reference: "pay_10461"})
	check(err)

	// doc:dispute
	// Ada says the ₦9,800 item never arrived. Freeze that money at the seller:
	d, err := l.Disputes().Open(ctx, ledger.DisputeInput{
		TransactionID: pay.TransactionID,
		Reference:     "dispute_2231",
		Reason:        "item not received",
	})
	if err != nil {
		return
	}
	// Investigation finds for Ada: the held money goes back to her.
	_, err = l.Disputes().Resolve(ctx, d.TransactionID, ledger.DisputeUpheld, nil)
	// Found for the seller instead: ledger.DisputeRejected releases it to them.
	// enddoc
	check(err)

	pay2, err := l.Payments().Transfer(ctx, ledger.TransferInput{From: adaNGN.ID, To: chidi.ID, Amount: ledger.Amount("6000", "NGN"), Reference: "pay_10470"})
	check(err)

	// doc:refund
	// One of three items was out of stock: Chidi Stores refunds ₦2,000 of ₦6,000.
	_, err = l.Transactions().RefundPartial(ctx, pay2.TransactionID, ledger.PartialRefundInput{
		Amount:    ledger.Amount("2000", ""),
		Reference: "refund_10470_1",
		Reason:    "item out of stock",
	})
	// A second partial refund above the ₦4,000 left fails with ErrAmountExceeded.
	// For a full reversal in one step: l.Transactions().Refund(ctx, id, ledger.RefundInput{Reason: "…"})
	// enddoc
	check(err)

	// doc:pots
	rent, err := l.Customers().OpenPot(ctx, ledger.PotInput{
		ParentID: adaNGN.ID, Name: "Rent", Goal: ledger.Amount("600000", ""),
	})
	if err != nil {
		return
	}
	// Save ₦25,000 into it on the 1st of every month for a year.
	plan, err := l.Payments().Schedule(ctx, ledger.RecurringInput{
		Transfer: ledger.TransferInput{
			From: adaNGN.ID, To: rent.ID,
			Amount: ledger.Amount("25000", "NGN"), Fee: ledger.NoFee,
			Reference: "rent_plan_ada_2026", Description: "Rent savings",
		},
		First: time.Date(2026, 10, 1, 7, 0, 0, 0, lagos),
		Every: ledger.Monthly,
		Count: 12,
	})
	if err != nil {
		return
	}
	fmt.Println(len(plan), plan[0].EntryStatus) // 12 scheduled
	// enddoc
	check(err)

	fund(chidi.ID, "600000", "NGN", "chidi_bank_transfer_0925")

	// doc:payroll
	// Chidi Stores pays its staff on the 25th: all or nothing.
	staff := map[string]string{tundeNGN.ID: "85000", adaNGN.ID: "120000"}
	var items []ledger.TransferInput
	for accountID, salary := range staff {
		items = append(items, ledger.TransferInput{
			From: chidi.ID, To: accountID, Amount: ledger.Amount(salary, "NGN"),
			Fee: ledger.NoFee, Reference: "payroll_2026_09_" + accountID,
		})
	}
	batch, err := l.Payments().Bulk(ctx, ledger.BulkTransferInput{Items: items, Atomic: true})
	// enddoc
	check(err)
	say("payroll batch %s (%s)", batch.BatchID, batch.Status)

	// doc:limits
	_, err = l.Payments().Transfer(ctx, ledger.TransferInput{
		From: adaNGN.ID, To: tundeNGN.ID, Amount: ledger.Amount("150000", "NGN"), Reference: "p2p_big",
	})
	if errors.Is(err, ledger.ErrLimitExceeded) {
		// tier2 allows ₦100,000 per transfer: offer Ada the upgrade to tier3
	}
	u, err := l.Accounts().LimitUsage(ctx, adaNGN.ID)
	fmt.Printf("You can send ₦%s more today\n", u.DailyLeft.Display)
	// enddoc
	check(err)
	say("limits: left %s used %s", u.DailyLeft.Display, u.DailyUsed.Display)

	core.Advance(30 * 24 * time.Hour)

	// doc:statement
	// Ada downloads her September statement from the app.
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, lagos)
	to := from.AddDate(0, 1, 0)
	st, err := l.Accounts().Statement(ctx, adaNGN.ID, from, to)
	if err != nil {
		return
	}
	fmt.Println(st.Opening.Display, st.Credits.Display, st.Debits.Display, st.Closing.Display)
	for _, line := range st.Lines {
		fmt.Println(line.Date.Format("02 Jan"), line.Description, line.Credit.Display, line.Debit.Display, line.Balance.Display)
	}
	var csv bytes.Buffer
	_ = st.WriteCSV(&csv) // or straight to an http.ResponseWriter
	// enddoc
	if out := os.Getenv("DOCS_CSV_OUT"); out != "" { // used when regenerating docs.html
		check(os.WriteFile(out, csv.Bytes(), 0o644))
	}
	say("statement: opening %s closing %s lines %d", st.Opening.Display, st.Closing.Display, len(st.Lines))

	// doc:freeze
	_ = l.Accounts().Freeze(ctx, tundeNGN.ID, "suspected account takeover")
	_, err = l.Payments().Transfer(ctx, ledger.TransferInput{From: tundeNGN.ID, To: adaNGN.ID,
		Amount: ledger.Amount("100", "NGN"), Reference: "p2p_x1"})
	// errors.Is(err, ledger.ErrAccountFrozen) == true
	_ = l.Accounts().Unfreeze(ctx, tundeNGN.ID)
	// enddoc
	if !errors.Is(err, ledger.ErrAccountFrozen) {
		t.Fatalf("freeze: %v", err)
	}

	// doc:settlement
	// Paystack's settlement report: ₦4,880,000 collected yesterday, ₦73,200
	// in fees, ₦4,806,800 paid into your bank.
	_, err = l.Operations().RecordSettlement(ctx, ledger.SettlementInput{
		Provider:  "paystack",
		Direction: ledger.SettlementCollection,
		Amount:    ledger.Amount("4880000", "NGN"),
		Fee:       ledger.Amount("73200", ""),
		Reference: "paystack_settlement_2026_09_30",
	})
	// Dr @BankOperating 4,806,800 · Dr @ProviderFees 73,200 · Cr @PaystackClearing 4,880,000
	// @PaystackClearing now shows only what Paystack still owes you.
	// enddoc
	check(err)

	// doc:suspense
	// ₦15,000 arrived with no matching customer reference.
	_, err = l.Operations().ParkInSuspense(ctx, ledger.SuspenseInput{
		Amount: ledger.Amount("15000", "NGN"), Provider: "paystack",
		Reference: "paystack_T7700011", Note: "sender: OKAFOR J., narration blank",
	})
	if err != nil {
		return
	}
	// Support identifies it as Tunde's:
	_, err = l.Operations().AllocateSuspense(ctx, ledger.AllocateInput{
		AccountID: tundeNGN.ID, Amount: ledger.Amount("15000", "NGN"),
		Reference: "alloc_7700011", SuspenseReference: "paystack_T7700011",
	})
	// enddoc
	check(err)

	// doc:adjust
	_, err = l.Operations().Adjust(ctx, ledger.AdjustInput{
		AccountID: adaNGN.ID, Type: ledger.AdjustCredit,
		Amount:      ledger.Amount("500", "NGN"),
		Reason:      "Cashback: September promo",
		RequestedBy: "ops.kemi", ApprovedBy: "lead.femi", // must be two different people
		Reference: "adj_cashback_ada_09",
	})
	// enddoc
	check(err)

	// doc:trial
	tb, err := l.Operations().TrialBalance(ctx, "NGN")
	if err != nil {
		return
	}
	if !tb.Balanced {
		// entries were lost or posted outside this ledger: page someone
	}
	fmt.Println("customers hold", tb.CustomerFunds.Display) // what you owe customers
	for _, a := range tb.System {
		fmt.Println(a.Indicator, a.Balance.Display) // @FeeIncome, @PaystackClearing, …
	}
	// enddoc
	check(err)
	say("trial: balanced=%v customers=%s", tb.Balanced, tb.CustomerFunds.Display)

	// doc:watch
	payout, _ := l.Accounts().GetSystem(ctx, "@FlutterwaveClearing", "NGN")
	_, err = l.Operations().WatchBalance(ctx, ledger.WatchInput{
		AccountID: payout.ID,
		Operator:  ">",
		Threshold: ledger.Amount("10000000", ""), // ₦10m of payouts not yet settled
	})
	// Arrives as a "balance.monitor" webhook (Event.Alert) after every update
	// while true, so debounce your alert.
	// enddoc
	check(err)

	// doc:reconcile
	report := []ledger.ExternalRecord{ // parsed from Paystack's settlement CSV
		{ID: "PSK-88120", Amount: ledger.Amount("15000", "NGN"), Reference: "paystack_T7700011", Date: from},
		{ID: "PSK-88121", Amount: ledger.Amount("4300", "NGN"), Reference: "paystack_T0000404", Date: from},
	}
	run, err := l.Reconciliations().Start(ctx, ledger.ReconcileInput{Source: "paystack", Records: report})
	if err != nil {
		return
	}
	for !run.Done() {
		time.Sleep(2 * time.Second)
		run, err = l.Reconciliations().Get(ctx, run.ID)
	}
	fmt.Println(run.Matched, "matched,", run.Unmatched, "to investigate") // 1 matched, 1 to investigate
	// enddoc
	check(err)
	say("recon: %d/%d", run.Matched, run.Unmatched)

	// doc:observe
	var observed ledger.Ledger = ledger.Instrument(svc, ledger.LogObserver(slog.Default()))
	// INFO ledger call op=Payments.Transfer duration=4.1ms
	// WARN ledger call refused op=Payments.Transfer error="… insufficient funds"
	// enddoc
	_ = observed

	// doc:webhook
	// Blnk's global webhook: notification.webhook.url in blnk.json, signed
	// with server.secret_key.
	http.Handle("/webhooks/blnk", svc.WebhookHandler(
		ledger.WebhookConfig{Secret: os.Getenv("BLNK_SECRET_KEY")},
		func(ctx context.Context, e *ledger.Event) error {
			switch e.Name {
			case ledger.EventTransactionApplied:
				// push notification: "₦" + e.Receipt.Amount.Display + " received"
			case ledger.EventTransactionVoid:
				// a hold or pending withdrawal was cancelled or expired
			case ledger.EventBalanceAlert:
				// e.Alert.AccountID crossed e.Alert.Threshold
			case ledger.EventBulkFailed:
				// e.Bulk.BatchID failed: e.Bulk.Error
			}
			return nil // an error answers 500 and Blnk retries: keep handlers idempotent
		}))
	// enddoc
	say("events emitted: %d", len(core.Events()))
	finished = true
}

// doc:testing
func TestRentTransfer(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{}) // in-memory Blnk, no Docker
	ctx := context.Background()

	ada, _ := svc.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN"})
	landlord, _ := svc.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN"})
	svc.Payments().Deposit(ctx, ledger.DepositInput{AccountID: ada.ID,
		Amount: ledger.Amount("300000", "NGN"), Fee: ledger.NoFee, Reference: "seed", Provider: "paystack"})

	_, err := svc.Payments().Transfer(ctx, ledger.TransferInput{From: ada.ID, To: landlord.ID,
		Amount: ledger.Amount("250000", "NGN"), Fee: ledger.NoFee, Reference: "rent_sept"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Accounts().Get(ctx, landlord.ID)
	if got.Balance.Display != "250000.00" {
		t.Fatalf("landlord got %s", got.Balance.Display)
	}

	core.Advance(25 * time.Hour)      // expire holds, run scheduled transfers
	for _, e := range core.Events() { // the webhooks Blnk would have sent
		_ = e
	}
}

// enddoc
