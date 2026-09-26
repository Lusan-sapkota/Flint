# Flint docs

Flint is a small, fully offline chat UI for local Ollama models: one Go
binary, SQLite, and a light server-rendered UI, with nothing fetched from
a CDN. The idea behind it is that a well-guided 3-4B model isn't an
inferior model but an under-scaffolded one, so Flint's job is to be the
best scaffolding around a small model without getting heavy itself:
context that never overflows, shell commands that always wait for your
approval, memory across chats, and web search only when you ask. Ollama
cloud models work too, under the same rules, tagged as cloud, and each
account can set its own context window for local and for cloud models.

![A shell command proposed by the model, waiting for Approve, Deny or Reply instead](images/hero-approval.png)

## Quick start

You need [Ollama](https://ollama.com) running with a model pulled, for
example `ollama pull qwen2.5:3b`.

**With Docker** (recommended; no clone needed). On Linux:

```bash
mkdir flint && cd flint && mkdir data
curl -O https://raw.githubusercontent.com/Lusan-sapkota/Flint/main/docker-compose.yml
docker compose up -d
```

Then open `http://localhost:3141`. On Mac and Windows, use
`docker-compose.desktop.yml` instead, as shown in
[Install and run](deployment.md).

**Or from source**, with Go installed:

```bash
git clone https://github.com/Lusan-sapkota/Flint.git
cd Flint/backend
go run .
```

Then open `http://localhost:8080` and sign up.

## The docs

**Get started**

- [Install and run](deployment.md): Docker and source, environment
  variables, health checks, and which models need which capabilities.

**Using Flint**

- [Features](features.md): everything Flint does, from a user's point of
  view.
- [Security](security.md): accounts, rate limits, the shell-command
  safety layers, and what leaves the machine.

**How it works**

- [Architecture](architecture.md): the pieces, repository layout, data
  model, a chat turn, the streaming protocol, where the ideas come from
  and what was deliberately not built.
- [Context management](context-management.md): how a conversation is
  fit into a small model's window.

**Evidence**

- [Experiments](experiments.md): the measurements behind each design
  decision, including what failed and was rejected.
- [Benchmark](benchmark.md): the scored benchmark, with and without
  each piece of scaffolding.
- [Testing](testing.md): unit tests, live checks against Ollama, the
  long-chat run and the web re-ranking set. The scripts it uses are
  [tools/long_chat.py](tools/long_chat.py) and
  [tools/web_rerank.py](tools/web_rerank.py).

**Project**

- [Changelog](changelog.md): what changed in each release.

## Screenshots

![An @web question answered from three ranked sources, listed above the answer](images/web-query.png)

![@memory save marks where a memory was saved; @memory recalls it in the chat](images/memory.png)

## Elsewhere

- [Source code](https://github.com/Lusan-sapkota/Flint) and
  [releases](https://github.com/Lusan-sapkota/Flint/releases)
- [Report a bug](https://github.com/Lusan-sapkota/Flint/issues), or a
  security problem privately through the repository's Security tab.

Every change to Flint updates the pages it affects, in the same commit.
