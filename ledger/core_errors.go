package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/otyang/humanblnk/blnk"
)

// Domain errors. Check with errors.Is; the underlying *blnk.APIError is still
// reachable with errors.As for logging (status, code, details).
var (
	ErrInvalidInput       = errors.New("ledger: invalid input")
	ErrInsufficientFunds  = errors.New("ledger: insufficient funds")
	ErrNotFound           = errors.New("ledger: not found")
	ErrDuplicateReference = errors.New("ledger: reference already used for a different transaction")
	ErrAlreadyFinalized   = errors.New("ledger: transaction already confirmed, cancelled or refunded")
	ErrNotPending         = errors.New("ledger: transaction is not pending (INFLIGHT)")
	ErrAmountExceeded     = errors.New("ledger: amount exceeds remaining held amount")
	ErrAccountFrozen      = errors.New("ledger: account is frozen or blocked")
	ErrAccountClosed      = errors.New("ledger: account is closed")
	ErrPendingActivity    = errors.New("ledger: account has pending (INFLIGHT) activity")
	ErrLimitExceeded      = errors.New("ledger: transaction limit exceeded")
	ErrApprovalRequired   = errors.New("ledger: approval required")
	ErrSignature          = errors.New("ledger: invalid webhook signature")
	ErrFeePricing         = errors.New("ledger: fee could not be priced")
	ErrConflict           = errors.New("ledger: conflicting operation in progress")
	ErrUnavailable        = errors.New("ledger: ledger temporarily unavailable")
	ErrUnauthorized       = errors.New("ledger: unauthorized")

	// ErrLedgerNotFound: the ledger doesn't exist. errors.Is(err, ErrNotFound) also holds.
	ErrLedgerNotFound = fmt.Errorf("ledger: ledger not found: %w", ErrNotFound)
	// ErrLedgerFrozen: the account's ledger is frozen, so no money can leave it.
	ErrLedgerFrozen = errors.New("ledger: ledger is frozen")
	// ErrWrongKind: the id names another kind of object, e.g. a ledger id passed
	// to a customer-account method, or a house account where a customer account is required.
	// errors.Is(err, ErrInvalidInput) also holds.
	ErrWrongKind = fmt.Errorf("ledger: id names the wrong kind of object: %w", ErrInvalidInput)
)

// OpError carries the operation name, a domain sentinel, and the cause.
type OpError struct {
	Op    string
	Kind  error
	Cause error
}

// Error formats the operation, the sentinel and the cause.
func (e *OpError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("%s: %v", e.Op, e.Kind)
	}
	return fmt.Sprintf("%s: %v: %v", e.Op, e.Kind, e.Cause)
}

// Unwrap exposes both the sentinel and the cause to errors.Is / errors.As.
func (e *OpError) Unwrap() []error {
	if e.Cause == nil {
		return []error{e.Kind}
	}
	return []error{e.Kind, e.Cause}
}

func invalid(op, format string, args ...any) error {
	return &OpError{Op: op, Kind: ErrInvalidInput, Cause: fmt.Errorf(format, args...)}
}

// mapErr translates Blnk error codes into domain sentinels.
func mapErr(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var oe *OpError
	if errors.As(err, &oe) {
		return err
	}

	var ae *blnk.APIError
	if !errors.As(err, &ae) {
		return &OpError{Op: op, Kind: ErrUnavailable, Cause: err} // network / decode
	}

	kind := ErrUnavailable
	switch ae.Code {
	case blnk.CodeInsufficientFunds:
		kind = ErrInsufficientFunds
	case blnk.CodeDuplicateReference:
		kind = ErrDuplicateReference
	case blnk.CodeAlreadyCommitted, blnk.CodeAlreadyVoided, blnk.CodeAlreadyRefunded:
		kind = ErrAlreadyFinalized
	case blnk.CodeNotInflight:
		kind = ErrNotPending
	case blnk.CodeCommitAmountExceeded:
		kind = ErrAmountExceeded
	case blnk.CodeConflict:
		kind = ErrConflict
	case blnk.CodeLedgerNotFound:
		kind = ErrLedgerNotFound
	case blnk.CodeTxnNotFound, blnk.CodeBalanceNotFound, blnk.CodeNotFound,
		blnk.CodeMetadataEntityNotFound, blnk.CodeHistoryNotFound,
		blnk.CodeMonitorNotFound, blnk.CodeIdentityNotFound:
		kind = ErrNotFound
	default:
		switch {
		case ae.Status == 404:
			kind = ErrNotFound
		case ae.Status == 401 || ae.Status == 403:
			kind = ErrUnauthorized
		case ae.Status == 409:
			kind = ErrConflict
		case ae.Status == 423:
			kind = ErrConflict
		case ae.Status >= 400 && ae.Status < 500 && ae.Status != 429:
			kind = ErrInvalidInput
		}
	}
	return &OpError{Op: op, Kind: kind, Cause: err}
}
