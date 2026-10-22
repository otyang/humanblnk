// Package transactions records, reverses and reads transactions. Use
// it through humanblnk.Client.Transactions.
package transactions

import (
	"time"

	"github.com/otyang/humanblnk/internal/core"
	"github.com/otyang/humanblnk/internal/holds"
)

// RecordHold is how long a split Record holds, or a RecordAll batch, may
// wait for its capture. Record captures it at once; the expiry only
// matters if the process stops between the two, when it releases the money
// rather than keep it reserved. A retry within it finishes the capture.
const RecordHold = 15 * time.Minute

// Service records, reverses and reads transactions.
type Service struct {
	k     *core.Core
	holds *holds.Service
}

// New returns a Service on k.
func New(k *core.Core) *Service { return &Service{k: k, holds: holds.New(k)} }
