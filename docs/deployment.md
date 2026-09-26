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
to it. The image is published at `ghcr.io/lusan-sapkota/flint` (see
[Publishing the image](#publishing-the-image)), and the compose files in
the repository root are ready-to-use examples. Download one; there's no
need to clone the repository.

**Linux:**

```bash
mkdir flint && cd flint && mkdir data
curl -O https://raw.githubusercontent.com/Lusan-sapkota/Flint/main/docker-compose.yml
docker compose up -d
```

The container uses host networking, so it reaches Ollama at
`localhost:11434` exactly as a native Flint would, and Ollama never has to
listen beyond localhost.

**Mac and Windows (Docker Desktop):**

```bash
mkdir flint && cd flint && mkdir data
curl -O https://raw.githubusercontent.com/Lusan-sapkota/Flint/main/docker-compose.desktop.yml
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
- **Data:** everything Flint stores (chats, memories, summaries,
  settings, attachments) lives in the `data/` folder next to the compose
  file. It's a plain folder, not a Docker volume, so it survives
  `docker compose down -v` and rebuilds, and you can back it up like any
  other files. It's gone only when you delete the folder. Create it
  yourself before the first start: if Docker creates it, it's owned by
  root and the container can't write to it. The container runs as uid
  1000, the usual first user on Linux. It's separate from
  `backend/data/`, which a native `go run .` uses.
- **Folders:** folder attach and the shell tool only see the container's
  filesystem. To work on a project, bind-mount it (add a `volumes:` entry
  such as `- /home/you/project:/home/flint/project`), or run Flint natively.
- **Shell:** the image is Alpine (23.5 MB), not scratch, because the shell
  tool needs `sh`. Commands only have the tools Alpine ships.
- **Listening address:** the image sets `HOST=0.0.0.0`, so a plain
  `docker run -p` works. The Linux compose file narrows it back to
  `127.0.0.1`, since host networking would otherwise expose it to the
  network.

- **Updating:** `docker compose pull && docker compose up -d` fetches the
  newest `latest`, and `./data` is kept. `latest` only moves for normal
  releases, not pre-releases like `v0.2.0-beta`. To stay on one version,
  change the tag in the compose file, for example
  `ghcr.io/lusan-sapkota/flint:0.1` (patch releases of 0.1) or `:0.1.0`
  (exactly that one). Nothing updates automatically.

### Building the image yourself

From a clone of the repository:

```bash
docker build -t ghcr.io/lusan-sapkota/flint:latest .
```

The compose files then use that local image instead of downloading one.
Remove it (`docker rmi ghcr.io/lusan-sapkota/flint:latest`) to go back to
the published image, since otherwise the local copy keeps shadowing it.

### Publishing the image

`.github/workflows/docker.yml` builds the image for `linux/amd64` and
`linux/arm64` and pushes it to GitHub Container Registry whenever a version
tag is pushed, and at no other time:

```bash
git tag v0.1.0
git push origin v0.1.0
```

That publishes `0.1.0`, `0.1` and `latest`. A pre-release tag such as
`v0.2.0-beta` publishes only its own version, so `latest` keeps pointing
at the newest normal release. It authenticates with the
workflow's own `GITHUB_TOKEN`, so no secrets or extra accounts are needed.
After the first publish, check the package's visibility on GitHub (profile
→ Packages → flint → Package settings) and set it to **Public** if it isn't,
or nobody else can pull it. The workflow file has to exist in the tagged
commit, so tag a commit that includes it.

The repository has two rulesets, which apply to the owner too:

- **`main`** can't be force-pushed or deleted, since it's what people
  clone and what the download links point at.
- **`v*` tags** can't be deleted or moved once pushed, so a published
  version always means the same code. A broken release is fixed by
  tagging the next version (`v0.1.1`), not by re-tagging.

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
- **`@web` re-ranking** uses `nomic-embed-text`, and it's optional, not a
  dependency. Without it `@web` still works, using Brave's own ranking.
  With it, the model only loads for the moment a search runs, then Ollama
  unloads it when idle (5 minutes by default). Flint never pulls it on its
  own: Settings → Connection offers a Pull button when a Brave key is set
  and the model is missing, and the Installed list marks it.

A plain chat runs with a 4096-token window and a folder chat with 8192;
see [context-management.md](context-management.md).
