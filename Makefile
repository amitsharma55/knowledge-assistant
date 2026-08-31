.PHONY: dev-up dev-down seed run-api run-ingest run-ui test build

dev-up:
	docker compose -f deploy/docker/docker-compose.yml up -d

dev-down:
	docker compose -f deploy/docker/docker-compose.yml down -v

seed:
	go run ./services/ingestion/cmd/indexer -source fixtures -fixtures fixtures/coupa -team coupa
	go run ./services/ingestion/cmd/indexer -source fixtures -fixtures fixtures/star  -team star
	go run ./services/ingestion/cmd/indexer -source fixtures -fixtures fixtures/hr    -team hr

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
