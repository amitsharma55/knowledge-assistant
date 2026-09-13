# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

- Local stack: follow README "Local dev" order, or use `/run-local`. `make seed` = pull embed model + drop index + index `fixtures/{coupa,star,hr}` + refresh.
- Tests: `go test ./...`; single test: `go test ./internal/rag -run TestName`. Also `go vet ./...`.
- Lint: `make lint` (golangci-lint v2, config in `.golangci.yml`).
- Terraform: `make tf-check` runs fmt, validate, `terraform test` (mocked AWS provider) and the script tests; it needs no credentials.
- UI (`ui/`): `npm --prefix ui test` (Vitest + React Testing Library; tests sit next to the code as `*.test.js[x]`), then a clean `npm --prefix ui run build`. There is no UI lint.
- Retrieval check: `python3 scripts/check_corpus.py` against a running chat-api (`--id`, `--team`, `--category`, `--exclude-category`, `--scores`, `--validate`). It is not an eval harness: 36 questions, so a 1–2 question swing is noise. Use `/check-retrieval`.
- Deploy (K8s, `deploy/k8s/`): `make k8s-check` is the offline gate (no AWS, no cluster). `deploy/k8s/deploy.sh up [tag]` brings the app up on the daily EKS cluster (needs `helm` + the 4a stack applied); `deploy/k8s/deploy.sh down` releases the ALB and deletes workloads (run before `terraform destroy`). `deploy/k8s/smoke.sh` checks a live deploy.

## Gotchas

- Terraform lives in `terraform/` and runs only from the owner's laptop. All configuration is in each stack's `terraform.tfvars` / gitignored `private.auto.tfvars`, never literals in `.tf` (`make tf-check` enforces it). Never run `terraform apply`, `destroy`, `import`, or `init` against the real backend unless asked; use `init -backend=false`.
- Chunk doc ids hash the chunk text. Any change to the chunker or `EmbedText` needs `make seed` (it drops the index first), or stale chunks rank against fresh ones.
- `EnsureIndex` refuses an index with no `team` field or a different vector width (i.e. the embedding model changed). The fix is a deliberate delete + reseed; never make it auto-recreate.
- Both binaries must build their embedder with `embed.New(embed.OptionsFromEnv())` — indexed and query vectors must come from one model. The mock embedder is never a fallback.
- `rag.Chunk.Score` is on the `(1+cos)/2` scale and `KA_RELEVANCE_FLOOR` (0.81) is calibrated per embedding backend. Recalibrate it when the embedder changes; any new retriever must return that scale.
- Two Ollamas: the container on 11435 serves `nomic-embed-text`; native Ollama.app on 11434 serves `gpt-oss:20b` for rerank/rewrite. A 404 on the embed model means calls hit the wrong one.
- `make run-api` sources `.env`, whose `KA_LLM_MODE` overrides anything set on the command line.
- `make dev-down` passes `-v` and deletes the Docker volumes (index and pulled model).
- `terraform/daily/` is the disposable stack (EKS + OpenSearch), applied and destroyed each working session. It reads the foundation stack via `terraform_remote_state` and creates no IAM. Its OpenSearch index is disposable; 4b rebuilds it from S3. Never leave it applied overnight.
- The 4b ALB (`deploy/k8s/base/ingress.yaml`) routes all traffic to the UI Service; the UI's nginx reverse-proxies `/v1` to chat-api (SSE is tuned there, not at the ALB). Do not add a second Ingress backend for chat-api.
- The reseed Job runs the distroless indexer image, which has no shell: the three teams run as sequential Pod containers (coupa/star as initContainers, hr as the main container), never a shell loop. Sequential ordering avoids two invocations racing to create the index on a 404.
- `KA_RELEVANCE_FLOOR` in `deploy/k8s/base/chat-api.yaml` is set to 0.81, calibrated for local nomic-768; it is provisional on Bedrock/Titan-v2. Recalibrate with `/check-retrieval` against the running stack and update the manifest.
- `deploy/k8s/deploy.sh down` must run before `terraform destroy` on the daily stack: the Ingress owns a real ALB whose ENIs otherwise block subnet deletion (a 4c ordering concern; `down` is the primitive).
- A dev server started from a Claude session dies when the session ends unless detached (`nohup … & disown`); `/run-local` does this.
- Team is required on every indexed doc and every request (`X-Team` header; dev identity via `X-Dev-User`) and is never inferred from content or path. Read `docs/superpowers/specs/2026-08-30-team-segregation-design.md` before changing retrieval scoping.
- `fixtures/` is fabricated demo data. Docs are self-contained `##` sections of under ~800 words, with retrievable facts as bullets, not tables.

## Conventions

- Comments explain why (the failure mode, the measured result), not what. Match that density when editing.
- Wrap errors with a package prefix: `fmt.Errorf("pkg: context: %w", err)`. Operator-facing errors say how to fix the problem.
- Tests use `httptest` servers for HTTP clients and hand-written `stubX` types in `*_test.go`.
- Go must be gofmt-clean. A hook formats `.go` files after Edit/Write; after changing Go files through the shell, run `gofmt -w` yourself.
- Designs go in `docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md`, plans in `docs/superpowers/plans/YYYY-MM-DD-<topic>.md`.

## Git

- Work on kebab-case branches; changes land by pushing directly to `main` (no PRs).
- Commit subject: an imperative sentence with no prefix. The body explains why.
- Only the repo owner pushes. Commit when asked; never `git push`.
