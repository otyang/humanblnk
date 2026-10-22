package ledger_test

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/money"
	"github.com/otyang/humanblnk/money/currency"
)

var ctx = context.Background()

// A bank transfer arrives through Paystack: the provider owes HumanBlnk until it
// settles, so the money comes from its clearing account.
func Example_deposit() {
	books := ledger.New(blnk.New("https://blnk.internal"), ledger.WithAccountLedger("ldg_wallets"))
	ngn := currency.MustGet(currency.NGN)
	amount := money.MustParse("50000", ngn)

	_, err := books.Transactions.Record(ctx, ledger.Transaction{
		Ref:         ledger.Reference("deposit", "paystack", "ps_ref_123"), // the provider's id
		From:        ledger.Single(ledger.ClearingAccount("paystack"), amount),
		To:          ledger.Single("bln_ada_ngn", amount),
		Description: "Bank transfer",
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		err = nil // the webhook was delivered twice; it's already recorded
	}
	_ = err
}

// A transfer with a fee is one atomic split: Bola is never paid without
// HumanBlnk's fee being recorded, or the other way round.
func Example_transferWithFee() {
	books := ledger.New(blnk.New("https://blnk.internal"))
	usd := currency.MustGet(currency.USD)
	amount := money.MustParse("100", usd)
	fee, _ := amount.Percent(decimal.RequireFromString("1.5"), money.Up) // a business rule
	total, _ := amount.Add(fee)

	_, _ = books.Transactions.Record(ctx, ledger.Transaction{
		Ref:  ledger.Reference("transfer", "tr_42"),
		From: ledger.Single("bln_ada_usd", total),
		To: ledger.Many(
			ledger.Posting{Account: "bln_bola_usd", Amount: amount},
			ledger.Posting{Account: ledger.FeeIncome, Amount: fee},
		),
		Description: "Transfer to Bola",
	})
}

// A payout reserves the money while the bank sends it, then settles or
// returns it when the provider reports back.
func Example_payoutHold() {
	books := ledger.New(blnk.New("https://blnk.internal"))
	amount := money.MustParse("250", currency.MustGet(currency.USD))

	hold, err := books.Holds.Place(ctx, ledger.Transaction{
		Ref:         ledger.Reference("payout", "po_7"),
		From:        ledger.Single("bln_ada_usd", amount),
		To:          ledger.Single(ledger.World, amount),
		Description: "Payout to bank",
	}, time.Now().Add(72*time.Hour))
	if err != nil {
		return
	}

	bankAccepted := true // from the provider's webhook
	if bankAccepted {
		_, _ = books.Holds.Capture(ctx, hold, money.Money{}) // all of it
	} else {
		_ = books.Holds.Cancel(ctx, hold)
	}
}
