package transactions_test

import (
	"context"
	"testing"
	"time"

	"github.com/otyang/humanblnk/ledger/types"
)

func TestGetEntryGathersASplit(t *testing.T) {
	s, f := newService(t)
	f.On("POST /transactions/filter", 200, `{"data":[
		{"transaction_id":"c1","reference":"transfer:t1-1","parent_transaction":"txn_p","status":"APPLIED","currency":"USD","precise_amount":10000,"precision":100,"source":"bln_usd","destination":"bln_usd2"},
		{"transaction_id":"c2","reference":"transfer:t1-2","parent_transaction":"txn_p","status":"APPLIED","currency":"USD","precise_amount":150,"precision":100,"source":"bln_usd","destination":"@FeeIncome"}],"total_count":2}`)
	e, err := s.Get(context.Background(), "txn_p")
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "txn_p" || e.Ref != "transfer:t1" || e.Amount.String() != "101.50" || len(e.From) != 1 || e.From[0].Amount.String() != "101.50" ||
		len(e.To) != 2 || e.To[1].Account != types.FeeIncome {
		t.Fatalf("entry %+v", e)
	}
	filter := f.Sent("POST /transactions/filter")[0].Body["filters"].([]any)[0].(map[string]any)
	if filter["field"] != "parent_transaction" || filter["value"] != "txn_p" {
		t.Fatalf("filter %v", filter)
	}
}

func TestGetEntryByReferenceFindsASplit(t *testing.T) {
	s, f := newService(t)
	f.On("GET /transactions/reference/transfer:t1-1", 200, `{"transaction_id":"c1","parent_transaction":"txn_p"}`)
	f.On("POST /transactions/filter", 200, `[{"transaction_id":"c1","reference":"transfer:t1-1","status":"APPLIED","currency":"USD","precise_amount":100,"precision":100,"source":"bln_usd","destination":"bln_usd2"}]`)
	e, err := s.GetByReference(context.Background(), "transfer:t1")
	if err != nil || e.ID != "txn_p" {
		t.Fatalf("%+v %v", e, err)
	}
}

func TestListEntriesFilters(t *testing.T) {
	s, f := newService(t)
	f.On("POST /transactions/filter", 200, `{"data":[],"total_count":0}`)
	_, _, err := s.List(context.Background(), types.EntryQuery{
		From: "bln_usd", Status: types.StatusApplied, Since: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := f.Sent("POST /transactions/filter")[0].Body
	fs := body["filters"].([]any)
	if len(fs) != 3 || fs[0].(map[string]any)["field"] != "source" || fs[2].(map[string]any)["value"] != "2026-10-01T00:00:00Z" ||
		body["limit"] != float64(50) || body["include_count"] != true {
		t.Fatalf("sent %v", body)
	}
}
