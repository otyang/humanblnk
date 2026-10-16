package source_test

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

func TestStatements(t *testing.T) {
	_, core, wallet, _ := setup(t, ledger.Config{})
	lagos := time.FixedZone("WAT", 3600)
	core.SetNow(time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)) // Ada joined in August
	ada := customer(wallet, "user_8812", "Ada", currency.NGN)
	adaNGN := ada.Accounts[currency.NGN].ID
	tunde := customer(wallet, "user_9034", "Tunde", currency.NGN)
	tundeNGN := tunde.Accounts[currency.NGN].ID
	shop := customer(wallet, "merchant_311", "Chidi Stores", currency.NGN)
	chidiNGN := shop.Accounts[currency.NGN].ID
	rent, err := wallet.OpenPot(ctx, ledger.PotInput{ParentID: adaNGN, Name: "Rent"})
	check(err)

	fund(wallet, adaNGN, "15000", currency.NGN, "paystack_T100100")

	// September, day by day.
	at := func(day, hour int) { core.SetNow(time.Date(2026, 9, day, hour, 0, 0, 0, time.UTC)) }
	at(1, 9)
	_, err = wallet.Deposit(ctx, ledger.DepositInput{AccountID: adaNGN, Amount: ledger.Amount("50000", currency.NGN),
		Fee: ledger.Amount("750", ""), Reference: "paystack_T100201", Provider: "paystack", Description: "Card top-up"})
	check(err)
	at(3, 12)
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: tundeNGN, Amount: ledger.Amount("5000", currency.NGN),
		Fee: ledger.Amount("10", ""), Reference: "p2p_20001", Description: "To Tunde: suya money"})
	check(err)
	at(5, 15)
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: chidiNGN, Amount: ledger.Amount("12500", currency.NGN),
		Fee: ledger.NoFee, Reference: "order_5101", Description: "Chidi Stores order 5101"})
	check(err)
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: rent.ID, Amount: ledger.Amount("20000", currency.NGN),
		Fee: ledger.NoFee, Reference: "rent_sep", Description: "Rent pot",
		ScheduledFor: time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)}) // created today, posts on the 25th
	check(err)
	at(10, 11)
	w, err := wallet.Withdraw(ctx, ledger.WithdrawInput{AccountID: adaNGN, Amount: ledger.Amount("10000", currency.NGN),
		Fee: ledger.Amount("50", ""), Reference: "payout_7001", Provider: "flutterwave", Description: "To GTBank"})
	check(err)
	at(11, 9)
	_, err = wallet.Confirm(ctx, w.TransactionID, nil) // Flutterwave reports success the next day
	check(err)
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: tundeNGN, To: adaNGN, Amount: ledger.Amount("2500", currency.NGN),
		Fee: ledger.NoFee, Reference: "p2p_20002", Description: "From Tunde: half of the cab"})
	check(err)
	at(18, 14)
	_, err = wallet.Withdraw(ctx, ledger.WithdrawInput{AccountID: adaNGN, Amount: ledger.Amount("3000", currency.NGN),
		Fee: ledger.NoFee, Reference: "payout_7002", Provider: "flutterwave"}) // never confirmed: expires after 24 hours
	check(err)

	begin("statements/history")
	// sample: statements/history
	page, err := wallet.History(ctx, adaNGN, ledger.HistoryQuery{Limit: 5}) // newest first
	check(err)
	fmt.Println("total:", page.Total)
	for _, r := range page.Items {
		fmt.Println(r.CreatedAt.In(lagos).Format("02 Jan"), r.Reference, r.EntryStatus, r.Amount.Display, r.Description)
	}
	// end
	end()

	begin("statements/filter")
	// sample: statements/filter
	in, err := wallet.History(ctx, adaNGN, ledger.HistoryQuery{
		Direction: ledger.Incoming,                           // or ledger.Outgoing; default ledger.All
		Statuses:  []string{"posted"},                        // or Blnk's "APPLIED"; empty means all
		Since:     time.Date(2026, 9, 1, 0, 0, 0, 0, lagos),  // created at or after
		Until:     time.Date(2026, 10, 1, 0, 0, 0, 0, lagos), // created before
	})
	check(err)
	for _, r := range in.Items {
		fmt.Println("in:", r.Reference, r.Amount.Display, r.Description)
	}

	planned, err := wallet.History(ctx, adaNGN, ledger.HistoryQuery{Statuses: []string{"scheduled"}})
	check(err)
	for _, r := range planned.Items {
		fmt.Println("scheduled:", r.Reference, r.Amount.Display, "for", r.Raw.ScheduledFor.In(lagos).Format("02 Jan 15:04"))
	}

	second, err := wallet.History(ctx, adaNGN, ledger.HistoryQuery{Limit: 5, Offset: 5})
	check(err)
	fmt.Println("page 2:", len(second.Items), "of", second.Total)
	// end
	end()

	at(25, 9) // the rent transfer posts on the 25th
	core.SetNow(time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC))
	fund(wallet, adaNGN, "8000", currency.NGN, "paystack_T100301") // October

	begin("statements/statement")
	// sample: statements/statement
	// Ada downloads her September statement.
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, lagos)
	to := from.AddDate(0, 1, 0) // up to, not including, 1 October
	st, err := wallet.Statement(ctx, adaNGN, from, to)
	check(err)
	fmt.Println("opening", st.Opening.Display, "in", st.Credits.Display, "out", st.Debits.Display, "closing", st.Closing.Display)
	for _, l := range st.Lines {
		amount := "+" + l.Credit.Display
		if l.Debit.IsPositive() {
			amount = "-" + l.Debit.Display
		}
		fmt.Println(l.Date.In(lagos).Format("02 Jan"), l.Description, "|", l.Counterpart, "|", amount, "=", l.Balance.Display)
	}
	// end
	end()

	begin("statements/csv")
	// sample: statements/csv
	check(st.WriteCSV(os.Stdout)) // or an http.ResponseWriter, a file, a bytes.Buffer
	// end
	end()

	begin("statements/balance-at")
	// sample: statements/balance-at
	for _, moment := range []time.Time{
		time.Date(2026, 9, 1, 0, 0, 0, 0, lagos),   // start of September
		time.Date(2026, 9, 10, 18, 0, 0, 0, lagos), // withdrawal pending: not counted
		time.Date(2026, 9, 11, 18, 0, 0, 0, lagos), // confirmed
		time.Date(2026, 10, 1, 0, 0, 0, 0, lagos),  // month end
	} {
		b, err := wallet.BalanceAt(ctx, adaNGN, moment)
		check(err)
		fmt.Println(moment.Format("02 Jan 15:04"), b.Display)
	}
	now, err := wallet.GetAccount(ctx, adaNGN)
	check(err)
	fmt.Println("now", now.Balance.Display, "available", now.Available.Display)
	// end
	end()

	begin("statements/errors")
	// sample: statements/errors
	_, err = wallet.Statement(ctx, adaNGN, to, from) // backwards
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	_, err = wallet.Statement(ctx, ledger.AccountID(ledger.FeeIncomeAccount), from, to)
	fmt.Println(errors.Is(err, ledger.ErrWrongKind), err)

	_, err = wallet.History(ctx, "bln_doesnotexist", ledger.HistoryQuery{})
	fmt.Println(errors.Is(err, ledger.ErrNotFound))
	// end
	end()
}
