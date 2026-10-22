package ledger_test

import (
	"testing"

	"github.com/otyang/humanblnk/blnk/blnkfake"
	"github.com/otyang/humanblnk/internal/core"
	"github.com/otyang/humanblnk/internal/ledger"
)

// newService is a ledger.Service on a fake Blnk (see blnkfake.New).
func newService(t *testing.T) (*ledger.Service, *blnkfake.Fake) {
	t.Helper()
	c, f := blnkfake.New(t)
	return ledger.New(core.New(c, "ldg_wallets")), f
}
