# Testing

## Unit tests

```bash
cd backend
go test ./...
```

These need no Ollama and no network. `context_test.go` and
`summary_test.go` cover the context pipeline: the budget, summaries
replacing the messages they cover, drop and cut to fit, chunk boundaries,
merge candidates and verbatim user notes. `memory_test.go` covers
`@memory` parsing, per-account scoping, any-word search, the index
following edits, and the memory budget. `summary_test.go` also covers the forced chunk
behind `@compact`.

## Rendering

Markdown, math and the no-remote-content rule run in the browser, so
there's no Go test for them. They were checked by rendering these inputs
through `renderMarkdown` on a live page, and confirming no request left
localhost: `\(\pi r^2\)`, `$E=mc^2$`, `$$\sum…$$`, `\[…\]`, "costs $5 and
$10, or $5-$10", `$` inside inline code and a fenced block, broken
`\(\frac{1}{\)`, `![cat](https://…)`, raw `<img src="https://…">` next to a
`data:` image, and `<p style="background:url(…)">`, `<svg><image href>`,
`<video src>`. Recheck these after changing chat.js's markdown setup.

## Live checks

`backend/live_test.go` is behind the `live` build tag, so `go test ./...`
never runs it. It needs Ollama running and a **copy** of a Flint database
with at least one conversation:

```bash
sqlite3 backend/data/chat.db ".backup /tmp/copy.db"   # .backup, not cp: the DB runs in WAL mode
cd backend
LIVE_DB=/tmp/copy.db LIVE_MODEL=qwen2.5-3b-instruct go test -tags live -run TestLiveSummarizeChunk -v .
LIVE_DB=/tmp/copy.db go test -tags live -run TestLiveDumpHistory -v .
```

- `TestLiveSummarizeChunk` prints the summary the model writes for the
  oldest chunk that can be summarized. Add `SHOW=1` to also print the
  transcript it was given. Use it to try out summary prompts on a fixed
  input, without rerunning a whole chat.
- `TestLiveDumpHistory` prints exactly what the next request would send:
  role, tool-call count and the start of each message. This is how the
  tool-call drift in E8 was found.

## The long-chat run

`docs/tools/long_chat.py` drives one 19-turn chat with a folder attached
through the real HTTP API and reports tool calls per turn, peak context
use and overflow errors. It plants facts, makes the model use tools, and
then asks for the facts back.

Run it against a scratch backend and a scratch copy of a folder:

```bash
mkdir -p /tmp/flint-run && cp backend/*.go /tmp/flint-run/
cd backend && go build -o /tmp/flint-test .
PORT=8097 DB_PATH=/tmp/flint-run.db ATTACHMENTS_DIR=/tmp/flint-run-att /tmp/flint-test &
python3 ../docs/tools/long_chat.py /tmp/flint-run qwen2.5-3b-instruct http://localhost:8097
```

The script approves only commands that look read-only and denies the
rest. The denials are part of the test, since they're what pushed the
model into confused text-only replies in E8. Because summaries are
written in the background, wait a minute after it finishes before looking
at them:

```bash
sqlite3 /tmp/flint-run.db "select id, level, first_message_id, last_message_id, merged, user_notes, content from summaries"
```

(The system `sqlite3` may be built without FTS5. The summaries table is a
plain table, so this works either way.)

To compare against another version, build it from a git worktree and run
the same script on a different port against the same Ollama, one run
after the other. Runs vary, so compare the tool-call and overflow counts
across the whole run, not single replies.

## Using your installed models without sudo

The system Ollama service keeps its models in
`/usr/share/ollama/.ollama/models`. That folder is world-readable, so a
second Ollama started as your own user can serve those models read-only
while the service is stopped:

```bash
OLLAMA_NOPRUNE=1 OLLAMA_MODELS=/usr/share/ollama/.ollama/models ollama serve
```

A plain `ollama serve` as your own user looks in `~/.ollama/models`
instead and won't see them.
