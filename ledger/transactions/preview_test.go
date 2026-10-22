package transactions_test

import (
	"context"
	"errors"
	"testing"

	"github.com/otyang/humanblnk/clients/blnk/blnkfake"
	"github.com/otyang/humanblnk/ledger/types"
	"github.com/otyang/humanblnk/money"
)

func TestPreviewRecord(t *testing.T) {
	s, f := newService(t)
	f.On("POST /transactions", 200, `{"dry_run":true,"would_apply":false,"rejection":{"code":"TXN_INSUFFICIENT_FUNDS","message":"insufficient funds"},
		"balances":[{"balance_id":"bln_usd","currency":"USD","current_balance":"500","resulting_balance":"-500","current_available":"500","resulting_available":"-500"}]}`)
	p, err := s.Preview(context.Background(), types.Transaction{
		Ref: "r", From: types.Single("bln_usd", money.MustParse("10", blnkfake.USD)), To: types.Single("bln_usd2", money.MustParse("10", blnkfake.USD)), Description: "d",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.WouldApply || !errors.Is(p.Rejection, types.ErrInsufficientFunds) || p.Changes[0].After.String() != "-5.00" {
		t.Fatalf("%+v", p)
	}
	if f.Sent("POST /transactions")[0].Body["dry_run"] != true {
		t.Fatal("dry_run not sent")
	}
}
