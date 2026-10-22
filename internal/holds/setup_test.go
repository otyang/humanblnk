package holds_test

import (
	"testing"

	"github.com/otyang/humanblnk/blnk/blnkfake"
	"github.com/otyang/humanblnk/internal/core"
	"github.com/otyang/humanblnk/internal/holds"
)

// newService is a holds.Service on a fake Blnk (see blnkfake.New) where no
// hold has been captured yet.
func newService(t *testing.T) (*holds.Service, *blnkfake.Fake) {
	t.Helper()
	c, f := blnkfake.New(t)
	f.On("POST /transactions/filter", 200, `[]`)
	return holds.New(core.New(c, "ldg_wallets")), f
}
