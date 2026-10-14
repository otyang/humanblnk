package blnk

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Webhook signing headers. Blnk signs global webhooks (and transaction hooks)
// when server.secret_key is set: X-Blnk-Signature is the lowercase hex
// HMAC-SHA256 of timestamp + "." + raw body, keyed with the secret, and
// X-Blnk-Timestamp is the unix time in seconds.
const (
	HeaderSignature = "X-Blnk-Signature"
	HeaderTimestamp = "X-Blnk-Timestamp"
)

var (
	ErrBadSignature = errors.New("blnk: webhook signature mismatch")
	ErrStale        = errors.New("blnk: webhook timestamp outside tolerance")
)

// Sign returns the signature Blnk would send for body at timestamp ts.
func Sign(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature checks a webhook's signature and that its timestamp is within
// tolerance of now (which rejects replays of old deliveries). tolerance <= 0
// skips the age check.
func VerifySignature(secret, signature, ts string, body []byte, now time.Time, tolerance time.Duration) error {
	if signature == "" || ts == "" {
		return fmt.Errorf("%w: missing %s or %s header", ErrBadSignature, HeaderSignature, HeaderTimestamp)
	}
	want := Sign(secret, ts, body)
	if !hmac.Equal([]byte(want), []byte(signature)) {
		return ErrBadSignature
	}
	if tolerance > 0 {
		sec, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return fmt.Errorf("%w: bad timestamp %q", ErrStale, ts)
		}
		if d := now.Sub(time.Unix(sec, 0)); d > tolerance || d < -tolerance {
			return ErrStale
		}
	}
	return nil
}
