package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// DepositInput records money received from outside through a provider.
type DepositInput struct {
	AccountID AccountID // customer account credited
	Amount    Money     // gross amount received from the provider
	Fee       Money     // deducted from Amount, credited to the fee income account; required unless Config.Fees is set (NoFee for free)
	Reference string    // REQUIRED idempotency key, e.g. the provider's payment id

	// Provider that collected the money, e.g. "paystack" → debits @PaystackClearing.
	// Empty uses Config.DepositClearingAccount. ClearingAccount overrides with an exact indicator.
	Provider        string
	ClearingAccount string

	// Pending creates the deposit INFLIGHT so you can run checks before Confirm/Cancel.
	Pending     bool
	Description string
	MetaData    map[string]any
}

// WithdrawInput sends money out through a provider.
type WithdrawInput struct {
	AccountID AccountID // customer account debited
	Amount    Money     // amount the customer receives
	Fee       Money     // charged on top of Amount; required unless Config.Fees is set (NoFee for free)
	Reference string    // REQUIRED idempotency key
	// Provider that pays out, e.g. "flutterwave" → credits @FlutterwaveClearing.
	// Empty uses Config.PayoutClearingAccount. ClearingAccount overrides with an exact indicator.
	Provider        string
	ClearingAccount string

	// Immediate settles straight away. By default withdrawals are held INFLIGHT
	// until you Confirm (provider success) or Cancel (provider failure).
	Immediate   bool
	ExpiresIn   time.Duration // overrides Config.WithdrawalHoldTTL
	Description string
	MetaData    map[string]any
}

// TransferInput moves money between accounts.
type TransferInput struct {
	From           AccountID // customer account, or a house account as AccountID("@Name")
	To             AccountID // customer account, or a house account as AccountID("@Name")
	Amount         Money     // amount the recipient receives
	Fee            Money     // charged to sender on top of Amount; required unless Config.Fees is set (NoFee for free)
	Reference      string
	AllowOverdraft bool
	Pending        bool // create INFLIGHT; Confirm/Cancel later
	ScheduledFor   time.Time
	Description    string
	MetaData       map[string]any
}

// BulkTransferInput is a batch of transfers (Wallet().Bulk).
type BulkTransferInput struct {
	Items []TransferInput
	// Atomic: all succeed or all fail. Set it for payroll-style batches (the zero value is false).
	Atomic  bool
	Pending bool // all items INFLIGHT; Confirm/Cancel the batch with BatchID
	Async   bool // process in background; outcome arrives via bulk_transaction.* webhook
}

// Deposit records money received from outside: the provider's clearing
// account (e.g. @PaystackClearing) is debited and the account credited, less
// the fee, which goes to the fee income account. Pending holds it INFLIGHT
// until Wallet().Confirm.
func (s walletAPI) Deposit(ctx context.Context, in DepositInput) (*Receipt, error) {
	const op = "ledger.Wallet.Deposit"
	req, err := s.depositReq(ctx, op, in)
	if err != nil {
		return nil, err
	}
	return s.post(ctx, op, req)
}

func (s *Service) depositReq(ctx context.Context, op string, in DepositInput) (blnk.CreateTransactionRequest, error) {
	var none blnk.CreateTransactionRequest
	if err := customerOnly(op, in.AccountID); err != nil {
		return none, err
	}
	fee, err := s.feeFor(ctx, op, in.Fee, FeeRequest{Kind: FeeDeposit, Amount: in.Amount, Provider: in.Provider}, in.AccountID, nil)
	if err != nil {
		return none, err
	}
	amount, fee, err := s.prepare(op, in.AccountID, in.Amount, fee, in.Reference)
	if err != nil {
		return none, err
	}
	if fee.cmp(amount) >= 0 {
		return none, invalid(op, "fee must be less than the deposit amount")
	}
	if err := s.checkActive(ctx, op, nil, in.AccountID); err != nil {
		return none, err
	}

	req := s.baseReq(amount, in.Reference, in.Description, in.MetaData, "deposit")
	req.Source = s.clearing(in.ClearingAccount, in.Provider, s.cfg.DepositClearingAccount)
	req.AllowOverdraft = true // external source mirrors money received from outside
	req.Inflight = in.Pending
	if fee.IsPositive() {
		// Gross amount leaves the clearing account; fee goes to fee income, the rest to the account.
		req.Destinations = []blnk.Distribution{
			{Identifier: s.cfg.FeeIncomeAccount, PreciseDistribution: fee.PreciseAmount.String(), Narration: "Deposit fee"},
			{Identifier: string(in.AccountID), Distribution: "left", Narration: firstNonEmpty(in.Description, "Deposit")},
		}
		feeMeta(req.MetaData, fee, amount.add(neg(fee)))
	} else {
		req.Destination = string(in.AccountID)
	}
	return req, nil
}

// Withdraw sends money out: the account is debited Amount + Fee, the
// provider's clearing account credited Amount. It is held INFLIGHT until
// Wallet().Confirm (provider success) or Cancel (failure), and auto-cancels
// after Config.WithdrawalHoldTTL; Immediate posts it at once.
func (s walletAPI) Withdraw(ctx context.Context, in WithdrawInput) (*Receipt, error) {
	const op = "ledger.Wallet.Withdraw"
	req, err := s.withdrawReq(ctx, op, in)
	if err != nil {
		return nil, err
	}
	return s.post(ctx, op, req)
}

func (s *Service) withdrawReq(ctx context.Context, op string, in WithdrawInput) (blnk.CreateTransactionRequest, error) {
	var none blnk.CreateTransactionRequest
	if err := customerOnly(op, in.AccountID); err != nil {
		return none, err
	}
	fee, err := s.feeFor(ctx, op, in.Fee, FeeRequest{Kind: FeeWithdrawal, Amount: in.Amount, Provider: in.Provider}, in.AccountID, nil)
	if err != nil {
		return none, err
	}
	amount, fee, err := s.prepare(op, in.AccountID, in.Amount, fee, in.Reference)
	if err != nil {
		return none, err
	}
	if err := s.checkActive(ctx, op, []AccountID{in.AccountID}); err != nil {
		return none, err
	}
	if err := s.checkLimits(ctx, op, in.AccountID, amount.add(fee).PreciseAmount); err != nil {
		return none, err
	}
	sink := s.clearing(in.ClearingAccount, in.Provider, s.cfg.PayoutClearingAccount)

	req := s.baseReq(amount.add(fee), in.Reference, in.Description, in.MetaData, "withdrawal")
	req.Source = string(in.AccountID)
	if fee.IsPositive() {
		req.Destinations = []blnk.Distribution{
			{Identifier: sink, PreciseDistribution: amount.PreciseAmount.String(), Narration: firstNonEmpty(in.Description, "Withdrawal")},
			{Identifier: s.cfg.FeeIncomeAccount, PreciseDistribution: fee.PreciseAmount.String(), Narration: "Withdrawal fee"},
		}
		feeMeta(req.MetaData, fee, amount)
	} else {
		req.Destination = sink
	}
	if !in.Immediate {
		req.Inflight = true
		ttl := in.ExpiresIn
		if ttl == 0 {
			ttl = s.cfg.WithdrawalHoldTTL
		}
		if ttl > 0 {
			req.InflightExpiryDate = s.cfg.Now().Add(ttl).UTC().Format(time.RFC3339)
		}
	}
	return req, nil
}

// Transfer moves money between accounts. The sender pays Amount + Fee; the
// recipient gets Amount. Pending holds it INFLIGHT; ScheduledFor posts it later.
func (s walletAPI) Transfer(ctx context.Context, in TransferInput) (*Receipt, error) {
	const op = "ledger.Wallet.Transfer"
	in, err := s.withTransferFee(ctx, op, in, nil)
	if err != nil {
		return nil, err
	}
	return s.move(ctx, op, in, "transfer")
}

// withTransferFee fills in a customer transfer's fee (see feeFor). Internal
// flows build transfers without it: they never charge a fee.
func (s *Service) withTransferFee(ctx context.Context, op string, in TransferInput, from *blnk.Balance) (TransferInput, error) {
	fee, err := s.feeFor(ctx, op, in.Fee, FeeRequest{Kind: FeeTransfer, Amount: in.Amount}, in.From, from)
	in.Fee = fee
	return in, err
}

// move is Transfer with an explicit operation name and txn_type, used by the
// higher-level flows (escrow, suspense, settlement, adjustments, ...).
func (s *Service) move(ctx context.Context, op string, in TransferInput, kind string) (*Receipt, error) {
	req, err := s.moveReq(ctx, op, in, kind)
	if err != nil {
		return nil, err
	}
	return s.post(ctx, op, req)
}

// moveReq validates a transfer and builds its request without sending it.
func (s *Service) moveReq(ctx context.Context, op string, in TransferInput, kind string) (blnk.CreateTransactionRequest, error) {
	var none blnk.CreateTransactionRequest
	amount, fee, err := s.prepare(op, in.From, in.Amount, in.Fee, in.Reference)
	if err != nil {
		return none, err
	}
	if in.To == "" {
		return none, invalid(op, "destination account is required")
	}
	if in.From == in.To {
		return none, invalid(op, "cannot transfer to the same account")
	}
	if err := s.checkActive(ctx, op, []AccountID{in.From}, in.To); err != nil {
		return none, err
	}
	if limited(kind) {
		if err := s.checkLimits(ctx, op, in.From, amount.add(fee).PreciseAmount); err != nil {
			return none, err
		}
	}
	req, err := s.transferReq(in, amount, fee, kind)
	if err != nil {
		return none, invalid(op, "%v", err)
	}
	return req, nil
}

// Bulk posts up to 10,000 transfers as one Blnk batch, e.g. payroll.
// Atomic makes them all-or-nothing; Pending holds them all INFLIGHT; Async
// processes them in the background with the outcome sent by webhook.
func (s walletAPI) Bulk(ctx context.Context, in BulkTransferInput) (*BulkReceipt, error) {
	const op = "ledger.Wallet.Bulk"
	if len(in.Items) == 0 {
		return nil, invalid(op, "at least one transfer is required")
	}
	if len(in.Items) > 10000 {
		return nil, invalid(op, "a batch holds at most 10,000 transfers")
	}
	seen := make(map[string]struct{}, len(in.Items))
	reqs := make([]blnk.CreateTransactionRequest, 0, len(in.Items))
	from := make([]AccountID, 0, len(in.Items))
	to := make([]AccountID, 0, len(in.Items))
	senders := map[AccountID]*blnk.Balance{} // one lookup per sender when Config.Fees prices items
	for i, it := range in.Items {
		if !it.Fee.isSet() && s.cfg.Fees != nil && it.From != "" && !it.From.isIndicator() && senders[it.From] == nil {
			b, err := s.c.GetBalance(ctx, string(it.From))
			if err != nil {
				return nil, mapErr(op, err)
			}
			senders[it.From] = b
		}
		var err error
		if it, err = s.withTransferFee(ctx, op, it, senders[it.From]); err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
		amount, fee, err := s.prepare(op, it.From, it.Amount, it.Fee, it.Reference)
		if err != nil {
			return nil, invalid(op, "item %d: %v", i, err)
		}
		if it.To == "" || it.From == it.To {
			return nil, invalid(op, "item %d: invalid destination", i)
		}
		if _, dup := seen[it.Reference]; dup {
			return nil, invalid(op, "item %d: duplicate reference %q in batch", i, it.Reference)
		}
		seen[it.Reference] = struct{}{}
		r, err := s.transferReq(it, amount, fee, "transfer")
		if err != nil {
			return nil, invalid(op, "item %d: %v", i, err)
		}
		r.SkipQueue, r.Inflight = false, false // controlled at batch level
		reqs = append(reqs, r)
		from, to = append(from, it.From), append(to, it.To)
	}
	if err := s.checkActiveWith(ctx, op, senders, from, to...); err != nil {
		return nil, err
	}
	resp, err := s.c.CreateBulkTransactions(ctx, blnk.BulkTransactionRequest{
		Atomic: in.Atomic, Inflight: in.Pending, RunAsync: in.Async,
		SkipQueue: !s.cfg.UseQueue, Transactions: reqs,
	})
	if err != nil {
		return nil, mapErr(op, err)
	}
	return &BulkReceipt{BatchID: resp.BatchID, Status: resp.Status, Count: resp.TransactionCount}, nil
}
