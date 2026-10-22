package types

import "errors"

var (
	// ErrInsufficientFunds is a debit the source account can't cover.
	ErrInsufficientFunds = errors.New("ledger: insufficient funds")
	// ErrDuplicate is a write that already happened: its reference holds
	// this same write, applied. A retry after a timeout should treat it as
	// success; the existing entry comes back with it.
	ErrDuplicate = errors.New("ledger: already recorded")
	// ErrReferenceUsed is a reference already taken by something that isn't
	// this write applied: a different transaction, or this one rolled back
	// with the rest of its split or batch, so no money moved. Retrying with
	// the same reference can never succeed: use a new one.
	ErrReferenceUsed = errors.New("ledger: reference used by a write that didn't apply")
	// ErrNotFound is an account, entry or hold that doesn't exist.
	ErrNotFound = errors.New("ledger: not found")
	// ErrUnbalanced is a transaction whose From and To postings don't add
	// up to the same amount.
	ErrUnbalanced = errors.New("ledger: postings don't balance")
	// ErrAlreadyReversed is an entry already reversed in full, or a
	// reversal that would undo more than the entry moved.
	ErrAlreadyReversed = errors.New("ledger: already reversed")
	// ErrHoldSettled is a hold already captured, cancelled or expired.
	ErrHoldSettled = errors.New("ledger: hold already settled")
	// ErrInvalid is a request refused before reaching Blnk, such as a
	// missing reference or a negative amount.
	ErrInvalid = errors.New("ledger: invalid request")
	// ErrReadBack is a write Blnk applied whose result couldn't be read
	// back. The money moved: don't retry the write; look it up by reference.
	ErrReadBack = errors.New("ledger: recorded, but reading it back failed")
)
