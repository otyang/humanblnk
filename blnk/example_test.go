package blnk_test

import (
	"fmt"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// Verify a webhook from Blnk: X-Blnk-Signature is HMAC-SHA256 of
// timestamp + "." + body with Blnk's server.secret_key.
func ExampleVerifySignature() {
	body := []byte(`{"event":"transaction.applied","data":{}}`)
	ts := "1792000000"
	sig := blnk.Sign("my-secret", ts, body) // what Blnk sends

	now := time.Unix(1792000060, 0)
	fmt.Println(blnk.VerifySignature("my-secret", sig, ts, body, now, 5*time.Minute))
	fmt.Println(blnk.VerifySignature("wrong", sig, ts, body, now, 5*time.Minute))
	// Output:
	// <nil>
	// blnk: webhook signature mismatch
}
