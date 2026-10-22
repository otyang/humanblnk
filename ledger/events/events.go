// Package events verifies and decodes Blnk webhooks. Use it through
// ledger.VerifyWebhook.
package events

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/otyang/humanblnk/clients/blnk"
	"github.com/otyang/humanblnk/ledger/internal/core"
	"github.com/otyang/humanblnk/ledger/types"
)

// maxBody caps how much of a webhook VerifyWebhook reads.
const maxBody = 1 << 20 // 1 MB

// VerifyWebhook checks a Blnk webhook's signature (Blnk's server.secret_key)
// and that it is no older than tolerance, then decodes it. A request that
// fails the check is an error; never act on its body.
func VerifyWebhook(secret string, r *http.Request, tolerance time.Duration) (types.Event, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return types.Event{}, fmt.Errorf("ledger: read webhook: %w", err)
	}
	if len(body) > maxBody {
		return types.Event{}, fmt.Errorf("ledger: webhook larger than %d bytes", maxBody)
	}
	sig, ts := r.Header.Get(blnk.HeaderSignature), r.Header.Get(blnk.HeaderTimestamp)
	if err := blnk.VerifySignature(secret, sig, ts, body, time.Now(), tolerance); err != nil {
		return types.Event{}, err
	}
	var raw struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return types.Event{}, fmt.Errorf("ledger: decode webhook: %w", err)
	}
	ev := types.Event{Kind: raw.Event, Data: raw.Data}
	if raw.Event == "balance.monitor" {
		var m blnk.BalanceMonitor
		if err := json.Unmarshal(raw.Data, &m); err != nil {
			return types.Event{}, fmt.Errorf("ledger: decode %s: %w", raw.Event, err)
		}
		ev.Watch, ev.Account = types.WatchID(m.MonitorID), types.AccountRef(m.BalanceID)
	}
	if strings.HasPrefix(raw.Event, "transaction.") {
		var tx blnk.Transaction
		if err := json.Unmarshal(raw.Data, &tx); err != nil {
			return types.Event{}, fmt.Errorf("ledger: decode %s: %w", raw.Event, err)
		}
		e, err := core.Entry(&tx)
		if err != nil {
			return types.Event{}, err
		}
		ev.Entry = &e
	}
	return ev, nil
}
