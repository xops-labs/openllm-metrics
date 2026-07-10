// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package policy

import (
	"context"
	"testing"
)

func TestNoop_AlwaysAllows(t *testing.T) {
	e := Noop()
	req := Request{Tenant: "acme", Provider: "openai", Model: "gpt-4o-mini"}

	d, err := e.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if d.Verdict != VerdictAllow {
		t.Errorf("Verdict = %v, want %v", d.Verdict, VerdictAllow)
	}
	if d.RuleVersion != "oss-default" {
		t.Errorf("RuleVersion = %q, want oss-default", d.RuleVersion)
	}
}
