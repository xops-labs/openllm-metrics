// proxy_demo.go — Zero-code instrumentation via the OpenLLM Metrics gateway.
//
// Run with:
//
//	OPENAI_BASE_URL=http://localhost:8085 go run proxy_demo.go
//
// The only change from a vanilla OpenAI HTTP call is overriding the base URL.
// No SDK import, no openllm.Init(), no wrapper code needed.  Unlike the
// Python/Node OpenAI SDKs, this script builds the full /v1/chat/completions
// path itself, so the base URL must NOT end in /v1.
//
// The gateway intercepts the request, records timing and token counts, and
// forwards it to api.openai.com.  After the call you will see the
// llm_gateway_* series on the gateway metrics port (:8086/metrics) and the
// aggregated llm_requests_total on the metrics-endpoint (:9092/metrics)
// and in Prometheus.
//
// Optional tenant headers let the gateway attribute the call without any
// application-side changes:
//
//	X-OLM-Tenant   acme
//	X-OLM-Team     platform
//	X-OLM-App      chatbot
//	X-OLM-Env      production
//	X-OLM-Project  customer-support
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Model string    `json:"model"`
	Usage *usage    `json:"usage"`
	Error *apiError `json:"error,omitempty"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func main() {
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8085"
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "OPENAI_API_KEY environment variable is required")
		os.Exit(1)
	}

	reqBody, _ := json.Marshal(chatRequest{
		Model: "gpt-4o-mini",
		Messages: []chatMessage{
			{Role: "user", Content: "Reply with exactly one word: pong"},
		},
	})

	req, _ := http.NewRequest(http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// Optional tenant headers — the gateway attaches them as metric labels.
	req.Header.Set("X-OLM-Tenant", "acme")
	req.Header.Set("X-OLM-Team", "platform")
	req.Header.Set("X-OLM-App", "proxy-demo")
	req.Header.Set("X-OLM-Env", "development")
	req.Header.Set("X-OLM-Project", "openllm-demo")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "request failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var chatResp chatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		fmt.Fprintf(os.Stderr, "decode failed: %v\n%s\n", err, body)
		os.Exit(1)
	}
	if chatResp.Error != nil {
		fmt.Fprintf(os.Stderr, "API error: %s (%s)\n", chatResp.Error.Message, chatResp.Error.Type)
		os.Exit(1)
	}

	fmt.Printf("Model:             %s\n", chatResp.Model)
	if chatResp.Usage != nil {
		fmt.Printf("Prompt tokens:     %d\n", chatResp.Usage.PromptTokens)
		fmt.Printf("Completion tokens: %d\n", chatResp.Usage.CompletionTokens)
	}
	fmt.Printf("Gateway base URL:  %s\n", baseURL)
	fmt.Println()
	fmt.Println("Done. Check http://localhost:8086/metrics for llm_gateway_requests_total,")
	fmt.Println("or http://localhost:9092/metrics / Prometheus for llm_requests_total.")
}
