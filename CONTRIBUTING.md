# Contributing to Flint

Thanks for your interest. Flint is small on purpose, so it helps to read
this before opening a pull request.

## What fits

Flint is scaffolding around small local models. A well-guided 3-4B model
isn't an inferior model. It's usually held back by unbounded context,
undisciplined tool use and missing persistence, not by what it can do.
A change fits if it helps a small model perform closer to its real
capability, through better context handling, better tool guidance or
better grounding, without making Flint heavy. A feature that only adds
weight doesn't fit, even if other chat UIs have it. That's why there's no
bundled RAG or embedding stack.

Some decisions are settled, each with its reasoning in
[docs/](docs/README.md): the pure-Go SQLite driver, cookie sessions rather
than JWT, human approval for every shell command, the vendored frontend
with no build step, and working fully offline except for an explicit
`@web`. Please open an issue to discuss before working against one of
these.

For anything bigger than a small fix, open an issue first so we can agree
on the approach before you spend time on it.

## Setup

You need Go (see `backend/go.mod` for the version) and
[Ollama](https://ollama.com) with a model pulled, for example
`ollama pull qwen2.5:3b`.

```bash
cd backend
go run .
```

Then open `http://localhost:8080`. The frontend in `frontend/` is plain
HTML, htmx, Alpine.js and Pico.css, vendored locally, with nothing to
build. Don't add CDN links or a build step.

## Before you open a pull request

- **Branch from `dev` and target `dev`.** `main` only gets releases.
- **Format and test:**

  ```bash
  cd backend
  gofmt -w .
  go test ./...
  ```

- **Update the docs in the same pull request.** Every change updates the
  pages in `docs/` it affects.
- **Measure changes to prompts or the context pipeline.** Run the
  benchmark ([docs/benchmark.md](docs/benchmark.md)) before and after,
  on a small task list with `--runs 3`, and add the numbers to
  [docs/experiments.md](docs/experiments.md) as a new E-numbered entry.
  If you're fixing something the benchmark found, measure the baseline
  first so the fix itself is measured. Test with a real folder attached,
  not a short synthetic prompt: short prompts hide failures that only show
  up once the real folder listing is in context.
- **Check behavior against the real model.** Don't rely on an API's docs
  alone. Ollama's actual behavior depends on the model and version, so
  confirm it with curl first.

## Code style

- Keep it lean: no speculative abstractions, no options nobody uses, no
  handling for cases that can't happen.
- Comments only for a non-obvious *why*: a hidden constraint, a subtle
  invariant, a workaround for a specific bug. Don't restate what
  well-named code already says.
- Follow what the surrounding code already does.

## Commit messages

- Explain why the change was made, not a line-by-line account of the
  diff.
- Keep testing notes out. What was measured belongs in
  `docs/experiments.md` or the pull request description.
- Wrap anything like `@web` in backticks. GitHub turns a bare `@word`
  into a mention of whoever owns that username.

## Security issues

Don't open a public issue. Report them privately through **Security →
Report a vulnerability** on GitHub. See
[docs/security.md](docs/security.md#reporting-a-vulnerability).

## License

Flint is licensed under AGPL-3.0. By contributing, you agree your
contribution is licensed the same way.
