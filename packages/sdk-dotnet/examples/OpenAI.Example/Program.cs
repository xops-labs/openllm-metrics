// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Minimal example: wrap an OpenAI SDK call so that OpenLLM Metrics emits the
// duration histogram, token counters, and llm_requests_total counter without
// the example ever touching prompt or completion text.
//
// This program does NOT depend on the real OpenAI SDK — it simulates a call.
// In production you would replace `await CallOpenAI(...)` with the real
// `OpenAIClient.GetChatClient(model).CompleteChatAsync(...)` invocation and
// pull token counts from the returned `Usage` object.

using OpenLLMMetrics;

OpenLLM.Init(
    serviceName: "openai-example",
    exporterEndpoint: "http://localhost:4317",
    defaultTags: new Dictionary<string, string>
    {
        ["deployment.environment"] = "development",
    });

using (var op = OpenLLM.StartLlmCall(
    provider: "openai",
    model: "gpt-4o-mini",
    route: "openai/us-east",
    tenant: "acme",
    team: "platform",
    app: "support-bot",
    env: "development",
    project: "demo"))
{
    try
    {
        var (promptTokens, completionTokens) = await CallOpenAI();
        op.SetPromptTokens(promptTokens);
        op.SetCompletionTokens(completionTokens);
    }
    catch (Exception)
    {
        op.SetErrorKind("upstream_error");
        throw;
    }
}

OpenLLM.Shutdown();
Console.WriteLine("done");

// Simulated call. The real OpenAI SDK returns a ChatCompletion whose
// .Usage.InputTokenCount / .Usage.OutputTokenCount values feed the setters
// above. We never look at the response message content here on purpose.
static async Task<(int input, int output)> CallOpenAI()
{
    await Task.Delay(40);
    return (123, 456);
}
