function flintAsk(question, detail = '') {
  const dialog = document.getElementById('flint-ask-dialog');
  dialog.querySelector('h3').textContent = question;
  dialog.querySelector('p').textContent = detail;
  dialog.querySelector('p').hidden = !detail;
  dialog.returnValue = '';
  dialog.showModal();
  return new Promise((resolve) => {
    dialog.addEventListener('close', () => resolve(dialog.returnValue === 'yes'), { once: true });
  });
}

async function flintLogout() {
  if (!(await flintAsk('Are you sure you want to log out?'))) return;
  try {
    await fetch('/api/logout', { method: 'POST' });
  } catch (e) {
    // ignore - redirecting anyway
  }
  window.location.href = '/login';
}

function flintConfirmDelete(title, message) {
  const dialog = document.getElementById('flint-confirm-dialog');
  const input = dialog.querySelector('input');
  dialog.querySelector('h3').textContent = title;
  dialog.querySelector('p').textContent = message;
  input.value = '';
  dialog.querySelector('button[value=confirm]').disabled = true;
  dialog.returnValue = '';
  dialog.showModal();
  return new Promise((resolve) => {
    dialog.addEventListener('close', () => resolve(dialog.returnValue === 'confirm' && input.value === 'DELETE'), { once: true });
  });
}

document.addEventListener('input', (e) => {
  if (e.target.id !== 'flint-confirm-typed') return;
  e.target.form.querySelector('button[value=confirm]').disabled = e.target.value !== 'DELETE';
});

document.addEventListener('keydown', (e) => {
  if (e.target.id !== 'flint-confirm-typed' || e.key !== 'Enter') return;
  e.preventDefault();
  if (e.target.value === 'DELETE') e.target.closest('dialog').close('confirm');
});

document.addEventListener('click', (e) => {
  if (e.target.id === 'flint-confirm-dialog' || e.target.id === 'flint-ask-dialog') e.target.close();
});
