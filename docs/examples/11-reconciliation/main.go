// Chapter 11: check a provider's settlement report against the books.
package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/docs/examples/demo"
	"github.com/otyang/humanblnk/money"
)

func main() {
	ctx, hb := demo.Setup()
	ada := demo.Customer(ctx, hb, "ada", demo.NGN)

	// Two deposits you recorded from Paystack webhooks.
	refs := []string{humanblnk.Reference("deposit", "paystack", "ps_a_"+demo.Run), humanblnk.Reference("deposit", "paystack", "ps_b_"+demo.Run)}
	for i, amount := range []string{"10000", "2500"} {
		m := money.MustParse(amount, demo.NGN)
		if _, err := hb.Transactions.Record(ctx, humanblnk.Transaction{
			Ref: refs[i], From: humanblnk.Single(humanblnk.ClearingAccount("paystack"), m), To: humanblnk.Single(humanblnk.AccountRef(ada.ID), m),
			Description: "Bank transfer",
		}); err != nil {
			log.Fatal(err)
		}
	}

	// 1. What counts as a match. Make the rule once and reuse it.
	// snippet:rule
	rule, err := hb.Reconciliation.AddRule(ctx, humanblnk.MatchRule{
		Name: "paystack: amount and reference " + demo.Run,
		Criteria: []humanblnk.Criterion{
			{Field: humanblnk.MatchAmount, Op: humanblnk.MatchEquals},
			{Field: humanblnk.MatchReference, Op: humanblnk.MatchEquals},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	// end-snippet

	// 2. Paystack's report: the two deposits, and one you never recorded.
	// Line ids are the provider's, and must be unique across every statement.
	// snippet:lines
	report := []humanblnk.StatementLine{
		{ID: "stl_1_" + demo.Run, Amount: money.MustParse("10000", demo.NGN), Reference: refs[0], Date: time.Now(), Source: "paystack"},
		{ID: "stl_2_" + demo.Run, Amount: money.MustParse("2500", demo.NGN), Reference: refs[1], Date: time.Now(), Source: "paystack"},
		{ID: "stl_3_" + demo.Run, Amount: money.MustParse("7000", demo.NGN), Reference: "deposit:paystack:ps_missing_" + demo.Run, Date: time.Now(), Source: "paystack"},
	}
	run, err := hb.Reconciliation.RunLines(ctx, report, humanblnk.Run{Rules: []humanblnk.RuleID{rule.ID}})
	if err != nil {
		log.Fatal(err)
	}
	// end-snippet

	// 3. It runs in the background: poll for the result.
	// snippet:poll
	var res humanblnk.RunResult
	for range 50 {
		if res, err = hb.Reconciliation.Result(ctx, run); err == nil && (res.Status == "completed" || res.Status == "failed") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	fmt.Printf("run %s: %s, %d matched, %d unmatched\n", res.ID, res.Status, res.Matched, res.Unmatched)
	// end-snippet

	// A bigger report comes as a file: CSV or JSON with the columns id,
	// amount, currency, reference, description and date, in major units.
	// snippet:upload
	csv := "id,amount,currency,reference,description,date\n" +
		"stl_file_1_" + demo.Run + ",10000,NGN," + refs[0] + ",Bank transfer," + time.Now().UTC().Format(time.RFC3339) + "\n"
	statement, lines, err := hb.Reconciliation.Upload(ctx, "paystack", "settlement.csv", strings.NewReader(csv))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("uploaded", statement, "with", lines, "line(s); run it with Reconciliation.Run")
	// end-snippet

	if err := hb.Reconciliation.RemoveRule(ctx, rule.ID); err != nil {
		log.Fatal(err)
	}
}
