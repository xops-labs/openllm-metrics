// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Example: instrument an AWS Bedrock Converse call using AWS SDK v2's
// bedrockruntime client. The SDK never sees prompt or completion text — only
// counts pulled from the response Usage struct.
package main

import (
	"context"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	openllm "github.com/yasvanth511/openllm-metrics-oss/packages/sdk-go"
)

func main() {
	ctx := context.Background()

	shutdown, err := openllm.Init(ctx, openllm.Options{
		ServiceName:      "example-bedrock",
		ServiceVersion:   "0.1.0",
		ExporterEndpoint: "http://localhost:4318",
	})
	if err != nil {
		log.Fatalf("openllm.Init: %v", err)
	}
	defer shutdown(ctx)

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("aws config: %v", err)
	}
	client := bedrockruntime.NewFromConfig(cfg)
	if err := converseOnce(ctx, client); err != nil {
		log.Fatalf("converse: %v", err)
	}
}

func converseOnce(ctx context.Context, client *bedrockruntime.Client) error {
	model := "anthropic.claude-3-5-sonnet-20240620-v1:0"

	op, ctx := openllm.StartLlmCall(ctx, openllm.CallOptions{
		Provider:      "bedrock",
		Model:         model,
		Route:         "bedrock/us-east-1",
		ServerAddress: "bedrock-runtime.us-east-1.amazonaws.com",
		Tenant:        "acme",
		Team:          "platform",
		App:           "chatbot",
		Env:           "production",
		Project:       "alpha",
	})
	defer op.End()

	resp, err := client.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId: aws.String(model),
		Messages: []types.Message{{
			Role: types.ConversationRoleUser,
			Content: []types.ContentBlock{
				&types.ContentBlockMemberText{Value: "ping"},
			},
		}},
	})
	if err != nil {
		op.SetErrorKind("provider_error")
		return err
	}
	if resp.Usage != nil {
		op.SetPromptTokens(int64(aws.ToInt32(resp.Usage.InputTokens)))
		op.SetCompletionTokens(int64(aws.ToInt32(resp.Usage.OutputTokens)))
	}
	return nil
}
