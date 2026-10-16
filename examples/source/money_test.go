package source_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

func TestMoney(t *testing.T) {
	svc, _, wallet, _ := setup(t, ledger.Config{})
	ada := customer(wallet, "user_8812", "Ada", currency.NGN, currency.USD, currency.BTC)
	adaNGN := ada.Accounts[currency.NGN].ID

	begin("money/amount")
	// sample: money/amount
	typed := ledger.Amount("0.456", "btc") // what the user typed; currency upper-cased
	fmt.Println(typed.Value, typed.Currency, "resolved:", typed.PreciseAmount != nil)

	m, err := svc.Parse("0.456", currency.BTC) // fills both forms from the currency's precision
	check(err)
	fmt.Println("Value:", m.Value, "PreciseAmount:", m.PreciseAmount, "Precision:", m.Precision, "Display:", m.Display)
	fmt.Println("String:", m.String(), "Fixed:", m.Fixed())
	// end
	end()

	begin("money/currencies")
	// sample: money/currencies
	for _, in := range [][2]string{
		{"100.56", currency.USD}, {"10000", currency.NGN}, {"1,500.50", currency.NGN}, {"5000", currency.JPY},
		{"1.250", currency.KWD}, {"20.5", currency.USDT}, {"0.00012345", currency.BTC},
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
		AccountID: adaNGN, Amount: ledger.Minor(1050075, currency.NGN), // ₦10,500.75
		Fee: ledger.NoFee, Reference: "paystack_T1001", Provider: "paystack",
	})
	check(err)
	fmt.Println("Minor:", r.Amount.Value, r.Amount.Display)

	// You hold the integer and the precision:
	p := ledger.Precise(big.NewInt(45600000), 100000000, currency.BTC)
	fmt.Println("Precise:", p.Value, p.PreciseAmount, p.Precision, p.Display)

	// A decimal against a precision you choose, without a Service:
	d, err := ledger.ParseDecimal("12.3456", "OJA", 10000)
	check(err)
	fmt.Println("ParseDecimal:", d.Value, d.PreciseAmount, d.Precision, d.Display)
	// end
	end()

	begin("money/rejected")
	// sample: money/rejected
	for _, in := range []string{"100.567", "1e3", "12abc", ""} {
		_, err := svc.Parse(in, currency.NGN)
		fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)
	}

	_, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN, Amount: ledger.Amount("250.505", currency.NGN),
		Fee: ledger.NoFee, Reference: "paystack_T1002", Provider: "paystack",
	})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)
	// end
	end()

	begin("money/fee")
	// sample: money/fee
	r, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN,
		Amount:    ledger.Amount("50000", currency.NGN),
		Fee:       ledger.Amount("750", ""), // empty currency: takes the amount's (NGN)
		Reference: "paystack_T1003",
		Provider:  "paystack",
	})
	check(err)
	fmt.Println("gross:", r.Amount.Display, "fee:", r.MetaData["fee"], "net:", r.MetaData["net_amount"])

	_, err = wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN, Amount: ledger.Amount("50000", currency.NGN),
		Fee: ledger.Amount("2", currency.USD), Reference: "paystack_T1004", Provider: "paystack",
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
		AccountID: ada.Accounts[currency.USD].ID, Amount: req.Amount,
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
		Currencies: []currency.Currency{
			// Start from the built-in currency and change what you need.
			currency.MustGet(currency.ETH).WithDisplay(currency.Display{Decimals: 4, TrimZeros: true}),
			currency.MustGet(currency.NGN).WithDisplay(currency.Display{Decimals: 0}), // whole naira on screen
		},
	})
	for _, in := range [][2]string{
		{"1250.75", currency.NGN}, {"100.5", currency.USD}, {"0.123456789", currency.ETH},
		{"0.5", currency.BTC}, {"20.5", currency.USDT},
	} {
		m, err := svc.Parse(in[0], in[1])
		check(err)
		fmt.Printf("%-12s %-4s Value=%-12s Display=%s\n", in[0], in[1], m.Value, m.Display)
	}

	acct, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.ETH})
	check(err)
	fmt.Println("rule for ETH:", svc.DisplayRule(currency.ETH), "balance shows:", acct.Balance.Display)
	// end
	end()

	begin("money/format")
	// sample: money/format
	eth, err := svc.Parse("0.123456789", currency.ETH)
	check(err)
	fiat := currency.Display{Decimals: 2}
	crypto := currency.Display{Decimals: 6, TrimZeros: true}
	fmt.Println(eth.Format(fiat), eth.Format(crypto), eth.Format(currency.MustGet(currency.ETH).Display))

	// Without a Money: smallest units and their precision.
	fmt.Println(fiat.Format(big.NewInt(1050075), 100))
	// end
	end()
}

func TestMoneyCurrencies(t *testing.T) {
	_, core, _, _ := setup(t, ledger.Config{})
	client, ledgerID := core.Client(), ledger.LedgerID(core.CreateLedger("Oja Nigeria"))

	begin("money/package")
	// sample: money/package
	ngn := currency.MustGet(currency.NGN) // a copy: the built-in list can't be changed
	fmt.Println(ngn.Code, ngn.Name, ngn.Symbol, ngn.Kind, ngn.Precision, ngn.Decimals(), ngn.Display)

	btc, ok := currency.Get("btc") // any case
	fmt.Println(ok, btc.Name, btc.Symbol, btc.Kind, btc.Precision, btc.Decimals(), btc.Display)

	fmt.Println("fiat:", len(currency.All(currency.Fiat)), "all:", len(currency.All()))
	for _, c := range currency.All(currency.Stablecoin) {
		fmt.Println(" ", c.Code, c.Name)
	}

	_, ok = currency.Get("NGM") // a typo; currency.NGM would not compile
	fmt.Println("NGM built in:", ok)
	// end
	end()

	begin("money/currency-json")
	// sample: money/currency-json
	b, err := json.Marshal(currency.MustGet(currency.NGN))
	check(err)
	fmt.Println(string(b))
	// end
	end()

	begin("money/precisions")
	// sample: money/precisions
	svc := ledger.New(client, ledger.Config{
		LedgerID: ledgerID,
		Currencies: []currency.Currency{
			// Your own: code, name, kind, symbol, precision.
			currency.New("OJA", "Oja Points", currency.Fiat, "pts", 10000),
			// A built-in one, renamed.
			currency.MustGet(currency.USDT).WithName("Tether"),
		},
	})
	oja, _ := svc.Currency("OJA")
	usdt, _ := svc.Currency(currency.USDT)
	fmt.Println(oja.Name, oja.Kind, oja.Precision, oja.Display)
	fmt.Println(usdt.Name, usdt.Precision, usdt.Display)
	fmt.Println("built in is unchanged:", currency.MustGet(currency.USDT).Name)

	pts, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "OJA"})
	check(err)
	fmt.Println("account precision:", pts.Precision, "shows:", pts.Balance.Display)
	// end
	end()

	adaNGN, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.NGN})
	check(err)
	fund(svc.Wallet(), adaNGN.ID, "15250.50", currency.NGN, "paystack_T1")

	begin("money/lookup")
	// sample: money/lookup
	acct, err := svc.Wallet().GetAccount(ctx, adaNGN.ID)
	check(err)
	c, _ := svc.Currency(acct.Currency) // what this service uses, overrides included
	fmt.Printf("%s%s (%s)\n", c.Symbol, acct.Balance.Display, c.Name)
	fmt.Println("currencies this service knows:", len(svc.Currencies()))
	// end
	end()

	begin("money/unknown")
	// sample: money/unknown
	_, err = svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGM"})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	_, err = svc.Parse("100", "NGM")
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput))

	open := ledger.New(client, ledger.Config{LedgerID: ledgerID, UnknownCurrency: ledger.AllowUnknown})
	acc, err := open.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGM"})
	check(err)
	fmt.Println("allowed:", acc.Currency, acc.Precision) // Config.DefaultPrecision
	// end
	end()

	begin("money/config-errors")
	// sample: money/config-errors
	bad := ledger.New(client, ledger.Config{
		LedgerID: ledgerID,
		Currencies: []currency.Currency{
			{Code: "GEM", Name: "Gem"},                               // no Precision
			currency.New("PTS", "Points", currency.Fiat, "pts", 250), // not a power of ten
			currency.MustGet(currency.BTC),
			currency.MustGet(currency.BTC), // listed twice
		},
	})
	err = bad.Check(ctx) // at start-up
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput))
	fmt.Println(err)
	// end
	end()
}
