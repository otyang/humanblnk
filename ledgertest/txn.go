package ledgertest

import (
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

type txn struct {
	blnk.Transaction
	precise   big.Int   // amount, smallest unit
	committed big.Int   // inflight: amount already committed
	voided    bool      // inflight: remainder released
	legs      []string  // split parent (never stored): its leg transactions
	expiresAt time.Time // inflight auto-void
	commitAt  time.Time // inflight auto-commit
	runAt     time.Time // scheduled
	refunded  bool
	batch     string
}

func (t *txn) view() blnk.Transaction {
	v := t.Transaction
	v.PreciseAmount = blnk.BigInt{}
	v.PreciseAmount.Set(&t.precise)
	if t.MetaData != nil {
		v.MetaData = blnk.MetaData{}
		for k, x := range t.MetaData {
			v.MetaData[k] = x
		}
	}
	v.Destinations = append([]blnk.Distribution(nil), t.Destinations...)
	return v
}

// stored is how Blnk 0.15 returns a stored row on reads: precision and
// amount_string come back empty.
func (t *txn) stored() blnk.Transaction {
	v := t.view()
	v.Precision, v.AmountString = "0", ""
	return v
}

// ids are the stored transactions behind t: its legs, or itself.
func (t *txn) ids() []string {
	if len(t.legs) > 0 {
		return t.legs
	}
	return []string{t.TransactionID}
}

func (t *txn) remaining() *big.Int { return new(big.Int).Sub(&t.precise, &t.committed) }

// leg is one source→destination movement inside a transaction.
type leg struct {
	src, dst *balance
	amt      *big.Int
	dist     blnk.Distribution
}

func (s *Server) createTxn(in blnk.CreateTransactionRequest, batch string) (*txn, error) {
	t, legs, err := s.plan(in)
	if err != nil {
		return nil, err
	}
	if in.DryRun {
		return t, nil
	}
	s.commitPlan(t, legs, batch)
	return t, nil
}

// plan validates a request and works out its legs without changing anything.
func (s *Server) plan(in blnk.CreateTransactionRequest) (*txn, []leg, error) {
	if strings.TrimSpace(in.Reference) == "" {
		return nil, nil, fail(400, blnk.CodeTxnValidation, "reference is required")
	}
	if _, dup := s.refs[in.Reference]; dup || s.splitRefs[in.Reference] {
		return nil, nil, fail(409, blnk.CodeDuplicateReference, "reference %s has already been used", in.Reference)
	}
	if in.PreciseAmount == nil || in.PreciseAmount.Sign() <= 0 {
		return nil, nil, fail(400, blnk.CodeInvalidAmount, "precise_amount must be positive")
	}
	if in.Currency == "" {
		return nil, nil, fail(400, blnk.CodeTxnValidation, "currency is required")
	}
	if strings.TrimSpace(in.Description) == "" {
		return nil, nil, fail(400, blnk.CodeTxnValidation, "description: cannot be blank.") // as Blnk 0.15
	}
	if len(in.Sources) > 0 {
		return nil, nil, fail(400, blnk.CodeTxnValidation, "emulator: multiple sources are not supported")
	}
	if (in.Destination == "") == (len(in.Destinations) == 0) {
		return nil, nil, fail(400, blnk.CodeTxnValidation, "set destination or destinations")
	}
	total := new(big.Int).Set(&in.PreciseAmount.Int)
	src, err := s.resolve(in.Source, in.Currency)
	if err != nil {
		return nil, nil, err
	}

	var legs []leg
	if in.Destination != "" {
		dst, err := s.resolve(in.Destination, in.Currency)
		if err != nil {
			return nil, nil, err
		}
		legs = []leg{{src, dst, total, blnk.Distribution{Identifier: in.Destination}}}
	} else {
		amts, err := distribute(total, in.Destinations)
		if err != nil {
			return nil, nil, err
		}
		for i, d := range in.Destinations {
			dst, err := s.resolve(d.Identifier, in.Currency)
			if err != nil {
				return nil, nil, err
			}
			legs = append(legs, leg{src, dst, amts[i], d})
		}
	}
	for _, l := range legs {
		if l.src == l.dst {
			return nil, nil, fail(400, blnk.CodeTxnValidation, "source and destination are the same balance")
		}
	}
	if !in.AllowOverdraft && in.ScheduledFor == "" && src.available().Cmp(total) < 0 {
		return nil, nil, fail(400, blnk.CodeInsufficientFunds, "insufficient funds in source balance %s", in.Source)
	}

	t := &txn{Transaction: blnk.Transaction{
		TransactionID: s.id("txn"), Reference: in.Reference, Currency: strings.ToUpper(in.Currency),
		Precision: jsonNum(in.Precision), Source: in.Source, Destination: in.Destination,
		Description: in.Description, AllowOverdraft: in.AllowOverdraft, Inflight: in.Inflight,
		SkipQueue: in.SkipQueue, CreatedAt: blnk.Time{Time: s.now}, EffectiveDate: blnk.Time{Time: s.now}, MetaData: in.MetaData,
		AmountString: decimal(total, in.Precision),
	}}
	t.Amount = jsonNumber(t.AmountString)
	t.precise.Set(total)
	switch {
	case in.ScheduledFor != "":
		at, err := time.Parse(time.RFC3339Nano, in.ScheduledFor)
		if err != nil {
			return nil, nil, fail(400, blnk.CodeTxnValidation, "scheduled_for must be RFC3339")
		}
		t.Status, t.runAt, t.ScheduledFor = blnk.StatusScheduled, at, blnk.Time{Time: at}
	case in.Inflight:
		t.Status = blnk.StatusInflight
	default:
		t.Status = blnk.StatusApplied
	}
	if in.Inflight {
		if t.expiresAt, err = optTime(in.InflightExpiryDate); err != nil {
			return nil, nil, err
		}
		if t.commitAt, err = optTime(in.InflightCommitDate); err != nil {
			return nil, nil, err
		}
		t.InflightExpiryDate = blnk.Time{Time: t.expiresAt}
		t.InflightCommitDate = blnk.Time{Time: t.commitAt}
	}
	return t, legs, nil
}

// commitPlan stores a planned transaction and moves the money. Like Blnk
// 0.15, a transaction with several destinations is stored only as its legs
// ("<ref>-1", "<ref>-2", …, each with parent_transaction = the parent id);
// the parent is returned to the caller but never stored. "@Name" accounts
// are stored by balance id.
func (s *Server) commitPlan(t *txn, legs []leg, batch string) {
	if t.Destination != "" {
		t.Source, t.Destination, t.batch = legs[0].src.BalanceID, legs[0].dst.BalanceID, batch
		s.store(t)
		if t.Status != blnk.StatusScheduled {
			s.apply(t, legs, t.Status == blnk.StatusInflight)
		}
		return
	}
	s.splitRefs[t.Reference] = true
	// Blnk doesn't keep legs in request order; storing them reversed makes
	// code that assumes the order fail in tests rather than in production.
	dests := make([]blnk.Distribution, len(legs))
	order := make([]int, len(legs))
	for i := range legs {
		order[i] = len(legs) - 1 - i
	}
	for n, i := range order {
		l := legs[i]
		c := &txn{Transaction: t.Transaction, expiresAt: t.expiresAt, commitAt: t.commitAt, runAt: t.runAt, batch: batch}
		c.TransactionID = s.id("txn")
		c.Reference = fmt.Sprintf("%s-%d", t.Reference, n+1)
		c.Source, c.Destination = l.src.BalanceID, l.dst.BalanceID
		c.ParentTransaction = t.TransactionID
		c.Destinations = nil
		c.MetaData = blnk.MetaData{}
		for k, v := range t.MetaData {
			c.MetaData[k] = v
		}
		c.precise.Set(l.amt)
		c.AmountString = decimal(l.amt, precisionOf(t))
		c.Amount = jsonNumber(c.AmountString)
		s.store(c)
		if c.Status != blnk.StatusScheduled {
			s.apply(c, []leg{l}, c.Status == blnk.StatusInflight)
		}
		t.legs = append(t.legs, c.TransactionID)
		dests[i] = l.dist
		dests[i].TransactionID = c.TransactionID
	}
	t.Destinations = dests
	s.splits[t.TransactionID] = t.legs
}

func (s *Server) apply(t *txn, legs []leg, inflight bool) {
	for _, l := range legs {
		if inflight {
			reserve(l.src, l.dst, l.amt, +1)
		} else {
			post(l.src, l.dst, l.amt)
		}
		l.src.snap(s.now)
		l.dst.snap(s.now)
		s.checkMonitors(l.src)
		s.checkMonitors(l.dst)
	}
	s.emit(eventFor(t.Status), t.view())
}

func (s *Server) store(t *txn) {
	s.txns[t.TransactionID] = t
	s.refs[t.Reference] = t.TransactionID
	s.order = append(s.order, t.TransactionID)
}

// legsOf returns the movement of a stored (single-destination) transaction.
func (s *Server) legsOf(t *txn) []leg {
	src, _ := s.resolve(t.Source, t.Currency)
	dst, _ := s.resolve(t.Destination, t.Currency)
	return []leg{{src, dst, new(big.Int).Set(&t.precise), blnk.Distribution{Identifier: t.Destination}}}
}

// distribute turns destinations into exact amounts: precise_distribution
// (smallest unit), "N%" of the total, and one "left" for the remainder.
func distribute(total *big.Int, ds []blnk.Distribution) ([]*big.Int, error) {
	out := make([]*big.Int, len(ds))
	sum := new(big.Int)
	left := -1
	for i, d := range ds {
		switch {
		case d.PreciseDistribution != "":
			v, ok := new(big.Int).SetString(d.PreciseDistribution, 10)
			if !ok || v.Sign() <= 0 {
				return nil, fail(400, blnk.CodeInvalidDistribution, "invalid precise_distribution %q", d.PreciseDistribution)
			}
			out[i] = v
		case d.Distribution == "left":
			if left >= 0 {
				return nil, fail(400, blnk.CodeInvalidDistribution, "only one destination can take what is left")
			}
			left = i
			continue
		case strings.HasSuffix(d.Distribution, "%"):
			p, ok := new(big.Rat).SetString(strings.TrimSuffix(d.Distribution, "%"))
			if !ok || p.Sign() <= 0 {
				return nil, fail(400, blnk.CodeInvalidDistribution, "invalid distribution %q", d.Distribution)
			}
			v := new(big.Rat).Mul(new(big.Rat).SetInt(total), p)
			v.Quo(v, big.NewRat(100, 1))
			out[i] = new(big.Int).Quo(v.Num(), v.Denom())
		default:
			return nil, fail(400, blnk.CodeInvalidDistribution, "destination %d needs a distribution", i)
		}
		sum.Add(sum, out[i])
	}
	rest := new(big.Int).Sub(total, sum)
	switch {
	case rest.Sign() < 0:
		return nil, fail(400, blnk.CodeInvalidDistribution, "distributions exceed the amount")
	case left >= 0:
		out[left] = rest
	case rest.Sign() != 0:
		return nil, fail(400, blnk.CodeInvalidDistribution, "distributions don't add up to the amount")
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Inflight
// ---------------------------------------------------------------------------

func (s *Server) updateInflight(id string, in blnk.UpdateInflightRequest) (*txn, error) {
	if ids, ok := s.batches[id]; ok {
		var last *txn
		for _, tid := range ids {
			t, err := s.inflightAction(tid, in.Status, nil)
			if err != nil {
				return nil, err
			}
			last = t
		}
		return last, nil
	}
	var partial *big.Int
	if in.PreciseAmount != nil {
		partial = &in.PreciseAmount.Int
	}
	return s.inflightAction(id, in.Status, partial)
}

// inflightAction commits (optionally partially) or voids an inflight
// transaction, recording the outcome as a child transaction.
func (s *Server) inflightAction(id, action string, partial *big.Int) (*txn, error) {
	if legIDs, isSplit := s.splits[id]; isSplit {
		// Like Blnk 0.15, a partial amount is committed on each leg in turn;
		// a leg smaller than it fails the call and leaves the earlier legs
		// committed.
		var first *txn
		for _, lid := range legIDs {
			c, err := s.inflightAction(lid, action, partial)
			if err != nil {
				return nil, err
			}
			if first == nil {
				first = c // Blnk answers with the first leg's outcome
			}
		}
		return first, nil
	}
	t, ok := s.txns[id]
	if !ok {
		return nil, fail(404, blnk.CodeTxnNotFound, "transaction %s not found", id)
	}
	if t.Status != blnk.StatusInflight {
		return nil, fail(400, blnk.CodeNotInflight, "transaction %s is not inflight", id)
	}
	if t.voided {
		return nil, fail(400, blnk.CodeAlreadyVoided, "transaction %s has already been voided", id)
	}
	rem := t.remaining()
	if rem.Sign() == 0 {
		return nil, fail(400, blnk.CodeAlreadyCommitted, "transaction %s has already been committed", id)
	}
	legs := s.legsOf(t)

	switch action {
	case blnk.ActionCommit:
		amt := rem
		if partial != nil {
			if partial.Sign() <= 0 {
				return nil, fail(400, blnk.CodeInvalidAmount, "amount must be positive")
			}
			if partial.Cmp(rem) > 0 {
				return nil, fail(400, blnk.CodeCommitAmountExceeded, "commit amount exceeds the remaining inflight amount")
			}
			amt = new(big.Int).Set(partial)
		}
		legs[0].amt = amt
		for _, l := range legs {
			reserve(l.src, l.dst, l.amt, -1)
			post(l.src, l.dst, l.amt)
		}
		t.committed.Add(&t.committed, amt)
		return s.child(t, blnk.StatusApplied, amt, legs), nil
	case blnk.ActionVoid:
		legs[0].amt = rem
		for _, l := range legs {
			reserve(l.src, l.dst, l.amt, -1)
		}
		t.voided = true
		return s.child(t, blnk.StatusVoid, rem, legs), nil
	}
	return nil, fail(400, blnk.CodeInvalidStatusAction, "status must be commit or void")
}

func (s *Server) child(parent *txn, status string, amt *big.Int, legs []leg) *txn {
	c := &txn{Transaction: parent.Transaction}
	c.TransactionID = s.id("txn")
	c.Reference = "ref_" + c.TransactionID
	c.ParentTransaction = parent.TransactionID
	c.Status = status
	c.Inflight = true // as Blnk marks the commit / void record of a pending transaction
	c.Destinations = nil
	c.CreatedAt = blnk.Time{Time: s.now}
	c.EffectiveDate = c.CreatedAt
	c.precise.Set(amt)
	c.AmountString = decimal(amt, precisionOf(parent))
	c.Amount = jsonNumber(c.AmountString)
	s.store(c)
	for _, l := range legs {
		l.src.snap(s.now)
		l.dst.snap(s.now)
		s.checkMonitors(l.src)
		s.checkMonitors(l.dst)
	}
	s.emit(eventFor(status), c.view())
	return c
}

func (s *Server) bulkInflight(items []blnk.BulkCommitItem, action string) blnk.BulkInflightResponse {
	var out blnk.BulkInflightResponse
	for _, it := range items {
		var partial *big.Int
		if it.PreciseAmount != nil {
			partial = &it.PreciseAmount.Int
		}
		r := blnk.BulkInflightResult{TransactionID: it.TransactionID, Status: "succeeded"}
		if _, err := s.inflightAction(it.TransactionID, action, partial); err != nil {
			ae := err.(*apiErr)
			r.Status, r.Code, r.Message = "failed", ae.code, ae.msg
			out.Failed++
		} else {
			out.Succeeded++
		}
		out.Results = append(out.Results, r)
	}
	return out
}

// runDue posts scheduled transactions and expires / auto-commits inflight ones.
func (s *Server) runDue() {
	for _, id := range append([]string(nil), s.order...) {
		t := s.txns[id]
		switch {
		case t.Status == blnk.StatusScheduled && !t.runAt.After(s.now):
			legs := s.legsOf(t)
			if !t.AllowOverdraft && legs[0].src.available().Cmp(&t.precise) < 0 {
				t.Status = blnk.StatusRejected
				s.emit(eventFor(t.Status), t.view())
				continue
			}
			t.Status = blnk.StatusApplied
			t.EffectiveDate = blnk.Time{Time: s.now} // counts from when it posted
			s.apply(t, legs, false)
		case t.Status == blnk.StatusInflight && !t.voided && t.remaining().Sign() > 0:
			if !t.commitAt.IsZero() && !t.commitAt.After(s.now) {
				_, _ = s.inflightAction(id, blnk.ActionCommit, nil)
			} else if !t.expiresAt.IsZero() && !t.expiresAt.After(s.now) {
				_, _ = s.inflightAction(id, blnk.ActionVoid, nil)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Refunds and bulk
// ---------------------------------------------------------------------------

func (s *Server) refund(id string, in blnk.RefundRequest) (*txn, error) {
	t, ok := s.txns[id]
	if !ok {
		return nil, fail(404, blnk.CodeTxnNotFound, "transaction %s not found", id)
	}
	if t.refunded {
		return nil, fail(400, blnk.CodeAlreadyRefunded, "transaction %s has already been refunded", id)
	}
	if t.Status != blnk.StatusApplied {
		return nil, fail(400, blnk.CodeTxnValidation, "only applied transactions can be refunded")
	}
	// Blnk reverses with allow_overdraft: true, so a refund never fails for funds.
	legs := s.legsOf(t)
	r := &txn{Transaction: t.Transaction}
	r.TransactionID = s.id("txn")
	r.Reference = t.TransactionID + "_refund"
	r.AllowOverdraft = true
	r.Source, r.Destination = t.Destination, t.Source
	r.ParentTransaction = t.TransactionID
	r.Status = blnk.StatusApplied
	r.CreatedAt = blnk.Time{Time: s.now}
	r.EffectiveDate = r.CreatedAt
	r.Description = firstNonEmpty(in.Description, "Refund of "+t.Reference)
	r.MetaData = blnk.MetaData{}
	for k, v := range t.MetaData {
		r.MetaData[k] = v
	}
	for k, v := range in.MetaData {
		r.MetaData[k] = v
	}
	r.precise.Set(&t.precise)
	s.store(r)
	for _, l := range legs {
		post(l.dst, l.src, l.amt)
		l.src.snap(s.now)
		l.dst.snap(s.now)
	}
	t.refunded = true
	s.emit(eventFor(r.Status), r.view())
	return r, nil
}

func (s *Server) bulk(in blnk.BulkTransactionRequest) (any, int, error) {
	if len(in.Transactions) == 0 {
		return nil, 0, fail(400, blnk.CodeBulkEmpty, "no transactions")
	}
	batch := s.id("batch")
	status := blnk.StatusApplied
	if in.Inflight {
		status = blnk.StatusInflight
	}
	seen := map[string]bool{}
	var ids []string
	if in.Atomic {
		// Validate everything first; plan() checks funds against current
		// balances, so apply as we go and roll back on failure.
		saved := s.save()
		for _, r := range in.Transactions {
			r.Inflight = in.Inflight
			if seen[r.Reference] {
				s.restore(saved)
				return nil, 0, fail(400, blnk.CodeDuplicateReference, "duplicate reference %s in batch", r.Reference)
			}
			seen[r.Reference] = true
			t, err := s.createTxn(r, batch)
			if err != nil {
				s.restore(saved)
				return nil, 0, err
			}
			ids = append(ids, t.ids()...)
		}
	} else {
		for _, r := range in.Transactions {
			r.Inflight = in.Inflight
			if t, err := s.createTxn(r, batch); err == nil {
				ids = append(ids, t.ids()...)
			}
		}
	}
	s.batches[batch] = ids
	out := blnk.BulkTransactionResponse{BatchID: batch, Status: strings.ToLower(status), TransactionCount: len(ids)}
	if in.RunAsync {
		out.Status = "processing"
		s.emit("bulk_transaction.applied", out)
	}
	return out, 201, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func eventFor(status string) string {
	switch status {
	case blnk.StatusApplied:
		return "transaction.applied"
	case blnk.StatusInflight:
		return "transaction.inflight"
	case blnk.StatusVoid:
		return "transaction.void"
	case blnk.StatusRejected:
		return "transaction.rejected"
	case blnk.StatusScheduled:
		return "transaction.scheduled"
	}
	return "transaction." + strings.ToLower(status)
}

func optTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fail(400, blnk.CodeTxnValidation, "invalid date %q", s)
	}
	return t, nil
}

func precisionOf(t *txn) int64 {
	p, _ := t.Precision.Int64()
	return p
}

func decimal(v *big.Int, precision int64) string {
	if precision <= 1 {
		return v.String()
	}
	r := new(big.Rat).SetFrac(v, big.NewInt(precision))
	dp := len(fmt.Sprint(precision)) - 1
	out := r.FloatString(dp)
	if strings.Contains(out, ".") {
		out = strings.TrimRight(strings.TrimRight(out, "0"), ".")
	}
	return out
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// preview answers a dry_run request (Core 0.15.3+): would it apply, and what
// would the balances become. Nothing is stored.
func (s *Server) preview(in blnk.CreateTransactionRequest) (any, int, error) {
	if _, dup := s.refs[in.Reference]; dup || s.splitRefs[in.Reference] {
		return nil, 0, fail(409, blnk.CodeDuplicateReference, "reference %s has already been used", in.Reference)
	}
	in.ScheduledFor = "" // ignored by dry run
	out := blnk.TransactionPreview{DryRun: true, WouldApply: true, Reference: in.Reference,
		Currency: strings.ToUpper(in.Currency), Precision: jsonNum(in.Precision)}
	if in.PreciseAmount != nil {
		out.PreciseAmount.Set(&in.PreciseAmount.Int)
	}
	saved := s.save()
	s.quiet = true
	defer func() { s.restore(saved); s.quiet = false }()
	t, legs, err := s.plan(in)
	if err != nil {
		ae, ok := err.(*apiErr)
		if !ok || ae.code != blnk.CodeInsufficientFunds {
			return nil, 0, err
		}
		out.WouldApply = false
		out.Rejection = &struct {
			Code    string `json:"code"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		}{ae.code, "insufficient_funds", ae.msg}
		return out, 200, nil
	}
	out.Status = t.Status
	seen := map[string]bool{}
	before := map[string][2]big.Int{}
	for _, l := range legs {
		for _, b := range []*balance{l.src, l.dst} {
			if !seen[b.BalanceID] {
				seen[b.BalanceID] = true
				var pair [2]big.Int
				pair[0].Set(&b.Balance.Balance.Int)
				pair[1].Set(b.available())
				before[b.BalanceID] = pair
			}
		}
	}
	s.apply(t, legs, t.Status == blnk.StatusInflight)
	for _, l := range legs {
		for role, b := range map[string]*balance{"source": l.src, "destination": l.dst} {
			if !seen[b.BalanceID] {
				continue
			}
			seen[b.BalanceID] = false
			pb := blnk.PreviewBalance{BalanceID: b.BalanceID, Role: role, Currency: b.Currency}
			pair := before[b.BalanceID]
			pb.CurrentBalance.Set(&pair[0])
			pb.CurrentAvailable.Set(&pair[1])
			pb.ResultingBalance.Set(&b.Balance.Balance.Int)
			pb.ResultingAvail.Set(b.available())
			out.Balances = append(out.Balances, pb)
		}
	}
	return out, 200, nil
}

// reconcile runs an instant one-to-one reconciliation: an external record
// matches an unmatched APPLIED transaction when every criterion of any rule
// holds (equals on reference / currency / amount with drift, contains on
// description).
func (s *Server) reconcile(in blnk.StartReconciliationRequest) (any, int, error) {
	if len(in.MatchingRuleIDs) == 0 || in.Strategy == "" {
		return nil, 0, fail(400, blnk.CodeValidation, "strategy and matching_rule_ids are required")
	}
	if in.Strategy != "one_to_one" {
		return nil, 0, fail(400, blnk.CodeValidation, "emulator: only one_to_one is supported")
	}
	var rules []*blnk.MatchingRule
	for _, id := range in.MatchingRuleIDs {
		r, ok := s.rules[id]
		if !ok {
			return nil, 0, fail(404, blnk.CodeNotFound, "matching rule %s not found", id)
		}
		rules = append(rules, r)
	}
	used := map[string]bool{}
	rec := &blnk.Reconciliation{ReconciliationID: s.id("recon"), Status: "completed", IsDryRun: in.DryRun,
		StartedAt: blnk.Time{Time: s.now}, CompletedAt: blnk.Time{Time: s.now}}
	for _, ext := range in.ExternalTransactions {
		matched := false
		for _, id := range s.order {
			t := s.txns[id]
			if used[id] || t.Status != blnk.StatusApplied {
				continue
			}
			for _, r := range rules {
				if ruleMatches(r, ext, t) {
					matched = true
					break
				}
			}
			if matched {
				used[id] = true
				break
			}
		}
		if matched {
			rec.MatchedTransactions++
		} else {
			rec.UnmatchedTransactions++
		}
	}
	s.recons[rec.ReconciliationID] = rec
	return map[string]any{"reconciliation_id": rec.ReconciliationID}, 200, nil
}

func ruleMatches(r *blnk.MatchingRule, ext blnk.ExternalTransaction, t *txn) bool {
	for _, c := range r.Criteria {
		switch c.Field + "/" + c.Operator {
		case "reference/equals":
			if ext.Reference != t.Reference {
				return false
			}
		case "currency/equals":
			if !strings.EqualFold(ext.Currency, t.Currency) {
				return false
			}
		case "description/contains":
			if !strings.Contains(strings.ToLower(t.Description), strings.ToLower(ext.Description)) {
				return false
			}
		case "amount/equals":
			ea, ok := new(big.Rat).SetString(ext.Amount.String())
			if !ok {
				return false
			}
			ta, _ := new(big.Rat).SetString(t.AmountString)
			if ta == nil {
				return false
			}
			diff := new(big.Rat).Sub(ea, ta)
			diff.Abs(diff)
			tol := new(big.Rat).Mul(ta, new(big.Rat).SetFloat64(c.AllowableDrift))
			if diff.Cmp(tol.Abs(tol)) > 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
