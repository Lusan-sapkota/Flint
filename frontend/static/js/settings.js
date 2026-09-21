document.addEventListener('alpine:init', () => {
  Alpine.data('settingsForm', (config) => ({
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
      } catch (e) {
        this.questionsError = 'Could not reach the server.';
      } finally {
        this.savingQuestions = false;
      }
    },
  }));
});
