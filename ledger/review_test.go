package ledger_test

import (
	"errors"
	"testing"
	"time"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/ledgertest"
)

func pay(t *testing.T, svc *ledger.Service, from, to ledger.AccountID, amt, ref string) *ledger.Receipt {
	t.Helper()
	r, err := svc.Wallet().Transfer(ctx, ledger.TransferInput{From: from, To: to, Amount: ledger.Amount(amt, currency.NGN), Fee: ledger.NoFee, Reference: ref})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// A payment is never paid back to its payer more than once, whatever mix of
// Refund, RefundPartial and disputes is used.
func TestPaidBackOnlyOnce(t *testing.T) {
	setup := func(t *testing.T) (*ledger.Service, *ledger.Account, *ledger.Account, *ledger.Receipt) {
		svc, _ := ledgertest.New(t, ledger.Config{})
		alice, bob := open(t, svc, currency.NGN), open(t, svc, currency.NGN)
		fund(t, svc, alice.ID, "100", currency.NGN, "d1")
		fund(t, svc, bob.ID, "500", currency.NGN, "d2")
		return svc, alice, bob, pay(t, svc, alice.ID, bob.ID, "100", "p1")
	}
	dispute := func(svc *ledger.Service, tx, amount, ref string) (*ledger.Receipt, error) {
		in := ledger.DisputeInput{TransactionID: tx, Reference: ref}
		if amount != "" {
			in.Amount = ledger.Amount(amount, currency.NGN)
		}
		return svc.Wallet().OpenDispute(ctx, in)
	}
	partial := func(svc *ledger.Service, tx, amount, ref string) error {
		_, err := svc.Wallet().RefundPartial(ctx, tx, ledger.PartialRefundInput{Amount: ledger.Amount(amount, currency.NGN), Reference: ref})
		return err
	}

	t.Run("dispute after a full refund", func(t *testing.T) {
		svc, alice, _, p := setup(t)
		if _, err := svc.Wallet().Refund(ctx, p.TransactionID, ledger.RefundInput{Reason: "r"}); err != nil {
			t.Fatal(err)
		}
		if _, err := dispute(svc, p.TransactionID, "", "disp1"); !errors.Is(err, ledger.ErrAlreadyFinalized) {
			t.Fatalf("dispute after refund: %v", err)
		}
		if got := bal(t, svc, alice.ID); got != "100" {
			t.Errorf("alice = %s, want 100", got)
		}
	})
	t.Run("two disputes", func(t *testing.T) {
		svc, _, _, p := setup(t)
		if _, err := dispute(svc, p.TransactionID, "60", "dA"); err != nil {
			t.Fatal(err)
		}
		if _, err := dispute(svc, p.TransactionID, "50", "dB"); !errors.Is(err, ledger.ErrAmountExceeded) {
			t.Fatalf("second dispute past what is left: %v", err)
		}
		d, err := dispute(svc, p.TransactionID, "", "dC") // the 40 left
		if err != nil || d.Amount.Value != "40" {
			t.Fatalf("dispute of the rest: %+v %v", d, err)
		}
		if again, err := dispute(svc, p.TransactionID, "", "dC"); err != nil || !again.Replayed {
			t.Fatalf("retrying a dispute replays it: %+v %v", again, err)
		}
	})
	t.Run("refund with a dispute open", func(t *testing.T) {
		svc, _, _, p := setup(t)
		if _, err := dispute(svc, p.TransactionID, "60", "dA"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Wallet().Refund(ctx, p.TransactionID, ledger.RefundInput{}); !errors.Is(err, ledger.ErrAlreadyFinalized) {
			t.Fatalf("full refund over an open dispute: %v", err)
		}
		if err := partial(svc, p.TransactionID, "50", "r1"); !errors.Is(err, ledger.ErrAmountExceeded) {
			t.Fatalf("partial refund past what is left: %v", err)
		}
		if err := partial(svc, p.TransactionID, "40", "r2"); err != nil {
			t.Fatalf("partial refund of the rest: %v", err)
		}
	})
	t.Run("a rejected dispute frees its amount", func(t *testing.T) {
		svc, _, _, p := setup(t)
		d, err := dispute(svc, p.TransactionID, "60", "dA")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Wallet().ResolveDispute(ctx, d.TransactionID, ledger.DisputeRejected, nil); err != nil {
			t.Fatal(err)
		}
		if err := partial(svc, p.TransactionID, "100", "r1"); err != nil {
			t.Fatalf("refund after a rejected dispute: %v", err)
		}
	})
	t.Run("a part-upheld dispute counts what went back", func(t *testing.T) {
		svc, alice, _, p := setup(t)
		d, err := dispute(svc, p.TransactionID, "60", "dA")
		if err != nil {
			t.Fatal(err)
		}
		forty := ledger.Amount("40", currency.NGN)
		if _, err := svc.Wallet().ResolveDispute(ctx, d.TransactionID, ledger.DisputeUpheld, &forty); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Wallet().ResolveDispute(ctx, d.TransactionID, ledger.DisputeRejected, nil); err != nil {
			t.Fatal(err)
		}
		if err := partial(svc, p.TransactionID, "61", "r1"); !errors.Is(err, ledger.ErrAmountExceeded) {
			t.Fatalf("refund past what is left: %v", err)
		}
		if err := partial(svc, p.TransactionID, "60", "r2"); err != nil {
			t.Fatal(err)
		}
		if got := bal(t, svc, alice.ID); got != "100" {
			t.Errorf("alice = %s, want exactly the 100 she paid", got)
		}
	})
}

// An account keeps the precision it was opened with: a Service configured
// with another one reads it correctly and refuses to post to it at the new one.
func TestAccountKeepsItsPrecision(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	a, b := open(t, svc, currency.NGN), open(t, svc, currency.NGN)
	fund(t, svc, a.ID, "12.34", currency.NGN, "d1")

	other := ledger.New(core.Client(), ledger.Config{LedgerID: a.LedgerID, Currencies: []currency.Currency{{Code: currency.NGN, Precision: 1000}}})
	acct, err := other.Wallet().GetAccount(ctx, a.ID)
	if err != nil || acct.Balance.Value != "12.34" || acct.Precision != 100 {
		t.Fatalf("read at the stored precision: %+v %v", acct, err)
	}
	_, err = other.Wallet().Transfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("1", currency.NGN), Fee: ledger.NoFee, Reference: "t1"})
	if !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("posting at a different precision: %v", err)
	}
	fresh, err := other.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.NGN})
	if err != nil || fresh.Precision != 1000 {
		t.Fatalf("a new account takes the new precision: %+v %v", fresh, err)
	}
}

// A Config.Currencies entry with a mistake blocks its currency instead of
// quietly running on the built-in entry.
func TestBrokenCurrencyEntryIsRefused(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{Currencies: []currency.Currency{
		{Code: currency.XRP, Precision: 100, Kind: "metal"},
		{Code: currency.ETH, Precision: 1e6, Kind: " Crypto "}, // any case is fine
	}})
	if _, err := svc.Parse("1.5", currency.XRP); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("Parse in a broken currency: %v", err)
	}
	if _, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.XRP}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("OpenAccount in a broken currency: %v", err)
	}
	if _, ok := svc.Currency(currency.XRP); ok {
		t.Error("a broken currency is still listed")
	}
	if eth, ok := svc.Currency(currency.ETH); !ok || eth.Kind != currency.Crypto || eth.Precision != 1e6 {
		t.Errorf("ETH: %+v %v", eth, ok)
	}
	if err := svc.Check(ctx); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Errorf("Check: %v", err)
	}
}

// Freezing an account stops Blnk posting its scheduled transfers; unfreezing
// lets the rest run.
func TestFreezeStopsScheduledTransfers(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	ops := svc.Ops()
	alice, bob := open(t, svc, currency.NGN), open(t, svc, currency.NGN)
	fund(t, svc, alice.ID, "100", currency.NGN, "d1")
	_, err := svc.Wallet().Schedule(ctx, ledger.RecurringInput{
		Transfer: ledger.TransferInput{From: alice.ID, To: bob.ID, Amount: ledger.Amount("40", currency.NGN), Fee: ledger.NoFee, Reference: "plan"},
		First:    core.Now().Add(time.Hour), Every: ledger.Daily, Count: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ops.FreezeAccount(ctx, alice.ID, "fraud"); err != nil {
		t.Fatal(err)
	}
	if err := ops.FreezeAccount(ctx, alice.ID, "fraud"); err != nil { // again: no second hold
		t.Fatal(err)
	}
	acct, _ := svc.Wallet().GetAccount(ctx, alice.ID)
	if acct.Balance.Value != "100" || acct.Available.Value != "0" {
		t.Fatalf("frozen: balance %s available %s", acct.Balance.Value, acct.Available.Value)
	}
	hold := ledger.Receipt{TransactionID: str(acct.MetaData["freeze_hold"])}
	if _, err := svc.Wallet().Cancel(ctx, hold.TransactionID); !errors.Is(err, ledger.ErrWrongKind) {
		t.Fatalf("Cancel on the freeze hold: %v", err)
	}
	if _, err := svc.Wallet().Confirm(ctx, hold.TransactionID, nil); !errors.Is(err, ledger.ErrWrongKind) {
		t.Fatalf("Confirm on the freeze hold: %v", err)
	}
	res, err := svc.Wallet().CancelMany(ctx, []string{hold.TransactionID})
	if err != nil || len(res) != 1 || !errors.Is(res[0].Err, ledger.ErrWrongKind) {
		t.Fatalf("CancelMany on the freeze hold: %+v %v", res, err)
	}

	core.Advance(2 * time.Hour) // first occurrence comes due while frozen
	if got := bal(t, svc, alice.ID); got != "100" {
		t.Fatalf("a scheduled transfer posted from a frozen account: alice=%s", got)
	}
	if err := ops.UnfreezeAccount(ctx, alice.ID); err != nil {
		t.Fatal(err)
	}
	core.Advance(24 * time.Hour) // second occurrence, after unfreezing
	if got := bal(t, svc, alice.ID); got != "60" {
		t.Errorf("after unfreezing: alice=%s, want 60", got)
	}
}

// A frozen account can still be closed: its freeze hold is released and the
// balance swept.
func TestCloseFrozenAccount(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	alice, bob := open(t, svc, currency.NGN), open(t, svc, currency.NGN)
	fund(t, svc, alice.ID, "100", currency.NGN, "d1")
	if err := svc.Ops().FreezeAccount(ctx, alice.ID, "fraud"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Ops().CloseAccount(ctx, alice.ID, ledger.CloseInput{Reason: "fraud", RequestedBy: "ops.kemi", ApprovedBy: "lead.femi",
		SweepTo: bob.ID, Reference: "close1"}); err != nil {
		t.Fatal(err)
	}
	if got := bal(t, svc, bob.ID); got != "100" {
		t.Errorf("bob = %s, want 100", got)
	}
}

// Refund reverses posted transactions only.
func TestRefundNeedsPostedTransaction(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	alice, bob := open(t, svc, currency.NGN), open(t, svc, currency.NGN)
	fund(t, svc, alice.ID, "100", currency.NGN, "d1")
	h, err := svc.Wallet().Hold(ctx, ledger.HoldInput{AccountID: alice.ID, Destination: bob.ID, Amount: ledger.Amount("30", currency.NGN), Reference: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Wallet().Refund(ctx, h.TransactionID, ledger.RefundInput{}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("refund of a pending hold: %v", err)
	}
	c, err := svc.Wallet().Confirm(ctx, h.TransactionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Wallet().Refund(ctx, c.TransactionID, ledger.RefundInput{}); err != nil {
		t.Fatalf("refund of the confirmation: %v", err)
	}
}

// Pots move money only to and from their parent; escrow accounts only
// through ReleaseEscrow and RefundEscrow.
func TestPotsAndEscrowsMoveOnlyTheirWay(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{Limits: []ledger.Limit{{Tier: "*", Currency: currency.NGN, PerTransaction: "10000"}}})
	w := svc.Wallet()
	alice, bob := open(t, svc, currency.NGN), open(t, svc, currency.NGN)
	fund(t, svc, alice.ID, "5000", currency.NGN, "d1")
	pot, err := w.OpenPot(ctx, ledger.PotInput{ParentID: alice.ID, Name: "Rent"})
	if err != nil {
		t.Fatal(err)
	}
	pay(t, svc, alice.ID, pot.ID, "3000", "save1") // parent to pot
	pay(t, svc, pot.ID, alice.ID, "1000", "back1") // pot to parent

	wrong := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, ledger.ErrWrongKind) {
			t.Errorf("%s: %v", what, err)
		}
	}
	_, err = w.Deposit(ctx, ledger.DepositInput{AccountID: pot.ID, Amount: ledger.Amount("10", currency.NGN), Fee: ledger.NoFee, Reference: "d2", Provider: "paystack"})
	wrong("deposit into a pot", err)
	_, err = w.Withdraw(ctx, ledger.WithdrawInput{AccountID: pot.ID, Amount: ledger.Amount("10", currency.NGN), Fee: ledger.NoFee, Reference: "w1", Immediate: true})
	wrong("withdraw from a pot", err)
	_, err = w.Transfer(ctx, ledger.TransferInput{From: pot.ID, To: bob.ID, Amount: ledger.Amount("10", currency.NGN), Fee: ledger.NoFee, Reference: "t1"})
	wrong("pot pays a third party", err)
	_, err = w.Transfer(ctx, ledger.TransferInput{From: bob.ID, To: pot.ID, Amount: ledger.Amount("10", currency.NGN), Fee: ledger.NoFee, Reference: "t2"})
	wrong("third party pays a pot", err)
	_, err = w.Bulk(ctx, ledger.BulkTransferInput{Items: []ledger.TransferInput{{From: pot.ID, To: bob.ID, Amount: ledger.Amount("10", currency.NGN), Fee: ledger.NoFee, Reference: "b1"}}})
	wrong("bulk from a pot", err)

	esc, err := w.OpenEscrow(ctx, ledger.EscrowInput{Payer: alice.ID, Beneficiary: bob.ID, Amount: ledger.Amount("500", currency.NGN), Reference: "e1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Transfer(ctx, ledger.TransferInput{From: esc.ID, To: alice.ID, Amount: ledger.Amount("500", currency.NGN), Fee: ledger.NoFee, Reference: "t3"})
	wrong("transfer out of an escrow", err)
	_, err = w.Hold(ctx, ledger.HoldInput{AccountID: esc.ID, Destination: alice.ID, Amount: ledger.Amount("500", currency.NGN), Reference: "h1"})
	wrong("hold on an escrow", err)
	if _, err := w.ReleaseEscrow(ctx, esc.ID, ledger.EscrowSettleInput{Reference: "rel1"}); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// Bulk reports each item, counts only what Blnk recorded, and replays a retry.
func TestBulkItemsAndRetry(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	alice, bob, carol := open(t, svc, currency.NGN), open(t, svc, currency.NGN), open(t, svc, currency.NGN)
	fund(t, svc, alice.ID, "100", currency.NGN, "d1")
	in := ledger.BulkTransferInput{Items: []ledger.TransferInput{
		{From: alice.ID, To: bob.ID, Amount: ledger.Amount("50", currency.NGN), Fee: ledger.Amount("1", ""), Reference: "b1"},
		{From: carol.ID, To: bob.ID, Amount: ledger.Amount("50", currency.NGN), Fee: ledger.NoFee, Reference: "b2"}, // carol has nothing
	}}
	r, err := svc.Wallet().Bulk(ctx, in) // stops at b2; b1 stays posted
	if !errors.Is(err, ledger.ErrInsufficientFunds) || r == nil {
		t.Fatalf("partial batch: %+v %v", r, err)
	}
	if r.Status != "partial" || r.Count != 1 || len(r.Items) != 2 || !r.Items[0].Recorded || r.Items[0].Status != ledger.EntryPosted || r.Items[1].Recorded {
		t.Fatalf("receipt: %+v", r)
	}

	fund(t, svc, carol.ID, "50", currency.NGN, "d2")
	again, err := svc.Wallet().Bulk(ctx, in) // b1 replays, b2 is sent now
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if again.Count != 2 || again.Items[0].TransactionID != r.Items[0].TransactionID {
		t.Fatalf("retry receipt: %+v", again)
	}
	if got := bal(t, svc, bob.ID); got != "100" {
		t.Errorf("bob = %s, want 100 (each item once)", got)
	}
	if third, err := svc.Wallet().Bulk(ctx, in); err != nil || !third.Replayed || third.Count != 2 {
		t.Fatalf("replay: %+v %v", third, err)
	}

	changed := in
	changed.Items = append([]ledger.TransferInput(nil), in.Items...)
	changed.Items[0].Amount = ledger.Amount("49", currency.NGN)
	if _, err := svc.Wallet().Bulk(ctx, changed); !errors.Is(err, ledger.ErrDuplicateReference) {
		t.Fatalf("retry with a changed item: %v", err)
	}
}

// A dispute can be opened when the recipient is frozen: that is when the
// money most needs ring-fencing. The freeze keeps holding the rest.
func TestDisputeWithFrozenRecipient(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	alice, bob := open(t, svc, currency.NGN), open(t, svc, currency.NGN)
	fund(t, svc, alice.ID, "100", currency.NGN, "d1")
	fund(t, svc, bob.ID, "30", currency.NGN, "d2")
	p := pay(t, svc, alice.ID, bob.ID, "100", "p1")
	if err := svc.Ops().FreezeAccount(ctx, bob.ID, "fraud"); err != nil {
		t.Fatal(err)
	}
	d, err := svc.Wallet().OpenDispute(ctx, ledger.DisputeInput{TransactionID: p.TransactionID, Reference: "disp1"})
	if err != nil {
		t.Fatalf("dispute on a frozen recipient: %v", err)
	}
	acct, _ := svc.Wallet().GetAccount(ctx, bob.ID)
	if acct.Available.Value != "0" || acct.Status != "frozen" {
		t.Fatalf("bob after the dispute: available %s, %s", acct.Available.Value, acct.Status)
	}
	if _, err := svc.Wallet().ResolveDispute(ctx, d.TransactionID, ledger.DisputeUpheld, nil); err != nil {
		t.Fatal(err)
	}
	if got := bal(t, svc, alice.ID); got != "100" {
		t.Errorf("alice = %s, want 100", got)
	}
	if err := svc.Ops().UnfreezeAccount(ctx, bob.ID); err != nil {
		t.Fatal(err)
	}
	if acct, _ := svc.Wallet().GetAccount(ctx, bob.ID); acct.Available.Value != "30" {
		t.Errorf("bob after unfreezing: available %s, want 30", acct.Available.Value)
	}
}

// Amounts, rates and percentages are plain decimals.
func TestStrictDecimals(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	for _, in := range []string{"200/2", "0x10", "0x1p-2", "0b11", "0o7", "1e3", "1.2.3", "--1", "Inf"} {
		if m, err := svc.Parse(in, currency.USD); !errors.Is(err, ledger.ErrInvalidInput) {
			t.Errorf("Parse(%q) = %s, %v", in, m.Value, err)
		}
	}
	for in, want := range map[string]string{"1_000": "1000", "1,500.50": "1500.5", ".5": "0.5", "7.": "7", " 12 ": "12"} {
		if m, err := svc.Parse(in, currency.USD); err != nil || m.Value != want {
			t.Errorf("Parse(%q) = %q, %v; want %s", in, m.Value, err, want)
		}
	}
	usd, ngn := open(t, svc, currency.USD), open(t, svc, currency.NGN)
	fund(t, svc, usd.ID, "10", currency.USD, "d1")
	_, err := svc.Wallet().Exchange(ctx, ledger.ExchangeInput{From: usd.ID, To: ngn.ID, Sell: ledger.Amount("1", currency.USD), Rate: "3000/2", Fee: ledger.NoFee, Reference: "fx1"})
	if !errors.Is(err, ledger.ErrInvalidInput) {
		t.Errorf("a fraction as a rate: %v", err)
	}
}

// Currency codes are trimmed and upper-cased wherever they come in.
func TestCurrencyCodeTrimmed(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	a, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: " ngn "})
	if err != nil || a.Currency != currency.NGN {
		t.Fatalf("OpenAccount: %+v %v", a, err)
	}
	if m := ledger.Minor(150, " ngn "); m.Currency != currency.NGN {
		t.Errorf("Minor: %q", m.Currency)
	}
	b := open(t, svc, currency.NGN)
	fund(t, svc, a.ID, "10", " ngn", "d1")
	if _, err := svc.Wallet().Transfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Money{Value: "5", Currency: "NGN "}, Fee: ledger.NoFee, Reference: "t1"}); err != nil {
		t.Fatal(err)
	}
}

// OpenEscrow in a currency that is no longer configured is refused before
// anything is created, and a retry must be the same escrow.
func TestOpenEscrowChecksFirst(t *testing.T) {
	loose, core := ledgertest.New(t, ledger.Config{UnknownCurrency: ledger.AllowUnknown})
	a, b := open(t, loose, "ZZZ"), open(t, loose, "ZZZ")
	fund(t, loose, a.ID, "100", "ZZZ", "d1")

	strict := ledger.New(core.Client(), ledger.Config{LedgerID: a.LedgerID})
	_, err := strict.Wallet().OpenEscrow(ctx, ledger.EscrowInput{Payer: a.ID, Beneficiary: b.ID, Amount: ledger.Amount("10", "ZZZ"), Reference: "e1"})
	if !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("unknown currency: %v", err)
	}
	if page, err := strict.Wallet().FindAccounts(ctx, ledger.AccountQuery{Kind: ledger.EscrowAccount}); err != nil || page.Total != 0 {
		t.Fatalf("an escrow account was left behind: %+v %v", page, err)
	}

	alice, bob, carol := open(t, loose, currency.NGN), open(t, loose, currency.NGN), open(t, loose, currency.NGN)
	fund(t, loose, alice.ID, "1000", currency.NGN, "d2")
	in := ledger.EscrowInput{Payer: alice.ID, Beneficiary: bob.ID, Amount: ledger.Amount("500", currency.NGN), Reference: "e2"}
	first, err := loose.Wallet().OpenEscrow(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := loose.Wallet().OpenEscrow(ctx, in); err != nil || again.ID != first.ID {
		t.Fatalf("retry: %+v %v", again, err)
	}
	for name, changed := range map[string]ledger.EscrowInput{
		"amount":      {Payer: alice.ID, Beneficiary: bob.ID, Amount: ledger.Amount("400", currency.NGN), Reference: "e2"},
		"beneficiary": {Payer: alice.ID, Beneficiary: carol.ID, Amount: ledger.Amount("500", currency.NGN), Reference: "e2"},
	} {
		if _, err := loose.Wallet().OpenEscrow(ctx, changed); !errors.Is(err, ledger.ErrDuplicateReference) {
			t.Errorf("retry with a different %s: %v", name, err)
		}
	}
}

func str(v any) string { s, _ := v.(string); return s }
