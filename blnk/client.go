// Package blnk is a thin, typed HTTP client for the Blnk Core REST API
// (https://docs.blnkfinance.com/reference/overview).
//
// It maps one method to one endpoint and does no business logic. The
// domain-level interface (deposit, withdraw, transfer, ...) lives in the
// sibling package `ledger`.
package blnk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to a single Blnk Core instance.
type Client struct {
	baseURL    string
	apiKey     string
	http       *http.Client
	maxRetries int
	backoff    time.Duration
	userAgent  string
}

// Option configures a Client.
type Option func(*Client)

// WithAPIKey sets the X-Blnk-Key header (required when Blnk runs in secure mode).
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithHTTPClient replaces the default *http.Client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithRetries sets how many times retryable failures are retried
// (423 lock contention, 429 rate limit, 503 queue backpressure, 502/504 and
// network errors). Committing or voiding a pending transaction carries no
// reference that would make a repeat harmless, so those calls are retried only
// on answers that show Blnk didn't act (423, 429, 503), never after a network
// error or gateway timeout: check the transaction before trying again.
func WithRetries(n int, base time.Duration) Option {
	return func(c *Client) { c.maxRetries, c.backoff = n, base }
}

// WithUserAgent overrides the User-Agent header.
func WithUserAgent(ua string) Option { return func(c *Client) { c.userAgent = ua } }

// New creates a client. baseURL is your Core URL, e.g. "https://core.example.com"
// or, for an Enterprise Core instance, "https://ENTERPRISE_PUBLIC_URL/core".
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		http:       &http.Client{Timeout: 30 * time.Second},
		maxRetries: 3,
		backoff:    200 * time.Millisecond,
		userAgent:  "humanblnk-go/" + Version,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func retryable(status int) bool {
	return status == http.StatusLocked || status == http.StatusTooManyRequests ||
		status == http.StatusServiceUnavailable || status == http.StatusBadGateway ||
		status == http.StatusGatewayTimeout
}

// do performs a JSON request. Retrying a transaction POST is safe because Blnk
// enforces idempotency on `reference`; a replay surfaces as TXN_DUPLICATE_REFERENCE.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	return c.send(ctx, method, path, in, out, true)
}

// doOnce performs a request that must not be applied twice (an inflight
// commit has no reference, so a repeated partial commit would capture twice).
// It is retried only when Blnk certainly did nothing.
func (c *Client) doOnce(ctx context.Context, method, path string, in, out any) error {
	return c.send(ctx, method, path, in, out, false)
}

func (c *Client) send(ctx context.Context, method, path string, in, out any, repeatable bool) error {
	var payload []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("blnk: encode request: %w", err)
		}
		payload = b
	}
	return c.doRaw(ctx, method, path, "application/json", payload, out, repeatable)
}

// doRaw sends payload as-is with the given content type and decodes a JSON
// response. Unless repeatable, a failure that may have reached Blnk (network
// error, 502, 504) is returned without retrying.
func (c *Client) doRaw(ctx context.Context, method, path, contentType string, payload []byte, out any, repeatable bool) error {
	if !strings.HasPrefix(c.baseURL, "http://") && !strings.HasPrefix(c.baseURL, "https://") {
		return fmt.Errorf("blnk: base URL %q must start with http:// or https:// (is BLNK_URL set?)", c.baseURL)
	}
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			wait := c.backoff * time.Duration(1<<(attempt-1))
			wait += time.Duration(rand.Int63n(int64(c.backoff) + 1)) // jitter
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}

		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
		if err != nil {
			return fmt.Errorf("blnk: build request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.userAgent)
		if payload != nil {
			req.Header.Set("Content-Type", contentType)
		}
		if c.apiKey != "" {
			req.Header.Set("X-Blnk-Key", c.apiKey)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = fmt.Errorf("blnk: %s %s: %w", method, path, err)
			if !repeatable {
				return lastErr
			}
			continue
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("blnk: read response: %w", readErr)
			if !repeatable {
				return lastErr
			}
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out == nil || len(bytes.TrimSpace(raw)) == 0 {
				return nil
			}
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("blnk: decode %s %s response: %w", method, path, err)
			}
			return nil
		}

		apiErr := parseAPIError(resp.StatusCode, raw)
		if retryable(resp.StatusCode) && (repeatable || refused(resp.StatusCode)) {
			lastErr = apiErr
			continue
		}
		return apiErr
	}
	return lastErr
}

// refused reports whether a retryable status shows Blnk turned the request
// away without acting on it (as opposed to a gateway that may have passed it on).
func refused(status int) bool {
	return status == http.StatusLocked || status == http.StatusTooManyRequests ||
		status == http.StatusServiceUnavailable
}

func esc(s string) string { return url.PathEscape(s) }

// IsRetryable reports whether err is a transient failure worth retrying later.
func IsRetryable(err error) bool {
	var ae *APIError
	if errors.As(err, &ae) {
		return retryable(ae.Status)
	}
	return false
}
