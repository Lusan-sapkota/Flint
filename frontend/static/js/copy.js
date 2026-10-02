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

document.addEventListener('click', async (e) => {
  const btn = e.target.closest('.flint-code-copy');
  if (!btn) return;
  await flintCopy(btn.closest('.flint-code').querySelector('code').textContent);
  btn.textContent = 'Copied';
  setTimeout(() => (btn.textContent = 'Copy'), 1500);
});
