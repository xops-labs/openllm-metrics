# compliance

> **Status: planning notes.** Nothing in this document describes shipped
> functionality — these are design considerations for future work. The only
> compliance-relevant properties shipped today are the no-prompt/no-completion
> telemetry design and the hash-chained append-only audit ledger.

Compliance, privacy, and audit planning.

- GDPR / DPDP data subject rights — confirm the gateway never persists user prompt or completion content; document the data minimization stance.
- PCI exclusion — the gateway must not be on a PCI flow path. LLM payloads frequently contain unstructured data, but card data is explicitly out of scope and any flow that mixes them must be refused or routed away from the gateway.
- HIPAA-aligned PHI handling — when downstream tenants send PHI to LLMs, the gateway must redact or refuse based on tenant policy. No PHI in telemetry, scoring, or audit records.
- Provider data-handling agreements — track each provider's data retention, training-opt-out, and DPA terms. Surface these to tenants per policy.
- Audit ledger export format for policy mutations, budget changes, and routing decisions — append-only, hash-chained, and verifiable on export.
- Identity separation — human admins versus service-principal callers; MFA required for admins; least-privilege tokens for service-to-service calls.
- Decisioning boundary — scoring formulas, routing algorithms, and governance decision logic are not implemented in this repo.

Compliance assets are guidance and evidence, not legal advice. Get qualified legal review before any externally-facing financial, health-data, or token-related integration.
