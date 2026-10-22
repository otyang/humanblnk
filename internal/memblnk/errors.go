package memblnk

import (
	"fmt"
	"net/http"

	"github.com/otyang/humanblnk/blnk"
)

// errorBody is Blnk's error response.
type errorBody struct {
	Error       string      `json:"error"`
	ErrorDetail errorDetail `json:"error_detail"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func apiError(code, format string, args ...any) errorBody {
	msg := fmt.Sprintf(format, args...)
	return errorBody{Error: msg, ErrorDetail: errorDetail{Code: code, Message: msg}}
}

// failure is a refusal: its status and body.
type failure struct {
	status int
	body   errorBody
}

func (f *failure) Error() string { return f.body.ErrorDetail.Message }

func fail(status int, code, format string, args ...any) *failure {
	return &failure{status: status, body: apiError(code, format, args...)}
}

// The refusals memblnk makes. humanblnk branches on the code (and on 404),
// never on the status; none of these is a status the client retries.
func notFound(code, what, id string) *failure {
	return fail(http.StatusNotFound, code, "%s %s not found", what, id)
}

func invalid(code, format string, args ...any) *failure {
	return fail(http.StatusBadRequest, code, format, args...)
}

func conflict(code, format string, args ...any) *failure {
	return fail(http.StatusConflict, code, format, args...)
}

func insufficientFunds(balance string) *failure {
	return fail(http.StatusBadRequest, blnk.CodeInsufficientFunds, "failed to apply transaction to balances: insufficient funds in source balance %s", balance)
}

// respond turns a result and a refusal into what a handler returns.
func respond(status int, v any, err *failure) (int, any) {
	if err != nil {
		return err.status, err.body
	}
	return status, v
}
