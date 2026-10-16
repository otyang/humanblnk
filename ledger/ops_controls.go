package ledger

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// FreezeAccount marks the account frozen: every movement touching it is refused
// (ErrAccountFrozen). Blnk itself doesn't block on status; this layer does,
// on every movement. A closed account can't be frozen.
func (s opsAPI) FreezeAccount(ctx context.Context, id AccountID, reason string) error {
	return s.setAccountStatus(ctx, "ledger.Ops.FreezeAccount", id, "frozen", reason)
}

// UnfreezeAccount marks a frozen account active again. A closed account stays closed.
func (s opsAPI) UnfreezeAccount(ctx context.Context, id AccountID) error {
	return s.setAccountStatus(ctx, "ledger.Ops.UnfreezeAccount", id, "active", "")
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

// checkActive rejects a movement touching a frozen, blocked or closed
// customer account, or taking money out of an account whose ledger is frozen.
// from are the accounts money leaves, to the ones it reaches. House accounts
// are never blocked.
func (s *Service) checkActive(ctx context.Context, op string, from []AccountID, to ...AccountID) error {
	return s.checkActiveWith(ctx, op, nil, from, to...)
}

// checkActiveWith is checkActive reusing balances the caller already read.
func (s *Service) checkActiveWith(ctx context.Context, op string, known map[AccountID]*blnk.Balance, from []AccountID, to ...AccountID) error {
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
				return mapErr(op, err)
			}
		}
		if strings.HasPrefix(b.Indicator, "@") {
			continue // a house account given by its balance id
		}
		switch st, _ := b.MetaData["status"].(string); st {
		case "frozen", "blocked":
			return &OpError{Op: op, Kind: ErrAccountFrozen, Cause: fmt.Errorf("account %s is %s", id, st)}
		case "closed":
			return &OpError{Op: op, Kind: ErrAccountClosed, Cause: fmt.Errorf("account %s is closed", id)}
		}
		if !sending[id] || b.LedgerID == "" {
			continue
		}
		lg, err := s.ledgerFor(ctx, op, LedgerID(b.LedgerID), false)
		if err != nil {
			return err
		}
		if lg.Status == "frozen" {
			return &OpError{Op: op, Kind: ErrLedgerFrozen, Cause: fmt.Errorf("ledger %s of account %s is frozen", b.LedgerID, id)}
		}
	}
	return nil
}

// FreezeLedger marks the ledger frozen: no money can leave its customer accounts.
// Money can still come in.
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
	ApprovedBy  string // REQUIRED; must differ from RequestedBy when both are set
	RequestedBy string

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
	if strings.TrimSpace(in.ApprovedBy) == "" {
		return nil, &OpError{Op: op, Kind: ErrApprovalRequired, Cause: fmt.Errorf("ApprovedBy is required: closing an account is final")}
	}
	if in.RequestedBy != "" && strings.EqualFold(in.RequestedBy, in.ApprovedBy) {
		return nil, &OpError{Op: op, Kind: ErrApprovalRequired, Cause: fmt.Errorf("requester cannot approve their own closure")}
	}
	if b.InflightDebitBalance.Sign() != 0 || b.InflightCreditBalance.Sign() != 0 {
		return nil, &OpError{Op: op, Kind: ErrPendingActivity,
			Cause: fmt.Errorf("confirm or cancel pending transactions on %s first", accountID)}
	}
	if err := s.closeBlockers(ctx, op, accountID); err != nil {
		return nil, err
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
		amount := s.money(&b.Balance.Int, s.Precision(b.Currency), b.Currency)
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
	sched, _, err := s.c.FilterTransactions(ctx, blnk.FilterRequest{
		Filters: []blnk.Filter{
			{Field: "balance_id", Operator: "eq", Value: string(id)},
			{Field: "status", Operator: "eq", Value: blnk.StatusScheduled},
		},
		Limit: 1,
	})
	if err != nil {
		return mapErr(op, err)
	}
	if len(sched) > 0 {
		return &OpError{Op: op, Kind: ErrPendingActivity, Cause: fmt.Errorf(
			"account %s has scheduled transfers (e.g. %s, due %s); they must post or be removed first",
			id, sched[0].Reference, sched[0].ScheduledFor.Format(time.RFC3339))}
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
