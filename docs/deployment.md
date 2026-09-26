# Running Flint

## From source

Requires Go (see `backend/go.mod`) and a running Ollama.

```bash
ollama serve                 # or the system service
cd backend
go run .                     # must run from backend/: assets resolve via ../frontend
```

Open `http://localhost:8080`, sign up, and pick a model.

To build a static binary (no cgo, any OS):

```bash
cd backend && CGO_ENABLED=0 go build -o flint .
```

## Environment variables

| Variable | Default | |
|---|---|---|
| `PORT` | `8080` | |
| `DB_PATH` | `data/chat.db` | SQLite file, relative to `backend/` |
| `ATTACHMENTS_DIR` | `data/attachments` | uploaded files |
| `OLLAMA_BASE_URL` | `http://localhost:11434` | server default; each user can override it in Settings |
| `FLINT_ABLATE` | empty | benchmark only: switches scaffolding off, see [benchmark.md](benchmark.md). Leave unset. |

## Docker

```bash
docker compose up -d
docker compose exec ollama ollama pull qwen2.5:3b
```

Compose runs Flint alongside its own Ollama container. Chat data and
models each live in a named volume. Notes:

- Flint is published on `127.0.0.1:3141` only: open
  `http://localhost:3141`. The port is deliberately uncommon, since 8080
  is where most dev servers go. Set `FLINT_PORT` to use another one
  (`FLINT_PORT=4000 docker compose up -d`). Inside the container, Flint
  still listens on 8080.
- The image is Alpine, not scratch, because the shell tool needs `sh`.
  Only the tools Alpine ships are available to commands.
- Folder attach and the shell tool only see the container's filesystem.
  Bind-mount a project folder to work on it.
- A GPU needs the usual Compose GPU configuration on the `ollama` service.
- The image build and `compose up` haven't been run for real yet. The
  container's file layout was verified by running the static binary from
  the same directory structure.

## Health

`GET /healthz` checks the database and Ollama's `/api/version`. It returns
200 with `{"db":"ok","ollama":"ok"}`, or 503 naming what failed.

## Shutdown

SIGINT and SIGTERM stop accepting requests and give in-flight ones up to
10 seconds to finish. A background summarization cut short by shutdown is
just redone later.

## Models

Anything Ollama serves works for chat. What Flint uses:

- **Tool calling** (folder attach) needs a model with the `tools`
  capability, such as qwen2.5-3b-instruct or qwen3.5-4b.
- **Images** need `vision` (qwen3.5-4b).
- **Thinking** needs `thinking` (qwen3.5-4b).
- **`@web` re-ranking** uses `nomic-embed-text`. Without it, results are
  used unranked.

A plain chat runs with a 4096-token window and a folder chat with 8192;
see [context-management.md](context-management.md).
