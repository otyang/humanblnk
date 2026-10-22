package accounts_test

import (
	"testing"

	"github.com/otyang/humanblnk/blnk/blnkfake"
	"github.com/otyang/humanblnk/internal/accounts"
	"github.com/otyang/humanblnk/internal/core"
)

// newService is a accounts.Service on a fake Blnk (see blnkfake.New).
func newService(t *testing.T) (*accounts.Service, *blnkfake.Fake) {
	t.Helper()
	c, f := blnkfake.New(t)
	return accounts.New(core.New(c, "ldg_wallets")), f
}
