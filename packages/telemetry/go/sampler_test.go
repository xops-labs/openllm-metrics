// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package telemetry

import (
	"strings"
	"testing"
)

func TestBuildSampler_DefaultRatios(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		env       Environment
		ratio     float64
		wantMatch string
	}{
		// Ratios of 1.0 collapse to AlwaysOnSampler in the OTel SDK.
		{"dev defaults to always-on", EnvDev, 0, "AlwaysOn"},
		{"staging defaults to always-on", EnvStaging, 0, "AlwaysOn"},
		{"production defaults to 10 percent", EnvProduction, 0, "0.1"},
		{"explicit ratio overrides env", EnvProduction, 0.5, "0.5"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := ServiceConfig{
				ServiceName:    "svc",
				ServiceVersion: "0.0.0",
				Environment:    tc.env,
				SamplingRatio:  tc.ratio,
			}
			s := buildSampler(cfg)
			desc := s.Description()
			if !strings.Contains(desc, tc.wantMatch) {
				t.Fatalf("env=%s ratio=%v sampler=%q want match %q", tc.env, tc.ratio, desc, tc.wantMatch)
			}
		})
	}
}

func TestServiceConfig_Validate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cfg     ServiceConfig
		wantErr bool
	}{
		{"missing name", ServiceConfig{ServiceVersion: "1", Environment: EnvDev}, true},
		{"missing version", ServiceConfig{ServiceName: "svc", Environment: EnvDev}, true},
		{"invalid env", ServiceConfig{ServiceName: "svc", ServiceVersion: "1", Environment: "qa"}, true},
		{"ratio > 1", ServiceConfig{ServiceName: "svc", ServiceVersion: "1", Environment: EnvDev, SamplingRatio: 2}, true},
		{"valid", ServiceConfig{ServiceName: "svc", ServiceVersion: "1", Environment: EnvStaging}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.cfg.validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("validate() err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
