# Architecture

Flint is one Go binary that serves both the web UI and a JSON/streaming
API, stores everything in SQLite, and talks to a local
[Ollama](https://ollama.com) for the models. There's no build step, no
CDN and no other service to run.

```
browser (htmx + Alpine.js + Pico.css, all vendored)
   │  same-origin HTTP: server-rendered pages + /api/*
   ▼
flint (Go, net/http)
   ├── SQLite (modernc.org/sqlite, pure Go, WAL, one connection)
   ├── attachment files on disk
   ├── Ollama HTTP API (chat, embeddings, model management)
   └── Brave Search API, only on an explicit `@web` query
```

## The bet

A well-guided 3-4B local model isn't an inferior model, just an
under-scaffolded one. Flint's work goes into the scaffolding around it:
careful context management, a structured tool loop with human approval,
and durable history. It avoids anything that only adds weight, which is
why there's no bundled RAG or embedding stack.

## Repository layout

```
backend/            Go module; run from here (asset paths are ../frontend/...)
  main.go           config from env, routes, graceful shutdown, /healthz
  auth.go           signup, login, logout, sessions, requireAuth
  recovery.go       security-question password recovery
  ratelimit.go      in-memory sliding-window limiter for auth endpoints
  handlers.go       conversations, messages, streaming turns, tool approval
  context.go        request budget and history fitting (see context-management.md)
  summary.go        background layered summaries
  memory.go         `@memory` save, draft, recall, folder memories, memory API
  ollama.go         Ollama client: chat (streaming and not), models, embeddings
  tools.go          the run_shell tool definition and reasoning nudge
  shield.go         hard block list for catastrophic commands
  preconditions.go  cheap checks before a command is offered for approval
  folder.go         folder manifest and per-turn anchor header
  images.go         attachment upload, storage and image token estimate
  websearch.go      Brave Search, then re-ranking via Ollama embeddings
  views.go          server-rendered pages
  db.go             schema, migrations, all SQL, chat search
frontend/
  templates/        html/template pages (base, chat, settings, login, signup, recover)
  static/js/        chat.js (streaming chat UI), settings.js, app.js, vendored libraries
  static/css/       flint.css on top of Pico.css
docs/               this documentation
Dockerfile, docker-compose.yml
```

## Data model

| Table | Holds |
|---|---|
| `users` | account, bcrypt password hash, own Ollama URL, preferred models, Brave API key |
| `sessions` | session id (the cookie value), user, expiry |
| `security_questions` | recovery questions, answers bcrypt-hashed |
| `conversations` | owner, title, model, attached folder, last context use, token ratio |
| `messages` | role (`user`/`assistant`/`system`/`tool`), content, thinking, tool calls |
| `attachments` | metadata; the file itself lives under `ATTACHMENTS_DIR` |
| `commands` | every model-proposed shell command, its status, output and exit code |
| `summaries` | layered summaries covering message id ranges (see context-management.md) |
| `memories` | facts a user saved with `@memory`, optionally tied to a folder |
| `memories_fts` | FTS5 index over memories, kept in sync by triggers |
| `messages_fts` | FTS5 index over message text, kept in sync by triggers |

Deleting a conversation cascades to all of these. Rows are never shared
between users.

Schema changes that `CREATE TABLE IF NOT EXISTS` can't make on an existing
database (new columns, the FTS index) are applied in `migrate` at startup.
Each step is idempotent.

## A chat turn

1. `POST /api/conversations/{id}/messages` takes the conversation's lock
   (`lockConversation`), so overlapping requests to one conversation run
   in arrival order.
2. The message and any attachments are validated as a whole, then saved.
   An `@web` query runs its search first. An `@memory` recall adds the
   matching memories first, and `@memory save` is handled on its own with
   no chat turn (see features.md).
3. `streamAssistantTurn` builds the budget and the fitted history,
   appends the tool nudge and folder anchor to the last message when a
   folder is attached, and streams the model's reply.
4. A tool call is checked by the shield, then by the preconditions, then
   saved as a pending command. The stream ends, and the user approves or
   denies it with a separate request, which runs the command and continues
   the same turn.
5. After a final text reply come the stats line, then title generation on
   the first message, then a background summarization pass.

## Streaming protocol

Replies stream as `text/plain`: the model's tokens, plus marker lines the
client consumes:

| Marker | Meaning |
|---|---|
| `<<<LOADING>>>` | the model isn't loaded yet; a cold start can take a while |
| `<<<THINK>>>"..."` | one JSON-string chunk of thinking, shown apart from the answer |
| `<<<CONTEXT>>>{"used":N,"max":M}` | context use for this response, sent after every model response |
| `<<<TOOL_RESULT>>>{"status":..,"output":..}` | what an approved or denied command produced, for display |
| `<<<TOOL_CALL>>>{"id":..,"command":..}` | a pending command awaiting approval; ends the stream |
| `<<<STATS>>>{"tokensPerSec":N}` | generation speed; ends a final text reply |
| `<<<MEMORY_DRAFT>>>{"text":..}` | a drafted memory for the user to review; ends the stream |

## Fixed decisions

The reasons behind the settled choices (pure-Go SQLite, a single
connection, cookie sessions instead of JWT, 404 instead of 403, no CORS,
native tool calls, the nudge placement and so on) are recorded in the
repository's agent notes. The ones that shape behavior are also described
in [security.md](security.md) and
[context-management.md](context-management.md).
