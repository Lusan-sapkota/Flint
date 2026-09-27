# Flint — Agent Guidance

This file provides instructions for AI coding assistants and agentic tools working in this repository to maintain consistency with Flint's architectural design and project standards.

## Project Goal & Thesis

Flint is a small, fully offline chat UI for local Ollama models:
- **Stack**: Single Go binary, SQLite persistence, and a light server-rendered frontend (htmx, Alpine.js, Pico.css), all vendored locally with **zero CDN calls** and **zero build step**.
- **Philosophy**: A well-guided 3–4B local model isn't an inferior model, it's an under-scaffolded one. The bottleneck is unbounded context, undisciplined tool use, and lack of persistence. Flint's job is to be the best possible scaffolding around a small model without becoming heavy itself.
- **Decision filter**: A feature belongs only if it helps a small model perform closer to its capability (context fitting, structured tools, agent decomposition, memory across chats). Reject features that only add weight (no bundled RAG or heavy embedding pipelines).

## Git & Commit Workflow

- **Branch**: Always work on and target the `dev` branch. `main` is reserved for releases.
- **Commit Messages**:
  - Explain *why* a change was made, not a line-by-line diff narration.
  - Keep testing narration out of commit messages (testing details belong in `docs_backup/experiments.md` or PR description).
  - **Backtick every `@word`**: Wrap `@agent`, `@web`, `@memory`, `@compact` in backticks (` `@agent` `). Bare `@word` turns into a GitHub username mention and corrupts release notes.
  - **No `Co-Authored-By` trailers**.
- **Verification**: Run `gofmt -w .` and `go test ./...` in `backend/` before any commit.

## Documentation System (Static HTML)

Flint uses **native, zero-dependency static HTML documentation** hosted directly on GitHub Pages (`main` `/docs` with `.nojekyll` and `docs/CNAME`):

- **Architecture**:
  - Edge-to-edge 3-column layout inspired by React docs (`react.dev`):
    1. **Top Nav**: Sticky bar with logo, version pill, Ctrl+K search trigger, and repository links.
    2. **Left Sidebar**: Grouped hierarchical navigation.
    3. **Center Column**: Breadcrumbs, "Copy link", clean typography, code block copy buttons, and responsive tables.
    4. **Right Sidebar**: "ON THIS PAGE" Table of Contents with active ScrollSpy tracking headings as the user scrolls.
  - **Offline Client Search (<kbd>Ctrl</kbd>+<kbd>K</kbd> / `/`)**: Instant fuzzy search across all pages and sections powered by `docs/assets/js/search-index.js`.
  - **Responsiveness**: All 3 columns fit side-by-side on desktop viewports (down to 961px); smoothly switches to an off-canvas drawer on mobile/tablet (≤ 960px).
- **Updating Docs**:
  - **Source files**: Maintain and edit raw Markdown files directly in `docs/` (e.g. `docs/features.md`).
  - **Generator script**: Run `python3 build_docs.py` to regenerate all HTML pages in `docs/` and `docs/assets/js/search-index.js`.
  - **Changelog preservation**: `docs/changelog.md` is preserved in `docs/` so the maintainer release script (`awk '/^## \[0.2.0\]/{f=1;next} /^## \[/{f=0} f' docs/changelog.md > /tmp/notes.md`) continues to work.
  - Every feature or architectural change must update the affected documentation in the same commit.

## Settled Architecture Decisions (Do Not Relitigate)

- **Pure-Go SQLite**: Uses `modernc.org/sqlite` (no cgo) for effortless cross-compilation and zero external runtime dependencies.
- **Single Connection WAL**: SQLite runs with `SetMaxOpenConns(1)` and WAL mode.
- **Session Auth**: Bcrypt passwords + HttpOnly cookie sessions stored in SQLite (no JWT). Cross-account access returns 404 to avoid leaking resource existence.
- **Human Approval for Shell Commands**: Every model-requested shell command requires explicit human approval (Approve / Deny / Reply). Never bypass confirmation.
- **Offline First**: Zero external CDN dependencies in the UI. Web search occurs only when explicitly triggered with `@web`.
