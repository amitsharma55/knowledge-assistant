# Retrieval eval golden set

`golden.json` is the labelled question set the eval harness (`cmd/eval`) scores
retrieval against. It is **not** `check_corpus.py`: that one asks "did the right
fact reach the model" (binary, 36 questions); this one measures ranked-retrieval
quality — recall@K, precision@K, MRR, NDCG@K — and reports them **per category**
so a near-twin regression and a literal-token lift stay separate numbers.

Run it:

```
go run ./cmd/eval                 # dense-only vs rerank-on, all categories
go run ./cmd/eval -k 5            # metrics at K=5
```

It needs the embed model reachable (same as any real-retrieval path — see the
embedder gotchas in the repo CLAUDE.md); rerank-on also needs the rerank model.

## Format

A JSON array of entries. Each entry:

| field      | meaning |
|------------|---------|
| `id`       | unique, kebab-case; prefix by category (`lt-`, `nt-`, `nm-`) by convention |
| `query`    | the question, as a user would type it |
| `team`     | `coupa` \| `star` \| `hr` — the team the query is scoped to |
| `category` | `literal_token` \| `near_twin` \| `normal` |
| `relevant` | one or more labels marking the chunks that should answer it |

A **label** keys on `pageId` (the fixture filename without `.md`) and optional
`section` (the literal `##` heading). It does **not** use the chunk's hash id:
those rehash whenever the chunker changes. Omit `section` to match any section of
the page (right for near-twins, where the page itself is the discriminator);
include it to pin one section (right for literal-token, where one section holds
the token). `grade` is the relevance weight for NDCG — use `2` for the section
that directly answers the query and `1` for a secondary section that also helps.

```json
{
  "id": "lt-example",
  "query": "...",
  "team": "coupa",
  "category": "literal_token",
  "relevant": [
    { "pageId": "some-fixture", "section": "Some Heading", "grade": 2 }
  ]
}
```

## Categories

- **`literal_token`** — the answer hinges on an exact token, code, or ID that a
  dense embedding tends to blur (e.g. `AVR-TIMEOUT`, a job name, an error code).
  This is where hybrid/BM25 is *expected* to help.
- **`near_twin`** — a sibling document shares most of the query's vocabulary and
  is the wrong answer (e.g. `louisiana-processing-rules` vs
  `texas-processing-rules`, identically-titled sections). This is where hybrid
  previously *regressed* — the number to watch.
- **`normal`** — neither; an ordinary paraphrasable question. The control group.

## TODO — expand to 50–100 entries

The 7 entries here are a seed that fixes the format, not a measurement set. 7
queries (2 + 2 + 3) are far too few to trust a per-category average.

- [ ] Grow each category to ~15–30 entries (keep the three roughly balanced).
- [ ] **near_twin**: mine `fixtures/star/*-processing-rules.md` and the
      `*-data-call-overview.md` set — every state pair with an identically-titled
      section is a candidate. Vary which state is the target.
- [ ] **literal_token**: pull exact codes/IDs from `fixtures/coupa` (error codes,
      job names like `snow-request-pull`, field names) and `fixtures/star`
      (error codes, NAIC/MCAS identifiers).
- [ ] **normal**: paraphrase questions whose answer lives in one clear section
      across all three teams.
- [ ] Label honestly — only sections you have read and confirmed answer the
      query. Do not guess grades.
