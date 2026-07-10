// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package openaiclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Endpoints under the OpenAI Admin v1 surface. Constants so tests can pin
// expected URL paths and httptest servers can route on them.
const (
	UsagePath       = "/v1/organization/usage/completions"
	CostPath        = "/v1/organization/costs"
	HeaderAuth      = "Authorization"
	HeaderRateLimit = "x-ratelimit-remaining-requests"
	HeaderRateReset = "x-ratelimit-reset-requests"
	HeaderRequestID = "x-request-id"
)

// ErrCircuitOpen is returned when the circuit breaker has tripped and is
// rejecting calls fast. The poller loop should record the failure metric
// and skip until the next cycle.
var ErrCircuitOpen = errors.New("openaiclient: circuit breaker open")

// ErrRateLimited is returned after the retry budget is exhausted on a 429.
var ErrRateLimited = errors.New("openaiclient: rate limited (retry budget exhausted)")

// ErrServerError is returned after the retry budget is exhausted on 5xx.
var ErrServerError = errors.New("openaiclient: server error (retry budget exhausted)")

// RateLimitInfo captures the rate-limit headers OpenAI returns so the poller
// can emit `llm_rate_limit_events_total` and operators can build dashboards.
type RateLimitInfo struct {
	// Remaining is the parsed value of x-ratelimit-remaining-requests, or
	// -1 when the header is missing / unparseable.
	Remaining int64
	// ResetAfter is the duration until quota resets (best-effort parse of
	// x-ratelimit-reset-requests).
	ResetAfter time.Duration
	// HitRateLimit is true if the request returned 429 at any point in the
	// retry chain (used to emit llm_rate_limit_events_total).
	HitRateLimit bool
	// RequestID is x-request-id from the final response. Safe to log.
	RequestID string
}

// Config holds runtime knobs for the client. All fields have safe defaults
// when zero so callers can leave most unset.
type Config struct {
	BaseURL                 string
	APIKey                  string
	HTTPClient              *http.Client
	MaxRetries              int
	CircuitBreakerThreshold int
	CircuitBreakerCooldown  time.Duration
	// Now is injectable so tests can drive the breaker deterministically.
	Now func() time.Time
}

// Client wraps the OpenAI Admin Usage + Cost endpoints with backoff +
// circuit breaker. Never logs the API key.
type Client struct {
	cfg Config

	mu               sync.Mutex
	consecutiveFails int
	circuitOpenedAt  time.Time
}

// New constructs a Client and applies defaults.
func New(cfg Config) *Client {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 4
	}
	if cfg.CircuitBreakerThreshold == 0 {
		cfg.CircuitBreakerThreshold = 5
	}
	if cfg.CircuitBreakerCooldown == 0 {
		cfg.CircuitBreakerCooldown = 60 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg}
}

// FetchWindow pulls usage + cost rows for [start, end) (UTC). The two
// endpoints are queried in series; the cost call is skipped if usage fails.
func (c *Client) FetchWindow(ctx context.Context, start, end time.Time) (CombinedWindow, RateLimitInfo, error) {
	var combined CombinedWindow
	var rl RateLimitInfo

	usageBody, urli, err := c.get(ctx, UsagePath, windowQuery(start, end))
	rl = mergeRateLimit(rl, urli)
	if err != nil {
		return combined, rl, err
	}
	if err := json.Unmarshal(usageBody, &combined.Usage); err != nil {
		return combined, rl, fmt.Errorf("openaiclient: decode usage: %w", err)
	}

	costBody, crli, err := c.get(ctx, CostPath, windowQuery(start, end))
	rl = mergeRateLimit(rl, crli)
	if err != nil {
		return combined, rl, err
	}
	if err := json.Unmarshal(costBody, &combined.Cost); err != nil {
		return combined, rl, fmt.Errorf("openaiclient: decode cost: %w", err)
	}

	return combined, rl, nil
}

func mergeRateLimit(a, b RateLimitInfo) RateLimitInfo {
	if b.Remaining != 0 || b.HitRateLimit || b.ResetAfter != 0 || b.RequestID != "" {
		// Prefer the most recent non-zero info.
		if b.RequestID != "" {
			a.RequestID = b.RequestID
		}
		if b.Remaining != 0 {
			a.Remaining = b.Remaining
		}
		if b.ResetAfter != 0 {
			a.ResetAfter = b.ResetAfter
		}
		if b.HitRateLimit {
			a.HitRateLimit = true
		}
	}
	return a
}

// get performs one GET with retries + circuit breaker. Returns the raw
// response body, the parsed rate-limit info, and an error.
func (c *Client) get(ctx context.Context, path string, query url.Values) ([]byte, RateLimitInfo, error) {
	if c.circuitIsOpen() {
		return nil, RateLimitInfo{}, ErrCircuitOpen
	}

	u := c.cfg.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var rl RateLimitInfo
	var lastErr error

	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		body, info, status, err := c.doOnce(ctx, u)
		rl = mergeRateLimit(rl, info)

		switch {
		case err != nil:
			// Network / transport failure — retry with backoff up to budget.
			lastErr = err
			c.recordFailure()
			if !sleepCtx(ctx, backoff(attempt)) {
				return nil, rl, ctx.Err()
			}
			continue
		case status == http.StatusOK:
			c.recordSuccess()
			return body, rl, nil
		case status == http.StatusTooManyRequests:
			rl.HitRateLimit = true
			lastErr = ErrRateLimited
			// Respect Retry-After if set, otherwise exponential backoff.
			wait := backoff(attempt)
			if rl.ResetAfter > 0 && rl.ResetAfter < 2*time.Minute {
				wait = rl.ResetAfter
			}
			if !sleepCtx(ctx, wait) {
				return nil, rl, ctx.Err()
			}
			continue
		case status >= 500:
			lastErr = fmt.Errorf("%w: status=%d", ErrServerError, status)
			c.recordFailure()
			if !sleepCtx(ctx, backoff(attempt)) {
				return nil, rl, ctx.Err()
			}
			continue
		case status >= 400:
			// 4xx other than 429 are NOT retried — config / auth problem.
			c.recordSuccess() // not a server-side fault, do not trip the breaker
			return nil, rl, fmt.Errorf("openaiclient: %s status=%d", path, status)
		}
	}

	return nil, rl, lastErr
}

// doOnce performs a single HTTP attempt and returns body, parsed rate-limit
// headers, status code, and any transport error.
func (c *Client) doOnce(ctx context.Context, fullURL string) ([]byte, RateLimitInfo, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, RateLimitInfo{}, 0, fmt.Errorf("openaiclient: build request: %w", err)
	}
	// Authorization header is set per request and never logged. The Client
	// struct holds the key in memory only and forwards it raw to the
	// transport; we never echo it back.
	req.Header.Set(HeaderAuth, "Bearer "+c.cfg.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		// Scrub any accidental key echo from the error (defense in depth).
		return nil, RateLimitInfo{}, 0, scrubKey(err, c.cfg.APIKey)
	}
	defer func() { _ = resp.Body.Close() }()

	rl := parseRateLimit(resp.Header)

	if resp.StatusCode != http.StatusOK {
		// Drain body for diagnostics but cap at 4KiB to avoid blowing memory.
		_, _ = io.CopyN(io.Discard, resp.Body, 4096)
		return nil, rl, resp.StatusCode, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // 16 MiB safety cap
	if err != nil {
		return nil, rl, resp.StatusCode, scrubKey(err, c.cfg.APIKey)
	}
	return body, rl, resp.StatusCode, nil
}

func parseRateLimit(h http.Header) RateLimitInfo {
	info := RateLimitInfo{Remaining: -1}
	if v := h.Get(HeaderRateLimit); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			info.Remaining = n
		}
	}
	if v := h.Get(HeaderRateReset); v != "" {
		// OpenAI returns Go-style duration strings ("8m20s") or bare seconds.
		if d, err := time.ParseDuration(v); err == nil {
			info.ResetAfter = d
		} else if secs, err := strconv.ParseFloat(v, 64); err == nil {
			info.ResetAfter = time.Duration(secs * float64(time.Second))
		}
	}
	if v := h.Get(HeaderRequestID); v != "" {
		info.RequestID = v
	}
	return info
}

func windowQuery(start, end time.Time) url.Values {
	q := url.Values{}
	q.Set("start_time", strconv.FormatInt(start.UTC().Unix(), 10))
	q.Set("end_time", strconv.FormatInt(end.UTC().Unix(), 10))
	q.Set("bucket_width", "1d")
	return q
}

// backoff computes an exponential delay with full jitter, capped at 30s.
//
// attempt is 0-indexed. attempt=0 => ~[0, 1s); attempt=4 => ~[0, 16s).
func backoff(attempt int) time.Duration {
	base := time.Second << uint(attempt)
	if base > 30*time.Second {
		base = 30 * time.Second
	}
	// rand.Int63n requires n>0; base in ns is always > 0 here.
	return time.Duration(rand.Int63n(int64(base)))
}

// sleepCtx waits for d or until ctx is cancelled. Returns false on cancel.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (c *Client) circuitIsOpen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.consecutiveFails < c.cfg.CircuitBreakerThreshold {
		return false
	}
	// Breaker has tripped; check cooldown.
	if c.cfg.Now().Sub(c.circuitOpenedAt) >= c.cfg.CircuitBreakerCooldown {
		// Half-open: allow one probe by resetting the counter to threshold-1.
		c.consecutiveFails = c.cfg.CircuitBreakerThreshold - 1
		return false
	}
	return true
}

func (c *Client) recordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveFails++
	if c.consecutiveFails == c.cfg.CircuitBreakerThreshold {
		c.circuitOpenedAt = c.cfg.Now()
	}
}

func (c *Client) recordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveFails = 0
	c.circuitOpenedAt = time.Time{}
}

// CircuitOpen returns true if the breaker is currently open. Exposed for
// metrics + tests.
func (c *Client) CircuitOpen() bool {
	return c.circuitIsOpen()
}

// scrubKey removes any literal occurrence of `key` from err.Error() defense
// in depth in case a transport library ever embeds the URL or header in an
// error. Returns the original error when key is empty.
func scrubKey(err error, key string) error {
	if err == nil || key == "" {
		return err
	}
	msg := err.Error()
	if !strings.Contains(msg, key) {
		return err
	}
	return errors.New(strings.ReplaceAll(msg, key, "***redacted***"))
}
