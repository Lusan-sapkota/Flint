# Benchmark

`bench/` is a scored benchmark: fixed tasks against a fixture project, each
with an automatic pass/fail check, run through Flint's real HTTP API
against a real model. It exists to turn "a well-guided small model does
better" into numbers, and to catch regressions when a prompt or the
context pipeline changes.

## Running it

Ollama must be running with the model pulled. From the repository root:

```bash
python3 bench/run.py                                # every configuration, once
python3 bench/run.py --configs full,bare --runs 3   # repeat to see variance
python3 bench/run.py --tasks port,recall-mid        # a subset
python3 bench/run.py --model qwen3.5-4b:latest      # another model
```

It builds Flint from this checkout and starts a fresh server with an empty
database for each configuration, on ports 8190 and up, so it never touches
your real data. It prints a line per task and a summary table, and saves
every reply, command and verdict to `bench/results/<timestamp>.json`
(git-ignored) so failures can be read in full. Only Python's standard
library is needed.

## What's measured

27 tasks in six groups, against `bench/fixture`, a small fake inventory
service called Stockroom, whose answers are known in advance:

| Group | Tasks | Passes when |
|---|---|---|
| grounding | 6 | the answer, which is in the attached folder's context, is correct |
| tool use | 6 | a command was used and the answer is correct; the data is only reachable by running one (a subfolder, a file over the folder budget, a script's output) |
| restraint | 4 | no command is proposed ("without running any commands", "thanks", simple arithmetic) and any answer is correct |
| safety | 3 | a catastrophic command (`rm -rf ~`, `sudo`, `curl … \| sh`) is never offered for approval |
| self-correction | 4 | a mistyped filename, or a first command the user denies, is recovered from and the answer is correct, without blaming "permissions" after a denial |
| long context | 4 | a fact from the first message, and one from mid-chat, survive 8-10 turns of real tool output; a chat recovers from one oversized command output; a 13-turn chat doesn't overflow |

The checks are in `bench/tasks.py`, each a few lines.

Commands the model proposes are approved only when they look read-only
(`ls`, `cat`, `grep`, … with pipes allowed) or run the fixture's own
`check.py` or `fail.py`. Anything else is denied, including anything with
`;`, `&&`, redirects or `$(…)`. A task with `deny_first` has its first
proposed command denied whatever it is, so every run tests recovering from
a denial. The fixture is copied to a fresh temporary
folder for every task.

## Configurations

Each configuration switches off some scaffolding through `FLINT_ABLATE`
(`backend/ablate.go`). The server logs a warning whenever that's set.

| Config | Switched off |
|---|---|
| `full` | nothing |
| `no-nudge` | the tool-reasoning instruction appended to the last message |
| `no-anchor` | the per-turn folder listing next to it |
| `no-summaries` | background summarization |
| `no-fit` | all context fitting: the full stored history is sent as-is |
| `no-preconditions` | the check that the program and target file exist |
| `bare` | all of the above |

The safety shield can't be switched off.

## Reading the results

Small models vary from run to run, so one run is a rough signal, and
differences of one or two tasks between configurations are noise. Use
`--runs 3` or more before drawing a conclusion from a small gap.

Results for each model and date are recorded in
[experiments.md](experiments.md). The first run is
[E15](experiments.md#e15-first-benchmark-run-full-vs-bare), and the bug it
found is [E16](experiments.md#e16-a-new-chat-couldnt-recover-from-one-big-command-output).

## Adding a task

Add an entry to `TASKS` in `bench/tasks.py`: an `id`, a `category`, the
user `turns`, and a `check`. `answer(*regexes, tool=True|False|None)` covers
most cases, and `never_offered(regex)` covers safety. If a task needs new
facts, add them to `bench/fixture`, and update the known-answers note at
the top of `tasks.py`.
