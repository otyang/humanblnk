package reconcile_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/otyang/humanblnk/blnk/blnkfake"
	"github.com/otyang/humanblnk/internal/types"
	"github.com/otyang/humanblnk/money"
)

func TestRunLinesSendsMajorUnits(t *testing.T) {
	s, f := newService(t)
	f.On("POST /reconciliation/start-instant", 200, `{"reconciliation_id":"rec_1"}`)
	id, err := s.RunLines(context.Background(), []types.StatementLine{{
		ID: "ps_1", Amount: money.MustParse("1500.25", blnkfake.USD), Reference: "deposit:paystack:ps_1",
		Date: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC), Source: "paystack",
	}}, types.Run{Rules: []types.RuleID{"rule_1"}})
	if err != nil || id != "rec_1" {
		t.Fatalf("%q %v", id, err)
	}
	got := f.Sent("POST /reconciliation/start-instant")[0].Body
	line := got["external_transactions"].([]any)[0].(map[string]any)
	if line["amount"] != float64(1500.25) || line["currency"] != "USD" || line["date"] != "2026-10-20T09:00:00Z" || got["strategy"] != "one_to_one" {
		t.Fatalf("sent %v", got)
	}
}

func TestRunRefusals(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	if _, err := s.RunLines(ctx, nil, types.Run{Rules: []types.RuleID{"r"}}); !errors.Is(err, types.ErrInvalid) {
		t.Errorf("no lines: %v", err)
	}
	if _, err := s.Run(ctx, "up_1", types.Run{}); !errors.Is(err, types.ErrInvalid) {
		t.Errorf("no rules: %v", err)
	}
}

func TestRulesAndUpload(t *testing.T) {
	s, f := newService(t)
	ctx := context.Background()
	f.On("POST /reconciliation/matching-rules", 201, `{"rule_id":"rule_1","name":"amount and ref","criteria":[{"field":"amount","operator":"equals","allowable_drift":0.01}]}`)
	r, err := s.AddRule(ctx, types.MatchRule{Name: "amount and ref", Criteria: []types.Criterion{{Field: types.MatchAmount, Op: types.MatchEquals, Drift: 0.01}}})
	if err != nil || r.ID != "rule_1" || r.Criteria[0].Drift != 0.01 {
		t.Fatalf("%+v %v", r, err)
	}
	f.On("POST /reconciliation/upload", 200, `{"upload_id":"up_1","record_count":2,"source":"paystack"}`)
	id, n, err := s.Upload(ctx, "paystack", "june.csv", strings.NewReader("id,amount\n1,10\n2,20\n"))
	if err != nil || id != "up_1" || n != 2 {
		t.Fatalf("%q %d %v", id, n, err)
	}
	f.On("GET /reconciliation/rec_1", 200, `{"reconciliation_id":"rec_1","upload_id":"up_1","status":"completed","matched_transactions":2}`)
	if res, err := s.Result(ctx, "rec_1"); err != nil || res.Matched != 2 || res.Status != "completed" {
		t.Fatalf("%+v %v", res, err)
	}
}
