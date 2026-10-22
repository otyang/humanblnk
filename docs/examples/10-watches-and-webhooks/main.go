// Chapter 10: watch an account's balance, and receive Blnk's webhooks
// safely.
package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"time"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/docs/examples/demo"
	"github.com/otyang/humanblnk/money"
)

// secret is Blnk's server.secret_key, which signs its webhooks.
const secret = "change-me"

// webhook is the handler you mount at the URL Blnk sends webhooks to.
// snippet:handler
func webhook(w http.ResponseWriter, r *http.Request) {
	ev, err := humanblnk.VerifyWebhook(secret, r, 5*time.Minute)
	if err != nil {
		http.Error(w, "bad signature", http.StatusUnauthorized) // never act on its body
		fmt.Println("  rejected:", err)
		return
	}
	switch {
	case ev.Kind == "balance.monitor":
		fmt.Println("  watch", ev.Watch, "fired on", ev.Account, "- e.g. text the customer 'low balance'")
	case ev.Entry != nil: // transaction.applied, transaction.rejected, …
		fmt.Println("  ", ev.Kind, ev.Entry.Ref, ev.Entry.Amount)
	}
	w.WriteHeader(http.StatusOK)
}

// end-snippet

func main() {
	ctx, hb := demo.Setup()
	ada := demo.Customer(ctx, hb, "ada", demo.NGN)
	demo.Deposit(ctx, hb, ada, "5000")

	// 1. Tell Blnk to fire "balance.monitor" while Ada's balance is under ₦1,000.
	// snippet:watch
	low, err := hb.Watches.Create(ctx, humanblnk.Watch{
		Account: humanblnk.AccountRef(ada.ID), Field: humanblnk.WatchBalance, Op: humanblnk.WatchBelow,
		Threshold: money.MustParse("1000", demo.NGN), Description: "low balance alert",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("watch", low.ID, "on", low.Account, low.Field, low.Op, low.Threshold)
	// end-snippet

	// Move the line: Change makes the new watch before deleting the old one.
	// snippet:manage
	low, err = hb.Watches.Change(ctx, low.ID, humanblnk.Watch{
		Account: humanblnk.AccountRef(ada.ID), Field: humanblnk.WatchBalance, Op: humanblnk.WatchBelow,
		Threshold: money.MustParse("2000", demo.NGN), Description: "low balance alert",
	})
	if err != nil {
		log.Fatal(err)
	}
	watches, _ := hb.Watches.List(ctx, humanblnk.AccountRef(ada.ID))
	fmt.Println("watches on Ada:", len(watches), "| threshold now", watches[0].Threshold)
	// end-snippet

	// 2. Receive webhooks. Here a test server stands in for your service,
	// and the requests are signed the way Blnk signs them.
	srv := httptest.NewServer(http.HandlerFunc(webhook))
	defer srv.Close()
	send := func(sign string, body string) {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		req, _ := http.NewRequest(http.MethodPost, srv.URL, bytes.NewBufferString(body))
		req.Header.Set(blnk.HeaderTimestamp, ts)
		req.Header.Set(blnk.HeaderSignature, blnk.Sign(sign, ts, []byte(body)))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		res.Body.Close()
		fmt.Println("  ->", res.Status)
	}
	body := `{"event":"balance.monitor","data":{"monitor_id":"` + string(low.ID) + `","balance_id":"` + ada.ID + `"}}`
	fmt.Println("a genuine webhook:")
	send(secret, body)
	fmt.Println("a forged one:")
	send("not-the-secret", body)

	// 3. Stop watching.
	// snippet:delete
	if err := hb.Watches.Delete(ctx, low.ID); err != nil {
		log.Fatal(err)
	}
	fmt.Println("watch deleted")
	// end-snippet
}
