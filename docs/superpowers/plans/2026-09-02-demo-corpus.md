# Demo Corpus and Question Set Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the five-document placeholder corpus with 23 documents describing the real integrations, written against a ~30-question test set that is authored first.

**Architecture:** Questions come first in `fixtures/tests.jsonl`; documents are then written to answer them. A checking script (`scripts/check_corpus.py`) seeds the index, runs each question through the real scoped API, and asserts that required keywords appear in the retrieved context and forbidden ones do not. That script is the test cycle for every document task — the equivalent of "run the tests" for prose.

**Tech Stack:** Markdown (corpus), JSON Lines (question set), Python 3 (checking script, stdlib only), Go (two small source changes), OpenSearch + Ollama via `make dev-up`.

**Spec:** `docs/superpowers/specs/2026-09-02-demo-corpus-design.md`

## Global Constraints

- **Company:** State Farm Insurance. Hostnames and tenant identifiers are obvious non-production placeholders (`sf-demo.coupahost.com`, `sf-demo.icertis.com`). Never a real production endpoint.
- **All content is fabricated.** Real system and team names; every field name, job name, schedule, volume, error code, owner and metric is invented.
- **Each `##` section is self-contained and under ~800 words.** `chunker.Split(md, 800, 100)` splits on headings first, then 800-word windows; an overflowing section's second window loses its heading context.
- **Facts that must be retrievable are bullet lists, not tables.** `Split` joins with `strings.Join(words, " ")`, destroying newlines: a table becomes one run of `| a | b | | c | d |`. Tables are allowed only for scannable overviews no question depends on.
- **Teams:** slugs stay `coupa`, `star`, `hr`. Only the `coupa` display name changes.
- **Every document starts with an `# H1` title and a 2–4 sentence prose introduction**, then `##` sections. Match the register of the existing `fixtures/coupa/icertis-coupa-integration.md`.
- **Commit after every task.** Work happens on the `demo-corpus` branch.

---

### Task 1: Reset the corpus and rename the Coupa team

Clears the four documents that describe systems which do not exist, and renames the team to what it is actually called. After this task the index is intentionally empty — that is the correct state, not a failure.

**Files:**
- Modify: `internal/team/team.go:29`
- Modify: `internal/team/team_test.go` (add one test)
- Delete: `fixtures/coupa/icertis-coupa-integration.md`, `fixtures/coupa/servicenow-integration.md`, `fixtures/star/avr-integration.md`, `fixtures/star/salesforce-integration.md`, `fixtures/hr/leave-policy.md`
- Create: `fixtures/README.md`

**Interfaces:**
- Consumes: nothing.
- Produces: `team.DefaultInfos()` returns `{Slug: "coupa", DisplayName: "Coupa AWS Middleware"}`. The UI reads display names from `GET /v1/teams` (`ui/src/App.jsx:129`), so no UI change is needed.

- [ ] **Step 1: Write the failing test**

Append to `internal/team/team_test.go`:

```go
func TestDefaultInfosDisplayNames(t *testing.T) {
	want := map[string]string{
		"coupa": "Coupa AWS Middleware",
		"star":  "Star",
		"hr":    "HR",
	}
	infos := DefaultInfos()
	if len(infos) != len(want) {
		t.Fatalf("DefaultInfos() returned %d teams, want %d", len(infos), len(want))
	}
	for _, info := range infos {
		w, ok := want[info.Slug]
		if !ok {
			t.Errorf("unexpected team slug %q", info.Slug)
			continue
		}
		if info.DisplayName != w {
			t.Errorf("DisplayName for %q = %q, want %q", info.Slug, info.DisplayName, w)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/team/ -run TestDefaultInfosDisplayNames -v`
Expected: FAIL — `DisplayName for "coupa" = "Coupa", want "Coupa AWS Middleware"`

- [ ] **Step 3: Rename the team**

In `internal/team/team.go`, change line 29:

```go
		{Slug: "coupa", DisplayName: "Coupa AWS Middleware"},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/team/ -v`
Expected: PASS

- [ ] **Step 5: Delete the placeholder corpus**

```bash
rm fixtures/coupa/icertis-coupa-integration.md \
   fixtures/coupa/servicenow-integration.md \
   fixtures/star/avr-integration.md \
   fixtures/star/salesforce-integration.md \
   fixtures/hr/leave-policy.md
```

- [ ] **Step 6: Create `fixtures/README.md`**

```markdown
# Demo corpus — synthetic content

Every document in this directory is **fabricated**. The team names and
integration names are real; every field name, job name, schedule, volume,
error code, owner and metric in them is invented for demonstration and
testing.

These documents read convincingly. Do not treat any of them as
documentation, do not page anyone named in them, and do not copy a
schedule or endpoint out of them into anything real.

Hostnames and tenant identifiers are deliberate non-production
placeholders (`sf-demo.coupahost.com`, `sf-demo.icertis.com`).

The corpus is written against `tests.jsonl`, the question set it must be
able to answer. See `docs/superpowers/specs/2026-09-02-demo-corpus-design.md`.
```

- [ ] **Step 7: Verify the whole suite still passes**

Run: `go test ./...`
Expected: all packages ok. `make seed` is expected to index 0 chunks until Task 3; do not run it here.

- [ ] **Step 8: Commit**

```bash
git add internal/team/team.go internal/team/team_test.go fixtures/
git commit -m "Reset the demo corpus and rename the Coupa team

The four deleted documents describe systems that do not exist: AVR as a
security scanner, ServiceNow as incident sync, and a Salesforce
integration Star does not own. They are replaced from scratch rather than
edited.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 2: Question set and checking script

The question set is the specification for every document that follows, so it is written and reviewed before any prose. The script it ships with is the test cycle for Tasks 3–10.

**Files:**
- Create: `fixtures/tests.jsonl`
- Create: `scripts/check_corpus.py`

**Interfaces:**
- Produces: `fixtures/tests.jsonl`, one JSON object per line with fields `id` (string, stable, referenced by later tasks), `question` (string), `team` (`coupa`|`star`|`hr`), `category` (`direct_fact`|`spanning`|`near_twin`|`cross_team`|`unanswerable`|`temporal`), `keywords` (array of strings that must appear in retrieved context), `anti_keywords` (array of strings that must not appear), `reference_answer` (string), and optionally `expect_suggestion` (team slug, `cross_team` only).
- Produces: `scripts/check_corpus.py`, invoked as `python3 scripts/check_corpus.py [--validate] [--id ID]... [--category CAT] [--exclude-category CAT]... [--team TEAM] [--scores]`. Exit 0 when every selected question passes, 1 otherwise.

- [ ] **Step 1: Write the checking script**

Create `scripts/check_corpus.py`. Standard library only.

```python
#!/usr/bin/env python3
"""Check that the corpus can answer fixtures/tests.jsonl.

Validates the question-set schema, and (unless --validate) runs each
selected question through the running chat-api on the team it is scoped
to, asserting that every keyword appears somewhere in the retrieved
context and that no anti_keyword does.

This is not the evaluation harness: no MRR, no nDCG, no LLM judge. It
answers one question -- did retrieval surface the right material -- which
is what the corpus tasks need to iterate against.
"""
import argparse
import json
import pathlib
import sys
import urllib.error
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parent.parent
TESTS = ROOT / "fixtures" / "tests.jsonl"
API = "http://localhost:8080/v1/chat/messages"
CATEGORIES = {"direct_fact", "spanning", "near_twin", "cross_team",
              "unanswerable", "temporal"}
TEAMS = {"coupa", "star", "hr"}


def load():
    tests, seen = [], set()
    for n, line in enumerate(TESTS.read_text(encoding="utf-8").splitlines(), 1):
        line = line.strip()
        if not line:
            continue
        try:
            t = json.loads(line)
        except json.JSONDecodeError as e:
            sys.exit(f"{TESTS}:{n}: invalid JSON: {e}")
        for field in ("id", "question", "team", "category", "keywords",
                      "anti_keywords", "reference_answer"):
            if field not in t:
                sys.exit(f"{TESTS}:{n}: missing required field {field!r}")
        if t["id"] in seen:
            sys.exit(f"{TESTS}:{n}: duplicate id {t['id']!r}")
        seen.add(t["id"])
        if t["team"] not in TEAMS:
            sys.exit(f"{TESTS}:{n}: unknown team {t['team']!r}")
        if t["category"] not in CATEGORIES:
            sys.exit(f"{TESTS}:{n}: unknown category {t['category']!r}")
        if t["category"] == "cross_team" and "expect_suggestion" not in t:
            sys.exit(f"{TESTS}:{n}: cross_team question needs expect_suggestion")
        if t["category"] != "unanswerable" and not t["keywords"]:
            sys.exit(f"{TESTS}:{n}: {t['category']} question needs keywords")
        tests.append(t)
    return tests


def retrieve(test):
    """Return (chunks, suggestion) from one scoped chat request."""
    body = json.dumps({"message": test["question"]}).encode()
    req = urllib.request.Request(API, data=body, method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("X-Team", test["team"])
    req.add_header("X-Dev-User", "dev@example.com")
    chunks, suggestion, event = [], None, None
    with urllib.request.urlopen(req, timeout=120) as resp:
        for raw in resp:
            line = raw.decode("utf-8").rstrip("\n")
            if line.startswith("event:"):
                event = line[6:].strip()
            elif line.startswith("data:"):
                payload = line[5:].strip()
                if event == "retrieval":
                    chunks = json.loads(payload)
                elif event == "suggestion":
                    suggestion = json.loads(payload)
                elif event == "error":
                    sys.exit(f"api error for {test['id']}: {payload}")
    return chunks, suggestion


def check(test):
    chunks, suggestion = retrieve(test)
    haystack = "\n".join(c["text"] for c in chunks).lower()
    missing = [k for k in test["keywords"] if k.lower() not in haystack]
    forbidden = [k for k in test["anti_keywords"] if k.lower() in haystack]
    top = chunks[0]["score"] if chunks else 0.0

    problems = []
    if missing:
        problems.append("missing keywords: " + ", ".join(missing))
    if forbidden:
        problems.append("LEAKED anti-keywords: " + ", ".join(forbidden))
    if test["category"] == "cross_team":
        want = test["expect_suggestion"]
        got = [s["team"] for s in (suggestion or [])]
        if want not in got:
            problems.append(f"expected a suggestion for {want}, got {got or 'none'}")
    return top, problems


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--validate", action="store_true",
                   help="check the schema only; do not call the API")
    p.add_argument("--id", action="append", default=[])
    p.add_argument("--category")
    p.add_argument("--exclude-category", action="append", default=[],
                   help="skip a category; used to defer cross_team checks "
                        "until the answering team's documents exist")
    p.add_argument("--team")
    p.add_argument("--scores", action="store_true",
                   help="print each question's top score, for floor calibration")
    args = p.parse_args()

    tests = load()
    print(f"schema ok: {len(tests)} questions")
    if args.validate:
        return 0

    selected = [t for t in tests
                if (not args.id or t["id"] in args.id)
                and (not args.category or t["category"] == args.category)
                and t["category"] not in args.exclude_category
                and (not args.team or t["team"] == args.team)]
    if not selected:
        sys.exit("no questions matched the filters")

    failures = 0
    for t in selected:
        try:
            top, problems = check(t)
        except urllib.error.URLError as e:
            sys.exit(f"cannot reach {API}: {e}\nIs `make run-api` running?")
        status = "PASS" if not problems else "FAIL"
        if problems:
            failures += 1
        score = f"  top={top:.4f}" if args.scores else ""
        print(f"[{status}] {t['id']:<24} {t['category']:<12}{score}  {t['question'][:60]}")
        for problem in problems:
            print(f"         {problem}")
    print(f"\n{len(selected) - failures}/{len(selected)} passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 2: Write the question set**

Create `fixtures/tests.jsonl`, one object per line. The `reference_answer` states the fact; documents in later tasks must make it true. Where a fact is invented here (a schedule, an error code), that exact value is what the document must use.

Write these 30 questions. Field order does not matter; `anti_keywords` is `[]` where nothing is forbidden.

**Coupa AWS Middleware — ServiceNow (`coupa-snow-*`)**

1. `coupa-snow-01`, `direct_fact` — "Which fields does the ServiceNow product sync send from Coupa?" keywords: `sys_id`, `u_coupa_item_id`, `catalog`. Reference answer names the Coupa→ServiceNow product-item field list.
2. `coupa-snow-02`, `direct_fact` — "How often does the ServiceNow request sync pull purchased product data into Coupa?" keywords: `snow-request-pull`, `every 10 minutes`. anti_keywords: `every 2 min`.
3. `coupa-snow-03`, `spanning` — "If the ServiceNow product sync fails with a duplicate item error, what do I do?" keywords: `SNOW-409`, `replay`, `u_coupa_item_id`. Needs the field name from `servicenow-product-sync.md` and the procedure from `servicenow-runbook.md`.

**Coupa AWS Middleware — Icertis (`coupa-icertis-*`)**

4. `coupa-icertis-01`, `direct_fact` — "What are the three phases of the Icertis contract integration?" keywords: `fetch`, `load`, `resync`.
5. `coupa-icertis-02`, `direct_fact` — "Which Icertis field becomes the Coupa external ID?" keywords: `icertis_contract_id`, `external-id`.
6. `coupa-icertis-03`, `spanning` — "What happens if the resync confirmation back to Icertis fails after the contract loaded in Coupa?" keywords: `ICT-503`, `orphaned`, `icertis-resync-retry`. Spans overview and resync runbook; the answer must state the contract is loaded in Coupa but unconfirmed in Icertis.
7. `coupa-icertis-04`, `temporal` — "When did the Icertis integration start handling amendment versioning?" keywords: `amendment`, `2026-04`. Requires a dated changelog entry.

**Coupa AWS Middleware — AVR (`coupa-avr-*`)**

8. `coupa-avr-01`, `direct_fact` — "Is AVR a push or pull integration?" keywords: `pull`, `synchronous`, `Coupa calls`. Answer: Coupa calls the middleware's REST endpoint, which calls the on-prem SOAP service.
9. `coupa-avr-02`, `direct_fact` — "Which SOAP operations does the AVR service expose?" keywords: `GetInvoiceDetail`, `GetPurchaseOrder`, `GetReceipt`.
10. `coupa-avr-03`, `direct_fact` — "Which on-prem systems supply AVR's data?" keywords: `PeopleSoft`, `expense`.
11. `coupa-avr-04`, `spanning` — "What JSON field does the AVR SOAP element InvoiceNbr map to, and what does Coupa call it?" keywords: `InvoiceNbr`, `invoiceNumber`. Spans field mapping and REST facade.
12. `coupa-avr-05`, `spanning` — "What does the middleware return to Coupa when the on-prem SOAP service times out?" keywords: `504`, `AVR-TIMEOUT`, `30 seconds`. Spans REST facade and runbook.

**Coupa AWS Middleware — cross-cutting (`coupa-arch-*`)**

13. `coupa-arch-01`, `direct_fact` — "Who is on call for the Coupa AWS Middleware integrations?" keywords: `Middleware Platform`, `PagerDuty`.
14. `coupa-arch-02`, `spanning` — "Which AWS account and region do the Coupa middleware integrations run in, and what fronts the AVR REST API?" keywords: `us-east-1`, `API Gateway`. Spans architecture and REST facade.

**Star — pipeline and Louisiana (`star-la-*`, `star-pipe-*`)**

15. `star-pipe-01`, `direct_fact` — "What format is data written to S3 in for the data calls?" keywords: `parquet`.
16. `star-pipe-02`, `direct_fact` — "Where do the data call reports get published for reporting?" keywords: `QuickSight`.
17. `star-la-01`, `near_twin` — "What is the submission deadline for the Louisiana data call?" keywords: `Louisiana`, `LDI`, `March 1`. anti_keywords: `Texas`, `TDI`.
18. `star-la-02`, `near_twin` — "How does the Louisiana data call aggregate premium by coverage?" keywords: `Louisiana`, `written premium`, `effective date basis`. anti_keywords: `Texas`, `TDI`, `earned premium`.
19. `star-la-03`, `near_twin` — "Which reports does the Louisiana data call produce?" keywords: `la-premium-summary`, `la-loss-detail`. anti_keywords: `tx-premium-summary`, `Texas`.
20. `star-la-04`, `spanning` — "How does Louisiana premium data get from the mainframe to QuickSight?" keywords: `Redshift`, `parquet`, `QuickSight`, `Louisiana`. Spans the pipeline document and the Louisiana overview.

**Star — Texas (`star-tx-*`)**

21. `star-tx-01`, `near_twin` — "What is the submission deadline for the Texas data call?" keywords: `Texas`, `TDI`, `May 15`. anti_keywords: `Louisiana`, `LDI`, `March 1`.
22. `star-tx-02`, `near_twin` — "How does the Texas data call aggregate premium by coverage?" keywords: `Texas`, `earned premium`, `exposure period basis`. anti_keywords: `Louisiana`, `LDI`, `written premium`.
23. `star-tx-03`, `near_twin` — "Which reports does the Texas data call produce?" keywords: `tx-premium-summary`, `tx-catastrophe-exposure`. anti_keywords: `la-premium-summary`, `Louisiana`.
24. `star-tx-04`, `temporal` — "When did the Texas data call add catastrophe exposure reporting?" keywords: `catastrophe`, `2026-06`. anti_keywords: `Louisiana`.

**HR (`hr-*`)**

25. `hr-01`, `direct_fact` — "How many days of annual leave do employees accrue?" keywords: `25 days`, `monthly`.
26. `hr-02`, `direct_fact` — "Which Workday object holds the reporting line for an employee?" keywords: `Workday`, `supervisory organization`.
27. `hr-03`, `direct_fact` — "How often does payroll data sync from Workday?" keywords: `workday-payroll-sync`, `every 4 hours`.
28. `hr-04`, `spanning` — "Who approves a compensation change above band?" keywords: `compensation`, `band`, `approval`. Spans compensation and org structure.

**Cross-team and unanswerable**

29. `cross-01`, `cross_team`, team `coupa`, expect_suggestion `hr` — "What are the compensation bands for a senior engineer?" keywords: `[]`. anti_keywords: `[]`.
30. `cross-02`, `cross_team`, team `star`, expect_suggestion `coupa` — "How do I resend a failed Icertis contract to Coupa?" keywords: `[]`. anti_keywords: `[]`.
31. `cross-03`, `cross_team`, team `hr`, expect_suggestion `star` — "Which reports go to the Texas Department of Insurance?" keywords: `[]`. anti_keywords: `[]`.
32. `unans-01`, `unanswerable`, team `coupa` — "What is the office wifi password?" keywords: `[]`. reference_answer states the corpus does not cover this.
33. `unans-02`, `unanswerable`, team `star` — "Which vendor supplies our actuarial pricing models?" keywords: `[]`.
34. `unans-03`, `unanswerable`, team `hr` — "What is the parking policy at the Bloomington campus?" keywords: `[]`.

(36 questions, satisfying the spec's "approximately 30". The per-category
counts land at 13 `direct_fact`, 7 `spanning`, 8 `near_twin`, 3 `cross_team`,
3 `unanswerable`, 2 `temporal` — the spec's table was a target, and the extra
`direct_fact` and `spanning` questions fall out of there being 23 documents to
cover. `cross_team` questions carry empty `keywords` because the point is that
nothing relevant is retrievable in the asking team.)

- [ ] **Step 3: Validate the schema**

Run: `python3 scripts/check_corpus.py --validate`
Expected: `schema ok: 36 questions`, exit 0.

- [ ] **Step 4: Commit**

```bash
git add fixtures/tests.jsonl scripts/check_corpus.py
git commit -m "Add the corpus question set and its checking script

Questions are written before the documents so the corpus is not written
to flatter them. anti_keywords catches the Louisiana/Texas leakage that
MRR and nDCG are silent about.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

- [ ] **Step 5: STOP — human review gate**

Do not start Task 3. Present the question set to the user for review. This is where the team's knowledge of what people actually ask enters the design, and where invented facts that contradict reality get caught. Every later task is written against these questions, so changes are cheap now and expensive later.

---

### Task 3: ServiceNow documents

**Files:**
- Create: `fixtures/coupa/servicenow-product-sync.md`, `fixtures/coupa/servicenow-request-sync.md`, `fixtures/coupa/servicenow-runbook.md`

**Interfaces:**
- Consumes: `fixtures/tests.jsonl` ids `coupa-snow-01`, `coupa-snow-02`, `coupa-snow-03`.
- Produces: the job names `snow-product-push` and `snow-request-pull`, and error code `SNOW-409`, referenced by Task 6's escalation document.

- [ ] **Step 1: Write `servicenow-product-sync.md`**

Direction: Coupa → ServiceNow, product and catalog items. Required `##` sections: *What syncs* (scope and filters); *Fields sent to ServiceNow* (bullet list including `sys_id`, `u_coupa_item_id`, `catalog`, plus 6–10 more); *Jobs* (including `snow-product-push` with an invented schedule); *Volume* (invented daily counts).

- [ ] **Step 2: Write `servicenow-request-sync.md`**

Direction: ServiceNow → Coupa, request data for purchased product. Required sections: *What syncs*; *Fields sent to Coupa* (bullet list); *Jobs* — must include `snow-request-pull` running **every 10 minutes**, matching `coupa-snow-02`; *Volume*.

- [ ] **Step 3: Write `servicenow-runbook.md`**

Required sections: *Error codes* — bullet list including **`SNOW-409` duplicate item**, keyed on `u_coupa_item_id`, with the replay procedure; 4–6 further invented codes; *Replay procedure*; *When to escalate*.

- [ ] **Step 4: Seed and check**

```bash
make dev-up && make seed
make run-api   # in another shell, or background it
python3 scripts/check_corpus.py --id coupa-snow-01 --id coupa-snow-02 --id coupa-snow-03 --scores
```

Expected: 3/3 passed. If `coupa-snow-03` fails, the two facts it spans are probably in one document — that question exists to prove spanning retrieval works, so move the procedure into the runbook rather than duplicating the field name.

- [ ] **Step 5: Commit**

```bash
git add fixtures/coupa/
git commit -m "Add ServiceNow integration documents

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 4: Icertis documents

**Files:**
- Create: `fixtures/coupa/icertis-contract-overview.md`, `fixtures/coupa/icertis-field-mapping.md`, `fixtures/coupa/icertis-resync-runbook.md`

**Interfaces:**
- Consumes: ids `coupa-icertis-01` through `coupa-icertis-04`.
- Produces: error code `ICT-503` and job name `icertis-resync-retry`, referenced by Task 6.

- [ ] **Step 1: Write `icertis-contract-overview.md`**

Required sections: *How it works* — the three phases named **fetch**, **load**, **resync**, matching `coupa-icertis-01`; *Sync scope* (which contracts qualify); *Jobs*; *Changelog* — dated entries, including one in **2026-04** recording that amendment versioning was added, matching `coupa-icertis-04`.

- [ ] **Step 2: Write `icertis-field-mapping.md`**

Required section *Fields sent to Coupa*: bullet list, including `icertis_contract_id` mapped to Coupa's **`external-id`**, matching `coupa-icertis-02`, plus 10–12 further fields. Reuse the shape of the deleted Icertis document; its register was correct.

- [ ] **Step 3: Write `icertis-resync-runbook.md`**

Required sections: *Error codes* — including **`ICT-503`**, the resync confirmation failure, whose description must state the contract is loaded in Coupa but left **orphaned** (unconfirmed) in Icertis; *Recovery* — the **`icertis-resync-retry`** job and how to run it; *When to escalate*. These three keywords are what `coupa-icertis-03` retrieves on.

- [ ] **Step 4: Seed and check**

```bash
make seed
python3 scripts/check_corpus.py --id coupa-icertis-01 --id coupa-icertis-02 --id coupa-icertis-03 --id coupa-icertis-04 --scores
```

Expected: 4/4 passed.

- [ ] **Step 5: Commit**

```bash
git add fixtures/coupa/
git commit -m "Add Icertis contract integration documents

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 5: AVR documents

The protocol facade — Coupa calls REST/JSON, the middleware calls the on-prem SOAP service. This is the set the engineering audience will probe hardest.

**Files:**
- Create: `fixtures/coupa/avr-soap-service.md`, `fixtures/coupa/avr-rest-facade.md`, `fixtures/coupa/avr-field-mapping.md`, `fixtures/coupa/avr-runbook.md`

**Interfaces:**
- Consumes: ids `coupa-avr-01` through `coupa-avr-05`, and `coupa-arch-02`.
- Produces: `AVR-TIMEOUT`, the 30-second timeout, and the API Gateway reference, all used by Task 6.

- [ ] **Step 1: Write `avr-soap-service.md`**

Required sections: *The on-prem service* (what it is, where it runs, connectivity); *Operations* — bullet list naming **`GetInvoiceDetail`**, **`GetPurchaseOrder`**, **`GetReceipt`** with their inputs, matching `coupa-avr-02`; *Source systems* — **PeopleSoft** and **expense** reports plus 1–2 more, matching `coupa-avr-03`.

- [ ] **Step 2: Write `avr-rest-facade.md`**

Required sections: *Request model* — must state explicitly that this is a **pull**, **synchronous** model, that **Coupa calls** the middleware, matching `coupa-avr-01`; *Endpoints* (REST paths, auth); *Fronting* — **API Gateway**, for `coupa-arch-02`; *Timeouts and errors* — the **30 second** upstream timeout returning **`504`** with **`AVR-TIMEOUT`**, for `coupa-avr-05`.

- [ ] **Step 3: Write `avr-field-mapping.md`**

Required section *SOAP to JSON*: bullet list mapping SOAP elements to JSON fields, including **`InvoiceNbr`** → **`invoiceNumber`** for `coupa-avr-04`, plus 12–15 further mappings across invoice, PO and receipt.

- [ ] **Step 4: Write `avr-runbook.md`**

Required sections: *Error codes* (including `AVR-TIMEOUT` with the operator-facing procedure); *On-prem connectivity loss*; *SOAP faults*; *When to escalate*.

- [ ] **Step 5: Seed and check**

```bash
make seed
python3 scripts/check_corpus.py --id coupa-avr-01 --id coupa-avr-02 --id coupa-avr-03 --id coupa-avr-04 --id coupa-avr-05 --scores
```

Expected: 5/5 passed.

- [ ] **Step 6: Commit**

```bash
git add fixtures/coupa/
git commit -m "Add AVR SOAP-to-REST facade documents

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 6: Coupa architecture and escalation

**Files:**
- Create: `fixtures/coupa/coupa-middleware-architecture.md`, `fixtures/coupa/coupa-oncall-escalation.md`

**Interfaces:**
- Consumes: ids `coupa-arch-01`, `coupa-arch-02`; job and error-code names produced by Tasks 3–5.

- [ ] **Step 1: Write `coupa-middleware-architecture.md`**

Required sections: *Overview* (how ServiceNow, Icertis and AVR sit together); *AWS footprint* — must name **`us-east-1`** for `coupa-arch-02`; *Shared components*; *Data stores*.

- [ ] **Step 2: Write `coupa-oncall-escalation.md`**

Required sections: *Ownership* — the **Middleware Platform** team, with **PagerDuty** as the paging path, matching `coupa-arch-01`; *Escalation by integration* — referencing the real job and error-code names from Tasks 3–5 so the cross-references are consistent; *Out-of-hours*.

- [ ] **Step 3: Seed and check**

```bash
make seed
python3 scripts/check_corpus.py --team coupa --exclude-category cross_team --scores
```

Expected: 15/15 passed — the 14 Coupa integration questions plus `unans-01`, which passes trivially since it has no keywords. `cross_team` is excluded because `cross-01` is scoped to `coupa` but is answered by HR documents that do not exist until Task 9; it is checked in Task 10.

Every question from Tasks 3–5 must still pass, not just the two new ones. A question that passed in an 8-chunk index and fails in a 60-chunk one is the first real retrieval signal this project has had; investigate rather than paper over it.

- [ ] **Step 4: Commit**

```bash
git add fixtures/coupa/
git commit -m "Add Coupa middleware architecture and escalation documents

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 7: Data call pipeline and Louisiana

**Files:**
- Create: `fixtures/star/data-call-pipeline.md`, `fixtures/star/louisiana-data-call-overview.md`, `fixtures/star/louisiana-processing-rules.md`, `fixtures/star/louisiana-report-catalog.md`

**Interfaces:**
- Consumes: ids `star-pipe-01`, `star-pipe-02`, `star-la-01` through `star-la-04`.
- Produces: the section structure that Task 8's Texas documents mirror, and the Louisiana values Task 8's `anti_keywords` forbid.

- [ ] **Step 1: Write `data-call-pipeline.md`**

State-agnostic mechanics only — no Louisiana or Texas specifics, since both states' documents depend on this one for spanning questions. Required sections: *Extract* (**Redshift** and mainframe); *Staging* (S3, **parquet**, for `star-pipe-01`); *Report execution* (SQL); *Publishing* (**QuickSight**, for `star-pipe-02`); *Scheduling and orchestration*.

- [ ] **Step 2: Write `louisiana-data-call-overview.md`**

Required sections: *Scope* — **Louisiana**, the **LDI** regulator; *Submission deadline* — **March 1**, matching `star-la-01`; *Data sources*; *Contacts*.

- [ ] **Step 3: Write `louisiana-processing-rules.md`**

The state-specific data elements and business logic; the document `star-la-02` and `star-la-05` retrieve on. Required sections: *Required data elements* — including **`parish_code`** (Louisiana reports by parish, not county), **`coastal_zone_indicator`**, `fortified_roof_credit` and `citizens_takeout_flag`, alongside the common exposure elements; *Louisiana Citizens takeouts* — takeout policies **excluded** from **voluntary market** counts for their first renewal term; *Exclusions*; *Validation rules*. Every section must be recognisably Louisiana, since Texas equivalents follow in Task 8 and the two must not blur.

- [ ] **Step 4: Write `louisiana-report-catalog.md`**

Required section *Reports*: bullet list including **`la-premium-summary`** and **`la-loss-detail`** for `star-la-03`, plus 4–6 more, all prefixed `la-`.

- [ ] **Step 5: Seed and check**

```bash
make seed
python3 scripts/check_corpus.py --id star-pipe-01 --id star-pipe-02 --id star-la-01 --id star-la-02 --id star-la-03 --id star-la-04 --id star-la-05 --scores
```

Expected: 7/7 passed. `anti_keywords` for Texas cannot fail yet — no Texas document exists — so these results are provisional until Task 8.

- [ ] **Step 6: Commit**

```bash
git add fixtures/star/
git commit -m "Add data call pipeline and Louisiana documents

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 8: Texas documents and the near-twin check

The point of maximum retrieval difficulty. Texas documents deliberately mirror Louisiana's structure while differing in every regulatory value.

**Files:**
- Create: `fixtures/star/texas-data-call-overview.md`, `fixtures/star/texas-processing-rules.md`, `fixtures/star/texas-report-catalog.md`

**Interfaces:**
- Consumes: ids `star-tx-01` through `star-tx-04`, and re-runs all `star-la-*` ids now that their `anti_keywords` can actually fail.

- [ ] **Step 1: Write `texas-data-call-overview.md`**

Mirror the Louisiana overview's section structure. Required: *Scope* — **Texas**, the **TDI** regulator; *Submission deadline* — **May 15**, matching `star-tx-01`; *Data sources*; *Contacts*.

- [ ] **Step 2: Write `texas-processing-rules.md`**

Required sections mirroring Louisiana's, with values that are **materially different**, not reworded copies: *Required data elements* — including **`county_fips`**, **`windstorm_pool_indicator`**, `tier_1_coastal_county` and `mitigation_credit_code`, matching `star-tx-02`; *TWIA-ceded exposure* — **ceded** exposure **reported separately** from retained, tier-1 coastal counties aggregated on their own, matching `star-tx-05`; *Exclusions*; *Validation rules*; *Changelog* — a dated entry in **2026-06** adding catastrophe exposure reporting, matching `star-tx-04`.

The word "parish" must not appear in any Texas document, and "county" must not appear in any Louisiana one. These are the terms the `anti_keywords` police, and they are the confusion with real consequences in a regulatory filing.

- [ ] **Step 3: Write `texas-report-catalog.md`**

Required section *Reports*: bullet list including **`tx-premium-summary`** and **`tx-catastrophe-exposure`** for `star-tx-03`, plus 4–6 more, all prefixed `tx-`.

- [ ] **Step 4: Seed and run the full Star check**

```bash
make seed
python3 scripts/check_corpus.py --team star --exclude-category cross_team --scores
```

Expected: 13/13 passed — the 12 Star questions plus `unans-02` — with **zero** `LEAKED anti-keywords` lines. `cross-02` is deferred to Task 10 because whether the suggestion fires depends on the relevance floor, which is not calibrated until then.

A leak here is the most informative failure in the plan: it means retrieval cannot separate two near-identical documents, which is exactly the risk the near-twin pair exists to expose. Do not fix it by making the documents more different — that destroys the test. Record the failure, leave it, and raise it: it is evidence for the reranking work, and a demo of the fix is worth more than a corpus that never had the problem.

- [ ] **Step 5: Commit**

```bash
git add fixtures/star/
git commit -m "Add Texas data call documents

Deliberately mirrors the Louisiana structure with different regulatory
values, so retrieval has to separate two near-identical documents.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 9: HR documents

**Files:**
- Create: `fixtures/hr/workday-org-structure.md`, `fixtures/hr/workday-compensation.md`, `fixtures/hr/workday-payroll-integration.md`, `fixtures/hr/leave-policy.md`

**Interfaces:**
- Consumes: ids `hr-01` through `hr-04`, and `cross-01` (whose answer must exist under `hr` for the suggestion to have anything to count).

- [ ] **Step 1: Write `workday-org-structure.md`**

Required sections: *Supervisory organizations* — must use the term **supervisory organization** for the reporting line, matching `hr-02`; *Worker records*; *Position management*.

- [ ] **Step 2: Write `workday-compensation.md`**

Required sections: *Compensation bands* — including senior-engineer bands, which is what `cross-01` must find under `hr`; *Approval* — who signs off a change **above band**, for `hr-04`; *Review cycle*.

- [ ] **Step 3: Write `workday-payroll-integration.md`**

Required sections: *What syncs*; *Jobs* — the payroll sync schedule matching `hr-03`; *Field mapping* (bullet list).

- [ ] **Step 4: Write `leave-policy.md`**

Required sections: *Annual leave* — **25 days**, accrued **monthly**, matching `hr-01`; *Carry-over*; *Escalation*. The deleted original is a reasonable starting point; expand it so it is not the only substantial thing in the team.

- [ ] **Step 5: Seed and check**

```bash
make seed
python3 scripts/check_corpus.py --team hr --exclude-category cross_team --scores
```

Expected: 5/5 passed — `hr-01` through `hr-04` plus `unans-03`. `cross-01` is scoped to `coupa` and `cross-03` depends on the uncalibrated floor; both are checked in Task 10.

- [ ] **Step 6: Commit**

```bash
git add fixtures/hr/
git commit -m "Add Workday and leave policy documents

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 10: Full-corpus verification and relevance floor calibration

Replaces the estimated `KA_RELEVANCE_FLOOR` with a value derived from the answerable-versus-unanswerable split, as the spec requires.

**Files:**
- Modify: `services/chat-api/internal/config/config.go` (the `RelevanceFloor` default and its comment)
- Modify: `README.md` (the embeddings section's floor paragraph)

**Interfaces:**
- Consumes: the complete corpus and all 33 questions.

- [ ] **Step 1: Reseed from clean and run everything**

```bash
curl -X DELETE http://localhost:9200/kb-chunks && make seed
python3 scripts/check_corpus.py --scores
```

Expected: 33/36 or better. The three `cross_team` questions pass only if the suggestion fires, which depends on the floor being calibrated — if they fail here, that is expected; continue to Step 2 and re-check after Step 4.

- [ ] **Step 2: Collect the two score distributions**

```bash
python3 scripts/check_corpus.py --category unanswerable --scores
python3 scripts/check_corpus.py --category direct_fact --scores
```

Record the highest top-score among the three `unanswerable` questions, and the lowest among the 13 `direct_fact` questions. The floor belongs between them.

- [ ] **Step 3: Choose the floor**

Set it midway between the two figures from Step 2. If they overlap — an unanswerable question outscoring an answerable one — no threshold separates the sets. Do not pick a number anyway: record the overlap, leave the floor as it is, and report it. That result is a genuine finding about cosine similarity's limits and is the argument for gating on rerank position instead.

- [ ] **Step 4: Update the default and its comment**

In `services/chat-api/internal/config/config.go`, set `envFloat("KA_RELEVANCE_FLOOR", <chosen value>)` and replace the paragraph describing the value as calibrated from two data points with the measured ranges from Step 2, naming the corpus and question set it was derived from.

- [ ] **Step 5: Update the README**

In `README.md`, update the `KA_RELEVANCE_FLOOR` paragraph in the Embeddings section with the new value and the measured ranges.

- [ ] **Step 6: Re-run the full check and the Go suite**

```bash
python3 scripts/check_corpus.py --scores
go test ./...
```

Expected: 36/36 passed, including all three `cross_team` questions now that the floor separates the sets; all Go packages ok. If the sets overlapped at Step 3 and the floor was left unchanged, the `cross_team` questions will still fail — record that and report it rather than forcing a number.

- [ ] **Step 7: Commit**

```bash
git add services/chat-api/internal/config/config.go README.md
git commit -m "Calibrate the relevance floor against the corpus

Replaces an estimate derived from two data points with a value measured
from the answerable-versus-unanswerable split across the question set.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Notes for the executor

- **`make seed` requires the stack.** `make dev-up` first, and the Ollama model must be pulled (`make embed-pull`, which `seed` depends on).
- **Seeding only upserts.** It never removes chunks for documents deleted from `fixtures/`, so every check step should delete the index first: `curl -s -X DELETE http://localhost:9200/kb-chunks && make seed`. `make seed` forces an index refresh at the end; without one, a query issued immediately after seeding sees an empty index and reads as a retrieval failure.
- **Restart `chat-api` after reseeding only if it was started before the index existed.** It queries OpenSearch per request and does not cache. When restarting, kill by port — `go run` leaves a child that survives a `pkill` on the parent, and a stale server silently keeps the port while the new one dies on bind:
  ```bash
  lsof -t -nP -iTCP:8080 -sTCP:LISTEN | xargs -r kill
  ```
  Then confirm the new log has no `address already in use` before trusting any result.
- **`KA_LLM_MODE=mock` is fine throughout.** Every check in this plan reads the `retrieval` and `suggestion` events, not generated text.
- **Documents are fabricated, but internally consistent.** A job name invented in Task 3 must be spelled identically in Task 6. Grep before inventing a second name for the same thing.
