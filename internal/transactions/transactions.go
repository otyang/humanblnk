// Package transactions records, reverses and reads ledger transactions. Use
// it through humanblnk.Ledger.Transactions.
package transactions

import "github.com/otyang/humanblnk/internal/core"

// Service records, reverses and reads transactions.
type Service struct{ k *core.Core }

// New returns a Service on k.
func New(k *core.Core) *Service { return &Service{k} }
