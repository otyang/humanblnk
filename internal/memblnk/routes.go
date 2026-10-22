package memblnk

import (
	"net/http"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

func (s *Server) routes() http.Handler {
	m := http.NewServeMux()
	on := func(pattern string, h handler) { m.Handle(pattern, s.locked(h)) }

	on("GET /health", func(*http.Request, []byte) (int, any) { return http.StatusOK, map[string]string{"status": "UP"} })

	on("POST /ledgers", s.createLedger)
	on("GET /ledgers", s.listLedgers)
	on("POST /ledgers/filter", s.filterLedgers)
	on("GET /ledgers/{id}", s.getLedger)
	on("PUT /ledgers/{id}", s.renameLedger)

	on("POST /balances", s.createBalance)
	on("POST /balances/filter", s.filterBalances)
	on("GET /balances/{id}", s.getBalance)
	on("GET /balances/indicator/{indicator}/currency/{currency}", s.getBalanceByIndicator)

	on("POST /identities", s.createIdentity)
	on("POST /identities/filter", s.filterIdentities)
	on("GET /identities/{id}", s.getIdentity)

	on("POST /{id}/metadata", s.updateMetadata)

	on("POST /transactions", s.createTransaction)
	on("POST /transactions/bulk", s.createBulk)
	on("POST /transactions/filter", s.filterTransactions)
	on("GET /transactions/{id}", s.getTransaction)
	m.HandleFunc("GET /transactions/reference/{ref}", func(w http.ResponseWriter, r *http.Request) {
		// Blnk's router decodes an escaped "/" and finds no route: a bare 404,
		// whatever the reference holds.
		if strings.Contains(strings.ToUpper(r.URL.RawPath), "%2F") {
			http.Error(w, "404 page not found", http.StatusNotFound)
			return
		}
		s.locked(s.getTransactionByRef)(w, r)
	})
	on("PUT /transactions/inflight/{id}", s.updateInflight)

	on("POST /balance-monitors", s.createMonitor)
	on("GET /balance-monitors", s.listMonitors)
	on("GET /balance-monitors/{id}", s.getMonitor)
	on("GET /balance-monitors/balances/{id}", s.listMonitorsByBalance)
	on("PUT /balance-monitors/{id}", s.updateMonitor)
	on("DELETE /balance-monitors/{id}", s.deleteMonitor)

	on("POST /reconciliation/upload", s.uploadStatement)
	on("POST /reconciliation/matching-rules", s.createRule)
	on("PUT /reconciliation/matching-rules/{id}", s.updateRule)
	on("DELETE /reconciliation/matching-rules/{id}", s.deleteRule)
	on("POST /reconciliation/start", s.startReconciliation)
	on("POST /reconciliation/start-instant", s.startInstantReconciliation)
	on("GET /reconciliation/{id}", s.getReconciliation)

	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, apiError(blnk.CodeNotFound, "memblnk doesn't serve %s %s", r.Method, r.URL.Path))
	})
	return m
}
