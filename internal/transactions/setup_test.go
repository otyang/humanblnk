package transactions_test

import (
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
