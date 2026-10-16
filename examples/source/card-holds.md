title: Card holds and capture
group: wallet
order: 5
lead: Reserve money on an account like a card authorisation, then capture all or part of it, release the rest, or let it lapse on a timer.
---
## The situation

Ada pays for dinner with her Oja virtual card. The card network first asks Oja to authorise ₦18,500, and only later captures what the restaurant settles. In between, the money must stay out of Ada's reach. `wallet.Hold` reserves it without moving it; `wallet.Confirm` captures it and `wallet.Cancel` releases it.

Pick the call by situation:

| Situation | Use |
|---|---|
| A card authorisation | `wallet.Hold` with `ExpiresAt` |
| The merchant captures the full amount | `wallet.Confirm(ctx, id, nil)` |
| The merchant captures less, or in several parts | `wallet.Confirm` with an amount, then `wallet.Cancel` for the rest |
| The authorisation is reversed | `wallet.Cancel` |
| A hold that captures itself at a set time | `CaptureAt` |
| A held payment between two customers | `wallet.Transfer` with `Pending: true`, see [Transfers](transfers.html) |
| A payout waiting on a provider | `wallet.Withdraw`, see [Withdrawals and payouts](withdrawals.html) |
| Money held until an order is delivered | `wallet.OpenEscrow`, see [Escrow](escrow.html) |

## Authorise: wallet.Hold

@sample authorise | The restaurant asks for ₦18,500

- `Destination` is where captured money goes: a house account such as `ledger.MerchantPayableAccount` (`@MerchantPayable`), or a merchant's own account. It is required.
- `Reference` is required. Use the network's authorisation id so a repeated request returns the same hold.
- The hold is `pending`. `Balance` is unchanged, `Available` drops by ₦18,500 and `PendingOut` shows it. The destination shows it as `PendingIn`.
- A hold has no fee.

## Capture in full

@sample capture | The restaurant settles the full bill

The money leaves Ada's `Balance` and is posted to `@MerchantPayable`, where it waits until Oja pays the card network.

## Capture part, release the rest

@sample partial | A fuel station captures twice, then releases the rest

- `Confirm` with an amount captures only that much. Call it again for another part. The amount's currency can be left empty; it is taken from the hold.
- `Cancel` releases whatever is still held. Its receipt's `Amount` is the amount released.
- You can't capture more than is still held: `ErrAmountExceeded`.

## Paying a merchant's own account

@sample merchant | Chidi Stores takes a deposit hold

When `Destination` is a customer account, the merchant sees the money as `PendingIn` until capture, and in its `Balance` after.

The merchant's account must be active. A hold paying a frozen or closed account is refused, and so is a capture into one that was frozen or closed after the hold was placed. Releasing the hold with `Cancel` still works. See the errors sample below and [Freeze and unfreeze](freeze.html).

## Timers: ExpiresAt and CaptureAt

@sample timers | A hotel hold that lapses and a subscription that captures itself

- `ExpiresAt` releases the hold at that time if it hasn't been captured. Card networks drop an uncaptured authorisation after some days; match their window.
- `CaptureAt` captures whatever is still held at that time. It must be before `ExpiresAt`, or the hold would be released first.
- Blnk runs both timers itself and sends a webhook: `transaction.applied` for the capture, `transaction.void` for the release. See [Webhooks](webhooks.html).
- On real Blnk, `CaptureAt` needs Blnk's inflight commit queue to be running. The in-memory Blnk runs both timers when its clock moves (`core.Advance`).

## Holds and limits

A hold is spending as far as [limits](limits.html) are concerned: it counts as soon as it is placed, not when it is captured.

@sample limits | ₦150,000 a day, with a ₦40,000 hold

- `wallet.LimitUsage` shows `DailyUsed` and `DailyLeft` including open holds.
- A released hold gives its room back. A captured one keeps counting.

## Options

| Field | What it changes |
|---|---|
| `AccountID` | the customer account the money is held on; never a house account |
| `Destination` | where captured money goes; required; a customer account or `ledger.AccountID("@Name")` |
| `Amount` | how much to hold |
| `Reference` | required idempotency key |
| `ExpiresAt` | release automatically at this time |
| `CaptureAt` | capture automatically at this time; must be before `ExpiresAt` |
| `Description` | shown on the statement; "Hold" if empty |
| `MetaData` | your own keys, e.g. the merchant's name and MCC |

## What happens in Blnk

A hold is one Blnk transaction from the customer to the destination, created `INFLIGHT` with `txn_type: hold`. `ExpiresAt` becomes `inflight_expiry_date` and `CaptureAt` becomes `inflight_commit_date`.

| Step | Account | Debit | Credit |
|---|---|---|---|
| Hold ₦18,500 | Ada's NGN account | ₦18,500 pending | |
| | `@MerchantPayable` | | ₦18,500 pending |
| Capture | Ada's NGN account | ₦18,500 | |
| | `@MerchantPayable` | | ₦18,500 |

Each capture or release is recorded by Blnk as its own child transaction of the hold: `APPLIED` for a capture, `VOID` for a release. A release posts nothing.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInsufficientFunds` | the available balance is less than the hold | decline the authorisation |
| `ErrLimitExceeded` | the hold is over the account's limit | decline; see [Limits](limits.html) |
| `ErrInvalidInput` | no `Destination` or `Reference`; `CaptureAt` not before `ExpiresAt` | fix the call |
| `ErrAmountExceeded` | capturing more than is still held | capture at most what is left |
| `ErrAlreadyFinalized` | capturing or releasing a hold already released, expired or fully captured | treat as done |
| `ErrNotPending` | the id is not a pending transaction, e.g. a capture's own id | pass the hold's `TransactionID` |
| `ErrAccountFrozen` / `ErrAccountClosed` / `ErrLedgerFrozen` | the paying account, or its ledger, can't send money | decline |
| `ErrAccountFrozen` / `ErrAccountClosed` | `Destination` is a frozen or closed customer account, on `Hold` or on `Confirm` | decline the hold; release an open one with `Cancel` |

## Related

- [Withdrawals and payouts](withdrawals.html): the same confirm and cancel calls, with `ConfirmMany` and `CancelMany` for batches.
- [Disputes](disputes.html): hold money that has already been paid.
- [House accounts](house-accounts.html): `@MerchantPayable` and the rest of the chart.
