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
| `HOST` | `127.0.0.1` | address to listen on; localhost only by default, since commands run on this machine and signup is open |
| `PORT` | `8080` | |
| `DB_PATH` | `data/chat.db` | SQLite file, relative to `backend/` |
| `ATTACHMENTS_DIR` | `data/attachments` | uploaded files |
| `OLLAMA_BASE_URL` | `http://localhost:11434` | server default; each user can override it in Settings |
| `FLINT_ABLATE` | empty | benchmark only: switches scaffolding off, see [benchmark.md](benchmark.md). Leave unset. |

## Docker

Docker runs **Flint only**. Ollama stays the normal app on your machine,
where it already has your GPU and your models, and the container talks
to it.

**Linux:**

```bash
docker compose up -d
```

The container uses host networking, so it reaches Ollama at
`localhost:11434` exactly as a native Flint would, and Ollama never has to
listen beyond localhost.

**Mac and Windows (Docker Desktop):**

```bash
docker compose -f docker-compose.desktop.yml up -d
```

Host networking doesn't work there the same way, because Docker runs in a
small VM. This file uses ordinary port mapping, and reaches Ollama through
`host.docker.internal`, which Docker Desktop forwards to your machine.
This setup is untested so far, since it was built on Linux; if it doesn't
reach Ollama, please open an issue.

Then open `http://localhost:3141`. Notes:

- **Port:** 3141 is deliberately uncommon, since 8080 is where most dev
  servers go. Set `FLINT_PORT` to use another
  (`FLINT_PORT=4000 docker compose up -d`). Either way it's published on
  `127.0.0.1` only.
- **Data:** chats and attachments live in the `flint-data` volume and
  survive restarts and rebuilds.
- **Folders:** folder attach and the shell tool only see the container's
  filesystem. To work on a project, bind-mount it (add a `volumes:` entry
  such as `- /home/you/project:/home/flint/project`), or run Flint natively.
- **Shell:** the image is Alpine (23.5 MB), not scratch, because the shell
  tool needs `sh`. Commands only have the tools Alpine ships.
- **Listening address:** the image sets `HOST=0.0.0.0`, so a plain
  `docker run -p` works. The Linux compose file narrows it back to
  `127.0.0.1`, since host networking would otherwise expose it to the
  network.

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
