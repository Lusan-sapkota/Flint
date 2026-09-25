# Flint

A small, fully offline chat UI for local Ollama models.

No CDN calls, no telemetry, no cloud dependency of any kind by default.
Every asset the frontend needs (htmx, Alpine.js, Pico.css) is vendored
locally, and the backend only ever talks to your local Ollama daemon.

The one deliberate exception: typing `@web <query>` triggers a real web
search (via the Brave Search API, your own API key configured in Settings)
— it only fires on explicit request, only sends the literal query text, and
is a no-op if you haven't configured a key.

## Stack

- **Backend:** Go, single static binary, SQLite for persistence (pure-Go
  driver, no cgo)
- **Frontend:** htmx + Alpine.js + Pico.css, vendored under
  `frontend/static/`, no build step
- **Model runtime:** [Ollama](https://ollama.com), running locally

## Why

Existing local-LLM web UIs are either too heavy for what they need to do
(bundled RAG/Whisper/torch stacks pulling multi-GB images for plain chat)
or too minimal to keep any history (browser-only, no persistence). Flint
sits in between: a tiny Go service that proxies chat requests to Ollama
and logs conversations to an indexed SQLite database, with a lightweight
server-rendered frontend on top.

The bet underneath that: a well-guided 3-4B local model is not an inferior
model, it's an under-scaffolded one. The usual bottleneck isn't the model's
own capability, it's naive unbounded context, no tool-calling discipline,
and no real persistence around it. Flint's job is to be the best possible
scaffolding for a small model — careful context management, structured
tool-calling with human approval, durable history — without becoming heavy
itself. Not the biggest, not the smallest: the best-guided.

## Status

Functional: auth (signup/login/security-question recovery), chat with
streaming responses, image and folder attachments, human-approved shell
tool calls, and a settings page are all in place end to end.

## Running

Make sure [Ollama](https://ollama.com) is running locally first
(`ollama serve`), then:

```bash
cd backend
go run .
```

Open `http://localhost:8080`. Optional env vars: `PORT`, `DB_PATH`,
`OLLAMA_BASE_URL` (defaults to `http://localhost:11434`),
`ATTACHMENTS_DIR`.

### Docker

```bash
docker compose up -d
docker compose exec ollama ollama pull qwen2.5:3b
```

This runs Flint alongside its own Ollama container, with the chat
history and the models each in a named volume. Flint is bound to
`127.0.0.1:8080` only, because signup is open and approved shell commands
run inside the container. Folder attach and the shell tool can only see
files inside the container, so bind-mount any project folder you want to
work on.

## License

AGPL-3.0 — see [LICENSE](./LICENSE).
