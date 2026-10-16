package source_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/otyang/humanblnk/ledger"
)

func TestMoney(t *testing.T) {
	svc, _, wallet, _ := setup(t, ledger.Config{})
	ada := customer(wallet, "user_8812", "Ada", "NGN", "USD", "BTC")
	adaNGN := ada.Accounts["NGN"].ID

	begin("money/amount")
	// sample: money/amount
	typed := ledger.Amount("0.456", "btc") // what the user typed; currency upper-cased
	fmt.Println(typed.Value, typed.Currency, "resolved:", typed.PreciseAmount != nil)

	m, err := svc.Parse("0.456", "BTC") // fills both forms from the currency's precision
	check(err)
	fmt.Println("Value:", m.Value, "PreciseAmount:", m.PreciseAmount, "Precision:", m.Precision)
	fmt.Println("String:", m.String(), "Fixed:", m.Fixed())
	// end
	end()

	begin("money/currencies")
	// sample: money/currencies
	for _, in := range [][2]string{
		{"100.56", "USD"}, {"10000", "NGN"}, {"1,500.50", "NGN"}, {"5000", "JPY"},
		{"1.250", "KWD"}, {"20.5", "USDT"}, {"0.00012345", "BTC"},
	} {
		m, err := svc.Parse(in[0], in[1])
		check(err)
		fmt.Printf("%-12s %-5s precise=%-10s precision=%d\n", in[0], m.Currency, m.PreciseAmount, m.Precision)
	}
	// end
	end()

	begin("money/other-forms")
	// sample: money/other-forms
	// You already hold the integer in the smallest unit:
	r, err := wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN, Amount: ledger.Minor(1050075, "NGN"), // ₦10,500.75
		Fee: ledger.NoFee, Reference: "paystack_T1001", Provider: "paystack",
	})
	check(err)
	fmt.Println("Minor:", r.Amount.Value, r.Amount.Display)

	// You hold the integer and the precision:
	p := ledger.Precise(big.NewInt(45600000), 100000000, "BTC")
	fmt.Println("Precise:", p.Value, p.PreciseAmount, p.Precision)

	// A decimal against a precision you choose, without a Service:
	d, err := ledger.ParseDecimal("12.3456", "OJA", 10000)
	check(err)
	fmt.Println("ParseDecimal:", d.Value, d.PreciseAmount, d.Precision)
	// end
	end()

	begin("money/rejected")
	// sample: money/rejected
	for _, in := range []string{"100.567", "1e3", "12abc"} {
		_, err := svc.Parse(in, "NGN")
		fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)
	}

	_, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN, Amount: ledger.Amount("250.505", "NGN"),
		Fee: ledger.NoFee, Reference: "paystack_T1002", Provider: "paystack",
	})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)
	// end
	end()

	begin("money/fee")
	// sample: money/fee
	r, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN,
		Amount:    ledger.Amount("50000", "NGN"),
		Fee:       ledger.Amount("750", ""), // empty currency: takes the amount's (NGN)
		Reference: "paystack_T1003",
		Provider:  "paystack",
	})
	check(err)
	fmt.Println("gross:", r.Amount.Display, "fee:", r.MetaData["fee"], "net:", r.MetaData["net_amount"])

	_, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN, Amount: ledger.Amount("50000", "NGN"),
		Fee: ledger.Amount("2", "USD"), Reference: "paystack_T1004", Provider: "paystack",
	})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)
	fmt.Println("NoFee is zero:", ledger.NoFee.IsZero())
	// end
	end()

	begin("money/json")
	// sample: money/json
	out, err := json.Marshal(r.Amount) // e.g. in your API response
	check(err)
	fmt.Println(string(out))

	// What a client sends: amount as a string, or as a bare JSON number.
	var req struct {
		Amount ledger.Money `json:"amount"`
	}
	check(json.Unmarshal([]byte(`{"amount": {"amount": 0.1, "currency": "usd"}}`), &req))
	fmt.Println("decoded:", req.Amount.Value, req.Amount.Currency)

	tr, err := wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: ada.Accounts["USD"].ID, Amount: req.Amount,
		Fee: ledger.NoFee, Reference: "paystack_T1005", Provider: "paystack",
	})
	check(err)
	fmt.Println("posted:", tr.Amount.Value, tr.Amount.PreciseAmount, tr.Amount.Precision)
	// end
	end()
}

func TestMoneyDisplay(t *testing.T) {
	_, core, _, _ := setup(t, ledger.Config{})
	client, ledgerID := core.Client(), ledger.LedgerID(core.CreateLedger("Oja Nigeria"))

	begin("money/display")
	// sample: money/display
	svc := ledger.New(client, ledger.Config{
		LedgerID: ledgerID,
		Display: map[string]ledger.DisplayRule{
			"ETH": {Decimals: 4, TrimZeros: true}, // crypto style: cut to 4, drop trailing zeros
			"NGN": {Decimals: 0},                  // whole naira on screen
		},
	})
	for _, in := range [][2]string{
		{"1250.75", "NGN"}, {"100.5", "USD"}, {"0.123456789", "ETH"}, {"0.5", "BTC"}, {"20.5", "USDT"},
	} {
		m, err := svc.Parse(in[0], in[1])
		check(err)
		fmt.Printf("%-12s %-4s Value=%-12s Display=%s\n", in[0], in[1], m.Value, svc.Display(m))
	}

	acct, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "ETH"})
	check(err)
	fmt.Println("rule for ETH:", svc.DisplayRule("ETH"), "balance shows:", acct.Balance.Display)
	// end
	end()

	begin("money/format")
	// sample: money/format
	eth, err := svc.Parse("0.123456789", "ETH")
	check(err)
	fiat := ledger.DisplayRule{Decimals: 2}
	crypto := ledger.DisplayRule{Decimals: 6, TrimZeros: true}
	fmt.Println(fiat.Format(eth), crypto.Format(eth), ledger.BuiltinDisplay["ETH"].Format(eth))
	// end
	end()
}

func TestMoneyPrecisions(t *testing.T) {
	_, core, _, _ := setup(t, ledger.Config{})
	client, ledgerID := core.Client(), ledger.LedgerID(core.CreateLedger("Oja Nigeria"))

	begin("money/precisions")
	// sample: money/precisions
	svc := ledger.New(client, ledger.Config{
		LedgerID:         ledgerID,
		Precisions:       map[string]int64{"OJA": 10000, "NGN": 100}, // your own points token
		DefaultPrecision: 100,                                        // the default
	})
	fmt.Println("OJA:", svc.Precision("OJA"), "BTC:", svc.Precision("BTC"),
		"JPY:", svc.Precision("JPY"), "GHS:", svc.Precision("GHS"))
	fmt.Println("built in for ETH:", ledger.BuiltinPrecisions["ETH"])

	pts, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "OJA"})
	check(err)
	fmt.Println("account precision:", pts.Precision, "shows:", pts.Balance.Display)
	// end
	end()
}
