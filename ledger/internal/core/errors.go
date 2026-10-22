package core

import (
	"errors"
	"fmt"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger/types"
)

// Invalid is types.ErrInvalid with a reason.
func Invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", types.ErrInvalid, fmt.Sprintf(format, args...))
}

// MapErr turns Blnk errors into the ledger's, keeping Blnk's for logs.
func MapErr(err error) error {
	if err == nil {
		return nil
	}
	var ae *blnk.APIError
	if errors.As(err, &ae) {
		if mapped := CodeErr(ae.Code); mapped != nil {
			return fmt.Errorf("%w: %w", mapped, err)
		}
	}
	if blnk.IsNotFound(err) {
		return fmt.Errorf("%w: %w", types.ErrNotFound, err)
	}
	return err
}

// CodeErr is the ledger error for a Blnk error code, or nil.
func CodeErr(code string) error {
	switch code {
	case blnk.CodeInsufficientFunds:
		return types.ErrInsufficientFunds
	case blnk.CodeDuplicateReference:
		return types.ErrDuplicate
	case blnk.CodeAlreadyRefunded:
		return types.ErrAlreadyReversed
	case blnk.CodeAlreadyCommitted, blnk.CodeAlreadyVoided, blnk.CodeNotInflight:
		return types.ErrHoldSettled
	case blnk.CodeTxnNotFound, blnk.CodeBalanceNotFound, blnk.CodeLedgerNotFound, blnk.CodeIdentityNotFound:
		return types.ErrNotFound
	}
	return nil
}
