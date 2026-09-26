# Security

Flint is a personal tool for your own machine. It runs model-proposed shell
commands as the user it runs as, and anyone who can reach the page can sign
up, so keep it on localhost (the default, and what Docker Compose publishes).
Accounts exist to keep your own conversations and settings organized, not
to make it safe to share with other people.

## Accounts and sessions

- Passwords and security-question answers are hashed with bcrypt and
  never returned.
- Sessions are random ids stored in SQLite and sent as an `HttpOnly`,
  `SameSite=Lax` cookie. They last 30 days. Logging out deletes the
  session, and a password reset deletes all of that user's sessions.
- Every conversation, message, attachment, command and memory is scoped
  to its owner. Another account's resources return **404, not 403**, so their
  existence isn't revealed.
- Recovery lookups answer the same way whether or not the email exists.
- Deleting an account (`DELETE /api/me`) needs the password, plus every
  security answer when questions are set, so a stolen session alone can't
  erase an account. A wrong password or answer gets the same message. It
  deletes the user row, which cascades to every session, conversation,
  message, command, summary, memory and security question, then removes
  the attachment files from disk.

## Rate limits

An in-memory sliding window per client IP (`r.RemoteAddr`):

| Endpoints | Limit |
|---|---|
| login | 5 per 5 minutes |
| signup | 5 per 5 minutes, separate from login |
| recovery questions + reset (shared) | 5 per 30 minutes |
| account deletion | 5 per 30 minutes |

`X-Forwarded-For` is ignored on purpose. Without a trusted-proxy list,
anyone could set it to get around the limit. Behind a reverse proxy, every
client therefore shares the proxy's IP and its limit.

## Shell commands

Layers, in order:

1. **Human approval.** Every command is held as pending, and its exact
   text is shown unchanged. Nothing runs without an explicit approve.
   This is the real safety net.
2. **Shield** (`shield.go`). Blocked when proposed and again when run:
   `rm -rf` aimed at `/`, `~`, or the whole current directory (`.`, `*`), fork bombs, `sudo`, piping `curl`/`wget`
   into a shell, `mkfs`, `dd` to `/dev/*`, and reading `/etc/shadow` or
   SSH private keys. The model is told to ask you to run a genuinely
   needed blocked command yourself, not to work around the block. This is
   a floor, not a guarantee: a Turing-complete shell can evade any pattern
   list.
3. **Preconditions** (`preconditions.go`). The program must exist, and a
   simple read (`cat`, `head`, `tail`, ...) must target a path that
   exists. Compound commands skip the path check. A failure goes straight
   back to the model, without asking you to approve something that can't
   work.
4. **Serialization.** Every request touching a conversation takes that
   conversation's lock, and approve/deny re-reads the command's status
   inside the lock. A double-clicked approve can't run a command twice.

The working directory is the attached folder, but a command can still
`cd` elsewhere or use absolute paths. Commands time out after 60 seconds,
and their output is capped at 20,000 characters.

## Network

- Flint talks to Ollama, and to Brave Search only for an explicit `@web`
  message using that user's own key. Nothing else leaves the machine.
- The UI is same-origin with the API, so no CORS headers are set.
- Flint listens on `127.0.0.1` by default (`HOST`), and Docker Compose
  keeps it there, because of open signup and the shell tool.
- Model output is rendered as Markdown through DOMPurify, restricted to
  HTML and MathML (no SVG). Rendering must never fetch from the network:
  remote markdown images become links, `<img>` keeps only local or `data:`
  sources, and media tags plus `style`, `srcset`, `poster` and
  `background` are stripped, since any of them could load a remote URL
  (for example, a tracking pixel in a reply).
- Web search sources are shown as links only if they're `http(s)`, so a
  result can't smuggle in a `javascript:` link.

## Memory

A saved memory is sent back to the model in later chats, so a wrong one
keeps misleading it. That's why a model-drafted memory is only saved after
the user has read and approved it, and why memories are only written or
searched on an explicit `@memory` command.

## Reporting a vulnerability

Please report security problems privately through the repository's
**Security → Report a vulnerability** on GitHub, not in a public issue.
That matters especially for anything that gets around the shell-command
safety layers.
