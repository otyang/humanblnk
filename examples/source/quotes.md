title: Quotes for confirm screens
group: wallet
order: 6
lead: Show the customer the fee, the total and their balance after, and whether the payment will go through, before anything moves.
---
## The situation

Before Ada taps Confirm, the Oja app shows: "Send ₦12,500 to Tunde. Fee ₦10. Total ₦12,510. Balance after ₦47,490." The quote calls give you every figure on that screen. They run the same checks as the real call, then ask Blnk to dry-run it, so `Allowed` is Blnk's own answer. Nothing is posted.

| Before | Quote with | Then call |
|---|---|---|
| A top-up | `wallet.QuoteDeposit` | `wallet.Deposit` |
| A bank withdrawal | `wallet.QuoteWithdraw` | `wallet.Withdraw` |
| A payment to another account | `wallet.QuoteTransfer` | `wallet.Transfer` |
| A currency conversion | `wallet.QuoteExchange` | `wallet.Exchange` |

Each quote takes the same input struct as the call it previews.

## Quote, then execute

@sample transfer | The confirm screen for Ada's rent share

- Build the input once, with its `Reference`, when the confirm screen opens. Quote it, and send the same input when the customer confirms.
- `Config.Fees` prices the quote exactly as it will price the call. It runs again on the real call; if prices can change in between, set `Fee: q.Fee` on the call to charge what you showed.
- `q.String()` gives a one-line summary for logs.

## The fields of a Quote

| Field | Meaning |
|---|---|
| `Amount` | the amount as entered: for a deposit the gross received, otherwise what the recipient gets |
| `Fee` | the fee, from `Fee` or `Config.Fees` |
| `Debit` | the total leaving the paying side: amount plus fee for a transfer or withdrawal; the gross for a deposit |
| `Credit` | what reaches the recipient: the net amount for a deposit, `Amount` otherwise |
| `Account` | the customer account the quote is about: the sender, or the account topped up |
| `AvailableBefore` | that account's available balance now |
| `AvailableAfter` | its available balance after the call |
| `Allowed` | whether the call would go through |
| `Reason` | why not, when `Allowed` is false; test it with `errors.Is` |
| `CheckedByLedger` | true when Blnk dry-ran it; false when a local check already refused it |

## Deposits and withdrawals

@sample deposit | "Top up ₦50,000": the fee before Ada pays Paystack

For a deposit, `Debit` is the gross amount and `Credit` the amount Ada will get, so `AvailableAfter` goes up by the net.

@sample withdraw | "Withdraw ₦20,000": what reaches the bank and what it costs

## Exchanges: ExchangeQuote

@sample exchange | "Convert $100 at ₦1,532.40"

| Field | Meaning |
|---|---|
| `Sold` | what leaves `From`, without the fee |
| `Bought` | what arrives in `To` |
| `Fee` | the fee, in the sell currency |
| `Rate` | the rate, as you gave it |
| `Debit` | `Sold` plus `Fee` |
| `Account`, `AvailableBefore`, `AvailableAfter` | the paying account and its available balance, in the sell currency |
| `Allowed`, `Reason`, `CheckedByLedger` | as for `Quote`; Blnk dry-runs both legs |

See [Currency exchange](currency-exchange.html) for rates and rounding.

## When the answer is no

A business-rule refusal is not an error. The quote comes back with `Allowed: false` and the reason, so the app can explain it.

@sample refused | Too much, over the limit, a frozen recipient

These five errors come back as `Reason`:

| Reason | Show the customer |
|---|---|
| `ErrInsufficientFunds` | "Your balance is too low"; `AvailableAfter` shows by how much |
| `ErrLimitExceeded` | "This is over your limit"; `wallet.LimitUsage` says what is left |
| `ErrAccountFrozen` | "This account can't receive or send money right now" |
| `ErrAccountClosed` | "This account is closed" |
| `ErrLedgerFrozen` | "Payments are paused" |

A quote that is refused locally, like the frozen recipient here, never reaches Blnk, so `CheckedByLedger` is false.

## Errors

Malformed input is an error, as it would be on the real call.

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInvalidInput` | no `Reference`; too many decimal places; a bad rate; no fee and no `Config.Fees`; the same account on both sides | fix the input |
| `ErrWrongKind` | a house account where a customer account is required | pass a customer account |
| `ErrDuplicateReference` | the reference is already used, e.g. quoting again after the call | quote before the call; give a new payment a new reference |
| `ErrFeePricing` | `Config.Fees` failed | fix the pricing function |
| `ErrUnavailable` | Blnk can't be reached | show a retry message |

## Related

- [Transfers between customers](transfers.html), [Deposits and top-ups](deposits.html), [Withdrawals and payouts](withdrawals.html), [Currency exchange](currency-exchange.html).
- [Fees](fees.html): the pricing function behind `Fee`.
- [Limits](limits.html): what `ErrLimitExceeded` checks.
