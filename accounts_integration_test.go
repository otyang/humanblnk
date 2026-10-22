//go:build integration

package humanblnk_test

import (
	"sync"
	"testing"

	"github.com/otyang/humanblnk"
)

// Opening the same customer account many times at once, as retries and
// double-clicks do, gives every caller the same account.
func TestIntegrationOpenForCustomerRace(t *testing.T) {
	ctx, w := setup(t)
	owner, err := w.books.Identities.Create(ctx, humanblnk.Meta{"customer_id": "cus_" + w.run})
	if err != nil {
		t.Fatal(err)
	}
	found, err := w.books.Identities.FindByCustomer(ctx, "cus_"+w.run)
	if err != nil || found.ID != owner {
		t.Fatalf("FindByCustomer: %+v %v, want %s", found, err, owner)
	}
	if _, err := w.books.Identities.UpdateMeta(ctx, owner, humanblnk.Meta{"tier": "gold"}); err != nil {
		t.Fatal(err)
	}
	if got, err := w.books.Identities.Get(ctx, owner); err != nil || got.Meta["tier"] != "gold" || got.Meta["customer_id"] != "cus_"+w.run {
		t.Fatalf("after UpdateMeta: %+v %v", got, err)
	}
	want := humanblnk.CustomerAccount{Owner: owner, Currency: iusd}

	const n = 8
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := w.books.Accounts.OpenForCustomer(ctx, want)
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = a.ID
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("callers got different accounts: %v", ids)
		}
	}

	// Later opens return it too, and another currency is a second account.
	again, err := w.books.Accounts.OpenForCustomer(ctx, want)
	if err != nil || again.ID != ids[0] {
		t.Fatalf("reopen: %+v %v", again, err)
	}
	ngn, err := w.books.Accounts.OpenForCustomer(ctx, humanblnk.CustomerAccount{Owner: owner, Currency: ingn})
	if err != nil || ngn.ID == ids[0] {
		t.Fatalf("NGN: %+v %v", ngn, err)
	}
	all, _, err := w.books.Accounts.List(ctx, humanblnk.AccountQuery{Owner: owner})
	if err != nil {
		t.Fatal(err)
	}
	spares := 0
	for _, a := range all {
		if a.Meta["duplicate_of"] != nil {
			spares++
		}
	}
	// One process takes turns, so the race leaves no spares.
	if len(all) != 2 || spares != 0 {
		t.Fatalf("%d accounts, %d spares; want USD and NGN only", len(all), spares)
	}
}
