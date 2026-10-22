package identities_test

import (
	"context"
	"errors"
	"testing"

	"github.com/otyang/humanblnk/blnk/blnkfake"
	"github.com/otyang/humanblnk/internal/core"
	"github.com/otyang/humanblnk/internal/identities"
	"github.com/otyang/humanblnk/internal/types"
)

func TestCreateSendsNoPersonalData(t *testing.T) {
	c, f := blnkfake.New(t)
	s := identities.New(core.New(c, "ldg_wallets"))
	f.On("POST /identities", 201, `{"identity_id":"idt_new"}`)
	id, err := s.Create(context.Background(), types.Meta{"customer_id": "cus_1"})
	if err != nil || id != "idt_new" {
		t.Fatalf("%q %v", id, err)
	}
	body := f.Sent("POST /identities")[0].Body
	if len(body) != 1 || body["meta_data"].(map[string]any)["customer_id"] != "cus_1" {
		t.Fatalf("sent %v, want only meta_data", body)
	}
}

func TestGetAndUpdateMeta(t *testing.T) {
	c, f := blnkfake.New(t)
	s := identities.New(core.New(c, "ldg_wallets"))
	f.On("GET /identities/idt_ada", 200, `{"identity_id":"idt_ada","meta_data":{"customer_id":"cus_1"},"created_at":"2026-10-22T09:00:00Z"}`)
	got, err := s.Get(context.Background(), "idt_ada")
	if err != nil || got.ID != "idt_ada" || got.Meta["customer_id"] != "cus_1" || got.CreatedAt.IsZero() {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.Get(context.Background(), "idt_missing"); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}

	f.On("POST /idt_ada/metadata", 200, `{"meta_data":{"customer_id":"cus_1","tier":"gold"}}`)
	md, err := s.UpdateMeta(context.Background(), "idt_ada", types.Meta{"tier": "gold"})
	if err != nil || md["customer_id"] != "cus_1" || md["tier"] != "gold" {
		t.Fatalf("%v %v", md, err)
	}
	if sent := f.Sent("POST /idt_ada/metadata")[0].Body["meta_data"].(map[string]any); len(sent) != 1 {
		t.Fatalf("sent %v, want only the new key", sent)
	}
	if _, err := s.UpdateMeta(context.Background(), "idt_ada", nil); !errors.Is(err, types.ErrInvalid) {
		t.Fatalf("empty: %v", err)
	}
}

func TestFindByCustomer(t *testing.T) {
	c, f := blnkfake.New(t)
	s := identities.New(core.New(c, "ldg_wallets"))
	f.On("POST /identities/filter", 200, `[{"identity_id":"idt_first","meta_data":{"customer_id":"cus_1"}}]`)
	got, err := s.FindByCustomer(context.Background(), "cus_1")
	if err != nil || got.ID != "idt_first" {
		t.Fatalf("%+v %v", got, err)
	}
	body := f.Sent("POST /identities/filter")[0].Body
	filter := body["filters"].([]any)[0].(map[string]any)
	if filter["field"] != "meta_data.customer_id" || filter["value"] != "cus_1" || body["sort_order"] != "asc" {
		t.Fatalf("filtered with %v", body)
	}
	f.On("POST /identities/filter", 200, `[]`)
	if _, err := s.FindByCustomer(context.Background(), "cus_2"); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("none: %v", err)
	}
}
