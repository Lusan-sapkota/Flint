# Flint docs

Every change to Flint updates the pages it affects, in the same commit.

- [architecture.md](architecture.md): the pieces, repository layout, data
  model, what happens in a chat turn, and the streaming protocol.
- [features.md](features.md): what Flint does, from a user's point of view.
- [security.md](security.md): accounts, rate limits, the shell-command
  safety layers, and what leaves the machine.
- [deployment.md](deployment.md): running from source or Docker,
  environment variables, health checks, and which models need which
  capabilities.
- [context-management.md](context-management.md): how a conversation is
  fit into a small model's window.
- [experiments.md](experiments.md): the measurements behind each design
  decision, including what failed and was rejected.
- [testing.md](testing.md): unit tests, live checks against Ollama, and
  the long-chat run.
- [tools/long_chat.py](tools/long_chat.py): the scripted 19-turn chat used
  in the experiments.
