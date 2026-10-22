package blnk

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Stable error codes from https://docs.blnkfinance.com/operating/error-codes,
// as of Blnk Core 0.15.6 (this module needs Core 0.15.4 or later). Branch on
// these, never on message text.
const (
	CodeMalformedRequest = "GEN_MALFORMED_REQUEST"
	CodeValidation       = "GEN_VALIDATION_ERROR"
	CodeBadRequest       = "GEN_BAD_REQUEST"
	CodeNotFound         = "GEN_NOT_FOUND"
	CodeConflict         = "GEN_CONFLICT"
	CodeResourceLocked   = "GEN_RESOURCE_LOCKED"
	CodeRateLimited      = "GEN_RATE_LIMITED"
	CodeInternal         = "GEN_INTERNAL"
	CodeMissingParameter = "GEN_MISSING_PARAMETER"
	CodePayloadTooLarge  = "GEN_PAYLOAD_TOO_LARGE"

	CodeMissingAPIKey     = "AUTH_MISSING_API_KEY"
	CodeInvalidAPIKey     = "AUTH_INVALID_API_KEY"
	CodeInsufficientScope = "AUTH_INSUFFICIENT_PERMISSIONS"

	CodeLedgerNotFound   = "LGR_NOT_FOUND"
	CodeLedgerDuplicate  = "LGR_DUPLICATE"
	CodeBalanceNotFound  = "BAL_NOT_FOUND"
	CodeBalanceInvalid   = "BAL_VALIDATION_ERROR"
	CodeHistoryNotFound  = "BAL_HISTORY_NOT_FOUND"
	CodeInvalidTimestamp = "BAL_INVALID_TIMESTAMP"
	CodeMonitorNotFound  = "BAL_MONITOR_NOT_FOUND"
	CodeIdentityNotFound = "IDT_NOT_FOUND"
	CodeIdentityInvalid  = "IDT_VALIDATION_ERROR"

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
	CodePrecisionNotInteger    = "TXN_PRECISION_NOT_INTEGER"
	CodeQueueBackpressure      = "QUEUE_BACKPRESSURE"
	CodeMetadataEntityNotFound = "META_ENTITY_NOT_FOUND"
	CodeMetadataInvalidID      = "META_INVALID_ENTITY_ID"
	CodeMetadataUnsupported    = "META_UNSUPPORTED_ENTITY"

	CodeExpiredAPIKey         = "AUTH_EXPIRED_API_KEY"
	CodeInvalidBearerToken    = "AUTH_INVALID_BEARER_TOKEN"
	CodeMissingPrincipal      = "AUTH_MISSING_PRINCIPAL"
	CodeUnknownResource       = "AUTH_UNKNOWN_RESOURCE"
	CodeMasterKeyRequired     = "AUTH_MASTER_KEY_REQUIRED"
	CodeCrossOwnerAccess      = "AUTH_CROSS_OWNER_ACCESS"
	CodeScopeEscalation       = "AUTH_SCOPE_ESCALATION"
	CodeMetricsDisabled       = "AUTH_METRICS_DISABLED"
	CodeMetricsTokenRequired  = "AUTH_METRICS_TOKEN_REQUIRED"
	CodeAPIKeyNotFound        = "APIKEY_NOT_FOUND"
	CodeAPIKeyInvalid         = "APIKEY_INVALID"
	CodeAPIKeyOwnerRequired   = "APIKEY_OWNER_REQUIRED"
	CodeFieldAlreadyTokenized = "IDT_FIELD_ALREADY_TOKENIZED"
	CodeFieldNotFound         = "IDT_FIELD_NOT_FOUND"
	CodeFieldNotTokenizable   = "IDT_FIELD_NOT_TOKENIZABLE"
	CodeFieldNotTokenized     = "IDT_FIELD_NOT_TOKENIZED"
	CodeTokenizationDisabled  = "IDT_TOKENIZATION_DISABLED"
	CodeAccountNotFound       = "ACC_NOT_FOUND"
	CodeAccountDuplicate      = "ACC_DUPLICATE"
	CodeAccountGeneration     = "ACC_GENERATION_FAILED"
	CodeHookNotFound          = "HOOK_NOT_FOUND"
	CodeHookInvalid           = "HOOK_INVALID"
	CodeHookOperationFailed   = "HOOK_OPERATION_FAILED"
	CodeSearchFailed          = "SRCH_FAILED"
	CodeSearchQueryInvalid    = "SRCH_QUERY_INVALID"
	CodeReindexInProgress     = "SRCH_REINDEX_IN_PROGRESS"
	CodeReindexNotStarted     = "SRCH_REINDEX_NOT_STARTED"
	CodeBackupFailed          = "ADMIN_BACKUP_FAILED"

	CodeReconNotFound             = "RECON_NOT_FOUND"
	CodeReconRuleNotFound         = "RECON_RULE_NOT_FOUND"
	CodeReconRuleInvalid          = "RECON_RULE_INVALID"
	CodeReconMatchingRulesMissing = "RECON_MATCHING_RULES_REQUIRED"
	CodeReconExternalTxnsMissing  = "RECON_EXTERNAL_TXNS_REQUIRED"
	CodeReconStartFailed          = "RECON_START_FAILED"
	CodeReconUploadFailed         = "RECON_UPLOAD_FAILED"
	CodeReconUploadProcessing     = "RECON_UPLOAD_PROCESSING_FAILED"
	CodeReconUploadURLInvalid     = "RECON_UPLOAD_URL_INVALID"
	CodeReconUploadHostNotAllowed = "RECON_UPLOAD_HOST_NOT_ALLOWED"
)

// APIError is a non-2xx response from Blnk.
type APIError struct {
	Status  int            // HTTP status
	Code    string         // error_detail.code
	Message string         // display text only
	Details map[string]any // error_detail.details when it is an object (a list, e.g. filter parse errors, is only in Body)
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

// IsNotFound reports whether err is Blnk saying something doesn't exist: a
// 404 carrying one of its error codes (BAL_NOT_FOUND, TXN_NOT_FOUND, …). A
// bare 404 is a path Blnk doesn't serve, such as a wrong base URL or a proxy,
// and says nothing about whether the thing exists.
func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound && ae.Code != ""
}

func parseAPIError(status int, raw []byte) *APIError {
	e := &APIError{Status: status, Body: string(raw)}
	var body struct {
		Error       json.RawMessage `json:"error"`
		Errors      json.RawMessage `json:"errors"`
		ErrorDetail *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			// Raw: Core sends an object, a list or a string here, and a
			// mismatch must not lose the code.
			Details json.RawMessage `json:"details"`
		} `json:"error_detail"`
	}
	if json.Unmarshal(raw, &body) == nil {
		if body.ErrorDetail != nil {
			e.Code = body.ErrorDetail.Code
			e.Message = body.ErrorDetail.Message
			_ = json.Unmarshal(body.ErrorDetail.Details, &e.Details)
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
