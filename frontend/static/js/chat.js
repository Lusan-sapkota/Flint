async function flintLogout() {
  try {
    await fetch('/api/logout', { method: 'POST' });
  } catch (e) {
    // ignore - redirecting anyway
  }
  window.location.href = '/login';
}

async function flintDeleteConversation(id, button) {
  const row = button.closest('.flint-conversation-row');
  const title = row.querySelector('a').textContent;
  if (!(await flintConfirmDelete(`Delete "${title}"?`, 'This permanently deletes the conversation, its messages, and any files attached to it.'))) return;
  const res = await fetch(`/api/conversations/${id}`, { method: 'DELETE' });
  if (!res.ok && res.status !== 404) {
    button.title = 'Could not delete this conversation';
    return;
  }
  if (window.location.pathname === `/c/${id}`) {
    window.location.href = '/';
  } else {
    row.remove();
  }
}

document.addEventListener('alpine:init', () => {
  const TOOL_CALL_MARKER = '<<<TOOL_CALL>>>';
  const TOOL_RESULT_MARKER = '<<<TOOL_RESULT>>>';
  const HOLDBACK = TOOL_CALL_MARKER.length + 8;

  Alpine.data('chatApp', (config) => ({
    conversationId: config.conversationId,
    model: config.model,
    attachedFolder: config.attachedFolder,
    timeline: config.timeline || [],
    input: '',
    attachments: [],
    attachmentError: '',
    streaming: false,
    folderInput: '',
    folderBusy: false,
    folderError: '',
    streamingBubble: null,

    init() {
      this.scrollToBottom();
    },

    get pendingCommand() {
      const last = this.timeline[this.timeline.length - 1];
      return last && last.kind === 'command' && last.commandStatus === 'pending' ? last : null;
    },

    isStreamingBubble(item) {
      return this.streamingBubble === item;
    },

    scrollToBottom() {
      this.$nextTick(() => {
        const el = this.$refs.thread;
        if (el) el.scrollTop = el.scrollHeight;
      });
    },

    onFileChange(e) {
      this.addFiles(e.target.files);
      e.target.value = '';
    },

    // Shared by the file picker and clipboard paste, so both go through the
    // same 4-per-turn cap, size check, and preview construction.
    addFiles(fileList) {
      this.attachmentError = '';
      const files = Array.from(fileList || []);
      if (files.length === 0) return;

      const room = 4 - this.attachments.length;
      if (room <= 0) {
        this.attachmentError = 'Up to 4 attachments per message.';
        return;
      }
      const accepted = files.slice(0, room);
      if (files.length > accepted.length) {
        this.attachmentError = 'Up to 4 attachments per message - some files were skipped.';
      }

      for (const f of accepted) {
        if (f.size > 8 * 1024 * 1024) {
          this.attachmentError = `${f.name} is over the 8 MB attachment limit and was skipped.`;
          continue;
        }
        const isImage = f.type.startsWith('image/');
        const reader = new FileReader();
        reader.onload = () => this.attachments.push({ dataUrl: reader.result, filename: f.name, isImage });
        reader.readAsDataURL(f);
      }
    },

    handlePaste(e) {
      const items = e.clipboardData && e.clipboardData.items;
      if (!items) return;
      const files = [];
      for (const item of items) {
        if (item.kind === 'file') {
          const f = item.getAsFile();
          if (f) files.push(f);
        }
      }
      if (files.length === 0) return; // no file data - let normal text paste happen
      e.preventDefault();
      this.addFiles(files);
    },

    removeAttachment(i) {
      this.attachments.splice(i, 1);
    },

    async attachFolder() {
      const folder = this.folderInput.trim();
      if (!folder || !this.conversationId) return;
      this.folderBusy = true;
      this.folderError = '';
      try {
        const res = await fetch(`/api/conversations/${this.conversationId}/attach`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ folder }),
        });
        const data = await res.json().catch(() => ({}));
        if (!res.ok) {
          this.folderError = data.error || 'Could not attach that folder.';
          return;
        }
        this.attachedFolder = data.folder;
        this.folderInput = '';
        this.timeline.push({
          kind: 'system',
          content: `Attached folder: ${data.folder}\n${(data.files || []).length} file(s) included.`,
        });
        this.scrollToBottom();
      } catch (e) {
        this.folderError = 'Could not reach the server.';
      } finally {
        this.folderBusy = false;
      }
    },

    async send() {
      const content = this.input.trim();
      if (!content || this.streaming || this.pendingCommand || !this.conversationId) return;

      const attachments = this.attachments.map((a) => ({ data: a.dataUrl, filename: a.filename }));
      this.timeline.push({ kind: 'user', content, pendingAttachments: this.attachments });
      this.input = '';
      this.attachments = [];
      this.attachmentError = '';
      this.scrollToBottom();

      this.streaming = true;
      try {
        const res = await fetch(`/api/conversations/${this.conversationId}/messages`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ content, attachments }),
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          this.timeline.push({ kind: 'system', content: `Error: ${data.error || res.statusText}` });
          return;
        }
        await this.consumeStream(res.body, { expectResultMarker: false });
        this.refreshTitle();
      } catch (e) {
        this.timeline.push({ kind: 'system', content: 'Error: could not reach the server.' });
      } finally {
        this.streaming = false;
        this.streamingBubble = null;
      }
    },

    async approve(cmd) {
      await this.decide(cmd, 'approve');
    },

    async deny(cmd) {
      await this.decide(cmd, 'deny');
    },

    async decide(cmd, action) {
      this.streaming = true;
      cmd.commandStatus = 'resolving';
      try {
        const res = await fetch(`/api/conversations/${this.conversationId}/commands/${cmd.commandId}/${action}`, {
          method: 'POST',
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          cmd.commandStatus = 'unknown';
          this.timeline.push({ kind: 'system', content: `Error: ${data.error || res.statusText}` });
          return;
        }
        await this.consumeStream(res.body, { expectResultMarker: true, targetCmd: cmd });
      } catch (e) {
        cmd.commandStatus = 'unknown';
        this.timeline.push({ kind: 'system', content: 'Error: could not reach the server.' });
      } finally {
        this.streaming = false;
        this.streamingBubble = null;
      }
    },

    // Reads a streamed plain-text response token by token, appending it live
    // into a fresh assistant bubble. A literal "<<<TOOL_CALL>>>{json}\n" at
    // the very end of the stream (the server always ends the response right
    // after writing it) is held back rather than shown, then swapped for a
    // command card once the full JSON has arrived. When expectResultMarker
    // is set (an approve/deny continuation), a leading
    // "<<<TOOL_RESULT>>>{json}\n" line is parsed first to fill in what the
    // command actually did, before the rest of the stream is treated the
    // same way.
    async consumeStream(body, { expectResultMarker, targetCmd } = {}) {
      const reader = body.getReader();
      const decoder = new TextDecoder();
      let buf = '';

      const readChunk = async () => {
        const { done, value } = await reader.read();
        if (value) buf += decoder.decode(value, { stream: true });
        return done;
      };

      if (expectResultMarker) {
        while (buf.indexOf('\n') === -1) {
          if (await readChunk()) break;
        }
        const nl = buf.indexOf('\n');
        if (nl !== -1) {
          const line = buf.slice(0, nl);
          buf = buf.slice(nl + 1);
          if (line.startsWith(TOOL_RESULT_MARKER) && targetCmd) {
            try {
              const obj = JSON.parse(line.slice(TOOL_RESULT_MARKER.length));
              targetCmd.commandStatus = obj.status;
              targetCmd.commandResult = obj.output;
            } catch (e) {
              targetCmd.commandStatus = 'unknown';
            }
          }
        } else if (targetCmd) {
          // stream ended before the result marker ever arrived (e.g. a
          // dropped connection) - don't leave the card stuck on "Running…"
          targetCmd.commandStatus = 'unknown';
        }
      }

      let pending = buf;
      buf = '';
      let markerFound = false;
      let bubble = null;

      const ensureBubble = () => {
        if (!bubble) {
          this.timeline.push({ kind: 'assistant', content: '' });
          bubble = this.timeline[this.timeline.length - 1];
          this.streamingBubble = bubble;
        }
        return bubble;
      };
      const appendVisible = (text) => {
        if (!text) return;
        ensureBubble().content += text;
        this.scrollToBottom();
      };

      while (true) {
        if (!markerFound) {
          const idx = pending.indexOf(TOOL_CALL_MARKER);
          if (idx !== -1) {
            markerFound = true;
            appendVisible(pending.slice(0, idx));
            pending = pending.slice(idx);
          } else if (pending.length > HOLDBACK) {
            const safeLen = pending.length - HOLDBACK;
            appendVisible(pending.slice(0, safeLen));
            pending = pending.slice(safeLen);
          }
        }
        const done = await readChunk();
        pending += buf;
        buf = '';
        if (done) break;
      }

      if (!markerFound) {
        const idx = pending.indexOf(TOOL_CALL_MARKER);
        if (idx !== -1) {
          markerFound = true;
          appendVisible(pending.slice(0, idx));
          pending = pending.slice(idx);
        }
      }

      this.streamingBubble = null;

      if (markerFound) {
        const jsonPart = pending.slice(TOOL_CALL_MARKER.length).trim();
        try {
          const obj = JSON.parse(jsonPart);
          if (bubble && bubble.content.trim() === '') {
            this.timeline.splice(this.timeline.indexOf(bubble), 1);
          }
          this.timeline.push({ kind: 'command', commandId: obj.id, commandText: obj.command, commandStatus: 'pending' });
        } catch (e) {
          appendVisible('\n' + pending);
        }
      } else {
        appendVisible(pending);
        if (bubble && bubble.content.trim() === '') {
          this.timeline.splice(this.timeline.indexOf(bubble), 1);
        }
      }
      this.scrollToBottom();
    },

    async refreshTitle() {
      try {
        const res = await fetch(`/api/conversations/${this.conversationId}`);
        if (!res.ok) return;
        const data = await res.json();
        const link = document.querySelector(`[data-conversation-link="${this.conversationId}"]`);
        if (link) link.textContent = data.title;
      } catch (e) {
        // non-critical - the sidebar just keeps its stale title until reload
      }
    },

    submitOnEnter(e) {
      if (e.shiftKey) return;
      e.preventDefault();
      this.send();
    },

    statusLabel(status) {
      switch (status) {
        case 'success':
          return 'Succeeded';
        case 'failed':
          return 'Failed';
        case 'denied':
          return 'Denied';
        case 'blocked':
          return 'Blocked by safety shield';
        case 'precondition_failed':
          return 'Precondition failed';
        case 'pending':
          return 'Awaiting your approval';
        case 'resolving':
          return 'Running…';
        default:
          return 'Unknown';
      }
    },
  }));
});
