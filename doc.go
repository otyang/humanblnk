// Package humanblnk keeps a wallet's or bank's books in Blnk Core 0.15.6.
// Callers work in bookkeeping terms and exact money (money.Money), and
// humanblnk makes Blnk follow the rules a money ledger needs, which Blnk
// alone leaves to the caller. It is a library, not a server, and it moves
// money only in Blnk: payment providers and banks stay the caller's to talk
// to.
//
// Build one Client at start-up and share it:
//
//	hb := humanblnk.New(blnk.New(url, blnk.WithAPIKey(key)),
//		humanblnk.WithAccountLedger(walletsLedgerID),
//		humanblnk.WithCustomerLedgers(savingsLedgerID),
//	)
//
// The ledgers themselves are made once, by a setup script, with
// Ledgers.Create.
//
// A Client remembers the currency of up to 100,000 recently used accounts
// (about 20 MiB), so it needn't ask Blnk before every write.
// WithAccountCacheSize changes that; either way memory stays bounded however
// many accounts a long-running service touches. The tutorial at https://otyang.github.io/humanblnk/docs/
// walks through each task with a program to run.
//
// # Vocabulary
//
//   - A Ledger is a Blnk ledger: a named group of accounts, such as
//     "Customer Wallets". Blnk's own General Ledger holds the system
//     accounts.
//   - An Account is a Blnk balance: money in one currency. A customer's
//     account always has an owner (an Identity holding no personal data)
//     and is opened with Accounts.OpenForCustomer, one per customer ledger
//     and currency. Your company's own accounts are made with
//     Accounts.CreateInternal, outside the customer ledgers. An Account is
//     not Blnk's "accounts" feature, which stores bank account numbers and
//     lives outside this package.
//   - System accounts are your company's own, and the only accounts that may
//     go below zero: World (money outside your books: deposits come from it,
//     payouts go to it), FeeIncome, Adjustments, ClearingAccount(provider)
//     for what a payment provider owes you until it settles, and any other
//     SystemAccount(name).
//   - A Transaction is what you ask for: money From some Postings To others,
//     in one atomic write. One side may have several postings (a split, such
//     as an amount plus a fee), not both. Transactions.Record moves the
//     money; it is not a log of a move made elsewhere.
//   - An Entry is what was recorded.
//   - A hold reserves money until it is captured or cancelled
//     (Holds.Place, Holds.Capture, Holds.Cancel).
//   - A Reversal moves money back the way an entry moved it, in full or in
//     part.
//
// # Services
//
// A Client groups its operations by what they act on. Each service also has
// a type of its own (IdentityService, AccountService, ...), so code that
// needs only one can take just that.
//
//   - Identities: Create, Get, UpdateMeta, FindByCustomer. Owners for
//     customer accounts, holding only references such as customer_id.
//   - Accounts: OpenForCustomer, CreateInternal, Get, List, and System for a
//     system account by name and currency.
//   - Transactions: Record, RecordAll, Preview, Reverse, Get,
//     GetByReference, List. Moving money now, undoing it, and reading what
//     was recorded. Reverse takes an entry's id, or a captured hold's: the
//     id Capture returned, or the hold's own.
//   - Holds: Place, Capture, Cancel. Reserving money now, settling it later.
//   - Watches: Create, Get, List, Change, Delete. Thresholds that fire Blnk's
//     balance.monitor webhook.
//   - Reconciliation: Upload, AddRule, ChangeRule, RemoveRule, Run,
//     RunLines, Result. Matching provider and bank statements against
//     entries.
//   - Ledgers: Create, Get, List, Rename.
//
// VerifyWebhook checks a Blnk webhook's signature and age, then decodes it
// into an Event.
//
// # Rules
//
// Every operation keeps the same rules:
//
//   - Amounts reach Blnk only in their currency's smallest unit, with that
//     currency's precision, never one a caller chose. Blnk balances are bare
//     integers, so one transaction at another precision would mix units
//     silently. Entries read back are checked against the same precision.
//   - A transaction's currency must match every account it touches. Blnk
//     does not check this: it would apply USD 10.00 to an NGN account as
//     NGN 10.00.
//   - Every money movement (Record, RecordAll, Place, and a partial Reverse)
//     carries a reference, built from the caller's own ids with Reference
//     and the same on every retry, so a retry cannot apply it twice. A
//     retried Record or RecordAll returns ErrDuplicate with the entry; a
//     retried Place returns the hold already placed (ErrHoldSettled if it
//     was released since), and a Reverse retried with its Ref the reversal
//     already made. ErrReferenceUsed means the reference is taken by
//     something other than this write: a different transaction, or this one
//     refused partway. Any reference works, "/" included; a split and a single
//     transaction can't share one, so each write first checks that.
//   - Writes are applied synchronously (Blnk's skip_queue), so a refusal such
//     as ErrInsufficientFunds is returned, not discovered later as a
//     rejected status.
//   - A write applies in full or not at all, and a refused one moves nothing,
//     not even for a moment. Blnk moves a split a posting at a time: applied,
//     one refused partway has already moved its earlier postings, which Blnk
//     refunds only later. So Record sends a split as a hold and captures it
//     at once, and RecordAll its batch the same way: a hold refused partway
//     is released before Blnk answers. The entry still reads as the one
//     applied transaction it is.
//   - Only system accounts may go below zero; a customer account never may.
//     Blnk allows an overdraft per transaction, not per source, so a split
//     taking from both (a refund of a transfer with a fee, say) goes to Blnk
//     as an atomic batch, one transaction per source, and reads back as the
//     split it is.
//   - Reversals, full or partial, are numbered per entry. They take turns,
//     even concurrently, and never undo more than the entry moved. A reversal
//     takes from where the entry put the money, so a customer who spent it
//     can't be pushed below zero.
//   - Holds.Cancel only releases a hold; Transactions.Reverse is the only way
//     to undo money that moved. Corrections stay in the books as entries of
//     their own.
//   - Customer data stays out of Blnk: identities hold references such as
//     customer_id, nothing more.
//
// Check Account.Available, not Account.Total, before letting a customer
// spend. From Accounts.Get, Available is Total less outgoing holds and queued debits;
// Accounts.List leaves out queued debits.
//
// # Retries
//
// The blnk client already retries transient failures (3 times by default;
// see blnk.WithRetries), and only where a repeat is harmless. After that, a
// *blnk.APIError with a 4xx status means Blnk refused the call; a network
// error or a gateway timeout means the outcome is unknown, so repeat the
// call with the same arguments if it is safe to:
//
//   - Safe: Transactions.Record and RecordAll (same Ref), Reverse (same Ref;
//     a full reversal without one returns ErrAlreadyReversed once done),
//     Holds.Place, Holds.Cancel, a full Holds.Capture,
//     Accounts.OpenForCustomer, Identities.UpdateMeta, Ledgers.Rename,
//     Reconciliation.ChangeRule, every read, and every delete (a repeat may
//     return ErrNotFound).
//   - Wait a few seconds first: a partial Holds.Capture. Blnk's commit takes
//     no reference, and a partial capture still running in Blnk isn't
//     visible yet, so a quick repeat can capture twice.
//   - Not safe, so look first: Identities.Create (use FindByCustomer),
//     Accounts.CreateInternal, Ledgers.Create, Watches.Create and Change
//     (use List), and Reconciliation.Upload, AddRule, Run and RunLines.
//
// # Errors
//
// Compare errors with errors.Is, never on the message:
//
//   - ErrInsufficientFunds: the source account can't cover it; nothing moved.
//   - ErrDuplicate: this exact write already happened, applied. Treat it as
//     success: the existing entry comes back with it.
//   - ErrReferenceUsed: this write did not apply; the reference holds a
//     different transaction, or this one was refused partway (for a Reverse,
//     its Ref names a reversal of another amount). After a refusal, record
//     it again under a new reference; otherwise investigate how two writes
//     got the same reference.
//   - ErrNotFound: no such account, entry, hold, ledger, identity or watch.
//   - ErrUnbalanced: From and To don't add up to the same amount.
//   - ErrAlreadyReversed: nothing, or not that much, is left to reverse.
//   - ErrHoldSettled: the hold was already captured, cancelled or expired;
//     the id isn't a hold; a repeated Capture asked for a different amount,
//     or more than is left; a split hold has a posting released (say, by
//     its expiry), so it can't be captured whole; or a Place found its hold
//     released.
//   - ErrInvalid: refused before reaching Blnk, such as a missing reference,
//     a negative amount, the wrong ledger, or reversing an entry that isn't
//     applied. Don't retry it unchanged.
//   - ErrReadBack: RecordAll applied, but reading the entries back failed.
//     Don't record it again; look the entries up by reference.
//   - money.ErrCurrencyMismatch: an amount's currency doesn't match the
//     account's. money.ErrUnknownCurrency: a Money that was never set, or an
//     unknown currency code.
//
// When Blnk refused the call, the error also wraps Blnk's, for logs; a Blnk
// error humanblnk has no name for comes back as a *blnk.APIError.
// VerifyWebhook returns blnk.ErrBadSignature or blnk.ErrStale when the check
// fails, and a plain error for a body it can't read or decode.
//
// # Testing
//
// Package humanblnktest runs a Client against an in-memory Blnk, so code
// built on humanblnk can be tested with no Blnk, Docker or network:
//
//	hb := humanblnktest.New(t)
//	ada := hb.Customer(t, "USD")
//	hb.Fund(t, ada, "100.00")
//
// It copies the Blnk Core 0.15.6 behaviour humanblnk relies on, and
// humanblnk's contract tests run against it and a real Blnk alike.
//
// # Limits
//
//   - A transaction can't be many to many: one side may have several
//     postings, not both. RecordAll takes one posting on each side.
//   - A split is reversed and captured only in full.
//   - Transactions.List shows Blnk's own records, each posting of a split
//     its own entry. A split or RecordAll batch, held and captured, is there
//     twice: its postings held (StatusHeld) and their captures
//     (StatusApplied), the captures under the transaction's reference. List
//     with Status StatusApplied for what moved. Blnk can't filter "from or
//     to" together with other conditions, so an account's full history is
//     two List calls, one with From, one with To.
//   - List calls return 1 to 100 items per page (20 by default).
//   - A watch threshold is at most 2^53 in the currency's smallest unit.
//     Watches.Change makes a new watch with a new ID, and both may fire once
//     while it switches.
//   - Reconciliation.RunLines takes at most 10,000 lines; upload a file for
//     more.
//   - Accounts.OpenForCustomer calls racing from different processes can
//     each open an account. All of them then return the oldest; the others
//     are left empty and marked with meta_data.duplicate_of.
//   - A split Record, or a RecordAll, whose process stops between hold and
//     capture leaves the money reserved, not moved: the destination doesn't
//     have it yet, and the source can't spend it. Retried within RecordHold
//     (15 minutes) it is captured; after that the hold expires, the money is
//     released, and the reference is used (ErrReferenceUsed). Keep Blnk's
//     worker running: it expires holds.
package humanblnk
