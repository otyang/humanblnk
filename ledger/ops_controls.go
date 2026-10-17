package ledger

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// FreezeAccount marks the account frozen: every movement through this
// package touching it is refused (ErrAccountFrozen). A closed account can't
// be frozen.
//
// Blnk doesn't block on status, and runs scheduled transfers (Schedule) on
// its own. So FreezeAccount also holds the account's whole available
// balance on FrozenFundsAccount: a scheduled transfer that comes due while
// the account is frozen finds nothing to spend and Blnk rejects it.
// UnfreezeAccount releases the hold. The balance itself doesn't change.
//
// Holds created with HoldInput.CaptureAt were authorised before the freeze
// and still capture on time, as a card authorisation does; Cancel them if
// they must not. A scheduled transfer created with AllowOverdraft still
// posts. Freezing again is safe: the hold isn't doubled.
func (s opsAPI) FreezeAccount(ctx context.Context, id AccountID, reason string) error {
	const op = "ledger.Ops.FreezeAccount"
	if err := s.setAccountStatus(ctx, op, id, "frozen", reason); err != nil {
		return err
	}
	return s.holdFrozen(ctx, op, id, reason)
}

// UnfreezeAccount marks a frozen account active again and releases the hold
// FreezeAccount placed. A closed account stays closed.
func (s opsAPI) UnfreezeAccount(ctx context.Context, id AccountID) error {
	const op = "ledger.Ops.UnfreezeAccount"
	if err := s.releaseFrozen(ctx, op, id); err != nil {
		return err
	}
	return s.setAccountStatus(ctx, op, id, "active", "")
}

// metaFreezeHold records on a frozen account the hold FreezeAccount placed.
const metaFreezeHold = "freeze_hold"

// holdFrozen holds a frozen account's available balance on
// FrozenFundsAccount, unless a hold from an earlier FreezeAccount is still
// in place.
func (s *Service) holdFrozen(ctx context.Context, op string, id AccountID, reason string) error {
	b, err := s.c.GetBalance(ctx, string(id))
	if err != nil {
		return mapErr(op, err)
	}
	if prev := str(b.MetaData[metaFreezeHold]); prev != "" {
		if t, err := s.c.GetTransaction(ctx, prev); err == nil && t.Status == blnk.StatusInflight && t.Inflight {
			return nil // already held
		}
	}
	avail := b.Available()
	if avail.Sign() <= 0 {
		return nil
	}
	amount := s.money(avail, s.precisionOf(b), b.Currency)
	var nonce [6]byte
	_, _ = rand.Read(nonce[:])
	req := s.baseReq(amount, fmt.Sprintf("freeze_%s_%d_%x", id, s.cfg.Now().Unix(), nonce),
		"Frozen: "+firstNonEmpty(reason, "account frozen"), nil, "freeze")
	req.Source, req.Destination, req.Inflight = string(id), FrozenFundsAccount, true
	r, err := s.post(ctx, op, req)
	if err != nil {
		return fmt.Errorf("account %s is frozen, but its balance could not be held against scheduled transfers: %w", id, err)
	}
	_, err = s.c.UpdateMetadata(ctx, string(id), blnk.MetaData{metaFreezeHold: r.TransactionID})
	return mapErr(op, err)
}

// releaseFrozen voids the hold FreezeAccount placed on id, if any.
func (s *Service) releaseFrozen(ctx context.Context, op string, id AccountID) error {
	b, err := s.loadAccount(ctx, op, id)
	if err != nil {
		return err
	}
	hold := str(b.MetaData[metaFreezeHold])
	if hold == "" {
		return nil
	}
	_, err = s.c.UpdateInflight(ctx, hold, blnk.UpdateInflightRequest{Status: blnk.ActionVoid, SkipQueue: !s.cfg.UseQueue})
	if err != nil && !blnk.IsNotFound(err) && !errors.Is(mapErr(op, err), ErrNotPending) && !errors.Is(mapErr(op, err), ErrAlreadyFinalized) {
		return mapErr(op, err)
	}
	_, err = s.c.UpdateMetadata(ctx, string(id), blnk.MetaData{metaFreezeHold: ""})
	return mapErr(op, err)
}

func (s *Service) setAccountStatus(ctx context.Context, op string, id AccountID, status, reason string) error {
	b, err := s.loadAccount(ctx, op, id)
	if err != nil {
		return err
	}
	if str(b.MetaData["status"]) == "closed" {
		return &OpError{Op: op, Kind: ErrAccountClosed, Cause: fmt.Errorf("account %s is closed", id)}
	}
	_, err = s.c.UpdateMetadata(ctx, string(id), blnk.MetaData{
		"status": status, "status_reason": reason,
		"status_changed_at": s.cfg.Now().UTC().Format(time.RFC3339),
	})
	return mapErr(op, err)
}

// subAccounts refuses a customer movement between from and to (either may
// be empty: a deposit has no customer sender) that misuses a pot or an
// escrow account. An escrow account's money moves only through
// ReleaseEscrow and RefundEscrow, which pay whoever the escrow names. A
// pot's money moves only to and from its parent account, so it can't be
// used to pay others, or withdrawn, around the parent's limits.
func subAccounts(op string, bals map[AccountID]*blnk.Balance, from, to AccountID) error {
	for _, side := range [][2]AccountID{{from, to}, {to, from}} {
		b := bals[side[0]]
		if b == nil {
			continue
		}
		switch AccountKind(str(b.MetaData[metaKind])) {
		case EscrowAccount:
			return &OpError{Op: op, Kind: ErrWrongKind, Cause: fmt.Errorf(
				"%s is an escrow account; its money moves only with ReleaseEscrow or RefundEscrow", side[0])}
		case PotAccount:
			if parent := AccountID(str(b.MetaData["parent_account"])); side[1] != parent {
				return &OpError{Op: op, Kind: ErrWrongKind, Cause: fmt.Errorf(
					"%s is a savings pot; money moves only between it and its parent account %s", side[0], parent)}
			}
		}
	}
	return nil
}

// checkActive rejects a movement touching a frozen, blocked or closed
// customer account, or taking money out of an account whose ledger is frozen.
// from are the accounts money leaves, to the ones it reaches. House accounts
// are never blocked.
func (s *Service) checkActive(ctx context.Context, op string, from []AccountID, to ...AccountID) error {
	return s.checkActiveWith(ctx, op, nil, from, to...)
}

// checkActiveWith is checkActive reusing balances the caller already read.
func (s *Service) checkActiveWith(ctx context.Context, op string, known map[AccountID]*blnk.Balance, from []AccountID, to ...AccountID) error {
	_, err := s.loadActive(ctx, op, known, from, to...)
	return err
}

// loadActive is checkActiveWith returning the customer accounts it read.
func (s *Service) loadActive(ctx context.Context, op string, known map[AccountID]*blnk.Balance, from []AccountID, to ...AccountID) (map[AccountID]*blnk.Balance, error) {
	got := map[AccountID]*blnk.Balance{}
	sending := make(map[AccountID]bool, len(from))
	for _, id := range from {
		sending[id] = true
	}
	checked := make(map[AccountID]bool, len(from)+len(to))
	for _, id := range append(append([]AccountID(nil), from...), to...) {
		if id.isIndicator() || checked[id] {
			continue // house accounts, or already checked
		}
		checked[id] = true
		b := known[id]
		if b == nil {
			var err error
			if b, err = s.c.GetBalance(ctx, string(id)); err != nil {
				return nil, mapErr(op, err)
			}
		}
		if strings.HasPrefix(b.Indicator, "@") {
			continue // a house account given by its balance id
		}
		got[id] = b
		if err := s.samePrecision(op, b); err != nil {
			return nil, err
		}
		switch st, _ := b.MetaData["status"].(string); st {
		case "frozen", "blocked":
			return nil, &OpError{Op: op, Kind: ErrAccountFrozen, Cause: fmt.Errorf("account %s is %s", id, st)}
		case "closed":
			return nil, &OpError{Op: op, Kind: ErrAccountClosed, Cause: fmt.Errorf("account %s is closed", id)}
		}
		if !sending[id] || b.LedgerID == "" {
			continue
		}
		lg, err := s.ledgerFor(ctx, op, LedgerID(b.LedgerID), false)
		if err != nil {
			return nil, err
		}
		if lg.Status == "frozen" {
			return nil, &OpError{Op: op, Kind: ErrLedgerFrozen, Cause: fmt.Errorf("ledger %s of account %s is frozen", b.LedgerID, id)}
		}
	}
	return got, nil
}

// FreezeLedger marks the ledger frozen: no money can leave its customer
// accounts through this package. Money can still come in. Unlike
// FreezeAccount it holds no balances, so transfers Blnk already has
// scheduled (Schedule) still post; freeze the accounts themselves to stop
// those.
func (s opsAPI) FreezeLedger(ctx context.Context, id LedgerID, reason string) error {
	return s.setLedgerStatus(ctx, "ledger.Ops.FreezeLedger", id, "frozen", reason)
}

// UnfreezeLedger marks a frozen ledger active again.
func (s opsAPI) UnfreezeLedger(ctx context.Context, id LedgerID) error {
	return s.setLedgerStatus(ctx, "ledger.Ops.UnfreezeLedger", id, "active", "")
}

func (s *Service) setLedgerStatus(ctx context.Context, op string, id LedgerID, status, reason string) error {
	if id == GeneralLedgerID {
		return &OpError{Op: op, Kind: ErrWrongKind, Cause: fmt.Errorf("%s holds house accounts, which are never frozen", id)}
	}
	s.forgetLedger(id)
	l, err := s.ledgerFor(ctx, op, id, false)
	if err != nil {
		return err
	}
	if l.Status == "duplicate" {
		return invalid(op, "ledger %s is a duplicate of %s", id, l.DuplicateOf)
	}
	_, err = s.c.UpdateMetadata(ctx, string(id), blnk.MetaData{
		"status": status, "status_reason": reason,
		"status_changed_at": s.cfg.Now().UTC().Format(time.RFC3339),
	})
	s.forgetLedger(id)
	return mapErr(op, err)
}

// CloseInput closes an account (Ops().CloseAccount). Closing is final: a closed
// account is never reopened (open a new one instead), so it needs a reason
// and a second person's approval, like Ops().Adjust.
type CloseInput struct {
	Reason      string // REQUIRED, e.g. "customer request", "dormant 24 months"
	ApprovedBy  string // REQUIRED: who approved it; must be someone else
	RequestedBy string // REQUIRED: who asked for it

	// SweepTo receives any remaining balance: another customer account, or a
	// house account such as AccountID("@PayoutClearing"). Required when the
	// balance isn't zero.
	SweepTo   AccountID
	Reference string // REQUIRED when there is a balance to sweep
}

// CloseAccount moves any remaining balance to SweepTo and marks the account
// closed, for good. It refuses while anything is pending (INFLIGHT) or
// scheduled to or from the account, while a pot under it is still open or an
// escrow it pays into or out of still holds money, and when the balance is
// negative. Every movement touching a closed account is refused afterwards
// (ErrAccountClosed). Closing a frozen account is allowed; sweeping money out
// of an account whose ledger is frozen is not, nor into a frozen or closed
// account.
func (s opsAPI) CloseAccount(ctx context.Context, accountID AccountID, in CloseInput) (*Account, error) {
	const op = "ledger.Ops.CloseAccount"
	b, err := s.loadAccount(ctx, op, accountID)
	if err != nil {
		return nil, err
	}
	if str(b.MetaData["status"]) == "closed" {
		return s.toAccount(b), nil
	}
	if strings.TrimSpace(in.Reason) == "" {
		return nil, invalid(op, "reason is required: closing an account is final")
	}
	if err := fourEyes(op, "closure", in.RequestedBy, in.ApprovedBy); err != nil {
		return nil, err
	}
	if err := s.closeBlockers(ctx, op, accountID); err != nil {
		return nil, err
	}
	// A frozen account's freeze hold isn't pending activity: release it so
	// the balance can be swept. Nothing can be scheduled on it any more.
	if str(b.MetaData[metaFreezeHold]) != "" {
		if err := s.releaseFrozen(ctx, op, accountID); err != nil {
			return nil, err
		}
		if b, err = s.loadAccount(ctx, op, accountID); err != nil {
			return nil, err
		}
	}
	if b.InflightDebitBalance.Sign() != 0 || b.InflightCreditBalance.Sign() != 0 {
		return nil, &OpError{Op: op, Kind: ErrPendingActivity,
			Cause: fmt.Errorf("confirm or cancel pending transactions on %s first", accountID)}
	}
	switch b.Balance.Sign() {
	case -1:
		return nil, invalid(op, "account %s is overdrawn; settle it before closing", accountID)
	case 1:
		if in.SweepTo == "" || strings.TrimSpace(in.Reference) == "" {
			return nil, invalid(op, "account has a balance: SweepTo and Reference are required")
		}
		if in.SweepTo == accountID {
			return nil, invalid(op, "cannot sweep an account into itself")
		}
		// A frozen account can still be swept on closure; a frozen ledger can't:
		// nothing leaves it. The account it is swept to must be able to take it.
		lg, err := s.ledgerFor(ctx, op, LedgerID(b.LedgerID), false)
		if err != nil {
			return nil, err
		}
		if lg.Status == "frozen" {
			return nil, &OpError{Op: op, Kind: ErrLedgerFrozen, Cause: fmt.Errorf("ledger %s of account %s is frozen", b.LedgerID, accountID)}
		}
		if err := s.checkActive(ctx, op, nil, in.SweepTo); err != nil {
			return nil, err
		}
		amount := s.money(&b.Balance.Int, s.precisionOf(b), b.Currency)
		// Built directly so a frozen account can still be swept on closure.
		req, err := s.transferReq(TransferInput{
			From: accountID, To: in.SweepTo, Amount: amount, Reference: in.Reference,
			Description: "Account closure", MetaData: map[string]any{"close_reason": in.Reason},
		}, amount, s.money(newInt(0), amount.Precision, amount.Currency), "account_closure")
		if err != nil {
			return nil, invalid(op, "%v", err)
		}
		if _, err := s.post(ctx, op, req); err != nil {
			return nil, err
		}
	}
	if _, err := s.c.UpdateMetadata(ctx, string(accountID), blnk.MetaData{
		"status": "closed", "status_reason": in.Reason,
		"closed_approved_by": in.ApprovedBy, "closed_requested_by": in.RequestedBy,
		"status_changed_at": s.cfg.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return nil, mapErr(op, err)
	}
	return s.Wallet().GetAccount(ctx, accountID)
}

// closeBlockers refuses closing an account that money could still reach or
// leave after it is closed: scheduled transfers (Blnk posts them itself, so
// this layer couldn't stop them later), pots still open under it, and escrows
// still holding money that would pay into or out of it.
func (s *Service) closeBlockers(ctx context.Context, op string, id AccountID) error {
	sched, err := s.filterAll(ctx, []blnk.Filter{
		{Field: "balance_id", Operator: "eq", Value: string(id)},
		{Field: "status", Operator: "eq", Value: blnk.StatusScheduled},
	})
	if err != nil {
		return mapErr(op, err)
	}
	for i := range sched {
		ran, err := s.scheduledOutcome(ctx, &sched[i])
		if err != nil {
			return mapErr(op, err)
		}
		if ran == nil {
			return &OpError{Op: op, Kind: ErrPendingActivity, Cause: fmt.Errorf(
				"account %s has scheduled transfers (e.g. %s, due %s); they must post or be removed first",
				id, sched[i].Reference, sched[i].ScheduledFor.Format(time.RFC3339))}
		}
	}
	pots, err := s.Wallet().FindAccounts(ctx, AccountQuery{Kind: PotAccount, MetaData: map[string]string{"parent_account": string(id)}, Limit: 100})
	if err != nil {
		return err
	}
	for _, p := range pots.Items {
		if p.Status != "closed" {
			return &OpError{Op: op, Kind: ErrPendingActivity, Cause: fmt.Errorf("close pot %s (%s) first", p.ID, str(p.MetaData["pot_name"]))}
		}
	}
	for _, side := range []string{"escrow_payer", "escrow_beneficiary"} {
		escrows, err := s.Wallet().FindAccounts(ctx, AccountQuery{Kind: EscrowAccount, MetaData: map[string]string{side: string(id)}, Limit: 100})
		if err != nil {
			return err
		}
		for _, e := range escrows.Items {
			if e.Balance.Sign() > 0 {
				return &OpError{Op: op, Kind: ErrPendingActivity, Cause: fmt.Errorf("escrow %s still holds %s; release or refund it first", e.ID, e.Balance)}
			}
		}
	}
	return nil
}
