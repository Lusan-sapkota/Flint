# Features

Everything Flint does, grouped by area. Update this page with every
feature change.

## Accounts

- **Open signup**: full name, email, and a password of at least 8
  characters. Sessions last 30 days.
- **Log in and log out.** Logging out ends the session on the server.
- **Private per account.** Each account sees only its own conversations,
  attachments, commands and memories. Another account's items answer
  "not found".
- **Password recovery without email.** Set 2-5 security questions in
  Settings. To recover, enter your email, answer the questions, and choose
  a new password. Answers are compared case- and whitespace-insensitively.
  A reset signs out every session of that account.
- **Rate limiting** on login, signup and recovery (see
  [security.md](security.md)).

## Chat

- **Streaming replies**, rendered as Markdown (sanitized with DOMPurify):
  headings, lists, tables, quotes, links, task lists and strikethrough.
  Code blocks show their language and have their own Copy button, with no
  syntax highlighting. Diagram code such as Mermaid shows as a code block
  you can copy into any viewer.
- **Math** renders offline with Temml as native MathML: `\(…\)` or `$…$`
  inline, `\[…\]` or `$$…$$` as a block. Dollar amounts like "$5 and $10"
  stay plain text, `$` inside code is left alone, and broken LaTeX shows an
  error instead of breaking the reply.
- **No remote content.** Rendering a reply never fetches anything. A
  remote image becomes a link you can open on purpose, and raw HTML from
  the model can't load media, styles or SVG from the network.
- **Copy** any message.
- **Thinking toggle** (the light-bulb button). For models with the
  thinking capability, such as qwen3.5-4b, it switches between reasoning
  first and answering directly. The model's thinking is shown separately
  from its answer. The toggle is disabled for models without thinking.
- **Stop generating.** A stopped reply keeps what was already shown, so
  the history matches the screen. A tool call cut off mid-stream is
  dropped, so it can never be approved.
- **Edit the last message.** This resends it and discards everything
  after it, including a pending command. Its attachments carry over.
- **Generation speed** (tokens per second) under each reply.
- **Context meter.** Next to the folder chip, after a thin divider: a row of thin bars, grey
  for the whole window and bright for how much of it the last request used, and
  the token count ("192 / 4.1k") to its right. Hover it for details. It
  turns the accent color above 80%. It updates after every response,
  including tool steps.
- **Model loading notice.** Shown while Ollama loads a model, instead of
  bare dots.
- **Missing-model badge.** If a chat's model has been removed from
  Ollama, the header says so and points you to Settings or a new chat.
- **Last-activity time** in the chat header.
- **Scrolling tips** under the message box: a slow marquee through
  `@web`, `@memory save`, `@memory <words>` and `@compact`. It pauses on hover and
  shows one static tip if your system asks for reduced motion.
- **Enter** sends, and **Shift+Enter** adds a new line.

## Conversations

- **New chat** with a model picker. It defaults to the model you used
  most recently.
- **Automatic titles.** The first message sets a placeholder right away.
  After the first reply, the model writes a 2-6 word title, unless you've
  already renamed the chat.
- **Rename and delete.** Deleting also removes the chat's attachment
  files.
- **Search across chats** from the sidebar. It matches titles and your
  and the model's messages as you type, and shows a snippet of the match.
- **Sidebar.** It collapses to a narrow rail and remembers that choice.
  The rail has New chat and Search buttons; Search opens the sidebar with
  the cursor in the search box. On phones the sidebar becomes a slide-over
  menu. Hovering a chat shows its full title. Clicking the chat you're
  already in does nothing, rather than reloading it.

## Attachments

- **Files and images** from the attach button or by pasting. Up to 4
  files per message, 8 MB each. Every file is validated before any part
  of the message is saved.
- **Images** (PNG, JPEG, GIF, WebP, BMP) go to vision models. Only the
  most recent message with images keeps sending them. Older ones become a
  note telling the model to ask for a re-send, because one large image can
  use half a small window.
- **Other files** are stored for you to see and download. The model only
  gets a note that a file was attached, and the UI warns that the model
  can't see it.

## Folder attach and the shell tool

- **Attach a folder**, typed or picked with the folder browser (it lists
  subfolder names only, starting at the server user's home). Flint reads
  the files directly inside it (not subfolders), skips binaries and large
  files, and includes up to 12 KB of text as the chat's standing context.
  Files past that are listed as not included.
- **Shell commands with approval.** With a folder attached, the model can
  propose shell commands through Ollama's native tool calling. **Every
  command waits for you to approve or deny it.** Nothing runs
  automatically. The command text is shown exactly as proposed, and it
  runs with the folder as its working directory, which is a convenience,
  not a sandbox.
- **Safety shield.** Catastrophic patterns (`rm -rf /`, `sudo`, piping a
  download into a shell, and so on) are blocked before you ever see them.
- **Precondition check.** A command whose program or read target doesn't
  exist goes back to the model without asking you.
- **Clear results.** Every result states success or failure with the
  exit code, and the output is shown in the chat.
- **Bounded retries.** After 3 tool steps since your last message, the
  model has to answer in text.
- **Live folder listing.** Each turn, a fresh listing of the folder's top
  level goes next to the model's reply point, so it knows what exists.

## Web search (`@web`)

- Start a message with `@web <query>` to search the web through the Brave
  Search API, using your own key from Settings. Flint runs the search,
  not the model, and it sends only the query text.
- The top 10 results are re-ranked by similarity to the query with
  Ollama's `nomic-embed-text`, and the best 3 go into the chat.
- While it runs, the chat shows *Searching the web for "…"*. Afterwards a
  collapsed one-line card above the answer (*Searched the web for "…" ·
  3 sources*) opens to list the results it was based on, as links with
  their site names, so you can check the answer yourself. The
  card is also there after a reload. A small model can confidently state
  wrong facts from memory, and the sources show where the answer really
  came from.
- With no key configured, the flag is stripped from the saved message and
  you're shown a notice. The request still goes through.

## Memory (`@memory`)

Facts you choose to keep across chats. Like `@web`, memory is only used
when you ask for it. The model never decides on its own to save or look
things up.

- **`@memory save <text>`** saves that text exactly as written.
- **`@memory save`** on its own asks the model to draft the lasting facts
  from this chat. The draft appears as a card you can edit, then **Save
  memory** or **Discard**. Nothing is saved until you approve it, because
  a small model's draft can be wrong, and a wrong memory would come back
  in every later chat.
- **`@memory <words>`** searches your memories (any word can match) and
  adds the best matches to this chat, within an eighth of the model's
  window. If nothing matches, you're told.
- **Folder memories.** A memory saved in a chat with a folder attached
  belongs to that folder. It loads automatically in every chat that
  attaches the same folder, newest first, within an eighth of the window.
- **Manage memories** in Settings: read, edit or delete them.
- Memory commands aren't added to the chat history. An edit can't turn a
  message into a memory command.

## Long conversations

A chat never runs out of room, and you never have to compact it yourself,
though you can:

- Older turns are summarized in the background once they add up to a
  quarter of the window. Your own messages are kept word for word inside
  the summaries.
- Until a summary is ready, the oldest messages are left out of that
  request, and the model is told how many. Very large command output is
  cut down so every request fits.
See [context-management.md](context-management.md).

### Compacting by hand (`@compact`)

Send `@compact` as a message to condense the chat right away, instead of
waiting for older messages to fill a quarter of the window.

- **What it does:** summarizes everything eligible, oldest first, and
  merges summaries up a level when one level gets too full, the same way
  the automatic pass does.
- **What it shows:** each step as it happens, such as "condensed 3
  messages into a summary" or "merged 3 older summaries into one", and
  then a closing line.
- **What always stays word for word:** your first message, the folder
  context, the latest tool call and its result, your latest message and
  the last 6 messages. Your own words inside the condensed part are kept
  word for word in the summary.
- **Nothing to do:** if everything older is already summarized, it says
  so and changes nothing.
- **Already running:** if the automatic pass is working on the same chat,
  it says so. Try again a moment later.
- **Limits:** up to 12 steps per command. A very long chat that was never
  compacted may need `@compact` twice.
- **Not part of the chat:** the command and its report aren't saved to
  the chat history, and an edit can't turn a message into `@compact`.
- Nothing is deleted. The original messages stay in the database; the
  summary only replaces them in what the model is sent.

## Settings

- **Ollama URL**: your own, falling back to the server default.
- **Preferred models**, to limit which models the new-chat picker shows.
- **Model management**: see which models are loaded, inspect their
  details, pull new ones with a progress bar, and delete them.
- **Brave Search API key** for `@web`.
- **Memories**: edit and delete.
- **Security questions** for password recovery.

## Running it

- One static binary with SQLite, no build step, no CDN. The frontend
  libraries ship with it.
- A published Docker image (`ghcr.io/lusan-sapkota/flint`) and a Compose
  file that runs Flint in a container using the Ollama
  already on your machine. See [deployment.md](deployment.md).
- `/healthz` checks the database and Ollama.
