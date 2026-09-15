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
            if t["category"] != "cross_team":
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
    # Score the context the model actually receives, not the raw candidate
    # pool. The orchestrator emits every retrieved chunk marked used /
    # dropped; only used chunks (selected + backfilled) reach the prompt. A
    # near-twin the reranker correctly dropped (e.g. the Texas chunk under a
    # Louisiana query) is still in the pool, and scanning it would fail the
    # anti_keyword check for content the model never sees. Fall back to the
    # whole pool if no chunk carries the flag (older API).
    used = [c for c in chunks if c.get("used")]
    scored = used if used else chunks
    haystack = "\n".join(c["text"] for c in scored).lower()
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
