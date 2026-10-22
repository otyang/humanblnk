# Deposits

**Scenario:** Ada pays ₦25,000 into her wallet by bank transfer through
Paystack. Paystack's webhook tells HumanBlnk, then sends it again. Days later
Paystack settles the money into HumanBlnk's bank account.

## Where the money comes from

When the webhook arrives, Paystack has the money, not HumanBlnk. So the deposit
comes **from Paystack's clearing account**, `ledger.ClearingAccount("paystack")`
(`@PaystackClearing`). It goes below zero: Paystack owes HumanBlnk that much. When
Paystack settles, it returns to zero, so a balance left in a clearing account
is money still in transit.

## Step 1: record the deposit

This is what your Paystack webhook handler does with a successful charge:

{{code 04-deposits/main.go deposit}}

| `Transaction` field | |
| --- | --- |
| `Ref` | required. Built with `ledger.Reference` from **your own ids**, never a random value. Here it's Paystack's id for the charge, so a retried webhook carries the same one. |
| `From`, `To` | postings: `ledger.Single(account, amount)`, or `ledger.Many(...)` for a split (chapter 5) |
| `Description` | required |
| `Meta` | optional references, such as the provider's id |

`ledger.AccountRef(wallet.ID)` names a customer's account;
`ledger.ClearingAccount(...)`, `ledger.World` and `ledger.FeeIncome` name
system accounts.

## Step 2: the retried webhook

Paystack resends webhooks on timeouts. Recording again with the same `Ref`
can't apply twice:

{{code 04-deposits/main.go retry}}

A forged or buggy webhook that reuses the reference for another amount is
caught too:

{{code 04-deposits/main.go forged}}

| Error | Means | Do |
| --- | --- | --- |
| `ErrDuplicate` | this exact deposit already happened | treat as success; the existing entry comes back with it |
| `ErrReferenceUsed` | the reference holds something else | investigate; never treat as success |

## Step 3: look it up

`GetByReference` is how you ask "did this already happen?", for example from
a support tool. `Get(ctx, id)` fetches by entry id.

{{code 04-deposits/main.go lookup}}

## Step 4: Paystack settles

When the money reaches HumanBlnk's bank, Paystack no longer owes it. It moves from
outside HumanBlnk (`@World`) into the clearing account, bringing it back to zero:

{{code 04-deposits/main.go settle}}

Chapter 11 checks Paystack's settlement report against these deposits.

## The whole example

{{run 04-deposits}}
