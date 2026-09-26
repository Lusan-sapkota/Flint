"""Scores `@web` re-ranking configs on fixed Brave results (E20).

Usage: python3 docs/tools/web_rerank.py [ollama_url]
Needs each model in CONFIGS pulled; a missing one is skipped. The results
file holds 15 real Brave searches (title, URL, snippet, as Flint parses
them); LABELS are relevance grades judged from title and snippet before
any model ran: 2 answers the query, 1 on topic, 0 off topic or stale.
"""
import json, math, sys, urllib.error, urllib.request
from pathlib import Path

OLLAMA = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:11434"
D = json.loads((Path(__file__).parent / "web_rerank_results.json").read_text())
LABELS = {
 "Kathmandu weather in October": [2,2,2,1,2,2,1,2,2,1],
 "Nepal monsoon season months": [0,2,2,2,1,2,1,1,2,2],
 "alpine.js x-for key": [2,1,2,0,0,1,0,1,0,0],
 "bcrypt cost factor recommended 2026": [2,2,2,2,2,2,0,1,0,1],
 "best way to store coffee beans": [2,1,2,2,2,2,1,1,1,2],
 "docker compose network_mode host on mac": [2,2,1,1,0,0,1,1,0,0],
 "how to undo last git commit keep changes": [2,2,2,1,2,2,2,0,2,1],
 "latest stable Go release version": [0,0,0,0,2,1,0,0,0,0],
 "ollama keep_alive parameter default": [2,2,0,1,0,0,1,2,2,1],
 "python sort list of dicts by key": [2,1,0,2,0,0,2,2,0,1],
 "rust borrow checker error E0502 explanation": [1,0,2,1,2,2,1,2,1,1],
 "sqlite FTS5 external content table triggers": [2,0,1,1,0,1,1,2,0,1],
 "symptoms of vitamin D deficiency": [1,2,1,0,2,1,2,2,1,1],
 "what is htmx hx-swap outerHTML": [1,0,0,2,1,2,1,0,0,0],
 "who won the 2026 FIFA World Cup final": [2,0,2,0,2,1,1,1,2,0],
}
ARCTIC_Q = "Represent this sentence for searching relevant passages: "
CONFIGS = {
 "brave order (no model)": None,
 "nomic, as Flint does now": ("nomic-embed-text", "", ""),
 "nomic + search_query/document prefixes": ("nomic-embed-text", "search_query: ", "search_document: "),
 "arctic 33m, no prefix": ("snowflake-arctic-embed:33m", "", ""),
 "arctic 33m + query prefix": ("snowflake-arctic-embed:33m", ARCTIC_Q, ""),
}
def post(path, body):
    r = urllib.request.urlopen(urllib.request.Request(OLLAMA + path, json.dumps(body).encode(), {"Content-Type": "application/json"}))
    return json.load(r)
def cos(a, b):
    return sum(x*y for x, y in zip(a, b)) / (math.sqrt(sum(x*x for x in a)) * math.sqrt(sum(y*y for y in b)))
def dcg(g): return sum(x / math.log2(i + 2) for i, x in enumerate(g))
for name, cfg in CONFIGS.items():
    gains, ndcgs = [], []
    try:
        for q, rs in D.items():
            lab = LABELS[q]
            order = list(range(10))
            if cfg:
                model, qp, dp = cfg
                inputs = [qp + q] + [dp + r["Title"] + " " + r["Snippet"] for r in rs]
                v = post("/api/embed", {"model": model, "input": inputs})["embeddings"]
                order = sorted(range(10), key=lambda i: -cos(v[0], v[i + 1]))
            top = [lab[i] for i in order[:3]]
            gains.append(sum(top))
            ndcgs.append(dcg(top) / dcg(sorted(lab, reverse=True)[:3]))
    except urllib.error.HTTPError as e:
        print(f"{name:42s} skipped: {e}")
        continue
    print(f"{name:42s} top-3 gain {sum(gains)}/{6 * len(gains)}  nDCG@3 {sum(ndcgs) / len(ndcgs):.3f}")
