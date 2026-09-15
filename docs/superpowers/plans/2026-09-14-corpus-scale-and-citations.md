# Corpus Scale and Citations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Grow the demo corpus from 23 to ~150 documents so retrieval must *rank* (not merely retrieve) the right source, and give every document a real-looking system-native citation link.

**Architecture:** Three tiers of fixture documents — 23 frozen gold docs, ~90 LLM-generated adversarial distractors, ~35 new question-first docs — all in the existing `fixtures/{coupa,star,hr}/` layout. The only code change is teaching the fixture indexer to read a YAML front-matter `url:`/`updated:` block (today it hardcodes a `file://` URL). Correctness of generated content is enforced by an automated retrieval guardrail (`scripts/check_corpus.py --scores`), not by hand-reading every document. `KA_RELEVANCE_FLOOR` is recalibrated at the end against gold-vs-distractor separation.

**Tech Stack:** Go (`services/ingestion/cmd/indexer`), Markdown fixtures, `scripts/check_corpus.py` (Python), OpenSearch, `make seed`.

**Spec:** `docs/superpowers/specs/2026-09-14-corpus-scale-and-citations-design.md`

## Global Constraints

- **The 23 gold docs and all 36 existing rows of `fixtures/tests.jsonl` are frozen.** Gold docs may gain front-matter only; their body content and the existing test rows must not change.
- **Baseline is 34/36, not 36/36 (measured 2026-09-14).** `check_corpus.py` now scores only the chunks the model actually reads (`used` = selected + backfilled), not the raw candidate pool. The 2 known failures — `star-la-01` and `star-tx-01`, both the "submission deadline" near-twin — are genuine near-twin bleed (the opposite state's deadline chunk is itself selected) and are **flaky by ±1** because the `gpt-oss:20b` reranker is non-deterministic. Accepted as a demonstrable retrieval limit, not a blocker.
- **Task 3 guardrail is a per-question delta, not an absolute pass count.** No gold question that passes at the batch's start may flip to fail because of a distractor. `star-la-01`/`star-tx-01` flipping is reranker noise, not a distractor regression — re-run to confirm before acting.
- **No distractor may satisfy a gold question's `keywords`.** A distractor that becomes a true positive corrupts the eval and must be edited or deleted.
- **Every corpus change is followed by `make seed`** — it drops the index first; chunk doc ids hash chunk text, so stale chunks otherwise rank against fresh ones.
- **Links are system-native placeholder hosts only**, never a corporate/State Farm wiki: ServiceNow `sf-demo.service-now.com`, Coupa `sf-demo.coupahost.com`, Icertis `sf-demo.icertis.com`, Workday `wd5-impl.workday.com/sf_demo`.
- **Go must be gofmt-clean**; a hook formats after Edit/Write, but run `gofmt -w` after any shell-based Go edit. `go test ./...` and `go vet ./...` must pass.
- **No new Go dependencies.** Front-matter is parsed by hand (simple `key: value` lines); do not add a YAML library — `go.mod` has none today.
- Module path is `github.com/example/knowledge-assistant`.

---

### Task 1: Front-matter parsing in the fixture indexer

Teach `loadFixtures` to read an optional leading `---`-delimited block for `url:` and `updated:`, strip it from the body before chunking, and fall back to today's behavior when it is absent.

**Files:**
- Modify: `services/ingestion/cmd/indexer/main.go` (`loadFixtures`, ~lines 178-206; add helper `parseFrontMatter`)
- Test: `services/ingestion/cmd/indexer/main_test.go`

**Interfaces:**
- Consumes: `chunker.Title(md, fallback string) string`, `chunker.Split`, the `page` struct (`SpaceKey, ID, Title, Markdown, WebURL, UpdatedAt`).
- Produces: `parseFrontMatter(raw string) (meta map[string]string, body string)` — returns `nil` meta and the original string when there is no leading `---` block or no closing `---`; otherwise the parsed `key: value` pairs and the body with the block (and its trailing newline) removed.

- [ ] **Step 1: Write the failing tests**

Add to `services/ingestion/cmd/indexer/main_test.go`:

```go
func TestParseFrontMatter(t *testing.T) {
	raw := "---\nurl: https://sf-demo.service-now.com/kb?id=1\nupdated: 2026-08-30\n---\n# AVR Runbook\n\nbody\n"
	meta, body := parseFrontMatter(raw)
	if meta["url"] != "https://sf-demo.service-now.com/kb?id=1" {
		t.Fatalf("url = %q", meta["url"])
	}
	if meta["updated"] != "2026-08-30" {
		t.Fatalf("updated = %q", meta["updated"])
	}
	if body != "# AVR Runbook\n\nbody\n" {
		t.Fatalf("body not stripped to H1: %q", body)
	}
}

func TestParseFrontMatterAbsent(t *testing.T) {
	raw := "# No Front Matter\n\nbody\n"
	meta, body := parseFrontMatter(raw)
	if meta != nil {
		t.Fatalf("meta = %v, want nil", meta)
	}
	if body != raw {
		t.Fatalf("body altered when no front matter present")
	}
}

func TestParseFrontMatterUnterminated(t *testing.T) {
	// A doc that opens with --- but never closes it is treated as having no
	// front matter, so a stray leading rule is never swallowed.
	raw := "---\nnot really front matter\n# Title\n"
	meta, body := parseFrontMatter(raw)
	if meta != nil || body != raw {
		t.Fatalf("unterminated block should be a no-op: meta=%v", meta)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./services/ingestion/cmd/indexer/ -run TestParseFrontMatter -v`
Expected: FAIL — `undefined: parseFrontMatter`.

- [ ] **Step 3: Implement `parseFrontMatter`**

Add to `services/ingestion/cmd/indexer/main.go`:

```go
// parseFrontMatter reads an optional leading "---"-delimited block of simple
// key: value lines (no nesting, no YAML types) and returns it plus the body
// with the block removed. A document without a leading "---", or one that
// opens "---" but never closes it, is returned unchanged with nil meta -- so a
// stray leading horizontal rule is never mistaken for front matter.
func parseFrontMatter(raw string) (map[string]string, string) {
	if !strings.HasPrefix(raw, "---\n") {
		return nil, raw
	}
	rest := raw[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return nil, raw
	}
	block, body := rest[:end], rest[end+len("\n---\n"):]
	meta := map[string]string{}
	for _, line := range strings.Split(block, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		meta[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return meta, body
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./services/ingestion/cmd/indexer/ -run TestParseFrontMatter -v`
Expected: PASS (all three).

- [ ] **Step 5: Wire `parseFrontMatter` into `loadFixtures`**

In `loadFixtures`, replace the body of the loop that builds each `page`. Change from using `string(body)` directly to:

```go
		meta, md := parseFrontMatter(string(body))
		id := strings.TrimSuffix(e.Name(), ".md")
		webURL := "file://" + filepath.Join(dir, e.Name())
		if u := meta["url"]; u != "" {
			webURL = u
		}
		updated := time.Now()
		if d := meta["updated"]; d != "" {
			if parsed, perr := time.Parse("2006-01-02", d); perr == nil {
				updated = parsed
			}
		}
		pages = append(pages, page{
			SpaceKey:  space,
			ID:        id,
			Title:     chunker.Title(md, strings.ReplaceAll(id, "-", " ")),
			Markdown:  md,
			WebURL:    webURL,
			UpdatedAt: updated,
		})
```

(`md` is the front-matter-stripped body, so `chunker.Title`, `chunker.Split` and `EmbedText` never see the block.)

- [ ] **Step 6: Write a `loadFixtures` integration test**

Add to `main_test.go` (uses `t.TempDir`):

```go
func TestLoadFixturesReadsFrontMatter(t *testing.T) {
	dir := t.TempDir()
	doc := "---\nurl: https://sf-demo.coupahost.com/doc/42\nupdated: 2026-08-30\n---\n# AVR Runbook\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "avr-runbook.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	pages, err := loadFixtures(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 {
		t.Fatalf("got %d pages", len(pages))
	}
	p := pages[0]
	if p.WebURL != "https://sf-demo.coupahost.com/doc/42" {
		t.Fatalf("WebURL = %q", p.WebURL)
	}
	if p.Title != "AVR Runbook" {
		t.Fatalf("Title = %q, want the H1", p.Title)
	}
	if strings.Contains(p.Markdown, "url:") {
		t.Fatalf("front matter leaked into body: %q", p.Markdown)
	}
	if !p.UpdatedAt.Equal(time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("UpdatedAt = %v", p.UpdatedAt)
	}
}
```

Add the `os` import to the test file if it is not already present.

- [ ] **Step 7: Run the full indexer test package and vet**

Run: `go test ./services/ingestion/cmd/indexer/ -v && go vet ./services/ingestion/cmd/indexer/`
Expected: PASS, no vet complaints. Then `gofmt -l services/ingestion/cmd/indexer/` prints nothing.

- [ ] **Step 8: Commit**

```bash
git add services/ingestion/cmd/indexer/main.go services/ingestion/cmd/indexer/main_test.go
git commit -m "Read front-matter url/updated in the fixture indexer

Fixture citations were indexed with file:// URLs, which read as fake in
the demo. loadFixtures now honors an optional --- block so each document
can carry a system-native source link; absent, behavior is unchanged."
```

---

### Task 2: Add front-matter links to the 23 gold documents

A metadata-only edit to the frozen gold docs: prepend a front-matter block with a system-native `url:` and an `updated:` date. No body text changes.

**Files:**
- Modify: every `.md` under `fixtures/coupa/`, `fixtures/star/`, `fixtures/hr/` (the 23 existing gold docs)
- Reference: `fixtures/README.md`

**Interfaces:**
- Consumes: `parseFrontMatter` from Task 1.
- Produces: gold docs whose citations now carry real-looking URLs. No new symbols.

- [ ] **Step 1: Choose a URL per document by system.** Map each doc to its owning system's placeholder host. Examples (follow the pattern for every file):

| Document | URL |
|---|---|
| `coupa/servicenow-product-sync.md` | `https://sf-demo.service-now.com/kb_view.do?sysparm_article=KB0010042` |
| `coupa/icertis-contract-overview.md` | `https://sf-demo.icertis.com/contracts/docs/overview` |
| `coupa/avr-runbook.md` | `https://sf-demo.coupahost.com/middleware/avr/runbook` |
| `hr/workday-payroll-integration.md` | `https://wd5-impl.workday.com/sf_demo/d/payroll-integration` |
| `star/texas-processing-rules.md` | `https://sf-demo.coupahost.com/star/tx/processing-rules` |

Use distinct paths per document (the citation groups by document, so URLs must differ). Star has no external portal; use a Coupa/internal placeholder host consistently.

- [ ] **Step 2: Prepend the front-matter block to each gold doc.** For each file, insert above the H1:

```
---
url: <the chosen URL>
updated: 2026-08-30
---
```

Do not otherwise edit the body. Vary `updated:` dates modestly across docs (e.g. 2026-07 to 2026-09) so the `temporal` question category has real spread to exercise.

- [ ] **Step 3: Reseed and confirm gold retrieval is unchanged.**

Run: `make seed` then `python3 scripts/check_corpus.py` (chat-api must be running in mock-LLM mode — use `/run-local` or `/check-retrieval`).
Expected: all 36 gold questions still pass (keyword presence, no anti-keyword hits). This is the frozen-gold guarantee: adding front-matter must not move retrieval.

- [ ] **Step 4: Spot-check a citation link in the UI.** Open the local UI, ask one gold question (e.g. a Coupa AVR question), and confirm the source rail now shows a `sf-demo.*` link, not a `file://` path.

- [ ] **Step 5: Commit**

```bash
git add fixtures/
git commit -m "Give the 23 gold fixtures system-native citation links

Front-matter only; no body changes. Citations now resolve to placeholder
ServiceNow/Coupa/Icertis/Workday URLs instead of file:// paths."
```

---

### Task 3: Generate Tier 2 adversarial distractors (batched, gated)

Generate ~90 distractor documents whose job is to be ranked *below* gold. Work in batches by team; after each batch, the retrieval guardrail must stay green before continuing.

**Files:**
- Create: ~90 new `.md` files under `fixtures/{coupa,star,hr}/` (weighted toward coupa, matching the corpus's depth-over-breadth bias)
- Reference: `fixtures/tests.jsonl` (read-only — distractors add no rows)

**Interfaces:**
- Consumes: Task 1 front-matter parsing (every distractor carries `url:` + `updated:`); `scripts/check_corpus.py --scores`.
- Produces: distractor docs. No test rows, no code.

- [ ] **Step 1: Write the three distractor generation prompts.** One per kind, each producing a single house-style Markdown doc with front-matter:
  - **near-twin:** given a gold doc, reuse its exact vocabulary (`AVR-TIMEOUT`, `icertis_contract_id`, `snow-product-push`, error codes, job names) in a *different, wrong* context that must NOT answer the gold question about that term.
  - **adjacent-system:** invent a plausible sibling integration that does not exist in gold (another Coupa middleware flow, another Star report, another Workday area), same house style.
  - **cross-team lookalike:** a doc on one team resembling another team's material.
  Each prompt must emit a front-matter `url:` on the correct system host and an `updated:` date, an H1 title, and `##` sections under ~800 words with retrievable facts as bullets (per the `fixtures/` convention).

- [ ] **Step 2: Generate the coupa batch (~45 docs).** Write files with descriptive slug filenames. Keep hostnames to the placeholder set only; invent no real-looking employee names or real endpoints.

- [ ] **Step 3: Sample-review the coupa batch.** Read ~5 of the 45 for house style, placeholder-only hosts, and no accidentally-real data. Fix the batch's systematic issues, not every file.

- [ ] **Step 4: Reseed and run the guardrail gate.**

Run: `make seed` then `python3 scripts/check_corpus.py --scores`
Expected, for the coupa gold questions specifically:
  - every gold question still retrieves its gold doc **above `KA_RELEVANCE_FLOOR`** (`top=` score printed by `--scores` ≥ floor);
  - no `anti_keywords` regressions;
  - no distractor satisfies a gold question's `keywords` (inspect any gold question that newly fails — a distractor that became a true positive must be edited or deleted).
A doc that trips the gate is fixed or removed before Step 5. Do not proceed with a red gate.

- [ ] **Step 5: Repeat Steps 2-4 for star (~25 docs), then hr (~20 docs).** Run the guardrail gate after each team's batch, scoped with `--team` to keep the signal readable (e.g. `python3 scripts/check_corpus.py --scores --team star`).

- [ ] **Step 6: Full-corpus guardrail.**

Run: `python3 scripts/check_corpus.py --scores` (all teams)
Expected: all 36 gold questions green; note the `top=` scores — they feed Task 5.

- [ ] **Step 7: Commit** (one commit per team batch is fine; or one at the end)

```bash
git add fixtures/
git commit -m "Add Tier 2 adversarial distractors to the demo corpus

~90 generated near-twin, adjacent-system and cross-team documents that
reuse gold vocabulary in wrong contexts, so retrieval has to rank gold
above plausible competition. Guardrail (check_corpus.py --scores) green:
every gold question still out-ranks its distractors above the floor."
```

---

### Task 4: Add Tier 3 new question-first content

Add ~35 genuinely new documents *within* coupa/star/hr, written question-first like the original corpus, with their questions appended to `tests.jsonl`.

**Files:**
- Modify: `fixtures/tests.jsonl` (append new rows only)
- Create: ~35 new `.md` files under `fixtures/{coupa,star,hr}/`

**Interfaces:**
- Consumes: the `tests.jsonl` schema enforced by `scripts/check_corpus.py` `load()` — required fields `id, question, team, category, keywords, anti_keywords, reference_answer`; `category` in {`direct_fact, spanning, near_twin, cross_team, unanswerable, temporal`}; `cross_team` rows need `expect_suggestion`.
- Produces: new gold docs + gold questions. Each new doc carries front-matter `url:`/`updated:`.

- [ ] **Step 1: Draft the new questions FIRST** and append them to `fixtures/tests.jsonl` — one JSON object per line, matching the existing rows' shape. Cover the new systems you intend to document; include a spread across categories (some `direct_fact`, some `near_twin`, at least one `temporal` leaning on `updated:` dates). Do not write the documents yet.

- [ ] **Step 2: Validate the question-set schema before writing docs.**

Run: `python3 scripts/check_corpus.py --validate`
Expected: PASS (schema only; no API needed). Fix any missing-field / bad-category / duplicate-id errors.

- [ ] **Step 3: Write the documents to answer those questions**, in house style, each with front-matter, H1, `##` sections under ~800 words, facts as bullets. These carry the "survives interrogation" bar — write them as carefully as the original 23.

- [ ] **Step 4: Reseed and confirm the new questions pass.**

Run: `make seed` then `python3 scripts/check_corpus.py`
Expected: all questions green — the original 36 AND the new Tier-3 rows.

- [ ] **Step 5: Commit**

```bash
git add fixtures/
git commit -m "Add Tier 3 question-first content to the demo corpus

New systems within the existing teams, written questions-first with rows
appended to tests.jsonl, for demo breadth and a fresh honestly-measured
slice of gold."
```

---

### Task 5: Recalibrate `KA_RELEVANCE_FLOOR`

With the full ~150-doc corpus seeded, re-set the relevance floor from real gold-vs-distractor separation rather than the old estimate, and update the manifest.

**Files:**
- Modify: `deploy/k8s/base/chat-api.yaml` (`KA_RELEVANCE_FLOOR`, currently `0.81`)
- Reference: local `.env` / `make run-api` env if the local floor is set there too

**Interfaces:**
- Consumes: `scripts/check_corpus.py --scores` output (`top=` per question).
- Produces: a recalibrated floor value. No code.

- [ ] **Step 1: Collect the score distribution.**

Run: `python3 scripts/check_corpus.py --scores` and record, per gold question, the `top=` score of the correct document. Separately note the top scores distractors reach for those same queries (inspect the `retrieval` event, or temporarily lower the floor to see near-misses).

- [ ] **Step 2: Choose the floor to sit in the gap** — above the highest score a *distractor* reaches on a gold query, below the lowest score a *gold* document needs to clear. Set it from the frozen gold questions' separation, not from distractor scores in isolation (avoids over-fitting to synthetic noise). If gold and distractor bands overlap (no clean gap), keep the floor conservative and record the overlap as a follow-up for reranking work — do not force a value that drops real gold.

- [ ] **Step 3: Update `deploy/k8s/base/chat-api.yaml`** with the new value and a comment noting it was recalibrated against the ~150-doc corpus on this date. Update the local `.env` value if present so local and deployed floors agree.

- [ ] **Step 4: Confirm the offline deploy gate still passes.**

Run: `make k8s-check`
Expected: PASS.

- [ ] **Step 5: Re-run the guardrail at the new floor.**

Run: `python3 scripts/check_corpus.py` (restart chat-api so it picks up the new floor)
Expected: all gold questions still green at the recalibrated floor.

- [ ] **Step 6: Commit**

```bash
git add deploy/k8s/base/chat-api.yaml .env
git commit -m "Recalibrate KA_RELEVANCE_FLOOR against the ~150-doc corpus

The old floor rested on a corpus where nearly everything cleared it. With
adversarial distractors present, set the floor in the gold-vs-distractor
gap measured by check_corpus.py --scores."
```

---

### Task 6: Update corpus documentation

Reflect the placeholder-links convention and the new corpus size in the README.

**Files:**
- Modify: `fixtures/README.md`

**Interfaces:** none.

- [ ] **Step 1: Add a placeholder-links note** to `fixtures/README.md`: the source links in front-matter (`sf-demo.service-now.com`, `sf-demo.coupahost.com`, `sf-demo.icertis.com`, Workday) are non-production placeholders, same as the hostnames already called out — do not open or trust them.

- [ ] **Step 2: Update the corpus-size line** if the README states a document count, to reflect ~150 across three tiers, and point at this design doc.

- [ ] **Step 3: Commit**

```bash
git add fixtures/README.md
git commit -m "Note placeholder citation links and new corpus size in fixtures README"
```

---

## Self-Review

**Spec coverage:**
- Tier 1 frozen + links → Task 2. Tier 2 distractors + guardrail → Task 3. Tier 3 question-first → Task 4. Links code change → Task 1. Floor recalibration → Task 5. README placeholder note → Task 6. Ranking-demonstrable validation → guardrail steps in Tasks 3/5. All spec sections mapped.

**Placeholder scan:** Code steps (Task 1) carry full Go source and test code. Content-generation tasks (3, 4) are inherently generative; they are pinned to concrete commands, the exact guardrail gate, and the `tests.jsonl` schema rather than "write tests for the above." No "TBD"/"handle edge cases" left.

**Type consistency:** `parseFrontMatter(raw string) (map[string]string, string)` is defined in Task 1 and consumed by name in Tasks 2-4. `page` fields (`WebURL`, `UpdatedAt`, `Markdown`, `Title`) match `services/ingestion/cmd/indexer/main.go`. `chunker.Title(md, fallback)` signature matches. `check_corpus.py --scores`/`--validate`/`--team` flags match the script.
