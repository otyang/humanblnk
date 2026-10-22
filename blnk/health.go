package blnk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Health is Blnk Core's GET /health answer.
type Health struct {
	Status string `json:"status"` // "UP" when Core and its database are reachable
	Reason string `json:"reason,omitempty"`
}

// Health checks that Core is up and can reach its database. A configuration
// mistake in the client (bad URL, Cloud instance ID) is returned here too.
func (c *Client) Health(ctx context.Context) (*Health, error) {
	var out Health
	if err := c.do(ctx, http.MethodGet, "/health", nil, &out); err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == http.StatusServiceUnavailable {
			_ = json.Unmarshal([]byte(ae.Body), &out)
			if out.Status != "" {
				return &out, fmt.Errorf("blnk: Core is %s: %s", out.Status, out.Reason)
			}
		}
		return nil, err
	}
	if !strings.EqualFold(out.Status, "UP") {
		return &out, fmt.Errorf("blnk: Core is %s: %s", out.Status, out.Reason)
	}
	return &out, nil
}
