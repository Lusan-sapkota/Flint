# Flint

A small, fully offline chat UI for local Ollama models.

No CDN calls, no telemetry, no cloud dependency of any kind. Every asset
the frontend needs (htmx, Alpine.js, Pico.css) is vendored locally, and
the backend only ever talks to your local Ollama daemon.

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

## Status

Early scaffold. Structure is in place; backend logic and frontend views
are being built out.

## Project layout

```
flint/
├── backend/          Go module — HTTP server, SQLite storage, Ollama proxy
│   └── data/         chat.db lives here at runtime (gitignored)
└── frontend/
    ├── templates/    server-rendered HTML templates
    └── static/
        ├── css/      vendored Pico.css
        └── js/       vendored htmx.min.js, alpine.min.js
```

## Running

```bash
cd backend
go run .
```

More detailed setup instructions will land here as the backend takes shape.

## License

MIT — see [LICENSE](./LICENSE).
