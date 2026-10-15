package ledger

import (
	"context"
	"errors"
	"math/big"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// Quote previews a movement before the customer confirms it: what is
// charged, what arrives, and the paying account's available balance after.
type Quote struct {
	Amount Money // the amount as entered (deposit: gross received; withdraw/transfer: what the recipient gets)
	Fee    Money
	Debit  Money // total leaving the paying account
	Credit Money // total reaching the customer account (deposit: net of fee)

	Account         AccountID // the customer account the quote is about
	AvailableBefore Money
	AvailableAfter  Money
	Allowed         bool
	Reason          error // why not, when !Allowed (ErrInsufficientFunds, ErrLimitExceeded, ErrAccountFrozen, ...)
	CheckedByLedger bool  // true when Blnk itself dry-ran it (Config.CoreDryRun)
}

// QuoteTransfer previews Transfer.
func (s paymentsAPI) QuoteTransfer(ctx context.Context, in TransferInput) (*Quote, error) {
	const op = "ledger.Payments.QuoteTransfer"
	in, err := s.withTransferFee(ctx, op, in, nil)
	if err != nil {
		return s.quote(ctx, op, in.From, in.From, blnk.CreateTransactionRequest{}, err)
	}
	req, err := s.moveReq(ctx, op, in, "transfer")
	return s.quote(ctx, op, in.From, in.From, req, err)
}

// QuoteWithdraw previews Withdraw.
func (s paymentsAPI) QuoteWithdraw(ctx context.Context, in WithdrawInput) (*Quote, error) {
	const op = "ledger.Payments.QuoteWithdraw"
	req, err := s.withdrawReq(ctx, op, in)
	return s.quote(ctx, op, in.AccountID, in.AccountID, req, err)
}

// QuoteDeposit previews Deposit (fee and net credited).
func (s paymentsAPI) QuoteDeposit(ctx context.Context, in DepositInput) (*Quote, error) {
	const op = "ledger.Payments.QuoteDeposit"
	req, err := s.depositReq(ctx, op, in)
	return s.quote(ctx, op, in.AccountID, "", req, err)
}

// quote turns a built request into a Quote. Business-rule failures (funds,
// limits, frozen accounts) become a quote with Allowed=false; malformed input
// is returned as an error.
func (s *Service) quote(ctx context.Context, op string, account, payer AccountID, req blnk.CreateTransactionRequest, buildErr error) (*Quote, error) {
	if buildErr != nil {
		if !refusal(buildErr) {
			return nil, buildErr
		}
		return &Quote{Account: account, Allowed: false, Reason: buildErr}, nil
	}
	cur, prec := req.Currency, req.Precision
	m := func(v *big.Int) Money { return s.money(v, prec, cur) }
	total := &req.PreciseAmount.Int

	// Fee legs are the ones paying the fee income account.
	fee := new(big.Int)
	for _, d := range req.Destinations {
		if d.Identifier == s.cfg.FeeIncomeAccount && d.PreciseDistribution != "" {
			v, _ := new(big.Int).SetString(d.PreciseDistribution, 10)
			fee.Add(fee, v)
		}
	}
	q := &Quote{Account: account, Fee: m(fee), Debit: m(total), Allowed: true}
	if payer == "" { // deposit: the customer receives total − fee
		q.Amount, q.Credit = m(total), m(new(big.Int).Sub(total, fee))
	} else {
		q.Amount, q.Credit = m(new(big.Int).Sub(total, fee)), m(new(big.Int).Sub(total, fee))
	}

	b, err := s.c.GetBalance(ctx, string(account))
	if err != nil {
		return nil, mapErr(op, err)
	}
	avail := b.Available()
	q.AvailableBefore = m(avail)
	after := new(big.Int).Set(avail)
	if payer == "" {
		after.Add(after, new(big.Int).Sub(total, fee))
	} else {
		after.Sub(after, total)
		if after.Sign() < 0 && !req.AllowOverdraft {
			q.Allowed = false
			q.Reason = &OpError{Op: op, Kind: ErrInsufficientFunds}
		}
	}
	q.AvailableAfter = m(after)

	if s.cfg.CoreDryRun && q.Allowed {
		p, err := s.c.PreviewTransaction(ctx, req)
		if err != nil {
			return nil, mapErr(op, err)
		}
		q.CheckedByLedger = true
		if !p.WouldApply {
			q.Allowed = false
			code := ""
			if p.Rejection != nil {
				code = p.Rejection.Code
			}
			q.Reason = mapErr(op, &blnk.APIError{Status: 400, Code: code, Message: "dry run rejected"})
		}
	}
	return q, nil
}

// refusal reports whether err is a business-rule "no" rather than bad input.
func refusal(err error) bool {
	for _, k := range []error{ErrInsufficientFunds, ErrLimitExceeded, ErrAccountFrozen, ErrAccountClosed, ErrLedgerFrozen} {
		if errors.Is(err, k) {
			return true
		}
	}
	return false
}

// String summarises the quote, e.g. for logs.
func (q *Quote) String() string {
	var b strings.Builder
	b.WriteString("debit " + q.Debit.String() + ", fee " + q.Fee.String() + ", credit " + q.Credit.String())
	if !q.Allowed && q.Reason != nil {
		b.WriteString(" (not allowed: " + q.Reason.Error() + ")")
	}
	return b.String()
}
