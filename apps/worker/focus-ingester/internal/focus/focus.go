// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package focus types the subset of FOCUS (FinOps Foundation Open Cost &
// Usage Specification) fields the ingester needs to consume from the
// upstream llm-usage-exporter `/focus.json` endpoint.
//
// FOCUS itself is broad; we model only the fields the reconciliation
// pipeline reads. The full record is round-tripped to Postgres as JSONB
// so nothing is lost — extracted columns exist for query speed only.
package focus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// Record is the subset of FOCUS fields the ingester reads. The raw payload
// is also kept on Raw so the database row can preserve every field upstream
// emitted.
type Record struct {
	BillingAccountID  string    `json:"BillingAccountId"`
	InvoiceID         string    `json:"InvoiceId,omitempty"`
	ChargeCategory    string    `json:"ChargeCategory,omitempty"`
	ServiceName       string    `json:"ServiceName,omitempty"`
	BilledCost        float64   `json:"BilledCost"`
	ListCost          float64   `json:"ListCost,omitempty"`
	PricingCurrency   string    `json:"PricingCurrency"`
	ChargePeriodStart time.Time `json:"ChargePeriodStart"`
	ChargePeriodEnd   time.Time `json:"ChargePeriodEnd"`

	// Provider-specific tags carried by the upstream exporter. These are
	// the join keys against control_plane.label_mappings.
	Provider         string `json:"Provider,omitempty"`
	Model            string `json:"Model,omitempty"`
	TenantExternalID string `json:"TenantExternalId,omitempty"`
	TenancyID        string `json:"TenancyId,omitempty"`

	// Raw is the unmodified JSON object as returned by upstream. The
	// ingester stores this in the focus_records.raw_focus JSONB column.
	Raw json.RawMessage `json:"-"`
}

// Client wraps the HTTP client used to fetch the FOCUS payload.
type Client struct {
	URL  string
	HTTP *http.Client
}

// New returns a Client with the supplied timeout.
func New(url string, timeout time.Duration) *Client {
	return &Client{
		URL:  url,
		HTTP: &http.Client{Timeout: timeout},
	}
}

// Fetch GETs the upstream /focus.json endpoint and returns the decoded
// records. Each record's Raw field captures the original JSON object so
// the database row can preserve any field upstream emits beyond the typed
// subset above.
func (c *Client) Fetch(ctx context.Context) ([]Record, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("focus: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("focus: GET %s: %w", c.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.CopyN(io.Discard, resp.Body, 1<<14)
		return nil, fmt.Errorf("focus: GET %s: status %d", c.URL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20)) // 32 MiB cap
	if err != nil {
		return nil, fmt.Errorf("focus: read body: %w", err)
	}

	// Try envelope form first; fall back to a bare array.
	var env struct {
		Records []json.RawMessage `json:"records"`
	}
	var rawRecords []json.RawMessage
	if err := json.Unmarshal(body, &env); err == nil && len(env.Records) > 0 {
		rawRecords = env.Records
	} else {
		var arr []json.RawMessage
		if aErr := json.Unmarshal(body, &arr); aErr != nil {
			return nil, fmt.Errorf("focus: decode body: %w", err)
		}
		rawRecords = arr
	}

	out := make([]Record, 0, len(rawRecords))
	for i, raw := range rawRecords {
		var r Record
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("focus: decode record %d: %w", i, err)
		}
		r.Raw = raw
		out = append(out, r)
	}
	return out, nil
}

// MinorUnits converts a USD-denominated float to integer minor units
// (1 unit = 0.01 USD). Records with a non-USD currency are returned as 0
// to keep the canonical event sane — F017 will own cross-currency.
func MinorUnits(amount float64, currency string) int64 {
	if !strings.EqualFold(currency, "USD") && currency != "" {
		return 0
	}
	if amount < 0 {
		return 0
	}
	return int64(math.Round(amount * 100.0))
}

// SourceEventID derives the stable handle for a FOCUS record. Stability
// across re-polls is required so the focus_records ingest is idempotent at
// the source_event_id level.
func SourceEventID(r Record) string {
	return fmt.Sprintf("focus:%s:%s:%s:%s",
		r.BillingAccountID,
		r.ChargePeriodStart.UTC().Format(time.RFC3339),
		r.ChargePeriodEnd.UTC().Format(time.RFC3339),
		r.ServiceName,
	)
}
