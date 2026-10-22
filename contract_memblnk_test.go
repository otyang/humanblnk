//go:build !integration

package humanblnk_test

import (
	"testing"

	"github.com/otyang/humanblnk/internal/memblnk"
)

// backend is an in-memory Blnk of the test's own.
func backend(t *testing.T) string {
	t.Helper()
	s := memblnk.New()
	t.Cleanup(s.Close)
	return s.URL
}
