# Bedrock Model Clients Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the indexer and chat-api Bedrock backends — Titan v2 embeddings, gpt-oss-20b rerank and rewrite — plus a Secrets-Manager-sourced Claude key, so the pipeline runs in the AWS/EKS daily environment without Ollama.

**Architecture:** Each Bedrock backend is a `bedrock.go` sibling of the existing `ollama.go` in `internal/embed`, `internal/rerank`, `internal/rewrite`, selected by a new `"bedrock"` mode in the same factories and config switches. Transport-independent prompt-building and JSON parsing are extracted into shared helpers so the Ollama and Bedrock backends differ only in the call. A small `internal/awsx` package builds the AWS SDK client; credentials come from the SDK default chain (EKS Pod Identity). The answer LLM stays on the direct Anthropic API; only the key's source changes.

**Tech Stack:** Go 1.25, `aws-sdk-go-v2` (`config`, `service/bedrockruntime`, `service/secretsmanager`), standard-library `net/http` and `encoding/json`, `httptest` + hand-written stubs for tests.

**Spec:** `docs/superpowers/specs/2026-09-12-bedrock-model-clients-design.md`

## Global Constraints

- **Go must be gofmt-clean.** A hook formats `.go` files after Edit/Write; after changing Go through the shell run `gofmt -w` yourself. `go vet ./...` must pass.
- **Wrap errors with a package prefix**, operator-facing: `fmt.Errorf("pkg: context: %w", err)`.
- **Tests are offline.** No test makes a live AWS call and none needs credentials. Use hand-written `stubX` types (repo convention) for the AWS SDK seams and `httptest` where an HTTP server fits. `go test ./...` stays green.
- **One embedder for both binaries.** Both build it via `embed.New(embed.OptionsFromEnv())`; never construct an embedder directly, and the mock embedder is never a silent fallback.
- **Local defaults are unchanged:** `KA_EMBED_MODE=ollama`, `KA_RERANK_MODE=off`, `KA_REWRITE_MODE=off`, `KA_LLM_MODE=mock`. Bedrock is opt-in via env.
- **Titan is 1024-dim; nomic-embed-text is 768-dim.** Changing the embedder changes the index vector width, so `EnsureIndex` refuses the old index — a deliberate drop-and-reseed (`make seed`) is required. This is the owner's live step, not part of any code task.
- **`KA_RELEVANCE_FLOOR` (0.81) is calibrated for nomic-embed-text and does not transfer to Titan.** Recalibration is owner-run (Task 8's doc); no code task sets a new number.
- **Commits are local only — never `git push`** (only the repo owner pushes). Commit subject is an imperative sentence with **no prefix** (not `feat:`), and the body explains why. End every commit message with:

  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01RZE4EXAxZNiFi4YURxWXzN
  ```

---

## File Structure

| Path | Responsibility |
|---|---|
| `internal/awsx/awsx.go` | Build a `*bedrockruntime.Client` from the SDK default credential chain; region from `AWS_REGION` |
| `internal/awsx/awsx_test.go` | Region resolution + non-nil client |
| `internal/embed/bedrock.go` | Titan v2 embedder behind an `invoker` interface |
| `internal/embed/bedrock_test.go` | Payload + response-parse + dim guard, via stub invoker |
| `internal/embed/new.go` | Add `"bedrock"` case to `New` and `OptionsFromEnv` |
| `internal/embed/new_test.go` | `New` returns the Bedrock embedder and dim for `Mode:"bedrock"` |
| `internal/rerank/prompt.go` | Shared `buildRerankPrompt` + `parseOrder` (extracted from `ollama.go`) |
| `internal/rerank/prompt_test.go` | Prompt shape + tolerant order parse |
| `internal/rerank/bedrock.go` | gpt-oss reranker via Converse, behind a `converser` interface |
| `internal/rerank/bedrock_test.go` | Converse request + reasoning-strip parse, via stub converser |
| `internal/rewrite/prompt.go` | Shared `buildRewritePrompt` + `parseQuery` (extracted from `ollama.go`) |
| `internal/rewrite/prompt_test.go` | Prompt shape + tolerant query parse |
| `internal/rewrite/bedrock.go` | gpt-oss rewriter via Converse |
| `internal/rewrite/bedrock_test.go` | Converse request + parse, via stub converser |
| `services/chat-api/internal/config/config.go` | Add `KA_ANTHROPIC_SECRET_ID`; drop `bedrock` from `LLMMode`; remove dead `BedrockModelID`/`BedrockRegion` |
| `services/chat-api/internal/secrets/secrets.go` | Read the Claude key from Secrets Manager behind a `secretGetter` interface |
| `services/chat-api/internal/secrets/secrets_test.go` | Success + fail-fast, via stub |
| `services/chat-api/cmd/server/main.go` | Secret load at startup; wire `"bedrock"` rerank/rewrite/embed; remove `bedrock` LLM branch |
| `services/chat-api/internal/bedrock/bedrock.go` | **Delete** (dead scaffolding); keep `mock.go` |
| `deploy/k8s/chat-api.yaml` | Fix the env block to the real config surface (image/host placeholders stay) |
| `docs/reference/` or README retrieval-check section | Owner-run Titan recalibration procedure |
| `go.mod` / `go.sum` | Add the three `aws-sdk-go-v2` modules |

---

## Task 1: AWS client helper (`internal/awsx`)

**Files:**
- Create: `internal/awsx/awsx.go`
- Test: `internal/awsx/awsx_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces: `func BedrockRuntime(ctx context.Context) (*bedrockruntime.Client, error)` — region from `AWS_REGION` via the SDK default chain.

- [ ] **Step 1: Add the SDK dependencies**

Run:
```bash
go get github.com/aws/aws-sdk-go-v2/config@latest \
       github.com/aws/aws-sdk-go-v2/service/bedrockruntime@latest \
       github.com/aws/aws-sdk-go-v2/service/secretsmanager@latest
go mod tidy
```
Expected: `go.mod` gains the three modules; `go.sum` updates.

- [ ] **Step 2: Write the failing test**

```go
package awsx

import (
	"context"
	"testing"
)

func TestBedrockRuntimeUsesRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	// No credentials needed: the client is built lazily and signs only on a call.
	c, err := BedrockRuntime(context.Background())
	if err != nil {
		t.Fatalf("BedrockRuntime: %v", err)
	}
	if c == nil {
		t.Fatal("BedrockRuntime returned a nil client")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/awsx/ -run TestBedrockRuntimeUsesRegion -v`
Expected: FAIL — `undefined: BedrockRuntime`.

- [ ] **Step 4: Write the implementation**

```go
// Package awsx builds AWS SDK clients from the ambient credential chain.
//
// In EKS, Pod Identity exposes credentials through the container-credentials
// endpoint that config.LoadDefaultConfig reads automatically, so there is no
// credential handling here and no static keys anywhere.
package awsx

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

// BedrockRuntime returns a Bedrock Runtime client. The region comes from the
// environment (AWS_REGION); calls fail fast if it is unset.
func BedrockRuntime(ctx context.Context) (*bedrockruntime.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("awsx: load aws config: %w", err)
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("awsx: no AWS region; set AWS_REGION")
	}
	return bedrockruntime.NewFromConfig(cfg), nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/awsx/ -v && go vet ./internal/awsx/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/awsx/
git add go.mod go.sum internal/awsx/
git commit   # subject: "Add AWS SDK Bedrock client helper" + attribution trailers
```

---

## Task 2: Titan v2 embedder (`internal/embed/bedrock.go`)

**Files:**
- Create: `internal/embed/bedrock.go`, `internal/embed/bedrock_test.go`
- Modify: `internal/embed/new.go`, `internal/embed/new_test.go`

**Interfaces:**
- Consumes: `rag.Embedder` (`Embed(ctx, text) ([]float32, error)`), `awsx.BedrockRuntime`.
- Produces: `Bedrock` struct with fields `Model string`, `Dim int`, and an `invoker` it depends on; `embed.New` returns it for `Mode:"bedrock"`.

- [ ] **Step 1: Write the failing test (payload + parse + dim guard)**

```go
package embed

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

type stubInvoker struct {
	gotBody []byte
	out     []byte
	err     error
}

func (s *stubInvoker) InvokeModel(_ context.Context, in *bedrockruntime.InvokeModelInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error) {
	s.gotBody = in.Body
	if s.err != nil {
		return nil, s.err
	}
	return &bedrockruntime.InvokeModelOutput{Body: s.out}, nil
}

func TestBedrockEmbedRequestAndParse(t *testing.T) {
	stub := &stubInvoker{out: []byte(`{"embedding":[0.1,0.2,0.3],"inputTextTokenCount":2}`)}
	e := Bedrock{Model: "amazon.titan-embed-text-v2:0", Dim: 3, api: stub}

	v, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v) != 3 || v[0] != 0.1 {
		t.Fatalf("unexpected vector %v", v)
	}

	var req struct {
		InputText  string `json:"inputText"`
		Dimensions int    `json:"dimensions"`
		Normalize  bool   `json:"normalize"`
	}
	if err := json.Unmarshal(stub.gotBody, &req); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	if req.InputText != "hello world" || req.Dimensions != 3 || !req.Normalize {
		t.Fatalf("unexpected request %+v", req)
	}
}

func TestBedrockEmbedDimMismatch(t *testing.T) {
	stub := &stubInvoker{out: []byte(`{"embedding":[0.1,0.2]}`)}
	e := Bedrock{Model: "amazon.titan-embed-text-v2:0", Dim: 3, api: stub}
	if _, err := e.Embed(context.Background(), "x"); err == nil {
		t.Fatal("expected a dimension-mismatch error")
	}
}

var _ = aws.String // keep the aws import if unused after edits
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/embed/ -run TestBedrockEmbed -v`
Expected: FAIL — `undefined: Bedrock`.

- [ ] **Step 3: Write the implementation**

```go
package embed

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

// invoker is the slice of the Bedrock Runtime client this package uses. A
// narrow interface keeps the SDK out of tests, which stub it.
type invoker interface {
	InvokeModel(ctx context.Context, in *bedrockruntime.InvokeModelInput, opts ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error)
}

// Bedrock embeds text with Amazon Titan Text Embeddings v2. Dim is the width
// the caller expects and is enforced on every response, for the same reason
// the Ollama embedder enforces it: the index mapping fixes the width, and a
// mismatch otherwise surfaces far from its cause.
type Bedrock struct {
	Model string
	Dim   int
	api   invoker
}

func (b Bedrock) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(map[string]any{
		"inputText":  text,
		"dimensions": b.Dim,
		"normalize":  true,
	})
	if err != nil {
		return nil, fmt.Errorf("bedrock embed: encode request: %w", err)
	}
	out, err := b.api.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(b.Model),
		Body:        body,
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
	})
	if err != nil {
		return nil, fmt.Errorf("bedrock embed: invoke %s: %w", b.Model, err)
	}
	var resp struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.Unmarshal(out.Body, &resp); err != nil {
		return nil, fmt.Errorf("bedrock embed: decode response: %w", err)
	}
	if b.Dim > 0 && len(resp.Embedding) != b.Dim {
		return nil, fmt.Errorf(
			"bedrock embed: model %q returned a %d-dimension vector but %d was configured; "+
				"set KA_EMBED_DIM to match KA_EMBED_MODEL, then delete and reindex",
			b.Model, len(resp.Embedding), b.Dim)
	}
	return resp.Embedding, nil
}
```

- [ ] **Step 4: Wire `"bedrock"` into the factory**

In `internal/embed/new.go`, add a case to `New`'s switch (before `default`):

```go
	case "bedrock":
		client, err := awsx.BedrockRuntime(context.Background())
		if err != nil {
			return nil, 0, fmt.Errorf("embed: %w", err)
		}
		return Bedrock{Model: o.Model, Dim: o.Dim, api: client}, o.Dim, nil
```

Add the imports `"context"` and `"github.com/example/knowledge-assistant/internal/awsx"` to `new.go`. Update the `Mode` doc comment on `Options` to `"ollama" | "bedrock" | "mock"` and the error string in `default` to list `bedrock`.

- [ ] **Step 5: Add the factory test**

In `internal/embed/new_test.go`:

```go
func TestNewBedrock(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	e, dim, err := New(Options{Mode: "bedrock", Model: "amazon.titan-embed-text-v2:0", Dim: 1024})
	if err != nil {
		t.Fatalf("New bedrock: %v", err)
	}
	if dim != 1024 {
		t.Fatalf("dim = %d, want 1024", dim)
	}
	if _, ok := e.(Bedrock); !ok {
		t.Fatalf("got %T, want Bedrock", e)
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/embed/ -v && go vet ./internal/embed/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/embed/
git add internal/embed/
git commit   # subject: "Add Bedrock Titan embedder" + attribution trailers
```

---

## Task 3: Extract shared rerank prompt/parse helpers

Pure refactor — behavior and existing tests unchanged. This isolates the transport-independent parts so Task 4's Bedrock backend reuses them.

**Files:**
- Create: `internal/rerank/prompt.go`, `internal/rerank/prompt_test.go`
- Modify: `internal/rerank/ollama.go`

**Interfaces:**
- Produces: `func buildRerankPrompt(query string, chunks []rag.Chunk) string`; `func parseOrder(content string) ([]int, error)` (tolerant: extracts the JSON object from any surrounding text); `systemPrompt` and `orderSchema` move to `prompt.go` unchanged.

- [ ] **Step 1: Write the failing test for the tolerant parser**

```go
package rerank

import "testing"

func TestParseOrderStripsReasoning(t *testing.T) {
	// A reasoning model may emit its thinking before the JSON.
	in := `We should rank chunk 2 first. {"order":[2,1,3]} done.`
	got, err := parseOrder(in)
	if err != nil {
		t.Fatalf("parseOrder: %v", err)
	}
	if len(got) != 3 || got[0] != 2 {
		t.Fatalf("got %v, want [2 1 3]", got)
	}
}

func TestParseOrderEmpty(t *testing.T) {
	if _, err := parseOrder(`{"order":[]}`); err == nil {
		t.Fatal("expected error on empty ranking")
	}
}

func TestBuildRerankPromptNumbersChunks(t *testing.T) {
	p := buildRerankPrompt("q?", []rag.Chunk{{Text: "a"}, {Text: "b"}})
	if !contains(p, "# CHUNK ID: 1") || !contains(p, "# CHUNK ID: 2") {
		t.Fatalf("prompt missing chunk ids:\n%s", p)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rerank/ -run 'TestParseOrder|TestBuildRerankPrompt' -v`
Expected: FAIL — `undefined: parseOrder` / `buildRerankPrompt`.

- [ ] **Step 3: Create `prompt.go` and move the shared parts**

Move `systemPrompt` and `orderSchema` from `ollama.go` into a new `internal/rerank/prompt.go`, and add:

```go
package rerank

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/knowledge-assistant/internal/rag"
)

// buildRerankPrompt renders the user turn: the question and the numbered
// chunks, in retrieval order.
func buildRerankPrompt(query string, chunks []rag.Chunk) string {
	var u strings.Builder
	fmt.Fprintf(&u, "Question:\n\n%s\n\nChunks:\n\n", query)
	for i, c := range chunks {
		fmt.Fprintf(&u, "# CHUNK ID: %d\nsource: %s — %s\n\n%s\n\n", i+1, c.PageTitle, c.SectionPath, c.Text)
	}
	fmt.Fprintf(&u, "Rank all %d chunk ids by relevance to the question, most relevant first.", len(chunks))
	return u.String()
}

// parseOrder reads {"order":[ints]} out of a model reply, tolerating any
// reasoning text around the JSON object. Ollama's structured output returns
// clean JSON; Bedrock's Converse may wrap it in prose, so extraction is done
// here rather than relying on the transport.
func parseOrder(content string) ([]int, error) {
	obj, err := extractJSONObject(content)
	if err != nil {
		return nil, fmt.Errorf("rerank: decode ranking %q: %w", truncate(content, 200), err)
	}
	var ranking struct {
		Order []int `json:"order"`
	}
	if err := json.Unmarshal([]byte(obj), &ranking); err != nil {
		return nil, fmt.Errorf("rerank: decode ranking %q: %w", truncate(content, 200), err)
	}
	if len(ranking.Order) == 0 {
		return nil, fmt.Errorf("rerank: model returned an empty ranking")
	}
	return ranking.Order, nil
}

// extractJSONObject returns the substring from the first '{' to the last '}'.
func extractJSONObject(s string) (string, error) {
	i, j := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}')
	if i < 0 || j < i {
		return "", fmt.Errorf("no JSON object found")
	}
	return s[i : j+1], nil
}
```

- [ ] **Step 4: Reduce `ollama.go` to use the helpers**

In `ollama.go`'s `Rerank`, replace the inline prompt-building with `u := buildRerankPrompt(query, chunks)` (used as the user message content), and replace the inline ranking decode + empty check with:

```go
	order, err := parseOrder(out.Message.Content)
	if err != nil {
		return nil, err
	}
	return Reorder(chunks, order), nil
```

Delete the now-moved `systemPrompt`/`orderSchema` declarations from `ollama.go`. Keep `truncate` where the whole package can see it (it stays in `ollama.go`; `prompt.go` uses it).

- [ ] **Step 5: Run the whole package's tests**

Run: `go test ./internal/rerank/ -v && go vet ./internal/rerank/`
Expected: PASS — new helper tests **and** the pre-existing Ollama tests (behavior unchanged).

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/rerank/
git add internal/rerank/
git commit   # subject: "Extract shared rerank prompt and parser" + attribution trailers
```

---

## Task 4: Bedrock reranker (`internal/rerank/bedrock.go`)

**Files:**
- Create: `internal/rerank/bedrock.go`, `internal/rerank/bedrock_test.go`
- Modify: `services/chat-api/cmd/server/main.go` (wire `"bedrock"` mode)

**Interfaces:**
- Consumes: `buildRerankPrompt`, `parseOrder`, `Reorder`, `systemPrompt` (Task 3); `awsx.BedrockRuntime`.
- Produces: `Bedrock` struct with `Model string` and a `converser`; implements `rag.Reranker`.

- [ ] **Step 0: Confirm the Converse contract (research, no code)**

Read the pinned `aws-sdk-go-v2/service/bedrockruntime` docs for `Converse`: the exact `types.SystemContentBlock`, `types.Message`/`types.ContentBlock` constructors, `types.InferenceConfiguration`, and the `ConverseOutput.Output` union member for the assistant message. Confirm `openai.gpt-oss-20b-1:0` is invokable via `Converse` and how it returns text (it may emit a reasoning block before the text block). Note the exact type names you find; the code below uses the v1.x names and may need adjusting to the pinned version.

- [ ] **Step 1: Write the failing test (Converse request + reasoning-strip parse)**

```go
package rerank

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/example/knowledge-assistant/internal/rag"
)

type stubConverser struct {
	in   *bedrockruntime.ConverseInput
	text string
}

func (s *stubConverser) Converse(_ context.Context, in *bedrockruntime.ConverseInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	s.in = in
	return &bedrockruntime.ConverseOutput{
		Output: &brtypes.ConverseOutputMemberMessage{
			Value: brtypes.Message{
				Role:    brtypes.ConversationRoleAssistant,
				Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: s.text}},
			},
		},
	}, nil
}

func TestBedrockRerankReordersAndSendsSystem(t *testing.T) {
	stub := &stubConverser{text: `thinking... {"order":[3,1,2]}`}
	r := Bedrock{Model: "openai.gpt-oss-20b-1:0", api: stub}
	chunks := []rag.Chunk{{Text: "a"}, {Text: "b"}, {Text: "c"}}

	got, err := r.Rerank(context.Background(), "q?", chunks)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if got[0].Text != "c" {
		t.Fatalf("expected chunk c first, got %q", got[0].Text)
	}
	if aws.ToString(stub.in.ModelId) != "openai.gpt-oss-20b-1:0" {
		t.Fatalf("wrong model id %q", aws.ToString(stub.in.ModelId))
	}
	if len(stub.in.System) == 0 {
		t.Fatal("system prompt not sent")
	}
}

func TestBedrockRerankShortCircuits(t *testing.T) {
	stub := &stubConverser{text: "should not be called"}
	r := Bedrock{Model: "m", api: stub}
	one := []rag.Chunk{{Text: "a"}}
	got, err := r.Rerank(context.Background(), "q", one)
	if err != nil || len(got) != 1 {
		t.Fatalf("short-circuit failed: %v %v", got, err)
	}
	if stub.in != nil {
		t.Fatal("model was called for a single chunk")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rerank/ -run TestBedrockRerank -v`
Expected: FAIL — `undefined: Bedrock`.

- [ ] **Step 3: Write the implementation**

```go
package rerank

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/example/knowledge-assistant/internal/rag"
)

type converser interface {
	Converse(ctx context.Context, in *bedrockruntime.ConverseInput, opts ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

// Bedrock reranks with gpt-oss-20b on Bedrock via the Converse API. It shares
// the prompt and the tolerant parser with the Ollama backend; only the call
// differs.
type Bedrock struct {
	Model string
	api   converser
}

func (b Bedrock) Rerank(ctx context.Context, query string, chunks []rag.Chunk) ([]rag.Chunk, error) {
	if len(chunks) < 2 {
		return chunks, nil
	}
	out, err := b.api.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId: aws.String(b.Model),
		System:  []brtypes.SystemContentBlock{&brtypes.SystemContentBlockMemberText{Value: systemPrompt}},
		Messages: []brtypes.Message{{
			Role:    brtypes.ConversationRoleUser,
			Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: buildRerankPrompt(query, chunks)}},
		}},
		InferenceConfig: &brtypes.InferenceConfiguration{Temperature: aws.Float32(0)},
	})
	if err != nil {
		return nil, fmt.Errorf("rerank: bedrock converse %s: %w", b.Model, err)
	}
	order, err := parseOrder(converseText(out))
	if err != nil {
		return nil, err
	}
	return Reorder(chunks, order), nil
}

// converseText concatenates the assistant message's text blocks, skipping any
// reasoning-only blocks a model such as gpt-oss emits first.
func converseText(out *bedrockruntime.ConverseOutput) string {
	msg, ok := out.Output.(*brtypes.ConverseOutputMemberMessage)
	if !ok {
		return ""
	}
	var s string
	for _, block := range msg.Value.Content {
		if t, ok := block.(*brtypes.ContentBlockMemberText); ok {
			s += t.Value
		}
	}
	return s
}
```

- [ ] **Step 4: Wire `"bedrock"` in `main.go`**

In `services/chat-api/cmd/server/main.go`, add to the `RerankMode` switch:

```go
	case "bedrock":
		client, err := awsx.BedrockRuntime(ctx)
		if err != nil {
			log.Error("bedrock reranker", "err", err)
			os.Exit(1)
		}
		orch.Reranker = rerank.Bedrock{Model: cfg.RerankModel, api: client}
		log.Info("using reranker", "mode", "bedrock", "model", cfg.RerankModel)
```

Note: `rerank.Bedrock.api` is unexported, so add an exported constructor `rerank.NewBedrock(model string, api converser) Bedrock` **or** export the field. Choose the constructor: add to `bedrock.go`:

```go
// NewBedrock builds a Bedrock reranker. The client is taken as the narrow
// converser interface so callers in tests can substitute a stub.
func NewBedrock(model string, api converser) Bedrock { return Bedrock{Model: model, api: api} }
```

and in `main.go` use `rerank.NewBedrock(cfg.RerankModel, client)`. Update the unknown-mode error string to list `bedrock`. Ensure `main.go` has a `ctx` in scope (use `context.Background()` if not) and imports `awsx`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/rerank/ ./services/chat-api/... -v && go vet ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/rerank/ services/chat-api/
git add internal/rerank/ services/chat-api/cmd/server/main.go
git commit   # subject: "Add Bedrock gpt-oss reranker" + attribution trailers
```

---

## Task 5: Extract shared rewrite prompt/parse helpers

Same shape as Task 3, for `internal/rewrite`.

**Files:**
- Create: `internal/rewrite/prompt.go`, `internal/rewrite/prompt_test.go`
- Modify: `internal/rewrite/ollama.go`

**Interfaces:**
- Produces: `func buildRewritePrompt(question string, history []rag.Turn, maxTurns int) string`; `func parseQuery(content string) (string, error)` (tolerant JSON extraction of `{"query":"..."}`); `systemPrompt`/`querySchema` move to `prompt.go`.

- [ ] **Step 1: Write the failing test**

```go
package rewrite

import (
	"testing"

	"github.com/example/knowledge-assistant/internal/rag"
)

func TestParseQueryStripsReasoning(t *testing.T) {
	got, err := parseQuery(`We resolve the reference. {"query":"Louisiana data call deadline"}`)
	if err != nil {
		t.Fatalf("parseQuery: %v", err)
	}
	if got != "Louisiana data call deadline" {
		t.Fatalf("got %q", got)
	}
}

func TestParseQueryEmpty(t *testing.T) {
	if _, err := parseQuery(`{"query":"  "}`); err == nil {
		t.Fatal("expected error on empty query")
	}
}

func TestBuildRewritePromptTrimsHistory(t *testing.T) {
	hist := make([]rag.Turn, 10)
	for i := range hist {
		hist[i] = rag.Turn{Role: "user", Content: "x"}
	}
	p := buildRewritePrompt("latest?", hist, 6)
	if !strContains(p, "Latest user message:") {
		t.Fatalf("prompt missing latest marker:\n%s", p)
	}
}

func strContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rewrite/ -run 'TestParseQuery|TestBuildRewritePrompt' -v`
Expected: FAIL — `undefined: parseQuery` / `buildRewritePrompt`.

- [ ] **Step 3: Create `prompt.go`**

Move `systemPrompt`, `orderSchemaKey`, `querySchema` from `ollama.go` into `internal/rewrite/prompt.go`, and add:

```go
package rewrite

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/knowledge-assistant/internal/rag"
)

func buildRewritePrompt(question string, history []rag.Turn, maxTurns int) string {
	if maxTurns > 0 && len(history) > maxTurns {
		history = history[len(history)-maxTurns:]
	}
	var u strings.Builder
	if len(history) == 0 {
		u.WriteString("There is no conversation yet; this is the user's first message.\n\n")
	} else {
		u.WriteString("Conversation so far:\n\n")
		for _, t := range history {
			fmt.Fprintf(&u, "%s: %s\n\n", t.Role, truncate(t.Content, 1500))
		}
	}
	fmt.Fprintf(&u, "Latest user message:\n\n%s\n\nWrite the knowledge base search query.", question)
	return u.String()
}

func parseQuery(content string) (string, error) {
	i, j := strings.IndexByte(content, '{'), strings.LastIndexByte(content, '}')
	if i < 0 || j < i {
		return "", fmt.Errorf("rewrite: decode query %q: no JSON object found", truncate(content, 200))
	}
	var parsed struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(content[i:j+1]), &parsed); err != nil {
		return "", fmt.Errorf("rewrite: decode query %q: %w", truncate(content, 200), err)
	}
	if strings.TrimSpace(parsed.Query) == "" {
		return "", fmt.Errorf("rewrite: model returned an empty query")
	}
	return strings.TrimSpace(parsed.Query), nil
}
```

- [ ] **Step 4: Reduce `ollama.go` to use the helpers**

In `Rewrite`, replace the history-trim + prompt-build block with `u := buildRewritePrompt(question, history, o.maxTurns())`, and replace the decode + empty-check tail with:

```go
	return parseQuery(out.Message.Content)
```

Delete the moved declarations from `ollama.go`; keep `truncate` and `maxTurns()` there.

- [ ] **Step 5: Run the package's tests**

Run: `go test ./internal/rewrite/ -v && go vet ./internal/rewrite/`
Expected: PASS (new + pre-existing).

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/rewrite/
git add internal/rewrite/
git commit   # subject: "Extract shared rewrite prompt and parser" + attribution trailers
```

---

## Task 6: Bedrock rewriter (`internal/rewrite/bedrock.go`)

**Files:**
- Create: `internal/rewrite/bedrock.go`, `internal/rewrite/bedrock_test.go`
- Modify: `services/chat-api/cmd/server/main.go` (wire `"bedrock"`)

**Interfaces:**
- Consumes: `buildRewritePrompt`, `parseQuery`, `systemPrompt` (Task 5); `awsx.BedrockRuntime`.
- Produces: `Bedrock` struct (`Model string`, `MaxTurns int`, a `converser`); `NewBedrock(model string, maxTurns int, api converser) Bedrock`; implements `rag.Rewriter`.

- [ ] **Step 1: Write the failing test**

```go
package rewrite

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/example/knowledge-assistant/internal/rag"
)

type stubConverser struct {
	in   *bedrockruntime.ConverseInput
	text string
}

func (s *stubConverser) Converse(_ context.Context, in *bedrockruntime.ConverseInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	s.in = in
	return &bedrockruntime.ConverseOutput{
		Output: &brtypes.ConverseOutputMemberMessage{
			Value: brtypes.Message{Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: s.text}}},
		},
	}, nil
}

func TestBedrockRewrite(t *testing.T) {
	stub := &stubConverser{text: `{"query":"AVR field mapping"}`}
	r := NewBedrock("openai.gpt-oss-20b-1:0", 6, stub)
	got, err := r.Rewrite(context.Background(), "I mean AVR", []rag.Turn{{Role: "user", Content: "fields?"}})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if got != "AVR field mapping" {
		t.Fatalf("got %q", got)
	}
	if aws.ToString(stub.in.ModelId) != "openai.gpt-oss-20b-1:0" {
		t.Fatalf("wrong model %q", aws.ToString(stub.in.ModelId))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rewrite/ -run TestBedrockRewrite -v`
Expected: FAIL — `undefined: NewBedrock`.

- [ ] **Step 3: Write the implementation**

```go
package rewrite

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/example/knowledge-assistant/internal/rag"
)

type converser interface {
	Converse(ctx context.Context, in *bedrockruntime.ConverseInput, opts ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

// Bedrock rewrites follow-up questions with gpt-oss-20b on Bedrock via Converse.
type Bedrock struct {
	Model    string
	MaxTurns int
	api      converser
}

func NewBedrock(model string, maxTurns int, api converser) Bedrock {
	return Bedrock{Model: model, MaxTurns: maxTurns, api: api}
}

func (b Bedrock) Rewrite(ctx context.Context, question string, history []rag.Turn) (string, error) {
	out, err := b.api.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId: aws.String(b.Model),
		System:  []brtypes.SystemContentBlock{&brtypes.SystemContentBlockMemberText{Value: systemPrompt}},
		Messages: []brtypes.Message{{
			Role:    brtypes.ConversationRoleUser,
			Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: buildRewritePrompt(question, history, b.MaxTurns)}},
		}},
		InferenceConfig: &brtypes.InferenceConfiguration{Temperature: aws.Float32(0)},
	})
	if err != nil {
		return "", fmt.Errorf("rewrite: bedrock converse %s: %w", b.Model, err)
	}
	msg, ok := out.Output.(*brtypes.ConverseOutputMemberMessage)
	if !ok {
		return "", fmt.Errorf("rewrite: bedrock returned no message")
	}
	var text string
	for _, block := range msg.Value.Content {
		if t, ok := block.(*brtypes.ContentBlockMemberText); ok {
			text += t.Value
		}
	}
	return parseQuery(text)
}
```

- [ ] **Step 4: Wire `"bedrock"` in `main.go`**

Add to the `RewriteMode` switch, mirroring the reranker:

```go
	case "bedrock":
		client, err := awsx.BedrockRuntime(ctx)
		if err != nil {
			log.Error("bedrock rewriter", "err", err)
			os.Exit(1)
		}
		orch.Rewriter = rewrite.NewBedrock(cfg.RewriteModel, 0, client)
		log.Info("using query rewriter", "mode", "bedrock", "model", cfg.RewriteModel)
```

Update the unknown-mode error string to list `bedrock`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/rewrite/ ./services/chat-api/... -v && go vet ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/rewrite/ services/chat-api/
git add internal/rewrite/ services/chat-api/cmd/server/main.go
git commit   # subject: "Add Bedrock gpt-oss rewriter" + attribution trailers
```

---

## Task 7: Secrets-Manager Claude key + config cleanup

**Files:**
- Create: `services/chat-api/internal/secrets/secrets.go`, `.../secrets_test.go`
- Modify: `services/chat-api/internal/config/config.go`, `services/chat-api/cmd/server/main.go`
- Delete: `services/chat-api/internal/bedrock/bedrock.go`

**Interfaces:**
- Produces: `func FetchAPIKey(ctx context.Context, api secretGetter, secretID string) (string, error)`; `secretGetter` = one-method interface over `secretsmanager.GetSecretValue`.

- [ ] **Step 1: Write the failing test**

```go
package secrets

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type stubSM struct {
	val string
	err error
}

func (s stubSM) GetSecretValue(_ context.Context, _ *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(s.val)}, nil
}

func TestFetchAPIKey(t *testing.T) {
	got, err := FetchAPIKey(context.Background(), stubSM{val: "sk-test"}, "ka/anthropic-api-key")
	if err != nil {
		t.Fatalf("FetchAPIKey: %v", err)
	}
	if got != "sk-test" {
		t.Fatalf("got %q", got)
	}
}

func TestFetchAPIKeyError(t *testing.T) {
	_, err := FetchAPIKey(context.Background(), stubSM{err: errors.New("denied")}, "id")
	if err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./services/chat-api/internal/secrets/ -v`
Expected: FAIL — `undefined: FetchAPIKey`.

- [ ] **Step 3: Write the implementation**

```go
// Package secrets loads runtime secrets from AWS Secrets Manager.
package secrets

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type secretGetter interface {
	GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput, opts ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// FetchAPIKey returns the plaintext secret value for secretID.
func FetchAPIKey(ctx context.Context, api secretGetter, secretID string) (string, error) {
	out, err := api.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(secretID)})
	if err != nil {
		return "", fmt.Errorf("secrets: get %q: %w", secretID, err)
	}
	if out.SecretString == nil || strings.TrimSpace(*out.SecretString) == "" {
		return "", fmt.Errorf("secrets: %q has no string value", secretID)
	}
	return strings.TrimSpace(*out.SecretString), nil
}

// Client builds a Secrets Manager client from the ambient credential chain.
func Client(ctx context.Context) (*secretsmanager.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("secrets: load aws config: %w", err)
	}
	return secretsmanager.NewFromConfig(cfg), nil
}
```

- [ ] **Step 4: Update `config.go`**

- Add field `AnthropicSecretID string` with a doc comment: *"If set, the Claude key is read from this Secrets Manager id at startup instead of ANTHROPIC_API_KEY."*
- In `Load()`, add `AnthropicSecretID: os.Getenv("KA_ANTHROPIC_SECRET_ID"),`.
- Change the `LLMMode` doc comment to `// "mock" | "anthropic"`.
- Delete the `BedrockModelID` and `BedrockRegion` fields and their two lines in `Load()` (dead once the Bedrock LLM path is gone; Bedrock backends read `AWS_REGION` via `internal/awsx`).

- [ ] **Step 5: Update `main.go`**

- Before the `LLMMode` switch, resolve the key from Secrets Manager when configured:

```go
	if cfg.AnthropicSecretID != "" {
		sm, err := secrets.Client(ctx)
		if err != nil {
			log.Error("secrets client", "err", err)
			os.Exit(1)
		}
		key, err := secrets.FetchAPIKey(ctx, sm, cfg.AnthropicSecretID)
		if err != nil {
			log.Error("read anthropic key from secrets manager", "err", err)
			os.Exit(1) // fail fast: a broken key must stop the process, not degrade it
		}
		cfg.AnthropicAPIKey = key
	}
```

- Remove the `case "bedrock":` branch from the `LLMMode` switch (the one that warned and fell back to `bedrock.MockLLM{}`). Keep the `default` mock branch, but change it to use the surviving mock type. Since `mock.go` stays in the `bedrock` package, `bedrock.MockLLM{}` still compiles; leave the `default` branch as-is and drop only the `bedrock` case and, if now unused, the `"bedrock"` mention in comments.
- Ensure `ctx` exists (add `ctx := context.Background()` near the top of `main` if not already present) and import `services/chat-api/internal/secrets`.

- [ ] **Step 6: Delete the dead scaffolding**

```bash
git rm services/chat-api/internal/bedrock/bedrock.go
```
Confirm `services/chat-api/internal/bedrock/mock.go` (with `MockLLM`) remains and still compiles.

- [ ] **Step 7: Run the full suite**

Run: `go build ./... && go test ./... && go vet ./...`
Expected: PASS. In particular `services/chat-api/...` builds without `BedrockModelID`/`BedrockRegion`.

- [ ] **Step 8: Commit**

```bash
gofmt -w services/chat-api/
git add -A services/chat-api/
git commit   # subject: "Read the Claude key from Secrets Manager and drop the dead Bedrock LLM path" + attribution trailers
```

---

## Task 8: Manifest env fix + owner-run recalibration doc

**Files:**
- Modify: `deploy/k8s/chat-api.yaml`
- Create/Modify: the retrieval-check docs (README "Retrieval check" section, referenced from CLAUDE.md)

- [ ] **Step 1: Fix the chat-api manifest env block**

In `deploy/k8s/chat-api.yaml`, replace the env entries so they match the config surface. Keep the `<account>` image and the ingress host placeholders untouched (those are sub-project 4).

```yaml
          env:
            - { name: KA_LLM_MODE, value: anthropic }
            - { name: AWS_REGION, value: us-east-1 }
            - { name: KA_ANTHROPIC_SECRET_ID, value: ka/anthropic-api-key }
            - { name: KA_EMBED_MODE, value: bedrock }
            - { name: KA_EMBED_MODEL, value: "amazon.titan-embed-text-v2:0" }
            - { name: KA_EMBED_DIM, value: "1024" }
            - { name: KA_RERANK_MODE, value: bedrock }
            - { name: KA_RERANK_MODEL, value: "openai.gpt-oss-20b-1:0" }
            - { name: KA_REWRITE_MODE, value: bedrock }
            - { name: KA_REWRITE_MODEL, value: "openai.gpt-oss-20b-1:0" }
            - { name: KA_OPENSEARCH_URL, valueFrom: { secretKeyRef: { name: ka-config, key: opensearch_url } } }
```

Remove the old `KA_LLM_MODE=bedrock` and `KA_BEDROCK_MODEL` lines. Do **not** set `ANTHROPIC_API_KEY` — the key now comes from Secrets Manager. (The `ingestion-cron.yaml` embedder becomes Bedrock in sub-project 3/4 alongside the S3 source; leave it for now.)

- [ ] **Step 2: Verify the manifest parses**

Run: `python3 -c "import yaml,sys; list(yaml.safe_load_all(open('deploy/k8s/chat-api.yaml')))" && echo OK`
Expected: `OK` (no `kubectl`/cluster needed).

- [ ] **Step 3: Write the recalibration procedure**

Add a subsection to the README's retrieval-check area (the section CLAUDE.md points `/check-retrieval` at), titled "Recalibrating for a new embedder", containing:

```markdown
### Recalibrating for a new embedder (owner-run, needs AWS)

Titan v2 (1024-dim, normalized) has a different cosine distribution than
nomic-embed-text (768-dim), so `KA_RELEVANCE_FLOOR` must be re-derived and
the index reseeded. Neither Claude nor CI can do this — it needs live Bedrock.

1. Export the Bedrock embed settings and AWS creds:
   `KA_EMBED_MODE=bedrock KA_EMBED_MODEL=amazon.titan-embed-text-v2:0 KA_EMBED_DIM=1024 AWS_REGION=us-east-1`.
2. `make seed` — drops the 768-dim index first, so the width change is clean.
3. `python3 scripts/check_corpus.py --scores` — read the lowest answerable
   top-score and the highest not-answerable-here top-score.
4. Set `KA_RELEVANCE_FLOOR` to the midpoint of that gap (as the config comment
   in `services/chat-api/internal/config/config.go` documents for nomic).
5. `python3 scripts/check_corpus.py --validate` to confirm the split.
```

- [ ] **Step 4: Commit**

```bash
git add deploy/k8s/chat-api.yaml README.md
git commit   # subject: "Align chat-api manifest with Bedrock config and document recalibration" + attribution trailers
```

---

## Self-Review

**Spec coverage:**
- Titan embedder → Task 2. gpt-oss rerank → Tasks 3–4. gpt-oss rewrite → Tasks 5–6. Secrets-Manager key → Task 7. Config surface → Tasks 2/4/6/7. Manifest fix → Task 8. Owner-run recalibration → Task 8 doc. Delete stale stub / drop `bedrock` LLM mode → Task 7. Shared-helper refactor → Tasks 3/5. `internal/awsx` + SDK deps → Task 1. Offline-test convention → every task. All spec sections map to a task.

**Placeholder scan:** No `TBD`/`TODO`/"handle edge cases"/"similar to Task N". Task 4 Step 0 is a genuine research step (the spec's stated Converse unknown), not a code placeholder; the code that follows is concrete and only its SDK type names may need version-pinning adjustment, which Step 0 verifies.

**Type consistency:** `invoker` (embed, `InvokeModel`) and `converser` (rerank/rewrite, `Converse`) are distinct and each defined in the package that uses it. `NewBedrock` signatures differ intentionally: `rerank.NewBedrock(model, api)` vs `rewrite.NewBedrock(model, maxTurns, api)`. `buildRerankPrompt`/`parseOrder` (rerank) and `buildRewritePrompt`/`parseQuery` (rewrite) are package-local. `FetchAPIKey`/`secretGetter` (Task 7) match their test. `awsx.BedrockRuntime(ctx)` is used consistently in Tasks 1/2/4/6.

**Note on the two `truncate`s and two `systemPrompt`s:** they are package-scoped (one per package), so `rerank` and `rewrite` each keep their own — no collision.
