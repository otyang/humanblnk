# Customers and accounts

**Scenario:** Ada signs up. She gets a naira wallet straight away, and later
a dollar wallet. HumanBlnk also needs an escrow pool of its own.

## Step 1: the owner

Every customer account has an **owner**: a Blnk identity that holds **no
personal data**. Ada's name, phone number and KYC stay in your own customer
database; the identity holds only references such as your customer id.

Find it, or make it the first time, so a retried sign-up doesn't make a
second one. `Identities.Create` on its own isn't safe to repeat.

{{code 02-onboarding/main.go owner}}

## Step 2: open her wallets

`OpenForCustomer` **is** safe to repeat. A customer has at most one account
per ledger and currency, and calling it again returns that one. So you can
call it whenever you need Ada's wallet, instead of storing ids everywhere.

{{code 02-onboarding/main.go open}}

| `CustomerAccount` field | |
| --- | --- |
| `Owner` | required |
| `Currency` | one per account: a second currency is a second account |
| `Ledger` | optional: empty is the `WithAccountLedger` one. Use your Savings ledger for a savings account. Must be a customer ledger. |

## Step 3: read balances

{{code 02-onboarding/main.go balances}}

An `Account` has three amounts:

| Field | Meaning |
| --- | --- |
| `Total` | everything recorded |
| `Held` | reserved by holds not yet captured or cancelled |
| `Available` | `Total` less held and queued debits: **what she can spend now** |

Always decide "can she pay?" on `Available`, read with `Get`. `List` is
quicker for many accounts, but its `Available` leaves queued debits out.

## Step 4: tag the owner later

`UpdateMeta` merges: keys you don't send keep their values. References only,
never personal data.

{{code 02-onboarding/main.go meta}}

## HumanBlnk's own accounts

An account that belongs to HumanBlnk uses `CreateInternal`, in a ledger that isn't
a customer one. The owner is optional. Unlike `OpenForCustomer`, it makes a
new account on every call: if it times out, look with `List` first.

{{code 02-onboarding/main.go internal}}

The rules hold both ways, so mistakes fail early:

{{code 02-onboarding/main.go rules}}

| Call | Result |
| --- | --- |
| `OpenForCustomer` without an owner | `ErrInvalid` |
| `OpenForCustomer` in a ledger that isn't a customer ledger | `ErrInvalid` |
| `CreateInternal` in a customer ledger | `ErrInvalid`: use OpenForCustomer |
| `CreateInternal` in the General Ledger | `ErrInvalid`: Blnk makes system accounts itself |

## The whole example

{{run 02-onboarding}}
