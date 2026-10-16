package ledger

import (
	"math/big"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// EntryStatus is the accounting view of Blnk's transaction status.
type EntryStatus string

const (
	EntryPosted    EntryStatus = "posted"    // Blnk APPLIED: in the balances
	EntryPending   EntryStatus = "pending"   // Blnk INFLIGHT: funds reserved, not posted
	EntryVoided    EntryStatus = "voided"    // Blnk VOID: pending entry cancelled, never posted
	EntryQueued    EntryStatus = "queued"    // Blnk QUEUED: accepted, not yet processed
	EntryScheduled EntryStatus = "scheduled" // Blnk SCHEDULED: will post at scheduled_for
	EntryRejected  EntryStatus = "rejected"  // Blnk REJECTED: refused (e.g. insufficient funds)
)

var toEntryStatus = map[string]EntryStatus{
	blnk.StatusApplied:   EntryPosted,
	blnk.StatusInflight:  EntryPending,
	blnk.StatusVoid:      EntryVoided,
	blnk.StatusQueued:    EntryQueued,
	blnk.StatusScheduled: EntryScheduled,
	blnk.StatusRejected:  EntryRejected,
}

// EntryStatusOf maps a Blnk status (APPLIED, INFLIGHT, ...) to an EntryStatus.
func EntryStatusOf(blnkStatus string) EntryStatus {
	if s, ok := toEntryStatus[strings.ToUpper(blnkStatus)]; ok {
		return s
	}
	return EntryStatus(strings.ToLower(blnkStatus))
}

// blnkStatusOf accepts either vocabulary ("posted" or "APPLIED") and returns Blnk's.
func blnkStatusOf(s string) string {
	for b, e := range toEntryStatus {
		if strings.EqualFold(s, string(e)) {
			return b
		}
	}
	return strings.ToUpper(s)
}

// Receipt is the domain view of a Blnk transaction.
type Receipt struct {
	TransactionID     string         `json:"transaction_id"`
	ParentTransaction string         `json:"parent_transaction,omitempty"`
	Reference         string         `json:"reference"`
	Status            string         `json:"status"`       // Blnk's: QUEUED | APPLIED | INFLIGHT | VOID | REJECTED | SCHEDULED
	EntryStatus       EntryStatus    `json:"entry_status"` // accounting view: posted | pending | voided | queued | scheduled | rejected
	Amount            Money          `json:"amount"`       // {"amount":"0.456","currency":"BTC","precise_amount":"45600000","precision":100000000}
	Source            string         `json:"source"`
	Destination       string         `json:"destination,omitempty"`
	Legs              []Leg          `json:"legs,omitempty"` // populated for split (fee) transactions
	Description       string         `json:"description,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	MetaData          map[string]any `json:"meta_data,omitempty"`

	Queued   bool `json:"queued,omitempty"`   // accepted by the queue; final status arrives later (webhook / poll)
	Replayed bool `json:"replayed,omitempty"` // reference already existed with the same payload; this is the original

	Raw *blnk.Transaction `json:"-"`
}

// Pending reports whether funds are reserved but not yet settled.
func (r *Receipt) Pending() bool { return r.Status == blnk.StatusInflight }

// Settled reports whether funds have moved.
func (r *Receipt) Settled() bool { return r.Status == blnk.StatusApplied }

// Posted reports whether the entry is in the balances (same as Settled).
func (r *Receipt) Posted() bool { return r.EntryStatus == EntryPosted }

// Leg is one destination of a transaction with several, e.g. a payment and its fee.
type Leg struct {
	Account       string `json:"account"`
	Amount        Money  `json:"amount"`       // set for fixed legs (fee, payout); zero for "left"/"%"
	Distribution  string `json:"distribution"` // as sent: "left", "20%", or the precise integer
	TransactionID string `json:"transaction_id,omitempty"`
	Narration     string `json:"narration,omitempty"`
}

// BulkReceipt is the outcome of Wallet().Bulk.
type BulkReceipt struct {
	BatchID string
	Status  string // applied | inflight | processing
	Count   int
}

func (s *Service) inflightReceipt(txID string, r *blnk.UpdateInflightResponse) *Receipt {
	rc := s.toReceipt(&r.Transaction)
	if rc.TransactionID == "" {
		rc.TransactionID = txID
	}
	if r.Queued {
		rc.Queued = true
	}
	return rc
}

// txPrecision is the precision the transaction was recorded with (falls back to config).
func (s *Service) txPrecision(t *blnk.Transaction) int64 {
	if p, err := t.Precision.Int64(); err == nil && p > 0 {
		return p
	}
	return s.Precision(t.Currency)
}

// txMoney builds the amount in both forms from what Blnk stored.
func (s *Service) txMoney(t *blnk.Transaction) Money {
	return s.money(&t.PreciseAmount.Int, s.txPrecision(t), t.Currency)
}

func (s *Service) toReceipt(t *blnk.Transaction) *Receipt {
	precision := s.txPrecision(t)
	r := &Receipt{
		TransactionID:     t.GetID(),
		ParentTransaction: t.ParentTransaction,
		Reference:         t.Reference,
		Status:            t.Status,
		EntryStatus:       EntryStatusOf(t.Status),
		Amount:            s.txMoney(t),
		Source:            t.Source,
		Destination:       t.Destination,
		Description:       t.Description,
		CreatedAt:         t.CreatedAt.Time,
		MetaData:          t.MetaData,
		Queued:            t.Status == blnk.StatusQueued,
		Raw:               t,
	}
	for _, legs := range [][]blnk.Distribution{t.Sources, t.Destinations} {
		for _, d := range legs {
			leg := Leg{Account: d.Identifier, Distribution: d.Distribution, TransactionID: d.TransactionID, Narration: d.Narration}
			if d.PreciseDistribution != "" {
				if v, ok := new(big.Int).SetString(d.PreciseDistribution, 10); ok {
					leg.Amount = s.money(v, precision, t.Currency)
				}
				if leg.Distribution == "" {
					leg.Distribution = d.PreciseDistribution
				}
			}
			r.Legs = append(r.Legs, leg)
		}
	}
	return r
}
