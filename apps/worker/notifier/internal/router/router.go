// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package router resolves an inbound alert event to the set of channels it
// should fan out to.
//
// Matching is a simple set-membership check on the rule's `match` document:
//
//	{
//	  "severity": ["critical", "high"],
//	  "source":   ["slo", "quota"]
//	}
//
// An empty / missing array means "any value matches", consistent with the
// scope semantics in the F029 policy schema. A rule with no match keys at all
// matches every alert in its tenant; that is intentional (a tenant-wide
// "fan-out everything to one channel" rule is a common ops pattern).
package router

import (
	"encoding/json"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/notifier/internal/store"
)

// Alert is the subset of the inbound alert event the router operates on.
// See packages/contracts/notifications/v1/alert-event.schema.json for the
// full shape; sinks re-decode the raw payload themselves.
type Alert struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Severity string `json:"severity"`
	Source   string `json:"source"`
}

// Match is the on-disk shape of notification_rules.match.
type Match struct {
	Severity []string `json:"severity,omitempty"`
	Source   []string `json:"source,omitempty"`
}

// Dispatch is one rule × channel fan-out target.
type Dispatch struct {
	Rule    store.Rule
	Channel store.Channel
}

// Route returns every (rule, channel) pair that should receive the alert.
// Channels referenced by a rule but not present in chanMap (soft-deleted,
// foreign-key-broken, etc.) are silently skipped.
func Route(a Alert, rules []store.Rule, chanMap map[string]store.Channel) []Dispatch {
	var out []Dispatch
	for _, r := range rules {
		if !ruleMatches(r, a) {
			continue
		}
		for _, cid := range r.ChannelIDs {
			ch, ok := chanMap[cid]
			if !ok {
				continue
			}
			out = append(out, Dispatch{Rule: r, Channel: ch})
		}
	}
	return out
}

func ruleMatches(r store.Rule, a Alert) bool {
	if len(r.Match) == 0 {
		return true
	}
	var m Match
	if err := json.Unmarshal(r.Match, &m); err != nil {
		// Malformed match document → treat as non-matching to fail closed.
		return false
	}
	if !inSet(m.Severity, a.Severity) {
		return false
	}
	if !inSet(m.Source, a.Source) {
		return false
	}
	return true
}

// inSet returns true when allowed is empty (any value matches) or contains v.
func inSet(allowed []string, v string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, x := range allowed {
		if x == v {
			return true
		}
	}
	return false
}
