// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package ingester wires the FOCUS client, the mapping store, the FOCUS
// writer, and the bus producer into a single poll cycle.
//
// One cycle: fetch /focus.json, for each record resolve the mapping,
// insert into focus_records, emit llm.usage.reconciled to the bus.
package ingester

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/focus-ingester/internal/focus"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/focus-ingester/internal/store"
)

// SourceService identifies this ingester to downstream consumers.
const SourceService = "apps/worker/focus-ingester"

// ReconciledEvent matches the F008 llm.usage.reconciled payload byte-for-byte.
type ReconciledEvent struct {
	SchemaVersion               string `json:"schema_version"`
	EventID                     string `json:"event_id"`
	SourceEventID               string `json:"source_event_id"`
	Source                      string `json:"source"`
	SourceService               string `json:"source_service"`
	Provider                    string `json:"provider"`
	Model                       string `json:"model"`
	Tenant                      string `json:"tenant"`
	Team                        string `json:"team"`
	App                         string `json:"app,omitempty"`
	Env                         string `json:"env"`
	Project                     string `json:"project,omitempty"`
	Region                      string `json:"region,omitempty"`
	BillingAccountID            string `json:"billing_account_id"`
	InvoiceID                   string `json:"invoice_id,omitempty"`
	ServiceName                 string `json:"service_name,omitempty"`
	ChargeCategory              string `json:"charge_category,omitempty"`
	ReconciledCostUSDMinorUnits int64  `json:"reconciled_cost_usd_minor_units"`
	ListCostUSDMinorUnits       int64  `json:"list_cost_usd_minor_units,omitempty"`
	PricingCurrency             string `json:"pricing_currency,omitempty"`
	PeriodStart                 string `json:"period_start"`
	PeriodEnd                   string `json:"period_end"`
	ReconciledAt                string `json:"reconciled_at"`
}

// Emitter is the bus producer surface the ingester depends on.
type Emitter interface {
	Emit(ctx context.Context, ev ReconciledEvent) error
}

// Ingester is the cycle runner.
type Ingester struct {
	client  *focus.Client
	store   store.Store
	emitter Emitter
	now     func() time.Time
}

// Result reports per-cycle counts the metrics package consumes.
type Result struct {
	Fetched   int
	Persisted int
	Emitted   int
	Unmapped  int
	Dropped   int
}

// New constructs an Ingester.
func New(client *focus.Client, s store.Store, e Emitter) *Ingester {
	return &Ingester{
		client:  client,
		store:   s,
		emitter: e,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// RunOnce executes one poll cycle. Per-record errors are counted but do not
// abort the cycle; only fetch errors return early.
func (i *Ingester) RunOnce(ctx context.Context) (Result, error) {
	records, err := i.client.Fetch(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("ingester: fetch: %w", err)
	}

	res := Result{Fetched: len(records)}
	now := i.now()
	nowStr := now.Format(time.RFC3339)

	for _, r := range records {
		mapping, err := i.store.Lookup(ctx, store.Key{
			Provider:         r.Provider,
			TenantExternalID: r.TenantExternalID,
			TenancyID:        r.TenancyID,
		})
		if err != nil {
			// Database error — skip this record; the next cycle will retry
			// because focus_records is keyed by source_event_id, not by
			// auto-id.
			res.Dropped++
			continue
		}

		tenantID, tenant, team, app, env, project, region := i.resolve(mapping)
		if !mapping.Found {
			res.Unmapped++
		}
		if tenantID == "" || tenant == "" {
			// Without a tenant_id the focus_records write would violate the
			// NOT NULL constraint. Drop and let operators fix the mapping.
			res.Dropped++
			continue
		}

		costMinor := focus.MinorUnits(r.BilledCost, r.PricingCurrency)
		listMinor := focus.MinorUnits(r.ListCost, r.PricingCurrency)

		raw := r.Raw
		if len(raw) == 0 {
			// Defensive: marshal the typed record back so raw_focus is never
			// NULL. Production payloads always have Raw set by focus.Fetch.
			raw, _ = json.Marshal(r)
		}

		row := store.FocusRow{
			TenantID:                    tenantID,
			SourceEventID:               focus.SourceEventID(r),
			Provider:                    canonicalProvider(r.Provider),
			Model:                       strings.ToLower(strings.TrimSpace(r.Model)),
			BillingAccountID:            r.BillingAccountID,
			InvoiceID:                   r.InvoiceID,
			ServiceName:                 r.ServiceName,
			ChargeCategory:              r.ChargeCategory,
			ReconciledCostUSDMinorUnits: costMinor,
			ListCostUSDMinorUnits:       listMinor,
			PricingCurrency:             defaultCurrency(r.PricingCurrency),
			PeriodStart:                 r.ChargePeriodStart.UTC(),
			PeriodEnd:                   r.ChargePeriodEnd.UTC(),
			RawFocus:                    raw,
		}
		if err := i.store.InsertFocus(ctx, row); err != nil {
			res.Dropped++
			continue
		}
		res.Persisted++

		ev := ReconciledEvent{
			SchemaVersion:               telemetrycontracts.SchemaVersion,
			EventID:                     uuid.NewString(),
			SourceEventID:               row.SourceEventID,
			Source:                      telemetrycontracts.SourceExporter,
			SourceService:               SourceService,
			Provider:                    row.Provider,
			Model:                       row.Model,
			Tenant:                      tenant,
			Team:                        team,
			App:                         app,
			Env:                         env,
			Project:                     project,
			Region:                      region,
			BillingAccountID:            row.BillingAccountID,
			InvoiceID:                   row.InvoiceID,
			ServiceName:                 row.ServiceName,
			ChargeCategory:              row.ChargeCategory,
			ReconciledCostUSDMinorUnits: row.ReconciledCostUSDMinorUnits,
			ListCostUSDMinorUnits:       row.ListCostUSDMinorUnits,
			PricingCurrency:             row.PricingCurrency,
			PeriodStart:                 row.PeriodStart.Format(time.RFC3339),
			PeriodEnd:                   row.PeriodEnd.Format(time.RFC3339),
			ReconciledAt:                nowStr,
		}
		if err := i.emitter.Emit(ctx, ev); err != nil {
			// The row was persisted; if the bus is down the row will still
			// be in Postgres. The reconciliation drift pipeline can rebuild
			// the bus topic by re-emitting from focus_records.
			continue
		}
		res.Emitted++
	}
	return res, nil
}

func (i *Ingester) resolve(m store.Mapping) (tenantID, tenant, team, app, env, project, region string) {
	// Unmapped records yield all-empty values (store.Lookup returns a zero
	// Mapping when no row matches). Operators MUST seed a row in
	// label_mappings for unmatched billing_account_ids; until they do, the
	// ingester records the unmapped counter and drops the row.
	return m.TenantID, m.TenantSlug, m.TeamSlug, m.AppSlug, m.CanonicalEnv, m.CanonicalProject, m.CanonicalRegion
}

// canonicalProvider lower-cases and trims the provider value. The schema
// enum is enforced upstream by the schemalint contract test.
func canonicalProvider(p string) string {
	return strings.ToLower(strings.TrimSpace(p))
}

func defaultCurrency(c string) string {
	if c == "" {
		return "USD"
	}
	return c
}
