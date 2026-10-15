package ledger_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/ledgertest"
)

// Ledgers(), Accounts() and System() each act on one kind of object only.

func feeIncomeID(t *testing.T, svc *ledger.Service) ledger.AccountID {
	t.Helper()
	a, err := svc.System().Get(ctx, ledger.SystemRef{Indicator: ledger.FeeIncomeAccount, Currency: "NGN"})
	if err != nil {
		t.Fatal(err)
	}
	return ledger.AccountID(a.ID)
}

func TestAccountsRefuseOtherKindsOfID(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	a := open(t, svc, "NGN")
	if _, err := svc.Payments().Deposit(ctx, ledger.DepositInput{AccountID: a.ID, Amount: ledger.Amount("1000", "NGN"),
		Fee: ledger.Amount("10", "NGN"), Reference: "d1", Provider: "paystack"}); err != nil {
		t.Fatal(err)
	}
	fee := feeIncomeID(t, svc) // a real balance id ("bln_…") of a system account
	ledgerID := ledger.AccountID(a.LedgerID)

	for name, id := range map[string]ledger.AccountID{
		"system balance id": fee, "indicator": ledger.FeeIncomeAccount, "ledger id": ledgerID,
		"general ledger": ledger.AccountID(ledger.GeneralLedgerID), "transaction id": "txn_000001",
	} {
		calls := map[string]error{}
		_, calls["Get"] = svc.Accounts().Get(ctx, id)
		calls["Freeze"] = svc.Accounts().Freeze(ctx, id, "oops")
		calls["Unfreeze"] = svc.Accounts().Unfreeze(ctx, id)
		_, calls["Close"] = svc.Accounts().Close(ctx, id, ledger.CloseInput{SweepTo: a.ID, Reference: "sweep"})
		_, calls["History"] = svc.Accounts().History(ctx, id, ledger.HistoryQuery{})
		_, calls["BalanceAt"] = svc.Accounts().BalanceAt(ctx, id, core.Now())
		_, calls["Statement"] = svc.Accounts().Statement(ctx, id, core.Now().Add(-time.Hour), core.Now())
		_, calls["LimitUsage"] = svc.Accounts().LimitUsage(ctx, id)
		_, calls["ListPots"] = svc.Customers().ListPots(ctx, id)
		_, calls["Escrows.Get"] = svc.Escrows().Get(ctx, id)
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
	l, err := svc.Ledgers().Get(ctx, a.LedgerID)
	if err != nil || l.Status != "active" {
		t.Fatalf("ledger touched: %+v %v", l, err)
	}
}

func TestCustomerOnlyInputsRefuseSystemAccounts(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	a := open(t, svc, "NGN")
	sysAcct := ledger.AccountID(ledger.SuspenseAccount)
	errs := map[string]error{}
	_, errs["Deposit"] = svc.Payments().Deposit(ctx, ledger.DepositInput{AccountID: sysAcct, Amount: ledger.Amount("1", "NGN"), Fee: ledger.NoFee, Reference: "x1"})
	_, errs["Withdraw"] = svc.Payments().Withdraw(ctx, ledger.WithdrawInput{AccountID: sysAcct, Amount: ledger.Amount("1", "NGN"), Fee: ledger.NoFee, Reference: "x2"})
	_, errs["Hold"] = svc.Pending().Hold(ctx, ledger.HoldInput{AccountID: sysAcct, Destination: a.ID, Amount: ledger.Amount("1", "NGN"), Reference: "x3"})
	_, errs["Exchange"] = svc.Payments().Exchange(ctx, ledger.ExchangeInput{From: a.ID, To: sysAcct, Sell: ledger.Amount("1", "NGN"), Rate: "1", Fee: ledger.NoFee, Reference: "x4"})
	_, errs["AllocateSuspense"] = svc.Operations().AllocateSuspense(ctx, ledger.AllocateInput{AccountID: ledger.AccountID(ledger.DepositClearingAccount), Amount: ledger.Amount("1", "NGN"), Reference: "x5"})
	_, errs["Adjust"] = svc.Operations().Adjust(ctx, ledger.AdjustInput{AccountID: sysAcct, Type: ledger.AdjustCredit, Amount: ledger.Amount("1", "NGN"), Reason: "r", ApprovedBy: "ops", Reference: "x6"})
	_, errs["Escrow payer"] = svc.Escrows().Open(ctx, ledger.EscrowInput{Payer: sysAcct, Beneficiary: a.ID, Amount: ledger.Amount("1", "NGN"), Reference: "x7"})
	_, errs["OpenPot"] = svc.Customers().OpenPot(ctx, ledger.PotInput{ParentID: sysAcct, Name: "Rent"})
	for call, err := range errs {
		if !errors.Is(err, ledger.ErrWrongKind) {
			t.Errorf("%s: want ErrWrongKind, got %v", call, err)
		}
	}
	// Adjust's counterparty must be a system account.
	_, err := svc.Operations().Adjust(ctx, ledger.AdjustInput{AccountID: a.ID, Type: ledger.AdjustCredit, Amount: ledger.Amount("1", "NGN"),
		Reason: "r", ApprovedBy: "ops", Reference: "x8", Counterparty: string(open(t, svc, "NGN").ID)})
	if !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("customer counterparty: %v", err)
	}
}

func TestFindNeverListsSystemAccounts(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	a := open(t, svc, "NGN")
	fund(t, svc, a.ID, "100", "NGN", "in") // creates @PaystackClearing in NGN
	p, err := svc.Accounts().Find(ctx, ledger.AccountQuery{Currency: "NGN"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].ID != a.ID || p.Total != 1 {
		t.Fatalf("find: %+v", p)
	}
	if _, err := svc.Accounts().Find(ctx, ledger.AccountQuery{Currency: "NGN", Kind: "system"}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("kind system: %v", err)
	}
	sysAccts, err := svc.System().List(ctx, "NGN")
	if err != nil || len(sysAccts) != 1 || sysAccts[0].Indicator != "@PaystackClearing" {
		t.Fatalf("system list: %+v %v", sysAccts, err)
	}
}

func TestOpenIgnoresManagedMetadata(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	a, err := svc.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN", MetaData: map[string]any{
		"account_kind": "escrow", "status": "frozen", "external_id": "spoof", "owner": "ada",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != ledger.CustomerAccount || a.Status != "active" || a.MetaData["external_id"] != nil || a.MetaData["owner"] != "ada" {
		t.Fatalf("managed keys not dropped: %+v", a)
	}
	// Pots still get their kind.
	pot, err := svc.Customers().OpenPot(ctx, ledger.PotInput{ParentID: a.ID, Name: "Rent"})
	if err != nil || pot.Kind != ledger.PotAccount || pot.Parent != a.ID {
		t.Fatalf("pot: %+v %v", pot, err)
	}
}

func TestLedgersCreateIsIdempotent(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	first, err := svc.Ledgers().Create(ctx, ledger.CreateLedgerInput{Key: "wallets-ng", Name: "Wallets NG", MetaData: map[string]any{"region": "ng", "status": "frozen"}})
	if err != nil || first.Replayed || first.Key != "wallets-ng" || first.Status != "active" || first.MetaData["region"] != "ng" {
		t.Fatalf("create: %+v %v", first, err)
	}
	again, err := svc.Ledgers().Create(ctx, ledger.CreateLedgerInput{Key: "wallets-ng", Name: "Wallets NG"})
	if err != nil || !again.Replayed || again.ID != first.ID {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if _, err := svc.Ledgers().Create(ctx, ledger.CreateLedgerInput{Key: "wallets-ng", Name: "Something else"}); !errors.Is(err, ledger.ErrDuplicateReference) {
		t.Fatalf("same key, other name: %v", err)
	}
	if _, err := svc.Ledgers().Create(ctx, ledger.CreateLedgerInput{Name: "No key"}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("no key: %v", err)
	}
	byKey := 0
	for offset := 0; ; offset += 100 {
		p, err := svc.Ledgers().List(ctx, ledger.LedgerQuery{Limit: 100, Offset: offset})
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
	if l, err := svc.Ledgers().Create(ctx, ledger.CreateLedgerInput{Key: "payroll"}); err != nil || l.Name != "payroll" {
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

	got, err := svc.Ledgers().Create(ctx, ledger.CreateLedgerInput{Key: "wallets", Name: "Wallets"})
	if err != nil || got.ID != ledger.LedgerID(older.LedgerID) || !got.Replayed {
		t.Fatalf("want the oldest ledger %s: %+v %v", older.LedgerID, got, err)
	}
	dup, err := svc.Ledgers().Get(ctx, ledger.LedgerID(newer.LedgerID))
	if err != nil || dup.Status != "duplicate" || dup.DuplicateOf != got.ID {
		t.Fatalf("twin not marked: %+v %v", dup, err)
	}
	_, err = svc.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN", LedgerID: dup.ID})
	if !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("open in duplicate: %v", err)
	}
	if err := svc.Ledgers().Freeze(ctx, dup.ID, "x"); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("freeze duplicate: %v", err)
	}
	if _, err := svc.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN", LedgerID: got.ID}); err != nil {
		t.Fatalf("open in canonical: %v", err)
	}
}

func TestOpenChecksTheLedger(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	_, err := svc.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN", LedgerID: "ldg_missing"})
	if !errors.Is(err, ledger.ErrLedgerNotFound) || !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("missing ledger: %v", err)
	}
	_, err = svc.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN", LedgerID: ledger.GeneralLedgerID})
	if !errors.Is(err, ledger.ErrWrongKind) {
		t.Fatalf("general ledger: %v", err)
	}
	_, err = svc.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN", LedgerID: "bln_000001"})
	if !errors.Is(err, ledger.ErrWrongKind) {
		t.Fatalf("balance id as ledger: %v", err)
	}
	if err := svc.Ledgers().Freeze(ctx, ledger.GeneralLedgerID, "x"); !errors.Is(err, ledger.ErrWrongKind) {
		t.Fatalf("freeze general ledger: %v", err)
	}
	if _, err := svc.Ledgers().Get(ctx, "bln_000001"); !errors.Is(err, ledger.ErrWrongKind) {
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
	if err := ledger.New(nil, ledger.Config{}).Check(ctx); err != nil {
		t.Fatalf("no ledger configured: %v", err)
	}
}

func TestFrozenLedgerBlocksOnlyOutgoingMoney(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{EnforceAccountStatus: true})
	ng, err := svc.Ledgers().Create(ctx, ledger.CreateLedgerInput{Key: "ng"})
	if err != nil {
		t.Fatal(err)
	}
	in := func(cur string) *ledger.Account {
		a, err := svc.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: cur, LedgerID: ng.ID})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	a, b := in("NGN"), in("NGN")
	outside := open(t, svc, "NGN") // in the default ledger
	fund(t, svc, a.ID, "1000", "NGN", "a_in")
	fund(t, svc, outside.ID, "1000", "NGN", "o_in")

	if err := svc.Ledgers().Freeze(ctx, ng.ID, "regulator hold"); err != nil {
		t.Fatal(err)
	}
	if l, _ := svc.Ledgers().Get(ctx, ng.ID); l.Status != "frozen" || l.MetaData["status_reason"] != "regulator hold" {
		t.Fatalf("status: %+v", l)
	}
	// Out: refused.
	_, err = svc.Payments().Transfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("10", "NGN"), Fee: ledger.NoFee, Reference: "t1"})
	if !errors.Is(err, ledger.ErrLedgerFrozen) {
		t.Fatalf("transfer out: %v", err)
	}
	_, err = svc.Payments().Withdraw(ctx, ledger.WithdrawInput{AccountID: a.ID, Amount: ledger.Amount("10", "NGN"), Fee: ledger.NoFee, Reference: "w1"})
	if !errors.Is(err, ledger.ErrLedgerFrozen) {
		t.Fatalf("withdraw: %v", err)
	}
	q, err := svc.Payments().QuoteWithdraw(ctx, ledger.WithdrawInput{AccountID: a.ID, Amount: ledger.Amount("10", "NGN"), Fee: ledger.NoFee, Reference: "w1"})
	if err != nil || q.Allowed || !errors.Is(q.Reason, ledger.ErrLedgerFrozen) {
		t.Fatalf("quote: %+v %v", q, err)
	}
	// In: still allowed, from outside and from another ledger, with a fee to @FeeIncome.
	if _, err := svc.Payments().Deposit(ctx, ledger.DepositInput{AccountID: b.ID, Amount: ledger.Amount("100", "NGN"),
		Fee: ledger.Amount("5", "NGN"), Reference: "b_in", Provider: "paystack"}); err != nil {
		t.Fatalf("deposit in: %v", err)
	}
	if _, err := svc.Payments().Transfer(ctx, ledger.TransferInput{From: outside.ID, To: a.ID, Amount: ledger.Amount("10", "NGN"),
		Fee: ledger.Amount("1", "NGN"), Reference: "o_to_a"}); err != nil {
		t.Fatalf("transfer in: %v", err)
	}
	// System accounts are never blocked.
	if _, err := svc.Operations().RecordSettlement(ctx, ledger.SettlementInput{Provider: "paystack", Amount: ledger.Amount("100", "NGN"), Reference: "s1"}); err != nil {
		t.Fatalf("settlement: %v", err)
	}
	if got := sys(t, svc, ledger.FeeIncomeAccount, "NGN"); got != "6" {
		t.Fatalf("fee income: %s", got)
	}

	_, err = svc.Accounts().Close(ctx, a.ID, ledger.CloseInput{SweepTo: outside.ID, Reference: "close_a"})
	if !errors.Is(err, ledger.ErrLedgerFrozen) {
		t.Fatalf("close sweeping out of a frozen ledger: %v", err)
	}
	if err := svc.Ledgers().Unfreeze(ctx, ng.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Payments().Transfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("10", "NGN"), Fee: ledger.NoFee, Reference: "t2"}); err != nil {
		t.Fatalf("after unfreeze: %v", err)
	}
	_ = core
}

func TestLedgerStatusCacheExpires(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{EnforceAccountStatus: true})
	other := ledger.New(core.Client(), ledger.Config{Now: core.Now}) // another process
	a, b := open(t, svc, "NGN"), open(t, svc, "NGN")
	fund(t, svc, a.ID, "100", "NGN", "in")
	move := func(ref string) error {
		_, err := svc.Payments().Transfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("1", "NGN"), Fee: ledger.NoFee, Reference: ref})
		return err
	}
	if err := move("t1"); err != nil { // caches the ledger as active
		t.Fatal(err)
	}
	if err := other.Ledgers().Freeze(ctx, a.LedgerID, "x"); err != nil {
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

func TestSystemNamespace(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	a := open(t, svc, "NGN")
	start := core.Now()
	if _, err := svc.Payments().Deposit(ctx, ledger.DepositInput{AccountID: a.ID, Amount: ledger.Amount("1000", "NGN"),
		Fee: ledger.Amount("15", "NGN"), Reference: "d1", Provider: "paystack"}); err != nil {
		t.Fatal(err)
	}
	core.Advance(time.Minute)
	ref := ledger.SystemRef{Indicator: "@PaystackClearing", Currency: "ngn"}
	clr, err := svc.System().Get(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if clr.Type != ledger.GLAsset || clr.Normal != ledger.DebitNormal || clr.Balance.Value != "-1000" ||
		clr.Natural.Value != "1000" || clr.LedgerID != ledger.GeneralLedgerID || clr.Ref().Indicator != ref.Indicator {
		t.Fatalf("clearing: %+v", clr)
	}
	fee, _ := svc.System().Get(ctx, ledger.SystemRef{Indicator: ledger.FeeIncomeAccount, Currency: "NGN"})
	if fee.Type != ledger.GLIncome || fee.Natural.Value != "15" {
		t.Fatalf("fee: %+v", fee)
	}
	h, err := svc.System().History(ctx, ref, ledger.HistoryQuery{})
	if err != nil || len(h.Items) == 0 {
		t.Fatalf("history: %+v %v", h, err)
	}
	at, err := svc.System().BalanceAt(ctx, ref, core.Now())
	if err != nil || at.Value != "-1000" {
		t.Fatalf("balance at: %v %v", at, err)
	}
	st, err := svc.System().Statement(ctx, ledger.SystemRef{Indicator: ledger.FeeIncomeAccount, Currency: "NGN"}, start, core.Now())
	if err != nil || st.Indicator != ledger.FeeIncomeAccount || st.Closing.Value != "15" || len(st.Lines) != 1 {
		t.Fatalf("statement: %+v %v", st, err)
	}
	// System() takes indicators only.
	for _, bad := range []ledger.SystemRef{{Indicator: string(a.ID), Currency: "NGN"}, {Indicator: string(a.LedgerID), Currency: "NGN"}} {
		if _, err := svc.System().Get(ctx, bad); !errors.Is(err, ledger.ErrWrongKind) {
			t.Fatalf("%s: %v", bad, err)
		}
	}
	if _, err := svc.System().Get(ctx, ledger.SystemRef{Indicator: "@FeeIncome"}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("no currency: %v", err)
	}
	tb, err := svc.Operations().TrialBalance(ctx, "NGN")
	if err != nil || !tb.Balanced || len(tb.System) != 2 {
		t.Fatalf("trial balance: %+v %v", tb, err)
	}
	// Watch a system account by its ref.
	w, err := svc.Operations().WatchBalance(ctx, ledger.WatchInput{System: ref, Operator: "<", Threshold: ledger.Amount("0", "")})
	if err != nil || w.AccountID != clr.ID {
		t.Fatalf("watch: %+v %v", w, err)
	}
	if _, err := svc.Operations().WatchBalance(ctx, ledger.WatchInput{AccountID: a.ID, System: ref, Operator: "<"}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("both set: %v", err)
	}
}

func TestReturnSuspense(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	a := open(t, svc, "NGN")
	for _, ref := range []string{"unknown_1", "unknown_2"} {
		if _, err := svc.Operations().ParkInSuspense(ctx, ledger.SuspenseInput{Amount: ledger.Amount("500", "NGN"), Provider: "paystack", Reference: ref}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Operations().AllocateSuspense(ctx, ledger.AllocateInput{AccountID: a.ID, Amount: ledger.Amount("500", "NGN"), Reference: "alloc_1", SuspenseReference: "unknown_1"}); err != nil {
		t.Fatal(err)
	}
	r, err := svc.Operations().ReturnSuspense(ctx, ledger.ReturnSuspenseInput{Provider: "paystack", Amount: ledger.Amount("500", "NGN"), Reference: "ret_2", SuspenseReference: "unknown_2"})
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
	if _, err := svc.Operations().ReturnSuspense(ctx, ledger.ReturnSuspenseInput{Amount: ledger.Amount("1", "NGN"), Reference: "ret_3"}); !errors.Is(err, ledger.ErrInsufficientFunds) {
		t.Fatalf("return more than parked: %v", err)
	}
}

func TestInstrumentCoversNewNamespaces(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	var ops []string
	l := ledger.Instrument(svc, ledger.ObserverFunc(func(c context.Context, op string) (context.Context, func(error)) {
		ops = append(ops, op)
		return c, func(error) {}
	}))
	_, _ = l.Ledgers().Create(ctx, ledger.CreateLedgerInput{Key: "k"})
	_, _ = l.System().List(ctx, "NGN")
	if len(ops) != 2 || ops[0] != "Ledgers.Create" || ops[1] != "System.List" {
		t.Fatalf("ops: %v", ops)
	}
}
