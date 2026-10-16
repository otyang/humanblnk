package ledger

import (
	"context"
	"errors"
	"math/big"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// ExchangeQuote previews Exchange: what is sold, bought and charged, and the
// paying account's available balance after.
type ExchangeQuote struct {
	Sold   Money  // leaves From (excluding Fee)
	Bought Money  // arrives in To
	Fee    Money  // in the sell currency
	Rate   string // as given
	Debit  Money  // total leaving From: Sold + Fee

	Account         AccountID // From
	AvailableBefore Money
	AvailableAfter  Money
	Allowed         bool
	Reason          error // why not, when !Allowed (ErrInsufficientFunds, ErrLimitExceeded, ErrAccountFrozen, ...)
	CheckedByLedger bool  // true when Blnk dry-ran both legs
}

// QuoteExchange previews Exchange without moving money. It runs the same checks
// (rate, amounts, fee, account and ledger status, limits) and then has Blnk
// dry-run both legs. Business-rule refusals come back as Allowed=false;
// malformed input is an error.
func (s walletAPI) QuoteExchange(ctx context.Context, in ExchangeInput) (*ExchangeQuote, error) {
	const op = "ledger.Wallet.QuoteExchange"
	p, err := s.planExchange(ctx, op, in)
	if err != nil {
		if !refusal(err) {
			return nil, err
		}
		return &ExchangeQuote{Account: in.From, Rate: in.Rate, Allowed: false, Reason: err}, nil
	}
	m := func(v *big.Int) Money { return s.money(v, p.sell.Precision, p.sell.Currency) }
	debit := p.sell.add(p.fee)
	debit.Display = s.Display(debit)
	before := p.from.Available()
	after := new(big.Int).Sub(before, debit.PreciseAmount)
	q := &ExchangeQuote{
		Sold: p.sell, Bought: p.buy, Fee: p.fee, Rate: in.Rate, Debit: debit,
		Account: in.From, AvailableBefore: m(before), AvailableAfter: m(after), Allowed: true,
	}
	if after.Sign() < 0 {
		q.Allowed, q.Reason = false, &OpError{Op: op, Kind: ErrInsufficientFunds}
		return q, nil
	}
	for _, leg := range []blnk.CreateTransactionRequest{p.debit, p.credit} {
		reason, err := s.dryRun(ctx, op, leg)
		if err != nil {
			return nil, err
		}
		if reason != nil {
			q.Allowed, q.Reason = false, reason
			return q, nil
		}
	}
	q.CheckedByLedger = true
	return q, nil
}

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
	CheckedByLedger bool  // true when Blnk itself dry-ran it (always, once the local checks pass)
}

// QuoteTransfer previews Transfer.
func (s walletAPI) QuoteTransfer(ctx context.Context, in TransferInput) (*Quote, error) {
	const op = "ledger.Wallet.QuoteTransfer"
	in, err := s.withTransferFee(ctx, op, in, nil)
	if err != nil {
		return s.quote(ctx, op, in.From, in.From, blnk.CreateTransactionRequest{}, err)
	}
	req, err := s.moveReq(ctx, op, in, "transfer")
	return s.quote(ctx, op, in.From, in.From, req, err)
}

// QuoteWithdraw previews Withdraw.
func (s walletAPI) QuoteWithdraw(ctx context.Context, in WithdrawInput) (*Quote, error) {
	const op = "ledger.Wallet.QuoteWithdraw"
	req, err := s.withdrawReq(ctx, op, in)
	return s.quote(ctx, op, in.AccountID, in.AccountID, req, err)
}

// QuoteDeposit previews Deposit (fee and net credited).
func (s walletAPI) QuoteDeposit(ctx context.Context, in DepositInput) (*Quote, error) {
	const op = "ledger.Wallet.QuoteDeposit"
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

	if q.Allowed {
		reason, err := s.dryRun(ctx, op, req)
		if err != nil {
			return nil, err
		}
		q.CheckedByLedger = true
		if reason != nil {
			q.Allowed, q.Reason = false, reason
		}
	}
	return q, nil
}

// dryRun asks Blnk to check req without writing anything (dry_run, Core
// 0.15.3+; this module requires 0.15.4). It returns why Blnk would refuse it,
// or nil when it would apply.
func (s *Service) dryRun(ctx context.Context, op string, req blnk.CreateTransactionRequest) (error, error) {
	p, err := s.c.PreviewTransaction(ctx, req)
	if err != nil {
		return nil, mapErr(op, err)
	}
	if p.WouldApply {
		return nil, nil
	}
	code, msg := "", "dry run rejected"
	if p.Rejection != nil {
		code, msg = p.Rejection.Code, firstNonEmpty(p.Rejection.Message, p.Rejection.Reason, msg)
	}
	return mapErr(op, &blnk.APIError{Status: 400, Code: code, Message: msg}), nil
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
