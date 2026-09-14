# Scaling the Demo Corpus and Making Citations Demo-Real

Date: 2026-09-14
Status: Approved for planning

## Problem

The corpus is 23 hand-crafted documents answering the 36 questions in
`fixtures/tests.jsonl`. That corpus did its first job — it replaced five wrong
documents with material engineers can interrogate — but it is too small to
demonstrate *ranking*. Against a `TopK` of 8, a query pulls back a meaningful
fraction of the whole corpus, so almost anything relevant is retrieved and
nothing has to be ranked *above* a plausible competitor. `KA_RELEVANCE_FLOOR`
(0.81) therefore rests on a corpus where nearly everything clears the bar.

Separately, retrieved documents already surface to the user as citations —
the `retrieval` and `citation` SSE events, `ui/src/citations.js`, the `[n]`
markers and the source rail all work today — but every fixture document is
indexed with `URL: file://<path>` (`services/ingestion/cmd/indexer/main.go`
`loadFixtures`). Citations render and link, but the links point at local file
paths and read as fake in a demo.

## Goals

- A corpus large enough (~150 documents) that retrieval must *discriminate*:
  the right document has to out-rank plausible near-misses, not merely appear.
- Preserve the existing eval as ground truth — the 36 question-first gold
  questions stay the measured contract, uncontaminated by generated content.
- Give every document a real-looking, system-native source link so the
  citation UI is demo-ready.
- A re-measured `KA_RELEVANCE_FLOOR`, calibrated under the new retrieval
  pressure rather than against a corpus where everything clears it.

## Non-Goals

- A metrics eval harness (MRR / nDCG / LLM judge). `scripts/check_corpus.py`
  stays keyword-presence-plus-scores; a real metric harness is a later task.
- Reranking / query-rewrite quality work. This corpus makes such work
  *measurable*; it does not do it.
- Changing the citation UI, the SSE contract, or the `Chunk`/`index.Doc`
  schema. `URL`, `PageTitle`, `PageID`, `SectionPath` already carry everything
  citations need.
- The S3 / crawler path. Fixtures remain the demo source; nothing here changes
  the S3 seam.

## Decisions

Settled during brainstorming; recorded so the plan does not relitigate them.

1. **Grow to demonstrate ranking, not coverage.** The reason to reach ~150 is
   retrieval *pressure* — plausible competition the retriever must rank down —
   not more facts. Volume of topically-unrelated documents would leave ranking
   just as easy as it is now.
2. **The 23 gold documents and 36 questions are frozen.** They are the honest,
   question-first measured core. Generated content is added *around* them and
   is never allowed to become the answer to a gold question.
3. **Three tiers, not 150 uniform documents.** Gold (frozen), adversarial
   distractors (bulk, LLM-generated), and a small slice of genuinely new
   content-plus-questions. Only the last tier carries the "survives
   interrogation" review cost, so it stays small.
4. **Distractors are LLM-generated with sampled review.** They must be
   plausible, not correct; hand-reviewing ~90 documents is not warranted.
   Correctness is enforced by the retrieval guardrail (below), not by reading
   each one.
5. **Links are system-native, not a corporate wiki.** Each document links to
   the system it describes — a ServiceNow, Coupa, Icertis or Workday portal —
   using obvious non-production placeholder hosts. No State Farm / corporate
   Confluence link.
6. **Links ride front-matter, parsed in `loadFixtures`.** A `url:` (and
   optional `updated:`) key in each document's YAML front-matter, read where
   `WebURL` is currently synthesized. Title already prefers the H1 via
   `chunker.Title`, so no title change is needed.

## Corpus Design

Target ~150 documents in the existing `fixtures/{coupa,star,hr,<new>}/` layout.

### Tier 1 — Gold (23 documents, frozen)

The current corpus, unchanged, plus front-matter `url:` added to each (a
metadata edit, not a content edit). `fixtures/tests.jsonl` is untouched. These
remain the documents the 36 gold questions must retrieve.

### Tier 2 — Adversarial distractors (~90 documents, generated)

Documents whose sole purpose is to be ranked *below* gold for gold queries.
Three kinds, weighted toward the categories where ranking actually decides:

- **Near-twins** — reuse gold vocabulary (`AVR-TIMEOUT`, `icertis_contract_id`,
  `snow-product-push`, error codes, job names) in a *different, wrong* context.
  These are the hardest distractors and the point of the exercise; they map to
  the existing `near_twin` test category.
- **Adjacent systems** — plausible sibling integrations that do not exist in
  gold (other Coupa middleware flows, other Star data-call reports, other HR
  Workday areas), sharing house style and some vocabulary.
- **Cross-team lookalikes** — documents on one team that resemble another
  team's material, to keep per-team scoping honest at scale.

Distractors add no rows to `tests.jsonl`. Their value is measured indirectly:
gold questions must still retrieve gold *above the floor* with distractors
present.

### Tier 3 — New content + questions (~35 documents, reviewed)

Genuinely new systems *within the existing coupa / star / hr teams* — no new
team is introduced — written question-first exactly like the original corpus:
questions drafted and reviewed first, then documents written to answer them,
then their rows appended to `tests.jsonl`. This is the only tier that carries
the interrogation-review bar, which is why it is small. It buys demo breadth
and a fresh, honestly-measured slice of gold.

## Links / Citations

### Data flow (already built — do not rebuild)

`index.Doc.URL` → OpenSearch `url` keyword → `rag.Chunk.URL` → `retrieval` and
`citation` SSE events → `ui/src/citations.js` groups by document → source rail
+ `[n]` markers. The only weak link is where `URL` is *set*.

### The one change

In `services/ingestion/cmd/indexer/main.go` `loadFixtures`, parse optional YAML
front-matter delimited by `---` at the top of each `.md` file:

```
---
url: https://sf-demo.service-now.com/kb_view.do?sysparm_article=KB0012345
updated: 2026-08-30
---
# AVR Runbook
...
```

- `url:` populates `page.WebURL` instead of the current `file://` fallback.
- `updated:` (optional) populates `page.UpdatedAt`; absent, keep `time.Now()`.
- The front-matter block is **stripped from `Markdown`** before chunking, so it
  never enters chunk text, `EmbedText`, or `chunker.Title` (which must still see
  the H1 as the first line).
- No front-matter → today's behavior (`file://` URL, `time.Now()`), so the S3
  and GitLab sources and any un-migrated fixture are unaffected.

Host convention (placeholders, non-production): ServiceNow
`sf-demo.service-now.com`, Coupa `sf-demo.coupahost.com`, Icertis
`sf-demo.icertis.com`, Workday `wd5-impl.workday.com/sf_demo`. `fixtures/README.md`
already marks the corpus synthetic; it gains a line noting the links are
placeholders too.

## Generation Process

1. **Seed prompts per distractor kind.** A small set of generation prompts that
   take a gold document (or a system description) and produce a near-twin /
   adjacent / cross-team distractor in house style, with front-matter `url:`.
2. **Generate in batches by team**, writing `.md` files into the team
   directories. Filenames are descriptive slugs (the citation title comes from
   the H1, the id from the slug).
3. **Sampled human review** — spot-check a sample per batch for house style,
   placeholder-only hostnames, and no accidentally-real data. Not a full read.
4. **Guardrail gate (mandatory, automated).** After `make seed`, run
   `scripts/check_corpus.py --scores`:
   - every gold question still retrieves its gold document, above
     `KA_RELEVANCE_FLOOR`;
   - no `anti_keywords` regressions;
   - no distractor satisfies a gold question's `keywords` — a distractor that
     becomes a *true positive* silently corrupts the eval and must be edited or
     removed.
   A batch that fails the gate is fixed or dropped before the next batch.
5. **Recalibrate the floor.** With the full corpus seeded, use
   `check_corpus.py --scores` to inspect gold-vs-distractor separation and
   re-set `KA_RELEVANCE_FLOOR` (and the `deploy/k8s/base/chat-api.yaml`
   manifest value) to sit in the gap.

## Validation

- **Ranking is demonstrable:** with distractors present, at least the
  `near_twin` gold questions show the gold chunk out-scoring its twin — visible
  in `--scores` output. (Pre-expansion, twins barely exist to out-rank.)
- **Eval integrity:** `check_corpus.py` (schema validation + keyword presence)
  stays green on all 36 + new Tier-3 questions.
- **Links:** a live query in the UI shows citations whose links are
  system-native placeholder URLs, not `file://` paths.
- **Reseed discipline:** every corpus change goes through `make seed` (drops
  the index first) per the doc-id-hash gotcha; front-matter changes count.
- **Go tests:** `loadFixtures` front-matter parsing gets unit coverage
  (front-matter present / absent / malformed; block stripped from body; H1 and
  `chunker.Title` still correct).

## File Layout

- `fixtures/{coupa,star,hr}/*.md` — Tier 1 gains front-matter; Tier 2 adds
  files here.
- `fixtures/tests.jsonl` — appended with Tier-3 questions only (Tier 3 lives in
  the existing coupa/star/hr directories; no new team).
- `services/ingestion/cmd/indexer/main.go` — front-matter parsing in
  `loadFixtures` (+ a small helper, unit-tested).
- `deploy/k8s/base/chat-api.yaml` — recalibrated `KA_RELEVANCE_FLOOR`.
- `fixtures/README.md` — placeholder-links note.

## Risks

- **A distractor answers a gold question.** Primary correctness risk;
  neutralized by guardrail step 4 running after every batch, not just at the
  end.
- **Generated homogeneity.** LLM distractors can converge on one voice, making
  them easy to rank apart. Mitigated by varying seed prompts and by sampled
  review; the near-twin requirement (reusing exact gold vocabulary) works
  against homogeneity.
- **Floor over-fit.** Recalibrating to the generated corpus could tune the
  floor to synthetic noise. Mitigated by setting it from gold-vs-distractor
  *separation* on the frozen gold questions, not from distractor scores.
