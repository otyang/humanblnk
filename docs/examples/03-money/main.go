// Chapter 3: exact amounts with money. No Blnk needed.
package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/otyang/humanblnk/money"
	"github.com/otyang/humanblnk/money/currency"
	"github.com/shopspring/decimal"
)

func main() {
	// Look the currency up once. Get is for codes people type; Lookup for
	// codes from storage.
	// snippet:currency
	ngn, ok := currency.Get("NGN")
	if !ok {
		panic("no NGN")
	}
	usd := currency.MustGet(currency.USD) // for constants you know exist
	// end-snippet

	// Parse what a person or client wrote.
	// snippet:parse
	price, err := money.Parse("1500.50", ngn)
	fmt.Println("price:", price, price.Code(), err)

	for _, bad := range []string{"1500.505", "1e3", "1,500"} {
		_, err := money.Parse(bad, ngn)
		fmt.Printf("Parse(%q): %v\n", bad, err)
	}
	// end-snippet

	// A 1.5% fee, rounded up: you never undercharge by a fraction of a kobo.
	// snippet:arith
	fee, _ := price.Percent(decimal.RequireFromString("1.5"), money.Up)
	total, _ := price.Add(fee)
	fmt.Println("fee:", fee, "| total:", total)

	// Only one currency at a time.
	_, err = price.Add(money.MustParse("10", usd))
	fmt.Println("NGN + USD:", err, "| is ErrCurrencyMismatch:", errors.Is(err, money.ErrCurrencyMismatch))

	// Compare with Cmp and Equal, never ==.
	cmp, _ := total.Cmp(price)
	fmt.Println("total > price:", cmp > 0, "| 1.5 equals 1.50:", money.MustParse("1.5", usd).Equal(money.MustParse("1.50", usd)))
	// end-snippet

	// The smallest unit, as a ledger stores it.
	// snippet:minor
	minor, _ := total.Minor()
	fmt.Println("in kobo:", minor, "| precision:", total.Precision())
	back, _ := money.FromMinor(minor, ngn)
	fmt.Println("back from kobo:", back)
	// end-snippet

	// Rounding only where you ask, in a direction you choose.
	// snippet:round
	share := decimal.RequireFromString("100").Div(decimal.NewFromInt(3)) // 33.333…
	payout, _ := money.Round(share, ngn, money.Down)                     // pay out: round down
	fmt.Println("a third of 100, paid out:", payout)
	// end-snippet

	// JSON is exact both ways.
	// snippet:json
	b, _ := json.Marshal(total)
	fmt.Println("JSON:", string(b))
	var decoded money.Money
	err = json.Unmarshal([]byte(`{"amount":"99.99","currency":"USD"}`), &decoded)
	fmt.Println("decoded:", decoded, decoded.Code(), err)
	// end-snippet
}
