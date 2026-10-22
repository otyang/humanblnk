package events_test

import (
	"errors"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger/events"
)

func TestVerifyWebhook(t *testing.T) {
	body := `{"event":"transaction.applied","data":{"transaction_id":"txn_1","status":"APPLIED","currency":"USD","precise_amount":2500,"precision":100,"source":"@World","destination":"bln_usd"}}`
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	r := httptest.NewRequest("POST", "/hooks/blnk", strings.NewReader(body))
	r.Header.Set(blnk.HeaderTimestamp, ts)
	r.Header.Set(blnk.HeaderSignature, blnk.Sign("secret", ts, []byte(body)))
	ev, err := events.VerifyWebhook("secret", r, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != "transaction.applied" || ev.Entry == nil || ev.Entry.Amount.String() != "25.00" {
		t.Fatalf("%+v", ev)
	}

	forged := httptest.NewRequest("POST", "/hooks/blnk", strings.NewReader(body))
	forged.Header.Set(blnk.HeaderTimestamp, ts)
	forged.Header.Set(blnk.HeaderSignature, blnk.Sign("guess", ts, []byte(body)))
	if _, err := events.VerifyWebhook("secret", forged, 5*time.Minute); !errors.Is(err, blnk.ErrBadSignature) {
		t.Fatalf("forged: %v", err)
	}
}

func TestWatchEvent(t *testing.T) {
	body := `{"event":"balance.monitor","data":{"monitor_id":"mon_1","balance_id":"bln_usd","condition":{"field":"balance","operator":"<"}}}`
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	r := httptest.NewRequest("POST", "/hooks/blnk", strings.NewReader(body))
	r.Header.Set(blnk.HeaderTimestamp, ts)
	r.Header.Set(blnk.HeaderSignature, blnk.Sign("secret", ts, []byte(body)))
	ev, err := events.VerifyWebhook("secret", r, time.Minute)
	if err != nil || ev.Watch != "mon_1" || ev.Account != "bln_usd" || ev.Entry != nil {
		t.Fatalf("%+v %v", ev, err)
	}
}
