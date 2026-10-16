// Command example walks through a full account flow.
//
// Against a real Blnk Core instance:
//
//	BLNK_URL=https://your-core.example.com BLNK_API_KEY=... BLNK_LEDGER_ID=ldg_... go run ./cmd/example
//
// With BLNK_URL unset it runs against the in-memory emulator (ledgertest),
// so it works with no setup:
//
//	go run ./cmd/example
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/ledgertest"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	url := os.Getenv("BLNK_URL")
	if url == "" {
		core := ledgertest.NewServer()
		defer core.Close()
		url = core.URL()
		fmt.Println("BLNK_URL not set: running against the in-memory Blnk emulator (nothing is saved)")
	}
	client := blnk.New(url, blnk.WithAPIKey(os.Getenv("BLNK_API_KEY")))

	ledgerID := ledger.LedgerID(os.Getenv("BLNK_LEDGER_ID"))
	if ledgerID == "" {
		// Create is idempotent by Key: running this again returns the same ledger.
		l, err := ledger.New(client, ledger.Config{}).Ops().CreateLedger(ctx, ledger.CreateLedgerInput{
			Key: "customer-accounts", Name: "Customer Accounts",
		})
		must(err)
		ledgerID = l.ID
		fmt.Println("ledger", ledgerID, "replayed:", l.Replayed)
	}

	service := ledger.New(client, ledger.Config{
		LedgerID: ledgerID, // NGN, USD and BTC are built in; Config.Currencies adds others
	})
	must(service.Check(ctx))        // fails now if Config.LedgerID doesn't exist
	var svc ledger.Ledger = service // depend on the interface; Service adds helpers like ComputeFee
	run := time.Now().UnixNano()    // unique references for this demo run

	alice, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN", MetaData: map[string]any{"owner": "alice"}})
	must(err)
	bob, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN", MetaData: map[string]any{"owner": "bob"}})
	must(err)

	// 1. Deposit ₦10,000 via Paystack, ₦150 fee → Alice gets ₦9,850.
	//    Dr @PaystackClearing 10,000 / Cr @FeeIncome 150 / Cr Alice 9,850.
	//    Amounts are typed as decimals; Blnk stores precise_amount 1000000 + precision 100.
	dep, err := svc.Wallet().Deposit(ctx, ledger.DepositInput{
		AccountID: alice.ID, Amount: ledger.Amount("10000", "NGN"), Fee: ledger.Amount("150", "NGN"),
		Reference: fmt.Sprintf("paystack_%d", run), Provider: "paystack",
	})
	must(err)
	fmt.Printf("deposit: %s %s (%s) amount=%s precise=%s precision=%d\n",
		dep.TransactionID, dep.Status, dep.EntryStatus, dep.Amount.Value, dep.Amount.PreciseAmount, dep.Amount.Precision)

	// 2. Transfer ₦2,000 to Bob with a ₦10 fee.
	tr, err := svc.Wallet().Transfer(ctx, ledger.TransferInput{
		From: alice.ID, To: bob.ID, Amount: ledger.Amount("2000", "NGN"), Fee: ledger.Amount("10", "NGN"),
		Reference: fmt.Sprintf("p2p_%d", run), Description: "Lunch",
	})
	must(err)
	fmt.Println("transfer:", tr.TransactionID, tr.Status)

	// 3. Withdraw ₦5,000 — pending until the payout provider confirms.
	//    Dr Alice 5,050 / Cr @FlutterwaveClearing 5,000 / Cr @FeeIncome 50.
	wd, err := svc.Wallet().Withdraw(ctx, ledger.WithdrawInput{
		AccountID: alice.ID, Amount: ledger.Amount("5000", "NGN"), Fee: ledger.Amount("50", "NGN"),
		Reference: fmt.Sprintf("payout_%d", run), Provider: "flutterwave",
	})
	must(err)
	fmt.Println("withdrawal:", wd.TransactionID, wd.EntryStatus) // pending

	// ... provider webhook says success:
	_, err = svc.Wallet().Confirm(ctx, wd.TransactionID, nil)
	must(err)

	// 4. Overspending is a typed error, not a string match.
	_, err = svc.Wallet().Transfer(ctx, ledger.TransferInput{
		Fee:  ledger.NoFee,
		From: bob.ID, To: alice.ID, Amount: ledger.Amount("9999999.99", "NGN"), Reference: fmt.Sprintf("too_much_%d", run),
	})
	if errors.Is(err, ledger.ErrInsufficientFunds) {
		fmt.Println("as expected: insufficient funds")
	}

	// 5. Card-style hold, partial capture, release the rest.
	hold, err := svc.Wallet().Hold(ctx, ledger.HoldInput{
		AccountID: bob.ID, Destination: ledger.MerchantPayableAccount, Amount: ledger.Amount("1000", "NGN"),
		Reference: fmt.Sprintf("auth_%d", run), ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	})
	must(err)
	part := ledger.Amount("600", "") // currency/precision taken from the hold
	_, err = svc.Wallet().Confirm(ctx, hold.TransactionID, &part)
	must(err)
	_, err = svc.Wallet().Cancel(ctx, hold.TransactionID)
	must(err)

	// 6. Balances and history.
	for _, id := range []ledger.AccountID{alice.ID, bob.ID} {
		w, err := svc.Wallet().GetAccount(ctx, id)
		must(err)
		fmt.Printf("%s balance=%s %s available=%s %s\n", w.MetaData["owner"], w.Balance.Display, w.Currency, w.Available.Display, w.Currency)
	}
	page, err := svc.Wallet().History(ctx, alice.ID, ledger.HistoryQuery{Statuses: []string{string(ledger.EntryPosted)}, Limit: 10})
	must(err)
	for _, r := range page.Items {
		fmt.Println(" ", r.CreatedAt.Format(time.RFC3339), r.EntryStatus, r.Amount, r.Source, "→", r.Destination)
	}

	// 7. Crypto: debit exactly 0.456 BTC. Stored as precise_amount 45600000, precision 100000000.
	btc, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "BTC"})
	must(err)
	_, err = svc.Wallet().Deposit(ctx, ledger.DepositInput{
		Fee:       ledger.NoFee,
		AccountID: btc.ID, Amount: ledger.Amount("1.25", "BTC"), Reference: fmt.Sprintf("btc_in_%d", run), Provider: "custody",
	})
	must(err)
	out, err := svc.Wallet().Withdraw(ctx, ledger.WithdrawInput{
		AccountID: btc.ID, Amount: ledger.Amount("0.456", "BTC"), Fee: ledger.Amount("0.0001", ""),
		Reference: fmt.Sprintf("btc_out_%d", run), Provider: "onchain", Immediate: true,
	})
	must(err)
	j, _ := json.Marshal(out.Amount)
	fmt.Println("btc withdrawal:", string(j)) // {"amount":"0.4561","currency":"BTC","precise_amount":"45610000","precision":100000000}

	// 8. Quote before charging: fee from a rule, total debit, balance after.
	fee, err := service.ComputeFee(ledger.FeeRule{Percent: "1.5", Min: "10", Max: "2000"}, ledger.Amount("1000", "NGN"))
	must(err)
	q, err := svc.Wallet().QuoteTransfer(ctx, ledger.TransferInput{From: alice.ID, To: bob.ID, Amount: ledger.Amount("1000", "NGN"), Fee: fee, Reference: fmt.Sprintf("q_%d", run)})
	must(err)
	fmt.Println("quote:", q)

	// 9. Marketplace order: seller gets the rest after the platform's 10% and a fixed delivery fee.
	_, err = svc.Wallet().Split(ctx, ledger.SplitInput{
		From: alice.ID, Amount: ledger.Amount("1500", "NGN"), Reference: fmt.Sprintf("order_%d", run),
		Parts: []ledger.SplitPart{
			{To: ledger.FeeIncomeAccount, Percent: "10"},
			{To: ledger.MerchantPayableAccount, Amount: ledger.Amount("200", "")},
			{To: bob.ID, Rest: true},
		},
	})
	must(err)

	// 10. Paystack settles the day's collections to the bank, keeping a ₦150 fee:
	//     Dr @BankOperating 9,850 / Dr @ProviderFees 150 / Cr @PaystackClearing 10,000.
	_, err = svc.Ops().RecordSettlement(ctx, ledger.SettlementInput{
		Provider: "paystack", Amount: ledger.Amount("10000", "NGN"), Fee: ledger.Amount("150", ""),
		Reference: fmt.Sprintf("stl_%d", run),
	})
	must(err)

	// 11. Statement and trial balance.
	st, err := svc.Wallet().Statement(ctx, alice.ID, time.Now().Add(-time.Hour), time.Now().Add(time.Minute))
	must(err)
	fmt.Printf("alice statement: opening %s, %d entries, closing %s\n", st.Opening, len(st.Lines), st.Closing)
	tb, err := svc.Ops().TrialBalance(ctx, "NGN")
	must(err)
	fmt.Printf("NGN trial balance: customers hold %s, net %s, balanced=%v\n", tb.CustomerFunds, tb.Net, tb.Balanced)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
