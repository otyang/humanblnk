package humanblnk_test

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/money"
	"github.com/otyang/humanblnk/money/currency"
)

var ctx = context.Background()

// A bank transfer arrives through Paystack: the provider owes HumanBlnk until it
// settles, so the money comes from its clearing account.
func Example_deposit() {
	hb := humanblnk.New(blnk.New("https://blnk.internal"), humanblnk.WithAccountLedger("ldg_wallets"))
	ngn := currency.MustGet(currency.NGN)
	amount := money.MustParse("50000", ngn)

	_, err := hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref:         humanblnk.Reference("deposit", "paystack", "ps_ref_123"), // the provider's id
		From:        humanblnk.Single(humanblnk.ClearingAccount("paystack"), amount),
		To:          humanblnk.Single("bln_ada_ngn", amount),
		Description: "Bank transfer",
	})
	if errors.Is(err, humanblnk.ErrDuplicate) {
		err = nil // the webhook was delivered twice; it's already recorded
	}
	_ = err
}

// A transfer with a fee is one atomic split: Bola is never paid without
// HumanBlnk's fee being recorded, or the other way round.
func Example_transferWithFee() {
	hb := humanblnk.New(blnk.New("https://blnk.internal"))
	usd := currency.MustGet(currency.USD)
	amount := money.MustParse("100", usd)
	fee, _ := amount.Percent(decimal.RequireFromString("1.5"), money.Up) // a business rule
	total, _ := amount.Add(fee)

	_, _ = hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref:  humanblnk.Reference("transfer", "tr_42"),
		From: humanblnk.Single("bln_ada_usd", total),
		To: humanblnk.Many(
			humanblnk.Posting{Account: "bln_bola_usd", Amount: amount},
			humanblnk.Posting{Account: humanblnk.FeeIncome, Amount: fee},
		),
		Description: "Transfer to Bola",
	})
}

// A payout reserves the money while the bank sends it, then settles or
// returns it when the provider reports back.
func Example_payoutHold() {
	hb := humanblnk.New(blnk.New("https://blnk.internal"))
	amount := money.MustParse("250", currency.MustGet(currency.USD))

	hold, err := hb.Holds.Place(ctx, humanblnk.Transaction{
		Ref:         humanblnk.Reference("payout", "po_7"),
		From:        humanblnk.Single("bln_ada_usd", amount),
		To:          humanblnk.Single(humanblnk.World, amount),
		Description: "Payout to bank",
	}, time.Now().Add(72*time.Hour))
	if err != nil {
		return
	}

	bankAccepted := true // from the provider's webhook
	if bankAccepted {
		_, _ = hb.Holds.Capture(ctx, hold, money.Money{}) // all of it
	} else {
		_ = hb.Holds.Cancel(ctx, hold)
	}
}
