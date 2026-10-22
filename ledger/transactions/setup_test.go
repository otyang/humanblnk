package transactions_test

import (
	"testing"

	"github.com/otyang/humanblnk/clients/blnk/blnkfake"
	"github.com/otyang/humanblnk/ledger/internal/core"
	"github.com/otyang/humanblnk/ledger/transactions"
)

// newService is a transactions.Service on a fake Blnk (see blnkfake.New).
func newService(t *testing.T) (*transactions.Service, *blnkfake.Fake) {
	t.Helper()
	c, f := blnkfake.New(t)
	return transactions.New(core.New(c, "ldg_wallets")), f
}
