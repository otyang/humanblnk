package ledgertest

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/otyang/humanblnk/blnk"
)

// state is a deep copy of everything a failed atomic batch must roll back.
type state struct {
	seq        int
	balances   map[string]*balance
	indicators map[string]string
	txns       map[string]*txn
	order      []string
	refs       map[string]string
	events     []Event
}

func (s *Server) save() state {
	st := state{
		seq: s.seq, balances: map[string]*balance{}, indicators: map[string]string{},
		txns: map[string]*txn{}, order: append([]string(nil), s.order...),
		refs: map[string]string{}, events: append([]Event(nil), s.events...),
	}
	for k, b := range s.balances {
		c := &balance{Balance: b.view(), history: append([]snapshot(nil), b.history...)}
		c.Balance.Balance = blnk.BigInt{}
		c.Balance.Balance.Set(&b.Balance.Balance.Int)
		for _, p := range []struct{ dst, src *blnk.BigInt }{
			{&c.CreditBalance, &b.CreditBalance}, {&c.DebitBalance, &b.DebitBalance},
			{&c.InflightBalance, &b.InflightBalance}, {&c.InflightCreditBalance, &b.InflightCreditBalance},
			{&c.InflightDebitBalance, &b.InflightDebitBalance},
		} {
			*p.dst = blnk.BigInt{}
			p.dst.Set(&p.src.Int)
		}
		st.balances[k] = c
	}
	for k, v := range s.indicators {
		st.indicators[k] = v
	}
	for k, t := range s.txns {
		c := *t
		c.precise = big.Int{}
		c.precise.Set(&t.precise)
		c.committed = big.Int{}
		c.committed.Set(&t.committed)
		c.children = append([]string(nil), t.children...)
		st.txns[k] = &c
	}
	for k, v := range s.refs {
		st.refs[k] = v
	}
	return st
}

func (s *Server) restore(st state) {
	s.seq, s.balances, s.indicators = st.seq, st.balances, st.indicators
	s.txns, s.order, s.refs, s.events = st.txns, st.order, st.refs, st.events
}

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

func jsonNum(v int64) json.Number { return json.Number(fmt.Sprint(v)) }

func jsonNumber(s string) json.Number { return json.Number(s) }
