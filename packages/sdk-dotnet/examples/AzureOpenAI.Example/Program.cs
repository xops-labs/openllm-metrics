// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Minimal example: wrap an Azure OpenAI call. Azure callers typically pass the
// deployment name as `model` and the region/endpoint as part of `route` so
// FinOps dashboards can split spend by deployment and region. The body is
// otherwise identical to the OpenAI example.

using OpenLLMMetrics;

OpenLLM.Init(
    serviceName: "azure-openai-example",
    exporterEndpoint: "http://localhost:4317",
    defaultTags: new Dictionary<string, string>
    {
        ["deployment.environment"] = "development",
    });

using (var op = OpenLLM.StartLlmCall(
    provider: "azure_openai",
    model: "gpt-4o-mini",
    route: "azure/eastus2/my-deployment",
    tenant: "acme",
    team: "platform",
    app: "rag-service",
    env: "development",
    project: "demo"))
{
    try
    {
        var (promptTokens, completionTokens) = await CallAzureOpenAI();
        op.SetPromptTokens(promptTokens);
        op.SetCompletionTokens(completionTokens);
        // Optional: dollarize on the spot if you have a price table loaded.
        op.SetUsageDollars(0.0042m);
    }
    catch (TimeoutException)
    {
        op.SetErrorKind("timeout");
        throw;
    }
    catch (Exception)
    {
        op.SetErrorKind("upstream_error");
        throw;
    }
}

OpenLLM.Shutdown();
Console.WriteLine("done");

// Simulated call. In production this would be
// `azureOpenAIClient.GetChatClient(deploymentName).CompleteChatAsync(...)`,
// and the token counts come from the response's Usage object.
static async Task<(int input, int output)> CallAzureOpenAI()
{
    await Task.Delay(35);
    return (210, 87);
}
