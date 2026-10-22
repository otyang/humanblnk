package types_test

import (
	"testing"

	"github.com/otyang/humanblnk/internal/types"
)

func TestSystemAccounts(t *testing.T) {
	for got, want := range map[types.AccountRef]string{
		types.World:                       "@World",
		types.FeeIncome:                   "@FeeIncome",
		types.SystemAccount("Rewards"):    "@Rewards",
		types.ClearingAccount("paystack"): "@PaystackClearing",
	} {
		if string(got) != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
	if got := types.Reference("payout", "po_1", "2"); got != "payout:po_1:2" {
		t.Errorf("reference %s", got)
	}
}
