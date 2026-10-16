package source_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

func TestCloseAccount(t *testing.T) {
	_, core, wallet, ops := setup(t, ledger.Config{})
	ada := customer(wallet, "user_ada", "Ada", currency.NGN)
	adaNGN := ada.Accounts[currency.NGN]
	tundeNGN := customer(wallet, "user_tunde", "Tunde", currency.NGN).Accounts[currency.NGN]
	shop := customer(wallet, "merchant_311", "Chidi", currency.NGN).Accounts[currency.NGN]
	fund(wallet, adaNGN.ID, "40000", currency.NGN, "seed_ada")
	fund(wallet, tundeNGN.ID, "125000", currency.NGN, "seed_tunde")

	// Ada has a second naira account she used for her side business.
	side, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.NGN, IdentityID: ada.ID,
		MetaData: map[string]any{"purpose": "side business"}})
	check(err)
	fund(wallet, side.ID, "15500", currency.NGN, "seed_ada_side")

	begin("close-account/sweep")
	// sample: close-account/sweep
	// Ada no longer needs her side-business account. The rest goes to her main account.
	closed, err := ops.CloseAccount(ctx, side.ID, ledger.CloseInput{
		Reason:      "customer request",
		RequestedBy: "ops.kemi",
		ApprovedBy:  "lead.femi",
		SweepTo:     adaNGN.ID,
		Reference:   "close_ada_side",
	})
	check(err)
	fmt.Println("status:", closed.Status, "balance:", closed.Balance.Display)
	fmt.Println("reason:", closed.MetaData["status_reason"])
	fmt.Println("requested by:", closed.MetaData["closed_requested_by"], "approved by:", closed.MetaData["closed_approved_by"])

	main, err := wallet.GetAccount(ctx, adaNGN.ID)
	check(err)
	fmt.Println("Ada's main account:", main.Balance.Display)

	sweep, err := wallet.GetTransactionByReference(ctx, "close_ada_side")
	check(err)
	fmt.Println("sweep:", sweep.Amount.Display, sweep.Source, "->", sweep.Destination)
	fmt.Println("sweep metadata:", sweep.MetaData["txn_type"], "/", sweep.MetaData["close_reason"])
	// end
	end()

	begin("close-account/payout")
	// sample: close-account/payout
	// Tunde leaves Oja. His ₦125,000 is paid to his bank through Flutterwave.
	payout := ledger.AccountID(ledger.ClearingAccount("flutterwave")) // "@FlutterwaveClearing"
	closed, err = ops.CloseAccount(ctx, tundeNGN.ID, ledger.CloseInput{
		Reason:      "customer left: final payout to GTBank ****4410",
		RequestedBy: "ops.kemi",
		ApprovedBy:  "lead.femi",
		SweepTo:     payout,
		Reference:   "close_tunde",
	})
	check(err)
	fmt.Println("status:", closed.Status, "balance:", closed.Balance.Display)

	fw, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: string(payout), Currency: currency.NGN})
	check(err)
	fmt.Println(fw.Indicator, "balance:", fw.Balance.Display)
	// end
	end()

	begin("close-account/zero")
	// sample: close-account/zero
	// An account opened by mistake and never used: no SweepTo or Reference needed.
	spare, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.NGN, ExternalID: "user_ada_dup"})
	check(err)
	closed, err = ops.CloseAccount(ctx, spare.ID, ledger.CloseInput{
		Reason:      "duplicate account",
		RequestedBy: "ops.kemi",
		ApprovedBy:  "lead.femi",
	})
	check(err)
	fmt.Println(closed.ID, closed.Status, closed.Balance.Display)
	// end
	end()

	begin("close-account/approval")
	// sample: close-account/approval
	_, err = ops.CloseAccount(ctx, adaNGN.ID, ledger.CloseInput{RequestedBy: "ops.kemi", ApprovedBy: "lead.femi"})
	fmt.Println("no reason:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.CloseAccount(ctx, adaNGN.ID, ledger.CloseInput{Reason: "customer request", ApprovedBy: "lead.femi"})
	fmt.Println("no requester:", errors.Is(err, ledger.ErrApprovalRequired))

	_, err = ops.CloseAccount(ctx, adaNGN.ID, ledger.CloseInput{Reason: "customer request", RequestedBy: "ops.kemi"})
	fmt.Println("no approver:", errors.Is(err, ledger.ErrApprovalRequired))

	_, err = ops.CloseAccount(ctx, adaNGN.ID, ledger.CloseInput{Reason: "customer request",
		RequestedBy: "ops.kemi", ApprovedBy: "Ops.Kemi"})
	fmt.Println("self-approval:", errors.Is(err, ledger.ErrApprovalRequired))
	fmt.Println(err)

	_, err = ops.CloseAccount(ctx, adaNGN.ID, ledger.CloseInput{Reason: "customer request",
		RequestedBy: "ops.kemi", ApprovedBy: "lead.femi"})
	fmt.Println("balance, no SweepTo:", errors.Is(err, ledger.ErrInvalidInput))
	// end
	end()

	// Ada now wants to close her main account. Things are still running on it.
	hold, err := wallet.Hold(ctx, ledger.HoldInput{AccountID: adaNGN.ID, Destination: shop.ID,
		Amount: ledger.Amount("3000", currency.NGN), Reference: "card_auth_7001"})
	check(err)

	begin("close-account/pending")
	// sample: close-account/pending
	in := ledger.CloseInput{
		Reason: "customer request", RequestedBy: "ops.kemi", ApprovedBy: "lead.femi",
		SweepTo: ledger.AccountID("@PayoutClearing"), Reference: "close_ada_main",
	}

	// A card hold at Chidi Stores is still pending.
	_, err = ops.CloseAccount(ctx, adaNGN.ID, in)
	fmt.Println("pending hold:", errors.Is(err, ledger.ErrPendingActivity))
	_, err = wallet.Cancel(ctx, hold.TransactionID)
	check(err)

	// A standing order to Chidi Stores is due tomorrow.
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN.ID, To: shop.ID, Amount: ledger.Amount("2500", currency.NGN),
		Fee: ledger.NoFee, Reference: "so_ada_chidi_1", ScheduledFor: core.Now().Add(24 * time.Hour)})
	check(err)
	_, err = ops.CloseAccount(ctx, adaNGN.ID, in)
	fmt.Println("scheduled transfer:", errors.Is(err, ledger.ErrPendingActivity))
	fmt.Println(err)
	// end
	end()

	core.Advance(25 * time.Hour) // the standing order posts

	rent, err := wallet.OpenPot(ctx, ledger.PotInput{ParentID: adaNGN.ID, Name: "Rent"})
	check(err)
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN.ID, To: rent.ID, Amount: ledger.Amount("6000", currency.NGN),
		Fee: ledger.NoFee, Reference: "pot_rent_in_1"})
	check(err)
	esc, err := wallet.OpenEscrow(ctx, ledger.EscrowInput{Payer: adaNGN.ID, Beneficiary: shop.ID,
		Amount: ledger.Amount("8000", currency.NGN), Reference: "escrow_10450"})
	check(err)

	begin("close-account/pots-escrows")
	// sample: close-account/pots-escrows
	// Ada's Rent pot is still open.
	_, err = ops.CloseAccount(ctx, adaNGN.ID, in)
	fmt.Println("open pot:", errors.Is(err, ledger.ErrPendingActivity))

	// Close the pot first, sweeping its money back to the main account.
	_, err = ops.CloseAccount(ctx, rent.ID, ledger.CloseInput{
		Reason: "parent account closing", RequestedBy: "ops.kemi", ApprovedBy: "lead.femi",
		SweepTo: adaNGN.ID, Reference: "close_ada_rent",
	})
	check(err)

	// An escrow for an order from Chidi Stores still holds ₦8,000.
	_, err = ops.CloseAccount(ctx, adaNGN.ID, in)
	fmt.Println("escrow holding money:", errors.Is(err, ledger.ErrPendingActivity))
	fmt.Println(err)

	// The order arrives: the escrow is released to Chidi Stores.
	_, err = wallet.ReleaseEscrow(ctx, esc.ID, ledger.EscrowSettleInput{Reference: "escrow_10450_release"})
	check(err)

	closed, err = ops.CloseAccount(ctx, adaNGN.ID, in)
	check(err)
	fmt.Println("now:", closed.Status, closed.Balance.Display)
	// end
	end()

	begin("close-account/final")
	// sample: close-account/final
	acct, err := wallet.GetAccount(ctx, adaNGN.ID)
	check(err)
	fmt.Println("status:", acct.Status)

	_, err = wallet.Deposit(ctx, ledger.DepositInput{AccountID: adaNGN.ID, Amount: ledger.Amount("1000", currency.NGN),
		Fee: ledger.NoFee, Reference: "paystack_T8801", Provider: "paystack"})
	fmt.Println("deposit:", errors.Is(err, ledger.ErrAccountClosed))

	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: shop.ID, To: adaNGN.ID,
		Amount: ledger.Amount("500", currency.NGN), Fee: ledger.NoFee, Reference: "chidi_refund_44"})
	fmt.Println("transfer in:", errors.Is(err, ledger.ErrAccountClosed))

	err = ops.UnfreezeAccount(ctx, adaNGN.ID)
	fmt.Println("reopen with UnfreezeAccount:", errors.Is(err, ledger.ErrAccountClosed))

	again, err := ops.CloseAccount(ctx, adaNGN.ID, ledger.CloseInput{}) // already closed: returns it
	check(err)
	fmt.Println("close again:", again.Status, "reason kept:", again.MetaData["status_reason"])
	// end
	end()

	begin("close-account/errors")
	// sample: close-account/errors
	chidiSide, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.NGN, ExternalID: "merchant_311_b"})
	check(err)
	fund(wallet, chidiSide.ID, "9000", currency.NGN, "seed_chidi_b")
	in = ledger.CloseInput{Reason: "merchant request", RequestedBy: "ops.kemi", ApprovedBy: "lead.femi",
		SweepTo: adaNGN.ID, Reference: "close_chidi_b"}

	_, err = ops.CloseAccount(ctx, chidiSide.ID, in) // Ada's account is closed
	fmt.Println("sweep into closed:", errors.Is(err, ledger.ErrAccountClosed))

	check(ops.FreezeAccount(ctx, shop.ID, "chargeback review"))
	in.SweepTo = shop.ID
	_, err = ops.CloseAccount(ctx, chidiSide.ID, in)
	fmt.Println("sweep into frozen:", errors.Is(err, ledger.ErrAccountFrozen))

	in.SweepTo = chidiSide.ID
	_, err = ops.CloseAccount(ctx, chidiSide.ID, in)
	fmt.Println("sweep into itself:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.CloseAccount(ctx, ledger.AccountID(ledger.FeeIncomeAccount), in)
	fmt.Println("house account:", errors.Is(err, ledger.ErrWrongKind))

	// Nothing leaves a frozen ledger, closure sweeps included.
	agents, err := ops.CreateLedger(ctx, ledger.CreateLedgerInput{Key: "oja-agents", Name: "Oja Agents"})
	check(err)
	ikeja, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.NGN, LedgerID: agents.ID})
	check(err)
	fund(wallet, ikeja.ID, "7000", currency.NGN, "seed_ikeja")
	check(ops.FreezeLedger(ctx, agents.ID, "CBN directive 2026/114"))
	_, err = ops.CloseAccount(ctx, ikeja.ID, ledger.CloseInput{Reason: "agent offboarded",
		RequestedBy: "ops.kemi", ApprovedBy: "lead.femi", SweepTo: "@PayoutClearing", Reference: "close_ikeja"})
	fmt.Println("frozen ledger:", errors.Is(err, ledger.ErrLedgerFrozen))

	// A frozen account can still be closed and swept.
	check(ops.FreezeAccount(ctx, chidiSide.ID, "merchant offboarding"))
	in.SweepTo = ledger.AccountID("@PayoutClearing")
	closed, err = ops.CloseAccount(ctx, chidiSide.ID, in)
	check(err)
	fmt.Println("frozen account closed:", closed.Status, closed.Balance.Display)
	// end
	end()
}
