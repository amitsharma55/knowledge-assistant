.PHONY: dev-up dev-down embed-pull seed run-api run-ingest run-ui test build

dev-up:
	docker compose -f deploy/docker/docker-compose.yml up -d

dev-down:
	docker compose -f deploy/docker/docker-compose.yml down -v

# Pull the embedding model into the ollama container. Separate target
# because the first pull downloads ~270MB; seed depends on it so an
# un-pulled model surfaces here rather than as a 404 mid-ingest.
embed-pull:
	docker compose -f deploy/docker/docker-compose.yml exec -T ollama \
		ollama pull $${KA_EMBED_MODEL:-nomic-embed-text}

seed: embed-pull
	go run ./services/ingestion/cmd/indexer -source fixtures -fixtures fixtures/coupa -team coupa
	go run ./services/ingestion/cmd/indexer -source fixtures -fixtures fixtures/star  -team star
	go run ./services/ingestion/cmd/indexer -source fixtures -fixtures fixtures/hr    -team hr
	@# Newly indexed docs are not searchable until OpenSearch refreshes
	@# (1s by default). Without this, a query issued immediately after
	@# seeding sees an empty index and looks like a retrieval failure.
	@curl -s -X POST "$${KA_OPENSEARCH_URL:-http://localhost:9200}/$${KA_OPENSEARCH_INDEX:-kb-chunks}/_refresh" >/dev/null

run-api:
	@if [ -f .env ]; then set -a; . ./.env; set +a; fi; \
	KA_LLM_MODE=$${KA_LLM_MODE:-mock} \
	KA_OPENSEARCH_URL=$${KA_OPENSEARCH_URL:-http://localhost:9200} \
	go run ./services/chat-api/cmd/server

run-ingest:
	go run ./services/ingestion/cmd/indexer

run-ui:
	cd ui && npm install && npm run dev

test:
	go test ./...

build:
	CGO_ENABLED=0 go build -o bin/chat-api ./services/chat-api/cmd/server
	CGO_ENABLED=0 go build -o bin/indexer  ./services/ingestion/cmd/indexer
