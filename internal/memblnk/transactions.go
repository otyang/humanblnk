package memblnk

import (
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// txn is a recorded transaction and, for a hold, what has become of it.
type txn struct {
	blnk.Transaction
	amount *big.Int // precise_amount

	// A hold (Inflight):
	committed *big.Int // the total of its commits
	voided    bool
	expires   time.Time
}

// remaining is what of a hold is neither committed nor voided.
func (t *txn) remaining() *big.Int {
	if t.voided {
		return new(big.Int)
	}
	return new(big.Int).Sub(t.amount, t.committed)
}

// leg is one posting of a request: a transaction Blnk records on its own.
type leg struct {
	ref    string
	source string // as named in the request: a balance id or "@indicator"
	dest   string
	amount *big.Int
}

// request is a decoded transaction request, checked and split into legs.
type request struct {
	blnk.CreateTransactionRequest
	DryRun  bool `json:"dry_run"`
	legs    []leg
	split   bool
	expires time.Time
}

// parse checks r as Blnk does and breaks it into its legs. A split merges an
// account named twice into one leg.
func parse(body []byte) (*request, *failure) {
	var r request
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, invalid(blnk.CodeMalformedRequest, "%v", err)
	}
	if err := r.check(); err != nil {
		return nil, err
	}
	return &r, nil
}

func (r *request) check() *failure {
	switch {
	case r.Reference == "":
		return invalid(blnk.CodeTxnValidation, "reference is required")
	case r.Currency == "":
		return invalid(blnk.CodeTxnValidation, "currency is required")
	case r.Description == "":
		return invalid(blnk.CodeTxnValidation, "description is required")
	case r.PreciseAmount == nil || r.PreciseAmount.Sign() <= 0:
		return invalid(blnk.CodeInvalidAmount, "precise_amount must be positive")
	case r.Precision <= 0:
		return invalid(blnk.CodePrecisionNotInteger, "precision must be a positive integer")
	case len(r.Sources) > 0 && len(r.Destinations) > 0:
		return invalid(blnk.CodeTxnValidation, "a transaction can't have both sources and destinations")
	case (r.Source == "") == (len(r.Sources) == 0):
		return invalid(blnk.CodeTxnValidation, "set one of source or sources")
	case (r.Destination == "") == (len(r.Destinations) == 0):
		return invalid(blnk.CodeTxnValidation, "set one of destination or destinations")
	}
	if r.InflightExpiryDate != "" {
		t, err := time.Parse(time.RFC3339, r.InflightExpiryDate)
		if err != nil {
			return invalid(blnk.CodeTxnValidation, "inflight_expiry_date: %v", err)
		}
		r.expires = t
	}
	total := &r.PreciseAmount.Int
	ds := r.Destinations
	if len(r.Sources) > 0 {
		ds = r.Sources
	}
	if len(ds) == 0 {
		r.legs = []leg{{ref: r.Reference, source: r.Source, dest: r.Destination, amount: new(big.Int).Set(total)}}
		return nil
	}
	r.split = true
	sum, byAccount, order := new(big.Int), map[string]*big.Int{}, []string{}
	for _, d := range ds {
		if d.PreciseDistribution == "" {
			return invalid(blnk.CodeInvalidDistribution, "memblnk takes precise_distribution only, not %q", d.Distribution)
		}
		v, ok := new(big.Int).SetString(d.PreciseDistribution, 10)
		if !ok || v.Sign() <= 0 {
			return invalid(blnk.CodeInvalidDistribution, "bad precise_distribution %q", d.PreciseDistribution)
		}
		sum.Add(sum, v)
		if prev, ok := byAccount[d.Identifier]; ok {
			prev.Add(prev, v)
			continue
		}
		byAccount[d.Identifier] = v
		order = append(order, d.Identifier)
	}
	if sum.Cmp(total) != 0 {
		return invalid(blnk.CodeInvalidDistribution, "distributions add up to %s, not %s", sum, total)
	}
	for i, account := range order {
		l := leg{ref: r.Reference + "-" + strconv.Itoa(i+1), source: r.Source, dest: account, amount: byAccount[account]}
		if len(r.Sources) > 0 {
			l.source, l.dest = account, r.Destination
		}
		r.legs = append(r.legs, l)
	}
	return nil
}

// resolve is the balance an account names: by id, or "@indicator" in the
// request's currency, made in the General Ledger the first time. With
// create false, a missing system balance comes back as a zero one not stored.
func (s *Server) resolve(account, currency string, create bool) (*blnk.Balance, bool, *failure) {
	if len(account) > 1 && account[0] == '@' {
		if b := s.indicator(account, currency); b != nil {
			return b, false, nil
		}
		b := &blnk.Balance{BalanceID: newID("bln_"), LedgerID: GeneralLedger, Indicator: account, Currency: currency}
		if create {
			b.CreatedAt = blnk.Time{Time: s.now()}
			s.addBalance(b)
		}
		return b, true, nil
	}
	b, ok := s.balances[account]
	if !ok {
		return nil, false, notFound(blnk.CodeBalanceNotFound, "balance", account)
	}
	return b, false, nil
}

// available is what a debit may take from b: balance less held and queued.
func available(b *blnk.Balance) *big.Int { return b.Available() }

// funds refuses a debit of amount from b it can't cover, unless the request
// allows an overdraft (to its limit, if it has one).
func funds(b *blnk.Balance, amount *big.Int, r *request) *failure {
	left := new(big.Int).Sub(available(b), amount)
	if left.Sign() >= 0 {
		return nil
	}
	if !r.AllowOverdraft {
		return insufficientFunds(b.BalanceID)
	}
	if r.OverdraftLimit != "" {
		limit, ok := new(big.Rat).SetString(string(r.OverdraftLimit))
		if !ok {
			return invalid(blnk.CodeTxnValidation, "bad overdraft_limit %q", r.OverdraftLimit)
		}
		limit.Mul(limit, new(big.Rat).SetInt64(r.Precision))
		if new(big.Rat).SetInt(new(big.Int).Neg(left)).Cmp(limit) > 0 {
			return insufficientFunds(b.BalanceID)
		}
	}
	return nil
}

// move applies amount from src to dst: held if inflight, else applied.
func (s *Server) move(src, dst *blnk.Balance, amount *big.Int, inflight bool) {
	if inflight {
		src.InflightDebitBalance.Add(&src.InflightDebitBalance.Int, amount)
		dst.InflightCreditBalance.Add(&dst.InflightCreditBalance.Int, amount)
	} else {
		src.DebitBalance.Add(&src.DebitBalance.Int, amount)
		src.Balance.Sub(&src.Balance.Int, amount)
		dst.CreditBalance.Add(&dst.CreditBalance.Int, amount)
		dst.Balance.Add(&dst.Balance.Int, amount)
	}
	for _, b := range []*blnk.Balance{src, dst} {
		b.InflightBalance.Sub(&b.InflightCreditBalance.Int, &b.InflightDebitBalance.Int)
		b.Version++
	}
	s.balanceChanged(src)
	if dst != src {
		s.balanceChanged(dst)
	}
}

// release takes amount off a hold's inflight balances.
func (s *Server) release(src, dst *blnk.Balance, amount *big.Int) {
	src.InflightDebitBalance.Sub(&src.InflightDebitBalance.Int, amount)
	dst.InflightCreditBalance.Sub(&dst.InflightCreditBalance.Int, amount)
	for _, b := range []*blnk.Balance{src, dst} {
		b.InflightBalance.Sub(&b.InflightCreditBalance.Int, &b.InflightDebitBalance.Int)
		b.Version++
	}
	s.balanceChanged(src)
	if dst != src {
		s.balanceChanged(dst)
	}
}

// record stores a transaction from src to dst, created at, and applies it.
func (s *Server) record(base blnk.Transaction, ref string, src, dst *blnk.Balance, amount *big.Int, expires, at time.Time) *txn {
	t := &txn{Transaction: base, amount: new(big.Int).Set(amount), committed: new(big.Int), expires: expires}
	t.TransactionID = newID("txn_")
	t.Reference = ref
	t.Source, t.Destination = src.BalanceID, dst.BalanceID
	t.Sources, t.Destinations = nil, nil
	t.PreciseAmount = blnk.BigInt{Int: *new(big.Int).Set(amount)}
	t.Amount, t.AmountString = major(amount, base.Precision)
	t.CreatedAt = blnk.Time{Time: at}
	if t.EffectiveDate.IsZero() {
		t.EffectiveDate = t.CreatedAt
	}
	t.MetaData = maps.Clone(base.MetaData)
	t.Status = blnk.StatusApplied
	if t.Inflight {
		t.Status = blnk.StatusInflight
	}
	s.txns[t.TransactionID] = t
	s.byRef[ref] = t
	s.txnOrder = append(s.txnOrder, t.TransactionID)
	s.move(src, dst, amount, t.Inflight)
	s.emit("transaction."+lower(t.Status), t.Transaction)
	return t
}

// major is amount in major units: a display float and its exact decimal.
func major(amount *big.Int, precision json.Number) (json.Number, string) {
	p, ok := new(big.Rat).SetString(precision.String())
	if !ok || p.Sign() == 0 {
		p = big.NewRat(1, 1)
	}
	r := new(big.Rat).Quo(new(big.Rat).SetInt(amount), p)
	digits := len(p.FloatString(0)) - 1 // precision 100 is 2 decimals
	exact := r.FloatString(max(digits, 0))
	f, _ := r.Float64()
	return json.Number(strconv.FormatFloat(f, 'f', -1, 64)), exact
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// base is the transaction r's legs share. Blnk records a split's postings
// with atomic false and "atomic" in their metadata instead.
func base(r *request, parent string) blnk.Transaction {
	t := blnk.Transaction{
		ParentTransaction:  parent,
		Precision:          json.Number(strconv.FormatInt(r.Precision, 10)),
		Currency:           r.Currency,
		Description:        r.Description,
		AllowOverdraft:     r.AllowOverdraft,
		OverdraftLimit:     r.OverdraftLimit,
		Atomic:             r.Atomic,
		Inflight:           r.Inflight,
		SkipQueue:          r.SkipQueue,
		InflightExpiryDate: blnk.Time{Time: r.expires},
		MetaData:           r.MetaData,
	}
	if r.split && r.Atomic {
		t.Atomic = false
		t.MetaData = maps.Clone(r.MetaData)
		if t.MetaData == nil {
			t.MetaData = blnk.MetaData{}
		}
		t.MetaData["atomic"] = true
	}
	return t
}

// apply records r's legs in order, all created at the same moment as Blnk
// records a split's postings (so a newest-first listing keeps their order).
// If one fails, those before it stay applied, are rolled back after the
// delay if the split is atomic, as Blnk does, and the failure is returned.
func (s *Server) apply(r *request, parent string) ([]*txn, *failure) {
	var done []*txn
	at := s.now()
	rollback := func(ts []*txn) {
		if r.Atomic {
			s.rollback(ts)
		}
	}
	for _, l := range r.legs {
		if s.byRef[l.ref] != nil {
			rollback(done)
			return nil, conflict(blnk.CodeDuplicateReference, "reference %s has already been used", l.ref)
		}
		src, _, err := s.resolve(l.source, r.Currency, true)
		if err != nil {
			rollback(done)
			return nil, err
		}
		dst, _, err := s.resolve(l.dest, r.Currency, true)
		if err != nil {
			rollback(done)
			return nil, err
		}
		if err := funds(src, l.amount, r); err != nil {
			rollback(done)
			return nil, err
		}
		done = append(done, s.record(base(r, parent), l.ref, src, dst, l.amount, r.expires, at))
	}
	return done, nil
}

// rollback undoes the applied part of a split or batch that failed, as
// Blnk does: holds are voided at once, and transactions refunded after the
// delay (Blnk's worker), under "<id>_refund".
func (s *Server) rollback(ts []*txn) {
	var applied []*txn
	for _, t := range ts {
		if !t.Inflight {
			applied = append(applied, t)
			continue
		}
		if left := t.remaining(); left.Sign() > 0 {
			s.release(s.balances[t.Source], s.balances[t.Destination], left)
			t.voided = true
			s.child(t, t.TransactionID+"_refund", blnk.StatusVoid, left)
		}
	}
	if len(applied) == 0 {
		return
	}
	s.later(s.rollbackDelay, func() {
		for _, t := range applied {
			src, dst := s.balances[t.Source], s.balances[t.Destination]
			ref := t.TransactionID + "_refund"
			refund := t.Transaction
			refund.ParentTransaction = ""
			refund.Inflight = false
			refund.Description = "Refund of " + t.TransactionID
			s.record(refund, ref, dst, src, t.amount, time.Time{}, s.now())
		}
	})
}

// --- Endpoints ---

func (s *Server) createTransaction(_ *http.Request, body []byte) (int, any) {
	r, err := parse(body)
	if err != nil {
		return respond(0, nil, err)
	}
	if r.DryRun {
		return http.StatusOK, s.preview(r)
	}
	if !r.split {
		ts, err := s.apply(r, "")
		if err != nil {
			return respond(0, nil, err)
		}
		return http.StatusCreated, ts[0].Transaction
	}
	parent := newID("txn_")
	ts, err := s.apply(r, parent)
	if err != nil {
		return respond(0, nil, err)
	}
	// Blnk answers a split with the split itself, which it doesn't store.
	out := base(r, "")
	out.TransactionID = parent
	out.Reference = r.Reference
	out.Source, out.Destination = r.Source, r.Destination
	out.Sources, out.Destinations = r.Sources, r.Destinations
	out.PreciseAmount = *r.PreciseAmount
	out.Amount, out.AmountString = major(&r.PreciseAmount.Int, out.Precision)
	out.Status = blnk.StatusApplied
	if r.Inflight {
		out.Status = blnk.StatusInflight
	}
	out.CreatedAt = ts[0].CreatedAt
	return http.StatusCreated, out
}

// createBulk records a batch, each item naming the batch as its parent.
// Atomic, a failing item rolls the earlier ones back as a failed split's
// postings are; otherwise the earlier ones stand.
func (s *Server) createBulk(_ *http.Request, body []byte) (int, any) {
	var in struct {
		blnk.BulkTransactionRequest
		DryRun bool `json:"dry_run"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return respond(0, nil, invalid(blnk.CodeMalformedRequest, "%v", err))
	}
	switch {
	case in.DryRun:
		return respond(0, nil, invalid(blnk.CodeBadRequest, "memblnk doesn't preview batches"))
	case len(in.Transactions) == 0:
		return respond(0, nil, invalid(blnk.CodeBulkEmpty, "no transactions"))
	}
	// Every item is checked before any is applied.
	reqs := make([]*request, len(in.Transactions))
	for i, item := range in.Transactions {
		item.Inflight = item.Inflight || in.Inflight
		item.SkipQueue = item.SkipQueue || in.SkipQueue
		raw, _ := json.Marshal(item)
		r, err := parse(raw)
		if err == nil && r.split {
			err = invalid(blnk.CodeTxnValidation, "memblnk doesn't take splits in a batch")
		}
		if err != nil {
			return respond(0, nil, invalid(err.body.ErrorDetail.Code, "transaction %d: %s", i, err.body.ErrorDetail.Message))
		}
		reqs[i] = r
	}
	batch := newID("bulk_")
	var done []*txn
	for i, r := range reqs {
		ts, err := s.apply(r, batch)
		if err != nil {
			if in.Atomic {
				s.rollback(done)
			}
			err.body.ErrorDetail.Message = fmt.Sprintf("transaction %d: %s", i, err.body.ErrorDetail.Message)
			err.body.Error = err.body.ErrorDetail.Message
			return respond(0, nil, err)
		}
		done = append(done, ts...)
	}
	status := "applied"
	if in.Inflight {
		status = "inflight"
	}
	return http.StatusCreated, blnk.BulkTransactionResponse{BatchID: batch, Status: status, TransactionCount: len(done)}
}

func (s *Server) getTransaction(r *http.Request, _ []byte) (int, any) {
	t, ok := s.txns[r.PathValue("id")]
	if !ok {
		return respond(0, nil, notFound(blnk.CodeTxnNotFound, "transaction", r.PathValue("id")))
	}
	return http.StatusOK, t.Transaction
}

// getTransactionByRef answers as Blnk does, with only some fields: no
// precision (0), overdraft or flags. Use Get for the full record.
func (s *Server) getTransactionByRef(r *http.Request, _ []byte) (int, any) {
	t, ok := s.byRef[r.PathValue("ref")]
	if !ok {
		return respond(0, nil, notFound(blnk.CodeTxnNotFound, "transaction with reference", r.PathValue("ref")))
	}
	short := t.Transaction
	short.Precision, short.OverdraftLimit, short.AmountString = "0", "0", ""
	short.AllowOverdraft, short.Atomic, short.Inflight, short.SkipQueue = false, false, false, false
	short.InflightExpiryDate, short.InflightCommitDate, short.ScheduledFor = blnk.Time{}, blnk.Time{}, blnk.Time{}
	return http.StatusOK, short
}

func (s *Server) filterTransactions(_ *http.Request, body []byte) (int, any) {
	items := make([]any, len(s.txnOrder))
	for i, id := range s.txnOrder {
		items[i] = s.txns[id].Transaction
	}
	return s.filter(body, items)
}

// --- Previews ---

// preview reports what r would do, writing nothing: each leg is projected
// on top of the ones before it.
func (s *Server) preview(r *request) blnk.TransactionPreview {
	out := blnk.TransactionPreview{
		DryRun: true, WouldApply: true, Reference: r.Reference, Currency: r.Currency,
		PreciseAmount: *r.PreciseAmount, Precision: json.Number(strconv.FormatInt(r.Precision, 10)),
		Status: blnk.StatusApplied,
	}
	if r.Inflight {
		out.Status = blnk.StatusInflight
	}
	out.Amount, _ = major(&r.PreciseAmount.Int, out.Precision)
	type projected struct {
		before, after blnk.Balance
		role          string
		virtual       bool
	}
	seen := map[string]*projected{}
	var order []string
	project := func(account, role string) (*projected, *failure) {
		b, virtual, err := s.resolve(account, r.Currency, false)
		if err != nil {
			return nil, err
		}
		key := b.BalanceID
		if virtual {
			key = account // not stored yet, so its id isn't stable
		}
		if p, ok := seen[key]; ok {
			return p, nil
		}
		p := &projected{before: copyBalance(b), after: copyBalance(b), role: role, virtual: virtual}
		seen[key] = p
		order = append(order, key)
		return p, nil
	}
	reject := func(f *failure) {
		out.WouldApply = false
		out.Rejection = &blnk.PreviewRejection{Code: f.body.ErrorDetail.Code, Reason: f.body.ErrorDetail.Code, Message: f.body.ErrorDetail.Message}
	}
	for _, l := range r.legs {
		if s.byRef[l.ref] != nil {
			reject(conflict(blnk.CodeDuplicateReference, "reference %s has already been used", l.ref))
			break
		}
		src, err := project(l.source, "source")
		if err != nil {
			reject(err)
			break
		}
		dst, err := project(l.dest, "destination")
		if err != nil {
			reject(err)
			break
		}
		if err := funds(&src.after, l.amount, r); err != nil {
			reject(err)
			break
		}
		applyTo(&src.after, &dst.after, l.amount, r.Inflight)
		leg := blnk.PreviewLeg{Identifier: l.dest, Role: "destination", PreciseAmount: blnk.BigInt{Int: *l.amount}}
		if len(r.Sources) > 0 {
			leg.Identifier, leg.Role = l.source, "source"
		}
		out.Legs = append(out.Legs, leg)
	}
	for _, key := range order {
		p := seen[key]
		out.Balances = append(out.Balances, blnk.PreviewBalance{
			BalanceID: p.before.BalanceID, Role: p.role, Currency: p.before.Currency, Virtual: p.virtual,
			CurrentBalance: p.before.Balance, CurrentAvailable: blnk.BigInt{Int: *available(&p.before)},
			CurrentCreditBalance: p.before.CreditBalance, CurrentDebitBalance: p.before.DebitBalance,
			CurrentInflightDebitBalance: p.before.InflightDebitBalance, CurrentInflightCreditBalance: p.before.InflightCreditBalance,
			ResultingBalance: p.after.Balance, ResultingAvailable: blnk.BigInt{Int: *available(&p.after)},
			ResultingCreditBalance: p.after.CreditBalance, ResultingDebitBalance: p.after.DebitBalance,
			ResultingInflightDebitBalance: p.after.InflightDebitBalance, ResultingInflightCreditBalance: p.after.InflightCreditBalance,
		})
	}
	if !r.split {
		out.Legs = nil
	}
	return out
}

// copyBalance is b with its own copies of the amounts.
func copyBalance(b *blnk.Balance) blnk.Balance {
	c := *b
	for _, f := range []*blnk.BigInt{&c.Balance, &c.CreditBalance, &c.DebitBalance, &c.InflightBalance, &c.InflightCreditBalance, &c.InflightDebitBalance, &c.QueuedCreditBalance, &c.QueuedDebitBalance} {
		f.Int = *new(big.Int).Set(&f.Int)
	}
	return c
}

// applyTo is move on copies, without side effects.
func applyTo(src, dst *blnk.Balance, amount *big.Int, inflight bool) {
	if inflight {
		src.InflightDebitBalance.Add(&src.InflightDebitBalance.Int, amount)
		dst.InflightCreditBalance.Add(&dst.InflightCreditBalance.Int, amount)
		return
	}
	src.DebitBalance.Add(&src.DebitBalance.Int, amount)
	src.Balance.Sub(&src.Balance.Int, amount)
	dst.CreditBalance.Add(&dst.CreditBalance.Int, amount)
	dst.Balance.Add(&dst.Balance.Int, amount)
}
