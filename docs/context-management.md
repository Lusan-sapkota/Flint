# Context management

How Flint fits a conversation into a small model's context window, and why
each step exists. The numbers behind each decision are in
[experiments.md](experiments.md).

A small local model has a small window, 4096 or 8192 tokens here. Every
request resends the whole history, because Ollama keeps no state between
requests. Left alone, a long chat overflows the window, and Ollama then
either quietly drops messages from the middle or rejects the request (see
[E1](experiments.md#e1-what-ollama-does-when-a-prompt-is-too-big) and
[E4](experiments.md#e4-the-folder-manifest-was-bigger-than-the-window)).
Flint's job is to decide what the model sees, instead of leaving that to
chance.

## The request budget

Every request sets `num_ctx` explicitly (`numCtxFor` in `context.go`): 4096
for a plain chat and 8192 with a folder attached. A request without it
makes Ollama reload the model at its server default, and the budget needs
to know the real window.

```
budget = num_ctx - responseReserve (1024) - tool schema - tool nudge, anchor and folder memories
```

Token counts are estimated as characters / 4, then corrected by a
per-conversation ratio (`tokenCounter`, the `token_ratio` column). After
each reply, Flint divides Ollama's real `prompt_eval_count` by its own
estimate and stores the result, clamped to 0.5-4. Characters / 4 was off by
2x on dense text (E1); on the Go-code chats in E5-E10 the measured ratio
settled at 1.14-1.21. Turns that send images skip calibration, since
images have their own estimate.

The one gap is a new chat, which has no measurement yet. If its first
estimate undercounts and Ollama rejects the request as too big, the
rejection states the real size. Flint recalibrates from that, refits and
retries once, before anything has streamed
([E16](experiments.md#e16-a-new-chat-couldnt-recover-from-one-big-command-output)).

Images are estimated from their pixel size, width x height / 1024 tokens
with a floor of 256, which matches qwen3.5-4b (E3). Formats whose size the
Go standard library can't read (webp, bmp) count as 1024. Only the latest
message with images still sends them. Older images become a text note
telling the model to ask for a re-send.

## What always goes to the model verbatim

- the first system message (the folder-attach manifest)
- the first user message (usually the goal)
- the last 6 messages (`protectedWindow`)
- the latest message with images
- the latest tool call and its result, even after a summary covers them
  (E8: without one real example, qwen2.5-3b stopped making structured tool
  calls and wrote commands as plain text)

The folder manifest is capped at 12 KB (`maxAttachTotalSize`), about 3.6k
tokens of Go code, so it fits well inside the 8192 window (E4).

**Later system messages.** Summaries, context notes, `@web` results and
`@memory` recalls are system messages placed later in the history, closer
to generation. Some chat templates refuse that: qwen3.5's raises "System
message must be at the beginning". On that exact error `withSystemFallback`
(ollama.go) resends with every system message after the first sent as a
user message, and remembers the model until Flint restarts, so other
models keep the placement the experiments measured (E22).

## Fitting everything else, cheapest loss first

`buildOptimizedHistory` does these steps in order:

1. **Summaries replace what they cover.** Messages inside a summary's range
   are left out, and one system message holding all the current summaries
   goes where they were.
2. **Tool output and later system messages decay.** The older they are,
   the more they're truncated. Failed commands decay 3x slower, and
   exploratory commands (`ls`, `cat`, ...) decay 2x faster. User and
   assistant messages never decay. Truncating them taught the model to end
   its own replies with "...[truncated]" (E5).
3. **Old tool cycles condense to one line** once the history is over budget.
4. **The oldest unprotected messages are dropped whole**, and the model is
   told `[N earlier messages omitted ...]`. A tool result is never kept
   without the call it answers.
5. **Tool output is cut down, oldest first**, if the protected messages
   alone are still over budget. One recent `cat` can be 20k characters
   (E10). The model never writes tool output, so the cut marker can't be
   copied.

After step 5 the prompt fits the budget. The only exception is protected
text other than tool output that is bigger than the window by itself.

## Summaries

`summary.go`. These run in the background after a reply has finished
streaming, so they never delay or hold open a response. History built
before a summary lands just falls back to step 4. A conversation has at
most one summarization running at a time (`Server.summarizing`), and one
pass writes at most 4 summaries.

**When.** Once the not-yet-summarized messages older than the protected
window are worth at least a quarter of the window (1024 or 2048 tokens),
the oldest run of them becomes one level-0 summary. A chunk never reaches
the latest user message, the recent window or the latest image, and never
separates a tool call from its result. Editing a message only ever
truncates from the latest user message, so an edit can't invalidate a
summary.

**Manual.** `@compact` (`compactNow`) runs the same steps right away,
with no minimum chunk size, until nothing eligible is left, up to 12
steps per command. It shares the one-pass-per-conversation guard with the
background summarizer, so the two never run together.

**Layers.** When a level holds more than 3 summaries (`summaryFanout`),
its oldest 3 merge into one summary a level up. Old content therefore gets
condensed again only a logarithmic number of times, instead of on every
pass. Merged summaries and all raw messages are kept in the database, so
any summary can be rebuilt.

**Two parts per summary.**

- *What the user said* (`user_notes`) is copied, not generated: each user
  message, clipped to 300 characters. Merges combine these without passing
  them through the model. Only when the combined text outgrows 1/16 of the
  window does the model condense it, on its own, with a prompt that keeps
  every fact, name, number and instruction. The model can re-read a file
  with a tool, but a user fact the summary drops is gone for good. In
  testing, qwen2.5-3b dropped one twice when asked to keep it (E7).
- *What the assistant found and did* is written by the model: file names,
  commands, identifiers, whether each command succeeded, and what is still
  open. The summary uses at most a thirty-second of the window
  (`summaryTokens`).

**Prompting.** The instructions go after the transcript in a single user
message, not in a system prompt. With a 2k-token transcript ahead of it,
qwen2.5-3b ignored a system prompt (E6), the same effect CLAUDE.md records
for the tool nudge. Summaries use the conversation's own model and the
same `num_ctx`, so they never force a model reload.

## Memories in the request

- **Folder memories** go into the text appended to the last message,
  together with the folder anchor and the tool nudge, and they take at
  most an eighth of the window. As a system message after the manifest,
  the model ignored them ([E14](experiments.md#e14-where-folder-memories-go)).
- **`@memory <words>` recalls** are saved as a system message before the
  user's message, like `@web` results. After that they decay like any
  later system message.

## Not built yet

- **Retrieval over summaries.** Every current summary is already sent,
  and their total size is bounded, so there's nothing to choose between
  yet. Retrieval would only help if details lost in merges turn out to
  matter. The cheaper first step would be a deterministic index from file
  names in the attached folder to the messages and summaries that mention
  them, with no model calls and no embeddings.
- **A bigger window.** Memory would allow 32k for qwen2.5-3b on a 6 GB
  GPU ([E12](experiments.md#e12-memory-cost-of-a-bigger-window)). Speed and
  quality at that size aren't measured yet.
