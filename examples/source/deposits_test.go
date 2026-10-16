package source_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/otyang/humanblnk/ledger"
)

// depositsPricing is Oja's fee table for the deposits page: 1.5% on naira
// deposits, capped at ₦2,000. Everything else is free.
func depositsPricing(ctx context.Context, r ledger.FeeRequest) (ledger.Money, error) {
	if r.Kind == ledger.FeeDeposit && r.Amount.Currency == "NGN" {
		return ledger.ComputeFee(ledger.FeeRule{Percent: "1.5", Max: "2000"}, r.Amount)
	}
	return ledger.NoFee, nil
}

func TestDeposits(t *testing.T) {
	_, _, wallet, ops := setup(t, ledger.Config{Fees: depositsPricing})
	ada := customer(wallet, "user_8812", "Ada", "NGN", "BTC")
	adaNGN, adaBTC := ada.Accounts["NGN"].ID, ada.Accounts["BTC"].ID
	tunde := customer(wallet, "user_9034", "Tunde", "NGN")
	tundeNGN := tunde.Accounts["NGN"].ID

	begin("deposits/provider")
	// sample: deposits/provider
	// Paystack's charge.success webhook says ₦50,000 arrived for Ada.
	r, err := wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN,
		Amount:    ledger.Amount("50000", "NGN"),
		Fee:       ledger.NoFee,          // free top-up (see below for fees)
		Reference: "paystack_T629384756", // Paystack's own reference
		Provider:  "paystack",            // debits @PaystackClearing
	})
	check(err)
	fmt.Println(r.Amount.Display, r.Amount.Currency, r.EntryStatus, r.MetaData["txn_type"])

	// A bank transfer to Ada's virtual account, booked against an exact clearing account.
	_, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID:       adaNGN,
		Amount:          ledger.Amount("20000", "NGN"),
		Fee:             ledger.NoFee,
		Reference:       "va_credit_88120",
		ClearingAccount: "@WemaVirtualClearing", // overrides Provider
	})
	check(err)

	for _, name := range []string{"@PaystackClearing", "@WemaVirtualClearing"} {
		h, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: name, Currency: "NGN"})
		check(err)
		fmt.Println(name, h.Balance.Display, "natural:", h.Natural.Display)
	}
	fmt.Println(ledger.ClearingAccount("paystack"), ledger.ClearingAccount("flutterwave"), ledger.ClearingAccount("@Paystack"))
	// end
	end()

	begin("deposits/fee")
	// sample: deposits/fee
	r, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: tundeNGN,
		Amount:    ledger.Amount("10000", "NGN"), // gross: what Paystack collected
		Fee:       ledger.Amount("100", ""),      // deducted; currency taken from Amount
		Reference: "paystack_T629384801",
		Provider:  "paystack",
	})
	check(err)
	fmt.Println("gross:", r.Amount.Display)
	for _, leg := range r.Legs {
		name := leg.Account
		if leg.Account == string(tundeNGN) {
			name = "Tunde"
		}
		amount := leg.Amount.Display // set on fixed legs
		if leg.Distribution == "left" {
			amount = "the rest"
		}
		fmt.Println(" leg:", name, amount, leg.Narration)
	}
	fmt.Println("fee:", r.MetaData["fee"], "net:", r.MetaData["net_amount"])

	acct, err := wallet.GetAccount(ctx, tundeNGN)
	check(err)
	fmt.Println("Tunde's balance:", acct.Balance.Display)
	// end
	end()

	begin("deposits/config-fees")
	// sample: deposits/config-fees
	// Fee left out: Config.Fees prices it (1.5%, capped at ₦2,000).
	r, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: tundeNGN,
		Amount:    ledger.Amount("200000", "NGN"),
		Reference: "paystack_T629384900",
		Provider:  "paystack",
	})
	check(err)
	fmt.Println("priced fee:", r.MetaData["fee"], "net:", r.MetaData["net_amount"])

	// An explicit Fee wins over Config.Fees, e.g. a promotion.
	r, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: tundeNGN,
		Amount:    ledger.Amount("5000", "NGN"),
		Fee:       ledger.NoFee,
		Reference: "paystack_T629384950",
		Provider:  "paystack",
	})
	check(err)
	fmt.Println("promo legs:", len(r.Legs), "fee meta set:", r.MetaData["fee"] != nil)

	fees, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: ledger.FeeIncomeAccount, Currency: "NGN"})
	check(err)
	fmt.Println("@FeeIncome:", fees.Balance.Display)
	// end
	end()

	begin("deposits/replay")
	// sample: deposits/replay
	// Paystack sends the same webhook twice.
	again, err := wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN,
		Amount:    ledger.Amount("50000", "NGN"),
		Fee:       ledger.NoFee,
		Reference: "paystack_T629384756",
		Provider:  "paystack",
	})
	check(err)
	fmt.Println("replayed:", again.Replayed, "credited once:", again.TransactionID != "")

	// Same reference, different amount: a real conflict, not a retry.
	_, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN,
		Amount:    ledger.Amount("60000", "NGN"),
		Fee:       ledger.NoFee,
		Reference: "paystack_T629384756",
		Provider:  "paystack",
	})
	fmt.Println("conflict:", errors.Is(err, ledger.ErrDuplicateReference))

	acct, err = wallet.GetAccount(ctx, adaNGN)
	check(err)
	fmt.Println("Ada's balance:", acct.Balance.Display)
	// end
	end()

	begin("deposits/pending")
	// sample: deposits/pending
	// A large transfer arrives: hold it until compliance has looked at it.
	held, err := wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN,
		Amount:    ledger.Amount("900000", "NGN"),
		Fee:       ledger.NoFee,
		Reference: "paystack_T700000001",
		Provider:  "paystack",
		Pending:   true,
	})
	check(err)
	acct, err = wallet.GetAccount(ctx, adaNGN)
	check(err)
	fmt.Println(held.EntryStatus, "balance:", acct.Balance.Display, "pending in:", acct.PendingIn.Display)

	ok, err := wallet.Confirm(ctx, held.TransactionID, nil) // checks passed
	check(err)
	acct, err = wallet.GetAccount(ctx, adaNGN)
	check(err)
	fmt.Println(ok.EntryStatus, "balance:", acct.Balance.Display, "pending in:", acct.PendingIn.Display)

	// Another one fails the checks: cancel it and nothing is credited.
	bad, err := wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN, Amount: ledger.Amount("750000", "NGN"), Fee: ledger.NoFee,
		Reference: "paystack_T700000002", Provider: "paystack", Pending: true,
	})
	check(err)
	voided, err := wallet.Cancel(ctx, bad.TransactionID)
	check(err)
	acct, err = wallet.GetAccount(ctx, adaNGN)
	check(err)
	fmt.Println(voided.EntryStatus, "balance:", acct.Balance.Display, "pending in:", acct.PendingIn.Display)
	// end
	end()

	begin("deposits/crypto")
	// sample: deposits/crypto
	// Your custody provider reports 0.0125 BTC arrived on-chain.
	r, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaBTC,
		Amount:    ledger.Amount("0.0125", "BTC"),
		Fee:       ledger.NoFee,
		Reference: "btc_9f2c1e77:0", // txid:vout, unique per output
		Provider:  "custody",        // debits @CustodyClearing
	})
	check(err)
	fmt.Println(r.Amount.Value, "BTC =", r.Amount.PreciseAmount, "satoshi at precision", r.Amount.Precision)

	_, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaBTC,
		Amount:    ledger.Amount("0.000000001", "BTC"), // 9 decimals: BTC has 8
		Fee:       ledger.NoFee,
		Reference: "btc_9f2c1e77:1",
		Provider:  "custody",
	})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)
	// end
	end()

	begin("deposits/status")
	// sample: deposits/status
	check(ops.FreezeAccount(ctx, tundeNGN, "KYC review"))
	_, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: tundeNGN, Amount: ledger.Amount("1000", "NGN"), Fee: ledger.NoFee,
		Reference: "paystack_T800000001", Provider: "paystack",
	})
	fmt.Println("frozen account:", errors.Is(err, ledger.ErrAccountFrozen))
	check(ops.UnfreezeAccount(ctx, tundeNGN))

	// A frozen ledger stops money leaving, not money arriving.
	check(ops.FreezeLedger(ctx, ada.Accounts["NGN"].LedgerID, "incident 42"))
	_, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN, Amount: ledger.Amount("1000", "NGN"), Fee: ledger.NoFee,
		Reference: "paystack_T800000002", Provider: "paystack",
	})
	fmt.Println("frozen ledger, deposit ok:", err == nil)
	check(ops.UnfreezeLedger(ctx, ada.Accounts["NGN"].LedgerID))
	// end
	end()

	_, _, plainWallet, _ := setup(t, ledger.Config{}) // a second service with no Config.Fees
	bola := customer(plainWallet, "user_9100", "Bola", "NGN").Accounts["NGN"].ID
	_, err = ops.CloseAccount(ctx, tundeNGN, ledger.CloseInput{
		Reason: "customer request", RequestedBy: "support_17", ApprovedBy: "ops_lead_2",
		SweepTo: ledger.AccountID(ledger.PayoutClearingAccount), Reference: "close_tunde_ngn",
	})
	check(err)

	begin("deposits/errors")
	// sample: deposits/errors
	// plainWallet belongs to a service with no Config.Fees.
	_, err = plainWallet.Deposit(ctx, ledger.DepositInput{ // and no Fee
		AccountID: bola, Amount: ledger.Amount("1000", "NGN"), Reference: "dep_1",
	})
	fmt.Println("no fee:", errors.Is(err, ledger.ErrInvalidInput), err)

	_, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN, Amount: ledger.Amount("1000", "NGN"), Fee: ledger.NoFee,
	})
	fmt.Println("no reference:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN, Amount: ledger.Amount("100", "NGN"), Fee: ledger.Amount("100", ""),
		Reference: "dep_2",
	})
	fmt.Println("fee >= amount:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN, Amount: ledger.Amount("1000", "USD"), Fee: ledger.NoFee, Reference: "dep_3",
	})
	fmt.Println("wrong currency:", errors.Is(err, ledger.ErrInvalidInput)) // refused by Blnk

	_, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: ledger.AccountID(ledger.FeeIncomeAccount), Amount: ledger.Amount("1000", "NGN"),
		Fee: ledger.NoFee, Reference: "dep_4",
	})
	fmt.Println("house account:", errors.Is(err, ledger.ErrWrongKind))

	// Tunde's NGN account has been closed with ops.CloseAccount.
	_, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: tundeNGN, Amount: ledger.Amount("1000", "NGN"), Fee: ledger.NoFee, Reference: "dep_5",
	})
	fmt.Println("closed account:", errors.Is(err, ledger.ErrAccountClosed))
	// end
	end()
}
