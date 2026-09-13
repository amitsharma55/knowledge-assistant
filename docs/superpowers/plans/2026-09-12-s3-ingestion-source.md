# S3 Ingestion Source Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the indexer read a team's documents from the `ka-docs` S3 bucket (`indexer -source s3 -team <t> -bucket <b>`), reusing the existing chunk/embed/index pipeline.

**Architecture:** A new `services/ingestion/internal/s3src` package lists a team's prefix and turns each object into a normalized `Doc`, behind a narrow S3 interface that tests stub. `internal/awsx` gains an `S3` client builder alongside the Bedrock one. The indexer gains a `-source s3` case that maps each `Doc` to its existing `page` shape; everything downstream is unchanged. Team stays explicit via the required `-team` flag; the source only ever lists that team's prefix.

**Tech Stack:** Go 1.25, `aws-sdk-go-v2/service/s3`, `internal/extract` (PDF/MD/TXT), standard-library `io`; hand-written stubs for tests.

**Spec:** `docs/superpowers/specs/2026-09-12-s3-ingestion-source-design.md`

## Global Constraints

- **Go must be gofmt-clean.** A hook formats `.go` files after Edit/Write; after shell edits run `gofmt -w`. `go vet ./...` must pass.
- **Wrap errors with a package prefix**, operator-facing: `fmt.Errorf("s3src: context: %w", err)`.
- **Tests are offline.** No test makes a live AWS call or needs credentials. Stub the S3 `api` interface with a hand-written type. `go test ./...` stays green.
- **Team is explicit, never inferred.** The run carries `-team`; the source lists only that team's prefix. Do not derive team from a key.
- **Reuse, do not re-implement, format handling:** call `internal/extract.FromBytes`. docx is out of scope (extract does not support it).
- **Hard vs. soft failures:** transport errors and *zero ingestible objects under the prefix* fail the run; a single object that fails to extract or is empty is logged and skipped.
- **Commits are local only — never `git push`.** Commit subject is an imperative sentence with no prefix; end every message with:

  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01RZE4EXAxZNiFi4YURxWXzN
  ```
- **Branch:** this work is done on a kebab-case branch (e.g. `s3-ingestion-source`), not `main`.

---

## File Structure

| Path | Responsibility |
|---|---|
| `internal/awsx/awsx.go` | Add `S3(ctx) (*s3.Client, error)` |
| `internal/awsx/awsx_test.go` | Region test for `S3` |
| `services/ingestion/internal/s3src/s3src.go` | `Client`, `Doc`, `FetchAll`, the `api` interface |
| `services/ingestion/internal/s3src/s3src_test.go` | Offline tests with a stub `api` |
| `services/ingestion/cmd/indexer/main.go` | `-source s3` case, `-bucket`/`-prefix` flags, `Doc`→`page` mapping |
| `services/ingestion/cmd/indexer/main_test.go` | Test the `Doc`→`page` mapping helper |
| `Makefile` | Owner-run `seed-s3` target |
| `README.md` | Document the S3 source and `make seed-s3` |
| `go.mod` / `go.sum` | Add `aws-sdk-go-v2/service/s3` |

---

## Task 1: `awsx.S3` client helper

**Files:**
- Modify: `internal/awsx/awsx.go`, `internal/awsx/awsx_test.go`, `go.mod`, `go.sum`

**Interfaces:**
- Produces: `func S3(ctx context.Context) (*s3.Client, error)`.

- [ ] **Step 1: Add the dependency**

Run:
```bash
go get github.com/aws/aws-sdk-go-v2/service/s3@latest
```
(`go mod tidy` runs in a later step once code imports it.)

- [ ] **Step 2: Write the failing test**

Append to `internal/awsx/awsx_test.go`:

```go
func TestS3UsesRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	c, err := S3(context.Background())
	if err != nil {
		t.Fatalf("S3: %v", err)
	}
	if c == nil {
		t.Fatal("S3 returned a nil client")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/awsx/ -run TestS3UsesRegion -v`
Expected: FAIL — `undefined: S3`.

- [ ] **Step 4: Implement**

Add to `internal/awsx/awsx.go` (add `"github.com/aws/aws-sdk-go-v2/service/s3"` to the import block):

```go
// S3 returns an S3 client. The region comes from the environment (AWS_REGION);
// calls fail fast if it is unset.
func S3(ctx context.Context) (*s3.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("awsx: load aws config: %w", err)
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("awsx: no AWS region; set AWS_REGION")
	}
	return s3.NewFromConfig(cfg), nil
}
```

- [ ] **Step 5: Tidy, run tests, vet**

Run: `go mod tidy && go test ./internal/awsx/ -v && go vet ./internal/awsx/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/awsx/
git add internal/awsx/ go.mod go.sum
git commit   # subject: "Add awsx S3 client helper" + attribution trailers
```

---

## Task 2: `s3src` source package

**Files:**
- Create: `services/ingestion/internal/s3src/s3src.go`, `services/ingestion/internal/s3src/s3src_test.go`

**Interfaces:**
- Consumes: `internal/extract.FromBytes`.
- Produces: `type Client struct { API api; Bucket, Prefix string }`; `type Doc struct { Key string; Text string; LastModified time.Time }`; `func (c *Client) FetchAll(ctx context.Context) ([]Doc, error)`; `type api interface { ListObjectsV2(...); GetObject(...) }`.

- [ ] **Step 1: Write the failing tests**

Create `services/ingestion/internal/s3src/s3src_test.go`:

```go
package s3src

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// stubS3 serves canned objects. listPages is returned one page per call;
// bodies maps key -> content. getErr, if set, fails GetObject.
type stubS3 struct {
	listPages []*s3.ListObjectsV2Output
	callN     int
	bodies    map[string]string
	getErr    error
}

func (s *stubS3) ListObjectsV2(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	out := s.listPages[s.callN]
	s.callN++
	return out, nil
}

func (s *stubS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	body := s.bodies[aws.ToString(in.Key)]
	return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader(body))}, nil
}

func obj(key string, size int64, mod time.Time) s3types.Object {
	return s3types.Object{Key: aws.String(key), Size: aws.Int64(size), LastModified: aws.Time(mod)}
}

func page(truncated bool, next string, objs ...s3types.Object) *s3.ListObjectsV2Output {
	out := &s3.ListObjectsV2Output{Contents: objs, IsTruncated: aws.Bool(truncated)}
	if next != "" {
		out.NextContinuationToken = aws.String(next)
	}
	return out
}

func TestFetchAllBuildsDocs(t *testing.T) {
	mod := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	stub := &stubS3{
		listPages: []*s3.ListObjectsV2Output{
			page(false, "", obj("coupa/a.md", 10, mod)),
		},
		bodies: map[string]string{"coupa/a.md": "# Title A\n\nbody"},
	}
	c := &Client{API: stub, Bucket: "b", Prefix: "coupa/"}
	docs, err := c.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(docs) != 1 || docs[0].Key != "coupa/a.md" || !strings.Contains(docs[0].Text, "Title A") {
		t.Fatalf("unexpected docs %+v", docs)
	}
	if !docs[0].LastModified.Equal(mod) {
		t.Fatalf("lost LastModified: %v", docs[0].LastModified)
	}
}

func TestFetchAllPaginates(t *testing.T) {
	mod := time.Now()
	stub := &stubS3{
		listPages: []*s3.ListObjectsV2Output{
			page(true, "tok", obj("coupa/a.md", 3, mod)),
			page(false, "", obj("coupa/b.md", 3, mod)),
		},
		bodies: map[string]string{"coupa/a.md": "a", "coupa/b.md": "b"},
	}
	c := &Client{API: stub, Bucket: "b", Prefix: "coupa/"}
	docs, err := c.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("want 2 docs across pages, got %d", len(docs))
	}
}

func TestFetchAllSkipsUnsupportedAndEmpty(t *testing.T) {
	mod := time.Now()
	stub := &stubS3{
		listPages: []*s3.ListObjectsV2Output{
			page(false, "",
				obj("coupa/pic.png", 100, mod), // unsupported ext
				obj("coupa/dir/", 0, mod),      // placeholder
				obj("coupa/empty.md", 0, mod),  // empty body
				obj("coupa/ok.md", 5, mod),     // good
			),
		},
		bodies: map[string]string{"coupa/empty.md": "", "coupa/ok.md": "# Ok\n\nx"},
	}
	c := &Client{API: stub, Bucket: "b", Prefix: "coupa/"}
	docs, err := c.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(docs) != 1 || docs[0].Key != "coupa/ok.md" {
		t.Fatalf("expected only ok.md, got %+v", docs)
	}
}

func TestFetchAllZeroObjectsIsError(t *testing.T) {
	stub := &stubS3{listPages: []*s3.ListObjectsV2Output{page(false, "")}}
	c := &Client{API: stub, Bucket: "b", Prefix: "coupa/"}
	if _, err := c.FetchAll(context.Background()); err == nil {
		t.Fatal("expected error when no ingestible objects found")
	}
}

func TestFetchAllGetErrorFails() {} // placeholder removed below
```

Note: replace the last placeholder with a real test:

```go
func TestFetchAllGetError(t *testing.T) {
	mod := time.Now()
	stub := &stubS3{
		listPages: []*s3.ListObjectsV2Output{page(false, "", obj("coupa/a.md", 3, mod))},
		getErr:    io.ErrUnexpectedEOF,
	}
	c := &Client{API: stub, Bucket: "b", Prefix: "coupa/"}
	if _, err := c.FetchAll(context.Background()); err == nil {
		t.Fatal("expected GetObject error to fail the run")
	}
}
```

(Delete the `TestFetchAllGetErrorFails` placeholder line; it exists only to mark where the real test goes.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./services/ingestion/internal/s3src/ -v`
Expected: FAIL — `undefined: Client` etc.

- [ ] **Step 3: Implement**

Create `services/ingestion/internal/s3src/s3src.go`:

```go
// Package s3src ingests a team's documents from an S3 prefix. Team is never
// inferred from a key: the caller lists exactly one team's prefix and stamps
// the team itself.
package s3src

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/example/knowledge-assistant/internal/extract"
)

// api is the slice of the S3 client this package uses; tests stub it.
type api interface {
	ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type Client struct {
	API    api
	Bucket string
	Prefix string
	Log    *slog.Logger
}

// Doc is one ingestible object's extracted text.
type Doc struct {
	Key          string
	Text         string
	LastModified time.Time
}

func (c *Client) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.Default()
}

// FetchAll lists the prefix and returns the extracted text of every ingestible
// object. Transport errors and an empty result are returned as errors; a single
// object that fails to extract or is empty is logged and skipped.
func (c *Client) FetchAll(ctx context.Context) ([]Doc, error) {
	var docs []Doc
	var token *string
	for {
		out, err := c.API.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.Bucket),
			Prefix:            aws.String(c.Prefix),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("s3src: list %s/%s: %w", c.Bucket, c.Prefix, err)
		}
		for _, o := range out.Contents {
			key := aws.ToString(o.Key)
			if strings.HasSuffix(key, "/") || aws.ToInt64(o.Size) == 0 {
				continue // directory placeholder or empty object
			}
			if !extract.Supported(key) {
				continue // not a format we can read
			}
			body, err := c.get(ctx, key)
			if err != nil {
				return nil, err // transport error is fatal
			}
			text, err := extract.FromBytes(key, body)
			if err != nil || strings.TrimSpace(text) == "" {
				c.log().Warn("s3src: skipping unreadable object", "key", key, "err", err)
				continue
			}
			docs = append(docs, Doc{Key: key, Text: text, LastModified: aws.ToTime(o.LastModified)})
		}
		if !aws.ToBool(out.IsTruncated) {
			break
		}
		token = out.NextContinuationToken
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("s3src: no ingestible documents under %s/%s; check the bucket, prefix and credentials", c.Bucket, c.Prefix)
	}
	return docs, nil
}

func (c *Client) get(ctx context.Context, key string) ([]byte, error) {
	out, err := c.API.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.Bucket), Key: aws.String(key)})
	if err != nil {
		return nil, fmt.Errorf("s3src: get %s: %w", key, err)
	}
	defer out.Body.Close()
	body, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("s3src: read %s: %w", key, err)
	}
	return body, nil
}
```

This uses a new `extract.Supported(name) bool`. Add it to `internal/extract/extract.go` next to `FromBytes`, sharing the same extension list:

```go
// Supported reports whether FromBytes can read the file named name.
func Supported(name string) bool {
	n := strings.ToLower(name)
	return strings.HasSuffix(n, ".pdf") ||
		strings.HasSuffix(n, ".md") ||
		strings.HasSuffix(n, ".markdown") ||
		strings.HasSuffix(n, ".txt") ||
		strings.HasSuffix(n, ".text")
}
```

(Match the exact suffix set `FromBytes` already switches on; adjust the list to whatever `FromBytes` accepts so the two never disagree.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go mod tidy && go test ./services/ingestion/internal/s3src/ ./internal/extract/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w services/ingestion/internal/s3src/ internal/extract/
git add services/ingestion/internal/s3src/ internal/extract/ go.mod go.sum
git commit   # subject: "Add S3 ingestion source package" + attribution trailers
```

---

## Task 3: Wire `-source s3` into the indexer

**Files:**
- Modify: `services/ingestion/cmd/indexer/main.go`
- Test: `services/ingestion/cmd/indexer/main_test.go`

**Interfaces:**
- Consumes: `s3src.Client`/`Doc`, `awsx.S3`, existing `page`, `chunker.Title`.
- Produces: `func s3Page(bucket, prefix string, d s3src.Doc) page` (unexported, testable).

- [ ] **Step 1: Write the failing test for the mapping helper**

Add to `services/ingestion/cmd/indexer/main_test.go`:

```go
func TestS3PageMapping(t *testing.T) {
	mod := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	p := s3Page("ka-docs", "coupa/", s3src.Doc{
		Key:          "coupa/avr-field-mapping.md",
		Text:         "# AVR Field Mapping\n\nbody",
		LastModified: mod,
	})
	if p.ID != "avr-field-mapping" {
		t.Fatalf("PageID = %q, want avr-field-mapping", p.ID)
	}
	if p.Title != "AVR Field Mapping" {
		t.Fatalf("Title = %q, want the H1", p.Title)
	}
	if p.WebURL != "s3://ka-docs/coupa/avr-field-mapping.md" {
		t.Fatalf("WebURL = %q", p.WebURL)
	}
	if !p.UpdatedAt.Equal(mod) {
		t.Fatalf("UpdatedAt not carried through")
	}
}
```

Add the imports `"time"` and the `s3src` package to `main_test.go` if not present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./services/ingestion/cmd/indexer/ -run TestS3PageMapping -v`
Expected: FAIL — `undefined: s3Page`.

- [ ] **Step 3: Add the mapping helper and the source case**

Add the helper to `main.go`:

```go
// s3Page maps an S3 object to the ingester's page shape. PageID is the key with
// the prefix stripped and extension removed; the H1 becomes the title (falling
// back to the filename slug for PDF/TXT), exactly as the fixtures source does.
func s3Page(bucket, prefix string, d s3src.Doc) page {
	rel := strings.TrimPrefix(d.Key, prefix)
	id := strings.TrimSuffix(rel, filepath.Ext(rel))
	return page{
		SpaceKey:  "S3",
		ID:        id,
		Title:     chunker.Title(d.Text, strings.ReplaceAll(id, "-", " ")),
		Markdown:  d.Text,
		WebURL:    "s3://" + bucket + "/" + d.Key,
		UpdatedAt: d.LastModified,
	}
}
```

Add the flags near the others:

```go
	bucket := flag.String("bucket", envOr("KA_DOCS_BUCKET", ""), "S3 docs bucket (source=s3)")
	prefix := flag.String("prefix", "", "S3 key prefix (source=s3); defaults to \"<team>/\"")
```

Add the `s3` case to the source switch:

```go
	case "s3":
		if *bucket == "" {
			log.Error("source=s3 requires -bucket or KA_DOCS_BUCKET")
			os.Exit(2)
		}
		p := *prefix
		if p == "" {
			p = tm.Slug() + "/"
		}
		client, cerr := awsx.S3(ctx)
		if cerr != nil {
			log.Error("s3 client", "err", cerr)
			os.Exit(1)
		}
		src := &s3src.Client{API: client, Bucket: *bucket, Prefix: p, Log: log}
		var docs []s3src.Doc
		docs, err = src.FetchAll(ctx)
		for _, d := range docs {
			pages = append(pages, s3Page(*bucket, p, d))
		}
```

Add imports `"github.com/example/knowledge-assistant/internal/awsx"` and `"github.com/example/knowledge-assistant/services/ingestion/internal/s3src"`. Update the `-source` flag usage string to `gitlab | fixtures | s3` and the unknown-source path is already handled by `default`.

- [ ] **Step 4: Run tests + build to verify they pass**

Run: `go build ./... && go test ./services/ingestion/... -v && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w services/ingestion/
git add services/ingestion/cmd/indexer/
git commit   # subject: "Wire the S3 source into the indexer" + attribution trailers
```

---

## Task 4: Owner-run `make seed-s3` + README

**Files:**
- Modify: `Makefile`, `README.md`

- [ ] **Step 1: Add the `seed-s3` target**

Add to `Makefile` (tabs, not spaces, for recipe lines). It copies each team's fixtures to its prefix; `KA_DOCS_BUCKET` must be set and AWS credentials present:

```make
# Copy the fabricated fixture corpus into the docs bucket, one prefix per team.
# Owner-run: needs AWS credentials and KA_DOCS_BUCKET. Mirrors the per-team
# layout the S3 source expects (s3://$(KA_DOCS_BUCKET)/<team>/).
seed-s3:
	@test -n "$(KA_DOCS_BUCKET)" || { echo "set KA_DOCS_BUCKET"; exit 1; }
	aws s3 cp fixtures/coupa "s3://$(KA_DOCS_BUCKET)/coupa/" --recursive --exclude "*" --include "*.md"
	aws s3 cp fixtures/star  "s3://$(KA_DOCS_BUCKET)/star/"  --recursive --exclude "*" --include "*.md"
	aws s3 cp fixtures/hr    "s3://$(KA_DOCS_BUCKET)/hr/"    --recursive --exclude "*" --include "*.md"
```

- [ ] **Step 2: Verify the target parses and guards**

Run: `make -n seed-s3` (dry run; prints the commands) and `KA_DOCS_BUCKET= make seed-s3` — the second must fail with `set KA_DOCS_BUCKET`.
Expected: dry-run prints three `aws s3 cp` lines; the unset-var run exits non-zero.

- [ ] **Step 3: Document the S3 source in the README**

Add a subsection to the ingestion area of `README.md`:

```markdown
### Ingesting from S3 (the AWS/EKS environment)

Documents live in the docs bucket under one prefix per team
(`s3://$KA_DOCS_BUCKET/<team>/`). The indexer reads a single team's prefix per
run; team is passed explicitly and never inferred from a key. PDF, Markdown and
plain text are ingested (via `internal/extract`).

Owner-run (needs AWS credentials):

    export KA_DOCS_BUCKET=ka-docs-<account-id>
    make seed-s3                                   # one-time: load the fixture corpus
    indexer -source s3 -team coupa -bucket $KA_DOCS_BUCKET
    indexer -source s3 -team star  -bucket $KA_DOCS_BUCKET
    indexer -source s3 -team hr    -bucket $KA_DOCS_BUCKET

`make seed-s3` copies `fixtures/<team>/` to each prefix; the fixtures already
sit under `fixtures/{coupa,star,hr}/`.
```

- [ ] **Step 4: Commit**

```bash
git add Makefile README.md
git commit   # subject: "Add owner-run S3 seeding target and document the S3 source" + attribution trailers
```

---

## Self-Review

**Spec coverage:** `awsx.S3` → Task 1. `s3src` package (list/paginate/extract/skip/zero-error/get-error) → Task 2. `-source s3` wiring + flags + `Doc`→`page` → Task 3. `make seed-s3` + README → Task 4. Per-team prefix, explicit team, PDF/MD/TXT reuse, hard/soft error split, offline tests — all present. `ingestion-cron.yaml` intentionally untouched (spec non-goal). All spec sections map to a task.

**Placeholder scan:** No `TBD`/`TODO`/"handle edge cases". The one deliberate marker — `TestFetchAllGetErrorFails` in Task 2 Step 1 — is called out in the same step with its real replacement and an instruction to delete the marker; not a silent placeholder.

**Type consistency:** `s3src.Client{API, Bucket, Prefix, Log}`, `s3src.Doc{Key, Text, LastModified}`, and `FetchAll(ctx) ([]Doc, error)` match across the test (Task 2), the implementation (Task 2), and the indexer wiring (Task 3). `s3Page(bucket, prefix string, d s3src.Doc) page` matches between its test (Task 3 Step 1) and definition (Task 3 Step 3). `extract.Supported(name string) bool` is introduced in Task 2 and used in `FetchAll`. `awsx.S3(ctx)` matches Task 1's definition and Task 3's use. The `page` fields used (`SpaceKey`, `ID`, `Title`, `Markdown`, `WebURL`, `UpdatedAt`) match the existing struct in `main.go`.
