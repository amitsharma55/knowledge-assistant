#!/usr/bin/env python3
"""Pretty live view of the chat-api retrieval pipeline.

The server logs one JSON object per pipeline step (KA_TRACE=1 adds per-chunk and
prompt detail). Tailing that raw is unreadable; this follows the log and renders
each step as an aligned, coloured block.

    python3 scripts/trace.py                 # follows $TMPDIR/ka-dev/chat-api.log
    python3 scripts/trace.py path/to.log     # or an explicit file
    tail -f some.log | python3 scripts/trace.py -   # or read a pipe

It is a viewer, not an eval: it only formats what the server already logs.
"""
import json
import os
import sys
import time

C = {
    "q": "\033[1;36m", "hdr": "\033[1;33m", "dim": "\033[2m",
    "sel": "\033[32m", "back": "\033[33m", "warn": "\033[31m", "off": "\033[0m",
}
if not sys.stdout.isatty():
    C = {k: "" for k in C}


def doc(chunk_id):
    # ids are team:doc:hash; the doc slug is the human-readable part.
    parts = chunk_id.split(":")
    return parts[1] if len(parts) > 2 else chunk_id


def render(e, state):
    msg = e.get("msg", "")

    if msg == "chat query":
        print(f"\n{C['q']}{'━'*70}{C['off']}")
        print(f"{C['q']}QUERY{C['off']}  {C['dim']}[{e.get('team')}/{e.get('user')}]{C['off']}")
        print(f"  {C['q']}{e.get('q','')}{C['off']}")
        print(f"{C['q']}{'━'*70}{C['off']}")

    elif msg == "greeting; skipping retrieval":
        print(f"  {C['dim']}· greeting — no retrieval{C['off']}")

    elif msg == "rewrote query for retrieval":
        print(f"  {C['dim']}· rewrote: {e.get('asked')!r} → {e.get('searched')!r}{C['off']}")

    elif msg == "retrieved from KB":
        print(f"\n  {C['hdr']}RETRIEVED{C['off']} {C['dim']}(vector order, pool={e.get('pool')} topk={e.get('topk')}){C['off']}")

    elif msg in ("trace: retrieved", "trace: reranked"):
        if msg == "trace: reranked" and state.get("last") != "trace: reranked":
            print(f"\n  {C['hdr']}RERANKED{C['off']} {C['dim']}(order sent to selection){C['off']}")
        print(f"    #{e.get('rank',0):>2}  {e.get('score',0):.3f}  "
              f"{doc(e.get('id','')):<32}  {C['dim']}{e.get('section','')}{C['off']}")

    elif msg == "below relevance floor; probing other teams":
        print(f"  {C['warn']}· below relevance floor ({e.get('floor')}){C['off']}")

    elif msg == "context assembled; streaming answer":
        print(f"\n  {C['hdr']}CONTEXT{C['off']}  "
              f"retrieved={e.get('retrieved')}  "
              f"{C['sel']}selected={e.get('selected')}{C['off']}  "
              f"{C['back']}backfilled={e.get('backfilled')}{C['off']}  "
              f"used={e.get('used')}")

    elif msg == "trace: prompt":
        for label, key in (("SYSTEM PROMPT", "system"), ("USER PROMPT (context + question)", "user")):
            print(f"\n  {C['hdr']}{label}{C['off']}")
            for line in e.get(key, "").splitlines():
                print(f"  {C['dim']}│{C['off']} {line}")

    elif e.get("level") in ("WARN", "ERROR"):
        print(f"  {C['warn']}· {e.get('level')}: {msg} {e.get('err','')}{C['off']}")
    else:
        return  # startup/other noise
    state["last"] = msg


def follow(f, state):
    f.seek(0, os.SEEK_END)  # only new queries, not the backlog
    while True:
        line = f.readline()
        if not line:
            time.sleep(0.2)
            continue
        feed(line, state)


def feed(line, state):
    line = line.strip()
    if not line:
        return
    try:
        render(json.loads(line), state)
    except (json.JSONDecodeError, KeyError):
        pass  # non-JSON (e.g. make/go build output) — skip


def main():
    arg = sys.argv[1] if len(sys.argv) > 1 else os.path.join(
        os.environ.get("TMPDIR", "/tmp"), "ka-dev", "chat-api.log")
    state = {}
    try:
        if arg == "-":
            for line in sys.stdin:
                feed(line, state)
        else:
            with open(arg) as f:
                follow(f, state)
    except KeyboardInterrupt:
        pass
    except FileNotFoundError:
        sys.exit(f"no log at {arg} — is chat-api running? (start it with /run-local)")


if __name__ == "__main__":
    main()
