package source_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

func TestFreeze(t *testing.T) {
	_, core, wallet, ops := setup(t, ledger.Config{})
	adaNGN := customer(wallet, "user_ada", "Ada", currency.NGN).Accounts[currency.NGN]
	tundeNGN := customer(wallet, "user_tunde", "Tunde", currency.NGN).Accounts[currency.NGN]
	fund(wallet, adaNGN.ID, "50000", currency.NGN, "seed_ada")
	fund(wallet, tundeNGN.ID, "80000", currency.NGN, "seed_tunde")

	// Tunde asks for a payout before the alert fires.
	payout, err := wallet.Withdraw(ctx, ledger.WithdrawInput{AccountID: tundeNGN.ID, Amount: ledger.Amount("20000", currency.NGN),
		Fee: ledger.NoFee, Reference: "wd_tunde_501", Provider: "flutterwave"})
	check(err)
	// A card hold at Chidi Stores, and a standing order to Ada due tomorrow.
	shop := customer(wallet, "merchant_311", "Chidi", currency.NGN).Accounts[currency.NGN]
	cardHold, err := wallet.Hold(ctx, ledger.HoldInput{AccountID: tundeNGN.ID, Destination: shop.ID,
		Amount: ledger.Amount("10000", currency.NGN), Reference: "card_auth_9001"})
	check(err)
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: tundeNGN.ID, To: adaNGN.ID, Amount: ledger.Amount("2000", currency.NGN),
		Fee: ledger.NoFee, Reference: "so_tunde_rent_1", ScheduledFor: core.Now().Add(24 * time.Hour)})
	check(err)
	// Ada starts a pending ₦1,500 transfer to Tunde.
	incoming, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN.ID, To: tundeNGN.ID, Amount: ledger.Amount("1500", currency.NGN),
		Fee: ledger.NoFee, Reference: "p2p_ada_pending_9", Pending: true})
	check(err)

	begin("freeze/account")
	// sample: freeze/account
	// Fraud monitoring flags a login on Tunde's account from a new device.
	err = ops.FreezeAccount(ctx, tundeNGN.ID, "suspected account takeover")
	check(err)

	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: tundeNGN.ID, To: adaNGN.ID,
		Amount: ledger.Amount("5000", currency.NGN), Fee: ledger.NoFee, Reference: "p2p_tunde_77"})
	fmt.Println("send out:", errors.Is(err, ledger.ErrAccountFrozen))

	_, err = wallet.Withdraw(ctx, ledger.WithdrawInput{AccountID: tundeNGN.ID,
		Amount: ledger.Amount("5000", currency.NGN), Fee: ledger.NoFee, Reference: "wd_tunde_502", Provider: "flutterwave"})
	fmt.Println("cash out:", errors.Is(err, ledger.ErrAccountFrozen))

	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN.ID, To: tundeNGN.ID,
		Amount: ledger.Amount("1000", currency.NGN), Fee: ledger.NoFee, Reference: "p2p_ada_12"})
	fmt.Println("receive from Ada:", errors.Is(err, ledger.ErrAccountFrozen))

	_, err = wallet.Hold(ctx, ledger.HoldInput{AccountID: adaNGN.ID, Destination: tundeNGN.ID,
		Amount: ledger.Amount("1000", currency.NGN), Reference: "hold_ada_12"})
	fmt.Println("hold paying Tunde:", errors.Is(err, ledger.ErrAccountFrozen))

	_, err = wallet.Deposit(ctx, ledger.DepositInput{AccountID: tundeNGN.ID,
		Amount: ledger.Amount("3000", currency.NGN), Fee: ledger.NoFee, Reference: "paystack_T5501", Provider: "paystack"})
	fmt.Println("top up:", errors.Is(err, ledger.ErrAccountFrozen))
	fmt.Println(err)
	// end
	end()

	begin("freeze/status")
	// sample: freeze/status
	acct, err := wallet.GetAccount(ctx, tundeNGN.ID)
	check(err)
	fmt.Println("status:", acct.Status)
	fmt.Println("reason:", acct.MetaData["status_reason"])
	fmt.Println("since:", acct.MetaData["status_changed_at"])
	fmt.Println("balance:", acct.Balance.Display, "available:", acct.Available.Display)

	q, err := wallet.QuoteTransfer(ctx, ledger.TransferInput{From: tundeNGN.ID, To: adaNGN.ID,
		Amount: ledger.Amount("5000", currency.NGN), Fee: ledger.NoFee, Reference: "p2p_tunde_78"})
	check(err) // a refusal is not an error here: it is in the quote
	fmt.Println("quote allowed:", q.Allowed, "frozen:", errors.Is(q.Reason, ledger.ErrAccountFrozen))
	// end
	end()

	begin("freeze/pending")
	// sample: freeze/pending
	// Tunde's account is frozen, but four things were set up before the freeze.

	// 1. Flutterwave reports the ₦20,000 payout as paid.
	r, err := wallet.Confirm(ctx, payout.TransactionID, nil)
	check(err)
	fmt.Println("confirm payout:", r.Status)

	// 2. Chidi Stores voids the ₦10,000 card hold.
	r, err = wallet.Cancel(ctx, cardHold.TransactionID)
	check(err)
	fmt.Println("cancel card hold:", r.Status)

	// 3. Ada's pending ₦1,500 transfer to Tunde can't settle into a frozen account.
	_, err = wallet.Confirm(ctx, incoming.TransactionID, nil)
	fmt.Println("confirm transfer in:", errors.Is(err, ledger.ErrAccountFrozen))
	r, err = wallet.Cancel(ctx, incoming.TransactionID) // the money goes back to Ada
	check(err)
	fmt.Println("cancel transfer in:", r.Status)

	// 4. The ₦2,000 standing order to Ada falls due.
	core.Advance(24 * time.Hour)
	so, err := wallet.GetTransactionByReference(ctx, "so_tunde_rent_1")
	check(err)
	fmt.Println("scheduled transfer:", so.Status)

	acct, err = wallet.GetAccount(ctx, tundeNGN.ID)
	check(err)
	fmt.Println("status:", acct.Status, "balance:", acct.Balance.Display)
	// end
	end()

	begin("freeze/unfreeze")
	// sample: freeze/unfreeze
	// Tunde confirms the new device was his. Support lifts the freeze.
	err = ops.UnfreezeAccount(ctx, tundeNGN.ID)
	check(err)

	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: tundeNGN.ID, To: adaNGN.ID,
		Amount: ledger.Amount("5000", currency.NGN), Fee: ledger.NoFee, Reference: "p2p_tunde_77"})
	check(err)

	acct, err = wallet.GetAccount(ctx, tundeNGN.ID)
	check(err)
	fmt.Println("status:", acct.Status, "balance:", acct.Balance.Display)
	// end
	end()

	begin("freeze/ledger")
	// sample: freeze/ledger
	// Agent floats live in their own ledger. A regulator orders a hold on all of them.
	agents, err := ops.CreateLedger(ctx, ledger.CreateLedgerInput{Key: "oja-agents", Name: "Oja Agents"})
	check(err)
	ikeja, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.NGN, LedgerID: agents.ID, ExternalID: "agent_ikeja"})
	check(err)
	fund(wallet, ikeja.ID, "200000", currency.NGN, "seed_ikeja")

	err = ops.FreezeLedger(ctx, agents.ID, "CBN directive 2026/114")
	check(err)
	lg, err := ops.GetLedger(ctx, agents.ID)
	check(err)
	fmt.Println("ledger:", lg.Status, "-", lg.MetaData["status_reason"])

	// Out of the ledger: refused.
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: ikeja.ID, To: adaNGN.ID,
		Amount: ledger.Amount("1000", currency.NGN), Fee: ledger.NoFee, Reference: "float_out_1"})
	fmt.Println("agent pays Ada:", errors.Is(err, ledger.ErrLedgerFrozen))

	// Into the ledger: allowed.
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN.ID, To: ikeja.ID,
		Amount: ledger.Amount("1000", currency.NGN), Fee: ledger.NoFee, Reference: "float_in_1"})
	fmt.Println("Ada pays agent:", err == nil)
	_, err = wallet.Deposit(ctx, ledger.DepositInput{AccountID: ikeja.ID, Amount: ledger.Amount("50000", currency.NGN),
		Fee: ledger.NoFee, Reference: "paystack_T5520", Provider: "paystack"})
	fmt.Println("agent tops up:", err == nil)

	// House postings carry on.
	_, err = ops.RecordSettlement(ctx, ledger.SettlementInput{Provider: "paystack",
		Amount: ledger.Amount("100000", currency.NGN), Reference: "paystack_stl_0901"})
	fmt.Println("Paystack settlement:", err == nil)

	err = ops.UnfreezeLedger(ctx, agents.ID)
	check(err)
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: ikeja.ID, To: adaNGN.ID,
		Amount: ledger.Amount("1000", currency.NGN), Fee: ledger.NoFee, Reference: "float_out_1"})
	fmt.Println("after unfreeze, agent pays Ada:", err == nil)
	// end
	end()

	begin("freeze/ttl")
	// sample: freeze/ttl
	// The admin tool runs in its own process, with its own Service.
	admin := ledger.New(core.Client(), ledger.Config{Now: core.Now})

	// The API server moved money a moment ago, so it has the ledger cached as active.
	err = admin.Ops().FreezeLedger(ctx, agents.ID, "CBN directive 2026/115")
	check(err)

	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: ikeja.ID, To: adaNGN.ID,
		Amount: ledger.Amount("1000", currency.NGN), Fee: ledger.NoFee, Reference: "float_out_2"})
	fmt.Println("API server, same moment:", err == nil)

	core.Advance(31 * time.Second) // past the default 30s LedgerStatusTTL
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: ikeja.ID, To: adaNGN.ID,
		Amount: ledger.Amount("1000", currency.NGN), Fee: ledger.NoFee, Reference: "float_out_3"})
	fmt.Println("API server, 31s later:", errors.Is(err, ledger.ErrLedgerFrozen))
	// end
	end()

	begin("freeze/errors")
	// sample: freeze/errors
	// A closed account can't be frozen or unfrozen.
	old, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.NGN, ExternalID: "user_ada_old"})
	check(err)
	_, err = ops.CloseAccount(ctx, old.ID, ledger.CloseInput{Reason: "duplicate account",
		RequestedBy: "ops.kemi", ApprovedBy: "lead.femi"})
	check(err)
	err = ops.FreezeAccount(ctx, old.ID, "review")
	fmt.Println("freeze closed:", errors.Is(err, ledger.ErrAccountClosed))
	err = ops.UnfreezeAccount(ctx, old.ID)
	fmt.Println("unfreeze closed:", errors.Is(err, ledger.ErrAccountClosed))

	// House accounts and the general ledger are never frozen.
	err = ops.FreezeAccount(ctx, ledger.AccountID(ledger.FeeIncomeAccount), "review")
	fmt.Println("freeze @FeeIncome:", errors.Is(err, ledger.ErrWrongKind))
	err = ops.FreezeLedger(ctx, ledger.GeneralLedgerID, "review")
	fmt.Println("freeze general ledger:", errors.Is(err, ledger.ErrWrongKind))

	err = ops.FreezeAccount(ctx, "bln_doesnotexist", "review")
	fmt.Println("unknown account:", errors.Is(err, ledger.ErrNotFound))
	// end
	end()
}
