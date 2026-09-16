# KB Upload Review & PII Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Intercept the `persist=true` upload path so a document is PII-scanned, hard-blocked if it contains high-severity sensitive data, and otherwise held in a human review queue until an admin approves it into the index.

**Architecture:** A deterministic regex PII detector (`internal/pii`) and a pending-review queue (`internal/review`) both sit behind interfaces. The upload handler scans extracted text: high-severity findings return `422`; clean docs are enqueued as `pending`. A new admin handler lists pending items and, on approval, runs the existing `ingest.Ingest` into OpenSearch. Session-scoped upload is untouched.

**Tech Stack:** Go 1.25 (chi router, `httptest`), `github.com/xuri/excelize/v2` for `.xlsx`, stdlib `archive/zip`/`regexp` for `.docx`, React + Vitest for the UI.

**Spec:** `docs/superpowers/specs/2026-09-15-kb-upload-review-pii-design.md`

## Global Constraints

- Go must be gofmt-clean; wrap errors as `fmt.Errorf("pkg: context: %w", err)`; operator-facing errors say how to fix.
- Comments explain *why* (failure mode / measured result), not *what*.
- Team is required on every request and every indexed doc and is never inferred from content or path. The review queue is team-scoped.
- Tests use `httptest` servers for HTTP and hand-written `stubX` types in `*_test.go`.
- HTTP routes are registered on the chi router in `services/chat-api/cmd/server/main.go`; the upload route is `POST /v1/uploads`.
- The persist path must not call `ingest.Ingest` inline — indexing happens only on admin approval.
- Indexed output on approval must be byte-identical to today's persist path: `SpaceKey: "UPLOAD"`, `URL: "upload://<id>/<filename>"`, `PageID: <reviewID>`, team from the item.
- Only the repo owner runs `git push`; committing is fine when reached in steps.

---

### Task 1: `internal/pii` — deterministic detector

**Files:**
- Create: `internal/pii/pii.go`
- Test: `internal/pii/pii_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Severity int` with `const (Low Severity = iota; High)`
  - `type Finding struct { Category string; Severity Severity; Excerpt string; Offset int }`
  - `type Detector interface { Scan(text string) []Finding }`
  - `func NewRegexDetector() Detector`
  - `func HasHigh(findings []Finding) bool`

- [ ] **Step 1: Write the failing test**

```go
package pii

import "testing"

func TestRegexDetector_HighSeverity(t *testing.T) {
	d := NewRegexDetector()
	cases := []struct {
		name, text, cat string
	}{
		{"ssn", "employee ssn 123-45-6789 on file", "ssn"},
		{"visa card", "card 4111 1111 1111 1111 charged", "credit_card"},
		{"dob", "DOB: 04/12/1985", "dob"},
		{"bank", "account number 000123456789 at bank", "bank_account"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := d.Scan(c.text)
			if !HasHigh(fs) {
				t.Fatalf("expected a high-severity finding, got %+v", fs)
			}
			found := false
			for _, f := range fs {
				if f.Category == c.cat {
					found = true
					if f.Severity != High {
						t.Errorf("%s: severity = %v, want High", c.cat, f.Severity)
					}
				}
			}
			if !found {
				t.Errorf("category %q not found in %+v", c.cat, fs)
			}
		})
	}
}

func TestRegexDetector_LowSeverity(t *testing.T) {
	d := NewRegexDetector()
	fs := d.Scan("reach me at jane@example.com or (415) 555-0142")
	if HasHigh(fs) {
		t.Fatalf("email/phone must not be high severity: %+v", fs)
	}
	got := map[string]bool{}
	for _, f := range fs {
		got[f.Category] = true
	}
	if !got["email"] || !got["phone"] {
		t.Errorf("want email+phone, got %+v", fs)
	}
}

func TestRegexDetector_LuhnRejectsInvalidCard(t *testing.T) {
	d := NewRegexDetector()
	// 16 digits that fail the Luhn check must not be reported as a card.
	fs := d.Scan("order id 1234 5678 9012 3456 shipped")
	for _, f := range fs {
		if f.Category == "credit_card" {
			t.Fatalf("Luhn-invalid number reported as card: %+v", f)
		}
	}
}

func TestRegexDetector_Clean(t *testing.T) {
	d := NewRegexDetector()
	if fs := d.Scan("The invoice resend runbook lives in Coupa."); len(fs) != 0 {
		t.Fatalf("clean text produced findings: %+v", fs)
	}
}

func TestFinding_ExcerptIsMasked(t *testing.T) {
	d := NewRegexDetector()
	fs := d.Scan("ssn 123-45-6789")
	for _, f := range fs {
		if f.Category == "ssn" && (f.Excerpt == "" || containsRun(f.Excerpt, "123-45-6789")) {
			t.Fatalf("excerpt must be masked, got %q", f.Excerpt)
		}
	}
}

func containsRun(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}
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

Run: `go test ./internal/pii/`
Expected: FAIL — `undefined: NewRegexDetector` (package has no implementation yet).

- [ ] **Step 3: Write minimal implementation**

```go
// Package pii detects personal / sensitive information in extracted document
// text so the upload path can hard-block high-severity content (SSN, card,
// bank, DOB) before it ever reaches a reviewer or the index. Detection is
// deterministic regex so it is testable and free; the Detector interface is
// the seam where an LLM or AWS Comprehend detector drops in later.
package pii

import (
	"regexp"
	"strings"
)

type Severity int

const (
	Low Severity = iota
	High
)

type Finding struct {
	Category string
	Severity Severity
	Excerpt  string // masked snippet for the reviewer, never the raw value
	Offset   int    // byte offset of the match in the scanned text
}

type Detector interface {
	Scan(text string) []Finding
}

// rule pairs a category+severity with a pattern. When the pattern has a
// capturing group, group 1 is the sensitive value (the surrounding keyword,
// e.g. "DOB:", is matched but not reported). luhn=true additionally requires
// the digits to pass the Luhn check, which suppresses order ids and other
// 13-16 digit runs that are not payment cards.
type rule struct {
	category string
	severity Severity
	re       *regexp.Regexp
	luhn     bool
}

type regexDetector struct{ rules []rule }

func NewRegexDetector() Detector {
	return &regexDetector{rules: []rule{
		{"ssn", High, regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`), false},
		{"credit_card", High, regexp.MustCompile(`\b(?:\d[ -]?){13,16}\b`), true},
		{"dob", High, regexp.MustCompile(`(?i)(?:dob|date of birth)\D{0,10}(\d{1,2}[/-]\d{1,2}[/-]\d{2,4})`), false},
		{"bank_account", High, regexp.MustCompile(`(?i)(?:account (?:no|number|#)|routing)\D{0,10}(\d{6,17})`), false},
		{"email", Low, regexp.MustCompile(`\b[\w.+-]+@[\w-]+\.[\w.-]+\b`), false},
		{"phone", Low, regexp.MustCompile(`\b(?:\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`), false},
	}}
}

func (d *regexDetector) Scan(text string) []Finding {
	var out []Finding
	for _, r := range d.rules {
		for _, loc := range r.re.FindAllStringSubmatchIndex(text, -1) {
			// value is the captured group if present, else the whole match.
			vs, ve := loc[0], loc[1]
			if len(loc) >= 4 && loc[2] >= 0 {
				vs, ve = loc[2], loc[3]
			}
			val := text[vs:ve]
			if r.luhn && !luhnValid(val) {
				continue
			}
			out = append(out, Finding{
				Category: r.category,
				Severity: r.severity,
				Excerpt:  mask(val),
				Offset:   vs,
			})
		}
	}
	return out
}

func HasHigh(findings []Finding) bool {
	for _, f := range findings {
		if f.Severity == High {
			return true
		}
	}
	return false
}

// mask keeps the last 4 characters and replaces the rest with a bullet so a
// reviewer sees the shape without the raw value leaking into logs or the UI.
func mask(s string) string {
	digits := strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '.' {
			return -1
		}
		return r
	}, s)
	if len(digits) <= 4 {
		return strings.Repeat("•", len(digits))
	}
	return strings.Repeat("•", len(digits)-4) + digits[len(digits)-4:]
}

func luhnValid(s string) bool {
	sum, alt, n := 0, false, 0
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			continue
		}
		n++
		d := int(c - '0')
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return n >= 13 && sum%10 == 0
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pii/ && go vet ./internal/pii/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pii/
git commit -m "Add deterministic regex PII detector behind a Detector interface"
```

---

### Task 2: `internal/extract` — add `.docx` and `.xlsx`

**Files:**
- Modify: `internal/extract/extract.go`
- Test: `internal/extract/extract_test.go` (add cases)
- Modify: `go.mod` / `go.sum` (add `github.com/xuri/excelize/v2`)

**Interfaces:**
- Consumes: nothing new.
- Produces: `extract.FromBytes` and `extract.Supported` additionally handle `.docx` and `.xlsx`.

- [ ] **Step 1: Add the excelize dependency**

Run: `go get github.com/xuri/excelize/v2@latest`
Expected: `go.mod` gains the require line; `go.sum` updated.

- [ ] **Step 2: Write the failing tests**

Add to `internal/extract/extract_test.go` (tests build their own inputs — no binary fixtures in the repo):

```go
func TestFromBytes_XLSX(t *testing.T) {
	f := excelize.NewFile()
	_ = f.SetCellValue("Sheet1", "A1", "Team")
	_ = f.SetCellValue("Sheet1", "B1", "Owner")
	_ = f.SetCellValue("Sheet1", "A2", "Coupa")
	_ = f.SetCellValue("Sheet1", "B2", "Alex")
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	got, err := extract.FromBytes("report.xlsx", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Coupa") || !strings.Contains(got, "Alex") {
		t.Fatalf("xlsx text missing cell values: %q", got)
	}
}

func TestFromBytes_DOCX(t *testing.T) {
	data := makeDocx(t, "Invoice resend runbook", "Retry from the Coupa queue.")
	got, err := extract.FromBytes("runbook.docx", data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Invoice resend runbook") || !strings.Contains(got, "Coupa queue") {
		t.Fatalf("docx text missing paragraphs: %q", got)
	}
}

func TestSupported_OfficeFormats(t *testing.T) {
	for _, n := range []string{"a.docx", "b.xlsx"} {
		if !extract.Supported(n) {
			t.Errorf("Supported(%q) = false, want true", n)
		}
	}
}

// makeDocx builds the minimal OOXML zip that FromBytes needs: a
// word/document.xml with each argument as its own <w:p><w:t> paragraph.
func makeDocx(t *testing.T, paras ...string) []byte {
	t.Helper()
	var body strings.Builder
	for _, p := range paras {
		body.WriteString("<w:p><w:r><w:t>" + p + "</w:t></w:r></w:p>")
	}
	doc := `<?xml version="1.0"?><w:document xmlns:w="x"><w:body>` + body.String() + `</w:body></w:document>`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(doc)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
```

Add imports `archive/zip`, `bytes`, `strings`, and `github.com/xuri/excelize/v2` to the test file.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/extract/ -run 'DOCX|XLSX|Office'`
Expected: FAIL — `.docx`/`.xlsx` hit the `default` branch returning `ErrUnsupported`.

- [ ] **Step 4: Implement the two extractors**

In `internal/extract/extract.go`, extend the dispatch switch and `Supported`, and add the two functions:

```go
// in FromBytes' switch, before default:
	case strings.HasSuffix(name, ".xlsx"):
		return fromXLSX(data)
	case strings.HasSuffix(name, ".docx"):
		return fromDOCX(data)
```

```go
// add ".xlsx" and ".docx" to the Supported() suffix list.
```

```go
import (
	"archive/zip"
	"regexp"
	// ...existing imports...
	"github.com/xuri/excelize/v2"
)

// wTextRe pulls the text out of every <w:t> run. We deliberately avoid a full
// OOXML parse: a regex over document.xml is enough to recover readable prose
// for chunking, and adds no dependency.
var wTextRe = regexp.MustCompile(`(?s)<w:t[^>]*>(.*?)</w:t>`)

func fromDOCX(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("extract: docx open: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("extract: docx read: %w", err)
		}
		xmlBytes, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return "", fmt.Errorf("extract: docx read: %w", err)
		}
		var b strings.Builder
		for _, m := range wTextRe.FindAllSubmatch(xmlBytes, -1) {
			b.Write(m[1])
			b.WriteByte(' ')
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("extract: docx: no word/document.xml")
}

// fromXLSX flattens every sheet to "header: value" lines so a spreadsheet
// becomes retrievable prose. Tables chunk poorly (see spec Risks); this is the
// pragmatic demo behavior, not high-quality tabular handling.
func fromXLSX(data []byte) (string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("extract: xlsx open: %w", err)
	}
	defer f.Close()
	var b strings.Builder
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			return "", fmt.Errorf("extract: xlsx rows: %w", err)
		}
		if len(rows) == 0 {
			continue
		}
		headers := rows[0]
		for _, row := range rows[1:] {
			for i, cell := range row {
				if cell == "" {
					continue
				}
				h := ""
				if i < len(headers) {
					h = headers[i]
				}
				b.WriteString(h + ": " + cell + "\n")
			}
		}
	}
	return b.String(), nil
}
```

Add `io` to the imports if not already present.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/extract/ && go vet ./internal/extract/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/extract/ go.mod go.sum
git commit -m "Extract text from .docx and .xlsx uploads"
```

---

### Task 3: `internal/review` — pending queue

**Files:**
- Create: `internal/review/review.go`
- Test: `internal/review/review_test.go`

**Interfaces:**
- Consumes: `internal/pii` (`pii.Finding`).
- Produces:
  - `type Status string` with `Pending`, `Approved`, `Rejected`
  - `type Item struct { ID, Team, Uploader, Filename, Text string; Findings []pii.Finding; Status Status; Bytes int; CreatedAt time.Time }`
  - `type Store interface { Enqueue(ctx, Item) (string, error); List(ctx, team string) ([]Item, error); Get(ctx, id string) (Item, error); SetStatus(ctx, id string, s Status) error }`
  - `func NewMemoryStore() Store`
  - `var ErrNotFound = errors.New("review: item not found")`

- [ ] **Step 1: Write the failing test**

```go
package review

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryStore_EnqueueListGet(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	id, err := s.Enqueue(ctx, Item{Team: "coupa", Filename: "a.pdf", Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty id")
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != Pending || got.Filename != "a.pdf" {
		t.Fatalf("unexpected item: %+v", got)
	}
	list, err := s.List(ctx, "coupa")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %d, want 1", len(list))
	}
}

func TestMemoryStore_ListIsTeamScoped(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_, _ = s.Enqueue(ctx, Item{Team: "coupa", Filename: "a.pdf"})
	_, _ = s.Enqueue(ctx, Item{Team: "star", Filename: "b.pdf"})
	list, _ := s.List(ctx, "coupa")
	if len(list) != 1 || list[0].Team != "coupa" {
		t.Fatalf("team A must not see team B items: %+v", list)
	}
}

func TestMemoryStore_SetStatusRemovesFromPendingList(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	id, _ := s.Enqueue(ctx, Item{Team: "coupa", Filename: "a.pdf"})
	if err := s.SetStatus(ctx, id, Approved); err != nil {
		t.Fatal(err)
	}
	list, _ := s.List(ctx, "coupa")
	if len(list) != 0 {
		t.Fatalf("approved item still in pending list: %+v", list)
	}
	got, _ := s.Get(ctx, id)
	if got.Status != Approved {
		t.Fatalf("status = %q, want approved", got.Status)
	}
}

func TestMemoryStore_SetStatusMissing(t *testing.T) {
	s := NewMemoryStore()
	if err := s.SetStatus(context.Background(), "nope", Approved); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/review/`
Expected: FAIL — `undefined: NewMemoryStore`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package review holds documents uploaded for the knowledge base between the
// PII scan and admin approval. Nothing here is indexed; promotion to the index
// happens in the admin handler on approval. The Store interface is the seam:
// MemoryStore is the demo backend, and an S3-backed durable store drops in for
// production without changing callers.
package review

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/example/knowledge-assistant/internal/pii"
	"github.com/google/uuid"
)

type Status string

const (
	Pending  Status = "pending"
	Approved Status = "approved"
	Rejected Status = "rejected"
)

var ErrNotFound = errors.New("review: item not found")

type Item struct {
	ID        string
	Team      string
	Uploader  string
	Filename  string
	Text      string // extracted text, ready to ingest on approval
	Findings  []pii.Finding
	Status    Status
	Bytes     int
	CreatedAt time.Time
}

type Store interface {
	Enqueue(ctx context.Context, it Item) (string, error)
	List(ctx context.Context, team string) ([]Item, error) // pending only, team-scoped
	Get(ctx context.Context, id string) (Item, error)
	SetStatus(ctx context.Context, id string, s Status) error
}

type memoryStore struct {
	mu    sync.Mutex
	items map[string]Item
}

func NewMemoryStore() Store { return &memoryStore{items: map[string]Item{}} }

func (m *memoryStore) Enqueue(_ context.Context, it Item) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	it.Status = Pending
	if it.CreatedAt.IsZero() {
		it.CreatedAt = time.Now()
	}
	m.items[it.ID] = it
	return it.ID, nil
}

func (m *memoryStore) List(_ context.Context, team string) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Item
	for _, it := range m.items {
		if it.Team == team && it.Status == Pending {
			out = append(out, it)
		}
	}
	return out, nil
}

func (m *memoryStore) Get(_ context.Context, id string) (Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	return it, nil
}

func (m *memoryStore) SetStatus(_ context.Context, id string, s Status) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return ErrNotFound
	}
	it.Status = s
	m.items[id] = it
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/review/ && go vet ./internal/review/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/review/
git commit -m "Add in-memory pending-review queue behind a Store interface"
```

---

### Task 4: Rewire `upload.go` persist path to scan-then-enqueue

**Files:**
- Modify: `services/chat-api/internal/handler/upload.go`
- Test: `services/chat-api/internal/handler/upload_test.go` (create if absent)

**Interfaces:**
- Consumes: `pii.Detector`, `pii.HasHigh`, `review.Store`, `review.Item`.
- Produces: `UploadHandler` gains fields `Detector pii.Detector` and `Review review.Store`; the persist branch no longer calls `ingest.Ingest`. New response shape for persist: `{ "uploadId", "filename", "bytes", "status": "pending_review", "reviewId", "findings": [...] }`. High-severity → `422`.

- [ ] **Step 1: Write the failing test**

```go
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/knowledge-assistant/internal/pii"
	"github.com/example/knowledge-assistant/internal/review"
	"github.com/example/knowledge-assistant/services/chat-api/internal/middleware"
	"log/slog"
)

func multipartTxt(t *testing.T, field, filename, body, persist string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile(field, filename)
	_, _ = fw.Write([]byte(body))
	_ = w.WriteField("persist", persist)
	_ = w.Close()
	return &buf, w.FormDataContentType()
}

// withScope injects the team scope + user the handler reads, mirroring the
// middleware in production.
func withScope(r *http.Request) *http.Request {
	ctx := middleware.ContextWithScope(r.Context(), middleware.DemoScope("coupa"), "dev@example.com")
	return r.WithContext(ctx)
}

func TestUpload_Persist_CleanEnqueues(t *testing.T) {
	store := review.NewMemoryStore()
	h := &UploadHandler{
		Review:   store,
		Detector: pii.NewRegexDetector(),
		Log:      slog.Default(),
	}
	body, ct := multipartTxt(t, "file", "runbook.txt", "Resend the invoice from Coupa.", "true")
	req := withScope(httptest.NewRequest(http.MethodPost, "/v1/uploads", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "pending_review" || resp["reviewId"] == "" {
		t.Fatalf("unexpected response: %v", resp)
	}
	list, _ := store.List(context.Background(), "coupa")
	if len(list) != 1 {
		t.Fatalf("expected 1 pending item, got %d", len(list))
	}
}

func TestUpload_Persist_SensitiveBlocks(t *testing.T) {
	store := review.NewMemoryStore()
	h := &UploadHandler{Review: store, Detector: pii.NewRegexDetector(), Log: slog.Default()}
	body, ct := multipartTxt(t, "file", "hr.txt", "Employee SSN 123-45-6789.", "true")
	req := withScope(httptest.NewRequest(http.MethodPost, "/v1/uploads", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422; body = %s", rec.Code, rec.Body.String())
	}
	if list, _ := store.List(context.Background(), "coupa"); len(list) != 0 {
		t.Fatalf("sensitive doc must not be enqueued, got %d", len(list))
	}
}
```

> Note: if `middleware.ContextWithScope`/`DemoScope` test helpers do not already exist, add them next to `ScopeFromContext` in `scope.go` (thin constructors used only by tests), or replace these two lines with however existing handler tests build a scoped request — check `chats.go`'s tests for the established pattern before inventing one.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./services/chat-api/internal/handler/ -run TestUpload_Persist`
Expected: FAIL — `UploadHandler` has no `Review`/`Detector` fields.

- [ ] **Step 3: Modify the handler**

In `upload.go`:

1. Add fields to the struct and drop the now-unused indexing on this path:

```go
type UploadHandler struct {
	Sessions *session.Store // session-scoped uploads (unchanged)
	Detector pii.Detector   // scans persist-path uploads before queueing
	Review   review.Store   // pending queue; approval (not upload) indexes
	Log      *slog.Logger
}
```

2. Replace the `if persist { ... ingest.Ingest ... }` block with scan-then-enqueue:

```go
	if persist {
		findings := h.Detector.Scan(text)
		if pii.HasHigh(findings) {
			http.Error(w, "this document appears to contain sensitive information "+
				"(e.g. SSN or card number); remove it and upload again",
				http.StatusUnprocessableEntity)
			return
		}
		reviewID, err := h.Review.Enqueue(r.Context(), review.Item{
			Team:     scope.Team().Slug(),
			Uploader: uid,
			Filename: hdr.Filename,
			Text:     text,
			Findings: findings, // low-severity only reaches here
			Bytes:    len(data),
		})
		if err != nil {
			http.Error(w, "review enqueue failed", http.StatusInternalServerError)
			return
		}
		resp.Status = "pending_review"
		resp.ReviewID = reviewID
		resp.Findings = findings
	}
```

3. Extend `uploadResp` and update imports (`internal/pii`, `internal/review`; drop `internal/index` and `internal/ingest` if no longer referenced on this path — `ingest.Chunks`/`ingest.Page` are still used by session mode, so keep `internal/ingest`):

```go
type uploadResp struct {
	UploadID string        `json:"uploadId"`
	Filename string        `json:"filename"`
	Bytes    int           `json:"bytes"`
	Chunks   int           `json:"chunks,omitempty"`
	Status   string        `json:"status,omitempty"`
	ReviewID string        `json:"reviewId,omitempty"`
	Findings []pii.Finding `json:"findings,omitempty"`
}
```

Remove the `Persisted` field and the `Indexer`/`Embedder` fields from `UploadHandler` (session mode does not use them; the embedder lives in the session store).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./services/chat-api/internal/handler/ && go vet ./services/chat-api/...`
Expected: PASS. (The build will also fail at `main.go` until Task 6 — that is expected; run the handler package tests in isolation here.)

- [ ] **Step 5: Commit**

```bash
git add services/chat-api/internal/handler/upload.go services/chat-api/internal/handler/upload_test.go
git commit -m "Gate persist uploads: scan for PII, block high-severity, else enqueue for review"
```

---

### Task 5: Admin handler + `RequireAdmin` middleware

**Files:**
- Create: `services/chat-api/internal/handler/admin.go`
- Test: `services/chat-api/internal/handler/admin_test.go`
- Create: `services/chat-api/internal/middleware/admin.go`
- Test: `services/chat-api/internal/middleware/admin_test.go`

**Interfaces:**
- Consumes: `review.Store`, `ingest.Ingest`, `index.Indexer`, `rag.Embedder`, `middleware.ScopeFromContext`, `middleware.UserFromContext`.
- Produces:
  - `func middleware.RequireAdmin(admins []string) func(http.Handler) http.Handler` — 403 unless `UserFromContext` is in `admins`.
  - `type AdminHandler struct { Review review.Store; Indexer *index.Indexer; Embedder rag.Embedder; Log *slog.Logger }`
  - methods `List(w,r)`, `Approve(w,r)`, `Reject(w,r)` (chi URL param `id`).

- [ ] **Step 1: Write the failing middleware test**

```go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireAdmin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mw := RequireAdmin([]string{"dev@example.com"})

	t.Run("allows admin", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil).WithContext(
			ContextWithScope(nil, DemoScope("coupa"), "dev@example.com"))
		rec := httptest.NewRecorder()
		mw(next).ServeHTTP(rec, r)
		if rec.Code != 200 {
			t.Fatalf("admin blocked: %d", rec.Code)
		}
	})
	t.Run("blocks non-admin", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil).WithContext(
			ContextWithScope(nil, DemoScope("coupa"), "someone@example.com"))
		rec := httptest.NewRecorder()
		mw(next).ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("non-admin allowed: %d", rec.Code)
		}
	})
}
```

> If `ContextWithScope`/`DemoScope` were not added in Task 4, add them now in `scope.go` — they are the shared test seam for injecting identity, e.g.:
> ```go
> // ContextWithScope is used by tests to inject the scope+user the real
> // middleware would set. DemoScope builds a scope for the named team.
> func ContextWithScope(ctx context.Context, s rag.Scope, user string) context.Context {
> 	if ctx == nil { ctx = context.Background() }
> 	ctx = context.WithValue(ctx, scopeKey, s)
> 	return context.WithValue(ctx, userIDKey, user)
> }
> ```
> Match the real context-key constants already in `scope.go`/`auth.go`; if a `DemoScope` constructor is awkward because `rag.Scope` is an interface, reuse whatever concrete scope `WithScope` builds. Confirm the exact key names before writing.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./services/chat-api/internal/middleware/ -run TestRequireAdmin`
Expected: FAIL — `undefined: RequireAdmin`.

- [ ] **Step 3: Implement the middleware**

```go
// admin.go
package middleware

import "net/http"

// RequireAdmin gates the review endpoints. For the demo, admins is an env
// allowlist (KA_ADMIN_USERS) checked against the X-Dev-User identity that Auth
// resolves; the default dev user is the demo admin. Prod replaces this with a
// Cognito group check.
func RequireAdmin(admins []string) func(http.Handler) http.Handler {
	set := map[string]bool{}
	for _, a := range admins {
		set[a] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !set[UserFromContext(r.Context())] {
				http.Error(w, "admin access required", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./services/chat-api/internal/middleware/`
Expected: PASS.

- [ ] **Step 5: Write the failing admin-handler test**

```go
package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/knowledge-assistant/internal/review"
	"github.com/go-chi/chi/v5"
	"log/slog"
)

func TestAdmin_ListAndReject(t *testing.T) {
	store := review.NewMemoryStore()
	id, _ := store.Enqueue(context.Background(), review.Item{Team: "coupa", Filename: "a.pdf", Text: "hi"})
	h := &AdminHandler{Review: store, Log: slog.Default()}

	// List
	req := withScope(httptest.NewRequest(http.MethodGet, "/v1/admin/pending", nil))
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != 200 || !bytesContains(rec.Body.Bytes(), "a.pdf") {
		t.Fatalf("list failed: %d %s", rec.Code, rec.Body.String())
	}

	// Reject (does not index)
	rr := chi.NewRouteContext()
	rr.URLParams.Add("id", id)
	req2 := withScope(httptest.NewRequest(http.MethodPost, "/v1/admin/pending/"+id+"/reject", nil)).
		WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rr))
	// re-inject scope on top of the chi ctx:
	req2 = withScope(req2)
	rec2 := httptest.NewRecorder()
	h.Reject(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("reject failed: %d", rec2.Code)
	}
	got, _ := store.Get(context.Background(), id)
	if got.Status != review.Rejected {
		t.Fatalf("status = %q, want rejected", got.Status)
	}
}

func bytesContains(b []byte, s string) bool { return len(b) > 0 && indexOfBytes(b, s) >= 0 }
func indexOfBytes(b []byte, s string) int {
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == s {
			return i
		}
	}
	return -1
}
```

> The chi URL-param + scope layering above is fiddly; before writing, look at how `chats_test.go` builds a request that carries both a chi route param (`{id}`) and the scope context, and copy that exact construction rather than the sketch here.

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./services/chat-api/internal/handler/ -run TestAdmin`
Expected: FAIL — `undefined: AdminHandler`.

- [ ] **Step 7: Implement the admin handler**

```go
// admin.go
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/knowledge-assistant/internal/index"
	"github.com/example/knowledge-assistant/internal/ingest"
	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/internal/review"
	"github.com/example/knowledge-assistant/services/chat-api/internal/middleware"
	"github.com/go-chi/chi/v5"
)

type AdminHandler struct {
	Review   review.Store
	Indexer  *index.Indexer // nil in fixtures mode; Approve then returns 503
	Embedder rag.Embedder
	Log      *slog.Logger
}

func (h *AdminHandler) List(w http.ResponseWriter, r *http.Request) {
	scope, ok := middleware.ScopeFromContext(r.Context())
	if !ok {
		http.Error(w, "no team scope", http.StatusForbidden)
		return
	}
	items, err := h.Review.List(r.Context(), scope.Team().Slug())
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func (h *AdminHandler) Reject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Review.SetStatus(r.Context(), id, review.Rejected); err != nil {
		h.status404or500(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Approve promotes the reviewed document into the index, then marks it
// approved. Status advances only after a successful index so a transient
// index failure leaves the item pending and retryable.
func (h *AdminHandler) Approve(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	it, err := h.Review.Get(r.Context(), id)
	if err != nil {
		h.status404or500(w, err)
		return
	}
	if h.Indexer == nil {
		http.Error(w, "indexing unavailable (fixtures mode)", http.StatusServiceUnavailable)
		return
	}
	page := ingest.Page{
		Team:      it.Team,
		SpaceKey:  "UPLOAD",
		PageID:    it.ID,
		Title:     it.Filename,
		URL:       "upload://" + it.ID + "/" + it.Filename,
		Markdown:  it.Text,
		UpdatedAt: time.Now(),
	}
	if _, err := ingest.Ingest(r.Context(), page, h.Embedder, h.Indexer); err != nil {
		http.Error(w, "index failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	if err := h.Review.SetStatus(r.Context(), id, review.Approved); err != nil {
		http.Error(w, "status update failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *AdminHandler) status404or500(w http.ResponseWriter, err error) {
	if errors.Is(err, review.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.Error(w, "internal error", http.StatusInternalServerError)
}
```

- [ ] **Step 8: Run to verify it passes**

Run: `go test ./services/chat-api/internal/handler/ -run TestAdmin`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add services/chat-api/internal/handler/admin.go services/chat-api/internal/handler/admin_test.go services/chat-api/internal/middleware/admin.go services/chat-api/internal/middleware/admin_test.go services/chat-api/internal/middleware/scope.go
git commit -m "Add admin review endpoints and RequireAdmin gate; approval indexes the doc"
```

---

### Task 6: Wire detector, review store, admin routes into `main.go`

**Files:**
- Modify: `services/chat-api/cmd/server/main.go`
- Modify: `services/chat-api/internal/config/config.go` (add `AdminUsers []string` from `KA_ADMIN_USERS`)
- Test: `services/chat-api/internal/config/config_test.go` (if present, add a case)

**Interfaces:**
- Consumes: everything from Tasks 1–5.
- Produces: a running server where `POST /v1/uploads` enqueues and `/v1/admin/pending*` is served behind `RequireAdmin`.

- [ ] **Step 1: Add config field**

In `config.go`, add `AdminUsers []string` and populate it by splitting `KA_ADMIN_USERS` on commas (trim spaces, drop empties). Default empty. Document that `KA_ADMIN_USERS=dev@example.com` makes the demo dev user the admin.

- [ ] **Step 2: Construct the collaborators and update the upload route**

In `main.go`, after `sessions := session.NewStore(...)`:

```go
	detector := pii.NewRegexDetector()
	reviewStore := review.NewMemoryStore()
```

Update the upload route to the new field set (remove `Indexer`/`Embedder`, add `Detector`/`Review`):

```go
	authed.Method(http.MethodPost, "/v1/uploads", &handler.UploadHandler{
		Sessions: sessions, Detector: detector, Review: reviewStore, Log: log,
	})
```

- [ ] **Step 3: Register the admin routes**

```go
	admin := &handler.AdminHandler{
		Review: reviewStore, Indexer: uploadIndexer, Embedder: embedder, Log: log,
	}
	adminAuthed := authed.With(middleware.RequireAdmin(cfg.AdminUsers))
	adminAuthed.Get("/v1/admin/pending", admin.List)
	adminAuthed.Post("/v1/admin/pending/{id}/approve", admin.Approve)
	adminAuthed.Post("/v1/admin/pending/{id}/reject", admin.Reject)
```

Add imports for `internal/pii` and `internal/review`.

- [ ] **Step 4: Build and run the whole suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS across the module.

- [ ] **Step 5: Manual smoke (mock LLM, no OpenSearch needed for the block path)**

Run:
```bash
KA_ADMIN_USERS=dev@example.com make run-api   # or the documented local start
# clean doc → pending_review
curl -sS -X POST localhost:8080/v1/uploads -H 'X-Team: coupa' \
  -F persist=true -F file=@README.md | jq
# sensitive doc → 422
printf 'ssn 123-45-6789' > /tmp/pii.txt
curl -sS -o /dev/null -w '%{http_code}\n' -X POST localhost:8080/v1/uploads \
  -H 'X-Team: coupa' -F persist=true -F file=@/tmp/pii.txt
# admin sees the pending item
curl -sS localhost:8080/v1/admin/pending -H 'X-Team: coupa' | jq
```
Expected: first returns `"status":"pending_review"`; second prints `422`; third lists the README item.

- [ ] **Step 6: Commit**

```bash
git add services/chat-api/cmd/server/main.go services/chat-api/internal/config/
git commit -m "Wire PII detector, review store and admin review routes into chat-api"
```

---

### Task 7: UI — warning, formats, response handling, admin panel

**Files:**
- Modify: `ui/src/components/Composer.jsx` (warning + accept-list + response handling)
- Modify: `ui/src/api.js` (add admin calls)
- Modify: `ui/src/App.jsx` (mount the admin panel)
- Create: `ui/src/components/AdminPanel.jsx`
- Test: `ui/src/components/AdminPanel.test.jsx`, extend `ui/src/api.test.js`

**Interfaces:**
- Consumes: `POST /v1/uploads` new response (`status: "pending_review"` or `422`), `GET /v1/admin/pending`, `POST /v1/admin/pending/{id}/{approve|reject}`.
- Produces: `listPending(team)`, `approvePending(id, team)`, `rejectPending(id, team)` in `api.js`; `<AdminPanel team=... />` component.

- [ ] **Step 1: Add the API helpers (write the failing test first)**

In `ui/src/api.test.js`, add a test that `listPending` calls `/v1/admin/pending` with the team header (mock `fetch`, mirror the existing test style in that file). Then implement in `api.js`:

```js
export async function listPending(team) {
  const res = await fetch('/v1/admin/pending', { headers: headers(team) });
  if (!res.ok) throw new Error('list pending failed');
  return res.json();
}
export async function approvePending(id, team) {
  const res = await fetch(`/v1/admin/pending/${id}/approve`, { method: 'POST', headers: headers(team) });
  if (!res.ok) throw new Error('approve failed');
}
export async function rejectPending(id, team) {
  const res = await fetch(`/v1/admin/pending/${id}/reject`, { method: 'POST', headers: headers(team) });
  if (!res.ok) throw new Error('reject failed');
}
```

Run: `npm --prefix ui test -- api` → PASS.

- [ ] **Step 2: Composer — warning, accept-list, response handling**

In `Composer.jsx`:
- Extend the file input `accept` to `.pdf,.md,.txt,.markdown,.docx,.xlsx`.
- Near the persist checkbox, render standing warning copy: **"Don't upload documents containing sensitive personal information (SSNs, card numbers, etc.)."**
- The upload caller (in `App.jsx` `onUpload`) must handle the new outcomes: a `422` from `/v1/uploads` surfaces an inline error ("This document appears to contain sensitive information — remove it and upload again"); a `status === 'pending_review'` response shows "Sent for admin review" instead of the old "· saved" chip. Update `uploadFile` in `api.js` to throw a typed error carrying the 422 body so the UI can distinguish it from a generic failure.

Run: `npm --prefix ui test` → existing tests still PASS (adjust any Composer test asserting the old accept-list/persist chip text).

- [ ] **Step 3: AdminPanel component (write the failing test first)**

`ui/src/components/AdminPanel.test.jsx`: render `<AdminPanel team="coupa" />` with `listPending` mocked to return one item `{ ID:'x', filename:'a.pdf', findings:[] }`; assert the filename renders and that clicking "Approve" calls `approvePending('x','coupa')`. Then implement `AdminPanel.jsx`:

```jsx
import { useEffect, useState } from 'react';
import { listPending, approvePending, rejectPending } from '../api';

export default function AdminPanel({ team }) {
  const [items, setItems] = useState([]);
  const refresh = () => listPending(team).then(setItems).catch(() => setItems([]));
  useEffect(() => { refresh(); }, [team]);
  const act = async (id, fn) => { await fn(id, team); refresh(); };
  if (!items.length) return <div className="p-3 text-sm text-muted">No documents awaiting review.</div>;
  return (
    <ul className="divide-y">
      {items.map(it => (
        <li key={it.ID} className="flex items-center justify-between gap-2 p-3">
          <span className="text-sm">
            {it.filename} · {it.uploader}
            {it.findings?.length ? ` · ${it.findings.length} flag(s)` : ''}
          </span>
          <span className="flex gap-2">
            <button onClick={() => act(it.ID, approvePending)} className="text-xs text-success">Approve</button>
            <button onClick={() => act(it.ID, rejectPending)} className="text-xs text-danger">Reject</button>
          </span>
        </li>
      ))}
    </ul>
  );
}
```

(Match the JSON field casing the Go handler emits — Go marshals exported fields as `ID`, `Filename`, `Uploader`, `Findings` unless json tags are added. If the admin test in Task 5 shows different casing, either add `json:"..."` tags to `review.Item` in Task 3 for a stable lowercase contract, or read the emitted names here. Prefer adding json tags to `review.Item` so both sides use `filename`/`uploader`/`findings`/`id` — update Task 3's struct accordingly if you take this route.)

Run: `npm --prefix ui test -- AdminPanel` → PASS.

- [ ] **Step 4: Mount the panel in App.jsx**

Add a lightweight way to reach the panel — e.g. a "Review queue" item in the sidebar or top bar that toggles `<AdminPanel team={team} />`. For the demo the panel is always reachable (the default dev user is the admin); no client-side role check is required. Keep the change minimal and consistent with existing `Sidebar`/`TopBar` patterns.

Run: `npm --prefix ui test && npm --prefix ui run build` → PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add ui/src
git commit -m "Add upload sensitivity warning, office formats, and admin review panel to the UI"
```

---

## Self-Review notes

- **Spec coverage:** office extraction (Task 2)  · PII detector + severity + Luhn (Task 1)  · hard-block high-severity + user warning (Tasks 4, 7)  · review queue behind interface (Task 3)  · persist rewired to enqueue (Task 4)  · admin surface + env-allowlist authz (Tasks 5, 6)  · approval indexes byte-identically (Task 5)  · UI responses + panel (Task 7). All spec sections map to a task.
- **JSON contract caveat:** Task 7 Step 3 flags the Go↔JSON field-casing decision and routes it back to adding `json` tags on `review.Item` (Task 3) — resolve it there so both sides agree on `id`/`filename`/`uploader`/`findings`.
- **Test-helper caveat:** Tasks 4–5 depend on a scope-injection test seam (`ContextWithScope`/`DemoScope`). Before writing those tests, confirm how existing handler tests (`chats_test.go`) build a scoped, chi-param request and reuse that exact pattern; the sketches here are a fallback, not gospel.
- **Deferred items stay deferred:** no S3 store, no LLM detector, no OCR, no real auth — all interface seams, per spec Risks.
```
