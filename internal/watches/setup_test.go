package watches_test

import (
	"testing"

	"github.com/otyang/humanblnk/blnk/blnkfake"
	"github.com/otyang/humanblnk/internal/core"
	"github.com/otyang/humanblnk/internal/watches"
)

// newService is a watches.Service on a fake Blnk (see blnkfake.New).
func newService(t *testing.T) (*watches.Service, *blnkfake.Fake) {
	t.Helper()
	c, f := blnkfake.New(t)
	return watches.New(core.New(c, "ldg_wallets")), f
}
