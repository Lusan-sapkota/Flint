window.FLINT_SEARCH_INDEX = [
  {
    "page": "Overview",
    "url": "index.html",
    "title": "Overview",
    "group": "Get started",
    "snippet": "Offline chat UI for local Ollama models"
  },
  {
    "page": "Overview",
    "url": "index.html#quick-start",
    "title": "Quick start",
    "group": "Get started",
    "snippet": "You need Ollama running with a model pulled, for\nexample ollama pull qwen2.5:3b."
  },
  {
    "page": "Overview",
    "url": "index.html#the-docs",
    "title": "The docs",
    "group": "Get started",
    "snippet": "Get started"
  },
  {
    "page": "Overview",
    "url": "index.html#screenshots",
    "title": "Screenshots",
    "group": "Get started",
    "snippet": ""
  },
  {
    "page": "Overview",
    "url": "index.html#elsewhere",
    "title": "Elsewhere",
    "group": "Get started",
    "snippet": "Every change to Flint updates the pages it affects, in the same commit."
  },
  {
    "page": "Install and run",
    "url": "deployment.html",
    "title": "Install and run",
    "group": "Get started",
    "snippet": "Docker, source, environment variables, health checks"
  },
  {
    "page": "Install and run",
    "url": "deployment.html#docker",
    "title": "Docker",
    "group": "Get started",
    "snippet": "Docker runs Flint only. Ollama stays the normal app on your machine,\nwhere it already has your GPU and your models, and the container talks\n"
  },
  {
    "page": "Install and run",
    "url": "deployment.html#from-source",
    "title": "From source",
    "group": "Get started",
    "snippet": "Requires Go (see backend/go.mod) and a running Ollama."
  },
  {
    "page": "Install and run",
    "url": "deployment.html#environment-variables",
    "title": "Environment variables",
    "group": "Get started",
    "snippet": "A new image is published for every release, for linux/amd64 and\nlinux/arm64:"
  },
  {
    "page": "Install and run",
    "url": "deployment.html#image-tags",
    "title": "Image tags",
    "group": "Get started",
    "snippet": "A new image is published for every release, for linux/amd64 and\nlinux/arm64:"
  },
  {
    "page": "Install and run",
    "url": "deployment.html#updates",
    "title": "Updates & offline philosophy",
    "group": "Get started",
    "snippet": "Flint has a strict zero-telemetry, offline-first design. It never checks for updates in the background or sends analytics."
  },
  {
    "page": "Install and run",
    "url": "deployment.html#health",
    "title": "Health",
    "group": "Get started",
    "snippet": "GET /healthz checks the database and Ollama's /api/version. It returns\n200 with {\"db\":\"ok\",\"ollama\":\"ok\"}, or 503 naming what failed."
  },
  {
    "page": "Install and run",
    "url": "deployment.html#shutdown",
    "title": "Shutdown",
    "group": "Get started",
    "snippet": "SIGINT and SIGTERM stop accepting requests and give in-flight ones up to\n10 seconds to finish. A background summarization cut short by shutd"
  },
  {
    "page": "Install and run",
    "url": "deployment.html#models",
    "title": "Models",
    "group": "Get started",
    "snippet": "Anything Ollama serves works for chat. What Flint uses:"
  },
  {
    "page": "Features",
    "url": "features.html",
    "title": "Features",
    "group": "Using Flint",
    "snippet": "Chats, tools, agents, memory, web search, and settings"
  },
  {
    "page": "Features",
    "url": "features.html#accounts",
    "title": "Accounts",
    "group": "Using Flint",
    "snippet": "Facts you choose to keep across chats. Like @web, memory is only used\nwhen you ask for it. The model never decides on its own to save or loo"
  },
  {
    "page": "Features",
    "url": "features.html#chat",
    "title": "Chat",
    "group": "Using Flint",
    "snippet": "Facts you choose to keep across chats. Like @web, memory is only used\nwhen you ask for it. The model never decides on its own to save or loo"
  },
  {
    "page": "Features",
    "url": "features.html#conversations",
    "title": "Conversations",
    "group": "Using Flint",
    "snippet": "Facts you choose to keep across chats. Like @web, memory is only used\nwhen you ask for it. The model never decides on its own to save or loo"
  },
  {
    "page": "Features",
    "url": "features.html#attachments",
    "title": "Attachments",
    "group": "Using Flint",
    "snippet": "Facts you choose to keep across chats. Like @web, memory is only used\nwhen you ask for it. The model never decides on its own to save or loo"
  },
  {
    "page": "Features",
    "url": "features.html#folder-attach-and-the-shell-tool",
    "title": "Folder attach and the shell tool",
    "group": "Using Flint",
    "snippet": "Facts you choose to keep across chats. Like @web, memory is only used\nwhen you ask for it. The model never decides on its own to save or loo"
  },
  {
    "page": "Features",
    "url": "features.html#web-search-web",
    "title": "Web search (@web)",
    "group": "Using Flint",
    "snippet": "Facts you choose to keep across chats. Like @web, memory is only used\nwhen you ask for it. The model never decides on its own to save or loo"
  },
  {
    "page": "Features",
    "url": "features.html#memory-memory",
    "title": "Memory (@memory)",
    "group": "Using Flint",
    "snippet": "Facts you choose to keep across chats. Like @web, memory is only used\nwhen you ask for it. The model never decides on its own to save or loo"
  },
  {
    "page": "Features",
    "url": "features.html#agents-agent",
    "title": "Agents (@agent)",
    "group": "Using Flint",
    "snippet": "@agent <task> splits a task into independent subtasks, each run by a\nseparate agent with its own clean context window, then combines their\nr"
  },
  {
    "page": "Features",
    "url": "features.html#long-conversations",
    "title": "Long conversations",
    "group": "Using Flint",
    "snippet": "A chat never runs out of room, and you never have to compact it yourself,\nthough you can:"
  },
  {
    "page": "Features",
    "url": "features.html#compacting-by-hand-compact",
    "title": "Compacting by hand (@compact)",
    "group": "Using Flint",
    "snippet": "Send @compact as a message to condense the chat right away, instead of\nwaiting for older messages to fill a quarter of the window."
  },
  {
    "page": "Features",
    "url": "features.html#settings",
    "title": "Settings",
    "group": "Using Flint",
    "snippet": "Laid out like the chat page: a sidebar of sections on the left and one\nsection at a time on the right. On phones the sections become a row o"
  },
  {
    "page": "Features",
    "url": "features.html#running-it",
    "title": "Running it",
    "group": "Using Flint",
    "snippet": ""
  },
  {
    "page": "Security",
    "url": "security.html",
    "title": "Security",
    "group": "Using Flint",
    "snippet": "Accounts, rate limits, command shield, and preconditions"
  },
  {
    "page": "Security",
    "url": "security.html#accounts-and-sessions",
    "title": "Accounts and sessions",
    "group": "Using Flint",
    "snippet": "An in-memory sliding window per client IP (r.RemoteAddr):"
  },
  {
    "page": "Security",
    "url": "security.html#rate-limits",
    "title": "Rate limits",
    "group": "Using Flint",
    "snippet": "An in-memory sliding window per client IP (r.RemoteAddr):"
  },
  {
    "page": "Security",
    "url": "security.html#shell-commands",
    "title": "Shell commands",
    "group": "Using Flint",
    "snippet": "Layers, in order:"
  },
  {
    "page": "Security",
    "url": "security.html#network",
    "title": "Network",
    "group": "Using Flint",
    "snippet": "A saved memory is sent back to the model in later chats, so a wrong one\nkeeps misleading it. That's why a model-drafted memory is only saved"
  },
  {
    "page": "Security",
    "url": "security.html#memory",
    "title": "Memory",
    "group": "Using Flint",
    "snippet": "A saved memory is sent back to the model in later chats, so a wrong one\nkeeps misleading it. That's why a model-drafted memory is only saved"
  },
  {
    "page": "Security",
    "url": "security.html#agents",
    "title": "Agents",
    "group": "Using Flint",
    "snippet": "Please report security problems privately through the repository's\nSecurity \u2192 Report a vulnerability on GitHub, not in a public issue.\nThat "
  },
  {
    "page": "Security",
    "url": "security.html#reporting-a-vulnerability",
    "title": "Reporting a vulnerability",
    "group": "Using Flint",
    "snippet": "Please report security problems privately through the repository's\nSecurity \u2192 Report a vulnerability on GitHub, not in a public issue.\nThat "
  },
  {
    "page": "Architecture",
    "url": "architecture.html",
    "title": "Architecture",
    "group": "How it works",
    "snippet": "Architecture, data model, chat turns, stream protocol"
  },
  {
    "page": "Architecture",
    "url": "architecture.html#the-bet",
    "title": "The bet",
    "group": "How it works",
    "snippet": "A well-guided 3-4B local model isn't an inferior model, just an\nunder-scaffolded one. Flint's work goes into the scaffolding around it:\ncare"
  },
  {
    "page": "Architecture",
    "url": "architecture.html#orchestration-layer",
    "title": "An orchestration layer, not just a chat UI",
    "group": "How it works",
    "snippet": "Flint is an active orchestration engine that sits between the human, the local filesystem, and Ollama, coordinating four essential subsystems."
  },
  {
    "page": "Architecture",
    "url": "architecture.html#why-not-generic-agent-clis",
    "title": "Why not generic agent CLIs or frontier wrappers?",
    "group": "How it works",
    "snippet": "Existing agent CLIs are built and prompt-tuned for frontier models. When pointed at a 3B-4B local model, they break down in practice."
  },
  {
    "page": "Architecture",
    "url": "architecture.html#resource-footprint",
    "title": "Resource footprint & philosophy",
    "group": "How it works",
    "snippet": "23 MB container vs. multi-GB stacks. Pure Go & SQLite with zero build step, leaving memory for Ollama and model weights."
  },
  {
    "page": "Architecture",
    "url": "architecture.html#repository-layout",
    "title": "Repository layout",
    "group": "How it works",
    "snippet": "Deleting a conversation cascades to its messages, attachments, commands,\nsummaries and agent runs (with their agents and transcripts); a mem"
  },
  {
    "page": "Architecture",
    "url": "architecture.html#data-model",
    "title": "Data model",
    "group": "How it works",
    "snippet": "Deleting a conversation cascades to its messages, attachments, commands,\nsummaries and agent runs (with their agents and transcripts); a mem"
  },
  {
    "page": "Architecture",
    "url": "architecture.html#a-chat-turn",
    "title": "A chat turn",
    "group": "How it works",
    "snippet": "Replies stream as text/plain: the model's tokens, plus marker lines the\nclient consumes:"
  },
  {
    "page": "Architecture",
    "url": "architecture.html#an-agent-run",
    "title": "An @agent run",
    "group": "How it works",
    "snippet": "Replies stream as text/plain: the model's tokens, plus marker lines the\nclient consumes:"
  },
  {
    "page": "Architecture",
    "url": "architecture.html#streaming-protocol",
    "title": "Streaming protocol",
    "group": "How it works",
    "snippet": "Replies stream as text/plain: the model's tokens, plus marker lines the\nclient consumes:"
  },
  {
    "page": "Architecture",
    "url": "architecture.html#fixed-decisions",
    "title": "Fixed decisions",
    "group": "How it works",
    "snippet": "The reasons behind the settled choices (pure-Go SQLite, a single\nconnection, cookie sessions instead of JWT, 404 instead of 403, no CORS,\nna"
  },
  {
    "page": "Architecture",
    "url": "architecture.html#where-the-ideas-come-from",
    "title": "Where the ideas come from",
    "group": "How it works",
    "snippet": "Each borrows from research, scaled down to what a small local tool can\nhonestly claim:"
  },
  {
    "page": "Architecture",
    "url": "architecture.html#deliberately-not-built",
    "title": "Deliberately not built",
    "group": "How it works",
    "snippet": "Neither should come back without solving the problem that stopped it."
  },
  {
    "page": "Context management",
    "url": "context-management.html",
    "title": "Context management",
    "group": "How it works",
    "snippet": "Budgeting, fitting, layered summaries, and verbatim retention"
  },
  {
    "page": "Context management",
    "url": "context-management.html#the-request-budget",
    "title": "The request budget",
    "group": "How it works",
    "snippet": "Every request sets num_ctx explicitly (numCtxFor in context.go). A\nrequest without it makes Ollama reload the model at its server default,\na"
  },
  {
    "page": "Context management",
    "url": "context-management.html#what-always-goes-to-the-model-verbatim",
    "title": "What always goes to the model verbatim",
    "group": "How it works",
    "snippet": "The folder manifest is capped at 12 KB (maxAttachTotalSize), about 3.6k\ntokens of Go code, so it fits well inside the 8192 window (E4)."
  },
  {
    "page": "Context management",
    "url": "context-management.html#fitting-everything-else-cheapest-loss-first",
    "title": "Fitting everything else, cheapest loss first",
    "group": "How it works",
    "snippet": "buildOptimizedHistory does these steps in order:"
  },
  {
    "page": "Context management",
    "url": "context-management.html#message-size-limit",
    "title": "Message size limit",
    "group": "How it works",
    "snippet": "A user message is never truncated, so one too big to fit is refused before it is saved."
  },
  {
    "page": "Context management",
    "url": "context-management.html#summaries",
    "title": "Summaries",
    "group": "How it works",
    "snippet": "summary.go. These run in the background after a reply has finished\nstreaming, so they never delay or hold open a response. History built\nbef"
  },
  {
    "page": "Context management",
    "url": "context-management.html#memories-in-the-request",
    "title": "Memories in the request",
    "group": "How it works",
    "snippet": ""
  },
  {
    "page": "Context management",
    "url": "context-management.html#not-built-yet",
    "title": "Not built yet",
    "group": "How it works",
    "snippet": ""
  },
  {
    "page": "Experiments",
    "url": "experiments.html",
    "title": "Experiments",
    "group": "Evidence",
    "snippet": "Measurements E1\u2013E25 behind every design decision"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e1-what-ollama-does-when-a-prompt-is-too-big",
    "title": "E1: What Ollama does when a prompt is too big",
    "group": "Evidence",
    "snippet": "A system message holding a code word, a filler user message of about\n10.6k tokens, then a question about the code word. Sent with\nnum_ctx: 2"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e2-the-default-window",
    "title": "E2: The default window",
    "group": "Evidence",
    "snippet": "Changed: every request, including title generation and\nsummarization, sets num_ctx. Otherwise requests would reload the model\nback and forth"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e3-how-many-tokens-an-image-costs",
    "title": "E3: How many tokens an image costs",
    "group": "Evidence",
    "snippet": "On qwen3.5-4b, prompt_eval_count with one image minus without:"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e4-the-folder-manifest-was-bigger-than-the-window",
    "title": "E4: The folder manifest was bigger than the window",
    "group": "Evidence",
    "snippet": "The first long-chat run failed on every turn: request (12168 tokens)\nexceeds the available context size (8192 tokens). The 40 KB folder\nmani"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e5-truncated-replies-teach-the-model-to-truncate",
    "title": "E5: Truncated replies teach the model to truncate",
    "group": "Evidence",
    "snippet": "From about the fifth turn of a run, every reply ended in \"...[truncated]\"\nand came back in about 0.5 s. The stored replies showed the model "
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e6-where-the-summary-instructions-go",
    "title": "E6: Where the summary instructions go",
    "group": "Evidence",
    "snippet": "Summarizing the same 34-message chunk (with the prompt as a system\nmessage unless noted):"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e7-keeping-what-the-user-said",
    "title": "E7: Keeping what the user said",
    "group": "Evidence",
    "snippet": "The test fact is planted in turn 2, not in the protected first message:\nstaging runs on port 9123, my manager is Tom Okafor, never touch the"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e8-summaries-removed-the-tool-call-examples",
    "title": "E8: Summaries removed the tool-call examples",
    "group": "Evidence",
    "snippet": "Tool calls per turn (turns 0-18) across long-chat runs:"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e9-a-tool-nudge-that-allows-answering-without-tools-rejected",
    "title": "E9: A tool nudge that allows answering without tools (rejected)",
    "group": "Evidence",
    "snippet": "The last recall question (\"Without running any commands: what port is\nstaging on...\") went to grep even when the summary held the answer. Th"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e10-protected-messages-can-overflow-on-their-own",
    "title": "E10: Protected messages can overflow on their own",
    "group": "Evidence",
    "snippet": "After E8's fix, one run had 3 overflow errors (9122, 9291 and 10665\ntokens against 8192). The baseline had 1 (8531). The recent window and\nt"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e11-verbatim-user-notes-through-a-merge",
    "title": "E11: Verbatim user notes through a merge",
    "group": "Evidence",
    "snippet": "The first full run after the E7 change:"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e12-memory-cost-of-a-bigger-window",
    "title": "E12: Memory cost of a bigger window",
    "group": "Evidence",
    "snippet": "Each model loaded fresh at each num_ctx, with the size Ollama reports\n(/api/ps) and total GPU memory in use (nvidia-smi, 6 GB card, about\n0."
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e13-drafting-a-memory-from-a-chat",
    "title": "E13: Drafting a memory from a chat",
    "group": "Evidence",
    "snippet": "@memory save on a two-message chat: \"We're planning Project Astra.\nDecision: we use Postgres 16, not MySQL. I prefer answers under 100\nwords"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e14-where-folder-memories-go",
    "title": "E14: Where folder memories go",
    "group": "Evidence",
    "snippet": "A memory saved in one folder chat (\"always run go vet before tagging a\nrelease\"), then \"Without running any commands: what is the deploy rul"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e15-first-benchmark-run-full-vs-bare",
    "title": "E15: First benchmark run, full vs bare",
    "group": "Evidence",
    "snippet": "The first run of bench/ (benchmark.md) on\nqwen2.5-3b-instruct, one run per task. full has all scaffolding on;\nbare switches off the nudge, a"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e16-a-new-chat-couldnt-recover-from-one-big-command-output",
    "title": "E16: A new chat couldn't recover from one big command output",
    "group": "Evidence",
    "snippet": "full's long-context tasks all ended in exceeds the available context\nsize (12,628 tokens against 8192). Context fitting works from a token\ne"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e17-the-tool-nudge-is-left-off-when-the-user-says-not-to-run-commands",
    "title": "E17: The tool nudge is left off when the user says not to run commands",
    "group": "Evidence",
    "snippet": "E15 pointed at the tool nudge as the cause of the restraint failures. This\nis the --runs 3 measurement, on qwen2.5-3b-instruct with the fixt"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e18-a-denied-command-made-the-model-claim-it-lacked-permissions",
    "title": "E18: A denied command made the model claim it lacked permissions",
    "group": "Evidence",
    "snippet": "After a denied command, qwen2.5-3b often said \"I don't have the necessary\npermissions\" and gave up, or made up the command's output (E15, an"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e19-asked-to-run-something-in-a-chat-without-a-folder",
    "title": "E19: Asked to run something in a chat without a folder",
    "group": "Evidence",
    "snippet": "The shell tool is only offered once a folder is attached. In a chat\nwithout one, asked \"can you run the command to check it?\" after a versio"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e20-which-embedding-model-re-ranks-web-results-best",
    "title": "E20: Which embedding model re-ranks @web results best",
    "group": "Evidence",
    "snippet": "@web re-ranks Brave's top 10 by cosine similarity to the query and\nkeeps 3. Asked whether a smaller model than nomic-embed-text (274 MB)\nwou"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e21-web-stated-a-month-old-version-as-the-latest",
    "title": "E21: @web stated a month-old version as the latest",
    "group": "Evidence",
    "snippet": "@web what is the latest Ollama release? answered 0.33.x on 2026-09-26,\nwhen GitHub's latest stable was 0.34.4 (v0.40.0 existed only as an rc"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e22-qwen35-refuses-a-system-message-that-isnt-first",
    "title": "E22: qwen3.5 refuses a system message that isn't first",
    "group": "Evidence",
    "snippet": "@memory lcore on qwen3.5-4b failed with Ollama 500: \"Jinja Exception:\nSystem message must be at the beginning\". Its template has an explicit"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e23-the-window-size-for-cloud-models-and-bigger-gpus",
    "title": "E23: The window size for cloud models and bigger GPUs",
    "group": "Evidence",
    "snippet": "A chat with nemotron-3-ultra:cloud showed \"157 / 4.1k\": the fixed\n4096/8192 window was meant for a 6 GB GPU, and a cloud model reports\nconte"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e24-concurrency-and-planning-for-agent-before-any-code",
    "title": "E24: Concurrency and planning for @agent, before any code",
    "group": "Evidence",
    "snippet": "Measured before designing @agent, which runs subtasks as separate\nmodel calls. RTX 4050 Laptop (6 GB, ~900 MiB used by the desktop),\nOllama "
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e25-agent-against-a-normal-chat",
    "title": "E25: @agent against a normal chat",
    "group": "Evidence",
    "snippet": "Four split tasks added to the benchmark (independent questions about\ndifferent files; see benchmark.md), run as a normal chat,\nfull configur"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e26-an-agent-with-no-files-searched-for-a-file-name",
    "title": "E26: An agent with no files searched for a file name",
    "group": "Evidence",
    "snippet": "An agent given no files guessed file names instead of searching their contents; a grep with no match now says so."
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e27-file-names-in-an-answer-checked-against-the-folder",
    "title": "E27: File names in an answer, checked against the folder",
    "group": "Evidence",
    "snippet": "File names in a folder-chat answer that aren't in the folder are listed under the reply; 9 of 9 flags were invented paths."
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e28-telling-the-model-its-last-answer-named-missing-files",
    "title": "E28: Telling the model its last answer named missing files",
    "group": "Evidence",
    "snippet": "A note in the next request listing invented file names: building on an invented file fell from 4/7 to 0/8."
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e29-real-file-paths-in-the-anchor",
    "title": "E29: Real file paths in the anchor",
    "group": "Evidence",
    "snippet": "Listing the folder's real file paths in the anchor: right file 3/30 -> 29/30, invented names 13/30 -> 1/30."
  },
  {
    "page": "Experiments",
    "url": "experiments.html#e30-read-write-and-edit-tools",
    "title": "E30: Read, write and edit tools",
    "group": "Evidence",
    "snippet": "read_file without approval, edits as an approved diff; old_text/new_text beat line ranges 19/30 to 14/30."
  },
  {
    "page": "Security",
    "url": "security.html#file-tools",
    "title": "File tools",
    "group": "Using Flint",
    "snippet": "Reads run without approval with a local model, confined to the folder; writes and edits need approval as a diff, and a stale edit is refused."
  },
  {
    "page": "Features",
    "url": "features.html#why-edits-are-one-file-at-a-time",
    "title": "Why edits are one file at a time",
    "group": "Using Flint",
    "snippet": "Why there's no multi-file review card or hunk-by-hunk accept yet, and what Edit on the diff card does instead."
  },
  {
    "page": "Experiments",
    "url": "experiments.html#baseline-before-any-agent-code",
    "title": "Baseline, before any agent code",
    "group": "Evidence",
    "snippet": "Four split tasks added to the benchmark (independent questions about\ndifferent files; see benchmark.md), run as a normal chat,\nfull configur"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#while-building-the-agents",
    "title": "While building the agents",
    "group": "Evidence",
    "snippet": "Found live on the fixture, before the benchmark:"
  },
  {
    "page": "Experiments",
    "url": "experiments.html#open-questions",
    "title": "Open questions",
    "group": "Evidence",
    "snippet": ""
  },
  {
    "page": "Benchmark",
    "url": "benchmark.html",
    "title": "Benchmark",
    "group": "Evidence",
    "snippet": "Scored benchmark tasks and ablation results"
  },
  {
    "page": "Benchmark",
    "url": "benchmark.html#running-it",
    "title": "Running it",
    "group": "Evidence",
    "snippet": "Ollama must be running with the model pulled. From the repository root:"
  },
  {
    "page": "Benchmark",
    "url": "benchmark.html#whats-measured",
    "title": "What's measured",
    "group": "Evidence",
    "snippet": "35 tasks in eight groups, against bench/fixture, a small fake inventory\nservice called Stockroom, whose answers are known in advance:"
  },
  {
    "page": "Benchmark",
    "url": "benchmark.html#configurations",
    "title": "Configurations",
    "group": "Evidence",
    "snippet": "Each configuration switches off some scaffolding through FLINT_ABLATE\n(backend/ablate.go). The server logs a warning whenever that's set."
  },
  {
    "page": "Benchmark",
    "url": "benchmark.html#reading-the-results",
    "title": "Reading the results",
    "group": "Evidence",
    "snippet": "Small models vary from run to run, so one run is a rough signal, and\ndifferences of one or two tasks between configurations are noise. Use\n-"
  },
  {
    "page": "Benchmark",
    "url": "benchmark.html#adding-a-task",
    "title": "Adding a task",
    "group": "Evidence",
    "snippet": "Add an entry to TASKS in bench/tasks.py: an id, a category, the\nuser turns, and a check. answer(*regexes, tool=True|False|None) covers\nmost "
  },
  {
    "page": "Testing",
    "url": "testing.html",
    "title": "Testing",
    "group": "Evidence",
    "snippet": "Unit tests, live verification, long chats, and re-ranking"
  },
  {
    "page": "Testing",
    "url": "testing.html#unit-tests",
    "title": "Unit tests",
    "group": "Evidence",
    "snippet": "These need no Ollama and no network. context_test.go and\nsummary_test.go cover the context pipeline: the budget, summaries\nreplacing the mes"
  },
  {
    "page": "Testing",
    "url": "testing.html#rendering",
    "title": "Rendering",
    "group": "Evidence",
    "snippet": "Markdown, math and the no-remote-content rule run in the browser, so\nthere's no Go test for them. They were checked by rendering these input"
  },
  {
    "page": "Testing",
    "url": "testing.html#live-checks",
    "title": "Live checks",
    "group": "Evidence",
    "snippet": "backend/live_test.go is behind the live build tag, so go test ./...\nnever runs it. It needs Ollama running and a copy of a Flint database\nwi"
  },
  {
    "page": "Testing",
    "url": "testing.html#web-re-ranking",
    "title": "Web re-ranking",
    "group": "Evidence",
    "snippet": "docs/tools/web_rerank.py scores embedding models and prefixes for\n@web re-ranking on 15 saved, hand-graded Brave searches\n(web_rerank_result"
  },
  {
    "page": "Testing",
    "url": "testing.html#the-long-chat-run",
    "title": "The long-chat run",
    "group": "Evidence",
    "snippet": "docs/tools/long_chat.py drives one 19-turn chat with a folder attached\nthrough the real HTTP API and reports tool calls per turn, peak conte"
  },
  {
    "page": "Testing",
    "url": "testing.html#using-your-installed-models-without-sudo",
    "title": "Using your installed models without sudo",
    "group": "Evidence",
    "snippet": "The system Ollama service keeps its models in\n/usr/share/ollama/.ollama/models. That folder is world-readable, so a\nsecond Ollama started as"
  },
  {
    "page": "Changelog",
    "url": "changelog.html",
    "title": "Changelog",
    "group": "Project",
    "snippet": "Release history and version comparisons"
  },
  {
    "page": "Changelog",
    "url": "changelog.html#020---2026-09-27",
    "title": "0.2.0 - 2026-09-27",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#added",
    "title": "Added",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#changed",
    "title": "Changed",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#014---2026-09-26",
    "title": "0.1.4 - 2026-09-26",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#added_1",
    "title": "Added",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#changed_1",
    "title": "Changed",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#fixed",
    "title": "Fixed",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#013---2026-09-26",
    "title": "0.1.3 - 2026-09-26",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#added_2",
    "title": "Added",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#changed_2",
    "title": "Changed",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#fixed_1",
    "title": "Fixed",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#012---2026-09-26",
    "title": "0.1.2 - 2026-09-26",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#changed_3",
    "title": "Changed",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#fixed_2",
    "title": "Fixed",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#011---2026-09-26",
    "title": "0.1.1 - 2026-09-26",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#changed_4",
    "title": "Changed",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#added_3",
    "title": "Added",
    "group": "Project",
    "snippet": "First release."
  },
  {
    "page": "Changelog",
    "url": "changelog.html#010---2026-09-26",
    "title": "0.1.0 - 2026-09-26",
    "group": "Project",
    "snippet": "First release."
  }
];
