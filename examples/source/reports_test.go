package source_test

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"testing"
	"time"

	"github.com/otyang/humanblnk/ledger"
)

func TestReports(t *testing.T) {
	svc, core, wallet, ops := setup(t, ledger.Config{})

	begin("reports/activity")
	// sample: reports/activity
	// A day at Oja, 1 September.
	ada := customer(wallet, "user_8812", "Ada", "NGN")
	tunde := customer(wallet, "user_9034", "Tunde", "NGN")
	chidi := customer(wallet, "merchant_311", "Chidi Stores", "NGN")
	adaNGN, tundeNGN, chidiNGN := ada.Accounts["NGN"].ID, tunde.Accounts["NGN"].ID, chidi.Accounts["NGN"].ID

	// Money in through Paystack, ₦750 and ₦450 fees.
	_, err := wallet.Deposit(ctx, ledger.DepositInput{AccountID: adaNGN, Amount: ledger.Amount("50000", "NGN"),
		Fee: ledger.Amount("750", ""), Reference: "paystack_T100", Provider: "paystack"})
	check(err)
	_, err = wallet.Deposit(ctx, ledger.DepositInput{AccountID: tundeNGN, Amount: ledger.Amount("30000", "NGN"),
		Fee: ledger.Amount("450", ""), Reference: "paystack_T200", Provider: "paystack"})
	check(err)

	// Ada pays Tunde; Tunde pays Chidi Stores.
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: tundeNGN, Amount: ledger.Amount("12500", "NGN"),
		Fee: ledger.Amount("25", ""), Reference: "p2p_001"})
	check(err)
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: tundeNGN, To: chidiNGN, Amount: ledger.Amount("8000", "NGN"),
		Fee: ledger.NoFee, Reference: "pay_order_10450"})
	check(err)

	// Chidi Stores cashes out ₦5,000 through Flutterwave; Flutterwave confirms.
	w, err := wallet.Withdraw(ctx, ledger.WithdrawInput{AccountID: chidiNGN, Amount: ledger.Amount("5000", "NGN"),
		Fee: ledger.Amount("50", ""), Reference: "payout_7001", Provider: "flutterwave"})
	check(err)
	_, err = wallet.Confirm(ctx, w.TransactionID, nil)
	check(err)

	// Ada gets ₦500 cashback.
	_, err = ops.Adjust(ctx, ledger.AdjustInput{AccountID: adaNGN, Type: ledger.AdjustCredit,
		Amount: ledger.Amount("500", "NGN"), Reason: "Cashback: September promo",
		RequestedBy: "ops.kemi", ApprovedBy: "lead.femi", Reference: "adj_cashback_ada_09"})
	check(err)

	// Next day: Paystack settles Ada's ₦50,000 (keeping ₦750), the bank funds Flutterwave.
	core.Advance(24 * time.Hour)
	_, err = ops.RecordSettlement(ctx, ledger.SettlementInput{Provider: "paystack", Direction: ledger.SettlementCollection,
		Amount: ledger.Amount("50000", "NGN"), Fee: ledger.Amount("750", ""), Reference: "paystack_stl_0902"})
	check(err)
	_, err = ops.RecordSettlement(ctx, ledger.SettlementInput{Provider: "flutterwave", Direction: ledger.SettlementPayout,
		Amount: ledger.Amount("5000", "NGN"), Reference: "flw_stl_0902"})
	check(err)

	for _, id := range []ledger.AccountID{adaNGN, tundeNGN, chidiNGN} {
		a, err := wallet.GetAccount(ctx, id)
		check(err)
		fmt.Println(a.MetaData["external_id"], a.Balance.Display)
	}
	// end
	end()

	begin("reports/trial")
	// sample: reports/trial
	tb, err := ops.TrialBalance(ctx, "NGN")
	check(err)
	fmt.Println("balanced:", tb.Balanced, "net:", tb.Net.Display)
	fmt.Println("customer funds:", tb.CustomerFunds.Display, "in", tb.CustomerAccounts, "accounts")

	// House is in creation order; sort it for a stable report.
	sort.Slice(tb.House, func(i, j int) bool { return tb.House[i].Indicator < tb.House[j].Indicator })
	for _, h := range tb.House {
		fmt.Printf("%-21s %-9s %-6s natural %10s   blnk %10s\n",
			h.Indicator, h.Type, h.Normal, h.Natural.Display, h.Balance.Display)
	}
	// end
	end()

	begin("reports/nightly")
	// sample: reports/nightly
	// Run once a night, for every currency you hold.
	for _, cur := range []string{"NGN", "USD"} {
		tb, err := ops.TrialBalance(ctx, cur)
		check(err)
		if !tb.Balanced {
			// Entries were lost or posted outside humanblnk: page someone.
			fmt.Println(cur, "OUT OF BALANCE by", tb.Net.Display)
			continue
		}
		fmt.Println(cur, "balanced,", tb.CustomerAccounts, "customer accounts, funds", tb.CustomerFunds.Display)
	}
	// end
	end()

	begin("reports/safeguarding")
	// sample: reports/safeguarding
	tb, err = ops.TrialBalance(ctx, "NGN")
	check(err)
	// Net house assets: what Oja holds or is owed, less what it owes others.
	net := new(big.Int)
	for _, h := range tb.House {
		switch h.Type {
		case ledger.GLAsset: // @BankOperating, @PaystackClearing, ...
			net.Add(net, h.Natural.PreciseAmount)
		case ledger.GLLiability: // @PayoutClearing, @Suspense, ...
			net.Sub(net, h.Natural.PreciseAmount)
		}
	}
	own := new(big.Int).Sub(net, tb.CustomerFunds.PreciseAmount)
	fmt.Println("net house assets:", svc.Display(ledger.Precise(net, 100, "NGN")))
	fmt.Println("owed to customers:", tb.CustomerFunds.Display)
	fmt.Println("Oja's own money:  ", svc.Display(ledger.Precise(own, 100, "NGN")))
	// end
	end()

	// Some October activity, so month-end figures differ from today's.
	core.SetNow(time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC))
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: tundeNGN, To: adaNGN, Amount: ledger.Amount("1000", "NGN"),
		Fee: ledger.Amount("25", ""), Reference: "p2p_002"})
	check(err)

	begin("reports/month-end")
	// sample: reports/month-end
	sepStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	octStart := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	fees := ledger.HouseRef{Indicator: ledger.FeeIncomeAccount, Currency: "NGN"}

	atMonthEnd, err := ops.HouseBalanceAt(ctx, fees, octStart)
	check(err)
	now, err := ops.GetHouseAccount(ctx, fees)
	check(err)
	fmt.Println("fee income at 30 Sep:", atMonthEnd.Display, " today:", now.Balance.Display)

	st, err := ops.HouseStatement(ctx, fees, sepStart, octStart)
	check(err)
	fmt.Println(st.Indicator, "opening", st.Opening.Display, "credits", st.Credits.Display,
		"debits", st.Debits.Display, "closing", st.Closing.Display)
	for _, l := range st.Lines {
		fmt.Printf("  %s  %-16s %-18s +%s\n", l.Date.Format("02 Jan"), l.Description, l.Counterpart, l.Credit.Display)
	}
	// end
	end()

	begin("reports/clearing")
	// sample: reports/clearing
	// What Paystack still owes at month-end, to compare with its own report.
	psk := ledger.HouseRef{Indicator: ledger.ClearingAccount("paystack"), Currency: "NGN"}
	owed, err := ops.HouseBalanceAt(ctx, psk, octStart)
	check(err)
	fmt.Println(psk.Indicator, "as Blnk stores it:", owed.Display)

	h, err := ops.GetHouseAccount(ctx, psk)
	check(err)
	fmt.Println(h.Type, h.Normal, "so Paystack owes", h.Natural.Display)
	// end
	end()

	begin("reports/customer")
	// sample: reports/customer
	ast, err := wallet.Statement(ctx, adaNGN, sepStart, octStart)
	check(err)
	fmt.Println("Ada, September: opening", ast.Opening.Display, "closing", ast.Closing.Display)
	for _, l := range ast.Lines {
		fmt.Printf("  %-22s in %9s  out %9s  balance %9s\n", l.Reference, l.Credit.Display, l.Debit.Display, l.Balance.Display)
	}
	// end
	end()

	begin("reports/errors")
	// sample: reports/errors
	_, err = ops.TrialBalance(ctx, "")
	fmt.Println("no currency:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.HouseStatement(ctx, ledger.HouseRef{Indicator: "FeeIncome", Currency: "NGN"}, sepStart, octStart)
	fmt.Println("no @:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.HouseBalanceAt(ctx, ledger.HouseRef{Indicator: "@Suspense", Currency: "NGN"}, octStart)
	fmt.Println("never used:", errors.Is(err, ledger.ErrNotFound))

	_, err = ops.HouseStatement(ctx, fees, octStart, sepStart)
	fmt.Println("from after to:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.HouseStatement(ctx, ledger.HouseRef{Indicator: string(adaNGN), Currency: "NGN"}, sepStart, octStart)
	fmt.Println("customer account:", errors.Is(err, ledger.ErrWrongKind))
	// end
	end()
}
