package blnk

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Stable error codes from https://docs.blnkfinance.com/advanced/error-codes
// (Blnk Core 0.15.0+). Branch on these, never on message text.
const (
	CodeMalformedRequest = "GEN_MALFORMED_REQUEST"
	CodeValidation       = "GEN_VALIDATION_ERROR"
	CodeBadRequest       = "GEN_BAD_REQUEST"
	CodeNotFound         = "GEN_NOT_FOUND"
	CodeConflict         = "GEN_CONFLICT"
	CodeResourceLocked   = "GEN_RESOURCE_LOCKED"
	CodeRateLimited      = "GEN_RATE_LIMITED"
	CodeInternal         = "GEN_INTERNAL"

	CodeMissingAPIKey     = "AUTH_MISSING_API_KEY"
	CodeInvalidAPIKey     = "AUTH_INVALID_API_KEY"
	CodeInsufficientScope = "AUTH_INSUFFICIENT_PERMISSIONS"

	CodeLedgerNotFound  = "LGR_NOT_FOUND"
	CodeBalanceNotFound = "BAL_NOT_FOUND"

	CodeTxnNotFound            = "TXN_NOT_FOUND"
	CodeInsufficientFunds      = "TXN_INSUFFICIENT_FUNDS"
	CodeInvalidAmount          = "TXN_INVALID_AMOUNT"
	CodeInvalidDistribution    = "TXN_INVALID_DISTRIBUTION"
	CodeDuplicateReference     = "TXN_DUPLICATE_REFERENCE"
	CodeNotInflight            = "TXN_NOT_INFLIGHT"
	CodeAlreadyCommitted       = "TXN_ALREADY_COMMITTED"
	CodeAlreadyVoided          = "TXN_ALREADY_VOIDED"
	CodeAlreadyRefunded        = "TXN_ALREADY_REFUNDED"
	CodeCommitAmountExceeded   = "TXN_COMMIT_AMOUNT_EXCEEDED"
	CodeInvalidStatusAction    = "TXN_INVALID_STATUS_ACTION"
	CodeBulkEmpty              = "TXN_BULK_EMPTY"
	CodeBulkLimitExceeded      = "TXN_BULK_LIMIT_EXCEEDED"
	CodeTxnValidation          = "TXN_VALIDATION_ERROR"
	CodeQueueBackpressure      = "QUEUE_BACKPRESSURE"
	CodeMetadataEntityNotFound = "META_ENTITY_NOT_FOUND"
)

// APIError is a non-2xx response from Blnk.
type APIError struct {
	Status  int            // HTTP status
	Code    string         // error_detail.code (may be empty on Core < 0.15.0)
	Message string         // display text only
	Details map[string]any // error_detail.details
	Body    string         // raw body, for logging
}

// Error formats the status, code and message.
func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("blnk: %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("blnk: %d: %s", e.Status, e.Message)
}

// HasCode reports whether err is an *APIError carrying one of codes.
func HasCode(err error, codes ...string) bool {
	var ae *APIError
	if !errors.As(err, &ae) {
		return false
	}
	for _, c := range codes {
		if ae.Code == c {
			return true
		}
	}
	return false
}

// IsNotFound reports whether err is any kind of 404 from Blnk.
func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

func parseAPIError(status int, raw []byte) *APIError {
	e := &APIError{Status: status, Body: string(raw)}
	var body struct {
		Error       json.RawMessage `json:"error"`
		Errors      json.RawMessage `json:"errors"`
		ErrorDetail *struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error_detail"`
	}
	if json.Unmarshal(raw, &body) == nil {
		if body.ErrorDetail != nil {
			e.Code = body.ErrorDetail.Code
			e.Message = body.ErrorDetail.Message
			e.Details = body.ErrorDetail.Details
		}
		if e.Message == "" {
			e.Message = rawString(body.Error)
		}
		if e.Message == "" {
			e.Message = rawString(body.Errors)
		}
	}
	if e.Message == "" {
		e.Message = strings.TrimSpace(string(raw))
	}
	if e.Message == "" {
		e.Message = http.StatusText(status)
	}
	return e
}

func rawString(r json.RawMessage) string {
	if len(r) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(r, &s) == nil {
		return s
	}
	return string(r)
}
