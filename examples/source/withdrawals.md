title: Withdrawals and payouts
group: wallet
order: 4
lead: Send money out to a bank or wallet through a provider: reserved while the provider works, confirmed or cancelled from its webhook, and cancelled by itself if the provider never answers.
---
## The situation

Ada withdraws ₦20,000 to her GTBank account. Oja asks Flutterwave to make the bank transfer, and Flutterwave answers minutes later with a webhook: success or failure. Until then the money must be out of Ada's reach but not yet gone. `wallet.Withdraw` does this by default: it reserves the money, and you confirm or cancel when the provider answers.

Pick the call by situation:

| Situation | Use |
|---|---|
| A payout through a provider that answers later | `wallet.Withdraw`, then `wallet.Confirm` or `wallet.Cancel` |
| A payout that has already happened, such as cash over an agent's counter | `wallet.Withdraw` with `Immediate: true` |
| A provider that may never answer | nothing: the hold cancels itself after `Config.WithdrawalHoldTTL` |
| A shorter or longer window for one payout | `ExpiresIn` |
| A provider report with many outcomes | `wallet.ConfirmMany` and `wallet.CancelMany` |
| Salaries or payouts to many people at once | `wallet.Bulk`, see [Bulk payouts](bulk-payouts.html) |

## Withdraw, then confirm

@sample confirm | Ada withdraws ₦20,000 through Flutterwave

- `Amount` is what reaches the bank. The fee is charged on top, so ₦20,050 is reserved.
- The withdrawal starts `pending`. `Balance` is unchanged, `Available` drops at once, and `PendingOut` shows the reserved money.
- Send `wd.TransactionID` (or your `Reference`) to the provider, so its webhook can be matched back.
- `wallet.Confirm(ctx, id, nil)` posts it when the provider reports success. The money leaves `Balance` and reaches `@FlutterwaveClearing` and `@FeeIncome`.
- `Provider: "flutterwave"` credits `@FlutterwaveClearing`. With no provider, `Config.PayoutClearingAccount` is used (`@PayoutClearing` by default). `ClearingAccount` overrides both.

## When the provider fails

@sample cancel | Flutterwave rejects Tunde's payout

`wallet.Cancel` releases everything reserved, the fee included. Nothing is posted, so nothing needs reversing.

## Paid already: Immediate

@sample immediate | An agent pays Ada ₦5,000 in cash

`Immediate: true` posts the withdrawal at once, with no hold and no expiry. Use it only when the money has already gone out. For a provider call that can still fail, keep the default.

## A provider that never answers

A pending withdrawal carries an expiry. If neither `Confirm` nor `Cancel` arrives in time, Blnk cancels it and the money returns to the customer.

@sample expiry | Two payouts that never get an answer

- The default window is `Config.WithdrawalHoldTTL`, 24 hours. A negative value turns expiry off.
- `ExpiresIn` overrides it for one withdrawal.
- Blnk does the cancelling and sends a `transaction.void` webhook. See [Webhooks](webhooks.html).
- The in-memory Blnk expires holds when its clock moves (`core.Advance`).

> warn: If the provider pays after the hold has expired, the customer has the money back and the bank account has it too. Set the window longer than your provider's longest delay, and reconcile its report against the ledger. See [Reconciliation](reconciliation.html).

## Many outcomes at once

A provider's settlement file reports many payouts. `ConfirmMany` and `CancelMany` take up to 100 transactions each.

@sample batch | Two payouts succeeded, one failed

- Each item succeeds or fails on its own. Check every `BatchResult`: `OK`, `Queued`, and `Err`, which is a ledger error you can test with `errors.Is`.
- The call itself only fails for an empty list, more than 100 items, or when Blnk can't be reached.
- `ConfirmItem.Partial` confirms part of a single-leg transaction. A withdrawal with a fee has two legs and is confirmed in full.

## Options

| Field | What it changes |
|---|---|
| `AccountID` | the customer account debited; required, and never a house account |
| `Amount` | what reaches the bank |
| `Fee` | charged on top; required unless `Config.Fees` is set; `ledger.NoFee` for free |
| `Reference` | required idempotency key; a retry with the same input returns the original with `Replayed` |
| `Provider` | clearing account credited, e.g. `"flutterwave"` for `@FlutterwaveClearing` |
| `ClearingAccount` | an exact clearing account, overriding `Provider` |
| `Immediate` | post at once instead of pending |
| `ExpiresIn` | the pending window for this withdrawal, overriding `Config.WithdrawalHoldTTL` |
| `Description` | shown on the statement; "Withdrawal" if empty |
| `MetaData` | your own keys, stored on the transaction |

## What happens in Blnk

A withdrawal is one Blnk transaction from the customer to the clearing account, `INFLIGHT` with `inflight_expiry_date` set unless `Immediate`. Its metadata has `txn_type: withdrawal`. Once confirmed:

| Account | Debit | Credit |
|---|---|---|
| Ada's NGN account | ₦20,050 | |
| `@FlutterwaveClearing` | | ₦20,000 |
| `@FeeIncome` | | ₦50 |

A cancel or an expiry posts nothing: the reservation is released. `@FlutterwaveClearing` now holds what Oja owes Flutterwave for the payout, cleared when you fund it from your bank. See [Settlement](settlement.html).

> note: `ledger.Chart` treats any `@…Clearing` account it doesn't list as an asset. For a provider you only pay out through, add it as a liability at start-up, e.g. `ledger.Chart["@FlutterwaveClearing"] = ledger.GLDef{Type: ledger.GLLiability, Normal: ledger.CreditNormal}`, so reports read it the right way round.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInsufficientFunds` | available balance less than amount plus fee | show the balance |
| `ErrLimitExceeded` | amount plus fee is over a per-transaction, daily or monthly limit | show `wallet.LimitUsage`; see [Limits](limits.html) |
| `ErrAlreadyFinalized` | confirming or cancelling a withdrawal already confirmed, cancelled or expired | treat as done; read it with `wallet.GetTransaction` |
| `ErrNotPending` | the id is not a pending transaction | check you passed the withdrawal's id |
| `ErrInvalidInput` | no `Reference`; no fee and no `Config.Fees`; a partial confirm of a withdrawal with a fee | fix the call |
| `ErrAccountFrozen` / `ErrAccountClosed` | the account is frozen or closed | stop the payout |
| `ErrLedgerFrozen` | the account's ledger is frozen | wait until operations unfreezes it |
| `ErrDuplicateReference` | the reference was used for a different withdrawal | investigate; don't pay out twice |

## Related

- [Quotes for confirm screens](quotes.html): "you receive ₦20,000, fee ₦50" before Ada confirms.
- [Deposits and top-ups](deposits.html): money coming in.
- [Card holds and capture](card-holds.html): the same confirm and cancel calls on holds.
- [Settlement](settlement.html) and [Reconciliation](reconciliation.html).
