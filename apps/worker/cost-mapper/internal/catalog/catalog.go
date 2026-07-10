// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package catalog loads the per-provider pricing YAML files from
// platform/pricing/ and exposes a deterministic lookup keyed by
// (provider, model).
//
// The catalog is intentionally simple: tokens × per-1k rate = USD. There is
// no scoring weight, no routing rank, no policy threshold — anything more
// elaborate than this pure-function multiplication is OSS-deferred and lives
// in this repository.
//
// All rates are expressed per 1,000 tokens (the cross-provider convention).
// Output-only rates (e.g. embeddings) carry output_per_1k = 0.0.
package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// ErrModelNotPriced is returned when (provider, model) has no entry in the
// catalog. The caller should record the miss as a metric and skip the event
// rather than emit a $0 estimate, which would silently corrupt drift math.
var ErrModelNotPriced = errors.New("catalog: (provider, model) not priced")

// ErrInvalidCatalog is returned when a YAML file fails validation.
var ErrInvalidCatalog = errors.New("catalog: invalid pricing file")

// Rate is the per-1k-tokens price for a single (provider, model) pair.
type Rate struct {
	Provider    string
	Model       string
	InputPer1K  float64
	OutputPer1K float64
	Currency    string
	Approximate bool
}

// providerFile is the YAML shape for one platform/pricing/<provider>.yaml.
type providerFile struct {
	Provider      string  `yaml:"provider"`
	Currency      string  `yaml:"currency"`
	Unit          string  `yaml:"unit"`
	Approximate   bool    `yaml:"approximate"`
	EffectiveFrom string  `yaml:"effective_from"`
	Models        []entry `yaml:"models"`
}

type entry struct {
	Model       string  `yaml:"model"`
	InputPer1K  float64 `yaml:"input_per_1k"`
	OutputPer1K float64 `yaml:"output_per_1k"`
}

// Catalog holds the in-memory price table.
type Catalog struct {
	mu      sync.RWMutex
	rates   map[string]Rate // key: provider+"|"+model
	version string
}

// New constructs an empty Catalog. Call Load to populate.
func New() *Catalog {
	return &Catalog{rates: make(map[string]Rate, 32)}
}

// LoadDir reads every *.yaml file in dir and merges entries into the
// Catalog. A reload is atomic: the new map fully replaces the old one only
// when every file parses cleanly.
func (c *Catalog) LoadDir(dir string) error {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return fmt.Errorf("catalog: glob %s: %w", dir, err)
	}
	if len(matches) == 0 {
		return fmt.Errorf("%w: no *.yaml files in %s", ErrInvalidCatalog, dir)
	}
	sort.Strings(matches)

	next := make(map[string]Rate, 32)
	versionParts := make([]string, 0, len(matches))

	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("catalog: read %s: %w", path, err)
		}
		var pf providerFile
		if err := yaml.Unmarshal(raw, &pf); err != nil {
			return fmt.Errorf("catalog: parse %s: %w", path, err)
		}
		if pf.Provider == "" {
			return fmt.Errorf("%w: %s missing provider", ErrInvalidCatalog, path)
		}
		if pf.Unit != "" && pf.Unit != "per_1k_tokens" {
			return fmt.Errorf("%w: %s unsupported unit %q (only per_1k_tokens)",
				ErrInvalidCatalog, path, pf.Unit)
		}
		currency := pf.Currency
		if currency == "" {
			currency = "USD"
		}
		for _, e := range pf.Models {
			if e.Model == "" {
				return fmt.Errorf("%w: %s has model entry with empty name",
					ErrInvalidCatalog, path)
			}
			key := keyFor(pf.Provider, e.Model)
			if _, dup := next[key]; dup {
				return fmt.Errorf("%w: duplicate (%s, %s) across pricing files",
					ErrInvalidCatalog, pf.Provider, e.Model)
			}
			next[key] = Rate{
				Provider:    canonical(pf.Provider),
				Model:       canonical(e.Model),
				InputPer1K:  e.InputPer1K,
				OutputPer1K: e.OutputPer1K,
				Currency:    currency,
				Approximate: pf.Approximate,
			}
		}
		versionParts = append(versionParts, filepath.Base(path)+"@"+pf.EffectiveFrom)
	}

	c.mu.Lock()
	c.rates = next
	c.version = strings.Join(versionParts, ";")
	c.mu.Unlock()
	return nil
}

// Lookup returns the Rate for (provider, model). Both inputs are normalized
// (lowercase + trim) before lookup so callers do not have to.
func (c *Catalog) Lookup(provider, model string) (Rate, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.rates[keyFor(provider, model)]
	if !ok {
		return Rate{}, fmt.Errorf("%w: (%s, %s)", ErrModelNotPriced,
			canonical(provider), canonical(model))
	}
	return r, nil
}

// Version returns an opaque identifier for the currently loaded catalog
// (used to stamp drift records with the catalog state that produced them).
func (c *Catalog) Version() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.version
}

// Size returns the number of priced (provider, model) entries — useful for
// startup logging.
func (c *Catalog) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.rates)
}

func keyFor(provider, model string) string {
	return canonical(provider) + "|" + canonical(model)
}

func canonical(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
