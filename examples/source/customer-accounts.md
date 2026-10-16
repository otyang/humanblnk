title: Create customer accounts
group: wallet
order: 1
lead: Open accounts for people and businesses: one call for a whole customer, single accounts, savings pots and escrows, all safe to retry.
---
## Before you start

Customer accounts live in a Blnk ledger. Create it once when you provision, put its id in `Config.LedgerID`, and call `svc.Check(ctx)` at start-up so a wrong id fails at boot instead of at the first sign-up. See [Provision ledgers](ledgers.html).

Pick the call by situation:

| Situation | Use |
|---|---|
| A new user signs up | `wallet.OpenCustomer` with `ExternalID` |
| An existing user wants another currency | `wallet.OpenCustomer` again with the same `ExternalID`, or `wallet.OpenAccount` with their `IdentityID` |
| One account with no person behind it, or in another ledger | `wallet.OpenAccount` |
| A savings goal inside a customer's wallet | `wallet.OpenPot` |
| Money held until an order is delivered | `wallet.OpenEscrow` |

## A person: wallet.OpenCustomer

The usual way. One call creates a Blnk **identity** (who the customer is) and **one account per currency**.

@sample person | Ada signs up with naira and dollar accounts

- `ada.ID` is the identity. `ada.Accounts` maps each currency to its `*ledger.Account`.
- Use the account's `ID` (`bln_…`) for deposits, transfers and everything else.
- `MetaData` is copied onto the identity and onto every account. The `tier` key is what [limits](limits.html) and [fees](fees.html) read.
- Real Blnk ids are longer, e.g. `bln_6f1c…`; the in-memory Blnk used for these examples numbers them.

## A business

Set `Type: "organization"` and give `OrganizationName` instead of a person's name.

@sample business | Chidi Stores Ltd, a merchant

## Retrying, or adding a currency later

`ExternalID` is your own user id. Call `OpenCustomer` again with the same one and nothing is duplicated: existing accounts are reused and missing currencies are opened. That makes a sign-up that timed out safe to repeat, and gives you a one-line way to add a currency.

@sample retry

## One account: wallet.OpenAccount

Use `OpenAccount` when you want a single account rather than a whole customer.

### For a customer who already has an identity

@sample existing-identity | Ada adds a USDT account

### With no identity

For an agent's float, an internal sub-wallet or anything without a person behind it. `ExternalID` makes it safe to retry here too: the same `ExternalID` and currency return the existing account.

@sample no-identity

### In a different ledger

`LedgerID` overrides `Config.LedgerID`, e.g. one ledger per country. The ledger must exist; this is checked before anything is created.

@sample other-ledger

## A savings pot: wallet.OpenPot

A pot is a sub-account under a customer account. It takes its parent's currency and identity, can have a goal, and money moves in and out of it with an ordinary transfer. Opening the same name twice returns the existing pot. More in [Savings pots and standing orders](savings-pots.html).

@sample pot | A rent pot with a ₦600,000 goal

## An escrow account: wallet.OpenEscrow

Opening an escrow creates its own account and moves the money into it at once, so the money has left the payer but isn't the seller's yet. More in [Escrow](escrow.html).

@sample escrow | Ada pays ₦80,000 into escrow for an order from Chidi Stores

## Reading accounts back

@sample read

- `GetCustomer` returns the identity and its main accounts by currency. Pots and escrows are left out; find them with `FindAccounts` and `Kind`.
- `FindAccounts` filters by identity, currency, kind, status or any metadata key, such as your own `external_id`. It never returns house accounts such as `@FeeIncome`; read those with `ops.GetHouseAccount`.
- `Available` is what the customer can spend now: the balance minus holds and pending withdrawals.

## What happens in Blnk

| Call | Blnk records |
|---|---|
| `OpenCustomer` | one identity (`POST /identities`) and one balance per currency (`POST /balances`), each marked `status: active` and `account_kind: customer` |
| `OpenAccount` | one balance, in `Config.LedgerID` or the `LedgerID` you gave |
| `OpenPot` | one balance linked to its parent with `parent_account` and `pot_name` metadata |
| `OpenEscrow` | one balance, then a transfer from the payer into it |

No money moves when an account is opened, except for an escrow. Precision comes from the currency: NGN and USD 100, BTC 100000000, USDT 1000000, and so on.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInvalidInput` | no currency; a person with no name; an organization with no `OrganizationName`; an unknown `Type` | fix the call |
| `ErrLedgerNotFound` | `Config.LedgerID` or `LedgerID` names a ledger that doesn't exist | fix the configuration; `svc.Check` catches it at start-up |
| `ErrWrongKind` | a pot or escrow on a house account such as `@FeeIncome` | pass a customer account |
| `ErrUnavailable` | Blnk can't be reached after retries | retry later with the same `ExternalID` |

## Reserved metadata

The keys humanblnk manages (`status`, `account_kind`, `external_id`) are ignored when you pass them in `MetaData`, so an account can't be opened already frozen or disguised as a pot. Your own keys are kept. Freeze with `ops.FreezeAccount`; set the user id with `ExternalID`.

@sample reserved
