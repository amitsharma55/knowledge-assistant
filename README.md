# Knowledge Assistant

Chat-based assistant that answers questions about internal integrations,
grounded in Confluence documentation.

- Architecture: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- Chat API (Go): [`services/chat-api/`](services/chat-api/)
- Ingestion (Go): [`services/ingestion/`](services/ingestion/)
- UI (React): [`ui/`](ui/)
- Deploy (K8s): [`deploy/k8s/`](deploy/k8s/)

## Local dev

```sh
# start OpenSearch + LocalStack (S3) + Postgres
make dev-up

# seed sample docs and index them
make seed

# run chat-api with mock Bedrock
make run-api

# in another shell
make run-ui
# open http://localhost:5173
```

Set `KA_LLM_MODE=mock` to skip Bedrock calls; the API returns canned answers
using retrieved chunks so you can iterate on retrieval without spending
tokens.

## Deploy

CI builds images to ECR on merge to `main` and rolls out via Helm; see
[`deploy/k8s/chart/`](deploy/k8s/chart/).
