package reconcile_test

import (
	"testing"

	"github.com/otyang/humanblnk/clients/blnk/blnkfake"
	"github.com/otyang/humanblnk/ledger/internal/core"
	"github.com/otyang/humanblnk/ledger/reconcile"
)

// newService is a reconcile.Service on a fake Blnk (see blnkfake.New).
func newService(t *testing.T) (*reconcile.Service, *blnkfake.Fake) {
	t.Helper()
	c, f := blnkfake.New(t)
	return reconcile.New(core.New(c, "ldg_wallets")), f
}
