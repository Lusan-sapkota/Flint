<p align="center">
  <img src="frontend/static/image/flint-512.png" alt="Flint" width="120">
</p>

<h1 align="center">Flint</h1>

<p align="center">A small, fully offline chat UI for local Ollama models, built to get the most out of small models.</p>

No CDN calls, no telemetry, no cloud dependency. Everything the frontend
needs is vendored, and the backend only talks to your local Ollama. The one
deliberate exception is `@web <query>`, which searches the web through the
Brave Search API with your own key, only when you ask, sending only the
query text.

## Why

Local-LLM web UIs tend to be either too heavy (bundled RAG, Whisper and
torch stacks in a multi-GB image, just to chat) or too minimal (no history
at all). Flint sits in between: one small Go binary, SQLite, and a light
server-rendered UI.

The bet underneath: a well-guided 3-4B model isn't an inferior model, it's
an under-scaffolded one. The usual bottleneck isn't the model, it's naive
unbounded context, no discipline around tool calls, and no memory. Flint's
job is to be the best possible scaffolding around a small model without
becoming heavy itself.

## Highlights

- **Long chats that never overflow.** A real context budget per request,
  background layered summaries that keep your own words verbatim, and
  `@compact` to condense on demand.
  ([context management](docs/context-management.md))
- **Shell tools you stay in control of.** Attach a folder and the model can
  propose commands through Ollama's native tool calling. Every command
  waits for your approval, a shield blocks catastrophic ones outright, and
  commands that can't work are caught before they reach you.
  ([security](docs/security.md))
- **Memory across chats.** `@memory save` keeps what matters (drafts are
  reviewed before saving), `@memory <words>` recalls it, and memories tied
  to a folder load whenever that folder is attached.
- **Web search on request.** `@web <query>`, re-ranked locally with
  Ollama embeddings.
- **Everything a chat UI needs:** streaming, thinking toggle, images and
  file attachments, chat search, offline math rendering, editable last
  message, automatic titles, model management.
  ([all features](docs/features.md))
- **Tested against real models, and written down.** Every design decision
  in the context pipeline comes with the measurement behind it, including
  the attempts that failed. ([experiments](docs/experiments.md))

## Who it's for

Flint is a personal tool, built for one person on one machine: mine, a
laptop with a 6 GB GPU running 3-4B models. Everything is sized and tested
for that: the context budgets, the prompts, the models it's tuned on. It
runs on your own computer, listens on localhost, and isn't meant to be
exposed to a network or shared with other people. If you run it on a bigger
rig and something breaks or behaves oddly, please open an issue: that's
exactly the feedback I can't get from my own hardware.

## Quick start

With [Ollama](https://ollama.com) running and a model pulled (for example
`ollama pull qwen2.5:3b`):

```bash
cd backend
go run .
```

Open `http://localhost:8080` and sign up. Or with Docker, which runs its
own Ollama alongside Flint:

```bash
docker compose up -d
docker compose exec ollama ollama pull qwen2.5:3b
```

Environment variables, Docker notes and which models need which
capabilities: [deployment](docs/deployment.md).

## Docs

| | |
|---|---|
| [Features](docs/features.md) | everything Flint does |
| [Architecture](docs/architecture.md) | layout, data model, a chat turn, the streaming protocol |
| [Context management](docs/context-management.md) | how a chat fits a small window |
| [Security](docs/security.md) | accounts, the shell-command safety layers, what leaves the machine |
| [Deployment](docs/deployment.md) | running from source or Docker |
| [Experiments](docs/experiments.md) | the measurements behind the design |
| [Benchmark](docs/benchmark.md) | scored tasks, with and without each piece of scaffolding |
| [Testing](docs/testing.md) | unit tests, live checks, the long-chat run |

## Stack

Go and SQLite (pure-Go driver, no cgo) in a single static binary; htmx,
Alpine.js and Pico.css with no build step; [Ollama](https://ollama.com) for
the models.

## License

AGPL-3.0, see [LICENSE](./LICENSE).
