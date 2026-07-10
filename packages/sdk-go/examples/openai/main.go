// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Example: instrument an OpenAI chat completion using the community
// github.com/sashabaranov/go-openai client. The SDK never sees prompt or
// completion text — only counts pulled from response.Usage.
package main

import (
	"context"
	"log"
	"os"

	openllm "github.com/yasvanth511/openllm-metrics-oss/packages/sdk-go"
	openai "github.com/sashabaranov/go-openai"
)

func main() {
	ctx := context.Background()

	shutdown, err := openllm.Init(ctx, openllm.Options{
		ServiceName:      "example-openai",
		ServiceVersion:   "0.1.0",
		ExporterEndpoint: "http://localhost:4318",
	})
	if err != nil {
		log.Fatalf("openllm.Init: %v", err)
	}
	defer shutdown(ctx)

	client := openai.NewClient(os.Getenv("OPENAI_API_KEY"))
	model := openai.GPT4oMini

	if err := chatOnce(ctx, client, model); err != nil {
		log.Fatalf("chat: %v", err)
	}
}

func chatOnce(ctx context.Context, client *openai.Client, model string) error {
	op, ctx := openllm.StartLlmCall(ctx, openllm.CallOptions{
		Provider: "openai",
		Model:    model,
		Route:    "openai/us",
		Tenant:   "acme",
		Team:     "platform",
		App:      "chatbot",
		Env:      "production",
		Project:  "alpha",
	})
	defer op.End()

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: "ping"},
		},
	})
	if err != nil {
		op.SetErrorKind("provider_error")
		return err
	}
	op.SetPromptTokens(int64(resp.Usage.PromptTokens))
	op.SetCompletionTokens(int64(resp.Usage.CompletionTokens))
	return nil
}
