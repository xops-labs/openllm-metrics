# OpenLLM Metrics — local development helpers
# Requires: Docker Engine 24+
# For migrate targets: go install github.com/pressly/goose/v3/cmd/goose@latest

.PHONY: dev-up dev-down dev-clean dev-logs dev-ps \
        dev-migrate dev-migrate-status dev-migrate-rollback \
        quickstart quickstart-down quickstart-logs quickstart-clean

# ── Stack lifecycle ───────────────────────────────────────────────────────────

dev-up:
	docker compose up -d

dev-down:
	docker compose down

dev-clean:
	docker compose down -v

dev-logs:
	docker compose logs -f

dev-ps:
	docker compose ps

# ── Database migrations ───────────────────────────────────────────────────────
# Runs after postgres is healthy. Requires goose on PATH.

dev-migrate:
	./tools/scripts/migrate.sh apply control_plane
	./tools/scripts/migrate.sh apply gateway
	./tools/scripts/migrate.sh apply scoring
	./tools/scripts/migrate.sh apply audit

dev-migrate-status:
	./tools/scripts/migrate.sh status control_plane
	./tools/scripts/migrate.sh status gateway
	./tools/scripts/migrate.sh status scoring
	./tools/scripts/migrate.sh status audit

dev-migrate-rollback:
	./tools/scripts/migrate.sh rollback audit
	./tools/scripts/migrate.sh rollback scoring
	./tools/scripts/migrate.sh rollback gateway
	./tools/scripts/migrate.sh rollback control_plane

# ── Data-path quickstart (F011) ───────────────────────────────────────────────
# Single-file compose for the core data path: the F009 poller, the F010
# aggregator, the F017 cost-mapper / F023 reconciler workers, and the infra
# they need (the exporter chain is gated behind --profile exporter). Set
# OPENAI_ADMIN_API_KEY for live OpenAI billing data; the stack boots without
# it (see platform/deployment/compose/.env.example).

QUICKSTART_COMPOSE := platform/deployment/compose/quickstart.yml

quickstart:
	docker compose -f $(QUICKSTART_COMPOSE) up -d

quickstart-down:
	docker compose -f $(QUICKSTART_COMPOSE) down

quickstart-logs:
	docker compose -f $(QUICKSTART_COMPOSE) logs -f

quickstart-clean:
	docker compose -f $(QUICKSTART_COMPOSE) down -v
