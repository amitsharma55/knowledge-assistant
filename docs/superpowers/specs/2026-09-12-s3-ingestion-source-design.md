# S3 Ingestion Source

Date: 2026-09-12
Status: Approved for planning

## Problem

The indexer can pull documents from GitLab or from local fixture files, but not
from the `ka-docs` S3 bucket the foundation provisioned. In the AWS/EKS daily
environment there is no GitLab and no local fixture tree; documents are placed
into S3 directly (a crawler comes later). Without an S3 source, the daily
environment has no way to load its corpus, and the ingestion pod's IAM role —
which the foundation already grants read access to the docs bucket — has nothing
to use it for.

This spec covers sub-project 3 of the roadmap in
`docs/superpowers/specs/2026-09-10-aws-foundation-design.md`:

1. Bedrock model clients (done).
2. Permanent foundation (done).
3. **S3 ingestion source — this spec.**
4. Daily stack (EKS, OpenSearch domain, deploy, up/down).

## Goals

- The indexer can read documents from the docs bucket: `indexer -source s3
  -team <t> -bucket <b>`, chunk, embed and index them exactly as the GitLab and
  fixture sources already do.
- Team stays explicit and is never inferred from content or a key: the run
  carries `-team`, and the source lists only that team's prefix.
- PDF, Markdown and plain text are ingested by reusing `internal/extract`.
- All new code is unit-tested offline with a stubbed S3 client — no AWS
  credentials and no live calls — so `go test ./...` and `go vet ./...` stay
  green in CI.

## Non-Goals

- **The daily stack and its CronJob topology** (sub-project 4). Wiring
  `deploy/k8s/ingestion-cron.yaml` to the S3 source means choosing a per-team
  job structure (one CronJob per team vs. one parameterized job); that is a
  deploy decision for #4. This spec leaves the manifest alone and flags it.
- **An S3 snapshot store.** The `storage.Store` (`PutRaw`/`PutNormalized`) is a
  local-dev inspection aid; its call site is already best-effort. This spec adds
  a *read* source only and leaves `Store` as `LocalDisk`.
- **docx and other formats `internal/extract` does not support.** Extract covers
  PDF, Markdown and TXT; docx would need a new dependency. Out of scope.
- **Populating the bucket from the application.** Documents are placed into S3
  out of band (the upload handler does not write there, and no crawler exists
  yet). This spec provides only an owner-run seeding convenience for the demo.
- **Relevance-floor or embedding changes.** Unchanged from sub-project 1.

## Decisions

- **Per-team prefix, one run per team.** Objects live under
  `s3://<bucket>/<team>/…`. The operator runs the indexer once per team with
  `-team=<team>`; the source lists that team's prefix. Team comes from the flag,
  never from parsing a key, so the strict team rule holds. This also maps
  cleanly onto #4's deploy (a CronJob per team) and lets each team re-index
  independently. Rejected: a flat bucket (cannot hold more than one team without
  cross-contamination) and a single run that loops all team prefixes (derives
  team from the key's top segment — too close to inferring team from path).
- **Reuse `internal/extract`.** `extract.FromBytes(key, bytes)` already backs the
  upload handler and dispatches on extension (PDF/MD/TXT). The source reuses it
  rather than re-implementing format handling. Markdown passes through as text,
  so `chunker.Title` still lifts the H1; PDF/TXT titles fall back to the
  filename, exactly as the fixtures source does.
- **Credentials via the SDK default chain.** A new `awsx.S3(ctx)` sits beside
  `awsx.BedrockRuntime`, so S3 uses EKS Pod Identity in the cluster and the
  ambient chain locally. No static keys.
- **Hard vs. soft failures.** A `ListObjectsV2`/`GetObject` transport error and
  *finding zero ingestible objects under the prefix* are hard errors that fail
  the run — a daily reseed that silently indexes nothing almost always means a
  misconfigured bucket, prefix or credentials, and the indexer already refuses
  to report success after indexing nothing. A single object that fails to
  extract or is empty is logged and skipped, so one bad PDF does not kill a
  team's reindex.

## Design

### Components

| Unit | Responsibility |
|---|---|
| `services/ingestion/internal/s3src` | List a team's prefix and turn each object into a normalized `Doc`, behind a narrow S3 interface |
| `internal/awsx` (extend) | `S3(ctx) (*s3.Client, error)` from the ambient credential chain |
| `services/ingestion/cmd/indexer/main.go` | New `-source s3` case reading `-bucket`/`-prefix`, mapping `Doc` → the existing `page` |

`s3src` mirrors the existing `gitlab` package. Its client takes the S3 API
behind a one-method-per-call interface (`ListObjectsV2`, `GetObject`) so tests
substitute a stub, matching the repo's `httptest`/`stubX` convention.

```go
package s3src

type api interface {
	ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type Client struct {
	API    api
	Bucket string
	Prefix string
}

type Doc struct {
	Key          string
	Text         string
	LastModified time.Time
}

func (c *Client) FetchAll(ctx context.Context) ([]Doc, error)
```

### Flow

1. `FetchAll` pages through `ListObjectsV2` under `Prefix`, following the
   continuation token so more than 1000 objects work.
2. It skips directory-placeholder keys (trailing `/`, zero bytes) and keys whose
   extension `extract` does not support.
3. For each remaining key it calls `GetObject`, reads the body, and runs
   `extract.FromBytes(key, body)`. Empty or failed extractions are logged and
   skipped.
4. It returns `[]Doc`. If the result is empty (no ingestible object under the
   prefix), `FetchAll` returns an error.

In `main.go`, the `s3` case builds the client with `awsx.S3` and maps each `Doc`
to the existing `page`:

- `PageID` = key with the prefix stripped and extension removed
- `Title` = `chunker.Title(doc.Text, filenameSlug)`
- `Markdown` = `doc.Text`
- `WebURL` = `s3://<bucket>/<key>`
- `UpdatedAt` = `doc.LastModified`

Everything after — chunking, Titan embedding, team stamping via `docID`, bulk
index — is unchanged.

### Configuration surface

| Flag / env | Default | Meaning |
|---|---|---|
| `-source s3` | `gitlab` | select the S3 source |
| `-bucket` / `KA_DOCS_BUCKET` | — | docs bucket, e.g. `ka-docs-341813136741` |
| `-prefix` | `<team>/` | key prefix to list; keeps team explicit |
| `AWS_REGION` | — | region for the S3 client |

Local (`fixtures`/`gitlab`) defaults are unchanged.

### Error handling

Errors wrap with a package prefix and read for an operator
(`fmt.Errorf("s3src: context: %w", err)`).

- `ListObjectsV2` / `GetObject` transport errors → returned, failing the run.
- Zero ingestible objects under the prefix → an error naming the bucket and
  prefix, so a misconfiguration is caught rather than silently indexing nothing.
- A single object that fails to extract or extracts to empty → logged and
  skipped.

## Testing

Offline, with a hand-written stub of the `api` interface.

- Lists under the prefix and builds `Doc`s: markdown H1 → title, `s3://` URL,
  `LastModified` carried through.
- Pagination across two `ListObjectsV2` pages via the continuation token.
- Skips an unsupported extension (e.g. `.png`) and a zero-byte/empty object.
- Hard error when no ingestible object is found under the prefix.
- Hard error when `GetObject` fails.

No test makes a live AWS call.

## Owner-run steps (need AWS)

Documented in the README ingestion section:

1. `make seed-s3` — copies `fixtures/<team>/` → `s3://$KA_DOCS_BUCKET/<team>/`
   for each team with `aws s3 cp`. The fixtures already sit under
   `fixtures/{coupa,star,hr}/`, so they map 1:1 onto the per-team prefixes.
2. Per team: `indexer -source s3 -team <t> -bucket $KA_DOCS_BUCKET`.
3. `python3 scripts/check_corpus.py` to confirm retrieval, as usual.

## File structure

| Path | Change |
|---|---|
| `services/ingestion/internal/s3src/s3src.go` | New: `Client`, `Doc`, `FetchAll`, the `api` interface |
| `services/ingestion/internal/s3src/s3src_test.go` | New: offline tests with a stub `api` |
| `internal/awsx/awsx.go` | Add `S3(ctx) (*s3.Client, error)` |
| `internal/awsx/awsx_test.go` | Add a region test for `S3` |
| `services/ingestion/cmd/indexer/main.go` | New `-source s3` case + `-bucket`/`-prefix` flags |
| `Makefile` | Add the owner-run `seed-s3` target |
| `README.md` | Document the S3 source and `make seed-s3` |
| `go.mod` / `go.sum` | Add `aws-sdk-go-v2/service/s3` |
