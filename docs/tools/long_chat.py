#!/usr/bin/env python3
"""Drive one long, scripted chat against a running Flint backend.

This is the end-to-end test behind docs/experiments.md: 19 turns with a
folder attached. It plants facts early (one in the protected first message,
one mid-chat), asks about files so the model uses the shell tool, then asks
for the facts back once the older messages have been summarized.

Commands the model proposes are approved only when they look read-only
(ls, cat, grep, wc, head, tail, find without -delete/-exec and with no
chaining or redirection); everything else is denied. Point it at a scratch
copy of a folder, never at a real project.

Usage:
    python3 docs/tools/long_chat.py FOLDER MODEL [BASE_URL]

It signs up a fresh throwaway account, so run it against a scratch
database (DB_PATH=/tmp/x.db), not your real one.
"""

import http.cookiejar
import json
import re
import sys
import time
import urllib.request
import uuid

FOLDER, MODEL = sys.argv[1], sys.argv[2]
BASE = sys.argv[3] if len(sys.argv) > 3 else "http://localhost:8080"

PROMPTS = [
    "Before we start: my name is Priya, the project codename is BLUEJAY, and the deadline is Friday the 3rd. Please keep answers short.",
    "List the Go files in this folder.",
    "Note for later, it matters: our staging server runs on port 9123, my manager is Tom Okafor, and we must never touch the payments table in production.",
    "What does shield.go do? Look at it.",
    "How many lines is handlers.go?",
    "What does ratelimit.go do and what are its limits?",
    "Look at preconditions.go and explain the check in two sentences.",
    "Which file defines the database schema? Check.",
    "What table stores attachments and what are its columns?",
    "grep for 'maxToolAttemptsPerTurn' and tell me where it is used.",
    "What does folder.go's buildAnchorHeader do?",
    "Look at websearch.go - which API does it call?",
    "What is the value of protectedWindow in context.go?",
    "Summarize recovery.go in one sentence.",
    "What port does main.go listen on by default?",
    "How are titles generated? Look at handlers.go.",
    "Without running any commands: what is my name, the project codename, and the deadline?",
    "Without running any commands: which file had the rate limiter and what limits did it use?",
    "Without running any commands: what port is staging on, who is my manager, and which table must we never touch?",
]

SAFE = ("ls", "cat", "grep", "wc", "head", "tail", "find")
UNSAFE = ("-delete", "-exec", ">", "rm ", "mv ", ";", "&&", "|")

opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))


def req(path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    r = urllib.request.Request(BASE + path, data, {"Content-Type": "application/json"})
    return opener.open(r, timeout=600).read().decode()


def looks_read_only(cmd):
    words = cmd.split()
    return bool(words) and words[0] in SAFE and not any(x in cmd for x in UNSAFE)


email = f"longchat-{uuid.uuid4().hex[:8]}@example.test"
req("/api/signup", {"full_name": "Long Chat", "email": email, "password": "password123"})
req("/api/login", {"email": email, "password": "password123"})
cid = json.loads(req("/api/conversations", {"model": MODEL}))["id"]
req(f"/api/conversations/{cid}/attach", {"folder": FOLDER})

tool_calls, peak, overflows = [], 0, 0
for i, prompt in enumerate(PROMPTS):
    start = time.time()
    out = req(f"/api/conversations/{cid}/messages", {"content": prompt})
    calls = 0
    transcript = out
    # A turn allows at most 3 tool cycles (maxToolAttemptsPerTurn); 4 is a safe bound.
    for _ in range(4):
        m = re.search(r"<<<TOOL_CALL>>>(\{.*\})", out)
        if not m:
            break
        c = json.loads(m.group(1))
        verdict = "approve" if looks_read_only(c["command"] or "") else "deny"
        print(f"    {verdict.upper()}: {c['command'][:100]}")
        calls += 1
        out = req(f"/api/conversations/{cid}/commands/{c['id']}/{verdict}", {})
        transcript += out
    for ctx in re.findall(r"<<<CONTEXT>>>(\{.*?\})", transcript):
        peak = max(peak, json.loads(ctx)["used"])
    if "exceeds the available context size" in transcript:
        overflows += 1
    tool_calls.append(calls)
    reply = re.sub(r"<<<[A-Z_]+>>>.*", "", out).strip()
    print(f"[{i}] ({time.time() - start:.1f}s) USER: {prompt}\n     BOT: {reply[:300]!r}")
    time.sleep(1)

print()
print("tool calls per turn:", " ".join(f"{i}:{n}" for i, n in enumerate(tool_calls)))
print("peak context used:", peak)
print("overflow errors:", overflows)
print("conversation:", cid)
