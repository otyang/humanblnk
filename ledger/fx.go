package ledger

import (
	"context"
	"math/big"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// ExchangeInput converts money between two accounts in different currencies,
// e.g. a customer's USD account to their NGN account.
type ExchangeInput struct {
	From string // account debited, in the sell currency
	To   string // account credited, in the buy currency

	// Set Sell (how much leaves From) or Buy (how much must arrive in To).
	Sell Money
	Buy  Money

	// Rate is units of the buy currency per 1 unit of the sell currency,
	// e.g. "1550.25" for USD→NGN. The buy amount rounds down and a computed
	// sell amount rounds up, so rounding never costs the house.
	Rate string

	Fee         Money  // in the sell currency, charged on top to the fee income account; required unless Config.Fees is set (NoFee for free)
	Reference   string // the two legs are recorded as Reference+"_sell" and Reference+"_buy"
	Pool        string // default FXPoolAccount
	Description string
	MetaData    map[string]any
}

// ExchangeReceipt is both legs of an exchange.
type ExchangeReceipt struct {
	Sold     Money  // debited from From (excluding Fee)
	Bought   Money  // credited to To
	Fee      Money  // fee charged in the sell currency
	Rate     string // as given
	Debit    *Receipt
	Credit   *Receipt
	Replayed bool
}

// Exchange debits From into the FX pool in the sell currency and credits To out
// of the pool in the buy currency, as one atomic batch: both legs post or
// neither does. Each currency's pool balance is the house's open position.
func (s paymentsAPI) Exchange(ctx context.Context, in ExchangeInput) (*ExchangeReceipt, error) {
	const op = "ledger.Payments.Exchange"
	if err := s.validateMove(op, in.From, in.Reference); err != nil {
		return nil, err
	}
	if in.To == "" || in.To == in.From {
		return nil, invalid(op, "a different destination account is required")
	}
	rate, ok := new(big.Rat).SetString(strings.TrimSpace(in.Rate))
	if !ok || rate.Sign() <= 0 {
		return nil, invalid(op, "rate %q must be a positive decimal", in.Rate)
	}
	if in.Sell.isSet() == in.Buy.isSet() {
		return nil, invalid(op, "set exactly one of Sell or Buy")
	}
	from, err := s.c.GetBalance(ctx, in.From)
	if err != nil {
		return nil, mapErr(op, err)
	}
	to, err := s.c.GetBalance(ctx, in.To)
	if err != nil {
		return nil, mapErr(op, err)
	}
	sellCur, buyCur := strings.ToUpper(from.Currency), strings.ToUpper(to.Currency)
	if sellCur == buyCur {
		return nil, invalid(op, "both accounts are in %s; use Transfer", sellCur)
	}
	sellPrec, buyPrec := s.Precision(sellCur), s.Precision(buyCur)

	// buy_precise = sell_precise × rate × buyPrec / sellPrec
	scale := new(big.Rat).Mul(rate, big.NewRat(buyPrec, sellPrec))
	var sell, buy Money
	if in.Sell.isSet() {
		if sell, err = s.resolveIn(op, in.Sell, sellCur); err != nil {
			return nil, err
		}
		v := new(big.Rat).Mul(new(big.Rat).SetInt(sell.PreciseAmount), scale)
		buy = s.money(roundRat(v, RoundDown), buyPrec, buyCur)
	} else {
		if buy, err = s.resolveIn(op, in.Buy, buyCur); err != nil {
			return nil, err
		}
		v := new(big.Rat).Quo(new(big.Rat).SetInt(buy.PreciseAmount), scale)
		sell = s.money(roundRat(v, RoundUp), sellPrec, sellCur)
	}
	if !sell.IsPositive() || !buy.IsPositive() {
		return nil, invalid(op, "amount is too small to exchange at rate %s", in.Rate)
	}

	pool := firstNonEmpty(in.Pool, FXPoolAccount)
	md := map[string]any{
		"fx_pair": sellCur + "/" + buyCur, "fx_rate": in.Rate, "fx_reference": in.Reference,
		"fx_sell": sell.Value, "fx_buy": buy.Value,
	}
	for k, v := range in.MetaData {
		md[k] = v
	}
	fee, err := s.feeFor(ctx, op, in.Fee, FeeRequest{Kind: FeeExchange, Amount: sell, BuyCurrency: buyCur}, in.From, from)
	if err != nil {
		return nil, err
	}
	debit, err := s.moveReq(ctx, op, TransferInput{
		From: in.From, To: pool, Amount: sell, Fee: fee, Reference: in.Reference + "_sell",
		Description: firstNonEmpty(in.Description, "Exchange "+sellCur+" to "+buyCur), MetaData: md,
	}, "exchange")
	if err != nil {
		return nil, err
	}
	credit, err := s.moveReq(ctx, op, TransferInput{
		From: pool, To: in.To, Amount: buy, Reference: in.Reference + "_buy", AllowOverdraft: true,
		Description: firstNonEmpty(in.Description, "Exchange "+sellCur+" to "+buyCur), MetaData: md,
	}, "exchange")
	if err != nil {
		return nil, err
	}
	legs, err := s.atomic(ctx, op, []blnk.CreateTransactionRequest{debit, credit})
	if err != nil {
		return nil, err
	}
	charged := s.money(new(big.Int).Sub(&debit.PreciseAmount.Int, sell.PreciseAmount), sellPrec, sellCur)
	return &ExchangeReceipt{
		Sold: sell, Bought: buy, Fee: charged, Rate: in.Rate,
		Debit: legs[0], Credit: legs[1], Replayed: legs[0].Replayed,
	}, nil
}

// resolveIn resolves m in currency cur, rejecting a different explicit currency.
func (s *Service) resolveIn(op string, m Money, cur string) (Money, error) {
	if m.Currency != "" && !strings.EqualFold(m.Currency, cur) {
		return Money{}, invalid(op, "amount currency %s differs from account currency %s", m.Currency, cur)
	}
	r, err := s.resolve(op, m, cur)
	if err != nil {
		return Money{}, err
	}
	if !r.IsPositive() {
		return Money{}, invalid(op, "amount must be positive")
	}
	return r, nil
}

// atomic posts reqs as one all-or-nothing batch, synchronously, and returns each
// transaction as stored. If the references already exist with the same payloads
// (a retry), the originals come back with Replayed set.
func (s *Service) atomic(ctx context.Context, op string, reqs []blnk.CreateTransactionRequest) ([]*Receipt, error) {
	for i := range reqs {
		reqs[i].SkipQueue, reqs[i].Inflight = false, false
	}
	_, err := s.c.CreateBulkTransactions(ctx, blnk.BulkTransactionRequest{
		Atomic: true, SkipQueue: true, Transactions: reqs,
	})
	replay := blnk.HasCode(err, blnk.CodeDuplicateReference)
	if err != nil && !replay {
		return nil, mapErr(op, err)
	}
	out := make([]*Receipt, len(reqs))
	for i, r := range reqs {
		t, lerr := s.byReference(ctx, r.Reference)
		switch {
		case lerr != nil && replay:
			return nil, mapErr(op, err)
		case lerr != nil:
			return nil, mapErr(op, lerr)
		case replay && !s.samePayload(ctx, t, r):
			return nil, mapErr(op, err)
		}
		out[i] = s.toReceipt(t)
		out[i].Replayed = replay
	}
	return out, nil
}
