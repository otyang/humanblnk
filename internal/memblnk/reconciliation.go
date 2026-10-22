package memblnk

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"math"
	"math/big"
	"mime"
	"mime/multipart"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// upload is a stored statement.
type upload struct {
	source string
	lines  []blnk.ExternalTransaction
}

// uploadStatement stores a CSV or JSON statement with the columns id,
// amount, currency, reference, description and date.
func (s *Server) uploadStatement(r *http.Request, body []byte) (int, any) {
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || params["boundary"] == "" {
		return respond(0, nil, invalid(blnk.CodeReconUploadFailed, "want a multipart upload"))
	}
	form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(32 << 20)
	if err != nil {
		return respond(0, nil, invalid(blnk.CodeReconUploadFailed, "%v", err))
	}
	files := form.File["file"]
	if len(files) == 0 {
		return respond(0, nil, invalid(blnk.CodeReconUploadFailed, "no file"))
	}
	f, err := files[0].Open()
	if err != nil {
		return respond(0, nil, invalid(blnk.CodeReconUploadFailed, "%v", err))
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		return respond(0, nil, invalid(blnk.CodeReconUploadFailed, "%v", err))
	}
	var lines []blnk.ExternalTransaction
	if strings.EqualFold(path.Ext(files[0].Filename), ".json") {
		err = json.Unmarshal(raw, &lines)
	} else {
		lines, err = readCSV(raw)
	}
	if err != nil {
		return respond(0, nil, invalid(blnk.CodeReconUploadProcessing, "%v", err))
	}
	source := ""
	if v := form.Value["source"]; len(v) > 0 {
		source = v[0]
	}
	id := newID("upload_")
	s.uploads[id] = &upload{source: source, lines: lines}
	return http.StatusOK, blnk.UploadResponse{UploadID: id, RecordCount: len(lines), Source: source}
}

func readCSV(raw []byte) ([]blnk.ExternalTransaction, error) {
	rows, err := csv.NewReader(bytes.NewReader(raw)).ReadAll()
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	get := func(row []string, name string) string {
		if i, ok := col[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	var out []blnk.ExternalTransaction
	for _, row := range rows[1:] {
		out = append(out, blnk.ExternalTransaction{
			ID: get(row, "id"), Amount: json.Number(get(row, "amount")), Currency: get(row, "currency"),
			Reference: get(row, "reference"), Description: get(row, "description"), Date: get(row, "date"),
		})
	}
	return out, nil
}

func (s *Server) createRule(_ *http.Request, body []byte) (int, any) {
	var m blnk.MatchingRule
	if err := json.Unmarshal(body, &m); err != nil {
		return respond(0, nil, invalid(blnk.CodeMalformedRequest, "%v", err))
	}
	if m.Name == "" || len(m.Criteria) == 0 {
		return respond(0, nil, invalid(blnk.CodeReconRuleInvalid, "a rule needs a name and criteria"))
	}
	now := blnk.Time{Time: s.now()}
	m.RuleID, m.CreatedAt, m.UpdatedAt = newID("rule_"), now, now
	s.rules[m.RuleID] = &m
	return http.StatusCreated, &m
}

func (s *Server) updateRule(r *http.Request, body []byte) (int, any) {
	m, ok := s.rules[r.PathValue("id")]
	if !ok {
		return respond(0, nil, notFound(blnk.CodeReconRuleNotFound, "matching rule", r.PathValue("id")))
	}
	var in blnk.MatchingRule
	if err := json.Unmarshal(body, &in); err != nil {
		return respond(0, nil, invalid(blnk.CodeMalformedRequest, "%v", err))
	}
	if in.Name == "" || len(in.Criteria) == 0 {
		return respond(0, nil, invalid(blnk.CodeReconRuleInvalid, "a rule needs a name and criteria"))
	}
	m.Name, m.Description, m.Criteria, m.UpdatedAt = in.Name, in.Description, in.Criteria, blnk.Time{Time: s.now()}
	return http.StatusOK, m
}

func (s *Server) deleteRule(r *http.Request, _ []byte) (int, any) {
	if s.rules[r.PathValue("id")] == nil {
		return respond(0, nil, notFound(blnk.CodeReconRuleNotFound, "matching rule", r.PathValue("id")))
	}
	delete(s.rules, r.PathValue("id"))
	return http.StatusOK, map[string]string{"message": "matching rule deleted"}
}

func (s *Server) startReconciliation(_ *http.Request, body []byte) (int, any) {
	var in blnk.StartReconciliationRequest
	if err := json.Unmarshal(body, &in); err != nil {
		return respond(0, nil, invalid(blnk.CodeMalformedRequest, "%v", err))
	}
	up, ok := s.uploads[in.UploadID]
	if !ok {
		return respond(0, nil, notFound(blnk.CodeReconNotFound, "upload", in.UploadID))
	}
	return s.reconcile(in, up.lines)
}

func (s *Server) startInstantReconciliation(_ *http.Request, body []byte) (int, any) {
	var in blnk.StartReconciliationRequest
	if err := json.Unmarshal(body, &in); err != nil {
		return respond(0, nil, invalid(blnk.CodeMalformedRequest, "%v", err))
	}
	if len(in.ExternalTransactions) == 0 {
		return respond(0, nil, invalid(blnk.CodeReconExternalTxnsMissing, "no external transactions"))
	}
	return s.reconcile(in, in.ExternalTransactions)
}

func (s *Server) getReconciliation(r *http.Request, _ []byte) (int, any) {
	run, ok := s.runs[r.PathValue("id")]
	if !ok {
		return respond(0, nil, notFound(blnk.CodeReconNotFound, "reconciliation", r.PathValue("id")))
	}
	return http.StatusOK, run
}

// reconcile matches lines one to one against applied transactions: a line
// matches the first unmatched one that every criterion of any of the rules
// holds for. The run completes at once.
func (s *Server) reconcile(in blnk.StartReconciliationRequest, lines []blnk.ExternalTransaction) (int, any) {
	if len(in.MatchingRuleIDs) == 0 {
		return respond(0, nil, invalid(blnk.CodeReconMatchingRulesMissing, "no matching rules"))
	}
	var rules []*blnk.MatchingRule
	for _, id := range in.MatchingRuleIDs {
		m, ok := s.rules[id]
		if !ok {
			return respond(0, nil, notFound(blnk.CodeReconRuleNotFound, "matching rule", id))
		}
		rules = append(rules, m)
	}
	if in.Strategy != "" && in.Strategy != "one_to_one" {
		return respond(0, nil, invalid(blnk.CodeBadRequest, "memblnk reconciles one_to_one only, not %s", in.Strategy))
	}
	started := s.now()
	used := map[string]bool{}
	matched := 0
	for _, line := range lines {
		for _, id := range s.txnOrder {
			t := s.txns[id]
			if used[id] || t.Status != blnk.StatusApplied {
				continue
			}
			if anyRule(rules, line, t) {
				used[id] = true
				matched++
				break
			}
		}
	}
	run := &blnk.Reconciliation{
		ReconciliationID: newID("recon_"), UploadID: in.UploadID, Status: "completed",
		MatchedTransactions: matched, UnmatchedTransactions: len(lines) - matched,
		StartedAt: blnk.Time{Time: started}, CompletedAt: blnk.Time{Time: s.now()},
	}
	s.runs[run.ReconciliationID] = run
	return http.StatusOK, map[string]string{"reconciliation_id": run.ReconciliationID}
}

func anyRule(rules []*blnk.MatchingRule, line blnk.ExternalTransaction, t *txn) bool {
	for _, r := range rules {
		all := true
		for _, c := range r.Criteria {
			if !criterion(c, line, t) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// criterion reports whether line and t satisfy c. Amounts compare in major
// units, within AllowableDrift as a fraction; dates within AllowableDrift
// seconds, or on the same day without one.
func criterion(c blnk.MatchingCriterion, line blnk.ExternalTransaction, t *txn) bool {
	switch c.Field {
	case "amount":
		ext, ok1 := new(big.Rat).SetString(line.Amount.String())
		in, ok2 := new(big.Rat).SetString(t.AmountString)
		if !ok1 || !ok2 {
			return false
		}
		diff := new(big.Rat).Sub(ext, in)
		switch c.Operator {
		case "greater_than":
			return diff.Sign() > 0
		case "less_than":
			return diff.Sign() < 0
		}
		limit := new(big.Rat).Mul(new(big.Rat).Abs(in), new(big.Rat).SetFloat64(c.AllowableDrift))
		return new(big.Rat).Abs(diff).Cmp(limit) <= 0
	case "date":
		ext, err := time.Parse(time.RFC3339, line.Date)
		if err != nil {
			return false
		}
		in := t.CreatedAt.Time
		switch c.Operator {
		case "greater_than":
			return ext.After(in)
		case "less_than":
			return ext.Before(in)
		}
		if c.AllowableDrift > 0 {
			return math.Abs(ext.Sub(in).Seconds()) <= c.AllowableDrift
		}
		return ext.UTC().Format(time.DateOnly) == in.UTC().Format(time.DateOnly)
	case "reference":
		return compareText(c, line.Reference, t.Reference)
	case "description":
		return compareText(c, line.Description, t.Description)
	case "currency":
		return compareText(c, line.Currency, t.Currency)
	}
	return false
}

// compareText matches as Blnk does: equals ignores case; contains holds if
// either text contains the other, or they are within AllowableDrift percent
// of the longer one's length in edits.
func compareText(c blnk.MatchingCriterion, ext, in string) bool {
	a, b := strings.ToLower(ext), strings.ToLower(in)
	if c.Operator != "contains" {
		return a == b
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return true
	}
	longest := max(len([]rune(a)), len([]rune(b)))
	return edits(a, b) <= int(float64(longest)*c.AllowableDrift/100)
}

// edits is the Levenshtein distance between a and b.
func edits(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := range ra {
		cur := make([]int, len(rb)+1)
		cur[0] = i + 1
		for j := range rb {
			cost := 1
			if ra[i] == rb[j] {
				cost = 0
			}
			cur[j+1] = min(prev[j+1]+1, cur[j]+1, prev[j]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
