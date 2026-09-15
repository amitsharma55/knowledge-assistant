# Demo corpus — synthetic content

Every document in this directory is **fabricated**. The team names and
integration names are real; every field name, job name, schedule, volume,
error code, owner and metric in them is invented for demonstration and
testing.

These documents read convincingly. Do not treat any of them as
documentation, do not page anyone named in them, and do not copy a
schedule or endpoint out of them into anything real.

Hostnames and tenant identifiers are deliberate non-production
placeholders (`sf-demo.coupahost.com`, `sf-demo.icertis.com`,
`sf-demo.service-now.com`, `wd5-impl.workday.com/sf_demo`). Each document
carries a `url:` in YAML front-matter that becomes its citation link; those
links are placeholders too — do not open or trust them.

The corpus is ~150 documents in three tiers: the original hand-crafted gold
documents the `tests.jsonl` questions are written against; adversarial
distractors (near-twins, adjacent systems, cross-team lookalikes) that exist
to be ranked *below* gold so retrieval has to discriminate; and newer
question-first documents. Distractors add no rows to `tests.jsonl`. See
`docs/superpowers/specs/2026-09-02-demo-corpus-design.md` and
`docs/superpowers/specs/2026-09-14-corpus-scale-and-citations-design.md`.

The corpus is written against `tests.jsonl`, the question set it must be
able to answer.
