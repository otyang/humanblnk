package ledger_test

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

// TestLiveCoreSafeguards checks against a real Blnk Core the safeguards that
// depend on how Core stores things: disputes and refunds capped against each
// other (void children found by parent_transaction), freeze holds stopping
// scheduled transfers, bulk read-back and retry, and each account's stored
// precision. It is skipped unless BLNK_LIVE_URL is set; the scheduled part
// waits for Core's workers, up to two minutes.
func TestLiveCoreSafeguards(t *testing.T) {
	url := os.Getenv("BLNK_LIVE_URL")
	if url == "" {
		t.Skip("set BLNK_LIVE_URL to run against a real Blnk Core")
	}
	c := blnk.New(url, blnk.WithAPIKey(os.Getenv("BLNK_LIVE_API_KEY")))
	l, err := c.CreateLedger(ctx, blnk.CreateLedgerRequest{Name: "humanblnk live safeguards"})
	if err != nil {
		t.Fatal(err)
	}
	svc := ledger.New(c, ledger.Config{LedgerID: ledger.LedgerID(l.LedgerID)})
	w := svc.Wallet()
	run := fmt.Sprint(time.Now().UnixNano())
	ngn := func(v string) ledger.Money { return ledger.Amount(v, currency.NGN) }
	acct := func() *ledger.Account {
		a, err := w.OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.NGN})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	deposit := func(id ledger.AccountID, v, ref string) {
		if _, err := w.Deposit(ctx, ledger.DepositInput{AccountID: id, Amount: ngn(v), Fee: ledger.NoFee, Reference: ref + run, Provider: "paystack"}); err != nil {
			t.Fatal(err)
		}
	}
	balance := func(id ledger.AccountID) *ledger.Account {
		a, err := w.GetAccount(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	transfer := func(from, to ledger.AccountID, v, ref string) *ledger.Receipt {
		r, err := w.Transfer(ctx, ledger.TransferInput{From: from, To: to, Amount: ngn(v), Fee: ledger.NoFee, Reference: ref + run})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	alice, bob := acct(), acct()
	if p := str(balance(alice.ID).MetaData["precision"]); p != "100" {
		t.Errorf("stored precision = %q, want \"100\"", p)
	}
	deposit(alice.ID, "300", "dep_a_")
	deposit(bob.ID, "500", "dep_b_")

	// Disputes and refunds share one cap. A part-upheld dispute counts only
	// what went back; its void child (the released rest) is found by
	// parent_transaction.
	p := transfer(alice.ID, bob.ID, "100", "pay1_")
	d, err := w.OpenDispute(ctx, ledger.DisputeInput{TransactionID: p.TransactionID, Amount: ngn("60"), Reference: "disp1_" + run})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.RefundPartial(ctx, p.TransactionID, ledger.PartialRefundInput{Amount: ngn("50"), Reference: "rp1_" + run}); !errors.Is(err, ledger.ErrAmountExceeded) {
		t.Errorf("refund past an open dispute: %v", err)
	}
	forty := ngn("40")
	if _, err := w.ResolveDispute(ctx, d.TransactionID, ledger.DisputeUpheld, &forty); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ResolveDispute(ctx, d.TransactionID, ledger.DisputeRejected, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := w.RefundPartial(ctx, p.TransactionID, ledger.PartialRefundInput{Amount: ngn("61"), Reference: "rp2_" + run}); !errors.Is(err, ledger.ErrAmountExceeded) {
		t.Errorf("refund past what is left after a part-upheld dispute: %v", err)
	}
	if _, err := w.RefundPartial(ctx, p.TransactionID, ledger.PartialRefundInput{Amount: ngn("60"), Reference: "rp3_" + run}); err != nil {
		t.Errorf("refund of what is left: %v", err)
	}
	if _, err := w.OpenDispute(ctx, ledger.DisputeInput{TransactionID: p.TransactionID, Reference: "disp2_" + run}); !errors.Is(err, ledger.ErrAlreadyFinalized) {
		t.Errorf("dispute of a payment already paid back: %v", err)
	}
	full := transfer(alice.ID, bob.ID, "50", "pay2_")
	if _, err := w.Refund(ctx, full.TransactionID, ledger.RefundInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.OpenDispute(ctx, ledger.DisputeInput{TransactionID: full.TransactionID, Reference: "disp3_" + run}); !errors.Is(err, ledger.ErrAlreadyFinalized) {
		t.Errorf("dispute after a full refund: %v", err)
	}

	// Refund needs a posted transaction.
	h, err := w.Hold(ctx, ledger.HoldInput{AccountID: alice.ID, Destination: bob.ID, Amount: ngn("10"), Reference: "hold1_" + run})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Refund(ctx, h.TransactionID, ledger.RefundInput{}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Errorf("refund of a pending hold: %v", err)
	}
	if _, err := w.Cancel(ctx, h.TransactionID); err != nil {
		t.Fatal(err)
	}

	// Bulk: per-item results, a dropped item, then a retry that sends only it.
	carol := acct()
	in := ledger.BulkTransferInput{Items: []ledger.TransferInput{
		{From: alice.ID, To: bob.ID, Amount: ngn("5"), Fee: ledger.Amount("1", ""), Reference: "bulk1_" + run},
		{From: carol.ID, To: bob.ID, Amount: ngn("5"), Fee: ledger.NoFee, Reference: "bulk2_" + run},
	}}
	br, err := w.Bulk(ctx, in)
	if !errors.Is(err, ledger.ErrInsufficientFunds) || br == nil || br.Count != 1 || !br.Items[0].Recorded || br.Items[1].Recorded {
		t.Fatalf("bulk with one item short of funds: %+v %v", br, err)
	}
	deposit(carol.ID, "5", "dep_c_")
	if br, err = w.Bulk(ctx, in); err != nil || br.Count != 2 {
		t.Errorf("bulk retry: %+v %v", br, err)
	}
	if br, err = w.Bulk(ctx, in); err != nil || !br.Replayed || br.Count != 2 {
		t.Errorf("bulk replay: %+v %v", br, err)
	}

	// Freezing holds the balance, so a scheduled transfer coming due while
	// frozen is rejected by Core; unfreezing releases it.
	dave, erin := acct(), acct()
	deposit(dave.ID, "100", "dep_d_")
	plan, err := w.Schedule(ctx, ledger.RecurringInput{
		Transfer: ledger.TransferInput{From: dave.ID, To: erin.ID, Amount: ngn("40"), Fee: ledger.NoFee, Reference: "plan_" + run},
		First:    time.Now().Add(5 * time.Second), Every: ledger.Daily, Count: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ledger.EntryStatusOf(plan[0].Status) != ledger.EntryScheduled || balance(dave.ID).Balance.Value != "100" {
		t.Fatalf("Schedule posted at once: %s, dave = %s", plan[0].Status, balance(dave.ID).Balance.Value)
	}
	if err := svc.Ops().FreezeAccount(ctx, dave.ID, "live test"); err != nil {
		t.Fatal(err)
	}
	frozen := balance(dave.ID)
	if frozen.Available.Value != "0" || frozen.Balance.Value != "100" {
		t.Errorf("frozen: balance %s, available %s", frozen.Balance.Value, frozen.Available.Value)
	}
	if _, err := w.Cancel(ctx, str(frozen.MetaData["freeze_hold"])); !errors.Is(err, ledger.ErrWrongKind) {
		t.Errorf("Cancel on the freeze hold: %v", err)
	}
	var status ledger.EntryStatus
	for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); time.Sleep(3 * time.Second) {
		r, err := w.GetTransaction(ctx, plan[0].TransactionID)
		if err != nil {
			t.Fatal(err)
		}
		if status = ledger.EntryStatusOf(r.Status); status != ledger.EntryScheduled && status != ledger.EntryQueued {
			break
		}
	}
	if status != ledger.EntryRejected {
		t.Errorf("scheduled transfer from a frozen account: %s, want rejected", status)
	}
	if got := balance(dave.ID).Balance.Value; got != "100" {
		t.Errorf("dave = %s, want 100 (scheduled transfer status %s)", got, status)
	}
	if err := svc.Ops().UnfreezeAccount(ctx, dave.ID); err != nil {
		t.Fatal(err)
	}
	if got := balance(dave.ID).Available.Value; got != "100" {
		t.Errorf("after unfreezing, available = %s, want 100", got)
	}
}
