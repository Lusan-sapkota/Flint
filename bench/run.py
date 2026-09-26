#!/usr/bin/env python3
"""Flint's scored benchmark: fixed tasks with automatic pass/fail checks.

Each configuration runs on its own fresh Flint server and database, built
from this checkout, with some scaffolding switched off through
FLINT_ABLATE (see backend/ablate.go). Comparing configurations shows what
each piece of scaffolding is actually worth on a given model.

Usage (Ollama must be running, with the model pulled):
    python3 bench/run.py                          # every config, once
    python3 bench/run.py --configs full,bare --runs 3
    python3 bench/run.py --tasks port,recall-mid  # a subset

Results: a table on stdout and bench/results/<timestamp>.json with every
reply and command, for looking at why a task failed.

Proposed commands are approved only when they look read-only (or run one of
the fixture's own scripts) and denied otherwise. The fixture is copied to a
temporary folder for every task, so nothing outside it is touched.
"""

import argparse
import http.cookiejar
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

from tasks import TASKS

ROOT = Path(__file__).resolve().parent.parent
FIXTURE = Path(__file__).resolve().parent / "fixture"

CONFIGS = {
    "full": "",
    "no-nudge": "nudge",
    "no-anchor": "anchor",
    "no-summaries": "summaries",
    "no-fit": "fit",
    "no-preconditions": "preconditions",
    "bare": "nudge,anchor,summaries,fit,preconditions",
}

SAFE = {"ls", "cat", "head", "tail", "wc", "grep", "find", "sort", "uniq", "cut"}
FIXTURE_SCRIPTS = {"check.py", "fail.py"}
UNSAFE = re.compile(r"[;&><`]|\$\(|-delete|-exec|\brm\b|\bmv\b")


def looks_safe(command):
    """Approve read-only pipelines and the fixture's own scripts only."""
    if not command or UNSAFE.search(command):
        return False
    for part in command.split("|"):
        words = part.split()
        if not words:
            return False
        if words[0] in ("python3", "python") and len(words) == 2 and Path(words[1]).name in FIXTURE_SCRIPTS:
            continue
        if words[0] not in SAFE:
            return False
    return True


class Client:
    def __init__(self, base):
        self.base = base
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def req(self, path, body=None, method=None):
        data = json.dumps(body).encode() if body is not None else None
        r = urllib.request.Request(self.base + path, data, {"Content-Type": "application/json"}, method=method)
        try:
            return self.opener.open(r, timeout=900).read().decode()
        except urllib.error.HTTPError as e:
            return f"[error: HTTP {e.code} {e.read().decode()}]"


def run_turn(client, cid, text, turn, record, deny_first=False):
    """Send one message, drive its tool loop to the end, return the reply."""
    out = client.req(f"/api/conversations/{cid}/messages", {"content": text})
    transcript = out
    # A turn allows at most 3 tool cycles (maxToolAttemptsPerTurn); 4 is a safe bound.
    for _ in range(4):
        m = re.search(r"<<<TOOL_CALL>>>(\{.*\})", out)
        if not m:
            break
        call = json.loads(m.group(1))
        verdict = "approve" if looks_safe(call["command"]) else "deny"
        if deny_first and not record["commands"]:
            verdict = "deny"
        record["commands"].append({"turn": turn, "command": call["command"], "verdict": verdict})
        out = client.req(f"/api/conversations/{cid}/commands/{call['id']}/{verdict}", {})
        transcript += out
    record["blocked"] += re.findall(r"\[Blocked a proposed command: ([^\]]*)\]", transcript)
    record["precondition_failed"] += re.findall(r"\[Precondition failed: ([^\]]*)\]", transcript)
    for ctx in re.findall(r"<<<CONTEXT>>>(\{.*?\})", transcript):
        record["peak_context"] = max(record["peak_context"], json.loads(ctx)["used"])
    if "[error:" in transcript:
        record["errors"].append(turn)
    reply = re.sub(r"<<<[A-Z_]+>>>.*", "", out).strip()
    record["replies"].append(reply)
    return reply


def run_task(client, model, task):
    record = {"replies": [], "commands": [], "blocked": [], "precondition_failed": [], "errors": [], "peak_context": 0}
    cid = json.loads(client.req("/api/conversations", {"model": model}))["id"]
    workdir = None
    if task.get("folder", True):
        workdir = tempfile.mkdtemp(prefix="flint-bench-")
        shutil.copytree(FIXTURE, workdir, dirs_exist_ok=True)
        client.req(f"/api/conversations/{cid}/attach", {"folder": workdir})
    start = time.time()
    for i, text in enumerate(task["turns"]):
        run_turn(client, cid, text, i, record, task.get("deny_first", False))
        time.sleep(1)
    record["seconds"] = round(time.time() - start, 1)
    ok, why = task["check"](record)
    record["pass"], record["why"] = ok, why
    if workdir:
        shutil.rmtree(workdir, ignore_errors=True)
    return record


def start_server(binary, port, ablate, workdir):
    env = dict(os.environ, PORT=str(port), DB_PATH=str(workdir / "bench.db"),
               ATTACHMENTS_DIR=str(workdir / "att"), FLINT_ABLATE=ablate)
    log = open(workdir / "server.log", "w")
    proc = subprocess.Popen([binary], cwd=ROOT / "backend", env=env, stdout=log, stderr=log)
    for _ in range(50):
        try:
            urllib.request.urlopen(f"http://localhost:{port}/healthz", timeout=2)
            return proc
        except urllib.error.HTTPError:
            return proc  # up, even if Ollama is reported unhealthy
        except OSError:
            time.sleep(0.2)
    proc.kill()
    sys.exit(f"server for {ablate or 'full'} did not start; see {workdir / 'server.log'}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", default="qwen2.5-3b-instruct:latest")
    ap.add_argument("--configs", default=",".join(CONFIGS))
    ap.add_argument("--runs", type=int, default=1)
    ap.add_argument("--tasks", default="")
    ap.add_argument("--port", type=int, default=8190)
    args = ap.parse_args()

    configs = args.configs.split(",")
    for c in configs:
        if c not in CONFIGS:
            sys.exit(f"unknown config {c!r}; known: {', '.join(CONFIGS)}")
    tasks = [t for t in TASKS if not args.tasks or t["id"] in args.tasks.split(",")]

    tmp = Path(tempfile.mkdtemp(prefix="flint-bench-srv-"))
    binary = tmp / "flint"
    subprocess.run(["go", "build", "-o", str(binary), "."], cwd=ROOT / "backend", check=True)

    results = {"model": args.model, "runs": args.runs, "started": time.strftime("%Y-%m-%d %H:%M"), "configs": {}}
    out_dir = Path(__file__).resolve().parent / "results"
    out_dir.mkdir(exist_ok=True)
    out = out_dir / (time.strftime("%Y%m%d-%H%M%S") + ".json")
    for ci, config in enumerate(configs):
        workdir = tmp / config
        workdir.mkdir()
        port = args.port + ci
        proc = start_server(binary, port, CONFIGS[config], workdir)
        try:
            client = Client(f"http://localhost:{port}")
            client.req("/api/signup", {"full_name": "Bench", "email": "bench@example.test", "password": "password123"})
            client.req("/api/login", {"email": "bench@example.test", "password": "password123"})
            per_task = {}
            for task in tasks:
                per_task[task["id"]] = []
                for run in range(args.runs):
                    rec = run_task(client, args.model, task)
                    per_task[task["id"]].append(rec)
                    results["configs"][config] = per_task
                    # Saved after every task, so a stopped run keeps what it did.
                    out.write_text(json.dumps(results, indent=1))
                    mark = "PASS" if rec["pass"] else "FAIL"
                    print(f"[{config}] {task['id']:<22} run {run + 1}: {mark}  {rec['why']}  ({rec['seconds']}s)", flush=True)
        finally:
            proc.terminate()
            proc.wait()

    shutil.rmtree(tmp, ignore_errors=True)

    print_summary(results, tasks, configs)
    print(f"\nfull results: {out}")


def print_summary(results, tasks, configs):
    width = max(len(c) for c in configs)
    print("\n" + "task".ljust(22) + "".join(c.rjust(width + 2) for c in configs))
    totals = {c: [0, 0] for c in configs}
    for task in tasks:
        row = task["id"].ljust(22)
        for c in configs:
            recs = results["configs"][c][task["id"]]
            passed = sum(r["pass"] for r in recs)
            totals[c][0] += passed
            totals[c][1] += len(recs)
            row += f"{passed}/{len(recs)}".rjust(width + 2)
        print(row)
    print("TOTAL".ljust(22) + "".join(f"{p}/{n}".rjust(width + 2) for p, n in totals.values()))
    cats = sorted({t["category"] for t in tasks})
    for cat in cats:
        ids = [t["id"] for t in tasks if t["category"] == cat]
        row = f"  {cat}".ljust(22)
        for c in configs:
            recs = [r for i in ids for r in results["configs"][c][i]]
            row += f"{sum(r['pass'] for r in recs)}/{len(recs)}".rjust(width + 2)
        print(row)


if __name__ == "__main__":
    main()
