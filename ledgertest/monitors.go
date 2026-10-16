package ledgertest

import "math/big"

// checkMonitors emits balance.monitor for every monitor on b whose condition
// holds, after every update (level-triggered, like Blnk).
func (s *Server) checkMonitors(b *balance) {
	for _, m := range s.monitors {
		if m.BalanceID != b.BalanceID || m.Condition.PreciseValue == nil {
			continue
		}
		var v *big.Int
		switch m.Condition.Field {
		case "balance":
			v = &b.Balance.Balance.Int
		case "credit_balance":
			v = &b.CreditBalance.Int
		case "debit_balance":
			v = &b.DebitBalance.Int
		case "inflight_balance":
			v = &b.InflightBalance.Int
		case "inflight_debit_balance":
			v = &b.InflightDebitBalance.Int
		case "inflight_credit_balance":
			v = &b.InflightCreditBalance.Int
		default:
			continue
		}
		c := v.Cmp(&m.Condition.PreciseValue.Int)
		// Blnk's evaluator: "=" is accepted on create but never matches.
		hit := map[string]bool{
			">": c > 0, ">=": c >= 0, "<": c < 0, "<=": c <= 0, "==": c == 0, "!=": c != 0,
		}[m.Condition.Operator]
		if hit {
			s.emit("balance.monitor", m)
		}
	}
}
