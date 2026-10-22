package memblnk

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// createMonitor stores a monitor as Blnk does: precise_value is
// int64(value × precision) worked out in float64, the description is
// ignored, and the callback URL is kept but never returned.
func (s *Server) createMonitor(_ *http.Request, body []byte) (int, any) {
	var m blnk.BalanceMonitor
	if err := json.Unmarshal(body, &m); err != nil {
		return respond(0, nil, invalid(blnk.CodeMalformedRequest, "%v", err))
	}
	if err := s.checkMonitor(&m); err != nil {
		return respond(0, nil, err)
	}
	pv, err := preciseValue(m.Condition)
	if err != nil {
		return respond(0, nil, err)
	}
	m.MonitorID = newID("mon_")
	m.Condition.PreciseValue = pv
	m.Description = ""
	m.CallBackURL = ""
	m.CreatedAt = blnk.Time{Time: s.now()}
	s.monitors[m.MonitorID] = &m
	s.monitorOrder = append(s.monitorOrder, m.MonitorID)
	return http.StatusCreated, &m
}

func (s *Server) checkMonitor(m *blnk.BalanceMonitor) *failure {
	if s.balances[m.BalanceID] == nil {
		return notFound(blnk.CodeBalanceNotFound, "balance", m.BalanceID)
	}
	if _, ok := monitorFields[m.Condition.Field]; !ok {
		return invalid(blnk.CodeBalanceInvalid, "unknown monitor field %q", m.Condition.Field)
	}
	return nil
}

// preciseValue is Blnk's int64(float64(value) × float64(precision)).
func preciseValue(c blnk.MonitorCondition) (*blnk.BigInt, *failure) {
	v, err := strconv.ParseFloat(c.Value.String(), 64)
	if err != nil {
		return nil, invalid(blnk.CodeBalanceInvalid, "bad monitor value %q", c.Value)
	}
	p := c.Precision
	if p == 0 {
		p = 1
	}
	return blnk.NewBigInt(int64(v * float64(p))), nil
}

func (s *Server) getMonitor(r *http.Request, _ []byte) (int, any) {
	m, ok := s.monitors[r.PathValue("id")]
	if !ok {
		return respond(0, nil, notFound(blnk.CodeMonitorNotFound, "monitor", r.PathValue("id")))
	}
	return http.StatusOK, m
}

func (s *Server) listMonitors(*http.Request, []byte) (int, any) {
	out := []*blnk.BalanceMonitor{}
	for _, id := range s.monitorOrder {
		if m := s.monitors[id]; m != nil {
			out = append(out, m)
		}
	}
	return http.StatusOK, out
}

func (s *Server) listMonitorsByBalance(r *http.Request, _ []byte) (int, any) {
	out := []*blnk.BalanceMonitor{}
	for _, id := range s.monitorOrder {
		if m := s.monitors[id]; m != nil && m.BalanceID == r.PathValue("id") {
			out = append(out, m)
		}
	}
	return http.StatusOK, out
}

// updateMonitor rewrites the balance, condition and description, and, as in
// Blnk, leaves precise_value as it was: a new threshold never takes effect.
func (s *Server) updateMonitor(r *http.Request, body []byte) (int, any) {
	m, ok := s.monitors[r.PathValue("id")]
	if !ok {
		return respond(0, nil, notFound(blnk.CodeMonitorNotFound, "monitor", r.PathValue("id")))
	}
	var in blnk.BalanceMonitor
	if err := json.Unmarshal(body, &in); err != nil {
		return respond(0, nil, invalid(blnk.CodeMalformedRequest, "%v", err))
	}
	if err := s.checkMonitor(&in); err != nil {
		return respond(0, nil, err)
	}
	m.BalanceID = in.BalanceID
	m.Condition.Field, m.Condition.Operator = in.Condition.Field, in.Condition.Operator
	m.Condition.Value, m.Condition.Precision = in.Condition.Value, in.Condition.Precision
	m.Description = in.Description
	return http.StatusOK, map[string]string{"message": "monitor updated"}
}

func (s *Server) deleteMonitor(r *http.Request, _ []byte) (int, any) {
	if s.monitors[r.PathValue("id")] == nil {
		return respond(0, nil, notFound(blnk.CodeMonitorNotFound, "monitor", r.PathValue("id")))
	}
	delete(s.monitors, r.PathValue("id"))
	return http.StatusOK, map[string]string{"message": "monitor deleted"}
}

// monitorFields reads each field a monitor can watch.
var monitorFields = map[string]func(*blnk.Balance) *big.Int{
	"balance":                 func(b *blnk.Balance) *big.Int { return &b.Balance.Int },
	"credit_balance":          func(b *blnk.Balance) *big.Int { return &b.CreditBalance.Int },
	"debit_balance":           func(b *blnk.Balance) *big.Int { return &b.DebitBalance.Int },
	"inflight_balance":        func(b *blnk.Balance) *big.Int { return &b.InflightBalance.Int },
	"inflight_debit_balance":  func(b *blnk.Balance) *big.Int { return &b.InflightDebitBalance.Int },
	"inflight_credit_balance": func(b *blnk.Balance) *big.Int { return &b.InflightCreditBalance.Int },
}

// balanceChanged fires balance.monitor for each monitor on b whose
// condition now holds. Blnk's evaluator knows > < >= <= == != only.
func (s *Server) balanceChanged(b *blnk.Balance) {
	for _, id := range s.monitorOrder {
		m := s.monitors[id]
		if m == nil || m.BalanceID != b.BalanceID || m.Condition.PreciseValue == nil {
			continue
		}
		c := monitorFields[m.Condition.Field](b).Cmp(&m.Condition.PreciseValue.Int)
		var fire bool
		switch m.Condition.Operator {
		case ">":
			fire = c > 0
		case "<":
			fire = c < 0
		case ">=":
			fire = c >= 0
		case "<=":
			fire = c <= 0
		case "==":
			fire = c == 0
		case "!=":
			fire = c != 0
		}
		if fire {
			s.emit("balance.monitor", m)
		}
	}
}

// --- Webhooks ---

// emit queues a webhook, sent once the state is unlocked. data may share
// state with the server, so it is encoded now, under mu.
func (s *Server) emit(kind string, data any) {
	if s.webhookURL == "" {
		return
	}
	body, err := json.Marshal(struct {
		Event string `json:"event"`
		Data  any    `json:"data"`
	}{kind, data})
	if err == nil {
		s.outbox = append(s.outbox, body)
	}
}

// webhookClient delivers webhooks; a receiver slower than this loses them.
var webhookClient = &http.Client{Timeout: 5 * time.Second}

// flush sends the queued webhooks, signed as Blnk signs them, in the
// background. Delivery is best effort: failures are dropped, and Close
// cancels what is still being sent.
func (s *Server) flush() {
	s.mu.Lock()
	out := s.outbox
	s.outbox = nil
	if len(out) == 0 || s.closed {
		s.mu.Unlock()
		return
	}
	s.background.Add(1) // under mu, so never after Close has begun waiting
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer s.background.Done()
		defer cancel()
		go func() { // Close cancels deliveries in flight
			select {
			case <-s.done:
				cancel()
			case <-ctx.Done():
			}
		}()
		for _, body := range out {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
			if err != nil {
				continue
			}
			ts := strconv.FormatInt(time.Now().Unix(), 10)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(blnk.HeaderTimestamp, ts)
			if s.secret != "" {
				req.Header.Set(blnk.HeaderSignature, blnk.Sign(s.secret, ts, body))
			}
			if resp, err := webhookClient.Do(req); err == nil {
				resp.Body.Close()
			}
		}
	}()
}
