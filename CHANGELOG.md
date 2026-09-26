# Changelog

Notable changes in each release. The measurements behind a change are in
[docs/experiments.md](docs/experiments.md).

## [0.1.3] - 2026-09-26

### Added

- Delete your account from Settings. It asks for your password and, if
  you've set security questions, their answers, then a typed `DELETE`
  confirmation. Everything in the account is deleted for good, including
  attachment files, and every session is signed out.

### Changed

- Settings is laid out like the chat page: sections in a sidebar
  (Connection, Models, Memories, Account), one at a time, instead of one
  long scrolling column. On phones they become tabs.

### Fixed

- Display math no longer shows a small vertical scrollbar.

## [0.1.2] - 2026-09-26

### Changed

- A denied command no longer makes the model claim it lacks permissions.
  It now hears that you chose not to run that one command, and tries
  another way or asks you instead of guessing the output (E18).
- The context meter shows how many older messages have been condensed
  into a summary, for example "100 / 4.1k · 3 condensed".
- Hovering a chat in the sidebar shows its full title.

### Fixed

- Clicking the chat you're already in no longer reloads the page.

## [0.1.1] - 2026-09-26

### Changed

- The tool-reasoning nudge is left off when your message says not to run
  commands ("without running", "don't run", ...). On the benchmark's
  restraint tasks this went from 0/9 to 7/9, with no loss in tool use (E17).
- During an `@web` reply, the search query and its sources are shown as
  it happens, in a collapsible sources card.
- The Docker quick start prints Flint's address once it's running.
- The `latest` image tag only moves for normal releases, not pre-releases.

### Added

- A contributing guide, the repository rules, and how to report a
  vulnerability privately.
- How to update a Docker install to a new release.

## [0.1.0] - 2026-09-26

First release.

- Chat with local Ollama models, with accounts, SQLite persistence,
  markdown and offline math rendering, image attachments, a thinking
  toggle for models that support it, and search across chats.
- Folder attachment, with a shell tool that only runs a command after you
  approve it, a shield against catastrophic commands, and precondition
  checks before a command is offered.
- Context management for small windows: an explicit token budget, layered
  background summaries, and recovery when a prompt overflows.
- `@web` search (Brave Search, your own key), `@memory` for facts that
  carry across chats, and `@compact` to condense a chat on demand.
- A context meter, a scored benchmark with switchable scaffolding, and
  project documentation.
- A Docker image for amd64 and arm64 on ghcr.io, run with Docker Compose
  against the Ollama already on your machine.

[0.1.3]: https://github.com/Lusan-sapkota/Flint/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/Lusan-sapkota/Flint/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/Lusan-sapkota/Flint/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/Lusan-sapkota/Flint/releases/tag/v0.1.0
