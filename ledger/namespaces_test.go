package ledger_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/ledgertest"
)

// Ledger, customer-account and house-account methods each act on one kind of object only.

func feeIncomeID(t *testing.T, svc *ledger.Service) ledger.AccountID {
	t.Helper()
	a, err := svc.Ops().GetHouseAccount(ctx, ledger.HouseRef{Indicator: ledger.FeeIncomeAccount, Currency: "NGN"})
	if err != nil {
		t.Fatal(err)
	}
	return ledger.AccountID(a.ID)
}

func TestAccountsRefuseOtherKindsOfID(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	a := open(t, svc, "NGN")
	if _, err := svc.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: a.ID, Amount: ledger.Amount("1000", "NGN"),
		Fee: ledger.Amount("10", "NGN"), Reference: "d1", Provider: "paystack"}); err != nil {
		t.Fatal(err)
	}
	fee := feeIncomeID(t, svc) // a real balance id ("bln_…") of a house account
	ledgerID := ledger.AccountID(a.LedgerID)

	for name, id := range map[string]ledger.AccountID{
		"house balance id": fee, "indicator": ledger.FeeIncomeAccount, "ledger id": ledgerID,
		"general ledger": ledger.AccountID(ledger.GeneralLedgerID), "transaction id": "txn_000001",
	} {
		calls := map[string]error{}
		_, calls["Get"] = svc.Wallet().GetAccount(ctx, id)
		calls["Freeze"] = svc.Ops().FreezeAccount(ctx, id, "oops")
		calls["Unfreeze"] = svc.Ops().UnfreezeAccount(ctx, id)
		_, calls["Close"] = svc.Ops().CloseAccount(ctx, id, ledger.CloseInput{SweepTo: a.ID, Reference: "sweep"})
		_, calls["History"] = svc.Wallet().History(ctx, id, ledger.HistoryQuery{})
		_, calls["BalanceAt"] = svc.Wallet().BalanceAt(ctx, id, core.Now())
		_, calls["Statement"] = svc.Wallet().Statement(ctx, id, core.Now().Add(-time.Hour), core.Now())
		_, calls["LimitUsage"] = svc.Wallet().LimitUsage(ctx, id)
		_, calls["ListPots"] = svc.Wallet().ListPots(ctx, id)
		_, calls["Wallet.GetEscrow"] = svc.Wallet().GetEscrow(ctx, id)
		for call, err := range calls {
			if !errors.Is(err, ledger.ErrWrongKind) || !errors.Is(err, ledger.ErrInvalidInput) {
				t.Errorf("%s with %s: want ErrWrongKind, got %v", call, name, err)
			}
		}
	}

	// Nothing was written: the fee income still has its money, the ledger has no status.
	if got := sys(t, svc, ledger.FeeIncomeAccount, "NGN"); got != "10" {
		t.Fatalf("fee income touched: %s", got)
	}
	l, err := svc.Ops().GetLedger(ctx, a.LedgerID)
	if err != nil || l.Status != "active" {
		t.Fatalf("ledger touched: %+v %v", l, err)
	}
}

func TestCustomerOnlyInputsRefuseHouseAccounts(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	a := open(t, svc, "NGN")
	sysAcct := ledger.AccountID(ledger.SuspenseAccount)
	errs := map[string]error{}
	_, errs["Deposit"] = svc.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: sysAcct, Amount: ledger.Amount("1", "NGN"), Fee: ledger.NoFee, Reference: "x1"})
	_, errs["Withdraw"] = svc.Wallet().Withdraw(ctx, ledger.WithdrawInput{AccountID: sysAcct, Amount: ledger.Amount("1", "NGN"), Fee: ledger.NoFee, Reference: "x2"})
	_, errs["Hold"] = svc.Wallet().Hold(ctx, ledger.HoldInput{AccountID: sysAcct, Destination: a.ID, Amount: ledger.Amount("1", "NGN"), Reference: "x3"})
	_, errs["Exchange"] = svc.Wallet().Exchange(ctx, ledger.ExchangeInput{From: a.ID, To: sysAcct, Sell: ledger.Amount("1", "NGN"), Rate: "1", Fee: ledger.NoFee, Reference: "x4"})
	_, errs["ClaimFromSuspense"] = svc.Ops().ClaimFromSuspense(ctx, ledger.ClaimFromSuspenseInput{AccountID: ledger.AccountID(ledger.DepositClearingAccount), Amount: ledger.Amount("1", "NGN"), Reference: "x5"})
	_, errs["Adjust"] = svc.Ops().Adjust(ctx, ledger.AdjustInput{AccountID: sysAcct, Type: ledger.AdjustCredit, Amount: ledger.Amount("1", "NGN"), Reason: "r", RequestedBy: "ops.kemi", ApprovedBy: "ops", Reference: "x6"})
	_, errs["Escrow payer"] = svc.Wallet().OpenEscrow(ctx, ledger.EscrowInput{Payer: sysAcct, Beneficiary: a.ID, Amount: ledger.Amount("1", "NGN"), Reference: "x7"})
	_, errs["OpenPot"] = svc.Wallet().OpenPot(ctx, ledger.PotInput{ParentID: sysAcct, Name: "Rent"})
	for call, err := range errs {
		if !errors.Is(err, ledger.ErrWrongKind) {
			t.Errorf("%s: want ErrWrongKind, got %v", call, err)
		}
	}
	// Adjust's counterparty must be a house account.
	_, err := svc.Ops().Adjust(ctx, ledger.AdjustInput{AccountID: a.ID, Type: ledger.AdjustCredit, Amount: ledger.Amount("1", "NGN"),
		Reason: "r", RequestedBy: "ops.kemi", ApprovedBy: "ops", Reference: "x8", Counterparty: string(open(t, svc, "NGN").ID)})
	if !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("customer counterparty: %v", err)
	}
}

func TestFindNeverListsHouseAccounts(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	a := open(t, svc, "NGN")
	fund(t, svc, a.ID, "100", "NGN", "in") // creates @PaystackClearing in NGN
	p, err := svc.Wallet().FindAccounts(ctx, ledger.AccountQuery{Currency: "NGN"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].ID != a.ID || p.Total != 1 {
		t.Fatalf("find: %+v", p)
	}
	if _, err := svc.Wallet().FindAccounts(ctx, ledger.AccountQuery{Currency: "NGN", Kind: "system"}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("kind system: %v", err)
	}
	sysAccts, err := svc.Ops().ListHouseAccounts(ctx, "NGN")
	if err != nil || len(sysAccts) != 1 || sysAccts[0].Indicator != "@PaystackClearing" {
		t.Fatalf("house list: %+v %v", sysAccts, err)
	}
}

func TestOpenIgnoresManagedMetadata(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	a, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN", MetaData: map[string]any{
		"account_kind": "escrow", "status": "frozen", "external_id": "spoof", "owner": "ada",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != ledger.CustomerAccount || a.Status != "active" || a.MetaData["external_id"] != nil || a.MetaData["owner"] != "ada" {
		t.Fatalf("managed keys not dropped: %+v", a)
	}
	// Pots still get their kind.
	pot, err := svc.Wallet().OpenPot(ctx, ledger.PotInput{ParentID: a.ID, Name: "Rent"})
	if err != nil || pot.Kind != ledger.PotAccount || pot.Parent != a.ID {
		t.Fatalf("pot: %+v %v", pot, err)
	}
}

func TestLedgersCreateIsIdempotent(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	first, err := svc.Ops().CreateLedger(ctx, ledger.CreateLedgerInput{Key: "wallets-ng", Name: "Wallets NG", MetaData: map[string]any{"region": "ng", "status": "frozen"}})
	if err != nil || first.Replayed || first.Key != "wallets-ng" || first.Status != "active" || first.MetaData["region"] != "ng" {
		t.Fatalf("create: %+v %v", first, err)
	}
	again, err := svc.Ops().CreateLedger(ctx, ledger.CreateLedgerInput{Key: "wallets-ng", Name: "Wallets NG"})
	if err != nil || !again.Replayed || again.ID != first.ID {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if _, err := svc.Ops().CreateLedger(ctx, ledger.CreateLedgerInput{Key: "wallets-ng", Name: "Something else"}); !errors.Is(err, ledger.ErrDuplicateReference) {
		t.Fatalf("same key, other name: %v", err)
	}
	if _, err := svc.Ops().CreateLedger(ctx, ledger.CreateLedgerInput{Name: "No key"}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("no key: %v", err)
	}
	byKey := 0
	for offset := 0; ; offset += 100 {
		p, err := svc.Ops().ListLedgers(ctx, ledger.LedgerQuery{Limit: 100, Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range p.Items {
			if l.Key == "wallets-ng" {
				byKey++
			}
		}
		if len(p.Items) < 100 {
			break
		}
	}
	if byKey != 1 {
		t.Fatalf("want one ledger for the key, got %d", byKey)
	}
	// Name defaults to the key.
	if l, err := svc.Ops().CreateLedger(ctx, ledger.CreateLedgerInput{Key: "payroll"}); err != nil || l.Name != "payroll" {
		t.Fatalf("default name: %+v %v", l, err)
	}
}

func TestLedgersCreateSettlesRacingTwins(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	// Two processes raced past the lookup and both created the ledger.
	c := core.Client()
	md := blnk.MetaData{"ledger_key": "wallets", "status": "active"}
	older, err := c.CreateLedger(ctx, blnk.CreateLedgerRequest{Name: "Wallets", MetaData: md})
	if err != nil {
		t.Fatal(err)
	}
	core.Advance(time.Second)
	newer, err := c.CreateLedger(ctx, blnk.CreateLedgerRequest{Name: "Wallets", MetaData: md})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.Ops().CreateLedger(ctx, ledger.CreateLedgerInput{Key: "wallets", Name: "Wallets"})
	if err != nil || got.ID != ledger.LedgerID(older.LedgerID) || !got.Replayed {
		t.Fatalf("want the oldest ledger %s: %+v %v", older.LedgerID, got, err)
	}
	dup, err := svc.Ops().GetLedger(ctx, ledger.LedgerID(newer.LedgerID))
	if err != nil || dup.Status != "duplicate" || dup.DuplicateOf != got.ID {
		t.Fatalf("twin not marked: %+v %v", dup, err)
	}
	_, err = svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN", LedgerID: dup.ID})
	if !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("open in duplicate: %v", err)
	}
	if err := svc.Ops().FreezeLedger(ctx, dup.ID, "x"); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("freeze duplicate: %v", err)
	}
	if _, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN", LedgerID: got.ID}); err != nil {
		t.Fatalf("open in canonical: %v", err)
	}
}

func TestOpenChecksTheLedger(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	_, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN", LedgerID: "ldg_missing"})
	if !errors.Is(err, ledger.ErrLedgerNotFound) || !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("missing ledger: %v", err)
	}
	_, err = svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN", LedgerID: ledger.GeneralLedgerID})
	if !errors.Is(err, ledger.ErrWrongKind) {
		t.Fatalf("general ledger: %v", err)
	}
	_, err = svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN", LedgerID: "bln_000001"})
	if !errors.Is(err, ledger.ErrWrongKind) {
		t.Fatalf("balance id as ledger: %v", err)
	}
	if err := svc.Ops().FreezeLedger(ctx, ledger.GeneralLedgerID, "x"); !errors.Is(err, ledger.ErrWrongKind) {
		t.Fatalf("freeze general ledger: %v", err)
	}
	if _, err := svc.Ops().GetLedger(ctx, "bln_000001"); !errors.Is(err, ledger.ErrWrongKind) {
		t.Fatalf("balance id to Ledgers: %v", err)
	}
}

func TestCheck(t *testing.T) {
	good, _ := ledgertest.New(t, ledger.Config{})
	if err := good.Check(ctx); err != nil {
		t.Fatal(err)
	}
	bad, _ := ledgertest.New(t, ledger.Config{LedgerID: "ldg_typo"})
	if err := bad.Check(ctx); !errors.Is(err, ledger.ErrLedgerNotFound) {
		t.Fatalf("bad ledger id: %v", err)
	}
	core := ledgertest.NewServer()
	defer core.Close()
	if err := ledger.New(core.Client(), ledger.Config{}).Check(ctx); err != nil {
		t.Fatalf("no ledger configured: %v", err)
	}
	// A misconfigured client fails Check before anything else.
	cloud := ledger.New(blnk.New(blnk.CloudProxyURL), ledger.Config{})
	if err := cloud.Check(ctx); err == nil || !strings.Contains(err.Error(), "instance ID") {
		t.Fatalf("cloud without instance id: %v", err)
	}
	if err := ledger.New(blnk.New(""), ledger.Config{}).Check(ctx); err == nil {
		t.Fatal("empty URL must fail Check")
	}
}

func TestQuoteExchange(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	usd, ngn := open(t, svc, "USD"), open(t, svc, "NGN")
	fund(t, svc, usd.ID, "150", "USD", "in")
	in := ledger.ExchangeInput{From: usd.ID, To: ngn.ID, Sell: ledger.Amount("100", "USD"), Rate: "1532.40",
		Fee: ledger.Amount("1", ""), Reference: "fx_q"}
	q, err := svc.Wallet().QuoteExchange(ctx, in)
	if err != nil || !q.Allowed || !q.CheckedByLedger || q.Bought.Value != "153240" || q.Debit.Value != "101" ||
		q.AvailableBefore.Value != "150" || q.AvailableAfter.Value != "49" || q.Fee.Value != "1" {
		t.Fatalf("quote: %+v %v", q, err)
	}
	if got := bal(t, svc, usd.ID); got != "150" {
		t.Fatalf("a quote must not move money: %s", got)
	}
	in.Sell = ledger.Amount("200", "USD")
	q, err = svc.Wallet().QuoteExchange(ctx, in)
	if err != nil || q.Allowed || !errors.Is(q.Reason, ledger.ErrInsufficientFunds) {
		t.Fatalf("too much: %+v %v", q, err)
	}
	if err := svc.Ops().FreezeAccount(ctx, usd.ID, "review"); err != nil {
		t.Fatal(err)
	}
	in.Sell = ledger.Amount("10", "USD")
	q, err = svc.Wallet().QuoteExchange(ctx, in)
	if err != nil || q.Allowed || !errors.Is(q.Reason, ledger.ErrAccountFrozen) {
		t.Fatalf("frozen: %+v %v", q, err)
	}
	in.Rate = "abc"
	if _, err := svc.Wallet().QuoteExchange(ctx, in); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("bad rate is an error, not a refusal: %v", err)
	}
}

func TestFrozenLedgerBlocksOnlyOutgoingMoney(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	ng, err := svc.Ops().CreateLedger(ctx, ledger.CreateLedgerInput{Key: "ng"})
	if err != nil {
		t.Fatal(err)
	}
	in := func(cur string) *ledger.Account {
		a, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: cur, LedgerID: ng.ID})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	a, b := in("NGN"), in("NGN")
	outside := open(t, svc, "NGN") // in the default ledger
	fund(t, svc, a.ID, "1000", "NGN", "a_in")
	fund(t, svc, outside.ID, "1000", "NGN", "o_in")

	if err := svc.Ops().FreezeLedger(ctx, ng.ID, "regulator hold"); err != nil {
		t.Fatal(err)
	}
	if l, _ := svc.Ops().GetLedger(ctx, ng.ID); l.Status != "frozen" || l.MetaData["status_reason"] != "regulator hold" {
		t.Fatalf("status: %+v", l)
	}
	// Out: refused.
	_, err = svc.Wallet().Transfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("10", "NGN"), Fee: ledger.NoFee, Reference: "t1"})
	if !errors.Is(err, ledger.ErrLedgerFrozen) {
		t.Fatalf("transfer out: %v", err)
	}
	_, err = svc.Wallet().Withdraw(ctx, ledger.WithdrawInput{AccountID: a.ID, Amount: ledger.Amount("10", "NGN"), Fee: ledger.NoFee, Reference: "w1"})
	if !errors.Is(err, ledger.ErrLedgerFrozen) {
		t.Fatalf("withdraw: %v", err)
	}
	q, err := svc.Wallet().QuoteWithdraw(ctx, ledger.WithdrawInput{AccountID: a.ID, Amount: ledger.Amount("10", "NGN"), Fee: ledger.NoFee, Reference: "w1"})
	if err != nil || q.Allowed || !errors.Is(q.Reason, ledger.ErrLedgerFrozen) {
		t.Fatalf("quote: %+v %v", q, err)
	}
	// In: still allowed, from outside and from another ledger, with a fee to @FeeIncome.
	if _, err := svc.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: b.ID, Amount: ledger.Amount("100", "NGN"),
		Fee: ledger.Amount("5", "NGN"), Reference: "b_in", Provider: "paystack"}); err != nil {
		t.Fatalf("deposit in: %v", err)
	}
	if _, err := svc.Wallet().Transfer(ctx, ledger.TransferInput{From: outside.ID, To: a.ID, Amount: ledger.Amount("10", "NGN"),
		Fee: ledger.Amount("1", "NGN"), Reference: "o_to_a"}); err != nil {
		t.Fatalf("transfer in: %v", err)
	}
	// House accounts are never blocked.
	if _, err := svc.Ops().RecordSettlement(ctx, ledger.SettlementInput{Provider: "paystack", Amount: ledger.Amount("100", "NGN"), Reference: "s1"}); err != nil {
		t.Fatalf("settlement: %v", err)
	}
	if got := sys(t, svc, ledger.FeeIncomeAccount, "NGN"); got != "6" {
		t.Fatalf("fee income: %s", got)
	}

	_, err = svc.Ops().CloseAccount(ctx, a.ID, ledger.CloseInput{SweepTo: outside.ID, Reference: "close_a", Reason: "r", RequestedBy: "ops.kemi", ApprovedBy: "ops"})
	if !errors.Is(err, ledger.ErrLedgerFrozen) {
		t.Fatalf("close sweeping out of a frozen ledger: %v", err)
	}
	if err := svc.Ops().UnfreezeLedger(ctx, ng.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Wallet().Transfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("10", "NGN"), Fee: ledger.NoFee, Reference: "t2"}); err != nil {
		t.Fatalf("after unfreeze: %v", err)
	}
	_ = core
}

func TestLedgerStatusCacheExpires(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	other := ledger.New(core.Client(), ledger.Config{Now: core.Now}) // another process
	a, b := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, a.ID, "100", "NGN", "in")
	move := func(ref string) error {
		_, err := svc.Wallet().Transfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("1", "NGN"), Fee: ledger.NoFee, Reference: ref})
		return err
	}
	if err := move("t1"); err != nil { // caches the ledger as active
		t.Fatal(err)
	}
	if err := other.Ops().FreezeLedger(ctx, a.LedgerID, "x"); err != nil {
		t.Fatal(err)
	}
	if err := move("t2"); err != nil {
		t.Fatalf("within the TTL the cached status applies: %v", err)
	}
	core.Advance(31 * time.Second)
	if err := move("t3"); !errors.Is(err, ledger.ErrLedgerFrozen) {
		t.Fatalf("after the TTL: %v", err)
	}
}

func TestHouseNamespace(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	a := open(t, svc, "NGN")
	start := core.Now()
	if _, err := svc.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: a.ID, Amount: ledger.Amount("1000", "NGN"),
		Fee: ledger.Amount("15", "NGN"), Reference: "d1", Provider: "paystack"}); err != nil {
		t.Fatal(err)
	}
	core.Advance(time.Minute)
	ref := ledger.HouseRef{Indicator: "@PaystackClearing", Currency: "ngn"}
	clr, err := svc.Ops().GetHouseAccount(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if clr.Type != ledger.GLAsset || clr.Normal != ledger.DebitNormal || clr.Balance.Value != "-1000" ||
		clr.Natural.Value != "1000" || clr.LedgerID != ledger.GeneralLedgerID || clr.Ref().Indicator != ref.Indicator {
		t.Fatalf("clearing: %+v", clr)
	}
	fee, _ := svc.Ops().GetHouseAccount(ctx, ledger.HouseRef{Indicator: ledger.FeeIncomeAccount, Currency: "NGN"})
	if fee.Type != ledger.GLIncome || fee.Natural.Value != "15" {
		t.Fatalf("fee: %+v", fee)
	}
	h, err := svc.Ops().HouseHistory(ctx, ref, ledger.HistoryQuery{})
	if err != nil || len(h.Items) == 0 {
		t.Fatalf("history: %+v %v", h, err)
	}
	at, err := svc.Ops().HouseBalanceAt(ctx, ref, core.Now())
	if err != nil || at.Value != "-1000" {
		t.Fatalf("balance at: %v %v", at, err)
	}
	st, err := svc.Ops().HouseStatement(ctx, ledger.HouseRef{Indicator: ledger.FeeIncomeAccount, Currency: "NGN"}, start, core.Now())
	if err != nil || st.Indicator != ledger.FeeIncomeAccount || st.Closing.Value != "15" || len(st.Lines) != 1 {
		t.Fatalf("statement: %+v %v", st, err)
	}
	// House-account methods take indicators only.
	for _, bad := range []ledger.HouseRef{{Indicator: string(a.ID), Currency: "NGN"}, {Indicator: string(a.LedgerID), Currency: "NGN"}} {
		if _, err := svc.Ops().GetHouseAccount(ctx, bad); !errors.Is(err, ledger.ErrWrongKind) {
			t.Fatalf("%s: %v", bad, err)
		}
	}
	if _, err := svc.Ops().GetHouseAccount(ctx, ledger.HouseRef{Indicator: "@FeeIncome"}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("no currency: %v", err)
	}
	tb, err := svc.Ops().TrialBalance(ctx, "NGN")
	if err != nil || !tb.Balanced || len(tb.House) != 2 {
		t.Fatalf("trial balance: %+v %v", tb, err)
	}
	// Watch a house account by its ref.
	w, err := svc.Ops().WatchBalance(ctx, ledger.WatchInput{House: ref, Operator: "<", Threshold: ledger.Amount("0", "")})
	if err != nil || w.AccountID != clr.ID {
		t.Fatalf("watch: %+v %v", w, err)
	}
	if _, err := svc.Ops().WatchBalance(ctx, ledger.WatchInput{AccountID: a.ID, House: ref, Operator: "<"}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("both set: %v", err)
	}
}

func TestReturnFromSuspense(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	a := open(t, svc, "NGN")
	for _, ref := range []string{"unknown_1", "unknown_2"} {
		if _, err := svc.Ops().HoldInSuspense(ctx, ledger.SuspenseInput{Amount: ledger.Amount("500", "NGN"), Provider: "paystack", Reference: ref}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Ops().ClaimFromSuspense(ctx, ledger.ClaimFromSuspenseInput{AccountID: a.ID, Amount: ledger.Amount("500", "NGN"), Reference: "alloc_1", SuspenseReference: "unknown_1"}); err != nil {
		t.Fatal(err)
	}
	r, err := svc.Ops().ReturnFromSuspense(ctx, ledger.ReturnFromSuspenseInput{Provider: "paystack", Amount: ledger.Amount("500", "NGN"), Reference: "ret_2", SuspenseReference: "unknown_2"})
	if err != nil || r.MetaData["txn_type"] != "suspense_return" {
		t.Fatalf("return: %+v %v", r, err)
	}
	if got := sys(t, svc, ledger.SuspenseAccount, "NGN"); got != "0" {
		t.Fatalf("suspense: %s", got)
	}
	if got := sys(t, svc, "@PaystackClearing", "NGN"); got != "-500" {
		t.Fatalf("clearing: %s", got)
	}
	if got := bal(t, svc, a.ID); got != "500" {
		t.Fatalf("account: %s", got)
	}
	if _, err := svc.Ops().ReturnFromSuspense(ctx, ledger.ReturnFromSuspenseInput{Amount: ledger.Amount("1", "NGN"), Reference: "ret_3"}); !errors.Is(err, ledger.ErrInsufficientFunds) {
		t.Fatalf("return more than held: %v", err)
	}
}

func TestInstrumentCoversNewNamespaces(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	var ops []string
	l := ledger.Instrument(svc, ledger.ObserverFunc(func(c context.Context, op string) (context.Context, func(error)) {
		ops = append(ops, op)
		return c, func(error) {}
	}))
	_, _ = l.Ops().CreateLedger(ctx, ledger.CreateLedgerInput{Key: "k"})
	_, _ = l.Ops().ListHouseAccounts(ctx, "NGN")
	if len(ops) != 2 || ops[0] != "Ops.CreateLedger" || ops[1] != "Ops.ListHouseAccounts" {
		t.Fatalf("ops: %v", ops)
	}
}

func TestCloseIsGuarded(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	main, other := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, main.ID, "1000", "NGN", "in")
	ok := ledger.CloseInput{Reason: "customer request", ApprovedBy: "ops.lead", RequestedBy: "support.ada", SweepTo: other.ID, Reference: "close_main"}
	try := func(in ledger.CloseInput) error {
		_, err := svc.Ops().CloseAccount(ctx, main.ID, in)
		return err
	}

	// A reason and a second person's approval are required.
	noReason := ok
	noReason.Reason = ""
	if err := try(noReason); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("no reason: %v", err)
	}
	noApprover := ok
	noApprover.ApprovedBy = ""
	if err := try(noApprover); !errors.Is(err, ledger.ErrApprovalRequired) {
		t.Fatalf("no approver: %v", err)
	}
	self := ok
	self.ApprovedBy = "Support.Ada"
	if err := try(self); !errors.Is(err, ledger.ErrApprovalRequired) {
		t.Fatalf("self-approval: %v", err)
	}

	// A scheduled transfer blocks closing until it has posted.
	if _, err := svc.Wallet().Transfer(ctx, ledger.TransferInput{From: main.ID, To: other.ID, Amount: ledger.Amount("10", "NGN"),
		Fee: ledger.NoFee, Reference: "later", ScheduledFor: core.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := try(ok); !errors.Is(err, ledger.ErrPendingActivity) {
		t.Fatalf("scheduled transfer: %v", err)
	}
	core.Advance(2 * time.Hour) // it posts

	// An open pot blocks closing.
	pot, err := svc.Wallet().OpenPot(ctx, ledger.PotInput{ParentID: main.ID, Name: "Rent"})
	if err != nil {
		t.Fatal(err)
	}
	if err := try(ok); !errors.Is(err, ledger.ErrPendingActivity) {
		t.Fatalf("open pot: %v", err)
	}
	if _, err := svc.Ops().CloseAccount(ctx, pot.ID, ledger.CloseInput{Reason: "parent closing", RequestedBy: "ops.kemi", ApprovedBy: "ops.lead"}); err != nil {
		t.Fatalf("close pot: %v", err)
	}

	// An escrow still holding money blocks closing, as payer or beneficiary.
	esc, err := svc.Wallet().OpenEscrow(ctx, ledger.EscrowInput{Payer: main.ID, Beneficiary: other.ID, Amount: ledger.Amount("100", "NGN"), Reference: "esc"})
	if err != nil {
		t.Fatal(err)
	}
	if err := try(ok); !errors.Is(err, ledger.ErrPendingActivity) {
		t.Fatalf("escrow holding money: %v", err)
	}
	if _, err := svc.Wallet().RefundEscrow(ctx, esc.ID, ledger.EscrowSettleInput{Reference: "esc_back"}); err != nil {
		t.Fatal(err)
	}

	closed, err := svc.Ops().CloseAccount(ctx, main.ID, ok)
	if err != nil || closed.Status != "closed" || closed.Balance.Value != "0" ||
		closed.MetaData["closed_approved_by"] != "ops.lead" || closed.MetaData["status_reason"] != "customer request" {
		t.Fatalf("close: %+v %v", closed, err)
	}
	if got := bal(t, svc, other.ID); got != "1000" {
		t.Fatalf("swept: %s", got)
	}
	// Closing again is a no-op, and a closed account can't be unfrozen back to life.
	if _, err := svc.Ops().CloseAccount(ctx, main.ID, ledger.CloseInput{}); err != nil {
		t.Fatalf("close again: %v", err)
	}
	if err := svc.Ops().UnfreezeAccount(ctx, main.ID); !errors.Is(err, ledger.ErrAccountClosed) {
		t.Fatalf("unfreeze closed: %v", err)
	}
}

func TestStatusIsAlwaysEnforced(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{}) // no option to turn it on: it always is
	a, b, c := open(t, svc, "NGN"), open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, a.ID, "100", "NGN", "in")
	pay, err := svc.Wallet().Transfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("40", "NGN"), Fee: ledger.NoFee, Reference: "p"})
	if err != nil {
		t.Fatal(err)
	}

	// A full refund moves money too, so it is checked like any movement:
	// out of a frozen recipient...
	if err := svc.Ops().FreezeAccount(ctx, b.ID, "review"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Wallet().Refund(ctx, pay.TransactionID, ledger.RefundInput{}); !errors.Is(err, ledger.ErrAccountFrozen) {
		t.Fatalf("refund out of frozen account: %v", err)
	}
	if err := svc.Ops().UnfreezeAccount(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	// ...or back into a closed payer.
	if _, err := svc.Ops().CloseAccount(ctx, a.ID, ledger.CloseInput{Reason: "left", RequestedBy: "ops.kemi", ApprovedBy: "ops", SweepTo: c.ID, Reference: "close_a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Wallet().Refund(ctx, pay.TransactionID, ledger.RefundInput{}); !errors.Is(err, ledger.ErrAccountClosed) {
		t.Fatalf("refund into closed account: %v", err)
	}
	if _, err := svc.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: a.ID, Amount: ledger.Amount("5", "NGN"), Fee: ledger.NoFee, Reference: "late"}); !errors.Is(err, ledger.ErrAccountClosed) {
		t.Fatalf("late deposit into closed account: %v", err)
	}
	// A closure can't sweep into a closed account either.
	if _, err := svc.Ops().CloseAccount(ctx, b.ID, ledger.CloseInput{Reason: "left", RequestedBy: "ops.kemi", ApprovedBy: "ops", SweepTo: a.ID, Reference: "close_b"}); !errors.Is(err, ledger.ErrAccountClosed) {
		t.Fatalf("sweep into closed account: %v", err)
	}
}
