package watches_test

import (
	"context"
	"errors"
	"testing"

	"github.com/otyang/humanblnk/blnk/blnkfake"
	"github.com/otyang/humanblnk/ledger/types"
	"github.com/otyang/humanblnk/money"
)

func TestCreateSendsTheThresholdInMinorUnits(t *testing.T) {
	s, f := newService(t)
	f.On("POST /balance-monitors", 201, `{"monitor_id":"mon_1","balance_id":"bln_usd","condition":{"field":"balance","operator":"<","value":100000,"precision":1,"precise_value":100000}}`)
	f.On("PUT /balance-monitors/mon_1", 200, `{"message":"ok"}`)
	w, err := s.Create(context.Background(), types.Watch{
		Account: "bln_usd", Field: types.WatchBalance, Op: types.WatchBelow,
		Threshold: money.MustParse("1000", blnkfake.USD), Description: "low balance",
	})
	if err != nil || w.ID != "mon_1" || w.Threshold.String() != "1000.00" || w.Description != "low balance" {
		t.Fatalf("%+v %v", w, err)
	}
	cond := f.Sent("POST /balance-monitors")[0].Body["condition"].(map[string]any)
	if cond["value"] != float64(100000) || cond["precision"] != float64(1) || cond["operator"] != "<" {
		t.Fatalf("condition %v", cond)
	}
	if put := f.Sent("PUT /balance-monitors/mon_1"); len(put) != 1 || put[0].Body["description"] != "low balance" {
		t.Fatalf("description not set: %v", put)
	}
}

func TestCreateRefusals(t *testing.T) {
	s, f := newService(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, types.Watch{Account: "bln_usd", Field: types.WatchBalance, Op: types.WatchBelow, Threshold: money.MustParse("1", blnkfake.NGN)}); !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Errorf("currency: %v", err)
	}
	if _, err := s.Create(ctx, types.Watch{Account: types.World, Field: types.WatchBalance, Op: types.WatchBelow, Threshold: money.MustParse("1", blnkfake.USD)}); !errors.Is(err, types.ErrInvalid) {
		t.Errorf("system account: %v", err)
	}
	if n := len(f.Sent("POST /balance-monitors")); n != 0 {
		t.Fatalf("%d monitors reached Blnk", n)
	}
}

func TestChangeCreatesBeforeDeleting(t *testing.T) {
	s, f := newService(t)
	f.On("POST /balance-monitors", 201, `{"monitor_id":"mon_2","balance_id":"bln_usd","condition":{"field":"balance","operator":"<","precise_value":50000}}`)
	f.On("DELETE /balance-monitors/mon_1", 200, `{}`)
	w, err := s.Change(context.Background(), "mon_1", types.Watch{
		Account: "bln_usd", Field: types.WatchBalance, Op: types.WatchBelow, Threshold: money.MustParse("500", blnkfake.USD),
	})
	if err != nil || w.ID != "mon_2" || len(f.Sent("DELETE /balance-monitors/mon_1")) != 1 {
		t.Fatalf("%+v %v", w, err)
	}
}

// Blnk reads a threshold as a float: beyond 2^53 in the smallest unit it
// can't store it exactly, so Create refuses it, and checks what Blnk stored.
func TestCreateThresholdMustBeExact(t *testing.T) {
	s, f := newService(t)
	ctx := context.Background()
	huge := types.Watch{Account: "bln_usd", Field: types.WatchBalance, Op: types.WatchBelow, Threshold: money.MustParse("100000000000000", blnkfake.USD)}
	if _, err := s.Create(ctx, huge); !errors.Is(err, types.ErrInvalid) || len(f.Sent("POST /balance-monitors")) != 0 {
		t.Fatalf("huge: %v", err)
	}
	f.On("POST /balance-monitors", 201, `{"monitor_id":"mon_1","balance_id":"bln_usd","condition":{"field":"balance","operator":"<","value":100000,"precision":1,"precise_value":99999}}`)
	if _, err := s.Create(ctx, types.Watch{Account: "bln_usd", Field: types.WatchBalance, Op: types.WatchBelow, Threshold: money.MustParse("1000", blnkfake.USD)}); err == nil {
		t.Fatal("a threshold stored wrong must be an error")
	}
}
