async function flintDeleteConversation(id, button) {
  const row = button.closest('.flint-conversation-row');
  const title = row.querySelector('a').textContent;
  if (!(await flintAsk(`Delete "${title}"?`, 'This permanently deletes the conversation, its messages, and any files attached to it.'))) return;
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

function flintEscapeHTML(s) {
  return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
}

// navigator.clipboard only exists in secure contexts; Flint is often
// reached over plain http on a LAN address, so fall back to execCommand.
async function flintCopy(text) {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.style.position = 'fixed';
  ta.style.opacity = '0';
  document.body.appendChild(ta);
  ta.select();
  document.execCommand('copy');
  ta.remove();
}

function flintFlashCopied(el) {
  el.classList.add('copied');
  setTimeout(() => el.classList.remove('copied'), 1500);
}

// Configured as soon as this script runs, not on DOMContentLoaded: Alpine
// (also deferred) starts first and renders a page's saved messages before
// that event, so they came out with marked's defaults - no code-block bar,
// no link attributes - while streamed ones were fine. marked, DOMPurify and
// temml are loaded before this file.

const isExternalURL = (url) => /^(https?:)?\/\//i.test((url || '').trim());

function flintRenderMath(tex, displayMode) {
  try {
    return temml.renderToString(tex, { displayMode, throwOnError: false });
  } catch (e) {
    return `<code>${flintEscapeHTML(tex)}</code>`;
  }
}

// Math is taken out before markdown sees it, since markdown would eat the
// backslashes in \( \) and \[ \]. A single $...$ follows pandoc's rule -
// no space just inside either $, and no digit right after the closing one
// - so "costs $5 and $10" stays plain text.
const mathExtensions = [
  {
    name: 'mathBlock',
    level: 'block',
    start: (src) => src.match(/\$\$|\\\[/)?.index,
    tokenizer(src) {
      const m = /^(?:\$\$([\s\S]+?)\$\$|\\\[([\s\S]+?)\\\])[ \t]*(?:\n|$)/.exec(src);
      if (m) return { type: 'mathBlock', raw: m[0], text: (m[1] ?? m[2]).trim() };
    },
    renderer: (token) => `<div class="flint-math-block">${flintRenderMath(token.text, true)}</div>`,
  },
  {
    name: 'mathInline',
    level: 'inline',
    start: (src) => {
      const i = src.search(/\$|\\\(/);
      return i === -1 ? undefined : i;
    },
    tokenizer(src) {
      let m = /^\\\(([\s\S]+?)\\\)/.exec(src);
      if (m) return { type: 'mathInline', raw: m[0], text: m[1].trim(), display: false };
      m = /^\$\$([\s\S]+?)\$\$/.exec(src);
      if (m) return { type: 'mathInline', raw: m[0], text: m[1].trim(), display: true };
      m = /^\$(?=\S)([^$\n]*?\S)\$(?!\d)/.exec(src);
      if (m) return { type: 'mathInline', raw: m[0], text: m[1], display: false };
    },
    renderer: (token) => flintRenderMath(token.text, token.display),
  },
];

marked.use({
  breaks: true,
  extensions: mathExtensions,
  renderer: {
    code({ text, lang }) {
      const label = (lang || '').split(/\s/)[0];
      return `<div class="flint-code"><div class="flint-code-bar"><span>${flintEscapeHTML(label || 'code')}</span>` +
        `<button type="button" class="flint-code-copy">Copy</button></div>` +
        `<pre><code>${flintEscapeHTML(text)}</code></pre></div>`;
    },
    // Flint promises nothing leaves the machine unless asked; a remote
    // image would be fetched just by rendering the reply. It becomes a link
    // the user can choose to open.
    image({ href, text }) {
      if (!isExternalURL(href)) return false;
      return `<a href="${flintEscapeHTML(href)}">[image: ${flintEscapeHTML(text || href)}]</a>`;
    },
  },
});

// Raw HTML in a reply gets the same rule: no SVG (its <image> fetches), no
// media tags or style/srcset/poster (all can fetch), and <img> keeps only a
// local source.
const SANITIZE_CONFIG = {
  USE_PROFILES: { html: true, mathMl: true },
  FORBID_TAGS: ['video', 'audio', 'source', 'track', 'picture'],
  FORBID_ATTR: ['style', 'srcset', 'poster', 'background'],
};

DOMPurify.addHook('afterSanitizeAttributes', (node) => {
  if (node.tagName === 'A') {
    node.setAttribute('target', '_blank');
    node.setAttribute('rel', 'noopener noreferrer');
  }
  if (node.tagName === 'IMG' && isExternalURL(node.getAttribute('src'))) {
    node.removeAttribute('src');
  }
});

document.addEventListener('click', async (e) => {
  const btn = e.target.closest('.flint-code-copy');
  if (!btn) return;
  await flintCopy(btn.closest('.flint-code').querySelector('code').textContent);
  btn.textContent = 'Copied';
  setTimeout(() => (btn.textContent = 'Copy'), 1500);
});

function flintRenameConversation(id, button) {
  const row = button.closest('.flint-conversation-row');
  const link = row.querySelector('a');
  const input = document.createElement('input');
  input.className = 'flint-conversation-rename';
  input.value = link.textContent;
  input.maxLength = 60;
  input.setAttribute('aria-label', 'Conversation name');
  row.classList.add('renaming');
  link.hidden = true;
  row.insertBefore(input, link);
  input.focus();
  input.select();

  let done = false;
  const finish = async (save) => {
    if (done) return;
    done = true;
    const title = input.value.trim();
    if (save && title && title !== link.textContent) {
      const res = await fetch(`/api/conversations/${id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title }),
      });
      if (res.ok) link.textContent = link.title = (await res.json()).title;
    }
    input.remove();
    link.hidden = false;
    row.classList.remove('renaming');
  };
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      finish(true);
    } else if (e.key === 'Escape') {
      finish(false);
    }
  });
  input.addEventListener('blur', () => finish(true));
}

document.addEventListener('alpine:init', () => {
  const TOOL_CALL_MARKER = '<<<TOOL_CALL>>>';
  const TOOL_RESULT_MARKER = '<<<TOOL_RESULT>>>';
  const STATS_MARKER = '<<<STATS>>>';
  const THINK_MARKER = '<<<THINK>>>';
  const LOADING_MARKER = '<<<LOADING>>>';
  const CONTEXT_MARKER = '<<<CONTEXT>>>';
  const MEMORY_DRAFT_MARKER = '<<<MEMORY_DRAFT>>>';
  const SEARCHING_MARKER = '<<<SEARCHING>>>';
  const SOURCES_MARKER = '<<<SOURCES>>>';
  const MEMORY_SAVED_MARKER = '<<<MEMORY_SAVED>>>';
  const AGENT_PLAN_MARKER = '<<<AGENT_PLAN>>>';
  const MARKERS = [TOOL_CALL_MARKER, STATS_MARKER, THINK_MARKER, LOADING_MARKER, CONTEXT_MARKER, MEMORY_DRAFT_MARKER, SEARCHING_MARKER, SOURCES_MARKER, MEMORY_SAVED_MARKER, AGENT_PLAN_MARKER];
  // One JSON value per line, consumed in place while the stream continues.
  const LINE_MARKERS = [CONTEXT_MARKER, THINK_MARKER, SEARCHING_MARKER, SOURCES_MARKER, MEMORY_SAVED_MARKER];
  const COMMANDS = [
    { cmd: '@agent ', hint: "split a task over the folder's files into separate agents" },
    { cmd: '@web ', hint: 'search the web first (needs a Brave key in Settings)' },
    { cmd: '@memory ', hint: 'recall saved memories' },
    { cmd: '@memory save', hint: 'keep what matters from this chat for later ones' },
    { cmd: '@compact', hint: 'condense older messages now' },
  ];
  const LONGEST_MARKER = Math.max(...MARKERS.map((m) => m.length));
  // Mirrors imageMimeExtensions in images.go.
  const SUPPORTED_IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'image/bmp'];
  const firstMarker = (s) => {
    const found = MARKERS.map((m) => s.indexOf(m)).filter((i) => i !== -1);
    return found.length ? Math.min(...found) : -1;
  };
  // Only a tail that could still grow into a marker is held back, so
  // ordinary text shows the moment it arrives instead of lagging behind.
  const partialMarkerAt = (s) => {
    for (let i = Math.max(0, s.length - LONGEST_MARKER); i < s.length; i++) {
      const tail = s.slice(i);
      if (MARKERS.some((m) => m.startsWith(tail))) return i;
    }
    return s.length;
  };

  Alpine.data('chatApp', (config) => ({
    conversationId: config.conversationId,
    model: config.model,
    attachedFolder: config.attachedFolder,
    timeline: config.timeline || [],
    input: '',
    attachments: [],
    attachmentError: '',
    streaming: false,
    replyingTo: false,
    changingFolder: false,
    folderInput: '',
    folderSuggestions: [],
    abortController: null,
    canThink: config.canThink,
    canSee: config.canSee,
    lastActive: config.lastActive,
    contextUsed: config.contextUsed,
    contextMax: config.contextMax,
    condensed: config.condensed,
    thinkOn: localStorage.getItem('flint-think') !== '0',
    editingIndex: null,
    editText: '',
    browser: { path: '', parent: '', dirs: [], error: '', loading: false },
    folderBusy: false,
    folderError: '',
    streamingBubble: null,
    modelLoading: false,
    searchingQuery: '',
    commandIndex: 0,
    suggestDismissed: false,

    init() {
      this.scrollToBottom();
    },

    get pendingCommand() {
      const last = this.timeline[this.timeline.length - 1];
      return last && last.kind === 'command' && last.commandStatus === 'pending' ? last : null;
    },

    // Model output is untrusted: everything marked produces goes through
    // DOMPurify before it touches the DOM.
    renderMarkdown(text) {
      return DOMPurify.sanitize(marked.parse(text || ''), SANITIZE_CONFIG);
    },

    // Search results come from the web, so only http(s) links are ever
    // turned into anchors; anything else would be a script-URL risk.
    sourceURL(url) {
      return /^https?:\/\//i.test(url || '') ? url : null;
    },

    sourceHost(url) {
      try {
        return new URL(url).hostname.replace(/^www\./, '');
      } catch (e) {
        return '';
      }
    },

    async copyText(text, el) {
      await flintCopy(text);
      flintFlashCopied(el);
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

      const tooBig = [];
      const unreadable = [];
      for (const f of accepted) {
        if (f.size > 8 * 1024 * 1024) {
          tooBig.push(f.name);
          continue;
        }
        // HEIC, TIFF, SVG, AVIF... would reach the model as a bare file
        // name, which is never what someone attaching a picture wants.
        if (f.type.startsWith('image/') && !SUPPORTED_IMAGE_TYPES.includes(f.type)) {
          unreadable.push(f.name);
          continue;
        }
        const isImage = SUPPORTED_IMAGE_TYPES.includes(f.type);
        const reader = new FileReader();
        reader.onload = () => this.attachments.push({ dataUrl: reader.result, filename: f.name, isImage });
        reader.readAsDataURL(f);
      }

      const problems = [];
      if (tooBig.length) problems.push(`${tooBig.join(', ')} ${tooBig.length > 1 ? 'are' : 'is'} over the 8 MB limit.`);
      if (unreadable.length) problems.push(`${unreadable.join(', ')} ${unreadable.length > 1 ? 'are image formats' : 'is an image format'} the model can't read - convert to PNG, JPEG, WebP, GIF or BMP.`);
      if (problems.length) this.attachmentError = `Skipped: ${problems.join(' ')}`;
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

    // Not blocking: the file is still kept with the message, the user just
    // shouldn't expect the model to have looked at it.
    get attachmentWarning() {
      const warnings = [];
      if (!this.canSee && this.attachments.some((a) => a.isImage)) {
        warnings.push(`${this.model} can't see images - it will only get the file name. Switch to a vision model to ask about the picture.`);
      }
      const files = this.attachments.filter((a) => !a.isImage).map((a) => a.filename);
      if (files.length) {
        warnings.push(`The model can't open ${files.join(', ')} - it only sees the name. Paste the text into your message if you want it read.`);
      }
      return warnings.join(' ');
    },

    removeAttachment(i) {
      this.attachments.splice(i, 1);
    },

    async listDirs(path) {
      const res = await fetch('/api/fs/dirs' + (path ? '?path=' + encodeURIComponent(path) : ''));
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error || 'Could not open that folder.');
      return data;
    },

    async browseTo(path) {
      this.browser.loading = true;
      this.browser.error = '';
      try {
        const data = await this.listDirs(path);
        Object.assign(this.browser, { path: data.path, parent: data.parent, dirs: data.dirs });
      } catch (e) {
        this.browser.error = e.message;
      } finally {
        this.browser.loading = false;
      }
    },

    openFolderBrowser() {
      this.$refs.folderDialog.showModal();
      this.browseTo(this.folderInput.trim());
    },

    useBrowsedFolder() {
      this.folderInput = this.browser.path;
      this.$refs.folderDialog.close();
      this.attachFolder();
    },

    // Suggests subfolders of whatever directory the typed path is inside,
    // filtered by the partial name after the last separator.
    async suggestFolders() {
      const typed = this.folderInput;
      const cut = Math.max(typed.lastIndexOf('/'), typed.lastIndexOf('\\'));
      if (cut < 0) {
        this.folderSuggestions = [];
        return;
      }
      const base = typed.slice(0, cut + 1);
      const partial = typed.slice(cut + 1).toLowerCase();
      try {
        const data = await this.listDirs(base);
        this.folderSuggestions = data.dirs
          .filter((d) => d.name.toLowerCase().startsWith(partial))
          .slice(0, 20)
          .map((d) => d.path);
      } catch (e) {
        this.folderSuggestions = [];
      }
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
        this.changingFolder = false;
        // A change rewrites the existing manifest on the server, so update its
        // card in place too rather than adding a second one.
        const content = `Attached folder: ${data.folder}\n${(data.files || []).length} file(s) included.`;
        const card = this.timeline.find((t) => t.kind === 'system' && t.content.startsWith('Attached folder: '));
        if (card) card.content = content;
        else this.timeline.push({ kind: 'system', content });
        this.scrollToBottom();
      } catch (e) {
        this.folderError = 'Could not reach the server.';
      } finally {
        this.folderBusy = false;
      }
    },

    async send() {
      const content = this.input.trim();
      if (!content || this.streaming || (this.pendingCommand && !this.replyingTo) || !this.conversationId) return;
      // "Reply instead": deny the pending command without letting the model
      // continue, then send this as an ordinary message it responds to.
      const cmd = this.pendingCommand;
      if (cmd) {
        const res = await fetch(`/api/conversations/${this.conversationId}/commands/${cmd.commandId}/deny?reply=1`, { method: 'POST' }).catch(() => null);
        if (!res || !res.ok) {
          this.timeline.push({ kind: 'system', content: 'Error: could not deny the command. Try again.' });
          return;
        }
        cmd.commandStatus = 'denied';
        this.replyingTo = false;
      }

      const attachments = this.attachments.map((a) => ({ data: a.dataUrl, filename: a.filename }));
      this.timeline.push({ kind: 'user', content, pendingAttachments: this.attachments });
      this.input = '';
      this.attachments = [];
      this.attachmentError = '';
      this.scrollToBottom();

      await this.streamTurn('POST', `/api/conversations/${this.conversationId}/messages${this.thinkQuery}`, { content, attachments });
    },

    // e.g. "9/25/26 12:02 AM", in the viewer's local time zone.
    formatDate(ms) {
      return new Date(ms)
        .toLocaleString('en-US', { month: 'numeric', day: 'numeric', year: '2-digit', hour: 'numeric', minute: '2-digit' })
        .replace(',', '');
    },

    formatTokens(n) {
      return n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n);
    },

    toggleThink() {
      this.thinkOn = !this.thinkOn;
      localStorage.setItem('flint-think', this.thinkOn ? '1' : '0');
    },

    // Omitted entirely for models without thinking: Ollama rejects
    // think:true there instead of ignoring it.
    get thinkQuery() {
      return this.canThink ? `?think=${this.thinkOn ? 1 : 0}` : '';
    },

    get lastUserIndex() {
      for (let i = this.timeline.length - 1; i >= 0; i--) {
        if (this.timeline[i].kind === 'user') return i;
      }
      return -1;
    },

    startEdit(index) {
      this.editingIndex = index;
      this.editText = this.timeline[index].content;
      this.$nextTick(() => {
        const el = document.querySelector('.flint-edit-box textarea');
        if (el) {
          el.focus();
          el.setSelectionRange(el.value.length, el.value.length);
        }
      });
    },

    cancelEdit() {
      this.editingIndex = null;
      this.editText = '';
    },

    // Mirrors the server: the edited message's reply and everything after
    // it go, along with @web results injected for the old text.
    async saveEdit() {
      const index = this.editingIndex;
      const content = this.editText.trim();
      const item = this.timeline[index];
      this.cancelEdit();
      if (!content || content === item.content || this.streaming) return;

      item.content = content;
      this.timeline.splice(index + 1);
      let start = index;
      while (start > 0 && this.timeline[start - 1].kind === 'system' && this.timeline[start - 1].content.startsWith('Web search results for ')) {
        start--;
      }
      this.timeline.splice(start, index - start);
      this.scrollToBottom();

      await this.streamTurn('PUT', `/api/conversations/${this.conversationId}/messages/last${this.thinkQuery}`, { content });
    },

    editKeydown(e) {
      if (e.key === 'Escape') {
        this.cancelEdit();
      } else if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        this.saveEdit();
      }
    },

    async streamTurn(method, url, payload) {
      this.streaming = true;
      this.abortController = new AbortController();
      try {
        const res = await fetch(url, {
          method,
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
          signal: this.abortController.signal,
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          this.timeline.push({ kind: 'system', content: `Error: ${data.error || res.statusText}` });
          return;
        }
        await this.consumeStream(res.body, { expectResultMarker: false });
        this.refreshTitle();
      } catch (e) {
        if (e.name === 'AbortError') {
          this.finishStopped();
        } else {
          this.timeline.push({ kind: 'system', content: 'Error: could not reach the server.' });
        }
      } finally {
        this.streaming = false;
        this.streamingBubble = null;
        this.abortController = null;
        this.lastActive = Date.now();
        this.modelLoading = false;
        this.searchingQuery = '';
      }
    },

    // Aborting the fetch closes the connection; the server sees its request
    // context cancelled, which cancels the call to Ollama and keeps
    // whatever part of the reply had already arrived.
    stop() {
      if (this.abortController) this.abortController.abort();
    },

    finishStopped() {
      const bubble = this.streamingBubble;
      if (bubble && bubble.content.trim() === '' && !bubble.thinking) {
        this.timeline.splice(this.timeline.indexOf(bubble), 1);
      }
      this.refreshTitle();
    },

    async approve(cmd) {
      await this.decide(cmd, 'approve');
    },

    // A drafted memory is only saved once the user approves it here, since
    // a small model's draft can be wrong and would resurface in later chats.
    async saveMemory(item) {
      item.memoryError = '';
      item.memoryStatus = 'saving';
      try {
        const res = await fetch('/api/memories', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ content: item.memoryText, conversation_id: this.conversationId }),
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          item.memoryError = data.error || res.statusText;
          item.memoryStatus = 'draft';
          return;
        }
        // Becomes the same card a reload shows at this spot.
        item.kind = 'memorySaved';
        item.content = item.memoryText;
      } catch (e) {
        item.memoryError = 'Could not reach the server.';
        item.memoryStatus = 'draft';
      }
    },

    agentRunLabel(status) {
      return { planned: 'Review before running', running: 'Running…', done: 'Done', failed: 'Failed', cancelled: 'Stopped', discarded: 'Discarded' }[status] || status;
    },

    addableFiles(run, agent) {
      return (run.folder_files || []).filter((f) => !agent.files.includes(f));
    },

    // Agents that can explore may also propose commands, each one more
    // model call and one more approval, up to the account's limit.
    agentEstimate(run) {
      const n = run.agents.length;
      const web = run.agents.filter((a) => a.webOn && a.web_query.trim() && !a.files.length).length;
      let text = `${n} agent${n === 1 ? '' : 's'}, then 1 call to combine their results: ${n + 1} model calls`;
      if (web) text += `, ${web} web search${web === 1 ? '' : 'es'}`;
      if (run.can_explore) text += `. Agents may also ask to run up to ${run.max_commands} commands each, every one needing your approval`;
      return text + '.';
    },

    // The server validates the edited plan again against the folder, so
    // whatever is typed here can only narrow what the agents may read.
    async runPlan(item) {
      item.planError = '';
      const agents = item.run.agents.map((a) => ({ task: a.task, files: a.files, web_query: a.webOn ? a.web_query : '' }));
      try {
        const res = await fetch(`/api/agent-runs/${item.run.id}/run`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ agents }),
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          item.planError = data.error || res.statusText;
        }
      } catch (e) {
        item.planError = 'Could not reach the server.';
      }
    },

    async discardPlan(item) {
      item.planError = '';
      try {
        const res = await fetch(`/api/agent-runs/${item.run.id}/discard`, { method: 'POST' });
        if (res.ok || res.status === 409) {
          item.run.status = 'discarded';
        } else {
          const data = await res.json().catch(() => ({}));
          item.planError = data.error || res.statusText;
        }
      } catch (e) {
        item.planError = 'Could not reach the server.';
      }
    },

    async deny(cmd) {
      await this.decide(cmd, 'deny');
    },

    async decide(cmd, action) {
      this.replyingTo = false;
      this.streaming = true;
      this.abortController = new AbortController();
      cmd.commandStatus = 'resolving';
      try {
        const res = await fetch(`/api/conversations/${this.conversationId}/commands/${cmd.commandId}/${action}${this.thinkQuery}`, {
          method: 'POST',
          signal: this.abortController.signal,
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          cmd.commandStatus = 'unknown';
          this.timeline.push({ kind: 'system', content: `Error: ${data.error || res.statusText}` });
          return;
        }
        await this.consumeStream(res.body, { expectResultMarker: true, targetCmd: cmd });
      } catch (e) {
        if (e.name === 'AbortError') {
          if (cmd.commandStatus === 'resolving') cmd.commandStatus = 'unknown';
          this.finishStopped();
        } else {
          cmd.commandStatus = 'unknown';
          this.timeline.push({ kind: 'system', content: 'Error: could not reach the server.' });
        }
      } finally {
        this.streaming = false;
        this.streamingBubble = null;
        this.abortController = null;
        this.lastActive = Date.now();
        this.modelLoading = false;
        this.searchingQuery = '';
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
          this.timeline.push({ kind: 'assistant', content: '', thinking: '' });
          bubble = this.timeline[this.timeline.length - 1];
          this.streamingBubble = bubble;
          this.modelLoading = false;
          this.searchingQuery = '';
        }
        return bubble;
      };
      const appendVisible = (text) => {
        if (!text) return;
        ensureBubble().content += text;
        this.scrollToBottom();
      };

      // Thinking and context lines are consumed in place and streaming
      // continues; a tool-call or stats marker ends the visible text for
      // this response.
      const drain = () => {
        while (!markerFound) {
          const idx = firstMarker(pending);
          if (idx === -1) {
            const safeLen = partialMarkerAt(pending);
            appendVisible(pending.slice(0, safeLen));
            pending = pending.slice(safeLen);
            return;
          }
          appendVisible(pending.slice(0, idx));
          pending = pending.slice(idx);
          if (pending.startsWith(LOADING_MARKER)) {
            this.modelLoading = true;
            pending = pending.slice(LOADING_MARKER.length).replace(/^\n/, '');
            continue;
          }
          const lineMarker = LINE_MARKERS.find((m) => pending.startsWith(m));
          if (!lineMarker) {
            markerFound = true;
            return;
          }
          const nl = pending.indexOf('\n');
          if (nl === -1) return; // rest of this line hasn't arrived yet
          try {
            const value = JSON.parse(pending.slice(lineMarker.length, nl));
            if (lineMarker === CONTEXT_MARKER) {
              this.contextUsed = value.used;
              this.contextMax = value.max;
              this.condensed = value.condensed;
            } else if (lineMarker === SEARCHING_MARKER) {
              this.searchingQuery = value.query;
            } else if (lineMarker === MEMORY_SAVED_MARKER) {
              this.timeline.push({ kind: 'memorySaved', content: value.content });
              this.scrollToBottom();
            } else if (lineMarker === SOURCES_MARKER) {
              this.searchingQuery = '';
              this.timeline.push({ kind: 'sources', query: value.query, sources: value.sources });
              this.scrollToBottom();
            } else {
              ensureBubble().thinking += value;
            }
          } catch (e) {
            // a malformed line only loses that fragment of reasoning, one
            // context-bar update, or the search status
          }
          pending = pending.slice(nl + 1);
        }
      };

      while (true) {
        drain();
        const done = await readChunk();
        pending += buf;
        buf = '';
        if (done) break;
      }
      drain();

      this.streamingBubble = null;

      if (markerFound && pending.startsWith(AGENT_PLAN_MARKER)) {
        try {
          const run = JSON.parse(pending.slice(AGENT_PLAN_MARKER.length).trim());
          if (bubble && bubble.content.trim() === '' && !bubble.thinking) {
            this.timeline.splice(this.timeline.indexOf(bubble), 1);
          }
          this.timeline.push({ kind: 'agentPlan', run, planError: '' });
        } catch (e) {
          appendVisible('\n[Could not read the agent plan.]');
        }
      } else if (markerFound && pending.startsWith(MEMORY_DRAFT_MARKER)) {
        try {
          const obj = JSON.parse(pending.slice(MEMORY_DRAFT_MARKER.length).trim());
          if (bubble && bubble.content.trim() === '' && !bubble.thinking) {
            this.timeline.splice(this.timeline.indexOf(bubble), 1);
          }
          this.timeline.push({ kind: 'memory', memoryText: obj.text, memoryStatus: 'draft', memoryError: '' });
        } catch (e) {
          appendVisible('\n[Could not read the memory draft.]');
        }
      } else if (markerFound && pending.startsWith(STATS_MARKER)) {
        try {
          const obj = JSON.parse(pending.slice(STATS_MARKER.length).trim());
          if (bubble) bubble.tokensPerSec = obj.tokensPerSec;
        } catch (e) {
          // stats are cosmetic - a malformed line just means none are shown
        }
      } else if (markerFound) {
        const jsonPart = pending.slice(TOOL_CALL_MARKER.length).trim();
        try {
          const obj = JSON.parse(jsonPart);
          if (bubble && bubble.content.trim() === '' && !bubble.thinking) {
            this.timeline.splice(this.timeline.indexOf(bubble), 1);
          }
          this.timeline.push({ kind: 'command', commandId: obj.id, commandText: obj.command, commandStatus: 'pending' });
        } catch (e) {
          appendVisible('\n' + pending);
        }
      } else {
        appendVisible(pending);
        if (bubble && bubble.content.trim() === '' && !bubble.thinking) {
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
        if (link) link.textContent = link.title = data.title;
      } catch (e) {
        // non-critical - the sidebar just keeps its stale title until reload
      }
    },

    // Shown while only the command word is typed, so the commands can be
    // found without reading the tips.
    get commandSuggestions() {
      const typed = this.input.toLowerCase();
      if (this.suggestDismissed || !/^@\w*$/.test(typed)) return [];
      return COMMANDS.filter((c) => c.cmd.startsWith(typed) && c.cmd !== typed);
    },

    pickCommand(c) {
      this.input = c.cmd;
      this.commandIndex = 0;
      this.$nextTick(() => this.$refs.input.focus());
    },

    commandKeydown(e) {
      const list = this.commandSuggestions;
      if (!list.length) return;
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        this.commandIndex = (this.commandIndex + (e.key === 'ArrowDown' ? 1 : list.length - 1)) % list.length;
      } else if (e.key === 'Tab') {
        e.preventDefault();
        this.pickCommand(list[this.commandIndex]);
      } else if (e.key === 'Escape') {
        this.suggestDismissed = true;
      }
    },

    submitOnEnter(e) {
      if (e.shiftKey) return;
      const list = this.commandSuggestions;
      if (list.length) {
        e.preventDefault();
        this.pickCommand(list[this.commandIndex]);
        return;
      }
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
