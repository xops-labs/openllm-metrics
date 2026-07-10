// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package openllm

// Options configures Init. ServiceName is required; everything else has a
// reasonable default. The struct form is the primary API; functional Option
// helpers are provided for callers who prefer that style.
type Options struct {
	// ServiceName is reported as service.name on every span and metric.
	// Required.
	ServiceName string
	// ServiceVersion is reported as service.version. Defaults to "0.0.0".
	ServiceVersion string
	// ExporterEndpoint is the OTLP/HTTP collector endpoint, e.g.
	// "http://localhost:4318". Empty falls back to the OTel SDK default
	// (which honors OTEL_EXPORTER_OTLP_ENDPOINT). The SDK uses HTTP/protobuf
	// so the same endpoint works for both the trace and metric exporters.
	ExporterEndpoint string
	// ExporterInsecure forces plaintext HTTP regardless of scheme. Useful
	// for local-stack development where the collector listens on
	// http://localhost:4318 without TLS.
	ExporterInsecure bool
	// DefaultTags are attached to every span and llm_* metric the SDK emits.
	// Use these for the multi-tenant bundle when every call shares the same
	// tenant/team/app/env/project; per-call values on CallOptions still
	// override.
	DefaultTags map[string]string
}

// Option is a functional option for Init. The struct form is the primary API;
// these helpers exist for callers who prefer fluent configuration.
type Option func(*Options)

// WithServiceVersion sets Options.ServiceVersion.
func WithServiceVersion(v string) Option {
	return func(o *Options) { o.ServiceVersion = v }
}

// WithExporterEndpoint sets Options.ExporterEndpoint.
func WithExporterEndpoint(endpoint string) Option {
	return func(o *Options) { o.ExporterEndpoint = endpoint }
}

// WithExporterInsecure forces plaintext HTTP for both OTLP exporters.
func WithExporterInsecure() Option {
	return func(o *Options) { o.ExporterInsecure = true }
}

// WithDefaultTag adds a single default tag. Multiple calls accumulate.
func WithDefaultTag(key, value string) Option {
	return func(o *Options) {
		if o.DefaultTags == nil {
			o.DefaultTags = make(map[string]string)
		}
		o.DefaultTags[key] = value
	}
}

// CallOptions configures a single LLM call. Provider and Model should always
// be set; the multi-tenant fields override any values pulled from
// Options.DefaultTags or baggage already on the inbound context.
type CallOptions struct {
	// Provider is the lower-case provider name, e.g. "openai", "anthropic".
	Provider string
	// Model is the canonical model name requested by the caller.
	Model string
	// Operation is the OTel GenAI operation name. Defaults to "chat".
	Operation string
	// Route is the routing label (provider+region or named route).
	Route string
	// ServerAddress is the provider endpoint host or region.
	ServerAddress string
	// Tenant / Team / App / Env / Project are the multi-tenant bundle.
	// Empty values fall back to Options.DefaultTags then inherited baggage.
	Tenant  string
	Team    string
	App     string
	Env     string
	Project string
}

// CallOption is a functional option for StartLlmCall.
type CallOption func(*CallOptions)

// WithProvider sets CallOptions.Provider.
func WithProvider(p string) CallOption { return func(c *CallOptions) { c.Provider = p } }

// WithModel sets CallOptions.Model.
func WithModel(m string) CallOption { return func(c *CallOptions) { c.Model = m } }

// WithOperation sets CallOptions.Operation.
func WithOperation(op string) CallOption { return func(c *CallOptions) { c.Operation = op } }

// WithRoute sets CallOptions.Route.
func WithRoute(r string) CallOption { return func(c *CallOptions) { c.Route = r } }

// WithServerAddress sets CallOptions.ServerAddress.
func WithServerAddress(a string) CallOption { return func(c *CallOptions) { c.ServerAddress = a } }

// WithTenant sets CallOptions.Tenant.
func WithTenant(t string) CallOption { return func(c *CallOptions) { c.Tenant = t } }

// WithTeam sets CallOptions.Team.
func WithTeam(t string) CallOption { return func(c *CallOptions) { c.Team = t } }

// WithApp sets CallOptions.App.
func WithApp(a string) CallOption { return func(c *CallOptions) { c.App = a } }

// WithEnv sets CallOptions.Env.
func WithEnv(e string) CallOption { return func(c *CallOptions) { c.Env = e } }

// WithProject sets CallOptions.Project.
func WithProject(p string) CallOption { return func(c *CallOptions) { c.Project = p } }
