//go:build integration

package humanblnk_test

import (
	"os"
	"testing"
)

// backend is the real Blnk Core at $BLNK_URL.
func backend(t *testing.T) string {
	t.Helper()
	url := os.Getenv("BLNK_URL")
	if url == "" {
		t.Skip("BLNK_URL not set")
	}
	return url
}
