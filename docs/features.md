# Features

## Accounts

- Open signup: full name, email, and a password of at least 8 characters.
  Sessions last 30 days.
- Each account sees only its own conversations, attachments and commands.
- **Password recovery without email.** Set 2-5 security questions in
  Settings. To recover, enter your email, answer the questions, and choose
  a new password. Answers are compared case- and whitespace-insensitively.
  A reset signs out every session of that account.

## Chat

- Streaming replies, with Markdown rendered through DOMPurify.
- **Thinking.** For models that report the capability (such as
  qwen3.5-4b), a toggle shows or hides the model's thinking, separately
  from its answer.
- **Stop.** A stopped reply keeps what was already shown, so the history
  matches the screen. A tool call cut off mid-stream is dropped, so it can
  never be approved.
- **Edit the last message.** This resends it and discards everything
  after it, including a pending command. Its attachments carry over.
- **Titles.** The first message sets a placeholder right away. After the
  first reply, the model writes a 2-6 word title, unless you've already
  renamed the chat.
- **Search across chats** from the sidebar. It matches titles and your
  and the model's messages, as you type.
- **Context bar.** Shows how much of the model's window the last request
  used. It updates after every response, including tool steps.
- **Model loading hint.** Shows when a model is being loaded rather than
  thinking.
- Rename and delete chats. Deleting removes the attachment files too.

## Attachments

- Up to 4 files per message, 8 MB each. Every file is validated before
  any part of the message is saved.
- Images (PNG, JPEG, GIF, WebP, BMP) go to vision models. Only the most
  recent message with images keeps sending them. Older ones become a note
  telling the model to ask for a re-send, because a single large image
  can use half a small window.
- Other files are stored for you to see and download. The model only
  gets a note that a file was attached, and the UI warns that the model
  can't see it.

## Folder attach and the shell tool

- Attach a folder, typed or picked with the server-side folder browser
  (subfolder names only, starting at the server user's home). Flint reads
  the files directly inside it (not subfolders), skips binaries and large
  files, and includes up to 12 KB of text as the conversation's standing
  context. Files past that are listed as not included.
- With a folder attached, the model can propose shell commands through
  Ollama's native tool calling. **Every command waits for you to approve
  or deny it.** Nothing runs automatically. The command runs with the
  folder as its working directory, which is a convenience, not a sandbox.
- Before a command reaches you, the shield blocks a short list of
  catastrophic patterns outright, and the precondition check rejects
  commands whose program or read target doesn't exist. See
  [security.md](security.md).
- Results always state success or failure with the exit code. After 3
  tool steps since your last message, the model has to answer in text.
- Each turn, a fresh listing of the folder's top level goes next to the
  model's reply point, so it knows what exists.

## Web search (`@web`)

- Starting a message with `@web <query>` searches the web through the
  Brave Search API with your own key (set in Settings). The search is
  triggered by Flint, not decided by the model, and it only sends the
  query text.
- The top 10 results are re-ranked by similarity to the query using
  Ollama's `nomic-embed-text`, and the best 3 go into the conversation.
- With no key configured, the `@web` flag is stripped from the saved
  message and you're shown a notice. The request doesn't fail.

## Settings

- Your own Ollama URL, falling back to the server default.
- Preferred models, to limit which models the new-chat picker shows.
- Pull, inspect and delete Ollama models, and see which are loaded.
- Your Brave Search API key.
- Security questions for recovery.

## Long conversations

Flint keeps a conversation inside the model's window without losing its
thread. It summarizes older turns in the background, keeps your own
words verbatim, and drops or cuts only what matters least. See
[context-management.md](context-management.md).
