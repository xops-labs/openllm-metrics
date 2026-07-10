// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package observer

import (
	"context"
	"testing"

	"github.com/yasvanth511/openllm-metrics-oss/apps/gateway/internal/busproducer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/gateway/internal/metrics"
)

// captureEmitter is a busproducer.Emitter test double that records the most
// recently emitted RuntimeEvent so the test can assert on its fields. Unlike
// the real BusEmitter it imposes no validation, so the observer's model
// precedence logic is exercised in isolation.
type captureEmitter struct {
	last   busproducer.RuntimeEvent
	called bool
}

func (c *captureEmitter) Emit(_ context.Context, ev busproducer.RuntimeEvent) error {
	c.last = ev
	c.called = true
	return nil
}

func (c *captureEmitter) Close() {}

// TestObserveCompletion_ModelCapture is the integration-level assertion for
// Phase 0: it confirms ObserveCompletion resolves the model through the
// documented precedence (ModelOverride > body-parsed model > rc.Model >
// "unknown") and stamps it onto the emitted RuntimeEvent.Model. The token
// parsers are covered separately in internal/usage; here the contract under
// test is that the captured model actually reaches the bus payload.
func TestObserveCompletion_ModelCapture(t *testing.T) {
	cases := []struct {
		name string
		rc   RequestContext
		comp Completion
		want string
	}{
		{
			// OpenAI carries the model in the response body, not the path,
			// so rc.Model is empty and the body-parsed model must win.
			name: "openai non-streaming body model",
			rc: RequestContext{
				Provider: ProviderOpenAI,
				Model:    "",
			},
			comp: Completion{
				StatusCode:   200,
				BytesSampled: []byte(`{"id":"chatcmpl-abc","object":"chat.completion","model":"gpt-4o","usage":{"prompt_tokens":42,"completion_tokens":128,"total_tokens":170}}`),
			},
			want: "gpt-4o",
		},
		{
			// Anthropic /v1/messages: top-level model in the body.
			name: "anthropic non-streaming body model",
			rc: RequestContext{
				Provider: ProviderAnthropic,
				Model:    "",
			},
			comp: Completion{
				StatusCode:   200,
				BytesSampled: []byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-3-5-sonnet-20241022","usage":{"input_tokens":58,"output_tokens":204}}`),
			},
			want: "claude-3-5-sonnet-20241022",
		},
		{
			// Azure responses are OpenAI-shaped; the model field still wins
			// over the (empty here) route deployment.
			name: "azure openai-shaped body model",
			rc: RequestContext{
				Provider: ProviderAzureOpenAI,
				Model:    "",
			},
			comp: Completion{
				StatusCode:   200,
				BytesSampled: []byte(`{"object":"chat.completion","model":"gpt-4o-mini","usage":{"prompt_tokens":11,"completion_tokens":22,"total_tokens":33}}`),
			},
			want: "gpt-4o-mini",
		},
		{
			// ModelOverride (the proxy streaming first-chunk hint) takes
			// precedence over a divergent body model. Anthropic streaming
			// relies on this because the trailing usage chunk omits the model.
			name: "model override wins over body",
			rc: RequestContext{
				Provider: ProviderAnthropic,
				Model:    "",
			},
			comp: Completion{
				StatusCode:    200,
				ModelOverride: "claude-3-opus",
				BytesSampled:  []byte(`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":204}}`),
			},
			want: "claude-3-opus",
		},
		{
			// Gemini surfaces the model in the URL path, so rc.Model is
			// pre-filled by Classify and the (empty) body must fall through.
			name: "path-based provider falls through to rc.Model",
			rc: RequestContext{
				Provider: ProviderGemini,
				Model:    "gemini-1.5-pro",
			},
			comp: Completion{
				StatusCode:   200,
				BytesSampled: nil,
			},
			want: "gemini-1.5-pro",
		},
		{
			// Nothing available anywhere: buildEvent stamps "unknown".
			name: "unknown fallback",
			rc: RequestContext{
				Provider: ProviderOpenAI,
				Model:    "",
			},
			comp: Completion{
				StatusCode:    200,
				ModelOverride: "",
				BytesSampled:  nil,
			},
			want: "unknown",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			emitter := &captureEmitter{}
			obs := New(metrics.New(), emitter, Defaults{})

			obs.ObserveCompletion(context.Background(), tc.rc, tc.comp)

			if !emitter.called {
				t.Fatalf("emitter.Emit was never called; no RuntimeEvent captured")
			}
			if got := emitter.last.Model; got != tc.want {
				t.Fatalf("RuntimeEvent.Model = %q, want %q", got, tc.want)
			}
		})
	}
}
