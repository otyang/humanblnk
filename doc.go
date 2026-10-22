// Package humanblnk is HumanBlnk's books, kept in Blnk. It speaks money.Money
// and HumanBlnk's own errors; nothing else in HumanBlnk moves money through
// blnk.
//
// The vocabulary is bookkeeping:
//
//   - An Account is a Blnk balance: money in one currency. A customer's
//     account always has an owner (a Blnk identity holding no personal
//     data) and is opened with Accounts.OpenForCustomer, at most one per
//     customer ledger and currency. HumanBlnk's own accounts are made with
//     Accounts.CreateInternal, outside the customer ledgers. An Account is
//     not Blnk's "accounts" feature, which stores bank account numbers and
//     lives outside this package.
//   - A Transaction is what you ask for: money From some Postings To others.
//     Transactions.Record moves the money; it is not a log of a move made
//     elsewhere.
//   - An Entry is what was recorded.
//   - A hold reserves money until it is captured or cancelled
//     (Holds.Place, Holds.Capture, Holds.Cancel). Cancel only releases a
//     hold; Transactions.Reverse is the only way to undo money that moved.
//
// Every operation keeps the same rules:
//
//   - Amounts reach Blnk only in their currency's smallest unit, with that
//     currency's precision, never one a caller chose (internal/core).
//   - A transaction's currency must match every account it touches. Blnk
//     does not check this: it would apply USD 10.00 to an NGN account as
//     ₦10.00.
//   - Every money movement (Record, RecordAll, Place, Reverse) carries a
//     reference, the same on every retry, so a retry cannot apply it twice. ErrDuplicate means it already happened;
//     ErrReferenceUsed means the reference holds something that didn't
//     apply, such as a split Blnk rolled back, and only a new one will do.
//   - Writes are applied synchronously (Blnk's skip_queue), so a refusal such
//     as ErrInsufficientFunds is returned, not discovered later.
//   - Callers see this package's errors; when Blnk refused the call, they
//     also wrap Blnk's, for logs. VerifyWebhook returns blnk.ErrBadSignature
//     or blnk.ErrStale.
package humanblnk
