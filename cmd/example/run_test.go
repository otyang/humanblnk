package main

import "testing"

// With BLNK_URL unset the example runs against the in-memory emulator.
func TestExampleRunsWithoutSetup(t *testing.T) {
	t.Setenv("BLNK_URL", "")
	t.Setenv("BLNK_LEDGER_ID", "")
	main()
}
