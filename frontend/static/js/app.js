document.addEventListener('alpine:init', () => {
  async function postJSON(url, body) {
    const res = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    let data = {};
    try {
      data = await res.json();
    } catch (e) {
      // no/invalid JSON body (e.g. 204 No Content) - not an error by itself
    }
    return { ok: res.ok, status: res.status, data };
  }

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
    fullName: '',
    email: '',
    password: '',
    error: '',
    loading: false,

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
        window.location.href = '/';
      } catch (e) {
        this.error = 'Could not reach the server.';
      } finally {
        this.loading = false;
      }
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
