# Payouts and holds

**Scenario:** Ada withdraws ₦20,000 to her bank. The money must leave her
spendable balance at once, but only really leaves when the bank confirms. A
second withdrawal is rejected by the bank. Then she takes a ride: ₦6,000 is
reserved on the estimate, and ₦4,200 is charged.

## How a hold works

1. **`Place`** reserves the amount. It leaves `From`'s `Available` at once,
   but doesn't reach `To` yet.
2. When the outside world answers, **`Capture`** moves it (all or part; the
   rest is released), or **`Cancel`** gives it all back.
3. An optional **expiry** cancels the hold if neither happens in time.

## Step 1: place the hold

{{code 06-payouts/main.go place}}

While it's held, `Accounts.Get` shows `Held` 20,000 and `Available` 20,000
less. Store the hold id with your payout, so the bank's callback can find
it. Placing the same transaction again (a retry) returns the same hold.

> **Expiry:** `time.Time{}` means none. If a payout's hold expires while the bank actually sent the money, HumanBlnk has paid out without taking it. For payouts, prefer no expiry and a job that chases stuck ones.

## Step 2: the bank confirms

The zero `Money` captures all of it. Capturing again (a retried callback)
returns the same capture and moves nothing more:

{{code 06-payouts/main.go capture}}

## Step 3: the bank rejects one

`Cancel` gives the money back, and is safe to repeat. Capturing after a
cancel is `ErrHoldSettled`:

{{code 06-payouts/main.go rejected}}

| Call | Repeated |
| --- | --- |
| `Capture(ctx, hold, sameAmount)` | returns the same capture, moves nothing more |
| `Cancel(ctx, hold)` | `nil` |
| `Capture` after `Cancel`, or a different amount after a capture | `ErrHoldSettled` |

## A partial capture

The ride reserved ₦6,000 on the estimate; the fare was ₦4,200. ₦4,200 moves
and the other ₦1,800 goes back to Ada at once:

{{code 06-payouts/main.go partial}}

A capture is final: whatever isn't captured is released. A split hold (a
payout and its fee, say) can only be captured in full.

> **Retrying a partial capture after a timeout:** wait a few seconds first. Blnk can't tell a repeated partial capture from a new one, so a retry that arrives while Blnk is still processing the first can capture twice. Full captures and cancels don't have this problem.

## The whole example

{{run 06-payouts}}

## Good to know

- A hold's own transaction stays `StatusHeld` (`INFLIGHT`) for good. The
  capture and the release are separate transactions under it, so don't use
  the hold's status to ask "did it settle?". `Capture`'s return value, or
  `ErrHoldSettled`, tells you.
- To undo money a capture already moved, use `Transactions.Reverse`
  (chapter 8). `Cancel` only releases money still held.
