package main

import (
	"testing"

	"github.com/otyang/humanblnk/ledgertest"
)

func TestExampleRunsAgainstEmulator(t *testing.T) {
	core := ledgertest.NewServer()
	defer core.Close()
	t.Setenv("BLNK_URL", core.URL())
	t.Setenv("BLNK_LEDGER_ID", "")
	main()
}
