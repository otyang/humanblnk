package transactions_test

import (
	"encoding/json"
	"maps"
	"testing"

	"github.com/otyang/humanblnk/blnk/blnkfake"
	"github.com/otyang/humanblnk/internal/core"
	"github.com/otyang/humanblnk/internal/transactions"
)

// newService is a transactions.Service on a fake Blnk (see blnkfake.New).
func newService(t *testing.T) (*transactions.Service, *blnkfake.Fake) {
	t.Helper()
	c, f := blnkfake.New(t)
	return transactions.New(core.New(c, "ldg_wallets")), f
}

// heldRecord answers, on a fake Blnk, for a transaction recorded as a hold
// under parent (a split's or batch's id), as Blnk keeps one: its legs
// INFLIGHT, each captured by an APPLIED child once parent is committed.
// Legs are Blnk transactions as JSON objects, without status or parent.
type heldRecord struct {
	parent    string
	legs      []map[string]any
	committed bool
}

func newHeldRecord(t *testing.T, f *blnkfake.Fake, parent string, legs ...string) *heldRecord {
	t.Helper()
	h := &heldRecord{parent: parent}
	for _, l := range legs {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatal(err)
		}
		m["status"], m["parent_transaction"], m["inflight"] = "INFLIGHT", parent, true
		h.legs = append(h.legs, m)
	}
	f.On("GET /transactions/"+parent, 404, blnkfake.APIError("TXN_NOT_FOUND"))
	f.OnFunc("PUT /transactions/inflight/"+parent, func(blnkfake.Call) (int, string) {
		if h.committed {
			return 400, blnkfake.APIError("TXN_ALREADY_COMMITTED")
		}
		h.committed = true
		return 200, `{}`
	})
	return h
}

// filter answers c if it asks for parent's legs or their captures.
func (h *heldRecord) filter(c blnkfake.Call) (int, string, bool) {
	fs := c.Body["filters"].([]any)
	first := fs[0].(map[string]any)
	if first["field"] != "parent_transaction" {
		return 0, "", false
	}
	status := ""
	if len(fs) > 1 {
		status, _ = fs[1].(map[string]any)["value"].(string)
	}
	var out []map[string]any
	switch {
	case first["value"] == h.parent && status != "APPLIED":
		out = h.legs
	default:
		for _, l := range h.legs {
			if first["value"] != l["transaction_id"] || !h.committed || (status != "" && status != "APPLIED") {
				continue
			}
			c := maps.Clone(l)
			c["transaction_id"], c["parent_transaction"], c["reference"] = l["transaction_id"].(string)+"_c", l["transaction_id"], "ref_"+l["transaction_id"].(string)
			c["status"], c["inflight"] = "APPLIED", false
			out = append(out, c)
		}
		if out == nil && !h.isLeg(first["value"]) {
			return 0, "", false
		}
	}
	b, _ := json.Marshal(out)
	if out == nil {
		b = []byte("[]")
	}
	return 200, string(b), true
}

func (h *heldRecord) isLeg(id any) bool {
	for _, l := range h.legs {
		if l["transaction_id"] == id {
			return true
		}
	}
	return false
}
