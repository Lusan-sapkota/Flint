function formatBytes(n) {
  if (!n || n <= 0) return '';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i += 1;
  }
  return `${n.toFixed(n >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
}

document.addEventListener('alpine:init', () => {
  const TABS = [
    { id: 'connection', label: 'Connection' },
    { id: 'models', label: 'Models' },
    { id: 'memories', label: 'Memories' },
    { id: 'account', label: 'Account' },
  ];
  const tabFromHash = () => (TABS.some((t) => `#${t.id}` === location.hash) ? location.hash.slice(1) : TABS[0].id);

  Alpine.data('settingsForm', (config) => ({
    tabs: TABS,
    tab: tabFromHash(),
    ollamaBaseURL: config.ollamaBaseURL || '',
    braveApiKey: config.braveApiKey || '',
    preferredModels: config.preferredModels || [],
    existingQuestions: config.questions || [],
    editQuestions: (config.questions || []).map((q) => ({ question: q.question, answer: '' })),
    savingGeneral: false,
    generalStatus: '',
    savingQuestions: false,
    questionsStatus: '',
    questionsError: '',
    deletePassword: '',
    deleteAnswers: [],
    deletingAccount: false,
    deleteAccountError: '',

    runningModels: [],
    runningLoading: false,
    runningError: '',
    deletingModel: null,
    deleteError: '',
    pullName: '',
    pulling: false,
    pullStatus: '',
    pullPercent: null,
    pullError: '',

    memories: [],
    memoriesLoading: true,
    memoriesError: '',

    init() {
      window.addEventListener('hashchange', () => { this.tab = tabFromHash(); });
      this.refreshRunning();
      this.loadMemories();
    },

    async loadMemories() {
      try {
        const res = await fetch('/api/memories');
        if (!res.ok) throw new Error(res.statusText);
        this.memories = (await res.json()).map((m) => ({ ...m, status: '' }));
      } catch (e) {
        this.memoriesError = 'Could not load memories.';
      } finally {
        this.memoriesLoading = false;
      }
    },

    async saveMemory(m) {
      m.status = 'Saving…';
      try {
        const res = await fetch(`/api/memories/${m.id}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ content: m.content }),
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          m.status = data.error || res.statusText;
          return;
        }
        m.status = 'Saved';
      } catch (e) {
        m.status = 'Could not reach the server.';
      }
    },

    async deleteMemory(m) {
      if (!(await flintConfirmDelete('Delete memory?', 'It will no longer be recalled in any chat. This cannot be undone.'))) return;
      try {
        const res = await fetch(`/api/memories/${m.id}`, { method: 'DELETE' });
        if (!res.ok && res.status !== 404) {
          m.status = res.statusText;
          return;
        }
        this.memories = this.memories.filter((x) => x.id !== m.id);
      } catch (e) {
        m.status = 'Could not reach the server.';
      }
    },

    async refreshRunning() {
      this.runningLoading = true;
      this.runningError = '';
      try {
        const res = await fetch('/api/models/running');
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          this.runningError = data.error || 'Could not reach Ollama.';
          return;
        }
        const data = await res.json();
        this.runningModels = (data.models || []).map((m) => ({
          name: m.name || m.model,
          sizeLabel: formatBytes(m.size_vram || m.size),
        }));
      } catch (e) {
        this.runningError = 'Could not reach the server.';
      } finally {
        this.runningLoading = false;
      }
    },

    async showModel(name) {
      const res = await fetch('/api/models/show', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) return `Error: ${data.error || res.statusText}`;
      return JSON.stringify(data, null, 2);
    },

    async deleteModel(name) {
      if (!(await flintConfirmDelete(`Delete ${name}?`, "This removes the model from the Ollama server itself, not just from Flint. You'd have to pull it again to use it."))) return;
      this.deleteError = '';
      this.deletingModel = name;
      try {
        const res = await fetch('/api/models', {
          method: 'DELETE',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name }),
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          this.deleteError = data.error || 'Could not delete that model.';
          return;
        }
        window.location.reload();
      } catch (e) {
        this.deleteError = 'Could not reach the server.';
      } finally {
        this.deletingModel = null;
      }
    },

    async pullModel() {
      const name = this.pullName.trim();
      if (!name || this.pulling) return;
      this.pulling = true;
      this.pullError = '';
      this.pullStatus = 'Starting…';
      this.pullPercent = null;
      try {
        const res = await fetch('/api/models/pull', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name }),
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          this.pullError = data.error || 'Could not reach Ollama.';
          return;
        }

        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buf = '';
        let sawSuccess = false;
        while (true) {
          const { done, value } = await reader.read();
          if (value) buf += decoder.decode(value, { stream: true });
          let nl;
          while ((nl = buf.indexOf('\n')) !== -1) {
            const line = buf.slice(0, nl).trim();
            buf = buf.slice(nl + 1);
            if (!line) continue;
            let obj;
            try {
              obj = JSON.parse(line);
            } catch (e) {
              continue;
            }
            if (obj.error) {
              this.pullError = obj.error;
              continue;
            }
            this.pullStatus = obj.status || this.pullStatus;
            if (obj.total) {
              this.pullPercent = Math.round(((obj.completed || 0) / obj.total) * 100);
            }
            if (obj.status === 'success') sawSuccess = true;
          }
          if (done) break;
        }
        if (sawSuccess && !this.pullError) {
          this.pullStatus = 'Done.';
          window.location.reload();
        } else if (!this.pullError) {
          this.pullError = 'The pull did not complete. Check the model name and try again.';
          this.pullStatus = '';
        }
      } catch (e) {
        this.pullError = 'Could not reach the server.';
      } finally {
        this.pulling = false;
      }
    },

    isPreferred(name) {
      return this.preferredModels.includes(name);
    },
    toggleModel(name) {
      const i = this.preferredModels.indexOf(name);
      if (i === -1) this.preferredModels.push(name);
      else this.preferredModels.splice(i, 1);
    },

    async saveGeneral() {
      this.savingGeneral = true;
      this.generalStatus = '';
      try {
        const res = await fetch('/api/me/settings', {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            ollama_base_url: this.ollamaBaseURL.trim() || null,
            preferred_models: this.preferredModels,
            brave_api_key: this.braveApiKey.trim() || null,
          }),
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          this.generalStatus = `Error: ${data.error || res.statusText}`;
          return;
        }
        this.generalStatus = 'Saved.';
      } catch (e) {
        this.generalStatus = 'Could not reach the server.';
      } finally {
        this.savingGeneral = false;
      }
    },

    addQuestionRow() {
      if (this.editQuestions.length >= 5) return;
      this.editQuestions.push({ question: '', answer: '' });
    },
    removeQuestionRow(i) {
      this.editQuestions.splice(i, 1);
    },

    async saveQuestions() {
      this.questionsError = '';
      if (this.editQuestions.length < 2 || this.editQuestions.length > 5) {
        this.questionsError = 'Provide between 2 and 5 questions.';
        return;
      }
      if (this.editQuestions.some((q) => !q.question.trim() || !q.answer.trim())) {
        this.questionsError = 'Each question needs both text and an answer.';
        return;
      }

      this.savingQuestions = true;
      this.questionsStatus = '';
      try {
        const res = await fetch('/api/me/security-questions', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            questions: this.editQuestions.map((q) => ({ question: q.question.trim(), answer: q.answer })),
          }),
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          this.questionsError = data.error || res.statusText;
          return;
        }
        this.existingQuestions = this.editQuestions.map((q) => ({ question: q.question.trim() }));
        this.editQuestions = this.existingQuestions.map((q) => ({ question: q.question, answer: '' }));
        this.questionsStatus = 'Saved. All prior answers were replaced.';
        this.deleteAnswers = [];
      } catch (e) {
        this.questionsError = 'Could not reach the server.';
      } finally {
        this.savingQuestions = false;
      }
    },

    async deleteAccount() {
      this.deleteAccountError = '';
      const answers = this.existingQuestions.map((_, i) => this.deleteAnswers[i] || '');
      if (!this.deletePassword || answers.some((a) => !a.trim())) {
        this.deleteAccountError = this.existingQuestions.length
          ? 'Enter your password and answer every question.'
          : 'Enter your password.';
        return;
      }
      if (!(await flintConfirmDelete('Delete your account?', 'Every chat, attachment, memory and setting in this account is deleted for good. This cannot be undone.'))) return;

      this.deletingAccount = true;
      try {
        const res = await fetch('/api/me', {
          method: 'DELETE',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ password: this.deletePassword, answers }),
        });
        if (res.status === 204) {
          window.location.href = '/signup';
          return;
        }
        const data = await res.json().catch(() => ({}));
        this.deleteAccountError = data.error || res.statusText;
      } catch (e) {
        this.deleteAccountError = 'Could not reach the server.';
      } finally {
        this.deletingAccount = false;
      }
    },
  }));
});
