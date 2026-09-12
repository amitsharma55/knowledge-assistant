---
name: run-local
description: Start the knowledge-assistant local stack (Docker services, fixture index, chat-api on :8080, UI on :5173) detached so it outlives the Claude session, verifying each piece. Use when asked to run, start, or restart the app locally, or before any task that needs chat-api or the UI running.
---

Run from the repo root. Each step has a check; skip the step if its check already passes, and report which steps ran versus were already up.

Logs go to `${TMPDIR:-/tmp}/ka-dev/` — `mkdir -p` it first.

1. **Config.** `.env` must exist. If it doesn't, run `cp .env.example .env` and stop: ask the user to fill in `ANTHROPIC_API_KEY`. Never print `.env`; to read one setting use `grep -E '^KA_LLM_MODE=' .env`.
2. **Docker.** Check with `docker info >/dev/null 2>&1`. If it fails, run `open -a Docker` and poll that check until it passes.
3. **Services.** Check that `curl -s -o /dev/null -w '%{http_code}' localhost:9200` returns 200. If not, run `make dev-up` and poll the check.
4. **Index.** Check with `curl -s localhost:9200/kb-chunks/_count`. If the index is missing or the count is 0, run `make seed`. The first run pulls the ~270MB embedding model.
5. **chat-api (:8080).** Check with `lsof -nP -iTCP:8080 -sTCP:LISTEN`. If nothing is listening, run:
   `nohup make run-api > "${TMPDIR:-/tmp}/ka-dev/chat-api.log" 2>&1 < /dev/null & disown`
   then poll `curl -sf localhost:8080/healthz`.
6. **UI (:5173).** Check the same way on 5173. If nothing is listening, run `npm --prefix ui install` (only if `ui/node_modules` is missing), then:
   `nohup npm --prefix ui run dev > "${TMPDIR:-/tmp}/ka-dev/ui.log" 2>&1 < /dev/null & disown`
   then poll `curl -s -o /dev/null -w '%{http_code}' localhost:5173/` until it returns 200.
7. If any poll runs past ~60s, show the last 30 lines of that step's log instead of retrying blindly.

Finish by printing the URLs (UI http://localhost:5173, API http://localhost:8080) and the log paths.

Notes:
- If `grep -E '^KA_(RERANK|REWRITE)_MODE=ollama' .env` matches, rerank/rewrite need the native Ollama.app on 11434 with `gpt-oss:20b`. Check `curl -s localhost:11434/api/tags` and tell the user if the model is missing; don't start or pull it yourself.
- To stop: `lsof -ti:8080 | xargs kill`, and the same for 5173. Don't run `make dev-down` unless asked; it deletes the Docker volumes.
- These processes are detached from the Claude session, but the user's own terminal is still the most durable place for long-running servers. Say so if they plan to keep the stack up all day.
