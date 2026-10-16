package source_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger"
)

func TestErrors(t *testing.T) {
	_, _, wallet, ops := setup(t, ledger.Config{
		Limits: []ledger.Limit{{Tier: "tier2", Currency: "NGN", PerTransaction: "100000"}},
	})
	ada := customer(wallet, "user_8812", "Ada", "NGN").Accounts["NGN"].ID
	tunde := customer(wallet, "user_9034", "Tunde", "NGN").Accounts["NGN"].ID
	fund(wallet, ada, "150000", "NGN", "paystack_T5001")

	begin("errors/is")
	// sample: errors/is
	_, err := wallet.Transfer(ctx, ledger.TransferInput{
		From: ada, To: tunde, Amount: ledger.Amount("90000", "NGN"),
		Fee: ledger.NoFee, Reference: "p2p_0001",
	})
	check(err)

	_, err = wallet.Transfer(ctx, ledger.TransferInput{
		From: ada, To: tunde, Amount: ledger.Amount("75000", "NGN"),
		Fee: ledger.NoFee, Reference: "p2p_0002",
	})
	if errors.Is(err, ledger.ErrInsufficientFunds) {
		fmt.Println("Ada doesn't have enough")
	}
	fmt.Println(err)
	// end
	end()

	begin("errors/as")
	// sample: errors/as
	var oe *ledger.OpError
	if errors.As(err, &oe) {
		fmt.Println("Op:   ", oe.Op)
		fmt.Println("Kind: ", oe.Kind)
		fmt.Println("Cause:", oe.Cause)
	}

	var ae *blnk.APIError // what Blnk answered, for your logs
	if errors.As(err, &ae) {
		fmt.Println("Blnk:", ae.Status, ae.Code)
	}
	// end
	end()

	begin("errors/refusals")
	// sample: errors/refusals
	// Over Ada's tier2 limit of ₦100,000 per transaction:
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: tunde, To: ada,
		Amount: ledger.Amount("100000.01", "NGN"), Fee: ledger.NoFee, Reference: "p2p_0003"})
	fmt.Println("limit:", errors.Is(err, ledger.ErrLimitExceeded))

	check(ops.FreezeAccount(ctx, tunde, "fraud review"))
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: ada, To: tunde,
		Amount: ledger.Amount("1000", "NGN"), Fee: ledger.NoFee, Reference: "p2p_0004"})
	fmt.Println("frozen:", errors.Is(err, ledger.ErrAccountFrozen))
	check(ops.UnfreezeAccount(ctx, tunde))

	old, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN"})
	check(err)
	_, err = ops.CloseAccount(ctx, old.ID, ledger.CloseInput{Reason: "duplicate account",
		RequestedBy: "support_amaka", ApprovedBy: "ops_lead_femi"})
	check(err)
	_, err = wallet.Deposit(ctx, ledger.DepositInput{AccountID: old.ID,
		Amount: ledger.Amount("500", "NGN"), Fee: ledger.NoFee, Reference: "paystack_T5002", Provider: "paystack"})
	fmt.Println("closed:", errors.Is(err, ledger.ErrAccountClosed))
	// end
	end()

	begin("errors/mistakes")
	// sample: errors/mistakes
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: ada, To: tunde,
		Amount: ledger.Amount("100", "NGN"), Fee: ledger.NoFee}) // no Reference
	fmt.Println("invalid:", errors.Is(err, ledger.ErrInvalidInput), err)

	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: ada, To: tunde,
		Amount: ledger.Amount("100", "NGN"), Reference: "p2p_0005"}) // no Fee and no Config.Fees
	fmt.Println("invalid:", errors.Is(err, ledger.ErrInvalidInput), err)

	_, err = wallet.Deposit(ctx, ledger.DepositInput{AccountID: ledger.AccountID(ledger.FeeIncomeAccount),
		Amount: ledger.Amount("100", "NGN"), Fee: ledger.NoFee, Reference: "paystack_T5003"})
	fmt.Println("wrong kind:", errors.Is(err, ledger.ErrWrongKind), "also invalid:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: ada, To: tunde, // p2p_0001 was ₦90,000
		Amount: ledger.Amount("500", "NGN"), Fee: ledger.NoFee, Reference: "p2p_0001"})
	fmt.Println("duplicate reference:", errors.Is(err, ledger.ErrDuplicateReference))

	_, err = wallet.GetAccount(ctx, "bln_999999")
	fmt.Println("not found:", errors.Is(err, ledger.ErrNotFound))

	_, err = wallet.OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN", LedgerID: "ldg_999999"})
	fmt.Println("ledger not found:", errors.Is(err, ledger.ErrLedgerNotFound), "also not found:", errors.Is(err, ledger.ErrNotFound))
	// end
	end()

	begin("errors/state")
	// sample: errors/state
	hold, err := wallet.Hold(ctx, ledger.HoldInput{AccountID: ada, Destination: tunde,
		Amount: ledger.Amount("2000", "NGN"), Reference: "card_auth_7781"})
	check(err)

	more := ledger.Amount("2500", "NGN")
	_, err = wallet.Confirm(ctx, hold.TransactionID, &more)
	fmt.Println("amount exceeded:", errors.Is(err, ledger.ErrAmountExceeded))

	_, err = ops.CloseAccount(ctx, ada, ledger.CloseInput{Reason: "customer request",
		ApprovedBy: "ops_lead_femi", SweepTo: tunde, Reference: "close_ada"})
	fmt.Println("pending activity:", errors.Is(err, ledger.ErrPendingActivity))

	_, err = wallet.Confirm(ctx, hold.TransactionID, nil)
	check(err)
	_, err = wallet.Confirm(ctx, hold.TransactionID, nil)
	fmt.Println("already finalized:", errors.Is(err, ledger.ErrAlreadyFinalized))

	paid, err := wallet.GetTransactionByReference(ctx, "p2p_0001") // posted, never pending
	check(err)
	_, err = wallet.Cancel(ctx, paid.TransactionID)
	fmt.Println("not pending:", errors.Is(err, ledger.ErrNotPending))

	_, err = ops.CloseAccount(ctx, tunde, ledger.CloseInput{Reason: "customer request",
		RequestedBy: "support_amaka", ApprovedBy: "support_amaka"})
	fmt.Println("approval required:", errors.Is(err, ledger.ErrApprovalRequired))
	// end
	end()

	begin("errors/retry")
	// sample: errors/retry
	// The app timed out waiting for Blnk and sends the same payment again.
	in := ledger.TransferInput{From: ada, To: tunde, Amount: ledger.Amount("1500", "NGN"),
		Fee: ledger.NoFee, Reference: "p2p_0006"}
	first, err := wallet.Transfer(ctx, in)
	check(err)
	second, err := wallet.Transfer(ctx, in)
	check(err)
	fmt.Println("same transaction:", second.TransactionID == first.TransactionID, "replayed:", second.Replayed)

	retryLater := func(err error) bool {
		return errors.Is(err, ledger.ErrUnavailable) || errors.Is(err, ledger.ErrConflict)
	}
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: ada, To: tunde,
		Amount: ledger.Amount("99000", "NGN"), Fee: ledger.NoFee, Reference: "p2p_0007"})
	fmt.Println("insufficient funds, retry later?", retryLater(err))
	// end
	end()

	begin("errors/message")
	// sample: errors/message
	userMessage := func(err error) string {
		switch {
		case err == nil:
			return "Done"
		case errors.Is(err, ledger.ErrInsufficientFunds):
			return "You don't have enough money for this"
		case errors.Is(err, ledger.ErrLimitExceeded):
			return "This is over your limit. Upgrade your account to send more"
		case errors.Is(err, ledger.ErrAccountFrozen), errors.Is(err, ledger.ErrLedgerFrozen):
			return "This account is on hold. Contact support"
		case errors.Is(err, ledger.ErrAccountClosed):
			return "This account is closed"
		case errors.Is(err, ledger.ErrUnavailable), errors.Is(err, ledger.ErrConflict):
			return "Something went wrong on our side. Try again in a moment"
		default:
			return "We couldn't complete this" // and log err: it is a bug or a bad request
		}
	}

	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: ada, To: tunde,
		Amount: ledger.Amount("99000", "NGN"), Fee: ledger.NoFee, Reference: "p2p_0008"})
	fmt.Println(userMessage(err))
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: ada, To: old.ID,
		Amount: ledger.Amount("100", "NGN"), Fee: ledger.NoFee, Reference: "p2p_0009"})
	fmt.Println(userMessage(err))
	// end
	end()
}
