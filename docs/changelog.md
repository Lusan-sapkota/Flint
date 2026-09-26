# Changelog

Notable changes in each release. The measurements behind a change are in
[experiments](experiments.md).

## [0.1.4] - 2026-09-26

### Added

- The docs are published at
  [flint.lusansapkota.com.np](https://flint.lusansapkota.com.np), from
  the `docs/` folder, with a home page, screenshots, and the reasoning
  behind the design (where the ideas come from, what was deliberately
  not built).
- Set your own context window in Settings → Connection, one for local
  models (for a GPU with room for more than the defaults) and one for
  cloud models, with common sizes (32k = 32768, 1M = 1048576, ...) to
  pick from. Cloud models get 32k by default instead of the 4k/8k meant
  for small local ones, and no model is given more than it supports.
- Ollama cloud models (`:cloud`) are tagged "cloud" in the model picker,
  Settings and the chat, with a line saying the chat is sent to
  ollama.com. Not being signed in to Ollama now says to run
  `ollama signin`.
- Change a chat's attached folder with the Change link on the folder
  chip. The new folder replaces the old one as the chat's standing
  context.
- A "Saved to memory" card where you saved a memory from a chat, which
  expands to show what was saved and stays after a reload.
- Settings → Memories shows which chat each memory was saved from, with
  a link back to it.
- **Reply instead** on a waiting shell command: tell the model what to do
  rather than just denying it. Your reply is an ordinary message, so
  `@web` and `@memory` work in it.
- Log out from Settings too, at the bottom of its sidebar.
- Edit your name and email, and change your password, in Settings →
  Account. A new email or password needs your current password, and a
  password change logs out your other devices.

### Changed

- `@web` results carry their publish dates, in the sources card and for
  the model, when Brave has one for every result. The model then answers
  "latest version" questions with its source's date instead of
  presenting a month-old page as current (E21).
- `@web` picks more relevant results: its re-ranking now sends
  `nomic-embed-text` the search prefixes it was trained with (E20).
- Logging out asks "Are you sure you want to log out?" first.
- The `@web` sources card spans the chat's full width too, and no longer
  jumps wider as it opens.
- Shell command cards span the chat's full width, so command output like
  `df -h` shows every column instead of scrolling sideways.
- Deleting a chat asks Yes / No instead of making you type DELETE. Typing
  DELETE stays for deleting a model, a memory or your account.
- In a chat without a folder, asking the model to run or check something
  gets the command to run yourself and a note that attaching a folder
  lets it run commands, instead of made-up output (E19).

### Fixed

- A chat whose first reply failed no longer tries to generate a title
  anyway, which cost a model call (quota, on a cloud model) and kept
  the input blocked while it ran.
- The "waiting for the reply" dots no longer stay on forever after an
  instant response, such as the `@compact` "Nothing to compact" notice or
  an error Ollama returns right away.
- A cloud model's details in Settings → Models show its context length.
- qwen3.5 no longer fails with "System message must be at the beginning"
  after `@memory`, a later `@web`, or once a long chat is summarized. Its
  chat template allows one system message, first; Flint now resends
  such messages as user messages for models that refuse them (E22).
- `nomic-embed-text` unloads as soon as an `@web` search has re-ranked
  its results, instead of staying in memory for Ollama's default 5
  minutes beside the chat model.
- When Ollama isn't reachable, Flint says so plainly (where it looked
  and what to do) instead of showing Go's raw connection error, and says
  it once per page instead of in every section.
- A saved Brave Search key is no longer sent back to the browser. It was
  in the Settings page and in `/api/me`, where the eye button or the page
  source showed it in full. Settings now shows only its last four
  characters, with Replace and Remove.
- After a reload, messages keep their `@web` / `@memory` tags as typed,
  and recalled memories show as "Recalled memories for …" after the
  message that asked, instead of the instruction text meant for the
  model. Collapsible headers keep their arrow on the same line.
- A denied command card shows just "Denied", not the instructions Flint
  writes for the model.
- Collapsible headers (sources, system messages) and link hovers no
  longer flash Pico's default blue.
- The "Attached folder" card no longer jumps sideways while it opens and
  closes.

## [0.1.3] - 2026-09-26

### Added

- Delete your account from Settings. It asks for your password and, if
  you've set security questions, their answers, then a typed `DELETE`
  confirmation. Everything in the account is deleted for good, including
  attachment files, and every session is signed out.
- Settings says when `nomic-embed-text` is missing and offers to pull it.
  It's optional: `@web` works without it, using Brave's ranking, and it
  only loads while a search runs. The Installed list marks it, and deleting
  it says what changes.
- Load and unload models from Settings → Models. Loading one unloads
  whatever else is running first.
- Settings → Connection lists your `@web` searches: the exact query sent
  to Brave, when, which chat, and how many this month.

### Changed

- Settings is laid out like the chat page: sections in a sidebar
  (Connection, Models, Memories, Account), one at a time, instead of one
  long scrolling column. On phones they become tabs.
- Thinking, sources, system messages and model details open and close
  with a short transition instead of snapping.

### Fixed

- Display math no longer shows a small vertical scrollbar.
- A model's details button shows a readable summary (family, parameters,
  quantization, context length, capabilities) instead of raw JSON or an
  empty box.

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

[0.1.4]: https://github.com/Lusan-sapkota/Flint/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/Lusan-sapkota/Flint/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/Lusan-sapkota/Flint/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/Lusan-sapkota/Flint/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/Lusan-sapkota/Flint/releases/tag/v0.1.0
