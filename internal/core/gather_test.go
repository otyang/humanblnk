package core

import (
	"testing"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/internal/types"
)

// Blnk lists a split's postings in no set order; Gather puts them back in
// the order they were asked for.
func TestGatherOrdersPostingsByNumber(t *testing.T) {
	leg := func(ref, to string, minor int64) blnk.Transaction {
		return blnk.Transaction{
			TransactionID: "txn_" + ref, Reference: ref, Status: blnk.StatusApplied, Currency: "USD", Precision: "100",
			PreciseAmount: *blnk.NewBigInt(minor), Source: "bln_ada", Destination: to,
		}
	}
	for _, listed := range [][]blnk.Transaction{
		{leg("pay-1", "bln_bola", 10000), leg("pay-2", "bln_fee", 150), leg("pay-10", "bln_cara", 5)},
		{leg("pay-10", "bln_cara", 5), leg("pay-2", "bln_fee", 150), leg("pay-1", "bln_bola", 10000)},
	} {
		e, err := Gather("txn_split", listed)
		if err != nil {
			t.Fatal(err)
		}
		want := []types.AccountRef{"bln_bola", "bln_fee", "bln_cara"}
		if e.Ref != "pay" || len(e.To) != 3 || e.Amount.String() != "101.55" {
			t.Fatalf("%+v", e)
		}
		for i, p := range e.To {
			if p.Account != want[i] {
				t.Fatalf("posting %d is %s, want %s (listed %s first)", i, p.Account, want[i], listed[0].Reference)
			}
		}
		if listed[0].Reference == "pay-10" && listed[2].Reference != "pay-1" {
			t.Fatal("Gather reordered the caller's slice")
		}
	}
}
