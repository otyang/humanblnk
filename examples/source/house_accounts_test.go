package source_test

import (
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/otyang/humanblnk/ledger"
)

func TestHouseAccounts(t *testing.T) {
	_, core, wallet, ops := setup(t, ledger.Config{})
	client := core.Client() // stands for your blnk.Client
	ada := customer(wallet, "user_8812", "Ada", "NGN")
	adaNGN := ada.Accounts["NGN"].ID
	tunde := customer(wallet, "user_9034", "Tunde", "NGN")
	tundeNGN := tunde.Accounts["NGN"].ID
	ledgerID := ada.Accounts["NGN"].LedgerID

	// A day of activity: two Paystack top-ups with fees, a Flutterwave payout,
	// and Paystack settling to the bank in the evening.
	_, err := wallet.Deposit(ctx, ledger.DepositInput{AccountID: adaNGN, Amount: ledger.Amount("50000", "NGN"),
		Fee: ledger.Amount("750", ""), Reference: "paystack_T100201", Provider: "paystack"})
	check(err)
	core.Advance(2 * time.Hour)
	_, err = wallet.Deposit(ctx, ledger.DepositInput{AccountID: tundeNGN, Amount: ledger.Amount("20000", "NGN"),
		Fee: ledger.Amount("300", ""), Reference: "paystack_T100202", Provider: "paystack"})
	check(err)
	core.Advance(2 * time.Hour)
	_, err = wallet.Withdraw(ctx, ledger.WithdrawInput{AccountID: adaNGN, Amount: ledger.Amount("10000", "NGN"),
		Fee: ledger.Amount("50", ""), Reference: "payout_7001", Provider: "flutterwave", Immediate: true})
	check(err)
	core.Advance(4 * time.Hour)
	_, err = ops.RecordSettlement(ctx, ledger.SettlementInput{Provider: "paystack", Direction: ledger.SettlementCollection,
		Amount: ledger.Amount("50000", "NGN"), Fee: ledger.Amount("750", ""), Reference: "paystack_stl_0901"})
	check(err)
	core.Advance(14 * time.Hour) // the next morning

	begin("house-accounts/get")
	// sample: house-accounts/get
	for _, name := range []string{ledger.FeeIncomeAccount, "@PaystackClearing"} {
		h, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: name, Currency: "NGN"})
		check(err)
		fmt.Println(h.Indicator, h.Currency, h.Type, h.Normal, "balance:", h.Balance.Display, "natural:", h.Natural.Display)
	}
	// end
	end()

	begin("house-accounts/list")
	// sample: house-accounts/list
	all, err := ops.ListHouseAccounts(ctx, "NGN")
	check(err)
	for _, h := range all {
		fmt.Printf("%-21s %-9s %-6s %10s\n", h.Indicator, h.Type, h.Normal, h.Natural.Display)
	}
	// end
	end()

	begin("house-accounts/chart")
	// sample: house-accounts/chart
	names := make([]string, 0, len(ledger.Chart))
	for name := range ledger.Chart {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		def := ledger.Chart[name]
		fmt.Printf("%-17s %-9s %s\n", name, def.Type, def.Normal)
	}

	fmt.Println(ledger.ClearingAccount("paystack"), ledger.ClearingAccount("@Flutterwave"), ledger.ClearingAccount(""))
	fmt.Println("@MonnifyClearing:", ledger.ChartEntry("@MonnifyClearing").Type, "| @Marketing:", ledger.ChartEntry("@Marketing").Type == ledger.GLUnknown)
	// end
	end()

	begin("house-accounts/history")
	// sample: house-accounts/history
	paystack := ledger.HouseRef{Indicator: "@PaystackClearing", Currency: "NGN"}
	h, err := ops.HouseHistory(ctx, paystack, ledger.HistoryQuery{}) // newest first; same query as wallet.History
	check(err)
	for _, r := range h.Items {
		fmt.Println(r.CreatedAt.Format("15:04"), r.Reference, r.Amount.Display, r.Description)
	}

	noon, err := ops.HouseBalanceAt(ctx, paystack, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	check(err)
	fmt.Println("at noon, as Blnk stores it:", noon.Display)
	// end
	end()

	begin("house-accounts/statement")
	// sample: house-accounts/statement
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	st, err := ops.HouseStatement(ctx, paystack, day, day.AddDate(0, 0, 1))
	check(err)
	fmt.Println(st.Indicator, "opening", st.Opening.Display, "closing", st.Closing.Display)
	for _, l := range st.Lines {
		fmt.Println(l.Date.Format("15:04"), l.Reference, "| credit", l.Credit.Display, "| debit", l.Debit.Display,
			"| balance", l.Balance.Display, "|", l.Counterpart)
	}
	// end
	end()

	saved, had := ledger.Chart["@FlutterwaveClearing"]
	t.Cleanup(func() { // other pages see the stock chart
		delete(ledger.Chart, "@Marketing")
		if had {
			ledger.Chart["@FlutterwaveClearing"] = saved
		} else {
			delete(ledger.Chart, "@FlutterwaveClearing")
		}
	})

	begin("house-accounts/custom")
	// sample: house-accounts/custom
	// At start-up, before any request is served.
	ledger.Chart["@FlutterwaveClearing"] = ledger.GLDef{Type: ledger.GLLiability, Normal: ledger.CreditNormal} // payouts only
	ledger.Chart["@Marketing"] = ledger.GLDef{Type: ledger.GLExpense, Normal: ledger.DebitNormal}

	// Oja pays Tunde a ₦500 sign-up bonus out of its marketing budget.
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: ledger.AccountID("@Marketing"), To: tundeNGN,
		Amount: ledger.Amount("500", "NGN"), Fee: ledger.NoFee, Reference: "bonus_9034", AllowOverdraft: true,
		Description: "Sign-up bonus"})
	check(err)

	for _, name := range []string{"@FlutterwaveClearing", "@Marketing"} {
		h, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: name, Currency: "NGN"})
		check(err)
		fmt.Println(h.Indicator, h.Type, h.Normal, "balance:", h.Balance.Display, "natural:", h.Natural.Display)
	}
	// end
	end()

	begin("house-accounts/overrides")
	// sample: house-accounts/overrides
	// Oja's books already used @Fees and @World before humanblnk.
	svc := ledger.New(client, ledger.Config{
		LedgerID:               ledgerID,
		FeeIncomeAccount:       "@Fees",  // default @FeeIncome
		DepositClearingAccount: "@World", // default @DepositClearing, when a deposit names no provider
		PayoutClearingAccount:  "@World", // default @PayoutClearing, when a withdrawal names no provider
	})
	r, err := svc.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: adaNGN, Amount: ledger.Amount("1000", "NGN"),
		Fee: ledger.Amount("15", ""), Reference: "bank_transfer_5531"}) // no Provider
	check(err)
	for _, leg := range r.Legs {
		fmt.Println("leg to", leg.Account, leg.Narration)
	}
	fees, err := svc.Ops().GetHouseAccount(ctx, ledger.HouseRef{Indicator: "@Fees", Currency: "NGN"})
	check(err)
	fmt.Println(fees.Indicator, fees.Type, fees.Natural.Display)
	// end
	end()

	begin("house-accounts/errors")
	// sample: house-accounts/errors
	income, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: "@FeeIncome", Currency: "NGN"})
	check(err)

	_, err = wallet.GetAccount(ctx, ledger.AccountID(income.ID)) // its balance id
	fmt.Println(errors.Is(err, ledger.ErrWrongKind), err)

	_, err = wallet.GetAccount(ctx, ledger.AccountID("@FeeIncome"))
	fmt.Println(errors.Is(err, ledger.ErrWrongKind))

	_, err = ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: "FeeIncome", Currency: "NGN"})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	_, err = ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: "@FeeIncome"})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	_, err = ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: "@FeeIncome", Currency: "GHS"}) // never used in GHS
	fmt.Println(errors.Is(err, ledger.ErrNotFound))
	// end
	end()
}
