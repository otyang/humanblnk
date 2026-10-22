package types

import (
	"time"

	"github.com/otyang/humanblnk/money"
)

// StatementID is an uploaded statement, RuleID a matching rule, and RunID a
// reconciliation run.
type (
	StatementID string
	RuleID      string
	RunID       string
)

// StatementLine is one line of a provider's or bank's statement.
type StatementLine struct {
	ID          string // the provider's id for it
	Amount      money.Money
	Reference   string
	Description string
	Date        time.Time
	Source      string // e.g. "paystack"
}

// MatchRule says when a statement line and a ledger entry match: every
// criterion must hold.
type MatchRule struct {
	ID          RuleID
	Name        string
	Description string
	Criteria    []Criterion
	CreatedAt   time.Time
}

// Criterion is one condition of a MatchRule.
type Criterion struct {
	Field   MatchField
	Op      MatchOp
	Value   string
	Pattern string // for MatchContains
	// Drift is how far apart the two may be and still match: a fraction of
	// the amount for MatchAmount (0.01 is 1%), seconds for MatchDate.
	Drift float64
}

// MatchField is what a criterion compares.
type MatchField string

// Match fields.
const (
	MatchAmount      MatchField = "amount"
	MatchDate        MatchField = "date"
	MatchReference   MatchField = "reference"
	MatchDescription MatchField = "description"
	MatchCurrency    MatchField = "currency"
)

// MatchOp is how a criterion compares.
type MatchOp string

// Match operators.
const (
	MatchEquals      MatchOp = "equals"
	MatchGreaterThan MatchOp = "greater_than"
	MatchLessThan    MatchOp = "less_than"
	MatchContains    MatchOp = "contains"
)

// Strategy is how many entries and lines can match each other.
type Strategy string

// Strategies.
const (
	OneToOne  Strategy = "one_to_one"
	OneToMany Strategy = "one_to_many" // one line, several entries
	ManyToOne Strategy = "many_to_one" // several lines, one entry
)

// Run is how to reconcile.
type Run struct {
	Rules    []RuleID
	Strategy Strategy
	GroupBy  string // for OneToMany and ManyToOne: the field that groups, e.g. "reference"
	DryRun   bool   // match without recording the results
}

// RunResult is a reconciliation run's progress and counts.
type RunResult struct {
	ID          RunID
	Statement   StatementID
	Status      string // started | in_progress | completed | failed
	Matched     int
	Unmatched   int
	StartedAt   time.Time
	CompletedAt time.Time
}
