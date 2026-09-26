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

// The parts of Ollama's /api/show worth reading, as [label, value] rows.
function modelSummary(data) {
  const details = data.details || {};
  const info = data.model_info || {};
  const arch = info['general.architecture'];
  // A cloud model reports an empty architecture, so its key is ".context_length".
  const context = info[`${arch ?? ''}.context_length`];
  return [
    ['Family', details.family],
    ['Parameters', details.parameter_size],
    ['Quantization', details.quantization_level],
    ['Context length', context && `${context.toLocaleString()} tokens`],
    ['Capabilities', (data.capabilities || []).join(', ')],
  ].filter(([, value]) => value);
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
    numCtx: config.numCtx || '',
    cloudNumCtx: config.cloudNumCtx || '',
    braveKeyHint: config.braveKeyHint,
    braveApiKey: '',
    replacingBraveKey: false,
    preferredModels: config.preferredModels || [],
    existingQuestions: config.questions || [],
    rankingModel: config.rankingModel,
    rankingModelMissing: config.rankingModelMissing,
    editQuestions: (config.questions || []).map((q) => ({ question: q.question, answer: '' })),
    savingGeneral: false,
    generalStatus: '',
    savingQuestions: false,
    questionsStatus: '',
    questionsError: '',
    savedName: config.fullName,
    savedEmail: config.email,
    profileName: config.fullName,
    profileEmail: config.email,
    profilePassword: '',
    savingProfile: false,
    profileStatus: '',
    profileError: '',
    currentPassword: '',
    newPassword: '',
    confirmPassword: '',
    changingPassword: false,
    passwordStatus: '',
    passwordError: '',

    deletePassword: '',
    deleteAnswers: [],
    deletingAccount: false,
    deleteAccountError: '',

    runningModels: [],
    runningLoading: false,
    runningError: '',
    modelBusy: null,
    modelActionError: '',
    deletingModel: null,
    deleteError: '',
    pullName: '',
    pulling: false,
    pullStatus: '',
    pullPercent: null,
    pullError: '',

    webSearches: [],
    webSearchesMonth: 0,
    webSearchesLoading: true,
    webSearchesError: '',

    memories: [],
    memoriesLoading: true,
    memoriesError: '',

    init() {
      window.addEventListener('hashchange', () => { this.tab = tabFromHash(); });
      this.refreshRunning();
      this.loadWebSearches();
      this.loadMemories();
    },

    async loadWebSearches() {
      try {
        const res = await fetch('/api/me/web-searches');
        const data = await res.json().catch(() => ({}));
        if (!res.ok) {
          this.webSearchesError = data.error || res.statusText;
          return;
        }
        this.webSearches = data.searches;
        this.webSearchesMonth = data.this_month;
      } catch (e) {
        this.webSearchesError = 'Could not reach the server.';
      } finally {
        this.webSearchesLoading = false;
      }
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

    isRunning(name) {
      return this.runningModels.some((m) => m.name === name);
    },

    async loadModel(name) {
      await this.modelAction('/api/models/load', name, `Couldn't load ${name}`);
    },
    async unloadModel(name) {
      await this.modelAction('/api/models/unload', name, `Couldn't unload ${name}`);
    },
    // One load or unload at a time: each can wait on the others finishing.
    async modelAction(url, name, failure) {
      this.modelBusy = name;
      this.modelActionError = '';
      try {
        const res = await fetch(url, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name }),
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          this.modelActionError = `${failure}: ${data.error || res.statusText}`;
        }
      } catch (e) {
        this.modelActionError = 'Could not reach the server.';
      } finally {
        this.modelBusy = null;
        await this.refreshRunning();
      }
    },

    // d is the model row's own Alpine state. A failed load leaves info
    // empty, so opening the row again retries.
    async toggleDetails(d, name) {
      d.open = !d.open;
      if (!d.open || d.info || d.loading) return;
      d.loading = true;
      d.error = '';
      try {
        const res = await fetch('/api/models/show', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name }),
        });
        const data = await res.json().catch(() => ({}));
        if (!res.ok) {
          d.error = `Couldn't load details: ${data.error || res.statusText}`;
          return;
        }
        d.info = modelSummary(data);
      } catch (e) {
        d.error = 'Could not reach the server.';
      } finally {
        d.loading = false;
      }
    },

    // Mirrors isRankingModel in websearch.go.
    isRankingModel(name) {
      return name === this.rankingModel || name.startsWith(`${this.rankingModel}:`);
    },
    pullRankingModel() {
      this.pullName = this.rankingModel;
      this.pullModel();
    },

    async deleteModel(name) {
      let message = "This removes the model from the Ollama server itself, not just from Flint. You'd have to pull it again to use it.";
      if (this.isRankingModel(name)) {
        message += " It's optional: Flint only uses it to re-rank @web results. Without it, @web keeps working with Brave's own ranking.";
      }
      if (!(await flintConfirmDelete(`Delete ${name}?`, message))) return;
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

    // The saved key never comes back to the page, so it's only sent when the
    // user typed a new one; an empty box means "keep the current key".
    async saveGeneral() {
      this.savingGeneral = true;
      this.generalStatus = '';
      const newKey = this.braveApiKey.trim();
      const body = {
        ollama_base_url: this.ollamaBaseURL.trim() || null,
        num_ctx: this.numCtx === '' || this.numCtx === null ? null : Number(this.numCtx),
        cloud_num_ctx: this.cloudNumCtx === '' || this.cloudNumCtx === null ? null : Number(this.cloudNumCtx),
        preferred_models: this.preferredModels,
      };
      if (newKey) body.brave_api_key = newKey;
      try {
        const res = await fetch('/api/me/settings', {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          this.generalStatus = `Error: ${data.error || res.statusText}`;
          return;
        }
        if (newKey) {
          this.braveKeyHint = newKey.slice(-4);
          this.braveApiKey = '';
          this.replacingBraveKey = false;
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

    async removeBraveKey() {
      if (!(await flintAsk('Remove your Brave Search key? @web stops working until you add one.'))) return;
      this.generalStatus = '';
      try {
        const res = await fetch('/api/me/settings', {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ brave_api_key: null }),
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          this.generalStatus = `Error: ${data.error || res.statusText}`;
          return;
        }
        this.braveKeyHint = '';
        this.generalStatus = 'Key removed.';
      } catch (e) {
        this.generalStatus = 'Could not reach the server.';
      }
    },

    async saveProfile() {
      this.profileError = '';
      this.profileStatus = '';
      if (!this.profileName.trim() || !this.profileEmail.trim()) {
        this.profileError = 'Enter your name and email.';
        return;
      }
      this.savingProfile = true;
      try {
        const res = await fetch('/api/me/profile', {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ full_name: this.profileName, email: this.profileEmail, password: this.profilePassword }),
        });
        const data = await res.json().catch(() => ({}));
        if (!res.ok) {
          this.profileError = data.error || res.statusText;
          return;
        }
        this.savedName = this.profileName = data.full_name;
        this.savedEmail = this.profileEmail = data.email;
        this.profilePassword = '';
        this.profileStatus = 'Profile saved.';
      } catch (e) {
        this.profileError = 'Could not reach the server.';
      } finally {
        this.savingProfile = false;
      }
    },

    async changePassword() {
      this.passwordError = '';
      this.passwordStatus = '';
      if (!this.currentPassword || !this.newPassword) {
        this.passwordError = 'Enter your current password and a new one.';
        return;
      }
      if (this.newPassword.length < 8) {
        this.passwordError = 'The new password needs at least 8 characters.';
        return;
      }
      if (this.newPassword !== this.confirmPassword) {
        this.passwordError = "The new passwords don't match.";
        return;
      }
      this.changingPassword = true;
      try {
        const res = await fetch('/api/me/password', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ current_password: this.currentPassword, new_password: this.newPassword }),
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          this.passwordError = data.error || res.statusText;
          return;
        }
        this.currentPassword = this.newPassword = this.confirmPassword = '';
        this.passwordStatus = 'Password changed. Your other devices were logged out.';
      } catch (e) {
        this.passwordError = 'Could not reach the server.';
      } finally {
        this.changingPassword = false;
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
