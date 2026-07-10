// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// demo-generator emits synthetic, schema-conformant telemetry onto the
// streaming bus so the full OpenLLM Metrics stack lights up with zero
// provider API keys. It is a demo aid, not a product service — compose gates
// it behind `--profile demo`.
//
// Three legs, mirroring the real capture surfaces:
//
//   - llm.runtime.normalized  — continuous proxy-mode request events
//     (drives llm_requests/tokens/errors/retries counters and, via the
//     cost-mapper, llm.cost.estimated).
//   - llm.usage.normalized    — periodic pull-mode billing rollups
//     (drives llm_cost_usd_total and the FinOps dashboards).
//   - llm.usage.reconciled    — periodic billing-truth windows with a few
//     percent of drift (drives the reconciler joins and the
//     llm_reconciliation_* drift series).
//
// Privacy invariant holds even in the demo: events carry counts, timings,
// labels, and costs only — there is no prompt or completion text to leak
// because none is ever synthesized.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"
	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"
)

// sourceService identifies demo traffic to downstream consumers and
// dashboards — operators can always tell synthetic signal from real signal.
const sourceService = "examples/demo-generator"

// Acme Corp demo tenant from platform/db/seeds/001_demo_data.sql — the admin
// console's default tenant, so the analytics screens show this traffic
// without any tenant switching.
const defaultTenant = "00000000-0000-0000-0002-000000000001"

type config struct {
	Brokers            []string
	Tenant             string
	RPS                float64
	UsageInterval      time.Duration
	ReconciledInterval time.Duration
	ListenAddr         string
}

func loadConfig() config {
	cfg := config{
		Brokers:            []string{"redpanda:9092"},
		Tenant:             defaultTenant,
		RPS:                4,
		UsageInterval:      30 * time.Second,
		ReconciledInterval: 60 * time.Second,
		ListenAddr:         ":8089",
	}
	if v := os.Getenv("OLM_DEMO_BROKERS"); v != "" {
		cfg.Brokers = strings.Split(v, ",")
	}
	if v := os.Getenv("OLM_DEMO_TENANT"); v != "" {
		cfg.Tenant = v
	}
	if v := os.Getenv("OLM_DEMO_RPS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 100 {
			cfg.RPS = f
		}
	}
	if v := os.Getenv("OLM_DEMO_USAGE_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 5*time.Second {
			cfg.UsageInterval = d
		}
	}
	if v := os.Getenv("OLM_DEMO_RECONCILED_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 10*time.Second {
			cfg.ReconciledInterval = d
		}
	}
	if v := os.Getenv("OLM_DEMO_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	return cfg
}

// counters is the tiny self-observability surface served on /healthz.
type counters struct {
	Runtime    atomic.Int64
	Usage      atomic.Int64
	Reconciled atomic.Int64
	Failed     atomic.Int64
}

// rollupKey buckets runtime traffic for the periodic pull-mode legs.
type rollupKey struct {
	workloadIdx int
}

type rollupAcc struct {
	InputTokens  int64
	OutputTokens int64
	Requests     int64
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := loadConfig()

	producer, err := busclient.NewProducer(busclient.Config{
		Brokers:  cfg.Brokers,
		ClientID: "openllm-demo-generator",
	})
	if err != nil {
		logger.Error("demo-generator: producer init failed", "error", err)
		os.Exit(1)
	}
	defer producer.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var c counters
	go serveHealth(ctx, cfg.ListenAddr, &c, logger)

	logger.Info("demo-generator: starting",
		"brokers", strings.Join(cfg.Brokers, ","),
		"tenant", cfg.Tenant,
		"rps", cfg.RPS,
		"usage_interval", cfg.UsageInterval.String(),
		"reconciled_interval", cfg.ReconciledInterval.String(),
	)

	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))

	emit := func(topic, eventID, tenant string, payload any) {
		raw, err := json.Marshal(payload)
		if err != nil {
			c.Failed.Add(1)
			logger.Error("demo-generator: marshal failed", "topic", topic, "error", err)
			return
		}
		if err := producer.ProduceEvent(ctx, topic, eventID, tenant, raw); err != nil {
			c.Failed.Add(1)
			logger.Warn("demo-generator: produce failed", "topic", topic, "error", err)
		}
	}

	// Rolling accumulators for the pull-mode legs. Single-goroutine event
	// loop below — no locking needed.
	usageAcc := map[rollupKey]*rollupAcc{}
	reconAcc := map[rollupKey]*rollupAcc{}
	usageStart := time.Now()
	reconStart := time.Now()

	interval := time.Duration(float64(time.Second) / cfg.RPS)
	requestTimer := time.NewTimer(interval)
	defer requestTimer.Stop()
	usageTicker := time.NewTicker(cfg.UsageInterval)
	defer usageTicker.Stop()
	reconTicker := time.NewTicker(cfg.ReconciledInterval)
	defer reconTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("demo-generator: shutting down",
				"runtime_events", c.Runtime.Load(),
				"usage_events", c.Usage.Load(),
				"reconciled_events", c.Reconciled.Load(),
				"failed", c.Failed.Load(),
			)
			return

		case now := <-requestTimer.C:
			idx := pickWorkloadIdx(rng)
			w := &demoWorkloads[idx]
			ev := synthesizeRuntime(rng, w, cfg.Tenant, now)
			emit(telemetrycontracts.TopicRuntimeNormalized, ev.EventID, ev.Tenant, ev)
			c.Runtime.Add(1)

			if ev.InputTokens != nil {
				key := rollupKey{workloadIdx: idx}
				for _, acc := range []map[rollupKey]*rollupAcc{usageAcc, reconAcc} {
					a := acc[key]
					if a == nil {
						a = &rollupAcc{}
						acc[key] = a
					}
					a.InputTokens += int64(*ev.InputTokens)
					a.OutputTokens += int64(*ev.OutputTokens)
					a.Requests++
				}
			}
			// ±50% jitter so the series do not look machine-stamped.
			next := time.Duration(float64(interval) * (0.5 + rng.Float64()))
			requestTimer.Reset(next)

		case now := <-usageTicker.C:
			for key, a := range usageAcc {
				w := &demoWorkloads[key.workloadIdx]
				ev := usageRollup(w, a, cfg.Tenant, usageStart, now)
				emit(telemetrycontracts.TopicUsageNormalized, ev.EventID, ev.Tenant, ev)
				c.Usage.Add(1)
			}
			usageAcc = map[rollupKey]*rollupAcc{}
			usageStart = now

		case now := <-reconTicker.C:
			for key, a := range reconAcc {
				w := &demoWorkloads[key.workloadIdx]
				ev := reconciledRollup(rng, w, a, cfg.Tenant, reconStart, now)
				emit(telemetrycontracts.TopicUsageReconciled, ev.EventID, ev.Tenant, ev)
				c.Reconciled.Add(1)
			}
			reconAcc = map[rollupKey]*rollupAcc{}
			reconStart = now
		}
	}
}

// pickWorkloadIdx returns a weighted-random workload index so rollups can
// key on it.
func pickWorkloadIdx(rng *rand.Rand) int {
	n := rng.IntN(totalWeight)
	for i := range demoWorkloads {
		n -= demoWorkloads[i].Weight
		if n < 0 {
			return i
		}
	}
	return len(demoWorkloads) - 1
}

// usageRollup converts one accumulated window into a pull-mode
// llm.usage.normalized event.
func usageRollup(w *workload, a *rollupAcc, tenant string, start, end time.Time) UsageEvent {
	cost := w.costUSD(a.InputTokens, a.OutputTokens)
	return UsageEvent{
		SchemaVersion:     "1",
		EventID:           uuid.NewString(),
		SourceEventID:     uuid.NewString(),
		SourceMode:        "pull",
		Source:            "exporter",
		SourceService:     sourceService,
		Provider:          w.Provider,
		Model:             w.Model,
		Operation:         w.Operation,
		Tenant:            tenant,
		Team:              w.Team,
		App:               w.App,
		Env:               w.Env,
		Region:            w.Region,
		InputTokens:       a.InputTokens,
		OutputTokens:      a.OutputTokens,
		TotalTokens:       a.InputTokens + a.OutputTokens,
		CostUSDMinorUnits: usdToMinor(cost),
		RequestCount:      a.Requests,
		PeriodStart:       start.UTC().Format(time.RFC3339Nano),
		PeriodEnd:         end.UTC().Format(time.RFC3339Nano),
		NormalizedAt:      end.UTC().Format(time.RFC3339Nano),
	}
}

// reconciledRollup converts one accumulated window into a billing-truth
// llm.usage.reconciled event. The reconciled amount drifts 0–6% above list to
// keep the drift panels non-zero, the way real provider invoices do.
func reconciledRollup(rng *rand.Rand, w *workload, a *rollupAcc, tenant string, start, end time.Time) ReconciledEvent {
	list := w.costUSD(a.InputTokens, a.OutputTokens)
	drift := 1.0 + rng.Float64()*0.06
	return ReconciledEvent{
		SchemaVersion:               "1",
		EventID:                     uuid.NewString(),
		SourceEventID:               uuid.NewString(),
		Source:                      "exporter",
		SourceService:               sourceService,
		Provider:                    w.Provider,
		Model:                       w.Model,
		Tenant:                      tenant,
		Team:                        w.Team,
		App:                         w.App,
		Env:                         w.Env,
		Region:                      w.Region,
		BillingAccountID:            "acme-demo-billing",
		ServiceName:                 w.Provider,
		ChargeCategory:              "usage",
		ReconciledCostUSDMinorUnits: usdToMinor(list * drift),
		ListCostUSDMinorUnits:       usdToMinor(list),
		PricingCurrency:             "USD",
		PeriodStart:                 start.UTC().Format(time.RFC3339Nano),
		PeriodEnd:                   end.UTC().Format(time.RFC3339Nano),
		ReconciledAt:                end.UTC().Format(time.RFC3339Nano),
	}
}

// usdToMinor converts a USD float to integer minor units (cents), rounding
// half away from zero. F008 §10: integer minor units in payloads, floats only
// in the metric registry.
func usdToMinor(usd float64) int64 {
	return int64(usd*100 + 0.5)
}

// serveHealth exposes /healthz with emit counters — enough for compose `ps`
// and a curl to confirm the demo is flowing.
func serveHealth(ctx context.Context, addr string, c *counters, logger *slog.Logger) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w,
			`{"status":"ok","runtime_events":%d,"usage_events":%d,"reconciled_events":%d,"failed":%d}`,
			c.Runtime.Load(), c.Usage.Load(), c.Reconciled.Load(), c.Failed.Load(),
		)
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		// Immediate close is fine — this server only answers /healthz.
		_ = srv.Close()
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("demo-generator: health server failed", "error", err)
	}
}
