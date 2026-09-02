# Demo Corpus and Evaluation Question Set

Date: 2026-09-02
Status: Approved for planning

## Problem

The assistant has working retrieval but almost nothing to retrieve from. The
corpus is five documents totalling 155 lines — roughly a dozen chunks, against
a `TopK` of 8. Retrieval returns most of the corpus for every query, so it can
neither demonstrate retrieval nor fail in an instructive way.

Worse, four of the five documents describe systems that do not exist. Comparing
them against the real estate:

| Repo today | Reality |
|---|---|
| `fixtures/star/avr-integration.md` — "Automated Vulnerability Response", security scanners, 13 Lambdas | AVR is a **Coupa AWS Middleware** integration serving invoice, PO and receipt data |
| `fixtures/coupa/servicenow-integration.md` — incidents and change requests | ServiceNow moves **product items** Coupa→ServiceNow and **request data** ServiceNow→Coupa |
| `fixtures/star/salesforce-integration.md` — CRM account/opportunity sync | Star owns **state regulatory data calls**; there is no Salesforce integration |

Only the Icertis document is roughly right in shape, and it omits the
resync-confirm step that is central to how that integration actually works.

The corpus is also the blocking dependency for three other pieces of work: the
evaluation harness has nothing to measure, `KA_RELEVANCE_FLOOR` rests on two
data points, and neither reranking nor LLM-based chunking can be shown to help
without a corpus where retrieval can fail.

## Goals

- A corpus that survives interrogation by the engineers who own these systems.
- Retrieval failures that are *possible*, so that fixing them is demonstrable.
- A question set that predates the documents, so the corpus is not written to
  flatter it.
- A measured relevance floor, replacing the current estimate.

## Non-Goals

Explicitly out of scope, each its own later task:

- The evaluation harness (metrics, LLM judge, dashboard).
- LLM-based semantic chunking.
- Reranking and query rewriting.
- The SharePoint crawler. Documents live in `fixtures/` for the demo; S3
  remains the production seam and nothing downstream changes when the crawler
  arrives.

## Decisions

Settled during brainstorming; recorded so the plan does not relitigate them.

1. **Mirror real systems, fabricate all internals.** Real team and integration
   names; every field name, job name, schedule, volume, error code and owner is
   invented. This is what makes the corpus land with an internal audience and
   what makes retrieval exercise the vocabulary the real system will see.
2. **Company is State Farm Insurance.** Hostnames and tenant identifiers stay
   obvious non-production placeholders. `fixtures/README.md` marks the corpus
   synthetic — these documents read convincingly enough that someone will
   eventually find a job name and try to page its owner.
3. **Replace the existing fixtures rather than extend them.** Four of five
   misdescribe real systems; leaving them risks a demo question retrieving a
   confident document about a system nobody owns.
4. **Depth over breadth**, weighted to Coupa AWS Middleware. The first audience
   is the engineering team that owns these integrations; the State Farm
   audience follows. A corpus that survives the first comfortably survives the
   second, not the reverse.
5. **Documents live in `fixtures/`, not a new `data/` directory.** The indexer
   already reads it and `make seed` already loops the three team directories.
   A second convention for the same thing buys nothing.
6. **Questions are written first**, reviewed, and only then are documents
   written against them.

## Corpus

23 documents, roughly 3,500–4,500 lines.

### Coupa AWS Middleware (12 documents)

ServiceNow is two integrations in opposite directions and is documented as
such. The separation is itself a retrieval test: "ServiceNow fields" is
ambiguous until the direction is known.

- `servicenow-product-sync.md` — Coupa → ServiceNow product/catalog items;
  overview and field mapping
- `servicenow-request-sync.md` — ServiceNow → Coupa request data for purchased
  product
- `servicenow-runbook.md` — failure modes, error codes, replay procedure

Icertis, including the resync-confirm leg missing from today's document:

- `icertis-contract-overview.md` — fetch from Icertis, load via Coupa API,
  resync back to Icertis to confirm the load
- `icertis-field-mapping.md`
- `icertis-resync-runbook.md` — the confirm-back leg is where this realistically
  breaks, so the error codes live here

AVR is a protocol facade: the on-prem application exposes SOAP, the AWS
middleware consumes it and serves REST/JSON to Coupa, synchronously, when Coupa
calls. It is the most technically distinctive integration in the estate and the
one engineers will probe hardest.

- `avr-soap-service.md` — the on-prem service, WSDL operations, PeopleSoft and
  expense reports as the data behind it
- `avr-rest-facade.md` — the REST/JSON API Coupa calls: endpoints, auth, the
  synchronous pull model
- `avr-field-mapping.md` — SOAP element to JSON field; the exact-string
  retrieval test
- `avr-runbook.md` — SOAP faults, on-prem connectivity loss, timeouts

Cross-cutting:

- `coupa-middleware-architecture.md` — how the three sit together on AWS
- `coupa-oncall-escalation.md` — ownership, escalation paths, on-call

### Star (7 documents)

Louisiana and Texas share the pipeline and diverge in processing logic, because
state law and regulation differ. The shared mechanics therefore belong in one
document and the divergence in per-state documents.

- `data-call-pipeline.md` — Redshift and mainframe extract, S3 parquet, SQL
  execution, QuickSight publish
- `louisiana-data-call-overview.md`, `louisiana-processing-rules.md`,
  `louisiana-report-catalog.md`
- `texas-data-call-overview.md`, `texas-processing-rules.md`,
  `texas-report-catalog.md`

This split makes "how does the Texas data call reach QuickSight" a spanning
question — mechanics in one document, Texas specifics in another — which is
how these documents would genuinely be written anyway.

### HR (4 documents)

- `workday-org-structure.md`, `workday-compensation.md`,
  `workday-payroll-integration.md`, `leave-policy.md`

HR is deliberately light. Its role is to be the *other* team, so that a Coupa
engineer asking about compensation triggers the cross-team suggestion.

### Engineered properties

Four properties are designed in rather than hoped for:

1. **Near-twin pair.** Louisiana and Texas share structure and pipeline but
   differ in regulatory processing rules. A wrong answer here is genuinely
   dangerous — a Louisiana aggregation rule returned for a Texas question is a
   mis-report to a regulator — which makes it the most persuasive failure to
   demonstrate avoiding.
2. **Spanning facts.** Pipeline + state; AVR SOAP service + field mapping;
   Icertis overview + resync runbook.
3. **Cross-team near-miss.** Compensation questions asked under Coupa,
   answerable only under HR.
4. **A real gap.** One plausible question the corpus deliberately does not
   answer, so the honest refusal is demonstrable.

## Question set

`fixtures/tests.jsonl`, following the reference implementation's schema with
two additions:

```json
{"question": "...", "team": "star", "keywords": ["..."],
 "anti_keywords": ["..."], "reference_answer": "...", "category": "near_twin"}
```

`team` exists because every query is scoped. The harness must exercise the real
scoped path, including asserting that a Coupa question retrieves nothing under
HR.

`anti_keywords` lists terms that must **not** appear in the retrieved context.
MRR and nDCG measure only whether the right chunk was retrieved; they are
silent on the wrong chunk being retrieved beside it. For the near-twin pair
that silence covers the entire risk, so a Texas question carries
`"anti_keywords": ["Louisiana", "LDI"]` and fails if the other state's rules
appear at all.

Approximately 30 questions:

| Category | Count | What it proves |
|---|---|---|
| `direct_fact` | 10 | Baseline retrieval; roughly one per major document |
| `spanning` | 6 | Answer requires two documents |
| `near_twin` | 6 | Louisiana/Texas precision; three matched pairs, all with `anti_keywords` |
| `cross_team` | 3 | Answerable only under another team; drives the suggestion |
| `unanswerable` | 3 | Honest refusal; also the negative set for floor calibration |
| `temporal` | 2 | Changelog-dated facts |

The three unanswerable questions do double duty as the negative set that lets
`KA_RELEVANCE_FLOOR` be derived from data.

## Writing conventions

Documents are split by `chunker.Split(md, 800, 100)`: top-level headings first,
then ~800-word windows with 100 words of overlap. Two consequences shape how
these documents are written.

**Each `##` section is self-contained and under ~800 words**, so it becomes
exactly one chunk — a whole answer to one question. Sections that overflow are
window-split, and the second window arrives without the heading context that
made the first interpretable.

**Facts that must be retrievable are written as bullet lists, not tables.**
`Split` reassembles text with `strings.Join(words, " ")`, destroying newlines,
so a markdown table becomes one run of `| field | type | | field | type |`.
Row boundaries blur, and an overflowing section loses the header row entirely
from the remainder. Tables remain appropriate for scannable overviews a human
reads; anything a question must retrieve is a bullet list. The existing Icertis
"Fields sent to Coupa" section already works this way, which is likely why it
retrieved at 0.9031 in manual testing.

This is designing around a chunker weakness, not fixing it. LLM-based chunking
would remove the constraint; the corpus should not block on that work.

Register follows the existing Icertis document: a short prose introduction,
then concrete `##` sections with real-looking identifiers. Regulatory
vocabulary (LDI, TDI, statistical reporting) is used where genuine, since that
is what makes the corpus read as real to the intended audience.

## Code changes

All small:

- Display name for `coupa` becomes "Coupa AWS Middleware" in
  `team.DefaultInfos()`. The slug stays `coupa`: it appears in URLs, the index,
  and the demo membership table, and the display name is what the UI shows.
- Delete the five existing fixture documents.
- Add `fixtures/README.md` marking the corpus synthetic.

`make seed` already loops the three team directories; no Makefile change.

## Sequencing

1. Draft ~30 questions with reference answers. **Review gate** — nothing else
   starts until the question set is settled. This is also where the team's
   knowledge of what people actually ask enters the design.
2. Write the 23 documents against those questions.
3. Reseed and verify retrieval per question by hand.
4. Derive `KA_RELEVANCE_FLOOR` from the answerable-versus-unanswerable split,
   replacing the current estimate and the note in `config.go` that describes it
   as calibrated from two data points.
5. Hand off to the evaluation harness as separate work.

Steps 1 and 2 are the bulk of the effort.

## Consequences

- The demo gains a corpus where retrieval can fail, which is what makes
  reranking, LLM chunking and the eval harness worth building and measurable
  once built.
- `KA_RELEVANCE_FLOOR` becomes a measured value rather than an estimate.
- The corpus is synthetic content describing real systems. It must stay marked
  as such, and it will drift from reality as the real integrations change.
  It is demo and test material, not documentation.
