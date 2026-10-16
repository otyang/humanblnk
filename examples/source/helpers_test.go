// Package source holds the code behind every page in examples/. Each sample
// sits between "// sample: <page>/<name>" and "// end", runs against the
// in-memory Blnk Core, and its printed output is what the page shows.
// scripts/genexamples.py builds the pages from these files.
package source_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/ledgertest"
)

// ctx is the context every sample uses.
var ctx = context.Background()

// curT is the running test, so check can fail it.
var curT *testing.T

// check stands for your own error handling in the samples.
func check(err error) {
	if err != nil {
		curT.Helper()
		curT.Fatal(err)
	}
}

// setup gives a test its own in-memory Blnk Core, at a fixed time so outputs
// are the same on every run.
func setup(t *testing.T, cfg ledger.Config) (*ledger.Service, *ledgertest.Server, ledger.Wallet, ledger.Ops) {
	t.Helper()
	curT = t
	svc, core := ledgertest.New(t, cfg)
	core.SetNow(time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	return svc, core, svc.Wallet(), svc.Ops()
}

// customer opens a customer with one account per currency.
func customer(w ledger.Wallet, externalID, first string, currencies ...string) *ledger.Customer {
	c, err := w.OpenCustomer(ctx, ledger.CustomerInput{FirstName: first, LastName: "Test",
		Currencies: currencies, ExternalID: externalID, MetaData: map[string]any{"tier": "tier2"}})
	check(err)
	return c
}

// fund deposits money with no fee, as if it arrived through Paystack.
func fund(w ledger.Wallet, id ledger.AccountID, amount, currency, ref string) {
	_, err := w.Deposit(ctx, ledger.DepositInput{AccountID: id, Amount: ledger.Amount(amount, currency),
		Fee: ledger.NoFee, Reference: ref, Provider: "paystack"})
	check(err)
}

// ---- output capture

var (
	outputs   = map[string]string{}
	capturing string
	captured  *os.File
	realOut   *os.File
)

// begin starts capturing what the sample prints.
func begin(name string) {
	f, err := os.CreateTemp("", "sample")
	check(err)
	capturing, captured, realOut = name, f, os.Stdout
	os.Stdout = f
	curT.Cleanup(func() {
		if capturing != "" {
			os.Stdout = realOut
			capturing = ""
		}
	})
}

// end stops capturing and keeps the output for the page.
func end() {
	os.Stdout = realOut
	_, _ = captured.Seek(0, io.SeekStart)
	b, _ := io.ReadAll(captured)
	captured.Close()
	os.Remove(captured.Name())
	outputs[capturing] = string(b)
	capturing = ""
}

func TestMain(m *testing.M) {
	code := m.Run()
	if path := os.Getenv("EXAMPLES_OUT"); path != "" && code == 0 {
		b, _ := json.MarshalIndent(outputs, "", "  ")
		if err := os.WriteFile(path, b, 0o644); err != nil {
			code = 1
		}
	}
	os.Exit(code)
}
