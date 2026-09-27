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
   ├── Ollama HTTP API (chat, embeddings, model management;
   │   a `:cloud` model is forwarded by Ollama to ollama.com)
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
  agent.go          `@agent` plan call and validation, limits, run and transcript API
  agentrun.go       running agents: concurrency, inputs, commands, budgets, results
  ablate.go         FLINT_ABLATE: switch scaffolding off for the benchmark
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
  static/js/        chat.js (streaming chat UI, markdown/math setup), settings.js, app.js,
                    vendored htmx, Alpine.js, marked, DOMPurify, Temml
  static/css/temml/ Temml's stylesheet and math font (local only)
  static/css/       flint.css on top of Pico.css
bench/              scored benchmark: run.py, tasks.py, fixture/
docs/               this documentation
Dockerfile, docker-compose.yml (Linux), docker-compose.desktop.yml (Mac/Windows)
.github/workflows/  docker.yml: publishes the image on version tags
```

## Data model

| Table | Holds |
|---|---|
| `users` | account, bcrypt password hash, own Ollama URL, preferred models, Brave API key, context window overrides (`num_ctx` local, `cloud_num_ctx` cloud), how many `@agent` agents run at once (`max_agents`, `cloud_max_agents`; empty = 2 local, 10 cloud) |
| `sessions` | session id (the cookie value), user, expiry |
| `security_questions` | recovery questions, answers bcrypt-hashed |
| `conversations` | owner, title, model, attached folder, last context use, token ratio |
| `messages` | role (`user`/`assistant`/`system`/`tool`), content, thinking, tool calls |
| `attachments` | metadata; the file itself lives under `ATTACHMENTS_DIR` |
| `commands` | every model-proposed shell command, its status, output and exit code; `agent_id` is set when an `@agent` agent proposed it |
| `summaries` | layered summaries covering message id ranges (see context-management.md) |
| `memories` | facts a user saved with `@memory`, optionally tied to a folder and to the chat it was saved from (`conversation_id`, cleared if that chat is deleted) |
| `memories_fts` | FTS5 index over memories, kept in sync by triggers |
| `messages_fts` | FTS5 index over message text, kept in sync by triggers |
| `agent_runs` | one `@agent` run in a chat: the task, its status (planned, running, done, failed, cancelled, discarded) and the combined answer |
| `agents` | one subtask of a run: its instruction, input files or web query, a note on inputs cut to fit, status, bounded result or error |
| `agent_messages` | each agent's full transcript, shown in the UI and never sent back to the model |

Deleting a conversation cascades to its messages, attachments, commands,
summaries and agent runs (with their agents and transcripts); a memory saved from it stays and only loses its link.
Deleting a user removes everything they own. Rows are never shared
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
   no chat turn, and so is `@compact` (see features.md).
3. `streamAssistantTurn` builds the budget and the fitted history,
   appends the tool nudge and folder anchor to the last message when a
   folder is attached (the nudge is left off when the user's message says
   not to run commands, see experiments.md E17), and streams the model's
   reply. Without a folder no tool is offered; if the message asks to run
   or check something, a note tells the model to give the command for the
   user to run instead of inventing output (E19).
4. A tool call is checked by the shield, then by the preconditions, then
   saved as a pending command. The stream ends, and the user approves or
   denies it with a separate request, which runs the command and continues
   the same turn. **Reply instead** denies it without continuing
   (`deny?reply=1`), and the user's reply is sent as an ordinary message.
5. After a final text reply come the stats line, then title generation on
   the first message (skipped if the reply failed), then a background
   summarization pass.

## An `@agent` run

1. **Plan.** `@agent <task>` makes one call to the chat's model with a JSON
   schema and the chat's own `num_ctx` (another size would reload the
   model, E24). It needs an attached folder. Go validates the plan
   (folder files only, no web queries from the model, inputs or
   exploration) and saves it as a `planned` run; the stream ends with
   `<<<AGENT_PLAN>>>`. Nothing enters the chat history. A web search is
   only added by the user on the card, and checked again at Run.
2. **Run.** `POST /api/agent-runs/{id}/run` takes the conversation's lock,
   re-checks the run is still planned, validates the edited plan again,
   and streams `<<<AGENTS>>>` lines until every agent ends. At most "max
   agents" run at once, in plan order; the rest wait.
3. **One agent.** Go reads its files (a big file keeps its start and end,
   sized at 1.5 chars per token so the prompt really fits) or runs its
   search, and builds a fresh history: instructions and the subtask with
   its inputs. With a tool-capable model and a folder, it may call the
   shell tool without a JSON format (a format suppresses tool calls,
   E25), with the tool nudge on the last message. Each call is checked by
   the shield and preconditions and waits for the user in the main chat;
   the decision arrives through
   `POST /api/agent-runs/{id}/commands/{cmdId}/{approve|deny}`, which
   doesn't take the conversation's lock. Then one call with the result
   schema gives `{"answer","found"}`, which Go checks and bounds. The
   whole transcript goes to `agent_messages`, never to the chat.
4. **Stop.** Closing the run's request (Stop, a closed tab) cancels every
   agent and every waiting command.

## Streaming protocol

Replies stream as `text/plain`: the model's tokens, plus marker lines the
client consumes:

| Marker | Meaning |
|---|---|
| `<<<LOADING>>>` | the model isn't loaded yet; a cold start can take a while |
| `<<<THINK>>>"..."` | one JSON-string chunk of thinking, shown apart from the answer |
| `<<<CONTEXT>>>{"used":N,"max":M,"condensed":C}` | context use for this response, sent after every model response; `condensed` is how many messages a summary replaced in the request |
| `<<<TOOL_RESULT>>>{"status":..,"output":..}` | what an approved or denied command produced, for display |
| `<<<TOOL_CALL>>>{"id":..,"command":..}` | a pending command awaiting approval; ends the stream |
| `<<<STATS>>>{"tokensPerSec":N}` | generation speed; ends a final text reply |
| `<<<MEMORY_SAVED>>>{"content":..}` | `@memory save <text>` stored that text; shown as the "Saved to memory" card |
| `<<<MEMORY_DRAFT>>>{"text":..}` | a drafted memory for the user to review; ends the stream |
| `<<<SEARCHING>>>{"query":..}` | an `@web` search has started |
| `<<<SOURCES>>>{"query":..,"sources":[{"title","url","date"?}]}` | the results the answer will be based on; `date` only when every result has one |
| `<<<AGENT_PLAN>>>{run}` | an `@agent` plan awaiting Run or Discard; ends the stream |
| `<<<AGENTS>>>{"type":"state",..}` | on an `@agent` run's own stream (`POST /api/agent-runs/{id}/run`): the run's status and every agent's status, result and error, sent on each change |
| `<<<AGENTS>>>{"type":"command",..}` | an agent asked to run a command (`status` pending), or what it did once decided (with `output`) |

## Fixed decisions

The reasons behind the settled choices (pure-Go SQLite, a single
connection, cookie sessions instead of JWT, 404 instead of 403, no CORS,
native tool calls, the nudge placement and so on) are described where
they apply: [security.md](security.md) for accounts, sessions and the
shell tool, [context-management.md](context-management.md) for prompts
and the window, and [experiments.md](experiments.md) for the measurements
behind them.

## Where the ideas come from

Each borrows from research, scaled down to what a small local tool can
honestly claim:

- **The retry budget** is a bounded verifier loop, after Snell et al. on
  test-time compute. A shell exit code is a free, high-precision
  verifier, so every tool result states success or failure explicitly
  (`[exit code: N]` or `[FAILED, exit code: N]`; a silent failure used to
  look like success), and after 3 tool calls in a row the model has to
  answer in text. A small retry budget beats one greedy attempt without
  a bigger model, while each retry still costs you an approval.
- **The shield** takes the idea of shielding (Alshiekh et al.): block
  unsafe actions before they run. Unlike that work it's a pattern list,
  not a proof. A shell is Turing-complete, so it's a floor under human
  approval, not a guarantee.
- **Preconditions** are the ToolGate / Hoare-triple idea at its smallest:
  check that a command can work before asking you to approve it.
- **Decay by observed signals.** How fast old tool output fades depends
  on what can actually be seen (a failed result fades 3x slower, an
  exploring command's output 2x faster), not on a belief filter whose
  transition probabilities would have to be invented, with no data to
  learn them from.

## Deliberately not built

- **A bundled RAG or embedding stack.** It's the weight Flint exists to
  avoid. `@web` re-ranking uses Ollama's own `/api/embed` instead.
- **Sampling several commands to detect confabulation** (semantic
  entropy, Farquhar et al.). It triples the model calls per tool
  decision, and grouping shell commands by syntax gives false alarms,
  because many different commands do the same thing (`find`, `ls -R`
  and `git ls-files` all count the same files).
- **Conformal abstention by embedding distance** to the folder
  manifest. With no calibration set, any cutoff would be arbitrary
  presented as a bound. The score doesn't track hallucination either: a
  correct "this code doesn't have that" is far from the manifest, and a
  wrong answer that name-drops real files is close to it.

Neither should come back without solving the problem that stopped it.
