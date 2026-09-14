.PHONY: dev-up dev-down embed-pull reset-index seed seed-s3 run-api run-ingest run-ui test lint tf-check tf-backend k8s-check images build

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

# Drop the fixture index before reseeding. A chunk's document id hashes its
# text, so any change to the chunker gives every chunk a new id: the reseed
# writes a full new copy and leaves the old one behind, and retrieval then
# ranks stale chunks against fresh ones. Safe here because this index holds
# nothing but fixtures; EnsureIndex deliberately refuses to drop an index on
# its own, so the deletion is explicit and lives only in this dev target.
reset-index:
	@curl -s -X DELETE "$${KA_OPENSEARCH_URL:-http://localhost:9200}/$${KA_OPENSEARCH_INDEX:-kb-chunks}" >/dev/null || true

# Loads .env the same way run-api does. Without it the indexer falls back to
# the compiled-in KA_OLLAMA_URL default (localhost:11434) and embeds against
# whatever answers there -- a native Ollama.app, if one is running, which
# 404s on nomic-embed-text.
seed: embed-pull reset-index
	@set -a; [ -f .env ] && . ./.env; set +a; \
	go run ./services/ingestion/cmd/indexer -source fixtures -fixtures fixtures/coupa -team coupa && \
	go run ./services/ingestion/cmd/indexer -source fixtures -fixtures fixtures/star  -team star && \
	go run ./services/ingestion/cmd/indexer -source fixtures -fixtures fixtures/hr    -team hr
	@# Newly indexed docs are not searchable until OpenSearch refreshes
	@# (1s by default). Without this, a query issued immediately after
	@# seeding sees an empty index and looks like a retrieval failure.
	@curl -s -X POST "$${KA_OPENSEARCH_URL:-http://localhost:9200}/$${KA_OPENSEARCH_INDEX:-kb-chunks}/_refresh" >/dev/null

# Copy the fabricated fixture corpus into the docs bucket, one prefix per team.
# Owner-run: needs AWS credentials and KA_DOCS_BUCKET. Mirrors the per-team
# layout the S3 source expects (s3://$(KA_DOCS_BUCKET)/<team>/).
seed-s3:
	@test -n "$(KA_DOCS_BUCKET)" || { echo "set KA_DOCS_BUCKET"; exit 1; }
	aws s3 cp fixtures/coupa "s3://$(KA_DOCS_BUCKET)/coupa/" --recursive --exclude "*" --include "*.md"
	aws s3 cp fixtures/star  "s3://$(KA_DOCS_BUCKET)/star/"  --recursive --exclude "*" --include "*.md"
	aws s3 cp fixtures/hr    "s3://$(KA_DOCS_BUCKET)/hr/"    --recursive --exclude "*" --include "*.md"

run-api:
	@if [ -f .env ]; then set -a; . ./.env; set +a; fi; \
	KA_LLM_MODE=$${KA_LLM_MODE:-mock} \
	KA_OPENSEARCH_URL=$${KA_OPENSEARCH_URL:-http://localhost:9200} \
	go run ./services/chat-api/cmd/server

run-ingest:
	@set -a; [ -f .env ] && . ./.env; set +a; \
	go run ./services/ingestion/cmd/indexer

run-ui:
	cd ui && npm install && npm run dev

test:
	go test ./...

lint:
	golangci-lint run ./...

tf-check:
	terraform/check.sh

k8s-check:
	deploy/k8s/check.sh

# Generate foundation/backend.hcl from the bootstrap stack's outputs so the state
# bucket name is never copied by hand. Bootstrap keeps its state locally, so this
# only reads that local state -- no backend calls, safe to run anytime after
# `terraform apply` in bootstrap. Then: (cd terraform/foundation && terraform init
# -backend-config=backend.hcl). backend.hcl is gitignored.
tf-backend:
	@bucket=$$(terraform -chdir=terraform/bootstrap output -raw state_bucket_name) && \
	region=$$(terraform -chdir=terraform/bootstrap output -raw region) && \
	printf 'bucket       = "%s"\nkey          = "foundation/terraform.tfstate"\nregion       = "%s"\nuse_lockfile = true\nencrypt      = true\n' \
		"$$bucket" "$$region" > terraform/foundation/backend.hcl && \
	echo "Wrote terraform/foundation/backend.hcl (bucket=$$bucket, region=$$region)"

# Owner-run: needs Docker (buildx) + AWS credentials. Builds all three service
# images for linux/amd64 and pushes them to foundation's ECR, tagged latest.
# Reads the registry from the foundation stack's outputs.
images:
	deploy/docker/build-push.sh

build:
	CGO_ENABLED=0 go build -o bin/chat-api ./services/chat-api/cmd/server
	CGO_ENABLED=0 go build -o bin/indexer  ./services/ingestion/cmd/indexer
