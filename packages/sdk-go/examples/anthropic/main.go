// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Example: instrument an Anthropic Messages call using the official
// github.com/anthropics/anthropic-sdk-go community client. The SDK never sees
// prompt or completion text — only counts pulled from message.Usage.
package main

import (
	"context"
	"log"

	"github.com/anthropics/anthropic-sdk-go"
	openllm "github.com/yasvanth511/openllm-metrics-oss/packages/sdk-go"
)

func main() {
	ctx := context.Background()

	shutdown, err := openllm.Init(ctx, openllm.Options{
		ServiceName:      "example-anthropic",
		ServiceVersion:   "0.1.0",
		ExporterEndpoint: "http://localhost:4318",
	})
	if err != nil {
		log.Fatalf("openllm.Init: %v", err)
	}
	defer shutdown(ctx)

	client := anthropic.NewClient()
	if err := messageOnce(ctx, client); err != nil {
		log.Fatalf("message: %v", err)
	}
}

func messageOnce(ctx context.Context, client *anthropic.Client) error {
	model := "claude-3-5-sonnet-latest"

	op, ctx := openllm.StartLlmCall(ctx, openllm.CallOptions{
		Provider: "anthropic",
		Model:    model,
		Route:    "anthropic/us",
		Tenant:   "acme",
		Team:     "platform",
		App:      "chatbot",
		Env:      "production",
		Project:  "alpha",
	})
	defer op.End()

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.F(model),
		MaxTokens: anthropic.F(int64(64)),
		Messages: anthropic.F([]anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("ping")),
		}),
	})
	if err != nil {
		op.SetErrorKind("provider_error")
		return err
	}
	op.SetPromptTokens(msg.Usage.InputTokens)
	op.SetCompletionTokens(msg.Usage.OutputTokens)
	return nil
}
