package books_test

import (
	"testing"

	"github.com/otyang/humanblnk/blnk/blnkfake"
	"github.com/otyang/humanblnk/internal/books"
	"github.com/otyang/humanblnk/internal/core"
)

// newService is a books.Service on a fake Blnk (see blnkfake.New).
func newService(t *testing.T) (*books.Service, *blnkfake.Fake) {
	t.Helper()
	c, f := blnkfake.New(t)
	return books.New(core.New(c, "ldg_wallets")), f
}
