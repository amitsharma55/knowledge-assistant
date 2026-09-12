---
name: check-retrieval
description: Measure retrieval on the fixture corpus after a change to chunking, embedding, indexing, reranking, rewriting or retrieval. Reseeds if needed, runs scripts/check_corpus.py against chat-api in mock-LLM mode, and diffs per-question results against the previous run. Use after changes under internal/chunker, internal/embed, internal/index, internal/ingest, internal/rag, internal/rerank, internal/rewrite or services/chat-api/internal/opensearch.
---

`$ARGUMENTS` is passed through to `check_corpus.py`, e.g. `--team coupa`, `--category near_twin`, `--id coupa-snow-01`.

1. **Stack.** OpenSearch (9200) and the embedding Ollama must be up. If they aren't, follow `/run-local` steps 1–4.
2. **Reseed only when what gets indexed changed:** the chunker, `EmbedText`, the embedder, the index mapping, or `internal/ingest`. In that case run `make seed`. Changes to retrieval, rerank or rewrite alone don't need it.
3. **Restart chat-api in mock-LLM mode.** This compiles the new code without spending Claude tokens: the script reads each reply stream to the end, so with a real LLM every question pays for an answer. `make run-api` can't do this because `.env` overrides `KA_LLM_MODE`. Instead:
   ```sh
   lsof -ti:8080 | xargs kill 2>/dev/null
   mkdir -p "${TMPDIR:-/tmp}/ka-dev"
   (set -a; . ./.env; set +a; KA_LLM_MODE=mock nohup go run ./services/chat-api/cmd/server \
     > "${TMPDIR:-/tmp}/ka-dev/chat-api-mock.log" 2>&1 < /dev/null &)
   ```
   Poll `curl -sf localhost:8080/healthz` for up to ~60s; on timeout, show the log tail. Rerank and rewrite still run as configured in `.env`; only the answer is mocked.
4. **Run and save.** `.data/` is gitignored.
   ```sh
   mkdir -p .data/eval
   OUT=".data/eval/$(date +%Y%m%d-%H%M%S)-$(git rev-parse --short HEAD)$(git diff --quiet || echo -dirty).txt"
   echo "# args: $ARGUMENTS" > "$OUT"
   python3 scripts/check_corpus.py --scores $ARGUMENTS | tee -a "$OUT"
   ```
5. **Compare** with the most recent earlier file in `.data/eval/` whose `# args:` line matches. Diff the status lines, extracted with `grep -oE '^\[(PASS|FAIL)\] +[^ ]+'`. Report:
   - questions that flipped,
   - pass totals, before and after,
   - any `top=` score that moved by more than 0.02 on a question whose status didn't change.

   If there's no earlier matching run, say this run is the baseline. Offer to measure the pre-change code for a before/after, and ask before stashing or checking anything out.
6. **Restore** the normal chat-api: kill the mock server on 8080, then run `/run-local` step 5.

Reading the result:
- There are 36 questions, so a 1–2 question difference is noise, not a finding. Say so when that's all that changed.
- `top=` is on the `(1+cos)/2` scale and depends on the embedding backend. Scores from runs on different embedders aren't comparable.
- Never edit `fixtures/tests.jsonl` or the fixtures to make a check pass.
