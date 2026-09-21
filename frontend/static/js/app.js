document.addEventListener('alpine:init', () => {
  async function requestJSON(method, url, body) {
    const res = await fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    let data = {};
    try {
      data = await res.json();
    } catch (e) {
      // no/invalid JSON body (e.g. 204 No Content) - not an error by itself
    }
    return { ok: res.ok, status: res.status, data };
  }

  const postJSON = (url, body) => requestJSON('POST', url, body);
  const putJSON = (url, body) => requestJSON('PUT', url, body);

  Alpine.data('loginForm', () => ({
    email: '',
    password: '',
    error: '',
    loading: false,

    async submit() {
      this.error = '';
      this.loading = true;
      try {
        const { ok, data } = await postJSON('/api/login', {
          email: this.email,
          password: this.password,
        });
        if (!ok) {
          this.error = data.error || 'Login failed.';
          return;
        }
        window.location.href = '/';
      } catch (e) {
        this.error = 'Could not reach the server.';
      } finally {
        this.loading = false;
      }
    },
  }));

  Alpine.data('signupForm', () => ({
    step: 1,
    fullName: '',
    email: '',
    password: '',
    error: '',
    loading: false,
    questions: [
      { question: '', answer: '' },
      { question: '', answer: '' },
    ],
    questionsError: '',

    async submit() {
      this.error = '';
      this.loading = true;
      try {
        const { ok, data } = await postJSON('/api/signup', {
          full_name: this.fullName,
          email: this.email,
          password: this.password,
        });
        if (!ok) {
          this.error = data.error || 'Signup failed.';
          return;
        }
        this.step = 2;
      } catch (e) {
        this.error = 'Could not reach the server.';
      } finally {
        this.loading = false;
      }
    },

    addQuestionRow() {
      if (this.questions.length >= 5) return;
      this.questions.push({ question: '', answer: '' });
    },
    removeQuestionRow(i) {
      if (this.questions.length <= 2) return;
      this.questions.splice(i, 1);
    },

    async saveQuestions() {
      this.questionsError = '';
      if (this.questions.some((q) => !q.question.trim() || !q.answer.trim())) {
        this.questionsError = 'Each question needs both text and an answer.';
        return;
      }
      this.loading = true;
      try {
        const { ok, data } = await putJSON('/api/me/security-questions', {
          questions: this.questions.map((q) => ({ question: q.question.trim(), answer: q.answer })),
        });
        if (!ok) {
          this.questionsError = data.error || 'Could not save your security questions.';
          return;
        }
        window.location.href = '/';
      } catch (e) {
        this.questionsError = 'Could not reach the server.';
      } finally {
        this.loading = false;
      }
    },

    skipQuestions() {
      window.location.href = '/';
    },
  }));

  Alpine.data('recoverForm', () => ({
    step: 1,
    email: '',
    questions: [],
    answers: [],
    newPassword: '',
    confirmPassword: '',
    error: '',
    loading: false,
    done: false,

    async lookup() {
      this.error = '';
      this.loading = true;
      try {
        const { ok, data } = await postJSON('/api/recovery/questions', { email: this.email });
        const questions = (ok && data.questions) || [];
        if (questions.length === 0) {
          this.error = 'No recovery questions are configured for that email.';
          return;
        }
        this.questions = questions;
        this.answers = questions.map(() => '');
        this.step = 2;
      } catch (e) {
        this.error = 'Could not reach the server.';
      } finally {
        this.loading = false;
      }
    },

    async reset() {
      this.error = '';
      if (this.newPassword.length < 8) {
        this.error = 'Password must be at least 8 characters.';
        return;
      }
      if (this.newPassword !== this.confirmPassword) {
        this.error = 'Passwords do not match.';
        return;
      }
      this.loading = true;
      try {
        const { ok, data } = await postJSON('/api/recovery/reset', {
          email: this.email,
          answers: this.answers,
          new_password: this.newPassword,
        });
        if (!ok) {
          this.error = data.error || 'Incorrect answers.';
          return;
        }
        this.done = true;
      } catch (e) {
        this.error = 'Could not reach the server.';
      } finally {
        this.loading = false;
      }
    },
  }));
});
